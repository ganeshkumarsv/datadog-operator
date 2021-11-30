// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package feature

import (
	"github.com/DataDog/datadog-operator/apis/datadoghq/v2alpha1"
	"github.com/go-logr/logr"

	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"
)

// Feature Feature interface
// It returns `true` if the Feature is used, else it return `false`.
type Feature interface {
	// Configure use to configure the internal of a Feature
	// It should return `true` if the feature is enabled, else `false`.
	Configure(dda *v2alpha1.DatadogAgent, options *Options) bool
	// ManageDependencies allows a feature to manage its dependencies.
	// Feature's dependencies should be added in the store.
	ManageDependencies(store DependenciesStoreClient) error
	// ManageClusterAgent allows a feature to configure the ClusterAgent's corev1.PodTemplateSpec
	// It should do nothing if the feature doesn't need to configure it.
	ManageClusterAgent(podTemplate *corev1.PodTemplateSpec) error
	// ManageNodeAgent allows a feature to configure the Node Agent's corev1.PodTemplateSpec
	// It should do nothing if the feature doesn't need to configure it.
	ManageNodeAgent(podTemplate *corev1.PodTemplateSpec) error
	// ManageClusterCheckRunnerAgent allows a feature to configure the ClusterCheckRunnerAgent's corev1.PodTemplateSpec
	// It should do nothing if the feature doesn't need to configure it.
	ManageClusterCheckRunnerAgent(podTemplate *corev1.PodTemplateSpec) error
}

// Options option that can be pass to the Interface.Configure function
type Options struct {
	SupportExtendedDaemonset bool

	Logger logr.Logger
}

// BuildFunc function type used by each Feature during its factory registration.
// It returns the Feature interface.
type BuildFunc func() Feature

// DependenciesStoreClient dependencies store client interface
type DependenciesStoreClient interface {
	AddOrUpdate(kind string, namespace string, name string, obj client.Object)
	Get(kind string, namespace, name string) (client.Object, bool)
}
