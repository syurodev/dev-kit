package telegramcontrol

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync/atomic"
	"testing"

	"devkit/internal/agenthub"
)

func TestRemoteSessionDefaultsOffAndNotificationsRequireOptIn(t *testing.T) {
	s, c, _, messages := harness(t)
	s.state.Config.SessionIDs = nil // Legacy persisted configuration has no session opt-ins.
	s.observed["session"] = agenthub.SessionStatusRunning
	s.reconcile(context.Background(), c)
	if len(*messages) != 0 {
		t.Fatal("disabled session sent a completion notification")
	}
	if _, err := s.session(context.Background(), "session"); err == nil {
		t.Fatal("disabled session remains remotely accessible")
	}
	if err := s.SetSessionEnabled(context.Background(), "session", true); err != nil {
		t.Fatal(err)
	}
	s.observed["session"] = agenthub.SessionStatusRunning
	s.reconcile(context.Background(), c)
	if len(*messages) != 1 {
		t.Fatal("enabled session did not send its completion notification")
	}
	if !slices.Contains(s.store.(*memoryStore).state.Config.SessionIDs, "session") {
		t.Fatal("opt-in not persisted")
	}
}
func TestSessionOptInFiltersPermissionsQuestionsAndRevokesOldButtons(t *testing.T) {
	s, c, _, messages := harness(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.state.Config.SessionIDs = nil
	permission := json.RawMessage(`{"options":[{"optionId":"yes","kind":"allow_once"}]}`)
	go func() { _, _ = s.deps.Permissions.Wait(ctx, "session", permission) }()
	question := json.RawMessage(`{"mode":"form","message":"Question","requestedSchema":{"type":"object","properties":{"answer":{"type":"string"}}}}`)
	go func() { _, _ = s.deps.Questions.Wait(ctx, "session", question) }()
	waitPending(t, func() bool {
		return len(s.deps.Permissions.PendingList()) == 1 && len(s.deps.Questions.PendingList()) == 1
	})
	s.reconcile(ctx, c)
	if len(*messages) != 0 || len(s.actions) != 0 {
		t.Fatal("disabled session leaked pending request content")
	}
	if err := s.SetSessionEnabled(ctx, "session", true); err != nil {
		t.Fatal(err)
	}
	s.reconcile(ctx, c)
	if len(*messages) != 2 {
		t.Fatal("enabled session pending requests not delivered")
	}
	var token string
	var old action
	for key, a := range s.actions {
		if a.source == "permission" {
			token = key
			old = a
			break
		}
	}
	if token == "" {
		t.Fatal("missing permission action")
	}
	s.selected = "session"
	s.readBuffer = "private buffered transcript"
	if err := s.SetSessionEnabled(ctx, "session", false); err != nil {
		t.Fatal(err)
	}
	if s.selected != "" || s.readBuffer != "" || len(s.actions) != 0 {
		t.Fatal("disable retained remote capabilities")
	}
	if err := s.SetSessionEnabled(ctx, "session", true); err != nil {
		t.Fatal(err)
	}
	s.handle(ctx, c, callback(token, old, 7, 7))
	if len(s.deps.Permissions.PendingList()) != 1 {
		t.Fatal("old button survived opt-out and opt-in")
	}
	s.reconcile(ctx, c)
	if len(*messages) != 4 {
		t.Fatal("opt-in did not reissue current permission/question cards")
	}
}
func TestSessionOptInWorkspaceAndPersistenceFailClosed(t *testing.T) {
	s, _, agents, _ := harness(t)
	agents.workspaceID = "not-allowed"
	if err := s.SetSessionEnabled(context.Background(), "session", true); err == nil {
		t.Fatal("workspace grant bypassed")
	}
	agents.workspaceID = "ws"
	s.store.(*memoryStore).fail = true
	if err := s.SetSessionEnabled(context.Background(), "session", false); err == nil {
		t.Fatal("persist failure ignored")
	}
	if !slices.Contains(s.state.Config.SessionIDs, "session") {
		t.Fatal("failed write changed the effective policy")
	}
}
func TestConfigurationDraftCannotOverwriteSessionOptIns(t *testing.T) {
	s, _, _, _ := harness(t)
	cfg := s.state.Config
	cfg.Enabled = false
	cfg.SessionIDs = []string{"other"}
	if err := s.Configure(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(s.state.Config.SessionIDs, []string{"session"}) {
		t.Fatal("stale settings draft changed session policy")
	}
}
func TestAgentInteractionRequiresSessionOptIn(t *testing.T) {
	s, _, _, _ := harness(t)
	s.state.Config.SessionIDs = nil
	if s.interactionAllowed(context.Background(), "agent:session") {
		t.Fatal("agent confirmation bypassed session opt-in")
	}
	if !s.interactionAllowed(context.Background(), "user-terminal") {
		t.Fatal("separate desktop confirmation setting was lost")
	}
}

type heldTurnAgent struct {
	*fakeAgents
	started, release chan struct{}
}

func (a *heldTurnAgent) Invoke(context.Context, agenthub.InvokeInput) (agenthub.InvokeResult, error) {
	close(a.started)
	<-a.release
	return agenthub.InvokeResult{Status: "idle", Summary: "private result"}, nil
}
func TestOptOutDuringRemoteTurnSuppressesItsResult(t *testing.T) {
	s, c, agents, messages := harness(t)
	held := &heldTurnAgent{fakeAgents: agents, started: make(chan struct{}), release: make(chan struct{})}
	s.deps.Agents = held
	s.selected = "session"
	s.handle(context.Background(), c, command("perform the task"))
	<-held.started
	if err := s.SetSessionEnabled(context.Background(), "session", false); err != nil {
		t.Fatal(err)
	}
	close(held.release)
	s.turns.Wait()
	if len(*messages) != 0 {
		t.Fatal("opted-out turn sent its result to Telegram")
	}
}

type countingTurnAgent struct {
	*fakeAgents
	calls atomic.Int32
}

func (a *countingTurnAgent) Invoke(context.Context, agenthub.InvokeInput) (agenthub.InvokeResult, error) {
	a.calls.Add(1)
	return agenthub.InvokeResult{Status: "idle"}, nil
}
func TestExpiredQuestionReplyCannotPromptAnotherSelectedSession(t *testing.T) {
	s, c, agents, _ := harness(t)
	counted := &countingTurnAgent{fakeAgents: agents}
	s.deps.Agents = counted
	s.selected = "session"
	update := command("answer to a question from an opted-out session")
	update.Message.Reply = &Message{ID: 999}
	s.handle(context.Background(), c, update)
	s.turns.Wait()
	if counted.calls.Load() != 0 {
		t.Fatal("expired reply was rerouted as a prompt")
	}
}

type sessionCatalogAgents struct {
	*fakeAgents
	catalog []agenthub.AgentSession
}

func (a *sessionCatalogAgents) ListSessions(context.Context) ([]agenthub.AgentSession, error) {
	return a.catalog, nil
}
func TestDeletedRemoteSessionIDsDoNotConsumeCapacity(t *testing.T) {
	s, _, _, _ := harness(t)
	s.state.Config.SessionIDs = nil
	for i := 0; i < 500; i++ {
		s.state.Config.SessionIDs = append(s.state.Config.SessionIDs, fmt.Sprintf("deleted-%d", i))
	}
	if err := s.SetSessionEnabled(context.Background(), "session", true); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(s.state.Config.SessionIDs, []string{"session"}) {
		t.Fatal("deleted session IDs retained capacity")
	}
}
func TestRemoteSessionCapStillAppliesToLiveSessionsAndIsIdempotent(t *testing.T) {
	s, _, agents, _ := harness(t)
	catalog := &sessionCatalogAgents{fakeAgents: agents}
	s.state.Config.SessionIDs = nil
	for i := 0; i < 500; i++ {
		id := fmt.Sprintf("live-%d", i)
		s.state.Config.SessionIDs = append(s.state.Config.SessionIDs, id)
		catalog.catalog = append(catalog.catalog, agenthub.AgentSession{ID: id})
	}
	s.deps.Agents = catalog
	if err := s.SetSessionEnabled(context.Background(), "session", true); err == nil {
		t.Fatal("live-session cap bypassed")
	}
	s.state.Config.SessionIDs[0] = "session"
	if err := s.SetSessionEnabled(context.Background(), "session", true); err != nil {
		t.Fatal("already enabled session rejected at capacity")
	}
	if len(s.state.Config.SessionIDs) != 500 {
		t.Fatal("idempotent enable changed membership")
	}
}
