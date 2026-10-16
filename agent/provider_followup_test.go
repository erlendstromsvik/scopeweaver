package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/llmrec"
	"github.com/Autumn-27/artex/sidequestion"
	"github.com/Autumn-27/norma/compaction"
	"github.com/Autumn-27/norma/llm"
)

// The proxy terminates HTTP locally while preserving the configured official host.
func followupClaude(t *testing.T, model, effort string, official bool, handler http.HandlerFunc) llm.Provider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	cfg := ConfigFrom("anthropic", model, srv.URL, "fixture-key", "")
	if official {
		cfg.BaseURL = "http://api.anthropic.com"
		cfg.Proxy = srv.URL
	}
	cfg.ReasoningEffort = effort
	cfg.Retry = RetryConfig{ConnectAttempts: -1, EmptyAttempts: -1}
	p, err := cfg.NewProvider()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func followupAnthropic(w http.ResponseWriter, stream bool, blocks []map[string]any) {
	if !stream {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"role": "assistant", "content": blocks, "stop_reason": "end_turn", "usage": map[string]int{"input_tokens": 100, "output_tokens": 20}})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprint(w, "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":100,\"output_tokens\":1}}}\n\n")
	for i, b := range blocks {
		start := b
		if b["type"] == "thinking" {
			start = map[string]any{"type": "thinking", "thinking": "", "signature": ""}
		}
		raw, _ := json.Marshal(start)
		fmt.Fprintf(w, "data: {\"type\":\"content_block_start\",\"index\":%d,\"content_block\":%s}\n\n", i, raw)
		if b["type"] == "thinking" {
			fmt.Fprintf(w, "data: {\"type\":\"content_block_delta\",\"index\":%d,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":%q}}\n\ndata: {\"type\":\"content_block_delta\",\"index\":%d,\"delta\":{\"type\":\"signature_delta\",\"signature\":%q}}\n\n", i, b["thinking"], i, b["signature"])
		}
		if b["type"] == "text" {
			fmt.Fprintf(w, "data: {\"type\":\"content_block_delta\",\"index\":%d,\"delta\":{\"type\":\"text_delta\",\"text\":%q}}\n\n", i, b["text"])
		}
	}
	fmt.Fprint(w, "data: {\"type\":\"message_delta\",\"delta\":{},\"usage\":{\"output_tokens\":10}}\n\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":20}}\n\ndata: {\"type\":\"message_stop\"}\n\n")
}

// S1/S15: replay survives the real Norma Accumulator, including empty signatures.
func TestFollowupClaudeReplay(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, kind := range []string{"summarized", "empty", "redacted"} {
			t.Run(fmt.Sprintf("%s/stream=%t", kind, stream), func(t *testing.T) {
				think := map[string]any{"type": "thinking", "thinking": "summary", "signature": "exact-signature"}
				if kind == "empty" {
					think["thinking"] = ""
				}
				if kind == "redacted" {
					think = map[string]any{"type": "redacted_thinking", "data": "opaque-data"}
				}
				var bodies []map[string]any
				p := followupClaude(t, "claude-opus-5-5", "low", true, func(w http.ResponseWriter, r *http.Request) {
					var body map[string]any
					json.NewDecoder(r.Body).Decode(&body)
					bodies = append(bodies, body)
					followupAnthropic(w, body["stream"] == true, []map[string]any{think, {"type": "tool_use", "id": "tool-1", "name": "probe", "input": map[string]any{}}})
				})
				req := llm.CompletionRequest{System: []string{"system"}, Messages: []llm.Message{llm.UserText("go")}}
				var msg llm.Message
				if stream {
					a := llm.NewAccumulator()
					for ev, err := range p.Stream(t.Context(), req) {
						if err != nil {
							t.Fatal(err)
						}
						a.Add(ev)
					}
					msg = a.Message()
				} else {
					var err error
					msg, _, _, err = p.Complete(t.Context(), req)
					if err != nil {
						t.Fatal(err)
					}
				}
				req.Messages = append(req.Messages, msg, llm.Message{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultText("tool-1", "ok", false)}})
				if _, _, _, err := p.Complete(t.Context(), req); err != nil {
					t.Fatal(err)
				}
				for i, body := range bodies {
					thinking, _ := body["thinking"].(map[string]any)
					if thinking["type"] != "adaptive" || thinking["display"] != "summarized" {
						t.Errorf("turn %d thinking=%v", i, thinking)
					}
				}
				messages := bodies[1]["messages"].([]any)
				var content []any
				for _, m := range messages {
					m := m.(map[string]any)
					if m["role"] == "assistant" {
						content = m["content"].([]any)
					}
				}
				if len(content) != 2 {
					t.Fatalf("replayed content=%v", content)
				}
				got, _ := json.Marshal(content[0])
				want, _ := json.Marshal(think)
				if string(got) != string(want) {
					t.Errorf("replay=%s want %s", got, want)
				}
			})
		}
	}
}

// S3/S7/S10: real graph/conversation callers plus judge's exact request contract.
func TestFollowupAnthropicRequests(t *testing.T) {
	for _, tc := range []struct {
		model, effort      string
		official, disabled bool
	}{
		{"claude-sonnet-5-5", "low", true, false}, {"claude-opus-5-5", "max", true, false}, {"claude-haiku-5-5", "xhigh", true, false}, {"claude-haiku-5-5", "max", true, false}, {"claude-opus-5-5", "low", true, false}, {"claude-sonnet-5-5", "max", true, false}, {"claude-haiku-5-5", "high", true, true}, {"claude-sonnet-5-5", "low", false, true}, {"claude-sonnet-4-6", "low", true, true},
	} {
		t.Run(fmt.Sprintf("%s/%s/official=%t", tc.model, tc.effort, tc.official), func(t *testing.T) {
			var bodies []map[string]any
			p := followupClaude(t, tc.model, tc.effort, tc.official, func(w http.ResponseWriter, r *http.Request) {
				var b map[string]any
				json.NewDecoder(r.Body).Decode(&b)
				bodies = append(bodies, b)
				followupAnthropic(w, b["stream"] == true, []map[string]any{{"type": "text", "text": "done"}})
			})
			if _, err := compaction.ProviderSummarizer(p, 1024, true)(t.Context(), []llm.Message{llm.UserText("history")}); err != nil {
				t.Fatal(err)
			}
			if _, err := NewCompactor(p, tc.model).compress(t.Context(), &coldGraph{}, block{}, map[int64]*db.Node{}); err != nil {
				t.Fatal(err)
			}
			temp := 0.0
			req := llm.CompletionRequest{System: []string{"judge"}, Messages: []llm.Message{llm.UserText("review")}, Thinking: "disabled", Temperature: &temp}
			for _, err := range p.Stream(t.Context(), req) {
				if err != nil {
					t.Fatal(err)
				}
			}
			snap := sidequestion.Snapshot{Request: llm.CompletionRequest{System: []string{"side question"}, Messages: []llm.Message{llm.UserText("history")}}, Model: sidequestion.Model{WindowTokens: 200000}}
			replay := sidequestion.Replay{Load: func(_ context.Context, after int64) ([]sidequestion.Exchange, error) {
				var exchanges []sidequestion.Exchange
				for i := after + 1; i <= 21; i++ {
					exchanges = append(exchanges, sidequestion.Exchange{Ordinal: i, Status: "completed", Question: "past", Answer: "past answer"})
				}
				return exchanges, nil
			}}
			if _, _, err := (sidequestion.SideQuestionService{Provider: p}).Respond(t.Context(), snap, "question", replay, sidequestion.ContextOptions{}, nil); err != nil {
				t.Fatal(err)
			}

			for i, b := range bodies {
				thinking, _ := b["thinking"].(map[string]any)
				if !tc.official && (thinking["display"] != nil || (i >= 4 && len(thinking) > 0)) {
					t.Errorf("third-party default/display changed: %v", thinking)
				}
				if i < 4 && (thinking["type"] == "disabled") != tc.disabled {
					t.Errorf("caller %d thinking=%v", i, thinking)
				}
				system := b["system"].([]any)
				if system[0].(map[string]any)["cache_control"] == nil {
					t.Errorf("caller %d missing cache marker", i)
				}
			}
			_, hasTemp := bodies[2]["temperature"]
			if hasTemp != (tc.disabled && (!tc.official || tc.model == "claude-sonnet-4-6")) {
				t.Errorf("judge temperature present=%t", hasTemp)
			}
		})
	}
}

// S6: completion is a wire event, never inferred from clean EOF or incomplete.
func TestFollowupResponsesCompletion(t *testing.T) {
	for _, end := range []string{"response.completed", "response.incomplete", "eof", "http-error"} {
		t.Run(end, func(t *testing.T) {
			p := cloudProvider(t, "openai-responses", func(w http.ResponseWriter, r *http.Request) {
				if end == "http-error" {
					w.WriteHeader(400)
					fmt.Fprint(w, `{"error":{"message":"bad"}}`)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"answer\"}\n\n")
				if end != "eof" {
					fmt.Fprintf(w, "event: %s\ndata: {\"type\":%q,\"response\":{\"usage\":{\"input_tokens\":100,\"output_tokens\":20}}}\n\n", end, end)
				}
			})
			answer, err := (sidequestion.SideQuestionService{Provider: llmrec.Wrap(p, nil, "fixture", "", "", "", func() bool { return false })}).Answer(t.Context(), llm.CompletionRequest{}, true, nil)
			if (err == nil) != (end == "response.completed") {
				t.Fatalf("answer=%+v err=%v", answer, err)
			}
		})
	}
}

// S1/S15: a partially retained sequence must keep original thinking order.
func TestFollowupClaudePartialReplayOrder(t *testing.T) {
	blocks := []map[string]any{{"type": "thinking", "thinking": "", "signature": "sig-a"}, {"type": "redacted_thinking", "data": "data-b"}, {"type": "thinking", "thinking": "kept", "signature": "sig-c"}, {"type": "tool_use", "id": "multi-1", "name": "probe", "input": map[string]any{}}}
	var bodies []map[string]any
	p := followupClaude(t, "claude-sonnet-5-5", "high", true, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		followupAnthropic(w, body["stream"] == true, blocks)
	})
	req := llm.CompletionRequest{Messages: []llm.Message{llm.UserText("go")}}
	a := llm.NewAccumulator()
	for ev, err := range p.Stream(t.Context(), req) {
		if err != nil {
			t.Fatal(err)
		}
		a.Add(ev)
	}
	req.Messages = append(req.Messages, a.Message(), llm.Message{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultText("multi-1", "ok", false)}})
	if _, _, _, err := p.Complete(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	msgs := bodies[1]["messages"].([]any)
	content := msgs[1].(map[string]any)["content"]
	got, _ := json.Marshal(content)
	want, _ := json.Marshal(blocks)
	if string(got) != string(want) {
		t.Errorf("content=%s want %s", got, want)
	}
}

// S7: goals and the Recorder-free probe use the same boundary repair, in both modes.
func TestFollowupBoundaryCallers(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			var bodies []map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var b map[string]any
				json.NewDecoder(r.Body).Decode(&b)
				bodies = append(bodies, b)
				followupAnthropic(w, b["stream"] == true, []map[string]any{{"type": "text", "text": "OK"}})
			}))
			defer srv.Close()
			cfg := ConfigFrom("anthropic", "fixture", srv.URL, "", "")
			cfg.Stream = stream
			p, err := cfg.NewProvider()
			if err != nil {
				t.Fatal(err)
			}
			DecomposeGoalsWithProvider(t.Context(), p, t.TempDir(), "controlled goal", "", nil, nil, 0, !stream, 1024, nil)
			if _, text, err := TestConnection(t.Context(), cfg); err != nil || text != "OK" {
				t.Fatalf("probe=%q err=%v", text, err)
			}
			if len(bodies) != 2 {
				t.Fatalf("requests=%d want 2", len(bodies))
			}
			for i, b := range bodies {
				system := b["system"].([]any)
				if len(system) != 1 || system[0].(map[string]any)["cache_control"] == nil {
					t.Errorf("caller %d system=%v", i, system)
				}
			}
			if _, _, _, err := p.Complete(t.Context(), llm.CompletionRequest{System: []string{"static", "dynamic"}, DynamicBoundary: 1, Messages: []llm.Message{llm.UserText("go")}}); err != nil {
				t.Fatal(err)
			}
			system := bodies[2]["system"].([]any)
			if len(system) != 2 || system[0].(map[string]any)["cache_control"] == nil || system[1].(map[string]any)["cache_control"] != nil {
				t.Fatalf("explicit boundary changed: %v", system)
			}
		})
	}
}

// S3/S10: Haiku's accepted disabled override retains Norma's history filtering.
func TestFollowupClaudeAcceptedDisabledDoesNotReplay(t *testing.T) {
	var bodies []map[string]any
	p := followupClaude(t, "claude-haiku-5-5", "high", true, func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		bodies = append(bodies, b)
		followupAnthropic(w, false, []map[string]any{{"type": "thinking", "thinking": "reason", "signature": "sig"}, {"type": "tool_use", "id": "disabled-tool", "name": "probe", "input": map[string]any{}}})
	})
	req := llm.CompletionRequest{Messages: []llm.Message{llm.UserText("go")}}
	msg, _, _, err := p.Complete(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	req.Thinking = "disabled"
	req.Messages = append(req.Messages, msg, llm.Message{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultText("disabled-tool", "ok", false)}})
	if _, _, _, err = p.Complete(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(bodies[1]["messages"])
	if strings.Contains(string(raw), `"type":"thinking"`) {
		t.Fatalf("disabled replay contains thinking: %s", raw)
	}
}

func TestClaudeReplayBoundAndIsolation(t *testing.T) {
	first, second := &claudeReplay{}, &claudeReplay{}
	for i := range 129 {
		first.remember(fmt.Sprint(i), []json.RawMessage{json.RawMessage(`{"type":"thinking","thinking":"","signature":"sig"}`)})
	}
	if len(first.blocks) != 128 || first.blocks["0"] != nil || len(second.blocks) != 0 {
		t.Fatalf("replay bounds/isolation: %d/%d", len(first.blocks), len(second.blocks))
	}
	first.remember("oversized", []json.RawMessage{json.RawMessage(strings.Repeat("x", 4*1024*1024+1))})
	if len(first.blocks) != 128 || first.bytes > 4*1024*1024 {
		t.Fatalf("replay byte bound: entries=%d bytes=%d", len(first.blocks), first.bytes)
	}
}

// Unknown formats retain Norma's explicit rejection contract.
func TestFollowupEmptyFormatStillRejected(t *testing.T) {
	_, err := (Config{Model: "fixture", APIKey: "fixture-key"}).NewProvider()
	if err == nil || !strings.Contains(err.Error(), "unknown format") {
		t.Fatalf("empty format error=%v", err)
	}
}

// Dated snapshot ids of the same Claude 5.5 models need the same request handling.
func TestOfficialClaude55MatchesDatedSnapshots(t *testing.T) {
	for model, want := range map[string]bool{
		"claude-sonnet-5-5": true, "claude-opus-5-5-20261001": true, "claude-haiku-5-5-20260915": true,
		"claude-sonnet-4-6": false, "claude-sonnet-5-5-preview": false, "gpt-6-luna": false,
	} {
		cfg := ConfigFrom("anthropic", model, "https://api.anthropic.com", "k", "")
		if got := cfg.officialClaude55(); got != want {
			t.Errorf("%s: officialClaude55=%v want %v", model, got, want)
		}
	}
}
