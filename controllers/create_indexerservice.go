// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"

	cachev1 "github.com/stolostron/search-v2-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func (r *OCMSearchReconciler) createIndexerService(request reconcile.Request,
	service *corev1.Service,
	instance *cachev1.OCMSearch,
) (*reconcile.Result, error) {

	found := &corev1.Service{}
	log.Info("looking for indexer svc")
	err := r.Get(context.TODO(), types.NamespacedName{
		Name:      "search-indexer",
		Namespace: instance.Namespace,
	}, found)
	if err != nil && errors.IsNotFound(err) {

		err = r.Create(context.TODO(), service)
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

func (r *OCMSearchReconciler) IndexerService(instance *cachev1.OCMSearch) *corev1.Service {

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "search-indexer",
			Namespace:   instance.GetNamespace(),
			Annotations: map[string]string{"service.beta.openshift.io/serving-cert-secret-name": "search-indexer-certs"},
		},
	}
	svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{})

	svc.Spec.Ports[0].Name = "search-indexer"
	svc.Spec.Ports[0].Port = 3010
	svc.Spec.Ports[0].TargetPort = intstr.IntOrString{IntVal: 3010}
	svc.Spec.Ports[0].Protocol = corev1.ProtocolTCP
	svc.Spec.Selector = map[string]string{"name": "search-indexer"}

	err := controllerutil.SetControllerReference(instance, svc, r.Scheme)
	if err != nil {
		log.V(2).Info("Could not set control for search-indexer service")
	}
	return svc
}
