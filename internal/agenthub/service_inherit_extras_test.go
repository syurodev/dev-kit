package agenthub_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/storage"
)

// A session an agent creates may write what its parent may write: the
// parent's project (or a folder in it) plus the parent's extra read-write
// folders — read from the parent's stored row, never from the tool input.

func rowExtras(t *testing.T, svc *agenthub.Service, id string) []string {
	t.Helper()
	sess, err := svc.GetStatus(uiCtx(), id)
	if err != nil {
		t.Fatal(err)
	}
	return sess.ExtraReadWritePaths
}

func TestAgentChildInheritsParentExtraPaths(t *testing.T) {
	svc, mock, _ := newIdentityService(t)
	extraA, extraB := projectDir(t), projectDir(t)
	parent, err := svc.CreateSession(uiCtx(), agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t), ExtraReadWritePaths: []string{extraA, extraB},
	})
	if err != nil {
		t.Fatal(err)
	}
	child, err := svc.CreateSession(agentCtx(parent.ID), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{extraA, extraB}
	if got := mock.LastSpec().ExtraReadWritePaths; !slices.Equal(got, want) {
		t.Errorf("OpenSpec extras = %v, want %v", got, want)
	}
	if got := rowExtras(t, svc, child.ID); !slices.Equal(got, want) {
		t.Errorf("stored extras = %v, want %v", got, want)
	}
	if child.ProjectPath != parent.ProjectPath {
		t.Errorf("child cwd = %s, want the parent's %s", child.ProjectPath, parent.ProjectPath)
	}
}

func TestAgentChildOfParentWithoutExtrasGetsNone(t *testing.T) {
	svc, mock, _ := newIdentityService(t)
	parent, err := svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t)})
	if err != nil {
		t.Fatal(err)
	}
	child, err := svc.CreateSession(agentCtx(parent.ID), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock})
	if err != nil {
		t.Fatal(err)
	}
	if got := mock.LastSpec().ExtraReadWritePaths; len(got) != 0 {
		t.Errorf("OpenSpec extras = %v, want none", got)
	}
	if got := rowExtras(t, svc, child.ID); len(got) != 0 {
		t.Errorf("stored extras = %v, want none", got)
	}
}

// An agent can never supply paths: its own list is ignored, whatever the
// parent holds.
func TestAgentChildCannotAddPathsToTheInheritedOnes(t *testing.T) {
	svc, mock, _ := newIdentityService(t)
	granted, wished := projectDir(t), projectDir(t)
	parent, err := svc.CreateSession(uiCtx(), agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t), ExtraReadWritePaths: []string{granted},
	})
	if err != nil {
		t.Fatal(err)
	}
	child, err := svc.CreateSession(agentCtx(parent.ID), agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock, ExtraReadWritePaths: []string{wished},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := mock.LastSpec().ExtraReadWritePaths; !slices.Equal(got, []string{granted}) {
		t.Errorf("OpenSpec extras = %v, want only the parent's [%s]", got, granted)
	}
	if got := rowExtras(t, svc, child.ID); !slices.Equal(got, []string{granted}) {
		t.Errorf("stored extras = %v, want only the parent's", got)
	}
}

func TestAgentChildCwdMayBeInsideAParentExtraPath(t *testing.T) {
	svc, mock, _ := newIdentityService(t)
	extra := projectDir(t)
	sub := filepath.Join(extra, "svc")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	parent, err := svc.CreateSession(uiCtx(), agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t), ExtraReadWritePaths: []string{extra},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, cwd := range []string{extra, sub} {
		child, err := svc.CreateSession(agentCtx(parent.ID), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: cwd})
		if err != nil {
			t.Fatalf("cwd %s: %v", cwd, err)
		}
		if child.ProjectPath != cwd {
			t.Errorf("child cwd = %s, want %s", child.ProjectPath, cwd)
		}
		if got := mock.LastSpec().ExtraReadWritePaths; !slices.Equal(got, []string{extra}) {
			t.Errorf("cwd %s: extras = %v, want the parent's", cwd, got)
		}
	}
}

func TestAgentChildCwdOutsideParentAndExtrasIsRefused(t *testing.T) {
	svc, _, _ := newIdentityService(t)
	root, extra, outside := projectDir(t), projectDir(t), projectDir(t)
	parent, err := svc.CreateSession(uiCtx(), agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock, Cwd: root, ExtraReadWritePaths: []string{extra},
	})
	if err != nil {
		t.Fatal(err)
	}
	// A sibling that merely shares a prefix with an extra folder is not inside it.
	prefixed := extra + "-sibling"
	if err := os.MkdirAll(prefixed, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(prefixed) })
	for name, cwd := range map[string]string{
		"unrelated":      outside,
		"dotdot":         filepath.Join(extra, "..", filepath.Base(outside)),
		"prefix sibling": prefixed,
		"parent of root": filepath.Dir(root),
	} {
		_, err := svc.CreateSession(agentCtx(parent.ID), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: cwd})
		if !errors.Is(err, agenthub.ErrCwdOutsideParent) && !errors.Is(err, agenthub.ErrInvalidCwd) {
			t.Errorf("%s (%s): got %v, want a cwd refusal", name, cwd, err)
		}
	}
}

func TestAgentChildCwdSymlinkEscapeFromAnExtraPathIsRefused(t *testing.T) {
	svc, _, _ := newIdentityService(t)
	extra, secret := projectDir(t), projectDir(t)
	link := filepath.Join(extra, "link")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("no symlinks: %v", err)
	}
	parent, err := svc.CreateSession(uiCtx(), agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t), ExtraReadWritePaths: []string{extra},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.CreateSession(agentCtx(parent.ID), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: link})
	if !errors.Is(err, agenthub.ErrCwdOutsideParent) {
		t.Fatalf("got %v, want ErrCwdOutsideParent", err)
	}
}

// A symlinked extra path grants what it points at, and a path under the
// real location is inside it.
func TestAgentChildCwdMatchesAnExtraPathBehindASymlink(t *testing.T) {
	svc, _, _ := newIdentityService(t)
	real := projectDir(t)
	link := filepath.Join(projectDir(t), "extra-link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("no symlinks: %v", err)
	}
	parent, err := svc.CreateSession(uiCtx(), agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t), ExtraReadWritePaths: []string{link},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateSession(agentCtx(parent.ID), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: real}); err != nil {
		t.Fatalf("real location of a granted symlink: %v", err)
	}
}

// With a workspace, a child opened in an extra folder must be in one of the
// parent's workspace's folders; one in the parent's own project is not asked.
func TestAgentChildInExtraPathMustBeInTheParentsWorkspace(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO workspaces (id, name, root) VALUES ('ws', 'ws', '')"); err != nil {
		t.Fatal(err)
	}
	reg := agenthub.NewRegistry()
	reg.Register(agenthub.NewMockAdapter())
	svc := agenthub.NewService(reg, agenthub.NewSQLStore(db), nil)
	root, inWorkspace, notInWorkspace := projectDir(t), projectDir(t), projectDir(t)
	svc.MatchingWorkspaces = func(path string) ([]string, error) {
		if path == inWorkspace {
			return []string{"ws"}, nil
		}
		if path == root {
			return []string{"ws"}, nil
		}
		return nil, nil
	}
	parent, err := svc.CreateSession(uiCtx(), agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock, Cwd: root, WorkspaceID: "ws",
		ExtraReadWritePaths: []string{inWorkspace, notInWorkspace},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateSession(agentCtx(parent.ID), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: inWorkspace}); err != nil {
		t.Errorf("extra folder that is a workspace folder: %v", err)
	}
	if _, err := svc.CreateSession(agentCtx(parent.ID), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock}); err != nil {
		t.Errorf("parent's own project: %v", err)
	}
	_, err = svc.CreateSession(agentCtx(parent.ID), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: notInWorkspace})
	if !errors.Is(err, agenthub.ErrCwdOutsideParent) {
		t.Errorf("extra folder outside the workspace: got %v, want ErrCwdOutsideParent", err)
	}
}

// Authority only narrows or stays equal along a chain: a grandchild inherits
// from its own parent's row, and cwd rules are checked against that parent.
func TestGrandchildInheritsFromItsOwnParent(t *testing.T) {
	svc, mock, _ := newIdentityService(t)
	extraA, extraB := projectDir(t), projectDir(t)
	root, err := svc.CreateSession(uiCtx(), agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t), ExtraReadWritePaths: []string{extraA, extraB},
	})
	if err != nil {
		t.Fatal(err)
	}
	child, err := svc.CreateSession(agentCtx(root.ID), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: extraA})
	if err != nil {
		t.Fatal(err)
	}
	grand, err := svc.CreateSession(agentCtx(child.ID), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: extraB})
	if err != nil {
		t.Fatalf("grandchild in the other extra folder: %v", err)
	}
	want := []string{extraA, extraB}
	if got := mock.LastSpec().ExtraReadWritePaths; !slices.Equal(got, want) {
		t.Errorf("grandchild OpenSpec extras = %v, want %v", got, want)
	}
	if got := rowExtras(t, svc, grand.ID); !slices.Equal(got, want) {
		t.Errorf("grandchild stored extras = %v, want %v", got, want)
	}
	// The root's own project is not among the child's folders: not reachable.
	_, err = svc.CreateSession(agentCtx(child.ID), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: root.ProjectPath})
	if !errors.Is(err, agenthub.ErrCwdOutsideParent) {
		t.Errorf("grandchild in the root's project: got %v, want ErrCwdOutsideParent", err)
	}
}

// A person-created session is unchanged, with or without a parent.
func TestUICreatedSessionKeepsItsOwnExtras(t *testing.T) {
	svc, mock, _ := newIdentityService(t)
	parentExtra, ownExtra := projectDir(t), projectDir(t)
	parent, err := svc.CreateSession(uiCtx(), agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t), ExtraReadWritePaths: []string{parentExtra},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateSession(uiCtx(), agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t), ParentSessionID: parent.ID, ExtraReadWritePaths: []string{ownExtra},
	}); err != nil {
		t.Fatal(err)
	}
	if got := mock.LastSpec().ExtraReadWritePaths; !slices.Equal(got, []string{ownExtra}) {
		t.Errorf("UI session extras = %v, want only its own [%s]", got, ownExtra)
	}
}

// What the child is told (the brief) is built from the same list it runs
// under, not a narrower one.
func TestAgentChildBriefListsTheInheritedFolders(t *testing.T) {
	svc, _, _ := newIdentityService(t)
	var asked [][]string
	svc.SessionBrief = func(_, _ string, granted []string) string {
		asked = append(asked, granted)
		return "brief"
	}
	extra := projectDir(t)
	parent, err := svc.CreateSession(uiCtx(), agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t), ExtraReadWritePaths: []string{extra},
	})
	if err != nil {
		t.Fatal(err)
	}
	child, err := svc.CreateSession(agentCtx(parent.ID), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: extra})
	if err != nil {
		t.Fatal(err)
	}
	last := asked[len(asked)-1]
	if want := []string{child.ProjectPath, extra}; !slices.Equal(last, want) {
		t.Errorf("brief granted = %v, want %v", last, want)
	}
}
