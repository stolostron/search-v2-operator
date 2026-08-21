// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// Well-known OpenShift/Kubernetes namespaces referenced by the Search NetworkPolicies below.
// `kubernetes.io/metadata.name` is applied automatically by the API server to every namespace
// (since Kubernetes 1.21), so it is a safe, immutable label to select on.
const (
	nsLabelKey             = "kubernetes.io/metadata.name"
	openshiftKubeAPIServer = "openshift-kube-apiserver"
	openshiftMonitoring    = "openshift-monitoring"
	openshiftDNS           = "openshift-dns"
	multiclusterEngine     = "multicluster-engine"
)

// Container ports exposed by each Search component. These match the Service definitions in
// create_pgservice.go, create_indexerservice.go, create_apiservice.go, create_collectorservice.go,
// and the operator's own metrics/webhook ports in config/manager/manager.yaml.
const (
	postgresPort        = 5432
	indexerPort         = 3010
	apiPort             = 4010
	collectorPort       = 5010
	operatorWebhookPort = 9443
	operatorMetricsPort = 8080
	dnsPort             = 53
	kubeAPIServerPort   = 6443
)

func networkPolicyName(component string) string {
	return component + "-network-policy"
}

// namespaceSelectorPeer builds a NetworkPolicyPeer that matches all pods in the namespace
// identified by the well-known `kubernetes.io/metadata.name` label.
func namespaceSelectorPeer(namespaceName string) networkingv1.NetworkPolicyPeer {
	return networkingv1.NetworkPolicyPeer{
		NamespaceSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{nsLabelKey: namespaceName},
		},
	}
}

// podSelectorPeer builds a NetworkPolicyPeer that matches pods with the given labels in the
// same namespace as the NetworkPolicy.
func podSelectorPeer(labels map[string]string) networkingv1.NetworkPolicyPeer {
	return networkingv1.NetworkPolicyPeer{
		PodSelector: &metav1.LabelSelector{
			MatchLabels: labels,
		},
	}
}

func tcpPort(port int32) []networkingv1.NetworkPolicyPort {
	proto := corev1.ProtocolTCP
	p := intstr.FromInt32(port)
	return []networkingv1.NetworkPolicyPort{{Protocol: &proto, Port: &p}}
}

func monitoringIngressRule(port int32) networkingv1.NetworkPolicyIngressRule {
	return networkingv1.NetworkPolicyIngressRule{
		From:  []networkingv1.NetworkPolicyPeer{namespaceSelectorPeer(openshiftMonitoring)},
		Ports: tcpPort(port),
	}
}

func newNetworkPolicy(instance *searchv1alpha1.Search, component string,
	podLabels map[string]string) *networkingv1.NetworkPolicy {
	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      networkPolicyName(component),
			Namespace: instance.GetNamespace(),
			Labels:    generateLabels("network-policy", component),
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: podLabels},
			// Default to Ingress-only. Components that need Egress restriction
			// (e.g. postgres, which should never initiate outbound connections)
			// explicitly set policyTypes after construction.
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
			},
		},
	}
	return np
}

// withEgress adds the Egress policyType to a NetworkPolicy, making egress explicitly
// restricted by the policy's egress rules (deny-all egress by default unless rules are set).
func withEgress(np *networkingv1.NetworkPolicy) *networkingv1.NetworkPolicy {
	np.Spec.PolicyTypes = append(np.Spec.PolicyTypes, networkingv1.PolicyTypeEgress)
	return np
}

// setNPControllerRef sets the Search CR as the controller owner of the NetworkPolicy so it is
// cleaned up automatically if Search components are removed, consistent with the other
// create_*.go resources in this operator.
func setNPControllerRef(r *SearchReconciler, instance *searchv1alpha1.Search, np *networkingv1.NetworkPolicy) {
	if err := controllerutil.SetControllerReference(instance, np, r.Scheme); err != nil {
		log.V(2).Info("Could not set control for NetworkPolicy", "name", np.Name)
	}
}

// PostgresNetworkPolicy restricts access to the search-postgres pod.
//
// Rationale:
//   - Ingress: Only search-indexer (writes discovered resources) and search-api (serves
//     read-only GraphQL queries) need direct DB access. search-mcp-server is granted a
//     read-only DB role (see create_pgsecret.go) and connects directly, so it also needs
//     ingress access when deployed in the same namespace.
//   - Egress: PostgreSQL never initiates outbound connections, so no egress is required.
func (r *SearchReconciler) PostgresNetworkPolicy(instance *searchv1alpha1.Search) *networkingv1.NetworkPolicy {
	podLabels := generateLabels("name", postgresDeploymentName)
	np := newNetworkPolicy(instance, postgresDeploymentName, podLabels)
	np.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{
		{
			From: []networkingv1.NetworkPolicyPeer{
				podSelectorPeer(generateLabels("name", indexerDeploymentName)),
				podSelectorPeer(generateLabels("name", apiDeploymentName)),
				podSelectorPeer(map[string]string{"app.kubernetes.io/name": "acm-mcp-server"}),
			},
			Ports: tcpPort(postgresPort),
		},
	}
	// Egress: PostgreSQL never initiates outbound connections so all egress is denied.
	// withEgress() adds Egress to policyTypes; the empty egress list means deny-all.
	withEgress(np)
	np.Spec.Egress = []networkingv1.NetworkPolicyEgressRule{}
	setNPControllerRef(r, instance, np)
	return np
}

// IndexerNetworkPolicy restricts access to the search-indexer pod.
//
// Rationale:
//   - Ingress (ocm-proxyserver): Managed-cluster search-collector agents push discovered
//     resources to the indexer through the hub API server's aggregated API for
//     proxy.open-cluster-management.io. The kube-apiserver forwards these requests to the
//     ocm-proxyserver (multicluster-engine namespace), which then connects to the indexer.
//     Traffic is therefore sourced from ocm-proxyserver pods.
//   - Ingress (hub-local collector): The hub-local search-collector deployed by this operator
//     connects to the indexer directly (same namespace, no proxy), so its pod-selector ingress
//     is required in addition to the ocm-proxyserver rule above.
//   - Ingress (monitoring): Prometheus (openshift-monitoring) scrapes indexer metrics.
//   - Egress: The indexer writes aggregated data to search-postgres and watches hub-cluster
//     resources directly via the Kubernetes API, in addition to resolving Service DNS names.
func (r *SearchReconciler) IndexerNetworkPolicy(instance *searchv1alpha1.Search, mceNamespace string) *networkingv1.NetworkPolicy {
	podLabels := generateLabels("name", indexerDeploymentName)
	np := newNetworkPolicy(instance, indexerDeploymentName, podLabels)
	np.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{
		{
			// Managed-cluster collectors arrive via ocm-proxyserver in the MCE namespace.
			// The kube-apiserver aggregates proxy.open-cluster-management.io requests to
			// ocm-proxyserver, which then proxies to the indexer on port 3010.
			From: []networkingv1.NetworkPolicyPeer{{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{nsLabelKey: mceNamespace},
				},
				PodSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"control-plane": "ocm-proxyserver"},
				},
			}},
			Ports: tcpPort(indexerPort),
		},
		{
			// Hub-local collector: runs in the same namespace and pushes directly without proxying.
			From:  []networkingv1.NetworkPolicyPeer{podSelectorPeer(generateLabels("name", collectorDeploymentName))},
			Ports: tcpPort(indexerPort),
		},
		monitoringIngressRule(indexerPort),
	}
	setNPControllerRef(r, instance, np)
	return np
}

// APINetworkPolicy restricts access to the search-v2-api pod.
//
// Rationale:
//   - Ingress: search-v2-api serves the Search GraphQL API to other components running in the
//     same namespace (e.g. console-api) via its ClusterIP Service, so ingress is allowed from
//     pods in the same namespace. console-mce in the multicluster-engine namespace also
//     consumes the API. Prometheus (openshift-monitoring) scrapes the same port for metrics.
//   - Egress: The API queries search-postgres, and performs TokenReview/SubjectAccessReview
//     RBAC checks and ManagedCluster lookups against the Kubernetes API, in addition to
//     resolving Service DNS names.
func (r *SearchReconciler) APINetworkPolicy(instance *searchv1alpha1.Search, mceNamespace string) *networkingv1.NetworkPolicy {
	podLabels := generateLabels("name", apiDeploymentName)
	np := newNetworkPolicy(instance, apiDeploymentName, podLabels)
	np.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{
		{
			// Same-namespace consumers of the Search GraphQL API (e.g. console-api).
			From:  []networkingv1.NetworkPolicyPeer{podSelectorPeer(map[string]string{})},
			Ports: tcpPort(apiPort),
		},
		{
			// console-mce in the MCE namespace.
			From: []networkingv1.NetworkPolicyPeer{{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{nsLabelKey: mceNamespace},
				},
				PodSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "console-mce"},
				},
			}},
			Ports: tcpPort(apiPort),
		},
		monitoringIngressRule(apiPort),
	}
	setNPControllerRef(r, instance, np)
	return np
}

// CollectorNetworkPolicy restricts access to the hub-local search-collector pod (the collector
// instance the operator deploys directly on the hub cluster to index hub-local resources; the
// per-managed-cluster collectors deployed via the addon framework are governed separately).
//
// Rationale:
//   - Ingress: Only Prometheus (openshift-monitoring) needs to reach the collector, to scrape
//     metrics and hit the liveness/readiness endpoints.
//   - Egress: The collector watches hub-cluster resources via the Kubernetes API and pushes
//     discovered resources to search-indexer, in addition to resolving Service DNS names.
func (r *SearchReconciler) CollectorNetworkPolicy(instance *searchv1alpha1.Search) *networkingv1.NetworkPolicy {
	podLabels := generateLabels("name", collectorDeploymentName)
	np := newNetworkPolicy(instance, collectorDeploymentName, podLabels)
	np.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{
		monitoringIngressRule(collectorPort),
	}
	setNPControllerRef(r, instance, np)
	return np
}

// OperatorNetworkPolicy restricts access to the search-v2-operator controller-manager pod
// itself.
//
// Rationale:
//   - Ingress (webhook): The Kubernetes API server calls the operator's admission webhook
//     (CollectorConfig validation) on port 9443. The API server uses hostNetwork: true.
//     A ports-only rule (no From selector) is used because OVN-Kubernetes on OCP 4.22+ does
//     not reliably match hostNetwork traffic even with the documented empty
//     namespaceSelector+podSelector pattern. This is safe: the webhook is exposed only via a
//     ClusterIP Service (not reachable from outside the cluster) and authenticates callers
//     via TLS certificates issued by the API server's CA.
//   - Ingress (metrics): Prometheus (openshift-monitoring) scrapes the controller-runtime
//     metrics port.
//   - Egress: The operator manages nearly every resource type used by Search (Deployments,
//     Services, RBAC, addon framework CRs, etc.) on the hub API server, and resolves Service DNS
//     names.
func (r *SearchReconciler) OperatorNetworkPolicy(instance *searchv1alpha1.Search) *networkingv1.NetworkPolicy {
	podLabels := map[string]string{"app": "search", "control-plane": "controller-manager"}
	np := newNetworkPolicy(instance, "search-operator", podLabels)
	np.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{
		{
			// Webhook port: ports-only rule (no From selector) allows all sources.
			// OVN-Kubernetes on OCP 4.22+ does not reliably match hostNetwork traffic
			// (kube-apiserver) even with the documented empty namespaceSelector+podSelector
			// pattern. A ports-only rule is safe here because the webhook is ClusterIP-only
			// and uses TLS certificate authentication.
			Ports: tcpPort(operatorWebhookPort),
		},
		monitoringIngressRule(operatorMetricsPort),
	}
	setNPControllerRef(r, instance, np)
	return np
}

// NetworkPolicies returns every NetworkPolicy managed by the Search operator, one per
// component pod. Each policy only selects its own component's pods (never the whole
// namespace), so unrelated workloads sharing the namespace (e.g. other ACM components) are
// unaffected.
func (r *SearchReconciler) NetworkPolicies(ctx context.Context, instance *searchv1alpha1.Search) []*networkingv1.NetworkPolicy {
	mceNamespace := multiclusterEngine
	if r.DynamicClient != nil {
		if ns, err := r.getMCETargetNamespace(ctx); err != nil {
			log.V(2).Info("Could not resolve MCE target namespace, using default", "default", multiclusterEngine)
		} else {
			mceNamespace = ns
		}
	}
	return []*networkingv1.NetworkPolicy{
		r.PostgresNetworkPolicy(instance),
		r.IndexerNetworkPolicy(instance, mceNamespace),
		r.APINetworkPolicy(instance, mceNamespace),
		r.CollectorNetworkPolicy(instance),
		r.OperatorNetworkPolicy(instance),
	}
}
