package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

const (
	capacityKey = "node.kubernetes.io/capacity"
	ondemand    = "on-demand"
	spot        = "spot"

	mixSchedulerKey = "mix-scheduler-admission-webhook"
)

type App struct {
	mixSchedulerRequierd   bool
	notControllerNamespace map[string]struct{}

	SpotNodeAffinityPreferred     corev1.PreferredSchedulingTerm
	OndemandNodeAffinityPreferred corev1.PreferredSchedulingTerm
}

func NewDefaultApp() *App {
	return &App{
		mixSchedulerRequierd:   false,
		notControllerNamespace: map[string]struct{}{},

		// spot node affinity
		SpotNodeAffinityPreferred: corev1.PreferredSchedulingTerm{
			Weight: 10,
			Preference: corev1.NodeSelectorTerm{
				MatchExpressions: []corev1.NodeSelectorRequirement{
					{
						Key:      capacityKey,
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{spot},
					},
				},
			},
		},

		// on-demand node affinity
		OndemandNodeAffinityPreferred: corev1.PreferredSchedulingTerm{
			Weight: 1,
			Preference: corev1.NodeSelectorTerm{
				MatchExpressions: []corev1.NodeSelectorRequirement{
					{
						Key:      capacityKey,
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{ondemand},
					},
				},
			},
		},
	}
}

// isControllerNamespace is controller namespace
func (app *App) isControllerNamespace(namespace string) bool {
	_, ok := app.notControllerNamespace[namespace]
	return !ok
}

// instanceIsSkip skip instance
func (app *App) instanceIsSkip(namespace string, labels map[string]string) bool {
	return !app.isControllerNamespace(namespace) || (app.mixSchedulerRequierd && labels[mixSchedulerKey] != "true")
}

func (app *App) HandleMutate(w http.ResponseWriter, r *http.Request) {
	admissionReview := &admissionv1.AdmissionReview{}

	// read the AdmissionReview from the request json body
	err := readJSON(r, admissionReview)
	if err != nil {
		app.HandleError(w, r, err)
		return
	}

	var (
		affinity *corev1.Affinity
		selector *metav1.LabelSelector
	)

	switch admissionReview.Request.Kind.Kind {
	case "Deployment":
		// unmarshal the deployment from the AdmissionRequest
		deploy := &appsv1.Deployment{}
		if err := json.Unmarshal(admissionReview.Request.Object.Raw, deploy); err != nil {
			app.HandleError(w, r, fmt.Errorf("unmarshal to deploy: %v", err))
			return
		}

		if app.instanceIsSkip(deploy.Namespace, deploy.Labels) {
			jsonOk(w, r)
			return
		}

		selector = deploy.Spec.Selector
		affinity = FillAffinity(deploy.Spec.Template.Spec)
	case "StatefulSet":
		// unmarshal the statefulset from the AdmissionRequest
		sts := &appsv1.StatefulSet{}
		if err := json.Unmarshal(admissionReview.Request.Object.Raw, sts); err != nil {
			app.HandleError(w, r, fmt.Errorf("unmarshal to statefulset: %v", err))
			return
		}

		if app.instanceIsSkip(sts.Namespace, sts.Labels) {
			jsonOk(w, r)
			return
		}

		selector = sts.Spec.Selector
		affinity = FillAffinity(sts.Spec.Template.Spec)
	default:
		klog.Errorf("unknown kind: %s", admissionReview.Request.Object.Object.GetObjectKind().GroupVersionKind().Kind)
		jsonOk(w, r)
		return
	}

	// pod anti-affinity
	podAntiAffinityPreferredWeightedPodAffinityTerm := corev1.WeightedPodAffinityTerm{
		Weight: 1,
		PodAffinityTerm: corev1.PodAffinityTerm{
			TopologyKey:   "kubernetes.io/hostname",
			LabelSelector: selector,
		},
	}

	affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution =
		append(affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution,
			[]corev1.PreferredSchedulingTerm{app.SpotNodeAffinityPreferred, app.OndemandNodeAffinityPreferred}...)

	affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution =
		append(affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution,
			podAntiAffinityPreferredWeightedPodAffinityTerm)

	// marshal the affinity back into the AdmissionReview
	affinityBytes, err := json.Marshal(affinity)
	if err != nil {
		app.HandleError(w, r, fmt.Errorf("marshal affinity: %v", err))
		return
	}

	// create the patch
	patch := []JSONPatchEntry{
		{
			OP:    "replace",
			Path:  "/spec/template/spec/affinity",
			Value: affinityBytes,
		},
	}

	patchBytes, err := json.Marshal(&patch)
	if err != nil {
		app.HandleError(w, r, fmt.Errorf("marshal patch: %v", err))
		return
	}

	patchType := admissionv1.PatchTypeJSONPatch

	// create the AdmissionResponse
	admissionResponse := &admissionv1.AdmissionResponse{
		UID:       admissionReview.Request.UID,
		Allowed:   true,
		Patch:     patchBytes,
		PatchType: &patchType,
	}

	respAdmissionReview := &admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AdmissionReview",
			APIVersion: "admission.k8s.io/v1",
		},
		Response: admissionResponse,
	}

	jsonOk(w, &respAdmissionReview)
}

type JSONPatchEntry struct {
	OP    string          `json:"op"`
	Path  string          `json:"path"`
	Value json.RawMessage `json:"value,omitempty"`
}

func FillAffinity(podSpec corev1.PodSpec) *corev1.Affinity {
	affinity := &corev1.Affinity{}
	if podSpec.Affinity == nil {
		affinity = &corev1.Affinity{
			NodeAffinity: &corev1.NodeAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{},
			},
			PodAntiAffinity: &corev1.PodAntiAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{},
			},
		}
	} else {
		affinity = podSpec.Affinity
	}

	if affinity.NodeAffinity == nil {
		affinity.NodeAffinity = &corev1.NodeAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{},
		}
	}

	if affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution == nil {
		affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution = []corev1.PreferredSchedulingTerm{}
	}

	if affinity.PodAntiAffinity == nil {
		affinity.PodAntiAffinity = &corev1.PodAntiAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{},
		}
	}

	if affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution == nil {
		affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution = []corev1.WeightedPodAffinityTerm{}
	}

	return affinity
}
