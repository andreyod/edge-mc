/*
Copyright 2023 The KubeStellar Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package mcclient

import (
	"context"
	"fmt"

	machmeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	clientscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	upcache "k8s.io/client-go/tools/cache"
)

type CrossClusterListerWatcher struct {
	ctx              context.Context
	clusterListWatch map[string]*upcache.ListWatch
}

func NewCrossClusterListerWatcher(ctx context.Context, clusterListWatch map[string]*upcache.ListWatch) *CrossClusterListerWatcher {
	clw := &CrossClusterListerWatcher{
		ctx:              ctx,
		clusterListWatch: clusterListWatch,
	}
	return clw
}

func (clw *CrossClusterListerWatcher) Watch(options metav1.ListOptions) (watch.Interface, error) {
	combinedCh := make(chan watch.Event)
	for clusterName, lwForCluster := range clw.clusterListWatch {
		clusterWatch, err := lwForCluster.Watch(options)
		if err != nil {
			// TODO: Should we fail, stop all watchers and return, or continue?
			continue
		}

		go func(clusterName string) {
			for {
				select {
				case <-clw.ctx.Done():
					clusterWatch.Stop()
				case event, ok := <-clusterWatch.ResultChan():
					if !ok {
						// TODO: restart watch for this cluster?
						clusterWatch.Stop()
						return
					}
					combinedCh <- event
				}
			}
		}(clusterName)
	}
	return watch.NewProxyWatcher(combinedCh), nil
}

func (clw *CrossClusterListerWatcher) List(options metav1.ListOptions) (runtime.Object, error) {
	var allItems []runtime.Object
	var firstList *runtime.Object
	for clusterName, lwForCluster := range clw.clusterListWatch {
		clusterList, err := lwForCluster.List(options)
		if err != nil {
			return nil, fmt.Errorf("List for cluster %s failed: %w", clusterName, err)
		}
		if firstList == nil {
			firstList = &clusterList
		}
		subItems, err := machmeta.ExtractList(clusterList)
		if err != nil {
			return nil, err
		}
		allItems = append(allItems, subItems...)
	}
	mergedList := *firstList
	err := machmeta.SetList(mergedList, allItems)
	if err != nil {
		return mergedList, err
	}
	return mergedList, nil
}

func ClusterListWatch(ctx context.Context, config *rest.Config, gv schema.GroupVersion, resource string, namespace string, listObj runtime.Object, opts *metav1.ListOptions) *upcache.ListWatch {
	config.GroupVersion = &gv
	config.APIPath = "/apis"
	if len(gv.Group) == 0 {
		config.APIPath = "/api"
	}
	config.NegotiatedSerializer = clientscheme.Codecs.WithoutConversion()
	if config.UserAgent == "" {
		config.UserAgent = rest.DefaultKubernetesUserAgent()
	}
	c, _ := rest.RESTClientFor(config)

	listFunc := func(options metav1.ListOptions) (result runtime.Object, err error) {
		result = listObj
		err = c.Get().
			Namespace(namespace).
			Resource(resource).
			VersionedParams(&options, clientscheme.ParameterCodec).
			Do(ctx).
			Into(result)
		return
	}
	watchFunc := func(options metav1.ListOptions) (watch.Interface, error) {
		opts.Watch = true
		return c.Get().
			Namespace(namespace).
			Resource(resource).
			VersionedParams(opts, clientscheme.ParameterCodec).
			Watch(ctx)
	}
	return &upcache.ListWatch{ListFunc: listFunc, WatchFunc: watchFunc}
}
