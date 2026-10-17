package sidequestion

import (
	"context"
	"github.com/Autumn-27/norma/harness"
	"github.com/Autumn-27/norma/llm"
	"testing"
)

func TestCaptureFinishKeepsProjectedWorkingSet(t *testing.T) {
	var snapshots []Snapshot
	ctx := WithPublisher(t.Context(), func(s Snapshot) { snapshots = append(snapshots, s) })
	ctx = WithWorkingSetProjector(ctx, func(_ context.Context, _ []llm.Message) []llm.Message {
		return []llm.Message{llm.UserText("projected memory")}
	})
	ctx, _ = Attach(ctx, Parent{ConversationID: 1}, harness.QueryDeps{CallModelSync: func(context.Context, llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
		return llm.Message{}, "", llm.Usage{}, nil
	}}, fakeProvider{})
	c := ctx.Value(captureKey{}).(*Capture)
	c.save(llm.CompletionRequest{Messages: []llm.Message{llm.UserText("observed request")}}, Model{})
	Finish(ctx, []llm.Message{llm.UserText("raw archived secret")})
	last := snapshots[len(snapshots)-1]
	if len(last.Request.Messages) != 1 || last.Request.Messages[0].Text() != "projected memory" {
		t.Fatal("raw history replaced active projection", last)
	}
}
