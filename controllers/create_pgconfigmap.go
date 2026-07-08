// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"
	"strings"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// PostgresConfigmap returns a configmap object for the search postgres controller for the operator.
func (r *SearchReconciler) PostgresConfigmap(instance *searchv1alpha1.Search) *corev1.ConfigMap {
	startScript := "postgresql-start.sh"
	ns := instance.GetNamespace()
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"cluster.open-cluster-management.io/backup": "",
			},
			Name:      postgresConfigmapName,
			Namespace: ns,
		},
	}
	work_mem := r.GetDBConfigFromSearchCR(context.TODO(), instance, "WORK_MEM")
	data := map[string]string{}
	data["custom-postgresql.conf"] = `# Customizations appended to postgresql.conf.
`

	data["postgresql.conf"] = `ssl = 'on'
ssl_cert_file = '/sslcert/tls.crt'
ssl_key_file = '/sslcert/tls.key'
ssl_ciphers = 'HIGH:!aNULL'
max_parallel_workers_per_gather = '8'
statement_timeout = '60000'
logging_collector = 'false'`

	data["postgresql-pre-start.sh"] = `#!/bin/bash
set -euo pipefail
DATA_DIR="/var/lib/pgsql/data"
echo "[INFO] Running before-start.sh pre-check..."
# Check if PG_VERSION exists
PG_VERSION_FILE="$DATA_DIR/userdata/PG_VERSION"
if [[ -f "$PG_VERSION_FILE" ]]; then
   CURRENT_VERSION=$(cat "$PG_VERSION_FILE")
   echo "[INFO] Detected existing PostgreSQL version: $CURRENT_VERSION"
else
   echo "[INFO] No existing PG_VERSION file found. Assuming fresh install."
   CURRENT_VERSION=""
fi
# Determine version of Postgres in this container
INSTALL_VERSION=$(postgres -V | awk '{print $3}' | cut -d. -f1)
echo "[INFO] Container PostgreSQL version: $INSTALL_VERSION"
# Only clear data if versions mismatch
if [[ "$CURRENT_VERSION" != "" && "$CURRENT_VERSION" != "$INSTALL_VERSION" ]]; then
   echo "[INFO] PG_VERSION mismatch ($CURRENT_VERSION vs $INSTALL_VERSION). Clearing data directory..."
   # Remove all files including hidden ones
   # It's okay to delete this data because it will repopulate with fresh data from the collectors.
   rm -rf "$DATA_DIR"/* "$DATA_DIR"/.[!.]*
else
   echo "[INFO] PG_VERSION is up-to-date or no previous data. Keeping existing data."
fi
echo "[INFO] Pre-check complete. Handing off to Postgres..."
`

	data[startScript] = `psql -d search -U searchuser -c "CREATE SCHEMA IF NOT EXISTS search"
psql -d search -U searchuser -c "CREATE TABLE IF NOT EXISTS search.resources (uid TEXT PRIMARY KEY, cluster TEXT, data JSONB)"
psql -d search -U searchuser -c "CREATE TABLE IF NOT EXISTS search.edges (sourceId TEXT, sourceKind TEXT,destId TEXT,destKind TEXT,edgeType TEXT,cluster TEXT, PRIMARY KEY(sourceId, destId, edgeType))"
psql -d search -U searchuser -c "CREATE INDEX IF NOT EXISTS data_kind_idx ON search.resources USING GIN ((data -> 'kind'))"
psql -d search -U searchuser -c "CREATE INDEX IF NOT EXISTS data_namespace_idx ON search.resources USING GIN ((data -> 'namespace'))"
psql -d search -U searchuser -c "CREATE INDEX IF NOT EXISTS data_name_idx ON search.resources USING GIN ((data ->  'name'))"
psql -d search -U searchuser -c "CREATE INDEX IF NOT EXISTS data_cluster_idx ON search.resources USING btree (cluster)"
psql -d search -U searchuser -c "CREATE INDEX IF NOT EXISTS data_composite_idx ON search.resources USING GIN ((data -> '_hubClusterResource'::text), (data -> 'namespace'::text), (data -> 'apigroup'::text), (data -> 'kind_plural'::text))"
psql -d search -U searchuser -c "CREATE INDEX IF NOT EXISTS data_hubCluster_idx ON search.resources USING GIN ((data ->  '_hubClusterResource')) WHERE data ? '_hubClusterResource'"
psql -d search -U searchuser -c "CREATE INDEX IF NOT EXISTS edges_sourceid_idx ON search.edges USING btree (sourceid)"
psql -d search -U searchuser -c "CREATE INDEX IF NOT EXISTS edges_destid_idx ON search.edges USING btree (destid)"
psql -d search -U searchuser -c "CREATE INDEX IF NOT EXISTS edges_cluster_idx ON search.edges USING btree (cluster)"
psql -d search -U searchuser -f /opt/app-root/src/postgresql-start/postgresql.sql
`

	work_memquery := "psql -d search -U searchuser -c \"ALTER ROLE searchuser set work_mem='" + work_mem + "'\""
	data[startScript] = data[startScript] + work_memquery
	// Provision read-only roles for search-v2-api and search-mcp-server.
	// Passwords are supplied via env vars mounted from the readonly Secrets.
	// The psql -v flag passes them as psql variables (:'name') to avoid shell injection.
	// Runs as the postgres superuser (peer auth) since CREATE ROLE requires elevated privilege.
	// psql variable substitution (:'varname') works in plain SQL statements but NOT inside
	// PL/pgSQL DO $$ blocks (the server receives the literal colon-prefixed string).
	// Use \if / \else / \endif psql meta-commands with plain CREATE/ALTER ROLE statements
	// so that :'varname' is substituted by the psql client before sending to the server.
	data[startScript] = data[startScript] + `
psql -d search -U postgres \
  -v "READONLY_API_PASSWORD=$READONLY_API_PASSWORD" \
  -v "READONLY_MCP_PASSWORD=$READONLY_MCP_PASSWORD" << 'EOSQL'
SELECT NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'search_api_ro') AS create_api_role \gset
\if :create_api_role
  CREATE ROLE search_api_ro WITH LOGIN PASSWORD :'READONLY_API_PASSWORD';
\else
  ALTER ROLE search_api_ro WITH PASSWORD :'READONLY_API_PASSWORD';
\endif
SELECT NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'search_mcp_ro') AS create_mcp_role \gset
\if :create_mcp_role
  CREATE ROLE search_mcp_ro WITH LOGIN PASSWORD :'READONLY_MCP_PASSWORD';
\else
  ALTER ROLE search_mcp_ro WITH PASSWORD :'READONLY_MCP_PASSWORD';
\endif
GRANT USAGE ON SCHEMA search TO search_api_ro, search_mcp_ro;
GRANT SELECT ON search.resources TO search_api_ro, search_mcp_ro;
DO $$
BEGIN
  IF EXISTS (
    SELECT FROM information_schema.tables
    WHERE table_schema = 'search' AND table_name = 'edges'
  ) THEN
    EXECUTE 'GRANT SELECT ON search.edges TO search_api_ro, search_mcp_ro';
  END IF;
END $$;
ALTER DEFAULT PRIVILEGES IN SCHEMA search GRANT SELECT ON TABLES TO search_api_ro, search_mcp_ro;
EOSQL
`
	data["postgresql.sql"] = `CREATE OR REPLACE FUNCTION search.intercluster_edges()
  RETURNS TRIGGER AS
$BODY$
BEGIN
IF (TG_OP = 'UPDATE')
THEN
  IF coalesce(NEW.data->>'_hostingSubscription','') <> coalesce(OLD.data->>'_hostingSubscription','')
  THEN
    DELETE FROM search.edges WHERE sourceid=OLD.uid OR destid=OLD.uid AND edgetype='interCluster';
  ELSE
    RETURN NEW;
  END IF;
END IF;
IF (TG_OP = 'INSERT') OR (TG_OP = 'UPDATE')
THEN
  IF NEW.data->>'_hostingSubscription' is not null
  THEN
    INSERT INTO search.edges(sourceid ,sourcekind,destid ,destkind ,edgetype ,cluster)
    SELECT NEW.uid AS sourceid, NEW.data ->> 'kind'::text AS sourcekind, res.uid AS destid,
      res.data ->> 'kind'::text AS destkind,'interCluster'::text AS edgetype, NEW.cluster
    FROM search.resources res
    WHERE data->>'kind' = 'Subscription' AND NEW.data->>'_hostingSubscription' is not null
    AND split_part(NEW.data ->> '_hostingSubscription'::text, '/'::text, 1) = res.data->>'namespace'
    AND split_part(NEW.data ->> '_hostingSubscription'::text, '/'::text, 2) = res.data->>'name'
    AND res.uid <> NEW.uid AND res.cluster <> NEW.cluster AND res.data ->> '_hostingSubscription' IS NULL
    ON CONFLICT (sourceid, destid, edgetype) DO NOTHING;
  ELSEIF NEW.data->>'_hostingSubscription' is null
  THEN
    INSERT INTO search.edges(sourceid ,sourcekind,destid ,destkind ,edgetype ,cluster)
    SELECT res.uid AS sourceid, res.data ->> 'kind'::text AS sourcekind, NEW.uid AS destid,
      NEW.data ->> 'kind'::text AS destkind, 'interCluster'::text AS edgetype, res.cluster
    FROM search.resources res
    WHERE res.data->>'kind' = 'Subscription'
    AND split_part(res.data ->> '_hostingSubscription'::text, '/'::text, 1) = NEW.data->>'namespace'
    AND split_part(res.data ->> '_hostingSubscription'::text, '/'::text, 2) = NEW.data->>'name'
    AND res.uid <> NEW.uid AND res.cluster <> NEW.cluster
    ON CONFLICT (sourceid, destid, edgetype) DO NOTHING;
  END IF;
  RETURN NEW;
ELSEIF (TG_OP = 'DELETE')
THEN
  DELETE FROM search.edges WHERE sourceid=OLD.uid OR destid=OLD.uid AND edgetype='interCluster';
  RETURN OLD;
END IF;
END;
$BODY$
language plpgsql;
DROP TRIGGER IF EXISTS resources_upsert on search.resources;
CREATE TRIGGER resources_upsert AFTER INSERT OR UPDATE ON search.resources FOR EACH ROW WHEN (NEW.data->>'kind' = 'Subscription') EXECUTE PROCEDURE search.intercluster_edges();
DROP TRIGGER IF EXISTS resources_delete on search.resources;
CREATE TRIGGER resources_delete AFTER DELETE ON search.resources FOR EACH ROW WHEN (OLD.data->>'kind' = 'Subscription') EXECUTE PROCEDURE search.intercluster_edges();
-- LISTEN/NOTIFY trigger for search-v2-api WebSocket subscription support.
-- Provisioned here so search-v2-api can connect with a read-only role (search_api_ro)
-- and does not require CREATE TRIGGER privilege on search.resources.
DROP TRIGGER IF EXISTS search_resources_notify_trigger ON search.resources;
DROP FUNCTION IF EXISTS search.notify_resources_change();
CREATE OR REPLACE FUNCTION search.notify_resources_change()
RETURNS trigger AS $$
DECLARE
    notification_payload json;
    new_data_json json;
    old_data_json json;
    new_data_size integer;
    old_data_size integer;
BEGIN
    IF TG_OP = 'DELETE' THEN
        old_data_size := OCTET_LENGTH(OLD.data::text);
        new_data_json := NULL;
        IF old_data_size < 7000 THEN
            old_data_json := OLD.data;
        ELSE
            old_data_json := NULL;
        END IF;
    ELSEIF TG_OP = 'INSERT' THEN
        new_data_size := OCTET_LENGTH(NEW.data::text);
        IF new_data_size < 7000 THEN
            new_data_json := NEW.data;
        ELSE
            new_data_json := NULL;
        END IF;
        old_data_json := NULL;
    ELSEIF TG_OP = 'UPDATE' THEN
        new_data_size := OCTET_LENGTH(NEW.data::text);
        old_data_size := OCTET_LENGTH(OLD.data::text);
        IF (new_data_size + old_data_size) < 7000 THEN
            new_data_json := NEW.data;
            old_data_json := OLD.data;
        ELSEIF old_data_size < 7000 THEN
            new_data_json := NULL;
            old_data_json := OLD.data;
        ELSE
            new_data_json := NULL;
            old_data_json := NULL;
        END IF;
    END IF;
    notification_payload := json_build_object(
        'operation', TG_OP,
        'uid', COALESCE(NEW.uid, OLD.uid),
        'cluster', COALESCE(NEW.cluster, OLD.cluster),
        'newData', new_data_json,
        'oldData', old_data_json,
        'timestamp', NOW()
    );
    IF OCTET_LENGTH(notification_payload::text) < 7500 THEN
        PERFORM pg_notify('search_resources_notify', notification_payload::text);
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    ELSE
        RETURN NEW;
    END IF;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER search_resources_notify_trigger
    AFTER INSERT OR UPDATE OR DELETE ON search.resources
    FOR EACH ROW
    EXECUTE FUNCTION search.notify_resources_change();
`
	cm.Data = data
	log.V(2).Info("Postgres configmap data populated")

	err := controllerutil.SetControllerReference(instance, cm, r.Scheme)
	if err != nil {
		log.V(2).Info("Could not set control for search-postgres configmap")
	}
	return cm
}

func UpdatePostgresConfigmap(existing, new *corev1.ConfigMap) {
	currentPostgresConfig := existing.Data["postgresql.conf"]
	customPostgresConfig := existing.Data["custom-postgresql.conf"]
	defaultPostgresConfig := new.Data["postgresql.conf"]

	// Check if migration is needed custom-postgres.conf was added in ACM 2.15.
	if customPostgresConfig == "" && currentPostgresConfig != defaultPostgresConfig {
		newCustomPostgresConfig := "# Customizations appended to postgresql.conf"
		for _, line := range strings.Split(currentPostgresConfig, "\n") {
			if !strings.Contains(defaultPostgresConfig, line) {
				newCustomPostgresConfig += "\n" + line
			}
		}
		new.Data["custom-postgresql.conf"] = newCustomPostgresConfig
		log.Info("Migrated ConfigMap search-postgres. Moved custom changes to custom-postgresql.conf")
	} else {
		// Merge custom-postgresql.conf into postgresql.conf
		if !strings.Contains(defaultPostgresConfig, customPostgresConfig) {
			new.Data["postgresql.conf"] = defaultPostgresConfig + "\n" + customPostgresConfig
		}
		// Preserve user-defined data in [custom-postgresql.conf]
		if customPostgresConfig != "" {
			new.Data["custom-postgresql.conf"] = customPostgresConfig
		}
	}
}
