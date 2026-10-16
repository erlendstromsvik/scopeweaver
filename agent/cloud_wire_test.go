package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/internal/dbtest"
	"github.com/Autumn-27/artex/locale"
	"github.com/Autumn-27/norma/agentcore"
	"github.com/Autumn-27/norma/compaction"
	"github.com/Autumn-27/norma/llm"
	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
)

func cloudReply(format string, tool bool) string {
	switch format {
	case "anthropic":
		if tool {
			return `{"role":"assistant","content":[{"type":"tool_use","id":"call_probe","name":"probe","input":{}}],"stop_reason":"tool_use","usage":{"input_tokens":1200,"output_tokens":20}}`
		}
		return `{"role":"assistant","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn","usage":{"input_tokens":1200,"output_tokens":20}}`
	case "openai-responses":
		if tool {
			return `{"status":"completed","output":[{"type":"function_call","id":"fc_probe","call_id":"call_probe","name":"probe","arguments":"{}"}],"usage":{"input_tokens":1200,"output_tokens":20}}`
		}
		return `{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}],"usage":{"input_tokens":1200,"output_tokens":20}}`
	case "ollama":
		return `{"done":true,"message":{"role":"assistant","content":"done"}}`
	default:
		if tool {
			return `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call_probe","type":"function","function":{"name":"probe","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":1200,"completion_tokens":20}}`
		}
		return `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1200,"completion_tokens":20}}`
	}
}

func cloudProvider(t *testing.T, format string, handler http.HandlerFunc) llm.Provider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	cfg := ConfigFrom(format, "fixture", srv.URL, "", "")
	cfg.Retry = RetryConfig{ConnectAttempts: -1, EmptyAttempts: -1}
	p, err := cfg.NewProvider()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// Send the exact request assembled by each production caller through both the
// pre-fix boundary and the current boundary, using the same real provider.
type cloudBoundaryComparison struct {
	llm.Provider
	legacyZero bool
	request    llm.CompletionRequest
}

func (p *cloudBoundaryComparison) Complete(ctx context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	p.request = req
	old := req
	if p.legacyZero {
		old.DynamicBoundary = 0
	}
	if _, _, _, err := p.Provider.Complete(ctx, old); err != nil {
		return llm.Message{}, "", llm.Usage{}, err
	}
	return p.Provider.Complete(ctx, req)
}

func TestDeferredSystemCallerWireMatrix(t *testing.T) {
	pg, err := db.Open(dbtest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	expID, err := pg.CreateExploration("cache boundary fixture", "no external calls")
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Exec(`DELETE FROM explorations WHERE id=$1`, expID)
	ts := pg.Exploration(expID)
	intentID, err := ts.AddNode(db.KindIntent, map[string]any{"summary": "Return done; do not call tools."}, 0, "running", "planner", nil)
	if err != nil {
		t.Fatal(err)
	}
	original := ToolAugment
	defer func() { ToolAugment = original }()
	for _, format := range []string{"anthropic", "openai", "openai-responses", "ollama"} {
		for _, caller := range []string{"planner", "worker", "mainagent", "chat"} {
			for _, global := range []bool{false, true} {
				for _, noa := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/global=%t/noa=%t", format, caller, global, noa), func(t *testing.T) {
						var bodies [][]byte
						inner := cloudProvider(t, format, func(w http.ResponseWriter, r *http.Request) {
							raw, err := io.ReadAll(r.Body)
							if err != nil {
								t.Error(err)
							}
							bodies = append(bodies, raw)
							w.Header().Set("Content-Type", "application/json")
							fmt.Fprint(w, cloudReply(format, false))
						})
						p := &cloudBoundaryComparison{Provider: inner, legacyZero: !global && (!noa || format == "ollama")}
						ToolAugment = func(context.Context, string) ([]actool.CoreTool, DeferredInfo, func()) {
							def := DeferredInfo{}
							if global {
								def.GlobalNames = []string{"mcp__fixture__read"}
								def.Deferred = def.GlobalNames
							}
							return nil, def, func() {}
						}
						root := t.TempDir()
						native := func() bool { return format == "ollama" }
						enabled := func() bool { return noa }
						switch caller {
						case "planner":
							a := NewPlanner(p, "fixture", root, nil, 32768, 3)
							a.SetNonStreaming(func() bool { return true })
							a.SetNativeOllamaResolver(native)
							a.SetNoaEnabled(enabled)
							_, _, err = a.Plan(t.Context(), expID, nil, ts, "controlled fixture", nil, nil)
						case "worker":
							a := NewWorker(p, "fixture", root, nil, 32768, 3)
							a.SetNonStreaming(func() bool { return true })
							a.SetNativeOllamaResolver(native)
							a.SetNoaEnabled(enabled)
							_, _, err = a.Execute(t.Context(), "fixture-worker", expID, nil, ts, &db.Node{ID: intentID, Payload: json.RawMessage(`{"summary":"Return done"}`)}, nil, nil, nil, nil)
						case "mainagent":
							a := NewMainAgent(p, "fixture", root, nil, 32768, 3)
							a.SetNonStreaming(func() bool { return true })
							a.SetNativeOllamaResolver(native)
							a.SetNoaEnabled(enabled)
							_, err = a.Chat(t.Context(), expID, 0, nil, ts, "controlled fixture", "Return done", nil, nil, nil, nil, nil)
						case "chat":
							a := NewChatAgent(p, "fixture", root, nil, 32768)
							a.SetNonStreaming(func() bool { return true })
							a.SetNativeOllamaResolver(native)
							a.SetNoaEnabled(enabled)
							_, err = a.Chat(t.Context(), "mainagent", "conv-fixture", "Return done", 3, 0, false, nil)
						}
						if err != nil {
							t.Fatal(err)
						}
						if len(bodies) != 2 {
							t.Fatalf("requests=%d want 2", len(bodies))
						}
						hasCompress := false
						for _, tool := range p.request.Tools {
							if tool.Name == "Compress" {
								hasCompress = true
							}
						}
						if hasCompress != (noa && format != "ollama") {
							t.Fatalf("noa Compress=%t; native <=64K must skip noa", hasCompress)
						}
						if noa && format != "ollama" && p.request.DynamicBoundary != len(p.request.System) {
							t.Fatalf("noa boundary=%d want full system length %d", p.request.DynamicBoundary, len(p.request.System))
						}
						if format != "anthropic" || noa {
							if !bytes.Equal(bodies[0], bodies[1]) {
								t.Fatalf("wire changed from main:\nbefore=%s\nafter=%s", bodies[0], bodies[1])
							}
							return
						}
						var wire struct {
							System []struct {
								Text         string            `json:"text"`
								CacheControl map[string]string `json:"cache_control"`
							} `json:"system"`
						}
						if err := json.Unmarshal(bodies[1], &wire); err != nil {
							t.Fatal(err)
						}
						markers := 0
						for _, block := range wire.System {
							if block.CacheControl != nil {
								markers++
								if block.CacheControl["type"] != "ephemeral" {
									t.Fatalf("marker=%v", block.CacheControl)
								}
							}
						}
						if markers != 1 {
							t.Fatalf("system markers=%d want exactly 1; wire=%s", markers, bodies[1])
						}
						if !strings.Contains(wire.System[0].Text, p.request.System[p.request.DynamicBoundary-1]) {
							t.Fatal("cache marker does not cover the static system")
						}
					})
				}
			}
		}
	}
}

func TestCloudWorkerToolPrefixAndClaudeTemplate(t *testing.T) {
	for _, format := range []string{"anthropic", "openai", "openai-responses"} {
		t.Run(format, func(t *testing.T) {
			var bodies [][]byte
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				bodies = append(bodies, raw)
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, strings.ReplaceAll(cloudReply(format, len(bodies) <= 2), "call_probe", fmt.Sprintf("call_probe_%d", len(bodies))))
			}))
			defer srv.Close()
			cfg := ConfigFrom(format, "fixture", srv.URL, "", "")
			cfg.MaxTokens = 16384
			if format == "anthropic" {
				cfg.Model = "claude-haiku-5-5"
				cfg.ThinkingType = "disabled"
				cfg.ReasoningEffort = "low"
			}
			p, err := cfg.NewProvider()
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			system, boundary := deferredSystem(workerSystem("", "", root, root, locale.En), DeferredInfo{})
			calls := 0
			probe := actool.Build(actool.Spec{Name: "probe", Description: "Read a controlled fixture.", Schema: map[string]any{"type": "object", "properties": map[string]any{}}, Run: func(context.Context, json.RawMessage, *actool.ToolContext) (actool.Result, error) {
				calls++
				return actool.Text("fixture result"), nil
			}})
			session := agentcore.NewSession(agentcore.Options{Provider: p, SystemPrompt: append(system, outputLanguageInstruction(locale.En)), DynamicBoundary: boundary, Tools: append(defaultToolsExcept("MultiEdit", "Glob", "Grep"), probe), PermissionMode: permission.ModeBypass, WorkingDir: root, MaxTurns: 3, MaxTokens: cfg.MaxTokens, NonStreaming: true, DisableBackgroundTasks: true})
			defer session.Close()
			for _, err := range session.Prompt(t.Context(), "Read the controlled fixture then finish.") {
				if err != nil {
					t.Fatal(err)
				}
			}
			if len(bodies) != 3 || calls != 2 {
				t.Fatalf("model calls=%d tool calls=%d want 3/2", len(bodies), calls)
			}
			var first, second map[string]json.RawMessage
			if err := json.Unmarshal(bodies[0], &first); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(bodies[1], &second); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"system", "instructions", "tools"} {
				if !bytes.Equal(first[key], second[key]) {
					t.Fatalf("%s changed:\n%s\n%s", key, first[key], second[key])
				}
			}
			historyKey := "messages"
			if format == "openai-responses" {
				historyKey = "input"
			}
			var before, after []json.RawMessage
			if err := json.Unmarshal(first[historyKey], &before); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(second[historyKey], &after); err != nil {
				t.Fatal(err)
			}
			if len(after) <= len(before) {
				t.Fatalf("history did not grow: %s", second[historyKey])
			}
			for i := range before {
				if !bytes.Equal(before[i], after[i]) {
					t.Fatalf("history prefix changed at %d:\n%s\n%s", i, before[i], after[i])
				}
			}
			if !bytes.HasPrefix(second[historyKey], bytes.TrimSuffix(first[historyKey], []byte("]"))) {
				t.Fatalf("history byte prefix changed:\n%s\n%s", first[historyKey], second[historyKey])
			}
			static := append([]byte(nil), first["system"]...)
			static = append(static, first["instructions"]...)
			if format == "openai" {
				static = append(static, before[0]...)
			}
			static = append(static, first["tools"]...)
			estimate := compaction.EstimateTokens([]llm.Message{llm.UserText(string(static))})
			if estimate < 1024 {
				t.Fatalf("static prefix estimate=%d want >=1024", estimate)
			}
			t.Logf("static prefix estimate=%d tokens; history %d -> %d entries", estimate, len(before), len(after))
			if format == "anthropic" {
				for i, wire := range []map[string]json.RawMessage{first, second} {
					if string(wire["model"]) != `"claude-haiku-5-5"` || string(wire["max_tokens"]) != "16384" || string(wire["thinking"]) != `{"type":"disabled"}` || string(wire["output_config"]) != `{"effort":"low"}` {
						t.Fatalf("turn %d template tuple: %s", i+1, bodies[i])
					}
				}
				var history []struct {
					Role    string `json:"role"`
					Content []struct {
						Type      string `json:"type"`
						ID        string `json:"id"`
						ToolUseID string `json:"tool_use_id"`
					} `json:"content"`
				}
				if err := json.Unmarshal(second["messages"], &history); err != nil {
					t.Fatal(err)
				}
				use, result := false, false
				for _, m := range history {
					for _, b := range m.Content {
						switch b.Type {
						case "thinking", "redacted_thinking":
							t.Fatal("thinking block in disabled history")
						case "tool_use":
							use = m.Role == "assistant" && b.ID == "call_probe_1"
						case "tool_result":
							result = m.Role == "user" && b.ToolUseID == "call_probe_1"
						}
					}
				}
				if !use || !result {
					t.Fatalf("missing paired tool history: %s", second["messages"])
				}
			}
		})
	}
}
