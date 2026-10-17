package server

import (
	"context"
	"github.com/Autumn-27/artex/chatstate"
	"github.com/Autumn-27/artex/locale"
	"github.com/Autumn-27/norma/llm"
	"strings"
	"testing"
)

func TestChatQueueAcceptedLocaleSurvivesDispatch(t *testing.T) {
	f := newHistoryFixture(t)
	seen := locale.En
	setRetestProvider(f.s, retestProvider{complete: func(ctx context.Context, _ llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
		seen = locale.FromContext(ctx)
		return llm.UserText("answer"), "end_turn", llm.Usage{}, nil
	}})
	input, err := f.s.m.pg.AdmitChatInput(locale.WithLang(t.Context(), locale.Ko), f.ref, "locale-once", "queue", "", chatstate.Prompt{Text: "original text"})
	if err != nil {
		t.Fatal(err)
	}
	same, err := f.s.m.pg.AdmitChatInput(locale.WithLang(t.Context(), locale.ZhHans), f.ref, "locale-once", "queue", "", input.Prompt)
	if err != nil || same.ID != input.ID {
		t.Fatal(same, err)
	}
	f.s.dispatchChatGate(f.ref.GateKey())
	waitRetestIdle(t, f.s)
	if seen != locale.Ko {
		t.Fatal("accepted language lost before execution", seen)
	}
}
func TestChatQueuePreflightFailureVisibleAndEditable(t *testing.T) {
	f := newHistoryFixture(t)
	f.s.chatAgent = nil
	_, err := f.s.m.pg.AdmitChatInput(t.Context(), f.ref, "no-profile", "queue", "", chatstate.Prompt{Text: "visible failed prompt"})
	if err != nil {
		t.Fatal(err)
	}
	f.s.dispatchChatGate(f.ref.GateKey())
	waitRetestIdle(t, f.s)
	var users, errors, mutable int
	err = f.s.m.pg.QueryRow(`SELECT count(*) FILTER(WHERE kind='user'),count(*) FILTER(WHERE is_error),count(*) FILTER(WHERE kind='user' AND history_mutable) FROM conversation_activities WHERE conversation_id=$1`, f.ref.ConversationID).Scan(&users, &errors, &mutable)
	if err != nil || users != 1 || errors < 1 || mutable != 1 {
		t.Fatal("accepted failed prompt invisible or cannot be corrected", users, errors, mutable, err)
	}
}

func TestChatQueuePreflightLocale(t *testing.T) {
	f := newHistoryFixture(t)
	f.s.chatAgent = nil
	_, err := f.s.m.pg.AdmitChatInput(locale.WithLang(t.Context(), locale.Ko), f.ref, "localized-failure", "queue", "", chatstate.Prompt{Text: "original text"})
	if err != nil {
		t.Fatal(err)
	}
	f.s.dispatchChatGate(f.ref.GateKey())
	waitRetestIdle(t, f.s)
	var detail string
	if err = f.s.m.pg.QueryRow(`SELECT detail FROM conversation_activities WHERE conversation_id=$1 AND is_error ORDER BY id DESC LIMIT 1`, f.ref.ConversationID).Scan(&detail); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail, "대화를 시작할 수 없습니다") {
		t.Fatal("accepted locale not used for preflight feedback", detail)
	}
}
