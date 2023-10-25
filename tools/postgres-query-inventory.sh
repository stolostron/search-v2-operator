#!/bin/bash
# Inventory of Postgres queries used by search.
# Executes each query against the database to gather performance data.
#
# HOW TO RUN THIS SCRIPT:
#   oc login ... <RHACM Hub>
#   bash ./postgres-query-inventory.sh

echo "Gathering execution data for the search queries..."

NAMESPACE=open-cluster-management
POSTGRES_POD=$(oc get pods -n ${NAMESPACE} | grep search-postgres | awk '{print $1}')

# Select a random cluster name and UID to use in the queries.
RANDOM_CLUSTER=$(oc exec -t ${POSTGRES_POD} -n ${NAMESPACE} -- psql -d search -U searchuser --csv -c "SELECT cluster from search.resources ORDER BY RANDOM() LIMIT 1;" | tail -1)
RANDOM_UID=$(oc exec -t ${POSTGRES_POD} -n ${NAMESPACE} -- psql -d search -U searchuser --csv -c "SELECT uid from search.resources ORDER BY RANDOM() LIMIT 1;" | tail -1)
echo "RANDOM UID: ${RANDOM_UID}"
echo "RANDOM CLUSTER: ${RANDOM_CLUSTER}"

QUERY_INVENTORY=(
    "SELECT count(*) FROM search.resources;"
    "EXPLAIN ANALYZE SELECT uid, data FROM search.resources WHERE cluster='${RANDOM_CLUSTER}' AND uid!='cluster__${RANDOM_CLUSTER}';"
    "EXPLAIN ANALYZE SELECT count(*) FROM search.resources WHERE cluster='${RANDOM_CLUSTER}' AND data->'_hubClusterResource' IS NOT NULL;"
    "EXPLAIN ANALYZE SELECT sourceid, edgetype, destid FROM search.edges WHERE edgetype!='interCluster' AND cluster='${RANDOM_CLUSTER}';"
    "EXPLAIN DELETE from search.resources WHERE uid IN ('${RANDOM_UID}');"
    "EXPLAIN ANALYZE SELECT DISTINCT data->'name' from search.resources;"
    "EXPLAIN ANALYZE SELECT DISTINCT data->'name' as d from search.resources ORDER BY d ASC LIMIT 1000000;"
    "EXPLAIN ANALYZE SELECT DISTINCT data->'name' as d from search.resources ORDER BY d ASC;"
    "EXPLAIN ANALYZE SELECT DISTINCT data->'status' from search.resources;"
    "EXPLAIN ANALYZE SELECT DISTINCT data->'status' as d from search.resources ORDER BY d ASC;"
    "EXPLAIN ANALYZE SELECT * from search.resources WHERE data->'kind' ? 'Secret';"
    "EXPLAIN ANALYZE SELECT * from search.resources WHERE data->>'kind' = 'Secret';"
    "EXPLAIN ANALYZE SELECT * from search.resources WHERE data->>'kind' ILIKE 'secret';"
    "EXPLAIN ANALYZE SELECT * from search.resources WHERE data->'kind' ?| ARRAY['Secret','Pod'];"
    "EXPLAIN ANALYZE SELECT * from search.resources WHERE data->'status' ?| ARRAY['Running','Failed'];"
    "EXPLAIN ANALYZE SELECT * from search.resources WHERE data->'label' @> '{\"app\":\"search\"}';"
   )

# Executes the queries.
for query in "${QUERY_INVENTORY[@]}"
do
    oc exec -t ${POSTGRES_POD} -n ${NAMESPACE} -- psql -d search -U searchuser -e -c "$query"
done

