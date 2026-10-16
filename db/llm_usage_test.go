package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"
)

// S5: use a separate schema so this really starts with the pre-reasoning table.
func oldUsageDatabase(t *testing.T) *DB {
	t.Helper()
	pool, err := sql.Open("pgx", testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	d := &DB{pool}
	schema := fmt.Sprintf("usage_old_%d", time.Now().UnixNano())
	if _, err := d.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = d.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); _ = d.Close() })
	if _, err := d.Exec(`SET search_path TO ` + schema); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE TABLE llm_usage (
 id BIGSERIAL PRIMARY KEY, ts TIMESTAMPTZ NOT NULL DEFAULT now(), task_id TEXT,
 exploration_id BIGINT, worker TEXT, model TEXT, profile_name TEXT, latency_ms INTEGER,
 input_tokens INTEGER NOT NULL DEFAULT 0, output_tokens INTEGER NOT NULL DEFAULT 0,
 cache_read INTEGER NOT NULL DEFAULT 0, cache_write INTEGER NOT NULL DEFAULT 0, status TEXT
 ); INSERT INTO llm_usage(task_id,input_tokens,output_tokens) VALUES('old',11,7)`); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestLLMUsageReasoningMigration(t *testing.T) {
	d := oldUsageDatabase(t)
	for range 2 {
		if err := d.EnsureLLMUsageTable(); err != nil {
			t.Fatal(err)
		}
	}
	var input, output, reasoning int
	if err := d.QueryRow(`SELECT input_tokens,output_tokens,reasoning_tokens FROM llm_usage WHERE task_id='old'`).Scan(&input, &output, &reasoning); err != nil {
		t.Fatal(err)
	}
	if input != 11 || output != 7 || reasoning != 0 {
		t.Fatalf("old row changed: %d %d %d", input, output, reasoning)
	}
	if err := d.InsertLLMUsage(&LLMUsage{TaskID: "new", InputTokens: 13, OutputTokens: 9, ReasoningTokens: 4}); err != nil {
		t.Fatal(err)
	}
	if err := d.EnsureLLMUsageTable(); err != nil {
		t.Fatal(err)
	}
	if err := d.QueryRow(`SELECT reasoning_tokens FROM llm_usage WHERE task_id='new'`).Scan(&reasoning); err != nil {
		t.Fatal(err)
	}
	if reasoning != 4 {
		t.Fatalf("reasoning=%d want 4", reasoning)
	}
	var nullable, defaultValue string
	if err := d.QueryRow(`SELECT is_nullable,column_default FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='llm_usage' AND column_name='reasoning_tokens'`).Scan(&nullable, &defaultValue); err != nil {
		t.Fatal(err)
	}
	if nullable != "NO" || defaultValue != "0" {
		t.Fatalf("column nullable=%s default=%s", nullable, defaultValue)
	}
}

func assertUsageJSON(t *testing.T, value any, want map[string]any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	// JSON numbers are decoded consistently for an exact old-key/value assertion.
	expected, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var normalized map[string]any
	if err := json.Unmarshal(expected, &normalized); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, normalized) {
		t.Errorf("usage=%s want %s", raw, expected)
	}
}

// S5: reasoning is a separate subset of output; totals and ordering stay unchanged.
func TestLLMUsageReasoningAggregates(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := d.EnsureLLMUsageTable(); err != nil {
		t.Fatal(err)
	}
	key := fmt.Sprintf("reasoning-%d", time.Now().UnixNano())
	t.Cleanup(func() { _, _ = d.Exec(`DELETE FROM llm_usage WHERE profile_name=$1`, key) })
	for _, u := range []LLMUsage{
		{TaskID: key, ProfileName: key, Model: "a", Worker: "judge", InputTokens: 11, OutputTokens: 7, CacheRead: 3, CacheWrite: 2, ReasoningTokens: 4, Status: "ok"},
		{TaskID: key, ProfileName: key, Model: "a", Worker: "judge", InputTokens: 13, OutputTokens: 9, CacheRead: 5, CacheWrite: 1, ReasoningTokens: 6, Status: "error"},
		{TaskID: key, ProfileName: key, Model: "b", Worker: "web_search", InputTokens: 1, OutputTokens: 1, ReasoningTokens: 1, Status: "ok"},
	} {
		if err := d.InsertLLMUsage(&u); err != nil {
			t.Fatal(err)
		}
	}
	models, err := d.TokenByModel(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatalf("models=%+v", models)
	}
	assertUsageJSON(t, models[0], map[string]any{"model": "a", "calls": 2, "input_tokens": 24, "output_tokens": 16, "cache_read_tokens": 8, "cache_write_tokens": 3, "reasoning_tokens": 10})
	profiles, err := d.UsageByProfile()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range profiles {
		if p.ProfileName == key {
			found = true
			assertUsageJSON(t, p, map[string]any{"profile_name": key, "calls": 3, "tasks": 1, "input_tokens": 25, "output_tokens": 17, "cache_read_tokens": 8, "cache_write_tokens": 3, "reasoning_tokens": 11})
		}
	}
	if !found {
		t.Fatal("profile missing")
	}
	daily, err := d.UsageDaily(1)
	if err != nil {
		t.Fatal(err)
	}
	found = false
	today := time.Now().UTC().Format("2006-01-02")
	for _, p := range daily {
		if p.ProfileName == key {
			found = true
			assertUsageJSON(t, p, map[string]any{"profile_name": key, "date": today, "input_tokens": 25, "output_tokens": 17, "cache_read_tokens": 8, "reasoning_tokens": 11})
		}
	}
	if !found {
		t.Fatal("daily missing")
	}
	// Isolate judge totals without disturbing other suites' fixture rows.
	if _, err := d.Exec(`UPDATE llm_usage SET worker=$1 WHERE worker='judge' AND profile_name<>$2`, key, key); err != nil {
		t.Fatal(err)
	}
	defer d.Exec(`UPDATE llm_usage SET worker='judge' WHERE worker=$1`, key)
	judge, err := d.JudgeUsageStats(1)
	if err != nil {
		t.Fatal(err)
	}
	assertUsageJSON(t, judge, map[string]any{"calls": 2, "input_tokens": 24, "output_tokens": 16, "cache_read_tokens": 8, "cache_write_tokens": 3, "reasoning_tokens": 10, "daily": []any{map[string]any{"date": today, "calls": 2, "input_tokens": 24, "output_tokens": 16, "reasoning_tokens": 10}}})
}

// S5: the restore shim must handle both omitted and explicitly null old fields.
func TestLLMUsageReasoningLegacyRestoreRows(t *testing.T) {
	for _, suffix := range []string{"", `,"reasoning_tokens":null`} {
		t.Run(fmt.Sprintf("field=%s", suffix), func(t *testing.T) {
			d := oldUsageDatabase(t)
			if _, err := d.Exec(`ALTER TABLE llm_usage ADD COLUMN reasoning_tokens INTEGER NOT NULL DEFAULT 0`); err != nil {
				t.Fatal(err)
			}
			tx, err := d.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			raw := json.RawMessage(`[{"id":2,"ts":"2026-10-16T00:00:00Z","task_id":"legacy","input_tokens":11,"output_tokens":7,"cache_read":0,"cache_write":0` + suffix + `}]`)
			if err := insertArchiveRows(tx, "llm_usage", raw); err != nil {
				t.Fatal(err)
			}
			var reasoning int
			if err := tx.QueryRow(`SELECT reasoning_tokens FROM llm_usage WHERE task_id='legacy'`).Scan(&reasoning); err != nil {
				t.Fatal(err)
			}
			if reasoning != 0 {
				t.Fatalf("reasoning=%d want 0", reasoning)
			}
		})
	}
}
