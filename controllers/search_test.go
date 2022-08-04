// Copyright (c) 2021 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"context"
	"strings"
	"testing"
	"time"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// Verifies that the expected volume is contained within the list of given volumes from a deployment.
func verifyContainsVolumes(t *testing.T, volumes []corev1.Volume, expectedVolume string) {
	for _, v := range volumes {
		if v.Name == expectedVolume {
			return
		}
	}
	t.Errorf("Failed to find the expected volume: %s", expectedVolume)
}

// Verifies that the expected data content is contained within the configmap.
func verifyConfigmapDataContent(t *testing.T, cm *corev1.ConfigMap, expectedKey string, expectedValue string) {
	if val, ok := cm.Data[expectedKey]; ok {
		if strings.Contains(val, expectedValue) {
			return
		}
		t.Errorf("Failed to find the expected value: %s within the data key: %s in configmap: %s", expectedValue, expectedKey, cm.Name)
	}
	t.Errorf("Failed to find the expected data key: %s within the configmap: %s", expectedKey, cm.Name)
}

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

	//check for postgres deployment volumes
	volumes := deploy.Spec.Template.Spec.Volumes
	verifyContainsVolumes(t, volumes, "search-postgres-certs")
	verifyContainsVolumes(t, volumes, "postgresql-cfg")

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

	//check for configmap
	configmap3 := &corev1.ConfigMap{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name: "search-postgres",
	}, configmap3)

	if err != nil {
		t.Fatalf("Failed to get configmap %s: %v", "search-postgres", err)
	}

	verifyConfigmapDataContent(t, configmap3, "postgresql.conf", "ssl = 'on'")
	verifyConfigmapDataContent(t, configmap3, "postgresql-start.sh", "CREATE SCHEMA IF NOT EXISTS search")

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

	//Test finalizer
	search.ObjectMeta.DeletionTimestamp = &v1.Time{Time: time.Now()}
	search.ObjectMeta.Finalizers = []string{"search.open-cluster-management.io/finalizer"}
	err = cl.Update(context.TODO(), search)
	if err != nil {
		t.Fatalf("Failed to update Search CR: (%v)", err)
	}
	_, err = r.Reconcile(context.TODO(), req)
	if err != nil {
		t.Fatalf("reconcile for finalizer: (%v)", err)
	}

}
