# ACM-32474: Implementation Plan — Read-Only PostgreSQL Users for search-v2-api and search-mcp-server

**Jira:** https://redhat.atlassian.net/browse/ACM-35503
**Parent:** https://redhat.atlassian.net/browse/ACM-32474
**Date:** 2026-06-15
**Revision:** 2 — moved implementation to search-v2-operator; expanded scope to cover search-v2-api

---

## Problem

Both `search-v2-api` and `search-mcp-server` connect to the ACM PostgreSQL database using
the `searchuser` read-write credential from the `search-postgres` Secret. This credential
is also used by `search-indexer` to write data.

Both consumers are architecturally read-only (SELECT only), but the database-layer
enforcement is absent. If either application were compromised, the attacker would hold
a credential capable of arbitrary writes, deletes, and schema changes.

---

## Solution Architecture

The `search-v2-operator` is the correct owner of this change. It already:
- creates and owns the `search-postgres` Secret and Deployment
- provisions the database schema, tables, indexes, and triggers via ConfigMap startup scripts
- manages the `search-v2-api` Deployment env vars

The fix adds two dedicated read-only PostgreSQL roles provisioned at database startup.
Applications receive credentials via new Secrets. No Helm hook Jobs, no external images,
no network policy concerns.

**Roles:**

| Role | Privileges | Consumer |
|---|---|---|
| `search_api_ro` | `USAGE ON SCHEMA search`, `SELECT ON search.resources, search.edges` | search-v2-api |
| `search_mcp_ro` | `USAGE ON SCHEMA search`, `SELECT ON search.resources, search.edges` | search-mcp-server |

**Layers of read-only enforcement after this change:**

| Layer | search-v2-api | search-mcp-server |
|---|---|---|
| Database | `search_api_ro` role — SELECT only | `search_mcp_ro` role — SELECT only |
| Application | SQL builder (goqu) — only generates SELECTs | `validateQuery()` — rejects all mutations |

---

## Repos and Files Changed

### `search-v2-operator` (primary)

**`controllers/create_pgsecret.go`** — add two new Secret constructors:

```go
const (
    apiReadonlySecretName = "search-postgres-api-readonly"
    mcpReadonlySecretName = "search-postgres-mcp-readonly"
)

func (r *SearchReconciler) APIReadonlySecret(instance *searchv1alpha1.Search) *corev1.Secret {
    secret := &corev1.Secret{
        ObjectMeta: metav1.ObjectMeta{
            Name:      apiReadonlySecretName,
            Namespace: instance.GetNamespace(),
        },
        Type: corev1.SecretTypeOpaque,
    }
    secret.StringData = map[string]string{
        "database-user":     "search_api_ro",
        "database-password": generatePass(16),
        "database-name":     DBNAME,
    }
    controllerutil.SetControllerReference(instance, secret, r.Scheme)
    return secret
}

func (r *SearchReconciler) MCPReadonlySecret(instance *searchv1alpha1.Search) *corev1.Secret {
    // same pattern, name: mcpReadonlySecretName, user: search_mcp_ro
}
```

The existing `createSecret()` helper in `common.go` only creates if not found — passwords
are stable across reconciles (same pattern as `search-postgres`).

---

**`controllers/create_pgdeployment.go`** — mount new Secret passwords as env vars so the
startup script can set the role passwords:

```go
// Add to the postgres container env:
newSecretEnvVar("READONLY_API_PASSWORD", "database-password", apiReadonlySecretName),
newSecretEnvVar("READONLY_MCP_PASSWORD", "database-password", mcpReadonlySecretName),
```

---

**`controllers/create_pgconfigmap.go`** — two additions:

**1. Append to `postgresql-start.sh`** — create roles using the postgres superuser (peer
auth is available since the script runs as the `postgres` OS user):

```bash
# Create read-only roles
psql -d search -U postgres <<'EOSQL'
DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'search_api_ro') THEN
    CREATE ROLE search_api_ro WITH LOGIN PASSWORD :'READONLY_API_PASSWORD';
  ELSE
    ALTER ROLE search_api_ro WITH PASSWORD :'READONLY_API_PASSWORD';
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'search_mcp_ro') THEN
    CREATE ROLE search_mcp_ro WITH LOGIN PASSWORD :'READONLY_MCP_PASSWORD';
  ELSE
    ALTER ROLE search_mcp_ro WITH PASSWORD :'READONLY_MCP_PASSWORD';
  END IF;
END $$;
GRANT USAGE ON SCHEMA search TO search_api_ro, search_mcp_ro;
GRANT SELECT ON search.resources TO search_api_ro, search_mcp_ro;
GRANT SELECT ON search.edges TO search_api_ro, search_mcp_ro;
ALTER DEFAULT PRIVILEGES IN SCHEMA search
  GRANT SELECT ON TABLES TO search_api_ro, search_mcp_ro;
EOSQL
```

> **Note:** `:'READONLY_API_PASSWORD'` is a psql variable reference, set via `-v` flag or
> environment. The passwords must be passed to psql without appearing in shell arguments
> (to avoid exposure via `/proc/PID/cmdline`). The `PGPASSWORD` pattern or psql `\set`
> from env is the safe approach — exact technique to be finalized during implementation.

> **Note on `GRANT SELECT ON search.edges`:** The `search.edges` table may not exist on
> older clusters. Wrap in a `DO $$ IF EXISTS ...` block or handle the error gracefully.

**2. Append NOTIFY trigger to `postgresql.sql`** — move the content of
`search-v2-api/pkg/database/listenerTrigger.sql` here, so the operator creates the trigger
at database startup. The API no longer needs to create it (and therefore no longer needs
`CREATE TRIGGER` privilege on `search.resources`):

```sql
-- LISTEN/NOTIFY trigger for search-v2-api subscription support
DROP TRIGGER IF EXISTS search_resources_notify_trigger ON search.resources;
DROP FUNCTION IF EXISTS search.notify_resources_change();
CREATE OR REPLACE FUNCTION search.notify_resources_change()
  RETURNS trigger AS $$ ... $$ LANGUAGE plpgsql;
CREATE TRIGGER search_resources_notify_trigger
  AFTER INSERT OR UPDATE OR DELETE ON search.resources
  FOR EACH ROW EXECUTE FUNCTION search.notify_resources_change();
```

---

**`controllers/search_controller.go`** — call `createSecret` for the two new Secrets
before the postgres Deployment is created/updated (following the existing pattern at line
179):

```go
result, err = r.createSecret(ctx, r.APIReadonlySecret(instance))
// handle result/err
result, err = r.createSecret(ctx, r.MCPReadonlySecret(instance))
// handle result/err
```

---

**`controllers/create_apideployment.go`** — change `DB_USER` and `DB_PASS` to reference
the new read-only Secret (lines 22–23):

```go
// before
newSecretEnvVar("DB_USER", "database-user", "search-postgres"),
newSecretEnvVar("DB_PASS", "database-password", "search-postgres"),

// after
newSecretEnvVar("DB_USER", "database-user", apiReadonlySecretName),
newSecretEnvVar("DB_PASS", "database-password", apiReadonlySecretName),
```

`DB_NAME` remains pointing to `search-postgres` (database name is not a credential).

---

### `search-v2-api` (secondary)

**`pkg/database/listener.go`** — remove the trigger/function setup SQL. The trigger now
exists from database startup (provisioned by the operator). The listener only needs to
`LISTEN search_resources_notify`, which requires no special privilege.

The `listenerTrigger.sql` file can be removed or kept as documentation of what the
operator installs.

---

### `search-mcp-server` (minor)

**`helm/acm-mcp-server/templates/secret.yaml`** — change the auto-discovery lookup to use
the read-only Secret keys instead of the admin Secret:

```yaml
# before
{{- $searchSecret := lookup "v1" "Secret" $acmNamespace "search-postgres" }}
{{- $dbUser := index $searchSecret.data "database-user" | b64dec }}
{{- $dbPass := index $searchSecret.data "database-password" | b64dec }}

# after
{{- $mcpSecret := lookup "v1" "Secret" $acmNamespace "search-postgres-mcp-readonly" }}
{{- $dbUser := index $mcpSecret.data "database-user" | b64dec }}
{{- $dbPass := index $mcpSecret.data "database-password" | b64dec }}
```

`$dbName` and `$dbHost` remain derived from `search-postgres` (non-credential fields).

No Go code changes in search-mcp-server.

---

## Upgrade Path

On upgrade from an existing installation:

1. Operator reconcile runs — new Secrets (`search-postgres-api-readonly`,
   `search-postgres-mcp-readonly`) are created first (before Deployment update).
2. Postgres Deployment is updated to add `READONLY_API_PASSWORD` and `READONLY_MCP_PASSWORD`
   env vars — postgres pod restarts.
3. `postgresql-start.sh` runs on restart — creates the new roles idempotently.
4. `search-v2-api` Deployment is updated to reference the new read-only Secret — API pod
   restarts with the new credentials.
5. `search-mcp-server` Helm chart is upgraded — picks up the new `search-postgres-mcp-readonly`
   Secret.

`search-indexer` is unaffected — it continues using the admin `search-postgres` Secret.

---

## Acceptance Criteria

- [ ] `search_api_ro` and `search_mcp_ro` roles created idempotently on each postgres pod restart
- [ ] Both roles have `USAGE ON SCHEMA search`, `SELECT ON search.resources/edges`, and `ALTER DEFAULT PRIVILEGES` for future tables
- [ ] `search-v2-api` pod connects as `search_api_ro` (verified via `SELECT current_user`)
- [ ] `search-mcp-server` pod connects as `search_mcp_ro`
- [ ] `search-indexer` still connects as `searchuser` (unaffected)
- [ ] Admin credentials (`searchuser`) are not mounted in either read-only application pod
- [ ] LISTEN/NOTIFY subscription feature continues to work in `search-v2-api` (trigger pre-created by operator)
- [ ] `make test` passes in `search-v2-operator`
- [ ] `make test-race` passes in `search-v2-api` and `search-mcp-server`

---

## Risks and Open Questions

| Risk / Question | Notes |
|---|---|
| `search.edges` may not exist on all clusters | Wrap GRANT in a conditional; investigate whether all target clusters have this table |
| psql variable syntax for passwords | Use `psql -v READONLY_API_PASSWORD="$READONLY_API_PASSWORD"` or similar — confirm safe quoting to avoid shell injection |
| Postgres peer auth availability | Confirm Red Hat UBI postgres image allows `psql -U postgres` without password from the `postgres` OS user in the startup scripts |
| `listenerTrigger.sql` ownership | Removing trigger setup from `search-v2-api` requires coordinated release with `search-v2-operator` update; operator must ship first |
| Clusters where `search-mcp-server` is deployed before operator upgrade | `search-postgres-mcp-readonly` Secret won't exist yet; Helm lookup will return empty and DSN will be malformed — add guard in `secret.yaml` |

---

## Out of Scope

- Password rotation for `search_api_ro` / `search_mcp_ro`
- Row-level security or column-level restrictions
- Removing `validateQuery()` from search-mcp-server
- Any changes to `search-indexer` credentials
