// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const SearchCollector = "search-collector"

func (r *SearchReconciler) CollectorService(instance *searchv1alpha1.Search) *corev1.Service {

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "search-collector",
			Namespace: instance.GetNamespace(),
			Labels:    map[string]string{"search-monitor": SearchCollector},
		},
	}
	svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{})

	svc.Spec.Ports[0].Name = SearchCollector
	svc.Spec.Ports[0].Port = 5010
	svc.Spec.Ports[0].TargetPort = intstr.IntOrString{IntVal: 5010}
	svc.Spec.Ports[0].Protocol = corev1.ProtocolTCP
	svc.Spec.Selector = map[string]string{"name": SearchCollector}

	err := controllerutil.SetControllerReference(instance, svc, r.Scheme)
	if err != nil {
		log.V(2).Info("Could not set control for search-collector service")
	}
	return svc
}
