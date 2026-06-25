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
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// log is for logging in this package.
var collectorconfiglog = logf.Log.WithName("collectorconfig-resource")

// webhookClient is the Kubernetes client used to list integration team CollectorConfigs
// during admission. Set once in SetupWebhookWithManager; nil in unit tests that don't
// register a manager (the integration-config check is skipped when nil).
var webhookClient client.Client

// Precompiled validation patterns.
var (
	fieldNamePattern   = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9\-_.]*$`)
	fieldSuffixPattern = regexp.MustCompile(`^[a-z0-9\-.]+$`)
)

func (r *CollectorConfig) SetupWebhookWithManager(mgr ctrl.Manager) error {
	webhookClient = mgr.GetClient()
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		WithValidator(r).
		Complete()
}

//+kubebuilder:webhookconfiguration:mutating=false,name=search-v2-operator-validating-webhook-configuration
//+kubebuilder:webhook:path=/validate-search-open-cluster-management-io-v1alpha1-collectorconfig,mutating=false,failurePolicy=fail,sideEffects=None,groups=search.open-cluster-management.io,resources=collectorconfigs,verbs=create;update;delete,versions=v1alpha1,name=vcollectorconfig.kb.io,admissionReviewVersions=v1,serviceName=search-v2-operator-webhook-service

var _ webhook.CustomValidator = &CollectorConfig{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type
func (r *CollectorConfig) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	cc, ok := obj.(*CollectorConfig)
	if !ok {
		return nil, fmt.Errorf("expected a CollectorConfig object but got %T", obj)
	}
	collectorconfiglog.Info("validate create", "name", cc.Name)

	if err := rejectIfProtected(ctx, cc); err != nil {
		return nil, err
	}

	if err := cc.validateCollectorConfig(); err != nil {
		return nil, err
	}

	return nil, validateExcludeAgainstIntegrationConfigs(ctx, cc)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type
func (r *CollectorConfig) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	cc, ok := newObj.(*CollectorConfig)
	if !ok {
		return nil, fmt.Errorf("expected a CollectorConfig object but got %T", newObj)
	}
	oldCC, ok := oldObj.(*CollectorConfig)
	if !ok {
		return nil, fmt.Errorf("expected a CollectorConfig object but got %T", oldObj)
	}
	collectorconfiglog.Info("validate update", "name", cc.Name)

	// Check both old and new objects — prevents stripping the owner reference to bypass protection.
	if err := rejectIfProtected(ctx, cc); err != nil {
		return nil, err
	}
	if err := rejectIfProtected(ctx, oldCC); err != nil {
		return nil, err
	}

	if err := cc.validateCollectorConfig(); err != nil {
		return nil, err
	}

	return nil, validateExcludeAgainstIntegrationConfigs(ctx, cc)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type
func (r *CollectorConfig) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	cc, ok := obj.(*CollectorConfig)
	if !ok {
		return nil, fmt.Errorf("expected a CollectorConfig object but got %T", obj)
	}
	collectorconfiglog.Info("validate delete", "name", cc.Name)

	if err := rejectIfProtected(ctx, cc); err != nil {
		return nil, err
	}

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

		hasFields := len(rule.Fields) > 0

		// Validate wildcard kind "*": cannot be used with fields (include only —
		// exclude rules handle this separately in validateExcludeRule).
		if rule.Action == ActionInclude {
			for _, k := range rule.ResourceSelector.Kinds {
				if k == "*" && hasFields {
					allErrs = append(allErrs, field.Invalid(
						rulePath.Child("resourceSelector", "kinds"),
						rule.ResourceSelector.Kinds,
						"wildcard kind \"*\" cannot be used with fields",
					))
					break
				}
			}
		}

		// Validate Fields (only for Include actions)
		if rule.Action == ActionInclude && hasFields {
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

		// Validate CollectAdditionalPrinterColumnsPriority if present
		if rule.CollectAdditionalPrinterColumnsPriority != nil && *rule.CollectAdditionalPrinterColumnsPriority < -1 {
			allErrs = append(allErrs, field.Invalid(
				rulePath.Child("collectAdditionalPrinterColumnsPriority"),
				*rule.CollectAdditionalPrinterColumnsPriority,
				"must be >= -1 (-1 disables collection)",
			))
		}

		// Validate FieldSuffix format if present (include only — exclude rules reject
		// fieldSuffix entirely in validateExcludeRule, avoiding duplicate errors).
		if rule.Action == ActionInclude && rule.FieldSuffix != "" {
			if !isValidFieldSuffix(rule.FieldSuffix) {
				allErrs = append(allErrs, field.Invalid(
					rulePath.Child("fieldSuffix"),
					rule.FieldSuffix,
					"must contain only lowercase alphanumeric characters, '-', or '.'",
				))
			}
		}

		// Validate exclude-specific constraints
		if rule.Action == ActionExclude {
			allErrs = append(allErrs, validateExcludeRule(&rule, rulePath)...)
		}
	}

	if len(allErrs) == 0 {
		return nil
	}

	return allErrs.ToAggregate()
}

// protectedKinds lists resource types the search RBAC engine depends on.
// Excluding them would break per-cluster and namespace-scoped access control:
//   - ManagedCluster: used to scope search results to clusters a user can access
//   - Namespace: used to scope search results to namespaces a user can access
//
// ManagedClusterSet and ManagedClusterSetBinding are NOT listed here because the
// RBAC engine does not query them directly — they are used by placement/policy, not search.
var protectedKinds = map[string]string{
	"ManagedCluster": "cluster.open-cluster-management.io",
	"Namespace":      "", // core group
}

// validateExcludeRule enforces constraints specific to exclude rules:
//   - Cannot target ManagedCluster or Namespace (search RBAC engine depends on them)
//   - Cannot specify fields, collectConditions, or fieldSuffix (meaningless on an exclude)
func validateExcludeRule(rule *CollectionRule, path *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	// Reject fields, collectConditions, collectAnnotations, and fieldSuffix on exclude rules.
	if len(rule.Fields) > 0 {
		allErrs = append(allErrs, field.Invalid(
			path.Child("fields"),
			rule.Fields,
			"fields cannot be specified on an exclude rule",
		))
	}
	if rule.CollectConditions != nil {
		allErrs = append(allErrs, field.Invalid(
			path.Child("collectConditions"),
			*rule.CollectConditions,
			"collectConditions cannot be set on an exclude rule",
		))
	}
	if rule.CollectAnnotations != nil {
		allErrs = append(allErrs, field.Invalid(
			path.Child("collectAnnotations"),
			*rule.CollectAnnotations,
			"collectAnnotations cannot be set on an exclude rule",
		))
	}
	if rule.FieldSuffix != "" {
		allErrs = append(allErrs, field.Invalid(
			path.Child("fieldSuffix"),
			rule.FieldSuffix,
			"fieldSuffix cannot be specified on an exclude rule",
		))
	}

	// Reject exclusion of protected resource types.
	// Check both specific kind names and wildcard kinds — apiGroups:["cluster.open-cluster-management.io"]
	// kinds:["*"] would exclude ManagedCluster just as effectively as naming it explicitly.
	allErrs = append(allErrs, validateProtectedKinds(rule, path)...)
	return allErrs
}

// validateProtectedKinds checks that an exclude rule does not target ManagedCluster or Namespace,
// either by name or via a wildcard kind on their apiGroup.
func validateProtectedKinds(rule *CollectionRule, path *field.Path) field.ErrorList {
	var allErrs field.ErrorList
	for _, kind := range rule.ResourceSelector.Kinds {
		if kind == "*" {
			allErrs = append(allErrs, validateWildcardKindAgainstProtected(rule.ResourceSelector.APIGroups, path)...)
			continue
		}
		if protectedGroup, protected := protectedKinds[kind]; protected {
			allErrs = append(allErrs, validateSpecificKindAgainstProtected(kind, protectedGroup, rule.ResourceSelector.APIGroups, path)...)
		}
	}
	return allErrs
}

// validateWildcardKindAgainstProtected rejects kinds:["*"] when the rule's apiGroups contain
// a group that holds a protected kind (e.g. cluster.open-cluster-management.io → ManagedCluster).
func validateWildcardKindAgainstProtected(apiGroups []string, path *field.Path) field.ErrorList {
	var allErrs field.ErrorList
	for protectedKind, protectedGroup := range protectedKinds {
		for _, apiGroup := range apiGroups {
			if apiGroup == "*" || apiGroup == protectedGroup {
				allErrs = append(allErrs, field.Invalid(
					path.Child("resourceSelector", "apiGroups"),
					apiGroup,
					"cannot exclude all kinds in this apiGroup — it contains "+
						protectedKind+", which search depends on for RBAC and cluster-scoped queries",
				))
				break
			}
		}
	}
	return allErrs
}

// validateSpecificKindAgainstProtected rejects a specific protected kind when the rule's
// apiGroups match the kind's group (including the global wildcard "*").
func validateSpecificKindAgainstProtected(kind, protectedGroup string, apiGroups []string, path *field.Path) field.ErrorList {
	var allErrs field.ErrorList
	for _, apiGroup := range apiGroups {
		if apiGroup == protectedGroup || apiGroup == "*" {
			allErrs = append(allErrs, field.Invalid(
				path.Child("resourceSelector", "kinds"),
				kind,
				"cannot exclude "+kind+" — search depends on it for RBAC and cluster-scoped queries",
			))
			break
		}
	}
	return allErrs
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
			"must be a valid JSONPath expression (e.g. \".spec.myField\" or \"{.spec.myField}\")",
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

// isValidJSONPath performs basic JSONPath syntax validation.
// Accepts both braced ("{.spec.myField}") and unbraced (".spec.myField") forms —
// the collector normalizes to braced form at runtime (ACM-33144).
func isValidJSONPath(jsonPath string) bool {
	// Normalize to braced form for validation regardless of input format.
	normalized := "{" + strings.TrimSuffix(strings.TrimPrefix(jsonPath, "{"), "}") + "}"

	// Must start with {. — at least one path segment is required.
	if !strings.HasPrefix(normalized, "{.") {
		return false
	}

	// Must have content after the opening {.
	inner := strings.TrimPrefix(strings.TrimSuffix(normalized, "}"), "{.")
	if len(inner) < 1 {
		return false
	}

	// Parse-based validation using the k8s jsonpath library.
	jp := jsonpath.New("collectorconfig-field")
	return jp.Parse(normalized) == nil
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

// isOperatorOwned returns true if the CollectorConfig has a controller owner reference,
// indicating it is managed by the operator and should not be modified directly.
func isOperatorOwned(cc *CollectorConfig) bool {
	for _, ref := range cc.GetOwnerReferences() {
		if ref.Controller != nil && *ref.Controller {
			return true
		}
	}
	return false
}

// rejectIfProtected returns an error if the config has a controller owner reference
// and the caller is not a service account in the same namespace (i.e., not the operator).
func rejectIfProtected(ctx context.Context, cc *CollectorConfig) error {
	if !isOperatorOwned(cc) {
		return nil
	}

	req, err := admission.RequestFromContext(ctx)
	if err != nil {
		return fmt.Errorf("could not extract admission request: %v", err)
	}

	// Allow any service account in the resource's namespace.
	// Only the operator namespace should have write RBAC for CollectorConfigs,
	// so namespace-scoped SA check is sufficient.
	expectedPrefix := fmt.Sprintf("system:serviceaccount:%s:", req.Namespace)
	if strings.HasPrefix(req.UserInfo.Username, expectedPrefix) {
		return nil
	}

	return fmt.Errorf("%s is managed by the search operator and cannot be modified directly", cc.Name)
}

// validateExcludeAgainstIntegrationConfigs rejects exclude rules on non-integration
// CollectorConfigs that would conflict with an integration team's include rules.
// This mirrors the merge-time protection in the operator (excludeOverlapsIntegrationIncludes)
// but surfaces the error at admission time for an immediate feedback.
//
// The check is skipped when:
//   - webhookClient is nil (unit tests without a registered manager)
//   - the submitted CollectorConfig is itself an integration team config
//   - the CollectorConfig has no exclude rules
func validateExcludeAgainstIntegrationConfigs(ctx context.Context, cc *CollectorConfig) error {
	if webhookClient == nil {
		return nil
	}
	// Integration team configs are allowed to exclude — they own their own rules.
	if cc.Labels[IntegrationTeamLabel] == IntegrationTeamLabelValue {
		return nil
	}

	// Collect exclude rules from the submitted config.
	var excludeRules []CollectionRule
	for _, rule := range cc.Spec.CollectionRules {
		if rule.Action == ActionExclude {
			excludeRules = append(excludeRules, rule)
		}
	}
	if len(excludeRules) == 0 {
		return nil
	}

	// List integration team CollectorConfigs.
	integrationList := &CollectorConfigList{}
	if err := webhookClient.List(ctx, integrationList,
		client.InNamespace(cc.Namespace),
		client.MatchingLabels{IntegrationTeamLabel: IntegrationTeamLabelValue},
	); err != nil {
		// Log and allow — a list failure should not block valid CRD operations.
		collectorconfiglog.Error(err, "could not list integration team CollectorConfigs; skipping overlap check")
		return nil
	}

	// Reject any exclude that overlaps an integration include.
	var allErrs field.ErrorList
	rulesPath := field.NewPath("spec", "collectionRules")
	for i, excludeRule := range cc.Spec.CollectionRules {
		if excludeRule.Action != ActionExclude {
			continue
		}
		for _, ic := range integrationList.Items {
			for _, teamRule := range ic.Spec.CollectionRules {
				if teamRule.Action != ActionInclude {
					continue
				}
				if webhookSetsIntersect(excludeRule.ResourceSelector.APIGroups, teamRule.ResourceSelector.APIGroups) &&
					webhookSetsIntersect(excludeRule.ResourceSelector.Kinds, teamRule.ResourceSelector.Kinds) {
					allErrs = append(allErrs, field.Invalid(
						rulesPath.Index(i).Child("resourceSelector"),
						excludeRule.ResourceSelector,
						"cannot exclude these resources — they are necessary for system functionality",
					))
				}
			}
		}
	}

	if len(allErrs) == 0 {
		return nil
	}
	return allErrs.ToAggregate()
}

// webhookSetsIntersect returns true when two string slices share at least one element,
// treating "*" as a universal match for all values on the other side.
// This mirrors controllers.setsIntersect but is local to the webhook package to
// avoid an import cycle.
func webhookSetsIntersect(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == "*" || y == "*" || x == y {
				return true
			}
		}
	}
	return false
}
