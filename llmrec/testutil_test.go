package llmrec

import (
	"testing"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/internal/dbtest"
)

// openTestDB returns a *db.DB for the guarded test DSN, with the tables the
// metering tests query (llm_usage, llm_records) created. Upstream CI builds
// these in its init-test-db step; nothing else does locally, so the tests
// create them themselves. Both Ensure* calls are idempotent
// (CREATE TABLE IF NOT EXISTS), so repeated calls across tests are safe.
func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	pg, err := db.Open(dbtest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := pg.EnsureLLMUsageTable(); err != nil {
		t.Fatal(err)
	}
	if err := pg.EnsureLLMRecordsTable(); err != nil {
		t.Fatal(err)
	}
	return pg
}
