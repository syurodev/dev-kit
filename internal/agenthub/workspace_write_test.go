package agenthub_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/identity"
	"devkit/internal/storage"
)

type grantRig struct {
	svc                  *agenthub.Service
	root                 agenthub.AgentSession
	primary, audit, away string
	folders              []agenthub.WorkspaceFolder
}

func newGrantRig(t *testing.T) *grantRig {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	// The session row's workspace_id is a foreign key.
	if _, err := db.Exec("INSERT INTO workspaces (id, name, root) VALUES ('ws1', 'ws1', '')"); err != nil {
		t.Fatal(err)
	}
	reg := agenthub.NewRegistry()
	reg.Register(agenthub.NewMockAdapter())
	svc := agenthub.NewService(reg, agenthub.NewSQLStore(db), nil)
	svc.MCPEndpoints = &fakeEndpoints{}
	base := t.TempDir()
	r := &grantRig{svc: svc, primary: filepath.Join(base, "event-router"), audit: filepath.Join(base, "audit"), away: filepath.Join(base, "elsewhere")}
	for _, d := range []string{r.primary, r.audit, r.away} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	r.folders = []agenthub.WorkspaceFolder{{Name: "event-router", Path: r.primary}, {Name: "fsap-audit-service", Path: r.audit}}
	svc.MatchingWorkspaces = func(cwd string) ([]string, error) {
		if cwd == r.away {
			return nil, nil
		}
		return []string{"ws1"}, nil
	}
	svc.WorkspaceFolders = func(id string) (string, []agenthub.WorkspaceFolder, error) {
		if id != "ws1" {
			return "", nil, nil
		}
		return "fsap-event", r.folders, nil
	}
	root, err := svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: r.primary, WorkspaceID: "ws1"})
	if err != nil {
		t.Fatal(err)
	}
	r.root = root
	return r
}

func isRefused(err error) bool {
	var refused *agenthub.GrantRefusedError
	return errors.As(err, &refused)
}

func TestPrepareWorkspaceWritePlansAFolderOfTheWorkspace(t *testing.T) {
	r := newGrantRig(t)
	plan, err := r.svc.PrepareWorkspaceWrite(agentCtx(r.root.ID), "fsap-audit-service")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Path != r.audit || plan.WorkspaceName != "fsap-event" || plan.SessionID != r.root.ID || plan.AlreadyGranted {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestPrepareWorkspaceWriteNotesAFolderAlreadyWritable(t *testing.T) {
	r := newGrantRig(t)
	plan, err := r.svc.PrepareWorkspaceWrite(agentCtx(r.root.ID), "event-router") // the session's own cwd
	if err != nil || !plan.AlreadyGranted {
		t.Fatalf("plan = %+v, err = %v; the cwd is already writable", plan, err)
	}
}

func TestPrepareWorkspaceWriteRefusals(t *testing.T) {
	r := newGrantRig(t)
	loose, err := r.svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: r.away})
	if err != nil {
		t.Fatal(err)
	}
	kid, err := r.svc.CreateSession(agentCtx(r.root.ID), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock})
	if err != nil {
		t.Fatal(err)
	}
	closed, err := r.svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: r.audit, WorkspaceID: "ws1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.svc.CloseSession(uiCtx(), closed.ID); err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	cases := []struct {
		name   string
		ctx    context.Context
		folder string
		mutate func()
	}{
		{"a terminal caller", identity.WithCaller(context.Background(), identity.UserTerminal()), "fsap-audit-service", nil},
		{"the window", uiCtx(), "fsap-audit-service", nil},
		{"no folder named", agentCtx(r.root.ID), "  ", nil},
		{"a folder that is not in the workspace", agentCtx(r.root.ID), "not-a-service", nil},
		{"a sub-session", agentCtx(kid.ID), "fsap-audit-service", nil},
		{"a session outside every workspace", agentCtx(loose.ID), "fsap-audit-service", nil},
		{"a closed session", agentCtx(closed.ID), "fsap-audit-service", nil},
		{"the filesystem root", agentCtx(r.root.ID), "fsap-audit-service", func() {
			r.folders[1].Path = "/"
		}},
		{"the home folder", agentCtx(r.root.ID), "fsap-audit-service", func() {
			r.folders[1].Path = home
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			saved := r.folders[1].Path
			t.Cleanup(func() { r.folders[1].Path = saved })
			if c.mutate != nil {
				c.mutate()
			}
			if _, err := r.svc.PrepareWorkspaceWrite(c.ctx, c.folder); !isRefused(err) {
				t.Fatalf("err = %v, want a *GrantRefusedError", err)
			}
		})
	}
}

func TestCommitWorkspaceWriteStoresTheFolder(t *testing.T) {
	r := newGrantRig(t)
	ctx := agentCtx(r.root.ID)
	plan, err := r.svc.PrepareWorkspaceWrite(ctx, "fsap-audit-service")
	if err != nil {
		t.Fatal(err)
	}
	res, err := r.svc.CommitWorkspaceWrite(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Granted || res.AlreadyGranted || res.Effective.Git != agenthub.EffectNow || res.Effective.NewSessions != agenthub.EffectNow ||
		res.Effective.ShellAndFiles != agenthub.EffectAfterThisTurn {
		t.Fatalf("result = %+v", res)
	}
	if got := rowExtras(t, r.svc, r.root.ID); !slices.Equal(got, []string{r.audit}) {
		t.Fatalf("stored extras = %v", got)
	}
	// Asking again changes nothing and asks nobody.
	again, err := r.svc.PrepareWorkspaceWrite(ctx, "fsap-audit-service")
	if err != nil || !again.AlreadyGranted {
		t.Fatalf("second plan = %+v, %v", again, err)
	}
	res, err = r.svc.CommitWorkspaceWrite(ctx, again)
	if err != nil || !res.AlreadyGranted || len(rowExtras(t, r.svc, r.root.ID)) != 1 {
		t.Fatalf("second commit = %+v, %v", res, err)
	}
}

func TestCommitWorkspaceWriteRefusesAnotherSessionsPlan(t *testing.T) {
	r := newGrantRig(t)
	plan, err := r.svc.PrepareWorkspaceWrite(agentCtx(r.root.ID), "fsap-audit-service")
	if err != nil {
		t.Fatal(err)
	}
	other, err := r.svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: r.audit, WorkspaceID: "ws1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.CommitWorkspaceWrite(agentCtx(other.ID), plan); !isRefused(err) {
		t.Fatalf("err = %v, want a refusal", err)
	}
	if got := rowExtras(t, r.svc, r.root.ID); len(got) != 0 {
		t.Fatalf("a refused commit stored %v", got)
	}
}

func TestCommitWorkspaceWriteRechecksAfterTheWait(t *testing.T) {
	t.Run("the service was removed from the workspace", func(t *testing.T) {
		r := newGrantRig(t)
		ctx := agentCtx(r.root.ID)
		plan, _ := r.svc.PrepareWorkspaceWrite(ctx, "fsap-audit-service")
		r.folders = r.folders[:1]
		r.svc.WorkspaceFolders = func(string) (string, []agenthub.WorkspaceFolder, error) { return "fsap-event", r.folders, nil }
		if _, err := r.svc.CommitWorkspaceWrite(ctx, plan); !isRefused(err) {
			t.Fatalf("err = %v, want a refusal", err)
		}
		if got := rowExtras(t, r.svc, r.root.ID); len(got) != 0 {
			t.Fatalf("stored %v", got)
		}
	})
	t.Run("the service now points elsewhere", func(t *testing.T) {
		r := newGrantRig(t)
		ctx := agentCtx(r.root.ID)
		plan, _ := r.svc.PrepareWorkspaceWrite(ctx, "fsap-audit-service")
		r.folders[1].Path = r.away
		if _, err := r.svc.CommitWorkspaceWrite(ctx, plan); !isRefused(err) {
			t.Fatalf("err = %v, want a refusal: the person approved another folder", err)
		}
		if got := rowExtras(t, r.svc, r.root.ID); len(got) != 0 {
			t.Fatalf("stored %v", got)
		}
	})
	t.Run("the session was closed", func(t *testing.T) {
		r := newGrantRig(t)
		ctx := agentCtx(r.root.ID)
		plan, _ := r.svc.PrepareWorkspaceWrite(ctx, "fsap-audit-service")
		if err := r.svc.CloseSession(uiCtx(), r.root.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := r.svc.CommitWorkspaceWrite(ctx, plan); !isRefused(err) {
			t.Fatalf("err = %v, want a refusal", err)
		}
	})
}

// A sub-session keeps the copy it took when it was created; one created after the
// grant takes the parent's new list (service.go inheritedExtraPaths).
func TestAGrantReachesOnlyChildrenCreatedAfterIt(t *testing.T) {
	r := newGrantRig(t)
	ctx := agentCtx(r.root.ID)
	before, err := r.svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock})
	if err != nil {
		t.Fatal(err)
	}
	plan, _ := r.svc.PrepareWorkspaceWrite(ctx, "fsap-audit-service")
	if _, err := r.svc.CommitWorkspaceWrite(ctx, plan); err != nil {
		t.Fatal(err)
	}
	after, err := r.svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock})
	if err != nil {
		t.Fatal(err)
	}
	if got := rowExtras(t, r.svc, before.ID); len(got) != 0 {
		t.Fatalf("the earlier child has %v: a grant must not widen an existing child", got)
	}
	if got := rowExtras(t, r.svc, after.ID); !slices.Equal(got, []string{r.audit}) {
		t.Fatalf("the later child has %v, want the parent's list", got)
	}
}
