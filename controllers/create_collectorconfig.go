// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"context"
	"fmt"
	"sort"
	"strings"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// addBackupLabel patches the backup label onto a CollectorConfig if it is missing.
// The search.open-cluster-management.io API group is excluded from the automatic resources
// backup (backup.go excludedAPIGroups). Resources labeled with backupLabel are picked up by
// the acm-resources-generic-schedule backup instead. All source CollectorConfigs — integration
// team configs, user-collector-config, and any future per-cluster override configs — should
// carry this label so they survive a hub backup/restore cycle.
func (r *SearchReconciler) addBackupLabel(ctx context.Context, cc *searchv1alpha1.CollectorConfig) error {
	if _, hasLabel := cc.Labels[backupLabel]; hasLabel {
		return nil
	}
	patch := client.MergeFrom(cc.DeepCopy())
	if cc.Labels == nil {
		cc.Labels = map[string]string{}
	}
	cc.Labels[backupLabel] = ""
	if err := r.Patch(ctx, cc, patch); err != nil {
		log.Error(err, "Could not add backup label to CollectorConfig", "name", cc.Name)
		return err
	}
	log.V(2).Info("Added backup label to CollectorConfig", "name", cc.Name)
	return nil
}

const (
	userCollectorConfigName   = "user-collector-config"
	mergedCollectorConfigName = "merged-collector-config"

	webhookConfigName     = "search-v2-operator-validating-webhook-configuration"
	caInjectionAnnotation = "service.beta.openshift.io/inject-cabundle"
)

// ensureWebhookCAInjection ensures the ValidatingWebhookConfiguration has the
// OpenShift service-ca CA injection annotation. OLM creates the VWC from the
// CSV webhookdefinitions, which doesn't support custom annotations. Without
// this annotation, the service-ca controller won't inject the CA bundle and
// the webhook TLS handshake will fail.
func (r *SearchReconciler) ensureWebhookCAInjection(ctx context.Context) error {
	vwc := &admissionregistrationv1.ValidatingWebhookConfiguration{}
	err := r.Get(ctx, types.NamespacedName{Name: webhookConfigName}, vwc)
	if err != nil {
		if errors.IsNotFound(err) {
			log.V(2).Info("ValidatingWebhookConfiguration not found, skipping CA injection annotation")
			return nil
		}
		return err
	}

	if vwc.Annotations[caInjectionAnnotation] == "true" {
		log.V(2).Info("Webhook CA injection annotation already present")
		return nil
	}

	if vwc.Annotations == nil {
		vwc.Annotations = map[string]string{}
	}
	vwc.Annotations[caInjectionAnnotation] = "true"

	log.Info("Adding webhook CA injection annotation to ValidatingWebhookConfiguration")
	return r.Update(ctx, vwc, &client.UpdateOptions{})
}

// excludeOverlapsIntegrationIncludes reports whether a user exclude rule targets
// resources that any integration team include rule also covers.
// Two rules overlap when their apiGroups and kinds are not disjoint — i.e. there
// exists at least one resource matched by both selectors (wildcards "*" match all).
// When overlap is detected, the integration team include takes precedence and the
// user exclude is dropped from the merged spec.
func excludeOverlapsIntegrationIncludes(
	excludeRule searchv1alpha1.CollectionRule,
	teamConfigs []searchv1alpha1.CollectorConfig,
) bool {
	for _, tc := range teamConfigs {
		for _, teamRule := range tc.Spec.CollectionRules {
			if teamRule.Action != searchv1alpha1.ActionInclude {
				continue
			}
			if rulesOverlap(excludeRule.ResourceSelector, teamRule.ResourceSelector) {
				return true
			}
		}
	}
	return false
}

// rulesOverlap returns true when two ResourceSelectors could match the same resource.
// Wildcards ("*") in either selector match all values on the other side.
func rulesOverlap(a, b searchv1alpha1.ResourceSelector) bool {
	if !setsIntersect(a.APIGroups, b.APIGroups) {
		return false
	}
	return setsIntersect(a.Kinds, b.Kinds)
}

// setsIntersect returns true when two string slices share at least one element,
// treating "*" as a universal match for all values on the other side.
func setsIntersect(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == "*" || y == "*" || x == y {
				return true
			}
		}
	}
	return false
}

// updateUserCCStatus writes an Applied status condition on user-collector-config to surface
// any exclude rules that were silently dropped during the merge because integration team
// include rules take precedence. When no rules were dropped the condition is set to True.
func (r *SearchReconciler) updateUserCCStatus(
	ctx context.Context,
	userCC *searchv1alpha1.CollectorConfig,
	droppedMessages []string,
) error {
	conditionStatus := metav1.ConditionTrue
	reason := searchv1alpha1.CollectorConfigReasonApplied
	message := "Configuration merged successfully."

	if len(droppedMessages) > 0 {
		conditionStatus = metav1.ConditionFalse
		reason = searchv1alpha1.CollectorConfigReasonRulesSkipped
		message = strings.Join(droppedMessages, "; ")
	}

	newCondition := metav1.Condition{
		Type:               searchv1alpha1.CollectorConfigConditionApplied,
		Status:             conditionStatus,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	}

	existing := apimeta.FindStatusCondition(userCC.Status.Conditions, searchv1alpha1.CollectorConfigConditionApplied)
	if existing != nil && existing.Status == conditionStatus {
		newCondition.LastTransitionTime = existing.LastTransitionTime
	}

	apimeta.SetStatusCondition(&userCC.Status.Conditions, newCondition)

	if err := r.Status().Update(ctx, userCC); err != nil {
		return err
	}
	return nil
}

// createOrUpdateMergedCollectorConfig discovers all integration team CollectorConfig CRs (by label)
// and the user-collector-config (by name), merges their CollectionRules, and writes the result
// to merged-collector-config.
func (r *SearchReconciler) createOrUpdateMergedCollectorConfig(
	ctx context.Context,
	instance *searchv1alpha1.Search,
) (*reconcile.Result, error) {
	namespace := instance.GetNamespace()

	// List all integration team CollectorConfigs by label.
	teamConfigs := &searchv1alpha1.CollectorConfigList{}
	err := r.List(ctx, teamConfigs,
		client.InNamespace(namespace),
		client.MatchingLabels{searchv1alpha1.IntegrationTeamLabel: searchv1alpha1.IntegrationTeamLabelValue},
	)
	if err != nil {
		log.Error(err, "Could not list integration team CollectorConfigs")
		return &reconcile.Result{}, err
	}

	// Sort by name for deterministic merge order.
	sort.Slice(teamConfigs.Items, func(i, j int) bool {
		return teamConfigs.Items[i].Name < teamConfigs.Items[j].Name
	})

	// FUTURE: detect and handle rule collisions between integration team configs.

	// Build merged spec: integration team rules first, then user rules.
	// Also ensure each source config carries the backup label so it survives hub backup/restore.
	mergedSpec := searchv1alpha1.CollectorConfigSpec{}
	for i := range teamConfigs.Items {
		tc := &teamConfigs.Items[i]
		mergedSpec.CollectionRules = append(mergedSpec.CollectionRules, tc.Spec.CollectionRules...)
		if err := r.addBackupLabel(ctx, tc); err != nil {
			return &reconcile.Result{}, err
		}
	}

	// Get user-collector-config. Not found is fine, user may not have created one.
	userCC := &searchv1alpha1.CollectorConfig{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      userCollectorConfigName,
		Namespace: namespace,
	}, userCC)
	if err != nil && !errors.IsNotFound(err) {
		return &reconcile.Result{}, err
	}
	if err == nil {
		var droppedRuleMessages []string
		for _, rule := range userCC.Spec.CollectionRules {
			// Integration team wins: drop user exclude rules that overlap with any
			// integration team include so they cannot suppress integration-required collection.
			if rule.Action == searchv1alpha1.ActionExclude &&
				excludeOverlapsIntegrationIncludes(rule, teamConfigs.Items) {
				msg := fmt.Sprintf(
					"exclude rule for kinds %v (apiGroups %v) was not applied — "+
						"resource is necessary for system functionality",
					rule.ResourceSelector.Kinds, rule.ResourceSelector.APIGroups)
				log.Info("Skipping user exclude rule — resource necessary for system functionality",
					"kinds", rule.ResourceSelector.Kinds,
					"apiGroups", rule.ResourceSelector.APIGroups)
				droppedRuleMessages = append(droppedRuleMessages, msg)
				continue
			}
			mergedSpec.CollectionRules = append(mergedSpec.CollectionRules, rule)
		}
		if userCC.Spec.CollectNamespaces != nil {
			mergedSpec.CollectNamespaces = userCC.Spec.CollectNamespaces.DeepCopy()
		}
		if err := r.addBackupLabel(ctx, userCC); err != nil {
			return &reconcile.Result{}, err
		}
		// Surface dropped rules to the user via the Applied status condition on user-collector-config.
		if err := r.updateUserCCStatus(ctx, userCC, droppedRuleMessages); err != nil {
			log.Error(err, "Could not update user-collector-config status after dropping rules")
		}
	}

	// Ensure non-nil slice so DeepEqual works consistently.
	if mergedSpec.CollectionRules == nil {
		mergedSpec.CollectionRules = []searchv1alpha1.CollectionRule{}
	}

	// Get or create merged-collector-config.
	found := &searchv1alpha1.CollectorConfig{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      mergedCollectorConfigName,
		Namespace: namespace,
	}, found)
	if err != nil && errors.IsNotFound(err) {
		merged := &searchv1alpha1.CollectorConfig{
			TypeMeta: metav1.TypeMeta{
				Kind:       "CollectorConfig",
				APIVersion: searchv1alpha1.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      mergedCollectorConfigName,
				Namespace: namespace,
			},
			Spec: mergedSpec,
		}
		if errRef := controllerutil.SetControllerReference(instance, merged, r.Scheme); errRef != nil {
			log.Error(errRef, "Could not set controller reference for merged-collector-config")
			return &reconcile.Result{}, errRef
		}
		err = r.Create(ctx, merged)
		if err != nil {
			log.Error(err, "Could not create merged-collector-config")
			return &reconcile.Result{}, err
		}
		log.V(2).Info("Created merged-collector-config", "ruleCount", len(mergedSpec.CollectionRules))
		return nil, nil
	} else if err != nil {
		return &reconcile.Result{}, err
	}

	// Update only if the spec has changed.
	if !equality.Semantic.DeepEqual(found.Spec, mergedSpec) {
		found.Spec = mergedSpec
		if err := r.Update(ctx, found); err != nil {
			log.Error(err, "Could not update merged-collector-config")
			return &reconcile.Result{}, err
		}
		log.V(2).Info("Updated merged-collector-config", "ruleCount", len(mergedSpec.CollectionRules))
	}

	return nil, nil
}
