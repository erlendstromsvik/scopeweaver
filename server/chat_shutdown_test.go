package server

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/chatstate"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/internal/dbtest"
	"github.com/Autumn-27/norma/llm"
)

func TestChatShutdownDrainsFinalizationBeforeDatabaseClose(t *testing.T) {
	s, fid := newRetestServer(t)
	ctx, cancel := context.WithCancelCause(s.ctx)
	s.ctx = ctx
	defer cancel(agent.AbortShutdown)
	started, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	persisted := make(chan error, 1)
	c, err := s.m.pg.CreateConversation("auto", "shutdown fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	ref := chatstate.SessionRef{ConversationID: c.ID}
	setRetestProvider(s, retestProvider{complete: func(ctx context.Context, _ llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		<-release
		// Simulate a provider's deferred persistence after it observes cancellation.
		_, err := s.m.pg.AppendConvActivity(c.ID, db.Activity{Worker: "auto", Kind: "text", Summary: "shutdown finalization marker"})
		persisted <- err
		return llm.Message{}, "", llm.Usage{}, ctx.Err()
	}})
	send := retestRequest(s.pgSendConversationMessage, http.MethodPost, c.ID, `{"message":"first"}`)
	if send.Code != http.StatusAccepted {
		t.Fatalf("send: %d %s", send.Code, send.Body)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("provider never started")
	}
	state, err := s.m.pg.ChatControlState(t.Context(), ref)
	if err != nil || state.Active == nil {
		t.Fatalf("active: %+v %v", state, err)
	}
	runID := state.Active.ID
	if _, err = s.m.pg.AdmitChatInput(t.Context(), ref, "queued-before-shutdown", "queue", "", chatstate.Prompt{Text: "second"}); err != nil {
		t.Fatal(err)
	}
	cancel(agent.AbortShutdown)
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("provider missed shutdown cancellation")
	}

	// An uncooperative provider cannot hold shutdown forever; timing out must not
	// falsely report a finished run or allow another dispatcher to start.
	deadline, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	err = s.DrainChats(deadline)
	stop()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked drain: %v", err)
	}
	if err = s.dispatchChatInput(ref.GateKey(), ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("dispatch during shutdown: %v", err)
	}
	denied := retestRequest(s.pgSendConversationMessage, http.MethodPost, c.ID, `{"message":"too late"}`)
	if denied.Code != http.StatusServiceUnavailable {
		t.Fatalf("shutdown admission: %d %s", denied.Code, denied.Body)
	}
	running, err := s.m.pg.ChatRun(t.Context(), runID)
	if err != nil || running.Status != "running" {
		t.Fatalf("unfinalized run: %+v %v", running, err)
	}

	drainCtx, drainCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer drainCancel()
	drained := make(chan error, 1)
	go func() { drained <- s.DrainChats(drainCtx) }()
	close(release)
	released = true
	if err = <-drained; err != nil {
		t.Fatal(err)
	}
	if err = <-persisted; err != nil {
		t.Fatalf("final persistence: %v", err)
	}
	finished, err := s.m.pg.ChatRun(t.Context(), runID)
	if err != nil || finished.Status != "unknown" {
		t.Fatalf("interrupted run: %+v %v", finished, err)
	}
	state, err = s.m.pg.ChatControlState(t.Context(), ref)
	if err != nil || state.Active != nil || !state.Paused || len(state.Inputs) != 2 {
		t.Fatalf("drained state: %+v %v", state, err)
	}
	var queued int
	for _, input := range state.Inputs {
		if input.Status == "queued" {
			queued++
		}
	}
	if queued != 1 {
		t.Fatalf("shutdown dispatched follow-up: %+v", state.Inputs)
	}
	var markers int
	if err = s.m.pg.QueryRow(`SELECT count(*) FROM conversation_activities WHERE conversation_id=$1 AND summary='shutdown finalization marker'`, c.ID).Scan(&markers); err != nil || markers != 1 {
		t.Fatalf("final marker rows=%d: %v", markers, err)
	}

	// The runtime performs this close only after DrainChats. Reopen a real DB
	// connection to prove final status and marker survived rather than checking
	// only in-memory chatBusy bookkeeping.
	if err = s.m.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := db.Open(dbtest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	defer reopened.DeleteConversation(c.ID)
	defer reopened.DeleteFinding(fid)
	finished, err = reopened.ChatRun(t.Context(), runID)
	if err != nil || finished.Status != "unknown" {
		t.Fatalf("reopened run: %+v %v", finished, err)
	}
	if err = reopened.QueryRow(`SELECT count(*) FROM conversation_activities WHERE conversation_id=$1 AND summary='shutdown finalization marker'`, c.ID).Scan(&markers); err != nil || markers != 1 {
		t.Fatalf("reopened final marker rows=%d: %v", markers, err)
	}
	t.Logf("run %s: running -> unknown before DB close; input queue=1; marker rows=%d before and after reopen; late admission=503", runID, markers)
}

func TestChatShutdownWithNoRunsAndRepeatedDrain(t *testing.T) {
	s := &Server{ctx: context.Background()}
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := s.DrainChats(ctx)
		cancel()
		if err != nil {
			t.Fatalf("drain %d: %v", i, err)
		}
	}
}
