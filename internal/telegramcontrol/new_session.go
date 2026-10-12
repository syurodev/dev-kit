package telegramcontrol

import (
	"context"
	"errors"
	"slices"
	"strings"

	"devkit/internal/agenthub"
)

func (s *Service) newSessionCommand(ctx context.Context, c *Client, arg string) {
	parts := strings.Fields(arg)
	if len(parts) > 0 {
		if len(parts) < 2 || len(parts) > 4 {
			s.send(ctx, c, "Dùng /new hoặc /new <agent> <workspace ID> [model ID] [effort]", nil)
			return
		}
		sess, err := s.createRemoteSession(ctx, parts[1], parts[0])
		if err != nil {
			s.send(ctx, c, err.Error(), nil)
			return
		}
		s.send(ctx, c, "Đã tạo và chọn session "+sess.ID, nil)
		for i, kind := range []string{"model", "effort"} {
			if len(parts) > i+2 {
				if err := s.changeSetting(ctx, c, action{source: kind, session: sess.ID, value: parts[i+2]}); err != nil {
					s.send(ctx, c, "Session đã tạo, nhưng "+err.Error()+". Dùng /models hoặc /effort để chọn lại.", nil)
					return
				}
			}
		}
		if len(parts) == 2 {
			s.showSetting(ctx, c, "", "model", true)
		} else if len(parts) == 3 {
			s.showSetting(ctx, c, "", "effort", true)
		} else {
			s.send(ctx, c, "Session đã sẵn sàng. Gửi tin nhắn để bắt đầu.", nil)
		}
		return
	}
	for token, a := range s.actions {
		if strings.HasPrefix(a.source, "new:") {
			delete(s.actions, token)
		}
	}
	ws, err := s.deps.Workspaces.ListWorkspaces()
	if err != nil {
		s.send(ctx, c, "Không đọc được workspace.", nil)
		return
	}
	var buttons [][]Button
	for _, w := range ws {
		if s.allowed(w.ID) {
			buttons = append(buttons, []Button{s.button(clip(w.Name, 60), action{source: "new:workspace", id: w.ID})})
		}
	}
	if len(buttons) == 0 {
		s.send(ctx, c, "Chưa có workspace được phép. Chọn workspace trong Settings → Remote control.", nil)
		return
	}
	// Configuration bounds workspaces to 100; keep messages within a bounded menu.
	for start := 0; start < len(buttons); start += 10 {
		page := buttons[start:min(start+10, len(buttons))]
		msg := s.send(ctx, c, "Tạo session · chọn workspace", page)
		if msg != 0 {
			s.bind(page, msg)
		}
	}
}
func (s *Service) createRemoteSession(ctx context.Context, ws, vendor string) (agenthub.AgentSession, error) {
	if !s.allowed(ws) {
		return agenthub.AgentSession{}, errors.New("workspace không còn được phép")
	}
	if !slices.Contains(s.deps.Agents.ListVendorTypes(), agenthub.AgentType(vendor)) || agenthub.AgentType(vendor) == agenthub.AgentTypeMock {
		return agenthub.AgentSession{}, errors.New("agent chưa được đăng ký")
	}
	folders, err := s.deps.Workspaces.ListServices(ws)
	if err != nil || len(folders) == 0 {
		return agenthub.AgentSession{}, errors.New("workspace chưa có thư mục để tạo session")
	}
	sess, err := s.deps.Agents.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentType(vendor), WorkspaceID: ws, Cwd: folders[0].Path})
	if err != nil {
		return sess, errors.New("không tạo được session; kiểm tra agent trong DevKit")
	}
	s.selected = sess.ID
	s.before = 0
	s.readBuffer = ""
	s.record("create", sess.ID, true)
	return sess, nil
}
func (s *Service) completeNewSession(ctx context.Context, c *Client, a action) error {
	if a.source == "new:workspace" {
		if !s.allowed(a.id) {
			return errors.New("workspace không còn được phép")
		}
		// Consume the workspace menu; multiple taps cannot reopen old creation choices.
		for token, item := range s.actions {
			if item.source == "new:workspace" || item.source == "new:create" {
				delete(s.actions, token)
			}
		}
		var buttons [][]Button
		for _, vendor := range s.deps.Agents.ListVendorTypes() {
			if vendor != agenthub.AgentTypeMock {
				buttons = append(buttons, []Button{s.button(string(vendor), action{source: "new:create", id: a.id, value: string(vendor)})})
			}
		}
		if len(buttons) == 0 {
			return errors.New("chưa có agent được đăng ký")
		}
		msg := s.send(ctx, c, "Tạo session · chọn agent", buttons)
		if msg != 0 {
			s.bind(buttons, msg)
		}
		return nil
	}
	if a.source == "new:create" {
		// Consume before spawn: an uncertain creation must never be retried by a button.
		for token, item := range s.actions {
			if item.source == "new:create" {
				delete(s.actions, token)
			}
		}
		sess, err := s.createRemoteSession(ctx, a.id, a.value)
		if err != nil {
			return err
		}
		s.send(ctx, c, "Đã tạo và chọn session "+sess.ID, nil)
		s.showSetting(ctx, c, "", "model", true)
		return nil
	}
	if a.session != s.selected {
		return errors.New("session đã đổi; chọn lại session")
	}
	if _, err := s.session(ctx, a.session); err != nil {
		return err
	}
	if a.source == "new:effort" {
		for token, item := range s.actions {
			if item.session == a.session && item.source == "new:effort" {
				delete(s.actions, token)
			}
		}
		s.showSetting(ctx, c, "", "effort", true)
		return nil
	}
	if a.source == "new:ready" {
		for token, item := range s.actions {
			if item.session == a.session && strings.HasPrefix(item.source, "new:") {
				delete(s.actions, token)
			}
		}
		s.send(ctx, c, "Session đã sẵn sàng. Gửi tin nhắn để bắt đầu.", nil)
		return nil
	}
	return errors.New("yêu cầu tạo session không hợp lệ")
}
