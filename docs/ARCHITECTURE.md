# search-v2-operator Architecture

## Overview

search-v2-operator is a Kubernetes operator built with kubebuilder/controller-runtime. It owns the full lifecycle of ACM Search infrastructure on the hub cluster: it provisions and configures PostgreSQL, search-indexer, search-v2-api, and search-collector from a single `Search` custom resource.

```text
Search CR (search-v2-operator)
        │
        ▼
  SearchReconciler
        │
        ├──► PostgreSQL Deployment + Service + Secret + ConfigMap + PVC
        ├──► search-indexer  Deployment + Service + ConfigMap
        ├──► search-v2-api   Deployment + Service
        ├──► search-collector Deployment + Service
        ├──► RBAC (ClusterRole, ClusterRoleBinding, ServiceAccount)
        ├──► ServiceMonitors (Prometheus)
        ├──► CollectorConfig (merged from user + integration configs)
        └──► Addon framework (ManagedClusterAddon CSR approval)
```

## Packages

| Package | Responsibility |
|---|---|
| `main` | Bootstrap: register schemes (search, monitoring, OCM cluster, admission), create manager, register `SearchReconciler` and `CollectorConfig` webhook, start health probes |
| `controllers` | All reconciliation logic. One controller (`SearchReconciler`) handles the `Search` CR. Each Kubernetes resource type has its own `create_*.go` file. `defaults.go` holds resource request/limit constants. |
| `api/v1alpha1` | CRD type definitions (`Search`, `CollectorConfig`). `CollectorConfig` has a defaulting/validating webhook. Changes here require `make manifests` + `make generate`. |
| `addon` | OCM addon integration. `CreateAddonOnce` runs once per process lifetime to register the search-collector addon and handle `CertificateSigningRequest` approval for managed clusters. |

## CRD: Search

The `Search` CR is a singleton named `search-v2-operator` (hardcoded — only one exists per cluster).

Key spec fields:

| Field | Effect |
|---|---|
| `spec.deployments` | Per-component resource requests, limits, replica counts, node selectors, tolerations, and env var overrides |
| `spec.dbStorage.storageClassName` | If set, provisions a PVC for PostgreSQL instead of using emptyDir |
| `spec.dbConfig` | ConfigMap name with PostgreSQL parameter overrides |
| `metadata.annotations["search-pause: true"]` | Halts reconciliation without deleting resources |

## CRD: CollectorConfig

Allows integration teams and users to customize search-collector behaviour (resource limits, collection intervals, excluded resources). The operator merges all `CollectorConfig` objects into a single authoritative config that drives the collector deployment.

- Integration team configs: CRs with label `search.open-cluster-management.io/config-type: integration`, merged first (sorted by name)
- User config: CR named `user-collector-config` (`userCollectorConfigName`), overlaid last
- Operator-managed output: CR named `merged-collector-config` (`mergedCollectorConfigName`) — the controller watch skips this name to prevent reconcile loops

The webhook (`api/v1alpha1/collectorconfig_webhook.go`) sets defaults and validates on admission.

### Built-in integration CollectorConfigs (ACM-37052)

Integration teams (CNV, OLM, GRC, Kyverno, Gatekeeper, Argo, ACM app lifecycle) contribute a
plain `CollectorConfig` YAML file to `config/integration_collector_configs/` instead of writing
Go code. `config/integrationconfigs.go` embeds that directory (`//go:embed`) at build time.

**This is seeded once per operator process, at manager startup — not on every reconcile.**
`IntegrationCollectorConfigSeeder` (`controllers/integration_collectorconfig_seeder.go`) is a
`manager.Runnable` added via `mgr.Add(...)` in `main.go`. Its `Start` calls
`applyIntegrationCollectorConfigs` (`controllers/create_integration_collectorconfigs.go`), which
walks the embedded files and for each **unconditionally creates or overwrites** the CR with that
fixed name — no diffing against customizations, no hash, no attempt to detect "is this a new
release." If the API/webhook isn't ready yet (a known startup race — the CollectorConfig
webhook's CA bundle injection can take a couple of reconciles), `Start` retries on a fixed
interval until it succeeds once, then returns; it does not use `sync.Once`, since that would
permanently give up after a single failed attempt.

Because this only runs at startup, a team can freely edit their canonical config
(`cnv-integration-collector-config`, etc.) and it persists for the life of that pod — the reset
only happens on the *next* restart/upgrade, at which point it's unconditionally reverted to
whatever's currently embedded. **A team that wants a change to survive across restarts before
it's officially shipped creates a differently-named CollectorConfig instead of editing the
canonical one** (e.g. `cnv-integration-collector-config-2`) — the seeder only knows about its
fixed set of embedded names, so any other name is left alone entirely, and the merge step already
discovers integration configs by label rather than name, so it picks up any number of them
automatically. To ship a change permanently, the team opens a PR updating their YAML in
`config/integration_collector_configs/` — the next operator upgrade applies it to every cluster
that hasn't switched to a differently-named override for that team.

**Known accepted limitation (tech preview):** this is deliberately not a smart merge. A
customization to the canonical name is only safe for as long as the pod doesn't restart; there's
no attempt to distinguish "this changed because of a new release" from "this changed because
someone edited it" — restarting always wins. There's also no cleanup path if a team removes their
YAML file entirely: the previously-created CR becomes orphaned and is left as-is.

**Namespace is discovered dynamically, not read from an env var.** `WATCH_NAMESPACE`/`POD_NAMESPACE`
are not reliably set in real deployments — verified empty on a live ACM 5.0.0-153 install during
testing, causing the seeder to fail with `"an empty namespace may not be set during creation"` on
the first version of this code. The rest of the reconciler never has this problem because it
gets its namespace from the reconciled `Search` object itself (the manager watches cluster-wide
when `WATCH_NAMESPACE` is empty). `IntegrationCollectorConfigSeeder.resolveSearch` fixes this the
same way: it looks up the live `Search` CR (named `OperatorName`) and uses its namespace, retrying
via the same poll loop if the CR doesn't exist yet. It requires exactly one match rather than
returning the first one found — `Search` is namespaced with no uniqueness webhook, so nothing at
the API level actually prevents a duplicate — and it also carries back the CR's `search-pause`
state, since the seeder writes cluster state just like `Reconcile` and must honor the same pause
guarantee (checked the same way, via `IsPaused`, before any write).

The same startup race also means a canonical config can already exist without
`IntegrationTeamLabel` (e.g. state left over from before this label existed). `applyOneIntegrationCollectorConfig`
treats a missing label the same as a spec mismatch — it's not just cosmetic: both the webhook's
exclude-overlap check and the merge step discover integration configs by this label, so an
unlabeled canonical config would silently stop being protected and stop being merged.

As each apiGroup gets covered by a real integration config, it's removed from the temporary
`protectedAPIGroups` safety net in the webhook (see below) — the dynamic
`validateExcludeAgainstIntegrationConfigs` check already protects anything with a real `include`
rule in an integration-labeled CollectorConfig, regardless of how that CR was created.

## Reconcile flow

Each reconcile call processes the `Search` CR in a fixed sequence. Note: seeding the built-in
integration CollectorConfigs (above) happens once at manager startup via
`IntegrationCollectorConfigSeeder`, outside of this per-CR reconcile sequence entirely.

1. **Addon setup** (`once.Do`) — registers the OCM addon framework once per process.
2. **Status update** (pod events only) — updates `Search.Status` with pod readiness; skips full reconcile.
3. **Finalizer** — adds/removes `search.open-cluster-management.io/finalizer`; on deletion, cleans up cluster-scoped resources (ClusterRole, ClusterRoleBinding, ManagedServiceAccount, ClusterManagementAddon owner refs).
4. **Pause check** — if `search-pause: true` annotation is present, returns immediately.
5. **PVC** — if `spec.dbStorage.storageClassName` is set and PVC is absent, creates it; retries in 10s if not ready.
6. **RBAC** — ServiceAccount, ClusterRoles, ClusterRoleBindings.
7. **CollectorConfig merge** — merges user + integration configs into the authoritative merged config.
8. **PostgreSQL** — Secret, Service, Deployment, ConfigMap.
9. **Component services** — Indexer, API, Collector Services.
10. **ServiceMonitors** — Prometheus ServiceMonitors for indexer, api, collector.
11. **Component deployments** — Collector, Indexer, API Deployments.
12. **ConfigMaps** — Indexer ConfigMap, Postgres ConfigMap, Search CA cert.
13. **Feature configurations** — Global search, fine-grained RBAC, virtual machine integration.
14. **Prometheus alert rules** — PVC usage alert.
15. **One-time migrations** (`cleanOnce.Do`) — removes legacy serviceMonitor setup from `openshift-monitoring` (introduced ACM 2.9) and removes Search ownerRef from ClusterManagementAddon (introduced ACM 2.10).

## Watch sources

The controller re-queues on changes from multiple sources:

| Source | Condition | Action |
|---|---|---|
| `Search` CR | Any change | Full reconcile |
| `Deployment` | Owned by Search CR | Full reconcile |
| `Secret` | Owned by Search CR | Full reconcile |
| `ConfigMap` | Owned by Search CR, or named `SEARCH_GLOBAL_CONFIG` | Full reconcile |
| `Pod` | Has search labels | Status-only reconcile |
| `ClusterRole` | Matches search role name | Full reconcile |
| `ManagedCluster` | Is a managed hub (has `hub.open-cluster-management.io` cluster claim) | Full reconcile (global search setup) |
| `CollectorConfig` | Named `user-collector-config` or has label `search.open-cluster-management.io/config-type: integration` | Full reconcile |

## Feature configurations

Three optional setup passes run during each reconcile:

- **Global search** (`reconcileGlobalSearch`): Gated by the `global-search-preview` feature. Detects managed-hub clusters via `ManagedCluster.status.clusterClaims` and toggles `FEATURE_FEDERATED_SEARCH` on the search-api deployment accordingly.
- **Fine-grained RBAC** (`reconcileFineGrainedRBACConfiguration`): Toggles `FEATURE_FINE_GRAINED_RBAC` on the search-api deployment and updates a status condition. Does not create `ManagedServiceAccount` or `ClusterPermission` resources.
- **Virtual machine integration** (`reconcileVirtualMachineConfiguration`): Gated by the `virtual-machine-preview` feature. Creates `ManagedServiceAccount` and `ClusterPermission` resources for all managed clusters to enable VM resource access. KubeVirt/CNV detection is not yet implemented (noted as `FUTURE` in the code).

## Code generation

This repo uses kubebuilder code generation. Two steps are independent:

| Command | What it regenerates | When to run |
|---|---|---|
| `make manifests` | `config/crd/bases/*.yaml`, RBAC manifests | After editing structs/markers in `api/v1alpha1/` |
| `make generate` | `api/v1alpha1/zz_generated.deepcopy.go` | After adding/removing fields in `api/v1alpha1/` |

Both download `controller-gen` to `bin/` on first run if absent.

## Testing

`make test` uses `controller-runtime`'s `envtest` (real Kubernetes API server binary, no etcd). Assets are downloaded to `bin/` by `setup-envtest` on first run — this can take a minute. The `KUBEBUILDER_ASSETS` env var points the test binary at the downloaded assets.
