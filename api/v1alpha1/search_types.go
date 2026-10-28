// Copyright Contributors to the Open Cluster Management project

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Important: Run the "make manifests" command to regenerate the manifests after you modify this file.

// AvailabilityType
type AvailabilityType string

const (
	// HABasic creates application subscriptions with a replicaCount of 1.
	HABasic AvailabilityType = "Basic"
	// HAHigh creates application subscriptions with a replicaCount of 2.
	// Not supported for development preview.
	HAHigh AvailabilityType = "High"
)

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Search configures the ACM Search service, including database storage,
// replica counts, and resource limits for the collector, indexer, query API,
// and database deployments.
type Search struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SearchSpec   `json:"spec,omitempty"`
	Status SearchStatus `json:"status,omitempty"`
}

// SearchSpec defines the configuration for the Search service components.
// Most fields have production-ready defaults and do not need to be set.
type SearchSpec struct {
	// +optional
	// Storage configuration for the database.
	DBStorage StorageSpec `json:"dbStorage,omitempty"`

	// +optional
	// Name of a ConfigMap that overrides default PostgreSQL parameters for the
	// Search database, such as connection limits and memory-related settings.
	DBConfig string `json:"dbConfig,omitempty"`

	// +optional
	// Customization for search deployments.
	Deployments SearchDeployments `json:"deployments,omitempty"`

	// +optional
	// Not currently supported. Reserved for future use.
	AvailabilityConfig AvailabilityType `json:"availabilityConfig,omitempty"`

	// +optional
	// Not currently supported. Reserved for future use.
	// Secret should contain connection parameters [db_host, db_port, db_user, db_password, db_name, ca_cert]
	// Not supported for development preview.
	ExternalDBInstance string `json:"externalDBInstance,omitempty"`

	// +optional
	// Name of an existing Secret of type kubernetes.io/dockerconfigjson used
	// to pull Search component images from private registries.
	ImagePullSecret string `json:"imagePullSecret,omitempty"`

	// +optional
	// Kubernetes image pull policy for Search component images.
	// One of: Always, Never, IfNotPresent.
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// +optional
	// Define the nodes that you want to schedule with matching labels.
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// +optional
	// Define tolerations to schedule pods on nodes with matching taints.
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
}

type SearchDeployments struct {
	// +optional
	// Configuration for the PostgreSQL database deployment (replicas, resources,
	// image override, arguments, and environment variables).
	Database DeploymentConfig `json:"database,omitempty"`

	// +optional
	// Configuration for the indexer.
	Indexer DeploymentConfig `json:"indexer,omitempty"`

	// +optional
	// Configuration for the collector.
	Collector DeploymentConfig `json:"collector,omitempty"`

	// +optional
	// Configuration for the Search query API deployment (replicas, resources,
	// image override, arguments, and environment variables).
	QueryAPI DeploymentConfig `json:"queryapi,omitempty"`
}

type DeploymentConfig struct {
	// +optional
	// +kubebuilder:validation:Minimum:=1
	// Number of pod instances for the deployment.
	ReplicaCount int32 `json:"replicaCount,omitempty"`

	// +optional
	// Compute resources required by deployment.
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// +optional
	// Full image reference (registry/repository:tag) to override the default
	// image for this Search component deployment.
	ImageOverride string `json:"imageOverride,omitempty"`

	// +optional
	// Container Arguments
	Arguments []string `json:"arguments,omitempty"`

	// +optional
	// Container Env variables
	Env []corev1.EnvVar `json:"envVar,omitempty"`
}

type StorageSpec struct {
	// Name of the StorageClass used to provision the database PVC.
	// If not set, Search uses ephemeral emptyDir storage for the database.
	StorageClassName string `json:"storageClassName,omitempty"`
	// +optional
	// Size of the PersistentVolumeClaim for the Search database.
	// Example: "10Gi". Defaults to "10Gi".
	// +kubebuilder:default:="10Gi"
	Size *resource.Quantity `json:"size,omitempty"`
}

// SearchStatus defines the observed state of Search.
type SearchStatus struct {

	// Database used by Search.
	DB string `json:"db"`

	// Storage used by database
	Storage string `json:"storage"`

	// +optional
	// Conditions describing the current state of the Search deployment.
	// Condition types include Ready and Degraded.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true

// SearchList contains a list of Search.
type SearchList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Search `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Search{}, &SearchList{})
}
