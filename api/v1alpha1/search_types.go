// Copyright Contributors to the Open Cluster Management project

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

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
	// Control list of Kubernetes resources indexed by search-collector
	AllowDenyResources FilterSpec `json:"allowDenyResources,omitempty"`

	// +optional
	// Kubernetes secret name containing user provided db secret
	// Secret contains connection url, certificates
	CustomDbConfig string `json:"customDbConfig,omitempty"`
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
	API DeploymentConfig `json:"api,omitempty"`

	// +optional
	// Configuration for addon installed collector Deployment
	RemoteCollector DeploymentConfig `json:"remoteCollector,omitempty"`
}

type DeploymentConfig struct {
	// +optional
	// Number of pod instances for deployment
	ReplicaCount int `json:"replicaCount,omitempty"`

	// +optional
	// Compute Resources required by deployment
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

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
	APIGroups []string `json:"apiGroups,omitempty"`
	//Resource names to be filtered
	Resources []string `json:"resources,omitempty"`
	// +optional
	//Cluster Labels this filter to be applied
	ClusterLabels metav1.LabelSelector `json:"clusterLabels,omitempty"`
}

// SearchStatus defines the observed state of Search
type SearchStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// Human readable health state
	Health string `json:"health,omitempty"`

	// Database used by search
	DB string `json:"db,omitempty"`

	// Storage used by database
	StorageInUse string `json:"storageInUse,omitempty"`

	// +optional
	Conditions []SearchCondition `json:"conditions,omitempty"`
}

type SearchCondition struct {
	Type SearchConditionType `json:"type"`

	Status corev1.ConditionStatus `json:"status"`

	// Last time the condition transitioned
	LastTransitionTime metav1.Time `json:"lastTransitionTime"`

	// +optional
	// Reason for the condition's last transition.
	Reason string `json:"reason,omitempty"`

	// +optional
	// Human readable message
	Message string `json:"message,omitempty" `
}

// SearchConditionType
type SearchConditionType string

// AvailabilityType
type AvailabilityType string

const (
	// HABasic stands up most app subscriptions with a replicaCount of 1
	HABasic AvailabilityType = "Basic"
	// HAHigh stands up most app subscriptions with a replicaCount of 2
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
