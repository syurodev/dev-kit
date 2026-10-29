package agenthub

import "context"

// ModeRead says what reading a session's permission mode came to. The three
// answers are kept apart on purpose: a comparison may pass a session that has no
// mode concept, but never one whose mode could not be read.
type ModeRead int

const (
	// ModeUnreadable: the session or its recorded state could not be read, or
	// nothing says what its mode is (no live runtime and nothing recorded yet).
	// It is never evidence that the session has no mode.
	ModeUnreadable ModeRead = iota
	// ModeNone: the session's live runtime has no permission-mode concept.
	ModeNone
	// ModeKnown: the mode id is returned.
	ModeKnown
)

// ReadPermissionMode is the permission mode id a session is in now: its live
// runtime's, else the last one its transcript recorded (the same source a new
// child inherits from).
func (s *Service) ReadPermissionMode(ctx context.Context, sessionID string) (modeID string, read ModeRead) {
	if sessionID == "" {
		return "", ModeUnreadable
	}
	sess, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		return "", ModeUnreadable
	}
	if rt := s.liveRuntime(ctx, sess.ID); rt != nil {
		withModes, ok := rt.(RuntimeModes)
		if !ok {
			return "", ModeNone
		}
		if modes := withModes.Modes(); modes != nil && modes.CurrentModeID != "" {
			return modes.CurrentModeID, ModeKnown
		}
	}
	// The newest announcement may still be in the recorder's buffer.
	if err = s.recorder.Flush(ctx, sess.ID); err != nil {
		return "", ModeUnreadable
	}
	entry, found, err := s.store.FindTranscriptKind(ctx, sess.ID, UpdateCurrentMode)
	if err != nil || !found {
		return "", ModeUnreadable
	}
	if id := storedUpdate(entry).Mode; id != "" {
		return id, ModeKnown
	}
	return "", ModeUnreadable
}

// PermissionModeOf is ReadPermissionMode folded to "known or not". Callers that
// must tell "no mode concept" from "unreadable" use ReadPermissionMode.
func (s *Service) PermissionModeOf(ctx context.Context, sessionID string) (modeID string, known bool) {
	mode, read := s.ReadPermissionMode(ctx, sessionID)
	return mode, read == ModeKnown
}

// ModeNotWiderThan reports whether child's permission mode lets the agent do no
// more without asking than parent's, over the same ranking a child inherits by.
// The two names of the strictest asking mode (Claude's default, Codex's ask) are
// equal.
//
// known is false when the ranks could not be compared: ok is then true only for
// a parent that has no mode concept at all (nothing limits the child), and false
// for a mode that is not in the ranking or a child whose mode cannot be read
// while the parent has one, so the comparison fails closed. The same unranked id
// on both sides is carried over as is and passes, like an inherited one.
func ModeNotWiderThan(parent, child string) (ok, known bool) {
	switch {
	case parent == "" && child == "":
		return true, true
	case parent == "":
		return true, false
	case child == "":
		return false, false
	case parent == child:
		return true, true
	}
	pr, cr := modeRank(parent), modeRank(child)
	if pr < 0 || cr < 0 {
		return false, false
	}
	return cr <= pr, true
}
