package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/apperrors"
	"devkit/internal/interaction"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type fakeAgentHub struct {
	list       []agenthub.AgentSession
	created    agenthub.AgentSession
	status     agenthub.AgentSession
	invokeOut  agenthub.InvokeResult
	createIn   agenthub.CreateInput
	invokeIn   agenthub.InvokeInput
	cancelID   string
	statusID   string
	listCalled bool

	sendIn     agenthub.SendMessageInput
	sendOut    agenthub.SendMessageResult
	sendCalled bool
	peers      []agenthub.PeerInfo
	route      agenthub.PeerRelation
	routeTo    agenthub.AgentSession

	models    agenthub.ModelListing
	profiles  agenthub.ModelProfilesListing
	plan      agenthub.ConfigPlan
	planErr   error
	planIn    agenthub.ModelChoiceInput
	modelsFor agenthub.AgentType
}

func (f *fakeAgentHub) SendMessage(_ context.Context, in agenthub.SendMessageInput) (agenthub.SendMessageResult, error) {
	f.sendIn, f.sendCalled = in, true
	return f.sendOut, nil
}

func (f *fakeAgentHub) ListPeers(context.Context) ([]agenthub.PeerInfo, error) { return f.peers, nil }

func (f *fakeAgentHub) PeerRoute(context.Context, string) (agenthub.PeerRelation, agenthub.AgentSession, bool, error) {
	return f.route, f.routeTo, f.route != "", nil
}

func (f *fakeAgentHub) ListModels(_ context.Context, vendor agenthub.AgentType) (agenthub.ModelListing, error) {
	f.modelsFor = vendor
	return f.models, nil
}

func (f *fakeAgentHub) ListModelProfiles(context.Context) agenthub.ModelProfilesListing {
	return f.profiles
}

func (f *fakeAgentHub) PlanModelChoice(_ context.Context, in agenthub.ModelChoiceInput) (agenthub.ConfigPlan, error) {
	f.planIn = in
	return f.plan, f.planErr
}

func (f *fakeAgentHub) ListSessions(context.Context) ([]agenthub.AgentSession, error) {
	f.listCalled = true
	return f.list, nil
}

func (f *fakeAgentHub) CreateSession(_ context.Context, in agenthub.CreateInput) (agenthub.AgentSession, error) {
	f.createIn = in
	return f.created, nil
}

func (f *fakeAgentHub) GetStatus(_ context.Context, id string) (agenthub.AgentSession, error) {
	f.statusID = id
	return f.status, nil
}

func (f *fakeAgentHub) Invoke(_ context.Context, in agenthub.InvokeInput) (agenthub.InvokeResult, error) {
	f.invokeIn = in
	return f.invokeOut, nil
}

func (f *fakeAgentHub) Cancel(_ context.Context, id string) error {
	f.cancelID = id
	return nil
}

func agentHubTestDeps(caller *fakeCaller, hub AgentHubAPI) Deps {
	return Deps{
		Caller:   caller,
		Notes:    &fakeNotes{},
		Vault:    &fakeVault{},
		Search:   &fakeSearch{},
		Tools:    fakeTools{},
		Envs:     &fakeEnvs{},
		SSH:      &fakeSSH{},
		DB:       &fakeDB{},
		Kafka:    &fakeKafka{},
		AgentHub: hub,
	}
}

func TestAgentHubListAgentsTool(t *testing.T) {
	caller := &fakeCaller{}
	want := []agenthub.AgentSession{{
		ID:        "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		AgentType: agenthub.AgentTypeMock,
		Status:    agenthub.SessionStatusIdle,
		CreatedAt: time.Unix(1, 0).UTC(),
		UpdatedAt: time.Unix(1, 0).UTC(),
	}}
	hub := &fakeAgentHub{list: want}
	srv := NewServer(agentHubTestDeps(caller, hub))

	_, v, err := srv.toolAgentHubListAgents(context.Background(), &sdk.CallToolRequest{}, struct{}{})
	if err != nil {
		t.Fatalf("agenthub.listAgents: %v", err)
	}
	got, ok := unwrap(v).([]agenthub.AgentSession)
	if !ok || len(got) != 1 || got[0].ID != want[0].ID {
		t.Fatalf("output = %#v", v)
	}
	if !hub.listCalled {
		t.Fatal("expected ListSessions")
	}
	if caller.last.subject != "builtin.agenthub" || caller.last.capability != "agenthub:read" || caller.last.scope != "session:list" {
		t.Fatalf("Call = %+v", caller.last)
	}
	if caller.last.metadata["origin"] != OriginMCP || caller.last.metadata["action"] != "list_agents" {
		t.Fatalf("meta = %v", caller.last.metadata)
	}
}

func TestAgentHubCreateAgentSessionTool(t *testing.T) {
	caller := &fakeCaller{}
	parentID := "11111111-2222-3333-4444-555555555555"
	created := agenthub.AgentSession{
		ID:        "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		AgentType: agenthub.AgentTypeMock,
		Status:    agenthub.SessionStatusIdle,
	}
	hub := &fakeAgentHub{created: created}
	srv := NewServer(agentHubTestDeps(caller, hub))

	title := "demo"
	_, v, err := srv.toolAgentHubCreateAgentSession(context.Background(), &sdk.CallToolRequest{}, agentHubCreateInput{
		AgentType:       "mock",
		Cwd:             "/tmp/proj",
		CallerSessionID: parentID,
		Title:           &title,
	})
	if err != nil {
		t.Fatalf("agenthub.createAgentSession: %v", err)
	}
	sess, ok := unwrap(v).(agenthub.AgentSession)
	if !ok || sess.ID != created.ID {
		t.Fatalf("output = %#v", v)
	}
	if hub.createIn.AgentType != agenthub.AgentTypeMock || hub.createIn.Cwd != "/tmp/proj" {
		t.Fatalf("CreateInput = %+v", hub.createIn)
	}
	if hub.createIn.ParentSessionID != parentID {
		t.Fatalf("ParentSessionID = %q, want callerSessionId mapped", hub.createIn.ParentSessionID)
	}
	if hub.createIn.Title == nil || *hub.createIn.Title != title {
		t.Fatalf("Title = %v", hub.createIn.Title)
	}
	if caller.last.subject != "builtin.agenthub" || caller.last.capability != "agenthub:execute" || caller.last.scope != "session:new" {
		t.Fatalf("Call = %+v", caller.last)
	}
	if caller.last.metadata["origin"] != OriginMCP || caller.last.metadata["action"] != "create_session" {
		t.Fatalf("meta = %v", caller.last.metadata)
	}
	if caller.last.metadata["agent_type"] != "mock" {
		t.Fatalf("agent_type = %v", caller.last.metadata["agent_type"])
	}
}

func TestAgentHubInvokeAgentTool(t *testing.T) {
	caller := &fakeCaller{}
	sessionID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	callerID := "11111111-2222-3333-4444-555555555555"
	hub := &fakeAgentHub{invokeOut: agenthub.InvokeResult{Status: "completed", Summary: "ok", TurnCount: 1}}
	srv := NewServer(agentHubTestDeps(caller, hub))

	_, v, err := srv.toolAgentHubInvokeAgent(context.Background(), &sdk.CallToolRequest{}, agentHubInvokeInput{
		SessionID:       sessionID,
		Prompt:          "hello",
		CallerSessionID: callerID,
	})
	if err != nil {
		t.Fatalf("agenthub.invokeAgent: %v", err)
	}
	out, ok := unwrap(v).(agenthub.InvokeResult)
	if !ok || out.Status != "completed" || out.Summary != "ok" {
		t.Fatalf("output = %#v", v)
	}
	if hub.invokeIn.SessionID != sessionID || hub.invokeIn.Prompt != "hello" || hub.invokeIn.CallerSessionID != callerID {
		t.Fatalf("InvokeInput = %+v", hub.invokeIn)
	}
	if caller.last.subject != "builtin.agenthub" || caller.last.capability != "agenthub:execute" || caller.last.scope != "session:"+sessionID {
		t.Fatalf("Call = %+v", caller.last)
	}
	if caller.last.metadata["origin"] != OriginMCP || caller.last.metadata["action"] != "invoke" {
		t.Fatalf("meta = %v", caller.last.metadata)
	}
	if caller.last.metadata["session_id"] != sessionID {
		t.Fatalf("session_id = %v", caller.last.metadata["session_id"])
	}
}

func TestAgentHubGetSessionStatusTool(t *testing.T) {
	caller := &fakeCaller{}
	sessionID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	hub := &fakeAgentHub{status: agenthub.AgentSession{ID: sessionID, Status: agenthub.SessionStatusRunning}}
	srv := NewServer(agentHubTestDeps(caller, hub))

	_, v, err := srv.toolAgentHubGetSessionStatus(context.Background(), &sdk.CallToolRequest{}, agentHubSessionIDInput{SessionID: sessionID})
	if err != nil {
		t.Fatalf("agenthub.getSessionStatus: %v", err)
	}
	sess, ok := unwrap(v).(agenthub.AgentSession)
	if !ok || sess.Status != agenthub.SessionStatusRunning {
		t.Fatalf("output = %#v", v)
	}
	if hub.statusID != sessionID {
		t.Fatalf("GetStatus id = %q", hub.statusID)
	}
	if caller.last.subject != "builtin.agenthub" || caller.last.capability != "agenthub:read" || caller.last.scope != "session:"+sessionID {
		t.Fatalf("Call = %+v", caller.last)
	}
	if caller.last.metadata["action"] != "get_status" || caller.last.metadata["session_id"] != sessionID {
		t.Fatalf("meta = %v", caller.last.metadata)
	}
}

func TestAgentHubCancelAgentTool(t *testing.T) {
	caller := &fakeCaller{}
	sessionID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	hub := &fakeAgentHub{}
	srv := NewServer(agentHubTestDeps(caller, hub))

	_, _, err := srv.toolAgentHubCancelAgent(context.Background(), &sdk.CallToolRequest{}, agentHubSessionIDInput{SessionID: sessionID})
	if err != nil {
		t.Fatalf("agenthub.cancelAgent: %v", err)
	}
	if hub.cancelID != sessionID {
		t.Fatalf("Cancel id = %q", hub.cancelID)
	}
	if caller.last.subject != "builtin.agenthub" || caller.last.capability != "agenthub:execute" || caller.last.scope != "session:"+sessionID {
		t.Fatalf("Call = %+v", caller.last)
	}
	if caller.last.metadata["action"] != "cancel" || caller.last.metadata["origin"] != OriginMCP {
		t.Fatalf("meta = %v", caller.last.metadata)
	}
}

func TestAgentHubSessionScopeInvalidID(t *testing.T) {
	caller := &fakeCaller{}
	hub := &fakeAgentHub{status: agenthub.AgentSession{ID: "raw"}}
	srv := NewServer(agentHubTestDeps(caller, hub))

	_, _, err := srv.toolAgentHubGetSessionStatus(context.Background(), &sdk.CallToolRequest{}, agentHubSessionIDInput{SessionID: "not-a-uuid"})
	if err != nil {
		t.Fatalf("getSessionStatus: %v", err)
	}
	if caller.last.scope != "session:invalid" {
		t.Fatalf("scope = %q, want session:invalid", caller.last.scope)
	}
	if caller.last.metadata["session_id"] != "invalid" {
		t.Fatalf("session_id meta = %v", caller.last.metadata["session_id"])
	}
	if hub.statusID != "not-a-uuid" {
		t.Fatalf("domain still receives raw id, got %q", hub.statusID)
	}
}

func TestAgentHubNilDepsReturnsUnavailable(t *testing.T) {
	caller := &fakeCaller{}
	deps := agentHubTestDeps(caller, nil)
	srv := NewServer(deps)

	_, _, err := srv.toolAgentHubListAgents(context.Background(), &sdk.CallToolRequest{}, struct{}{})
	if err == nil {
		t.Fatal("expected unavailable error when AgentHub is nil")
	}
	var app *apperrors.AppError
	if !errors.As(err, &app) || app.Code != "internal_error" {
		t.Fatalf("err = %v, want internal_error AppError", err)
	}
	if !strings.Contains(app.Message, "Agent Hub is not available") {
		t.Fatalf("message = %q", app.Message)
	}
}

func TestHubSessionID(t *testing.T) {
	valid := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	if got := hubSessionID(valid); got != valid {
		t.Fatalf("hubSessionID(%q) = %q", valid, got)
	}
	if got := hubSessionID("short"); got != "invalid" {
		t.Fatalf("hubSessionID(short) = %q", got)
	}
	if got := hubSessionID("AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE"); got != "invalid" {
		t.Fatalf("uppercase should be invalid, got %q", got)
	}
}

// Preview is the agent's own words for the person's sidebar; no MCP tool
// that returns a session may carry it.
func TestAgentHubToolsDoNotLeakPreview(t *testing.T) {
	const secret = "secret-ish"
	id := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	leaky := agenthub.AgentSession{ID: id, AgentType: agenthub.AgentTypeMock, Status: agenthub.SessionStatusIdle, Preview: secret}
	hub := &fakeAgentHub{list: []agenthub.AgentSession{leaky}, created: leaky, status: leaky}
	srv := NewServer(agentHubTestDeps(&fakeCaller{}, hub))
	ctx := context.Background()
	req := &sdk.CallToolRequest{}

	_, listed, err := srv.toolAgentHubListAgents(ctx, req, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	_, created, err := srv.toolAgentHubCreateAgentSession(ctx, req, agentHubCreateInput{AgentType: "mock", Cwd: "/tmp/proj"})
	if err != nil {
		t.Fatal(err)
	}
	_, status, err := srv.toolAgentHubGetSessionStatus(ctx, req, agentHubSessionIDInput{SessionID: id})
	if err != nil {
		t.Fatal(err)
	}
	for name, v := range map[string]any{"listAgents": listed, "createAgentSession": created, "getSessionStatus": status} {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), secret) {
			t.Fatalf("%s leaked the preview: %s", name, raw)
		}
	}
}

func TestAgentHubSendMessageTool(t *testing.T) {
	caller := &fakeCaller{}
	target := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	hub := &fakeAgentHub{
		route:   agenthub.PeerSameWorkspace,
		sendOut: agenthub.SendMessageResult{MessageID: "m1", Delivery: agenthub.DeliverySteered, SessionID: target},
	}
	srv := NewServer(agentHubTestDeps(caller, hub))

	_, v, err := srv.toolAgentHubSendMessage(context.Background(), &sdk.CallToolRequest{}, agentHubSendMessageInput{SessionID: target, Message: "héllo", ReplyTo: "m0"})
	if err != nil {
		t.Fatalf("agenthub.sendMessage: %v", err)
	}
	out, ok := unwrap(v).(agenthub.SendMessageResult)
	if !ok || out.Delivery != agenthub.DeliverySteered || out.MessageID != "m1" {
		t.Fatalf("output = %#v", v)
	}
	if hub.sendIn.SessionID != target || hub.sendIn.Message != "héllo" || hub.sendIn.ReplyTo != "m0" || hub.sendIn.CrossWorkspaceApproved {
		t.Fatalf("SendMessageInput = %+v", hub.sendIn)
	}
	// A write on the session it names, audited without the text.
	if caller.last.subject != "builtin.agenthub" || caller.last.capability != "agenthub:execute" || caller.last.scope != "session:"+target {
		t.Fatalf("Call = %+v", caller.last)
	}
	if caller.last.metadata["action"] != "send_message" || caller.last.metadata["length"] != 5 || caller.last.metadata["session_id"] != target {
		t.Fatalf("meta = %v", caller.last.metadata)
	}
	for k, v := range caller.last.metadata {
		if str, ok := v.(string); ok && strings.Contains(str, "héllo") {
			t.Fatalf("the audit metadata %q carries the message text", k)
		}
	}
}

func TestAgentHubSendMessageScopeNeverCarriesRawInput(t *testing.T) {
	caller := &fakeCaller{}
	hub := &fakeAgentHub{route: agenthub.PeerSameWorkspace}
	srv := NewServer(agentHubTestDeps(caller, hub))
	if _, _, err := srv.toolAgentHubSendMessage(context.Background(), &sdk.CallToolRequest{}, agentHubSendMessageInput{SessionID: "x:*", Message: "m"}); err != nil {
		t.Fatal(err)
	}
	if caller.last.scope != "session:invalid" {
		t.Fatalf("scope = %q, want session:invalid for a malformed id", caller.last.scope)
	}
}

func TestAgentHubSendMessageOutsideTheWorkspaceAsksThePerson(t *testing.T) {
	target := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	title := "gamma"
	newHub := func() *fakeAgentHub {
		return &fakeAgentHub{
			route:   agenthub.PeerElsewhere,
			routeTo: agenthub.AgentSession{ID: target, Title: &title},
			sendOut: agenthub.SendMessageResult{MessageID: "m1", Delivery: agenthub.DeliveryStarted},
		}
	}
	answer := func(t *testing.T, h *interaction.Hub, choice string) {
		t.Helper()
		go func() {
			waitForPending(t, h)
			p := h.Pending()
			if p.AllowAlwaysOffered || p.Tool != "agenthub.sendMessage" || !strings.Contains(p.Summary, "walk the dog") || !strings.Contains(p.Summary, "gamma") {
				t.Errorf("pending = %+v: it must name the target, show the text and never offer allow-always", p)
			}
			if err := h.Complete(p.ID, choice); err != nil {
				t.Errorf("complete: %v", err)
			}
		}()
	}

	t.Run("allowed", func(t *testing.T) {
		hub := newHub()
		deps := agentHubTestDeps(&fakeCaller{}, hub)
		deps.Hub = interaction.NewHub(interaction.NoopPrompter{})
		answer(t, deps.Hub, "allow")
		_, v, err := NewServer(deps).toolAgentHubSendMessage(context.Background(), &sdk.CallToolRequest{}, agentHubSendMessageInput{SessionID: target, Message: "walk the dog"})
		if err != nil || unwrap(v).(agenthub.SendMessageResult).Delivery != agenthub.DeliveryStarted {
			t.Fatalf("v = %#v, err = %v", v, err)
		}
		if !hub.sendIn.CrossWorkspaceApproved {
			t.Fatal("the person said yes and the service was not told")
		}
	})
	t.Run("declined", func(t *testing.T) {
		hub := newHub()
		deps := agentHubTestDeps(&fakeCaller{}, hub)
		deps.Hub = interaction.NewHub(interaction.NoopPrompter{})
		answer(t, deps.Hub, "deny")
		_, v, err := NewServer(deps).toolAgentHubSendMessage(context.Background(), &sdk.CallToolRequest{}, agentHubSendMessageInput{SessionID: target, Message: "walk the dog"})
		if err != nil {
			t.Fatal(err)
		}
		if out := unwrap(v).(agenthub.SendMessageResult); out.Delivery != agenthub.DeliveryRefused || out.Code != agenthub.PeerDeclined {
			t.Fatalf("out = %+v", out)
		}
		if hub.sendCalled {
			t.Fatal("a declined message was sent anyway")
		}
	})
	t.Run("headless", func(t *testing.T) {
		hub := newHub()
		deps := agentHubTestDeps(&fakeCaller{}, hub)
		_, _, err := NewServer(deps).toolAgentHubSendMessage(context.Background(), &sdk.CallToolRequest{}, agentHubSendMessageInput{SessionID: target, Message: "walk the dog"})
		if err == nil || hub.sendCalled {
			t.Fatalf("with no one to ask the message must not go: err = %v, sent = %v", err, hub.sendCalled)
		}
	})
}

func TestAgentHubListPeersTool(t *testing.T) {
	caller := &fakeCaller{}
	hub := &fakeAgentHub{peers: []agenthub.PeerInfo{{SessionID: "s1", Title: "alpha", Relation: agenthub.PeerSameWorkspace}}}
	srv := NewServer(agentHubTestDeps(caller, hub))
	_, v, err := srv.toolAgentHubListPeers(context.Background(), &sdk.CallToolRequest{}, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := unwrap(v).([]agenthub.PeerInfo); !ok || len(got) != 1 || got[0].Title != "alpha" {
		t.Fatalf("output = %#v", v)
	}
	if caller.last.capability != "agenthub:read" || caller.last.scope != "session:list" || caller.last.metadata["action"] != "list_peers" {
		t.Fatalf("Call = %+v", caller.last)
	}
}

// The grants the container is tested against are the ones the tools ask for.
func TestPeerMessageGrantsAreWhatTheToolsAsk(t *testing.T) {
	caller := &fakeCaller{}
	hub := &fakeAgentHub{route: agenthub.PeerSameWorkspace}
	srv := NewServer(agentHubTestDeps(caller, hub))
	id := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	grants := PeerMessageGrants()

	if _, _, err := srv.toolAgentHubListPeers(context.Background(), &sdk.CallToolRequest{}, struct{}{}); err != nil {
		t.Fatal(err)
	}
	if got := [3]string{caller.last.subject, caller.last.capability, caller.last.scope}; got != grants[0] {
		t.Errorf("listPeers asked %v, grants say %v", got, grants[0])
	}
	if _, _, err := srv.toolAgentHubSendMessage(context.Background(), &sdk.CallToolRequest{}, agentHubSendMessageInput{SessionID: id, Message: "m"}); err != nil {
		t.Fatal(err)
	}
	if got := [3]string{caller.last.subject, caller.last.capability, caller.last.scope}; got != grants[1] {
		t.Errorf("sendMessage asked %v, grants say %v", got, grants[1])
	}
}
func TestAgentHubCreateSessionCarriesTheModelChoice(t *testing.T) {
	caller := &fakeCaller{}
	hub := &fakeAgentHub{created: agenthub.AgentSession{ID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"}}
	srv := NewServer(agentHubTestDeps(caller, hub))
	_, _, err := srv.toolAgentHubCreateAgentSession(context.Background(), &sdk.CallToolRequest{}, agentHubCreateInput{
		AgentType: "codex", Cwd: "/tmp/proj",
		InitialConfig: &agentHubInitialConfig{Model: " gpt-x ", Effort: "low"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := hub.createIn.InitialConfig; got["model"] != "gpt-x" || got["reasoning_effort"] != "low" || len(got) != 2 {
		t.Fatalf("InitialConfig = %v, want model and reasoning_effort", got)
	}
	if hub.createIn.ModelChoiceApproved {
		t.Fatal("a choice that needs no approval was marked approved")
	}
	if caller.last.metadata["model"] != "gpt-x" || caller.last.metadata["effort"] != "low" {
		t.Fatalf("audit = %v", caller.last.metadata)
	}
}

func TestAgentHubCreateSessionByProfileRecordsTheProfileNotThePrompt(t *testing.T) {
	caller := &fakeCaller{}
	hub := &fakeAgentHub{created: agenthub.AgentSession{ID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"}}
	srv := NewServer(agentHubTestDeps(caller, hub))
	if _, _, err := srv.toolAgentHubCreateAgentSession(context.Background(), &sdk.CallToolRequest{}, agentHubCreateInput{
		AgentType: "claude", Cwd: "/tmp/proj", Profile: "Code review",
	}); err != nil {
		t.Fatal(err)
	}
	if hub.createIn.Profile != "Code review" || hub.planIn.Profile != "Code review" {
		t.Fatalf("profile = %q / %q", hub.createIn.Profile, hub.planIn.Profile)
	}
	if caller.last.metadata["profile"] != "Code review" {
		t.Fatalf("audit = %v, want the profile named", caller.last.metadata)
	}
}

// A choice beyond the caller's own asks the person, with no allow_always, and
// nothing is created if they say no.
func TestAgentHubCreateSessionAsksBeforeAChoiceAboveTheCallers(t *testing.T) {
	hub := &fakeAgentHub{
		created: agenthub.AgentSession{ID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"},
		plan:    agenthub.ConfigPlan{Config: map[string]string{"model": "big"}, NeedsApproval: true, Why: []string{"model big (this session runs mid)"}},
	}
	for _, answer := range []string{"deny", "allow"} {
		t.Run(answer, func(t *testing.T) {
			hub.createIn = agenthub.CreateInput{}
			h := interaction.NewHub(interaction.NoopPrompter{})
			deps := agentHubTestDeps(&fakeCaller{}, hub)
			deps.Hub = h
			srv := NewServer(deps)
			go func() {
				waitForPending(t, h)
				p := h.Pending()
				if p.Kind != interaction.KindAgent || p.AllowAlwaysOffered || !strings.Contains(p.Summary, "model big") {
					t.Errorf("pending = %+v: want an agent question without allow_always naming the choice", p)
				}
				_ = h.Complete(p.ID, answer)
			}()
			_, _, err := srv.toolAgentHubCreateAgentSession(context.Background(), &sdk.CallToolRequest{}, agentHubCreateInput{
				AgentType: "mock", Cwd: "/tmp/proj", InitialConfig: &agentHubInitialConfig{Model: "big"},
			})
			if answer == "deny" {
				if err == nil || hub.createIn.AgentType != "" {
					t.Fatalf("a refused choice created a session (err %v, input %+v)", err, hub.createIn)
				}
				return
			}
			if err != nil || !hub.createIn.ModelChoiceApproved {
				t.Fatalf("approved choice: err %v, approved %v", err, hub.createIn.ModelChoiceApproved)
			}
		})
	}
}

func TestAgentHubCreateSessionPassesAPlanError(t *testing.T) {
	hub := &fakeAgentHub{planErr: errors.New("agenthub.invalidConfigOption: mock has no model \"x\" (it offers: a, b)")}
	srv := NewServer(agentHubTestDeps(&fakeCaller{}, hub))
	_, _, err := srv.toolAgentHubCreateAgentSession(context.Background(), &sdk.CallToolRequest{}, agentHubCreateInput{AgentType: "mock", Cwd: "/tmp/p", InitialConfig: &agentHubInitialConfig{Model: "x"}})
	if err == nil || !strings.Contains(err.Error(), "it offers: a, b") || hub.createIn.AgentType != "" {
		t.Fatalf("err = %v, created %+v", err, hub.createIn)
	}
}

func TestAgentHubModelToolsReadThroughTheirOwnGrants(t *testing.T) {
	caller := &fakeCaller{}
	hub := &fakeAgentHub{
		models:   agenthub.ModelListing{Vendors: []agenthub.VendorModels{{Vendor: "claude", Status: agenthub.ModelsOK}}},
		profiles: agenthub.ModelProfilesListing{Profiles: []agenthub.ModelProfileInfo{{Name: "Review"}}, Note: agenthub.ModelProfilesNote},
	}
	srv := NewServer(agentHubTestDeps(caller, hub))
	_, v, err := srv.toolAgentHubListModels(context.Background(), &sdk.CallToolRequest{}, agentHubListModelsInput{AgentType: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := unwrap(v).(agenthub.ModelListing); !ok || len(got.Vendors) != 1 || hub.modelsFor != "claude" {
		t.Fatalf("listModels = %#v for %q", v, hub.modelsFor)
	}
	if caller.last.capability != "agenthub:read" || caller.last.scope != "models:list" {
		t.Fatalf("Call = %+v", caller.last)
	}
	_, v, err = srv.toolAgentHubListModelProfiles(context.Background(), &sdk.CallToolRequest{}, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := unwrap(v).(agenthub.ModelProfilesListing); !ok || got.Profiles[0].Name != "Review" || got.Note == "" {
		t.Fatalf("listModelProfiles = %#v", v)
	}
	if caller.last.capability != "agenthub:read" || caller.last.scope != "profiles:list" {
		t.Fatalf("Call = %+v", caller.last)
	}
}
