#!/bin/bash

# HOW TO RUN THIS SCRIPT:
# oc login ... <RHACM Hub>
# oc exec -it search-postgres-xxxxx -n open-cluster-management -- /bin/bash < postgres-debug.sh

# DESCRIPTION:
# This script collects data from the Postgres instance usage by RHACM to debug issues with the search service.

# DATA COLLECTED:
# 1. POSTGRESQL DEBUG DATA
#    - POSTGRESQL configuration
#    - POSTGRESQL database size
#    - POSTGRESQL resources table size
#    - POSTGRESQL edges table size
#    - POSTGRESQL query activity
#    - POSTGRESQL running queries
#    - POSTGRESQL idle queries
#    - POSTGRESQL index usage
# 2. RHACM Resource Statistics:
#    - Total Resource count
#    - Total Cluster count
#    - Resource count by cluster
#    - Resource count by kind
#    - Namespaces count by cluster
#    - Resource count per cluster/namespace
#    - Kubernetes node count by cluster
# 3. RHACM Edges Statistics:
#    - Total Edge count
#    - Edge count by cluster
#    - Edges by edge type
#    - Intercluster edge count by cluster (hub to cluster)
#    - Total Inter-cluster edges count


printf "\n----- COLLECTING DEBUG DATA FROM POSTGRESQL -----\n"

printf "\n>>> POSTGRESQL Configuration:\n\n"
psql -d search -U searchuser -c "SHOW all;"

printf "\n>>> POSTGRESQL Database size:\n\n"
psql -d search -U searchuser -c "SELECT datname, pg_size_pretty(pg_database_size(datname)) from pg_database order by pg_database_size(datname) desc;"

printf "\n>>> POSTGRESQL resources table size:\n\n"
psql -d search -U searchuser -c "SELECT pg_size_pretty(pg_total_relation_size('search.resources'));"

printf "\n>>> POSTGRESQL edges table size:\n\n"
psql -d search -U searchuser -c "SELECT pg_size_pretty(pg_total_relation_size('search.edges'));"

printf "\n>>> POSTGRESQL Query activity by state:\n\n"
psql -d search -U searchuser -c "SELECT count(*),state FROM pg_stat_activity group by state;"

printf "\n>>> POSTGRESQL Running queries:\n\n"
psql -d search -U searchuser -c "SELECT pid, age(clock_timestamp(), query_start), usename, query FROM pg_stat_activity WHERE state != 'idle' AND query NOT ILIKE '%pg_stat_activity%'  ORDER BY query_start desc;"

printf "\n>>> POSTGRESQL Idle queries:\n\n"
psql -d search -U searchuser -c "SELECT pid, age(clock_timestamp(), query_start), usename, query FROM pg_stat_activity WHERE state = 'idle' AND query NOT ILIKE '%pg_stat_activity%'  ORDER BY query_start desc;"

printf "\n>>> POSTGRESQL Index usage:\n\n"
psql -d search -U searchuser -c "SELECT relname, 100 * idx_scan / (seq_scan + idx_scan) percent_of_times_index_used, n_live_tup rows_in_table
FROM pg_stat_user_tables ORDER BY n_live_tup DESC;"


printf "\n\n----- COLLECTING RESOURCE STATS -----\n"

printf "\n>>> Total resource count:\n\n"
psql -d search -U searchuser -c "SELECT count(*) FROM search.resources;"

printf "\n>>> Total managed cluster count:\n\n"
psql -d search -U searchuser -c "SELECT count(DISTINCT cluster) FROM search.resources;"

printf "\n>>> Resource count by cluster:\n\n"
psql -d search -U searchuser -c "SELECT cluster, count(uid) FROM search.resources GROUP BY cluster ORDER BY count DESC;"

printf "\n>>> Resource count by kind:\n\n"
psql -d search -U searchuser -c "SELECT data->>'apigroup' as apigroup, data->>'kind' as kind, count(uid) FROM search.resources GROUP BY apigroup,kind ORDER BY count DESC;"

printf "\n>>> Namespace count by cluster:\n\n"
psql -d search -U searchuser -c "SELECT cluster, count(DISTINCT data->'namespace') FROM search.resources GROUP BY cluster ORDER BY count DESC;"

printf "\n>>> Kubernetes Nodes, CPU, and Memory by cluster:\n\n"
psql -d search -U searchuser -c "SELECT cluster, data->'nodes' as nodes, data->'cpu' as cpu, data->>'memory' as memory FROM search.resources WHERE data->>'kind' = 'Cluster' ORDER BY nodes DESC;"

printf "\n>>> Resource count by cluster and namespace:\n\n"
psql -d search -U searchuser -c "SELECT cluster, data->>'namespace' as namespace, count(uid) FROM search.resources GROUP BY cluster, namespace ORDER BY cluster, count DESC;"


printf "\n\n----- COLLECTING EDGES STATS -----\n"

printf "\n>>> Total edges count:\n\n"
psql -d search -U searchuser -c "SELECT count(*) FROM search.edges;"

printf "\n>>> Edge count by cluster:\n\n"
psql -d search -U searchuser -c "SELECT cluster, count(*) FROM search.edges GROUP BY cluster ORDER BY count DESC;"

printf "\n>>> Edge count by type:\n\n"
psql -d search -U searchuser -c "SELECT edgetype, count(*) FROM search.edges GROUP BY edgetype ORDER BY count DESC;"

printf "\n>>> Intercluster edge count by cluster:\n\n"
psql -d search -U searchuser -c "SELECT cluster, count(*) FROM search.edges WHERE edgetype = 'intercluster' GROUP BY cluster ORDER BY count DESC;"

printf "/n>>> Total intercluster edge count:\n\n"
psql -d search -U searchuser -c "SELECT count(*) FROM search.edges WHERE edgetype = 'intercluster';"