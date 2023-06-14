package mcclient

import (
	"context"
	"sync"
	"time"

	lcv1 "github.com/kcp-dev/edge-mc/pkg/apis/logicalcluster/v1alpha1"
	edgefakeclient "github.com/kcp-dev/edge-mc/pkg/client/clientset/versioned/fake"
	ksinformers "github.com/kcp-dev/edge-mc/pkg/client/informers/externalversions"
	mcclientset "github.com/kcp-dev/edge-mc/pkg/mcclient/clientset"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

var _ = Describe("LogicalClusters client controller", func() {

	Context("With one cached cluster", func() {
		var client *multiClusterClient
		cachedCluster := "cluster1"
		//notCachedCluster := "cluster2"
		ctx := context.TODO()

		BeforeEach(func() {
			lc := lcv1.LogicalCluster{
				TypeMeta: metav1.TypeMeta{
					Kind:       "LogicalCluster",
					APIVersion: "v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster0",
				},
				Status: lcv1.LogicalClusterStatus{
					Phase:         lcv1.LogicalClusterPhaseReady,
					ClusterConfig: "kubeconfig",
				},
			}

			client = &multiClusterClient{
				ctx:        context.TODO(),
				configs:    make(map[string]*rest.Config),
				clientsets: make(map[string]*mcclientset.Clientset),
				lock:       sync.Mutex{},
			}
			client.configs[cachedCluster] = &rest.Config{}
			client.clientsets[cachedCluster] = &mcclientset.Clientset{}

			clientset := edgefakeclient.NewSimpleClientset(&lc) //put LC here in args
			clusterInformerFactory := ksinformers.NewSharedScopedInformerFactory(clientset, 0, metav1.NamespaceAll)
			clusterInformer := clusterInformerFactory.Logicalcluster().V1alpha1().LogicalClusters().Informer()
			//clusterInformerFactory.Start(ctx.Done()) //can't start here.

			logicalClusterController := newController(ctx, clusterInformer, client)
			go logicalClusterController.Run(2)
			time.Sleep(3 * time.Second)
			//	clientset.Tracker().Add(&lc)
			//clientset.Fake.PrependReactor()
			err := clusterInformer.GetIndexer().Add(lc) // have to be object(not LC). need to fake informer for this? with source
			Expect(err).NotTo(HaveOccurred())
			// Expect(clusterInformer.GetIndexer().Add(lc)).To(Succeed())  //need to fake informer for this?

			//logicalClusterController.enqueue(lc) // have to be object(not LC)
			logicalClusterController.queue.Add("cluster0")

		})

		It("Should return cluster config for cached cluster", func() {
			time.Sleep(3 * time.Second)
			config, err := client.ConfigForCluster("cluster0")
			Expect(err).NotTo(HaveOccurred())
			Expect(config).ShouldNot(BeNil())
		})
	})
})
