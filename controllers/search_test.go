// Copyright (c) 2021 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"context"
	"testing"
	"time"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestSearch_controller(t *testing.T) {
	var (
		name = "search-v2-operator"
	)
	search := &searchv1alpha1.Search{
		TypeMeta:   metav1.TypeMeta{Kind: "Search"},
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: searchv1alpha1.SearchSpec{
			DBStorage: searchv1alpha1.StorageSpec{
				StorageClassName: "test",
			},
		},
	}
	s := scheme.Scheme
	err := searchv1alpha1.SchemeBuilder.AddToScheme(s)
	if err != nil {
		t.Errorf("error adding search scheme: (%v)", err)
	}

	err = addonv1alpha1.AddToScheme(s)
	if err != nil {
		t.Errorf("error adding addon scheme: (%v)", err)
	}

	objs := []runtime.Object{search}
	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()

	r := &SearchReconciler{Client: cl, Scheme: s}

	// Mock request to simulate Reconcile() being called on an event for a watched resource .
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name: name,
		},
	}

	// trigger reconcile
	_, err = r.Reconcile(context.TODO(), req)
	if err != nil {
		t.Errorf("reconcile: (%v)", err)
	}

	//wait for update status
	time.Sleep(1 * time.Second)

	//check for deployment
	deploy := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name: "search-postgres",
	}, deploy)

	if err != nil {
		t.Fatalf("Failed to get deployment %s: %v", "search-postgres", err)
	}

	//check for service
	service := &corev1.Secret{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name: "search-postgres",
	}, service)

	if err != nil {
		t.Errorf("Failed to get service %s: %v", "search-postgres", err)
	}

	//check for secret
	secret := &corev1.Secret{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name: "search-postgres",
	}, secret)

	if err != nil {
		t.Fatalf("Failed to get secret %s: %v", "search-postgres", err)
	}

	//check for configmap
	configmap1 := &corev1.ConfigMap{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name: "search-ca-crt",
	}, configmap1)

	if err != nil {
		t.Errorf("Failed to get configmap %s: %v", "search-ca-crt", err)
	}

	//check for configmap
	configmap2 := &corev1.ConfigMap{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name: "search-indexer",
	}, configmap2)

	if err != nil {
		t.Fatalf("Failed to get configmap %s: %v", "search-indexer", err)
	}

	//check for Service Account
	serviceaccount := &corev1.ServiceAccount{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name: getServiceAccountName(),
	}, serviceaccount)
	if err != nil {
		t.Errorf("Failed to get serviceaccount %s: %v", getServiceAccountName(), err)
	}

	//check for Role
	role := &rbacv1.ClusterRole{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name: getRoleName(),
	}, role)
	if err != nil {
		t.Errorf("Failed to get role %s: %v", getRoleName(), err)
	}

	//check for RoleBinding
	rolebinding := &rbacv1.ClusterRoleBinding{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name: getRoleBindingName(),
	}, rolebinding)

	if err != nil {
		t.Errorf("Failed to get serviceaccount %s: %v", getRoleBindingName(), err)
	}

	//check for ClusterManagementAddon
	cma := &addonv1alpha1.ClusterManagementAddOn{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name: getClusterManagementAddonName(),
	}, cma)

	if err != nil {
		t.Errorf("Failed to get ClusterManagementAddOn %s: %v", getClusterManagementAddonName(), err)
	}

	//check for PVC
	pvc := &corev1.PersistentVolumeClaim{}
	storageClassName := search.Spec.DBStorage.StorageClassName
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name: getPVCName(storageClassName),
	}, pvc)

	if err != nil {
		t.Errorf("Failed to get PersistentVolumeClaim %s: %v", getPVCName(storageClassName), err)
	}

	//Test EmptyDir
	search = &searchv1alpha1.Search{
		TypeMeta:   metav1.TypeMeta{Kind: "Search"},
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       searchv1alpha1.SearchSpec{},
	}

	objsEmpty := []runtime.Object{search}
	// Create a fake client to mock API calls.
	cl = fake.NewClientBuilder().WithRuntimeObjects(objsEmpty...).Build()

	r = &SearchReconciler{Client: cl, Scheme: s}

	//check for PVC
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name: getPVCName(storageClassName),
	}, pvc)

	if !errors.IsNotFound(err) {
		t.Errorf("Emptydir expected but PVC found %v", err)
	}

}
