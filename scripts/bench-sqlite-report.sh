#!/usr/bin/env bash

set -euo pipefail

filter="${1:-}"
timestamp="$(date '+%Y%m%d-%H%M%S')"
report_dir="artifacts/bench/sqlite/${timestamp}"
bench_count="${BENCH_COUNT:-3}"

mkdir -p "${report_dir}"

sanitize_name() {
  local value="$1"
  value="${value//[^A-Za-z0-9._-]/-}"
  value="${value##-}"
  value="${value%%-}"
  if [[ -z "${value}" ]]; then
    value="sqlite"
  fi
  printf '%s\n' "${value}"
}

go_test_cmd=(go test -run '^$' -bench . -benchmem -count "${bench_count}" ./pkg/rain)
if [[ -n "${filter}" ]]; then
  go_test_cmd=(go test -run '^$' -bench "${filter}" -benchmem -count "${bench_count}" ./pkg/rain)
fi

commit_sha="$(git rev-parse --short HEAD 2>/dev/null || printf '%s\n' unknown)"
branch_name="$(git rev-parse --abbrev-ref HEAD 2>/dev/null || printf '%s\n' unknown)"
go_version="$(go version 2>/dev/null || printf '%s\n' unknown)"
platform="$(go env GOOS 2>/dev/null || printf '%s\n' unknown)/$(go env GOARCH 2>/dev/null || printf '%s\n' unknown)"
command_string="${go_test_cmd[*]}"

suffix="sqlite"
if [[ -n "${filter}" ]]; then
  suffix="$(sanitize_name "${filter}")"
fi

manifest_path="${report_dir}/manifest.txt"
raw_output_path="${report_dir}/${suffix}.txt"

write_manifest() {
  printf '%s\n' "Rain SQLite benchmark run"
  printf '%s\n' "Timestamp: $(date '+%Y-%m-%d %H:%M:%S %Z')"
  printf '%s\n' "Commit: ${commit_sha}"
  printf '%s\n' "Branch: ${branch_name}"
  printf '%s\n' "Go: ${go_version}"
  printf '%s\n' "Platform: ${platform}"
  printf '%s\n' "Report directory: ${report_dir}"
  printf '%s\n' "Command: ${command_string}"
  printf '%s\n' "Bench count: ${bench_count}"
  if [[ -n "${filter}" ]]; then
    printf '%s\n' "Benchmark filter: ${filter}"
  fi
  printf '\n'
  printf '%s\n' "Metrics"
  printf '%s\n' "- ns/op: average time per benchmark iteration"
  printf '%s\n' "- B/op: average bytes allocated per iteration"
  printf '%s\n' "- allocs/op: average heap allocations per iteration"
}

write_manifest | tee "${manifest_path}"

"${go_test_cmd[@]}" | tee "${raw_output_path}"
printf '\n'
printf '%s\n' "Saved run manifest: ${manifest_path}"
printf '%s\n' "Saved benchmark output: ${raw_output_path}"
