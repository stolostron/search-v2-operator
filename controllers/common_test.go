// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"os"
	"testing"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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
	actualCustomized := isDeploymentCustomized("search-api", instance)
	if !actualCustomized {
		t.Error("isDeploymentCustomized returned incorrect status")
	}
}
func TestResourcesCustomized(t *testing.T) {
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
	want := true
	if isResourcesCustomized("search-api", instance) != want {
		t.Error("QueryAPI is not customized")
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
	want := false
	if isResourcesCustomized("search-collector", instance) != want {
		t.Error("Collector is customized")
	}

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
