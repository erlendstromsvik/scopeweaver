package server

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/internal/dbtest"
)

// newTestServer builds a fully constructed, JWT-authenticated Server on the
// throwaway test DB. It skips when ARTEX_TEST_DSN is unset (dbtest). The service
// context is cancelled BEFORE New so no background loops run tasks; archive and
// side-question loops are drained.
func newTestServer(t *testing.T) (*Server, *Manager, func(method, path, body string) *httptest.ResponseRecorder) {
	t.Helper()
	dbtest.DSN(t) // skip unless ARTEX_TEST_DSN points at a throwaway DB
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	// Cancel BEFORE New so no background engine loops start (the
	// trafficEvidenceServer pattern); otherwise teardown hangs.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := New(ctx, m, t.TempDir(), t.TempDir(), t.TempDir())
	s.archiveWG.Wait()
	<-s.side.done
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
	return s, m, request
}

// newTestTask creates a task on the manager and registers cleanup (task_archives
// then the task itself) so a reused throwaway database stays clean. Returns the
// task and its numeric id.
func newTestTask(t *testing.T, m *Manager, name string) (*Task, int64) {
	t.Helper()
	task, err := m.CreateTask(name, "goal", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	tid := mustTaskID(t, task.ID)
	t.Cleanup(func() {
		m.pg.Exec(`DELETE FROM task_archives WHERE task_id=$1`, tid)
		m.pg.DeleteTask(tid)
	})
	return task, tid
}
