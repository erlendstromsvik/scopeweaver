package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/internal/dbtest"
	"github.com/Autumn-27/artex/llmrec"
	"github.com/Autumn-27/artex/sidequestion"
	"github.com/Autumn-27/norma/llm"
	"github.com/google/uuid"
)

// Shapes are transcribed from the official usage contracts linked in
// refs/providers.md §3. Extra write/reasoning fields expose pinned parser gaps;
// these fixtures prove decoding, not live provider caching or billing.
func TestCloudProviderUsageLedgerFixtures(t *testing.T) {
	pg, err := db.Open(dbtest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	if err := pg.EnsureLLMUsageTable(); err != nil {
		t.Fatal(err)
	}
	if err := pg.EnsureLLMRecordsTable(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, format, model, usage string
		want                       llm.Usage
		gap                        string
	}{
		{"DeepSeek", "openai", "deepseek-flash", `{"prompt_tokens":1200,"completion_tokens":20,"prompt_cache_hit_tokens":800,"prompt_cache_miss_tokens":400,"prompt_tokens_details":{"cached_tokens":800},"completion_tokens_details":{"reasoning_tokens":5}}`, llm.Usage{InputTokens: 1200, OutputTokens: 20, CacheReadTokens: 800}, ""},
		{"Z.ai", "openai", "glm-5.3", `{"prompt_tokens":5464,"completion_tokens":159,"total_tokens":5623,"prompt_tokens_details":{"cached_tokens":1664},"completion_tokens_details":{"reasoning_tokens":14}}`, llm.Usage{InputTokens: 5464, OutputTokens: 159, CacheReadTokens: 1664}, ""},
		{"Claude", "anthropic", "claude-haiku-5-5", `{"input_tokens":1100,"output_tokens":20,"cache_read_input_tokens":60,"cache_creation_input_tokens":40,"output_tokens_details":{"thinking_tokens":5}}`, llm.Usage{InputTokens: 1200, OutputTokens: 20, CacheReadTokens: 60, CacheWriteTokens: 40}, ""},
		{"OpenAI-Chat", "openai", "gpt-6-luna", `{"prompt_tokens":1200,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":800,"cache_write_tokens":40},"completion_tokens_details":{"reasoning_tokens":5}}`, llm.Usage{InputTokens: 1200, OutputTokens: 20, CacheReadTokens: 800, CacheWriteTokens: 40}, "cache_write_tokens is not decoded by Norma v0.4.3"},
		{"OpenAI-Responses", "openai-responses", "gpt-6-luna", `{"input_tokens":1200,"output_tokens":20,"input_tokens_details":{"cached_tokens":800,"cache_write_tokens":40},"output_tokens_details":{"reasoning_tokens":5}}`, llm.Usage{InputTokens: 1200, OutputTokens: 20, CacheReadTokens: 800, CacheWriteTokens: 40}, "cache_write_tokens is not decoded by Norma v0.4.3"},
		{"Qwen", "openai", "qwen3.8-flash", `{"prompt_tokens":1200,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":800,"cache_creation_input_tokens":40,"cache_creation":{"ephemeral_5m_input_tokens":40}},"completion_tokens_details":{"reasoning_tokens":5}}`, llm.Usage{InputTokens: 1200, OutputTokens: 20, CacheReadTokens: 800, CacheWriteTokens: 40}, "explicit cache creation is not decoded by Norma v0.4.3"},
		{"Kimi", "openai", "kimi-k3", `{"prompt_tokens":1200,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":800,"cache_write_tokens":40}}`, llm.Usage{InputTokens: 1200, OutputTokens: 20, CacheReadTokens: 800, CacheWriteTokens: 40}, "cache_write_tokens is not decoded by Norma v0.4.3"},
		{"OpenRouter", "openai", "deepseek/deepseek-flash", `{"prompt_tokens":1200,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":800,"cache_write_tokens":40},"completion_tokens_details":{"reasoning_tokens":5}}`, llm.Usage{InputTokens: 1200, OutputTokens: 20, CacheReadTokens: 800, CacheWriteTokens: 40}, "cache_write_tokens is not decoded by Norma v0.4.3"},
		{"DeepSeek-alias-only", "openai", "deepseek-flash", `{"prompt_tokens":1200,"completion_tokens":20,"prompt_cache_hit_tokens":800,"prompt_cache_miss_tokens":400}`, llm.Usage{InputTokens: 1200, OutputTokens: 20, CacheReadTokens: 800}, "top-level prompt_cache_hit_tokens is not decoded by Norma v0.4.3"},
		{"Kimi-legacy-alias-only", "openai", "kimi-k3", `{"prompt_tokens":1200,"completion_tokens":20,"cached_tokens":800}`, llm.Usage{InputTokens: 1200, OutputTokens: 20, CacheReadTokens: 800}, "legacy top-level cached_tokens is not decoded by Norma v0.4.3"},
	}
	for _, tc := range cases {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", tc.name, stream), func(t *testing.T) {
				requests := 0
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests++
					var request struct {
						Stream bool `json:"stream"`
					}
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Error(err)
					}
					if request.Stream != stream {
						t.Errorf("wire streaming=%t want %t", request.Stream, stream)
					}
					if !stream {
						w.Header().Set("Content-Type", "application/json")
						var body map[string]any
						if err := json.Unmarshal([]byte(cloudReply(tc.format, false)), &body); err != nil {
							t.Error(err)
						}
						body["usage"] = json.RawMessage(tc.usage)
						if err := json.NewEncoder(w).Encode(body); err != nil {
							t.Error(err)
						}
						return
					}
					w.Header().Set("Content-Type", "text/event-stream")
					switch tc.format {
					case "anthropic":
						var start map[string]any
						if err := json.Unmarshal([]byte(tc.usage), &start); err != nil {
							t.Error(err)
						}
						start["output_tokens"] = 1
						usage, _ := json.Marshal(start)
						fmt.Fprintf(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"role\":\"assistant\",\"content\":[],\"usage\":%s}}\n\n", usage)
						fmt.Fprint(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"done\"}}\n\n")
						fmt.Fprint(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{},\"usage\":{\"output_tokens\":10}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":20,\"output_tokens_details\":{\"thinking_tokens\":5}}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
					case "openai-responses":
						fmt.Fprint(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"done\"}\n\n")
						fmt.Fprintf(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":%s}}\n\n", tc.usage)
					default:
						fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\n")
						fmt.Fprintf(w, "data: {\"choices\":[],\"usage\":%s}\n\ndata: [DONE]\n\n", tc.usage)
					}
				}))
				defer srv.Close()
				cfg := ConfigFrom(tc.format, tc.model, srv.URL, "", "")
				if tc.format == "anthropic" {
					cfg.ThinkingType = "disabled"
				}
				cfg.Retry = RetryConfig{ConnectAttempts: -1, EmptyAttempts: -1}
				p, err := cfg.NewProvider()
				if err != nil {
					t.Fatal(err)
				}
				profile := "cloud-fixture-" + uuid.NewString()
				defer pg.Exec(`DELETE FROM llm_usage WHERE profile_name=$1`, profile)
				defer pg.Exec(`DELETE FROM llm_records WHERE profile_name=$1`, profile)
				recorder := llmrec.Wrap(p, pg, tc.model, profile, cfg.ThinkingType, cfg.ReasoningEffort, nil)
				req := llm.CompletionRequest{System: []string{"controlled fixture"}, Messages: []llm.Message{llm.UserText("answer")}}
				for range 2 {
					if stream {
						text := ""
						for ev, err := range recorder.Stream(t.Context(), req) {
							if err != nil {
								t.Fatal(err)
							}
							if ev.Type == llm.SETextDelta {
								text += ev.Text
							}
						}
						if text != "done" {
							t.Fatalf("stream text=%q", text)
						}
					} else {
						msg, _, usage, err := recorder.Complete(t.Context(), req)
						if err != nil {
							t.Fatal(err)
						}
						if msg.Text() != "done" || usage != tc.want {
							t.Errorf("atomic text=%q usage=%+v want %+v", msg.Text(), usage, tc.want)
						}
					}
				}
				rows, err := pg.Query(`SELECT input_tokens,output_tokens,cache_read,cache_write,reasoning_tokens,status FROM llm_usage WHERE profile_name=$1 ORDER BY id`, profile)
				if err != nil {
					t.Fatal(err)
				}
				defer rows.Close()
				count := 0
				for rows.Next() {
					count++
					var usage llm.Usage
					var status string
					var reasoning int
					if err := rows.Scan(&usage.InputTokens, &usage.OutputTokens, &usage.CacheReadTokens, &usage.CacheWriteTokens, &reasoning, &status); err != nil {
						t.Fatal(err)
					}
					var rawUsage map[string]any
					json.Unmarshal([]byte(tc.usage), &rawUsage)
					wantReasoning := 0
					for _, key := range []string{"completion_tokens_details", "output_tokens_details"} {
						if details, ok := rawUsage[key].(map[string]any); ok {
							for _, tokenKey := range []string{"reasoning_tokens", "thinking_tokens"} {
								if n, ok := details[tokenKey].(float64); ok {
									wantReasoning = int(n)
								}
							}
						}
					}
					if reasoning != wantReasoning {
						t.Errorf("reasoning=%d want %d", reasoning, wantReasoning)
					}
					if usage != tc.want || usage.CacheReadTokens > usage.InputTokens || status != "ok" {
						t.Errorf("ledger usage=%+v status=%s want %+v", usage, status, tc.want)
					}
				}
				if err := rows.Err(); err != nil {
					t.Fatal(err)
				}
				if tc.format == "anthropic" && stream {
					answer, err := (sidequestion.SideQuestionService{Provider: llmrec.Wrap(p, nil, tc.model, "", "", "", func() bool { return false })}).Answer(t.Context(), req, true, nil)
					if err != nil {
						t.Fatal(err)
					}
					if answer.Usage != tc.want {
						t.Errorf("side usage=%+v want %+v", answer.Usage, tc.want)
					}
					requests-- // Exclude the comparison call, which has no recorder.
				}
				if count != 2 || requests != 2 {
					t.Fatalf("ledger rows=%d HTTP requests=%d want 2/2", count, requests)
				}
				if tc.gap != "" {
					t.Logf("Regression covered: %s", tc.gap)
				}
			})
		}
	}
}
