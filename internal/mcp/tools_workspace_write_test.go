package mcp

import (
	"context"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
	"devkit/internal/identity"
	"devkit/internal/interaction"
)

type fakeWorkspaceWrite struct {
	plan      agenthub.WriteGrantPlan
	prepErr   error
	commitErr error
	prepared  int
	committed int
}

func (f *fakeWorkspaceWrite) PrepareWorkspaceWrite(_ context.Context, folder string) (agenthub.WriteGrantPlan, error) {
	f.prepared++
	f.plan.Folder = folder
	return f.plan, f.prepErr
}

func (f *fakeWorkspaceWrite) CommitWorkspaceWrite(_ context.Context, plan agenthub.WriteGrantPlan) (agenthub.WriteGrantResult, error) {
	f.committed++
	return agenthub.WriteGrantResult{Folder: plan.Folder, Granted: true,
		Effective: agenthub.WriteGrantEffect{Git: agenthub.EffectNow, NewSessions: agenthub.EffectNow, ShellAndFiles: agenthub.EffectAfterThisTurn}}, f.commitErr
}

const grantSID = "11111111-2222-3333-4444-555555555555"

// hubAnswering is a hub whose agent confirmation answers with answer (or err).
func hubAnswering(answer string, err error, asked *int) *interaction.Hub {
	h := interaction.NewHub(interaction.NoopPrompter{})
	h.ConfirmAgent = func(context.Context, interaction.ConfirmRequest) (string, error) {
		*asked++
		return answer, err
	}
	return h
}

func grantServer(t *testing.T, caller *fakeCaller, w WorkspaceWriteAPI, hub *interaction.Hub) *Server {
	t.Helper()
	d := agentHubTestDeps(caller, &fakeAgentHub{})
	d.WorkspaceWrite = w
	d.Hub = hub
	return NewServer(d)
}

func callGrant(srv *Server, folder string) (any, error) {
	ctx := identity.WithCaller(context.Background(), identity.Agent(grantSID))
	_, v, err := srv.toolWorkspaceRequestWrite(ctx, &sdk.CallToolRequest{}, workspaceRequestWriteInput{Folder: folder})
	return v, err
}

func TestRequestWriteAsksThenStores(t *testing.T) {
	asked, caller := 0, &fakeCaller{}
	w := &fakeWorkspaceWrite{plan: agenthub.WriteGrantPlan{SessionID: grantSID, Path: "/w/audit", WorkspaceName: "fsap-event"}}
	v, err := callGrant(grantServer(t, caller, w, hubAnswering("allow", nil, &asked)), "fsap-audit-service")
	if err != nil {
		t.Fatal(err)
	}
	got, ok := unwrap(v).(agenthub.WriteGrantResult)
	if !ok || !got.Granted || got.Effective.ShellAndFiles != agenthub.EffectAfterThisTurn {
		t.Fatalf("result = %#v", v)
	}
	if asked != 1 || w.committed != 1 {
		t.Fatalf("asked %d times, committed %d times; want one each", asked, w.committed)
	}
	if _, recorded := caller.last.metadata["folder"]; recorded || caller.last.metadata["action"] != "request_workspace_write" {
		t.Fatalf("audit metadata = %v: it carries the action and origin, never the agent's folder string", caller.last.metadata)
	}
	if caller.last.subject != "builtin.workspace" || caller.last.capability != "ws:grant" || caller.last.scope != "workspace:requestWrite" {
		t.Fatalf("Call = %+v", caller.last)
	}
}

func TestRequestWriteOfAWritableFolderAsksNobody(t *testing.T) {
	asked := 0
	w := &fakeWorkspaceWrite{plan: agenthub.WriteGrantPlan{SessionID: grantSID, AlreadyGranted: true}}
	if _, err := callGrant(grantServer(t, &fakeCaller{}, w, hubAnswering("allow", nil, &asked)), "x"); err != nil {
		t.Fatal(err)
	}
	if asked != 0 {
		t.Fatal("the person was asked about a folder the session can already write")
	}
}

func TestRequestWriteRefusals(t *testing.T) {
	cases := []struct {
		name      string
		w         *fakeWorkspaceWrite
		hub       func(*int) *interaction.Hub
		wantCode  string
		wantAsked int
		wantStore int
	}{
		{"a refused plan", &fakeWorkspaceWrite{prepErr: &agenthub.GrantRefusedError{Reason: "no"}}, func(a *int) *interaction.Hub { return hubAnswering("allow", nil, a) }, "workspace.grantRefused", 0, 0},
		{"the person declines", &fakeWorkspaceWrite{}, func(a *int) *interaction.Hub { return hubAnswering("deny", nil, a) }, "workspace.grantDeclined", 1, 0},
		{"nobody can answer", &fakeWorkspaceWrite{}, func(*int) *interaction.Hub { return &interaction.Hub{Headless: true} }, "workspace.grantUnavailable", 0, 0},
		{"a refusal at commit", &fakeWorkspaceWrite{commitErr: &agenthub.GrantRefusedError{Reason: "moved"}}, func(a *int) *interaction.Hub { return hubAnswering("allow", nil, a) }, "workspace.grantRefused", 1, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			asked := 0
			_, err := callGrant(grantServer(t, &fakeCaller{}, c.w, c.hub(&asked)), "fsap-audit-service")
			if got := errCode(err); got != c.wantCode {
				t.Fatalf("code = %q (%v), want %q", got, err, c.wantCode)
			}
			if asked != c.wantAsked || c.w.committed != c.wantStore {
				t.Fatalf("asked %d, committed %d; want %d and %d", asked, c.w.committed, c.wantAsked, c.wantStore)
			}
		})
	}
}

// allow_always must never short-circuit this question.
func TestRequestWriteIsNeverRemembered(t *testing.T) {
	asked := 0
	w := &fakeWorkspaceWrite{}
	srv := grantServer(t, &fakeCaller{}, w, hubAnswering("allow_always", nil, &asked))
	for range 2 {
		if _, err := callGrant(srv, "fsap-audit-service"); err != nil {
			t.Fatal(err)
		}
	}
	if asked != 2 {
		t.Fatalf("asked %d times for two requests", asked)
	}
}

func TestRequestWriteWithoutTheServiceSaysSo(t *testing.T) {
	d := agentHubTestDeps(&fakeCaller{}, &fakeAgentHub{})
	if _, err := callGrant(NewServer(d), "x"); errCode(err) != "internal_error" {
		t.Fatalf("err = %v", err)
	}
}

// The tool shares the `workspace` harness group with workspace.list: a session that
// turned the group off cannot call it (serving.go answers harness.groupOff).
func TestRequestWriteIsInTheWorkspaceHarnessGroup(t *testing.T) {
	if harness.Group("workspace.requestWrite") != harness.Group("workspace.list") {
		t.Fatal("workspace.requestWrite left the workspace group")
	}
}
