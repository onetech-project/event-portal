#!/usr/bin/env bash
# Creates and migrates the database the Go test suite uses.
#
# Safe to re-run: it drops and recreates, so it also serves as a reset after a
# test run leaves the schema in a bad state. Dropping is also what keeps this
# honest — schema_migrations goes with the database, so every version is replayed
# from scratch rather than trusted to already be there.
set -euo pipefail

CONTAINER="${POSTGRES_CONTAINER:-ticketing-postgres}"
DB="${TEST_DB_NAME:-ticketing_test}"
USER="${POSTGRES_USER:-ticketing}"
PASSWORD="${POSTGRES_PASSWORD:-ticketing}"
HOST_PORT="${POSTGRES_PORT:-5433}"
MIGRATE_IMAGE="${MIGRATE_IMAGE:-migrate/migrate:v4.19.0}"

cd "$(dirname "$0")/.."

if ! docker ps --format '{{.Names}}' | grep -qx "$CONTAINER"; then
  echo "error: container '$CONTAINER' is not running — start it with 'docker compose up -d postgres'" >&2
  exit 1
fi

echo "recreating $DB…"
docker exec "$CONTAINER" psql -U "$USER" -d postgres -c "DROP DATABASE IF EXISTS $DB;" > /dev/null
docker exec "$CONTAINER" psql -U "$USER" -d postgres -c "CREATE DATABASE $DB OWNER $USER;" > /dev/null

echo "applying migrations…"
if command -v migrate > /dev/null 2>&1; then
  migrate -path migrations \
    -database "postgres://$USER:$PASSWORD@localhost:$HOST_PORT/$DB?sslmode=disable" \
    up
else
  # No CLI on the PATH, so use the official image. Running it inside the database
  # container's own network namespace reaches Postgres on localhost:5432 whatever
  # the host port is mapped to, and needs no Compose network to exist.
  docker run --rm \
    --network "container:$CONTAINER" \
    --volume "$PWD/migrations:/migrations:ro" \
    "$MIGRATE_IMAGE" \
    -path=/migrations \
    -database="postgres://$USER:$PASSWORD@localhost:5432/$DB?sslmode=disable" \
    up
fi

echo "$DB is ready."
