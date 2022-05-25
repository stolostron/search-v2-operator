// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"
	"os"
	"strings"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

/* #nosec G101
gosec will flag the "secrets" as security violations. This flag will allow us to ignore it as a false positive.
*/
const (
	apiDeploymentName       = "search-api"
	collectorDeploymentName = "search-collector"
	indexerDeploymentName   = "search-indexer"
	postgresDeploymentName  = "search-postgres"

	indexerConfigmapName  = "search-indexer"
	postgresConfigmapName = "search-postgres"
	caCertConfigmapName   = "search-ca-crt"

	apiSecretName      = "search-api-certs"
	indexerSecretName  = "search-indexer-certs"
	postgresSecretName = "search-postgres-certs"
)

var (
	certDefaultMode       = int32(384)
	AnnotationSearchPause = "search-pause"
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

func getClusterManagementAddonName() string {
	return "search-collector"
}

func newMetadataEnvVar(name, key string) corev1.EnvVar {
	return corev1.EnvVar{
		Name: name,
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{
				FieldPath: key,
			},
		},
	}
}

func IsPaused(annotations map[string]string) bool {
	if annotations == nil {
		return false
	}
	if annotations[AnnotationSearchPause] != "" &&
		strings.EqualFold(annotations[AnnotationSearchPause], "true") {
		return true
	}
	return false
}

func getNodeSelector(deploymentName string, instance *searchv1alpha1.Search) map[string]string {
	if instance.Spec.NodeSelector != nil {
		return instance.Spec.NodeSelector
	}
	var result map[string]string
	return result
}

func getImagePullPolicy(deploymentName string, instance *searchv1alpha1.Search) corev1.PullPolicy {
	if instance.Spec.ImagePullPolicy != "" {
		return instance.Spec.ImagePullPolicy
	}
	// Dev preview option
	return corev1.PullAlways
}

func getPostgresVolume(instance *searchv1alpha1.Search) corev1.Volume {
	storageClass := instance.Spec.DBStorage.StorageClassName
	if storageClass != "" {
		pvcName := getPVCName(storageClass)
		return corev1.Volume{
			Name: "postgresdb",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: pvcName,
				},
			},
		}
	}
	return corev1.Volume{
		Name: "postgresdb",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}
}

func getContainerArgs(deploymentName string, instance *searchv1alpha1.Search) []string {
	var result []string
	deploymentConfig := getDeploymentConfig(deploymentName, instance)
	if deploymentConfig.Arguments != nil {
		return deploymentConfig.Arguments
	}
	return result
}

func getRoleName() string {
	return "search"
}
func getRoleBindingName() string {
	return "search"
}
func getPVCName(scName string) string {
	return scName + "-search"
}

func getAddonRoleName() string {
	return "open-cluster-management:addons:search-collector"
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
	cpu = resource.MustParse(defaultResoureMap[deployment]["CPURequest"])
	memory = resource.MustParse(defaultResoureMap[deployment]["MemoryRequest"])
	if !isResourcesCustomized(deployment, instance) {
		return corev1.ResourceList{
			corev1.ResourceCPU:    cpu,
			corev1.ResourceMemory: memory,
		}
	}
	deploymentConfig := getDeploymentConfig(deployment, instance)
	if deploymentConfig.Resources.Requests.Cpu() != nil {
		cpu = *deploymentConfig.Resources.Requests.Cpu()
	}
	if deploymentConfig.Resources.Requests.Memory() != nil {
		memory = *deploymentConfig.Resources.Requests.Memory()
	}

	return corev1.ResourceList{
		corev1.ResourceCPU:    cpu,
		corev1.ResourceMemory: memory,
	}
}

func getLimits(deployment string, instance *searchv1alpha1.Search) corev1.ResourceList {
	var cpu, memory resource.Quantity
	memory = resource.MustParse(defaultResoureMap[deployment]["MemoryLimit"])
	if !isResourcesCustomized(deployment, instance) {
		return corev1.ResourceList{
			corev1.ResourceMemory: memory,
		}
	}
	deploymentConfig := getDeploymentConfig(deployment, instance)

	if deploymentConfig.Resources.Limits.Cpu() != nil {
		cpu = *deploymentConfig.Resources.Limits.Cpu()
	}
	if deploymentConfig.Resources.Limits.Memory() != nil {
		memory = *deploymentConfig.Resources.Limits.Memory()
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
	if c, ok := defaultReplicaMap[deploymentName]; ok {
		count = c
	}
	deploymentConfig := getDeploymentConfig(deploymentName, instance)
	if deploymentConfig.ReplicaCount != 0 {
		return &deploymentConfig.ReplicaCount
	}
	return &count

}
func getImageSha(deploymentName string, instance *searchv1alpha1.Search) string {
	switch deploymentName {
	case apiDeploymentName:
		if instance.Spec.Deployments.QueryAPI.ImageOverride != "" {
			return instance.Spec.Deployments.QueryAPI.ImageOverride
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
	deploymentConfig := getDeploymentConfig(deploymentName, instance)
	return deploymentConfig.DeepCopy() != nil
}

func isResourcesCustomized(deploymentName string, instance *searchv1alpha1.Search) bool {
	if !isDeploymentCustomized(deploymentName, instance) {
		return false
	}
	deploymentConfig := getDeploymentConfig(deploymentName, instance)
	return deploymentConfig.Resources != nil
}

func (r *SearchReconciler) createConfigMap(ctx context.Context, cm *corev1.ConfigMap) (*reconcile.Result, error) {
	found := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      cm.Name,
		Namespace: cm.Namespace,
	}, found)
	if err != nil && errors.IsNotFound(err) {
		err = r.Create(ctx, cm)
		if err != nil {
			log.Error(err, "Could not create configmap")
			return &reconcile.Result{}, err
		}
	}
	log.V(2).Info("Created %s configmap ", cm.Name)
	return nil, nil
}

func getDeploymentConfig(name string, instance *searchv1alpha1.Search) searchv1alpha1.DeploymentConfig {
	var result searchv1alpha1.DeploymentConfig
	switch name {
	case apiDeploymentName:
		return instance.Spec.Deployments.QueryAPI
	case collectorDeploymentName:
		return instance.Spec.Deployments.Collector
	case indexerDeploymentName:
		return instance.Spec.Deployments.Indexer
	case postgresDeploymentName:
		return instance.Spec.Deployments.Database
	}
	return result
}

func (r *SearchReconciler) createOrUpdateDeployment(ctx context.Context, deploy *appsv1.Deployment) (*reconcile.Result, error) {
	found := &appsv1.Deployment{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      deploy.Name,
		Namespace: deploy.Namespace,
	}, found)
	if err != nil {
		if errors.IsNotFound(err) {
			err = r.Create(ctx, deploy)
			if err != nil {
				log.Error(err, "Could not create deployment")
				return &reconcile.Result{}, err
			}
			log.Info("Created  deployment " + deploy.Name)
			log.V(9).Info("Created deployment %+v", deploy)
			return nil, nil
		}
		log.Error(err, "Could not get deployment")
		return &reconcile.Result{}, err
	}
	if !DeploymentEquals(found, deploy) {
		if err := r.Update(ctx, deploy); err != nil {
			log.Error(err, "Could not update deployment")
			return nil, nil
		}
		log.V(9).Info("Updated deployment %+v", deploy)
	}
	return nil, nil
}

func (r *SearchReconciler) createService(ctx context.Context, svc *corev1.Service) (*reconcile.Result, error) {
	found := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      svc.Name,
		Namespace: svc.Namespace,
	}, found)
	if err != nil {
		if errors.IsNotFound(err) {
			err = r.Create(ctx, svc)
			if err != nil {
				log.Error(err, "Could not create service")
				return &reconcile.Result{}, err
			}
			log.Info("Created service " + svc.Name)
			log.V(9).Info("Created service %+v", svc)
			return nil, nil
		}
		log.Error(err, "Could not get service")
		return &reconcile.Result{}, err
	}
	return nil, nil
}

func (r *SearchReconciler) createSecret(ctx context.Context, secret *corev1.Secret) (*reconcile.Result, error) {
	found := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      secret.Name,
		Namespace: secret.Namespace,
	}, found)
	if err != nil {
		if errors.IsNotFound(err) {
			err = r.Create(ctx, secret)
			if err != nil {
				log.Error(err, "Could not create secret")
				return &reconcile.Result{}, err
			}
			log.Info("Created secret " + secret.Name)
			return nil, nil
		}
		log.Error(err, "Could not get secret")
		return &reconcile.Result{}, err
	}
	return nil, nil

}

func DeploymentEquals(current, new *appsv1.Deployment) bool {
	return equality.Semantic.DeepEqual(current.Spec, new.Spec)
}
