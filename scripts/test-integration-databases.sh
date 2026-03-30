#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_FILE="${ROOT_DIR}/compose.yaml"
export PATH="/opt/homebrew/bin:/usr/local/bin:${PATH}"

if ! command -v docker >/dev/null 2>&1; then
	echo "docker is required to run database integration tests" >&2
	exit 1
fi

compose_cmd=()
if docker compose version >/dev/null 2>&1; then
	compose_cmd=(docker compose -f "${COMPOSE_FILE}")
elif command -v docker-compose >/dev/null 2>&1; then
	compose_cmd=(docker-compose -f "${COMPOSE_FILE}")
else
	echo "docker compose or docker-compose is required" >&2
	exit 1
fi

cleanup() {
	"${compose_cmd[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

wait_for_health() {
	local service="$1"
	local retries="${2:-60}"
	local container_id

	for ((attempt = 1; attempt <= retries; attempt++)); do
		container_id="$("${compose_cmd[@]}" ps -a -q "${service}")"
		if [[ -z "${container_id}" ]]; then
			sleep 1
			continue
		fi

		local status
		status="$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "${container_id}")"
		if [[ "${status}" == "healthy" || "${status}" == "running" ]]; then
			return 0
		fi
		if [[ "${status}" == "exited" || "${status}" == "dead" ]]; then
			echo "service ${service} failed during startup" >&2
			"${compose_cmd[@]}" logs "${service}" >&2 || true
			return 1
		fi
		sleep 1
	done

	if [[ -z "${container_id:-}" ]]; then
		echo "service ${service} did not start" >&2
	else
		echo "service ${service} did not become healthy" >&2
		"${compose_cmd[@]}" logs "${service}" >&2 || true
	fi
	"${compose_cmd[@]}" logs "${service}" >&2 || true
	return 1
}

"${compose_cmd[@]}" up -d

wait_for_health postgres
wait_for_health mysql

export RAIN_POSTGRES_DSN="postgres://rain:rain@127.0.0.1:5432/rain_test?sslmode=disable"
export RAIN_MYSQL_DSN="rain:rain@tcp(127.0.0.1:3306)/rain_test?parseTime=true"

cd "${ROOT_DIR}"
go test -race ./pkg/rain -run '^Test(Postgres|MySQL)Integration'
