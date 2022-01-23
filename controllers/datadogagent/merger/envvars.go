// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package merger

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// EnvVarMergeFunction signature for corev1.EnvVar merge function
type EnvVarMergeFunction func(current, newEnv *corev1.EnvVar) (*corev1.EnvVar, error)

// DefaultEnvVarMergeFunction default corev1.EnvVar merge function
// default correspond to OverrideCurrentEnvVarMergeOption
func DefaultEnvVarMergeFunction(current, newEnv *corev1.EnvVar) (*corev1.EnvVar, error) {
	return OverrideCurrentEnvVarMergeFunction(current, newEnv)
}

// OverrideCurrentEnvVarMergeFunction used when the existing corev1.EnvVar new to be replace by the new one.
func OverrideCurrentEnvVarMergeFunction(current, newEnv *corev1.EnvVar) (*corev1.EnvVar, error) {
	return newEnv.DeepCopy(), nil
}

// IgnoreNewEnvVarMergeFunction used when the existing corev1.EnvVar needs to be kept.
func IgnoreNewEnvVarMergeFunction(current, newEnv *corev1.EnvVar) (*corev1.EnvVar, error) {
	return current.DeepCopy(), nil
}

// AppendToValueEnvVarMergeFunction used when we add the new value to the existing corev1.EnvVar.
func AppendToValueEnvVarMergeFunction(current, newEnv *corev1.EnvVar) (*corev1.EnvVar, error) {
	appendEnvVar := current.DeepCopy()
	appendEnvVar.Value = strings.Join([]string{current.Value, newEnv.Value}, " ")
	return appendEnvVar, nil
}

// ErrorOnMergeAttemptdEnvVarMergeFunction used to avoid replacing an existing EnvVar
func ErrorOnMergeAttemptdEnvVarMergeFunction(current, newEnv *corev1.EnvVar) (*corev1.EnvVar, error) {
	return nil, errMergeAttempted
}

// AddEnvVarToContainer used to add an EnvVar to a container
func AddEnvVarToContainer(container *corev1.Container, envvar *corev1.EnvVar, mergeFunc EnvVarMergeFunction) ([]corev1.EnvVar, error) {
	var found bool
	for id, cEnvVar := range container.Env {
		if envvar.Name == cEnvVar.Name {
			if mergeFunc == nil {
				mergeFunc = DefaultEnvVarMergeFunction
			}
			newEnvVar, err := mergeFunc(&cEnvVar, envvar)
			if err != nil {
				return nil, err
			}
			container.Env[id] = *newEnvVar
			found = true
		}
	}
	if !found {
		container.Env = append(container.Env, *envvar)
	}
	return container.Env, nil
}
