// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"testing"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
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

// containsPort returns true if the given port list contains the given TCP port.
func containsPort(ports []networkingv1.NetworkPolicyPort, port int32) bool {
	for _, p := range ports {
		if p.Port != nil && p.Port.IntVal == port {
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

	policies := r.NetworkPolicies(search)
	assert.Len(t, policies, 5, "expected one NetworkPolicy per Search component")

	names := map[string]bool{}
	for _, np := range policies {
		names[np.Name] = true
		// Every policy must be namespaced with the Search instance and own an owner reference.
		assert.Equal(t, search.Namespace, np.Namespace)
		assert.NotEmpty(t, np.OwnerReferences, "NetworkPolicy %s should be owned by the Search CR", np.Name)
		// Every policy must restrict both ingress and egress (default-deny unless allowed).
		assert.ElementsMatch(t, []networkingv1.PolicyType{
			networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress,
		}, np.Spec.PolicyTypes)
		// Every policy must scope to a specific set of pods, never the whole namespace.
		assert.NotEmpty(t, np.Spec.PodSelector.MatchLabels, "NetworkPolicy %s must not select all pods", np.Name)
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
	assert.True(t, containsPort(ingress.Ports, postgresPort))
	assert.True(t, containsPodSelectorLabel(ingress.From, "name", indexerDeploymentName), "indexer must reach postgres")
	assert.True(t, containsPodSelectorLabel(ingress.From, "name", apiDeploymentName), "api must reach postgres")
	assert.True(t, containsPodSelectorLabel(ingress.From, "app.kubernetes.io/name", "acm-mcp-server"),
		"mcp-server must reach postgres for read-only queries")
}

func TestIndexerNetworkPolicy(t *testing.T) {
	search := testSearchInstance()
	r := newTestReconcilerForNetworkPolicies(t, search)

	np := r.IndexerNetworkPolicy(search)

	assert.Equal(t, indexerDeploymentName, np.Spec.PodSelector.MatchLabels["name"])

	// Ingress: from kube-apiserver (proxied collector traffic) and monitoring (metrics).
	var sawAPIServer, sawMonitoring bool
	for _, rule := range np.Spec.Ingress {
		if containsNamespaceSelector(rule.From, openshiftKubeAPIServer) && containsPort(rule.Ports, indexerPort) {
			sawAPIServer = true
		}
		if containsNamespaceSelector(rule.From, openshiftMonitoring) && containsPort(rule.Ports, indexerPort) {
			sawMonitoring = true
		}
	}
	assert.True(t, sawAPIServer, "expected ingress from kube-apiserver namespace")
	assert.True(t, sawMonitoring, "expected ingress from openshift-monitoring namespace")

	// Egress: to postgres, kube-apiserver, and DNS.
	var sawPostgres, sawAPIServerEgress, sawDNS bool
	for _, rule := range np.Spec.Egress {
		if containsPodSelectorLabel(rule.To, "name", postgresDeploymentName) && containsPort(rule.Ports, postgresPort) {
			sawPostgres = true
		}
		if containsNamespaceSelector(rule.To, openshiftKubeAPIServer) {
			sawAPIServerEgress = true
		}
		if containsNamespaceSelector(rule.To, openshiftDNS) {
			sawDNS = true
		}
	}
	assert.True(t, sawPostgres, "expected egress to search-postgres")
	assert.True(t, sawAPIServerEgress, "expected egress to kube-apiserver")
	assert.True(t, sawDNS, "expected egress to DNS")
}

func TestAPINetworkPolicy(t *testing.T) {
	search := testSearchInstance()
	r := newTestReconcilerForNetworkPolicies(t, search)

	np := r.APINetworkPolicy(search)

	assert.Equal(t, apiDeploymentName, np.Spec.PodSelector.MatchLabels["name"])

	var sawSameNamespace, sawMonitoring bool
	for _, rule := range np.Spec.Ingress {
		if containsPort(rule.Ports, apiPort) {
			for _, from := range rule.From {
				if from.PodSelector != nil && from.NamespaceSelector == nil {
					sawSameNamespace = true
				}
			}
			if containsNamespaceSelector(rule.From, openshiftMonitoring) {
				sawMonitoring = true
			}
		}
	}
	assert.True(t, sawSameNamespace, "expected ingress from same-namespace consumers (e.g. console-api)")
	assert.True(t, sawMonitoring, "expected ingress from openshift-monitoring namespace")

	var sawPostgres, sawAPIServerEgress, sawDNS bool
	for _, rule := range np.Spec.Egress {
		if containsPodSelectorLabel(rule.To, "name", postgresDeploymentName) {
			sawPostgres = true
		}
		if containsNamespaceSelector(rule.To, openshiftKubeAPIServer) {
			sawAPIServerEgress = true
		}
		if containsNamespaceSelector(rule.To, openshiftDNS) {
			sawDNS = true
		}
	}
	assert.True(t, sawPostgres)
	assert.True(t, sawAPIServerEgress)
	assert.True(t, sawDNS)
}

func TestCollectorNetworkPolicy(t *testing.T) {
	search := testSearchInstance()
	r := newTestReconcilerForNetworkPolicies(t, search)

	np := r.CollectorNetworkPolicy(search)

	assert.Equal(t, collectorDeploymentName, np.Spec.PodSelector.MatchLabels["name"])
	assert.Len(t, np.Spec.Ingress, 1)
	assert.True(t, containsNamespaceSelector(np.Spec.Ingress[0].From, openshiftMonitoring))
	assert.True(t, containsPort(np.Spec.Ingress[0].Ports, collectorPort))

	var sawIndexer, sawAPIServerEgress, sawDNS bool
	for _, rule := range np.Spec.Egress {
		if containsPodSelectorLabel(rule.To, "name", indexerDeploymentName) {
			sawIndexer = true
		}
		if containsNamespaceSelector(rule.To, openshiftKubeAPIServer) {
			sawAPIServerEgress = true
		}
		if containsNamespaceSelector(rule.To, openshiftDNS) {
			sawDNS = true
		}
	}
	assert.True(t, sawIndexer, "expected egress to search-indexer")
	assert.True(t, sawAPIServerEgress)
	assert.True(t, sawDNS)
}

func TestOperatorNetworkPolicy(t *testing.T) {
	search := testSearchInstance()
	r := newTestReconcilerForNetworkPolicies(t, search)

	np := r.OperatorNetworkPolicy(search)

	assert.Equal(t, "controller-manager", np.Spec.PodSelector.MatchLabels["control-plane"])

	var sawWebhook, sawMonitoring bool
	for _, rule := range np.Spec.Ingress {
		if containsNamespaceSelector(rule.From, openshiftKubeAPIServer) && containsPort(rule.Ports, operatorWebhookPort) {
			sawWebhook = true
		}
		if containsNamespaceSelector(rule.From, openshiftMonitoring) && containsPort(rule.Ports, operatorMetricsPort) {
			sawMonitoring = true
		}
	}
	assert.True(t, sawWebhook, "expected ingress from kube-apiserver for admission webhook calls")
	assert.True(t, sawMonitoring, "expected ingress from openshift-monitoring for metrics")

	var sawAPIServerEgress, sawDNS bool
	for _, rule := range np.Spec.Egress {
		if containsNamespaceSelector(rule.To, openshiftKubeAPIServer) {
			sawAPIServerEgress = true
		}
		if containsNamespaceSelector(rule.To, openshiftDNS) {
			sawDNS = true
		}
	}
	assert.True(t, sawAPIServerEgress)
	assert.True(t, sawDNS)
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
