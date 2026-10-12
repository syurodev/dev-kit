package telegramcontrol

import (
	"context"
	"errors"
	"slices"
)

// SetSessionEnabled is called only by the desktop bridge. Workspace grants still apply.
func (s *Service) SetSessionEnabled(ctx context.Context, id string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if enabled {
		if s.deps.Agents == nil || s.deps.Locked() {
			return errors.New("unlock DevKit before enabling remote control")
		}
		sess, err := s.deps.Agents.GetStatus(ctx, id)
		if err != nil || !s.allowed(sess.WorkspaceID) {
			return errors.New("choose an allowed workspace in Remote control settings first")
		}
	}
	return s.setSessionEnabledLocked(ctx, id, enabled)
}
func (s *Service) setSessionEnabledLocked(ctx context.Context, id string, enabled bool) error {
	if id == "" {
		return errors.New("session is required")
	}
	if enabled && slices.Contains(s.state.Config.SessionIDs, id) {
		return nil
	}
	ids := slices.Clone(s.state.Config.SessionIDs)
	ids = slices.DeleteFunc(ids, func(value string) bool { return value == id })
	if enabled {
		if len(ids) >= 500 {
			list, err := s.deps.Agents.ListSessions(ctx)
			if err != nil {
				return errors.New("could not check existing remote sessions")
			}
			live := make(map[string]bool, len(list))
			for _, session := range list {
				live[session.ID] = true
			}
			ids = slices.DeleteFunc(ids, func(value string) bool { return !live[value] })
		}
		if len(ids) >= 500 {
			return errors.New("too many remote sessions")
		}
		ids = append(ids, id)
	}
	next := s.state
	next.Config.SessionIDs = ids
	if err := s.store.Save(next); err != nil {
		return err
	}
	s.state = next
	if !enabled {
		delete(s.observed, id)
		if s.selected == id {
			s.selected = ""
			s.before = 0
			s.readBuffer = ""
		}
		for token, a := range s.actions {
			if a.session == id {
				delete(s.actions, token)
			}
		}
		for message, f := range s.replies {
			if f.session == id {
				delete(s.replies, message)
			}
		}
		for key, d := range s.sent {
			if d.session == id {
				d.revoked = true
				s.sent[key] = d
			}
		}
	}
	return nil
}
