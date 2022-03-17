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
				API: searchv1alpha1.DeploymentConfig{
					ReplicaCount: 1,
				},
			},
		},
	}
	deploymentConfig := getDeploymentConfig("search-api", instance)
	if deploymentConfig.DeepCopy() == nil {
		t.Errorf("DeploymentConfig returned unexpectd nil")
	}
	actualCustomized := isDeploymentCustomized("search-api", instance)
	if !actualCustomized {
		t.Errorf("isDeploymentCustomized returned incorrect status")
	}
}
func TestResourcesCustomized(t *testing.T) {
	instance := &searchv1alpha1.Search{
		Spec: searchv1alpha1.SearchSpec{
			Deployments: searchv1alpha1.SearchDeployments{
				API: searchv1alpha1.DeploymentConfig{
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
		t.Errorf("API is not customized")
	}
}
func TestResourcesNotCustomized(t *testing.T) {
	instance := &searchv1alpha1.Search{
		Spec: searchv1alpha1.SearchSpec{
			Deployments: searchv1alpha1.SearchDeployments{
				API: searchv1alpha1.DeploymentConfig{
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
		t.Errorf("Collector is customized")
	}

	actualNodelSelector := getNodeSelector("search-collector", instance)
	if actualNodelSelector != nil {
		t.Errorf("NodeSelector Not expected")
	}
	actualImagePullPolicy := getImagePullPolicy("search-collector", instance)
	if actualImagePullPolicy != "Always" {
		t.Errorf("ImagePullPolicy Not expected")
	}
	actualImagePullSecret := getImagePullSecret("search-collector", instance)
	if actualImagePullSecret[0].Name != "search-pull-secret" {
		t.Errorf("ImagePullSecret Not expected")
	}
	actualImageSha := getImageSha("search-collector", instance)
	if actualImageSha != "value-from-env" {
		t.Errorf("ImageOverride with incorrect image")
	}
}
func TestAPICustomization(t *testing.T) {
	testFor := "search-api"
	instance := &searchv1alpha1.Search{
		Spec: searchv1alpha1.SearchSpec{
			Deployments: searchv1alpha1.SearchDeployments{
				API: searchv1alpha1.DeploymentConfig{
					ReplicaCount:    5,
					ImagePullPolicy: "IfNotPresent",
					ImagePullSecret: "personal-pull-secret",
					ImageOverride:   "quay.io/test-image:007",
					NodeSelector:    map[string]string{"key1": "val1"},
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
		t.Errorf("Incorrect NodeSelector")
	}
	actualImagePullPolicy := getImagePullPolicy(testFor, instance)
	if actualImagePullPolicy != "IfNotPresent" {
		t.Errorf("ImagePullPolicy Not expected")
	}
	actualReplicaCount := getReplicaCount(testFor, instance)
	if *actualReplicaCount != int32(5) {
		t.Errorf("ReplicaCount Not expected")
	}
	actualImagePullSecret := getImagePullSecret(testFor, instance)
	if actualImagePullSecret[0].Name != "personal-pull-secret" {
		t.Errorf("ImagePullSecret Not expected")
	}
	request_memory_want := "10Mi"
	request_cpu_want := "25m"
	limit_cpu_want := "40m"
	limit_memory_want := "25Mi"
	actualResourceRequirements := getResourceRequirements("search-api", instance)
	if actualResourceRequirements.Requests.Memory().String() != request_memory_want {
		t.Errorf("Request Memory Not expected")
	}
	if actualResourceRequirements.Requests.Cpu().String() != request_cpu_want {
		t.Errorf("Request Memory Not expected")
	}
	if actualResourceRequirements.Limits.Memory().String() != limit_memory_want {
		t.Errorf("Limit Memory Not expected")
	}
	if actualResourceRequirements.Limits.Cpu().String() != limit_cpu_want {
		t.Errorf("Limit CPU Not expected")
	}
	actual_image_sha := getImageSha(testFor, instance)
	if actual_image_sha != "quay.io/test-image:007" {
		t.Errorf("ImageOverride with incorrect image")
	}

}

func TestIndexerCustomization(t *testing.T) {
	testFor := "search-indexer"
	instance := &searchv1alpha1.Search{
		Spec: searchv1alpha1.SearchSpec{
			Deployments: searchv1alpha1.SearchDeployments{
				Indexer: searchv1alpha1.DeploymentConfig{
					ReplicaCount:    5,
					ImagePullPolicy: "IfNotPresent",
					ImagePullSecret: "personal-pull-secret",
					ImageOverride:   "quay.io/test-image:007",
					NodeSelector:    map[string]string{"key1": "val1"},
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
		t.Errorf("Incorrect NodeSelector")
	}
	actualImagePullPolicy := getImagePullPolicy(testFor, instance)
	if actualImagePullPolicy != "IfNotPresent" {
		t.Errorf("ImagePullPolicy Not expected")
	}
	actualReplicaCount := getReplicaCount(testFor, instance)
	if *actualReplicaCount != int32(5) {
		t.Errorf("ReplicaCount Not expected")
	}
	actualImagePullSecret := getImagePullSecret(testFor, instance)
	if actualImagePullSecret[0].Name != "personal-pull-secret" {
		t.Errorf("ImagePullSecret Not expected")
	}
	request_memory_want := "10Mi"
	request_cpu_want := "25m"
	limit_cpu_want := "40m"
	limit_memory_want := "25Mi"
	actualResourceRequirements := getResourceRequirements(testFor, instance)
	if actualResourceRequirements.Requests.Memory().String() != request_memory_want {
		t.Errorf("Request Memory Not expected")
	}
	if actualResourceRequirements.Requests.Cpu().String() != request_cpu_want {
		t.Errorf("Request Memory Not expected")
	}
	if actualResourceRequirements.Limits.Memory().String() != limit_memory_want {
		t.Errorf("Limit Memory Not expected")
	}
	if actualResourceRequirements.Limits.Cpu().String() != limit_cpu_want {
		t.Errorf("Limit CPU Not expected")
	}
	actual_image_sha := getImageSha(testFor, instance)
	if actual_image_sha != "quay.io/test-image:007" {
		t.Errorf("ImageOverride with incorrect image")
	}

}
func TestCollectorCustomization(t *testing.T) {
	testFor := "search-collector"
	instance := &searchv1alpha1.Search{
		Spec: searchv1alpha1.SearchSpec{
			Deployments: searchv1alpha1.SearchDeployments{
				Collector: searchv1alpha1.DeploymentConfig{
					ReplicaCount:    5,
					ImagePullPolicy: "IfNotPresent",
					ImagePullSecret: "personal-pull-secret",
					ImageOverride:   "quay.io/test-image:007",
					NodeSelector:    map[string]string{"key1": "val1"},
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
		t.Errorf("Incorrect NodeSelector")
	}
	actualImagePullPolicy := getImagePullPolicy(testFor, instance)
	if actualImagePullPolicy != "IfNotPresent" {
		t.Errorf("ImagePullPolicy Not expected")
	}
	actualReplicaCount := getReplicaCount(testFor, instance)
	if *actualReplicaCount != int32(5) {
		t.Errorf("ReplicaCount Not expected")
	}
	actualImagePullSecret := getImagePullSecret(testFor, instance)
	if actualImagePullSecret[0].Name != "personal-pull-secret" {
		t.Errorf("ImagePullSecret Not expected")
	}
	request_memory_want := "10Mi"
	request_cpu_want := "25m"
	limit_cpu_want := "40m"
	limit_memory_want := "25Mi"
	actualResourceRequirements := getResourceRequirements(testFor, instance)
	if actualResourceRequirements.Requests.Memory().String() != request_memory_want {
		t.Errorf("Request Memory Not expected")
	}
	if actualResourceRequirements.Requests.Cpu().String() != request_cpu_want {
		t.Errorf("Request Memory Not expected")
	}
	if actualResourceRequirements.Limits.Memory().String() != limit_memory_want {
		t.Errorf("Limit Memory Not expected")
	}
	if actualResourceRequirements.Limits.Cpu().String() != limit_cpu_want {
		t.Errorf("Limit CPU Not expected")
	}
	actual_image_sha := getImageSha(testFor, instance)
	if actual_image_sha != "quay.io/test-image:007" {
		t.Errorf("ImageOverride with incorrect image")
	}

}

func TestPostgresCustomization(t *testing.T) {
	testFor := "search-postgres"
	instance := &searchv1alpha1.Search{
		Spec: searchv1alpha1.SearchSpec{
			Deployments: searchv1alpha1.SearchDeployments{
				Database: searchv1alpha1.DeploymentConfig{
					ReplicaCount:    5,
					ImagePullPolicy: "IfNotPresent",
					ImagePullSecret: "personal-pull-secret",
					ImageOverride:   "quay.io/test-image:007",
					NodeSelector:    map[string]string{"key1": "val1"},
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
		t.Errorf("Incorrect NodeSelector")
	}
	actualImagePullPolicy := getImagePullPolicy(testFor, instance)
	if actualImagePullPolicy != "IfNotPresent" {
		t.Errorf("ImagePullPolicy Not expected")
	}
	actualReplicaCount := getReplicaCount(testFor, instance)
	if *actualReplicaCount != int32(5) {
		t.Errorf("ReplicaCount Not expected")
	}
	actualImagePullSecret := getImagePullSecret(testFor, instance)
	if actualImagePullSecret[0].Name != "personal-pull-secret" {
		t.Errorf("ImagePullSecret Not expected")
	}
	request_memory_want := "10Mi"
	request_cpu_want := "25m"
	limit_cpu_want := "40m"
	limit_memory_want := "25Mi"
	actualResourceRequirements := getResourceRequirements(testFor, instance)
	if actualResourceRequirements.Requests.Memory().String() != request_memory_want {
		t.Errorf("Request Memory Not expected")
	}
	if actualResourceRequirements.Requests.Cpu().String() != request_cpu_want {
		t.Errorf("Request Memory Not expected")
	}
	if actualResourceRequirements.Limits.Memory().String() != limit_memory_want {
		t.Errorf("Limit Memory Not expected")
	}
	if actualResourceRequirements.Limits.Cpu().String() != limit_cpu_want {
		t.Errorf("Limit CPU Not expected")
	}
	actual_image_sha := getImageSha(testFor, instance)
	if actual_image_sha != "quay.io/test-image:007" {
		t.Errorf("ImageOverride with incorrect image")
	}

}
