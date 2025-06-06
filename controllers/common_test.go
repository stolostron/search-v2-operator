// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"
	"os"
	"reflect"
	"testing"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
	var expectedMap = map[string]string{"POSTGRESQL_SHARED_BUFFERS": "64MB",
		"POSTGRESQL_EFFECTIVE_CACHE_SIZE": default_POSTGRESQL_EFFECTIVE_CACHE_SIZE,
		"WORK_MEM":                        "32MB"}

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

	// Validate the shared memory volume.
	var sharedMemoryVolume corev1.Volume
	for _, vol := range actualDep.Spec.Template.Spec.Volumes {
		if vol.Name == "dshm" {
			sharedMemoryVolume = vol
			break
		}
	}
	if sharedMemoryVolume.Name != "dshm" {
		t.Errorf("Expected shared volume dshm to be present, but got: %+v ", sharedMemoryVolume)
	}
	if !sharedMemoryVolume.VolumeSource.EmptyDir.SizeLimit.Equal(resource.MustParse("1Gi")) {
		t.Errorf("Expected shared volume SizeLimit to be 1Gi, but got: %+v ", sharedMemoryVolume.VolumeSource.EmptyDir.SizeLimit)
	}
}
