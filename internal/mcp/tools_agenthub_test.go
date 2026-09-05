package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/apperrors"

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
