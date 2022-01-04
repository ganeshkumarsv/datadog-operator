// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package merger

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

// VolumeMergeFunction signature for corev1.Volume merge function
type VolumeMergeFunction func(current, newVolume *corev1.Volume) (*corev1.Volume, error)

// DefaultVolumeMergeFunction default corev1.Volume merge function
// default correspond to OverrideCurrentVolumeMergeOption
func DefaultVolumeMergeFunction(current, newVolume *corev1.Volume) (*corev1.Volume, error) {
	return OverrideCurrentVolumeMergeFunction(current, newVolume)
}

// OverrideCurrentVolumeMergeFunction used when the existing corev1.Volume new to be replace by the new one.
func OverrideCurrentVolumeMergeFunction(current, newVolume *corev1.Volume) (*corev1.Volume, error) {
	return newVolume.DeepCopy(), nil
}

// MergeConfigMapItemsVolumeMergeFunction used when the existing corev1.VolumeMount new to be replace by the new one.
func MergeConfigMapItemsVolumeMergeFunction(current, newVolume *corev1.Volume) (*corev1.Volume, error) {
	if current.ConfigMap.Name != newVolume.ConfigMap.Name {
		return newVolume.DeepCopy(), nil
	}

	mergedVolume := current.DeepCopy()
	var err error
	mergedVolume.ConfigMap, err = mergeConfigMapVolumeSource(current.ConfigMap, newVolume.ConfigMap)

	return mergedVolume, err
}

// IgnoreNewVolumeMergeFunction used when the existing corev1.Volume needs to be kept.
func IgnoreNewVolumeMergeFunction(current, newVolume *corev1.Volume) (*corev1.Volume, error) {
	return current.DeepCopy(), nil
}

// ErrorOnMergeAttemptdVolumeMergeFunction used to avoid replacing an existing Volume
func ErrorOnMergeAttemptdVolumeMergeFunction(current, newVolume *corev1.Volume) (*corev1.Volume, error) {
	return nil, errMergeAttempted
}

// AddVolumeToPod use to add a corev1.Volume to a Pod
// the mergeFunc can be provided to change the default merge behavior
func AddVolumeToPod(podSpec *corev1.PodSpec, volumeMount *corev1.Volume, mergeFunc VolumeMergeFunction) ([]corev1.Volume, error) {
	var found bool
	for id, cVolume := range podSpec.Volumes {
		if volumeMount.Name == cVolume.Name {
			if mergeFunc == nil {
				mergeFunc = DefaultVolumeMergeFunction
			}
			newVolume, err := mergeFunc(&cVolume, volumeMount)
			if err != nil {
				return nil, err
			}
			podSpec.Volumes[id] = *newVolume
			found = true
		}
	}
	if !found {
		podSpec.Volumes = append(podSpec.Volumes, *volumeMount)
	}
	return podSpec.Volumes, nil
}

func mergeConfigMapVolumeSource(a, b *corev1.ConfigMapVolumeSource) (*corev1.ConfigMapVolumeSource, error) {
	newCMS := a.DeepCopy()

	allPath := make(map[string]string)

	for _, item := range a.Items {
		allPath[item.Path] = item.Key
	}

	for key, item := range b.Items {
		if oldKey, found := allPath[item.Path]; found {
			if item.Key != oldKey {
				return nil, fmt.Errorf("path %s already used", item.Path)
			}
			// else we need to merge it
			for id, tmpItem := range newCMS.Items {
				if tmpItem.Key == item.Key {
					newCMS.Items[id].Mode = mergeMode(tmpItem.Mode, item.Mode)
				}
			}
		} else {
			newCMS.Items = append(newCMS.Items, b.Items[key])
		}

		allPath[item.Path] = item.Key
	}

	return newCMS, nil
}

func mergeMode(a, b *int32) *int32 {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if *b > *a {
		return b
	}
	return a
}
