package server

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Autumn-27/artex/blueprint"
	"github.com/Autumn-27/artex/provision"
	"golang.org/x/crypto/bcrypt"
)

// TestApplyBlueprint is the DB-backed integration test for boot-time blueprint
// provisioning. It is GUARDED on ARTEX_TEST_DSN and skips when it is unset —
// it NEVER falls back to the app DSN / config.PostgresDSN() (that is the Track A
// footgun that wrote fixtures into production). The controller runs it against a
// throwaway Postgres: live check: controller.
func TestApplyBlueprint(t *testing.T) {
	dsn := os.Getenv("ARTEX_TEST_DSN")
	if dsn == "" {
		t.Skip("ARTEX_TEST_DSN not set; skipping DB-backed blueprint provisioning test (live check: controller)")
	}
	// Point the Manager/DB at the throwaway test database only. db.DSN precedence
	// is env ARTEX_PG_DSN > config file, so this wins without reading config.json.
	t.Setenv("ARTEX_PG_DSN", dsn)

	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Fatalf("open test manager against ARTEX_TEST_DSN: %v", err)
	}
	t.Cleanup(func() { m.Close() })

	// Build the Server exactly as production does (engine, agent resolver, maps)
	// so launchTask/admitTask run their real paths. Cancel the context right away
	// so the admitted task's engine goroutine exits before any real LLM call to
	// the (unreachable) test base URL.
	ctx, cancel := context.WithCancel(context.Background())
	td := t.TempDir()
	s := New(ctx, m, td, td, td)
	cancel()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	name := "test-bp-" + suffix
	companyName := "Blueprint Test Corp " + suffix
	goal := "Map the app for blueprint test " + suffix
	adminPassword := "correct horse battery staple"

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
			{Kind: "domain", Value: "bp-" + suffix + ".example"},
		},
		Constraints: []blueprint.Constraint{
			{Kind: "allow", Text: "only the in-scope domain"},
			{Kind: "deny", Text: "no exploitation"},
		},
		Task: blueprint.Task{
			Goal:                 goal,
			TimeoutSeconds:       3600,
			PlanHeartbeatSeconds: 600,
			CoverageEnabled:      false,
			SeedFirstIntent:      false,
		},
		Runtime: blueprint.Runtime{Workers: 3, LLMMaxConcurrent: 2, WebSearchEnabled: true},
		// Provider must satisfy the llm_profiles.format CHECK (openai/anthropic/
		// openai-responses). The non-routable base URL is never dialled here.
		LLM: blueprint.LLM{
			Provider: "openai", Model: "test-model",
			BaseURL: "http://127.0.0.1:1/v1", MaxTokens: 0,
			APIKey: "${env:IGNORED}",
		},
		Admin: blueprint.Admin{Username: "ARTEX", Password: "${env:IGNORED}"},
	}

	// The blueprint's secret refs are resolved by ResolveSecrets in production; in
	// the test we supply the already-resolved secrets directly to BuildPlan.
	plan, err := provision.BuildPlan(bp, provision.Secrets{
		AdminPassword: adminPassword,
		LLMAPIKey:     "test-api-key",
		LLMBaseURL:    bp.LLM.BaseURL,
	})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}

	pg := m.PG()

	// Make this run look like a fresh instance even on a reused throwaway DB:
	// clear the global marker and admin-hash rows (both use InsertSettingIfAbsent).
	mustExec(t, pg.DB, `DELETE FROM settings WHERE key IN ($1,$2)`, blueprintProvisionedKey, authPassKey)

	t.Cleanup(func() { cleanupBlueprintState(m, companyName, name, goal) })

	// --- First apply: provisions everything. ---
	if err := s.ApplyBlueprint(bp, plan); err != nil {
		t.Fatalf("first ApplyBlueprint: %v", err)
	}

	// Marker.
	if v, ok, _ := pg.GetSetting(blueprintProvisionedKey); !ok || v != name {
		t.Errorf("marker: got (%q, %v), want (%q, true)", v, ok, name)
	}
	// Admin password hash: present and verifies against the plaintext (not re-hashed).
	hash, ok, _ := pg.GetSetting(authPassKey)
	if !ok {
		t.Fatal("auth.password_hash not set")
	}
	if hash != plan.AdminPasswordHash {
		t.Errorf("auth.password_hash: got %q, want the plan hash %q", hash, plan.AdminPasswordHash)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(adminPassword)); err != nil {
		t.Errorf("stored hash does not verify against the admin password: %v", err)
	}

	// Settings.
	if got := m.Workers(); got != 3 {
		t.Errorf("workers: got %d, want 3", got)
	}
	if got := m.LLMMaxConcurrent(); got != 2 {
		t.Errorf("llm_max_concurrent: got %d, want 2", got)
	}
	if !pg.GetBool("web_search_enabled", false) {
		t.Error("web_search_enabled: got false, want true")
	}

	// Company.
	company, err := pg.Companies().GetCompanyByName(companyName)
	if err != nil || company == nil {
		t.Fatalf("company %q not found after provisioning (err=%v)", companyName, err)
	}
	companyID := company.ID

	// LLM profile: present and active.
	profileID := profileIDByName(t, pg.DB, name)
	var activeID int64
	if err := pg.QueryRow(`SELECT id FROM llm_profiles WHERE is_default`).Scan(&activeID); err != nil {
		t.Fatalf("read active profile: %v", err)
	}
	if activeID != profileID {
		t.Errorf("active profile: got id %d, want the provisioned profile id %d", activeID, profileID)
	}

	// Task: exists, bound to the company+profile, and admitted (not queued).
	var (
		taskID   int64
		expID    int64
		taskComp int64
		queued   bool
	)
	if err := pg.QueryRow(
		`SELECT id, exploration_id, COALESCE(company_id,0), queued FROM tasks WHERE goal=$1 AND deleted_at IS NULL`,
		goal).Scan(&taskID, &expID, &taskComp, &queued); err != nil {
		t.Fatalf("read provisioned task: %v", err)
	}
	if taskComp != companyID {
		t.Errorf("task company binding: got %d, want %d", taskComp, companyID)
	}
	if queued {
		t.Error("task is queued; expected it to be admitted (not queued)")
	}
	var profBindings int
	if err := pg.QueryRow(
		`SELECT count(*) FROM task_llm_profiles WHERE task_id=$1 AND profile_id=$2`,
		taskID, profileID).Scan(&profBindings); err != nil {
		t.Fatalf("read task profile binding: %v", err)
	}
	if profBindings != 1 {
		t.Errorf("task->profile bindings: got %d, want 1", profBindings)
	}

	// Constraints: both seeded with origin "system".
	constraints, err := pg.Exploration(expID).ListConstraints()
	if err != nil {
		t.Fatalf("list constraints: %v", err)
	}
	if len(constraints) != 2 {
		t.Fatalf("constraints: got %d, want 2", len(constraints))
	}
	for _, c := range constraints {
		if c.Origin != "system" {
			t.Errorf("constraint %q origin: got %q, want system", c.Text, c.Origin)
		}
	}

	// Authorization audit rows.
	if v, ok, _ := pg.GetSetting("blueprint.authorization.reference"); !ok || v != bp.Authorization.Reference {
		t.Errorf("authorization.reference: got (%q,%v), want %q", v, ok, bp.Authorization.Reference)
	}
	if v, ok, _ := pg.GetSetting("blueprint.authorization.authorized_by"); !ok || v != bp.Authorization.AuthorizedBy {
		t.Errorf("authorization.authorized_by: got (%q,%v), want %q", v, ok, bp.Authorization.AuthorizedBy)
	}
	if v, ok, _ := pg.GetSetting("blueprint.authorization.window"); !ok || v != "2026-10-20/2026-10-24" {
		t.Errorf("authorization.window: got (%q,%v), want %q", v, ok, "2026-10-20/2026-10-24")
	}

	// --- Second apply: must be idempotent (marker guard short-circuits). ---
	if err := s.ApplyBlueprint(bp, plan); err != nil {
		t.Fatalf("second ApplyBlueprint: %v", err)
	}
	if n := countRows(t, pg.DB, `SELECT count(*) FROM companies WHERE name=$1`, companyName); n != 1 {
		t.Errorf("companies after second apply: got %d, want 1", n)
	}
	if n := countRows(t, pg.DB, `SELECT count(*) FROM tasks WHERE goal=$1 AND deleted_at IS NULL`, goal); n != 1 {
		t.Errorf("tasks after second apply: got %d, want 1", n)
	}
	if v, ok, _ := pg.GetSetting(authPassKey); !ok || v != hash {
		t.Errorf("auth.password_hash changed after second apply: got %q, want unchanged %q", v, hash)
	}
	if v, ok, _ := pg.GetSetting(blueprintProvisionedKey); !ok || v != name {
		t.Errorf("marker after second apply: got (%q,%v), want %q", v, ok, name)
	}
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func countRows(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}

func profileIDByName(t *testing.T, db *sql.DB, name string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRow(`SELECT id FROM llm_profiles WHERE name=$1`, name).Scan(&id); err != nil {
		t.Fatalf("profile %q not found: %v", name, err)
	}
	return id
}

// cleanupBlueprintState best-effort removes what the test provisioned so a reused
// throwaway database stays usable. Errors are ignored; the controller runs
// against a disposable Postgres.
func cleanupBlueprintState(m *Manager, companyName, profileName, goal string) {
	pg := m.PG()
	rows, err := pg.Query(`SELECT id::text FROM tasks WHERE goal=$1`, goal)
	if err == nil {
		var ids []string
		for rows.Next() {
			var id string
			if rows.Scan(&id) == nil {
				ids = append(ids, id)
			}
		}
		rows.Close()
		for _, id := range ids {
			_, _ = m.DeleteTask(id, DeleteTaskOptions{})
		}
	}
	if company, _ := pg.Companies().GetCompanyByName(companyName); company != nil {
		_ = pg.Companies().DeleteCompany(company.ID)
	}
	_, _ = pg.Exec(`DELETE FROM llm_profiles WHERE name=$1`, profileName)
	_, _ = pg.Exec(`DELETE FROM settings WHERE key IN ($1,$2,$3,$4,$5)`,
		blueprintProvisionedKey, authPassKey,
		"blueprint.authorization.reference", "blueprint.authorization.authorized_by",
		"blueprint.authorization.window")
}
