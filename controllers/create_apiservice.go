// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func (r *SearchReconciler) createAPIService(request reconcile.Request,
	service *corev1.Service,
	instance *searchv1alpha1.Search,
) (*reconcile.Result, error) {
	return r.createOrUpdateService(context.TODO(), service)
	/*
		found := &corev1.Service{}
		err := r.Get(context.TODO(), types.NamespacedName{
			Name:      "search-search-api",
			Namespace: instance.Namespace,
		}, found)
		if err != nil && errors.IsNotFound(err) {
			err = r.Create(context.TODO(), service)
			if err != nil {
				log.Error(err, "Could not create search-search-api service")
				return &reconcile.Result{}, err
			}
		}
		if err := r.Update(context.TODO(), service); err != nil {
			log.Error(err, "Could not update %s service", "search-search-api")
			return &reconcile.Result{}, err
		}
		log.V(2).Info("Created %s service", "search-search-api")
		return nil, nil
	*/
}

func (r *SearchReconciler) APIService(instance *searchv1alpha1.Search) *corev1.Service {

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "search-search-api",
			Namespace:   instance.GetNamespace(),
			Annotations: map[string]string{"service.beta.openshift.io/serving-cert-secret-name": "search-api-certs"},
		},
	}
	svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{})

	svc.Spec.Ports[0].Name = "search-api"
	svc.Spec.Ports[0].Port = 4010
	svc.Spec.Ports[0].TargetPort = intstr.IntOrString{IntVal: 4010}
	svc.Spec.Ports[0].Protocol = corev1.ProtocolTCP
	svc.Spec.Selector = map[string]string{"name": "search-api"}

	err := controllerutil.SetControllerReference(instance, svc, r.Scheme)
	if err != nil {
		log.V(2).Info("Could not set control for search-api service")
	}
	return svc
}
