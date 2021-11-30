// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package dependencies

import (
	"context"
	"fmt"
	"sync"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewStore returns a new Store instance
func NewStore() *Store {
	return &Store{
		deps: make(map[string]map[string]client.Object),
	}
}

// AddOrUpdate used to add or update an object in the Store
// kind correspond to the object kind, and id can be `namespace/name` identifier of just
// `name` if we are talking about a cluster scope object like `ClusterRole`.
func (ds *Store) AddOrUpdate(kind string, namespace string, name string, obj client.Object) {
	ds.mutex.Lock()
	defer ds.mutex.Unlock()

	if _, found := ds.deps[kind]; !found {
		ds.deps[kind] = map[string]client.Object{}
	}
	id := buildID(namespace, name)
	ds.deps[kind][id] = obj
}

// Get returns the client.Object instance if it was previously added in the Store.
// kind correspond to the object kind, and id can be `namespace/name` identifier of just
// `name` if we are talking about a cluster scope object like `ClusterRole`.
// It also return a boolean to know if the Object was found.
func (ds *Store) Get(kind string, namespace string, name string) (client.Object, bool) {
	ds.mutex.RLock()
	defer ds.mutex.RUnlock()

	if _, found := ds.deps[kind]; !found {
		return nil, false
	}
	id := buildID(namespace, name)
	if obj, found := ds.deps[kind][id]; found {
		return obj, true
	}
	return nil, false
}

// Apply use to create/update resources in the api-server
func (ds *Store) Apply(ctx context.Context, k8sClient client.Client) []error {
	ds.mutex.RLock()
	defer ds.mutex.RUnlock()

	// TODO(clamoriniere): implement this function that will add/update resource in the api-server
	// We can use also this function to cleanup un-necessary resources. for that we need to get all
	// existing resource with a label selector and make a diff.
	return nil
}

// Cleanup use to cleanup resources that are not needed anymore
func (ds *Store) Cleanup(ctx context.Context, k8sClient client.Client, ddaNs, ddaName string) []error {
	ds.mutex.RLock()
	defer ds.mutex.RUnlock()

	// TODO(clamoriniere): implement this function that will add/update resource in the api-server
	// We can use also this function to cleanup un-necessary resources. for that we need to get all
	// existing resource with a label selector and make a diff.
	return nil
}

// Store Kubernetes resource dependencies store
// this store helps to keep track of every resources that the different agent deployments depend on.
type Store struct {
	deps  map[string]map[string]client.Object
	mutex sync.RWMutex
}

func buildID(ns, name string) string {
	if ns == "" {
		return name
	}
	return fmt.Sprintf("%s/%s", ns, name)
}
