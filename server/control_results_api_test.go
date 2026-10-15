package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/internal/dbtest"
)

// TestControlResultDTOs covers the DB-free DTO conversion: nil input yields an
// empty non-nil slice that marshals to [], and nil id lists become [] rather than
// null. Fields must map 1:1.
func TestControlResultDTOs(t *testing.T) {
	if got := controlResultDTOs(nil); got == nil {
		t.Fatal("nil input returned a nil slice")
	} else if b, _ := json.Marshal(got); string(b) != "[]" {
		t.Fatalf("nil input marshalled to %s, want []", b)
	}

	now := time.Now().UTC().Truncate(time.Second)
	rows := []*db.ControlResult{{
		ID:            7,
		TaskID:        3,
		Standard:      db.StandardASVS,
		RequirementID: "V1.1.1",
		Verdict:       db.VerdictPass,
		Method:        db.MethodManual,
		EvidenceRef:   "snap-42",
		Reason:        "note",
		Actor:         "tester",
		CreatedAt:     now,
		UpdatedAt:     now,
		// AssetsCovered / LinkedFindings left nil on purpose.
	}}
	b, err := json.Marshal(controlResultDTOs(rows))
	if err != nil {
		t.Fatal(err)
	}
	var got []controlResultDTO
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 dto, got %d", len(got))
	}
	d := got[0]
	if d.ID != 7 || d.TaskID != 3 || d.Standard != db.StandardASVS || d.RequirementID != "V1.1.1" ||
		d.Verdict != db.VerdictPass || d.Method != db.MethodManual || d.EvidenceRef != "snap-42" ||
		d.Reason != "note" || d.Actor != "tester" || !d.CreatedAt.Equal(now) || !d.UpdatedAt.Equal(now) {
		t.Fatalf("fields did not map 1:1: %+v", d)
	}
	if d.AssetsCovered == nil || d.LinkedFindings == nil {
		t.Fatalf("nil id lists were not normalized: %+v", d)
	}
	// The raw JSON must show [] for the empty id lists, not null.
	var raw []map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	if ac, ok := raw[0]["assets_covered"].([]any); !ok || len(ac) != 0 {
		t.Fatalf("assets_covered not []: %#v", raw[0]["assets_covered"])
	}
	if lf, ok := raw[0]["linked_findings"].([]any); !ok || len(lf) != 0 {
		t.Fatalf("linked_findings not []: %#v", raw[0]["linked_findings"])
	}
}

// TestListControlResultsHandler exercises the HTTP read path against a throwaway
// DB: unknown task -> 404, empty task -> [] , and two seeded rows returned
// ordered by standard then requirement id.
func TestListControlResultsHandler(t *testing.T) {
	dbtest.DSN(t) // skip unless ARTEX_TEST_DSN points at a throwaway DB
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Skipf("postgres unavailable (%v) — skipping", err)
	}
	defer m.Close()
	createdTaskIDs := []int64{}
	defer func() {
		for _, taskID := range createdTaskIDs {
			_ = m.pg.DeleteTask(taskID)
		}
	}()

	s := &Server{m: m}

	// Unknown id -> 404 with the existing "task not found" text.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/999999999/control-results", nil)
	req.SetPathValue("id", "999999999")
	s.listControlResults(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown task status=%d body=%s", rec.Code, rec.Body.String())
	}

	// New task -> 200 with an empty (never null) control_results list.
	task, err := m.CreateTask("control results task", "goal", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	taskID := mustTaskID(t, task.ID)
	createdTaskIDs = append(createdTaskIDs, taskID)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID+"/control-results", nil)
	req.SetPathValue("id", task.ID)
	s.listControlResults(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("empty task status=%d body=%s", rec.Code, rec.Body.String())
	}
	var empty struct {
		ControlResults []controlResultDTO `json:"control_results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &empty); err != nil {
		t.Fatal(err)
	}
	if empty.ControlResults == nil {
		t.Fatalf("empty task returned null control_results: %s", rec.Body.String())
	}
	if len(empty.ControlResults) != 0 {
		t.Fatalf("new task not empty: %+v", empty.ControlResults)
	}

	// Seed a pass (evidence + assets) and a not_applicable (reason). Insert the
	// wstg row first so the read order (standard then requirement id) is proven.
	if _, err := m.pg.UpsertControlResult(db.ControlResultInput{
		TaskID:        taskID,
		Standard:      db.StandardWSTG,
		RequirementID: "WSTG-INFO-01",
		Verdict:       db.VerdictNotApplicable,
		Reason:        "out of scope",
		Actor:         "tester",
	}); err != nil {
		t.Fatalf("UpsertControlResult(wstg): %v", err)
	}
	if _, err := m.pg.UpsertControlResult(db.ControlResultInput{
		TaskID:        taskID,
		Standard:      db.StandardASVS,
		RequirementID: "V1.1.1",
		Verdict:       db.VerdictPass,
		EvidenceRef:   "snap-1",
		AssetsCovered: []int64{11},
		Actor:         "tester",
	}); err != nil {
		t.Fatalf("UpsertControlResult(asvs): %v", err)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID+"/control-results", nil)
	req.SetPathValue("id", task.ID)
	s.listControlResults(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("seeded status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ControlResults []controlResultDTO `json:"control_results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.ControlResults) != 2 {
		t.Fatalf("want 2 rows, got %d (%s)", len(resp.ControlResults), rec.Body.String())
	}
	// asvs sorts before wstg.
	if resp.ControlResults[0].Standard != db.StandardASVS || resp.ControlResults[0].Verdict != db.VerdictPass {
		t.Fatalf("first row wrong: %+v", resp.ControlResults[0])
	}
	if len(resp.ControlResults[0].AssetsCovered) != 1 || resp.ControlResults[0].AssetsCovered[0] != 11 {
		t.Fatalf("pass row assets wrong: %+v", resp.ControlResults[0])
	}
	if resp.ControlResults[1].Standard != db.StandardWSTG || resp.ControlResults[1].Verdict != db.VerdictNotApplicable {
		t.Fatalf("second row wrong: %+v", resp.ControlResults[1])
	}
	if resp.ControlResults[1].Reason != "out of scope" {
		t.Fatalf("not_applicable reason wrong: %+v", resp.ControlResults[1])
	}
}
