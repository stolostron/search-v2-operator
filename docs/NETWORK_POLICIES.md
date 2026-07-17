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
- **`policyTypes: [Ingress, Egress]` on every policy.** Kubernetes NetworkPolicies are
  allow-lists: once a pod is selected by any policy for a given direction, all traffic in that
  direction is denied unless explicitly allowed. Declaring both directions ensures the CIS
  Kubernetes Benchmark recommendation ("policies should have a default deny for both ingress
  and egress") is met for every component.
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
| Ingress | `openshift-kube-apiserver` namespace | 3010/TCP | `search-collector` agents (hub-local and on every managed cluster) push discovered resources to the indexer through the hub API server's service-proxy, using their addon bootstrap kubeconfig — so this traffic is sourced from kube-apiserver pods, not directly from collector pods. |
| Ingress | `openshift-monitoring` namespace | 3010/TCP | Prometheus (`prometheus-k8s`) scrapes indexer metrics over the same HTTPS port used for data ingestion (see the indexer `ServiceMonitor`). |
| Egress | Pods labeled `name: search-postgres` | 5432/TCP | The indexer writes aggregated resource and relationship data to the database. |
| Egress | `openshift-kube-apiserver` namespace | 6443/TCP | The indexer watches hub-cluster resources directly via the Kubernetes API. |
| Egress | `openshift-dns` namespace | 53/TCP+UDP | Resolves in-cluster Service DNS names (e.g. `search-postgres.<ns>.svc`). |

### search-v2-api

| Direction | Peer | Port | Rationale |
|---|---|---|---|
| Ingress | Any pod in the same namespace | 4010/TCP | The API is consumed via its ClusterIP Service (`search-search-api`) directly by same-namespace clients such as `console-api` (see `console/backend/src/lib/search.ts`, which calls `https://search-search-api.<ns>.svc.cluster.local:4010`). It is not registered as an aggregated `APIService`, so traffic arrives directly from client pods rather than being proxied through the Kubernetes API server. |
| Ingress | `openshift-monitoring` namespace | 4010/TCP | Prometheus scrapes API metrics over the same HTTPS port used to serve GraphQL requests. |
| Egress | Pods labeled `name: search-postgres` | 5432/TCP | The API queries the database to answer GraphQL requests. |
| Egress | `openshift-kube-apiserver` namespace | 6443/TCP | The API performs `TokenReview`/`SubjectAccessReview` calls to authenticate/authorize each request and looks up `ManagedCluster` resources for federated (global) search. |
| Egress | `openshift-dns` namespace | 53/TCP+UDP | Resolves Service DNS names. |

### search-collector (hub-local)

This is the collector instance the operator deploys directly on the hub cluster to index
hub-local resources. Collectors on managed clusters are deployed separately through the addon
framework's `ManifestWork` mechanism (Helm chart in `addon/manifests/chart`), run in a different
cluster/namespace, and are out of scope for this operator's `NetworkPolicy` reconciliation.

| Direction | Peer | Port | Rationale |
|---|---|---|---|
| Ingress | `openshift-monitoring` namespace | 5010/TCP | Prometheus scrapes collector metrics and probes the liveness/readiness endpoints. |
| Egress | Pods labeled `name: search-indexer` | 3010/TCP | The collector pushes discovered hub-cluster resources to the indexer. |
| Egress | `openshift-kube-apiserver` namespace | 6443/TCP | The collector watches hub-cluster resources via the Kubernetes API. |
| Egress | `openshift-dns` namespace | 53/TCP+UDP | Resolves Service DNS names. |

### search-v2-operator (controller-manager)

| Direction | Peer | Port | Rationale |
|---|---|---|---|
| Ingress | `openshift-kube-apiserver` namespace | 9443/TCP | The Kubernetes API server calls the operator's `CollectorConfig` admission webhook (defaulting/validation). |
| Ingress | `openshift-monitoring` namespace | 8080/TCP | Prometheus scrapes controller-runtime metrics. |
| Egress | `openshift-kube-apiserver` namespace | 6443/TCP | The operator manages nearly every resource type used by Search — Deployments, Services, Secrets, RBAC, `ManagedClusterAddOn`/`ManifestWork`, `ManagedServiceAccount`, `ClusterPermission`, etc. |
| Egress | `openshift-dns` namespace | 53/TCP+UDP | Resolves Service DNS names. |

## Testing

Unit tests in `controllers/create_networkpolicies_test.go` verify:
- Exactly one `NetworkPolicy` is generated per component, each scoped to that component's own
  pods (never an empty/whole-namespace `podSelector`).
- Every policy declares both `Ingress` and `Egress` policy types.
- Each policy's specific ingress/egress peers and ports match the tables above.
- `reconcileNetworkPolicies` is idempotent (safe to run every reconcile without unnecessary
  updates).

Because these policies are enforced by the cluster's CNI plugin (not the Kubernetes API server),
functional verification — confirming that legitimate traffic still flows and that traffic
outside these rules is blocked — requires testing against a real cluster with a
NetworkPolicy-enforcing CNI (e.g. OVN-Kubernetes on OpenShift). See the coordinating test task
for that verification.
