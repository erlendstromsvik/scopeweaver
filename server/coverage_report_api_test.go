package server

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/internal/dbtest"
)

// TestCoverageReportHandler exercises the coverage-report endpoint over the full
// auth + routing path against a throwaway DB: a task with one ASVS pass for
// V1.1.1, then each format and each error case.
func TestCoverageReportHandler(t *testing.T) {
	dbtest.DSN(t) // skip unless ARTEX_TEST_DSN points at a throwaway DB
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	task, err := m.CreateTask("coverage report task", "goal", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	taskID := mustTaskID(t, task.ID)

	// Cancel BEFORE New so no background engine loops start on the task
	// (the trafficEvidenceServer pattern); otherwise teardown hangs.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := New(ctx, m, t.TempDir(), t.TempDir(), t.TempDir())
	s.archiveWG.Wait()
	<-s.side.done
	t.Cleanup(func() {
		m.pg.Exec(`DELETE FROM task_archives WHERE task_id=$1`, taskID)
		m.pg.DeleteTask(taskID)
	})
	token, err := signJWT(s.jwtKey)
	if err != nil {
		t.Fatal(err)
	}
	request := func(path string, auth bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		if auth {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}

	// Seed one ASVS pass for V1.1.1 (pass needs evidence + assets).
	if _, err := m.pg.UpsertControlResult(db.ControlResultInput{
		TaskID:        taskID,
		Standard:      db.StandardASVS,
		RequirementID: "V1.1.1",
		Verdict:       db.VerdictPass,
		Method:        db.MethodAutomated,
		EvidenceRef:   "snap-1",
		AssetsCovered: []int64{11},
		Actor:         "tester",
	}); err != nil {
		t.Fatalf("UpsertControlResult: %v", err)
	}

	// Seed a finding with refs so the Markdown "Findings by OWASP Top 10"
	// section (E.5c) has something to render.
	fid, err := m.pg.AddFinding(taskID, 0, "XSS", "Reflected XSS in search", "high", "s", "poc", "worker", nil)
	if err != nil {
		t.Fatalf("AddFinding: %v", err)
	}
	for _, r := range [][2]string{
		{db.SchemeCWE, "CWE-79"},
		{db.SchemeOWASPTop10, "A05:2025"},
		{db.SchemeASVS, "V5.1.1"},
	} {
		if _, err := m.pg.AddFindingRef(fid, r[0], r[1]); err != nil {
			t.Fatalf("AddFindingRef(%s,%s): %v", r[0], r[1], err)
		}
	}
	t.Cleanup(func() { m.pg.DeleteFindingsByTask(taskID) })

	base := "/api/tasks/" + task.ID + "/coverage-report"

	// No auth -> 401.
	if w := request(base, false); w.Code != http.StatusUnauthorized {
		t.Fatalf("no auth status=%d body=%s", w.Code, w.Body.String())
	}

	// Unknown task -> 404.
	if w := request("/api/tasks/999999999/coverage-report", true); w.Code != http.StatusNotFound {
		t.Fatalf("unknown task status=%d body=%s", w.Code, w.Body.String())
	}

	// format=json -> 200, the V1.1.1 row has verdict "pass".
	w := request(base+"?format=json", true)
	if w.Code != http.StatusOK {
		t.Fatalf("json status=%d body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("json content-type = %q", ct)
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, `attachment; filename="coverage-`+task.ID+`-`) || !strings.Contains(cd, `.json"`) {
		t.Errorf("json content-disposition = %q", cd)
	}
	var doc struct {
		Generated string            `json:"generated"`
		Summaries []any             `json:"summaries"`
		Rows      []json.RawMessage `json:"rows"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("json parse: %v (%s)", err, w.Body.String())
	}
	if doc.Generated == "" {
		t.Error("json missing generated")
	}
	var v111 map[string]any
	for _, raw := range doc.Rows {
		var row map[string]any
		if err := json.Unmarshal(raw, &row); err != nil {
			t.Fatal(err)
		}
		if row["requirement_id"] == "V1.1.1" {
			v111 = row
			break
		}
	}
	if v111 == nil {
		t.Fatal("V1.1.1 row missing from json")
	}
	if v111["verdict"] != "pass" {
		t.Errorf("V1.1.1 verdict = %v, want pass", v111["verdict"])
	}

	// format=csv -> text/csv with a header row.
	w = request(base+"?format=csv", true)
	if w.Code != http.StatusOK {
		t.Fatalf("csv status=%d body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/csv") {
		t.Errorf("csv content-type = %q", ct)
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, `.csv"`) {
		t.Errorf("csv content-disposition = %q", cd)
	}
	body := w.Body.Bytes()
	if !bytes.HasPrefix(body, []byte("\xEF\xBB\xBF")) {
		t.Error("csv missing BOM")
	}
	recs, err := csv.NewReader(bytes.NewReader(body[3:])).ReadAll()
	if err != nil {
		t.Fatalf("csv parse: %v", err)
	}
	if len(recs) < 2 || recs[0][0] != "Standard" || recs[0][1] != "Requirement" {
		t.Fatalf("csv header = %v", recs[0])
	}

	// format=md (default) -> contains "# Coverage report".
	w = request(base, true)
	if w.Code != http.StatusOK {
		t.Fatalf("md status=%d body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/markdown") {
		t.Errorf("md content-type = %q", ct)
	}
	if !strings.Contains(w.Body.String(), "# Coverage report") {
		t.Error("md missing '# Coverage report' header")
	}
	// E.5c: the Markdown report carries the findings-by-Top-10 section with the
	// seeded finding's name.
	if !strings.Contains(w.Body.String(), "## Findings by OWASP Top 10") {
		t.Error("md missing '## Findings by OWASP Top 10' section")
	}
	if !strings.Contains(w.Body.String(), "Reflected XSS in search") {
		t.Error("md missing the seeded finding name in the Top 10 section")
	}

	// format=pdf -> 400.
	if w := request(base+"?format=pdf", true); w.Code != http.StatusBadRequest {
		t.Fatalf("pdf format status=%d body=%s", w.Code, w.Body.String())
	}

	// asvs_level=9 -> 400.
	if w := request(base+"?format=json&asvs_level=9", true); w.Code != http.StatusBadRequest {
		t.Fatalf("asvs_level=9 status=%d body=%s", w.Code, w.Body.String())
	}

	// asvs_level=1 -> 200 (valid).
	if w := request(base+"?format=json&asvs_level=1", true); w.Code != http.StatusOK {
		t.Fatalf("asvs_level=1 status=%d body=%s", w.Code, w.Body.String())
	}
}
