// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (r *SearchReconciler) IndexerConfigmap(instance *searchv1alpha1.Search) *corev1.ConfigMap {

	ns := instance.GetNamespace()
	deploymentLabels := generateLabels("config", "acm-proxyserver")
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "search-indexer",
			Namespace: ns,
			Labels:    deploymentLabels,
		},
	}
	data := map[string]string{}
	data["service"] = ns + "/search-indexer"
	data["port"] = "3010"
	data["path"] = "/aggregator/clusters/"
	data["sub-resource"] = "/sync"
	data["use-id"] = "true"
	data["secret"] = ns + "/search-indexer-certs"
	data["caConfigMap"] = "search-ca-crt"
	cm.Data = data

	err := controllerutil.SetControllerReference(instance, cm, r.Scheme)
	if err != nil {
		log.V(2).Info("Could not set control for search-indexer configmap")
	}
	return cm
}
