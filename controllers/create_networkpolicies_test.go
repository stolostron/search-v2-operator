// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"testing"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newTestReconcilerForNetworkPolicies(t *testing.T, search *searchv1alpha1.Search) *SearchReconciler {
	t.Helper()
	s := scheme.Scheme
	err := searchv1alpha1.SchemeBuilder.AddToScheme(s)
	assert.NoError(t, err)

	objs := []runtime.Object{search}
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()
	return &SearchReconciler{Client: cl, Scheme: s}
}

func testSearchInstance() *searchv1alpha1.Search {
	return &searchv1alpha1.Search{
		TypeMeta:   metav1.TypeMeta{Kind: "Search"},
		ObjectMeta: metav1.ObjectMeta{Name: "search-v2-operator", Namespace: "test-namespace"},
		Spec:       searchv1alpha1.SearchSpec{},
	}
}

// containsTCPPort returns true only if the given port list contains an entry that is both
// TCP and the given port number, so a rule that (incorrectly) allowed UDP or a different port
// number doesn't pass this check by accident.
func containsTCPPort(ports []networkingv1.NetworkPolicyPort, port int32) bool {
	for _, p := range ports {
		if p.Port != nil && p.Port.IntVal == port &&
			p.Protocol != nil && *p.Protocol == corev1.ProtocolTCP {
			return true
		}
	}
	return false
}

// containsNamespaceSelector returns true if any of the peers selects the given namespace name
// via the well-known kubernetes.io/metadata.name label.
func containsNamespaceSelector(peers []networkingv1.NetworkPolicyPeer, namespaceName string) bool {
	for _, p := range peers {
		if p.NamespaceSelector != nil && p.NamespaceSelector.MatchLabels[nsLabelKey] == namespaceName {
			return true
		}
	}
	return false
}

// containsNamespaceAndPodSelector returns true if any peer combines a namespace selector for the
// given namespace name with a pod selector matching the given label key/value pair.
func containsNamespaceAndPodSelector(peers []networkingv1.NetworkPolicyPeer, namespaceName, podLabelKey, podLabelValue string) bool {
	for _, p := range peers {
		if p.NamespaceSelector != nil && p.NamespaceSelector.MatchLabels[nsLabelKey] == namespaceName &&
			p.PodSelector != nil && p.PodSelector.MatchLabels[podLabelKey] == podLabelValue {
			return true
		}
	}
	return false
}

// containsPodSelectorLabel returns true if any of the peers selects pods with the given label
// key/value pair.
func containsPodSelectorLabel(peers []networkingv1.NetworkPolicyPeer, key, value string) bool {
	for _, p := range peers {
		if p.PodSelector != nil && p.PodSelector.MatchLabels[key] == value {
			return true
		}
	}
	return false
}

func TestNetworkPolicies_AllComponentsPresent(t *testing.T) {
	search := testSearchInstance()
	r := newTestReconcilerForNetworkPolicies(t, search)

	policies := r.NetworkPolicies(t.Context(), search)
	assert.Len(t, policies, 5, "expected one NetworkPolicy per Search component")

	// postgresNetworkPolicyName has both Ingress and Egress policyTypes (deny-all egress).
	// All other components use Ingress-only (OVN-K cannot match ClusterIP in egress rules).
	ingressEgressPolicies := map[string]bool{
		networkPolicyName(postgresDeploymentName): true,
	}

	names := map[string]bool{}
	for _, np := range policies {
		names[np.Name] = true
		// Every policy must be namespaced with the Search instance and own an owner reference.
		assert.Equal(t, search.Namespace, np.Namespace)
		assert.NotEmpty(t, np.OwnerReferences, "NetworkPolicy %s should be owned by the Search CR", np.Name)
		// Every policy must scope to a specific set of pods, never the whole namespace.
		assert.NotEmpty(t, np.Spec.PodSelector.MatchLabels, "NetworkPolicy %s must not select all pods", np.Name)
		// Postgres restricts both ingress and egress; others restrict ingress only.
		if ingressEgressPolicies[np.Name] {
			assert.ElementsMatch(t, []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress,
			}, np.Spec.PolicyTypes, "postgres policy must set both Ingress and Egress")
		} else {
			assert.ElementsMatch(t, []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
			}, np.Spec.PolicyTypes, "non-postgres policy %s must use Ingress-only (no Egress)", np.Name)
		}
	}

	assert.True(t, names[networkPolicyName(postgresDeploymentName)])
	assert.True(t, names[networkPolicyName(indexerDeploymentName)])
	assert.True(t, names[networkPolicyName(apiDeploymentName)])
	assert.True(t, names[networkPolicyName(collectorDeploymentName)])
	assert.True(t, names[networkPolicyName("search-operator")])
}

func TestPostgresNetworkPolicy(t *testing.T) {
	search := testSearchInstance()
	r := newTestReconcilerForNetworkPolicies(t, search)

	np := r.PostgresNetworkPolicy(search)

	assert.Equal(t, postgresDeploymentName, np.Spec.PodSelector.MatchLabels["name"])
	assert.Empty(t, np.Spec.Egress, "postgres never initiates outbound connections")
	assert.Len(t, np.Spec.Ingress, 1)

	ingress := np.Spec.Ingress[0]
	assert.True(t, containsTCPPort(ingress.Ports, postgresPort))
	assert.True(t, containsPodSelectorLabel(ingress.From, "name", indexerDeploymentName), "indexer must reach postgres")
	assert.True(t, containsPodSelectorLabel(ingress.From, "name", apiDeploymentName), "api must reach postgres")
	assert.True(t, containsPodSelectorLabel(ingress.From, "app.kubernetes.io/name", "acm-mcp-server"),
		"mcp-server must reach postgres for read-only queries")
}

func TestIndexerNetworkPolicy(t *testing.T) {
	search := testSearchInstance()
	r := newTestReconcilerForNetworkPolicies(t, search)

	np := r.IndexerNetworkPolicy(search, "multicluster-engine")

	assert.Equal(t, indexerDeploymentName, np.Spec.PodSelector.MatchLabels["name"])

	// Ingress: from ocm-proxyserver (proxied managed-cluster collector traffic), the hub-local
	// collector (direct, same-namespace connection), and monitoring (metrics).
	var sawProxyServer, sawHubCollector, sawMonitoring bool
	for _, rule := range np.Spec.Ingress {
		if containsNamespaceAndPodSelector(rule.From, multiclusterEngine, "control-plane", "ocm-proxyserver") && containsTCPPort(rule.Ports, indexerPort) {
			sawProxyServer = true
		}
		if containsPodSelectorLabel(rule.From, "name", collectorDeploymentName) && containsTCPPort(rule.Ports, indexerPort) {
			sawHubCollector = true
		}
		if containsNamespaceSelector(rule.From, openshiftMonitoring) && containsTCPPort(rule.Ports, indexerPort) {
			sawMonitoring = true
		}
	}
	assert.True(t, sawProxyServer, "expected ingress from ocm-proxyserver in multicluster-engine namespace (proxied managed-cluster collectors)")
	assert.True(t, sawHubCollector, "expected ingress from hub-local collector pod (direct, same-namespace connection)")
	assert.True(t, sawMonitoring, "expected ingress from openshift-monitoring namespace")

	// Egress policyType is NOT set for indexer: OVN-Kubernetes cannot match
	// kubernetes.default.svc ClusterIP traffic in NetworkPolicy egress rules, so applying
	// Egress policyType would block the indexer from reaching the Kubernetes API.
	assert.NotContains(t, np.Spec.PolicyTypes, networkingv1.PolicyTypeEgress,
		"indexer NetworkPolicy must not set Egress policyType (would block kube API access)")
}

func TestAPINetworkPolicy(t *testing.T) {
	search := testSearchInstance()
	r := newTestReconcilerForNetworkPolicies(t, search)

	np := r.APINetworkPolicy(search, "multicluster-engine")

	assert.Equal(t, apiDeploymentName, np.Spec.PodSelector.MatchLabels["name"])

	var sawSameNamespace, sawConsoleMCE, sawMonitoring bool
	for _, rule := range np.Spec.Ingress {
		if containsTCPPort(rule.Ports, apiPort) {
			for _, from := range rule.From {
				if from.PodSelector != nil && from.NamespaceSelector == nil {
					sawSameNamespace = true
				}
			}
			if containsNamespaceAndPodSelector(rule.From, multiclusterEngine, "app", "console-mce") {
				sawConsoleMCE = true
			}
			if containsNamespaceSelector(rule.From, openshiftMonitoring) {
				sawMonitoring = true
			}
		}
	}
	assert.True(t, sawSameNamespace, "expected ingress from same-namespace consumers (e.g. console-api)")
	assert.True(t, sawConsoleMCE, "expected ingress from console-mce in multicluster-engine namespace")
	assert.True(t, sawMonitoring, "expected ingress from openshift-monitoring namespace")

	// Egress policyType is NOT set for api: OVN-Kubernetes cannot match
	// kubernetes.default.svc ClusterIP traffic in NetworkPolicy egress rules, so applying
	// Egress policyType would block the api from reaching the Kubernetes API.
	assert.NotContains(t, np.Spec.PolicyTypes, networkingv1.PolicyTypeEgress,
		"api NetworkPolicy must not set Egress policyType (would block kube API access)")
}

func TestCollectorNetworkPolicy(t *testing.T) {
	search := testSearchInstance()
	r := newTestReconcilerForNetworkPolicies(t, search)

	np := r.CollectorNetworkPolicy(search)

	assert.Equal(t, collectorDeploymentName, np.Spec.PodSelector.MatchLabels["name"])
	assert.Len(t, np.Spec.Ingress, 1)
	assert.True(t, containsNamespaceSelector(np.Spec.Ingress[0].From, openshiftMonitoring))
	assert.True(t, containsTCPPort(np.Spec.Ingress[0].Ports, collectorPort))

	// Egress policyType is NOT set for collector: OVN-Kubernetes cannot match
	// kubernetes.default.svc ClusterIP traffic in NetworkPolicy egress rules, so applying
	// Egress policyType would block the collector from reaching the Kubernetes API.
	assert.NotContains(t, np.Spec.PolicyTypes, networkingv1.PolicyTypeEgress,
		"collector NetworkPolicy must not set Egress policyType (would block kube API access)")
}

func TestOperatorNetworkPolicy(t *testing.T) {
	search := testSearchInstance()
	r := newTestReconcilerForNetworkPolicies(t, search)

	np := r.OperatorNetworkPolicy(search)

	assert.Equal(t, "controller-manager", np.Spec.PodSelector.MatchLabels["control-plane"])

	var sawWebhookOpenToAll, sawMetricsOpenToAll, sawMonitoring bool
	for _, rule := range np.Spec.Ingress {
		// Webhook rule must have NO From restriction (empty From = allow all sources).
		// The kube-apiserver uses hostNetwork: true, making its traffic unmatchable by
		// namespace/pod selectors. Webhook security relies on the Kubernetes admission
		// control model (only the API server routes to webhook services).
		if len(rule.From) == 0 && containsTCPPort(rule.Ports, operatorWebhookPort) {
			sawWebhookOpenToAll = true
		}
		if len(rule.From) == 0 && containsTCPPort(rule.Ports, operatorMetricsPort) {
			sawMetricsOpenToAll = true
		}
		if containsNamespaceSelector(rule.From, openshiftMonitoring) && containsTCPPort(rule.Ports, operatorMetricsPort) {
			sawMonitoring = true
		}
	}
	assert.True(t, sawWebhookOpenToAll,
		"expected webhook ingress with empty From (API server uses hostNetwork, cannot be selector-matched)")
	assert.False(t, sawMetricsOpenToAll,
		"metrics port must NOT have unrestricted ingress — only openshift-monitoring should reach it")
	assert.True(t, sawMonitoring, "expected ingress from openshift-monitoring for metrics")

	// Egress policyType is NOT set for the operator: OVN-Kubernetes cannot match
	// kubernetes.default.svc ClusterIP traffic in NetworkPolicy egress rules, so applying
	// Egress policyType would block the operator from reaching the Kubernetes API.
	assert.NotContains(t, np.Spec.PolicyTypes, networkingv1.PolicyTypeEgress,
		"operator NetworkPolicy must not set Egress policyType (would block kube API access)")
}

func TestReconcileNetworkPolicies_CreatesAndUpdates(t *testing.T) {
	search := testSearchInstance()
	r := newTestReconcilerForNetworkPolicies(t, search)
	ctx := t.Context()

	result, err := r.reconcileNetworkPolicies(ctx, search)
	assert.NoError(t, err)
	assert.Nil(t, result)

	npList := &networkingv1.NetworkPolicyList{}
	assert.NoError(t, r.List(ctx, npList))
	assert.Len(t, npList.Items, 5)

	// Reconciling again should be a no-op (idempotent) and not return an error.
	result, err = r.reconcileNetworkPolicies(ctx, search)
	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestCreateOrUpdateNetworkPolicy_RepairsDriftedLabelsAndOwnerRefs(t *testing.T) {
	search := testSearchInstance()
	r := newTestReconcilerForNetworkPolicies(t, search)
	ctx := t.Context()

	desired := r.PostgresNetworkPolicy(search)
	result, err := r.createOrUpdateNetworkPolicy(ctx, desired)
	assert.NoError(t, err)
	assert.Nil(t, result)

	// Simulate drift: someone strips the managed labels and owner reference directly on
	// the cluster (e.g. via kubectl edit), leaving the Spec untouched.
	found := &networkingv1.NetworkPolicy{}
	assert.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(desired), found))
	found.Labels = nil
	found.OwnerReferences = nil
	assert.NoError(t, r.Update(ctx, found))

	// Reconciling again must repair the drifted labels/ownerRefs even though Spec is
	// unchanged, so the policy is correctly identified and garbage-collected with Search.
	result, err = r.createOrUpdateNetworkPolicy(ctx, desired)
	assert.NoError(t, err)
	assert.Nil(t, result)

	repaired := &networkingv1.NetworkPolicy{}
	assert.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(desired), repaired))
	assert.Equal(t, desired.Labels, repaired.Labels)
	assert.NotEmpty(t, repaired.OwnerReferences)
}
