// Copyright Contributors to the Open Cluster Management project

package v1alpha1

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func validConfig() *CollectorConfig {
	return &CollectorConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: CollectorConfigSpec{
			CollectionRules: []CollectionRule{
				{
					Action: ActionInclude,
					ResourceSelector: ResourceSelector{
						APIGroups: []string{"apps"},
						Kinds:     []string{"Deployment"},
					},
				},
			},
		},
	}
}

// Reject a collection rule with an unsupported action value.
func TestRejectInvalidAction(t *testing.T) {
	c := validConfig()
	c.Spec.CollectionRules[0].Action = "invalid"
	_, err := c.ValidateCreate(context.Background(), c)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Unsupported value")
}

// Reject a rule with an empty apiGroups list.
func TestRejectEmptyAPIGroups(t *testing.T) {
	c := validConfig()
	c.Spec.CollectionRules[0].ResourceSelector.APIGroups = []string{}
	_, err := c.ValidateCreate(context.Background(), c)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must specify at least one apiGroup")
}

// Reject a rule with an empty kinds list.
func TestRejectEmptyKinds(t *testing.T) {
	c := validConfig()
	c.Spec.CollectionRules[0].ResourceSelector.Kinds = []string{}
	_, err := c.ValidateCreate(context.Background(), c)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must specify at least one kind")
}

// Reject fields when multiple kinds are specified (must be exactly 1).
func TestRejectMultipleKindsWithFields(t *testing.T) {
	c := validConfig()
	c.Spec.CollectionRules[0].ResourceSelector.Kinds = []string{"Deployment", "StatefulSet"}
	c.Spec.CollectionRules[0].Fields = []Field{{Name: "r", JSONPath: "{.spec.replicas}"}}
	_, err := c.ValidateCreate(context.Background(), c)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must specify exactly 1 kind when fields are defined")
}

// Reject fields when multiple apiGroups are specified (must be exactly 1).
func TestRejectMultipleAPIGroupsWithFields(t *testing.T) {
	c := validConfig()
	c.Spec.CollectionRules[0].ResourceSelector.APIGroups = []string{"apps", "batch"}
	c.Spec.CollectionRules[0].Fields = []Field{{Name: "r", JSONPath: "{.spec.replicas}"}}
	_, err := c.ValidateCreate(context.Background(), c)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must specify exactly 1 apiGroup when fields are defined")
}

// Reject a field with an empty name.
func TestRejectEmptyFieldName(t *testing.T) {
	c := validConfig()
	c.Spec.CollectionRules[0].Fields = []Field{{Name: "", JSONPath: "{.spec.replicas}"}}
	_, err := c.ValidateCreate(context.Background(), c)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "field name is required")
}

// Reject a field name that starts with a digit.
func TestRejectFieldNameStartingWithNumber(t *testing.T) {
	c := validConfig()
	c.Spec.CollectionRules[0].Fields = []Field{{Name: "1bad", JSONPath: "{.spec.replicas}"}}
	_, err := c.ValidateCreate(context.Background(), c)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must start with a letter")
}

// Reject a field name containing special characters.
func TestRejectFieldNameWithInvalidChars(t *testing.T) {
	c := validConfig()
	c.Spec.CollectionRules[0].Fields = []Field{{Name: "bad!name", JSONPath: "{.spec.replicas}"}}
	_, err := c.ValidateCreate(context.Background(), c)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must start with a letter")
}

// Reject a field with an empty jsonPath.
func TestRejectEmptyJSONPath(t *testing.T) {
	c := validConfig()
	c.Spec.CollectionRules[0].Fields = []Field{{Name: "status", JSONPath: ""}}
	_, err := c.ValidateCreate(context.Background(), c)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "jsonPath is required")
}

// Reject a jsonPath missing the {. } wrapper.
func TestRejectInvalidJSONPathMissingPrefix(t *testing.T) {
	c := validConfig()
	c.Spec.CollectionRules[0].Fields = []Field{{Name: "status", JSONPath: "spec.replicas"}}
	_, err := c.ValidateCreate(context.Background(), c)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must be a valid JSONPath expression")
}

// Reject a jsonPath with no path content inside the braces.
func TestRejectInvalidJSONPathEmptyInner(t *testing.T) {
	c := validConfig()
	c.Spec.CollectionRules[0].Fields = []Field{{Name: "status", JSONPath: "{.}"}}
	_, err := c.ValidateCreate(context.Background(), c)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must be a valid JSONPath expression")
}

// Reject a field with an unsupported type value.
func TestRejectInvalidFieldType(t *testing.T) {
	c := validConfig()
	c.Spec.CollectionRules[0].Fields = []Field{{Name: "status", JSONPath: "{.status}", Type: "boolean"}}
	_, err := c.ValidateCreate(context.Background(), c)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Unsupported value")
}

// Accept a field that omits the optional type.
func TestAcceptFieldWithNoType(t *testing.T) {
	c := validConfig()
	c.Spec.CollectionRules[0].Fields = []Field{{Name: "status", JSONPath: "{.status}"}}
	_, err := c.ValidateCreate(context.Background(), c)
	assert.NoError(t, err)
}

// Reject a fieldSuffix containing uppercase letters.
func TestRejectFieldSuffixWithUppercase(t *testing.T) {
	c := validConfig()
	c.Spec.CollectionRules[0].FieldSuffix = "Bad"
	_, err := c.ValidateCreate(context.Background(), c)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must contain only lowercase")
}

// Reject a fieldSuffix containing underscores.
func TestRejectFieldSuffixWithUnderscore(t *testing.T) {
	c := validConfig()
	c.Spec.CollectionRules[0].FieldSuffix = "bad_suffix"
	_, err := c.ValidateCreate(context.Background(), c)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must contain only lowercase")
}

// Accept a valid lowercase fieldSuffix with dots.
func TestAcceptValidFieldSuffix(t *testing.T) {
	c := validConfig()
	c.Spec.CollectionRules[0].FieldSuffix = "grc.v1"
	_, err := c.ValidateCreate(context.Background(), c)
	assert.NoError(t, err)
}

// Accept a fully populated rule with multiple fields and a suffix.
func TestAcceptValidConfigWithFieldsAndSuffix(t *testing.T) {
	c := validConfig()
	c.Spec.CollectionRules[0].Fields = []Field{
		{Name: "replicas", JSONPath: "{.status.replicas}", Type: DataTypeNumber},
		{Name: "image-name", JSONPath: "{.spec.containers[0].image}", Type: DataTypeString},
	}
	c.Spec.CollectionRules[0].FieldSuffix = "custom"
	_, err := c.ValidateCreate(context.Background(), c)
	assert.NoError(t, err)
}

// Accept a rule using the Exclude action.
func TestAcceptExcludeAction(t *testing.T) {
	c := validConfig()
	c.Spec.CollectionRules[0].Action = ActionExclude
	_, err := c.ValidateCreate(context.Background(), c)
	assert.NoError(t, err)
}

// Accept a rule with no fields defined.
func TestAcceptConfigWithNoFields(t *testing.T) {
	c := validConfig()
	_, err := c.ValidateCreate(context.Background(), c)
	assert.NoError(t, err)
}

// Accept a config with an empty collectionRules list.
func TestAcceptEmptyCollectionRules(t *testing.T) {
	c := validConfig()
	c.Spec.CollectionRules = []CollectionRule{}
	_, err := c.ValidateCreate(context.Background(), c)
	assert.NoError(t, err)
}

// Accept a config with multiple valid rules.
func TestAcceptMultipleValidRules(t *testing.T) {
	c := validConfig()
	c.Spec.CollectionRules = append(c.Spec.CollectionRules, CollectionRule{
		Action: ActionExclude,
		ResourceSelector: ResourceSelector{
			APIGroups: []string{""},
			Kinds:     []string{"Secret", "ConfigMap"},
		},
	})
	_, err := c.ValidateCreate(context.Background(), c)
	assert.NoError(t, err)
}

// Report multiple validation errors in a single response.
func TestReportMultipleErrors(t *testing.T) {
	c := validConfig()
	c.Spec.CollectionRules[0].ResourceSelector.APIGroups = []string{}
	c.Spec.CollectionRules[0].ResourceSelector.Kinds = []string{}
	_, err := c.ValidateCreate(context.Background(), c)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "apiGroups")
	assert.Contains(t, err.Error(), "kinds")
}

// ValidateUpdate rejects an invalid updated config.
func TestValidateUpdateRejectsInvalid(t *testing.T) {
	old := validConfig()
	updated := validConfig()
	updated.Spec.CollectionRules[0].ResourceSelector.APIGroups = []string{}
	_, err := updated.ValidateUpdate(context.Background(), old, updated)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must specify at least one apiGroup")
}

// ValidateUpdate accepts a valid updated config.
func TestValidateUpdateAcceptsValid(t *testing.T) {
	old := validConfig()
	updated := validConfig()
	_, err := updated.ValidateUpdate(context.Background(), old, updated)
	assert.NoError(t, err)
}

// ValidateDelete always succeeds (no-op).
func TestValidateDeleteAlwaysPasses(t *testing.T) {
	c := validConfig()
	_, err := c.ValidateDelete(context.Background(), c)
	assert.NoError(t, err)
}

// Accept collectConditions with a specific kind and no fields.
func TestAcceptCollectConditionsWithKind(t *testing.T) {
	collectConditions := true
	c := validConfig()
	c.Spec.CollectionRules[0].CollectConditions = &collectConditions
	_, err := c.ValidateCreate(context.Background(), c)
	assert.NoError(t, err)
}

// Accept collectConditions with multiple kinds and no fields.
func TestAcceptCollectConditionsWithMultipleKinds(t *testing.T) {
	collectConditions := true
	c := validConfig()
	c.Spec.CollectionRules[0].CollectConditions = &collectConditions
	c.Spec.CollectionRules[0].ResourceSelector.Kinds = []string{"Deployment", "StatefulSet"}
	_, err := c.ValidateCreate(context.Background(), c)
	assert.NoError(t, err)
}

// Accept collectConditions with wildcard kinds (apigroup-wide conditions).
func TestAcceptCollectConditionsWithWildcardKind(t *testing.T) {
	collectConditions := true
	c := validConfig()
	c.Spec.CollectionRules[0].CollectConditions = &collectConditions
	c.Spec.CollectionRules[0].ResourceSelector.Kinds = []string{"*"}
	_, err := c.ValidateCreate(context.Background(), c)
	assert.NoError(t, err)
}

// Accept wildcard kinds without collectConditions (no-op rule, but valid).
func TestAcceptWildcardKindWithoutCollectConditions(t *testing.T) {
	c := validConfig()
	c.Spec.CollectionRules[0].ResourceSelector.Kinds = []string{"*"}
	_, err := c.ValidateCreate(context.Background(), c)
	assert.NoError(t, err)
}

// Reject wildcard kinds with fields.
func TestRejectWildcardKindWithFields(t *testing.T) {
	collectConditions := true
	c := validConfig()
	c.Spec.CollectionRules[0].CollectConditions = &collectConditions
	c.Spec.CollectionRules[0].ResourceSelector.Kinds = []string{"*"}
	c.Spec.CollectionRules[0].Fields = []Field{{Name: "replicas", JSONPath: "{.spec.replicas}"}}
	_, err := c.ValidateCreate(context.Background(), c)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "wildcard kind")
}

// Reject collectConditions without apiGroups.
func TestRejectCollectConditionsWithoutApiGroups(t *testing.T) {
	collectConditions := true
	c := validConfig()
	c.Spec.CollectionRules[0].CollectConditions = &collectConditions
	c.Spec.CollectionRules[0].ResourceSelector.APIGroups = []string{}
	c.Spec.CollectionRules[0].ResourceSelector.Kinds = []string{"*"}
	_, err := c.ValidateCreate(context.Background(), c)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must specify at least one apiGroup")
}

// --- Webhook protection tests ---

func ctxWithUser(username string) context.Context {
	return admission.NewContextWithRequest(context.Background(), admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			UserInfo: authenticationv1.UserInfo{Username: username},
		},
	})
}

func operatorCtx() context.Context {
	return ctxWithUser("system:serviceaccount:open-cluster-management:search-serviceaccount")
}

func nonOperatorCtx() context.Context {
	return ctxWithUser("system:serviceaccount:open-cluster-management:default")
}

// Non-operator creating merged-collector-config → rejected.
func TestRejectNonOperatorCreateMerged(t *testing.T) {
	c := validConfig()
	c.Name = "merged-collector-config"
	_, err := c.ValidateCreate(nonOperatorCtx(), c)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "managed by the search operator")
}

// Non-operator creating integration-collector-config → rejected.
func TestRejectNonOperatorCreateIntegration(t *testing.T) {
	c := validConfig()
	c.Name = "integration-collector-config"
	_, err := c.ValidateCreate(nonOperatorCtx(), c)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "managed by the search operator")
}

// Non-operator updating merged-collector-config → rejected.
func TestRejectNonOperatorUpdateMerged(t *testing.T) {
	old := validConfig()
	old.Name = "merged-collector-config"
	updated := old.DeepCopy()
	_, err := updated.ValidateUpdate(nonOperatorCtx(), old, updated)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "managed by the search operator")
}

// Non-operator deleting merged-collector-config → rejected.
func TestRejectNonOperatorDeleteMerged(t *testing.T) {
	c := validConfig()
	c.Name = "merged-collector-config"
	_, err := c.ValidateDelete(nonOperatorCtx(), c)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "managed by the search operator")
}

// Non-operator deleting integration-collector-config → rejected.
func TestRejectNonOperatorDeleteIntegration(t *testing.T) {
	c := validConfig()
	c.Name = "integration-collector-config"
	_, err := c.ValidateDelete(nonOperatorCtx(), c)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "managed by the search operator")
}

// Non-operator creating customer-collector-config → allowed.
func TestAllowNonOperatorCreateCustomer(t *testing.T) {
	c := validConfig()
	c.Name = "customer-collector-config"
	_, err := c.ValidateCreate(nonOperatorCtx(), c)
	assert.NoError(t, err)
}

// Operator SA creating merged-collector-config → allowed.
func TestAllowOperatorCreateMerged(t *testing.T) {
	c := validConfig()
	c.Name = "merged-collector-config"
	_, err := c.ValidateCreate(operatorCtx(), c)
	assert.NoError(t, err)
}

// Operator SA updating integration-collector-config → allowed.
func TestAllowOperatorUpdateIntegration(t *testing.T) {
	old := validConfig()
	old.Name = "integration-collector-config"
	updated := old.DeepCopy()
	_, err := updated.ValidateUpdate(operatorCtx(), old, updated)
	assert.NoError(t, err)
}

// Operator SA deleting protected configs → allowed.
func TestAllowOperatorDeleteProtected(t *testing.T) {
	c := validConfig()
	c.Name = "merged-collector-config"
	_, err := c.ValidateDelete(operatorCtx(), c)
	assert.NoError(t, err)
}
