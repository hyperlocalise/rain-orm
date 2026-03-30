#!/usr/bin/env bash

set -euo pipefail

filter="${1:-}"
timestamp="$(date '+%Y%m%d-%H%M%S')"
report_dir="artifacts/bench/ormshowdown/${timestamp}"
normalized_dir="${report_dir}/normalized"
detail_dir="${report_dir}/benchstat"
csv_dir="${report_dir}/csv"
manifest_path="${report_dir}/manifest.txt"
bench_count="${BENCH_COUNT:-3}"
benchstat_bin="${BENCHSTAT_BIN:-benchstat}"
libraries=(raw rain bun gorm)
comparison_libraries=("${libraries[@]:1}")

mkdir -p "${report_dir}" "${normalized_dir}" "${detail_dir}" "${csv_dir}"

sanitize_name() {
  local value="$1"
  value="${value//[^A-Za-z0-9._-]/-}"
  value="${value##-}"
  value="${value%%-}"
  if [[ -z "${value}" ]]; then
    value="ormshowdown"
  fi
  printf '%s\n' "${value}"
}

assert_tooling() {
  if ! command -v "${benchstat_bin}" >/dev/null 2>&1; then
    printf '%s\n' "benchstat not found at '${benchstat_bin}'. Run 'make bootstrap' or set BENCHSTAT_BIN=/path/to/benchstat." >&2
    exit 1
  fi
}

write_manifest() {
  local commit_sha branch_name go_version platform
  commit_sha="$(git rev-parse --short HEAD 2>/dev/null || printf '%s\n' unknown)"
  branch_name="$(git rev-parse --abbrev-ref HEAD 2>/dev/null || printf '%s\n' unknown)"
  go_version="$(go version 2>/dev/null || printf '%s\n' unknown)"
  platform="$(go env GOOS 2>/dev/null || printf '%s\n' unknown)/$(go env GOARCH 2>/dev/null || printf '%s\n' unknown)"

  {
    printf '%s\n' "ORM showdown benchmark run"
    printf '%s\n' "Timestamp: $(date '+%Y-%m-%d %H:%M:%S %Z')"
    printf '%s\n' "Commit: ${commit_sha}"
    printf '%s\n' "Branch: ${branch_name}"
    printf '%s\n' "Go: ${go_version}"
    printf '%s\n' "Platform: ${platform}"
    printf '%s\n' "Bench count: ${bench_count}"
    if [[ -n "${filter}" ]]; then
      printf '%s\n' "Benchmark filter: ${filter}"
    fi
    printf '\n'
  } | tee "${manifest_path}"
}

run_library_benchmarks() {
  local library regex output_path command_string

  for library in "${libraries[@]}"; do
    regex="^BenchmarkORMShowdown/${library}/"
    if [[ -n "${filter}" ]]; then
      regex="${regex}${filter}"
    fi

    output_path="${report_dir}/${library}.txt"
    command_string="go test -run '^$' -bench '${regex}' -benchmem -count ${bench_count} ./benchmarks/ormshowdown/..."

    printf '%s\n' "${library}: ${command_string}" >> "${manifest_path}"
    printf '%s\n' "==> ${library}"
    go test -run '^$' -bench "${regex}" -benchmem -count "${bench_count}" ./benchmarks/ormshowdown/... | tee "${output_path}"
    sed -E "s#^(BenchmarkORMShowdown)/${library}/#\\1/#" "${output_path}" > "${normalized_dir}/${library}.txt"
    printf '\n'
  done
}

write_benchstat_outputs() {
  local summary_suffix summary_path library detail_path csv_path libs_csv
  local -a csv_inputs

  summary_suffix=""
  if [[ -n "${filter}" ]]; then
    summary_suffix="-$(sanitize_name "${filter}")"
  fi
  summary_path="${report_dir}/benchstat${summary_suffix}.txt"

  printf '%s\n' "==> benchstat (baseline: raw)"
  csv_inputs=()
  for library in "${comparison_libraries[@]}"; do
    detail_path="${detail_dir}/raw-vs-${library}.txt"
    csv_path="${csv_dir}/raw-vs-${library}.csv"

    printf '%s\n' "raw vs ${library}"
    "${benchstat_bin}" "${normalized_dir}/raw.txt" "${normalized_dir}/${library}.txt" > "${detail_path}"
    "${benchstat_bin}" -format csv "${normalized_dir}/raw.txt" "${normalized_dir}/${library}.txt" 2>/dev/null > "${csv_path}"
    csv_inputs+=("${csv_path}")
    printf '\n'
  done

  # benchstat -format csv currently emits tables like:
  #   ,old.txt,,new.txt,,,
  #   ,sec/op,CI,sec/op,CI,vs base,P
  # Parse the repeated metric headings to locate the two centre-value columns
  # rather than assuming fixed field numbers.
  libs_csv="$(IFS=,; printf '%s' "${comparison_libraries[*]}")"

  awk -F',' -v libs_csv="${libs_csv}" '
function trim(value) {
  gsub(/^[[:space:]]+|[[:space:]]+$/, "", value)
  return value
}

function ratio(oldv, newv) {
  if (oldv == "" || oldv == 0) {
    return "-"
  }
  return sprintf("%.2fx", newv / oldv)
}

BEGIN {
  lib_count = split(libs_csv, libs, ",")
  metric_count = 0
  row_count = 0
}

FNR == 1 {
  split(FILENAME, parts, "/")
  current_lib = parts[length(parts)]
  sub(/^raw-vs-/, "", current_lib)
  sub(/\.csv$/, "", current_lib)
  current_metric = ""
  base_col = 0
  compare_col = 0
  next
}

$1 == "" {
  metric = ""
  delete metric_cols
  metric_col_count = 0
  for (i = 2; i <= NF; i++) {
    value = trim($i)
    if (value == "sec/op" || value == "B/op" || value == "allocs/op") {
      metric_cols[++metric_col_count] = i
      if (metric == "") {
        metric = value
      }
    }
  }
  if (metric_col_count < 2) {
    next
  }
  if (!(metric in metric_seen)) {
    metric_seen[metric] = 1
    metric_order[++metric_count] = metric
  }
  current_metric = metric
  base_col = metric_cols[1]
  compare_col = metric_cols[2]
  next
}

$1 ~ /^ORMShowdown\// || $1 == "geomean" {
  benchmark = trim($1)
  if (!(benchmark in row_seen)) {
    row_seen[benchmark] = 1
    row_order[++row_count] = benchmark
  }
  if (current_metric != "" && base_col > 0 && compare_col > 0) {
    values[current_metric, benchmark, current_lib] = ratio(trim($(base_col)), trim($(compare_col)))
  }
}

END {
  for (m = 1; m <= metric_count; m++) {
    metric = metric_order[m]
    print metric " vs raw (lower is better)"
    header = sprintf("%-48s", "benchmark")
    for (i = 1; i <= lib_count; i++) {
      header = header sprintf(" %8s", libs[i])
    }
    print header
    for (r = 1; r <= row_count; r++) {
      benchmark = row_order[r]
      label = benchmark
      sub(/^ORMShowdown\//, "", label)
      printf "%-48s", label
      for (i = 1; i <= lib_count; i++) {
        lib = libs[i]
        value = values[metric, benchmark, lib]
        if (value == "") {
          value = "-"
        }
        printf " %8s", value
      }
      printf "\n"
    }
    if (m < metric_count) {
      printf "\n"
    }
  }
}
' "${csv_inputs[@]}" | tee "${summary_path}"

  printf '\n'
  printf '%s\n' "Saved ORM showdown benchmark reports: ${report_dir}"
  printf '%s\n' "Saved run manifest: ${manifest_path}"
  printf '%s\n' "Saved comparison summary: ${summary_path}"
  printf '%s\n' "Saved pairwise benchstat details: ${detail_dir}"
}

assert_tooling
write_manifest
run_library_benchmarks
write_benchstat_outputs
