package telegramcontrol

import (
	"context"
	"devkit/internal/identity"
	"devkit/internal/storage"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/audit"
	"devkit/internal/interaction"
	"devkit/internal/workspace"
)

type memoryStore struct {
	state State
	fail  bool
}

func (m *memoryStore) Load() (State, error) { return m.state, nil }
func (m *memoryStore) Save(state State) error {
	if m.fail {
		return errors.New("write failed")
	}
	m.state = state
	return nil
}

type fakeAgents struct {
	creations []agenthub.CreateInput
	Agents
	sessionID, workspaceID      string
	text                        string
	reads                       int
	models                      []agenthub.ConfigOption
	status                      agenthub.SessionStatus
	modelChanges                int
	changedOption, changedModel string
	prepareErr, changeErr       error
}

func (f *fakeAgents) ListSessions(context.Context) ([]agenthub.AgentSession, error) {
	return []agenthub.AgentSession{{ID: f.sessionID, WorkspaceID: f.workspaceID, Status: agenthub.SessionStatusIdle}}, nil
}
func (f *fakeAgents) GetStatus(_ context.Context, id string) (agenthub.AgentSession, error) {
	if id != f.sessionID {
		return agenthub.AgentSession{}, errors.New("not found")
	}
	status := f.status
	if status == "" {
		status = agenthub.SessionStatusIdle
	}
	return agenthub.AgentSession{ID: id, WorkspaceID: f.workspaceID, Status: status}, nil
}
func (f *fakeAgents) TranscriptPage(context.Context, string, int64, int) (agenthub.TranscriptPage, error) {
	f.reads++
	raw, _ := json.Marshal(agenthub.SessionUpdate{Kind: agenthub.UpdateConfigOptions, ConfigOptions: f.models})
	return agenthub.TranscriptPage{State: []agenthub.TranscriptEntry{{Kind: agenthub.UpdateConfigOptions, Payload: raw}}, Entries: []agenthub.TranscriptEntry{{Seq: 2, Text: f.text, Kind: agenthub.UpdateAgentMessageChunk}}, HasMore: true}, nil
}

func (f *fakeAgents) PrepareSession(context.Context, string) error { return f.prepareErr }
func (f *fakeAgents) SetConfigOption(_ context.Context, session, id, value string) error {
	if f.changeErr != nil {
		return f.changeErr
	}
	f.modelChanges++
	f.changedOption = id
	f.changedModel = value
	for i := range f.models {
		if f.models[i].ID == id {
			f.models[i].CurrentValue, _ = json.Marshal(value)
		}
	}
	return nil
}

func (f *fakeAgents) ListVendorTypes() []agenthub.AgentType {
	return []agenthub.AgentType{agenthub.AgentTypeClaude, agenthub.AgentTypeCodex}
}
func (f *fakeAgents) CreateSession(_ context.Context, in agenthub.CreateInput) (agenthub.AgentSession, error) {
	f.creations = append(f.creations, in)
	f.sessionID = "created-session"
	f.workspaceID = in.WorkspaceID
	return agenthub.AgentSession{ID: f.sessionID, WorkspaceID: in.WorkspaceID, AgentType: in.AgentType, Status: agenthub.SessionStatusIdle}, nil
}

type fakeWorkspaces struct{}

func (fakeWorkspaces) ListWorkspaces() ([]workspace.Workspace, error) {
	return []workspace.Workspace{{ID: "ws"}}, nil
}
func (fakeWorkspaces) ListServices(string) ([]workspace.WorkspaceService, error) { return nil, nil }
func harness(t *testing.T) (*Service, *Client, *fakeAgents, *[]string) {
	t.Helper()
	agents := &fakeAgents{sessionID: "session", workspaceID: "ws"}
	messages := []string{}
	var msgID int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			var text string
			_ = json.Unmarshal(body["text"], &text)
			messages = append(messages, text)
			msgID++
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"message_id": msgID}})
		} else {
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
		}
	}))
	t.Cleanup(server.Close)
	s, err := New(&memoryStore{state: State{Config: Config{Enabled: true, Workspaces: []string{"ws"}, SendDetails: true, ReadTranscript: true, ApproveInteractions: true}, UserID: 7, ChatID: 7}}, Deps{Agents: agents, Workspaces: fakeWorkspaces{}, Locked: func() bool { return false }, Permissions: agenthub.NewPermissionWaiter(nil), Questions: agenthub.NewElicitationWaiter(nil, nil), Interaction: interaction.NewHub(interaction.NoopPrompter{}), Audit: audit.NewMemorySink()})
	if err != nil {
		t.Fatal(err)
	}
	c := NewClient("super-secret")
	c.base = server.URL
	c.sendInterval = 0
	return s, c, agents, &messages
}
func callback(token string, a action, user, chat int64) Update {
	var u Update
	raw, _ := json.Marshal(map[string]any{"callback_query": map[string]any{"id": "cb", "from": map[string]any{"id": user}, "data": token, "message": map[string]any{"message_id": a.message, "chat": map[string]any{"id": chat, "type": "private"}}}})
	_ = json.Unmarshal(raw, &u)
	return u
}
func waitPending(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !fn() {
		if time.Now().After(deadline) {
			t.Fatal("request did not become pending")
		}
		time.Sleep(time.Millisecond)
	}
}
func TestPermissionWrongIdentityDuplicateAndDesktopRace(t *testing.T) {
	s, c, _, _ := harness(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	params := json.RawMessage(`{"toolCall":{"title":"Run command"},"options":[{"optionId":"once","kind":"allow_once"},{"optionId":"forever","kind":"allow_always"},{"optionId":"no","kind":"reject_once"}]}`)
	done := make(chan error, 1)
	go func() { _, err := s.deps.Permissions.Wait(ctx, "session", params); done <- err }()
	waitPending(t, func() bool { return len(s.deps.Permissions.PendingList()) == 1 })
	s.reconcile(ctx, c)
	var token string
	var a action
	for key, item := range s.actions {
		if item.value == "forever" {
			t.Fatal("persistent approval exposed")
		}
		if item.value == "once" {
			token = key
			a = item
		}
	}
	if token == "" {
		t.Fatal("no allow button")
	}
	s.handle(ctx, c, callback(token, a, 8, 7))
	s.handle(ctx, c, callback(token, a, 7, 8))
	if len(s.deps.Permissions.PendingList()) != 1 {
		t.Fatal("wrong identity resolved permission")
	}
	outcome := json.RawMessage(`{"outcome":{"outcome":"selected","optionId":"once"}}`)
	var wg sync.WaitGroup
	wg.Add(2)
	results := make(chan error, 2)
	go func() { defer wg.Done(); results <- s.complete(ctx, c, a) }()
	go func() { defer wg.Done(); results <- s.deps.Permissions.Complete(a.id, outcome) }()
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("completion winners=%d", wins)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := s.complete(ctx, c, a); err == nil {
		t.Fatal("stale request accepted")
	}
}
func TestDevKitInteractionOnceAndPolicy(t *testing.T) {
	s, c, _, _ := harness(t)
	ctx := context.Background()
	done := make(chan error, 1)
	go func() {
		done <- s.deps.Interaction.Confirm(ctx, interaction.ConfirmRequest{Env: "prod", Tool: "git push", Summary: "origin/task", EnvBlocks: true})
	}()
	waitPending(t, func() bool { return len(s.deps.Interaction.PendingList()) == 1 })
	s.reconcile(ctx, c)
	var a action
	for _, item := range s.actions {
		if item.source == "interaction" && item.value == "allow" {
			a = item
		}
		if item.value == "allow_always" {
			t.Fatal("prod always allow")
		}
	}
	if err := s.complete(ctx, c, a); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := s.complete(ctx, c, a); err == nil {
		t.Fatal("duplicate gate accepted")
	}
}
func TestQuestionRepliesPreviewAndSubmit(t *testing.T) {
	s, c, _, _ := harness(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	params := json.RawMessage(`{"mode":"form","message":"Pick options","requestedSchema":{"type":"object","properties":{"choice":{"type":"string","enum":["a","b"]},"ready":{"type":"boolean"}},"required":["choice","ready"]}}`)
	done := make(chan json.RawMessage, 1)
	go func() { raw, _ := s.deps.Questions.Wait(ctx, "session", params); done <- raw }()
	waitPending(t, func() bool { return len(s.deps.Questions.PendingList()) == 1 })
	s.reconcile(ctx, c)
	var start action
	for _, a := range s.actions {
		if a.source == "question:start" {
			start = a
		}
	}
	if err := s.complete(ctx, c, start); err != nil {
		t.Fatal(err)
	}
	var f *form
	for _, draft := range s.replies {
		f = draft
	}
	if f == nil {
		t.Fatal("no field prompt")
	}
	s.answerField(ctx, c, f, "invalid")
	if f.index != 0 {
		t.Fatal("invalid enum advanced")
	}
	s.answerField(ctx, c, f, "b")
	s.answerField(ctx, c, f, "true")
	var submit action
	for _, a := range s.actions {
		if a.source == "question:submit" {
			submit = a
		}
	}
	if err := s.complete(ctx, c, submit); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Action  string
		Content map[string]any
	}
	if err := json.Unmarshal(<-done, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != "accept" || result.Content["choice"] != "b" || result.Content["ready"] != true {
		t.Fatalf("answer=%+v", result)
	}
	if err := s.complete(ctx, c, submit); err == nil {
		t.Fatal("answered twice")
	}
}
func TestTranscriptRequiresScopeAndOptInAndDoesNotTruncate(t *testing.T) {
	s, c, agents, messages := harness(t)
	s.selected = "session"
	agents.text = strings.Repeat("nội dung 🌱 ", 800)
	s.state.Config.ReadTranscript = false
	s.read(context.Background(), c)
	if agents.reads != 0 {
		t.Fatal("read without opt-in")
	}
	s.state.Config.ReadTranscript = true
	agents.workspaceID = "other"
	s.read(context.Background(), c)
	if agents.reads != 0 {
		t.Fatal("read outside scope")
	}
	agents.workspaceID = "ws"
	before := len(*messages)
	s.read(context.Background(), c)
	for s.readBuffer != "" {
		s.more(context.Background(), c)
	}
	var all strings.Builder
	for _, text := range (*messages)[before:] {
		all.WriteString(strings.ReplaceAll(text, "\nDùng /more để đọc tiếp trang này.", ""))
	}
	if !strings.Contains(all.String(), agents.text) {
		t.Fatal("transcript lost text")
	}
}
func TestUnpairAndLockRevokeCapabilities(t *testing.T) {
	s, _, _, _ := harness(t)
	s.actions["token"] = action{}
	s.replies[1] = &form{}
	s.selected = "session"
	s.Suspend()
	if len(s.actions) != 0 || len(s.replies) != 0 || s.selected != "" {
		t.Fatal("lock retains remote capabilities")
	}
	if err := s.Unpair(); err != nil {
		t.Fatal(err)
	}
	if s.state.UserID != 0 || s.state.ChatID != 0 {
		t.Fatal("unpair retains user")
	}
}
func TestPairNeedsLocalAcceptanceAndExpires(t *testing.T) {
	s, c, _, _ := harness(t)
	s.botName = "testbot"
	link, err := s.Pair()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(link, "?start=") {
		t.Fatal(link)
	}
	m := &Message{Text: "/start " + s.nonce, From: User{ID: 9, Username: "person"}}
	m.Chat.Type = "private"
	m.Chat.ID = 9
	s.handle(context.Background(), c, Update{Message: m})
	if s.state.UserID != 7 || s.candidate.ID != 9 {
		t.Fatal("pair bypassed desktop")
	}
	if err := s.AcceptPair(8); err == nil {
		t.Fatal("wrong candidate accepted")
	}
	s.nonceUntil = time.Now().Add(-time.Second)
	if err := s.AcceptPair(9); err == nil {
		t.Fatal("expired pairing accepted")
	}
}
func TestUnsupportedQuestionConstraintsStayDesktop(t *testing.T) {
	_, err := newForm("id", "session", json.RawMessage(`{"mode":"form","requestedSchema":{"type":"object","properties":{"count":{"type":"number","minimum":2}}}}`))
	if err == nil {
		t.Fatal("constraint silently ignored")
	}
}
func TestClientNeverLeaksTokenOnNetworkError(t *testing.T) {
	c := NewClient("secret-value")
	c.base = "http://127.0.0.1:1"
	err := c.Do(context.Background(), "getMe", nil, nil)
	if err == nil || strings.Contains(err.Error(), "secret-value") || strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("unsafe error: %v", err)
	}
}

func TestAgentOwnedDevKitGateSharesPermissionTransport(t *testing.T) {
	s, c, _, _ := harness(t)
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO workspaces(id,name,root) VALUES('ws','workspace','')"); err != nil {
		t.Fatal(err)
	}
	registry := agenthub.NewRegistry()
	registry.Register(agenthub.NewMockAdapter())
	agents := agenthub.NewService(registry, agenthub.NewSQLStore(db), nil)
	agents.MatchingWorkspaces = func(string) ([]string, error) { return []string{"ws"}, nil }
	agents.ResolveGatePermission = s.deps.Permissions.ResolveGate
	sess, err := agents.CreateSession(context.Background(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir(), WorkspaceID: "ws"})
	if err != nil {
		t.Fatal(err)
	}
	s.deps.Agents = agents
	s.deps.Interaction.ConfirmAgent = agents.ConfirmTool
	ctx := identity.WithCaller(context.Background(), identity.Agent(sess.ID))
	done := make(chan error, 1)
	go func() {
		done <- s.deps.Interaction.Confirm(ctx, interaction.ConfirmRequest{Kind: interaction.KindGit, Tool: "Push task branch", Summary: "origin/task", Env: "prod", EnvBlocks: true, Target: interaction.Target{Repo: "repo", Branch: "task", Remote: "origin"}})
	}()
	waitPending(t, func() bool { return len(s.deps.Permissions.PendingList()) == 1 })
	if len(s.deps.Interaction.PendingList()) != 0 {
		t.Fatal("delegated gate duplicated into ordinary interactions")
	}
	s.reconcile(context.Background(), c)
	var a action
	for _, item := range s.actions {
		if item.source == "permission" && item.value == "allow" {
			a = item
		}
	}
	if err := s.complete(context.Background(), c, a); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if status, err := agents.GetStatus(context.Background(), sess.ID); err != nil || status.Status != agenthub.SessionStatusIdle {
		t.Fatalf("status=%+v, error=%v", status, err)
	}
}
func TestCancelledPermissionCannotBeApprovedFromOldCard(t *testing.T) {
	s, c, _, _ := harness(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := s.deps.Permissions.Wait(ctx, "session", json.RawMessage(`{"options":[{"optionId":"yes","kind":"allow_once"}]}`))
		done <- err
	}()
	waitPending(t, func() bool { return len(s.deps.Permissions.PendingList()) == 1 })
	s.reconcile(context.Background(), c)
	var a action
	for _, item := range s.actions {
		a = item
	}
	cancel()
	<-done
	if err := s.complete(context.Background(), c, a); err == nil {
		t.Fatal("cancelled permission approved")
	}
}
