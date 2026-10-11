package telegramcontrol

import (
	"context"
	"crypto/sha256"
	"devkit/internal/agenthub"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

func fingerprint(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
func (s *Service) bind(buttons [][]Button, msg int64) {
	for _, row := range buttons {
		for _, b := range row {
			a := s.actions[b.Data]
			a.message = msg
			s.actions[b.Data] = a
		}
	}
}
func (s *Service) reconcile(ctx context.Context, c *Client) {
	live := map[string]bool{}
	sessions, err := s.deps.Agents.ListSessions(ctx)
	if err == nil {
		next := map[string]agenthub.SessionStatus{}
		for _, sess := range sessions {
			if !s.allowed(sess.WorkspaceID) {
				continue
			}
			next[sess.ID] = sess.Status
			previous := s.observed[sess.ID]
			if !s.ownTurns[sess.ID] && (previous == agenthub.SessionStatusRunning || previous == agenthub.SessionStatusAwaitingPermission) && (sess.Status == agenthub.SessionStatusIdle || sess.Status == agenthub.SessionStatusError) {
				s.send(ctx, c, "Agent đã kết thúc lượt chạy: "+string(sess.Status)+"\nSession: "+sess.ID+"\nDùng /use "+sess.ID+" rồi /read để xem nội dung.", nil)
			}
		}
		s.observed = next
	}

	for _, p := range s.deps.Permissions.PendingList() {
		if _, err := s.session(ctx, p.SessionID); err != nil {
			continue
		}
		key := "permission:" + p.ID
		live[key] = true
		fp := fingerprint(p.Params)
		if _, ok := s.sent[key]; ok {
			continue
		}
		if !s.state.Config.SendDetails {
			continue
		}
		var req struct {
			Origin  string `json:"origin"`
			Summary string `json:"summary"`
			Env     string `json:"env"`
			Options []struct {
				ID   string `json:"optionId"`
				Name string `json:"name"`
				Kind string `json:"kind"`
			} `json:"options"`
		}
		if json.Unmarshal(p.Params, &req) != nil {
			continue
		}
		title := "Quyền agent"
		if req.Origin == "devkit_gate" {
			title = "Xác nhận DevKit"
		}
		text := title + "\nSession: " + p.SessionID + "\n" + string(p.Params)
		if !p.ExpiresAt.IsZero() {
			text += "\nHết hạn: " + p.ExpiresAt.Format(time.RFC3339)
		}
		// Never allow after omitting action context. Large requests need desktop review.
		var buttons [][]Button
		if len([]rune(text)) <= 3500 {
			for _, o := range req.Options {
				if o.Kind == "allow_once" || o.Kind == "reject_once" {
					label := "Cho phép lần này"
					if o.Kind == "reject_once" {
						label = "Từ chối"
					}
					buttons = append(buttons, []Button{s.button(label, action{source: "permission", id: p.ID, value: o.ID, session: p.SessionID, fingerprint: fp})})
				}
			}
		} else {
			text = "Yêu cầu quyền quá dài để duyệt qua Telegram. Xem trên desktop.\nSession: " + p.SessionID
		}
		msg := s.send(ctx, c, text, buttons)
		if msg != 0 {
			s.bind(buttons, msg)
			s.sent[key] = delivery{message: msg, fingerprint: fp}
		}
	}
	for _, q := range s.deps.Questions.PendingList() {
		if _, err := s.session(ctx, q.SessionID); err != nil {
			continue
		}
		key := "question:" + q.ID
		live[key] = true
		if _, ok := s.sent[key]; ok || !s.state.Config.SendDetails {
			continue
		}
		f, err := newForm(q.ID, q.SessionID, q.Params)
		if err != nil {
			msg := s.send(ctx, c, "Câu hỏi cần xử lý trên desktop (schema chưa hỗ trợ).\nSession: "+q.SessionID, nil)
			if msg != 0 {
				s.sent[key] = delivery{message: msg}
			}
			continue
		}
		buttons := [][]Button{{s.button("Trả lời", action{source: "question:start", id: q.ID, session: q.SessionID, fingerprint: f.fp})}, {s.button("Bỏ qua", action{source: "question:decline", id: q.ID, session: q.SessionID, fingerprint: f.fp}), s.button("Hủy", action{source: "question:cancel", id: q.ID, session: q.SessionID, fingerprint: f.fp})}}
		msg := s.send(ctx, c, "Câu hỏi agent\nSession: "+q.SessionID+"\n"+q.Message, buttons)
		if msg != 0 {
			s.bind(buttons, msg)
			s.sent[key] = delivery{message: msg, fingerprint: f.fp}
		}
	}
	if s.state.Config.ApproveInteractions {
		for _, p := range s.deps.Interaction.PendingList() {
			if p.Type != "confirm" {
				continue
			}
			key := "interaction:" + p.ID
			live[key] = true
			if _, ok := s.sent[key]; ok || !s.state.Config.SendDetails {
				continue
			}
			raw, _ := json.Marshal(p)
			fp := fingerprint(raw)
			text := "Xác nhận DevKit trên máy này\nBên gọi: " + p.Caller + "\n" + p.Tool + "\n" + p.Summary + "\nĐích: " + string(raw) + "\nMôi trường: " + p.Env + "\nHết hạn: " + p.ExpiresAt.Format(time.RFC3339)
			var buttons [][]Button
			if len([]rune(text)) <= 3500 {
				buttons = [][]Button{{s.button("Cho phép lần này", action{source: "interaction", id: p.ID, value: "allow", fingerprint: fp}), s.button("Từ chối", action{source: "interaction", id: p.ID, value: "deny", fingerprint: fp})}}
			} else {
				text = "Xác nhận quá dài; xem trên desktop."
			}
			msg := s.send(ctx, c, text, buttons)
			if msg != 0 {
				s.bind(buttons, msg)
				s.sent[key] = delivery{message: msg, fingerprint: fp}
			}
		}
	}
	for key, d := range s.sent {
		if live[key] {
			continue
		}
		_ = c.Do(ctx, "editMessageReplyMarkup", map[string]any{"chat_id": s.state.ChatID, "message_id": d.message, "reply_markup": map[string]any{"inline_keyboard": [][]Button{}}}, nil)
		delete(s.sent, key)
		for token, a := range s.actions {
			if a.source+":"+a.id == key || (strings.HasPrefix(a.source, "question:") && key == "question:"+a.id) {
				delete(s.actions, token)
			}
		}
		for msg, f := range s.replies {
			if key == "question:"+f.id {
				delete(s.replies, msg)
			}
		}
	}
}
func (s *Service) complete(ctx context.Context, c *Client, a action) error {
	stale := errors.New("Yêu cầu đã xử lý hoặc hết hiệu lực")
	if a.source == "model" {
		return s.changeModel(ctx, c, a)
	}
	if a.source == "select" {
		if _, err := s.session(ctx, a.session); err != nil {
			return err
		}
		s.selected = a.session
		s.before = 0
		s.readBuffer = ""
		s.send(ctx, c, "Đã chọn session "+a.session, nil)
		return nil
	}
	if a.session != "" {
		if _, err := s.session(ctx, a.session); err != nil {
			return err
		}
	}
	switch a.source {
	case "permission":
		for _, p := range s.deps.Permissions.PendingList() {
			if p.ID != a.id || fingerprint(p.Params) != a.fingerprint {
				continue
			}
			var req struct {
				Options []struct {
					ID   string `json:"optionId"`
					Kind string `json:"kind"`
				} `json:"options"`
			}
			if json.Unmarshal(p.Params, &req) != nil {
				return stale
			}
			for _, o := range req.Options {
				if o.ID == a.value && (o.Kind == "allow_once" || o.Kind == "reject_once") {
					raw, _ := json.Marshal(map[string]any{"outcome": map[string]string{"outcome": "selected", "optionId": o.ID}})
					return s.deps.Permissions.Complete(a.id, raw)
				}
			}
			return stale
		}
	case "interaction":
		if !s.state.Config.ApproveInteractions {
			return stale
		}
		for _, p := range s.deps.Interaction.PendingList() {
			raw, _ := json.Marshal(p)
			if p.ID == a.id && fingerprint(raw) == a.fingerprint && p.Type == "confirm" && (a.value == "allow" || a.value == "deny") {
				return s.deps.Interaction.Complete(a.id, a.value)
			}
		}
	default:
		for _, q := range s.deps.Questions.PendingList() {
			if q.ID != a.id || fingerprint(q.Params) != a.fingerprint {
				continue
			}
			if a.source == "question:decline" || a.source == "question:cancel" {
				raw, _ := json.Marshal(map[string]string{"action": strings.TrimPrefix(a.source, "question:")})
				return s.deps.Questions.Complete(q.ID, raw)
			}
			if a.source == "question:start" {
				f, err := newForm(q.ID, q.SessionID, q.Params)
				if err != nil {
					return err
				}
				for msg, old := range s.replies {
					if old.id == f.id {
						delete(s.replies, msg)
					}
				}
				s.askField(ctx, c, f)
				return nil
			}
			if a.source == "question:field" {
				f := s.replies[a.message]
				if f == nil || f.id != a.id || f.index >= len(f.fields) || f.fields[f.index] != a.field {
					return stale
				}
				text := a.value
				if f.props[a.field].Type == "string" {
					if json.Unmarshal([]byte(a.value), &text) != nil {
						return stale
					}
				}
				s.answerField(ctx, c, f, text)
				return nil
			}
			if a.source == "question:submit" {
				f := s.replies[a.message]
				if f == nil || f.id != a.id || f.index != len(f.fields) || f.fp != a.fingerprint {
					return stale
				}
				raw, _ := json.Marshal(map[string]any{"action": "accept", "content": f.answers})
				return s.deps.Questions.Complete(a.id, raw)
			}
		}
	}
	return stale
}

type property struct {
	Type        string            `json:"type"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Enum        []json.RawMessage `json:"enum"`
}
type form struct {
	id, session, fp string
	fields          []string
	props           map[string]property
	answers         map[string]any
	index           int
}

func newForm(id, session string, raw json.RawMessage) (*form, error) {
	var req struct {
		Mode   string `json:"mode"`
		Schema struct {
			Type       string                     `json:"type"`
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
		} `json:"requestedSchema"`
	}
	if json.Unmarshal(raw, &req) != nil || req.Mode != "form" || req.Schema.Type != "object" || len(req.Schema.Properties) > 20 {
		return nil, errors.New("Schema chưa hỗ trợ")
	}
	var root map[string]json.RawMessage
	var rootReq map[string]json.RawMessage
	_ = json.Unmarshal(raw, &rootReq)
	_ = json.Unmarshal(rootReq["requestedSchema"], &root)
	for key := range root {
		switch key {
		case "type", "properties", "required", "title", "description", "additionalProperties":
		default:
			return nil, errors.New("Schema cần xử lý trên desktop")
		}
	}
	f := &form{id: id, session: session, fp: fingerprint(raw), props: map[string]property{}, answers: map[string]any{}}
	for name, data := range req.Schema.Properties {
		// Unsupported validation keywords stay on desktop instead of silently weakening them.
		var keys map[string]json.RawMessage
		_ = json.Unmarshal(data, &keys)
		for key := range keys {
			switch key {
			case "type", "title", "description", "enum", "default", "items":
			default:
				return nil, errors.New("Ràng buộc cần xử lý trên desktop")
			}
		}
		var p property
		if json.Unmarshal(data, &p) != nil {
			return nil, errors.New("Schema sai")
		}
		switch p.Type {
		case "string", "boolean", "number", "integer":
		case "array":
			var item struct {
				Type string `json:"type"`
			}
			var itemKeys map[string]json.RawMessage
			_ = json.Unmarshal(keys["items"], &itemKeys)
			if len(itemKeys) != 1 || json.Unmarshal(keys["items"], &item) != nil || item.Type != "string" {
				return nil, errors.New("Mảng chưa hỗ trợ")
			}
		default:
			return nil, errors.New("Kiểu chưa hỗ trợ")
		}
		f.props[name] = p
		f.fields = append(f.fields, name)
	}
	for _, name := range req.Schema.Required {
		if _, ok := f.props[name]; !ok {
			return nil, errors.New("Trường bắt buộc không hợp lệ")
		}
	}
	sort.Strings(f.fields)
	return f, nil
}
func (s *Service) askField(ctx context.Context, c *Client, f *form) {
	if f.index >= len(f.fields) {
		raw, _ := json.MarshalIndent(f.answers, "", "  ")
		text := "Xem lại câu trả lời:\n" + string(raw)
		if len([]rune(text)) > 3500 {
			s.send(ctx, c, "Câu trả lời quá dài; hãy xử lý trên desktop.", nil)
			return
		}
		buttons := [][]Button{{s.button("Gửi câu trả lời", action{source: "question:submit", id: f.id, session: f.session, fingerprint: f.fp})}}
		msg := s.send(ctx, c, text, buttons)
		if msg != 0 {
			s.bind(buttons, msg)
			s.replies[msg] = f
		}
		return
	}
	name := f.fields[f.index]
	p := f.props[name]
	text := "Reply vào tin này để trả lời trường: " + name + " (" + p.Type + ")\n" + p.Title + "\n" + p.Description
	if len(p.Enum) > 0 {
		raw, _ := json.Marshal(p.Enum)
		text += "\nChọn một giá trị: " + string(raw)
	}
	if p.Type != "string" {
		text += "\nNhập giá trị JSON, ví dụ true, 42 hoặc [\"a\",\"b\"]."
	}
	var buttons [][]Button
	choices := p.Enum
	if p.Type == "boolean" && len(choices) == 0 {
		choices = []json.RawMessage{json.RawMessage("true"), json.RawMessage("false")}
	}
	if len(choices) <= 20 {
		for _, value := range choices {
			buttons = append(buttons, []Button{s.button(clip(string(value), 60), action{source: "question:field", id: f.id, session: f.session, fingerprint: f.fp, field: name, value: string(value)})})
		}
	}
	msg := s.send(ctx, c, text, buttons)
	if msg != 0 {
		s.bind(buttons, msg)
		s.replies[msg] = f
	}
}
func (s *Service) answerField(ctx context.Context, c *Client, f *form, text string) {
	if f.index >= len(f.fields) {
		return
	}
	live := false
	for _, q := range s.deps.Questions.PendingList() {
		if q.ID == f.id && fingerprint(q.Params) == f.fp {
			live = true
		}
	}
	if !live {
		s.send(ctx, c, "Câu hỏi không còn hiệu lực.", nil)
		return
	}
	if _, err := s.session(ctx, f.session); err != nil {
		return
	}
	p := f.props[f.fields[f.index]]
	var value any
	valid := true
	switch p.Type {
	case "string":
		value = text
	case "boolean":
		var v bool
		valid = json.Unmarshal([]byte(text), &v) == nil
		value = v
	case "number":
		var v float64
		valid = json.Unmarshal([]byte(text), &v) == nil
		value = v
	case "integer":
		var v int64
		valid = json.Unmarshal([]byte(text), &v) == nil
		value = v
	case "array":
		var v []string
		valid = json.Unmarshal([]byte(text), &v) == nil && v != nil
		value = v
	}
	if len(p.Enum) > 0 {
		matches := false
		encoded, _ := json.Marshal(value)
		for _, raw := range p.Enum {
			var item any
			if json.Unmarshal(raw, &item) == nil {
				canonical, _ := json.Marshal(item)
				if string(canonical) == string(encoded) {
					matches = true
				}
			}
		}
		valid = valid && matches
	}
	if !valid || len(text) > 8000 {
		s.send(ctx, c, "Giá trị không hợp lệ. Reply lại vào cùng câu hỏi.", nil)
		return
	}
	// Remove old field mappings so a second reply cannot answer the next field.
	for id, old := range s.replies {
		if old == f {
			delete(s.replies, id)
		}
	}
	f.answers[f.fields[f.index]] = value
	f.index++
	s.askField(ctx, c, f)
}
