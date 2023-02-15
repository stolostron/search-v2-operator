// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (r *SearchReconciler) IndexerService(instance *searchv1alpha1.Search) *corev1.Service {

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "search-indexer",
			Namespace:   instance.GetNamespace(),
			Annotations: map[string]string{"service.beta.openshift.io/serving-cert-secret-name": "search-indexer-certs"},
			Labels:      map[string]string{"search-monitor": "search-indexer"},
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
