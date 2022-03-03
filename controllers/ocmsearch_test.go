// Copyright (c) 2021 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"context"
	"testing"
	"time"

	cachev1 "github.com/stolostron/search-v2-operator/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestSearch_controller(t *testing.T) {
	var (
		name = "search-v2-operator"
	)
	ocmsearch := &cachev1.OCMSearch{
		TypeMeta:   metav1.TypeMeta{Kind: "OCMSearch"},
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       cachev1.OCMSearchSpec{},
	}
	s := scheme.Scheme
	err := cachev1.SchemeBuilder.AddToScheme(s)
	if err != nil {
		t.Errorf("error adding scheme: (%v)", err)
	}

	objs := []runtime.Object{ocmsearch}
	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()

	r := &OCMSearchReconciler{Client: cl, Scheme: s}

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
	role := &rbacv1.Role{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name: getRoleName(),
	}, role)
	if err != nil {
		t.Errorf("Failed to get role %s: %v", getRoleName(), err)
	}

	//check for RoleBinding
	rolebinding := &rbacv1.RoleBinding{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name: getRoleBindingName(),
	}, rolebinding)

	if err != nil {
		t.Errorf("Failed to get serviceaccount %s: %v", getRoleBindingName(), err)
	}

}
