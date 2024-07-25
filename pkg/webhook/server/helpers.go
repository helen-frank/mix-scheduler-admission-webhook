package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
)

// http helpers

// HandleError depending on error type
func (app *App) HandleError(w http.ResponseWriter, r *http.Request, err error) {
	jsonError(w, err.Error(), http.StatusBadRequest)
}

// readJSON from request body
func readJSON(r *http.Request, v interface{}) error {
	err := json.NewDecoder(r.Body).Decode(v)
	if err != nil {
		return fmt.Errorf("invalid JSON input")
	}

	return nil
}

// jsonOk renders json with 200 ok
func jsonOk(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, v)
}

// writeJSON to response body
func writeJSON(w http.ResponseWriter, v interface{}) {
	b, err := json.Marshal(v)
	if err != nil {
		http.Error(w, fmt.Sprintf("json encoding error: %v", err), http.StatusInternalServerError)
		return
	}

	writeBytes(w, b)
}

// writeBytes to response body
func writeBytes(w http.ResponseWriter, b []byte) {
	_, err := w.Write(b)
	if err != nil {
		http.Error(w, fmt.Sprintf("write error: %v", err), http.StatusInternalServerError)
		return
	}
}

// jsonError renders json with error
func jsonError(w http.ResponseWriter, errStr string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	writeJSON(w, &jsonErr{Err: errStr})
}

// jsonErr err
type jsonErr struct {
	Err string `json:"err"`
}

func writeNil(w http.ResponseWriter, admissionReview *admissionv1.AdmissionReview) {
	// create the AdmissionResponse
	admissionResponse := &admissionv1.AdmissionResponse{
		UID:     admissionReview.Request.UID,
		Allowed: true,
	}

	respAdmissionReview := &admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AdmissionReview",
			APIVersion: "admission.k8s.io/v1",
		},
		Response: admissionResponse,
	}

	writeJSON(w, respAdmissionReview)
}

func PodReady(pod *corev1.Pod) bool {
	for ci := range pod.Status.Conditions {
		if pod.Status.Conditions[ci].Type == corev1.PodReady && (pod.Status.Conditions[ci].Reason == "PodCompleted" || pod.Status.Conditions[ci].Status == corev1.ConditionTrue) {
			return true
		}
	}
	return false
}

func (app *App) podExistAndReadyOnNodeCapacity(capacity string, pod *corev1.Pod) bool {
	capacityNodes := make(map[string]struct{})

	if nodes, err := app.ListNode(labels.Set{capacityKey: capacity}.AsSelector()); err != nil {
		klog.Errorf("get %s nodes: %v", capacity, err)
		return false
	} else {
		if len(nodes) > 0 {
			for ni := range nodes {
				capacityNodes[nodes[ni].Name] = struct{}{}
			}
		} else {
			klog.Infof("no %s nodes", capacity)
			return false
		}
	}

	pods, err := app.ListPod(pod.Namespace, labels.Set(pod.Labels).AsSelector())
	if err != nil {
		klog.Errorf("get pod: %v", err)
		return false
	}

	for pi := range pods {
		if _, ok := capacityNodes[pods[pi].Spec.NodeName]; ok && PodReady(pods[pi]) {
			return true
		}
	}

	return false
}

func (app *App) podExistAndReadyOnNodeCapacityNum(capacity string, pod *corev1.Pod) int {
	capacityNodes := make(map[string]struct{})

	if nodes, err := app.ListNode(labels.Set{capacityKey: capacity}.AsSelector()); err != nil {
		klog.Errorf("get %s nodes: %v", capacity, err)
		return 0
	} else {
		if len(nodes) > 0 {
			for ni := range nodes {
				capacityNodes[nodes[ni].Name] = struct{}{}
			}
		} else {
			klog.Infof("no %s nodes", capacity)
			return 0
		}
	}

	pods, err := app.ListPod(pod.Namespace, labels.Set(pod.Labels).AsSelector())
	if err != nil {
		klog.Errorf("get pod: %v", err)
		return 0
	}

	num := 0

	for pi := range pods {
		if _, ok := capacityNodes[pods[pi].Spec.NodeName]; ok && PodReady(pods[pi]) {
			num++
		}
	}

	return num
}

func (app *App) nodeCapacity(nodeName string) string {
	node, err := app.GetNode(nodeName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("get node: %v", err)
		return ""
	}
	return node.Labels[capacityKey]
}

func (app *App) GetNamespace(name string, opts metav1.GetOptions) (*corev1.Namespace, error) {
	if app.informermanager.IsSynced() {
		return app.informermanager.NamespaceLister.Get(name)
	}
	return app.Client.CoreV1().Namespaces().Get(app.Ctx, name, opts)
}

func (app *App) GetPod(namespace, name string, opts metav1.GetOptions) (*corev1.Pod, error) {
	if app.informermanager.IsSynced() {
		return app.informermanager.PodLister.Pods(namespace).Get(name)
	}
	return app.Client.CoreV1().Pods(namespace).Get(app.Ctx, name, opts)
}

func (app *App) ListPod(namespace string, selector labels.Selector) ([]*corev1.Pod, error) {
	if app.informermanager.IsSynced() {
		return app.informermanager.PodLister.Pods(namespace).List(selector)
	}

	opts := metav1.ListOptions{LabelSelector: selector.String()}

	pods, err := app.Client.CoreV1().Pods(namespace).List(app.Ctx, opts)
	if err != nil {
		return nil, err
	}

	podList := make([]*corev1.Pod, len(pods.Items))
	for i := range pods.Items {
		podList[i] = &pods.Items[i]
	}

	return podList, nil
}

func (app *App) GetNode(name string, opts metav1.GetOptions) (*corev1.Node, error) {
	if app.informermanager.IsSynced() {
		return app.informermanager.NodeLister.Get(name)
	}
	return app.Client.CoreV1().Nodes().Get(app.Ctx, name, opts)
}

func (app *App) ListNode(selector labels.Selector) ([]*corev1.Node, error) {
	if app.informermanager.IsSynced() {
		return app.informermanager.NodeLister.List(selector)
	}

	opts := metav1.ListOptions{LabelSelector: selector.String()}

	nodes, err := app.Client.CoreV1().Nodes().List(app.Ctx, opts)
	if err != nil {
		return nil, err
	}

	nodeList := make([]*corev1.Node, len(nodes.Items))
	for i := range nodes.Items {
		nodeList[i] = &nodes.Items[i]
	}

	return nodeList, nil
}
