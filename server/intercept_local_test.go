package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/intercept"
	"github.com/Autumn-27/norma/llm"
)

func TestNativeJudgeDefaultsExplicitPolicyAndFailure(t *testing.T) {
	m := backportTestManager(t)
	// This suite owns an explicitly disposable database; restore settings after each case.
	keys := []string{"llm_judge_enabled", "llm_judge_profile_id", "llm_judge_timeout_seconds", "llm_judge_fail_action"}
	for _, key := range keys {
		old, found, _ := m.pg.GetSetting(key)
		if _, err := m.pg.Exec(`DELETE FROM settings WHERE key=$1`, key); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if found {
				_ = m.pg.SetSetting(key, old)
			} else {
				_, _ = m.pg.Exec(`DELETE FROM settings WHERE key=$1`, key)
			}
		})
	}
	localID, err := m.pg.SaveProfile(&db.LLMProfile{Name: "runtime-local-" + strconv.FormatInt(time.Now().UnixNano(), 10), Format: "ollama", BaseURL: "http://127.0.0.1:11434", Model: "test-model", Streaming: true})
	if err != nil {
		t.Fatal(err)
	}
	cloudID, err := m.pg.SaveProfile(&db.LLMProfile{Name: "runtime-cloud-" + strconv.FormatInt(time.Now().UnixNano(), 10), Format: "openai", BaseURL: "http://127.0.0.1:11434/v1", Model: "test-model", APIKey: "test-only", Streaming: true})
	if err != nil {
		t.Fatal(err)
	}
	previousActive, _ := m.pg.ActiveProfile()
	t.Cleanup(func() {
		_, _ = m.pg.Exec(`UPDATE llm_profiles SET is_default=false WHERE id IN ($1,$2)`, localID, cloudID)
		if previousActive != nil {
			_ = m.pg.SetActiveProfile(previousActive.ID)
		}
		_ = m.pg.DeleteProfile(localID)
		_ = m.pg.DeleteProfile(cloudID)
	})

	i := m.interceptor
	_ = m.pg.SetBool("llm_judge_enabled", true)
	for _, tc := range []struct {
		name    string
		id      int64
		timeout int
		action  string
	}{{"cloud", cloudID, 15, "allow"}, {"native", localID, 90, "ask"}} {
		t.Run(tc.name, func(t *testing.T) {
			_ = m.pg.SetSetting("llm_judge_profile_id", strconv.FormatInt(tc.id, 10))
			cfg := i.GetJudgeConfig()
			if cfg.TimeoutSeconds != tc.timeout || cfg.FailAction != tc.action {
				t.Fatalf("defaults=%+v", cfg)
			}
			i.SetReviewer(func(ctx context.Context, id int64, _ string, _ intercept.ReviewInput) (intercept.Decision, error) {
				deadline, ok := ctx.Deadline()
				if !ok {
					t.Fatal("missing deadline")
				}
				remain := time.Until(deadline)
				if remain < time.Duration(tc.timeout-1)*time.Second || remain > time.Duration(tc.timeout)*time.Second {
					t.Fatalf("deadline=%v", remain)
				}
				if id != tc.id {
					t.Fatalf("selected profile=%d", id)
				}
				return intercept.Decision{}, errors.New("local fixture failure")
			})
			d, ok := i.Judge(t.Context(), "Bash", json.RawMessage(`{"command":"true"}`))
			if !ok || d.Action != tc.action {
				t.Fatalf("failure=%+v", d)
			}
			i.SetReviewer(func(context.Context, int64, string, intercept.ReviewInput) (intercept.Decision, error) {
				return intercept.Decision{}, nil
			})
			d, _ = i.Judge(t.Context(), "Bash", json.RawMessage(`{"command":"true"}`))
			if d.Action != tc.action {
				t.Fatalf("malformed=%+v", d)
			}
		})
	}
	_ = m.pg.SetActiveProfile(localID)
	_ = m.pg.SetSetting("llm_judge_profile_id", "0")
	if cfg := i.GetJudgeConfig(); cfg.TimeoutSeconds != 90 || cfg.FailAction != "ask" {
		t.Fatalf("follow active=%+v", cfg)
	}
	_ = m.pg.SetSetting("llm_judge_timeout_seconds", "7")
	_ = m.pg.SetSetting("llm_judge_fail_action", "allow")
	if cfg := i.GetJudgeConfig(); cfg.TimeoutSeconds != 7 || cfg.FailAction != "allow" {
		t.Fatalf("saved override lost=%+v", cfg)
	}
	i.SetReviewer(func(ctx context.Context, _ int64, _ string, _ intercept.ReviewInput) (intercept.Decision, error) {
		return intercept.Decision{}, context.DeadlineExceeded
	})
	d, _ := i.Judge(t.Context(), "Bash", json.RawMessage(`{"command":"true"}`))
	if d.Action != "allow" {
		t.Fatalf("timeout ignored saved policy=%+v", d)
	}
}

func TestNativeJudgeUsesJSONModeAndStrictParser(t *testing.T) {
	m := backportTestManager(t)
	var reviews atomic.Int32
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/show":
			_, _ = w.Write([]byte(`{"capabilities":["tools"],"model_info":{}}`))
		case "/api/chat":
			var req struct {
				Messages []json.RawMessage `json:"messages"`
				Format   map[string]any    `json:"format"`
				Options  map[string]any    `json:"options"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			if len(req.Messages) == 0 {
				_, _ = w.Write([]byte(`{"done":true}`))
				return
			}
			if req.Format["type"] != "object" || req.Format["additionalProperties"] != false {
				t.Errorf("judge missing JSON mode: %+v", req)
			}
			if req.Options["num_ctx"] != float64(32768) {
				t.Errorf("context=%v", req.Options)
			}
			text := `{"decision":"allow","comment":"Operation: read a file; Effect if successful: return content; Rule: A5"}`
			if reviews.Add(1) > 1 {
				text = `{"decision":"allow"}`
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"role": "assistant", "content": text}, "done": true})
		default:
			t.Errorf("unexpected native route %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer fixture.Close()
	id, err := m.pg.SaveProfile(&db.LLMProfile{Name: "native-json-judge-" + strconv.FormatInt(time.Now().UnixNano(), 10), Format: "ollama", Model: "fixture", BaseURL: fixture.URL, Streaming: true, ContextWindowK: 32, MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.pg.DeleteProfile(id) })
	keys := map[string]string{"llm_judge_enabled": "true", "llm_judge_profile_id": strconv.FormatInt(id, 10), "llm_judge_timeout_seconds": "60", "llm_judge_fail_action": "ask"}
	for key, value := range keys {
		previous, found, _ := m.pg.GetSetting(key)
		if err := m.pg.SetSetting(key, value); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if found {
				_ = m.pg.SetSetting(key, previous)
			} else {
				_, _ = m.pg.Exec(`DELETE FROM settings WHERE key=$1`, key)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	td := t.TempDir()
	s := New(ctx, m, td, td, td)
	s.wireInterceptReviewer()
	d, ok := m.interceptor.Judge(t.Context(), "Bash", json.RawMessage(`{"command":"cat fixture.txt"}`))
	if !ok || d.Action != "allow" || d.ProfileID != id {
		t.Fatalf("native verdict=%+v enabled=%v", d, ok)
	}
	d, ok = m.interceptor.Judge(t.Context(), "Bash", json.RawMessage(`{"command":"cat fixture.txt"}`))
	if !ok || d.Action != "ask" || !d.ModelFallback {
		t.Fatalf("malformed native verdict did not ask: %+v", d)
	}
	if reviews.Load() != 2 {
		t.Fatalf("review requests=%d", reviews.Load())
	}

	// Bound/global routing has chain ID zero, but the real reviewer must still use
	// the concrete native configuration captured from that provider invocation.
	reviews.Store(0)
	_ = m.pg.SetSetting("llm_judge_profile_id", "0")
	originCtx := intercept.WithProviderOrigin(t.Context())
	cfg := agent.ConfigFrom("ollama", "fixture", fixture.URL, "", "")
	cfg.ContextWindowK = 32
	concrete := &originProvider{inner: &fakeProvider{}, cfg: cfg, id: 0}
	_, _, _, _ = concrete.Complete(originCtx, llm.CompletionRequest{})
	d, ok = m.interceptor.Judge(originCtx, "Bash", json.RawMessage(`{"command":"cat fixture.txt"}`))
	if !ok || d.Action != "allow" || d.ProfileID != 0 {
		t.Fatalf("concrete native origin was lost: %+v", d)
	}
}
