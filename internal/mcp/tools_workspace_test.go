package mcp

import (
	"context"
	"errors"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/identity"
	"devkit/internal/workspace"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type fakeWorkspaces struct {
	workspaces []workspace.Workspace
	services   map[string][]workspace.WorkspaceService
}

func (f fakeWorkspaces) ListWorkspaces() ([]workspace.Workspace, error) { return f.workspaces, nil }

func (f fakeWorkspaces) ListServices(id string) ([]workspace.WorkspaceService, error) {
	return f.services[id], nil
}

const (
	kong   = "/work/g-platform/kong-plugin"
	auth   = "/work/g-platform/fsap-authentication-service"
	config = "/work/g-platform/fsap-config-service"
	other  = "/work/billing/billing-service"
)

func workspaceFixture() fakeWorkspaces {
	return fakeWorkspaces{
		workspaces: []workspace.Workspace{
			{ID: "ws-security", Name: "fsap-security", Root: kong},
			{ID: "ws-billing", Name: "billing", Root: other},
		},
		services: map[string][]workspace.WorkspaceService{
			"ws-security": {
				{Name: "kong-plugin", Path: kong},
				{Name: "fsap-authentication-service", Path: auth},
				{Name: "fsap-config-service", Path: config},
			},
			"ws-billing": {{Name: "billing-service", Path: other}},
		},
	}
}

func workspaceServer(caller *fakeCaller, hub AgentHubAPI) *Server {
	d := agentHubTestDeps(caller, hub)
	d.Workspace = workspaceFixture()
	return NewServer(d)
}

// A terminal CLI sees every workspace, folders primary first, and the call
// goes through the gateway as a read of the workspace list.
func TestWorkspaceListForTheTerminal(t *testing.T) {
	caller := &fakeCaller{}
	srv := workspaceServer(caller, &fakeAgentHub{})
	ctx := identity.WithCaller(context.Background(), identity.UserTerminal())
	_, v, err := srv.toolWorkspaceList(ctx, &sdk.CallToolRequest{}, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := unwrap(v).(WorkspaceListResult)
	if !ok || len(got.Workspaces) != 2 || got.Cwd != "" {
		t.Fatalf("output = %#v", v)
	}
	sec := got.Workspaces[0]
	if sec.Name != "fsap-security" || len(sec.Folders) != 3 || !sec.Folders[0].Primary || sec.Folders[0].Path != kong || sec.Folders[1].Primary {
		t.Fatalf("fsap-security = %+v", sec)
	}
	for _, f := range sec.Folders {
		if f.Granted {
			t.Fatalf("granted means the calling session's grant; the terminal has none: %+v", f)
		}
	}
	if caller.last.subject != "builtin.workspace" || caller.last.capability != "ws:read" || caller.last.scope != "workspace:list" {
		t.Fatalf("Call = %+v", caller.last)
	}
	if caller.last.metadata["origin"] != OriginMCP || caller.last.metadata["action"] != "list_workspaces" {
		t.Fatalf("meta = %v", caller.last.metadata)
	}
}

// An agent sees only the workspace its folders are in, its cwd, and which
// folders its session may write.
func TestWorkspaceListForAnAgentSession(t *testing.T) {
	const sid = "11111111-2222-3333-4444-555555555555"
	hub := &fakeAgentHub{status: agenthub.AgentSession{ID: sid, ProjectPath: kong, ExtraReadWritePaths: []string{auth}}}
	srv := workspaceServer(&fakeCaller{}, hub)
	ctx := identity.WithCaller(context.Background(), identity.Agent(sid))
	_, v, err := srv.toolWorkspaceList(ctx, &sdk.CallToolRequest{}, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	got := unwrap(v).(WorkspaceListResult)
	if hub.statusID != sid {
		t.Fatalf("looked up session %q, want the caller's own", hub.statusID)
	}
	if got.Cwd != kong || len(got.Workspaces) != 1 || got.Workspaces[0].ID != "ws-security" {
		t.Fatalf("output = %+v, want only fsap-security and cwd kong-plugin", got)
	}
	granted := map[string]bool{}
	for _, f := range got.Workspaces[0].Folders {
		granted[f.Path] = f.Granted
	}
	if !granted[kong] || !granted[auth] || granted[config] {
		t.Fatalf("granted = %v, want kong and auth only", granted)
	}
}

// A session working in a subfolder of one service still belongs to the
// workspace; a folder name that merely starts the same does not.
func TestWorkspaceListMatchesFoldersNotPrefixes(t *testing.T) {
	const sid = "11111111-2222-3333-4444-555555555555"
	deep := &fakeAgentHub{status: agenthub.AgentSession{ID: sid, ProjectPath: config + "/src/main"}}
	ctx := identity.WithCaller(context.Background(), identity.Agent(sid))
	_, v, err := workspaceServer(&fakeCaller{}, deep).toolWorkspaceList(ctx, &sdk.CallToolRequest{}, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if got := unwrap(v).(WorkspaceListResult); len(got.Workspaces) != 1 || got.Workspaces[0].ID != "ws-security" {
		t.Fatalf("subfolder: %+v", got)
	}
	lookalike := &fakeAgentHub{status: agenthub.AgentSession{ID: sid, ProjectPath: kong + "-v2"}}
	_, v, err = workspaceServer(&fakeCaller{}, lookalike).toolWorkspaceList(ctx, &sdk.CallToolRequest{}, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if got := unwrap(v).(WorkspaceListResult); len(got.Workspaces) != 0 {
		t.Fatalf("kong-plugin-v2 is not in kong-plugin's workspace: %+v", got)
	}
}

// The gateway's refusal and a missing registry are errors, not an empty list.
func TestWorkspaceListErrors(t *testing.T) {
	denied := &fakeCaller{err: errors.New("permission denied")}
	if _, _, err := workspaceServer(denied, &fakeAgentHub{}).toolWorkspaceList(context.Background(), &sdk.CallToolRequest{}, struct{}{}); err == nil {
		t.Fatal("a refused call must be an error")
	}
	d := agentHubTestDeps(&fakeCaller{}, &fakeAgentHub{})
	if _, _, err := NewServer(d).toolWorkspaceList(context.Background(), &sdk.CallToolRequest{}, struct{}{}); err == nil {
		t.Fatal("no registry must be an error")
	}
}
