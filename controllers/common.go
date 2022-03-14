// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"os"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	apiDeploymentName       = "search-api"
	collectorDeploymentName = "search-collector"
	indexerDeploymentName   = "search-indexer"
	postgresDeploymentName  = "search-postgres"
)

func generateLabels(key, val string) map[string]string {
	allLabels := map[string]string{
		"component": "search-v2-operator",
	}
	allLabels[key] = val
	return allLabels
}

func getServiceAccountName() string {
	return "search-serviceaccount"
}

func getImagePullSecretName() string {
	return "search-pull-secret"
}

func getImagePullPolicy(deploymentName string, instance *searchv1alpha1.Search) corev1.PullPolicy {
	switch deploymentName {
	case apiDeploymentName:
		if instance.Spec.Deployments.API.ImagePullPolicy != "" {
			return instance.Spec.Deployments.API.ImagePullPolicy
		}
	case collectorDeploymentName:
		if instance.Spec.Deployments.Collector.ImagePullPolicy != "" {
			return instance.Spec.Deployments.Collector.ImagePullPolicy
		}
	case indexerDeploymentName:
		if instance.Spec.Deployments.Indexer.ImagePullPolicy != "" {
			return instance.Spec.Deployments.Indexer.ImagePullPolicy
		}
	case postgresDeploymentName:
		if instance.Spec.Deployments.Database.ImagePullPolicy != "" {
			return instance.Spec.Deployments.Database.ImagePullPolicy
		}
	}
	// Dev preview option
	return corev1.PullAlways
}

func getImagePullSecret(deploymentName string, instance *searchv1alpha1.Search) []corev1.LocalObjectReference {
	result := []corev1.LocalObjectReference{}
	switch deploymentName {
	case apiDeploymentName:
		if instance.Spec.Deployments.API.ImagePullSecret != "" {
			return append(result, corev1.LocalObjectReference{Name: instance.Spec.Deployments.API.ImagePullSecret})
		}
	case collectorDeploymentName:
		if instance.Spec.Deployments.Collector.ImagePullSecret != "" {
			return append(result, corev1.LocalObjectReference{Name: instance.Spec.Deployments.Collector.ImagePullSecret})
		}
	case indexerDeploymentName:
		if instance.Spec.Deployments.Indexer.ImagePullSecret != "" {
			return append(result, corev1.LocalObjectReference{Name: instance.Spec.Deployments.Indexer.ImagePullSecret})
		}
	case postgresDeploymentName:
		if instance.Spec.Deployments.Database.ImagePullSecret != "" {
			return append(result, corev1.LocalObjectReference{Name: instance.Spec.Deployments.Database.ImagePullSecret})
		}
	}
	default_pull_secret := getImagePullSecretName()
	return append(result, corev1.LocalObjectReference{Name: default_pull_secret})
}
func getRoleName() string {
	return "search"
}
func getRoleBindingName() string {
	return "search"
}
func getDeployment(deploymentName string, instance *searchv1alpha1.Search) *appsv1.Deployment {
	deploymentLabels := generateLabels("name", deploymentName)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: instance.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: deploymentLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: deploymentLabels,
				},
			},
		},
	}
}
func getResourceRequirements(deploymentName string, instance *searchv1alpha1.Search) corev1.ResourceRequirements {
	switch deploymentName {
	case apiDeploymentName:
		return corev1.ResourceRequirements{
			Requests: getRequests(apiDeploymentName, instance),
			Limits:   getLimits(apiDeploymentName, instance),
		}
	case collectorDeploymentName:
		return corev1.ResourceRequirements{
			Requests: getRequests(collectorDeploymentName, instance),
			Limits:   getLimits(collectorDeploymentName, instance),
		}
	case indexerDeploymentName:
		return corev1.ResourceRequirements{
			Requests: getRequests(indexerDeploymentName, instance),
			Limits:   getLimits(indexerDeploymentName, instance),
		}
	case postgresDeploymentName:
		return corev1.ResourceRequirements{
			Requests: getRequests(postgresDeploymentName, instance),
			Limits:   getLimits(postgresDeploymentName, instance),
		}
	}
	log.V(2).Info("Unknown deployment %s ", deploymentName)
	return corev1.ResourceRequirements{}
}

func getRequests(deployment string, instance *searchv1alpha1.Search) corev1.ResourceList {
	var cpu, memory resource.Quantity
	switch deployment {
	case apiDeploymentName:
		cpu = resource.MustParse(resoureMap[apiDeploymentName]["CPURequest"])
		if instance.Spec.Deployments.API.Resources.Requests.Cpu() != nil {
			cpu = *instance.Spec.Deployments.API.Resources.Requests.Cpu()
		}
		memory = resource.MustParse(resoureMap[apiDeploymentName]["MemoryRequest"])
		if instance.Spec.Deployments.API.Resources.Requests.Memory() != nil {
			memory = *instance.Spec.Deployments.API.Resources.Requests.Memory()
		}
	case collectorDeploymentName:
		cpu = resource.MustParse(resoureMap[collectorDeploymentName]["CPURequest"])
		if instance.Spec.Deployments.Collector.Resources.Requests.Cpu() != nil {
			cpu = *instance.Spec.Deployments.Collector.Resources.Requests.Cpu()
		}
		memory = resource.MustParse(resoureMap[collectorDeploymentName]["MemoryRequest"])
		if instance.Spec.Deployments.Collector.Resources.Requests.Memory() != nil {
			memory = *instance.Spec.Deployments.Collector.Resources.Requests.Memory()
		}
	case indexerDeploymentName:
		cpu = resource.MustParse(resoureMap[indexerDeploymentName]["CPURequest"])
		if instance.Spec.Deployments.Indexer.Resources.Requests.Cpu() != nil {
			cpu = *instance.Spec.Deployments.Indexer.Resources.Requests.Cpu()
		}
		memory = resource.MustParse(resoureMap[indexerDeploymentName]["MemoryRequest"])
		if instance.Spec.Deployments.Indexer.Resources.Requests.Memory() != nil {
			memory = *instance.Spec.Deployments.Indexer.Resources.Requests.Memory()
		}

	case postgresDeploymentName:
		cpu = resource.MustParse(resoureMap[postgresDeploymentName]["CPURequest"])
		if instance.Spec.Deployments.Database.Resources.Requests.Cpu() != nil {
			cpu = *instance.Spec.Deployments.Database.Resources.Requests.Cpu()
		}
		memory = resource.MustParse(resoureMap[postgresDeploymentName]["MemoryRequest"])
		if instance.Spec.Deployments.Database.Resources.Requests.Memory() != nil {
			memory = *instance.Spec.Deployments.Database.Resources.Requests.Memory()
		}
	}

	return corev1.ResourceList{
		corev1.ResourceCPU:    cpu,
		corev1.ResourceMemory: memory,
	}
}

func getLimits(deployment string, instance *searchv1alpha1.Search) corev1.ResourceList {
	var cpu, memory resource.Quantity
	switch deployment {
	case apiDeploymentName:
		if instance.Spec.Deployments.API.Resources.Requests.Cpu() != nil {
			cpu = *instance.Spec.Deployments.API.Resources.Requests.Cpu()
		}
		memory = resource.MustParse(resoureMap[apiDeploymentName]["MemoryLimit"])
		if instance.Spec.Deployments.API.Resources.Requests.Memory() != nil {
			memory = *instance.Spec.Deployments.API.Resources.Requests.Memory()
		}
	case collectorDeploymentName:
		if instance.Spec.Deployments.Collector.Resources.Requests.Cpu() != nil {
			cpu = *instance.Spec.Deployments.Collector.Resources.Requests.Cpu()
		}
		memory = resource.MustParse(resoureMap[collectorDeploymentName]["MemoryLimit"])
		if instance.Spec.Deployments.Collector.Resources.Requests.Memory() != nil {
			memory = *instance.Spec.Deployments.Collector.Resources.Requests.Memory()
		}
	case indexerDeploymentName:
		if instance.Spec.Deployments.Indexer.Resources.Requests.Cpu() != nil {
			cpu = *instance.Spec.Deployments.Indexer.Resources.Requests.Cpu()
		}
		memory = resource.MustParse(resoureMap[indexerDeploymentName]["MemoryLimit"])
		if instance.Spec.Deployments.Indexer.Resources.Requests.Memory() != nil {
			memory = *instance.Spec.Deployments.Indexer.Resources.Requests.Memory()
		}

	case postgresDeploymentName:
		if instance.Spec.Deployments.Database.Resources.Requests.Cpu() != nil {
			cpu = *instance.Spec.Deployments.Database.Resources.Requests.Cpu()
		}
		memory = resource.MustParse(resoureMap[postgresDeploymentName]["MemoryLimit"])
		if instance.Spec.Deployments.Database.Resources.Requests.Memory() != nil {
			memory = *instance.Spec.Deployments.Database.Resources.Requests.Memory()
		}
	}
	if cpu.String() == "<nil>" {
		return corev1.ResourceList{
			corev1.ResourceMemory: memory,
		}
	}
	return corev1.ResourceList{
		corev1.ResourceCPU:    cpu,
		corev1.ResourceMemory: memory,
	}
}

func getReplicaCount(deploymentName string, instance *searchv1alpha1.Search) *int32 {
	count := int32(1)
	switch deploymentName {
	case apiDeploymentName:
		if instance.Spec.Deployments.API.ReplicaCount != 0 {
			count = instance.Spec.Deployments.API.ReplicaCount
		}
		return &count
	case collectorDeploymentName:
		return &count
	case indexerDeploymentName:
		return &count
	case postgresDeploymentName:
		return &count
	}
	log.V(2).Info("Unknown deployment %s ", deploymentName)
	return &count

}
func getImageSha(deploymentName string, instance *searchv1alpha1.Search) string {
	switch deploymentName {
	case apiDeploymentName:
		if instance.Spec.Deployments.API.ImageOverride != "" {
			return instance.Spec.Deployments.API.ImageOverride
		}
		return os.Getenv("API_IMAGE")
	case collectorDeploymentName:
		if instance.Spec.Deployments.Collector.ImageOverride != "" {
			return instance.Spec.Deployments.Collector.ImageOverride
		}
		return os.Getenv("COLLECTOR_IMAGE")
	case indexerDeploymentName:
		if instance.Spec.Deployments.Indexer.ImageOverride != "" {
			return instance.Spec.Deployments.Indexer.ImageOverride
		}
		return os.Getenv("INDEXER_IMAGE")
	case postgresDeploymentName:
		if instance.Spec.Deployments.Database.ImageOverride != "" {
			return instance.Spec.Deployments.Database.ImageOverride
		}
		return os.Getenv("POSTGRES_IMAGE")
	}
	log.V(2).Info("Unknown deployment %s ", deploymentName)
	return ""
}
