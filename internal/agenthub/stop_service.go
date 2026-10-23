package agenthub

import (
	"context"
	"log"
	"time"
	"unicode/utf8"

	"devkit/internal/identity"

	"github.com/google/uuid"
)

// recordStop stores why a turn did not complete and, when the session has a
// parent that did not just receive the error as its own call's result, tells the
// parent once. A cancel is recorded (the status says "cancelled") but never
// announced: whoever cancelled knows.
func (s *Service) recordStop(ctx context.Context, sess AgentSession, turnErr error, timedOut bool, invoker identity.Caller) {
	stop := StopFromError(turnErr, time.Now(), timedOut)
	// A plain failure on an account whose stream just said it is at its limit
	// is that limit (Claude's refusal carries no sentinel; account_limit.go).
	stop = s.upgradeStopForReachedWindow(ctx, sess, stop)
	// A real limit keeps the account the turn ran as out until it is usable
	// again (account_limit.go). sess is the session after any account move.
	s.markAccountLimited(ctx, sess, stop)
	// A dead sign-in keeps the account out until it is signed in again.
	s.noteSignedOut(ctx, sess, stop, turnErr)
	if err := s.store.UpdateSessionStop(ctx, sess.ID, &stop, stop.Since); err != nil {
		log.Printf("agenthub: recording why session %s stopped: %v", sess.ID, err)
		return
	}
	s.notifyParentOfStop(sess, stop, invoker)
}

// stopRecovered is a turn that completed: the failure the parent was told about
// is over, so a later failure is news again.
func (s *Service) stopRecovered(sessionID string) {
	s.peers.mu.Lock()
	defer s.peers.mu.Unlock()
	s.peers.init()
	delete(s.peers.stopNoticed, sessionID)
}

// notifyParentOfStop sends a child's parent one notice per failure, through the
// ordinary delivery path (a busy parent is steered or queued, an idle one wakes).
// It is DevKit's notice, not a message from the child, so it is not a hop of an
// exchange: it is not counted against, or refused by, the hop and rate caps.
func (s *Service) notifyParentOfStop(child AgentSession, stop SessionStop, invoker identity.Caller) {
	if child.ParentSessionID == nil || stop.Kind == StopCancelled || stop.Kind == StopInterruptedByRestart {
		return
	}
	parentID := *child.ParentSessionID
	// The parent that invoked the turn itself and waited has the error as that
	// call's result; a second telling would be noise.
	if invoker.IsAgent() && invoker.SessionID == parentID {
		return
	}
	fingerprint := string(stop.Kind) + "\x00" + stop.Message
	s.peers.mu.Lock()
	s.peers.init()
	if s.peers.stopNoticed[child.ID] == fingerprint {
		s.peers.mu.Unlock()
		return
	}
	s.peers.stopNoticed[child.ID] = fingerprint
	s.peers.mu.Unlock()

	s.turnWork.Add(1)
	go func() {
		defer s.turnWork.Add(-1)
		ctx := context.Background()
		parent, err := s.store.GetSession(ctx, parentID)
		if err != nil || parent.Status == SessionStatusClosed {
			return
		}
		text := stopNoticeText(child, stop, time.Now())
		id := uuid.NewString()
		msg := &peerMessage{
			id:     id,
			from:   MessageSource{Kind: MessageKindDevKit, SessionID: child.ID, AgentType: child.AgentType, Name: sessionLabel(child), MessageID: id, Hop: 1},
			to:     parentID,
			text:   text,
			at:     s.peers.clock(),
			length: utf8.RuneCountInString(text),
			limit:  MaxPeerHops,
		}
		delivery, code, _, err := s.deliverPeer(ctx, msg)
		if err != nil {
			delivery, code = DeliveryRefused, PeerUndelivery
		}
		if delivery == DeliveryRefused {
			// Not delivered: let the next failure try again.
			s.peers.mu.Lock()
			if s.peers.stopNoticed[child.ID] == fingerprint {
				delete(s.peers.stopNoticed, child.ID)
			}
			s.peers.mu.Unlock()
			log.Printf("agenthub: telling session %s that its child %s stopped: %s %v", parentID, child.ID, code, err)
		}
		s.peerEvent(PeerMessageEvent{MessageID: id, From: msg.from, To: parentID, Length: msg.length, Delivery: delivery, Code: code})
	}()
}
