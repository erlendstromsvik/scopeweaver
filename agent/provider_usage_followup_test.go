package agent

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/internal/dbtest"
	"github.com/Autumn-27/artex/llmrec"
	"github.com/Autumn-27/artex/sidequestion"
	"github.com/Autumn-27/norma/agentcore"
	"github.com/Autumn-27/norma/llm"
	"github.com/Autumn-27/norma/transcript"
	"github.com/google/uuid"
)

// S16: every HTTP attempt, including an HTTP-200 empty retry, resets extras.
func TestFollowupUsageLastAttempt(t *testing.T) {
	pg, err := db.Open(dbtest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	if err := pg.EnsureLLMUsageTable(); err != nil {
		t.Fatal(err)
	}
	for _, stream := range []bool{false, true} {
		for _, mode := range []string{"status-retry", "empty-retry", "all-fail", "partial-error"} {
			t.Run(fmt.Sprintf("%s/stream=%t", mode, stream), func(t *testing.T) {
				attempts := 0
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					attempts++
					usage := `{"prompt_tokens":100,"completion_tokens":20}`
					if attempts == 1 || mode == "all-fail" || mode == "partial-error" {
						usage = `{"prompt_tokens":100,"completion_tokens":20,"prompt_cache_hit_tokens":60,"prompt_tokens_details":{"cache_write_tokens":7},"completion_tokens_details":{"reasoning_tokens":5}}`
					}
					if mode == "all-fail" && attempts > 1 {
						usage = `{"prompt_tokens":100,"completion_tokens":20,"cached_tokens":8,"prompt_tokens_details":{"cache_write_tokens":2},"completion_tokens_details":{"reasoning_tokens":1}}`
					}
					if mode == "all-fail" || (mode == "status-retry" && attempts == 1) {
						w.WriteHeader(503)
						fmt.Fprintf(w, `{"error":{"message":"retry"},"usage":%s}`, usage)
						return
					}
					if !stream {
						w.Header().Set("Content-Type", "application/json")
						if mode == "empty-retry" && attempts == 1 {
							fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":""},"finish_reason":"stop"}],"usage":%s}`, usage)
						} else {
							fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":"answer"},"finish_reason":"stop"}],"usage":%s}`, usage)
						}
						return
					}
					w.Header().Set("Content-Type", "text/event-stream")
					if mode == "partial-error" {
						w.Header().Set("Content-Length", "999999")
					}
					if mode != "empty-retry" || attempts > 1 {
						fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"answer\"}}]}\n\n")
					}
					fmt.Fprintf(w, "data: {\"choices\":[],\"usage\":%s}\n\n", usage)
					if mode == "partial-error" {
						return
					} else {
						fmt.Fprint(w, "data: [DONE]\n\n")
					}
				}))
				defer srv.Close()
				cfg := ConfigFrom("openai", "fixture", srv.URL, "", "")
				cfg.Retry = RetryConfig{ConnectAttempts: 1, ConnectInterval: time.Millisecond, EmptyAttempts: 1, EmptyInterval: time.Millisecond}
				p, err := cfg.NewProvider()
				if err != nil {
					t.Fatal(err)
				}
				profile := uuid.NewString()
				defer pg.Exec(`DELETE FROM llm_usage WHERE profile_name=$1`, profile)
				rec := llmrec.Wrap(p, pg, "fixture", profile, "", "", func() bool { return false })
				if stream {
					for _, e := range rec.Stream(t.Context(), llm.CompletionRequest{}) {
						err = e
					}
				} else {
					_, _, _, err = rec.Complete(t.Context(), llm.CompletionRequest{})
				}
				wantErr := mode == "all-fail" || (mode == "partial-error" && stream)
				if (err != nil) != wantErr {
					t.Fatalf("err=%v want error=%t", err, wantErr)
				}
				wantRead, wantWrite, wantReason := 0, 0, 0
				if mode == "all-fail" {
					wantRead, wantWrite, wantReason = 8, 2, 1
				}
				if mode == "partial-error" {
					wantRead, wantWrite, wantReason = 60, 7, 5
				}
				var input, output, read, write, reason int
				var status string
				if err := pg.QueryRow(`SELECT input_tokens,output_tokens,cache_read,cache_write,reasoning_tokens,status FROM llm_usage WHERE profile_name=$1`, profile).Scan(&input, &output, &read, &write, &reason, &status); err != nil {
					t.Fatal(err)
				}
				if mode == "partial-error" && (input != 100 || output != 20) {
					t.Errorf("partial totals=%d/%d want 100/20", input, output)
				}
				if read != wantRead || write != wantWrite || reason != wantReason || (status == "error") != wantErr {
					t.Errorf("last usage=%d/%d/%d %s want %d/%d/%d error=%t", read, write, reason, status, wantRead, wantWrite, wantReason, wantErr)
				}
				if mode != "partial-error" && attempts != 2 {
					t.Errorf("attempts=%d want 2", attempts)
				}
			})
		}
	}
}

// S8/S18: the production Recorder is the server's provider boundary for both
// the real harness/transcript and side questions; each call has one ledger row.
func TestFollowupHarnessAndSideUsage(t *testing.T) {
	pg, err := db.Open(dbtest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	if err := pg.EnsureLLMUsageTable(); err != nil {
		t.Fatal(err)
	}
	p := cloudProvider(t, "anthropic", func(w http.ResponseWriter, r *http.Request) {
		followupAnthropic(w, true, []map[string]any{{"type": "text", "text": "answer"}})
	})
	profile := uuid.NewString()
	defer pg.Exec(`DELETE FROM llm_usage WHERE profile_name=$1`, profile)
	rec := llmrec.Wrap(p, pg, "fixture", profile, "", "", func() bool { return false })
	store := transcript.NewStore(t.TempDir())
	session := agentcore.NewSession(agentcore.Options{Provider: rec, MaxTurns: 1, DisableBackgroundTasks: true, Transcript: store, SessionID: "followup-usage"})
	defer session.Close()
	for range 2 {
		for _, err := range session.Prompt(t.Context(), "answer") {
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if session.Usage().OutputTokens != 20 {
		t.Errorf("session per-prompt output=%d want 20", session.Usage().OutputTokens)
	}
	records, err := store.Load(store.MainPath("followup-usage"))
	if err != nil {
		t.Fatal(err)
	}
	_, usage := transcript.Messages(records)
	if usage.OutputTokens != 40 {
		t.Errorf("transcript output=%d want 40", usage.OutputTokens)
	}
	answer, err := (sidequestion.SideQuestionService{Provider: rec}).Answer(t.Context(), llm.CompletionRequest{}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if answer.Usage.OutputTokens != 20 {
		t.Errorf("side question output=%d want 20", answer.Usage.OutputTokens)
	}
	var count, output int
	if err := pg.QueryRow(`SELECT count(*),sum(output_tokens) FROM llm_usage WHERE profile_name=$1`, profile).Scan(&count, &output); err != nil {
		t.Fatal(err)
	}
	if count != 3 || output != 60 {
		t.Errorf("ledger calls=%d output=%d want 3/60", count, output)
	}
}
