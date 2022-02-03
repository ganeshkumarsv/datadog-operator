// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package v1alpha1

import (
	"github.com/DataDog/datadog-operator/apis/datadoghq/v2alpha1"
)

func convertCLCSpec(src *DatadogAgentSpecClusterChecksRunnerSpec, dst *v2alpha1.DatadogAgent) {
	if src == nil {
		return
	}

	if src.Enabled != nil {
		features := getV2Features(dst)
		if features.ClusterChecks == nil {
			features.ClusterChecks = &v2alpha1.ClusterChecksFeatureConfig{}
		}

		features.ClusterChecks.UseClusterChecksRunners = src.Enabled
	}

	if src.Image != nil {
		getV2TemplateOverride(&dst.Spec, v2alpha1.ClusterChecksRunnerResourceName).Image = src.Image
	}

	if src.DeploymentName != "" {
		getV2TemplateOverride(&dst.Spec, v2alpha1.ClusterChecksRunnerResourceName).Name = src.DeploymentName
	}

	if src.Config != nil {
		if src.Config.LogLevel != nil {
			getV2Container(getV2TemplateOverride(&dst.Spec, v2alpha1.ClusterChecksRunnerResourceName), v2alpha1.ClusterChecksRunnersContainerName).LogLevel = src.Config.LogLevel
		}

		if src.Config.Resources != nil {
			getV2Container(getV2TemplateOverride(&dst.Spec, v2alpha1.ClusterChecksRunnerResourceName), v2alpha1.ClusterChecksRunnersContainerName).Resources = src.Config.Resources
		}

		if src.Config.Command != nil {
			getV2Container(getV2TemplateOverride(&dst.Spec, v2alpha1.ClusterChecksRunnerResourceName), v2alpha1.ClusterChecksRunnersContainerName).Command = src.Config.Command
		}

		if src.Config.Args != nil {
			getV2Container(getV2TemplateOverride(&dst.Spec, v2alpha1.ClusterChecksRunnerResourceName), v2alpha1.ClusterChecksRunnersContainerName).Args = src.Config.Args
		}

		if src.Config.Env != nil {
			getV2Container(getV2TemplateOverride(&dst.Spec, v2alpha1.ClusterChecksRunnerResourceName), v2alpha1.ClusterChecksRunnersContainerName).Env = src.Config.Env
		}

		if src.Config.VolumeMounts != nil {
			getV2Container(getV2TemplateOverride(&dst.Spec, v2alpha1.ClusterChecksRunnerResourceName), v2alpha1.ClusterChecksRunnersContainerName).VolumeMounts = src.Config.VolumeMounts
		}

		if src.Config.Volumes != nil {
			getV2TemplateOverride(&dst.Spec, v2alpha1.ClusterChecksRunnerResourceName).Volumes = src.Config.Volumes
		}

		if src.Config.HealthPort != nil {
			getV2Container(getV2TemplateOverride(&dst.Spec, v2alpha1.ClusterChecksRunnerResourceName), v2alpha1.ClusterChecksRunnersContainerName).HealthPort = src.Config.HealthPort
		}
	}

	if src.CustomConfig != nil {
		tmpl := getV2TemplateOverride(&dst.Spec, v2alpha1.ClusterChecksRunnerResourceName)
		if tmpl.CustomConfigurations == nil {
			tmpl.CustomConfigurations = make(map[v2alpha1.AgentConfigFileName]v2alpha1.CustomConfig)
		}

		tmpl.CustomConfigurations[v2alpha1.AgentGeneralConfigFile] = *convertConfigMapConfig(src.CustomConfig)
	}

	if src.Rbac != nil {
		getV2TemplateOverride(&dst.Spec, v2alpha1.ClusterChecksRunnerResourceName).CreateRbac = src.Rbac.Create
		getV2TemplateOverride(&dst.Spec, v2alpha1.ClusterChecksRunnerResourceName).ServiceAccountName = src.Rbac.ServiceAccountName
	}

	if src.Replicas != nil {
		getV2TemplateOverride(&dst.Spec, v2alpha1.ClusterChecksRunnerResourceName).Replicas = src.Replicas
	}

	if src.AdditionalAnnotations != nil {
		getV2TemplateOverride(&dst.Spec, v2alpha1.ClusterChecksRunnerResourceName).Annotations = src.AdditionalAnnotations
	}

	if src.AdditionalLabels != nil {
		getV2TemplateOverride(&dst.Spec, v2alpha1.ClusterChecksRunnerResourceName).Labels = src.AdditionalLabels
	}

	if src.PriorityClassName != "" {
		getV2TemplateOverride(&dst.Spec, v2alpha1.ClusterChecksRunnerResourceName).PriorityClassName = src.PriorityClassName
	}

	if src.Affinity != nil {
		getV2TemplateOverride(&dst.Spec, v2alpha1.ClusterChecksRunnerResourceName).Affinity = src.Affinity
	}

	if src.Tolerations != nil {
		getV2TemplateOverride(&dst.Spec, v2alpha1.ClusterChecksRunnerResourceName).Tolerations = src.Tolerations
	}

	if src.NodeSelector != nil {
		getV2TemplateOverride(&dst.Spec, v2alpha1.ClusterChecksRunnerResourceName).NodeSelector = src.NodeSelector
	}

	// TODO: NetworkPolicy field for CLC? In v2 we only have a single global NetworkPolicy configuration
}
