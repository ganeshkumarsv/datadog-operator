// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package datadogagent

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apicommon "github.com/DataDog/datadog-operator/apis/datadoghq/common"
	datadoghqv1alpha1 "github.com/DataDog/datadog-operator/apis/datadoghq/v1alpha1"
	apiutils "github.com/DataDog/datadog-operator/apis/utils"
	"github.com/DataDog/datadog-operator/controllers/datadogagent/common"
	objectvolume "github.com/DataDog/datadog-operator/controllers/datadogagent/object/volume"
	"github.com/DataDog/datadog-operator/controllers/datadogagent/orchestrator"
	cilium "github.com/DataDog/datadog-operator/pkg/cilium/v1"
	"github.com/DataDog/datadog-operator/pkg/controller/utils"
	"github.com/DataDog/datadog-operator/pkg/controller/utils/comparison"
	"github.com/DataDog/datadog-operator/pkg/controller/utils/datadog"
	"github.com/DataDog/datadog-operator/pkg/extendeddaemonset"
	"github.com/DataDog/datadog-operator/pkg/kubernetes"
	"github.com/DataDog/datadog-operator/pkg/kubernetes/rbac"

	"github.com/DataDog/datadog-operator/controllers/datadogagent/feature"
)

func (r *Reconciler) reconcileClusterAgent(logger logr.Logger, features []feature.Feature, dda *datadoghqv1alpha1.DatadogAgent, newStatus *datadoghqv1alpha1.DatadogAgentStatus) (reconcile.Result, error) {
	result, err := r.manageClusterAgentDependencies(logger, dda, newStatus)
	if utils.ShouldReturn(result, err) {
		return result, err
	}
	if !isClusterAgentEnabled(dda.Spec.ClusterAgent) {
		result, err = r.cleanupClusterAgent(logger, dda, newStatus)
		return result, err
	}

	if newStatus.ClusterAgent != nil &&
		newStatus.ClusterAgent.DeploymentName != "" &&
		newStatus.ClusterAgent.DeploymentName != getClusterAgentName(dda) {
		return result, fmt.Errorf("the Datadog cluster agent Deployment cannot be renamed once created")
	}

	nsName := types.NamespacedName{
		Name:      getClusterAgentName(dda),
		Namespace: dda.Namespace,
	}
	// ClusterAgentDeployment attached to this instance
	clusterAgentDeployment := &appsv1.Deployment{}
	if isClusterAgentEnabled(dda.Spec.ClusterAgent) {
		err := r.client.Get(context.TODO(), nsName, clusterAgentDeployment)
		if err != nil {
			if errors.IsNotFound(err) {
				logger.Info("the ClusterAgent deployment is not found", "name", nsName.Name, "namespace", nsName.Namespace)
				// Create and attach a ClusterAgentDeployment
				return r.createNewClusterAgentDeployment(logger, features, dda, newStatus)
			}
			return reconcile.Result{}, err
		}

		if result, err = r.updateClusterAgentDeployment(logger, features, dda, clusterAgentDeployment, newStatus); err != nil {
			return result, err
		}

		// Make sure we have at least one Cluster Agent available replica
		if clusterAgentDeployment.Status.AvailableReplicas == 0 {
			return reconcile.Result{RequeueAfter: defaultRequeuePeriod}, fmt.Errorf("cluster agent deployment is not ready yet: 0 pods available out of %d", clusterAgentDeployment.Status.Replicas)
		}
	}

	return reconcile.Result{}, nil
}

func (r *Reconciler) createNewClusterAgentDeployment(logger logr.Logger, features []feature.Feature, dda *datadoghqv1alpha1.DatadogAgent, newStatus *datadoghqv1alpha1.DatadogAgentStatus) (reconcile.Result, error) {
	newDCA, hash, err := newClusterAgentDeploymentFromInstance(logger, features, dda, nil)
	if err != nil {
		return reconcile.Result{}, err
	}

	// Set DatadogAgent instance  instance as the owner and controller
	if err = controllerutil.SetControllerReference(dda, newDCA, r.scheme); err != nil {
		return reconcile.Result{}, err
	}
	logger.Info("Creating a new Cluster Agent Deployment", "deployment.Namespace", newDCA.Namespace, "deployment.Name", newDCA.Name, "agentdeployment.Status.ClusterAgent.CurrentHash", hash)
	newStatus.ClusterAgent = &datadoghqv1alpha1.DeploymentStatus{}
	err = r.client.Create(context.TODO(), newDCA)
	now := metav1.NewTime(time.Now())
	if err != nil {
		updateStatusWithClusterAgent(nil, newStatus, &now)
		return reconcile.Result{}, err
	}

	updateStatusWithClusterAgent(newDCA, newStatus, &now)
	event := buildEventInfo(newDCA.Name, newDCA.Namespace, deploymentKind, datadog.CreationEvent)
	r.recordEvent(dda, event)
	return reconcile.Result{}, nil
}

func updateStatusWithClusterAgent(dca *appsv1.Deployment, newStatus *datadoghqv1alpha1.DatadogAgentStatus, updateTime *metav1.Time) {
	newStatus.ClusterAgent = updateDeploymentStatus(dca, newStatus.ClusterAgent, updateTime)
}

func (r *Reconciler) updateClusterAgentDeployment(logger logr.Logger, features []feature.Feature, dda *datadoghqv1alpha1.DatadogAgent, dca *appsv1.Deployment, newStatus *datadoghqv1alpha1.DatadogAgentStatus) (reconcile.Result, error) {
	newDCA, hash, err := newClusterAgentDeploymentFromInstance(logger, features, dda, dca.Spec.Selector)
	if err != nil {
		return reconcile.Result{}, err
	}

	needUpdate := !comparison.IsSameSpecMD5Hash(hash, dca.GetAnnotations())

	updateStatusWithClusterAgent(dca, newStatus, nil)

	if !needUpdate {
		return reconcile.Result{}, nil
	}
	logger.Info("Update ClusterAgent deployment", "name", dca.Name, "namespace", dca.Namespace)
	// Set DatadogAgent instance  instance as the owner and controller
	if err = controllerutil.SetControllerReference(dda, dca, r.scheme); err != nil {
		return reconcile.Result{}, err
	}
	logger.Info("Updating an existing Cluster Agent Deployment", "deployment.Namespace", newDCA.Namespace, "deployment.Name", newDCA.Name, "currentHash", hash)

	// Copy possibly changed fields
	updateDca := dca.DeepCopy()
	updateDca.Spec = *newDCA.Spec.DeepCopy()
	updateDca.Spec.Replicas = getReplicas(dca.Spec.Replicas, updateDca.Spec.Replicas)
	updateDca.Annotations = mergeAnnotationsLabels(logger, dca.GetAnnotations(), newDCA.GetAnnotations(), dda.Spec.ClusterAgent.KeepAnnotations)
	updateDca.Labels = mergeAnnotationsLabels(logger, dca.GetLabels(), newDCA.GetLabels(), dda.Spec.ClusterAgent.KeepLabels)

	now := metav1.NewTime(time.Now())
	err = kubernetes.UpdateFromObject(context.TODO(), r.client, updateDca, dca.ObjectMeta)
	if err != nil {
		return reconcile.Result{}, err
	}
	event := buildEventInfo(updateDca.Name, updateDca.Namespace, deploymentKind, datadog.UpdateEvent)
	r.recordEvent(dda, event)
	updateStatusWithClusterAgent(updateDca, newStatus, &now)
	return reconcile.Result{}, nil
}

// newClusterAgentDeploymentFromInstance creates a Cluster Agent Deployment from a given DatadogAgent
func newClusterAgentDeploymentFromInstance(logger logr.Logger, features []feature.Feature, dda *datadoghqv1alpha1.DatadogAgent, selector *metav1.LabelSelector) (*appsv1.Deployment, string, error) {
	labels := getDefaultLabels(dda, apicommon.DefaultClusterAgentResourceSuffix, getClusterAgentVersion(dda))
	labels[apicommon.AgentDeploymentNameLabelKey] = dda.Name
	labels[apicommon.AgentDeploymentComponentLabelKey] = apicommon.DefaultClusterAgentResourceSuffix

	if selector != nil {
		for key, val := range selector.MatchLabels {
			labels[key] = val
		}
	} else {
		selector = &metav1.LabelSelector{
			MatchLabels: map[string]string{
				apicommon.AgentDeploymentNameLabelKey:      dda.Name,
				apicommon.AgentDeploymentComponentLabelKey: apicommon.DefaultClusterAgentResourceSuffix,
			},
		}
	}

	annotations := getDefaultAnnotations(dda)

	dcaPodTemplate, err := newClusterAgentPodTemplate(logger, dda, labels, annotations)
	if err != nil {
		return nil, "", err
	}

	dca := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        getClusterAgentName(dda),
			Namespace:   dda.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: appsv1.DeploymentSpec{
			Template: dcaPodTemplate,
			Replicas: dda.Spec.ClusterAgent.Replicas,
			Selector: selector,
		},
	}

	for _, feat := range features {
		if errFeat := feat.ManageClusterAgent(&dca.Spec.Template); errFeat != nil {
			return nil, "", err
		}
	}

	hash, err := comparison.SetMD5DatadogAgentGenerationAnnotation(&dca.ObjectMeta, dca.Spec)
	return dca, hash, err
}

func (r *Reconciler) manageClusterAgentDependencies(logger logr.Logger, dda *datadoghqv1alpha1.DatadogAgent, newStatus *datadoghqv1alpha1.DatadogAgentStatus) (reconcile.Result, error) {
	result, err := r.manageAgentSecret(logger, dda, newStatus)
	if utils.ShouldReturn(result, err) {
		return result, err
	}

	result, err = r.manageExternalMetricsSecret(logger, dda, newStatus)
	if utils.ShouldReturn(result, err) {
		return result, err
	}

	result, err = r.manageConfigMap(logger, dda, getClusterAgentCustomConfigConfigMapName(dda), buildClusterAgentConfigurationConfigMap)
	if utils.ShouldReturn(result, err) {
		return result, err
	}

	result, err = r.manageClusterAgentService(logger, dda)
	if utils.ShouldReturn(result, err) {
		return result, err
	}

	result, err = r.manageMetricsServerService(logger, dda)
	if utils.ShouldReturn(result, err) {
		return result, err
	}

	result, err = r.manageMetricsServerAPIService(logger, dda)
	if utils.ShouldReturn(result, err) {
		return result, err
	}

	result, err = r.manageAdmissionControllerService(logger, dda)
	if utils.ShouldReturn(result, err) {
		return result, err
	}

	result, err = r.manageClusterAgentPDB(logger, dda)
	if utils.ShouldReturn(result, err) {
		return result, err
	}

	result, err = r.manageClusterAgentRBACs(logger, dda)
	if utils.ShouldReturn(result, err) {
		return result, err
	}

	result, err = r.manageConfigMap(logger, dda, getInstallInfoConfigMapName(dda), buildInstallInfoConfigMap)
	if utils.ShouldReturn(result, err) {
		return result, err
	}

	result, err = r.manageClusterAgentNetworkPolicy(logger, dda)
	if utils.ShouldReturn(result, err) {
		return result, err
	}

	result, err = r.manageOrchestratorExplorer(logger, dda)
	if utils.ShouldReturn(result, err) {
		return result, err
	}

	return reconcile.Result{}, nil
}

func buildClusterAgentConfigurationConfigMap(dda *datadoghqv1alpha1.DatadogAgent) (*corev1.ConfigMap, error) {
	if !isClusterAgentEnabled(dda.Spec.ClusterAgent) {
		return nil, nil
	}
	return buildConfigurationConfigMap(dda, datadoghqv1alpha1.ConvertCustomConfig(dda.Spec.ClusterAgent.CustomConfig), getClusterAgentCustomConfigConfigMapName(dda), datadoghqv1alpha1.ClusterAgentCustomConfigVolumeSubPath)
}

func (r *Reconciler) cleanupClusterAgent(logger logr.Logger, dda *datadoghqv1alpha1.DatadogAgent, newStatus *datadoghqv1alpha1.DatadogAgentStatus) (reconcile.Result, error) {
	nsName := types.NamespacedName{
		Name:      getClusterAgentName(dda),
		Namespace: dda.Namespace,
	}
	// ClusterAgentDeployment attached to this instance
	clusterAgentDeployment := &appsv1.Deployment{}
	if err := r.client.Get(context.TODO(), nsName, clusterAgentDeployment); err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}
	logger.Info("Deleting Cluster Agent Deployment", "deployment.Namespace", clusterAgentDeployment.Namespace, "deployment.Name", clusterAgentDeployment.Name)
	event := buildEventInfo(clusterAgentDeployment.Name, clusterAgentDeployment.Namespace, clusterRoleBindingKind, datadog.DeletionEvent)
	r.recordEvent(dda, event)
	if err := r.client.Delete(context.TODO(), clusterAgentDeployment); err != nil {
		return reconcile.Result{}, err
	}
	newStatus.ClusterAgent = nil
	return reconcile.Result{Requeue: true}, nil
}

// newClusterAgentPodTemplate generates a PodTemplate from a DatadogClusterAgentDeployment spec
func newClusterAgentPodTemplate(logger logr.Logger, dda *datadoghqv1alpha1.DatadogAgent, labels, annotations map[string]string) (corev1.PodTemplateSpec, error) {
	// copy Spec to configure the Cluster Agent Pod Template
	clusterAgentSpec := dda.Spec.ClusterAgent.DeepCopy()

	// confd volumes configuration
	confdVolumeSource := corev1.VolumeSource{
		EmptyDir: &corev1.EmptyDirVolumeSource{},
	}
	if dda.Spec.ClusterAgent.Config != nil && dda.Spec.ClusterAgent.Config.Confd != nil {
		confdVolumeSource = corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: dda.Spec.ClusterAgent.Config.Confd.ConfigMapName,
				},
			},
		}

		if len(dda.Spec.ClusterAgent.Config.Confd.Items) > 0 {
			for _, val := range dda.Spec.ClusterAgent.Config.Confd.Items {
				confdVolumeSource.ConfigMap.Items = append(confdVolumeSource.ConfigMap.Items, corev1.KeyToPath{
					Key:  val.Key,
					Path: val.Path,
				})
			}
		}
	}

	volumes := []corev1.Volume{
		{
			Name: datadoghqv1alpha1.InstallInfoVolumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: getInstallInfoConfigMapName(dda),
					},
				},
			},
		},
		{
			Name:         apicommon.ConfdVolumeName,
			VolumeSource: confdVolumeSource,
		},
	}
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "installinfo",
			SubPath:   "install_info",
			MountPath: "/etc/datadog-agent/install_info",
			ReadOnly:  true,
		},
		{
			Name:      apicommon.ConfdVolumeName,
			MountPath: apicommon.ConfdVolumePath,
			ReadOnly:  true,
		},
	}

	if dda.Spec.ClusterAgent.CustomConfig != nil {
		customConfigVolumeSource := objectvolume.GetVolumeFromCustomConfigSpec(
			datadoghqv1alpha1.ConvertCustomConfig(dda.Spec.ClusterAgent.CustomConfig),
			getClusterAgentCustomConfigConfigMapName(dda),
			datadoghqv1alpha1.AgentCustomConfigVolumeName,
		)
		volumes = append(volumes, customConfigVolumeSource)

		// Custom config (datadog-cluster.yaml) volume
		volumeMount := objectvolume.GetVolumeMountFromCustomConfigSpec(
			datadoghqv1alpha1.ConvertCustomConfig(dda.Spec.ClusterAgent.CustomConfig),
			datadoghqv1alpha1.ClusterAgentCustomConfigVolumeName,
			datadoghqv1alpha1.ClusterAgentCustomConfigVolumePath,
			datadoghqv1alpha1.ClusterAgentCustomConfigVolumeSubPath)
		volumeMounts = append(volumeMounts, volumeMount)
	}

	if isComplianceEnabled(&dda.Spec) {
		if dda.Spec.Agent.Security.Compliance.ConfigDir != nil {
			volumes = append(volumes, corev1.Volume{
				Name: datadoghqv1alpha1.SecurityAgentComplianceConfigDirVolumeName,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: dda.Spec.Agent.Security.Compliance.ConfigDir.ConfigMapName,
						},
					},
				},
			})
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:      datadoghqv1alpha1.SecurityAgentComplianceConfigDirVolumeName,
				MountPath: datadoghqv1alpha1.SecurityAgentComplianceConfigDirVolumePath,
				ReadOnly:  true,
			})
		}
	}

	if isOrchestratorExplorerEnabled(dda) {
		volume, volumeMount := getCustomConfigSpecVolumes(
			dda.Spec.Features.OrchestratorExplorer.Conf,
			datadoghqv1alpha1.OrchestratorExplorerConfigVolumeName,
			getOrchestratorExplorerConfName(dda),
			orchestratorExplorerCheckFolderName,
		)

		volumes = append(volumes, volume)
		volumeMounts = append(volumeMounts, volumeMount)
	}

	// Add other volumes
	if dda.Spec.ClusterAgent.Config != nil {
		volumes = append(volumes, dda.Spec.ClusterAgent.Config.Volumes...)
		volumeMounts = append(volumeMounts, dda.Spec.ClusterAgent.Config.VolumeMounts...)
	}

	envs, err := getEnvVarsForClusterAgent(logger, dda)
	if err != nil {
		return corev1.PodTemplateSpec{}, err
	}

	podSpec := corev1.PodSpec{
		ServiceAccountName: getClusterAgentServiceAccount(dda),
		Containers: []corev1.Container{
			{
				Name:            "cluster-agent",
				Image:           getImage(clusterAgentSpec.Image, dda.Spec.Registry),
				ImagePullPolicy: *clusterAgentSpec.Image.PullPolicy,
				Ports: []corev1.ContainerPort{
					{
						ContainerPort: 5005,
						Name:          "agentport",
						Protocol:      "TCP",
					},
				},
				Env:          envs,
				VolumeMounts: volumeMounts,
				Command:      getDefaultIfEmpty(dda.Spec.ClusterAgent.Config.Command, nil),
				Args:         getDefaultIfEmpty(dda.Spec.ClusterAgent.Config.Args, nil),
			},
		},
		Affinity:          getClusterAgentAffinity(clusterAgentSpec.Affinity),
		Tolerations:       clusterAgentSpec.Tolerations,
		PriorityClassName: dda.Spec.ClusterAgent.PriorityClassName,
		Volumes:           volumes,
	}

	newPodTemplate := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      make(map[string]string),
			Annotations: make(map[string]string),
		},
		Spec: podSpec,
	}

	for key, val := range labels {
		newPodTemplate.Labels[key] = val
	}

	for key, val := range annotations {
		newPodTemplate.Annotations[key] = val
	}

	for key, val := range dda.Spec.ClusterAgent.AdditionalLabels {
		newPodTemplate.Labels[key] = val
	}

	for key, val := range dda.Spec.ClusterAgent.AdditionalAnnotations {
		newPodTemplate.Annotations[key] = val
	}

	container := &newPodTemplate.Spec.Containers[0]

	if apiutils.BoolValue(dda.Spec.ClusterAgent.Config.ExternalMetrics.Enabled) {
		port := getClusterAgentMetricsProviderPort(*dda.Spec.ClusterAgent.Config)
		container.Ports = append(container.Ports, corev1.ContainerPort{
			ContainerPort: port,
			Name:          "metricsapi",
			Protocol:      "TCP",
		})
	}

	container.LivenessProbe = datadoghqv1alpha1.GetDefaultLivenessProbe()
	container.ReadinessProbe = datadoghqv1alpha1.GetDefaultReadinessProbe()

	if clusterAgentSpec.Config.Resources != nil {
		container.Resources = *clusterAgentSpec.Config.Resources
	}

	return newPodTemplate, nil
}

// getClusterAgentAffinity returns the pod anti affinity of the cluster agent
// the default anti affinity prefers scheduling the runners on different nodes if possible
// for better checks stability in case of node failure.
func getClusterAgentAffinity(affinity *corev1.Affinity) *corev1.Affinity {
	if affinity != nil {
		return affinity
	}

	return &corev1.Affinity{
		PodAntiAffinity: &corev1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
				{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							apicommon.AgentDeploymentComponentLabelKey: apicommon.DefaultClusterAgentResourceSuffix,
						},
					},
					TopologyKey: "kubernetes.io/hostname",
				},
			},
		},
	}
}

func getCustomConfigSpecVolumes(customConfig *datadoghqv1alpha1.CustomConfigSpec, volumeName, defaultCMName, configFolder string) (corev1.Volume, corev1.VolumeMount) {
	var volume corev1.Volume
	var volumeMount corev1.VolumeMount
	if customConfig != nil {
		volume = objectvolume.GetVolumeFromCustomConfigSpec(
			datadoghqv1alpha1.ConvertCustomConfig(customConfig),
			defaultCMName,
			volumeName,
		)
		// subpath only updated to Filekey if config uses configMap, default to ksmCoreCheckName for configData.
		volumeMount = objectvolume.GetVolumeMountFromCustomConfigSpec(
			datadoghqv1alpha1.ConvertCustomConfig(customConfig),
			volumeName,
			fmt.Sprintf("%s%s/%s", apicommon.ConfigVolumePath, apicommon.ConfdVolumePath, configFolder),
			"",
		)
	} else {
		volume = corev1.Volume{
			Name: volumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: defaultCMName,
					},
				},
			},
		}
		volumeMount = corev1.VolumeMount{
			Name:      volumeName,
			MountPath: fmt.Sprintf("%s%s/%s", apicommon.ConfigVolumePath, apicommon.ConfdVolumePath, configFolder),
			ReadOnly:  true,
		}
	}
	return volume, volumeMount
}

func getClusterAgentCustomConfigConfigMapName(dda *datadoghqv1alpha1.DatadogAgent) string {
	return fmt.Sprintf("%s-cluster-datadog-yaml", dda.Name)
}

// getEnvVarsForClusterAgent converts Cluster Agent Config into container env vars
func getEnvVarsForClusterAgent(logger logr.Logger, dda *datadoghqv1alpha1.DatadogAgent) ([]corev1.EnvVar, error) {
	spec := &dda.Spec

	complianceEnabled := isComplianceEnabled(&dda.Spec)

	envVars := []corev1.EnvVar{
		{
			Name:  datadoghqv1alpha1.DDClusterChecksEnabled,
			Value: strconv.FormatBool(isClusterChecksEnabled(&dda.Spec)),
		},
		{
			Name:  datadoghqv1alpha1.DDClusterAgentKubeServiceName,
			Value: getClusterAgentServiceName(dda),
		},
		{
			Name:      datadoghqv1alpha1.DDClusterAgentAuthToken,
			ValueFrom: getClusterAgentAuthToken(dda),
		},
		{
			Name:  datadoghqv1alpha1.DDLeaderElection,
			Value: "true",
		},
		{
			Name:  datadoghqv1alpha1.DDComplianceConfigEnabled,
			Value: strconv.FormatBool(complianceEnabled),
		},
		{
			Name:  datadoghqv1alpha1.DDCollectKubeEvents,
			Value: apiutils.BoolToString(spec.ClusterAgent.Config.CollectEvents),
		},
		{
			Name:  datadoghqv1alpha1.DDHealthPort,
			Value: strconv.Itoa(int(*spec.ClusterAgent.Config.HealthPort)),
		},
	}

	if spec.ClusterName != "" {
		envVars = append(envVars, corev1.EnvVar{
			Name:  datadoghqv1alpha1.DDClusterName,
			Value: spec.ClusterName,
		})
	}

	if complianceEnabled {
		if dda.Spec.Agent.Security.Compliance.CheckInterval != nil {
			envVars = append(envVars, corev1.EnvVar{
				Name:  datadoghqv1alpha1.DDComplianceConfigCheckInterval,
				Value: strconv.FormatInt(dda.Spec.Agent.Security.Compliance.CheckInterval.Nanoseconds(), 10),
			})
		}

		if dda.Spec.Agent.Security.Compliance.ConfigDir != nil {
			envVars = append(envVars, corev1.EnvVar{
				Name:  datadoghqv1alpha1.DDComplianceConfigDir,
				Value: datadoghqv1alpha1.SecurityAgentComplianceConfigDirVolumePath,
			})
		}
	}

	// TODO We should be able to disable the agent and still configure the Endpoint for the Cluster Agent.
	if apiutils.BoolValue(dda.Spec.Agent.Enabled) && spec.Agent.Config.DDUrl != nil {
		envVars = append(envVars, corev1.EnvVar{
			Name:  datadoghqv1alpha1.DDddURL,
			Value: *spec.Agent.Config.DDUrl,
		})
	}

	envVars = append(envVars, corev1.EnvVar{
		Name:  datadoghqv1alpha1.DDLogLevel,
		Value: *spec.ClusterAgent.Config.LogLevel,
	})

	envVars = append(envVars, corev1.EnvVar{
		Name:      datadoghqv1alpha1.DDAPIKey,
		ValueFrom: getAPIKeyFromSecret(dda),
	})

	if spec.Site != "" {
		envVars = append(envVars, corev1.EnvVar{
			Name:  datadoghqv1alpha1.DDSite,
			Value: spec.Site,
		})
	}

	if isMetricsProviderEnabled(spec.ClusterAgent) {
		envVars = append(envVars, corev1.EnvVar{
			Name:  datadoghqv1alpha1.DDMetricsProviderEnabled,
			Value: strconv.FormatBool(*spec.ClusterAgent.Config.ExternalMetrics.Enabled),
		})
		envVars = append(envVars, corev1.EnvVar{
			Name:  datadoghqv1alpha1.DDMetricsProviderPort,
			Value: strconv.Itoa(int(getClusterAgentMetricsProviderPort(*spec.ClusterAgent.Config))),
		})
		envVars = append(envVars, corev1.EnvVar{
			Name:      datadoghqv1alpha1.DDAppKey,
			ValueFrom: getAppKeyFromSecret(dda),
		})
		envVars = append(envVars, corev1.EnvVar{
			Name:  datadoghqv1alpha1.DDMetricsProviderUseDatadogMetric,
			Value: strconv.FormatBool(spec.ClusterAgent.Config.ExternalMetrics.UseDatadogMetrics),
		})
		envVars = append(envVars, corev1.EnvVar{
			Name:  datadoghqv1alpha1.DDMetricsProviderWPAController,
			Value: strconv.FormatBool(spec.ClusterAgent.Config.ExternalMetrics.WpaController),
		})

		externalMetricsEndpoint := dda.Spec.ClusterAgent.Config.ExternalMetrics.Endpoint
		if externalMetricsEndpoint != nil && *externalMetricsEndpoint != "" {
			envVars = append(envVars, corev1.EnvVar{
				Name:  datadoghqv1alpha1.DDExternalMetricsProviderEndpoint,
				Value: *externalMetricsEndpoint,
			})
		}

		if hasMetricsProviderCustomCredentials(spec.ClusterAgent) {
			apiSet, secretName, secretKey := utils.GetAPIKeySecret(dda.Spec.ClusterAgent.Config.ExternalMetrics.Credentials, getDefaultExternalMetricSecretName(dda))
			if apiSet {
				envVars = append(envVars, corev1.EnvVar{
					Name:      datadoghqv1alpha1.DDExternalMetricsProviderAPIKey,
					ValueFrom: buildEnvVarFromSecret(secretName, secretKey),
				})
			}

			appSet, secretName, secretKey := utils.GetAppKeySecret(dda.Spec.ClusterAgent.Config.ExternalMetrics.Credentials, getDefaultExternalMetricSecretName(dda))
			if appSet {
				envVars = append(envVars, corev1.EnvVar{
					Name:      datadoghqv1alpha1.DDExternalMetricsProviderAppKey,
					ValueFrom: buildEnvVarFromSecret(secretName, secretKey),
				})
			}
		}
	}

	// Cluster Checks config
	if apiutils.BoolValue(spec.ClusterAgent.Config.ClusterChecksEnabled) {
		envVars = append(envVars, []corev1.EnvVar{
			{
				Name:  datadoghqv1alpha1.DDExtraConfigProviders,
				Value: datadoghqv1alpha1.KubeServicesAndEndpointsConfigProviders,
			},
			{
				Name:  datadoghqv1alpha1.DDExtraListeners,
				Value: datadoghqv1alpha1.KubeServicesAndEndpointsListeners,
			},
		}...)
	}

	if isAdmissionControllerEnabled(spec.ClusterAgent) {
		envVars = append(envVars, corev1.EnvVar{
			Name:  datadoghqv1alpha1.DDAdmissionControllerEnabled,
			Value: strconv.FormatBool(*spec.ClusterAgent.Config.AdmissionController.Enabled),
		})
		envVars = append(envVars, corev1.EnvVar{
			Name:  datadoghqv1alpha1.DDAdmissionControllerMutateUnlabelled,
			Value: apiutils.BoolToString(spec.ClusterAgent.Config.AdmissionController.MutateUnlabelled),
		})
		envVars = append(envVars, corev1.EnvVar{
			Name:  datadoghqv1alpha1.DDAdmissionControllerServiceName,
			Value: getAdmissionControllerServiceName(dda),
		})
	}

	if isOrchestratorExplorerEnabled(dda) {
		envs, err := orchestrator.EnvVars(spec.Features.OrchestratorExplorer)
		if err != nil {
			return nil, err
		}

		envVars = append(envVars, envs...)
	}

	envVars = append(envVars, prometheusScrapeEnvVars(logger, dda)...)

	return append(envVars, spec.ClusterAgent.Config.Env...), nil
}

func getClusterAgentName(dda *datadoghqv1alpha1.DatadogAgent) string {
	if isClusterAgentEnabled(dda.Spec.ClusterAgent) && dda.Spec.ClusterAgent.DeploymentName != "" {
		return dda.Spec.ClusterAgent.DeploymentName
	}
	return fmt.Sprintf("%s-%s", dda.Name, "cluster-agent")
}

func getClusterAgentMetricsProviderPort(config datadoghqv1alpha1.ClusterAgentConfig) int32 {
	if config.ExternalMetrics != nil {
		return *config.ExternalMetrics.Port
	}
	return int32(apicommon.DefaultMetricsServerTargetPort)
}

func getAdmissionControllerServiceName(dda *datadoghqv1alpha1.DatadogAgent) string {
	if isClusterAgentEnabled(dda.Spec.ClusterAgent) &&
		dda.Spec.ClusterAgent.Config != nil &&
		dda.Spec.ClusterAgent.Config.AdmissionController != nil &&
		dda.Spec.ClusterAgent.Config.AdmissionController.ServiceName != nil {
		return *dda.Spec.ClusterAgent.Config.AdmissionController.ServiceName
	}
	return datadoghqv1alpha1.DefaultAdmissionServiceName
}

// manageClusterAgentRBACs creates deletes and updates the RBACs for the Cluster Agent
func (r *Reconciler) manageClusterAgentRBACs(logger logr.Logger, dda *datadoghqv1alpha1.DatadogAgent) (reconcile.Result, error) {
	if !isClusterAgentEnabled(dda.Spec.ClusterAgent) {
		return r.cleanupClusterAgentRbacResources(logger, dda)
	}

	if !isCreateRBACEnabled(dda.Spec.ClusterAgent.Rbac) {
		return reconcile.Result{}, nil
	}

	clusterAgentVersion := getClusterAgentVersion(dda)

	// Create ServiceAccount
	serviceAccountName := getClusterAgentServiceAccount(dda)
	serviceAccount := &corev1.ServiceAccount{}
	if err := r.client.Get(context.TODO(), types.NamespacedName{Name: serviceAccountName, Namespace: dda.Namespace}, serviceAccount); err != nil {
		if errors.IsNotFound(err) {
			return r.createServiceAccount(logger, dda, serviceAccountName, clusterAgentVersion)
		}
		return reconcile.Result{}, err
	}

	rbacResourcesName := getClusterAgentRbacResourcesName(dda)

	// Create or update ClusterRole
	clusterRole := &rbacv1.ClusterRole{}
	if err := r.client.Get(context.TODO(), types.NamespacedName{Name: rbacResourcesName}, clusterRole); err != nil {
		if errors.IsNotFound(err) {
			return r.createClusterAgentClusterRole(logger, dda, rbacResourcesName, clusterAgentVersion)
		}
		return reconcile.Result{}, err
	}
	if result, err := r.updateIfNeededClusterAgentClusterRole(logger, dda, rbacResourcesName, clusterAgentVersion, clusterRole); err != nil {
		return result, err
	}

	// Create ClusterRoleBinding
	clusterRoleBinding := &rbacv1.ClusterRoleBinding{}
	if err := r.client.Get(context.TODO(), types.NamespacedName{Name: rbacResourcesName}, clusterRoleBinding); err != nil {
		if errors.IsNotFound(err) {
			return r.createClusterRoleBindingFromInfo(logger, dda, roleBindingInfo{
				name:               rbacResourcesName,
				roleName:           rbacResourcesName,
				serviceAccountName: serviceAccountName,
			}, clusterAgentVersion)
		}
		return reconcile.Result{}, err
	}
	if result, err := r.updateIfNeededClusterRoleBinding(logger, dda, rbacResourcesName, rbacResourcesName, serviceAccountName, clusterAgentVersion, clusterRoleBinding); err != nil {
		return result, err
	}

	// Create or update Role
	role := &rbacv1.Role{}
	if err := r.client.Get(context.TODO(), types.NamespacedName{Name: rbacResourcesName, Namespace: dda.Namespace}, role); err != nil {
		if errors.IsNotFound(err) {
			return r.createClusterAgentRole(logger, dda, rbacResourcesName, clusterAgentVersion)
		}
		return reconcile.Result{}, err
	}
	if result, err := r.updateIfNeededClusterAgentRole(logger, dda, rbacResourcesName, clusterAgentVersion, role); err != nil {
		return result, err
	}

	// Create or update RoleBinding
	roleBinding := &rbacv1.RoleBinding{}
	if err := r.client.Get(context.TODO(), types.NamespacedName{Name: rbacResourcesName, Namespace: dda.Namespace}, roleBinding); err != nil {
		if errors.IsNotFound(err) {
			info := roleBindingInfo{
				name:               rbacResourcesName,
				roleName:           rbacResourcesName,
				serviceAccountName: getClusterAgentServiceAccount(dda),
			}
			return r.createClusterAgentRoleBinding(logger, dda, info, clusterAgentVersion)
		}
		return reconcile.Result{}, err
	}

	if isOrchestratorExplorerEnabled(dda) && !isOrchestratorExplorerClusterCheck(dda) {
		if result, err := r.createOrUpdateOrchestratorCoreRBAC(logger, dda, serviceAccountName, clusterAgentVersion, clusterAgentSuffix); err != nil {
			return result, err
		}
	} else if result, err := r.cleanupOrchestratorCoreRBAC(logger, dda, clusterAgentSuffix); err != nil {
		return result, err
	}

	metricsProviderEnabled := isMetricsProviderEnabled(dda.Spec.ClusterAgent)
	// Create or delete HPA ClusterRoleBinding
	hpaClusterRoleBindingName := getHPAClusterRoleBindingName(dda)
	if result, err := r.manageClusterRoleBinding(logger, dda, hpaClusterRoleBindingName, clusterAgentVersion, r.createHPAClusterRoleBinding, r.updateIfNeededHPAClusterRole, metricsProviderEnabled); err != nil {
		return result, err
	}

	// Create or delete external metrics reader ClusterRole and ClusterRoleBinding
	metricsReaderClusterRoleName := getExternalMetricsReaderClusterRoleName(dda, r.versionInfo)
	if result, err := r.manageClusterRole(logger, dda, metricsReaderClusterRoleName, clusterAgentVersion, r.createExternalMetricsReaderClusterRole, r.updateIfNeededExternalMetricsReaderClusterRole, metricsProviderEnabled); err != nil {
		return result, err
	}

	if result, err := r.manageClusterRoleBinding(logger, dda, metricsReaderClusterRoleName, clusterAgentVersion, r.createExternalMetricsReaderClusterRoleBinding, r.updateIfNeededClusterAgentClusterRoleBinding, metricsProviderEnabled); err != nil {
		return result, err
	}

	return reconcile.Result{}, nil
}

func (r *Reconciler) createClusterAgentClusterRole(logger logr.Logger, dda *datadoghqv1alpha1.DatadogAgent, name, agentVersion string) (reconcile.Result, error) {
	clusterRole := buildClusterAgentClusterRole(dda, name, agentVersion)
	logger.V(1).Info("createClusterAgentClusterRole", "clusterRole.name", clusterRole.Name)
	event := buildEventInfo(clusterRole.Name, clusterRole.Namespace, clusterRoleKind, datadog.CreationEvent)
	r.recordEvent(dda, event)
	return reconcile.Result{Requeue: true}, r.client.Create(context.TODO(), clusterRole)
}

func (r *Reconciler) createClusterAgentRole(logger logr.Logger, dda *datadoghqv1alpha1.DatadogAgent, name, agentVersion string) (reconcile.Result, error) {
	role := buildClusterAgentRole(dda, name, agentVersion)
	if err := controllerutil.SetControllerReference(dda, role, r.scheme); err != nil {
		return reconcile.Result{}, err
	}
	logger.V(1).Info("createClusterAgentRole", "role.name", role.Name)
	event := buildEventInfo(role.Name, role.Namespace, roleKind, datadog.CreationEvent)
	r.recordEvent(dda, event)
	return reconcile.Result{Requeue: true}, r.client.Create(context.TODO(), role)
}

func (r *Reconciler) createAgentClusterRole(logger logr.Logger, dda *datadoghqv1alpha1.DatadogAgent, name, agentVersion string) (reconcile.Result, error) {
	clusterRole := buildAgentClusterRole(dda, name, agentVersion)
	logger.V(1).Info("createAgentClusterRole", "clusterRole.name", clusterRole.Name)
	event := buildEventInfo(clusterRole.Name, clusterRole.Namespace, clusterRoleKind, datadog.CreationEvent)
	r.recordEvent(dda, event)
	err := r.client.Create(context.TODO(), clusterRole)
	return reconcile.Result{Requeue: true}, err
}

func (r *Reconciler) createClusterCheckRunnerClusterRole(logger logr.Logger, dda *datadoghqv1alpha1.DatadogAgent, name, agentVersion string) (reconcile.Result, error) {
	clusterRole := buildClusterCheckRunnerClusterRole(dda, name, agentVersion)
	logger.V(1).Info("createAgentClusterRole", "clusterRole.name", clusterRole.Name)
	event := buildEventInfo(clusterRole.Name, clusterRole.Namespace, clusterRoleKind, datadog.CreationEvent)
	r.recordEvent(dda, event)
	err := r.client.Create(context.TODO(), clusterRole)
	return reconcile.Result{Requeue: true}, err
}

func (r *Reconciler) updateIfNeededClusterAgentClusterRole(logger logr.Logger, dda *datadoghqv1alpha1.DatadogAgent, name, agentVersion string, clusterRole *rbacv1.ClusterRole) (reconcile.Result, error) {
	newClusterRole := buildClusterAgentClusterRole(dda, name, agentVersion)
	return r.updateIfNeededClusterRole(logger, dda, clusterRole, newClusterRole)
}

func (r *Reconciler) updateIfNeededClusterAgentRole(logger logr.Logger, dda *datadoghqv1alpha1.DatadogAgent, name, agentVersion string, role *rbacv1.Role) (reconcile.Result, error) {
	newRole := buildClusterAgentRole(dda, name, agentVersion)
	return r.updateIfNeededRole(logger, dda, role, newRole)
}

func (r *Reconciler) updateIfNeededAgentClusterRole(logger logr.Logger, dda *datadoghqv1alpha1.DatadogAgent, name, agentVersion string, clusterRole *rbacv1.ClusterRole) (reconcile.Result, error) {
	newClusterRole := buildAgentClusterRole(dda, name, agentVersion)
	return r.updateIfNeededClusterRole(logger, dda, clusterRole, newClusterRole)
}

func (r *Reconciler) updateIfNeededClusterCheckRunnerClusterRole(logger logr.Logger, dda *datadoghqv1alpha1.DatadogAgent, name, agentVersion string, clusterRole *rbacv1.ClusterRole) (reconcile.Result, error) {
	newClusterRole := buildClusterCheckRunnerClusterRole(dda, name, agentVersion)
	return r.updateIfNeededClusterRole(logger, dda, clusterRole, newClusterRole)
}

// cleanupClusterAgentRbacResources deletes ClusterRole, ClusterRoleBindings, and ServiceAccount of the Cluster Agent
func (r *Reconciler) cleanupClusterAgentRbacResources(logger logr.Logger, dda *datadoghqv1alpha1.DatadogAgent) (reconcile.Result, error) {
	rbacResourcesName := getClusterAgentRbacResourcesName(dda)
	// Delete ClusterRole
	if result, err := r.cleanupClusterRole(logger, dda, rbacResourcesName); err != nil {
		return result, err
	}
	// Delete Cluster Role Binding
	if result, err := r.cleanupClusterRoleBinding(logger, dda, rbacResourcesName); err != nil {
		return result, err
	}
	// Delete HPA Cluster Role Binding
	hpaClusterRoleBindingName := getHPAClusterRoleBindingName(dda)
	if result, err := r.cleanupClusterRoleBinding(logger, dda, hpaClusterRoleBindingName); err != nil {
		return result, err
	}

	externalMetricsReaderName := getExternalMetricsReaderClusterRoleName(dda, r.versionInfo)
	if result, err := r.cleanupClusterRoleBinding(logger, dda, externalMetricsReaderName); err != nil {
		return result, err
	}

	if result, err := r.cleanupClusterRole(logger, dda, externalMetricsReaderName); err != nil {
		return result, err
	}

	// Delete Service Account
	if result, err := r.cleanupServiceAccount(logger, r.client, dda, rbacResourcesName); err != nil {
		return result, err
	}
	return reconcile.Result{}, nil
}

func (r *Reconciler) createClusterAgentRoleBinding(logger logr.Logger, dda *datadoghqv1alpha1.DatadogAgent, info roleBindingInfo, agentVersion string) (reconcile.Result, error) {
	roleBinding := buildRoleBinding(dda, info, agentVersion)
	if err := controllerutil.SetControllerReference(dda, roleBinding, r.scheme); err != nil {
		return reconcile.Result{}, err
	}
	logger.V(1).Info("createClusterAgentRoleBinding", "roleBinding.name", roleBinding.Name, "roleBinding.Namespace", roleBinding.Namespace, "serviceAccount", info.serviceAccountName)
	event := buildEventInfo(roleBinding.Name, roleBinding.Namespace, roleBindingKind, datadog.CreationEvent)
	r.recordEvent(dda, event)
	return reconcile.Result{}, r.client.Create(context.TODO(), roleBinding)
}

func (r *Reconciler) updateIfNeededClusterAgentClusterRoleBinding(logger logr.Logger, dda *datadoghqv1alpha1.DatadogAgent, name, agentVersion string, clusterRoleBinding *rbacv1.ClusterRoleBinding) (reconcile.Result, error) {
	info := roleBindingInfo{
		name:               name,
		roleName:           getClusterAgentRbacResourcesName(dda),
		serviceAccountName: getClusterAgentServiceAccount(dda),
	}
	newRoleBinding := buildClusterRoleBinding(dda, info, agentVersion)
	return r.updateIfNeededClusterRoleBindingRaw(logger, dda, clusterRoleBinding, newRoleBinding)
}

// buildAgentClusterRole creates a ClusterRole object for the Agent based on its config
func buildAgentClusterRole(dda *datadoghqv1alpha1.DatadogAgent, name, version string) *rbacv1.ClusterRole {
	return buildClusterRole(dda, !isClusterAgentEnabled(dda.Spec.ClusterAgent), name, version)
}

// buildClusterCheckRunnerClusterRole creates a ClusterRole object for the ClusterCheckRunner based on its config
func buildClusterCheckRunnerClusterRole(dda *datadoghqv1alpha1.DatadogAgent, name, version string) *rbacv1.ClusterRole {
	return buildClusterRole(dda, true, name, version)
}

// buildClusterRole creates a ClusterRole object for the Agent based on its config
func buildClusterRole(dda *datadoghqv1alpha1.DatadogAgent, needClusterLevelRBAC bool, name, version string) *rbacv1.ClusterRole {
	clusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Labels: getDefaultLabels(dda, NewPartOfLabelValue(dda).String(), version),
			Name:   name,
		},
	}

	rbacRules := []rbacv1.PolicyRule{
		{
			// Get /metrics permissions
			NonResourceURLs: []string{rbac.MetricsURL},
			Verbs:           []string{rbac.GetVerb},
		},
		{
			// Kubelet connectivity
			APIGroups: []string{rbac.CoreAPIGroup},
			Resources: []string{
				rbac.NodeMetricsResource,
				rbac.NodeSpecResource,
				rbac.NodeProxyResource,
				rbac.NodeStats,
			},
			Verbs: []string{rbac.GetVerb},
		},
		{
			// Leader election check
			APIGroups: []string{rbac.CoreAPIGroup},
			Resources: []string{rbac.EndpointsResource},
			Verbs:     []string{rbac.GetVerb},
		},
		{
			// Leader election check
			APIGroups: []string{rbac.CoordinationAPIGroup},
			Resources: []string{rbac.LeasesResource},
			Verbs:     []string{rbac.GetVerb},
		},
	}

	if needClusterLevelRBAC {
		// Cluster Agent is disabled, the Agent needs extra permissions
		// to collect cluster level metrics and events
		rbacRules = append(rbacRules, getDefaultClusterAgentPolicyRules()...)

		if apiutils.BoolValue(dda.Spec.Agent.Enabled) {
			if apiutils.BoolValue(dda.Spec.Agent.Config.CollectEvents) {
				rbacRules = append(rbacRules, getEventCollectionPolicyRule())
			}

			if apiutils.BoolValue(dda.Spec.Agent.Config.LeaderElection) {
				rbacRules = append(rbacRules, getLeaderElectionPolicyRule()...)
			}
		}
	}

	clusterRole.Rules = rbacRules

	return clusterRole
}

// getDefaultClusterAgentPolicyRules returns the default policy rules for the Cluster Agent
// Can be used by the Agent if the Cluster Agent is disabled
func getDefaultClusterAgentPolicyRules() []rbacv1.PolicyRule {
	return append([]rbacv1.PolicyRule{
		{
			APIGroups: []string{rbac.CoreAPIGroup},
			Resources: []string{
				rbac.ServicesResource,
				rbac.EventsResource,
				rbac.EndpointsResource,
				rbac.PodsResource,
				rbac.NodesResource,
				rbac.ComponentStatusesResource,
				rbac.ConfigMapsResource,
				rbac.NamespaceResource,
			},
			Verbs: []string{
				rbac.GetVerb,
				rbac.ListVerb,
				rbac.WatchVerb,
			},
		},
		{
			APIGroups: []string{rbac.OpenShiftQuotaAPIGroup},
			Resources: []string{rbac.ClusterResourceQuotasResource},
			Verbs:     []string{rbac.GetVerb, rbac.ListVerb},
		},
		{
			NonResourceURLs: []string{rbac.VersionURL, rbac.HealthzURL},
			Verbs:           []string{rbac.GetVerb},
		},
	}, getLeaderElectionPolicyRule()...)
}

// buildClusterRoleBinding creates a ClusterRoleBinding object
func buildClusterRoleBinding(dda *datadoghqv1alpha1.DatadogAgent, info roleBindingInfo, agentVersion string) *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Labels: getDefaultLabels(dda, NewPartOfLabelValue(dda).String(), agentVersion),
			Name:   info.name,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbac.RbacAPIGroup,
			Kind:     rbac.ClusterRoleKind,
			Name:     info.roleName,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbac.ServiceAccountKind,
				Name:      info.serviceAccountName,
				Namespace: dda.Namespace,
			},
		},
	}
}

// buildClusterAgentClusterRole creates a ClusterRole object for the Cluster Agent based on its config
func buildClusterAgentClusterRole(dda *datadoghqv1alpha1.DatadogAgent, name, agentVersion string) *rbacv1.ClusterRole {
	clusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Labels: getDefaultLabels(dda, NewPartOfLabelValue(dda).String(), agentVersion),
			Name:   name,
		},
	}

	rbacRules := getDefaultClusterAgentPolicyRules()

	rbacRules = append(rbacRules, rbacv1.PolicyRule{
		// Horizontal Pod Autoscaling
		APIGroups: []string{rbac.AutoscalingAPIGroup},
		Resources: []string{rbac.HorizontalPodAutoscalersRecource},
		Verbs:     []string{rbac.ListVerb, rbac.WatchVerb},
	})

	rbacRules = append(rbacRules, rbacv1.PolicyRule{
		APIGroups: []string{rbac.CoreAPIGroup},
		Resources: []string{rbac.NamespaceResource},
		ResourceNames: []string{
			common.KubeSystemResourceName,
		},
		Verbs: []string{rbac.GetVerb},
	})

	if apiutils.BoolValue(dda.Spec.ClusterAgent.Config.CollectEvents) {
		rbacRules = append(rbacRules, getEventCollectionPolicyRule())
	}

	if isMetricsProviderEnabled(dda.Spec.ClusterAgent) {
		rbacRules = append(rbacRules,
			rbacv1.PolicyRule{
				APIGroups: []string{rbac.CoreAPIGroup},
				Resources: []string{rbac.ConfigMapsResource},
				ResourceNames: []string{
					common.DatadogCustomMetricsResourceName,
				},
				Verbs: []string{
					rbac.GetVerb,
					rbac.UpdateVerb,
				},
			},
			rbacv1.PolicyRule{
				APIGroups: []string{rbac.CoreAPIGroup},
				Resources: []string{rbac.ConfigMapsResource},
				ResourceNames: []string{
					common.ExtensionAPIServerAuthResourceName,
				},
				Verbs: []string{
					rbac.GetVerb,
					rbac.ListVerb,
					rbac.WatchVerb,
				},
			},
		)

		if dda.Spec.ClusterAgent.Config.ExternalMetrics.UseDatadogMetrics {
			rbacRules = append(rbacRules, rbacv1.PolicyRule{
				APIGroups: []string{rbac.DatadogAPIGroup},
				Resources: []string{rbac.DatadogMetricsResource},
				Verbs: []string{
					rbac.ListVerb,
					rbac.WatchVerb,
					rbac.CreateVerb,
					rbac.DeleteVerb,
				},
			})

			// Specific update rule for status subresource
			rbacRules = append(rbacRules, rbacv1.PolicyRule{
				APIGroups: []string{rbac.DatadogAPIGroup},
				Resources: []string{rbac.DatadogMetricsStatusResource},
				Verbs:     []string{rbac.UpdateVerb},
			})
		}

		if dda.Spec.ClusterAgent.Config.ExternalMetrics.WpaController {
			rbacRules = append(rbacRules, rbacv1.PolicyRule{
				APIGroups: []string{rbac.DatadogAPIGroup},
				Resources: []string{rbac.WpaResource},
				Verbs: []string{
					rbac.ListVerb,
					rbac.WatchVerb,
					rbac.GetVerb,
				},
			})
		}
	}

	if isAdmissionControllerEnabled(dda.Spec.ClusterAgent) {
		// MutatingWebhooksConfigs
		rbacRules = append(rbacRules, rbacv1.PolicyRule{
			APIGroups: []string{rbac.AdmissionAPIGroup},
			Resources: []string{rbac.MutatingConfigResource},
			Verbs: []string{
				rbac.GetVerb,
				rbac.ListVerb,
				rbac.WatchVerb,
				rbac.CreateVerb,
				rbac.UpdateVerb,
			},
		})

		// Secrets
		rbacRules = append(rbacRules, rbacv1.PolicyRule{
			APIGroups: []string{rbac.CoreAPIGroup},
			Resources: []string{rbac.SecretsResource},
			Verbs: []string{
				rbac.GetVerb,
				rbac.ListVerb,
				rbac.WatchVerb,
				rbac.CreateVerb,
				rbac.UpdateVerb,
			},
		})

		// ExtendedDaemonsetReplicaSets
		rbacRules = append(rbacRules, rbacv1.PolicyRule{
			APIGroups: []string{extendeddaemonset.GroupVersion.Group},
			Resources: []string{
				rbac.ExtendedDaemonSetReplicaSetResource,
			},
			Verbs: []string{rbac.GetVerb},
		})

		// Deployments, Replicasets, Statefulsets, Daemonsets,
		rbacRules = append(rbacRules, rbacv1.PolicyRule{
			APIGroups: []string{rbac.AppsAPIGroup},
			Resources: []string{
				rbac.DeploymentsResource,
				rbac.ReplicasetsResource,
				rbac.StatefulsetsResource,
				rbac.DaemonsetsResource,
			},
			Verbs: []string{rbac.GetVerb},
		})

		// Jobs
		rbacRules = append(rbacRules, rbacv1.PolicyRule{
			APIGroups: []string{rbac.BatchAPIGroup},
			Resources: []string{rbac.JobsResource},
			Verbs: []string{
				rbac.ListVerb,
				rbac.WatchVerb,
				rbac.GetVerb,
			},
		})

		// CronJobs
		rbacRules = append(rbacRules, rbacv1.PolicyRule{
			APIGroups: []string{rbac.BatchAPIGroup},
			Resources: []string{rbac.CronjobsResource},
			Verbs: []string{
				rbac.ListVerb,
				rbac.WatchVerb,
				rbac.GetVerb,
			},
		})
	}

	if isComplianceEnabled(&dda.Spec) {
		// ServiceAccounts and Namespaces
		rbacRules = append(rbacRules, rbacv1.PolicyRule{
			APIGroups: []string{rbac.CoreAPIGroup},
			Resources: []string{rbac.ServiceAccountResource, rbac.NamespaceResource},
			Verbs: []string{
				rbac.ListVerb,
			},
		})

		// PodSecurityPolicies
		rbacRules = append(rbacRules, rbacv1.PolicyRule{
			APIGroups: []string{rbac.PolicyAPIGroup},
			Resources: []string{rbac.PodSecurityPolicyResource},
			Verbs: []string{
				rbac.ListVerb,
				rbac.GetVerb,
				rbac.ListVerb,
				rbac.WatchVerb,
			},
		})

		// ClusterRoleBindings and RoleBindings
		rbacRules = append(rbacRules, rbacv1.PolicyRule{
			APIGroups: []string{rbac.RbacAPIGroup},
			Resources: []string{rbac.ClusterRoleBindingResource, rbac.RoleBindingResource},
			Verbs: []string{
				rbac.ListVerb,
			},
		})

		// NetworkPolicies
		rbacRules = append(rbacRules, rbacv1.PolicyRule{
			APIGroups: []string{rbac.NetworkingAPIGroup},
			Resources: []string{rbac.NetworkPolicyResource},
			Verbs: []string{
				rbac.ListVerb,
			},
		})
	}

	if isOrchestratorExplorerEnabled(dda) {
		// To get the kube-system namespace UID and generate a cluster ID
		rbacRules = append(rbacRules, rbacv1.PolicyRule{
			APIGroups:     []string{rbac.CoreAPIGroup},
			Resources:     []string{rbac.NamespaceResource},
			ResourceNames: []string{common.KubeSystemResourceName},
			Verbs:         []string{rbac.GetVerb},
		})
		// To create the cluster-id configmap
		rbacRules = append(rbacRules, rbacv1.PolicyRule{
			APIGroups:     []string{rbac.CoreAPIGroup},
			Resources:     []string{rbac.ConfigMapsResource},
			ResourceNames: []string{common.DatadogClusterIDResourceName},
			Verbs:         []string{rbac.GetVerb, rbac.CreateVerb, rbac.UpdateVerb},
		})

		rbacRules = append(rbacRules, rbacv1.PolicyRule{
			APIGroups: []string{rbac.AppsAPIGroup},
			Resources: []string{rbac.DeploymentsResource, rbac.ReplicasetsResource, rbac.DaemonsetsResource, rbac.StatefulsetsResource},
			Verbs:     []string{rbac.GetVerb, rbac.ListVerb, rbac.WatchVerb},
		})
	}

	clusterRole.Rules = rbacRules

	return clusterRole
}

// buildClusterAgentRole creates a Role object for the Cluster Agent based on its config
func buildClusterAgentRole(dda *datadoghqv1alpha1.DatadogAgent, name, agentVersion string) *rbacv1.Role {
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Labels:    getDefaultLabels(dda, dda.Name, agentVersion),
			Name:      name,
			Namespace: dda.Namespace,
		},
	}

	rbacRules := getLeaderElectionPolicyRule()

	rbacRules = append(rbacRules, rbacv1.PolicyRule{
		APIGroups: []string{rbac.CoreAPIGroup},
		Resources: []string{rbac.ConfigMapsResource},
		ResourceNames: []string{
			common.DatadogClusterIDResourceName,
		},
		Verbs: []string{rbac.GetVerb, rbac.UpdateVerb, rbac.CreateVerb},
	})

	if isMetricsProviderEnabled(dda.Spec.ClusterAgent) {
		rbacRules = append(rbacRules,
			rbacv1.PolicyRule{
				APIGroups: []string{rbac.CoreAPIGroup},
				Resources: []string{rbac.ConfigMapsResource},
				ResourceNames: []string{
					common.DatadogCustomMetricsResourceName,
				},
				Verbs: []string{
					rbac.GetVerb,
					rbac.UpdateVerb,
				},
			},
			rbacv1.PolicyRule{
				APIGroups: []string{rbac.CoreAPIGroup},
				Resources: []string{rbac.ConfigMapsResource},
				ResourceNames: []string{
					common.ExtensionAPIServerAuthResourceName,
				},
				Verbs: []string{
					rbac.GetVerb,
					rbac.ListVerb,
					rbac.WatchVerb,
				},
			},
		)
	}

	role.Rules = rbacRules

	return role
}

func (r *Reconciler) manageClusterAgentNetworkPolicy(logger logr.Logger, dda *datadoghqv1alpha1.DatadogAgent) (reconcile.Result, error) {
	spec := dda.Spec.ClusterAgent
	builder := clusterAgentNetworkPolicyBuilder{dda, spec.NetworkPolicy}
	if !apiutils.BoolValue(spec.Enabled) || spec.NetworkPolicy == nil || !apiutils.BoolValue(spec.NetworkPolicy.Create) {
		return r.cleanupNetworkPolicy(logger, dda, builder.Name())
	}

	return r.ensureNetworkPolicy(logger, dda, builder)
}

type clusterAgentNetworkPolicyBuilder struct {
	dda *datadoghqv1alpha1.DatadogAgent
	np  *datadoghqv1alpha1.NetworkPolicySpec
}

func (b clusterAgentNetworkPolicyBuilder) Name() string {
	return fmt.Sprintf("%s-%s", b.dda.Name, apicommon.DefaultClusterAgentResourceSuffix)
}

func (b clusterAgentNetworkPolicyBuilder) NetworkPolicySpec() *datadoghqv1alpha1.NetworkPolicySpec {
	return b.np
}

func (b clusterAgentNetworkPolicyBuilder) BuildKubernetesPolicy() *networkingv1.NetworkPolicy {
	dda := b.dda
	name := b.Name()

	egressRules := []networkingv1.NetworkPolicyEgressRule{
		// Egress to datadog intake and
		// kubeapi server
		{
			Ports: []networkingv1.NetworkPolicyPort{
				{
					Port: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 443,
					},
				},
			},
		},
	}

	ingressRules := []networkingv1.NetworkPolicyIngressRule{
		// Ingress for the node agents
		{
			Ports: []networkingv1.NetworkPolicyPort{
				{
					Port: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: apicommon.DefaultClusterAgentServicePort,
					},
				},
			},
			From: []networkingv1.NetworkPolicyPeer{
				{
					PodSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							kubernetes.AppKubernetesInstanceLabelKey: apicommon.DefaultAgentResourceSuffix,
							kubernetes.AppKubernetesPartOfLabelKey:   NewPartOfLabelValue(dda).String(),
						},
					},
				},
			},
		},
	}

	if apiutils.BoolValue(dda.Spec.ClusterAgent.Config.ClusterChecksEnabled) {
		ingressRules = append(ingressRules, networkingv1.NetworkPolicyIngressRule{
			Ports: []networkingv1.NetworkPolicyPort{
				{
					Port: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: apicommon.DefaultClusterAgentServicePort,
					},
				},
			},
			From: []networkingv1.NetworkPolicyPeer{
				{
					PodSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							kubernetes.AppKubernetesInstanceLabelKey: apicommon.DefaultClusterChecksRunnerResourceSuffix,
							kubernetes.AppKubernetesPartOfLabelKey:   NewPartOfLabelValue(dda).String(),
						},
					},
				},
			},
		})
	}

	if isMetricsProviderEnabled(dda.Spec.ClusterAgent) {
		ingressRules = append(ingressRules, networkingv1.NetworkPolicyIngressRule{
			Ports: []networkingv1.NetworkPolicyPort{
				{
					Port: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: int32(apicommon.DefaultMetricsServerTargetPort),
					},
				},
			},
		})
	}

	policy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Labels:    getDefaultLabels(dda, apicommon.DefaultClusterAgentResourceSuffix, getClusterAgentVersion(dda)),
			Name:      name,
			Namespace: dda.Namespace,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: b.PodSelector(),
			Ingress:     ingressRules,
			Egress:      egressRules,
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
		},
	}
	return policy
}

func (b clusterAgentNetworkPolicyBuilder) PodSelector() metav1.LabelSelector {
	return metav1.LabelSelector{
		MatchLabels: map[string]string{
			kubernetes.AppKubernetesInstanceLabelKey: apicommon.DefaultClusterAgentResourceSuffix,
			kubernetes.AppKubernetesPartOfLabelKey:   NewPartOfLabelValue(b.dda).String(),
		},
	}
}

func (b clusterAgentNetworkPolicyBuilder) ddFQDNs() []cilium.FQDNSelector {
	selectors := []cilium.FQDNSelector{}

	ddURL := b.dda.Spec.Agent.Config.DDUrl
	if ddURL != nil {
		selectors = append(selectors, cilium.FQDNSelector{
			MatchName: strings.TrimPrefix(*ddURL, "https://"),
		})
	}

	var site string
	if b.dda.Spec.Site != "" {
		site = b.dda.Spec.Site
	} else {
		site = defaultSite
	}

	selectors = append(selectors, []cilium.FQDNSelector{
		{
			MatchPattern: fmt.Sprintf("*-app.agent.%s", site),
		},
		{
			MatchName: fmt.Sprintf("orchestrator.%s", site),
		},
	}...)

	return selectors
}

func (b clusterAgentNetworkPolicyBuilder) ciliumIngressAgent() cilium.NetworkPolicySpec {
	ingress := cilium.IngressRule{
		ToPorts: []cilium.PortRule{
			{
				Ports: []cilium.PortProtocol{
					{
						Port:     "5000",
						Protocol: cilium.ProtocolTCP,
					},
					{
						Port:     "5005",
						Protocol: cilium.ProtocolTCP,
					},
				},
			},
		},
	}

	if b.dda.Spec.Agent.HostNetwork {
		ingress.FromEntities = []cilium.Entity{
			cilium.EntityHost,
			cilium.EntityRemoteNode,
		}
	} else {
		ingress.FromEndpoints = []metav1.LabelSelector{
			{
				MatchLabels: map[string]string{
					kubernetes.AppKubernetesInstanceLabelKey: apicommon.DefaultAgentResourceSuffix,
					kubernetes.AppKubernetesPartOfLabelKey:   fmt.Sprintf("%s-%s", b.dda.Namespace, b.dda.Name),
				},
			},
		}
	}

	return cilium.NetworkPolicySpec{
		Description:      "Ingress from agent",
		EndpointSelector: b.PodSelector(),
		Ingress:          []cilium.IngressRule{ingress},
	}
}

func (b clusterAgentNetworkPolicyBuilder) BuildCiliumPolicy() *cilium.NetworkPolicy {
	specs := []cilium.NetworkPolicySpec{
		ciliumEgressMetadataServerRule(b),
		ciliumEgressDNS(b),
		{
			Description:      "Egress to Datadog intake",
			EndpointSelector: b.PodSelector(),
			Egress: []cilium.EgressRule{
				{
					ToFQDNs: b.ddFQDNs(),
					ToPorts: []cilium.PortRule{
						{
							Ports: []cilium.PortProtocol{
								{
									Port:     "443",
									Protocol: cilium.ProtocolTCP,
								},
							},
						},
					},
				},
			},
		},
		{
			Description: "Egress to Kube API Server",
			Egress: []cilium.EgressRule{
				{
					// ToServices works only for endpoints
					// outside of the cluster This section
					// handles the case where the control
					// plane is outside of the cluster.
					ToServices: []cilium.Service{
						{
							K8sService: &cilium.K8sServiceNamespace{
								Namespace:   "default",
								ServiceName: "kubernetes",
							},
						},
					},
					ToEntities: []cilium.Entity{
						cilium.EntityHost,
						cilium.EntityRemoteNode,
					},
					ToPorts: []cilium.PortRule{
						{
							Ports: []cilium.PortProtocol{
								{
									Port:     "443",
									Protocol: cilium.ProtocolTCP,
								},
							},
						},
					},
				},
			},
		},
		b.ciliumIngressAgent(),
	}

	if apiutils.BoolValue(b.dda.Spec.ClusterAgent.Config.ClusterChecksEnabled) {
		specs = append(specs, cilium.NetworkPolicySpec{
			Description:      "Ingress from cluster workers",
			EndpointSelector: b.PodSelector(),
			Ingress: []cilium.IngressRule{
				{
					FromEndpoints: []metav1.LabelSelector{
						{
							MatchLabels: map[string]string{
								kubernetes.AppKubernetesInstanceLabelKey: apicommon.DefaultClusterChecksRunnerResourceSuffix,
								kubernetes.AppKubernetesPartOfLabelKey:   fmt.Sprintf("%s-%s", b.dda.Namespace, b.dda.Name),
							},
						},
					},
					ToPorts: []cilium.PortRule{
						{
							Ports: []cilium.PortProtocol{
								{
									Port:     "5005",
									Protocol: cilium.ProtocolTCP,
								},
							},
						},
					},
				},
			},
		})
	}

	if apiutils.BoolValue(b.dda.Spec.ClusterAgent.Config.ExternalMetrics.Enabled) {
		specs = append(specs, cilium.NetworkPolicySpec{
			Description:      "Ingress from API server for external metrics",
			EndpointSelector: b.PodSelector(),
			Ingress: []cilium.IngressRule{
				{
					FromEntities: []cilium.Entity{cilium.EntityWorld},
					ToPorts: []cilium.PortRule{
						{
							Ports: []cilium.PortProtocol{
								{
									Port:     strconv.Itoa(int(*b.dda.Spec.ClusterAgent.Config.ExternalMetrics.Port)),
									Protocol: cilium.ProtocolTCP,
								},
							},
						},
					},
				},
			},
		})
	}

	return &cilium.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Labels:    getDefaultLabels(b.dda, apicommon.DefaultClusterAgentResourceSuffix, getClusterAgentVersion(b.dda)),
			Name:      b.Name(),
			Namespace: b.dda.Namespace,
		},
		Specs: specs,
	}
}
