# Network Policies

**Related Jira: [ACM-37751](https://issues.redhat.com/browse/ACM-37751)**

The `search-v2-operator` reconciles one [`NetworkPolicy`](https://kubernetes.io/docs/concepts/services-networking/network-policies/)
per Search component pod, following the principle of least privilege: **deny by default,
allow only the specific traffic each component needs.** The policies are created/updated
as part of the normal reconcile loop (see `controllers/create_networkpolicies.go`) and are
owned by the `Search` custom resource, so they're automatically removed if Search is deleted.

## Design principles

- **One policy per component, scoped by `podSelector`.** Each `NetworkPolicy` selects only its
  own component's pods (e.g. `name: search-postgres`), never the whole namespace. This is
  important because the Search components share a namespace (`open-cluster-management` in a
  typical ACM install) with unrelated ACM components — a namespace-wide policy would
  inadvertently restrict traffic for pods that aren't managed by this operator.
- **`policyTypes: [Ingress]` by default; `[Ingress, Egress]` only for postgres.** Kubernetes
  NetworkPolicies are allow-lists: once a pod is selected by any policy for a given direction,
  all traffic in that direction is denied unless explicitly allowed. Ingress-only is used for
  all components except postgres because OVN-Kubernetes (the default CNI on OpenShift) handles
  `kubernetes.default.svc` ClusterIP traffic through the OVN service load balancer *before*
  NetworkPolicy evaluation, so no egress rule type can match kube-API traffic. Applying an
  Egress policyType to any component that calls the Kubernetes API would silently block it.
  Postgres is the sole exception: it never initiates outbound connections, so deny-all egress
  is both correct and safe for it.
- **Well-known namespace labels.** Ingress/egress rules that need to reference OpenShift system
  namespaces (API server, monitoring, DNS) use the `kubernetes.io/metadata.name` label, which
  the API server automatically stamps on every namespace (Kubernetes 1.21+). This avoids relying
  on custom labels that may not exist in every cluster.

## Component network flows

### search-postgres

| Direction | Peer | Port | Rationale |
|---|---|---|---|
| Ingress | Pods labeled `name: search-indexer` | 5432/TCP | The indexer writes discovered/aggregated resources to the database. |
| Ingress | Pods labeled `name: search-api` | 5432/TCP | The API serves read-only GraphQL queries backed by the database. |
| Ingress | Pods labeled `app.kubernetes.io/name: acm-mcp-server` | 5432/TCP | The operator provisions a read-only DB role (`search_mcp_ro`, see `create_pgsecret.go`) for the optional `search-mcp-server` to query data directly for AI/automation use cases. |
| Egress | *(none)* | — | PostgreSQL only responds to inbound connections; it never initiates outbound traffic. |

### search-indexer

| Direction | Peer | Port | Rationale |
|---|---|---|---|
| Ingress | `ocm-proxyserver` pods in `multicluster-engine` namespace | 3010/TCP | Managed-cluster `search-collector` agents push discovered resources to the indexer through the hub API server's aggregated API (`proxy.open-cluster-management.io`). The kube-apiserver forwards these requests to `ocm-proxyserver` in the `multicluster-engine` namespace, which then proxies to the indexer. Traffic is therefore sourced from `ocm-proxyserver` pods (labeled `control-plane: ocm-proxyserver`), not from kube-apiserver pods. |
| Ingress | Pods labeled `name: search-collector` | 3010/TCP | The hub-local collector deployed by this operator runs in the same namespace and connects to the indexer directly (no proxy). |
| Ingress | `openshift-monitoring` namespace | 3010/TCP | Prometheus (`prometheus-k8s`) scrapes indexer metrics over the same HTTPS port used for data ingestion (see the indexer `ServiceMonitor`). |
| Egress | *(not restricted — Ingress-only policy)* | — | OVN-Kubernetes handles `kubernetes.default.svc` ClusterIP traffic via the OVN service load balancer before NetworkPolicy evaluation, so no egress rule can match kube-API traffic. Applying an Egress policyType would silently block the indexer from reaching the Kubernetes API. |

### search-v2-api

| Direction | Peer | Port | Rationale |
|---|---|---|---|
| Ingress | Any pod in the same namespace | 4010/TCP | The API is consumed via its ClusterIP Service (`search-search-api`) directly by same-namespace clients such as `console-api` (see `console/backend/src/lib/search.ts`, which calls `https://search-search-api.<ns>.svc.cluster.local:4010`). It is not registered as an aggregated `APIService`, so traffic arrives directly from client pods rather than being proxied through the Kubernetes API server. |
| Ingress | `openshift-monitoring` namespace | 4010/TCP | Prometheus scrapes API metrics over the same HTTPS port used to serve GraphQL requests. |
| Egress | *(not restricted — Ingress-only policy)* | — | OVN-Kubernetes handles `kubernetes.default.svc` ClusterIP traffic via the OVN service load balancer before NetworkPolicy evaluation, so no egress rule can match kube-API traffic. Applying an Egress policyType would silently block the API from reaching the Kubernetes API (TokenReview/SubjectAccessReview calls). |

### search-collector (hub-local)

This is the collector instance the operator deploys directly on the hub cluster to index
hub-local resources. Collectors on managed clusters are deployed separately through the addon
framework's `ManifestWork` mechanism (Helm chart in `addon/manifests/chart`), run in a different
cluster/namespace, and are out of scope for this operator's `NetworkPolicy` reconciliation.

| Direction | Peer | Port | Rationale |
|---|---|---|---|
| Ingress | `openshift-monitoring` namespace | 5010/TCP | Prometheus scrapes collector metrics and probes the liveness/readiness endpoints. |
| Egress | *(not restricted — Ingress-only policy)* | — | OVN-Kubernetes handles `kubernetes.default.svc` ClusterIP traffic via the OVN service load balancer before NetworkPolicy evaluation, so no egress rule can match kube-API traffic. Applying an Egress policyType would silently block the collector from reaching the Kubernetes API (resource watch). |

### search-collector (managed cluster addon)

Managed-cluster collectors are deployed via the addon framework (Helm chart in
`addon/manifests/chart/templates/`). The chart is embedded in the search-v2-operator binary and
deployed at runtime to each managed cluster's `open-cluster-management-agent-addon` namespace.
When `prometheus.enabled` is true, Prometheus scraping is allowed; when false, the policy is
deny-all ingress — the collector has no legitimate inbound traffic in that case.

| Direction | Peer | Port | Rationale |
|---|---|---|---|
| Ingress | `openshift-monitoring` namespace *(only when `prometheus.enabled`)* | 5010/TCP | Prometheus scrapes collector metrics on the managed cluster. |
| Ingress | *(deny-all when `prometheus.enabled` is false)* | — | The collector serves no inbound traffic other than metrics; liveness/readiness probes from the kubelet bypass NetworkPolicy. |
| Egress | *(not restricted — Ingress-only policy)* | — | The collector uses `HUB_CONFIG` to send `clusterstatuses` requests to the hub API server's `proxy.open-cluster-management.io` aggregated API, which proxies traffic to the indexer. It also watches local cluster resources via the managed cluster's Kubernetes API. Egress is not restricted because destination addresses (hub API server, local API server) vary per deployment. |

### search-v2-operator (controller-manager)

| Direction | Peer | Port | Rationale |
|---|---|---|---|
| Ingress | `openshift-kube-apiserver` namespace | 9443/TCP | The Kubernetes API server calls the operator's `CollectorConfig` admission webhook (defaulting/validation). |
| Ingress | `openshift-monitoring` namespace | 8080/TCP | Prometheus scrapes controller-runtime metrics. |
| Egress | *(not restricted — Ingress-only policy)* | — | OVN-Kubernetes handles `kubernetes.default.svc` ClusterIP traffic via the OVN service load balancer before NetworkPolicy evaluation, so no egress rule can match kube-API traffic. Applying an Egress policyType would silently block the operator from reaching the Kubernetes API (it manages Deployments, Services, Secrets, RBAC, addon CRs, etc.). |

## Testing

Unit tests in `controllers/create_networkpolicies_test.go` verify:
- Exactly one `NetworkPolicy` is generated per component, each scoped to that component's own
  pods (never an empty/whole-namespace `podSelector`).
- `search-postgres` declares both `Ingress` and `Egress` policy types (deny-all egress).
- All other components declare `Ingress`-only and explicitly do **not** set `Egress` (the
  `assert.NotContains(...PolicyTypeEgress)` regression test guards this invariant).
- Each policy's specific ingress peers and ports match the tables above.
- `reconcileNetworkPolicies` is idempotent (safe to run every reconcile without unnecessary
  updates).

Because these policies are enforced by the cluster's CNI plugin (not the Kubernetes API server),
functional verification — confirming that legitimate traffic still flows and that traffic
outside these rules is blocked — requires testing against a real cluster with a
NetworkPolicy-enforcing CNI (e.g. OVN-Kubernetes on OpenShift). See the coordinating test task
for that verification.
