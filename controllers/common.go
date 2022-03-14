// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"
	"os"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
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

func getNodeSelector(deploymentName string, instance *searchv1alpha1.Search) map[string]string {
	var result map[string]string
	switch deploymentName {
	case apiDeploymentName:
		if instance.Spec.Deployments.API.NodeSelector != nil {
			return instance.Spec.Deployments.API.NodeSelector
		}
	case collectorDeploymentName:
		if instance.Spec.Deployments.Collector.NodeSelector != nil {
			return instance.Spec.Deployments.Collector.NodeSelector
		}
	case indexerDeploymentName:
		if instance.Spec.Deployments.Indexer.NodeSelector != nil {
			return instance.Spec.Deployments.Indexer.NodeSelector
		}
	case postgresDeploymentName:
		if instance.Spec.Deployments.Database.NodeSelector != nil {
			return instance.Spec.Deployments.Database.NodeSelector
		}
	}
	return result
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
	cpu = resource.MustParse(resoureMap[deployment]["CPURequest"])
	memory = resource.MustParse(resoureMap[deployment]["MemoryRequest"])
	if !isResourcesCustomized(deployment, instance) {
		return corev1.ResourceList{
			corev1.ResourceCPU:    cpu,
			corev1.ResourceMemory: memory,
		}
	}

	switch deployment {
	case apiDeploymentName:
		if instance.Spec.Deployments.API.Resources.Requests.Cpu() != nil {
			cpu = *instance.Spec.Deployments.API.Resources.Requests.Cpu()
		}
		if instance.Spec.Deployments.API.Resources.Requests.Memory() != nil {
			memory = *instance.Spec.Deployments.API.Resources.Requests.Memory()
		}
	case collectorDeploymentName:
		if instance.Spec.Deployments.Collector.Resources.Requests.Cpu() != nil {
			cpu = *instance.Spec.Deployments.Collector.Resources.Requests.Cpu()
		}
		if instance.Spec.Deployments.Collector.Resources.Requests.Memory() != nil {
			memory = *instance.Spec.Deployments.Collector.Resources.Requests.Memory()
		}
	case indexerDeploymentName:
		if instance.Spec.Deployments.Indexer.Resources.Requests.Cpu() != nil {
			cpu = *instance.Spec.Deployments.Indexer.Resources.Requests.Cpu()
		}
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
	memory = resource.MustParse(resoureMap[deployment]["MemoryLimit"])
	if !isResourcesCustomized(deployment, instance) {
		return corev1.ResourceList{
			corev1.ResourceMemory: memory,
		}
	}
	switch deployment {
	case apiDeploymentName:
		if instance.Spec.Deployments.API.Resources.Requests.Cpu() != nil {
			cpu = *instance.Spec.Deployments.API.Resources.Requests.Cpu()
		}
		if instance.Spec.Deployments.API.Resources.Requests.Memory() != nil {
			memory = *instance.Spec.Deployments.API.Resources.Requests.Memory()
		}
	case collectorDeploymentName:
		if instance.Spec.Deployments.Collector.Resources.Requests.Cpu() != nil {
			cpu = *instance.Spec.Deployments.Collector.Resources.Requests.Cpu()
		}
		if instance.Spec.Deployments.Collector.Resources.Requests.Memory() != nil {
			memory = *instance.Spec.Deployments.Collector.Resources.Requests.Memory()
		}
	case indexerDeploymentName:
		if instance.Spec.Deployments.Indexer.Resources.Requests.Cpu() != nil {
			cpu = *instance.Spec.Deployments.Indexer.Resources.Requests.Cpu()
		}
		if instance.Spec.Deployments.Indexer.Resources.Requests.Memory() != nil {
			memory = *instance.Spec.Deployments.Indexer.Resources.Requests.Memory()
		}

	case postgresDeploymentName:
		if instance.Spec.Deployments.Database.Resources.Requests.Cpu() != nil {
			cpu = *instance.Spec.Deployments.Database.Resources.Requests.Cpu()
		}
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

func hasDeployments(instance *searchv1alpha1.Search) bool {
	return instance.Spec.Deployments.DeepCopy() != nil

}

func isDeploymentCustomized(deploymentName string, instance *searchv1alpha1.Search) bool {
	if !hasDeployments(instance) {
		return false
	}
	switch deploymentName {
	case apiDeploymentName:
		if instance.Spec.Deployments.API.DeepCopy() != nil {
			return true
		}
	case collectorDeploymentName:
		if instance.Spec.Deployments.Collector.DeepCopy() != nil {
			return true
		}
	case indexerDeploymentName:
		if instance.Spec.Deployments.Indexer.DeepCopy() != nil {
			return true
		}
	case postgresDeploymentName:
		if instance.Spec.Deployments.Database.DeepCopy() != nil {
			return true
		}
	}
	return false
}

func isResourcesCustomized(deploymentName string, instance *searchv1alpha1.Search) bool {
	if !isDeploymentCustomized(deploymentName, instance) {
		return false
	}
	switch deploymentName {
	case apiDeploymentName:
		if instance.Spec.Deployments.API.Resources != nil {
			return true
		}
	case collectorDeploymentName:
		if instance.Spec.Deployments.Collector.Resources != nil {
			return true
		}
	case indexerDeploymentName:
		if instance.Spec.Deployments.Indexer.Resources != nil {
			return true
		}
	case postgresDeploymentName:
		if instance.Spec.Deployments.Database.Resources != nil {
			return true
		}
	}
	return false
}

func (r *SearchReconciler) createOrUpdateConfigMap(ctx context.Context, cm *corev1.ConfigMap) (*reconcile.Result, error) {
	found := &corev1.ConfigMap{}
	err := r.Get(context.TODO(), types.NamespacedName{
		Name:      cm.Name,
		Namespace: cm.Namespace,
	}, found)
	if err != nil && errors.IsNotFound(err) {
		err = r.Create(context.TODO(), cm)
		if err != nil {
			log.Error(err, "Could not create %s configmap", cm.Name)
			return &reconcile.Result{}, err
		}
	}
	if err := r.Update(context.TODO(), cm); err != nil {
		log.Error(err, "Could not update %s configmap", cm.Name)
		return &reconcile.Result{}, err
	}
	log.V(2).Info("Created %s configmap", cm.Name)
	return nil, nil
}

func (r *SearchReconciler) createOrUpdateDeployment(ctx context.Context, deploy *appsv1.Deployment) (*reconcile.Result, error) {
	found := &appsv1.Deployment{}
	err := r.Get(context.TODO(), types.NamespacedName{
		Name:      deploy.Name,
		Namespace: deploy.Namespace,
	}, found)
	if err != nil && errors.IsNotFound(err) {
		err = r.Create(context.TODO(), deploy)
		if err != nil {
			log.Error(err, "Could not create %s deployment", deploy.Name)
			return &reconcile.Result{}, err
		}
	}
	if err := r.Update(context.TODO(), deploy); err != nil {
		log.Error(err, "Could not update %s deployment", deploy.Name)
		return &reconcile.Result{}, err
	}
	log.V(2).Info("Created %s deployment", deploy.Name)
	return nil, nil
}

func (r *SearchReconciler) createOrUpdateService(ctx context.Context, svc *corev1.Service) (*reconcile.Result, error) {
	found := &corev1.Service{}
	err := r.Get(context.TODO(), types.NamespacedName{
		Name:      svc.Name,
		Namespace: svc.Namespace,
	}, found)
	if err != nil && errors.IsNotFound(err) {
		err = r.Create(context.TODO(), svc)
		if err != nil {
			log.Error(err, "Could not create %s service", svc.Name)
			return &reconcile.Result{}, err
		}
	}
	if err := r.Update(context.TODO(), svc); err != nil {
		log.Error(err, "Could not update %s service", svc.Name)
		return &reconcile.Result{}, err
	}
	log.V(2).Info("Created %s service", svc.Name)
	return nil, nil
}

func (r *SearchReconciler) createOrUpdateSecret(ctx context.Context, secret *corev1.Secret) (*reconcile.Result, error) {
	found := &corev1.Secret{}
	err := r.Get(context.TODO(), types.NamespacedName{
		Name:      secret.Name,
		Namespace: secret.Namespace,
	}, found)
	if err != nil && errors.IsNotFound(err) {
		err = r.Create(context.TODO(), secret)
		if err != nil {
			log.Error(err, "Could not create %s secret", secret.Name)
			return &reconcile.Result{}, err
		}
	}
	if err := r.Update(context.TODO(), secret); err != nil {
		log.Error(err, "Could not update %s secret", secret.Name)
		return &reconcile.Result{}, err
	}
	log.V(2).Info("Created %s secret", secret.Name)
	return nil, nil
}
