#!/usr/bin/env bash
# Creates and migrates the database the Go test suite uses.
#
# Safe to re-run: it drops and recreates, so it also serves as a reset after a
# test run leaves the schema in a bad state.
set -euo pipefail

CONTAINER="${POSTGRES_CONTAINER:-ticketing-postgres}"
DB="${TEST_DB_NAME:-ticketing_test}"
USER="${POSTGRES_USER:-ticketing}"

cd "$(dirname "$0")/.."

if ! docker ps --format '{{.Names}}' | grep -qx "$CONTAINER"; then
  echo "error: container '$CONTAINER' is not running — start it with 'docker compose up -d postgres'" >&2
  exit 1
fi

echo "recreating $DB…"
docker exec "$CONTAINER" psql -U "$USER" -d postgres -c "DROP DATABASE IF EXISTS $DB;" > /dev/null
docker exec "$CONTAINER" psql -U "$USER" -d postgres -c "CREATE DATABASE $DB OWNER $USER;" > /dev/null

echo "applying migrations…"
# Every migration in filename order, the same order Postgres runs them in from
# docker-entrypoint-initdb.d — adding a migration must not mean editing this.
for migration in migrations/*.sql; do
  echo "  $(basename "$migration")"
  docker exec -i "$CONTAINER" psql -U "$USER" -d "$DB" -q -v ON_ERROR_STOP=1 < "$migration"
done

echo "$DB is ready."
