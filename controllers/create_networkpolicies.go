// Copyright Contributors to the Open Cluster Management project
package controllers

import (
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
	kubeAPIServicePort  = 443 // kubernetes.default.svc ClusterIP port used by in-cluster clients
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

// dnsEgressRule allows resolving in-cluster and external DNS names via the OpenShift DNS
// operator (CoreDNS pods in the `openshift-dns` namespace). Every Search component needs this
// to resolve Service DNS names (e.g. search-postgres.<ns>.svc) and, in the collector's case,
// the hub API server host name.
func dnsEgressRule() networkingv1.NetworkPolicyEgressRule {
	tcp := corev1.ProtocolTCP
	udp := corev1.ProtocolUDP
	port := intstr.FromInt32(dnsPort)
	return networkingv1.NetworkPolicyEgressRule{
		To: []networkingv1.NetworkPolicyPeer{namespaceSelectorPeer(openshiftDNS)},
		Ports: []networkingv1.NetworkPolicyPort{
			{Protocol: &tcp, Port: &port},
			{Protocol: &udp, Port: &port},
		},
	}
}

// kubeAPIServerEgressRules allows a component to reach the Kubernetes/OpenShift API server,
// e.g. to watch resources, or perform TokenReview/SubjectAccessReview RBAC checks.
// Two rules are required:
//   - Port 6443 scoped to openshift-kube-apiserver: matches kube-apiserver pods directly.
//   - Port 443 via ipBlock for the service CIDR: in-cluster clients use the
//     kubernetes.default.svc ClusterIP (e.g. 172.30.0.1:443). In OVN-Kubernetes, ClusterIP
//     traffic is handled by the OVN service load balancer at the logical switch level. A
//     namespaceSelector or podSelector cannot match a ClusterIP; an ipBlock scoped to the
//     service network CIDR is required. The CIDR is read from the network.config cluster CR
//     at startup and injected via the SEARCH_SERVICE_CIDR env var (set by the operator itself).
func kubeAPIServerEgressRules(serviceCIDR string) []networkingv1.NetworkPolicyEgressRule {
	proto := corev1.ProtocolTCP
	port443 := intstr.FromInt32(kubeAPIServicePort)
	return []networkingv1.NetworkPolicyEgressRule{
		{
			To:    []networkingv1.NetworkPolicyPeer{namespaceSelectorPeer(openshiftKubeAPIServer)},
			Ports: tcpPort(kubeAPIServerPort),
		},
		{
			To: []networkingv1.NetworkPolicyPeer{
				{IPBlock: &networkingv1.IPBlock{CIDR: serviceCIDR}},
			},
			Ports: []networkingv1.NetworkPolicyPort{{Protocol: &proto, Port: &port443}},
		},
	}
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
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
		},
	}
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
	// No egress rules: PostgreSQL only responds to inbound connections.
	np.Spec.Egress = []networkingv1.NetworkPolicyEgressRule{}
	setNPControllerRef(r, instance, np)
	return np
}

// IndexerNetworkPolicy restricts access to the search-indexer pod.
//
// Rationale:
//   - Ingress: search-collector agents (both the hub-local collector and collectors running on
//     managed clusters) push discovered resources to the indexer through the hub API server's
//     service proxy using their addon bootstrap kubeconfig, so the traffic is sourced from
//     kube-apiserver pods. Prometheus (openshift-monitoring) scrapes the same port for metrics.
//   - Egress: The indexer writes aggregated data to search-postgres and watches hub-cluster
//     resources directly via the Kubernetes API, in addition to resolving Service DNS names.
func (r *SearchReconciler) IndexerNetworkPolicy(instance *searchv1alpha1.Search, serviceCIDR string) *networkingv1.NetworkPolicy {
	podLabels := generateLabels("name", indexerDeploymentName)
	np := newNetworkPolicy(instance, indexerDeploymentName, podLabels)
	np.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{
		{
			From:  []networkingv1.NetworkPolicyPeer{namespaceSelectorPeer(openshiftKubeAPIServer)},
			Ports: tcpPort(indexerPort),
		},
		monitoringIngressRule(indexerPort),
	}
	np.Spec.Egress = append([]networkingv1.NetworkPolicyEgressRule{
		{
			To:    []networkingv1.NetworkPolicyPeer{podSelectorPeer(generateLabels("name", postgresDeploymentName))},
			Ports: tcpPort(postgresPort),
		},
	}, append(kubeAPIServerEgressRules(serviceCIDR), dnsEgressRule())...)
	setNPControllerRef(r, instance, np)
	return np
}

// APINetworkPolicy restricts access to the search-v2-api pod.
//
// Rationale:
//   - Ingress: search-v2-api serves the Search GraphQL API to other components running in the
//     same namespace (e.g. console-api) via its ClusterIP Service, so ingress is allowed from
//     pods in the same namespace. Prometheus (openshift-monitoring) scrapes the same port for
//     metrics.
//   - Egress: The API queries search-postgres, and performs TokenReview/SubjectAccessReview
//     RBAC checks and ManagedCluster lookups against the Kubernetes API, in addition to
//     resolving Service DNS names.
func (r *SearchReconciler) APINetworkPolicy(instance *searchv1alpha1.Search, serviceCIDR string) *networkingv1.NetworkPolicy {
	podLabels := generateLabels("name", apiDeploymentName)
	np := newNetworkPolicy(instance, apiDeploymentName, podLabels)
	np.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{
		{
			// Same-namespace consumers of the Search GraphQL API (e.g. console-api).
			From:  []networkingv1.NetworkPolicyPeer{podSelectorPeer(map[string]string{})},
			Ports: tcpPort(apiPort),
		},
		monitoringIngressRule(apiPort),
	}
	np.Spec.Egress = append([]networkingv1.NetworkPolicyEgressRule{
		{
			To:    []networkingv1.NetworkPolicyPeer{podSelectorPeer(generateLabels("name", postgresDeploymentName))},
			Ports: tcpPort(postgresPort),
		},
	}, append(kubeAPIServerEgressRules(serviceCIDR), dnsEgressRule())...)
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
func (r *SearchReconciler) CollectorNetworkPolicy(instance *searchv1alpha1.Search, serviceCIDR string) *networkingv1.NetworkPolicy {
	podLabels := generateLabels("name", collectorDeploymentName)
	np := newNetworkPolicy(instance, collectorDeploymentName, podLabels)
	np.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{
		monitoringIngressRule(collectorPort),
	}
	np.Spec.Egress = append([]networkingv1.NetworkPolicyEgressRule{
		{
			To:    []networkingv1.NetworkPolicyPeer{podSelectorPeer(generateLabels("name", indexerDeploymentName))},
			Ports: tcpPort(indexerPort),
		},
	}, append(kubeAPIServerEgressRules(serviceCIDR), dnsEgressRule())...)
	setNPControllerRef(r, instance, np)
	return np
}

// OperatorNetworkPolicy restricts access to the search-v2-operator controller-manager pod
// itself.
//
// Rationale:
//   - Ingress: The Kubernetes API server calls the operator's admission webhook (CollectorConfig
//     defaulting/validation) on the webhook port. Prometheus (openshift-monitoring) scrapes the
//     controller-runtime metrics port.
//   - Egress: The operator manages nearly every resource type used by Search (Deployments,
//     Services, RBAC, addon framework CRs, etc.) on the hub API server, and resolves Service DNS
//     names.
func (r *SearchReconciler) OperatorNetworkPolicy(instance *searchv1alpha1.Search, serviceCIDR string) *networkingv1.NetworkPolicy {
	podLabels := map[string]string{"app": "search", "control-plane": "controller-manager"}
	np := newNetworkPolicy(instance, "search-operator", podLabels)
	np.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{
		{
			From:  []networkingv1.NetworkPolicyPeer{namespaceSelectorPeer(openshiftKubeAPIServer)},
			Ports: tcpPort(operatorWebhookPort),
		},
		monitoringIngressRule(operatorMetricsPort),
	}
	np.Spec.Egress = append(kubeAPIServerEgressRules(serviceCIDR), dnsEgressRule())
	setNPControllerRef(r, instance, np)
	return np
}

// NetworkPolicies returns every NetworkPolicy managed by the Search operator, one per
// component pod. Each policy only selects its own component's pods (never the whole
// namespace), so unrelated workloads sharing the namespace (e.g. other ACM components) are
// unaffected.
func (r *SearchReconciler) NetworkPolicies(instance *searchv1alpha1.Search, serviceCIDR string) []*networkingv1.NetworkPolicy {
	return []*networkingv1.NetworkPolicy{
		r.PostgresNetworkPolicy(instance),
		r.IndexerNetworkPolicy(instance, serviceCIDR),
		r.APINetworkPolicy(instance, serviceCIDR),
		r.CollectorNetworkPolicy(instance, serviceCIDR),
		r.OperatorNetworkPolicy(instance, serviceCIDR),
	}
}
