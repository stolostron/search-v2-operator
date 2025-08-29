// Copyright (c) 2021 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"context"
	"strings"
	"testing"
	"time"

	monitorv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
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
		name      = "search-v2-operator"
		namespace = "test-ns"
	)
	search := &searchv1alpha1.Search{
		TypeMeta: metav1.TypeMeta{Kind: "Search"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
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

	err = monitorv1.AddToScheme(s)
	if err != nil {
		t.Errorf("error adding monitor scheme: (%v)", err)
	}
	objs := []runtime.Object{search}
	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithStatusSubresource(search).WithRuntimeObjects(objs...).Build()

	r := &SearchReconciler{Client: cl, DynamicClient: fakeDynClient(), Scheme: s}

	// Mock request to simulate Reconcile() being called on an event for a watched resource .
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: namespace,
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
		Name:      "search-postgres",
		Namespace: namespace,
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
		Name:      "search-postgres",
		Namespace: namespace,
	}, service)

	if err != nil {
		t.Errorf("Failed to get service %s: %v", "search-postgres", err)
	}

	//check for secret
	secret := &corev1.Secret{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      "search-postgres",
		Namespace: namespace,
	}, secret)

	if err != nil {
		t.Fatalf("Failed to get secret %s: %v", "search-postgres", err)
	}

	//check for configmap
	configmap1 := &corev1.ConfigMap{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      "search-ca-crt",
		Namespace: namespace,
	}, configmap1)

	if err != nil {
		t.Errorf("Failed to get configmap %s: %v", "search-ca-crt", err)
	}

	//check for configmap
	configmap2 := &corev1.ConfigMap{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      "search-indexer",
		Namespace: namespace,
	}, configmap2)

	if err != nil {
		t.Fatalf("Failed to get configmap %s: %v", "search-indexer", err)
	}

	//check for configmap
	configmap3 := &corev1.ConfigMap{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      "search-postgres",
		Namespace: namespace,
	}, configmap3)

	if err != nil {
		t.Fatalf("Failed to get configmap %s: %v", "search-postgres", err)
	}

	verifyConfigmapDataContent(t, configmap3, "postgresql.conf", "ssl = 'on'")
	verifyConfigmapDataContent(t, configmap3, "postgresql-start.sh", "CREATE SCHEMA IF NOT EXISTS search")
	verifyConfigmapDataContent(t, configmap3, "custom-postgresql.conf", "# Customizations appended to postgresql.conf")

	//check for Service Account
	serviceaccount := &corev1.ServiceAccount{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      getServiceAccountName(),
		Namespace: namespace,
	}, serviceaccount)
	if err != nil {
		t.Errorf("Failed to get serviceaccount %s: %v", getServiceAccountName(), err)
	}

	//check for Role
	role := &rbacv1.ClusterRole{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      getRoleName(),
		Namespace: namespace,
	}, role)
	if err != nil {
		t.Errorf("Failed to get role %s: %v", getRoleName(), err)
	}

	//check for RoleBinding
	rolebinding := &rbacv1.ClusterRoleBinding{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      getRoleBindingName(),
		Namespace: namespace,
	}, rolebinding)

	if err != nil {
		t.Errorf("Failed to get serviceaccount %s: %v", getRoleBindingName(), err)
	}

	//check for PVC
	pvc := &corev1.PersistentVolumeClaim{}
	storageClassName := search.Spec.DBStorage.StorageClassName
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      getPVCName(storageClassName),
		Namespace: namespace,
	}, pvc)

	if err != nil {
		t.Errorf("Failed to get PersistentVolumeClaim %s: %v", getPVCName(storageClassName), err)
	}

	//Test EmptyDir
	search = &searchv1alpha1.Search{
		TypeMeta:   metav1.TypeMeta{Kind: "Search"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       searchv1alpha1.SearchSpec{},
	}

	objsEmpty := []runtime.Object{search}
	// Create a fake client to mock API calls.
	cl = fake.NewClientBuilder().WithStatusSubresource(search).WithRuntimeObjects(objsEmpty...).Build()

	r.Client = cl

	//check for PVC
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      getPVCName(storageClassName),
		Namespace: namespace,
	}, pvc)

	if !errors.IsNotFound(err) {
		t.Errorf("Emptydir expected but PVC found %v", err)
	}

	// Create a fake client to mock API calls.
	cl = fake.NewClientBuilder().WithStatusSubresource(search).WithRuntimeObjects(objsEmpty...).Build()

	r.Client = cl

	//Reconcile to check if the Finilizer is set
	_, err = r.Reconcile(context.TODO(), req)
	if err != nil {
		t.Logf("Error during reconcile: (%v)", err)
	}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      "search-v2-operator",
		Namespace: namespace,
	}, search)

	if err != nil {
		t.Logf("Failed to get Search: (%v)", err)
	}

	actual_finalizer := search.GetFinalizers()
	if len(actual_finalizer) != 1 || actual_finalizer[0] != "search.open-cluster-management.io/finalizer" {
		t.Errorf("Finalizer not set in search-v2-operator")
	}

	// Now delete the search CR
	err = cl.Delete(context.TODO(), search)
	if err != nil {
		t.Fatalf("Failed to update Search: (%v)", err)
	}
	_, err = r.Reconcile(context.TODO(), req)

	if err != nil {
		t.Logf("Error during reconcile: (%v)", err)
	}

	// We should expect Addon ClusterRole deleted by Finalizer
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      getAddonRoleName(),
		Namespace: namespace,
	}, role)

	if !errors.IsNotFound(err) {
		t.Errorf("Failed to delete Clusterrole %s", getAddonRoleName())
	}

	// We should expect Addon ClusterRolebinding deleted by Finalizer
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      getAddonRoleName(),
		Namespace: namespace,
	}, rolebinding)

	if !errors.IsNotFound(err) {
		t.Errorf("Failed to delete ClusterRoleBinding %s", getAddonRoleName())
	}

	// We should expect Search ClusterRole deleted by Finalizer
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      getRoleName(),
		Namespace: namespace,
	}, role)

	if !errors.IsNotFound(err) {
		t.Errorf("Failed to delete Clusterrole %s", getRoleName())
	}

	// We should expect Search ClusterRolebinding deleted by Finalizer
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      getRoleBindingName(),
		Namespace: namespace,
	}, rolebinding)

	if !errors.IsNotFound(err) {
		t.Errorf("Failed to delete ClusterRoleBinding %s", getRoleBindingName())
	}

}

func buildPod(name string, podCondition corev1.PodCondition) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{
			"app":  "search",
			"name": strings.Join(strings.Split(name, "-")[:2], "-"),
		}},
		Spec: corev1.PodSpec{},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.ContainersReady, Status: corev1.ConditionTrue, LastTransitionTime: metav1.Now()},
				podCondition,
			}},
	}
}

func TestSearch_controller_Status(t *testing.T) {
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
	runningCondition := corev1.PodCondition{Type: corev1.PodReady, Status: corev1.ConditionTrue, LastTransitionTime: metav1.Now()}
	collectorPod := buildPod("search-collector-abc", runningCondition)
	apiPod := buildPod("search-api-abc", runningCondition)
	indexerPod := buildPod("search-indexer-abc", runningCondition)
	postGresPod := buildPod("search-postgres-abc", runningCondition)

	s := scheme.Scheme
	err := searchv1alpha1.SchemeBuilder.AddToScheme(s)
	if err != nil {
		t.Errorf("error adding search scheme: (%v)", err)
	}

	err = addonv1alpha1.AddToScheme(s)
	if err != nil {
		t.Errorf("error adding addon scheme: (%v)", err)
	}

	objs := []runtime.Object{search, collectorPod, apiPod, indexerPod, postGresPod}
	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithStatusSubresource(search).WithRuntimeObjects(objs...).Build()

	r := &SearchReconciler{Client: cl, Scheme: s}

	// Mock request to simulate Reconcile() being called on an event for a watched resource.
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name: "Pod/search-api-abc",
		},
	}

	// trigger reconcile
	_, err = r.Reconcile(context.TODO(), req)
	if err != nil {
		t.Errorf("reconcile: (%v)", err)
	}

	//wait for update status
	time.Sleep(1 * time.Second)

	// fetch search-operator
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name: "search-v2-operator",
	}, search)

	if err != nil {
		t.Logf("Failed to get Search: (%v)", err)
	}
	// check if search status is set for api pod
	apiCondition := search.Status.Conditions[0]
	if apiCondition.Type != "Ready--search-api" ||
		apiCondition.Status != "True" ||
		apiCondition.Reason != "None" {
		t.Errorf("Failed to set status for api pod: (%v)", err)
	}

}

func TestSearch_controller_Status_Replicas3(t *testing.T) {
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
	runningCondition := corev1.PodCondition{Type: corev1.PodReady, Status: corev1.ConditionTrue, LastTransitionTime: metav1.Now()}
	errorCondition := corev1.PodCondition{Type: corev1.PodReady, Status: corev1.ConditionFalse, LastTransitionTime: metav1.Now(),
		Reason: "OOM", Message: "Error running pod"}
	collectorPod1 := buildPod("search-collector-abc1", runningCondition)
	collectorPod2 := buildPod("search-collector-abc2", errorCondition)
	collectorPod3 := buildPod("search-collector-abc3", runningCondition)

	apiPod := buildPod("search-api-abc", runningCondition)
	indexerPod := buildPod("search-indexer-abc", runningCondition)
	postGresPod := buildPod("search-postgres-abc", runningCondition)

	s := scheme.Scheme
	err := searchv1alpha1.SchemeBuilder.AddToScheme(s)
	if err != nil {
		t.Errorf("error adding search scheme: (%v)", err)
	}

	err = addonv1alpha1.AddToScheme(s)
	if err != nil {
		t.Errorf("error adding addon scheme: (%v)", err)
	}

	objs := []runtime.Object{search, collectorPod1, collectorPod2, collectorPod3, apiPod, indexerPod, postGresPod}
	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithStatusSubresource(search).WithRuntimeObjects(objs...).Build()

	r := &SearchReconciler{Client: cl, Scheme: s}

	// Mock request to simulate Reconcile() being called on an event for a watched resource.
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name: "Pod/search-collector-abc1",
		},
	}

	// trigger reconcile
	_, err = r.Reconcile(context.TODO(), req)
	if err != nil {
		t.Errorf("reconcile: (%v)", err)
	}

	//wait for update status
	time.Sleep(1 * time.Second)

	// fetch search-operator
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name: "search-v2-operator",
	}, search)
	if err != nil {
		t.Logf("Failed to get Search: (%v)", err)
	}
	// check if search status is set for api pod
	resultCondition := search.Status.Conditions[0]
	if resultCondition.Type != "Ready--search-collector" ||
		resultCondition.Status != "False" ||
		resultCondition.Reason != "OOM" ||
		resultCondition.Message != "Error running pod" {
		t.Errorf("Failed to set status for collector pod: (%v)", err)
	}

}

func TestSearch_controller_Status_Replicas0(t *testing.T) {
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
	runningCondition := corev1.PodCondition{Type: corev1.PodReady, Status: corev1.ConditionTrue, LastTransitionTime: metav1.Now()}

	apiPod := buildPod("search-api-abc", runningCondition)
	indexerPod := buildPod("search-indexer-abc", runningCondition)
	postGresPod := buildPod("search-postgres-abc", runningCondition)

	s := scheme.Scheme
	err := searchv1alpha1.SchemeBuilder.AddToScheme(s)
	if err != nil {
		t.Errorf("error adding search scheme: (%v)", err)
	}

	objs := []runtime.Object{search, apiPod, indexerPod, postGresPod}
	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithStatusSubresource(search).WithRuntimeObjects(objs...).Build()

	r := &SearchReconciler{Client: cl, Scheme: s}

	// Mock request to simulate Reconcile() being called on an event for a watched resource.
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name: "Pod/search-collector-abc",
		},
	}

	// trigger reconcile
	_, err = r.Reconcile(context.TODO(), req)
	if err != nil {
		t.Errorf("reconcile: (%v)", err)
	}

	//wait for update status
	time.Sleep(1 * time.Second)

	// fetch search-operator
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name: "search-v2-operator",
	}, search)
	if err != nil {
		t.Logf("Failed to get Search: (%v)", err)
	}
	// check if search status is set for api pod
	resultCondition := search.Status.Conditions[0]

	if resultCondition.Type != "Ready--search-collector" ||
		resultCondition.Status != "False" ||
		resultCondition.Reason != "NoPodsFound" ||
		resultCondition.Message != "Check status of deployment: search-collector" {
		t.Errorf("Failed to set status for collector pod: (%v)", err)
	}
}

func TestSearch_controller_Status_Update(t *testing.T) {
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
		Status: searchv1alpha1.SearchStatus{
			DB:      "db",
			Storage: "storage",
			Conditions: []metav1.Condition{
				{Type: "Ready--search-api", Reason: "None", Message: "None", Status: "True"},
				{Type: "Ready--search-collector", Reason: "None", Message: "None", Status: "True"}},
		},
	}
	runningCondition := corev1.PodCondition{Type: corev1.PodReady, Status: corev1.ConditionTrue, LastTransitionTime: metav1.Now()}

	apiPod := buildPod("search-api-abc", runningCondition)
	indexerPod := buildPod("search-indexer-abc", runningCondition)
	postGresPod := buildPod("search-postgres-abc", runningCondition)

	s := scheme.Scheme
	err := searchv1alpha1.SchemeBuilder.AddToScheme(s)
	if err != nil {
		t.Errorf("error adding search scheme: (%v)", err)
	}

	objs := []runtime.Object{search, apiPod, indexerPod, postGresPod}
	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithStatusSubresource(search).WithRuntimeObjects(objs...).Build()

	r := &SearchReconciler{Client: cl, Scheme: s}

	// Mock request to simulate Reconcile() being called on an event for a watched resource.
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name: "Pod/search-collector-abc",
		},
	}

	// trigger reconcile
	_, err = r.Reconcile(context.TODO(), req)
	if err != nil {
		t.Errorf("reconcile: (%v)", err)
	}

	//wait for update status
	time.Sleep(1 * time.Second)

	// fetch search-operator
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name: "search-v2-operator",
	}, search)

	if err != nil {
		t.Logf("Failed to get Search: (%v)", err)
	}
	// check if search status is set for api pod
	resultCondition := search.Status.Conditions[1]
	if resultCondition.Type != "Ready--search-collector" ||
		resultCondition.Status != "False" ||
		resultCondition.Reason != "NoPodsFound" ||
		resultCondition.Message != "Check status of deployment: search-collector" {
		t.Errorf("Failed to update status for collector pod: (%v)", err)
	}
	if search.Status.DB != "search" ||
		search.Status.Storage != "test" {
		t.Errorf("Failed to update db or storage for search CR instance: (%v)", err)

	}
}

// Verifies that the expected Environment value is present in Deployment
func verifyDeploymentEnv(t *testing.T, dep *appsv1.Deployment, expectedKey string, expectedValue string) {
	for _, val := range dep.Spec.Template.Spec.Containers[0].Env {
		if val.Name == expectedKey {
			if val.Value != expectedValue {
				t.Errorf("Unexpected envVar key : %s found  : %s expected: %s", expectedKey, val.Value, expectedValue)
			}
			return
		}
	}
	t.Errorf("Failed to find EnvVar: %s in deployment: %s", expectedKey, dep.Name)
}

// Verifies that the expected argument is present in Deployment
func verifyDeploymentArgs(t *testing.T, dep *appsv1.Deployment, expectedVal string) {
	for _, val := range dep.Spec.Template.Spec.Containers[0].Args {
		if val == expectedVal {
			return
		}
	}
	t.Errorf("Failed to find Args: %s in deployment: %s", expectedVal, dep.Name)
}
func TestSearch_controller_DBConfig(t *testing.T) {
	var expectedMap = map[string]string{}
	var (
		name = "search-v2-operator"
	)
	search := &searchv1alpha1.Search{
		TypeMeta:   metav1.TypeMeta{Kind: "Search"},
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: searchv1alpha1.SearchSpec{
			DBConfig: "searchcustomization",
		},
	}
	s := scheme.Scheme
	err := searchv1alpha1.SchemeBuilder.AddToScheme(s)
	if err != nil {
		t.Errorf("error adding search scheme: (%v)", err)
	}
	err = monitorv1.AddToScheme(s)
	if err != nil {
		t.Errorf("error adding monitor scheme: (%v)", err)
	}
	err = addonv1alpha1.AddToScheme(s)
	if err != nil {
		t.Errorf("error adding addon scheme: (%v)", err)
	}
	//create configmap which has the customization for postgres DB
	customConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "searchcustomization"},
		Data:       expectedMap,
	}

	objs := []runtime.Object{search, customConfigMap}
	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithStatusSubresource(search).WithRuntimeObjects(objs...).Build()

	r := &SearchReconciler{Client: cl, DynamicClient: fakeDynClient(), Scheme: s}
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
	//check for created search-postgres configmap
	configmap := &corev1.ConfigMap{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name: "search-postgres",
	}, configmap)

	if err != nil {
		t.Fatalf("Failed to get configmap %s: %v", "search-postgres", err)
	}

	verifyConfigmapDataContent(t, configmap, "postgresql.conf", "ssl = 'on'")
	verifyConfigmapDataContent(t, configmap, "postgresql-start.sh", "ALTER ROLE searchuser set work_mem='64MB'")

	//check for created search-postgres deployment
	dep := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name: "search-postgres",
	}, dep)

	if err != nil {
		t.Fatalf("Failed to get deployment %s: %v", "search-postgres", err)
	}
	//Should be the default constant values set in common.go
	verifyDeploymentEnv(t, dep, "WORK_MEM", "64MB")
	verifyDeploymentEnv(t, dep, "POSTGRESQL_SHARED_BUFFERS", "1GB")
	verifyDeploymentEnv(t, dep, "POSTGRESQL_EFFECTIVE_CACHE_SIZE", "2GB")
}

func TestSearch_controller_Metrics(t *testing.T) {
	var (
		name = "search-v2-operator"
	)
	search := &searchv1alpha1.Search{
		TypeMeta:   metav1.TypeMeta{Kind: "Search"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ocm"},
		Spec: searchv1alpha1.SearchSpec{
			DBStorage: searchv1alpha1.StorageSpec{
				StorageClassName: "test",
			},
		},
	}

	runningCondition := corev1.PodCondition{Type: corev1.PodReady, Status: corev1.ConditionTrue, LastTransitionTime: metav1.Now()}
	collectorPod := buildPod("search-collector-abc", runningCondition)
	apiPod := buildPod("search-api-abc", runningCondition)
	indexerPod := buildPod("search-indexer-abc", runningCondition)
	postGresPod := buildPod("search-postgres-abc", runningCondition)

	s := scheme.Scheme
	err := searchv1alpha1.SchemeBuilder.AddToScheme(s)
	if err != nil {
		t.Errorf("error adding search scheme: (%v)", err)
	}

	err = addonv1alpha1.AddToScheme(s)
	if err != nil {
		t.Errorf("error adding addon scheme: (%v)", err)
	}
	err = monitorv1.AddToScheme(s)
	if err != nil {
		t.Errorf("error adding monitor scheme: (%v)", err)
	}
	err = rbacv1.AddToScheme(s)
	if err != nil {
		t.Errorf("error adding rbac scheme: (%v)", err)
	}
	r := &SearchReconciler{Scheme: s, DynamicClient: fakeDynClient()}
	// legacy servicemonitor - should get deleted after reconcile
	// searchApiMonitor := r.ServiceMonitor(search, "search-api", "openshift-monitoring")
	objs := []runtime.Object{search, collectorPod, apiPod, indexerPod, postGresPod} //, searchApiMonitor}
	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithStatusSubresource(search).WithRuntimeObjects(objs...).Build()
	r.Client = cl

	// Mock request to simulate Reconcile() being called on an event for a watched resource.
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "ServiceMonitor/search-api-monitor",
			Namespace: search.Namespace,
		},
	}

	// trigger reconcile
	_, err = r.Reconcile(context.TODO(), req)
	if err != nil {
		t.Errorf("reconcile: (%v)", err)
	}
	//wait for update status
	time.Sleep(1 * time.Second)
	sm := &monitorv1.ServiceMonitor{TypeMeta: metav1.TypeMeta{Kind: "ServiceMonitor"}}
	roleb := &rbacv1.RoleBinding{TypeMeta: metav1.TypeMeta{Kind: "RoleBinding"}}
	role := &rbacv1.Role{TypeMeta: metav1.TypeMeta{Kind: "Role"}}
	// fetch search-service-monitor
	smErr := cl.Get(context.TODO(), types.NamespacedName{
		Name:      "search-api-monitor",
		Namespace: search.Namespace,
	}, sm)
	if smErr != nil {
		t.Errorf("Failed to get ServiceMonitor SearchApiMonitor: (%v)", smErr)
	}
	//service monitor should not be present in openshift-monitoring namespace
	smErr = cl.Get(context.TODO(), types.NamespacedName{
		Name:      "search-api-monitor",
		Namespace: "openshift-monitoring",
	}, sm)
	if smErr == nil && errors.IsNotFound(smErr) {
		t.Errorf("ServiceMonitor SearchApiMonitor present in openshift-monitoring namespace: (%v)", smErr)
	}
	//service monitor should not be present in openshift-monitoring namespace
	smErr = cl.Get(context.TODO(), types.NamespacedName{
		Name:      "search-indexer-monitor",
		Namespace: "openshift-monitoring",
	}, sm)
	if smErr == nil && errors.IsNotFound(smErr) {
		t.Errorf("ServiceMonitor SearchIndexerMonitor present in openshift-monitoring namespace: (%v)", smErr)
	}
	// fetch search-service-monitor
	smErr = cl.Get(context.TODO(), types.NamespacedName{
		Name:      "search-indexer-monitor",
		Namespace: search.Namespace,
	}, sm)
	if smErr != nil {
		t.Errorf("Failed to get ServiceMonitor SearchIndexerMonitor: (%v)", smErr)
	}
	// fetch metrics rolebinding
	rbErr := cl.Get(context.TODO(), types.NamespacedName{
		Name: SearchMetricsMonitor,
	}, roleb)
	if rbErr == nil {
		t.Errorf("Found RoleBinding SearchMonitor: (%v) when not expected", rbErr)
	}
	// fetch metrics role
	roleErr := cl.Get(context.TODO(), types.NamespacedName{
		Name: SearchMetricsMonitor,
	}, role)
	if roleErr == nil {
		t.Errorf("Found Role SearchMonitor: (%v) when not expected", roleErr)
	}
}

func TestSearch_controller_DBConfigAndEnvOverlap(t *testing.T) {
	var expectedMap = map[string]string{"POSTGRESQL_SHARED_BUFFERS": "11MB", "WORK_MEM": "12MB",
		"POSTGRESQL_EFFECTIVE_CACHE_SIZE": "23MB",
	}
	var (
		name = "search-v2-operator"
	)
	search := &searchv1alpha1.Search{
		TypeMeta:   metav1.TypeMeta{Kind: "Search"},
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: searchv1alpha1.SearchSpec{
			DBConfig: "searchcustomization",
			Deployments: searchv1alpha1.SearchDeployments{
				Database: searchv1alpha1.DeploymentConfig{
					Arguments: []string{"-v=1"},
					Env: []corev1.EnvVar{ // These are the values that goes into the ENV
						{Name: "POSTGRESQL_SHARED_BUFFERS", Value: "22MB"},
						{Name: "WORK_MEM", Value: "33MB"},
						{Name: "POSTGRESQL_EFFECTIVE_CACHE_SIZE", Value: "13MB"},
					},
				},
				Collector: searchv1alpha1.DeploymentConfig{
					Arguments: []string{"-v=2"},
					Env: []corev1.EnvVar{
						{Name: "collEnv", Value: "collEnvValue"},
					},
				},
				QueryAPI: searchv1alpha1.DeploymentConfig{
					Arguments: []string{"-v=3"},
					Env: []corev1.EnvVar{
						{Name: "apiEnv", Value: "apiEnvValue"},
					},
				},
				Indexer: searchv1alpha1.DeploymentConfig{
					Arguments: []string{"-v=4"},
					Env: []corev1.EnvVar{
						{Name: "indEnv", Value: "indEnvValue"},
					},
				},
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
	//configmap customization won't be applied to postgres
	customConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "searchcustomization"},
		Data:       expectedMap,
	}

	objs := []runtime.Object{search, customConfigMap}
	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithStatusSubresource(search).WithRuntimeObjects(objs...).Build()

	r := &SearchReconciler{Client: cl, DynamicClient: fakeDynClient(), Scheme: s}
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
	//check for created search-postgres configmap
	configmap := &corev1.ConfigMap{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name: "search-postgres",
	}, configmap)

	if err != nil {
		t.Fatalf("Failed to get configmap %s: %v", "search-postgres", err)
	}
	verifyConfigmapDataContent(t, configmap, "postgresql.conf", "ssl = 'on'")
	verifyConfigmapDataContent(t, configmap, "postgresql.conf", "ssl_ciphers = 'HIGH:!aNULL'")
	verifyConfigmapDataContent(t, configmap, "postgresql-start.sh", "ALTER ROLE searchuser set work_mem='33MB'")

	//check for created search-postgres deployment
	dep1 := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name: "search-postgres",
	}, dep1)

	if err != nil {
		t.Fatalf("Failed to get deployment %s: %v", "search-postgres", err)
	}
	verifyDeploymentEnv(t, dep1, "WORK_MEM", "33MB")
	verifyDeploymentEnv(t, dep1, "POSTGRESQL_SHARED_BUFFERS", "22MB")
	verifyDeploymentEnv(t, dep1, "POSTGRESQL_EFFECTIVE_CACHE_SIZE", "13MB")
	verifyDeploymentArgs(t, dep1, "-v=1")

	//check for created search-collector deployment
	dep2 := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name: "search-collector",
	}, dep2)

	if err != nil {
		t.Fatalf("Failed to get deployment %s: %v", "search-collector", err)
	}
	verifyDeploymentEnv(t, dep2, "collEnv", "collEnvValue")
	verifyDeploymentArgs(t, dep2, "-v=2")

	//check for created search-api deployment
	dep3 := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name: "search-api",
	}, dep3)

	if err != nil {
		t.Fatalf("Failed to get deployment %s: %v", "search-api", err)
	}
	verifyDeploymentEnv(t, dep3, "apiEnv", "apiEnvValue")
	verifyDeploymentArgs(t, dep3, "-v=3")

	//check for created search-indexer deployment
	dep4 := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name: "search-indexer",
	}, dep4)

	if err != nil {
		t.Fatalf("Failed to get deployment %s: %v", "search-indexer", err)
	}
	verifyDeploymentEnv(t, dep4, "indEnv", "indEnvValue")
	verifyDeploymentArgs(t, dep4, "-v=4")
}
