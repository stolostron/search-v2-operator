// Copyright Contributors to the Open Cluster Management project

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Important: Run the "make manifests" command to regenerate the manifests after you modify this file.

// DataType represents the data type of a collected field
// +kubebuilder:validation:Enum=bytes;slice;string;number;mapString
type DataType string

const (
	DataTypeBytes     DataType = "bytes"
	DataTypeSlice     DataType = "slice"
	DataTypeString    DataType = "string"
	DataTypeNumber    DataType = "number"
	DataTypeMapString DataType = "mapString"
)

// ActionType represents the action to take for a collection rule
// +kubebuilder:validation:Enum=include;exclude
type ActionType string

const (
	ActionInclude ActionType = "include"
	ActionExclude ActionType = "exclude"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=sccc

// CollectionConfig is the schema for the collection-configs API.
type CollectionConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec CollectionConfigSpec `json:"spec,omitempty"`
}

// CollectionConfigSpec defines the configuration changes made to the resources and fields indexed by Search Collectors.
type CollectionConfigSpec struct {
	// +optional
	// Specifies the namespaces where resources are to be indexed by Search Collectors
	CollectNamespaces *CollectNamespaces `json:"collectNamespaces,omitempty"`

	// +optional
	// Defines a list of rules for collecting resources and specific fields.
	CollectionRules []CollectionRule `json:"collectionRules,omitempty"`
}

// CollectNamespaces specifies the namespaces where resources are to be indexed by Search Collectors.
type CollectNamespaces struct {
	// +optional
	// NamespaceSelector determines namespaces on the managed cluster from which to collect resources.
	// The include and exclude parameters accept file path expressions to include and exclude namespaces by name.
	// The matchExpressions and matchLabels parameters specify namespaces to include by label.
	// See the Kubernetes labels and selectors documentation.
	// The resulting list is compiled by using the intersection of results from all parameters.
	// You must provide either include or at least one of matchExpressions or matchLabels to retrieve namespaces.
	NamespaceSelector *NamespaceSelector `json:"namespaceSelector,omitempty"`
}

// NamespaceSelector defines the selector for namespaces.
type NamespaceSelector struct {
	// +optional
	// [NOT IMPLEMENTED] Include is an array of filepath expressions to include objects by name.
	// +kubebuilder:validation:items:MinLength=1
	Include []string `json:"include,omitempty"`

	// +optional
	// [NOT IMPLEMENTED] Exclude is an array of filepath expressions to exclude objects by name.
	// +kubebuilder:validation:items:MinLength=1
	Exclude []string `json:"exclude,omitempty"`

	// +optional
	// [NOT IMPLEMENTED] MatchExpressions is an array of label selector requirements matching objects by label.
	MatchExpressions []metav1.LabelSelectorRequirement `json:"matchExpressions,omitempty"`

	// +optional
	// [NOT IMPLEMENTED] MatchLabels is a map of {key,value} pairs matching objects by label.
	MatchLabels map[string]string `json:"matchLabels,omitempty"`
}

// CollectionRule defines a rule for collecting resources and specific fields.
type CollectionRule struct {
	// Defines whether to Include (collect) or Exclude (deny collection) resources matching the selector.
	Action ActionType `json:"action"`

	// Specifies which resources this rule applies to.
	ResourceSelector ResourceSelector `json:"resourceSelector"`

	// +optional
	// Specifies the additional fields on the resource to index by Search Collectors.
	Fields []Field `json:"fields,omitempty"`

	// +optional
	// [NOT IMPLEMENTED] Specifies to collect annotations for the resource.
	CollectAnnotations *bool `json:"collectAnnotations,omitempty"`

	// +optional
	// [NOT IMPLEMENTED] Specifies to collect status conditions for the resource.
	CollectConditions *bool `json:"collectConditions,omitempty"`

	// +optional
	// [NOT IMPLEMENTED] Specifies to collect additionalPrinterColumns from the CRD with the specified priority or higher.
	CollectAdditionalPrinterColumnsPriority *int `json:"collectAdditionalPrinterColumnsPriority,omitempty"`
}

// ResourceSelector specifies which resources a rule applies to.
type ResourceSelector struct {
	// Specifies apiGroups of resources.
	APIGroups []string `json:"apiGroups"`

	// Specifies kinds of resources.
	Kinds []string `json:"kinds"`
}

// Field specifies an additional field on the resource to index by Search Collectors.
type Field struct {
	// Specifies the name of the collected item on the resource.
	Name string `json:"name"`

	// JSONPath to the field on the resource.
	JSONPath string `json:"jsonPath"`

	// +optional
	// +kubebuilder:default=string
	// Data type of resource field to be indexed by Search Collectors. Default is a string.
	Type DataType `json:"type,omitempty"`
}

// +kubebuilder:object:root=true

// CollectionConfigList contains a list of CollectionConfig.
type CollectionConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CollectionConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CollectionConfig{}, &CollectionConfigList{})
}
