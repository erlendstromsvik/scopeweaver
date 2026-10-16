package agent

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestClaudeReplayPreservesMessageFields(t *testing.T) {
	for _, userContent := range []string{`[{"type":"text","text":"go"}]`, `"go"`} {
		t.Run(userContent, func(t *testing.T) {
			replay := &claudeReplay{}
			thinking := json.RawMessage(`{"type":"thinking","thinking":"summary","signature":"exact-signature"}`)
			replay.remember("tool-1", []json.RawMessage{thinking})
			raw := `{"thinking":{"type":"adaptive"},"messages":[{"role":"user","content":` + userContent + `,"metadata":{"keep":true}},{"role":"assistant","content":[{"type":"tool_use","id":"tool-1","name":"probe","input":{}}],"extra":{"nested":[1,true,"keep"]},"name":"fixture"}]}`
			req, err := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", strings.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			if err := replay.rewrite(req); err != nil {
				t.Fatal(err)
			}
			var before, after struct {
				Messages []map[string]json.RawMessage `json:"messages"`
			}
			if err := json.Unmarshal([]byte(raw), &before); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(requestBodySnapshot(req)), &after); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before.Messages[0], after.Messages[0]) {
				t.Errorf("user message changed: %s", requestBodySnapshot(req))
			}
			for key, want := range before.Messages[1] {
				if key != "content" && string(after.Messages[1][key]) != string(want) {
					t.Errorf("assistant field %s=%s, want %s", key, after.Messages[1][key], want)
				}
			}
			var blocks []json.RawMessage
			if err := json.Unmarshal(after.Messages[1]["content"], &blocks); err != nil {
				t.Fatal(err)
			}
			if len(blocks) != 2 || thinkingIdentity(blocks[0]) != thinkingIdentity(thinking) {
				t.Fatalf("thinking not reinserted before tool use: %s", after.Messages[1]["content"])
			}
		})
	}
}
