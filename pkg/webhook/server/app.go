package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	"github.com/helen-frank/mix-scheduler-admission-webhook/pkg/informermanager"
)

const (
	capacityKey       = "node.kubernetes.io/capacity"
	ondemandKey       = "on-demand"
	spotKey           = "spot"
	spotWeithtKey     = "spot/weight"
	ondemandWeithtKey = "on-demand/weight"

	mixSchedulerKey = "mix-scheduler-admission-webhook"
)

type App struct {
	Client            kubernetes.Interface
	Ctx               context.Context
	OnDemandMinPodNum int
	SpotMinPodNum     int

	mixSchedulerRequierd   bool
	notControllerNamespace map[string]struct{}

	ondemandNodeSelector map[string]string
	spotNodeSelector     map[string]string

	informermanager *informermanager.SingleClusterManager

	stopCh chan struct{}
}

func NewDefaultApp(ctx context.Context) (*App, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return &App{
		Client:                 client,
		Ctx:                    ctx,
		OnDemandMinPodNum:      1,
		SpotMinPodNum:          1,
		mixSchedulerRequierd:   true,
		notControllerNamespace: map[string]struct{}{},

		ondemandNodeSelector: map[string]string{
			capacityKey: ondemandKey,
		},

		spotNodeSelector: map[string]string{
			capacityKey: spotKey,
		},

		informermanager: informermanager.NewSingleClusterManager(ctx, client),
		stopCh:          make(chan struct{}),
	}, nil
}

func (app *App) StartInformer() {
	go app.informermanager.StartInformer(app.stopCh)
}

func (app *App) StopInformer() {
	close(app.stopCh)
}

// isControllerNamespace is controller namespace
func (app *App) isControllerNamespace(namespace string) bool {
	_, ok := app.notControllerNamespace[namespace]
	return !ok
}

// instanceIsSkip skip instance
func (app *App) instanceIsSkip(namespace string, labels map[string]string) bool {
	if !app.isControllerNamespace(namespace) {
		return true
	}

	if val, ok := labels[mixSchedulerKey]; ok && val != "" && val != "true" {
		return true
	}

	if !app.mixSchedulerRequierd {
		return true
	}

	return false
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
		affinity     *corev1.Affinity
		selector     *metav1.LabelSelector
		nodeSelector = map[string]string{}
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
			klog.Info("instance is skip")
			writeNil(w, admissionReview)
			return
		}

		ns, err := app.GetNamespace(deploy.Namespace, metav1.GetOptions{})
		if err != nil {
			app.HandleError(w, r, fmt.Errorf("get namespace: %v", err))
			return
		}

		if val, ok := ns.Labels[mixSchedulerKey]; ok && val != "" && val != "true" {
			klog.Info("instance is skip")
			writeNil(w, admissionReview)
			return
		}

		selector = deploy.Spec.Selector
		if deploy.Spec.Template.Spec.NodeSelector != nil {
			nodeSelector = deploy.Spec.Template.Spec.NodeSelector
		}

		affinity = FillAffinity(deploy.Spec.Template.Spec)
	case "StatefulSet":
		// unmarshal the statefulset from the AdmissionRequest
		sts := &appsv1.StatefulSet{}
		if err := json.Unmarshal(admissionReview.Request.Object.Raw, sts); err != nil {
			app.HandleError(w, r, fmt.Errorf("unmarshal to statefulset: %v", err))
			return
		}

		if app.instanceIsSkip(sts.Namespace, sts.Labels) {
			klog.Info("instance is skip")
			writeNil(w, admissionReview)
			return
		}

		ns, err := app.GetNamespace(sts.Namespace, metav1.GetOptions{})
		if err != nil {
			app.HandleError(w, r, fmt.Errorf("get namespace: %v", err))
			return
		}

		if val, ok := ns.Labels[mixSchedulerKey]; ok && val != "" && val != "true" {
			klog.Info("instance is skip")
			writeNil(w, admissionReview)
			return
		}

		selector = sts.Spec.Selector
		if sts.Spec.Template.Spec.NodeSelector != nil {
			nodeSelector = sts.Spec.Template.Spec.NodeSelector
		}
		affinity = FillAffinity(sts.Spec.Template.Spec)
	case "Pod":
		// unmarshal the pod from the AdmissionRequest
		pod := &corev1.Pod{}
		if admissionReview.Request.Operation == admissionv1.Delete {
			if err := json.Unmarshal(admissionReview.Request.OldObject.Raw, pod); err != nil {
				app.HandleError(w, r, fmt.Errorf("unmarshal to pod: %v", err))
				return
			}
		} else {
			if err := json.Unmarshal(admissionReview.Request.Object.Raw, pod); err != nil {
				app.HandleError(w, r, fmt.Errorf("unmarshal to pod: %v", err))
				return
			}
		}

		if app.instanceIsSkip(pod.Namespace, pod.Labels) {
			klog.Info("instance is skip")
			writeNil(w, admissionReview)
			return
		}

		// preferentially scale pods on spot nodes
		if admissionReview.Request.Operation == admissionv1.Delete && app.nodeCapacity(pod.Spec.NodeName) == ondemandKey {
			if app.podExistOnNodeCapacityNum(spotKey, pod) >= app.SpotMinPodNum && app.podExistOnNodeCapacityNum(ondemandKey, pod) < app.OnDemandMinPodNum {
				app.HandleError(w, r, fmt.Errorf("preferentially scale pods on spot nodes"))
				return
			}

			klog.Info("preferentially scale pods on spot nodes")

			writeNil(w, admissionReview)
			return
		}

		if admissionReview.Request.Operation == admissionv1.Create {
			if app.podExistOnNodeCapacityNum(ondemandKey, pod) >= app.OnDemandMinPodNum {
				writeNil(w, admissionReview)
				return
			}

			klog.Info("preferentially scale pods on ondemand nodes")

			// 优先将ondemand节点分配pod
			nodeSelector = map[string]string{
				capacityKey: ondemandKey,
			}

			// marshal the nodeSelector
			nodeSelectorBytes, err := json.Marshal(nodeSelector)
			if err != nil {
				app.HandleError(w, r, fmt.Errorf("marshal affinity: %v", err))
				return
			}
			// create the patch
			patch := []JSONPatchEntry{
				{
					OP:    "replace",
					Path:  "/spec/nodeSelector",
					Value: nodeSelectorBytes,
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
			return
		}
		writeNil(w, admissionReview)
		return

	default:
		klog.Errorf("unknown kind: %s", admissionReview.Request.Object.Object.GetObjectKind().GroupVersionKind().Kind)
		writeNil(w, admissionReview)
		return
	}

	// pod anti-affinity
	affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution = []corev1.WeightedPodAffinityTerm{
		corev1.WeightedPodAffinityTerm{
			Weight: 100,
			PodAffinityTerm: corev1.PodAffinityTerm{
				TopologyKey:   "kubernetes.io/hostname",
				LabelSelector: selector,
			},
		},
	}

	// marshal the affinity back into the AdmissionReview
	affinityBytes, err := json.Marshal(affinity)
	if err != nil {
		app.HandleError(w, r, fmt.Errorf("marshal affinity: %v", err))
		return
	}

	// pod node selector
	nodeSelector[capacityKey] = app.spotNodeSelector[capacityKey]

	// marshal the nodeSelector
	nodeSelectorBytes, err := json.Marshal(nodeSelector)
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
		{
			OP:    "replace",
			Path:  "/spec/template/spec/nodeSelector",
			Value: nodeSelectorBytes,
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
	var affinity *corev1.Affinity
	if podSpec.Affinity == nil {
		affinity = &corev1.Affinity{
			PodAntiAffinity: &corev1.PodAntiAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{},
			},
		}
	} else {
		affinity = podSpec.Affinity
	}

	return affinity
}
