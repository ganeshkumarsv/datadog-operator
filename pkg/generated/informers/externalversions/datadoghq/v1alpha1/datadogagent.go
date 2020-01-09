// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-2019 Datadog, Inc.

// Code generated by informer-gen. DO NOT EDIT.

package v1alpha1

import (
	time "time"

	datadoghqv1alpha1 "github.com/DataDog/datadog-operator/pkg/apis/datadoghq/v1alpha1"
	versioned "github.com/DataDog/datadog-operator/pkg/generated/clientset/versioned"
	internalinterfaces "github.com/DataDog/datadog-operator/pkg/generated/informers/externalversions/internalinterfaces"
	v1alpha1 "github.com/DataDog/datadog-operator/pkg/generated/listers/datadoghq/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	cache "k8s.io/client-go/tools/cache"
)

// DatadogAgentInformer provides access to a shared informer and lister for
// DatadogAgents.
type DatadogAgentInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1alpha1.DatadogAgentLister
}

type datadogAgentInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
	namespace        string
}

// NewDatadogAgentInformer constructs a new informer for DatadogAgent type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewDatadogAgentInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredDatadogAgentInformer(client, namespace, resyncPeriod, indexers, nil)
}

// NewFilteredDatadogAgentInformer constructs a new informer for DatadogAgent type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredDatadogAgentInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options v1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.DatadoghqV1alpha1().DatadogAgents(namespace).List(options)
			},
			WatchFunc: func(options v1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.DatadoghqV1alpha1().DatadogAgents(namespace).Watch(options)
			},
		},
		&datadoghqv1alpha1.DatadogAgent{},
		resyncPeriod,
		indexers,
	)
}

func (f *datadogAgentInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredDatadogAgentInformer(client, f.namespace, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *datadogAgentInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&datadoghqv1alpha1.DatadogAgent{}, f.defaultInformer)
}

func (f *datadogAgentInformer) Lister() v1alpha1.DatadogAgentLister {
	return v1alpha1.NewDatadogAgentLister(f.Informer().GetIndexer())
}
