package telegramcontrol

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/audit"
	"devkit/internal/identity"
	"devkit/internal/interaction"
	"devkit/internal/workspace"
)

type Agents interface {
	ListSessions(context.Context) ([]agenthub.AgentSession, error)
	GetStatus(context.Context, string) (agenthub.AgentSession, error)
	TranscriptPage(context.Context, string, int64, int) (agenthub.TranscriptPage, error)
	Invoke(context.Context, agenthub.InvokeInput) (agenthub.InvokeResult, error)
	Cancel(context.Context, string) error
	ListVendorTypes() []agenthub.AgentType
	CreateSession(context.Context, agenthub.CreateInput) (agenthub.AgentSession, error)
	PrepareSession(context.Context, string) error
	SetConfigOption(context.Context, string, string, string) error
}
type Workspaces interface {
	ListWorkspaces() ([]workspace.Workspace, error)
	ListServices(string) ([]workspace.WorkspaceService, error)
}
type Deps struct {
	Agents      Agents
	Permissions *agenthub.PermissionWaiter
	Questions   *agenthub.ElicitationWaiter
	Interaction *interaction.Hub
	Workspaces  Workspaces
	Locked      func() bool
	Token       func(string) (string, error)
	Audit       audit.Sink
}
type Status struct {
	Config        Config `json:"config"`
	Paired        bool   `json:"paired"`
	UserID        int64  `json:"userId"`
	CandidateID   int64  `json:"candidateId"`
	CandidateName string `json:"candidateName"`
	BotName       string `json:"botName"`
	Error         string `json:"error"`
}
type action struct {
	source, id, value, session string
	field                      string
	message                    int64
	fingerprint                string
}
type delivery struct {
	session     string
	revoked     bool
	message     int64
	fingerprint string
}
type Service struct {
	mu                      sync.Mutex
	turns                   sync.WaitGroup
	store                   Store
	deps                    Deps
	state                   State
	cancel                  context.CancelFunc
	done                    chan struct{}
	generation              uint64
	botName, failure, nonce string
	tokenMark               string
	nonceUntil              time.Time
	candidate               User
	candidateChat           int64
	actions                 map[string]action
	sent                    map[string]delivery
	replies                 map[int64]*form
	selected                string
	before                  int64
	readBuffer              string
	observed                map[string]agenthub.SessionStatus
	ownTurns                map[string]bool
}

func New(store Store, deps Deps) (*Service, error) {
	state, err := store.Load()
	if err != nil {
		return nil, err
	}
	svc := &Service{store: store, deps: deps, state: state}
	svc.stopLocked()
	return svc, nil
}
func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Status{Config: s.state.Config, Paired: s.state.UserID != 0, UserID: s.state.UserID, CandidateID: s.candidate.ID, CandidateName: s.candidate.Username, BotName: s.botName, Error: s.failure}
}
func (s *Service) UsedBy(id string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == s.state.Config.AccountID {
		return []string{"Telegram remote control"}
	}
	return nil
}
func (s *Service) stopLocked() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.generation++
	s.actions = map[string]action{}
	s.sent = map[string]delivery{}
	s.replies = map[int64]*form{}
	s.selected = ""
	s.observed = map[string]agenthub.SessionStatus{}
	s.ownTurns = map[string]bool{}
	s.before = 0
	s.readBuffer = ""
	s.nonce = ""
	s.candidate = User{}
	s.candidateChat = 0
}

// Suspend is called before locking the vault, so no in-flight callback can win later.
func (s *Service) Suspend() { s.mu.Lock(); defer s.mu.Unlock(); s.stopLocked() }
func (s *Service) Close() {
	s.Suspend()
	s.mu.Lock()
	done := s.done
	s.mu.Unlock()
	if done != nil {
		<-done
	}
	s.turns.Wait()
}
func (s *Service) Configure(ctx context.Context, cfg Config) error {
	if len(cfg.Workspaces) > 100 {
		return errors.New("too many workspaces")
	}
	if cfg.Enabled {
		token, err := s.deps.Token(cfg.AccountID)
		if err != nil {
			return errors.New("choose an available local bot token")
		}
		client := NewClient(token)
		var bot User
		if err = client.Do(ctx, "getMe", map[string]any{}, &bot); err != nil {
			return err
		}
		var hook struct {
			URL string `json:"url"`
		}
		if err = client.Do(ctx, "getWebhookInfo", map[string]any{}, &hook); err != nil {
			return err
		}
		if hook.URL != "" {
			return errors.New("this bot has a webhook; use a dedicated bot without a webhook")
		}
		s.mu.Lock()
		s.botName = bot.Username
		s.mu.Unlock()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.state
	cfg.SessionIDs = slices.Clone(s.state.Config.SessionIDs)
	next.Config = cfg
	if cfg.AccountID != s.state.Config.AccountID {
		next.UserID = 0
		next.ChatID = 0
		next.Offset = 0
	}
	if err := s.store.Save(next); err != nil {
		return err
	}
	s.stopLocked()
	s.state = next
	s.failure = ""
	return nil
}
func (s *Service) Pair() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.state.Config.Enabled || s.deps.Locked() || s.botName == "" {
		return "", errors.New("enable Telegram while the vault is unlocked first")
	}
	s.nonce = randomID()
	s.nonceUntil = time.Now().Add(5 * time.Minute)
	s.candidate = User{}
	s.candidateChat = 0
	return "https://t.me/" + s.botName + "?start=" + s.nonce, nil
}
func (s *Service) AcceptPair(user int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deps.Locked() || user == 0 || user != s.candidate.ID || !time.Now().Before(s.nonceUntil) {
		return errors.New("pairing candidate expired")
	}
	next := s.state
	next.UserID = user
	next.ChatID = s.candidateChat
	if err := s.store.Save(next); err != nil {
		return err
	}
	s.stopLocked()
	s.state = next
	s.record("pair", "", true)
	return nil
}
func (s *Service) Unpair() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.state
	next.UserID = 0
	next.ChatID = 0
	if err := s.store.Save(next); err != nil {
		return err
	}
	s.record("unpair", "", true)
	s.stopLocked()
	s.state = next
	return nil
}

// Start supervisor survives lock/unlock and keeps one polling worker per generation.
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	defer s.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			if s.deps.Locked() || !s.state.Config.Enabled {
				if s.cancel != nil {
					s.stopLocked()
				}
				s.mu.Unlock()
				continue
			}
			if s.cancel != nil {
				token, err := s.deps.Token(s.state.Config.AccountID)
				if err != nil || fingerprint([]byte(token)) != s.tokenMark {
					s.stopLocked()
					next := s.state
					next.UserID = 0
					next.ChatID = 0
					next.Offset = 0
					if err := s.store.Save(next); err != nil {
						s.state.Config.Enabled = false
						s.failure = "Cannot persist token revocation"
					} else {
						s.state = next
						s.failure = "Bot token changed; pair again"
					}
				}
				s.mu.Unlock()
				continue
			}
			// Wait until the previous network worker has exited before starting another.
			if s.done != nil {
				select {
				case <-s.done:
				default:
					s.mu.Unlock()
					continue
				}
			}
			token, err := s.deps.Token(s.state.Config.AccountID)
			if err != nil {
				s.failure = "Bot token unavailable"
				s.mu.Unlock()
				continue
			}
			child, cancel := context.WithCancel(ctx)
			s.cancel = cancel
			s.done = make(chan struct{})
			done := s.done
			generation := s.generation
			s.tokenMark = fingerprint([]byte(token))
			client := NewClient(token)
			s.mu.Unlock()
			go func() { defer close(done); s.poll(child, client, generation) }()
		}
	}
}
func (s *Service) live(g uint64) bool {
	return g == s.generation && !s.deps.Locked() && s.state.Config.Enabled
}
func (s *Service) poll(ctx context.Context, c *Client, g uint64) {
	var bot User
	if err := c.Do(ctx, "getMe", map[string]any{}, &bot); err == nil {
		s.mu.Lock()
		if s.live(g) {
			s.botName = bot.Username
		}
		s.mu.Unlock()
	}

	// Keep Telegram's slash-command picker discoverable. This is metadata only;
	// every actual command still passes paired-user and workspace checks.
	_ = c.Do(ctx, "setMyCommands", map[string]any{"commands": telegramCommands}, nil)

	for ctx.Err() == nil {
		s.mu.Lock()
		if !s.live(g) {
			s.mu.Unlock()
			return
		}
		offset := s.state.Offset
		s.mu.Unlock()
		var updates []Update
		err := c.Do(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": 2, "allowed_updates": []string{"message", "callback_query"}}, &updates)
		s.mu.Lock()
		if !s.live(g) {
			s.mu.Unlock()
			return
		}
		if err != nil {
			s.failure = err.Error()
			s.mu.Unlock()
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}
		s.failure = ""
		for _, u := range updates {
			if u.ID < s.state.Offset {
				continue
			}
			// Persist before side effects: after a crash an uncertain command is not replayed.
			next := s.state
			next.Offset = u.ID + 1
			if err := s.store.Save(next); err != nil {
				s.failure = "Cannot persist Telegram offset"
				break
			}
			s.state = next
			s.handle(ctx, c, u)
		}
		if s.state.ChatID != 0 {
			s.reconcile(ctx, c)
		}
		s.mu.Unlock()
	}
}
func (s *Service) record(action, id string, allowed bool, subject ...string) {
	if s.deps.Audit == nil {
		return
	}
	outcome := audit.OutcomeDenied
	if allowed {
		outcome = audit.OutcomeAllowed
	}
	s.deps.Audit.Record(audit.Event{Caller: fmt.Sprintf("telegram:%d", s.state.UserID), At: time.Now().UTC(), SubjectID: "telegram", Capability: "remote:control", Scope: action, Outcome: outcome, Metadata: map[string]any{"requestId": id, "subject": strings.Join(subject, ",")}})
}
func (s *Service) send(ctx context.Context, c *Client, text string, buttons [][]Button) int64 {
	// Plain text prevents agent content from becoming Telegram markup.
	text = clip(text, 3800)
	body := map[string]any{"chat_id": s.state.ChatID, "text": text}
	if len(buttons) > 0 {
		body["reply_markup"] = map[string]any{"inline_keyboard": buttons}
	}
	var msg Message
	if err := c.Do(ctx, "sendMessage", body, &msg); err != nil {
		s.failure = err.Error()
		return 0
	}
	return msg.ID
}
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "\n…"
	}
	return s
}
func (s *Service) allowed(id string) bool {
	if !slices.Contains(s.state.Config.Workspaces, id) {
		return false
	}
	ws, err := s.deps.Workspaces.ListWorkspaces()
	if err != nil {
		return false
	}
	for _, w := range ws {
		if w.ID == id {
			return true
		}
	}
	return false
}
func (s *Service) session(ctx context.Context, id string) (agenthub.AgentSession, error) {
	sess, err := s.deps.Agents.GetStatus(ctx, id)
	if err != nil || !s.allowed(sess.WorkspaceID) || !slices.Contains(s.state.Config.SessionIDs, id) {
		return sess, errors.New("session outside allowed workspaces")
	}
	return sess, nil
}
func (s *Service) button(text string, a action) Button {
	id := randomID()
	s.actions[id] = a
	return Button{Text: text, Data: id}
}
func (s *Service) handle(ctx context.Context, c *Client, u Update) {
	if cb := u.Callback; cb != nil {
		valid := cb.Message != nil && cb.Message.Chat.Type == "private" && cb.From.ID == s.state.UserID && !cb.From.IsBot && cb.Message.Chat.ID == s.state.ChatID
		a, ok := s.actions[cb.Data]
		valid = valid && ok && a.message == cb.Message.ID
		result := "Yêu cầu không còn hiệu lực"
		if valid {
			ctx = identity.WithCaller(ctx, identity.Caller{Kind: identity.KindRemoteHuman})
			if err := s.complete(ctx, c, a); err == nil {
				result = "Đã nhận"
				s.record(a.source, a.id, true, a.session)
			} else {
				result = clip(err.Error(), 150)
				s.record(a.source, a.id, false)
			}
		}
		_ = c.Do(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": cb.ID, "text": result}, nil)
		return
	}
	m := u.Message
	if m == nil || m.Chat.Type != "private" || m.From.IsBot || len(m.Forward) > 0 {
		return
	}
	if strings.HasPrefix(m.Text, "/start ") && s.nonce != "" && time.Now().Before(s.nonceUntil) && subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(m.Text, "/start ")), []byte(s.nonce)) == 1 {
		s.candidate = m.From
		s.candidateChat = m.Chat.ID
		return
	}
	if m.From.ID != s.state.UserID || m.Chat.ID != s.state.ChatID || s.state.UserID == 0 {
		return
	}
	if m.Reply != nil {
		if f := s.replies[m.Reply.ID]; f != nil {
			s.answerField(ctx, c, f, m.Text)
			return
		}
	}
	ctx = identity.WithCaller(ctx, identity.Caller{Kind: identity.KindRemoteHuman})
	cmd, arg, _ := strings.Cut(strings.TrimSpace(m.Text), " ")
	cmd = strings.Split(cmd, "@")[0]
	if cmd != "/efforts" && cmd != "/models" && cmd != "/read" && cmd != "/older" && cmd != "/more" && cmd != "/sessions" && cmd != "/status" && cmd != "/pending" && cmd != "/help" && time.Since(time.Unix(m.Date, 0)) > 2*time.Minute {
		s.send(ctx, c, "Lệnh đã quá cũ; hãy gửi lại.", nil)
		return
	}
	switch cmd {
	case "/start", "/help":
		s.send(ctx, c, "/sessions — chọn session\n/read — đọc nội dung\n/more — đọc tiếp trang hiện tại\n/older — đọc trang trước\n/use <session ID> — chọn session\n/status — trạng thái\n/models — xem và chọn model\n/model <model ID> — chuyển model\n/stop — dừng lượt chạy\n/effort — xem và chọn effort\n/effort <ID> — đổi effort\n/new — tạo session bằng nút\n/new <agent> <workspace ID> [model ID] [effort]\n/pending — các yêu cầu chờ\nGửi tin nhắn để ra lệnh cho session đã chọn. Reply vào tin hỏi để trả lời.", nil)
	case "/sessions":
		for token, a := range s.actions {
			if a.source == "select" {
				delete(s.actions, token)
			}
		}
		sessions, err := s.deps.Agents.ListSessions(ctx)
		if err != nil {
			s.send(ctx, c, "Không đọc được danh sách session", nil)
			return
		}
		offset, _ := strconv.Atoi(arg)
		if offset < 0 || offset > 10000 {
			offset = 0
		}
		count, skipped := 0, 0
		for _, sess := range sessions {
			if !s.allowed(sess.WorkspaceID) || !slices.Contains(s.state.Config.SessionIDs, sess.ID) {
				continue
			}
			if skipped < offset {
				skipped++
				continue
			}
			title := sess.ID
			if sess.Title != nil {
				title = *sess.Title
			}
			b := s.button("Chọn session", action{source: "select", session: sess.ID})
			msg := s.send(ctx, c, clip(title, 200)+"\n"+string(sess.AgentType)+" · "+string(sess.Status), [][]Button{{b}})
			a := s.actions[b.Data]
			a.message = msg
			s.actions[b.Data] = a
			count++
			if count == 10 {
				s.send(ctx, c, fmt.Sprintf("Dùng /sessions %d để xem tiếp.", offset+count), nil)
				break
			}
		}
		if count == 0 {
			s.send(ctx, c, "Chưa có session bật điều khiển từ xa. Bật trong menu session của DevKit, hoặc dùng /new.", nil)
		}
	case "/more":
		s.more(ctx, c)
	case "/read", "/older":
		if cmd == "/read" {
			s.before = 0
			s.readBuffer = ""
		}
		s.read(ctx, c)
	case "/use":
		if _, err := s.session(ctx, arg); err != nil {
			s.send(ctx, c, "Session không được phép.", nil)
			return
		}
		s.selected = arg
		s.before = 0
		s.readBuffer = ""
		s.send(ctx, c, "Đã chọn session "+arg, nil)
	case "/status":
		sess, err := s.session(ctx, s.selected)
		if err != nil {
			s.send(ctx, c, "Hãy chọn session bằng /sessions", nil)
		} else {
			s.send(ctx, c, string(sess.AgentType)+" · "+string(sess.Status)+"\n"+sess.ID, nil)
		}
	case "/models":
		s.showModels(ctx, c, arg)
	case "/model":
		s.modelCommand(ctx, c, arg)
	case "/effort":
		s.settingCommand(ctx, c, arg, "effort")
	case "/efforts":
		s.showSetting(ctx, c, arg, "effort", false)
	case "/pending":
		s.send(ctx, c, "Đang đối soát yêu cầu chờ; yêu cầu còn hiệu lực được hiển thị bên dưới.", nil)
	case "/stop":
		if _, err := s.session(ctx, s.selected); err == nil {
			err = s.deps.Agents.Cancel(ctx, s.selected)
			if err == nil {
				s.send(ctx, c, "Đã dừng lượt chạy.", nil)
				s.record("stop", s.selected, true)
			}
		} else {
			s.send(ctx, c, "Hãy chọn session bằng /sessions", nil)
		}
	case "/new", "/session":
		s.newSessionCommand(ctx, c, arg)
	default:
		if strings.HasPrefix(cmd, "/") {
			s.send(ctx, c, "Lệnh chưa hỗ trợ. Dùng /help.", nil)
			return
		}
		sess, err := s.session(ctx, s.selected)
		if err != nil {
			s.send(ctx, c, "Hãy chọn session bằng /sessions", nil)
			return
		}
		if sess.Status != agenthub.SessionStatusIdle {
			s.send(ctx, c, "Session đang bận. Dùng /stop hoặc chờ lượt chạy kết thúc.", nil)
			return
		}
		id := sess.ID
		prompt := m.Text
		generation := s.generation
		s.record("prompt", id, true)
		s.ownTurns[id] = true
		s.turns.Add(1)
		go func() {
			defer s.turns.Done()
			result, err := s.deps.Agents.Invoke(ctx, agenthub.InvokeInput{SessionID: id, Prompt: prompt})
			s.mu.Lock()
			defer s.mu.Unlock()
			if !s.live(generation) {
				return
			}
			delete(s.ownTurns, id)
			delete(s.observed, id)
			if _, checkErr := s.session(ctx, id); checkErr != nil {
				return
			}
			text := "Agent đã kết thúc: " + result.Status
			if err != nil {
				text = "Lượt chạy thất bại; xem chi tiết trong DevKit."
			}
			if s.state.Config.ReadTranscript && err == nil {
				text += "\n" + result.Summary
			}
			s.send(ctx, c, text, nil)
		}()
	}
}
func (s *Service) read(ctx context.Context, c *Client) {
	if !s.state.Config.ReadTranscript {
		s.send(ctx, c, "Đọc nội dung chưa được bật trong Settings → Remote control → Telegram.", nil)
		return
	}
	if _, err := s.session(ctx, s.selected); err != nil {
		s.send(ctx, c, "Hãy chọn session bằng /sessions", nil)
		return
	}
	if s.readBuffer != "" {
		s.send(ctx, c, "Dùng /more để đọc hết trang hiện tại trước.", nil)
		return
	}
	page, err := s.deps.Agents.TranscriptPage(ctx, s.selected, s.before, 3)
	if err != nil {
		s.send(ctx, c, "Không đọc được transcript.", nil)
		return
	}
	if len(page.Entries) == 0 {
		s.send(ctx, c, "Không có nội dung ở trang này.", nil)
		return
	}
	var text strings.Builder
	for _, e := range page.Entries {
		text.WriteString(string(e.Kind) + "\n")
		if e.Text != "" {
			text.WriteString(e.Text)
		} else if len(e.Payload) > 0 {
			text.Write(e.Payload)
		}
		text.WriteString("\n\n")
	}
	s.before = page.Entries[0].Seq
	if page.HasMore {
		text.WriteString("Dùng /older để đọc phần trước đó.")
	}
	s.readBuffer = text.String()
	s.more(ctx, c)
	s.record("read", s.selected, true)
}
func (s *Service) more(ctx context.Context, c *Client) {
	if !s.state.Config.ReadTranscript {
		return
	}
	if _, err := s.session(ctx, s.selected); err != nil {
		s.readBuffer = ""
		return
	}
	runes := []rune(s.readBuffer)
	if len(runes) == 0 {
		s.send(ctx, c, "Không còn nội dung trong trang này. Dùng /older.", nil)
		return
	}
	n := min(len(runes), 3400)
	text := string(runes[:n])
	if n < len(runes) {
		text += "\nDùng /more để đọc tiếp trang này."
	}
	if s.send(ctx, c, text, nil) != 0 {
		s.readBuffer = string(runes[n:])
	}
}

var telegramCommands = []map[string]string{
	{"command": "sessions", "description": "Select a session"},
	{"command": "new", "description": "Create a session and choose model and effort"},
	{"command": "models", "description": "Choose the session model"},
	{"command": "effort", "description": "Choose reasoning effort"},
	{"command": "read", "description": "Read session messages"},
	{"command": "more", "description": "Continue reading the current page"},
	{"command": "older", "description": "Read earlier messages"},
	{"command": "status", "description": "Check session status"},
	{"command": "stop", "description": "Stop the selected session"},
	{"command": "pending", "description": "Review pending requests"},
	{"command": "help", "description": "Show all commands"},
}
