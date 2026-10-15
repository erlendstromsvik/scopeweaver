package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
)

// instanceStatusTestServer builds a DB-backed Server with a JWT-authenticated
// request helper, mirroring trafficEvidenceServer. NOT &Server{m: m} — that
// fixture gets 401 from the auth middleware.
func instanceStatusTestServer(t *testing.T) (*Server, func(method, path, body string) *httptest.ResponseRecorder) {
	t.Helper()
	// The sandbox points ARTEX_PG_DSN at a closed port: skip, never fail.
	dsn, _, err := db.DSN()
	if err != nil {
		t.Skipf("no database config (%v) — skipping (live check: controller)", err)
	}
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Skipf("open postgres (%v) — skipping (live check: controller)", err)
	}
	pingErr := conn.Ping()
	conn.Close()
	if pingErr != nil {
		t.Skipf("ping postgres (%v) — skipping (live check: controller)", pingErr)
	}
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	td := t.TempDir()
	s := New(ctx, m, td, td, td)
	cancel()
	token, err := signJWT(s.jwtKey)
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	return s, request
}

// cleanupBlueprintSettings removes the settings rows a test set so a reused
// throwaway database stays clean.
func cleanupBlueprintSettings(t *testing.T, s *Server) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = s.m.PG().Exec(`DELETE FROM settings WHERE key IN ($1,$2)`,
			blueprintProvisionedKey, blueprintTaskIDKey)
	})
}

func decodeInstanceStatus(t *testing.T, w *httptest.ResponseRecorder) instanceStatus {
	t.Helper()
	var st instanceStatus
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode %s: %v", w.Body, err)
	}
	return st
}

func TestInstanceStatusNotProvisioned(t *testing.T) {
	s, request := instanceStatusTestServer(t)
	cleanupBlueprintSettings(t, s)
	w := request("GET", "/api/instance/status", "")
	if w.Code != 200 {
		t.Fatalf("status: got %d, want 200 (%s)", w.Code, w.Body)
	}
	st := decodeInstanceStatus(t, w)
	if st.Provisioned {
		t.Errorf("provisioned: got true, want false (%s)", w.Body)
	}
}

func TestInstanceStatusProvisioned(t *testing.T) {
	s, request := instanceStatusTestServer(t)
	cleanupBlueprintSettings(t, s)
	pg := s.m.PG()

	task, err := s.m.CreateTask("status task", "status goal", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := strconv.ParseInt(task.ID, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pg.DeleteTask(taskID) })
	if err := pg.SetSetting(blueprintProvisionedKey, "bp-test"); err != nil {
		t.Fatal(err)
	}
	if err := pg.SetSetting(blueprintTaskIDKey, task.ID); err != nil {
		t.Fatal(err)
	}

	w := request("GET", "/api/instance/status", "")
	if w.Code != 200 {
		t.Fatalf("status: got %d, want 200 (%s)", w.Code, w.Body)
	}
	st := decodeInstanceStatus(t, w)
	if !st.Provisioned || st.Blueprint != "bp-test" || st.TaskID != task.ID || st.Terminal {
		t.Fatalf("running status: got %+v, want provisioned bp-test task %s non-terminal", st, task.ID)
	}

	for _, status := range []string{"done", "timeout"} {
		if err := pg.SetStatus(taskID, status); err != nil {
			t.Fatal(err)
		}
		w = request("GET", "/api/instance/status", "")
		if w.Code != 200 {
			t.Fatalf("status %s: got %d (%s)", status, w.Code, w.Body)
		}
		st = decodeInstanceStatus(t, w)
		if !st.Terminal || st.Status != status {
			t.Errorf("status %s: got %+v, want terminal status %q", status, st, status)
		}
	}

	// Deleted task: reported as terminal "missing" so a poller stops waiting.
	if err := pg.DeleteTask(taskID); err != nil {
		t.Fatal(err)
	}
	w = request("GET", "/api/instance/status", "")
	if w.Code != 200 {
		t.Fatalf("missing: got %d (%s)", w.Code, w.Body)
	}
	st = decodeInstanceStatus(t, w)
	if !st.Terminal || st.Status != "missing" {
		t.Errorf("missing task: got %+v, want terminal status \"missing\"", st)
	}
}

func TestInstanceStatusUnauthenticated(t *testing.T) {
	s, _ := instanceStatusTestServer(t)
	cleanupBlueprintSettings(t, s)
	r := httptest.NewRequest("GET", "/api/instance/status", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("unauthenticated: got %d, want 401", w.Code)
	}
}

func TestBlueprintCompletionWatcher(t *testing.T) {
	s, _ := instanceStatusTestServer(t)
	cleanupBlueprintSettings(t, s)
	pg := s.m.PG()

	task, err := s.m.CreateTask("watch task", "watch goal", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := strconv.ParseInt(task.ID, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pg.DeleteTask(taskID) })
	if err := pg.SetSetting(blueprintProvisionedKey, "bp-watch"); err != nil {
		t.Fatal(err)
	}
	if err := pg.SetSetting(blueprintTaskIDKey, task.ID); err != nil {
		t.Fatal(err)
	}

	// Capture the completion marker; restore the log in Cleanup.
	var buf bytes.Buffer
	origOut, origFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(origOut); log.SetFlags(origFlags) })

	// The watcher needs a live (uncancelled) server context to keep polling.
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s.ctx = ctx

	const interval = 20 * time.Millisecond
	s.WatchBlueprintCompletion(interval)
	if err := pg.SetStatus(taskID, "done"); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(s.m.dir, blueprintCompleteFile)
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("completion file never appeared: %s\nlog: %s", path, buf.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Let a couple more intervals pass so a duplicate marker would show up.
	time.Sleep(3 * interval)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var st instanceStatus
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatalf("decode completion file %s: %v", data, err)
	}
	if !st.Terminal || st.Status != "done" {
		t.Errorf("completion file: got %+v, want terminal done", st)
	}

	marker := fmt.Sprintf("[blueprint] COMPLETE task=#%s status=done", task.ID)
	if got := strings.Count(buf.String(), marker); got != 1 {
		t.Errorf("marker count: got %d, want exactly 1\nlog: %s", got, buf.String())
	}
}

// TestBlueprintCompletionNoTask verifies the watcher returns immediately when no
// blueprint task was ever admitted (no goroutine, no file).
func TestBlueprintCompletionNoTask(t *testing.T) {
	s, _ := instanceStatusTestServer(t)
	cleanupBlueprintSettings(t, s)
	s.WatchBlueprintCompletion(time.Millisecond)
	path := filepath.Join(s.m.dir, blueprintCompleteFile)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("completion file exists without a blueprint task: %v", err)
	}
}

// DB-free unit tests for the atomic completion-file writer.
func TestWriteBlueprintCompleteAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, blueprintCompleteFile)
	st := instanceStatus{Provisioned: true, Blueprint: "bp", TaskID: "7", Status: "done", Terminal: true, Findings: 3, Assets: 2}
	if err := writeBlueprintComplete(path, st); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got instanceStatus
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
	if got != st {
		t.Errorf("round-trip: got %+v, want %+v", got, st)
	}
	// No temp files left behind in the directory.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != blueprintCompleteFile {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("leftover files: %v", names)
	}
	// Rewrite replaces atomically (rename over an existing file).
	st2 := instanceStatus{Provisioned: true, Blueprint: "bp", TaskID: "7", Status: "timeout", Terminal: true}
	if err := writeBlueprintComplete(path, st2); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got != st2 {
		t.Errorf("rewrite: got %+v, want %+v", got, st2)
	}
}

// instanceStatusNow's pure decision logic is exercised through the DB-backed
// tests above; this guards the IsTerminal mapping the status surface relies on.
func TestInstanceStatusTerminalMapping(t *testing.T) {
	for _, s := range []string{"done", "failed", "timeout"} {
		if !db.IsTerminal(s) {
			t.Errorf("IsTerminal(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"open", "running", "paused", "queued", ""} {
		if db.IsTerminal(s) {
			t.Errorf("IsTerminal(%q) = true, want false", s)
		}
	}
}
