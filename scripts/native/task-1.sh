#!/usr/bin/env bash
set -e
cd "$(dirname "$0")/../.."
cd apps/server && go run cmd/server/main.go
