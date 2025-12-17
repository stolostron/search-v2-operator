// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"context"

	"slices"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const CONDITION_FINE_GRAINED_RBAC = "FineGrainedRBACReady"

func (r *SearchReconciler) reconcileFineGrainedRBACConfiguration(ctx context.Context,
	instance *searchv1alpha1.Search) (*reconcile.Result, error) {

	if instance.ObjectMeta.Annotations["fine-grained-rbac-preview"] == "true" { //nolint:staticcheck // "could remove embedded field 'ObjectMeta' from selector
		log.Info("The annotation fine-grained-rbac-preview=true is present. Updating configuration.")

		err := r.updateSearchApiDeployment(ctx, instance,
			corev1.EnvVar{Name: "FEATURE_FINE_GRAINED_RBAC", Value: "true"})
		if err != nil {
			log.Error(err, "Failed to configure fine-grained RBAC.")
			r.updateStatusCondition(ctx, instance, metav1.Condition{
				Type:               CONDITION_FINE_GRAINED_RBAC,
				Status:             metav1.ConditionFalse,
				Reason:             "ConfigurationError",
				Message:            "Failed to enable fine-grained RBAC.",
				LastTransitionTime: metav1.Now(),
			})
			return &reconcile.Result{}, err
		}

		r.updateStatusCondition(ctx, instance, metav1.Condition{
			Type:               CONDITION_FINE_GRAINED_RBAC,
			Status:             metav1.ConditionTrue,
			Reason:             "None",
			Message:            "Fine-grained RBAC enabled.",
			LastTransitionTime: metav1.Now(),
		})

	} else {
		log.V(3).Info("The fine-grained-rbac-preview annotation is false or not present.")

		err := r.updateSearchApiDeployment(ctx, instance, corev1.EnvVar{Name: "FEATURE_FINE_GRAINED_RBAC", Value: ""})
		if err != nil {
			log.Error(err, "Failed to remove env FEATURE_FINE_GRAINED_RBAC from the search api deployment.")
			return &reconcile.Result{}, err
		}

		err = r.removeStatusCondition(ctx, instance, CONDITION_FINE_GRAINED_RBAC)
		if err != nil {
			log.Error(err, "Failed to update status condition.", "type", CONDITION_FINE_GRAINED_RBAC)
			return &reconcile.Result{}, err
		}
	}
	return &reconcile.Result{}, nil
}

func (r *SearchReconciler) removeStatusCondition(ctx context.Context,
	instance *searchv1alpha1.Search, conditionType string) error {

	for i := range instance.Status.Conditions {
		if instance.Status.Conditions[i].Type == conditionType {
			// Remove the status condition.
			instance.Status.Conditions = slices.Delete(instance.Status.Conditions, i, i+1)

			err := r.commitSearchCRInstanceState(ctx, instance)
			return err
		}
	}

	return nil
}
