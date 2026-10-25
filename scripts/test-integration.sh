#!/usr/bin/env bash
# Runs Go integration tests against disposable Postgres and MySQL in Docker.
set -euo pipefail
cd "$(dirname "$0")/.."

if docker compose version >/dev/null 2>&1; then
	COMPOSE=(docker compose)
elif command -v docker-compose >/dev/null 2>&1; then
	COMPOSE=(docker-compose)
else
	echo "docker compose (or docker-compose) is required" >&2
	exit 1
fi

compose() {
	"${COMPOSE[@]}" -f docker-compose.test.yml "$@"
}

cleanup() {
	compose down -v >/dev/null 2>&1 || true
}
trap cleanup EXIT

compose up -d --wait
export TEST_DATABASE_URL="postgres://test:test@127.0.0.1:54329/test?sslmode=disable"
export MYSQL_SMOKE_DSN="mysql://root:test@tcp(127.0.0.1:33069)/smoketest?charset=utf8mb4"
cd apps/server
go test -count=1 ./...
