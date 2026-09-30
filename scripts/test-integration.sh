#!/usr/bin/env bash
# Runs every backend test, including the integration ones, against a fresh
# disposable database (aeropuerto_test) in the compose PostGIS container.
set -euo pipefail
cd "$(dirname "$0")/.."
docker compose up -d --wait db
docker compose exec -T db dropdb -U aeropuerto --if-exists aeropuerto_test
docker compose exec -T db createdb -U aeropuerto aeropuerto_test
# Al terminar (pasen o no las pruebas) la base de prueba no queda en el servidor.
trap 'docker compose exec -T db dropdb -U aeropuerto --if-exists aeropuerto_test' EXIT
# Uses the configured demo password; the tests refuse any other database name.
DB_PASSWORD=${DB_PASSWORD:-demo_local_only}
docker run --rm --network aeropuerto-demo_default \
  -e "TEST_DATABASE_URL=postgres://aeropuerto:${DB_PASSWORD}@db:5432/aeropuerto_test?sslmode=disable" \
  -v "$PWD/backend:/src" -w /src golang:1.25-alpine \
  sh -c 'go vet ./... && go test -count=1 ./...'
