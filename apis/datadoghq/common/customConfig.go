// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package common

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1 "k8s.io/api/core/v1"
)

// CustomConfig Allow to put custom configuration for the agent
type CustomConfig struct {
	// ConfigData corresponds to the configuration file content.
	ConfigData *string
	// Enable to specify a reference to an already existing ConfigMap.
	ConfigMap *ConfigFileConfigMap
}

// ConfigFileConfigMap contains configMap information used to store a config file.
type ConfigFileConfigMap struct {
	// The name of source ConfigMap.
	Name string
	// Items maps a ConfigMap data key to a file path mount.
	Items []corev1.KeyToPath
}

// GetConfName get the name of the Configmap for a CustomConfigSpec
func GetConfName(owner metav1.Object, conf *CustomConfig, defaultName string) string {
	// `configData` and `configMap` can't be set together.
	// Return the default if the conf is not overridden or if it is just overridden with the ConfigData.
	if conf != nil && conf.ConfigMap != nil {
		return conf.ConfigMap.Name
	}
	return fmt.Sprintf("%s-%s", owner.GetName(), defaultName)
}
