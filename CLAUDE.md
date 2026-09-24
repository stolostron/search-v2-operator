# search-v2-operator

Kubernetes operator (kubebuilder/controller-runtime) that deploys and manages all ACM Search components: PostgreSQL, search-indexer, search-v2-api, and search-collector.

For system architecture, data flows, and module layout, see [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Wiki

The project Wiki at <https://github.com/stolostron/search-v2-operator/wiki> is the canonical source for architecture, design decisions, and operational knowledge. **Load relevant wiki pages at the start of any task** — do not rely solely on source code.

Key pages and when to load them:

| Wiki page | Load when… |
|---|---|
| [Home](https://github.com/stolostron/search-v2-operator/wiki/Home) | Starting any task — overview and links |
| [Feature-Spec](https://github.com/stolostron/search-v2-operator/wiki/Feature-Spec) | Implementing or reviewing a feature |
| [Addon-Framework-Operator](https://github.com/stolostron/search-v2-operator/wiki/Addon-Framework-Operator) | Working on addon deployment, ManagedClusterAddOn, or collector rollout |
| [Request-flow-and-timeouts](https://github.com/stolostron/search-v2-operator/wiki/Request-flow-and-timeouts) | Debugging latency, timeouts, or collector-to-indexer sync issues |
| [Collector:-Configure-resources-collected](https://github.com/stolostron/search-v2-operator/wiki/Collector:-Configure-resources-collected) | Working on collection config, allowlists, or resource filtering |
| [Search-Query-API](https://github.com/stolostron/search-v2-operator/wiki/Search-Query-API) / [GraphQL-API](https://github.com/stolostron/search-v2-operator/wiki/GraphQL-API) | Working on the search-v2-api or query patterns |
| [Current-Limitations-of-User-Authorization](https://github.com/stolostron/search-v2-operator/wiki/Current-Limitations-of-User-Authorization) | Working on RBAC, authorization, or security issues |
| [PostgreSQL-Key-Tuning-Parameters](https://github.com/stolostron/search-v2-operator/wiki/PostgreSQL-Key-Tuning-Parameters) | Investigating database performance or configuration |
| [PostgreSQL-query-inventory](https://github.com/stolostron/search-v2-operator/wiki/PostgreSQL-query-inventory) | Reviewing or optimizing SQL queries in search-indexer |
| [Scale-and-performance-metrics](https://github.com/stolostron/search-v2-operator/wiki/Scale-and-performance-metrics) | Working on scale, performance, or capacity planning |
| [NodeSelectors-and-tolerations](https://github.com/stolostron/search-v2-operator/wiki/NodeSelectors-and-tolerations) | Working on scheduling, placement, or node affinity |
| [Global-Search-User-Configuration](https://github.com/stolostron/search-v2-operator/wiki/Global-Search-User-Configuration) | Working on global search or multi-hub configuration |
| [Integration-(E2E)-test-strategy](https://github.com/stolostron/search-v2-operator/wiki/Integration-(E2E)-test-strategy) | Writing or debugging E2E tests |

Fetch a wiki page with:
```bash
curl -s "https://github.com/stolostron/search-v2-operator/wiki/<Page-Name>" | python3 -c "
import sys, re
html = sys.stdin.read()
body = re.search(r'<div[^>]+class=\"[^\"]*markdown-body[^\"]*\"[^>]*>(.*?)</div>', html, re.DOTALL)
print(body.group(1) if body else html[:4000])
"
```

## Commands

```bash
make build          # Build manager binary to bin/manager
make run            # Run controller locally against current kubeconfig cluster
make test           # Run unit tests (downloads envtest assets to bin/ on first run — slow)
make lint           # Run golangci-lint + gosec (downloads golangci-lint if not present)
make manifests      # Regenerate CRD/RBAC manifests (run after editing api/v1alpha1/ types)
make generate       # Regenerate DeepCopy methods (run after editing api/v1alpha1/ types)
make install        # Install CRDs into the cluster (~/.kube/config)
make uninstall      # Remove CRDs from the cluster
make deploy         # Deploy the controller to the cluster
make undeploy       # Remove the controller from the cluster
make docker-build   # Build Docker image (also runs tests)
make clean          # Remove bin/ directory
```

After any change to `api/v1alpha1/` types, run **both** `make manifests` and `make generate`.

## Local run setup

`make run` requires these environment variables (get values from an active cluster with `make setup`):

```bash
export WATCH_NAMESPACE=open-cluster-management
export POSTGRES_IMAGE=<from cluster>
export COLLECTOR_IMAGE=<from cluster>
export API_IMAGE=<from cluster>
export INDEXER_IMAGE=<from cluster>
```

`make setup` prints ready-to-run `export` statements with the resolved image values (it uses `$(shell kubectl ...)` substitution, not raw kubectl commands).

## Non-obvious conventions

- **`make test` is slow on first run** — it downloads the kubebuilder envtest binary and Kubernetes API assets to `bin/`. Subsequent runs are fast.
- **`make run` triggers code generation** — it depends on `manifests generate fmt vet`, which downloads `controller-gen` to `bin/` if absent. Use `go run ./main.go` directly to skip this.
- **Single `Search` CR** — the operator is hardcoded to reconcile a CR named `search-v2-operator` (`OperatorName` constant in `controllers/search_controller.go`). There is exactly one per cluster.
- **Pause reconciliation** — annotate the `Search` CR with `search-pause: true` to halt reconciliation without deleting resources (e.g. during maintenance).
- **`make docker-build` runs tests first** — it depends on the `test` target.
- **`make manifests` and `make generate` are separate** — one regenerates CRD/RBAC YAML, the other regenerates Go DeepCopy methods. Both are needed after API type changes.
- **`docs/RBAC.md`** documents the RBAC roles and bindings created by the operator.
- **OLM bundle must be regenerated after RBAC changes** — see the OLM bundle section below.

## OLM bundle

The `bundle/` directory contains the OLM (Operator Lifecycle Manager) bundle used for production installs. It must be kept in sync with `config/rbac/` and `config/manifests/` whenever RBAC rules or the CSV spec change.

```bash
make bundle            # Regenerate bundle/manifests/search-v2-operator.clusterserviceversion.yaml
                       # and bundle/metadata/annotations.yaml from config/rbac/ + config/manifests/
make bundle-validate   # Run operator-sdk bundle validate (informational — see note below)
```

**When to run `make bundle`:**
- Any time you add or remove a `+kubebuilder:rbac` marker in a Go source file (after also running `make manifests` to update `config/rbac/role.yaml`)
- Any time you edit `config/manifests/` directly (e.g. CSV metadata, deployment spec, or webhook definitions)
- Before opening a PR that touches RBAC — the CSV `permissions:`/`clusterPermissions:` blocks in the bundle must match `config/rbac/role.yaml`, otherwise OLM-based installations will be missing the required permissions

**Known false-positive from `make bundle-validate`:** The OLM bundle validator reports `unsupported media type registry+v1 for bundle object` for `ClusterManagementAddOn`. This is a false positive — the `search-collector.clustermanagementaddon.yaml` manifest is intentionally included in the bundle and is valid; the OLM validator simply doesn't know the ACM CRD schema. This error can be ignored. `make bundle-validate` is provided as a separate target (not part of `make bundle`) so generation always succeeds cleanly.

**Important:** `make bundle` also stamps the current timestamp and local `operator-sdk` version into the CSV metadata. These cosmetic diffs are expected and harmless — commit them together with the substantive change.

**Known issue — `serviceAccountName` rewrite (handled automatically):** `operator-sdk generate bundle` v1.34+ copies the kustomize-prefixed SA name (`search-v2-operator-controller-manager`) into the CSV instead of the correct production SA name (`search-v2-operator`). The Makefile `bundle` target applies a `sed` workaround to restore the correct value automatically. This is a known limitation of the `namePrefix: search-v2-operator-` convention in `config/default/kustomization.yaml` and will require a larger kustomize config restructure to fix properly.

## Fleet Engineering Skills

All skills are available as slash commands. See the [Fleet Engineering skills catalog](https://github.com/OpenShift-Fleet/agentic-sdlc/blob/main/skills/README.md) for the full list with when-to-use guidance.

## Personal configuration

Read personal config at the start of any task that needs an assignee, email, or project key.
Use the tool-aware fallback chain: `~/.config/opencode/user.local.md` (OpenCode),
`.claude/user.local.md` (Claude Code), or `.cursor/rules/user.local.mdc` (Cursor, already in context).
If none exist, fall back to agent memory (`user-config`), then placeholders.
Run `make personalize` to generate all three files (if this repo uses Fleet Engineering tooling).

