// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"
	"os"
	"reflect"
	"testing"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGetDeploymentConfigForNil(t *testing.T) {
	instance := &searchv1alpha1.Search{
		Spec: searchv1alpha1.SearchSpec{
			Deployments: searchv1alpha1.SearchDeployments{
				QueryAPI: searchv1alpha1.DeploymentConfig{
					ReplicaCount: 1,
				},
			},
		},
	}
	deploymentConfig := getDeploymentConfig("search-api", instance)
	if deploymentConfig.DeepCopy() == nil {
		t.Error("DeploymentConfig returned unexpectd nil")
	}
}

func TestResourcesNotCustomized(t *testing.T) {
	instance := &searchv1alpha1.Search{
		Spec: searchv1alpha1.SearchSpec{
			Deployments: searchv1alpha1.SearchDeployments{
				QueryAPI: searchv1alpha1.DeploymentConfig{
					ReplicaCount: 1,
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"memory": resource.MustParse("25Mi"),
						},
						Requests: corev1.ResourceList{
							"cpu":    resource.MustParse("25m"),
							"memory": resource.MustParse("10Mi"),
						},
					},
				},
			},
		},
	}
	os.Setenv("COLLECTOR_IMAGE", "value-from-env")

	actualNodelSelector := getNodeSelector("search-collector", instance)
	if actualNodelSelector != nil {
		t.Error("NodeSelector Not expected")
	}
	actualImagePullPolicy := getImagePullPolicy("search-collector", instance)
	if actualImagePullPolicy != "IfNotPresent" {
		t.Error("ImagePullPolicy Not expected")
	}
	actualImageSha := getImageSha("search-collector", instance)
	if actualImageSha != "value-from-env" {
		t.Error("ImageOverride with incorrect image")
	}
}
func TestAPICustomization(t *testing.T) {
	testFor := "search-api"
	tol := corev1.Toleration{
		Key:      "node-role.kubernetes.io/infra",
		Effect:   corev1.TaintEffectNoSchedule,
		Operator: corev1.TolerationOpExists,
	}
	instance := &searchv1alpha1.Search{
		Spec: searchv1alpha1.SearchSpec{
			ImagePullPolicy: "IfNotPresent",
			ImagePullSecret: "personal-pull-secret",
			NodeSelector:    map[string]string{"key1": "val1"},
			Tolerations:     []corev1.Toleration{tol},
			Deployments: searchv1alpha1.SearchDeployments{
				QueryAPI: searchv1alpha1.DeploymentConfig{
					ReplicaCount:  5,
					ImageOverride: "quay.io/test-image:007",
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"memory": resource.MustParse("25Mi"),
							"cpu":    resource.MustParse("40m"),
						},
						Requests: corev1.ResourceList{
							"cpu":    resource.MustParse("25m"),
							"memory": resource.MustParse("10Mi"),
						},
					},
					Env: []corev1.EnvVar{
						{Name: "env1", Value: "value1"},
						{Name: "env2", Value: "value2"},
					},
				},
			},
		},
	}
	want := "val1"
	actualNodeSelector := getNodeSelector(testFor, instance)
	if actualNodeSelector["key1"] != want {
		t.Error("Incorrect NodeSelector")
	}
	wantEffect := corev1.TaintEffectNoSchedule
	wantOperator := corev1.TolerationOpExists
	actualTolerations := getTolerations(testFor, instance)
	if actualTolerations[0].Effect != wantEffect {
		t.Error("Incorrect Toleration Effect")
	}
	if actualTolerations[0].Operator != wantOperator {
		t.Error("Incorrect Toleration Operator")
	}
	actualImagePullPolicy := getImagePullPolicy(testFor, instance)
	if actualImagePullPolicy != "IfNotPresent" {
		t.Error("ImagePullPolicy Not expected")
	}
	actualReplicaCount := getReplicaCount(testFor, instance)
	if *actualReplicaCount != int32(5) {
		t.Error("ReplicaCount Not expected")
	}
	request_memory_want := "10Mi"
	request_cpu_want := "25m"
	limit_cpu_want := "40m"
	limit_memory_want := "25Mi"
	actualResourceRequirements := getResourceRequirements("search-api", instance)
	if actualResourceRequirements.Requests.Memory().String() != request_memory_want {
		t.Error("Request Memory Not expected")
	}
	if actualResourceRequirements.Requests.Cpu().String() != request_cpu_want {
		t.Error("Request Memory Not expected")
	}
	if actualResourceRequirements.Limits.Memory().String() != limit_memory_want {
		t.Error("Limit Memory Not expected")
	}
	if actualResourceRequirements.Limits.Cpu().String() != limit_cpu_want {
		t.Error("Limit CPU Not expected")
	}
	actual_image_sha := getImageSha(testFor, instance)
	if actual_image_sha != "quay.io/test-image:007" {
		t.Error("ImageOverride with incorrect image")
	}

	envVars := getContainerEnvVar("search-api", instance)
	if len(envVars) != 2 || envVars[0].Name != "env1" || envVars[0].Value != "value1" {
		t.Error("Env vars not set for search-api")
	}
}

func TestIndexerCustomization(t *testing.T) {
	testFor := "search-indexer"
	tol := corev1.Toleration{
		Key:      "node-role.kubernetes.io/infra",
		Effect:   corev1.TaintEffectNoSchedule,
		Operator: corev1.TolerationOpExists,
	}
	instance := &searchv1alpha1.Search{
		Spec: searchv1alpha1.SearchSpec{
			ImagePullPolicy: "IfNotPresent",
			ImagePullSecret: "personal-pull-secret",
			NodeSelector:    map[string]string{"key1": "val1"},
			Tolerations:     []corev1.Toleration{tol},
			Deployments: searchv1alpha1.SearchDeployments{
				Indexer: searchv1alpha1.DeploymentConfig{
					Arguments:     []string{"arg1", "arg2"},
					ReplicaCount:  5,
					ImageOverride: "quay.io/test-image:007",
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"memory": resource.MustParse("25Mi"),
							"cpu":    resource.MustParse("40m"),
						},
						Requests: corev1.ResourceList{
							"cpu":    resource.MustParse("25m"),
							"memory": resource.MustParse("10Mi"),
						},
					},
					Env: []corev1.EnvVar{
						{Name: "env1", Value: "value1"},
						{Name: "env2", Value: "value2"},
					},
				},
			},
		},
	}
	want := "val1"
	actualNodeSelector := getNodeSelector(testFor, instance)
	if actualNodeSelector["key1"] != want {
		t.Error("Incorrect NodeSelector")
	}
	wantEffect := corev1.TaintEffectNoSchedule
	wantOperator := corev1.TolerationOpExists
	actualTolerations := getTolerations(testFor, instance)
	if actualTolerations[0].Effect != wantEffect {
		t.Error("Incorrect Toleration Effect")
	}
	if actualTolerations[0].Operator != wantOperator {
		t.Error("Incorrect Toleration Operator")
	}
	actualImagePullPolicy := getImagePullPolicy(testFor, instance)
	if actualImagePullPolicy != "IfNotPresent" {
		t.Error("ImagePullPolicy Not expected")
	}
	actualReplicaCount := getReplicaCount(testFor, instance)
	if *actualReplicaCount != int32(5) {
		t.Error("ReplicaCount Not expected")
	}
	request_memory_want := "10Mi"
	request_cpu_want := "25m"
	limit_cpu_want := "40m"
	limit_memory_want := "25Mi"
	actualResourceRequirements := getResourceRequirements(testFor, instance)
	if actualResourceRequirements.Requests.Memory().String() != request_memory_want {
		t.Error("Request Memory Not expected")
	}
	if actualResourceRequirements.Requests.Cpu().String() != request_cpu_want {
		t.Error("Request Memory Not expected")
	}
	if actualResourceRequirements.Limits.Memory().String() != limit_memory_want {
		t.Error("Limit Memory Not expected")
	}
	if actualResourceRequirements.Limits.Cpu().String() != limit_cpu_want {
		t.Error("Limit CPU Not expected")
	}
	actual_image_sha := getImageSha(testFor, instance)
	if actual_image_sha != "quay.io/test-image:007" {
		t.Error("ImageOverride with incorrect image")
	}
	actual_args := getContainerArgs(testFor, instance)
	if actual_args == nil || len(actual_args) != 2 || actual_args[0] != "arg1" || actual_args[1] != "arg2" {
		t.Error("Incorrect Args parsed")
	}
	envVars := getContainerEnvVar(testFor, instance)
	if len(envVars) != 2 || envVars[0].Name != "env1" || envVars[0].Value != "value1" {
		t.Errorf("Env vars not set for %s", testFor)
	}
}
func TestCollectorCustomization(t *testing.T) {
	testFor := "search-collector"
	tol := corev1.Toleration{
		Key:      "node-role.kubernetes.io/infra",
		Effect:   corev1.TaintEffectNoSchedule,
		Operator: corev1.TolerationOpExists,
	}
	instance := &searchv1alpha1.Search{
		Spec: searchv1alpha1.SearchSpec{
			ImagePullPolicy: "IfNotPresent",
			ImagePullSecret: "personal-pull-secret",
			NodeSelector:    map[string]string{"key1": "val1"},
			Tolerations:     []corev1.Toleration{tol},
			Deployments: searchv1alpha1.SearchDeployments{
				Collector: searchv1alpha1.DeploymentConfig{
					ReplicaCount:  5,
					ImageOverride: "quay.io/test-image:007",
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"memory": resource.MustParse("25Mi"),
							"cpu":    resource.MustParse("40m"),
						},
						Requests: corev1.ResourceList{
							"cpu":    resource.MustParse("25m"),
							"memory": resource.MustParse("10Mi"),
						},
					},
					Env: []corev1.EnvVar{
						{Name: "env1", Value: "value1"},
						{Name: "env2", Value: "value2"},
					},
				},
			},
		},
	}
	want := "val1"
	actualNodeSelector := getNodeSelector(testFor, instance)
	if actualNodeSelector["key1"] != want {
		t.Error("Incorrect NodeSelector")
	}
	wantEffect := corev1.TaintEffectNoSchedule
	wantOperator := corev1.TolerationOpExists
	actualTolerations := getTolerations(testFor, instance)
	if actualTolerations[0].Effect != wantEffect {
		t.Error("Incorrect Toleration Effect")
	}
	if actualTolerations[0].Operator != wantOperator {
		t.Error("Incorrect Toleration Operator")
	}
	actualImagePullPolicy := getImagePullPolicy(testFor, instance)
	if actualImagePullPolicy != "IfNotPresent" {
		t.Error("ImagePullPolicy Not expected")
	}
	actualReplicaCount := getReplicaCount(testFor, instance)
	if *actualReplicaCount != int32(1) {
		t.Error("ReplicaCount Not expected")
	}
	request_memory_want := "10Mi"
	request_cpu_want := "25m"
	limit_cpu_want := "40m"
	limit_memory_want := "25Mi"
	actualResourceRequirements := getResourceRequirements(testFor, instance)
	if actualResourceRequirements.Requests.Memory().String() != request_memory_want {
		t.Error("Request Memory Not expected")
	}
	if actualResourceRequirements.Requests.Cpu().String() != request_cpu_want {
		t.Error("Request Memory Not expected")
	}
	if actualResourceRequirements.Limits.Memory().String() != limit_memory_want {
		t.Error("Limit Memory Not expected")
	}
	if actualResourceRequirements.Limits.Cpu().String() != limit_cpu_want {
		t.Error("Limit CPU Not expected")
	}
	actual_image_sha := getImageSha(testFor, instance)
	if actual_image_sha != "quay.io/test-image:007" {
		t.Error("ImageOverride with incorrect image")
	}
	actual_args := getContainerArgs(testFor, instance)
	if actual_args != nil {
		t.Error("Incorrect Args parsed")
	}
	envVars := getContainerEnvVar(testFor, instance)
	if len(envVars) != 2 || envVars[0].Name != "env1" || envVars[0].Value != "value1" {
		t.Errorf("Env vars not set for %s", testFor)
	}
}

func TestPostgresCustomization(t *testing.T) {
	testFor := "search-postgres"
	tol := corev1.Toleration{
		Key:      "node-role.kubernetes.io/infra",
		Effect:   corev1.TaintEffectNoSchedule,
		Operator: corev1.TolerationOpExists,
	}
	instance := &searchv1alpha1.Search{
		Spec: searchv1alpha1.SearchSpec{
			ImagePullPolicy: "IfNotPresent",
			ImagePullSecret: "personal-pull-secret",
			NodeSelector:    map[string]string{"key1": "val1"},
			Tolerations:     []corev1.Toleration{tol},
			Deployments: searchv1alpha1.SearchDeployments{
				Database: searchv1alpha1.DeploymentConfig{
					Arguments:     []string{"arg1"},
					ReplicaCount:  5,
					ImageOverride: "quay.io/test-image:007",
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"memory": resource.MustParse("25Mi"),
							"cpu":    resource.MustParse("40m"),
						},
						Requests: corev1.ResourceList{
							"cpu":    resource.MustParse("25m"),
							"memory": resource.MustParse("10Mi"),
						},
					},
					Env: []corev1.EnvVar{
						{Name: "env1", Value: "value1"},
						{Name: "env2", Value: "value2"},
					},
				},
			},
		},
	}
	want := "val1"
	actualNodeSelector := getNodeSelector(testFor, instance)
	if actualNodeSelector["key1"] != want {
		t.Error("Incorrect NodeSelector")
	}
	wantEffect := corev1.TaintEffectNoSchedule
	wantOperator := corev1.TolerationOpExists
	actualTolerations := getTolerations(testFor, instance)
	if actualTolerations[0].Effect != wantEffect {
		t.Error("Incorrect Toleration Effect")
	}
	if actualTolerations[0].Operator != wantOperator {
		t.Error("Incorrect Toleration Operator")
	}
	actualImagePullPolicy := getImagePullPolicy(testFor, instance)
	if actualImagePullPolicy != "IfNotPresent" {
		t.Error("ImagePullPolicy Not expected")
	}
	actualReplicaCount := getReplicaCount(testFor, instance)
	if *actualReplicaCount != int32(1) {
		t.Error("ReplicaCount Not expected")
	}
	request_memory_want := "10Mi"
	request_cpu_want := "25m"
	limit_cpu_want := "40m"
	limit_memory_want := "25Mi"
	actualResourceRequirements := getResourceRequirements(testFor, instance)
	if actualResourceRequirements.Requests.Memory().String() != request_memory_want {
		t.Error("Request Memory Not expected")
	}
	if actualResourceRequirements.Requests.Cpu().String() != request_cpu_want {
		t.Error("Request Memory Not expected")
	}
	if actualResourceRequirements.Limits.Memory().String() != limit_memory_want {
		t.Error("Limit Memory Not expected")
	}
	if actualResourceRequirements.Limits.Cpu().String() != limit_cpu_want {
		t.Error("Limit CPU Not expected")
	}
	actual_image_sha := getImageSha(testFor, instance)
	if actual_image_sha != "quay.io/test-image:007" {
		t.Error("ImageOverride with incorrect image")
	}
	actual_args := getContainerArgs(testFor, instance)
	if actual_args == nil || len(actual_args) != 1 || actual_args[0] != "arg1" {
		t.Error("Incorrect Args parsed")
	}

	actual_volume := getPostgresVolume(instance)
	if actual_volume.VolumeSource.EmptyDir == nil {
		t.Error("Incorrect Volume created")
	}
	envVars := getContainerEnvVar(testFor, instance)
	if len(envVars) != 2 || envVars[0].Name != "env1" || envVars[0].Value != "value1" {
		t.Errorf("Env vars not set for %s", testFor)
	}
}

func TestPostgresCustomizationPVC(t *testing.T) {
	tol := corev1.Toleration{
		Key:      "node-role.kubernetes.io/infra",
		Effect:   corev1.TaintEffectNoSchedule,
		Operator: corev1.TolerationOpExists,
	}
	instance := &searchv1alpha1.Search{
		Spec: searchv1alpha1.SearchSpec{
			ImagePullPolicy: "IfNotPresent",
			ImagePullSecret: "personal-pull-secret",
			NodeSelector:    map[string]string{"key1": "val1"},
			Tolerations:     []corev1.Toleration{tol},
			DBStorage: searchv1alpha1.StorageSpec{
				StorageClassName: "test",
			},
			Deployments: searchv1alpha1.SearchDeployments{
				Database: searchv1alpha1.DeploymentConfig{
					Arguments:     []string{"arg1"},
					ReplicaCount:  5,
					ImageOverride: "quay.io/test-image:007",
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"memory": resource.MustParse("25Mi"),
							"cpu":    resource.MustParse("40m"),
						},
						Requests: corev1.ResourceList{
							"cpu":    resource.MustParse("25m"),
							"memory": resource.MustParse("10Mi"),
						},
					},
				},
			},
		},
	}
	actual_volume := getPostgresVolume(instance)
	if actual_volume.VolumeSource.PersistentVolumeClaim.ClaimName != "test-search" {
		t.Error("Incorrect Volume created")
	}
}

func TestCustomDBConfig(t *testing.T) {
	var expectedMap = map[string]string{"postgresConfigMapPath": "SomePath"}

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
	//create configmap which has custom values for postgres DB
	customConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "searchcustomization"},
		Data:       expectedMap,
	}

	objs := []runtime.Object{search, customConfigMap}
	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()

	r := &SearchReconciler{Client: cl, Scheme: s}

	actualMap := r.getDBConfigData(context.TODO(), search)
	if len(actualMap) != len(expectedMap) {
		t.Errorf("Unexpected data in configmap. Expected: %d, Got:%d", len(expectedMap), len(actualMap))
	}
	if !reflect.DeepEqual(expectedMap, actualMap) {
		t.Errorf("Unexpected data content in configmap")
	}
}

func TestCpuLimitCustomization(t *testing.T) {
	testFor := "search-indexer"
	tol := corev1.Toleration{
		Key:      "node-role.kubernetes.io/infra",
		Effect:   corev1.TaintEffectNoSchedule,
		Operator: corev1.TolerationOpExists,
	}
	instance := &searchv1alpha1.Search{
		Spec: searchv1alpha1.SearchSpec{
			ImagePullPolicy: "IfNotPresent",
			ImagePullSecret: "personal-pull-secret",
			NodeSelector:    map[string]string{"key1": "val1"},
			Tolerations:     []corev1.Toleration{tol},
			Deployments: searchv1alpha1.SearchDeployments{
				Indexer: searchv1alpha1.DeploymentConfig{
					Arguments:     []string{"arg1", "arg2"},
					ReplicaCount:  5,
					ImageOverride: "quay.io/test-image:007",
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"memory": resource.MustParse("25Mi"),
						},
						Requests: corev1.ResourceList{
							"cpu":    resource.MustParse("25m"),
							"memory": resource.MustParse("10Mi"),
						},
					},
				},
			},
		},
	}

	actualResourceRequirements := getResourceRequirements(testFor, instance)
	if actualResourceRequirements.Limits.Cpu().String() != "0" {
		t.Error("Limit CPU Not expected")
	}

}

func TestMemoryCpuLimitCustomization(t *testing.T) {
	testFor := "search-indexer"
	tol := corev1.Toleration{
		Key:      "node-role.kubernetes.io/infra",
		Effect:   corev1.TaintEffectNoSchedule,
		Operator: corev1.TolerationOpExists,
	}
	instance := &searchv1alpha1.Search{
		Spec: searchv1alpha1.SearchSpec{
			ImagePullPolicy: "IfNotPresent",
			ImagePullSecret: "personal-pull-secret",
			NodeSelector:    map[string]string{"key1": "val1"},
			Tolerations:     []corev1.Toleration{tol},
			Deployments: searchv1alpha1.SearchDeployments{
				Indexer: searchv1alpha1.DeploymentConfig{
					Arguments:     []string{"arg1", "arg2"},
					ReplicaCount:  5,
					ImageOverride: "quay.io/test-image:007",
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{},
						Requests: corev1.ResourceList{
							"cpu":    resource.MustParse("25m"),
							"memory": resource.MustParse("10Mi"),
						},
					},
				},
			},
		},
	}

	actualResourceRequirements := getResourceRequirements(testFor, instance)
	if actualResourceRequirements.Limits.Cpu().String() != "0" {
		t.Error("Limit CPU Not expected")
	}
	if actualResourceRequirements.Limits.Memory().String() != "0" {
		t.Error("Limit Memory Not expected")
	}
}

func TestMemoryLimitCustomization(t *testing.T) {
	testFor := "search-indexer"
	tol := corev1.Toleration{
		Key:      "node-role.kubernetes.io/infra",
		Effect:   corev1.TaintEffectNoSchedule,
		Operator: corev1.TolerationOpExists,
	}
	instance := &searchv1alpha1.Search{
		Spec: searchv1alpha1.SearchSpec{
			ImagePullPolicy: "IfNotPresent",
			ImagePullSecret: "personal-pull-secret",
			NodeSelector:    map[string]string{"key1": "val1"},
			Tolerations:     []corev1.Toleration{tol},
			Deployments: searchv1alpha1.SearchDeployments{
				Indexer: searchv1alpha1.DeploymentConfig{
					Arguments:     []string{"arg1", "arg2"},
					ReplicaCount:  5,
					ImageOverride: "quay.io/test-image:007",
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"cpu": resource.MustParse("50m"),
						},
						Requests: corev1.ResourceList{
							"cpu":    resource.MustParse("25m"),
							"memory": resource.MustParse("10Mi"),
						},
					},
				},
			},
		},
	}

	actualResourceRequirements := getResourceRequirements(testFor, instance)
	if actualResourceRequirements.Limits.Cpu().String() != "50m" {
		t.Error("Limit CPU Not expected")
	}
	if actualResourceRequirements.Limits.Memory().String() != "0" {
		t.Error("Limit Memory Not expected")
	}
}

func TestPGDeployment(t *testing.T) {
	var expectedMap = map[string]string{"WORK_MEM": "32MB"}

	var configValueMap = map[string]string{"POSTGRESQL_SHARED_BUFFERS": "64MB",
		"WORK_MEM": "25MB", //this value is trumped by the envVar
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
					Env: []corev1.EnvVar{
						{Name: "WORK_MEM", Value: "32MB"},
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
	//create configmap which has custom values for postgres DB
	customConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "searchcustomization"},
		Data:       configValueMap,
	}

	objs := []runtime.Object{search, customConfigMap}
	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()

	r := &SearchReconciler{Client: cl, Scheme: s}
	actualDep := r.PGDeployment(search)

	// Validate Env variables
	for _, env := range actualDep.Spec.Template.Spec.Containers[0].Env {
		if env.Value != expectedMap[env.Name] {
			t.Errorf("Expected %s for %s, but got %s", expectedMap[env.Name], env.Name, env.Value)
		}
	}

}

// Tests for createOrUpdateConfigMap function
func TestCreateOrUpdateConfigMap_CreateNew(t *testing.T) {
	// Test case: ConfigMap doesn't exist, should create it
	search := &searchv1alpha1.Search{
		TypeMeta:   metav1.TypeMeta{Kind: "Search"},
		ObjectMeta: metav1.ObjectMeta{Name: "search-v2-operator", Namespace: "test-namespace"},
		Spec:       searchv1alpha1.SearchSpec{},
	}

	s := scheme.Scheme
	err := searchv1alpha1.SchemeBuilder.AddToScheme(s)
	assert.NoError(t, err)

	objs := []runtime.Object{search}
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()
	r := &SearchReconciler{Client: cl, Scheme: s}

	// Create a new ConfigMap
	newConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-configmap",
			Namespace: "test-namespace",
		},
		Data: map[string]string{
			"key1": "value1",
			"key2": "value2",
		},
	}

	// Call createOrUpdateConfigMap
	result, err := r.createOrUpdateConfigMap(context.Background(), newConfigMap)

	// Verify no error occurred
	assert.Nil(t, result)
	assert.Nil(t, err)

	// Verify ConfigMap was created
	created := &corev1.ConfigMap{}
	err = cl.Get(context.Background(), types.NamespacedName{
		Name:      "test-configmap",
		Namespace: "test-namespace",
	}, created)
	assert.Nil(t, err)

	// Verify data matches
	assert.Equal(t, "value1", created.Data["key1"])
	assert.Equal(t, "value2", created.Data["key2"])
}

func TestCreateOrUpdateConfigMap_UpdateExisting(t *testing.T) {
	// Test case: Regular ConfigMap exists and needs update
	search := &searchv1alpha1.Search{
		TypeMeta:   metav1.TypeMeta{Kind: "Search"},
		ObjectMeta: metav1.ObjectMeta{Name: "search-v2-operator", Namespace: "test-namespace"},
		Spec:       searchv1alpha1.SearchSpec{},
	}

	// Create existing ConfigMap
	existingConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-configmap",
			Namespace: "test-namespace",
		},
		Data: map[string]string{
			"key1": "old-value1",
			"key2": "old-value2",
		},
	}

	s := scheme.Scheme
	err := searchv1alpha1.SchemeBuilder.AddToScheme(s)

	assert.NoError(t, err)

	objs := []runtime.Object{search, existingConfigMap}
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()
	r := &SearchReconciler{Client: cl, Scheme: s}

	// Create updated ConfigMap
	updatedConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-configmap",
			Namespace: "test-namespace",
		},
		Data: map[string]string{
			"key1": "new-value1",
			"key2": "new-value2",
			"key3": "new-value3",
		},
	}

	// Call createOrUpdateConfigMap
	result, err := r.createOrUpdateConfigMap(context.Background(), updatedConfigMap)

	// Verify no error occurred
	assert.Nil(t, result)
	assert.Nil(t, err)

	// Verify ConfigMap was updated
	updated := &corev1.ConfigMap{}
	err = cl.Get(context.Background(), types.NamespacedName{
		Name:      "test-configmap",
		Namespace: "test-namespace",
	}, updated)
	assert.Nil(t, err)

	// Verify data was updated
	assert.Equal(t, "new-value1", updated.Data["key1"])
	assert.Equal(t, "new-value2", updated.Data["key2"])
	assert.Equal(t, "new-value3", updated.Data["key3"])
}

func TestCreateOrUpdateConfigMap_PostgresSpecialCase(t *testing.T) {
	// Test case: Postgres ConfigMap with custom configuration
	search := &searchv1alpha1.Search{
		TypeMeta:   metav1.TypeMeta{Kind: "Search"},
		ObjectMeta: metav1.ObjectMeta{Name: "search-v2-operator", Namespace: "test-namespace"},
		Spec:       searchv1alpha1.SearchSpec{},
	}

	// Create existing postgres ConfigMap with custom config
	existingConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      postgresConfigmapName, // "search-postgres"
			Namespace: "test-namespace",
		},
		Data: map[string]string{
			"postgresql.conf":        "ssl = 'on'\nssl_cert_file = '/sslcert/tls.crt'",
			"custom-postgresql.conf": "# My custom settings\nmax_connections = 200",
		},
	}

	s := scheme.Scheme
	err := searchv1alpha1.SchemeBuilder.AddToScheme(s)

	assert.NoError(t, err)

	objs := []runtime.Object{search, existingConfigMap}
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()
	r := &SearchReconciler{Client: cl, Scheme: s}

	// Create new postgres ConfigMap (simulating operator update)
	newConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      postgresConfigmapName,
			Namespace: "test-namespace",
		},
		Data: map[string]string{
			"postgresql.conf":        "ssl = 'on'\nssl_cert_file = '/sslcert/tls.crt'\nstatement_timeout = '60000'",
			"custom-postgresql.conf": "# Customizations appended to postgresql.conf.\n",
		},
	}

	// Call createOrUpdateConfigMap
	result, err := r.createOrUpdateConfigMap(context.Background(), newConfigMap)

	// Verify no error occurred
	assert.Nil(t, result)
	assert.Nil(t, err)

	// Verify ConfigMap was updated
	updated := &corev1.ConfigMap{}
	err = cl.Get(context.Background(), types.NamespacedName{
		Name:      postgresConfigmapName,
		Namespace: "test-namespace",
	}, updated)

	assert.Nil(t, err)

	// Verify custom config was preserved
	expectedCustomConf := "# My custom settings\nmax_connections = 200"
	assert.Equal(t, expectedCustomConf, updated.Data["custom-postgresql.conf"])

	// Verify postgresql.conf was merged with custom config
	expectedConf := "ssl = 'on'\nssl_cert_file = '/sslcert/tls.crt'\nstatement_timeout = '60000'\n# My custom settings\nmax_connections = 200"
	assert.Equal(t, expectedConf, updated.Data["postgresql.conf"])
}

func TestCreateOrUpdateConfigMap_NoUpdateNeeded(t *testing.T) {
	// Test case: ConfigMap exists and is already up to date
	search := &searchv1alpha1.Search{
		TypeMeta:   metav1.TypeMeta{Kind: "Search"},
		ObjectMeta: metav1.ObjectMeta{Name: "search-v2-operator", Namespace: "test-namespace"},
		Spec:       searchv1alpha1.SearchSpec{},
	}

	// Create existing ConfigMap that matches what we'll try to update
	existingConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-configmap",
			Namespace: "test-namespace",
		},
		Data: map[string]string{
			"key1": "value1",
			"key2": "value2",
		},
	}

	s := scheme.Scheme
	err := searchv1alpha1.SchemeBuilder.AddToScheme(s)

	assert.NoError(t, err)

	objs := []runtime.Object{search, existingConfigMap}
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()
	r := &SearchReconciler{Client: cl, Scheme: s}

	// Create identical ConfigMap (no changes needed)
	sameConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-configmap",
			Namespace: "test-namespace",
		},
		Data: map[string]string{
			"key1": "value1",
			"key2": "value2",
		},
	}

	// Call createOrUpdateConfigMap
	result, err := r.createOrUpdateConfigMap(context.Background(), sameConfigMap)

	// Verify no error occurred
	assert.Nil(t, result)
	assert.Nil(t, err)

	// Verify ConfigMap still exists and unchanged
	found := &corev1.ConfigMap{}
	err = cl.Get(context.Background(), types.NamespacedName{
		Name:      "test-configmap",
		Namespace: "test-namespace",
	}, found)

	assert.NoError(t, err)

	// Verify data remained the same
	assert.Equal(t, "value1", found.Data["key1"])
	assert.Equal(t, "value2", found.Data["key2"])
}

func TestCreateOrUpdateConfigMap_PostgresNoCustomConfig(t *testing.T) {
	// Test case: Postgres ConfigMap without custom configuration
	search := &searchv1alpha1.Search{
		TypeMeta:   metav1.TypeMeta{Kind: "Search"},
		ObjectMeta: metav1.ObjectMeta{Name: "search-v2-operator", Namespace: "test-namespace"},
		Spec:       searchv1alpha1.SearchSpec{},
	}

	// Create existing postgres ConfigMap without custom config
	existingConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      postgresConfigmapName,
			Namespace: "test-namespace",
		},
		Data: map[string]string{
			"postgresql.conf":        "ssl = 'on'\nssl_cert_file = '/sslcert/tls.crt'",
			"custom-postgresql.conf": "",
		},
	}

	s := scheme.Scheme
	err := searchv1alpha1.SchemeBuilder.AddToScheme(s)
	assert.NoError(t, err)

	objs := []runtime.Object{search, existingConfigMap}
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()
	r := &SearchReconciler{Client: cl, Scheme: s}

	// Create new postgres ConfigMap
	newConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      postgresConfigmapName,
			Namespace: "test-namespace",
		},
		Data: map[string]string{
			"postgresql.conf":        "ssl = 'on'\nssl_cert_file = '/sslcert/tls.crt'\nstatement_timeout = '60000'",
			"custom-postgresql.conf": "# Customizations appended to postgresql.conf",
		},
	}

	// Call createOrUpdateConfigMap
	result, err := r.createOrUpdateConfigMap(context.Background(), newConfigMap)

	// Verify no error occurred
	assert.Nil(t, result)
	assert.Nil(t, err)

	// Verify ConfigMap was updated
	updated := &corev1.ConfigMap{}
	err = cl.Get(context.Background(), types.NamespacedName{
		Name:      postgresConfigmapName,
		Namespace: "test-namespace",
	}, updated)
	assert.NoError(t, err)

	// Verify postgresql.conf was updated (no custom config to merge)
	expectedConf := "ssl = 'on'\nssl_cert_file = '/sslcert/tls.crt'\nstatement_timeout = '60000'"
	assert.Equal(t, expectedConf, updated.Data["postgresql.conf"])

	// Verify custom-postgresql.conf was set to new value (UpdatePostgresConfigmap preserves new value when existing is empty)
	expectedCustomConf := "# Customizations appended to postgresql.conf"
	assert.Equal(t, expectedCustomConf, updated.Data["custom-postgresql.conf"])
}
