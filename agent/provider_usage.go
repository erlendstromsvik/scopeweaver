package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"iter"
	"strings"

	"github.com/Autumn-27/artex/llmrec"
	"github.com/Autumn-27/norma/llm"
)

type rawUsageKey struct{}

type rawUsage struct {
	metadata  *llmrec.UsageMetadata
	usage     llm.Usage
	reasoning int
	terminals []string
}

func (u *rawUsage) reset() {
	u.usage = llm.Usage{}
	u.reasoning = 0
	u.terminals = nil
	u.metadata.Set(u.usage, 0)
}

func (u *rawUsage) observe(event string, data []byte) {
	var frame struct {
		Type    string          `json:"type"`
		Usage   json.RawMessage `json:"usage"`
		Message struct {
			Usage json.RawMessage `json:"usage"`
		} `json:"message"`
		Response struct {
			Usage json.RawMessage `json:"usage"`
		} `json:"response"`
	}
	if json.Unmarshal(data, &frame) != nil {
		return
	}
	if event == "" {
		event = frame.Type
	}
	if event == "response.completed" || event == "response.incomplete" {
		u.terminals = append(u.terminals, event)
	}
	raw := frame.Usage
	if len(frame.Message.Usage) > 0 {
		raw = frame.Message.Usage
	}
	if len(frame.Response.Usage) > 0 {
		raw = frame.Response.Usage
	}
	var fields struct {
		PromptTokens     int          `json:"prompt_tokens"`
		CompletionTokens int          `json:"completion_tokens"`
		InputTokens      int          `json:"input_tokens"`
		OutputTokens     int          `json:"output_tokens"`
		Hit              int          `json:"prompt_cache_hit_tokens"`
		Cached           int          `json:"cached_tokens"`
		Read             int          `json:"cache_read_input_tokens"`
		Write            int          `json:"cache_creation_input_tokens"`
		Prompt           detailsUsage `json:"prompt_tokens_details"`
		Input            detailsUsage `json:"input_tokens_details"`
		Completion       detailsUsage `json:"completion_tokens_details"`
		Output           detailsUsage `json:"output_tokens_details"`
	}
	if json.Unmarshal(raw, &fields) != nil {
		return
	}
	u.usage.InputTokens = max(u.usage.InputTokens, fields.PromptTokens, fields.InputTokens+fields.Read+fields.Write)
	u.usage.OutputTokens = max(u.usage.OutputTokens, fields.CompletionTokens, fields.OutputTokens)
	u.usage.CacheReadTokens = max(u.usage.CacheReadTokens, fields.Prompt.Cached, fields.Input.Cached, fields.Hit, fields.Cached, fields.Read)
	u.usage.CacheWriteTokens = max(u.usage.CacheWriteTokens, fields.Write, fields.Prompt.Write, fields.Input.Write, fields.Prompt.Creation, fields.Input.Creation)
	u.reasoning = max(u.reasoning, fields.Completion.Reasoning, fields.Output.Reasoning, fields.Output.Thinking)
	u.metadata.Set(u.usage, u.reasoning)
}

type detailsUsage struct {
	Cached    int `json:"cached_tokens"`
	Write     int `json:"cache_write_tokens"`
	Creation  int `json:"cache_creation_input_tokens"`
	Reasoning int `json:"reasoning_tokens"`
	Thinking  int `json:"thinking_tokens"`
}

// observeBody tees without extra reads or events. SSE is parsed one frame at a
// time; non-streaming JSON is decoded once its already-consumed body reaches EOF.
type observeBody struct {
	io.ReadCloser
	sse     bool
	pending []byte
	event   string
	data    []byte
	observe func(string, []byte)
	done    bool
}

func (b *observeBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.pending = append(b.pending, p[:n]...)
	if b.sse {
		for {
			i := bytes.IndexByte(b.pending, '\n')
			if i < 0 {
				break
			}
			line := strings.TrimRight(string(b.pending[:i]), "\r")
			b.pending = b.pending[i+1:]
			switch {
			case line == "":
				b.flush()
			case strings.HasPrefix(line, "event:"):
				b.event = strings.TrimSpace(line[6:])
			case strings.HasPrefix(line, "data:"):
				b.data = append(b.data, strings.TrimPrefix(line[5:], " ")...)
				b.data = append(b.data, '\n')
			}
		}
	}
	if err != nil {
		b.finish()
	}
	return n, err
}

func (b *observeBody) flush() {
	if len(b.data) > 0 {
		b.observe(b.event, bytes.TrimSpace(b.data))
	}
	b.event = ""
	b.data = nil
}
func (b *observeBody) finish() {
	if b.done {
		return
	}
	b.done = true
	if b.sse {
		b.flush()
	} else if len(b.pending) > 0 {
		b.observe("", b.pending)
	}
	b.pending = nil
}
func (b *observeBody) Close() error { b.finish(); return b.ReadCloser.Close() }

type cloudProviderAdapter struct {
	llm.Provider
	format         llm.Format
	claude         bool
	rejectDisabled bool
}

func (p cloudProviderAdapter) request(req llm.CompletionRequest) llm.CompletionRequest {
	if p.format == llm.FormatAnthropic && req.DynamicBoundary == 0 && len(req.System) > 0 {
		req.DynamicBoundary = len(req.System)
	}
	if p.claude {
		if req.Thinking == "disabled" && p.rejectDisabled {
			req.Thinking = ""
		}
		req.Temperature = nil
	}
	return req
}
func (p cloudProviderAdapter) Stream(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		metadata := llmrec.UsageMetadataFrom(ctx)
		if metadata == nil {
			ctx, metadata = llmrec.NewUsageMetadata(ctx)
		}
		raw := &rawUsage{metadata: metadata}
		ctx = context.WithValue(ctx, rawUsageKey{}, raw)
		for ev, err := range p.Provider.Stream(ctx, p.request(req)) {
			if ev.Type == llm.SEMessageStart || ev.Type == llm.SEMessageDelta {
				ev.Usage, _ = metadata.Enrich(ev.Usage)
			}
			if !yield(ev, err) || err != nil {
				return
			}
			// Norma emits a terminal delta for both completed and incomplete. Only the
			// corresponding observed completed frame authorizes an added stop event.
			if p.format == llm.FormatOpenAIResponses && ev.Type == llm.SEMessageDelta && len(raw.terminals) > 0 {
				terminal := raw.terminals[0]
				raw.terminals = raw.terminals[1:]
				if terminal == "response.completed" && !yield(llm.StreamEvent{Type: llm.SEMessageStop}, nil) {
					return
				}
			}
		}
	}
}
func (p cloudProviderAdapter) Complete(ctx context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	metadata := llmrec.UsageMetadataFrom(ctx)
	if metadata == nil {
		ctx, metadata = llmrec.NewUsageMetadata(ctx)
	}
	ctx = context.WithValue(ctx, rawUsageKey{}, &rawUsage{metadata: metadata})
	msg, stop, usage, err := p.Provider.Complete(ctx, p.request(req))
	usage, _ = metadata.Enrich(usage)
	if err != nil {
		usage = metadata.EnrichFailure(usage)
	}
	return msg, stop, usage, err
}
