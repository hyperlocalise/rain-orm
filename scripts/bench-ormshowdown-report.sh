#!/bin/zsh

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

mkdir -p "${report_dir}" "${normalized_dir}" "${detail_dir}" "${csv_dir}"

sanitize_name() {
  local value="$1"
  value="${value//[^A-Za-z0-9._-]/-}"
  value="${value##-}"
  value="${value%%-}"
  if [[ -z "${value}" ]]; then
    value="ormshowdown"
  fi
  print -r -- "${value}"
}

assert_tooling() {
  if ! command -v "${benchstat_bin}" >/dev/null 2>&1; then
    print -u2 -- "benchstat not found at '${benchstat_bin}'. Run 'make bootstrap' or set BENCHSTAT_BIN=/path/to/benchstat."
    exit 1
  fi
}

write_manifest() {
  local commit_sha branch_name go_version platform
  commit_sha="$(git rev-parse --short HEAD 2>/dev/null || print -r -- unknown)"
  branch_name="$(git rev-parse --abbrev-ref HEAD 2>/dev/null || print -r -- unknown)"
  go_version="$(go version 2>/dev/null || print -r -- unknown)"
  platform="$(go env GOOS 2>/dev/null || print -r -- unknown)/$(go env GOARCH 2>/dev/null || print -r -- unknown)"

  {
    print -r -- "ORM showdown benchmark run"
    print -r -- "Timestamp: $(date '+%Y-%m-%d %H:%M:%S %Z')"
    print -r -- "Commit: ${commit_sha}"
    print -r -- "Branch: ${branch_name}"
    print -r -- "Go: ${go_version}"
    print -r -- "Platform: ${platform}"
    print -r -- "Bench count: ${bench_count}"
    if [[ -n "${filter}" ]]; then
      print -r -- "Benchmark filter: ${filter}"
    fi
    print -r -- ""
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

    print -r -- "${library}: ${command_string}" >> "${manifest_path}"
    print -r -- "==> ${library}"
    go test -run '^$' -bench "${regex}" -benchmem -count "${bench_count}" ./benchmarks/ormshowdown/... | tee "${output_path}"
    sed -E "s#^(BenchmarkORMShowdown)/${library}/#\\1/#" "${output_path}" > "${normalized_dir}/${library}.txt"
    print -r -- ""
  done
}

write_benchstat_outputs() {
  local summary_suffix summary_path csv_inputs library detail_path csv_path

  summary_suffix=""
  if [[ -n "${filter}" ]]; then
    summary_suffix="-$(sanitize_name "${filter}")"
  fi
  summary_path="${report_dir}/benchstat${summary_suffix}.txt"

  print -r -- "==> benchstat (baseline: raw)"
  csv_inputs=()
  for library in "${libraries[@]:1}"; do
    detail_path="${detail_dir}/raw-vs-${library}.txt"
    csv_path="${csv_dir}/raw-vs-${library}.csv"

    print -r -- "raw vs ${library}"
    "${benchstat_bin}" "${normalized_dir}/raw.txt" "${normalized_dir}/${library}.txt" > "${detail_path}"
    "${benchstat_bin}" -format csv "${normalized_dir}/raw.txt" "${normalized_dir}/${library}.txt" 2>/dev/null > "${csv_path}"
    csv_inputs+=("${csv_path}")
    print -r -- ""
  done

  awk -F',' '
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
  libs[1] = "rain"
  libs[2] = "bun"
  libs[3] = "gorm"
  lib_count = 3
  metric_count = 0
  row_count = 0
}

FNR == 1 {
  split(FILENAME, parts, "/")
  current_lib = parts[length(parts)]
  sub(/^raw-vs-/, "", current_lib)
  sub(/\.csv$/, "", current_lib)
  next
}

$1 == "" && ($2 == "sec/op" || $2 == "B/op" || $2 == "allocs/op") {
  metric = trim($2)
  if (!(metric in metric_seen)) {
    metric_seen[metric] = 1
    metric_order[++metric_count] = metric
  }
  current_metric = metric
  next
}

$1 ~ /^ORMShowdown\// || $1 == "geomean" {
  benchmark = trim($1)
  if (!(benchmark in row_seen)) {
    row_seen[benchmark] = 1
    row_order[++row_count] = benchmark
  }
  values[current_metric, benchmark, current_lib] = ratio(trim($2), trim($4))
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

  print -r -- ""
  print -r -- "Saved ORM showdown benchmark reports: ${report_dir}"
  print -r -- "Saved run manifest: ${manifest_path}"
  print -r -- "Saved comparison summary: ${summary_path}"
  print -r -- "Saved pairwise benchstat details: ${detail_dir}"
}

assert_tooling
write_manifest
run_library_benchmarks
write_benchstat_outputs
