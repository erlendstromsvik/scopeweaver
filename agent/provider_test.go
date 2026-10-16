package agent

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Autumn-27/norma/llm"
)

func TestClaude55ConfigDisabledThinking(t *testing.T) {
	var body map[string]any
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		followupAnthropic(w, body["stream"] == true, []map[string]any{{"type": "text", "text": "done"}})
	}))
	defer srv.Close()
	// Route even the default official URL to the local fixture, without DNS or provider calls.
	original := http.DefaultTransport
	tr := original.(*http.Transport).Clone()
	tr.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
	}
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	http.DefaultTransport = tr
	t.Cleanup(func() { http.DefaultTransport = original; tr.CloseIdleConnections() })
	for _, model := range []string{"claude-sonnet-5-5", "claude-opus-5-5", "claude-haiku-5-5"} {
		for _, suffix := range []string{"", "-20261001"} {
			for _, base := range []string{"", "https://api.anthropic.com", "https://third-party.example"} {
				for _, effort := range []string{"", "low", "medium", "high", "xhigh", "max"} {
					t.Run(fmt.Sprintf("%s%s/%s/%s", model, suffix, base, effort), func(t *testing.T) {
						cfg := ConfigFrom("anthropic", model+suffix, base, "fixture-key", "")
						cfg.ThinkingType, cfg.ReasoningEffort = "disabled", effort
						cfg.Retry = RetryConfig{ConnectAttempts: -1, EmptyAttempts: -1}
						p, err := cfg.NewProvider()
						if err != nil {
							t.Fatal(err)
						}
						want := "disabled"
						if base != "https://third-party.example" && (model != "claude-haiku-5-5" || effort == "xhigh" || effort == "max") {
							want = "adaptive"
						}
						for _, stream := range []bool{false, true} {
							req := llm.CompletionRequest{Messages: []llm.Message{llm.UserText("go")}}
							if stream {
								for _, err := range p.Stream(t.Context(), req) {
									if err != nil {
										t.Fatal(err)
									}
								}
							} else if _, _, _, err := p.Complete(t.Context(), req); err != nil {
								t.Fatal(err)
							}
							thinking, _ := body["thinking"].(map[string]any)
							if thinking["type"] != want {
								t.Errorf("stream=%t: config disabled produced thinking=%v, want %s", stream, thinking, want)
							}
						}
					})
				}
			}
		}
	}
}
