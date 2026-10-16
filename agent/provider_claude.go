package agent

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
)

// claude55Model matches Claude 5.5 model ids, including dated snapshots.
var claude55Model = regexp.MustCompile(`^claude-[a-z]+-5-5(-\d{8})?$`)

func (c Config) officialClaude55() bool {
	if c.Provider() != "anthropic" || !claude55Model.MatchString(c.Model) {
		return false
	}
	if c.BaseURL == "" {
		return true
	}
	u, err := url.Parse(c.BaseURL)
	return err == nil && strings.EqualFold(u.Hostname(), "api.anthropic.com")
}

// Per-provider replay is bounded by both tool count and encoded bytes. Requests
// from concurrent sessions cannot replace each other's blocks (tool IDs key it).
type claudeReplay struct {
	mu     sync.Mutex
	blocks map[string][]json.RawMessage
	order  []string
	bytes  int
}

func (c *claudeReplay) remember(id string, blocks []json.RawMessage) {
	if len(blocks) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.blocks == nil {
		c.blocks = map[string][]json.RawMessage{}
	}
	if _, ok := c.blocks[id]; ok {
		return
	}
	size := 0
	for _, b := range blocks {
		size += len(b)
	}
	if size > 4*1024*1024 {
		return
	}
	for len(c.order) >= 128 || c.bytes+size > 4*1024*1024 {
		old := c.order[0]
		c.order = c.order[1:]
		for _, b := range c.blocks[old] {
			c.bytes -= len(b)
		}
		delete(c.blocks, old)
	}
	c.blocks[id] = append([]json.RawMessage(nil), blocks...)
	c.order = append(c.order, id)
	c.bytes += size
}

func thinkingIdentity(raw json.RawMessage) string {
	var b struct{ Type, Signature, Data string }
	json.Unmarshal(raw, &b)
	if b.Type == "thinking" {
		return b.Type + ":" + b.Signature
	}
	if b.Type == "redacted_thinking" {
		return b.Type + ":" + b.Data
	}
	return ""
}

func (c *claudeReplay) rewrite(req *http.Request) error {
	raw := requestBodySnapshot(req)
	var body map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &body) != nil {
		return nil
	}
	var thinking map[string]any
	_ = json.Unmarshal(body["thinking"], &thinking)
	active := thinking["type"] == "adaptive" || thinking["type"] == "enabled"
	if active {
		thinking["display"] = "summarized"
		body["thinking"], _ = json.Marshal(thinking)
	}
	var messages []map[string]json.RawMessage
	if active && json.Unmarshal(body["messages"], &messages) == nil {
		c.mu.Lock()
		for _, m := range messages {
			if string(m["role"]) != `"assistant"` {
				continue
			}
			var content []json.RawMessage
			if json.Unmarshal(m["content"], &content) != nil || content == nil {
				continue
			}
			for pos := 0; pos < len(content); pos++ {
				var block struct{ Type, ID string }
				json.Unmarshal(content[pos], &block)
				if block.Type != "tool_use" {
					continue
				}
				saved := c.blocks[block.ID]
				insert := pos
				// Work backwards so a missing earlier block precedes the next
				// retained block, rather than being appended out of order.
				for j := len(saved) - 1; j >= 0; j-- {
					key := thinkingIdentity(saved[j])
					found := -1
					for k := insert - 1; k >= 0; k-- {
						if thinkingIdentity(content[k]) == key {
							found = k
							break
						}
					}
					if found >= 0 {
						insert = found
						continue
					}
					content = append(content, nil)
					copy(content[insert+1:], content[insert:])
					content[insert] = saved[j]
					pos++
				}
			}
			for j, b := range content {
				var fields map[string]json.RawMessage
				json.Unmarshal(b, &fields)
				if string(fields["type"]) == `"thinking"` {
					if _, ok := fields["thinking"]; !ok {
						fields["thinking"] = json.RawMessage(`""`)
						content[j], _ = json.Marshal(fields)
					}
				}
			}
			m["content"], _ = json.Marshal(content)
		}
		c.mu.Unlock()
		body["messages"], _ = json.Marshal(messages)
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req.Body = io.NopCloser(bytes.NewReader(data))
	req.ContentLength = int64(len(data))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }
	return nil
}

type claudeObservation struct {
	replay   *claudeReplay
	thinking []json.RawMessage
	current  map[string]any
	index    int
}

func (o *claudeObservation) observe(_ string, data []byte) {
	var frame struct {
		Type    string                                     `json:"type"`
		Index   int                                        `json:"index"`
		Content []json.RawMessage                          `json:"content"`
		Block   map[string]any                             `json:"content_block"`
		Delta   struct{ Type, Thinking, Signature string } `json:"delta"`
	}
	if json.Unmarshal(data, &frame) != nil {
		return
	}
	if frame.Content != nil {
		for _, b := range frame.Content {
			o.block(b)
		}
		return
	}
	switch frame.Type {
	case "content_block_start":
		o.flush()
		o.index = frame.Index
		if frame.Block["type"] == "thinking" {
			o.current = frame.Block
			return
		}
		b, _ := json.Marshal(frame.Block)
		o.block(b)
	case "content_block_delta":
		if o.current == nil || frame.Index != o.index {
			return
		}
		switch frame.Delta.Type {
		case "thinking_delta":
			prev, _ := o.current["thinking"].(string)
			o.current["thinking"] = prev + frame.Delta.Thinking
		case "signature_delta":
			prev, _ := o.current["signature"].(string)
			o.current["signature"] = prev + frame.Delta.Signature
		}
	case "content_block_stop", "message_stop":
		o.flush()
	}
}
func (o *claudeObservation) flush() {
	if o.current != nil {
		b, _ := json.Marshal(o.current)
		o.block(b)
		o.current = nil
	}
}
func (o *claudeObservation) block(b json.RawMessage) {
	var block struct{ Type, ID string }
	json.Unmarshal(b, &block)
	switch block.Type {
	case "thinking", "redacted_thinking":
		o.thinking = append(o.thinking, append(json.RawMessage(nil), b...))
	case "tool_use":
		o.replay.remember(block.ID, o.thinking)
	}
}
