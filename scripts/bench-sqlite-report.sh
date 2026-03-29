#!/bin/zsh

set -euo pipefail

filter="${1:-}"
timestamp="$(date '+%Y%m%d-%H%M%S')"
report_dir="artifacts/bench"
mkdir -p "${report_dir}"

sanitize_name() {
  local value="$1"
  value="${value//[^A-Za-z0-9._-]/-}"
  value="${value##-}"
  value="${value%%-}"
  if [[ -z "${value}" ]]; then
    value="sqlite"
  fi
  print -r -- "${value}"
}

if [[ -n "${filter}" ]]; then
  base_name="$(sanitize_name "${filter}")"
else
  base_name="sqlite"
fi

report_path="${report_dir}/${base_name}-${timestamp}.txt"
bench_count="${BENCH_COUNT:-3}"
go_test_cmd=(go test -run '^$' -bench . -benchmem -count "${bench_count}" ./pkg/rain)

if [[ -n "${filter}" ]]; then
  go_test_cmd=(go test -run '^$' -bench "${filter}" -benchmem -count "${bench_count}" ./pkg/rain)
fi

commit_sha="$(git rev-parse --short HEAD 2>/dev/null || print -r -- unknown)"
branch_name="$(git rev-parse --abbrev-ref HEAD 2>/dev/null || print -r -- unknown)"
go_version="$(go version 2>/dev/null || print -r -- unknown)"
platform="$(go env GOOS 2>/dev/null || print -r -- unknown)/$(go env GOARCH 2>/dev/null || print -r -- unknown)"
command_string="${(j: :)go_test_cmd}"

{
  print -r -- "Rain SQLite benchmark run"
  print -r -- "Timestamp: $(date '+%Y-%m-%d %H:%M:%S %Z')"
  print -r -- "Commit: ${commit_sha}"
  print -r -- "Branch: ${branch_name}"
  print -r -- "Go: ${go_version}"
  print -r -- "Platform: ${platform}"
  print -r -- "Command: ${command_string}"
  if [[ -n "${filter}" ]]; then
    print -r -- "Benchmark filter: ${filter}"
  fi
  print -r -- ""
  print -r -- "Metrics"
  print -r -- "- ns/op: average time per benchmark iteration"
  print -r -- "- B/op: average bytes allocated per iteration"
  print -r -- "- allocs/op: average heap allocations per iteration"
  print -r -- ""
} > "${report_path}"

"${go_test_cmd[@]}" | tee -a "${report_path}"
print -r -- "Saved benchmark report: ${report_path}"
