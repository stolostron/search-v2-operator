// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"

	monitorv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const SearchMetricsMonitor = "search-metrics-monitor"

func (r *SearchReconciler) MetricsRole(instance *searchv1alpha1.Search) *rbacv1.Role {
	cr := &rbacv1.Role{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Role",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      SearchMetricsMonitor,
			Namespace: instance.GetNamespace(),
		},
		Rules: getMetricsRules(),
	}
	err := controllerutil.SetControllerReference(instance, cr, r.Scheme)
	if err != nil {
		log.Info("Could not set control for Role ", "name", SearchMetricsMonitor)
	}
	return cr
}

func (r *SearchReconciler) MetricsRoleBinding(instance *searchv1alpha1.Search) *rbacv1.RoleBinding {
	crb := &rbacv1.RoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind:       "RoleBinding",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      SearchMetricsMonitor,
			Namespace: instance.GetNamespace(),
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "Role",
			Name:     SearchMetricsMonitor,
			APIGroup: rbacv1.GroupName,
		},
		Subjects: []rbacv1.Subject{{
			Kind:      "ServiceAccount",
			Name:      "prometheus-k8s",
			Namespace: "openshift-monitoring",
		},
		},
	}
	err := controllerutil.SetControllerReference(instance, crb, r.Scheme)
	if err != nil {
		log.Info("Could not set control for RoleBinding", "name", SearchMetricsMonitor)
	}
	return crb
}

func (r *SearchReconciler) CollectorServiceMonitor(instance *searchv1alpha1.Search,
	deployment string, namespace string) *monitorv1.ServiceMonitor {
	cr := r.ServiceMonitor(instance, deployment, namespace)
	cr.Spec.Endpoints[0].Scheme = "http"
	cr.Spec.Endpoints[0].TLSConfig = nil
	return cr
}

func (r *SearchReconciler) ServiceMonitor(instance *searchv1alpha1.Search,
	deployment string, namespace string) *monitorv1.ServiceMonitor {
	smName := deployment + "-monitor"
	cr := &monitorv1.ServiceMonitor{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceMonitor",
			APIVersion: monitorv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      smName,
			Namespace: namespace,
		},
		Spec: monitorv1.ServiceMonitorSpec{
			JobLabel:          deployment,
			NamespaceSelector: monitorv1.NamespaceSelector{MatchNames: []string{instance.GetNamespace()}},
			Endpoints: []monitorv1.Endpoint{
				{
					Port:            deployment,
					Scheme:          "https",
					ScrapeTimeout:   "10s",
					Interval:        "60s",
					BearerTokenFile: "/var/run/secrets/kubernetes.io/serviceaccount/token",
					TLSConfig: &monitorv1.TLSConfig{
						SafeTLSConfig: monitorv1.SafeTLSConfig{InsecureSkipVerify: true}},
				},
			},
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{"search-monitor": deployment}},
		},
	}
	return cr
}

func (r *SearchReconciler) createServiceMonitor(ctx context.Context,
	smonitor *monitorv1.ServiceMonitor,
) (*reconcile.Result, error) {

	found := &monitorv1.ServiceMonitor{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      smonitor.Name,
		Namespace: smonitor.Namespace,
	}, found)
	if err != nil && errors.IsNotFound(err) {
		err := r.Create(ctx, smonitor)
		if err != nil {
			log.Error(err, "Could not create servicemonitor "+smonitor.Name)
			return &reconcile.Result{}, err
		}
		log.Info("Created servicemonitor " + smonitor.Namespace + "/" + smonitor.Name)
		log.V(9).Info("Created  servicemonitor ", "name", smonitor)
	}
	return nil, nil
}
