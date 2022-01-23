// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package kubernetesstatecore

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/DataDog/datadog-operator/apis/datadoghq/v1alpha1"
	"github.com/DataDog/datadog-operator/apis/datadoghq/v2alpha1"
	apiutils "github.com/DataDog/datadog-operator/apis/utils"
	"github.com/DataDog/datadog-operator/pkg/kubernetes"

	apicommon "github.com/DataDog/datadog-operator/apis/datadoghq/common"
	common "github.com/DataDog/datadog-operator/controllers/datadogagent/common"
	"github.com/DataDog/datadog-operator/controllers/datadogagent/feature"
	"github.com/DataDog/datadog-operator/controllers/datadogagent/merger"
	"github.com/DataDog/datadog-operator/controllers/datadogagent/object/rbac"
	"github.com/DataDog/datadog-operator/controllers/datadogagent/object/volume"
)

func init() {
	err := feature.Register(feature.KubernetesStateCoreIDType, buildKSMfeature)
	if err != nil {
		panic(err)
	}
}

func buildKSMfeature(options *feature.Options) feature.Feature {
	return &ksmFeature{
		rbacSuffix: common.ClusterAgentSuffix,
	}
}

type ksmFeature struct {
	enable               bool
	clusterChecksEnabled bool

	rbacSuffix         string
	serviceAccountName string

	owner               metav1.Object
	customConfig        *apicommon.CustomConfig
	configConfigMapName string
}

// Configure use to configure the feature from a v2alpha1.DatadogAgent instance.
func (f *ksmFeature) Configure(dda *v2alpha1.DatadogAgent) bool {
	if dda.Spec.Features.KubeStateMetricsCore != nil && apiutils.BoolValue(dda.Spec.Features.KubeStateMetricsCore.Enabled) {
		f.enable = true

		if dda.Spec.Features.KubeStateMetricsCore.Conf != nil {
			f.customConfig = v2alpha1.ConvertCustomConfig(dda.Spec.Features.KubeStateMetricsCore.Conf)
		}

		f.configConfigMapName = apicommon.GetConfName(dda, f.customConfig, apicommon.DefaultKubeStateMetricsCoreConf)
	}

	if dda.Spec.Features.ClusterChecksRunner != nil && apiutils.BoolValue(dda.Spec.Features.ClusterChecksRunner.Enabled) {
		f.clusterChecksEnabled = true
		f.rbacSuffix = common.CheckRunnersSuffix
		f.serviceAccountName = v2alpha1.GetClusterChecksRunnerServiceAccount(dda)
	} else {
		f.serviceAccountName = v2alpha1.GetClusterAgentServiceAccount(dda)
	}

	return f.enable
}

// ConfigureV1 use to configure the feature from a v1alpha1.DatadogAgent instance.
func (f *ksmFeature) ConfigureV1(dda *v1alpha1.DatadogAgent) bool {
	if dda.Spec.Features.KubeStateMetricsCore != nil {
		if apiutils.BoolValue(dda.Spec.Features.KubeStateMetricsCore.Enabled) {
			f.enable = true
		}

		if dda.Spec.ClusterAgent.Config != nil && apiutils.BoolValue(dda.Spec.ClusterAgent.Config.ClusterChecksEnabled) {
			if apiutils.BoolValue(dda.Spec.Features.KubeStateMetricsCore.ClusterCheck) {
				f.clusterChecksEnabled = true
				f.rbacSuffix = common.CheckRunnersSuffix
			}
		}

		if dda.Spec.Features.KubeStateMetricsCore.Conf != nil {
			f.customConfig = v1alpha1.ConvertCustomConfig(dda.Spec.Features.KubeStateMetricsCore.Conf)
			f.serviceAccountName = v1alpha1.GetClusterChecksRunnerServiceAccount(dda)
		} else {
			f.serviceAccountName = v1alpha1.GetClusterAgentServiceAccount(dda)
		}

		f.configConfigMapName = apicommon.GetConfName(dda, f.customConfig, apicommon.DefaultKubeStateMetricsCoreConf)
	}

	return f.enable
}

// ManageDependencies allows a feature to manage its dependencies.
// Feature's dependencies should be added in the store.
func (f *ksmFeature) ManageDependencies(store feature.DependenciesStoreClient) error {
	// Manage the Check Configuration in a configmap
	configCM, err := f.buildKSMCoreConfigMap()
	if err != nil {
		return err
	}
	if configCM != nil {
		store.AddOrUpdate(kubernetes.ConfigMapKind, configCM)
	}

	// Manager RBAC permission
	rbacName := getKubeStateMetricsRBACResourceName(f.owner, f.rbacSuffix)
	if clusterRole := buildKubeStateMetricsCoreRBAC(f.owner, rbacName, ""); clusterRole != nil {
		store.AddOrUpdate(kubernetes.ClusterRolesKind, configCM)
	}

	bindingInfo := rbac.RoleBindingInfo{
		Name:               rbacName,
		RoleName:           rbacName,
		ServiceAccountName: f.serviceAccountName,
	}
	if clusterRoleBinding := rbac.BuildClusterRoleBinding(f.owner, bindingInfo, ""); clusterRoleBinding != nil {
		store.AddOrUpdate(kubernetes.ClusterRoleBindingKind, clusterRoleBinding)
	}

	return nil
}

// ManageClusterAgent allows a feature to configure the ClusterAgent's corev1.PodTemplateSpec
// It should do nothing if the feature doesn't need to configure it.
func (f *ksmFeature) ManageClusterAgent(podTemplate *corev1.PodTemplateSpec) error {
	// Manage KSM config in confimap
	vol, volMount := volume.GetCustomConfigSpecVolumes(
		f.customConfig,
		apicommon.KubeStateMetricCoreVolumeName,
		f.configConfigMapName,
		ksmCoreCheckFolderName,
	)

	if _, err := merger.AddVolumeToPod(&podTemplate.Spec, &vol, nil); err != nil {
		return err
	}
	if _, err := merger.AddVolumeMountToContainer(&podTemplate.Spec.Containers[0], &volMount, nil); err != nil {
		return err
	}

	// Manage Envvar
	_, err := merger.AddEnvVarToContainer(&podTemplate.Spec.Containers[0], &corev1.EnvVar{
		Name:  apicommon.DDKubeStateMetricsCoreEnabled,
		Value: "true",
	}, nil)
	if err != nil {
		return err
	}
	_, err = merger.AddEnvVarToContainer(&podTemplate.Spec.Containers[0], &corev1.EnvVar{
		Name:  apicommon.DDKubeStateMetricsCoreConfigMap,
		Value: f.configConfigMapName,
	}, nil)
	if err != nil {
		return err
	}

	return nil
}

// ManageNodeAgent allows a feature to configure the Node Agent's corev1.PodTemplateSpec
// It should do nothing if the feature doesn't need to configure it.
func (f *ksmFeature) ManageNodeAgent(podTemplate *corev1.PodTemplateSpec) error {
	// Remove ksm v1 conf if the cluster checks are enabled and the ksm core is enabled
	for id := range podTemplate.Spec.Containers {
		if podTemplate.Spec.Containers[id].Name != common.AgentContainerName {
			continue
		}
		_, err := merger.AddEnvVarToContainer(&podTemplate.Spec.Containers[id], &corev1.EnvVar{
			Name:  apicommon.DDIgnoreAutoConf,
			Value: "kubernetes_state",
		}, merger.AppendToValueEnvVarMergeFunction)
		if err != nil {
			return err
		}
	}
	return nil
}

// ManageClusterCheckRunnerAgent allows a feature to configure the ClusterCheckRunnerAgent's corev1.PodTemplateSpec
// It should do nothing if the feature doesn't need to configure it.
func (f *ksmFeature) ManageClusterCheckRunnerAgent(podTemplate *corev1.PodTemplateSpec) error {
	return nil
}
