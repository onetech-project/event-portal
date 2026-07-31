#!/usr/bin/env bash
# Runs the backend test suite. Database-backed tests skip themselves when
# TEST_DATABASE_URL is unset, so this works with or without the Compose stack up.
set -euo pipefail

cd "$(dirname "$0")/.."

export TEST_DATABASE_URL="${TEST_DATABASE_URL:-postgres://ticketing:ticketing@localhost:5433/ticketing_test?sslmode=disable}"

# -p 1 runs one package at a time. Every database-backed package truncates the
# shared test schema on setup, so packages running concurrently would clobber each
# other's fixtures.
exec go test -p 1 "$@"
