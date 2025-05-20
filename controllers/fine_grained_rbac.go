// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"context"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func (r *SearchReconciler) reconcileFineGrainedRBACConfiguration(ctx context.Context,
	instance *searchv1alpha1.Search) (*reconcile.Result, error) {

	if instance.ObjectMeta.Annotations["fine-grained-rbac-preview"] == "true" {
		log.Info("The annotation fine-grained-rbac-preview=true is present. Updating configuration.")

		err := r.updateSearchApiDeployment(ctx, instance,
			corev1.EnvVar{Name: "FEATURE_FINE_GRAINED_RBAC", Value: "true"})
		if err != nil {
			log.Error(err, "Failed to set env FEATURE_FINE_GRAINED_RBAC on search-api deployment.")
			return &reconcile.Result{}, err
		}
	} else {
		log.V(3).Info("The fine-grained-rbac-preview annotation is false or not present.")

		err := r.updateSearchApiDeployment(ctx, instance, corev1.EnvVar{Name: "FEATURE_FINE_GRAINED_RBAC", Value: ""})
		if err != nil {
			log.Error(err, "Failed to remove env FEATURE_FINE_GRAINED_RBAC from the search api deployment.")
			return &reconcile.Result{}, err
		}
	}
	return &reconcile.Result{}, nil
}
