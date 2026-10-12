package telegramcontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"devkit/internal/agenthub"
)

// Session controls use live advertised settings, not the UI-only draft catalog.
func (s *Service) currentSetting(ctx context.Context, id, kind string) (agenthub.ConfigOption, string, error) {
	sess, err := s.session(ctx, id)
	if err != nil {
		return agenthub.ConfigOption{}, "", err
	}
	if sess.Status != agenthub.SessionStatusIdle || s.ownTurns[id] {
		return agenthub.ConfigOption{}, "", errors.New("chờ session kết thúc lượt chạy hoặc dùng /stop trước khi đổi cấu hình")
	}
	if err := s.deps.Agents.PrepareSession(ctx, id); err != nil {
		return agenthub.ConfigOption{}, "", errors.New("không tải được cấu hình; kiểm tra session và đăng nhập trong DevKit")
	}
	page, err := s.deps.Agents.TranscriptPage(ctx, id, 0, 1)
	if err != nil {
		return agenthub.ConfigOption{}, "", errors.New("không đọc được cấu hình của session")
	}
	for _, entry := range append(page.State, page.Entries...) {
		if entry.Kind != agenthub.UpdateConfigOptions {
			continue
		}
		var update agenthub.SessionUpdate
		if json.Unmarshal(entry.Payload, &update) != nil {
			continue
		}
		var model json.RawMessage
		for _, opt := range update.ConfigOptions {
			if opt.Category == "model" || opt.ID == "model" {
				model = opt.CurrentValue
			}
		}
		for _, opt := range update.ConfigOptions {
			matches := kind == "model" && (opt.Category == "model" || opt.ID == "model") || kind == "effort" && (opt.Category == "thought_level" || opt.ID == "reasoning_effort" || opt.ID == "effort")
			if matches && (opt.Type == "select" || opt.Type == "") && len(opt.Options) > 0 {
				raw, _ := json.Marshal(struct {
					Option agenthub.ConfigOption
					Model  json.RawMessage
				}{opt, model})
				return opt, fingerprint(raw), nil
			}
		}
	}
	return agenthub.ConfigOption{}, "", fmt.Errorf("agent chưa cung cấp lựa chọn %s cho model hiện tại", kind)
}
func (s *Service) showModels(ctx context.Context, c *Client, arg string) {
	s.showSetting(ctx, c, arg, "model", false)
}
func (s *Service) showSetting(ctx context.Context, c *Client, arg, kind string, creating bool) {
	opt, stamp, err := s.currentSetting(ctx, s.selected, kind)
	if err != nil {
		s.send(ctx, c, err.Error(), nil)
		if creating {
			s.send(ctx, c, "Session đã sẵn sàng. Gửi tin nhắn để bắt đầu.", nil)
		}
		return
	}
	for token, a := range s.actions {
		if a.source == kind {
			delete(s.actions, token)
		}
	}
	offset, _ := strconv.Atoi(arg)
	if offset < 0 || offset >= len(opt.Options) {
		offset = 0
	}
	var current string
	_ = json.Unmarshal(opt.CurrentValue, &current)
	label := "Model"
	listCommand := "/models"
	if kind == "effort" {
		label = "Effort"
		listCommand = "/efforts"
	}
	text := fmt.Sprintf("%s của session %s\nHiện tại: %s\n", label, s.selected, current)
	var buttons [][]Button
	end := min(offset+10, len(opt.Options))
	for _, value := range opt.Options[offset:end] {
		name := value.Name
		if name == "" {
			name = value.Value
		}
		text += "\n" + clip(name, 100) + " — " + clip(value.Value, 150)
		if value.Value == current {
			name = "✓ " + name
		}
		field := ""
		if creating {
			field = "new"
		}
		buttons = append(buttons, []Button{s.button(clip(name, 60), action{source: kind, session: s.selected, id: opt.ID, value: value.Value, fingerprint: stamp, field: field})})
	}
	text += "\n\nBấm nút hoặc dùng /" + kind + " <ID>."
	if end < len(opt.Options) {
		text += fmt.Sprintf("\n%s %d để xem tiếp.", listCommand, end)
	}
	if creating {
		source := "new:ready"
		if kind == "model" {
			source = "new:effort"
		}
		buttons = append(buttons, []Button{s.button("Giữ cấu hình hiện tại", action{source: source, session: s.selected})})
	}
	msg := s.send(ctx, c, text, buttons)
	if msg != 0 {
		s.bind(buttons, msg)
	}
}
func (s *Service) changeModel(ctx context.Context, c *Client, a action) error {
	return s.changeSetting(ctx, c, a)
}
func (s *Service) changeSetting(ctx context.Context, c *Client, a action) error {
	kind := a.source
	if kind != "model" && kind != "effort" {
		return errors.New("cấu hình không hợp lệ")
	}
	if a.session == "" || a.session != s.selected {
		return errors.New("session đã đổi; tải lại danh sách lựa chọn")
	}
	opt, stamp, err := s.currentSetting(ctx, a.session, kind)
	if err != nil {
		return err
	}
	if a.fingerprint != "" && (a.id != opt.ID || a.fingerprint != stamp) {
		return errors.New("cấu hình đã thay đổi; tải lại danh sách lựa chọn")
	}
	advertised := false
	for _, value := range opt.Options {
		if value.Value == a.value {
			advertised = true
			break
		}
	}
	if !advertised {
		return errors.New("giá trị không có trong danh sách của session")
	}
	sess, err := s.session(ctx, a.session)
	if err != nil {
		return err
	}
	if sess.Status != agenthub.SessionStatusIdle || s.ownTurns[a.session] {
		return errors.New("session đang bận; chờ lượt chạy kết thúc")
	}
	if err := s.deps.Agents.SetConfigOption(ctx, a.session, opt.ID, a.value); err != nil {
		return fmt.Errorf("không chuyển được %s; xem chi tiết trong DevKit", kind)
	}
	for token, item := range s.actions {
		if item.session == a.session && (item.source == kind || kind == "model" && item.source == "effort") {
			delete(s.actions, token)
		}
	}
	s.send(ctx, c, "Đã chuyển "+kind+" sang "+a.value+"\nSession: "+a.session, nil)
	if a.field == "new" {
		if kind == "model" {
			s.showSetting(ctx, c, "", "effort", true)
		} else {
			s.send(ctx, c, "Session đã sẵn sàng. Gửi tin nhắn để bắt đầu.", nil)
		}
	}
	return nil
}
func (s *Service) modelCommand(ctx context.Context, c *Client, arg string) {
	s.settingCommand(ctx, c, arg, "model")
}
func (s *Service) settingCommand(ctx context.Context, c *Client, arg, kind string) {
	value := strings.TrimSpace(arg)
	if value == "" {
		s.showSetting(ctx, c, "", kind, false)
		return
	}
	a := action{source: kind, session: s.selected, value: value}
	if err := s.changeSetting(ctx, c, a); err != nil {
		s.send(ctx, c, err.Error(), nil)
		s.record(kind, s.selected, false)
		return
	}
	s.record(kind, s.selected, true)
}
