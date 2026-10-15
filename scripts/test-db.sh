#!/usr/bin/env bash
# Run the database-backed Go suites (db, server, agent) against a throwaway
# PostgreSQL database that is created fresh and dropped on exit (even on failure),
# so the live/production database is never touched.
#
# The database is owned by a least-privilege test role so that role can run the
# schema migrations: PostgreSQL 15+ revoked PUBLIC CREATE on the public schema,
# so a role that does not own the database cannot create tables in it. The role
# usually lacks CREATEDB, so createdb runs as the current (owning) user and the
# tests then connect as the test role.
#
# Override the defaults via environment variables to match your local PostgreSQL:
#   TEST_DB_ROLE  (default sw_user_test)  role that owns the DB and runs the tests
#   TEST_DB_HOST  (default /tmp)          unix socket directory (or a TCP host)
#   TEST_DB_NAME  (default scopeweaver_test_$$)  must start with scopeweaver_test_
#
# Any extra arguments are forwarded to `go test` (e.g. -run, -v).
set -euo pipefail

TEST_DB_ROLE="${TEST_DB_ROLE:-sw_user_test}"
TEST_DB_HOST="${TEST_DB_HOST:-/tmp}"
DB="${TEST_DB_NAME:-scopeweaver_test_$$}"

cleanup() { dropdb --if-exists "$DB" >/dev/null 2>&1 || true; }
trap cleanup EXIT

createdb -O "$TEST_DB_ROLE" "$DB"
export ARTEX_TEST_DSN="postgres://${TEST_DB_ROLE}@/${DB}?host=${TEST_DB_HOST}&sslmode=disable"

echo "test database: ${DB} (owner ${TEST_DB_ROLE}, host ${TEST_DB_HOST})"
go test ./db/ ./server/ ./agent/ -count=1 "$@"
