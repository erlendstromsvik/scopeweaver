package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Autumn-27/norma/llm"
)

// The cloud economy templates (web/src/lib/llm-profile-presets.ts) must reach the
// wire as the fields each provider documents: effort, thinking and the output-limit
// field. Agent turns pass the template's max_tokens 16384 as the request limit.
func TestCloudEconomyTemplateWireFields(t *testing.T) {
	cases := []struct {
		id, format, model, thinking, effort, field string
		want                                       map[string]any
		absent                                     []string
	}{
		{id: "deepseek", format: "openai", model: "deepseek-flash", effort: "low",
			want: map[string]any{"reasoning_effort": "low", "max_tokens": 16384.0}, absent: []string{"max_completion_tokens", "thinking"}},
		{id: "claude", format: "anthropic", model: "claude-haiku-5-5", thinking: "disabled", effort: "low",
			want: map[string]any{"thinking.type": "disabled", "output_config.effort": "low", "max_tokens": 16384.0}},
		{id: "openai", format: "openai-responses", model: "gpt-6-luna", effort: "low",
			want: map[string]any{"reasoning.effort": "low", "max_output_tokens": 16384.0}, absent: []string{"max_tokens"}},
		{id: "qwen", format: "openai", model: "qwen3.8-flash", effort: "low", field: llm.MaxTokensFieldCompletion,
			want: map[string]any{"reasoning_effort": "low", "max_completion_tokens": 16384.0}, absent: []string{"max_tokens", "thinking"}},
		{id: "kimi", format: "openai", model: "kimi-k3", effort: "low", field: llm.MaxTokensFieldCompletion,
			want: map[string]any{"reasoning_effort": "low", "max_completion_tokens": 16384.0}, absent: []string{"max_tokens", "thinking"}},
		{id: "openrouter", format: "openai", model: "deepseek/deepseek-v4.1-flash", effort: "low",
			want: map[string]any{"reasoning_effort": "low", "max_tokens": 16384.0}, absent: []string{"max_completion_tokens", "thinking"}},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			var body map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, cloudReply(tc.format, false))
			}))
			defer srv.Close()
			cfg := ConfigFrom(tc.format, tc.model, srv.URL, "fixture-key", "")
			cfg.ThinkingType, cfg.ReasoningEffort, cfg.MaxTokensField = tc.thinking, tc.effort, tc.field
			cfg.Retry = RetryConfig{ConnectAttempts: -1, EmptyAttempts: -1}
			p, err := cfg.NewProvider()
			if err != nil {
				t.Fatal(err)
			}
			req := llm.CompletionRequest{System: []string{"fixture"}, Messages: []llm.Message{llm.UserText("answer")}, MaxTokens: 16384}
			if _, _, _, err := p.Complete(t.Context(), req); err != nil {
				t.Fatal(err)
			}
			if body["model"] != tc.model {
				t.Fatalf("model=%v want %s", body["model"], tc.model)
			}
			for path, want := range tc.want {
				if got := wireField(body, path); got != want {
					t.Errorf("%s=%v want %v; body=%v", path, got, want, body)
				}
			}
			for _, key := range tc.absent {
				if _, ok := body[key]; ok {
					t.Errorf("%s must be absent; body=%v", key, body)
				}
			}
		})
	}
}

// wireField reads a dotted path such as "thinking.type" from a decoded JSON body.
func wireField(body map[string]any, path string) any {
	var cur any = body
	start := 0
	for i := 0; i <= len(path); i++ {
		if i < len(path) && path[i] != '.' {
			continue
		}
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[path[start:i]]
		start = i + 1
	}
	return cur
}
