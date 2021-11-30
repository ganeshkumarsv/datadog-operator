// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package override

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/errors"

	"github.com/DataDog/datadog-operator/apis/datadoghq/v2alpha1"
)

// OverridePodTemplate use to override a corev1.PodTemplateSpec with a 2alpha1.DatadogAgentPodTemplateOverride.
func OverridePodTemplateSpec(podTemplateSpec *corev1.PodTemplateSpec, override *v2alpha1.DatadogAgentPodTemplateOverride) (*corev1.PodTemplateSpec, error) {
	// TODO(clamoriniere): implement OverridePodTemplate

	var errs []error
	// Loop over container
	for _, container := range override.Containers {
		if _, err := OverrideContainer(podTemplateSpec, &container); err != nil {
			errs = append(errs, err)
		}
	}

	return nil, errors.NewAggregate(errs)
}