#!/usr/bin/env bash

# HOW TO RUN THIS SCRIPT:
#   1. oc login ... <RHACM Hub>
#   2. From a machine with oc and awk available (not inside the pod):
#        ./postgres-debug-update-rate.sh
#      Or with options:
#        ./postgres-debug-update-rate.sh -i 30 -c 5
#        ./postgres-debug-update-rate.sh -p search-postgres-<pod>
#   Defaults: namespace open-cluster-management, DB search, tables search.resources
#   and search.edges, 60s interval, 1 sample. Run with -h for all options.
#
# DESCRIPTION:
#   Measures insert/update/delete rates for the ACM Search Postgres tables by
#   sampling pg_stat_user_tables over a time window (via oc exec + psql).
#   Also reports which resource kinds and edge combinations were touched in
#   that window (using xmin / txid), plus a best-effort insert-vs-update split
#   from data.created.
#
# OUTPUT:
#   - Header with target pod, DB, tables, interval, and sample count
#   - Per-sample table: resources and edges rates (ins/min, upd/min, del/min,
#     total/min) and raw update delta
#   - Top kinds touched in the xid window
#   - Kind split: insert_estimate vs update_estimate
#   - Top edge combos (sourceKind | edgeType | destKind)
#   - When samples > 1, averaged rates across samples


set -euo pipefail

NAMESPACE="open-cluster-management"
POD=""
DATABASE="search"
PGUSER="postgres"
SCHEMA="search"
TABLE="resources"
EDGES_TABLE="edges"
INTERVAL_SECONDS=60
SAMPLES=1
KINDS_LIMIT=15
EDGE_COMBOS_LIMIT=15

usage() {
  cat <<'EOF'
Measure PostgreSQL row change rates (updates/min, inserts/min, deletes/min)
for a table running inside an OpenShift pod.

Usage:
  postgres-debug-update-rate.sh [options]

Options:
  -n, --namespace <ns>     Pod namespace (default: open-cluster-management)
  -p, --pod <pod>          Postgres pod name (auto-detected if omitted)
  -d, --database <db>      Database name (default: search)
  -u, --user <user>        Postgres user (default: postgres)
  -s, --schema <schema>    Table schema (default: search)
  -t, --table <table>      Table name (default: resources)
      --edges-table <name> Edge table name (default: edges)
  -i, --interval <secs>    Seconds between samples (default: 60)
  -c, --samples <count>    Number of sample windows (default: 1)
  -k, --kinds-limit <n>    Max kinds to print per sample (default: 15)
      --edge-combos-limit <n>
                            Max edge combos to print per sample (default: 15)
  -h, --help               Show this help

Examples:
  # One 60s sample on search.resources
  ./ocp-postgres-update-rate-report.sh

  # Five 30s samples and averaged output
  ./ocp-postgres-update-rate-report.sh -i 30 -c 5

  # Explicit pod
  ./ocp-postgres-update-rate-report.sh -p search-postgres-bffd4cd6b-kz9qh
EOF
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "ERROR: required command not found: $1" >&2
    exit 1
  }
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      -n|--namespace)
        NAMESPACE="$2"; shift 2;;
      -p|--pod)
        POD="$2"; shift 2;;
      -d|--database)
        DATABASE="$2"; shift 2;;
      -u|--user)
        PGUSER="$2"; shift 2;;
      -s|--schema)
        SCHEMA="$2"; shift 2;;
      -t|--table)
        TABLE="$2"; shift 2;;
      --edges-table)
        EDGES_TABLE="$2"; shift 2;;
      -i|--interval)
        INTERVAL_SECONDS="$2"; shift 2;;
      -c|--samples)
        SAMPLES="$2"; shift 2;;
      -k|--kinds-limit)
        KINDS_LIMIT="$2"; shift 2;;
      --edge-combos-limit)
        EDGE_COMBOS_LIMIT="$2"; shift 2;;
      -h|--help)
        usage; exit 0;;
      *)
        echo "ERROR: unknown argument: $1" >&2
        usage
        exit 1
        ;;
    esac
  done
}

validate_args() {
  [[ "$INTERVAL_SECONDS" =~ ^[0-9]+$ ]] || { echo "ERROR: --interval must be an integer" >&2; exit 1; }
  [[ "$SAMPLES" =~ ^[0-9]+$ ]] || { echo "ERROR: --samples must be an integer" >&2; exit 1; }
  [[ "$KINDS_LIMIT" =~ ^[0-9]+$ ]] || { echo "ERROR: --kinds-limit must be an integer" >&2; exit 1; }
  [[ "$EDGE_COMBOS_LIMIT" =~ ^[0-9]+$ ]] || { echo "ERROR: --edge-combos-limit must be an integer" >&2; exit 1; }
  (( INTERVAL_SECONDS > 0 )) || { echo "ERROR: --interval must be > 0" >&2; exit 1; }
  (( SAMPLES > 0 )) || { echo "ERROR: --samples must be > 0" >&2; exit 1; }
  (( KINDS_LIMIT > 0 )) || { echo "ERROR: --kinds-limit must be > 0" >&2; exit 1; }
  (( EDGE_COMBOS_LIMIT > 0 )) || { echo "ERROR: --edge-combos-limit must be > 0" >&2; exit 1; }
}

detect_pod() {
  if [[ -n "$POD" ]]; then
    return
  fi

  POD="$(oc -n "$NAMESPACE" get pods -o name | sed -n 's#^pod/##p' | grep -m1 -E 'postgres')"
  if [[ -z "$POD" ]]; then
    echo "ERROR: could not auto-detect a postgres pod in namespace '$NAMESPACE'" >&2
    echo "Hint: pass --pod <name> explicitly" >&2
    exit 1
  fi
}

get_table_stats() {
  local schema="$1"
  local table="$2"
  local query
  query="select extract(epoch from now())::bigint,n_tup_ins,n_tup_upd,n_tup_del from pg_stat_user_tables where relid='${schema}.${table}'::regclass;"

  oc -n "$NAMESPACE" exec "$POD" -- \
    psql -U "$PGUSER" -d "$DATABASE" -Atqc "$query"
}

get_txid32() {
  oc -n "$NAMESPACE" exec "$POD" -- \
    psql -U "$PGUSER" -d "$DATABASE" -Atqc "select (txid_current() % 4294967296)::bigint;"
}

get_kind_breakdown() {
  local start_xid="$1"
  local end_xid="$2"
  local query

  query="select coalesce(data->>'kind','<null>') as kind,count(*) as cnt \
from ${SCHEMA}.${TABLE} \
where (xmin::text::bigint) > ${start_xid} and (xmin::text::bigint) <= ${end_xid} \
group by 1 order by 2 desc,1 limit ${KINDS_LIMIT};"

  oc -n "$NAMESPACE" exec "$POD" -- \
    psql -U "$PGUSER" -d "$DATABASE" -AtF '|' -qc "$query"
}

get_kind_split_breakdown() {
  local start_xid="$1"
  local end_xid="$2"
  local start_epoch="$3"
  local end_epoch="$4"
  local query

  query="with touched as ( \
  select coalesce(data->>'kind','<null>') as kind, \
         case \
           when (data->>'created') ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}T' \
             and (data->>'created')::timestamptz between to_timestamp(${start_epoch}) and to_timestamp(${end_epoch}) \
           then 'insert_estimate' \
           else 'update_estimate' \
         end as op \
  from ${SCHEMA}.${TABLE} \
  where (xmin::text::bigint) > ${start_xid} and (xmin::text::bigint) <= ${end_xid} \
), ranked as ( \
  select op, kind, count(*) as cnt, row_number() over (partition by op order by count(*) desc, kind) as rn \
  from touched \
  group by op, kind \
) \
select op, kind, cnt from ranked where rn <= ${KINDS_LIMIT} order by op, cnt desc, kind;"

  oc -n "$NAMESPACE" exec "$POD" -- \
    psql -U "$PGUSER" -d "$DATABASE" -AtF '|' -qc "$query"
}

get_edge_combo_breakdown() {
  local start_xid="$1"
  local end_xid="$2"
  local query

  query="select \
coalesce(sourcekind,'<null>') as source_kind, \
coalesce(edgetype,'<null>') as edge_type, \
coalesce(destkind,'<null>') as dest_kind, \
count(*) as cnt \
from ${SCHEMA}.${EDGES_TABLE} \
where (xmin::text::bigint) > ${start_xid} and (xmin::text::bigint) <= ${end_xid} \
group by 1,2,3 order by 4 desc,1,2,3 limit ${EDGE_COMBOS_LIMIT};"

  oc -n "$NAMESPACE" exec "$POD" -- \
    psql -U "$PGUSER" -d "$DATABASE" -AtF '|' -qc "$query"
}

calc_rate() {
  local delta="$1"
  local elapsed="$2"
  awk -v d="$delta" -v e="$elapsed" 'BEGIN { printf "%.2f", (d * 60.0) / e }'
}

main() {
  require_cmd oc
  require_cmd awk
  parse_args "$@"
  validate_args
  detect_pod

  echo "Postgres update-rate report"
  echo "  Namespace : $NAMESPACE"
  echo "  Pod       : $POD"
  echo "  Database  : $DATABASE"
  echo "  Resources : ${SCHEMA}.${TABLE}"
  echo "  Edges     : ${SCHEMA}.${EDGES_TABLE}"
  echo "  Interval  : ${INTERVAL_SECONDS}s"
  echo "  Samples   : $SAMPLES"
  echo "  Kinds top : $KINDS_LIMIT"
  echo "  Edge top  : $EDGE_COMBOS_LIMIT"
  echo

  printf "%-8s %-8s %-10s %-10s %-10s %-10s %-14s %-14s\n" \
    "sample" "elapsed" "section" "ins/min" "upd/min" "del/min" "total/min" "delta(upd)"

  local i
  local total_ins_rate=0
  local total_upd_rate=0
  local total_del_rate=0
  local total_total_rate=0
  local total_edges_ins_rate=0
  local total_edges_upd_rate=0
  local total_edges_del_rate=0
  local total_edges_total_rate=0

  for (( i=1; i<=SAMPLES; i++ )); do
    local start_res end_res
    local start_edges end_edges
    local start_xid end_xid
    local ts1 ins1 upd1 del1
    local ts2 ins2 upd2 del2
    local ets1 eins1 eupd1 edel1
    local ets2 eins2 eupd2 edel2
    local elapsed dins dupd ddel dtotal
    local edins edupd eddel edtotal
    local rins rupd rdel rtotal
    local erins erupd erdel ertotal

    start_xid="$(get_txid32)"
    start_res="$(get_table_stats "$SCHEMA" "$TABLE")"
    start_edges="$(get_table_stats "$SCHEMA" "$EDGES_TABLE")"
    sleep "$INTERVAL_SECONDS"
    end_res="$(get_table_stats "$SCHEMA" "$TABLE")"
    end_edges="$(get_table_stats "$SCHEMA" "$EDGES_TABLE")"
    end_xid="$(get_txid32)"

    IFS='|' read -r ts1 ins1 upd1 del1 <<<"$start_res"
    IFS='|' read -r ts2 ins2 upd2 del2 <<<"$end_res"
    IFS='|' read -r ets1 eins1 eupd1 edel1 <<<"$start_edges"
    IFS='|' read -r ets2 eins2 eupd2 edel2 <<<"$end_edges"

    if [[ -z "${ts1:-}" || -z "${ts2:-}" || -z "${ets1:-}" || -z "${ets2:-}" ]]; then
      echo "ERROR: failed to read stats for ${SCHEMA}.${TABLE} and/or ${SCHEMA}.${EDGES_TABLE}." >&2
      echo "Resources start: $start_res" >&2
      echo "Resources end  : $end_res" >&2
      echo "Edges start    : $start_edges" >&2
      echo "Edges end      : $end_edges" >&2
      exit 1
    fi

    elapsed=$(( ts2 - ts1 ))
    (( elapsed > 0 )) || elapsed="$INTERVAL_SECONDS"

    dins=$(( ins2 - ins1 ))
    dupd=$(( upd2 - upd1 ))
    ddel=$(( del2 - del1 ))
    dtotal=$(( dins + dupd + ddel ))

    edins=$(( eins2 - eins1 ))
    edupd=$(( eupd2 - eupd1 ))
    eddel=$(( edel2 - edel1 ))
    edtotal=$(( edins + edupd + eddel ))

    rins="$(calc_rate "$dins" "$elapsed")"
    rupd="$(calc_rate "$dupd" "$elapsed")"
    rdel="$(calc_rate "$ddel" "$elapsed")"
    rtotal="$(calc_rate "$dtotal" "$elapsed")"

    erins="$(calc_rate "$edins" "$elapsed")"
    erupd="$(calc_rate "$edupd" "$elapsed")"
    erdel="$(calc_rate "$eddel" "$elapsed")"
    ertotal="$(calc_rate "$edtotal" "$elapsed")"

    printf "%-8s %-8s %-10s %-10s %-10s %-10s %-14s %-14s\n" \
      "$i" "${elapsed}s" "resources" "$rins" "$rupd" "$rdel" "$rtotal" "$dupd"
    printf "%-8s %-8s %-10s %-10s %-10s %-10s %-14s %-14s\n" \
      "" "" "edges" "$erins" "$erupd" "$erdel" "$ertotal" "$edupd"

    echo "  kinds touched (by current row versions written in xid window ${start_xid}-${end_xid}):"
    local kind_rows kind_count=0
    kind_rows="$(get_kind_breakdown "$start_xid" "$end_xid")"
    if [[ -n "$kind_rows" ]]; then
      while IFS='|' read -r kind cnt; do
        [[ -z "${kind:-}" ]] && continue
        printf "    - %-40s %s\n" "$kind" "$cnt"
        kind_count=$(( kind_count + 1 ))
      done <<< "$kind_rows"
    fi
    if (( kind_count == 0 )); then
      echo "    - <none observed in xid window>"
    fi

    echo "  kind split (best-effort: insert vs update estimate from data.created timestamp):"
    local split_rows split_count=0
    split_rows="$(get_kind_split_breakdown "$start_xid" "$end_xid" "$ts1" "$ts2")"
    if [[ -n "$split_rows" ]]; then
      local current_op=""
      while IFS='|' read -r op kind cnt; do
        [[ -z "${op:-}" ]] && continue
        if [[ "$op" != "$current_op" ]]; then
          current_op="$op"
          echo "    ${current_op}:"
        fi
        printf "      - %-38s %s\n" "$kind" "$cnt"
        split_count=$(( split_count + 1 ))
      done <<< "$split_rows"
    fi
    if (( split_count == 0 )); then
      echo "    - <none observed in xid window>"
    fi

    echo "  edge combos touched (top sourceKind+edgeType+destKind in xid window ${start_xid}-${end_xid}):"
    local combo_rows combo_count=0
    combo_rows="$(get_edge_combo_breakdown "$start_xid" "$end_xid")"
    if [[ -n "$combo_rows" ]]; then
      while IFS='|' read -r src ekind dst cnt; do
        [[ -z "${src:-}" ]] && continue
        printf "    - %-22s | %-22s | %-22s %s\n" "$src" "$ekind" "$dst" "$cnt"
        combo_count=$(( combo_count + 1 ))
      done <<< "$combo_rows"
    fi
    if (( combo_count == 0 )); then
      echo "    - <none observed in xid window>"
    fi

    total_ins_rate=$(awk -v a="$total_ins_rate" -v b="$rins" 'BEGIN{printf "%.4f", a+b}')
    total_upd_rate=$(awk -v a="$total_upd_rate" -v b="$rupd" 'BEGIN{printf "%.4f", a+b}')
    total_del_rate=$(awk -v a="$total_del_rate" -v b="$rdel" 'BEGIN{printf "%.4f", a+b}')
    total_total_rate=$(awk -v a="$total_total_rate" -v b="$rtotal" 'BEGIN{printf "%.4f", a+b}')
    total_edges_ins_rate=$(awk -v a="$total_edges_ins_rate" -v b="$erins" 'BEGIN{printf "%.4f", a+b}')
    total_edges_upd_rate=$(awk -v a="$total_edges_upd_rate" -v b="$erupd" 'BEGIN{printf "%.4f", a+b}')
    total_edges_del_rate=$(awk -v a="$total_edges_del_rate" -v b="$erdel" 'BEGIN{printf "%.4f", a+b}')
    total_edges_total_rate=$(awk -v a="$total_edges_total_rate" -v b="$ertotal" 'BEGIN{printf "%.4f", a+b}')
  done

  if (( SAMPLES > 1 )); then
    local avg_ins avg_upd avg_del avg_total
    local avg_eins avg_eupd avg_edel avg_etotal
    avg_ins=$(awk -v s="$total_ins_rate" -v n="$SAMPLES" 'BEGIN{printf "%.2f", s/n}')
    avg_upd=$(awk -v s="$total_upd_rate" -v n="$SAMPLES" 'BEGIN{printf "%.2f", s/n}')
    avg_del=$(awk -v s="$total_del_rate" -v n="$SAMPLES" 'BEGIN{printf "%.2f", s/n}')
    avg_total=$(awk -v s="$total_total_rate" -v n="$SAMPLES" 'BEGIN{printf "%.2f", s/n}')
    avg_eins=$(awk -v s="$total_edges_ins_rate" -v n="$SAMPLES" 'BEGIN{printf "%.2f", s/n}')
    avg_eupd=$(awk -v s="$total_edges_upd_rate" -v n="$SAMPLES" 'BEGIN{printf "%.2f", s/n}')
    avg_edel=$(awk -v s="$total_edges_del_rate" -v n="$SAMPLES" 'BEGIN{printf "%.2f", s/n}')
    avg_etotal=$(awk -v s="$total_edges_total_rate" -v n="$SAMPLES" 'BEGIN{printf "%.2f", s/n}')
    echo
    printf "%-8s %-8s %-10s %-10s %-10s %-10s %-14s\n" "average" "-" "resources" "$avg_ins" "$avg_upd" "$avg_del" "$avg_total"
    printf "%-8s %-8s %-10s %-10s %-10s %-10s %-14s\n" "" "" "edges" "$avg_eins" "$avg_eupd" "$avg_edel" "$avg_etotal"
  fi
}

main "$@"
