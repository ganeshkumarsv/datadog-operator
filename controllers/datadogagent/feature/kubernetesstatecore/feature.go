// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package kubernetesstatecore

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/DataDog/datadog-operator/apis/datadoghq/v1alpha1"
	"github.com/DataDog/datadog-operator/apis/datadoghq/v2alpha1"
	apiutils "github.com/DataDog/datadog-operator/apis/utils"

	"github.com/DataDog/datadog-operator/controllers/datadogagent/feature"
)

func init() {
	err := feature.Register(feature.KubernetesStateCoreIDType, buildKSMfeature)
	if err != nil {
		panic(err)
	}
}

func buildKSMfeature(options *feature.Options) feature.Feature {
	return &ksmFeature{}
}

type ksmFeature struct {
	enable bool
}

// Configure use to configure the feature from a v2alpha1.DatadogAgent instance.
func (f *ksmFeature) Configure(dda *v2alpha1.DatadogAgent) bool {
	return true
}

// ConfigureV1 use to configure the feature from a v1alpha1.DatadogAgent instance.
func (f *ksmFeature) ConfigureV1(dda *v1alpha1.DatadogAgent) bool {
	if dda.Spec.Features.KubeStateMetricsCore != nil && apiutils.BoolValue(dda.Spec.Features.KubeStateMetricsCore.Enabled) {
		f.enable = true
	}

	return f.enable
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
