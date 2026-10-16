// Package dbtest centralizes how the database-backed test suites resolve their
// connection string and guards them against ever touching the live/production
// database.
//
// Background: the test harness historically resolved the same DSN the running
// application uses (config.PostgresDSN), so `go test ./db/... ./server/...`
// mutated the live database and, on 2026-10-13, wrote ~250 fixture rows into
// production. This package makes that impossible: DB-backed tests resolve their
// DSN only from ARTEX_TEST_DSN, skip when it is unset (never falling back to the
// app DSN), and fail fast if the configured DSN points at the same database the
// application uses.
//
// It imports only config, pgconn and the standard library — never db, server or
// agent — so it is free of import cycles and, being a test-only import, never
// ships in the production binary.
package dbtest

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/config"
	"github.com/jackc/pgx/v5/pgconn"

	_ "github.com/jackc/pgx/v5/stdlib" // register the "pgx" database/sql driver
)

// EnvTestDSN is the environment variable that must point at a throwaway test
// database for DB-backed tests to run. When it is unset those tests skip.
const EnvTestDSN = "ARTEX_TEST_DSN"

// advisoryLockKey is the process-wide PostgreSQL advisory lock the three
// DB-backed suites (db, server, agent) share so that a single `go test ./...`
// serializes their cross-package cleanup. It matches the key the old,
// hand-rolled TestMains used.
const advisoryLockKey = 7337741002

// liveDSN is the application's resolved DSN, captured once at package
// initialization — before any TestMain overrides ARTEX_PG_DSN to point at the
// test database. Capturing it here is what lets SameAsLive still recognize the
// live database after Main has redirected the environment. Empty when the app
// has no database configured (nothing to protect against).
var liveDSN = func() string {
	d, _, err := config.PostgresDSN()
	if err != nil {
		return ""
	}
	return d
}()

// SameAsLive reports whether dsn points at the same database the application
// uses: same host, port and database name (host and database compared
// case-insensitively). It returns false when the live DSN is unknown (empty) —
// there is then no production database to protect — or when either DSN cannot
// be parsed.
func SameAsLive(dsn string) bool {
	if strings.TrimSpace(liveDSN) == "" {
		return false
	}
	test, err := pgconn.ParseConfig(dsn)
	if err != nil {
		return false
	}
	live, err := pgconn.ParseConfig(liveDSN)
	if err != nil {
		return false
	}
	return strings.EqualFold(test.Host, live.Host) &&
		test.Port == live.Port &&
		strings.EqualFold(test.Database, live.Database)
}

// disabledDSN is what Main exports as ARTEX_PG_DSN when ARTEX_TEST_DSN is unset.
// config.PostgresDSN prefers ARTEX_PG_DSN over config.json, so code under test
// that resolves the app DSN gets this instead of the live database. It names a
// unix socket in a directory that does not exist, so connecting fails at once
// without any network access.
const disabledDSN = "postgres:///artex_dbtest_disabled?host=/nonexistent/artex-dbtest-disabled&sslmode=disable"

const skipMsg = "set " + EnvTestDSN + " to a throwaway database (see make test-db) — skipping DB-backed test"

const liveGuardMsg = "refusing to run tests against the live database; point " + EnvTestDSN + " at a throwaway DB"

// skipFataler is the subset of testing.TB that DSN uses. It exists so DSN's
// skip/fatal branches can be exercised with a fake in tests — testing.TB itself
// is a sealed interface that cannot be implemented outside the testing package.
// *testing.T and *testing.B satisfy it.
type skipFataler interface {
	Helper()
	Skip(args ...any)
	Fatalf(format string, args ...any)
}

// DSN returns the test database DSN for a single test, or skips/fails the test:
//   - ARTEX_TEST_DSN unset → t.Skip (never falls back to the app DSN).
//   - ARTEX_TEST_DSN points at the live database → t.Fatalf (the real guard:
//     the env var alone is not enough, someone could point it at production).
//   - otherwise → the configured test DSN.
func DSN(t testing.TB) string {
	return resolveDSN(t)
}

func resolveDSN(t skipFataler) string {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv(EnvTestDSN))
	if dsn == "" {
		t.Skip(skipMsg)
		return ""
	}
	if SameAsLive(dsn) {
		t.Fatalf("%s", liveGuardMsg)
		return ""
	}
	return dsn
}

// Main is the shared TestMain body for the db, server and agent suites. Call it
// as `os.Exit(dbtest.Main(m))`.
//
//   - ARTEX_TEST_DSN unset → run the suite without touching any database at all
//     (DB-backed tests skip themselves via DSN). ARTEX_PG_DSN is pointed at
//     disabledDSN, so a test that forgets to call DSN and resolves the app DSN
//     (db.DSN, server.NewManager) gets a connection error instead of the live
//     database from config.json.
//   - ARTEX_TEST_DSN points at the live database → print the guard message and
//     return non-zero without running anything.
//   - otherwise → redirect every downstream resolver (db.DSN, server.NewManager,
//     db's testDSN) at the test database by exporting ARTEX_PG_DSN, acquire the
//     shared advisory lock on it, and run the suite.
func Main(m *testing.M) int {
	dsn := strings.TrimSpace(os.Getenv(EnvTestDSN))
	if dsn == "" {
		if err := os.Setenv("ARTEX_PG_DSN", disabledDSN); err != nil {
			fmt.Fprintf(os.Stderr, "dbtest: set ARTEX_PG_DSN: %v\n", err)
			return 1
		}
		return m.Run()
	}
	if SameAsLive(dsn) {
		fmt.Fprintln(os.Stderr, liveGuardMsg)
		return 1
	}
	// Redirect db.DSN(), server.NewManager and db's testDSN at the test DB.
	// liveDSN was captured at init, so this override does not defeat the guard.
	if err := os.Setenv("ARTEX_PG_DSN", dsn); err != nil {
		fmt.Fprintf(os.Stderr, "dbtest: set ARTEX_PG_DSN: %v\n", err)
		return 1
	}

	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		// No usable connection — let the individual tests skip/fail on open.
		return m.Run()
	}
	defer conn.Close()
	if conn.Ping() != nil {
		return m.Run()
	}
	// Pin one pooled connection: pg_advisory_lock is per-session, so the lock
	// and the unlock must run on the same connection or the unlock silently
	// releases nothing.
	c, err := conn.Conn(context.Background())
	if err != nil {
		return m.Run()
	}
	defer c.Close()
	if _, err := c.ExecContext(context.Background(), `SELECT pg_advisory_lock($1)`, advisoryLockKey); err != nil {
		return m.Run()
	}
	defer c.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, advisoryLockKey) //nolint:errcheck
	return m.Run()
}
