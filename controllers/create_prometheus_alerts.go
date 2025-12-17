// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/equality"

	monitorv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	SearchPVCAlertRuleName = "search-pvc-info-alert"

	maxAppsCount            = 100
	maxManagedClustersCount = 10
	maxIndexerCountOver30m  = 100
)

// SearchPVCPrometheusRule creates a PrometheusRule for PVC info alert
func (r *SearchReconciler) SearchPVCPrometheusRule(instance *searchv1alpha1.Search) *monitorv1.PrometheusRule {
	pvcAbsentExpr := fmt.Sprintf(`absent(kube_persistentvolumeclaim_info{namespace="%s", persistentvolumeclaim=~".*-search"}) == 1`, instance.GetNamespace())

	manyManagedClustersExpr := fmt.Sprintf(`acm_managed_cluster_count > %d`, maxManagedClustersCount)

	manyAppsExpr := fmt.Sprintf(`( max(apiserver_storage_objects{resource="subscriptions.apps.open-cluster-management.io"}) +
		max(apiserver_storage_objects{resource="applicationsets.argoproj.io"}) ) > %d`, maxAppsCount)

	searchPostgresOOMExpr := fmt.Sprintf(`
		kube_pod_container_status_terminated_reason{
			namespace="%s",
			pod=~"search-postgres.*",
			reason="OOMKilled"
		} == 1
	`, instance.GetNamespace())

	searchIndexerOOMExpr := fmt.Sprintf(`
		kube_pod_container_status_terminated_reason{
			namespace="%s",
			pod=~"search-indexer.*",
			reason="OOMKilled"
		} == 1
	`, instance.GetNamespace())

	searchIndexerRequestSizeExpr := fmt.Sprintf(`increase(search_indexer_request_size_count[30m]) > %d`, maxIndexerCountOver30m)

	searchPVCCriticalExpr := fmt.Sprintf(`
		( %s )
		and on()
		(
			(%s) or (%s) or (%s) or (%s) or (%s)
		)
	`, pvcAbsentExpr, manyManagedClustersExpr, manyAppsExpr, searchPostgresOOMExpr, searchIndexerOOMExpr, searchIndexerRequestSizeExpr)

	rule := &monitorv1.PrometheusRule{
		TypeMeta: metav1.TypeMeta{
			Kind:       "PrometheusRule",
			APIVersion: monitorv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      SearchPVCAlertRuleName,
			Namespace: instance.GetNamespace(),
		},
		Spec: monitorv1.PrometheusRuleSpec{
			Groups: []monitorv1.RuleGroup{
				{
					Name: "search-pvc-alerts",
					Rules: []monitorv1.Rule{
						{
							Alert: "SearchPVCNotPresent",
							Expr:  intstr.FromString(pvcAbsentExpr),
							For:   monitorv1.Duration("5m"),
							Labels: map[string]string{
								"severity":  "info",
								"component": "search",
							},
							Annotations: map[string]string{
								"summary":     "Search Persistent Volume Claim is not present",
								"description": "Search PVC is not present in namespace " + instance.GetNamespace() + ". You should configure persistent storage for RHACM Search in production environments. See docs.redhat.com for more information about RHACM Search with persistent storage.",
								"message":     "Search is currently running without persistent storage. Consider configuring a PVC by setting spec.dbStorage.storageClassName in the RHACM Search CR for better performance.",
							},
						},
						{
							Alert: "SearchPVCNotPresentCritical",
							Expr:  intstr.FromString(searchPVCCriticalExpr),
							For:   monitorv1.Duration("5m"),
							Labels: map[string]string{
								"severity":  "critical",
								"component": "search",
							},
							Annotations: map[string]string{
								"summary":     "Search Persistent Volume Claim is not present and critical conditions are met",
								"description": "Search PVC is not present in namespace " + instance.GetNamespace() + ". You should configure persistent storage for RHACM Search in production environments. See docs.redhat.com for more information about RHACM Search with persistent storage.",
								"message":     "Search is currently running without persistent storage. System usage is high enough that persistent storage is needed to avoid performance issues. Consider configuring a PVC by setting spec.dbStorage.storageClassName in the RHACM Search CR for better performance.",
							},
						},
					},
				},
			},
		},
	}
	err := controllerutil.SetControllerReference(instance, rule, r.Scheme)
	if err != nil {
		log.Info("Could not set controller reference for PrometheusRule", "name", SearchPVCAlertRuleName)
	}
	return rule
}

// createOrUpdatePrometheusRule creates or updates a PrometheusRule
func (r *SearchReconciler) createOrUpdatePrometheusRule(ctx context.Context,
	rule *monitorv1.PrometheusRule,
) (*reconcile.Result, error) {
	found := &monitorv1.PrometheusRule{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      rule.Name,
		Namespace: rule.Namespace,
	}, found)
	if err != nil && errors.IsNotFound(err) {
		err := r.Create(ctx, rule)
		if err != nil {
			log.Error(err, "Could not create PrometheusRule "+rule.Name)
			return &reconcile.Result{}, err
		}
		log.Info("Created PrometheusRule " + rule.Namespace + "/" + rule.Name)
		return nil, nil
	}
	if err != nil {
		log.Error(err, "Could not get PrometheusRule")
		return &reconcile.Result{}, err
	}

	if !equality.Semantic.DeepEqual(found.Spec, rule.Spec) {
		found.Spec = rule.Spec
		err = r.Update(ctx, found)
		if err != nil {
			log.Error(err, "Could not update PrometheusRule")
			return &reconcile.Result{}, err
		}
		log.V(2).Info("Updated PrometheusRule " + rule.Namespace + "/" + rule.Name)
	}
	return nil, nil
}
