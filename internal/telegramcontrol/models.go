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

// modelOption reads the session's advertised configuration rather than the
// draft catalog, which may offer different models and requires UI identity.
func (s *Service) modelOption(ctx context.Context, id string) (agenthub.ConfigOption, error) {
	sess, err := s.session(ctx, id)
	if err != nil {
		return agenthub.ConfigOption{}, err
	}
	if sess.Status != agenthub.SessionStatusIdle || s.ownTurns[id] {
		return agenthub.ConfigOption{}, errors.New("Chờ session kết thúc lượt chạy hoặc dùng /stop trước khi chuyển model.")
	}
	if err := s.deps.Agents.PrepareSession(ctx, id); err != nil {
		return agenthub.ConfigOption{}, errors.New("Không tải được model; kiểm tra session và đăng nhập trong DevKit.")
	}
	page, err := s.deps.Agents.TranscriptPage(ctx, id, 0, 1)
	if err != nil {
		return agenthub.ConfigOption{}, errors.New("Không đọc được cấu hình model của session.")
	}
	for _, entry := range append(page.State, page.Entries...) {
		if entry.Kind != agenthub.UpdateConfigOptions {
			continue
		}
		var update agenthub.SessionUpdate
		if json.Unmarshal(entry.Payload, &update) != nil {
			continue
		}
		for _, option := range update.ConfigOptions {
			if (option.Category == "model" || option.ID == "model") && (option.Type == "select" || option.Type == "") && len(option.Options) > 0 {
				return option, nil
			}
		}
	}
	return agenthub.ConfigOption{}, errors.New("Agent chưa cung cấp danh sách model cho session này.")
}
func modelFingerprint(option agenthub.ConfigOption) string {
	raw, _ := json.Marshal(option)
	return fingerprint(raw)
}
func (s *Service) showModels(ctx context.Context, c *Client, arg string) {
	option, err := s.modelOption(ctx, s.selected)
	if err != nil {
		s.send(ctx, c, err.Error(), nil)
		return
	}
	// Replace previous menus so old callbacks cannot accumulate or select elsewhere.
	for token, a := range s.actions {
		if a.source == "model" {
			delete(s.actions, token)
		}
	}
	offset, _ := strconv.Atoi(arg)
	if offset < 0 || offset >= len(option.Options) {
		offset = 0
	}
	var current string
	_ = json.Unmarshal(option.CurrentValue, &current)
	text := fmt.Sprintf("Model của session %s\nHiện tại: %s\n", s.selected, current)
	var buttons [][]Button
	end := min(offset+10, len(option.Options))
	for _, value := range option.Options[offset:end] {
		name := value.Name
		if name == "" {
			name = value.Value
		}
		text += "\n" + clip(name, 100) + " — " + clip(value.Value, 150)
		label := name
		if value.Value == current {
			label = "✓ " + label
		}
		buttons = append(buttons, []Button{s.button(clip(label, 60), action{source: "model", session: s.selected, id: option.ID, value: value.Value, fingerprint: modelFingerprint(option)})})
	}
	text += "\n\nBấm nút hoặc dùng /model <model ID>."
	if end < len(option.Options) {
		text += fmt.Sprintf("\n/models %d để xem tiếp.", end)
	}
	msg := s.send(ctx, c, text, buttons)
	if msg != 0 {
		s.bind(buttons, msg)
	}
}
func (s *Service) changeModel(ctx context.Context, c *Client, a action) error {
	if a.session == "" || a.session != s.selected {
		return errors.New("Session đã đổi; dùng /models để chọn lại.")
	}
	option, err := s.modelOption(ctx, a.session)
	if err != nil {
		return err
	}
	if a.fingerprint != "" && (a.id != option.ID || a.fingerprint != modelFingerprint(option)) {
		return errors.New("Danh sách model đã thay đổi; dùng /models để tải lại.")
	}
	advertised := false
	for _, value := range option.Options {
		if value.Value == a.value {
			advertised = true
			break
		}
	}
	if !advertised {
		return errors.New("Model không có trong danh sách của session. Dùng /models.")
	}
	// Recheck lifecycle after preparing/reading: these calls can resume a runtime.
	sess, err := s.session(ctx, a.session)
	if err != nil {
		return err
	}
	if sess.Status != agenthub.SessionStatusIdle || s.ownTurns[a.session] {
		return errors.New("Session đang bận; chờ lượt chạy kết thúc.")
	}
	if err := s.deps.Agents.SetConfigOption(ctx, a.session, option.ID, a.value); err != nil {
		return errors.New("Không chuyển được model; xem chi tiết trong DevKit.")
	}
	for token, item := range s.actions {
		if item.source == "model" && item.session == a.session {
			delete(s.actions, token)
		}
	}
	s.send(ctx, c, "Đã chuyển model sang "+a.value+"\nSession: "+a.session, nil)
	return nil
}
func (s *Service) modelCommand(ctx context.Context, c *Client, arg string) {
	value := strings.TrimSpace(arg)
	if value == "" {
		s.showModels(ctx, c, "")
		return
	}
	a := action{source: "model", session: s.selected, value: value}
	if err := s.changeModel(ctx, c, a); err != nil {
		s.send(ctx, c, err.Error(), nil)
		s.record("model", s.selected, false)
		return
	}
	s.record("model", s.selected, true)
}
