#!/usr/bin/env bash
# Runs Go integration tests against a disposable Postgres in Docker.
set -euo pipefail
cd "$(dirname "$0")/.."

COMPOSE="docker compose -f docker-compose.test.yml"

cleanup() {
	$COMPOSE down -v >/dev/null 2>&1 || true
}
trap cleanup EXIT

$COMPOSE up -d --wait
export TEST_DATABASE_URL="postgres://test:test@127.0.0.1:54329/test?sslmode=disable"
cd apps/server
go test -count=1 ./...
