// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"os"
	"strings"

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
	trigger, fileErr := os.ReadFile("postgresql.sql")
	if fileErr != nil {
		log.Error(fileErr, "Error reading trigger sql file")
	}
	var triggers []string
	for _, str := range strings.Split(string(trigger), "|") {
		triggers = append(triggers, strings.TrimSpace(strings.ReplaceAll(str, "\n", " ")))
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

	data["postgresql.sql"] = strings.Join(triggers, "\n")

	cm.Data = data
	log.Info("configmap data populated")

	err := controllerutil.SetControllerReference(instance, cm, r.Scheme)
	if err != nil {
		log.V(2).Info("Could not set control for search-postgres configmap")
	}
	return cm
}
