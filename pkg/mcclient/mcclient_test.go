package mcclient

import (
	"context"
	"sync"

	mcclientset "github.com/kcp-dev/edge-mc/pkg/mcclient/clientset"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/rest"
)

var _ = Describe("Multi-cluster client cache", func() {

	Context("With one cached cluster", func() {
		var client *multiClusterClient
		cachedCluster := "cluster1"
		notCachedCluster := "cluster2"

		BeforeEach(func() {
			client = &multiClusterClient{
				ctx:        context.TODO(),
				configs:    make(map[string]*rest.Config),
				clientsets: make(map[string]*mcclientset.Clientset),
				lock:       sync.Mutex{},
			}
			client.configs[cachedCluster] = &rest.Config{}
			client.clientsets[cachedCluster] = &mcclientset.Clientset{}

		})

		It("Should return cluster config for cached cluster", func() {
			config, err := client.ConfigForCluster(cachedCluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(config).ShouldNot(BeNil())
		})

		It("Should fail to get config for uncached cluster", func() {
			_, err := client.ConfigForCluster(notCachedCluster)
			Expect(err).To(MatchError("failed to get config for cluster: cluster2"))
		})

		It("Should return clientset for cached cluster", func() {
			ksclient := client.Cluster(cachedCluster).KS()
			k8sclient := client.Cluster(cachedCluster).Kube()
			Expect(ksclient).Should(BeNil())
			Expect(k8sclient).Should(BeNil())
		})

		It("Should fail with panic on clientset get for uncached cluster", func() {
			Expect(func() { client.Cluster(notCachedCluster) }).Should(Panic())
		})
	})
})
