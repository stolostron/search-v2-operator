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

func (r *SearchReconciler) createPGService(request reconcile.Request,
	service *corev1.Service,
	instance *searchv1alpha1.Search,
) (*reconcile.Result, error) {
	return r.createOrUpdateService(context.TODO(), service)
	/*found := &corev1.Service{}
	err := r.Get(context.TODO(), types.NamespacedName{
		Name:      service.Name,
		Namespace: instance.Namespace,
	}, found)
	if err != nil && errors.IsNotFound(err) {

		err = r.Create(context.TODO(), service)
		if err != nil {
			log.Error(err, "Could not create %s service", service.Name)
			return &reconcile.Result{}, err
		} else {
			return nil, nil
		}
	} else if err != nil {
		return &reconcile.Result{}, err
	}

	return nil, nil
	*/
}

func (r *SearchReconciler) PGService(instance *searchv1alpha1.Search) *corev1.Service {

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "search-postgres",
			Namespace: instance.GetNamespace(),
		},
	}
	svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{})

	svc.Spec.Ports[0].Name = "search-postgres"
	svc.Spec.Ports[0].Port = 5432
	svc.Spec.Ports[0].TargetPort = intstr.IntOrString{IntVal: 5432}
	svc.Spec.Ports[0].Protocol = corev1.ProtocolTCP
	svc.Spec.Selector = map[string]string{"name": "search-postgres"}

	err := controllerutil.SetControllerReference(instance, svc, r.Scheme)
	if err != nil {
		log.V(2).Info("Could not set control for search-postgres service")
	}
	return svc
}
