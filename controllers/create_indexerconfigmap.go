// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"

	cachev1 "github.com/stolostron/search-v2-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func (r *OCMSearchReconciler) createIndexerConfigmap(request reconcile.Request,
	cm *corev1.ConfigMap,
	instance *cachev1.OCMSearch,
) (*reconcile.Result, error) {

	found := &corev1.ConfigMap{}
	err := r.Get(context.TODO(), types.NamespacedName{
		Name:      cm.Name,
		Namespace: instance.Namespace,
	}, found)
	if err != nil && errors.IsNotFound(err) {

		err = r.Create(context.TODO(), cm)
		if err != nil {
			return &reconcile.Result{}, err
		} else {
			return nil, nil
		}
	} else if err != nil {
		return &reconcile.Result{}, err
	}

	return nil, nil
}

func (r *OCMSearchReconciler) IndexerConfigmap(instance *cachev1.OCMSearch) *corev1.ConfigMap {

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
	data["service"] = ns + "/search-aggregator"
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