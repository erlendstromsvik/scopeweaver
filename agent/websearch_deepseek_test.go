package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/internal/dbtest"
	"github.com/Autumn-27/artex/llmrec"
	"github.com/Autumn-27/norma/agentcore"
	"github.com/Autumn-27/norma/transcript"
	"github.com/google/uuid"
)

func TestDeepSeekSearchInvalidProxyFailsClosed(t *testing.T) {
	for _, proxy := range []string{"http://%", "http://fixture:dummy-password@%", "http://", "proxy.example:8080", "ftp://proxy.example"} {
		t.Run(proxy, func(t *testing.T) {
			assertDeepSeekSearchConfigError(t, actool.WebSearchConfig{Proxy: proxy}, "proxy")
		})
	}
}

func TestDeepSeekSearchInvalidCAFailsClosed(t *testing.T) {
	badPEM := filepath.Join(t.TempDir(), "invalid.pem")
	if err := os.WriteFile(badPEM, []byte("not a certificate"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(t.TempDir(), "missing.pem"), badPEM} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			assertDeepSeekSearchConfigError(t, actool.WebSearchConfig{CACert: path}, "CA")
		})
	}
}

func TestDeepSeekSearchInsecureTLSIgnoresBadCA(t *testing.T) {
	// InsecureTLS wins and ignores CACert (Norma precedence); a bad CA must not fail here.
	badPEM := filepath.Join(t.TempDir(), "invalid.pem")
	if err := os.WriteFile(badPEM, []byte("not a certificate"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, ca := range []string{"", badPEM, filepath.Join(t.TempDir(), "missing.pem")} {
		if err := deepSeekSearchClientErr(actool.WebSearchConfig{InsecureTLS: true, CACert: ca}); err != nil {
			t.Errorf("InsecureTLS with CACert=%q: unexpected error %v", ca, err)
		}
	}
}

func assertDeepSeekSearchConfigError(t *testing.T, cfg actool.WebSearchConfig, want string) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		fmt.Fprint(w, deepSeekHitFrame+deepSeekUsageFrame)
	}))
	defer srv.Close()
	cfg.Backend, cfg.DeepSeekBaseURL, cfg.DeepSeekAPIKey, cfg.DeepSeekModel = "deepseek", srv.URL, "fixture", "fixture-model"
	core, err := NewDeepSeekWebSearch(cfg, DeepSeekSearchMeter{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := core.Call(t.Context(), json.RawMessage(`{"query":"fixture"}`), nil)
	if err != nil || !result.IsError || !strings.Contains(result.Flatten(), "Error searching web via deepseek:") || !strings.Contains(result.Flatten(), want) {
		t.Errorf("configuration must fail through tool error contract: result=%+v err=%v", result, err)
	}
	if strings.Contains(result.Flatten(), "dummy-password") {
		t.Error("tool error exposed proxy password")
	}
	if _, err := DeepSeekWebSearchProbe(t.Context(), cfg, DeepSeekSearchMeter{}, "fixture", 1); err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("probe configuration error=%v, want %s", err, want)
	}
	if got := hits.Load(); got != 0 {
		t.Errorf("invalid configuration made %d outbound requests, want 0", got)
	}
}

// S9: hit-limit results precede usage, so returning at the first result loses
// the metering frame. Exercise the existing production tool construction seam.
func TestDeepSeekSearchUsageAfterHitLimit(t *testing.T) {
	pg, err := db.Open(dbtest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	if err := pg.EnsureLLMUsageTable(); err != nil {
		t.Fatal(err)
	}
	task := "search-" + uuid.NewString()
	defer pg.Exec(`DELETE FROM llm_usage WHERE task_id=$1`, task)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/anthropic/v1/messages" || r.Header.Get("x-api-key") != "fixture-only" {
			t.Errorf("request contract: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"content_block_start\",\"content_block\":{\"type\":\"web_search_tool_result\",\"content\":[{\"type\":\"web_search_result\",\"title\":\"Fixture\",\"url\":\"https://example.com\"}]}}\n\n")
		w.(http.Flusher).Flush()
		time.Sleep(20 * time.Millisecond)
		fmt.Fprint(w, "data: {\"type\":\"message_delta\",\"usage\":{\"input_tokens\":100,\"output_tokens\":20,\"cache_read_input_tokens\":30,\"cache_creation_input_tokens\":5}}\n\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer srv.Close()
	opts := agentcore.Options{EnableWebSearch: true, WebSearchBackend: "deepseek", DeepSeekSearchBaseURL: srv.URL + "/anthropic", DeepSeekSearchAPIKey: "fixture-only", DeepSeekSearchModel: "fixture-model"}
	secureWebSearch(&opts, func() (bool, []string) { return false, nil }, DeepSeekSearchMeter{Store: pg, ProfileName: "fixture-profile"})
	ctx := llmrec.WithTaskID(transcript.WithSessionID(t.Context(), "exp42-worker-i1"), task)
	result, err := opts.Tools[0].Call(ctx, json.RawMessage(`{"query":"fixture","limit":1}`), nil)
	if err != nil || result.IsError {
		t.Fatalf("search: %+v %v", result, err)
	}
	var count, input, output int
	if err := pg.QueryRow(`SELECT COUNT(*),COALESCE(SUM(input_tokens),0),COALESCE(SUM(output_tokens),0) FROM llm_usage WHERE task_id=$1 AND worker='web_search'`, task).Scan(&count, &input, &output); err != nil {
		t.Fatal(err)
	}
	if count != 1 || input != 135 || output != 20 {
		t.Fatalf("search usage rows=%d input=%d output=%d; want 1,135,20", count, input, output)
	}
}

const deepSeekHitFrame = "data: {\"type\":\"content_block_start\",\"content_block\":{\"type\":\"web_search_tool_result\",\"content\":[{\"url\":\"https://example.com\"},{\"url\":\"https://example.com\"},{\"title\":\"Two\",\"url\":\"https://two.example\"}]}}\n\n"
const deepSeekUsageFrame = "data: {\"type\":\"message_delta\",\"usage\":{\"input_tokens\":100,\"output_tokens\":20,\"cache_read_input_tokens\":30,\"cache_creation_input_tokens\":5,\"output_tokens_details\":{\"thinking_tokens\":4}}}\n\n"
const deepSeekErrorFrame = "data: {\"type\":\"error\",\"error\":{\"message\":\"fixture failed\"}}\n\n"

// S9/S20: the tool and permission-bypassing probe share error/usage behavior.
func TestDeepSeekSearchMeteringMatrix(t *testing.T) {
	pg, err := db.Open(dbtest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	if err = pg.EnsureLLMUsageTable(); err != nil {
		t.Fatal(err)
	}
	for _, probe := range []bool{false, true} {
		for _, tc := range []struct {
			name, frames, status                               string
			httpStatus, limit, wantInput, wantOutput, wantHits int
			wantError, hang                                    bool
		}{
			{"normal", deepSeekHitFrame + deepSeekUsageFrame, "ok", 200, 3, 135, 20, 2, false, false},
			{"limit-before-usage", deepSeekHitFrame + deepSeekUsageFrame, "ok", 200, 1, 135, 20, 1, false, false},
			{"unknown-usage", deepSeekHitFrame, "ok", 200, 1, 0, 0, 1, false, false},
			{"start-only-unknown", "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":99}}}\n\n" + deepSeekHitFrame, "ok", 200, 1, 0, 0, 1, false, false},
			{"http-error", "fixture failed", "error", 503, 1, 0, 0, 0, true, false},
			{"early-stream-error", deepSeekErrorFrame, "error", 200, 1, 0, 0, 0, true, false},
			{"late-stream-error", deepSeekHitFrame + deepSeekUsageFrame + deepSeekErrorFrame, "error", 200, 1, 135, 20, 1, false, false},
			{"late-timeout", deepSeekHitFrame + deepSeekUsageFrame, "error", 200, 1, 135, 20, 1, false, true},
		} {
			t.Run(fmt.Sprintf("probe=%v/%s", probe, tc.name), func(t *testing.T) {
				task := "search-" + uuid.NewString()
				defer pg.Exec(`DELETE FROM llm_usage WHERE task_id=$1`, task)
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "text/event-stream")
					w.WriteHeader(tc.httpStatus)
					fmt.Fprint(w, tc.frames)
					w.(http.Flusher).Flush()
					if tc.hang {
						<-r.Context().Done()
					}
				}))
				defer srv.Close()
				cfg := actool.WebSearchConfig{Backend: "deepseek", DeepSeekBaseURL: srv.URL + "/v1/messages/", DeepSeekAPIKey: "fixture", DeepSeekModel: "model"}
				meter := DeepSeekSearchMeter{Store: pg, ProfileName: "search-profile"}
				core, e := NewDeepSeekWebSearch(cfg, meter)
				if e != nil {
					t.Fatal(e)
				}
				core.(*deepSeekSearch).drainTimeout = 30 * time.Millisecond
				ctx, cancel := context.WithCancel(llmrec.WithTaskID(transcript.WithSessionID(t.Context(), "exp42-planner"), task))
				defer cancel()
				var hits []actool.SearchResult
				if probe {
					// The probe calls the same search method; shorten only this fixture's
					// drain deadline through its context, leaving production's 10s intact.
					if tc.hang {
						time.AfterFunc(30*time.Millisecond, cancel)
					}
					hits, e = DeepSeekWebSearchProbe(ctx, cfg, meter, "fixture", tc.limit)
					if (e != nil) != tc.wantError {
						t.Fatalf("probe err=%v", e)
					}
				} else {
					input, _ := json.Marshal(map[string]any{"query": "fixture", "limit": tc.limit})
					result, callErr := core.Call(ctx, input, nil)
					if callErr != nil || result.IsError != tc.wantError {
						t.Fatalf("tool %+v %v", result, callErr)
					}
					if !result.IsError {
						var out struct {
							Results []actool.SearchResult `json:"results"`
						}
						if e = json.Unmarshal([]byte(result.Flatten()), &out); e != nil {
							t.Fatal(e)
						}
						hits = out.Results
					}
				}
				if len(hits) != tc.wantHits {
					t.Fatalf("hits=%+v", hits)
				}
				if len(hits) > 0 && (hits[0].Title != "example.com" || hits[0].Position != 1 || hits[0].Description != "") {
					t.Fatalf("result contract: %+v", hits)
				}
				var count, input, output, read, write, reasoning, exp int
				var status, profile, model string
				e = pg.QueryRow(`SELECT COUNT(*),COALESCE(SUM(input_tokens),0),COALESCE(SUM(output_tokens),0),COALESCE(SUM(cache_read),0),COALESCE(SUM(cache_write),0),COALESCE(SUM(reasoning_tokens),0),COALESCE(MAX(exploration_id),0),COALESCE(MAX(status),''),COALESCE(MAX(profile_name),''),COALESCE(MAX(model),'') FROM llm_usage WHERE task_id=$1 AND worker='web_search'`, task).Scan(&count, &input, &output, &read, &write, &reasoning, &exp, &status, &profile, &model)
				if e != nil {
					t.Fatal(e)
				}
				wantRead, wantWrite, wantReasoning := 0, 0, 0
				if tc.wantInput > 0 {
					wantRead, wantWrite, wantReasoning = 30, 5, 4
				}
				if count != 1 || input != tc.wantInput || output != tc.wantOutput || read != wantRead || write != wantWrite || reasoning != wantReasoning || exp != 42 || status != tc.status || profile != "search-profile" || model != "model" {
					t.Fatalf("row count=%d in=%d out=%d read=%d write=%d reasoning=%d exp=%d status=%q profile=%q model=%q", count, input, output, read, write, reasoning, exp, status, profile, model)
				}
			})
		}
	}
}

// S21: retain Norma's decision; the permission pipeline denies without invoking
// Call and therefore without outbound requests or a usage row.
func TestDeepSeekSearchPermissionsAndPolicy(t *testing.T) {
	pg, err := db.Open(dbtest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	if err = pg.EnsureLLMUsageTable(); err != nil {
		t.Fatal(err)
	}
	task := "search-" + uuid.NewString()
	defer pg.Exec(`DELETE FROM llm_usage WHERE task_id=$1`, task)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, deepSeekHitFrame+deepSeekUsageFrame)
	}))
	defer srv.Close()
	opts := agentcore.Options{EnableWebSearch: true, WebSearchBackend: "deepseek", DeepSeekSearchBaseURL: srv.URL, DeepSeekSearchAPIKey: "fixture", DeepSeekSearchModel: "fixture"}
	strict := false
	secureWebSearch(&opts, func() (bool, []string) { return strict, nil }, DeepSeekSearchMeter{Store: pg, ProfileName: "fixture"})
	tool := opts.Tools[0]
	original, _ := actool.NewWebSearch(actool.WebSearchConfig{Backend: "deepseek", DeepSeekBaseURL: srv.URL, DeepSeekAPIKey: "fixture", DeepSeekModel: "fixture"})
	input := json.RawMessage(`{"query":"fixture","limit":1}`)
	ctx := llmrec.WithTaskID(t.Context(), task)
	if !reflect.DeepEqual(tool.CheckPermissions(ctx, input, permission.Context{}), original.CheckPermissions(ctx, input, permission.Context{})) {
		t.Fatal("permission decision changed")
	}
	for _, deny := range []bool{true, false} {
		pc := permission.Context{Mode: permission.ModeBypass}
		if deny {
			pc.Disallowed = []string{"web_search"}
		}
		decision := permission.Evaluate(ctx, permission.Request{ToolName: tool.Name(), Input: input, IsReadOnly: tool.IsReadOnly(input), ToolDecision: tool.CheckPermissions(ctx, input, pc), Ctx: pc})
		if deny && decision.Behavior != permission.Deny {
			t.Fatal("deny ignored")
		}
		if decision.Behavior == permission.Allow {
			result, e := tool.Call(ctx, input, nil)
			if e != nil || result.IsError {
				t.Fatalf("%+v %v", result, e)
			}
		}
		want := 0
		if !deny {
			want = 1
		}
		var count int
		if e := pg.QueryRow(`SELECT COUNT(*) FROM llm_usage WHERE task_id=$1`, task).Scan(&count); e != nil {
			t.Fatal(e)
		}
		if count != want || int(calls.Load()) != want {
			t.Fatalf("denied=%v requests=%d rows=%d", deny, calls.Load(), count)
		}
	}
	strict = true
	result, e := tool.Call(ctx, input, nil)
	if e != nil || !result.IsError || !strings.Contains(result.Flatten(), "local_llm_only") || calls.Load() != 1 {
		t.Fatalf("local policy: %+v %v calls=%d", result, e, calls.Load())
	}
	var count int
	pg.QueryRow(`SELECT COUNT(*) FROM llm_usage WHERE task_id=$1`, task).Scan(&count)
	if count != 1 {
		t.Fatalf("blocked call metered: %d", count)
	}
}

func TestDeepSeekSearchNormaContract(t *testing.T) {
	for _, limit := range []int{0, 1, 11, 99} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			var requests []map[string]any
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != "fixture" || r.Header.Get("anthropic-version") != "2023-06-01" || r.Header.Get("Accept") != "text/event-stream" {
					t.Errorf("request contract: %+v", r)
				}
				var body map[string]any
				if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
					t.Error(e)
				}
				requests = append(requests, body)
				content := []map[string]any{}
				for i := range 25 {
					content = append(content, map[string]any{"url": fmt.Sprintf("https://example.com/%d", i), "title": "Fixture"})
				}
				frame, _ := json.Marshal(map[string]any{"type": "content_block_start", "content_block": map[string]any{"type": "web_search_tool_result", "content": content}})
				fmt.Fprintf(w, "data: %s\n\n%sdata: {\"type\":\"message_stop\"}\n\n", frame, deepSeekUsageFrame)
			}))
			defer srv.Close()
			cfg := actool.WebSearchConfig{Backend: "deepseek", DeepSeekBaseURL: srv.URL + "/v1", DeepSeekAPIKey: "fixture", DeepSeekModel: "fixture-model", InsecureTLS: true}
			original, e := actool.NewWebSearch(cfg)
			if e != nil {
				t.Fatal(e)
			}
			replacement, e := NewDeepSeekWebSearch(cfg, DeepSeekSearchMeter{})
			if e != nil {
				t.Fatal(e)
			}
			input, _ := json.Marshal(map[string]any{"query": " fixture ", "limit": limit})
			old, e := original.Call(t.Context(), input, nil)
			if e != nil {
				t.Fatal(e)
			}
			got, e := replacement.Call(t.Context(), input, nil)
			if e != nil {
				t.Fatal(e)
			}
			if got.Flatten() != old.Flatten() || got.IsError != old.IsError {
				t.Fatalf("result differs from Norma: %s\nwant %s", got.Flatten(), old.Flatten())
			}
			if len(requests) != 2 || !reflect.DeepEqual(requests[0], requests[1]) {
				t.Fatalf("request differs: %+v", requests)
			}
			for _, invalid := range []string{`{"query":" "}`, `{`} {
				a, ae := original.Call(t.Context(), json.RawMessage(invalid), nil)
				b, be := replacement.Call(t.Context(), json.RawMessage(invalid), nil)
				if (ae != nil) != (be != nil) || a.Flatten() != b.Flatten() || a.IsError != b.IsError {
					t.Fatalf("validation changed for %s", invalid)
				}
			}
		})
	}
}
