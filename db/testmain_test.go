package db

import (
	"os"
	"testing"

	"github.com/Autumn-27/artex/internal/dbtest"
)

// TestMain routes the db test suite through dbtest.Main, which refuses to run
// against the live database, skips when ARTEX_TEST_DSN is unset, and otherwise
// points every DSN resolver at the throwaway test database and holds the shared
// advisory lock (7337741002) so `go test ./...` serializes cross-package cleanup
// with the server and agent suites.
func TestMain(m *testing.M) {
	os.Exit(dbtest.Main(m))
}
