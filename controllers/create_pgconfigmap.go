// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// PostgresConfigmap returns a configmap object for the search postgres controller for the operator.
func (r *SearchReconciler) PostgresConfigmap(instance *searchv1alpha1.Search) *corev1.ConfigMap {

	ns := instance.GetNamespace()
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      postgresConfigmapName,
			Namespace: ns,
		},
	}
	Work_Mem := r.getDBConfig(context.TODO(), instance, "WORK_MEM")
	data := map[string]string{}
	data["postgresql.conf"] = `
ssl = 'on'
ssl_cert_file = '/sslcert/tls.crt'
ssl_key_file = '/sslcert/tls.key'`

	data["postgresql-start.sh"] = `
psql -d search -U searchuser -c "CREATE SCHEMA IF NOT EXISTS search"
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
psql -d search -U searchuser -f /opt/app-root/src/postgresql-start/postgresql.sql
`

	work_memquery := "psql -d search -U searchuser -c \"ALTER ROLE searchuser set work_mem='" + Work_Mem + "'\""
	data["postgresql-start.sh"] = data["postgresql-start.sh"] + work_memquery
	data["postgresql.sql"] = `
	CREATE OR REPLACE FUNCTION search.intercluster_edges() RETURNS TRIGGER AS $BODY$ BEGIN if(TG_OP = 'UPDATE') then if coalesce(NEW.data->>'_hostingSubscription','') <> coalesce(OLD.data->>'_hostingSubscription','') then DELETE FROM search.edges where sourceid=OLD.uid OR destid=OLD.uid and edgetype='interCluster'; end if; end if; if(TG_OP = 'INSERT') or (TG_OP = 'UPDATE') then if NEW.data->>'_hostingSubscription' is not null then INSERT INTO search.edges(sourceid ,sourcekind,destid ,destkind ,edgetype ,cluster) SELECT NEW.uid AS sourceid, NEW.data ->> 'kind'::text AS sourcekind, res.uid AS destid, res.data ->> 'kind'::text AS destkind,'interCluster'::text AS edgetype, NEW.cluster from search.resources res where data->>'kind' = 'Subscription' and NEW.data->>'_hostingSubscription' is not null and split_part(NEW.data ->> '_hostingSubscription'::text, '/'::text, 1) = res.data->>'namespace' and split_part(NEW.data ->> '_hostingSubscription'::text, '/'::text, 2) = res.data->>'name' and res.uid <> NEW.uid and res.cluster <> NEW.cluster and res.data ->> '_hostingSubscription' IS NULL ON CONFLICT (sourceid, destid, edgetype) DO NOTHING; elsif NEW.data->>'_hostingSubscription' is null then INSERT INTO search.edges(sourceid ,sourcekind,destid ,destkind ,edgetype ,cluster) SELECT res.uid AS sourceid, res.data ->> 'kind'::text AS sourcekind, NEW.uid AS destid, NEW.data ->> 'kind'::text AS destkind, 'interCluster'::text AS edgetype, res.cluster from search.resources res where res.data->>'kind' = 'Subscription' and split_part(res.data ->> '_hostingSubscription'::text, '/'::text, 1) = NEW.data->>'namespace' and split_part(res.data ->> '_hostingSubscription'::text, '/'::text, 2) = NEW.data->>'name' and res.uid <> NEW.uid and res.cluster <> NEW.cluster ON CONFLICT (sourceid, destid, edgetype) DO NOTHING; end if; RETURN NEW; elsif(TG_OP = 'DELETE') then DELETE FROM search.edges where sourceid=OLD.uid OR destid=OLD.uid and edgetype='interCluster'; RETURN OLD; end if;END; $BODY$ language plpgsql; DROP TRIGGER IF EXISTS resources_upsert on search.resources; CREATE TRIGGER resources_upsert AFTER INSERT OR UPDATE ON search.resources FOR EACH ROW WHEN (NEW.data->>'kind' = 'Subscription') EXECUTE PROCEDURE search.intercluster_edges(); DROP TRIGGER IF EXISTS resources_delete on search.resources; CREATE TRIGGER resources_delete AFTER DELETE ON search.resources FOR EACH ROW WHEN (OLD.data->>'kind' = 'Subscription') EXECUTE PROCEDURE search.intercluster_edges();`
	cm.Data = data
	log.V(2).Info("configmap data populated")

	err := controllerutil.SetControllerReference(instance, cm, r.Scheme)
	if err != nil {
		log.V(2).Info("Could not set control for search-postgres configmap")
	}
	return cm
}
