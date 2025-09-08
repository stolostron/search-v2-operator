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
statement_timeout = '60000'`

	data["postgresql-pre-start.sh"] = `#!/bin/bash
set -euo pipefail
DATA_DIR="/var/lib/pgsql/data"
USER="postgres"
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
   # Ensure ownership first
   chown -R "$USER":"$USER" "$DATA_DIR"
   # Remove all files including hidden ones
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
