package server

import (
	"errors"
	"github.com/Autumn-27/artex/chatstate"
	"github.com/Autumn-27/norma/transcript"
	"os"
	"path/filepath"
	"testing"
)

func TestChatDeleteClaimBarrier(t *testing.T) {
	f := newHistoryFixture(t)
	a, err := f.s.m.pg.AdmitChatInput(t.Context(), f.ref, "claimed-before-delete", "send", "", chatstate.Prompt{Text: "must not infer"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := f.s.m.pg.ClaimNextChatInput(t.Context(), f.ref.GateKey())
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.m.pg.CloseChatGate(t.Context(), f.ref); err != nil {
		t.Fatal(err)
	}
	if err = f.s.executeChatRun(t.Context(), *run); !errors.Is(err, chatstate.ErrGone) {
		t.Fatal("closed claim executed", a, err)
	}
	if f.calls.Load() != 0 {
		t.Fatal("provider called after delete barrier")
	}
	if _, err = f.s.m.pg.AdmitChatInput(t.Context(), f.ref, "late", "queue", "", chatstate.Prompt{Text: "late"}); !errors.Is(err, chatstate.ErrGone) {
		t.Fatal("closed parent accepted work", err)
	}
	f.s.m.pg.DeleteConversation(f.ref.ConversationID)
	if err = f.s.m.pg.SaveChatContext(t.Context(), f.ref, run.ID, chatstate.ContextView{Sequence: 99}); !errors.Is(err, chatstate.ErrGone) {
		t.Fatal("late context resurrected parent", err)
	}
}

func TestChatControlsArchiveStagesRevisionFiles(t *testing.T) {
	dir := t.TempDir()
	store := transcript.NewStore(filepath.Join(dir, "transcripts"))
	ref := chatstate.SessionRef{TaskID: 42, ExplorationID: 73, MainSeg: 2}
	revision, err := chatstate.PrepareTranscript(t.Context(), store, ref, nil)
	if err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(dir, "transcripts", "chat-rev-unrelated.jsonl")
	if err = os.WriteFile(unrelated, []byte("retained"), 0600); err != nil {
		t.Fatal(err)
	}
	stage, err := stageTaskArchiveFiles(dir, 987, "42", 73, revision)
	if err != nil {
		t.Fatal(err)
	}
	if len(stage.journal.Moves) != 2 {
		t.Fatal("missing revision manifest", stage.journal.Moves)
	}
	for _, suffix := range []string{".jsonl", ".jsonl.manifest.json"} {
		if _, err = os.Stat(filepath.Join(stage.payload, "files", "transcripts", revision.TranscriptID+suffix)); err != nil {
			t.Fatal(err)
		}
	}
	if err = stage.rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(unrelated); err != nil {
		t.Fatal("other chat files moved", err)
	}
	revision.SHA256 = "bad"
	if _, err = stageTaskArchiveFiles(dir, 988, "42", 73, revision); err == nil {
		t.Fatal("invalid manifest archived")
	}
}
