package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Autumn-27/artex/blueprint"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/internal/dbtest"
	"github.com/Autumn-27/artex/provision"
	"github.com/Autumn-27/artex/standards"
)

// TestBlueprintStandardsSeeding is the DB-backed test for E.4: a blueprint
// naming `standards: [asvs-l1-web]` seeds every selected requirement as a
// not_tested control result owned by the system, flags the not-automatable
// ones with a reason, and records the profile list in settings. It reuses the
// TestApplyBlueprint setup pattern (dbtest.DSN + throwaway manager + a Server
// whose context is cancelled before New so no engine goroutine runs).
// Skips in the sandbox: live check: controller.
func TestBlueprintStandardsSeeding(t *testing.T) {
	dsn := dbtest.DSN(t)
	t.Setenv("ARTEX_PG_DSN", dsn)

	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Fatalf("open test manager: %v", err)
	}
	t.Cleanup(func() { m.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	td := t.TempDir()
	s := New(ctx, m, td, td, td)
	cancel()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	name := "test-bp-std-" + suffix
	companyName := "Blueprint Std Corp " + suffix
	goal := "Map the app for standards seed test " + suffix

	bp := &blueprint.Blueprint{
		Version: 1,
		Name:    name,
		Authorization: &blueprint.Authorization{
			AuthorizedBy: "Jane Doe",
			Reference:    "ENG-" + suffix,
			Window:       &blueprint.Window{Start: "2026-10-20", End: "2026-10-24"},
		},
		Company: blueprint.Company{Name: companyName},
		Scope: []blueprint.ScopeEntry{
			{Kind: "domain", Value: "bps-" + suffix + ".example"},
		},
		Standards: []string{"asvs-l1-web"},
		Task: blueprint.Task{
			Goal:            goal,
			TimeoutSeconds:  3600,
			SeedFirstIntent: false,
		},
		Runtime: blueprint.Runtime{Workers: 1, LLMMaxConcurrent: 1},
		LLM: blueprint.LLM{
			Provider: "openai", Model: "test-model",
			BaseURL: "http://127.0.0.1:1/v1",
			APIKey:  "${env:IGNORED}",
		},
		Admin: blueprint.Admin{Username: "ARTEX", Password: "${env:IGNORED}"},
	}

	plan, err := provision.BuildPlan(bp, provision.Secrets{
		AdminPassword: "correct horse battery staple",
		LLMAPIKey:     "test-api-key",
		LLMBaseURL:    bp.LLM.BaseURL,
	})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}

	pg := m.PG()
	// Fresh-instance look on a reused throwaway DB (same guard as TestApplyBlueprint).
	mustExec(t, pg.DB, `DELETE FROM settings WHERE key IN ($1,$2)`, blueprintProvisionedKey, authPassKey)
	t.Cleanup(func() {
		cleanupBlueprintState(m, companyName, name, goal)
		_, _ = pg.Exec(`DELETE FROM settings WHERE key='blueprint.standards'`)
	})

	if err := s.ApplyBlueprint(bp, plan); err != nil {
		t.Fatalf("ApplyBlueprint: %v", err)
	}

	// The settings row records the profile list.
	if v, ok, _ := pg.GetSetting("blueprint.standards"); !ok || v != "asvs-l1-web" {
		t.Errorf("blueprint.standards: got (%q,%v), want (\"asvs-l1-web\",true)", v, ok)
	}

	// Resolve the seeded task id the same way the status endpoint does.
	rawTaskID, ok, _ := pg.GetSetting(blueprintTaskIDKey)
	if !ok {
		t.Fatal("blueprint.task_id not set after apply")
	}
	taskID := mustTaskID(t, rawTaskID)

	// Exactly one not_tested row per selected requirement, actor "system".
	catalog, err := standards.Embedded()
	if err != nil {
		t.Fatalf("embedded catalog: %v", err)
	}
	want, err := catalog.SelectRequirements([]string{"asvs-l1-web"})
	if err != nil {
		t.Fatalf("SelectRequirements: %v", err)
	}
	results, err := pg.ListControlResults(taskID)
	if err != nil {
		t.Fatalf("ListControlResults: %v", err)
	}
	if len(results) != len(want) {
		t.Fatalf("control results: got %d rows, want %d (selected requirements)", len(results), len(want))
	}
	got := map[string]*db.ControlResult{}
	for _, r := range results {
		if r.Verdict != db.VerdictNotTested {
			t.Errorf("row %s/%s verdict = %q, want not_tested", r.Standard, r.RequirementID, r.Verdict)
		}
		if r.Actor != "system" {
			t.Errorf("row %s/%s actor = %q, want system", r.Standard, r.RequirementID, r.Actor)
		}
		got[r.Standard+"\x00"+r.RequirementID] = r
	}
	for _, w := range want {
		if got[w.Standard+"\x00"+w.ID] == nil {
			t.Errorf("missing seeded row for %s/%s", w.Standard, w.ID)
		}
	}

	// V2.1.1 (documentation wording) carries the not-automatable reason;
	// V1.2.2 (level 1, no documentation wording) has none.
	v211 := got[db.StandardASVS+"\x00V2.1.1"]
	if v211 == nil {
		t.Fatal("V2.1.1 row missing")
	}
	if v211.Reason == "" {
		t.Error("V2.1.1 has no not-automatable reason")
	}
	v122 := got[db.StandardASVS+"\x00V1.2.2"]
	if v122 == nil {
		t.Fatal("V1.2.2 row missing")
	}
	if v122.Reason != "" {
		t.Errorf("V1.2.2 unexpectedly has reason %q", v122.Reason)
	}

	// The coverage report, with no asvs_level param, derives its ASVS scope
	// from the blueprint's profiles: max_level 1 for asvs-l1-web.
	token, err := signJWT(s.jwtKey)
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/api/tasks/"+rawTaskID+"/coverage-report?format=json", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("coverage-report status=%d body=%s", w.Code, w.Body.String())
	}
	var doc struct {
		Summaries []struct {
			Standard string `json:"standard"`
			MaxLevel int    `json:"max_level"`
		} `json:"summaries"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("json parse: %v (%s)", err, w.Body.String())
	}
	var asvsMax int = -1
	for _, sum := range doc.Summaries {
		if sum.Standard == db.StandardASVS {
			asvsMax = sum.MaxLevel
		}
	}
	if asvsMax != 1 {
		t.Errorf("ASVS summary max_level = %d, want 1 (from asvs-l1-web)", asvsMax)
	}
}
