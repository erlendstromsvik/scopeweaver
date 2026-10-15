package server

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Autumn-27/artex/internal/dbtest"
	"github.com/Autumn-27/artex/locale"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

// These tests delete password settings and require an explicitly disposable database.
func backportTestDatabaseNameAllowed(name string) bool {
	return strings.HasPrefix(name, "scopeweaver_test_") || strings.HasPrefix(name, "scopeweaver_adapter_")
}

func backportTestDatabaseAllowed(dsn string) bool {
	if dsn == "" {
		return false
	}
	parsed, err := pgx.ParseConfig(dsn)
	return err == nil && backportTestDatabaseNameAllowed(parsed.Database)
}

func backportTestManager(t *testing.T) *Manager {
	t.Helper()
	if !backportTestDatabaseAllowed(dbtest.DSN(t)) {
		t.Skip("requires an explicit ARTEX_TEST_DSN with database name scopeweaver_test_* or scopeweaver_adapter_*")
	}
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Skipf("postgres unavailable (%v)", err)
	}
	t.Cleanup(func() { m.Close() })
	var database string
	if err := m.pg.QueryRow(`SELECT current_database()`).Scan(&database); err != nil {
		t.Fatal(err)
	}
	if !backportTestDatabaseNameAllowed(database) {
		t.Fatalf("refusing to modify non-test database %q", database)
	}
	return m
}

func authTestServer(t *testing.T) *Server {
	t.Helper()
	return &Server{m: backportTestManager(t), jwtKey: []byte("01234567890123456789012345678901")}
}

func TestAuthFailsClosedWhenDataSourceUnavailable(t *testing.T) {
	for _, lang := range []locale.Lang{locale.En, locale.Ko} {
		t.Run(string(lang), func(t *testing.T) {
			s := authTestServer(t)
			token, err := signJWT(s.jwtKey)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.m.pg.Close(); err != nil {
				t.Fatal(err)
			}
			for _, route := range []string{"status", "init", "login", "change-password"} {
				t.Run(route, func(t *testing.T) {
					body := `{"password":"correct horse battery","username":"ARTEX","old_password":"correct horse battery","new_password":"another good password"}`
					r := httptest.NewRequest("POST", "/api/auth/"+route, strings.NewReader(body))
					r.Header.Set("Authorization", "Bearer "+token)
					w := httptest.NewRecorder()
					lw := &localeWriter{w, lang}
					switch route {
					case "status":
						s.authStatus(lw, r)
					case "init":
						s.authInit(lw, r)
					case "login":
						s.authLogin(lw, r)
					case "change-password":
						s.authChangePassword(lw, r)
					}
					if w.Code != 503 {
						t.Fatalf("status=%d want 503; body=%s", w.Code, w.Body.String())
					}
					var payload map[string]any
					if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
						t.Fatal(err)
					}
					if _, ok := payload["token"]; ok {
						t.Fatal("database failure issued a token")
					}
					if _, ok := payload["initialized"]; ok {
						t.Fatal("database failure reported initialization state")
					}
					want := "Data source is temporarily unavailable; please try again later"
					if lang == locale.Ko {
						want = "데이터 소스를 일시적으로 사용할 수 없습니다. 잠시 후 다시 시도하세요"
					}
					if payload["error"] != want {
						t.Fatalf("error=%v want %q", payload["error"], want)
					}
				})
			}
		})
	}
}

func TestAuthInitValidatesPassword(t *testing.T) {
	s := authTestServer(t)
	for _, tc := range []struct {
		name, pw string
		status   int
	}{
		{"seven characters", "1234567", 400},
		{"eight characters", "12345678", 200},
		{"eight Unicode characters", "암호암호암호암호", 200},
		{"three Unicode characters", "암호강", 400},
		{"72 bytes", strings.Repeat("a", 72), 200},
		{"73 bytes", strings.Repeat("a", 73), 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.m.pg.Exec(`DELETE FROM settings WHERE key=$1`, authPassKey)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _, _ = s.m.pg.Exec(`DELETE FROM settings WHERE key=$1`, authPassKey) })
			body, _ := json.Marshal(map[string]string{"password": tc.pw})
			w := httptest.NewRecorder()
			s.authInit(w, httptest.NewRequest("POST", "/api/auth/init", strings.NewReader(string(body))))
			if w.Code != tc.status {
				t.Fatalf("status=%d want %d; body=%s", w.Code, tc.status, w.Body.String())
			}
			hash, ok, err := s.m.pg.GetSetting(authPassKey)
			if err != nil {
				t.Fatal(err)
			}
			if tc.status == 400 && ok {
				t.Fatal("invalid password was saved")
			}
			if tc.status == 200 && bcrypt.CompareHashAndPassword([]byte(hash), []byte(tc.pw)) != nil {
				t.Fatal("valid password was not saved")
			}
		})
	}
}

func TestAuthInitConcurrentRequestsCannotOverwritePassword(t *testing.T) {
	s := authTestServer(t)
	if _, err := s.m.pg.Exec(`DELETE FROM settings WHERE key=$1`, authPassKey); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = s.m.pg.Exec(`DELETE FROM settings WHERE key=$1`, authPassKey) })
	const requests = 6
	statuses := make([]int, requests)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			body := fmt.Sprintf(`{"password":"password-%d"}`, i)
			w := httptest.NewRecorder()
			s.authInit(w, httptest.NewRequest("POST", "/api/auth/init", strings.NewReader(body)))
			statuses[i] = w.Code
		}()
	}
	close(start)
	wg.Wait()
	winner := -1
	for i, status := range statuses {
		if status == 200 {
			if winner != -1 {
				t.Fatalf("multiple initializations succeeded: %v", statuses)
			}
			winner = i
		} else if status != 403 {
			t.Fatalf("status=%d want 403 for losing request", status)
		}
	}
	if winner == -1 {
		t.Fatalf("no initialization succeeded: %v", statuses)
	}
	hash, _, err := s.m.pg.GetSetting(authPassKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(fmt.Sprintf("password-%d", winner))); err != nil {
		t.Fatal("winning password was overwritten")
	}
}

func TestBackportTestDatabaseGuardUsesEffectiveDatabase(t *testing.T) {
	for _, tc := range []struct {
		dsn  string
		want bool
	}{
		{"", false},
		{"postgres://localhost/artex", false},
		{"postgres://localhost/scopeweaver_test_fixture", true},
		{"postgres://localhost/scopeweaver_adapter_e2e", true},
		{"postgres://localhost/scopeweaver_test_fixture?dbname=artex", false},
		{"postgres://localhost/artex?dbname=scopeweaver_test_fixture", true},
		{"host=localhost dbname=scopeweaver_test_fixture", true},
	} {
		if got := backportTestDatabaseAllowed(tc.dsn); got != tc.want {
			t.Fatalf("dsn=%q allowed=%v want %v", tc.dsn, got, tc.want)
		}
	}
}
