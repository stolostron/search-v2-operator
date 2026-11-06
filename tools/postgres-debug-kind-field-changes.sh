#!/bin/bash

# HOW TO RUN THIS SCRIPT:
#   1. oc login ... <RHACM Hub>
#   2. From a machine with oc and awk available:
#        ./postgres-debug-kind-field-changes.sh --kind Namespace
#
# DESCRIPTION:
#   Samples one kind across xid windows (xmin based, like update-rate) and
#   compares each sample to the previous sample to report actual JSON field
#   value changes. Includes top-level fields and per-label-key granularity
#   (label.<key>). Supports multiple windows (default: 3 samples, 60s each)
#   and prints per-sample counts/rates plus an average summary.

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
Report JSON field value changes for one kind in search.resources
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
  -c, --samples <count>     Number of sample windows (default: 3; must be > 1)
      --top-fields <n>      Max top-level fields to print (default: 20)
      --top-labels <n>      Max label keys to print (default: 20)
  -h, --help                Show this help

Examples:
  ./postgres-debug-kind-field-changes.sh --kind Namespace -c 3
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
  (( SAMPLES > 1 )) || { echo "ERROR: --samples must be > 1 (sample #1 is baseline)" >&2; exit 1; }
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

  POD="$(oc -n "$NAMESPACE" get pods -o name | sed -n 's#^pod/##p' | { grep -m1 -E 'postgres' || true; })"
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

get_touched_field_hashes() {
  local start_xid="$1"
  local end_xid="$2"
  local escaped_kind
  local query
  escaped_kind="$(sql_escape_literal "$KIND")"

  query="with touched as ( \
  select uid, data \
  from ${SCHEMA}.${TABLE} \
  where data->>'kind'='${escaped_kind}' \
    and (xmin::text::bigint) > ${start_xid} \
    and (xmin::text::bigint) <= ${end_xid} \
) \
select t.uid, j.key as field, md5(j.value::text) as vhash \
from touched t, lateral jsonb_each(coalesce(t.data,'{}'::jsonb)) j \
union all \
select t.uid, 'label.' || l.key as field, md5(l.value::text) as vhash \
from touched t, lateral jsonb_each(coalesce(t.data->'label','{}'::jsonb)) l;"

  oc -n "$NAMESPACE" exec "$POD" -- \
    psql -U "$PGUSER" -d "$DATABASE" -AtF $'\t' -qc "$query"
}

print_ranked_file() {
  local file="$1"
  local top_n="$2"
  local empty_msg="$3"
  local count=0

  if [[ -s "$file" ]]; then
    while IFS=$'\t' read -r cnt name; do
      [[ -z "${name:-}" ]] && continue
      printf "  - %-48s %s\n" "$name" "$cnt"
      count=$(( count + 1 ))
    done < <(LC_ALL=C sort -t $'\t' -k1,1nr -k2,2 "$file" | head -n "$top_n")
  fi

  if (( count == 0 )); then
    echo "  - $empty_msg"
  fi
}

main() {
  require_cmd oc
  require_cmd awk
  require_cmd sort
  require_cmd join
  parse_args "$@"
  validate_args
  detect_pod

  local tmpdir
  tmpdir="$(mktemp -d)"
  trap 'rm -rf "$tmpdir"' EXIT

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
  echo "Note: sample #1 establishes baseline; change counts start at sample #2."

  local i
  local start_info end_info
  local total_touched_sum=0
  local total_insert_sum=0
  local total_update_sum=0
  local total_touched_rate=0
  local total_insert_rate=0
  local total_update_rate=0
  local total_field_changes_sum=0
  local total_label_changes_sum=0

  local prev_hash_file curr_hash_file prev_idx_file curr_idx_file joined_file
  local all_directional_file all_collapsed_file label_directional_file label_collapsed_file
  prev_hash_file="$tmpdir/prev-hashes.tsv"
  curr_hash_file="$tmpdir/curr-hashes.tsv"
  prev_idx_file="$tmpdir/prev-idx.tsv"
  curr_idx_file="$tmpdir/curr-idx.tsv"
  joined_file="$tmpdir/joined.tsv"
  all_directional_file="$tmpdir/all-directional.tsv"
  all_collapsed_file="$tmpdir/all-collapsed.tsv"
  label_directional_file="$tmpdir/label-directional.tsv"
  label_collapsed_file="$tmpdir/label-collapsed.tsv"

  : > "$prev_hash_file"

  end_info="$(get_epoch_txid32)"

  for (( i=1; i<=SAMPLES; i++ )); do
    local start_epoch start_xid end_epoch end_xid elapsed
    local summary_rows summary_total summary_insert_est summary_update_est
    local touched_rate insert_rate update_rate
    local window_total_field_entries window_total_label_entries

    start_info="$end_info"
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

    get_touched_field_hashes "$start_xid" "$end_xid" > "$curr_hash_file"
    window_total_field_entries=$(awk -F $'\t' 'NF>=3{if($2 !~ /^label\./) c++} END{print c+0}' "$curr_hash_file")
    window_total_label_entries=$(awk -F $'\t' 'NF>=3{if($2 ~ /^label\./) c++} END{print c+0}' "$curr_hash_file")

    echo
    printf -- "--------------------------------------------------------------------------------\n"
    printf "Sample %d/%d\n" "$i" "$SAMPLES"
    echo "xid window : ${start_xid} -> ${end_xid}"
    echo "time window: ${start_epoch} -> ${end_epoch} (${elapsed}s)"
    printf "touched=%s (%.2f/min)  insert_estimate=%s (%.2f/min)  update_estimate=%s (%.2f/min)\n" \
      "$summary_total" "$touched_rate" "$summary_insert_est" "$insert_rate" "$summary_update_est" "$update_rate"
    printf "window field entries=%s  window label entries=%s\n" "$window_total_field_entries" "$window_total_label_entries"

    if (( i == 1 )); then
      echo
      echo "Baseline sample: captured current field values for next-window diff."
      echo "No change report for sample #1."
    else
      awk -F $'\t' 'BEGIN{OFS="\t"} NF>=3 {print $1 "\034" $2, $3}' "$prev_hash_file" > "$prev_idx_file"
      awk -F $'\t' 'BEGIN{OFS="\t"} NF>=3 {print $1 "\034" $2, $3}' "$curr_hash_file" > "$curr_idx_file"
      LC_ALL=C sort -t $'\t' -k1,1 "$prev_idx_file" -o "$prev_idx_file"
      LC_ALL=C sort -t $'\t' -k1,1 "$curr_idx_file" -o "$curr_idx_file"
      join -t $'\t' -a 1 -a 2 -e '' -o '0,1.2,2.2' "$prev_idx_file" "$curr_idx_file" > "$joined_file"

      awk -F $'\t' \
        -v all_dir="$all_directional_file" \
        -v all_flat="$all_collapsed_file" \
        -v lbl_dir="$label_directional_file" \
        -v lbl_flat="$label_collapsed_file" '
        {
          key=$1; oldv=$2; newv=$3
          split(key, a, "\034")
          field=a[2]
          if (field == "") next

          if (oldv=="" && newv!="") d="+" field
          else if (oldv!="" && newv=="") d="-" field
          else if (oldv!=newv) d="~" field
          else next

          all_dir_cnt[d]++
          all_flat_cnt[field]++
          if (field ~ /^label\./) {
            lbl_dir_cnt[d]++
            lbl_flat_cnt[field]++
          }
        }
        END {
          for (k in all_dir_cnt)  print all_dir_cnt[k] "\t" k > all_dir
          for (k in all_flat_cnt) print all_flat_cnt[k] "\t" k > all_flat
          for (k in lbl_dir_cnt)  print lbl_dir_cnt[k] "\t" k > lbl_dir
          for (k in lbl_flat_cnt) print lbl_flat_cnt[k] "\t" k > lbl_flat
        }
      ' "$joined_file"

      local sample_field_changes sample_label_changes
      sample_field_changes=$(awk -F $'\t' '{s+=$1} END{print s+0}' "$all_directional_file" 2>/dev/null || echo 0)
      sample_label_changes=$(awk -F $'\t' '{s+=$1} END{print s+0}' "$label_directional_file" 2>/dev/null || echo 0)

      echo
      echo "Actual field value changes vs previous sample"
      echo "--------------------------------------------"
      echo "total field changes: $sample_field_changes"
      echo "total label changes: $sample_label_changes"

      echo
      echo "Top changed top-level fields (collapsed)"
      echo "-----------------------------------------"
      awk -F $'\t' 'NF>=2 && $2 !~ /^label\./ {print}' "$all_collapsed_file" > "$tmpdir/top-level-collapsed.tsv" || true
      print_ranked_file "$tmpdir/top-level-collapsed.tsv" "$TOP_FIELDS" "<none changed vs previous sample>"

      echo
      echo "Top changed label keys (collapsed)"
      echo "----------------------------------"
      print_ranked_file "$label_collapsed_file" "$TOP_LABELS" "<none changed vs previous sample>"

      total_field_changes_sum=$(( total_field_changes_sum + sample_field_changes ))
      total_label_changes_sum=$(( total_label_changes_sum + sample_label_changes ))
    fi

    total_touched_sum=$(( total_touched_sum + summary_total ))
    total_insert_sum=$(( total_insert_sum + summary_insert_est ))
    total_update_sum=$(( total_update_sum + summary_update_est ))
    total_touched_rate=$(awk -v a="$total_touched_rate" -v b="$touched_rate" 'BEGIN{printf "%.4f", a+b}')
    total_insert_rate=$(awk -v a="$total_insert_rate" -v b="$insert_rate" 'BEGIN{printf "%.4f", a+b}')
    total_update_rate=$(awk -v a="$total_update_rate" -v b="$update_rate" 'BEGIN{printf "%.4f", a+b}')

    cp "$curr_hash_file" "$prev_hash_file"
  done

  if (( SAMPLES > 1 )); then
    local avg_touched avg_insert avg_update
    local avg_touched_rate avg_insert_rate avg_update_rate
    local diff_windows avg_field_changes avg_label_changes
    avg_touched=$(awk -v s="$total_touched_sum" -v n="$SAMPLES" 'BEGIN{printf "%.2f", s/n}')
    avg_insert=$(awk -v s="$total_insert_sum" -v n="$SAMPLES" 'BEGIN{printf "%.2f", s/n}')
    avg_update=$(awk -v s="$total_update_sum" -v n="$SAMPLES" 'BEGIN{printf "%.2f", s/n}')
    avg_touched_rate=$(awk -v s="$total_touched_rate" -v n="$SAMPLES" 'BEGIN{printf "%.2f", s/n}')
    avg_insert_rate=$(awk -v s="$total_insert_rate" -v n="$SAMPLES" 'BEGIN{printf "%.2f", s/n}')
    avg_update_rate=$(awk -v s="$total_update_rate" -v n="$SAMPLES" 'BEGIN{printf "%.2f", s/n}')
    diff_windows=$(( SAMPLES - 1 ))
    avg_field_changes=$(awk -v s="$total_field_changes_sum" -v n="$diff_windows" 'BEGIN{printf "%.2f", s/n}')
    avg_label_changes=$(awk -v s="$total_label_changes_sum" -v n="$diff_windows" 'BEGIN{printf "%.2f", s/n}')

    echo
    echo "SUMMARY (averages across samples)"
    echo "---------------------------------"
    printf "touched avg=%s (%.2f/min)  insert_estimate avg=%s (%.2f/min)  update_estimate avg=%s (%.2f/min)\n" \
      "$avg_touched" "$avg_touched_rate" "$avg_insert" "$avg_insert_rate" "$avg_update" "$avg_update_rate"
    printf "actual field changes avg (samples 2..N): %s\n" "$avg_field_changes"
    printf "actual label changes avg (samples 2..N): %s\n" "$avg_label_changes"
  fi

  echo
  echo "Note: change counts are computed by comparing each sample to the previous sample snapshot."
}

main "$@"
