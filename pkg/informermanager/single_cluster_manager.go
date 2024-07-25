package informermanager

import (
	"context"
	"sync"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

type SingleClusterManager struct {
	PodLister       corev1.PodLister
	NodeLister      corev1.NodeLister
	NamespaceLister corev1.NamespaceLister
	factory         informers.SharedInformerFactory

	synced      bool
	syncRWMutex sync.RWMutex
}

func NewSingleClusterManager(ctx context.Context, client kubernetes.Interface) *SingleClusterManager {
	factory := informers.NewSharedInformerFactory(client, 0)
	podInformer := factory.Core().V1().Pods().Informer()
	nodeInformer := factory.Core().V1().Nodes().Informer()
	namespaceInformer := factory.Core().V1().Namespaces().Informer()

	podLister := factory.Core().V1().Pods().Lister()
	podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
		},
		DeleteFunc: func(obj interface{}) {
		},
	})

	nodeLister := factory.Core().V1().Nodes().Lister()
	nodeInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
		},
		DeleteFunc: func(obj interface{}) {
		},
	})

	namespaceLister := factory.Core().V1().Namespaces().Lister()
	namespaceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
		},
		DeleteFunc: func(obj interface{}) {
		},
	})

	return &SingleClusterManager{
		PodLister:       podLister,
		NodeLister:      nodeLister,
		NamespaceLister: namespaceLister,
		factory:         factory,
	}
}

func (s *SingleClusterManager) StartInformer(stopCh <-chan struct{}) {
	s.factory.Start(stopCh)

	s.syncRWMutex.Lock()
	defer s.syncRWMutex.Unlock()
	s.factory.WaitForCacheSync(stopCh)
	s.synced = true
}

func (s *SingleClusterManager) IsSynced() bool {
	s.syncRWMutex.RLock()
	defer s.syncRWMutex.RUnlock()
	return s.synced
}
