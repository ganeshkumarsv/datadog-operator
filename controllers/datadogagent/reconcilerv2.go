// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package datadogagent

import (
	"context"
	"fmt"

	datadoghqv1alpha1 "github.com/DataDog/datadog-operator/apis/datadoghq/v1alpha1"
	"github.com/DataDog/datadog-operator/apis/datadoghq/v1alpha1/patch"
	"github.com/DataDog/datadog-operator/apis/datadoghq/v2alpha1"
	"github.com/DataDog/datadog-operator/controllers/datadogagent/dependencies"
	"github.com/DataDog/datadog-operator/controllers/datadogagent/feature"
	"github.com/DataDog/datadog-operator/pkg/controller/utils"
	"github.com/go-logr/logr"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ReconcileV2 is similar to reconciler.Reconcile interface, but taking a context
func (r *Reconciler) ReconcileV2(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	var result reconcile.Result
	var err error

	reqLogger := r.log.WithValues("datadogagent", request.NamespacedName)
	reqLogger.Info("Reconciling DatadogAgent")

	// Fetch the DatadogAgent instance
	instance := &datadoghqv1alpha1.DatadogAgent{}
	err = r.client.Get(ctx, request.NamespacedName, instance)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return result, nil
		}
		// Error reading the object - requeue the request.
		return result, err
	}

	if result, err = r.handleFinalizer(reqLogger, instance); utils.ShouldReturn(result, err) {
		return result, err
	}

	var patched bool
	if instance, patched = patch.CopyAndPatchDatadogAgent(instance); patched {
		reqLogger.Info("Patching DatadogAgent")
		err = r.client.Update(ctx, instance)
		if err != nil {
			reqLogger.Error(err, "failed to update DatadogAgent")
			return reconcile.Result{}, err
		}
	}
	if err = datadoghqv1alpha1.IsValidDatadogAgent(&instance.Spec); err != nil {
		reqLogger.V(1).Info("Invalid spec", "error", err)
		return r.updateStatusIfNeeded(reqLogger, instance, &instance.Status, result, err)
	}

	instOverrideStatus := datadoghqv1alpha1.DefaultDatadogAgent(instance)
	instance, result, err = r.updateOverrideIfNeeded(reqLogger, instance, instOverrideStatus, result)
	if err != nil {
		return result, err
	}

	return r.reconcileV2Instance(ctx, reqLogger, hackFromV1toV2(instance))
}

func hackFromV1toV2(dda *datadoghqv1alpha1.DatadogAgent) *v2alpha1.DatadogAgent {
	// TODO(clamoriniere): hack to only illustrate the rest of the function
	return nil
}

func (r *Reconciler) reconcileV2Instance(ctx context.Context, logger logr.Logger, dda *v2alpha1.DatadogAgent) (reconcile.Result, error) {
	var result reconcile.Result

	features, err := feature.BuildFeatures(dda, reconcilerOptionsToFeatureOptions(&r.options, logger))
	if err != nil {
		return result, fmt.Errorf("unable to build features, err: %w", err)
	}

	// -----------------------
	// Manage dependencies
	// -----------------------
	depsStore := dependencies.NewStore()
	var errs []error
	for _, feat := range features {
		if featErr := feat.ManageDependencies(depsStore); err != nil {
			errs = append(errs, featErr)
		}
	}
	// Now create/update dependencies
	errs = append(errs, depsStore.Apply(ctx, r.client)...)
	if len(errs) > 0 {
		logger.V(2).Info("Dependencies apply error", "errs", errs)
		return result, errors.NewAggregate(errs)
	}
	// -----------------------

	// reconcile the deployments (Deployments, Daemonset)
	newStatus := dda.Status.DeepCopy()
	reconcileFuncs :=
		[]reconcileV2FuncInterface{
			r.reconcileV2ClusterAgent,
			// TODO(clamoriniere): implement r.reconcileV2ClusterChecksRunner,
			// TODO(clamoriniere): implement r.reconcileV2Agent,
		}
	for _, reconcileFunc := range reconcileFuncs {
		result, err = reconcileFunc(logger, features, dda, newStatus)
		if utils.ShouldReturn(result, err) {
			return r.updateStatusV2IfNeeded(logger, dda, newStatus, result, err)
		}
	}

	// Cleanup unused dependencies
	// Run it after the deployments reconcile
	if errs = depsStore.Cleanup(ctx, r.client, dda.Namespace, dda.Name); len(errs) > 0 {
		return result, errors.NewAggregate(errs)
	}

	// Always requeue
	if !result.Requeue && result.RequeueAfter == 0 {
		result.RequeueAfter = defaultRequeuePeriod
	}
	return r.updateStatusV2IfNeeded(logger, dda, newStatus, result, err)
}

type reconcileV2FuncInterface func(logger logr.Logger, features []feature.Feature, dda *v2alpha1.DatadogAgent, newStatus *v2alpha1.DatadogAgentStatus) (reconcile.Result, error)

func reconcilerOptionsToFeatureOptions(opts *ReconcilerOptions, logger logr.Logger) *feature.Options {
	return &feature.Options{
		SupportExtendedDaemonset: opts.SupportExtendedDaemonset,
		Logger:                   logger,
	}
}

func (r *Reconciler) reconcileV2ClusterAgent(logger logr.Logger, features []feature.Feature, dda *v2alpha1.DatadogAgent, newStatus *v2alpha1.DatadogAgentStatus) (reconcile.Result, error) {
	// ClusterAgentDeployment new Deployment instance
	clusterAgentDeployment := &appsv1.Deployment{}

	// TODO: Add generic information to the Deployment (name, namespace, labels ....)

	// Apply features change on the Deployment.spec.template
	var errs []error
	for _, feat := range features {
		if err := feat.ManageClusterAgent(&clusterAgentDeployment.Spec.Template); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		logger.V(2).Info("ManagerClusterAgent error", "errs", errs)
	}

	return reconcile.Result{}, nil
}

func (r *Reconciler) updateStatusV2IfNeeded(logger logr.Logger, agentdeployment *v2alpha1.DatadogAgent, newStatus *v2alpha1.DatadogAgentStatus, result reconcile.Result, currentError error) (reconcile.Result, error) {
	// TODO(clamoriniere): implement this function
	// it can be good to redesign the conditions in the status and use
	return result, nil
}
