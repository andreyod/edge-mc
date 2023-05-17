package kubernetes

import (
	"context"
	"strings"

	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	log "k8s.io/klog/v2"
)

type Interface interface {
	// Cluster returns clientset for given cluster.
	Cluster(name string) (client kubernetes.Interface)
	// NewListWatch returns cross-cluster ListWatch
	NewListWatch(gv schema.GroupVersion, listResult runtime.Object, resource string, namespace string, fieldSelector fields.Selector) *cache.ListWatch
}

type impl map[string]*rest.Config

func (i impl) Cluster(name string) kubernetes.Interface {
	if _, ok := i[name]; !ok {
		//TODO change to ClusterOrDie and panic? return an error?
		panic("invalid cluster name")
	}
	clientset, err := kubernetes.NewForConfig(i[name])
	if err != nil {
		log.Fatalf("get config: %v", err)
	}
	return clientset
}

func (i impl) NewListWatch(gv schema.GroupVersion, listResult runtime.Object, resource string, namespace string, fieldSelector fields.Selector) *cache.ListWatch {
	optionsModifier := func(options *metav1.ListOptions) {
		options.FieldSelector = fieldSelector.String()
	}
	return i.newFilteredListWatch(gv, listResult, resource, namespace, optionsModifier)
}

func (i impl) newFilteredListWatch(gv schema.GroupVersion, listResult runtime.Object, resource string, namespace string, optionsModifier func(options *metav1.ListOptions)) *cache.ListWatch {
	// TODO make it cross-cluster. iterate over i, combine list/watch output
	config := i["cluster1"]
	config.GroupVersion = &gv
	config.APIPath = "/api"
	config.NegotiatedSerializer = scheme.Codecs.WithoutConversion()

	if config.UserAgent == "" {
		config.UserAgent = rest.DefaultKubernetesUserAgent()
	}
	c, err := rest.RESTClientFor(config)
	if err != nil {
		log.Fatalf("get rest client failed: %v", err)
	}
	listFunc := func(options metav1.ListOptions) (runtime.Object, error) {
		optionsModifier(&options)
		err = c.Get().
			Namespace(namespace).
			Resource(resource).
			VersionedParams(&options, metav1.ParameterCodec).
			Do(context.TODO()).
			Into(listResult)
		if err != nil {
			return listResult, err
		}
		return listResult, nil
	}
	watchFunc := func(options metav1.ListOptions) (watch.Interface, error) {
		options.Watch = true
		optionsModifier(&options)
		return c.Get().
			Namespace(namespace).
			Resource(resource).
			VersionedParams(&options, metav1.ParameterCodec).
			Watch(context.TODO())
	}
	return &cache.ListWatch{ListFunc: listFunc, WatchFunc: watchFunc}
}

func NewMultiCluster(ctx context.Context, managerConfig *rest.Config) (Interface, error) {
	clients := make(impl)
	managerClient, err := kubernetes.NewForConfig(managerConfig)
	if err != nil {
		return clients, err
	}
	// TODO use factory
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
