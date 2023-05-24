package kubestellar

// kubestellar cluster-aware client impl

import (
	"context"
	"errors"
	"strings"

	ksclientset "github.com/kcp-dev/edge-mc/pkg/client/clientset/versioned"
	"github.com/kcp-dev/edge-mc/pkg/mcclient/listwatch"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	upkubernetes "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	log "k8s.io/klog/v2"
)

type KubestellarClusterInterface interface {
	// Cluster returns clientset for given cluster.
	Cluster(name string) (client ksclientset.Interface)
	// ConfigForCluster returns rest config for given cluster.
	ConfigForCluster(name string) (*rest.Config, error)
	// CrossClusterListWatch returns cross-cluster ListWatch
	CrossClusterListWatch(gv schema.GroupVersion, resource string, namespace string, fieldSelector fields.Selector) *listwatch.CrossClusterListerWatcher
}

type impl map[string]*rest.Config

func (i impl) Cluster(name string) ksclientset.Interface {
	if _, ok := i[name]; !ok {
		//TODO change to ClusterOrDie and panic? return an error?
		panic("invalid cluster name")
	}
	clientset, err := ksclientset.NewForConfig(i[name])
	if err != nil {
		log.Fatalf("get config: %v", err)
	}
	return clientset
}

func (i impl) ConfigForCluster(name string) (*rest.Config, error) {
	if _, ok := i[name]; !ok {
		return nil, errors.New("failed to get config for cluster")
	}
	return i[name], nil
}

func (i impl) CrossClusterListWatch(gv schema.GroupVersion, resource string, namespace string, fieldSelector fields.Selector) *listwatch.CrossClusterListerWatcher {
	optionsModifier := func(options *metav1.ListOptions) {
		options.FieldSelector = fieldSelector.String()
	}
	clusterLW := make(map[string]*cache.ListWatch)
	for cluster, config := range i {
		clusterLW[cluster] = listwatch.ClusterListWatch(config, gv, resource, namespace, optionsModifier)
	}
	return listwatch.NewCrossClusterListerWatcher(clusterLW)
}

func NewMultiCluster(ctx context.Context, managerConfig *rest.Config) (KubestellarClusterInterface, error) {
	clients := make(impl)
	managerClient, err := upkubernetes.NewForConfig(managerConfig)
	if err != nil {
		return clients, err
	}
	// TODO use factory to create informer for EdgeCluster(effi)
	informer := cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return managerClient.CoreV1().ConfigMaps(metav1.NamespaceDefault).List(ctx, options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return managerClient.CoreV1().ConfigMaps(metav1.NamespaceDefault).Watch(ctx, options)
			},
		},
		&apiv1.ConfigMap{},
		0, // no resync
		cache.Indexers{},
	)
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			// get raw config from object
			cm := obj.(*apiv1.ConfigMap)
			name := cm.Name
			if strings.HasPrefix(name, "cluster") {
				config, err := clientcmd.RESTConfigFromKubeConfig([]byte(cm.Data["raw_config"]))
				if err != nil {
					log.Errorf("failed to get cluster config from cm.data: %v", err)
					return
				}
				//TODO lock
				clients[cm.Name] = config
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			// same as create
		},
		DeleteFunc: func(obj interface{}) {
			// delete client for this cluster

		},
	})

	go informer.Run(ctx.Done())
	cache.WaitForNamedCacheSync("management", ctx.Done(), informer.HasSynced)

	return clients, nil
}
