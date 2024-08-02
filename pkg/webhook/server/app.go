package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
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

	if admissionReview.Request.Kind.Kind == "Pod" {
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
			respAdmissionReview, err := podCreateOperation(app, admissionReview, pod)
			if err != nil {
				app.HandleError(w, r, err)
				return
			} else if respAdmissionReview == nil {
				writeNil(w, admissionReview)
				return
			}

			jsonOk(w, &respAdmissionReview)
			return
		}

		writeNil(w, admissionReview)
		return
	}

	klog.Errorf("unknown kind: %s", admissionReview.Request.Object.Object.GetObjectKind().GroupVersionKind().Kind)
	writeNil(w, admissionReview)
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

func podCreateOperation(app *App, admissionReview *admissionv1.AdmissionReview, pod *corev1.Pod) (*admissionv1.AdmissionReview, error) {
	if app.podExistOnNodeCapacityNum(ondemandKey, pod) >= app.OnDemandMinPodNum {
		return nil, nil
	}

	klog.Info("preferentially scale pods on ondemand nodes")

	nodeSelector := map[string]string{
		capacityKey: ondemandKey,
	}

	// marshal the nodeSelector
	nodeSelectorBytes, err := json.Marshal(nodeSelector)
	if err != nil {
		return nil, fmt.Errorf("marshal affinity: %v", err)
	}

	// pod anti-affinity
	affinity := FillAffinity(pod.Spec)

	affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution = []corev1.WeightedPodAffinityTerm{
		corev1.WeightedPodAffinityTerm{
			Weight: 100,
			PodAffinityTerm: corev1.PodAffinityTerm{
				TopologyKey:   "kubernetes.io/hostname",
				LabelSelector: &metav1.LabelSelector{MatchLabels: pod.Labels},
			},
		},
	}

	// marshal the affinity back into the AdmissionReview
	affinityBytes, err := json.Marshal(affinity)
	if err != nil {
		return nil, fmt.Errorf("marshal affinity: %v", err)
	}

	// create the patch
	patch := []JSONPatchEntry{
		{
			OP:    "replace",
			Path:  "/spec/nodeSelector",
			Value: nodeSelectorBytes,
		},

		{
			OP:    "replace",
			Path:  "/spec/affinity",
			Value: affinityBytes,
		},
	}

	patchBytes, err := json.Marshal(&patch)
	if err != nil {
		return nil, fmt.Errorf("marshal patch: %v", err)
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

	return respAdmissionReview, nil
}
