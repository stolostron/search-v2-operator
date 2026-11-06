#!/bin/bash

# HOW TO RUN THIS SCRIPT:
#   1. oc login ... <RHACM Hub>
#   2. From a machine with oc and awk available:
#        ./postgres-debug-kind-field-changes.sh --kind Namespace
#
# DESCRIPTION:
#   Samples one kind across xid windows (xmin based, like update-rate) and
#   reports which JSON fields were touched on rows written in each window.
#   Includes top-level fields and per-label-key granularity (label.<key>).
#   Supports multiple windows (default: 3 samples, 60s each) and prints
#   per-sample counts/rates plus an average summary.

set -euo pipefail

NAMESPACE="open-cluster-management"
POD=""
DATABASE="search"
PGUSER="postgres"
SCHEMA="search"
TABLE="resources"
KIND=""
INTERVAL_SECONDS=60
SAMPLES=3
TOP_FIELDS=20
TOP_LABELS=20

usage() {
  cat <<'EOF'
Report touched JSON fields for one kind in search.resources
(xmin/xid windows; top-level fields + label.<key> keys).

Usage:
  postgres-debug-kind-field-changes.sh --kind <Kind> [options]

Options:
  -k, --kind <kind>         Resource kind to inspect (required)
  -n, --namespace <ns>      Pod namespace (default: open-cluster-management)
  -p, --pod <pod>           Postgres pod name (auto-detected if omitted)
  -d, --database <db>       Database name (default: search)
  -u, --user <user>         Postgres user (default: postgres)
  -s, --schema <schema>     Table schema (default: search)
  -t, --table <table>       Table name (default: resources)
  -i, --interval <secs>     Seconds between xid samples (default: 60)
  -c, --samples <count>     Number of sample windows (default: 3)
      --top-fields <n>      Max top-level fields to print (default: 20)
      --top-labels <n>      Max label keys to print (default: 20)
  -h, --help                Show this help

Examples:
  ./postgres-debug-kind-field-changes.sh --kind Namespace
  ./postgres-debug-kind-field-changes.sh --kind Application -i 30 --top-fields 25 --top-labels 30
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
      -k|--kind)
        KIND="$2"; shift 2;;
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
      -i|--interval)
        INTERVAL_SECONDS="$2"; shift 2;;
      -c|--samples)
        SAMPLES="$2"; shift 2;;
      --top-fields)
        TOP_FIELDS="$2"; shift 2;;
      --top-labels)
        TOP_LABELS="$2"; shift 2;;
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
  [[ -n "$KIND" ]] || { echo "ERROR: --kind is required" >&2; exit 1; }
  [[ "$INTERVAL_SECONDS" =~ ^[0-9]+$ ]] || { echo "ERROR: --interval must be an integer" >&2; exit 1; }
  [[ "$SAMPLES" =~ ^[0-9]+$ ]] || { echo "ERROR: --samples must be an integer" >&2; exit 1; }
  [[ "$TOP_FIELDS" =~ ^[0-9]+$ ]] || { echo "ERROR: --top-fields must be an integer" >&2; exit 1; }
  [[ "$TOP_LABELS" =~ ^[0-9]+$ ]] || { echo "ERROR: --top-labels must be an integer" >&2; exit 1; }
  (( INTERVAL_SECONDS > 0 )) || { echo "ERROR: --interval must be > 0" >&2; exit 1; }
  (( SAMPLES > 0 )) || { echo "ERROR: --samples must be > 0" >&2; exit 1; }
  (( TOP_FIELDS > 0 )) || { echo "ERROR: --top-fields must be > 0" >&2; exit 1; }
  (( TOP_LABELS > 0 )) || { echo "ERROR: --top-labels must be > 0" >&2; exit 1; }
}

calc_rate() {
  local delta="$1"
  local elapsed="$2"
  awk -v d="$delta" -v e="$elapsed" 'BEGIN { if (e <= 0) e=1; printf "%.2f", (d * 60.0) / e }'
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

sql_escape_literal() {
  printf "%s" "$1" | sed "s/'/''/g"
}

get_epoch_txid32() {
  oc -n "$NAMESPACE" exec "$POD" -- \
    psql -U "$PGUSER" -d "$DATABASE" -Atqc "select extract(epoch from now())::bigint,(txid_current() % 4294967296)::bigint;"
}

get_touched_summary() {
  local start_xid="$1"
  local end_xid="$2"
  local start_epoch="$3"
  local end_epoch="$4"
  local escaped_kind
  local query
  escaped_kind="$(sql_escape_literal "$KIND")"

  query="with touched as ( \
  select uid, \
         case \
           when (data->>'created') ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}T' \
             and (data->>'created')::timestamptz between to_timestamp(${start_epoch}) and to_timestamp(${end_epoch}) \
           then 'insert_estimate' \
           else 'update_estimate' \
         end as op \
  from ${SCHEMA}.${TABLE} \
  where data->>'kind'='${escaped_kind}' \
    and (xmin::text::bigint) > ${start_xid} \
    and (xmin::text::bigint) <= ${end_xid} \
) \
select op, count(*) from touched group by op \
union all \
select 'total_touched', count(*) from touched \
order by 1;"

  oc -n "$NAMESPACE" exec "$POD" -- \
    psql -U "$PGUSER" -d "$DATABASE" -AtF '|' -qc "$query"
}

get_top_level_fields() {
  local start_xid="$1"
  local end_xid="$2"
  local escaped_kind
  local query
  escaped_kind="$(sql_escape_literal "$KIND")"

  query="select j.key as field, count(*) as cnt \
from ${SCHEMA}.${TABLE} r, lateral jsonb_each(coalesce(r.data,'{}'::jsonb)) j \
where r.data->>'kind'='${escaped_kind}' \
  and (r.xmin::text::bigint) > ${start_xid} \
  and (r.xmin::text::bigint) <= ${end_xid} \
group by 1 order by 2 desc,1 limit ${TOP_FIELDS};"

  oc -n "$NAMESPACE" exec "$POD" -- \
    psql -U "$PGUSER" -d "$DATABASE" -AtF '|' -qc "$query"
}

get_label_fields() {
  local start_xid="$1"
  local end_xid="$2"
  local escaped_kind
  local query
  escaped_kind="$(sql_escape_literal "$KIND")"

  query="select 'label.' || l.key as field, count(*) as cnt \
from ${SCHEMA}.${TABLE} r, lateral jsonb_each(coalesce(r.data->'label','{}'::jsonb)) l \
where r.data->>'kind'='${escaped_kind}' \
  and (r.xmin::text::bigint) > ${start_xid} \
  and (r.xmin::text::bigint) <= ${end_xid} \
group by 1 order by 2 desc,1 limit ${TOP_LABELS};"

  oc -n "$NAMESPACE" exec "$POD" -- \
    psql -U "$PGUSER" -d "$DATABASE" -AtF '|' -qc "$query"
}

print_ranked_rows() {
  local rows="$1"
  local empty_msg="$2"
  local count=0

  if [[ -n "$rows" ]]; then
    while IFS='|' read -r name cnt; do
      [[ -z "${name:-}" ]] && continue
      printf "  - %-48s %s\n" "$name" "$cnt"
      count=$(( count + 1 ))
    done <<< "$rows"
  fi

  if (( count == 0 )); then
    echo "  - $empty_msg"
  fi
}

main() {
  require_cmd oc
  require_cmd awk
  parse_args "$@"
  validate_args
  detect_pod

  echo "Postgres kind field-change report (xmin window)"
  echo "  Namespace : $NAMESPACE"
  echo "  Pod       : $POD"
  echo "  Database  : $DATABASE"
  echo "  Table     : ${SCHEMA}.${TABLE}"
  echo "  Kind      : $KIND"
  echo "  Interval  : ${INTERVAL_SECONDS}s"
  echo "  Samples   : $SAMPLES"
  echo "  Top fields: $TOP_FIELDS"
  echo "  Top labels: $TOP_LABELS"
  echo
  echo "Running... waiting ${INTERVAL_SECONDS}s between samples."

  local i
  local total_touched_sum=0
  local total_insert_sum=0
  local total_update_sum=0
  local total_touched_rate=0
  local total_insert_rate=0
  local total_update_rate=0

  for (( i=1; i<=SAMPLES; i++ )); do
    local start_info end_info
    local start_epoch start_xid end_epoch end_xid elapsed
    local summary_rows summary_total summary_insert_est summary_update_est
    local touched_rate insert_rate update_rate

    start_info="$(get_epoch_txid32)"
    IFS='|' read -r start_epoch start_xid <<< "$start_info"
    sleep "$INTERVAL_SECONDS"
    end_info="$(get_epoch_txid32)"
    IFS='|' read -r end_epoch end_xid <<< "$end_info"
    elapsed=$(( end_epoch - start_epoch ))
    (( elapsed > 0 )) || elapsed="$INTERVAL_SECONDS"

    summary_rows="$(get_touched_summary "$start_xid" "$end_xid" "$start_epoch" "$end_epoch")"
    summary_total=0
    summary_insert_est=0
    summary_update_est=0

    if [[ -n "$summary_rows" ]]; then
      while IFS='|' read -r op cnt; do
        case "$op" in
          total_touched) summary_total="$cnt" ;;
          insert_estimate) summary_insert_est="$cnt" ;;
          update_estimate) summary_update_est="$cnt" ;;
        esac
      done <<< "$summary_rows"
    fi

    touched_rate="$(calc_rate "$summary_total" "$elapsed")"
    insert_rate="$(calc_rate "$summary_insert_est" "$elapsed")"
    update_rate="$(calc_rate "$summary_update_est" "$elapsed")"

    echo
    printf -- "--------------------------------------------------------------------------------\n"
    printf "Sample %d/%d\n" "$i" "$SAMPLES"
    echo "xid window : ${start_xid} -> ${end_xid}"
    echo "time window: ${start_epoch} -> ${end_epoch} (${elapsed}s)"
    printf "touched=%s (%.2f/min)  insert_estimate=%s (%.2f/min)  update_estimate=%s (%.2f/min)\n" \
      "$summary_total" "$touched_rate" "$summary_insert_est" "$insert_rate" "$summary_update_est" "$update_rate"

    echo
    echo "Top touched top-level fields"
    echo "----------------------------"
    print_ranked_rows "$(get_top_level_fields "$start_xid" "$end_xid")" "<none touched in xid window>"

    echo
    echo "Top touched label keys"
    echo "----------------------"
    print_ranked_rows "$(get_label_fields "$start_xid" "$end_xid")" "<none touched in xid window>"

    total_touched_sum=$(( total_touched_sum + summary_total ))
    total_insert_sum=$(( total_insert_sum + summary_insert_est ))
    total_update_sum=$(( total_update_sum + summary_update_est ))
    total_touched_rate=$(awk -v a="$total_touched_rate" -v b="$touched_rate" 'BEGIN{printf "%.4f", a+b}')
    total_insert_rate=$(awk -v a="$total_insert_rate" -v b="$insert_rate" 'BEGIN{printf "%.4f", a+b}')
    total_update_rate=$(awk -v a="$total_update_rate" -v b="$update_rate" 'BEGIN{printf "%.4f", a+b}')
  done

  if (( SAMPLES > 1 )); then
    local avg_touched avg_insert avg_update
    local avg_touched_rate avg_insert_rate avg_update_rate
    avg_touched=$(awk -v s="$total_touched_sum" -v n="$SAMPLES" 'BEGIN{printf "%.2f", s/n}')
    avg_insert=$(awk -v s="$total_insert_sum" -v n="$SAMPLES" 'BEGIN{printf "%.2f", s/n}')
    avg_update=$(awk -v s="$total_update_sum" -v n="$SAMPLES" 'BEGIN{printf "%.2f", s/n}')
    avg_touched_rate=$(awk -v s="$total_touched_rate" -v n="$SAMPLES" 'BEGIN{printf "%.2f", s/n}')
    avg_insert_rate=$(awk -v s="$total_insert_rate" -v n="$SAMPLES" 'BEGIN{printf "%.2f", s/n}')
    avg_update_rate=$(awk -v s="$total_update_rate" -v n="$SAMPLES" 'BEGIN{printf "%.2f", s/n}')

    echo
    echo "SUMMARY (averages across samples)"
    echo "---------------------------------"
    printf "touched avg=%s (%.2f/min)  insert_estimate avg=%s (%.2f/min)  update_estimate avg=%s (%.2f/min)\n" \
      "$avg_touched" "$avg_touched_rate" "$avg_insert" "$avg_insert_rate" "$avg_update" "$avg_update_rate"
  fi

  echo
  echo "Note: xmin-window mode reports fields on rows written in the window."
}

main "$@"
