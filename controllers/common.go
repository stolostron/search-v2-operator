// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"
	"os"
	"regexp"
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

const (
	apiDeploymentName       = "search-api"
	collectorDeploymentName = "search-collector"
	indexerDeploymentName   = "search-indexer"
	postgresDeploymentName  = "search-postgres"

	indexerConfigmapName  = "search-indexer"
	postgresConfigmapName = "search-postgres"
	caCertConfigmapName   = "search-ca-crt"

	apiSecretName      = "search-api-certs"      // #nosec G101 - False positive, this is a secret name, not a password
	indexerSecretName  = "search-indexer-certs"  // #nosec G101 - False positive, this is a secret name, not a password
	postgresSecretName = "search-postgres-certs" // #nosec G101 - False positive, this is a secret name, not a password

	ResourceHugePages2Mi corev1.ResourceName = "hugepages-2Mi"
	ResourceHugePages1Gi corev1.ResourceName = "hugepages-1Gi"
)

var (
	certDefaultMode       = int32(416)
	AnnotationSearchPause = "search-pause"
	dbDefaultMap          = map[string]string{
		"POSTGRESQL_EFFECTIVE_CACHE_SIZE": default_POSTGRESQL_EFFECTIVE_CACHE_SIZE,
		"POSTGRESQL_SHARED_BUFFERS":       default_POSTGRESQL_SHARED_BUFFERS,
		"WORK_MEM":                        default_WORK_MEM,
	}
)

func generateLabels(key, val string) map[string]string {
	allLabels := map[string]string{
		"component": "search-v2-operator",
		"app":       "search",
	}
	allLabels[key] = val
	return allLabels
}

func getServiceAccountName() string {
	return "search-serviceaccount"
}

// getAPIServiceAccountName returns the dedicated ServiceAccount used only by
// the search-api deployment. Impersonation rights (users/groups/serviceaccounts)
// are bound to this SA alone so that the postgres, indexer and collector pods —
// which share the generic search-serviceaccount — cannot escalate to
// system:masters via their mounted token.
func getAPIServiceAccountName() string {
	return "search-api-sa"
}

// getPostgresServiceAccountName returns the dedicated ServiceAccount for the
// search-postgres deployment. Postgres is a self-contained database process that
// makes no Kubernetes API calls at runtime — it only reads its credentials and TLS
// certificates from volumes that the kubelet mounts. Binding it to its own SA with
// no ClusterRole grants it the minimum possible privileges (none) while keeping
// it fully isolated from the write permissions held by the other search components.
func getPostgresServiceAccountName() string {
	return "search-postgres-sa"
}

// getIndexerServiceAccountName returns the dedicated ServiceAccount for the
// search-indexer deployment. The indexer's verified API surface is:
//   - TokenReviews create    — authenticates incoming collector bearer tokens
//   - Leases get/create/update — leader election lock
//   - ManagedClusters, ManagedClusterInfos, ManagedClusterAddons get/list/watch — cluster-sync
//   - Discovery (ServerResourcesForGroupVersion) — CRD presence checks (covered by wildcard)
//
// It does not use impersonate, configmap/secret writes, or any deployment-level
// write verbs, so it is isolated from the shared search-serviceaccount.
func getIndexerServiceAccountName() string {
	return "search-indexer-sa"
}

// getCollectorServiceAccountName returns the dedicated ServiceAccount for the
// search-collector deployment. The collector only needs read access (get/list/watch)
// on all cluster resources plus lease management for its heartbeat. Keeping it
// separate from the shared search-serviceaccount ensures that a compromise of the
// collector pod cannot be used to exercise the write permissions needed by other
// components (postgres, indexer).
func getCollectorServiceAccountName() string {
	return "search-collector-sa"
}

func getDefaultDBConfig(varName string) string {
	value, okay := dbDefaultMap[varName]
	if okay {
		return value
	}
	return ""
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

func getTolerations(deploymentName string, instance *searchv1alpha1.Search) []corev1.Toleration {
	if instance.Spec.Tolerations != nil {
		return instance.Spec.Tolerations
	}
	return []corev1.Toleration{}
}

func getPodSecurityContext() *corev1.PodSecurityContext {
	trueVal := true
	return &corev1.PodSecurityContext{
		RunAsNonRoot: &trueVal,
	}
}

func getContainerSecurityContext() *corev1.SecurityContext {
	falseVal := false
	trueVal := true
	return &corev1.SecurityContext{
		Privileged:               &falseVal,
		AllowPrivilegeEscalation: &falseVal,
		ReadOnlyRootFilesystem:   &trueVal,
		RunAsNonRoot:             &trueVal,
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
	}
}

func getImagePullPolicy(deploymentName string, instance *searchv1alpha1.Search) corev1.PullPolicy {
	if instance.Spec.ImagePullPolicy != "" {
		return instance.Spec.ImagePullPolicy
	}
	return corev1.PullIfNotPresent
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

// operatorManagedSecrets is the set of secrets created by the operator.
// CR-supplied env vars with valueFrom.secretKeyRef are only allowed to
// reference secrets in this set, preventing CR editors from mounting
// arbitrary namespace secrets into search containers.
var operatorManagedSecrets = map[string]struct{}{
	"search-postgres": {},
}

func getContainerEnvVar(deploymentName string, instance *searchv1alpha1.Search) []corev1.EnvVar {
	var result []corev1.EnvVar
	deploymentConfig := getDeploymentConfig(deploymentName, instance)
	if deploymentConfig.Env == nil {
		return result
	}
	// For postgres, skip WORK_MEM — it is read and sanitized by GetDBConfigFromSearchCR,
	// which handles CR env, dbconfig ConfigMap, and default priority in one place.
	for _, e := range deploymentConfig.Env {
		if deploymentName == postgresDeploymentName && e.Name == "WORK_MEM" {
			continue
		}
		// Drop env vars that reference secrets the operator didn't create.
		if e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil {
			if _, ok := operatorManagedSecrets[e.ValueFrom.SecretKeyRef.Name]; !ok {
				log.Info("Dropping env var referencing non-operator secret",
					"name", e.Name, "secret", e.ValueFrom.SecretKeyRef.Name)
				continue
			}
		}
		result = append(result, e)
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

func getSearchUserRoleName() string {
	return "global-search-user"
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
	log.V(2).Info("Unknown deployment ", "name", deploymentName)
	return corev1.ResourceRequirements{}
}

func getRequests(deployment string, instance *searchv1alpha1.Search) corev1.ResourceList {
	var cpu, memory, hugepages2Mi, hugepages1Gi resource.Quantity
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
	if deploymentConfig.Resources.Requests.Name(ResourceHugePages2Mi, resource.BinarySI) != nil {
		hugepages2Mi = *deploymentConfig.Resources.Requests.Name(ResourceHugePages2Mi, resource.BinarySI)
	}
	if deploymentConfig.Resources.Requests.Name(ResourceHugePages1Gi, resource.BinarySI) != nil {
		hugepages1Gi = *deploymentConfig.Resources.Requests.Name(ResourceHugePages1Gi, resource.BinarySI)
	}

	return limitRequestPopulatedCheck(cpu, memory, hugepages2Mi, hugepages1Gi, "request", deployment)
}

func getLimits(deployment string, instance *searchv1alpha1.Search) corev1.ResourceList {
	var cpu, memory, hugepages2Mi, hugepages1Gi resource.Quantity
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
	if deploymentConfig.Resources.Limits.Name(ResourceHugePages2Mi, resource.BinarySI) != nil {
		hugepages2Mi = *deploymentConfig.Resources.Limits.Name(ResourceHugePages2Mi, resource.BinarySI)
	}
	if deploymentConfig.Resources.Limits.Name(ResourceHugePages1Gi, resource.BinarySI) != nil {
		hugepages1Gi = *deploymentConfig.Resources.Limits.Name(ResourceHugePages1Gi, resource.BinarySI)
	}

	return limitRequestPopulatedCheck(cpu, memory, hugepages2Mi, hugepages1Gi, "limit", deployment)
}

func limitRequestPopulatedCheck(cpu, memory, hugepages2Mi, hugepages1Gi resource.Quantity,
	resource, deployment string) corev1.ResourceList {
	resourceList := corev1.ResourceList{}

	if cpu.String() != "<nil>" && cpu.CmpInt64(0) != 0 {
		resourceList[corev1.ResourceCPU] = cpu
	}
	if memory.String() != "<nil>" && memory.CmpInt64(0) != 0 {
		resourceList[corev1.ResourceMemory] = memory
	}
	if hugepages2Mi.String() != "<nil>" && hugepages2Mi.CmpInt64(0) != 0 {
		resourceList[ResourceHugePages2Mi] = hugepages2Mi
	}
	if hugepages1Gi.String() != "<nil>" && hugepages1Gi.CmpInt64(0) != 0 {
		resourceList[ResourceHugePages1Gi] = hugepages1Gi
	}
	return resourceList
}

func getReplicaCount(deploymentName string, instance *searchv1alpha1.Search) *int32 {
	count := int32(1)
	if c, ok := defaultReplicaMap[deploymentName]; ok {
		count = c
	}
	deploymentConfig := getDeploymentConfig(deploymentName, instance)
	if deploymentConfig.ReplicaCount > 0 {
		//Collector and postgres pods cannot scale up
		if deploymentName == collectorDeploymentName || deploymentName == postgresDeploymentName {
			return &count
		}
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
	log.V(2).Info("Unknown deployment ", "name", deploymentName)
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

func (r *SearchReconciler) addEnvToSearchAPI(ctx context.Context,
	instance *searchv1alpha1.Search) (*reconcile.Result, error) {
	found := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      SEARCH_GLOBAL_CONFIG,
		Namespace: instance.Namespace,
	}, found)
	if err != nil && !errors.IsNotFound(err) {
		log.Error(err, "Could not fetch configmap search-global-config")
		return &reconcile.Result{}, err
	} else if errors.IsNotFound(err) {
		log.V(2).Info("search-global-config configmap not present")
	} else {
		err := r.updateSearchApiDeployment(ctx, true, instance, corev1.EnvVar{Name: "HUB_NAME", Value: found.Data["hubName"]})
		if err != nil {
			log.Error(err, "Failed to set env HUB_NAME on search-api deployment")
			return &reconcile.Result{}, err
		}
		log.V(2).Info("Updated search api deployment with HUB_NAME env variable")
	}
	return nil, nil
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

	} else {
		startScript := "postgresql-start.sh"
		log.V(3).Info("Found DB Config ", "startScript", found.Data[startScript])
		log.V(3).Info("New DB Config ", "startScript", cm.Data[startScript])
		if found.Data[startScript] != cm.Data[startScript] {
			err = r.Update(ctx, cm)
			if err != nil {
				log.Error(err, "Could not update configmap")
				return &reconcile.Result{}, err
			}
		}
	}

	log.V(2).Info("Created configmap ", "name", cm.Name)
	return nil, nil
}

func (r *SearchReconciler) getDBConfigData(ctx context.Context, instance *searchv1alpha1.Search) map[string]string {
	var result map[string]string
	if instance.Spec.DBConfig == "" {
		return result
	}
	found := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      instance.Spec.DBConfig,
		Namespace: instance.Namespace,
	}, found)
	if err != nil && errors.IsNotFound(err) {
		return result
	}
	return found.Data
}

func (r *SearchReconciler) GetDBConfigFromSearchCR(ctx context.Context,
	instance *searchv1alpha1.Search, configName string) string {
	postgresDeployConfig := getDeploymentConfig(postgresDeploymentName, instance)
	// get value from env var section if present
	for _, env := range postgresDeployConfig.Env {
		if env.Name == configName {
			log.Info("Set config from search CR Environment variables for postgres", configName, env.Value)
			return sanitizeDBConfig(configName, env.Value)
		}
	}
	// get value from dbconfig configmap if present
	customMap := r.getDBConfigData(ctx, instance)
	if customMap != nil {
		value, present := customMap[configName]
		if present {
			log.Info("Set config from dbconfig configMap ", "configMap", instance.Spec.DBConfig, configName, value)
			return sanitizeDBConfig(configName, value)
		}
	}
	// get default value
	defaultValue := getDefaultDBConfig(configName)
	log.V(2).Info("Set config with default value", configName, defaultValue)
	return defaultValue
}

// workMemPattern matches valid PostgreSQL memory-unit values (e.g. "64MB", "4096kB", "1GB", "65536").
// Any value not matching this pattern is rejected to prevent shell/SQL injection into the
// generated postgresql-start.sh script.
var workMemPattern = regexp.MustCompile(`^[0-9]+(kB|MB|GB|TB)?$`)

// sanitizeDBConfig validates user-supplied DB config values. Returns the default
// if the value doesn't pass validation, preventing injection into shell scripts
// or SQL statements.
func sanitizeDBConfig(configName, value string) string {
	switch configName {
	case "WORK_MEM":
		if !workMemPattern.MatchString(value) {
			log.Info("Ignoring invalid WORK_MEM value; using default",
				"value", value, "default", default_WORK_MEM)
			return default_WORK_MEM
		}
	}
	return value
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
			log.V(9).Info("Created deployment ", "name", deploy)
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
		log.V(9).Info("Updated deployment ", "name", deploy)
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
			log.V(9).Info("Created service ", "name", svc)
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

// update status condition in search instance
func updateStatusCondition(instance *searchv1alpha1.Search, podList *corev1.PodList) {
	var podCondition metav1.Condition
	var readyType string

	for _, searchPod := range podList.Items {
		readyType = "Ready--" + strings.Join(strings.Split(searchPod.Name, "-")[:2], "-")
		for _, condition := range searchPod.Status.Conditions {
			//check for condition type 'Ready'
			if condition.Type == "Ready" &&
				(((metav1.Condition{}) == podCondition) || // condition is empty
					// status exists from a previous replica, but the new replica has a non-ready status
					((metav1.Condition{}) != podCondition && condition.Status != corev1.ConditionTrue)) {
				podCondition = metav1.Condition{
					Type:   readyType,
					Status: metav1.ConditionStatus(condition.Status),
				}
				// These are optional fields in the pod, but required in Search Instance
				// Check before assigning to avoid Error:
				// Invalid value: "": status.conditions.reason in body should be at least 1 chars long
				if !condition.LastTransitionTime.IsZero() {
					podCondition.LastTransitionTime = condition.LastTransitionTime
				}
				if len(condition.Reason) > 0 {
					podCondition.Reason = condition.Reason
				} else {
					podCondition.Reason = "None"
				}
				if len(condition.Message) > 0 {
					podCondition.Message = condition.Message
				} else {
					podCondition.Message = "None"
				}
				log.V(3).Info("podCondition: ", searchPod.Name, podCondition)
				break
			}
		}
	}
	var podPresent bool // bool to check if status for this pod already exists in Search instance
	for i, instanceCondition := range instance.Status.Conditions {
		// replace only for "Ready" condition and if the podCondition is not empty
		if instanceCondition.Type == readyType && (metav1.Condition{}) != podCondition {

			podPresent = true // status for this pod already exists in Search instance
			// replace instance with the latest condition for this pod
			instance.Status.Conditions[i] = podCondition
			break
		}
	}
	// append if the podCondition is not empty
	if !podPresent && (metav1.Condition{}) != podCondition { // status for this pod doesn't exist in Search instance
		instance.Status.Conditions = append(instance.Status.Conditions,
			podCondition)
	}
}

// check if labels has 'component: search-operator'
func searchLabels(labels map[string]string) bool {
	value, ok := labels["component"]
	return ok && value == "search-v2-operator"
}
