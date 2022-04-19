// Copyright Contributors to the Open Cluster Management project

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AvailabilityType
type AvailabilityType string

const (
	// HABasic stands up most app subscriptions with a replicaCount of 1
	HABasic AvailabilityType = "Basic"
	// HAHigh stands up most app subscriptions with a replicaCount of 2
	// Not supported for Dev preview
	HAHigh AvailabilityType = "High"
)

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Search is the Schema for the searches API
type Search struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SearchSpec   `json:"spec,omitempty"`
	Status SearchStatus `json:"status,omitempty"`
}

// SearchSpec defines the desired state of Search
type SearchSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// +optional
	// Storgeclass Name to be used by default db
	DBStorage StorageSpec `json:"dbStorage,omitempty"`

	// +optional
	//Configmap name contains parameters to override default db parameters
	DbConfig string `json:"dbConfig,omitempty"`

	// +optional
	// Customization for search deployments
	Deployments SearchDeployments `json:"deployments,omitempty"`

	// +optional
	// Specifies deployment replication for improved availability. Options are: Basic and High (default)
	AvailabilityConfig AvailabilityType `json:"availabilityConfig,omitempty"`

	// +optional
	// Not part of dev preview
	// Kubernetes secret name containing user provided db secret
	// Secret should contain connection parameters [db_host, db_port, db_user, db_password, db_name, ca_cert]
	// ExternalDBInstance string `json:"externalDBInstance,omitempty"`
}

type SearchDeployments struct {
	// +optional
	// Configuration for DB Deployment
	Database DeploymentConfig `json:"database,omitempty"`

	// +optional
	// Configuration for indexer Deployment
	Indexer DeploymentConfig `json:"indexer,omitempty"`

	// +optional
	// Configuration for collector Deployment
	Collector DeploymentConfig `json:"collector,omitempty"`

	// +optional
	// Configuration for api Deployment
	Query_API DeploymentConfig `json:"query_api,omitempty"`
}

type DeploymentConfig struct {
	// +optional
	// +kubebuilder:validation:Minimum:=1
	// Number of pod instances for deployment
	ReplicaCount int32 `json:"replicaCount,omitempty"`

	// +optional
	// Compute Resources required by deployment
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// +optional
	//Image_override
	ImageOverride string `json:"imageOverride,omitempty"`

	// +optional
	//ImagePullSecret
	ImagePullSecret string `json:"imagePullSecret,omitempty"`

	//ImagePullPolicy
	// +optional
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// NodeSelector to schedule on nodes with matching labels
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	//Proxy config , if remote collectors need to override
	// +optional
	ProxyConfig map[string]string `json:"proxyConfig,omitempty"`
}

type StorageSpec struct {
	// +optional
	// name of the storage class
	StorageClassName string `json:"storageClassName,omitempty"`
	// +optional
	// storage capacity
	Size *resource.Quantity `json:"size,omitempty"`
}

type FilterSpec struct {
	// +optional
	// Allowed resources from collector
	AllowedResources []ResourceListSpec `json:"allowedResources,omitempty"`

	// +optional
	// Denied resources from collector
	DeniedResources []ResourceListSpec `json:"deniedResources,omitempty"`
}

type ResourceListSpec struct {
	//API Group names to be filtered
	APIGroups []string `json:"apiGroups"`
	//Resource names to be filtered
	Resources []string `json:"resources"`
	// +optional
	//Cluster Labels this filter to be applied
	ClusterLabels metav1.LabelSelector `json:"clusterLabels,omitempty"`
}

// SearchStatus defines the observed state of Search
type SearchStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// Human readable health status
	Status string `json:"status"`

	// Database used by search
	DB string `json:"db"`

	// Storage used by database
	StorageInUse string `json:"storageInUse"`

	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true

// SearchList contains a list of Search
type SearchList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Search `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Search{}, &SearchList{})
}
