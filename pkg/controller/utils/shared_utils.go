// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package utils

import (
	"github.com/DataDog/datadog-operator/apis/datadoghq/common"
	datadoghqv1alpha1 "github.com/DataDog/datadog-operator/apis/datadoghq/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// GetDefaultCredentialsSecretName returns the default name for credentials secret
func GetDefaultCredentialsSecretName(dda *datadoghqv1alpha1.DatadogAgent) string {
	return dda.Name
}

// GetAPIKeySecret returns the API key secret name and the key inside the secret
// returns <is_set>, secretName, secretKey
func GetAPIKeySecret(credentials *datadoghqv1alpha1.DatadogCredentials, defaultName string) (bool, string, string) {
	if credentials.APISecret != nil {
		if credentials.APISecret.KeyName != "" {
			return true, credentials.APISecret.SecretName, credentials.APISecret.KeyName
		}

		return true, credentials.APISecret.SecretName, common.DefaultAPIKeyKey
	}

	if credentials.APIKeyExistingSecret != "" {
		return true, credentials.APIKeyExistingSecret, common.DefaultAPIKeyKey
	}

	if credentials.APIKey != "" {
		return true, defaultName, common.DefaultAPIKeyKey
	}

	return false, defaultName, common.DefaultAPIKeyKey
}

// GetAppKeySecret returns the APP key secret name and the key inside the secret
// returns <is_set>, secretName, secretKey
func GetAppKeySecret(credentials *datadoghqv1alpha1.DatadogCredentials, defaultName string) (bool, string, string) {
	if credentials.APPSecret != nil {
		if credentials.APPSecret.KeyName != "" {
			return true, credentials.APPSecret.SecretName, credentials.APPSecret.KeyName
		}

		return true, credentials.APPSecret.SecretName, common.DefaultAPPKeyKey
	}

	if credentials.AppKeyExistingSecret != "" {
		return true, credentials.AppKeyExistingSecret, common.DefaultAPPKeyKey
	}

	if credentials.AppKey != "" {
		return true, defaultName, common.DefaultAPPKeyKey
	}

	return false, defaultName, common.DefaultAPPKeyKey
}

// ShouldReturn returns if we should stop the reconcile loop based on result
func ShouldReturn(result reconcile.Result, err error) bool {
	if err != nil || result.Requeue || result.RequeueAfter > 0 {
		return true
	}
	return false
}
