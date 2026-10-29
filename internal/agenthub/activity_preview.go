package agenthub

import (
	"context"
	"sync"
	"time"
)

// agentTurnMessages holds, per session, the top-level words of a turn the agent
// started by itself (SessionActivity.AgentTurn), for the session preview.
//
// Invoke keeps the preview of a turn DevKit started (writePreview); nothing wrote
// one for a turn the vendor began, so the row kept the sentence from before it. The
// buffer is the same lastAgentMessage Invoke uses, so there is one rule for what a
// message is. It exists only between the turn's spawn and its end, is bounded
// (previewBufferBytes) and is memory only. mu is a leaf lock: it is never held
// while another is taken, so forget (which runs under Service.mu) can drop one.
type agentTurnMessages struct {
	mu sync.Mutex
	m  map[string]*lastAgentMessage
}

func (a *agentTurnMessages) begin(sessionID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.m == nil {
		a.m = make(map[string]*lastAgentMessage)
	}
	a.m[sessionID] = &lastAgentMessage{}
}

func (a *agentTurnMessages) get(sessionID string) *lastAgentMessage {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.m[sessionID]
}

// take removes the session's buffer and returns it, nil when there is none.
func (a *agentTurnMessages) take(sessionID string) *lastAgentMessage {
	a.mu.Lock()
	defer a.mu.Unlock()
	msg := a.m[sessionID]
	delete(a.m, sessionID)
	return msg
}

// trackAgentTurnMessage follows the preview of an agent-started turn through one
// update; ch is what the update did to the session's activity.
//
//   - the turn spawns: a new, empty buffer (what came before is not this turn's)
//   - while it runs and no Invoke holds the session: observe the update. If an
//     Invoke does hold it, that Invoke owns the preview: the buffer is dropped
//   - the turn ends: write the preview from its last message, through the same
//     PreviewFromMessage and UpdateSessionPreview as Invoke. It runs before the end
//     is notified (noteActivityChange), which is what the window refetches the row on
//
// A process that goes away mid-turn (forgetBackgroundWork) drops the buffer: a message
// cut off in the middle is not a preview.
func (s *Service) trackAgentTurnMessage(ctx context.Context, sessionID string, u SessionUpdate, ch activityChange) {
	switch {
	case !ch.Before.AgentTurn && ch.After.AgentTurn:
		s.agentTurnMsgs.begin(sessionID)
	case ch.Before.AgentTurn && !ch.After.AgentTurn:
		msg := s.agentTurnMsgs.take(sessionID)
		if msg == nil || s.invokeClaimed(sessionID) {
			return
		}
		if preview := PreviewFromMessage(msg.String()); preview != "" {
			_ = s.store.UpdateSessionPreview(ctx, sessionID, preview, time.Now().UTC())
		}
	default:
		msg := s.agentTurnMsgs.get(sessionID)
		if msg == nil {
			return
		}
		if s.invokeClaimed(sessionID) {
			s.agentTurnMsgs.take(sessionID)
			return
		}
		msg.observe(u)
	}
}

// invokeClaimed reports whether an Invoke (or a rewind or restore, which also claim
// the session) holds it right now.
func (s *Service) invokeClaimed(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, held := s.invoking[sessionID]
	return held
}
