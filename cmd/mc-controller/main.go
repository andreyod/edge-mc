package main

import (
	"context"
	"os"

	mckubernetes "github.com/kcp-dev/edge-mc/cmd/mc-controller/mcclient/kubernetes"
	api_v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	upkubernetes "k8s.io/client-go/kubernetes"
	upcache "k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	log "k8s.io/klog/v2"
	kind "sigs.k8s.io/kind/pkg/cluster"
)

// This is an example for kubernetes cluster-aware client and cross-cluster informer
// To run this example you have to create 3 regular kind clusters:
// kind create cluster -n cluster1
// kind create cluster -n cluster2
// kind create cluster -n management-cluster
// cd cmd/mc-controller
// go run *.go
func main() {
	ctx := context.Background()

	kubeConfigPath := os.Getenv("HOME") + "/.kube/config"
	managementClusterConfig, err := clientcmd.BuildConfigFromFlags("", kubeConfigPath)
	if err != nil {
		log.Fatalf("failed to build management cluster config: %v", err)
	}
	managementClientset, err := upkubernetes.NewForConfig(managementClusterConfig)
	if err != nil {
		log.Fatalf("failed to build management cluster clientset: %v", err)
	}

	// in management cluster create an object representing cluster1
	createClusterObject(ctx, managementClientset, "cluster1")
	log.Info("----------  Management cluster has 1 cluster object for 'cluster1'. Now create cluster-aware client")

	// create cluster aware client
	client, err := mckubernetes.NewMultiCluster(ctx, managementClusterConfig)
	if err != nil {
		log.Fatalf("get client failed: %v", err)
	}

	// Test the clienset
	log.Info("----------  List some resource in cluster1")
	list, _ := client.Cluster("cluster1").CoreV1().ConfigMaps(metav1.NamespaceDefault).List(ctx, metav1.ListOptions{})
	for _, cm := range list.Items {
		log.Infof("Cluster: cluster1 ; ObjName: %s", cm.Name)
	}

	log.Info("----------  Now add another cluster object for 'cluster2' and test the cluster-aware client")
	createClusterObject(ctx, managementClientset, "cluster2")
	//Test the clientsets
	list, _ = client.Cluster("cluster1").CoreV1().ConfigMaps(metav1.NamespaceDefault).List(ctx, metav1.ListOptions{})
	for _, cm := range list.Items {
		log.Infof("Cluster: cluster1 ; ObjName: %s", cm.Name)
	}
	list, _ = client.Cluster("cluster2").CoreV1().ConfigMaps(metav1.NamespaceDefault).List(ctx, metav1.ListOptions{})
	for _, cm := range list.Items {
		log.Infof("Cluster: cluster2 ; ObjName: %s", cm.Name)
	}

	//upcache.NewListWatchFromClient(managementClientset.CoreV1().RESTClient(), "configmaps", metav1.NamespaceDefault, fields.Everything())

	// create cross-cluster ListWatch
	lw := client.NewListWatch(api_v1.SchemeGroupVersion, &api_v1.ConfigMapList{}, "configmaps", metav1.NamespaceAll, fields.Everything())

	log.Info("----------  Create and run cross-cluster informer")
	informer := upcache.NewSharedIndexInformer(
		lw,
		&api_v1.ConfigMap{},
		0, // no resync
		upcache.Indexers{},
	)
	informer.AddEventHandler(upcache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			cm := obj.(*api_v1.ConfigMap)
			log.Infof("add event for obj: %s", cm.Name)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
		},
		DeleteFunc: func(obj interface{}) {
		},
	})

	go informer.Run(ctx.Done())
	upcache.WaitForNamedCacheSync("mc-informer", ctx.Done(), informer.HasSynced)
	li := informer.GetIndexer().ListKeys()
	for _, v := range li {
		log.Infof("informer key: %s", v)
	}
}

func createClusterObject(ctx context.Context, clientset upkubernetes.Interface, cluster string) {
	provider := kind.NewProvider()
	kubeconfig, err := provider.KubeConfig(cluster, false)
	if err != nil {
		log.Error("failed to get config from kind provider")
		return
	}
	//create configmap with data["raw_config"]kubeconfig
	data := make(map[string]string)
	data["raw_config"] = kubeconfig
	cm := api_v1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: cluster,
		},
		Data: data,
	}

	clientset.CoreV1().ConfigMaps(metav1.NamespaceDefault).Create(ctx, &cm, metav1.CreateOptions{})
}
