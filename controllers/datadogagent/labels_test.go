// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package datadogagent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/DataDog/datadog-operator/apis/datadoghq/v1alpha1"
)

func Test_NamespacedName(t *testing.T) {
	tests := []struct {
		name                   string
		agentName              string
		agentNamespace         string
		expectedNamespacedName types.NamespacedName
	}{
		{
			name:                   "without the split char",
			agentNamespace:         "foo",
			agentName:              "bar",
			expectedNamespacedName: types.NamespacedName{Namespace: "foo", Name: "bar"},
		},
		{
			name:                   "with the split char",
			agentNamespace:         "f-o-o",
			agentName:              "bar",
			expectedNamespacedName: types.NamespacedName{Namespace: "f-o-o", Name: "bar"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dda := v1alpha1.DatadogAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tt.agentName,
					Namespace: tt.agentNamespace,
				},
			}

			value := NewPartOfLabelValue(&dda)

			assert.Equal(t, tt.expectedNamespacedName, value.NamespacedName())
		})
	}
}
