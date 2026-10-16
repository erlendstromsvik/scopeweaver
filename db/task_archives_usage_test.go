package db

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// S17 and S5: new and legacy manifests through the real snapshot/cold/restore path.
func TestTaskArchiveReasoningUsage(t *testing.T) {
	for _, mode := range []string{"new", "absent", "null"} {
		t.Run(mode, func(t *testing.T) {
			d, err := Open(testDSN(t))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = d.Close() })
			if err := d.EnsureLLMUsageTable(); err != nil {
				t.Fatal(err)
			}
			if err := d.EnsureLLMRecordsTable(); err != nil {
				t.Fatal(err)
			}
			task, err := d.CreateTaskWithOptions("reasoning archive", "usage round trip", TaskCreateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			key := fmt.Sprintf("reasoning-archive-%d", task.ID)
			t.Cleanup(func() {
				_, _ = d.Exec(`DELETE FROM llm_usage WHERE profile_name=$1`, key)
				_, _ = d.Exec(`DELETE FROM task_archives WHERE task_id=$1`, task.ID)
				_ = d.DeleteTask(task.ID)
			})
			if err := d.SetPaused(task.ID, true); err != nil {
				t.Fatal(err)
			}
			if err := d.InsertLLMUsage(&LLMUsage{TaskID: fmt.Sprint(task.ID), ExplorationID: task.ExplorationID, ProfileName: key, Model: "test", InputTokens: 11, OutputTokens: 7, CacheRead: 3, CacheWrite: 2, ReasoningTokens: 4}); err != nil {
				t.Fatal(err)
			}
			archive, err := d.QueueTaskArchive(task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := d.ClaimTaskArchiveJob(t.Context()); err != nil {
				t.Fatal(err)
			}
			snapshot, err := d.SnapshotTaskArchive(task.ID)
			if err != nil {
				t.Fatal(err)
			}
			wantReasoning := 4
			if mode != "new" {
				wantReasoning = 0
				var rows []map[string]any
				if err := json.Unmarshal(snapshot.Tables["llm_usage"], &rows); err != nil {
					t.Fatal(err)
				}
				for _, row := range rows {
					if mode == "absent" {
						delete(row, "reasoning_tokens")
					} else {
						row["reasoning_tokens"] = nil
					}
				}
				snapshot.Tables["llm_usage"], err = json.Marshal(rows)
				if err != nil {
					t.Fatal(err)
				}
				// Older manifests also omit the new aggregate dimension.
				var stats map[string]any
				encoded, err := json.Marshal(snapshot.AggregateStats)
				if err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(encoded, &stats); err != nil {
					t.Fatal(err)
				}
				delete(stats["tokens"].(map[string]any), "reasoning_tokens")
				for _, field := range []string{"token_profiles", "token_daily"} {
					for _, row := range stats[field].([]any) {
						delete(row.(map[string]any), "reasoning_tokens")
					}
				}
				snapshot.AggregateStats = stats
			}
			if err := d.CompleteTaskArchive(archive.ID, snapshot, "/tmp/reasoning-test.tar.zst", "test", 10, 10); err != nil {
				t.Fatal(err)
			}
			ready, err := d.GetTaskArchive(archive.ID)
			if err != nil {
				t.Fatal(err)
			}
			var stats map[string]json.RawMessage
			if err := json.Unmarshal(ready.AggregateStats, &stats); err != nil {
				t.Fatal(err)
			}
			if mode == "new" {
				assertUsageJSON(t, json.RawMessage(stats["tokens"]), map[string]any{"calls": 1, "input_tokens": 11, "output_tokens": 7, "cache_read_tokens": 3, "cache_write_tokens": 2, "reasoning_tokens": 4})
			}
			checkTotals := func(calls, input, output, reasoning int) {
				t.Helper()
				profiles, err := d.UsageByProfile()
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, p := range profiles {
					if p.ProfileName == key {
						found = true
						assertUsageJSON(t, p, map[string]any{"profile_name": key, "calls": calls, "tasks": calls, "input_tokens": input, "output_tokens": output, "cache_read_tokens": 3, "cache_write_tokens": 2, "reasoning_tokens": reasoning})
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
				for _, p := range daily {
					if p.ProfileName == key {
						found = true
						assertUsageJSON(t, p, map[string]any{"profile_name": key, "date": time.Now().UTC().Format("2006-01-02"), "input_tokens": input, "output_tokens": output, "cache_read_tokens": 3, "reasoning_tokens": reasoning})
					}
				}
				if !found {
					t.Fatal("daily missing")
				}
			}
			checkTotals(1, 11, 7, wantReasoning)
			if mode == "new" {
				// Same profile and day from a live task must merge with the cold archive.
				if err := d.InsertLLMUsage(&LLMUsage{TaskID: key, ProfileName: key, Model: "test", InputTokens: 2, OutputTokens: 3, ReasoningTokens: 2}); err != nil {
					t.Fatal(err)
				}
				checkTotals(2, 13, 10, 6)
				if _, err := d.Exec(`DELETE FROM llm_usage WHERE task_id=$1`, key); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := d.QueueTaskArchiveRestore(archive.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := d.ClaimTaskArchiveJob(t.Context()); err != nil {
				t.Fatal(err)
			}
			if _, err := d.RestoreTaskArchive(archive.ID, snapshot, 0); err != nil {
				t.Fatal(err)
			}
			if err := d.CompleteTaskArchiveRestore(archive.ID); err != nil {
				t.Fatal(err)
			}
			var reasoning int
			if err := d.QueryRow(`SELECT reasoning_tokens FROM llm_usage WHERE task_id=$1`, fmt.Sprint(task.ID)).Scan(&reasoning); err != nil {
				t.Fatal(err)
			}
			if reasoning != wantReasoning {
				t.Fatalf("restored reasoning=%d want %d", reasoning, wantReasoning)
			}
			checkTotals(1, 11, 7, wantReasoning)
		})
	}
}
