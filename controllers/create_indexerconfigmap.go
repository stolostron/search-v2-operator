// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func (r *SearchReconciler) createIndexerConfigmap(request reconcile.Request,
	cm *corev1.ConfigMap,
	instance *searchv1alpha1.Search,
) (*reconcile.Result, error) {
	return r.createOrUpdateConfigMap(context.TODO(), cm)

	/*	found := &corev1.ConfigMap{}
		err := r.Get(context.TODO(), types.NamespacedName{
			Name:      cm.Name,
			Namespace: instance.Namespace,
		}, found)
		if err != nil && errors.IsNotFound(err) {

			err = r.Create(context.TODO(), cm)
			if err != nil {
				log.Error(err, "Could not create %s configmap", cm.Name)
				return &reconcile.Result{}, err
			} else {
				return nil, nil
			}
		} else if err != nil {
			return &reconcile.Result{}, err
		}

		return nil, nil*/
}

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
