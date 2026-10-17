package server

import (
	"context"
	"errors"
	"iter"
	"net/http"
	"testing"
	"time"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/internal/dbtest"
	"github.com/Autumn-27/norma/llm"
)

type shutdownSideProvider struct{ started, cancelled, release chan struct{} }

func (p *shutdownSideProvider) Complete(context.Context, llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	return llm.Message{}, "", llm.Usage{}, errors.New("fixture expects streaming")
}
func (p *shutdownSideProvider) Stream(ctx context.Context, _ llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(y func(llm.StreamEvent, error) bool) {
		if !y(llm.StreamEvent{Type: llm.SEMessageStart, Usage: llm.Usage{InputTokens: 19}}, nil) {
			return
		}
		if !y(llm.StreamEvent{Type: llm.SETextDelta, Text: "shutdown side marker"}, nil) {
			return
		}
		close(p.started)
		<-ctx.Done()
		close(p.cancelled)
		<-p.release
		y(llm.StreamEvent{}, ctx.Err())
	}
}

func TestChatShutdownDrainsSideAnswerAndCheckpointPublisher(t *testing.T) {
	s, fid := newRetestServer(t)
	ctx, cancel := context.WithCancel(s.ctx)
	s.ctx = ctx
	s.engine = NewEngine(s.m)
	s.jwtKey = []byte("shutdown-side-test-signing-key-only")
	s.initSideQuestions()
	t.Cleanup(func() {
		cancel()
		ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := s.DrainChats(ctx); err != nil {
			t.Error(err)
		}
	})
	p := &shutdownSideProvider{make(chan struct{}), make(chan struct{}), make(chan struct{})}
	released := false
	defer func() {
		cancel()
		if !released {
			close(p.release)
		}
	}()
	cfg := agent.Config{Format: llm.FormatOpenAI, BaseURL: "http://fixture.invalid", Model: "fixture", Stream: true}
	s.llmCfg = cfg
	s.llmOn = true
	s.llmDirect = bindSideProvider(p, cfg, 0, "fixture")
	token, err := signJWT(s.jwtKey)
	if err != nil {
		t.Fatal(err)
	}
	f := &sideHTTPFixture{s: s, m: s.m, handler: s.Handler(), token: token}
	parent, path := f.conversation(t)
	snap := f.checkpoint(t, parent)
	exchange := decodeSide(t, f.call(t, http.MethodPost, path, map[string]string{"question": "owned side shutdown fixture", "client_request_id": "shutdown"}, http.StatusAccepted))
	select {
	case <-p.started:
	case <-time.After(5 * time.Second):
		t.Fatal("side provider never started")
	}

	// Keep the periodic publisher's cancellation flush blocked. The old chatWG-
	// only drain incorrectly returned nil because no main chat was running.
	s.side.commands.Lock()
	commandsHeld := true
	defer func() {
		if commandsHeld {
			s.side.commands.Unlock()
		}
	}()
	snap.Version++
	snap.Request.Messages = []llm.Message{llm.UserText("final shutdown checkpoint")}
	s.side.mu.Lock()
	s.side.pending[parent.Key()] = snap
	s.side.latest[parent.Key()] = snap
	s.side.mu.Unlock()
	cancel()
	select {
	case <-p.cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("side provider missed cancellation")
	}
	bounded := func() {
		t.Helper()
		ctx, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
		err := s.DrainChats(ctx)
		stop()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("incomplete side drain returned %v", err)
		}
	}
	bounded()
	s.side.commands.Unlock()
	commandsHeld = false
	select {
	case <-s.side.done:
	case <-time.After(5 * time.Second):
		t.Fatal("checkpoint publisher did not exit")
	}
	checkpoint, err := s.m.pg.SideSnapshot(t.Context(), parent.Key())
	if err != nil || checkpoint == nil || checkpoint.Version != snap.Version || checkpoint.Request.Messages[0].Text() != "final shutdown checkpoint" {
		t.Fatalf("checkpoint flush: %+v %v", checkpoint, err)
	}
	// The publisher is done, but the side run still has final answer writes left.
	bounded()
	f.call(t, http.MethodPost, path, map[string]string{"question": "too late", "client_request_id": "late"}, http.StatusServiceUnavailable)
	close(p.release)
	released = true
	drainCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	if err := s.DrainChats(drainCtx); err != nil {
		t.Fatal(err)
	}
	row, err := s.m.pg.SideRequest(t.Context(), exchange.ID)
	if err != nil || row == nil || row.Status != "cancelled" || row.Answer != "shutdown side marker" || row.Usage.InputTokens != 19 {
		t.Fatalf("side finalization: %+v %v", row, err)
	}
	if err := s.m.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := db.Open(dbtest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	defer reopened.DeleteConversation(parent.ConversationID)
	defer reopened.DeleteFinding(fid)
	row, err = reopened.SideRequest(t.Context(), exchange.ID)
	if err != nil || row == nil || row.Status != "cancelled" || row.Answer != "shutdown side marker" || row.Usage.InputTokens != 19 {
		t.Fatalf("reopened side finalization: %+v %v", row, err)
	}
	checkpoint, err = reopened.SideSnapshot(t.Context(), parent.Key())
	if err != nil || checkpoint == nil || checkpoint.Version != snap.Version {
		t.Fatalf("reopened checkpoint: %+v %v", checkpoint, err)
	}
	t.Logf("side %s: cancelled answer and19inputtokens persisted before Close and after reopen; checkpoint version%d durable; late admission503", exchange.ID, snap.Version)
}
