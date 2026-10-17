package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"iter"
	"log"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/llmpool"
	"github.com/Autumn-27/artex/llmrec"
	"github.com/Autumn-27/norma/llm"
)

// textProv serves one text delta; failProv fails before any output with a
// status formatted like the SDK's errors (so llmpool's failover picks it up).
type scriptedProvider struct {
	events []llm.StreamEvent
	err    error
}

func (p *scriptedProvider) Stream(context.Context, llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		for _, ev := range p.events {
			if !yield(ev, nil) {
				return
			}
		}
		if p.err != nil {
			yield(llm.StreamEvent{}, p.err)
		}
	}
}

func (p *scriptedProvider) Complete(context.Context, llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	if p.err != nil {
		return llm.Message{}, "", llm.Usage{}, p.err
	}
	var blocks []llm.ContentBlock
	for _, ev := range p.events {
		if ev.Type == llm.SETextDelta {
			blocks = append(blocks, llm.TextBlock(ev.Text))
		}
	}
	return llm.Message{Role: llm.RoleAssistant, Content: blocks}, "end_turn", llm.Usage{}, nil
}

func textProv(text string) llm.Provider {
	return &scriptedProvider{events: []llm.StreamEvent{{Type: llm.SETextDelta, Text: text}}}
}

func failProv(err error) llm.Provider { return &scriptedProvider{err: err} }

// captureLogs redirects the standard logger for the duration of t.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	origOut, origFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(origOut); log.SetFlags(origFlags) })
	return &buf
}

func TestBackendHost(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"http://fedora:8090/v1", "fedora:8090"},
		{"https://spark.local:8000/", "spark.local:8000"},
		{"http://127.0.0.1:11434", "127.0.0.1:11434"},
		{"", "-"},
		{"garbage", "garbage"},
	} {
		if got := backendHost(tc.in); got != tc.want {
			t.Errorf("backendHost(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLoggedProviderEmitsOneLinePerRequest(t *testing.T) {
	ctx := llmrec.WithTaskID(context.Background(), "42")
	for _, tc := range []struct {
		name         string
		prov         llm.Provider
		stream       bool
		wantOutcome  string
		wantErrField bool
	}{
		{name: "stream ok", prov: textProv("hi"), stream: true, wantOutcome: "ok"},
		{name: "stream empty", prov: &scriptedProvider{}, stream: true, wantOutcome: "empty"},
		{name: "stream error", prov: failProv(errors.New("boom: status 500")), stream: true, wantOutcome: "error", wantErrField: true},
		{name: "complete ok", prov: textProv("hi"), wantOutcome: "ok"},
		{name: "complete empty", prov: &scriptedProvider{}, wantOutcome: "empty"},
		{name: "complete error", prov: failProv(errors.New("boom: status 402")), wantOutcome: "error", wantErrField: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureLogs(t)
			p := loggedProvider(tc.prov, "spark", "http://spark:8000/v1")
			if tc.stream {
				for range p.Stream(ctx, llm.CompletionRequest{}) {
				}
			} else {
				_, _, _, err := p.Complete(ctx, llm.CompletionRequest{})
				if tc.wantOutcome == "error" && err == nil {
					t.Fatal("expected error")
				}
			}
			lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
			var hit []string
			for _, l := range lines {
				if strings.Contains(l, "[llmreq]") {
					hit = append(hit, l)
				}
			}
			if len(hit) != 1 {
				t.Fatalf("want exactly one [llmreq] line, got %d in:\n%s", len(hit), logs.String())
			}
			line := hit[0]
			for _, want := range []string{
				"task=42",
				`backend="spark"`,
				"host=spark:8000",
				"ms=",
				"outcome=" + tc.wantOutcome,
			} {
				if !strings.Contains(line, want) {
					t.Errorf("line missing %q: %s", want, line)
				}
			}
			if tc.wantErrField && !strings.Contains(line, "err=") {
				t.Errorf("error line missing err= field: %s", line)
			}
		})
	}
}

// A pool failover must show the attempted backend and the fallback as two
// explicit [llmreq] lines (tried fedora → error, served by spark → ok).
func TestLoggedPoolFailover(t *testing.T) {
	ctx := llmrec.WithTaskID(context.Background(), "7")
	logs := captureLogs(t)
	head := loggedProvider(failProv(fmt.Errorf("anthropic: status 402: no credit")), "fedora", "http://fedora:8090/v1")
	tail := loggedProvider(textProv("served"), "spark", "http://spark:8000/v1")
	pool := llmpool.New(
		[]*llmpool.Member{
			{ID: 1, Name: "fedora", Rank: llmpool.RankActive, Prov: head},
			{ID: 2, Name: "spark", Rank: 0, Prov: tail},
		},
		llmpool.NewRegistry(nil, nil),
	)
	var text strings.Builder
	var lastErr error
	for ev, err := range pool.Stream(ctx, llm.CompletionRequest{}) {
		if err != nil {
			lastErr = err
			continue
		}
		if ev.Type == llm.SETextDelta {
			text.WriteString(ev.Text)
		}
	}
	if lastErr != nil {
		t.Fatalf("failover should have succeeded, got: %v", lastErr)
	}
	if text.String() != "served" {
		t.Fatalf("served text = %q", text.String())
	}
	out := logs.String()
	if !strings.Contains(out, `task=7 backend="fedora" host=fedora:8090`) || !strings.Contains(out, "outcome=error") {
		t.Errorf("missing failed attempt line for fedora:\n%s", out)
	}
	attempted, served := 0, 0
	for _, l := range strings.Split(out, "\n") {
		if !strings.Contains(l, "[llmreq]") {
			continue
		}
		switch {
		case strings.Contains(l, `backend="fedora"`) && strings.Contains(l, "outcome=error"):
			attempted++
		case strings.Contains(l, `backend="spark"`) && strings.Contains(l, "host=spark:8000") && strings.Contains(l, "outcome=ok") && strings.Contains(l, "ms="):
			served++
		}
	}
	if attempted != 1 || served != 1 {
		t.Errorf("want 1 attempted + 1 served [llmreq] line, got %d/%d:\n%s", attempted, served, out)
	}
}
