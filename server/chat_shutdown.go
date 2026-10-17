package server

import (
	"context"
	"errors"
	"github.com/Autumn-27/artex/sidequestion"
)

// DrainChats refuses further dispatches and waits for admitted background chats,
// including side answers, checkpoint publication and run finalization. The caller must cancel the
// server context and drain HTTP handlers first, and keep the database open until
// this method returns. A deadline bounds uncooperative tools or providers.
func (s *Server) DrainChats(ctx context.Context) error {
	s.chatMu.Lock()
	s.chatClosing = true
	if s.chatDrainDone == nil {
		s.chatDrainDone = make(chan struct{})
		go func() {
			// Side admission and run registration already serialize under commands.
			// Close that barrier before taking the completion-channel snapshot.
			var sideDone []<-chan struct{}
			if s.side != nil {
				s.side.commands.Lock()
				s.side.closing = true
				sideDone = s.cancelSideWhere(func(sidequestion.Parent) bool { return true })
				s.side.commands.Unlock()
			}
			s.chatWG.Wait()
			for _, ch := range sideDone {
				<-ch
			}
			if s.side != nil {
				<-s.side.done
				// Chat unwind may publish after the periodic publisher's cancel flush.
				// It is now stopped and all admitted runs have finished publishing.
				s.chatDrainErr = errors.Join(s.flushSideSnapshots(), s.flushChatContexts())
			}
			close(s.chatDrainDone)
		}()
	}
	done := s.chatDrainDone
	s.chatMu.Unlock()
	select {
	case <-done:
		return s.chatDrainErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
