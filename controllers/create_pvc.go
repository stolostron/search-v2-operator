// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *SearchReconciler) configurePVC(ctx context.Context, instance *searchv1alpha1.Search) bool {
	if err := r.createPVC(ctx, instance); err != nil {
		return false
	}
	return true
}

func (r *SearchReconciler) createPVC(ctx context.Context, instance *searchv1alpha1.Search) error {
	pvc := &corev1.PersistentVolumeClaim{}
	pvcName := getPVCName(instance.Spec.DBStorage.StorageClassName)
	namespace := instance.GetNamespace()
	storageClass := instance.Spec.DBStorage.StorageClassName
	storageSize := resource.MustParse("10Gi")
	if instance.Spec.DBStorage.Size != nil {
		storageSize = *instance.Spec.DBStorage.Size
	}
	resource := client.ObjectKey{Name: pvcName, Namespace: namespace}
	err := r.Get(ctx, resource, pvc)
	if err != nil && errors.IsNotFound(err) {
		err = r.Create(ctx, NewPVC(pvcName, namespace, storageClass, storageSize))
		if err != nil {
			log.Error(err, "Failed to create persistentvolumeclaim")
			return err
		}
	}
	if err != nil {
		log.Error(err, "Failed to get persistentvolumeclaim")
		return err
	}
	return nil
}

func (r *SearchReconciler) isPVCPresent(ctx context.Context, instance *searchv1alpha1.Search) bool {
	pvcs := &corev1.PersistentVolumeClaimList{}
	opts := []client.ListOption{
		client.InNamespace(instance.GetNamespace()),
	}
	if err := r.List(ctx, pvcs, opts...); err != nil {
		log.Info("Failed to list PersistentVolumeClaim in namespace" + instance.GetNamespace())
		return false
	}
	for _, pvc := range pvcs.Items {
		if pvc.GetName() == getPVCName(instance.Spec.DBStorage.StorageClassName) {
			log.V(2).Info("Found PersistentVolumeClaim in namespace")
			return true
		}
	}
	return false
}

func NewPVC(pvcName, namespace, storageClass string, storageSize resource.Quantity) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		TypeMeta: metav1.TypeMeta{
			Kind:       "PersistentVolumeClaim",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceName(corev1.ResourceStorage): storageSize,
				},
			},
			StorageClassName: &storageClass,
		},
	}
}
