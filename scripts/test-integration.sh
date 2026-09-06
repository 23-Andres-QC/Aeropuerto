#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
docker compose up -d --wait db
if ! docker compose exec -T db psql -U aeropuerto -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname='aeropuerto_test'" | rg -q 1; then
  docker compose exec -T db createdb -U aeropuerto aeropuerto_test
fi
# Uses the configured demo password; the test refuses any other database name.
DB_PASSWORD=${DB_PASSWORD:-demo_local_only}
docker run --rm --network aeropuerto-demo_default \
  -e "TEST_DATABASE_URL=postgres://aeropuerto:${DB_PASSWORD}@db:5432/aeropuerto_test?sslmode=disable" \
  -v "$PWD/backend:/src" -w /src golang:1.25-alpine \
  sh -c 'go test -v ./... && go vet ./...'
