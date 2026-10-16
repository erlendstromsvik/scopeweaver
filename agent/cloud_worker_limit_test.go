package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/internal/dbtest"
	"github.com/Autumn-27/norma/llm"
)

func TestWorkerZeroMaxTokensWireDefault(t *testing.T) {
	pg, err := db.Open(dbtest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	expID, err := pg.CreateExploration("controlled token-limit fixture", "no external calls")
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Exec(`DELETE FROM explorations WHERE id=$1`, expID)
	ts := pg.Exploration(expID)
	payload := map[string]any{"summary": "Return done; do not call tools."}
	intentID, err := ts.AddNode(db.KindIntent, payload, 0, "running", "planner", nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		MaxTokens int `json:"max_tokens"`
	}
	requests := 0
	p := cloudProvider(t, "openai", func(w http.ResponseWriter, r *http.Request) {
		requests++
		if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, cloudReply("openai", false))
	})
	profile := Config{MaxTokens: 0}
	worker := NewWorker(p, "fixture", t.TempDir(), nil, 200000, 3)
	worker.SetNonStreaming(func() bool { return true })
	worker.SetMaxTokens(func() int { return profile.MaxTokens })
	if _, _, err := worker.Execute(t.Context(), "fixture-worker", expID, nil, ts, &db.Node{ID: intentID, Payload: raw}, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if requests != 1 || wire.MaxTokens != 32768 {
		t.Fatalf("worker requests=%d max_tokens=%d want 1/32768", requests, wire.MaxTokens)
	}
}

func TestDirectProviderZeroMaxTokensDefaults(t *testing.T) {
	for _, format := range []string{"anthropic", "ollama"} {
		t.Run(format, func(t *testing.T) {
			var body struct {
				MaxTokens int `json:"max_tokens"`
				Options   struct {
					NumPredict int `json:"num_predict"`
				} `json:"options"`
			}
			p := cloudProvider(t, format, func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, cloudReply(format, false))
			})
			if _, _, _, err := p.Complete(t.Context(), llm.CompletionRequest{Messages: []llm.Message{llm.UserText("fixture")}}); err != nil {
				t.Fatal(err)
			}
			if format == "anthropic" && body.MaxTokens != 8192 {
				t.Fatalf("direct Anthropic max_tokens=%d want 8192", body.MaxTokens)
			}
			if format == "ollama" && body.Options.NumPredict != 4096 {
				t.Fatalf("direct native num_predict=%d want 4096", body.Options.NumPredict)
			}
		})
	}
}
