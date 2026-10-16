package dbtest

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Autumn-27/artex/config"
)

// withLiveDSN temporarily overrides the package-level liveDSN (captured at init)
// and restores it afterwards, so SameAsLive can be exercised deterministically.
func withLiveDSN(t *testing.T, dsn string) {
	t.Helper()
	prev := liveDSN
	liveDSN = dsn
	t.Cleanup(func() { liveDSN = prev })
}

func TestSameAsLive(t *testing.T) {
	const live = "postgres://sw_user@fedora:5432/scopeweaver?sslmode=disable"
	cases := []struct {
		name string
		live string
		dsn  string
		want bool
	}{
		{"identical url", live, live, true},
		{"same host port db different user/password", live, "postgres://other:pw@fedora:5432/scopeweaver", true},
		{"host case-insensitive", live, "postgres://sw_user@FEDORA:5432/scopeweaver", true},
		{"database case-insensitive", live, "postgres://sw_user@fedora:5432/SCOPEWEAVER", true},
		{"url form vs keyword form", live, "host=fedora port=5432 dbname=scopeweaver user=sw_user", true},
		{"different database", live, "postgres://sw_user@fedora:5432/scopeweaver_test_1", false},
		{"different host", live, "postgres://sw_user@otherhost:5432/scopeweaver", false},
		{"different port", live, "postgres://sw_user@fedora:6543/scopeweaver", false},
		{"empty live dsn means nothing to protect", "", live, false},
		{"unparseable test dsn", live, "::not a dsn::", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withLiveDSN(t, c.live)
			if got := SameAsLive(c.dsn); got != c.want {
				t.Fatalf("SameAsLive(%q) with live %q = %v, want %v", c.dsn, c.live, got, c.want)
			}
		})
	}
}

// fakeT records Skip/Fatalf without aborting, so resolveDSN's control flow can be
// inspected. testing.TB is sealed, so resolveDSN takes the small skipFataler
// interface this fake implements.
type fakeT struct {
	helperCalls int
	skipped     bool
	skipMsg     string
	fataled     bool
	fatalMsg    string
}

func (f *fakeT) Helper() { f.helperCalls++ }

func (f *fakeT) Skip(args ...any) {
	f.skipped = true
	f.skipMsg = fmt.Sprint(args...)
}

func (f *fakeT) Fatalf(format string, args ...any) {
	f.fataled = true
	f.fatalMsg = fmt.Sprintf(format, args...)
}

func TestResolveDSNSkipsWhenUnset(t *testing.T) {
	t.Setenv(EnvTestDSN, "") // empty is treated as unset (TrimSpace); restored by t.Setenv

	f := &fakeT{}
	got := resolveDSN(f)
	if got != "" {
		t.Fatalf("resolveDSN = %q, want empty", got)
	}
	if !f.skipped {
		t.Fatal("expected Skip to be called when ARTEX_TEST_DSN is unset")
	}
	if f.fataled {
		t.Fatalf("unexpected Fatalf: %s", f.fatalMsg)
	}
}

func TestResolveDSNFatalsOnLiveDSN(t *testing.T) {
	const live = "postgres://sw_user@fedora:5432/scopeweaver?sslmode=disable"
	withLiveDSN(t, live)
	t.Setenv(EnvTestDSN, live)

	f := &fakeT{}
	got := resolveDSN(f)
	if got != "" {
		t.Fatalf("resolveDSN = %q, want empty on same-as-live", got)
	}
	if !f.fataled {
		t.Fatal("expected Fatalf when ARTEX_TEST_DSN points at the live database")
	}
	if f.skipped {
		t.Fatalf("unexpected Skip: %s", f.skipMsg)
	}
}

func TestResolveDSNReturnsThrowawayDSN(t *testing.T) {
	const live = "postgres://sw_user@fedora:5432/scopeweaver?sslmode=disable"
	const testDSN = "postgres://sw_user_test@/scopeweaver_test_1?host=/tmp&sslmode=disable"
	withLiveDSN(t, live)
	t.Setenv(EnvTestDSN, testDSN)

	f := &fakeT{}
	got := resolveDSN(f)
	if got != testDSN {
		t.Fatalf("resolveDSN = %q, want %q", got, testDSN)
	}
	if f.skipped || f.fataled {
		t.Fatalf("unexpected skip=%v fatal=%v for a valid throwaway DSN", f.skipped, f.fataled)
	}
}

// TestDisabledDSNShadowsLiveConfig checks the value Main exports as ARTEX_PG_DSN
// when ARTEX_TEST_DSN is unset: it must win over a config.json that names the
// live database, and connecting to it must fail without reaching any server.
func TestDisabledDSNShadowsLiveConfig(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.json")
	live := `{"database":{"dsn":"postgres://sw_user@127.0.0.1:5432/scopeweaver?sslmode=disable"}}`
	if err := os.WriteFile(cfg, []byte(live), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SCOPEWEAVER_CONFIG", cfg)
	t.Setenv("ARTEX_PG_DSN", disabledDSN)

	got, _, err := config.PostgresDSN()
	if err != nil {
		t.Fatalf("PostgresDSN: %v", err)
	}
	if got != disabledDSN {
		t.Fatalf("PostgresDSN = %q, want disabledDSN", got)
	}

	conn, err := sql.Open("pgx", disabledDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.PingContext(ctx); err == nil {
		t.Fatal("ping disabledDSN succeeded, want a connection error")
	}
}
