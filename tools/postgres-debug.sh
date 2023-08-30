#!/bin/bash

# HOW TO RUN THIS SCRIPT:
# oc login ... <RHACM Hub>
# oc exec -it search-postgres-xxxx-xxxx -n open-cluster-management -- /bin/bash < postgres-debug.sh

# DESCRIPTION:
# This script collects data from the Postgres instance to help debug issues with the RHACM search service.

# DATA COLLECTED:
# 1. Resources Statistics:
#    - Total Resource count
#    - Total Cluster count
#    - Resource count by cluster
#    - Resource count by apigroup and kind (top 25)
#    - Namespaces count by cluster
#    - Resources by namespace (average, min, max)
#    - Kubernetes Nodes, CPU, and Memory by cluster
# 2. Edges Statistics:
#    - Total Edge count
#    - Edge count by cluster
#    - Edges by edge type
#    - Intercluster edge count by cluster (hub to cluster)
#    - Total Inter-cluster edges count
# 3. POSTGRESQL DEBUG DATA
#    - POSTGRESQL configuration
#    - POSTGRESQL database size
#    - POSTGRESQL resources table size
#    - POSTGRESQL edges table size
#    - POSTGRESQL query activity
#    - POSTGRESQL running queries
#    - POSTGRESQL idle queries
#    - POSTGRESQL index usage
#    - POSTGRESQL vacuum and table stats

psql -d search -U searchuser -c "SELECT NOW() as script_start_time;"
psql -d search -U searchuser -c "SELECT version();"

printf "\n\n----- COLLECTING RESOURCE STATS -----\n"

printf "\n>>> Total resource count:\n\n"
psql -d search -U searchuser -c "SELECT count(*) FROM search.resources;"

printf "\n>>> Managed cluster count:\n\n"
psql -d search -U searchuser -c "SELECT count(DISTINCT cluster) FROM search.resources;"

printf "\n>>> Resource count by cluster:\n\n"
psql -d search -U searchuser -c "SELECT cluster, count(uid) FROM search.resources GROUP BY cluster ORDER BY count DESC;"

printf "\n>>> Resource count by apigroup and kind (top 25):\n\n"
psql -d search -U searchuser -c "SELECT data->>'apigroup' as apigroup, data->>'kind' as kind, count(uid)
    FROM search.resources GROUP BY apigroup,kind ORDER BY count DESC LIMIT 25;"

printf "\n>>> Namespace count by managed cluster:\n\n"
psql -d search -U searchuser -c "SELECT cluster, count(DISTINCT data->'namespace')
    FROM search.resources GROUP BY cluster ORDER BY count DESC;"

printf "\n>>> Resources by namespace (average, min, max):\n\n"
psql -d search -U searchuser -c "SELECT avg(count) as avg, min(count) as min, max(count) as max
    FROM (SELECT cluster, data->>'namespace' as namespace, count(uid)
    FROM search.resources WHERE data->>'namespace' is not null GROUP BY cluster, namespace) as counts;"

printf "\n>>> Kubernetes Nodes, CPU, and Memory by cluster:\n\n"
psql -d search -U searchuser -c "SELECT cluster, data->'nodes' AS nodes, data->'cpu' AS cpu, data->>'memory' AS memory
    FROM search.resources WHERE data->>'kind' = 'Cluster' ORDER BY nodes DESC;"


printf "\n\n----- COLLECTING EDGES STATS -----\n"

printf "\n>>> Total edges count:\n\n"
psql -d search -U searchuser -c "SELECT count(*) FROM search.edges;"

printf "\n>>> Edge count by cluster:\n\n"
psql -d search -U searchuser -c "SELECT cluster, count(*) FROM search.edges GROUP BY cluster ORDER BY count DESC;"

printf "\n>>> Edge count by type:\n\n"
psql -d search -U searchuser -c "SELECT edgetype, count(*) FROM search.edges GROUP BY edgetype ORDER BY count DESC;"

printf "\n>>> Intercluster edge count by cluster:\n\n"
psql -d search -U searchuser -c "SELECT cluster, count(*) FROM search.edges WHERE edgetype = 'interCluster' GROUP BY cluster ORDER BY count DESC;"

printf "\n>>> Total interCluster edge count:\n\n"
psql -d search -U searchuser -c "SELECT count(*) FROM search.edges WHERE edgetype = 'interCluster';"


printf "\n----- COLLECTING DEBUG DATA FROM POSTGRESQL -----\n"

printf "\n>>> POSTGRESQL Configuration:\n\n"
psql -d search -U searchuser -c "SHOW all;"

printf "\n>>> POSTGRESQL Database size:\n\n"
psql -d search -U searchuser -c "SELECT datname, pg_size_pretty(pg_database_size(datname))
    FROM pg_database order by pg_database_size(datname) desc;"

printf "\n>>> POSTGRESQL resources table size:\n\n"
psql -d search -U searchuser -c "SELECT pg_size_pretty(pg_total_relation_size('search.resources'));"

printf "\n>>> POSTGRESQL edges table size:\n\n"
psql -d search -U searchuser -c "SELECT pg_size_pretty(pg_total_relation_size('search.edges'));"

printf "\n>>> POSTGRESQL Query activity by state:\n\n"
psql -d search -U searchuser -c "SELECT count(*),state FROM pg_stat_activity group by state;"

printf "\n>>> POSTGRESQL Running queries:\n\n"
psql -d search -U searchuser -c "SELECT pid, age(clock_timestamp(), query_start), usename, query
    FROM pg_stat_activity
    WHERE state != 'idle' AND query NOT ILIKE '%pg_stat_activity%' ORDER BY query_start desc;"

printf "\n>>> POSTGRESQL Idle queries:\n\n"
psql -d search -U searchuser -c "SELECT pid, age(clock_timestamp(), query_start), usename, query 
    FROM pg_stat_activity
    WHERE state = 'idle' AND query NOT ILIKE '%pg_stat_activity%'
    ORDER BY query_start desc;"

printf "\n>>> POSTGRESQL Index usage by table:\n\n"
psql -d search -U searchuser -c "SELECT relname, 100 * idx_scan / (seq_scan + idx_scan) percent_of_times_index_used, n_live_tup rows_in_table
    FROM pg_stat_user_tables ORDER BY n_live_tup DESC;"

printf "\n>>> POSTGRESQL Index usage:\n\n"
psql -d search -U searchuser -c "SELECT t.relname AS table_name, i.indexrelname AS index_name, t.seq_scan AS table_scans,
    i.idx_scan AS index_scans, round(i.idx_scan::numeric / (i.idx_scan + t.seq_scan) * 100, 2) AS index_scan_percentage
    FROM pg_stat_user_tables t JOIN pg_stat_user_indexes i ON t.relid = i.relid
    WHERE t.schemaname = 'search' AND t.seq_scan + i.idx_scan > 0
    ORDER BY index_scan_percentage DESC;"

printf "\n>>> POSTGRESQL Vacuum and table stats:\n\n"
psql -d search -U searchuser -c "SELECT relname,n_dead_tup,n_live_tup,last_vacuum,last_analyze,last_autovacuum,last_autoanalyze 
    FROM pg_stat_all_tables 
    WHERE schemaname = 'search';"

psql -d search -U searchuser -c "SELECT NOW() as script_end_time;"