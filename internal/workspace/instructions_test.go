package workspace_test

import (
	"errors"
	"strings"
	"testing"

	"devkit/internal/workspace"
)

func TestInstructionsStayWithWorkspaceAcrossFolderChanges(t *testing.T) {
	for _, backend := range []string{"memory", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			var repo workspace.Repository = workspace.NewMemoryRepository()
			if backend == "sqlite" {
				repo = newTestSQLRepo(t)
			}
			svc := workspace.NewService(repo)
			shared, other := t.TempDir(), t.TempDir()
			ws, _, err := svc.CreateWorkspace("first", []string{shared})
			if err != nil {
				t.Fatal(err)
			}
			second, _, err := svc.CreateWorkspace("second", []string{shared})
			if err != nil {
				t.Fatal(err)
			}
			text := "  Use Vietnamese.\nKeep API compatibility.\n"
			if err := svc.SetInstructions(ws.ID, text); err != nil {
				t.Fatal(err)
			}
			updated, _, err := svc.UpdateWorkspace(ws.ID, "renamed", []string{other, shared})
			if err != nil {
				t.Fatal(err)
			}
			if updated.Instructions != text {
				t.Fatalf("updated instructions = %q", updated.Instructions)
			}
			got, err := svc.GetInstructions(ws.ID)
			if err != nil || got != text {
				t.Fatalf("instructions = %q, %v", got, err)
			}
			got, err = svc.GetInstructions(second.ID)
			if err != nil || got != "" {
				t.Fatalf("overlapping workspace instructions = %q, %v", got, err)
			}
			if err := svc.SetInstructions(ws.ID, ""); err != nil {
				t.Fatal(err)
			}
			got, err = svc.GetInstructions(ws.ID)
			if err != nil || got != "" {
				t.Fatalf("cleared instructions = %q, %v", got, err)
			}
			if err := svc.DeleteWorkspace(ws.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.GetInstructions(ws.ID); !errors.Is(err, workspace.ErrWorkspaceNotFound) {
				t.Fatalf("deleted getter: %v", err)
			}
			if err := svc.SetInstructions(ws.ID, "gone"); !errors.Is(err, workspace.ErrWorkspaceNotFound) {
				t.Fatalf("deleted setter: %v", err)
			}
		})
	}
}

func TestInstructionsValidateUTF8ByteLimitBeforeWriting(t *testing.T) {
	svc := workspace.NewService(workspace.NewMemoryRepository())
	ws, _, err := svc.CreateWorkspace("first", nil)
	if err != nil {
		t.Fatal(err)
	}
	atLimit := strings.Repeat("é", workspace.MaxInstructionsBytes/2)
	if err := svc.SetInstructions(ws.ID, atLimit); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{atLimit + "a", string([]byte{0xff})} {
		if err := svc.SetInstructions(ws.ID, text); !errors.Is(err, workspace.ErrInvalidInstructions) {
			t.Fatalf("invalid instructions error: %v", err)
		}
		got, err := svc.GetInstructions(ws.ID)
		if err != nil || got != atLimit {
			t.Fatal("invalid update overwrote stored instructions")
		}
	}
}

func TestCompleteWorkspaceSaveIsAtomic(t *testing.T) {
	svc := workspace.NewService(newTestSQLRepo(t))
	tooLarge := strings.Repeat("x", workspace.MaxInstructionsBytes+1)
	if _, _, err := svc.CreateWorkspaceWithInstructions("invalid", nil, tooLarge); !errors.Is(err, workspace.ErrInvalidInstructions) {
		t.Fatalf("create error = %v", err)
	}
	listed, err := svc.ListWorkspaces()
	if err != nil || len(listed) != 0 {
		t.Fatalf("invalid creation wrote workspace: %+v, %v", listed, err)
	}
	a, b := t.TempDir(), t.TempDir()
	ws, _, err := svc.CreateWorkspaceWithInstructions("before", []string{a}, "original")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.UpdateWorkspaceWithInstructions(ws.ID, "after", []string{b}, tooLarge); !errors.Is(err, workspace.ErrInvalidInstructions) {
		t.Fatalf("update error = %v", err)
	}
	listed, err = svc.ListWorkspaces()
	if err != nil || len(listed) != 1 || listed[0] != ws {
		t.Fatalf("invalid update changed workspace: %+v, %v", listed, err)
	}
	folders, err := svc.ListServices(ws.ID)
	if err != nil || len(folders) != 1 || folders[0].Path != a {
		t.Fatalf("invalid update changed folders: %+v, %v", folders, err)
	}
	saved, _, err := svc.UpdateWorkspaceWithInstructions(ws.ID, "after", []string{b}, "replacement")
	if err != nil {
		t.Fatal(err)
	}
	listed, err = svc.ListWorkspaces()
	if err != nil || len(listed) != 1 || listed[0] != saved || saved.Instructions != "replacement" || saved.Root != b {
		t.Fatalf("complete update = %+v, %v", listed, err)
	}
	// Simulate a folder editor holding a stale copy when the dedicated setter saves.
	repo := newTestSQLRepo(t)
	if err := repo.CreateWorkspace(ws, nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetInstructions(ws.ID, "newer"); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateWorkspace(ws, nil, nil); err != nil {
		t.Fatal(err)
	}
	got, err := workspace.NewService(repo).GetInstructions(ws.ID)
	if err != nil || got != "newer" {
		t.Fatalf("folder edit overwrote concurrent instructions: %q, %v", got, err)
	}
}
