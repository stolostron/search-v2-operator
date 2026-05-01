// Copyright Contributors to the Open Cluster Management project

package v1alpha1

import (
	"context"
	"fmt"
	"k8s.io/client-go/util/jsonpath"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// log is for logging in this package.
var collectorconfiglog = logf.Log.WithName("collectorconfig-resource")

// Precompiled validation patterns.
var (
	fieldNamePattern   = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9\-_.]*$`)
	fieldSuffixPattern = regexp.MustCompile(`^[a-z0-9\-.]+$`)
)

func (r *CollectorConfig) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		WithValidator(r).
		Complete()
}

//+kubebuilder:webhook:path=/validate-search-open-cluster-management-io-v1alpha1-collectorconfig,mutating=false,failurePolicy=fail,sideEffects=None,groups=search.open-cluster-management.io,resources=collectorconfigs,verbs=create;update,versions=v1alpha1,name=vcollectorconfig.kb.io,admissionReviewVersions=v1

var _ webhook.CustomValidator = &CollectorConfig{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type
func (r *CollectorConfig) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	cc, ok := obj.(*CollectorConfig)
	if !ok {
		return nil, fmt.Errorf("expected a CollectorConfig object but got %T", obj)
	}
	collectorconfiglog.Info("validate create", "name", cc.Name)

	err := cc.validateCollectorConfig()
	return nil, err
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type
func (r *CollectorConfig) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	cc, ok := newObj.(*CollectorConfig)
	if !ok {
		return nil, fmt.Errorf("expected a CollectorConfig object but got %T", newObj)
	}
	collectorconfiglog.Info("validate update", "name", cc.Name)

	err := cc.validateCollectorConfig()
	return nil, err
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type
func (r *CollectorConfig) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	cc, ok := obj.(*CollectorConfig)
	if !ok {
		return nil, fmt.Errorf("expected a CollectorConfig object but got %T", obj)
	}
	collectorconfiglog.Info("validate delete", "name", cc.Name)

	// No validation needed for deletion
	return nil, nil
}

// validateCollectorConfig performs validation on the CollectorConfig spec
func (r *CollectorConfig) validateCollectorConfig() error {
	var allErrs field.ErrorList

	// Validate CollectionRules
	rulesPath := field.NewPath("spec", "collectionRules")
	for i, rule := range r.Spec.CollectionRules {
		rulePath := rulesPath.Index(i)

		// Validate action
		if rule.Action != ActionInclude && rule.Action != ActionExclude {
			allErrs = append(allErrs, field.NotSupported(
				rulePath.Child("action"),
				rule.Action,
				[]string{string(ActionInclude), string(ActionExclude)},
			))
		}

		// Validate ResourceSelector
		if err := r.validateResourceSelector(&rule.ResourceSelector, rulePath.Child("resourceSelector")); err != nil {
			allErrs = append(allErrs, err...)
		}

		// Validate Fields (only for Include actions)
		if rule.Action == ActionInclude && len(rule.Fields) > 0 {
			// When fields are specified, must have exactly 1 kind and 1 apiGroup
			if len(rule.ResourceSelector.Kinds) != 1 {
				allErrs = append(allErrs, field.Invalid(
					rulePath.Child("resourceSelector", "kinds"),
					rule.ResourceSelector.Kinds,
					"must specify exactly 1 kind when fields are defined",
				))
			}
			if len(rule.ResourceSelector.APIGroups) != 1 {
				allErrs = append(allErrs, field.Invalid(
					rulePath.Child("resourceSelector", "apiGroups"),
					rule.ResourceSelector.APIGroups,
					"must specify exactly 1 apiGroup when fields are defined",
				))
			}

			// Validate each field
			fieldsPath := rulePath.Child("fields")
			for j, customField := range rule.Fields {
				fieldPath := fieldsPath.Index(j)
				if err := r.validateField(&customField, fieldPath); err != nil {
					allErrs = append(allErrs, err...)
				}
			}
		}

		// Validate FieldSuffix if present
		if rule.FieldSuffix != "" {
			if !isValidFieldSuffix(rule.FieldSuffix) {
				allErrs = append(allErrs, field.Invalid(
					rulePath.Child("fieldSuffix"),
					rule.FieldSuffix,
					"must contain only lowercase alphanumeric characters, '-', or '.'",
				))
			}
		}
	}

	if len(allErrs) == 0 {
		return nil
	}

	return allErrs.ToAggregate()
}

// validateResourceSelector validates the ResourceSelector fields
func (r *CollectorConfig) validateResourceSelector(selector *ResourceSelector, path *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	// APIGroups validation
	if len(selector.APIGroups) == 0 {
		allErrs = append(allErrs, field.Required(path.Child("apiGroups"), "must specify at least one apiGroup"))
	}

	// Kinds validation
	if len(selector.Kinds) == 0 {
		allErrs = append(allErrs, field.Required(path.Child("kinds"), "must specify at least one kind"))
	}

	return allErrs
}

// validateField validates a single Field definition
func (r *CollectorConfig) validateField(customField *Field, path *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	// Name validation
	if customField.Name == "" {
		allErrs = append(allErrs, field.Required(path.Child("name"), "field name is required"))
	} else if !isValidFieldName(customField.Name) {
		allErrs = append(allErrs, field.Invalid(
			path.Child("name"),
			customField.Name,
			"must start with a letter and contain only alphanumeric characters, '-', '_', or '.'",
		))
	}

	// JSONPath validation
	if customField.JSONPath == "" {
		allErrs = append(allErrs, field.Required(path.Child("jsonPath"), "jsonPath is required"))
	} else if !isValidJSONPath(customField.JSONPath) {
		allErrs = append(allErrs, field.Invalid(
			path.Child("jsonPath"),
			customField.JSONPath,
			"must be a valid JSONPath expression starting with '{.' and ending with '}'",
		))
	}

	// DataType validation
	if customField.Type != "" {
		validTypes := []string{
			string(DataTypeString),
			string(DataTypeNumber),
			string(DataTypeBytes),
			string(DataTypeSlice),
			string(DataTypeMapString),
		}
		if !contains(validTypes, string(customField.Type)) {
			allErrs = append(allErrs, field.NotSupported(
				path.Child("type"),
				customField.Type,
				validTypes,
			))
		}
	}

	return allErrs
}

// isValidFieldName checks if a field name is valid
// Must start with a letter and contain only alphanumeric, '-', '_', or '.'
func isValidFieldName(name string) bool {
	return fieldNamePattern.MatchString(name)
}

// isValidFieldSuffix checks if a field suffix is valid
// Can only contain lowercase alphanumeric, '-', or '.'
func isValidFieldSuffix(suffix string) bool {
	return fieldSuffixPattern.MatchString(suffix)
}

// isValidJSONPath performs basic JSONPath syntax validation
func isValidJSONPath(jsonPath string) bool {
	// FUTURE: ACM-33144 removes necessity of '{}' in jsonPath and default to parse-based validation check
	// Must start with {. and end with }
	if !strings.HasPrefix(jsonPath, "{.") || !strings.HasSuffix(jsonPath, "}") {
		return false
	}

	// Basic validation - contains at least one path element
	inner := strings.TrimPrefix(strings.TrimSuffix(jsonPath, "}"), "{.")
	if len(inner) < 1 {
		return false
	}

	// Parse-based validation
	jp := jsonpath.New("collectorconfig-field")
	return jp.Parse(jsonPath) == nil
}

// contains checks if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
