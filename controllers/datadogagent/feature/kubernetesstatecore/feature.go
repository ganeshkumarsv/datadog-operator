// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package kubernetesstatecore

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/DataDog/datadog-operator/apis/datadoghq/v2alpha1"
	"github.com/DataDog/datadog-operator/controllers/datadogagent/feature"
)

func init() {
	err := feature.Register(feature.KubernetesStateCoreIDType, buildKSMfeature)
	if err != nil {
		panic(err)
	}
}

func buildKSMfeature() feature.Feature {
	return &ksmFeature{}
}

type ksmFeature struct{}

func (f *ksmFeature) Configure(dda *v2alpha1.DatadogAgent, options *feature.Options) bool {
	return true
}

// ManageDependencies allows a feature to manage its dependencies.
// Feature's dependencies should be added in the store.
func (f *ksmFeature) ManageDependencies(store feature.DependenciesStoreClient) error { return nil }

// ManageClusterAgent allows a feature to configure the ClusterAgent's corev1.PodTemplateSpec
// It should do nothing if the feature doesn't need to configure it.
func (f *ksmFeature) ManageClusterAgent(podTemplate *corev1.PodTemplateSpec) error { return nil }

// ManageNodeAgent allows a feature to configure the Node Agent's corev1.PodTemplateSpec
// It should do nothing if the feature doesn't need to configure it.
func (f *ksmFeature) ManageNodeAgent(podTemplate *corev1.PodTemplateSpec) error { return nil }

// ManageClusterCheckRunnerAgent allows a feature to configure the ClusterCheckRunnerAgent's corev1.PodTemplateSpec
// It should do nothing if the feature doesn't need to configure it.
func (f *ksmFeature) ManageClusterCheckRunnerAgent(podTemplate *corev1.PodTemplateSpec) error {
	return nil
}
