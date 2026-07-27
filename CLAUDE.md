# search-v2-operator

Kubernetes operator (kubebuilder/controller-runtime) that deploys and manages all ACM Search components: PostgreSQL, search-indexer, search-v2-api, and search-collector.

For system architecture, data flows, and module layout, see [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

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

**Known issue — `serviceAccountName` gets rewritten:** `make bundle` incorrectly changes `serviceAccountName: search-v2-operator` to `serviceAccountName: search-v2-operator-controller-manager` in three places in the CSV. This is caused by a mismatch between the kustomize `namePrefix: search-v2-operator-` in `config/default/kustomization.yaml` and newer versions of `operator-sdk generate bundle` (v1.34+), which now faithfully copies the prefixed SA name from kustomize output. The production SA is named `search-v2-operator` (no suffix). After running `make bundle`, restore the correct value manually:

```bash
sed -i 's/serviceAccountName: search-v2-operator-controller-manager/serviceAccountName: search-v2-operator/g' \
  bundle/manifests/search-v2-operator.clusterserviceversion.yaml
```

## Fleet Engineering Skills

All skills are available as slash commands. See the [Fleet Engineering skills catalog](https://github.com/OpenShift-Fleet/agentic-sdlc/blob/main/skills/README.md) for the full list with when-to-use guidance.

## Personal configuration

Read personal config at the start of any task that needs an assignee, email, or project key.
Use the tool-aware fallback chain: `~/.config/opencode/user.local.md` (OpenCode),
`.claude/user.local.md` (Claude Code), or `.cursor/rules/user.local.mdc` (Cursor, already in context).
If none exist, fall back to agent memory (`user-config`), then placeholders.
Run `make personalize` to generate all three files (if this repo uses Fleet Engineering tooling).

