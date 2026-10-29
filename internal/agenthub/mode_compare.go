package agenthub

import "context"

// PermissionModeOf is the permission mode id a session is in now: its live
// runtime's, else the last one its transcript recorded (the same source a new
// child inherits from). known is false for a session without a mode concept
// (a vendor that has none), an unknown or empty id, or one whose state cannot be
// read; the caller must treat that as "cannot say", never as the widest or the
// strictest mode.
func (s *Service) PermissionModeOf(ctx context.Context, sessionID string) (modeID string, known bool) {
	if sessionID == "" {
		return "", false
	}
	sess, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		return "", false
	}
	mode, ok := s.currentMode(ctx, sess)
	if !ok || mode.ID == "" {
		return "", false
	}
	return mode.ID, true
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
