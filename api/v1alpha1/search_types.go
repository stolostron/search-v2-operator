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

// Search is the schema for the searches API.
type Search struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SearchSpec   `json:"spec,omitempty"`
	Status SearchStatus `json:"status,omitempty"`
}

// SearchSpec defines the desired state of Search.
type SearchSpec struct {
	// +optional
	// Storage configuration for the database.
	DBStorage StorageSpec `json:"dbStorage,omitempty"`

	// +optional
	// The config map name contains parameters to override default database parameters.
	DBConfig string `json:"dbConfig,omitempty"`

	// +optional
	// Customization for search deployments.
	Deployments SearchDeployments `json:"deployments,omitempty"`

	// +optional
	// [PLACEHOLDER, NOT IMPLEMENTED] Specifies deployment replication for improved availability. Options are: Basic and High (default)
	AvailabilityConfig AvailabilityType `json:"availabilityConfig,omitempty"`

	// +optional
	// [PLACEHOLDER, NOT IMPLEMENTED] Kubernetes secret name containing user provided db secret
	// Secret should contain connection parameters [db_host, db_port, db_user, db_password, db_name, ca_cert]
	// Not supported for development preview.
	ExternalDBInstance string `json:"externalDBInstance,omitempty"`

	// +optional
	// ImagePullSecret
	ImagePullSecret string `json:"imagePullSecret,omitempty"`

	// +optional
	// ImagePullPolicy
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
	// Configuration for the database.
	Database DeploymentConfig `json:"database,omitempty"`

	// +optional
	// Configuration for the indexer.
	Indexer DeploymentConfig `json:"indexer,omitempty"`

	// +optional
	// Configuration for the collector.
	Collector DeploymentConfig `json:"collector,omitempty"`

	// +optional
	// Configuration for Query API.
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
	// Image_override
	ImageOverride string `json:"imageOverride,omitempty"`

	// +optional
	// Container Arguments
	Arguments []string `json:"arguments,omitempty"`

	// +optional
	// Container Env variables
	Env []corev1.EnvVar `json:"envVar,omitempty"`
}

type StorageSpec struct {
	// name of the storage class
	StorageClassName string `json:"storageClassName,omitempty"`
	// +optional
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
	// Conditions
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
