package server

import (
	"context"
	"iter"
	"log"
	"strings"
	"time"

	"github.com/Autumn-27/artex/llmrec"
	"github.com/Autumn-27/artex/locale"
	"github.com/Autumn-27/norma/llm"
)

// Per-request LLM observability (ROADMAP D.10): every request a profile
// provider serves emits exactly one [llmreq] log line with the owning task,
// the profile name and base_url host that served it, the time-to-response,
// and the outcome (ok / error / empty). Pool failover attempts each pass
// through their own member wrapper, so "tried fedora, failed, served by
// spark" shows up as two explicit lines. The line goes through log.Printf
// like the existing [llmpool]/[task-llm] lines, so it lands in normal
// instance/container logs.

// requestLogProvider decorates a profile provider with one log line per call.
type requestLogProvider struct {
	inner   llm.Provider
	backend string // profile name ("" when unresolvable)
	host    string // base_url host:port ("-" when unknown)
}

// loggedProvider wraps prov so each Stream/Complete call logs one [llmreq]
// line identifying the backend (profile name + base_url host) that served it.
func loggedProvider(prov llm.Provider, name, baseURL string) llm.Provider {
	return &requestLogProvider{inner: prov, backend: name, host: backendHost(baseURL)}
}

// backendHost extracts the host[:port] from a configured base URL without
// pulling in a full URL parse (base URLs are scheme://host[:port][/path]).
func backendHost(baseURL string) string {
	s := strings.TrimSpace(baseURL)
	if s == "" {
		return "-"
	}
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	} else {
		s = strings.TrimPrefix(s, "//")
	}
	if j := strings.IndexAny(s, "/?#"); j >= 0 {
		s = s[:j]
	}
	if s == "" {
		return "-"
	}
	return s
}

func (p *requestLogProvider) Stream(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		start := time.Now()
		content := false
		done := false
		finish := func(err error) {
			if done {
				return
			}
			done = true
			p.logReq(ctx, start, err, content)
		}
		defer func() { finish(ctx.Err()) }() // interrupted / early-abandon streams still get a line
		for ev, err := range p.inner.Stream(ctx, req) {
			if err != nil {
				finish(err)
				if !yield(ev, err) {
					return
				}
				return
			}
			if streamHasContent(ev) {
				content = true
			}
			if !yield(ev, nil) {
				return
			}
		}
		finish(nil)
	}
}

func (p *requestLogProvider) Complete(ctx context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	start := time.Now()
	msg, stop, usage, err := p.inner.Complete(ctx, req)
	p.logReq(ctx, start, err, messageHasContent(msg))
	return msg, stop, usage, err
}

// streamHasContent reports whether the event carries model output (text,
// thinking or a tool request). Lifecycle-only events do not.
func streamHasContent(ev llm.StreamEvent) bool {
	switch ev.Type {
	case llm.SETextDelta, llm.SEThinkingDelta, llm.SEToolInputJSON:
		return ev.Text != ""
	case llm.SEToolUseStart:
		return true
	default:
		return false
	}
}

// messageHasContent is the Complete-path counterpart of streamHasContent.
func messageHasContent(msg llm.Message) bool {
	for _, b := range msg.Content {
		switch b.Type {
		case llm.BlockText:
			if b.Text != "" {
				return true
			}
		case llm.BlockThinking:
			if b.Thinking != "" {
				return true
			}
		case llm.BlockToolUse:
			return true
		}
	}
	return false
}

func (p *requestLogProvider) logReq(ctx context.Context, start time.Time, err error, content bool) {
	outcome := "ok"
	switch {
	case err != nil:
		outcome = "error"
	case !content:
		outcome = "empty"
	}
	task := llmrec.TaskIDFrom(ctx)
	if task == "" {
		task = "-"
	}
	ms := time.Since(start).Milliseconds()
	if err != nil {
		log.Printf(locale.Text(locale.FromContext(ctx), "[llmreq] task=%s backend=%q host=%s ms=%d outcome=%s err=%v"),
			task, p.backend, p.host, ms, outcome, err)
		return
	}
	log.Printf(locale.Text(locale.FromContext(ctx), "[llmreq] task=%s backend=%q host=%s ms=%d outcome=%s"),
		task, p.backend, p.host, ms, outcome)
}
