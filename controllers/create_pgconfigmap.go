// Copyright Contributors to the Open Cluster Management project
package controllers

import (
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

	data := map[string]string{}
	data["postgresql.conf"] = `
ssl = 'on'
ssl_cert_file = '/sslcert/tls.crt'
ssl_key_file = '/sslcert/tls.key'`

	data["postgresql-start.sh"] = `
psql -d search -U searchuser -c "CREATE SCHEMA IF NOT EXISTS search"
psql -d search -U searchuser -c "CREATE TABLE IF NOT EXISTS search.resources (uid TEXT PRIMARY KEY, cluster TEXT, data JSONB, kind_plural TEXT)"
psql -d search -U searchuser -c "CREATE TABLE IF NOT EXISTS search.edges (sourceId TEXT, sourceKind TEXT,destId TEXT,destKind TEXT,edgeType TEXT,cluster TEXT, PRIMARY KEY(sourceId, destId, edgeType))"
psql -d search -U searchuser -c "CREATE INDEX IF NOT EXISTS data_kind_idx ON search.resources USING GIN ((data -> 'kind'))"
psql -d search -U searchuser -c "CREATE INDEX IF NOT EXISTS data_namespace_idx ON search.resources USING GIN ((data -> 'namespace'))"
psql -d search -U searchuser -c "CREATE INDEX IF NOT EXISTS data_name_idx ON search.resources USING GIN ((data ->  'name'))"
psql -d search -U searchuser -c "CREATE INDEX edges_sourceid_idx ON search.edges USING btree (sourceid)"
psql -d search -U searchuser -c "CREATE INDEX edges_destid_idx ON search.edges USING btree (destid)"`

	data["postgresql.sql"] = `psql -d search -U searchuser -c "CREATE OR REPLACE FUNCTION search.intercluster_edges() RETURNS TRIGGER AS
	$BODY$
	BEGIN
	raise notice 'running intercluster_edges: %', NEW;
	
	if  (TG_OP = 'UPDATE') then
	raise notice 'UPDATE : ';
	
	-- Incoming subscription is the remote subscription
	
	if coalesce(NEW.data->>'_hostingSubscription','') <> coalesce(OLD.data->>'_hostingSubscription','') then
	raise notice 'OLD and NEW _hostingSubscription not matching: %', NEW.data->>'_hostingSubscription';
	raise notice 'OLD _hostingSubscription: %', OLD.data->>'_hostingSubscription';
	raise notice 'NEW _hostingSubscription: %', NEW.data->>'_hostingSubscription';
	raise notice 'delete: %', OLD.uid';
	
		DELETE FROM search.edges
		 where sourceid=OLD.uid OR destid=OLD.uid
		 and edgetype='interCluster';
	end if;
	end if;
	
	if  (TG_OP = 'INSERT') or (TG_OP = 'UPDATE')  then
	raise notice 'INSERT or UPDATE: ';
	raise notice 'NEW uid: %', NEW.uid;
	raise notice 'NEW _hostingSubscription: %', NEW._hostingSubscription;
	raise notice 'NEW cluster: %', NEW.cluster;
	
	-- Incoming subscription is the remote subscription
	
	if NEW.data->>'_hostingSubscription' is not null then
	raise notice '_hostingSubscription is not null: %', NEW.data->>'_hostingSubscription';
	
	 INSERT INTO search.edges(sourceid ,sourcekind,destid ,destkind ,edgetype ,cluster)
	 SELECT NEW.uid AS sourceid,
			NEW.data ->> 'kind'::text AS sourcekind,
			res.uid AS destid,
			 res.data ->> 'kind'::text AS destkind,
			'interCluster'::text AS edgetype,
			NEW.cluster
		from  search.resources res
	where data->>'kind' = 'Subscription' and
	NEW.data->>'_hostingSubscription' is not null
	and split_part(NEW.data ->> '_hostingSubscription'::text, '/'::text, 1) = res.data->>'namespace' 
	and split_part(NEW.data ->> '_hostingSubscription'::text, '/'::text, 2) = res.data->>'name'
	and  res.uid <> NEW.uid
	ON CONFLICT (sourceid, destid, edgetype) 
	DO NOTHING; 
	end if;
	
	-- Incoming subscription is the hub subscription
	
	if NEW.data->>'_hostingSubscription' is null then
	raise notice '_hostingSubscription is null: %', NEW.data->>'_hostingSubscription';
	
	 INSERT INTO search.edges(sourceid ,sourcekind,destid ,destkind ,edgetype ,cluster)
	 SELECT res.uid AS sourceid,
			 res.data ->> 'kind'::text AS sourcekind,
			 NEW.uid AS destid,
			NEW.data ->> 'kind'::text AS destkind,
			'deployedBy'::text AS edgetype,
			res.cluster
		from  search.resources res
	where res.data->>'kind' = 'Subscription' 
	and split_part(res.data ->> '_hostingSubscription'::text, '/'::text, 1) = NEW.data->>'namespace' 
	and split_part(res.data ->> '_hostingSubscription'::text, '/'::text, 2) = NEW.data->>'name'
	and  res.uid <> NEW.uid
	ON CONFLICT (sourceid, destid, edgetype) 
	DO NOTHING; 
	end if;
	
	end if;
	
	
	 if  (TG_OP = 'DELETE') then
	 raise notice 'delete: %', OLD.data->>'_hostingSubscription';
	
		 DELETE FROM search.edges
		 where sourceid=OLD.uid OR destid=OLD.uid
		 and edgetype='interCluster';
	 end if;
	RETURN NEW;
	END;
	$BODY$
	language plpgsql;"
	psql -d search -U searchuser -c "DROP TRIGGER IF EXISTS resources_upsert on search.resources;"
	psql -d search -U searchuser -c "CREATE TRIGGER resources_upsert
		AFTER INSERT OR UPDATE ON "search.resources"
		FOR EACH ROW 
		WHEN (NEW.data->>'kind' = 'Subscription')
		EXECUTE PROCEDURE search.intercluster_edges();"
	psql -d search -U searchuser -c "DROP TRIGGER IF EXISTS resources_delete on search.resources;"
	psql -d search -U searchuser -c "CREATE TRIGGER resources_delete
		AFTER DELETE ON "search.resources"
		FOR EACH ROW 
		WHEN (OLD.data->>'kind' = 'Subscription')
		EXECUTE PROCEDURE search.intercluster_edges();"`

	cm.Data = data
	log.Info("configmap data populated")

	err := controllerutil.SetControllerReference(instance, cm, r.Scheme)
	if err != nil {
		log.V(2).Info("Could not set control for search-postgres configmap")
	}
	return cm
}
