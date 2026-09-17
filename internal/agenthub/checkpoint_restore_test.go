package agenthub_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/identity"
)

// Restoring files, against a real git. What is pinned here is the safety: who
// may ask, when it is refused, what it refuses to touch, and that the restore
// itself can always be undone.

func planFor(t *testing.T, svc *agenthub.Service, req agenthub.RestoreRequest) agenthub.RestorePreview {
	t.Helper()
	preview, err := svc.PlanRestore(context.Background(), req)
	if err != nil {
		t.Fatalf("PlanRestore: %v", err)
	}
	return preview
}

func actionPaths(preview agenthub.RestorePreview) []string {
	out := make([]string, 0, len(preview.Actions))
	for _, action := range preview.Actions {
		out = append(out, action.Path)
	}
	return out
}

func conflictOn(preview agenthub.RestorePreview, path string) (agenthub.RestoreConflict, bool) {
	for _, conflict := range preview.Conflicts {
		if conflict.Path == path {
			return conflict, true
		}
	}
	return agenthub.RestoreConflict{}, false
}

// A session with three turns, each changing one file, so the two modes have
// something to differ about.
func threeTurnSession(t *testing.T) (*agenthub.Service, agenthub.Store, string, string, []agenthub.CheckpointRecord) {
	t.Helper()
	var project string
	turn := 0
	svc, store, _, project := checkpointServiceWriting(t, func() {
		turn++
		switch turn {
		case 1:
			writeFile(t, project, "one.txt", "turn one\n")
		case 2:
			writeFile(t, project, "two.txt", "turn two\n")
		case 3:
			writeFile(t, project, "three.txt", "turn three\n")
		}
	})
	ctx := context.Background()
	writeFile(t, project, "kept.txt", "never touched\n")
	session, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: project})
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: session.ID, Prompt: "go"}); err != nil {
			t.Fatal(err)
		}
		waitForCheckpoint(t, store, session.ID, turn)
	}
	records := waitForCheckpoint(t, store, session.ID, 3)
	return svc, store, session.ID, project, records
}

// Undoing one turn leaves the turns after it alone. That is the whole
// distinction from "go back to before this turn", and it is a path-scope
// question, not a tree question — both aim at the same snapshot.
func TestUndoOneTurnLeavesLaterTurnsAlone(t *testing.T) {
	svc, _, sessionID, project, records := threeTurnSession(t)
	ctx := context.Background()

	preview := planFor(t, svc, agenthub.RestoreRequest{
		SessionID:       sessionID,
		Mode:            agenthub.RestoreUndoTurn,
		PromptMessageID: records[1].PromptMessageID,
	})
	if got := actionPaths(preview); len(got) != 1 || got[0] != "two.txt" {
		t.Fatalf("actions = %v, want only two.txt", got)
	}

	if _, err := svc.ApplyRestore(ctx, preview.Token, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, "two.txt")); !os.IsNotExist(err) {
		t.Errorf("two.txt survived its turn being undone: %v", err)
	}
	for _, name := range []string{"one.txt", "three.txt", "kept.txt"} {
		if _, err := os.Stat(filepath.Join(project, name)); err != nil {
			t.Errorf("%s was removed by an undo that should not have touched it: %v", name, err)
		}
	}
}

// Going back to before a turn reverses it and everything since — the union of
// what those turns changed — and still leaves alone what none of them touched.
func TestGoingBackToBeforeATurnReversesEverythingSince(t *testing.T) {
	svc, _, sessionID, project, records := threeTurnSession(t)
	ctx := context.Background()

	preview := planFor(t, svc, agenthub.RestoreRequest{
		SessionID:       sessionID,
		Mode:            agenthub.RestoreToBeforeTurn,
		PromptMessageID: records[1].PromptMessageID,
	})
	if got := actionPaths(preview); len(got) != 2 {
		t.Fatalf("actions = %v, want two.txt and three.txt", got)
	}

	if _, err := svc.ApplyRestore(ctx, preview.Token, nil); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"two.txt", "three.txt"} {
		if _, err := os.Stat(filepath.Join(project, name)); !os.IsNotExist(err) {
			t.Errorf("%s survived: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(project, "one.txt")); err != nil {
		t.Errorf("the turn before the one being reversed was undone too: %v", err)
	}
}

// A restore is the one operation that destroys work, so it can always be
// reversed: the safety snapshot is taken before anything is written, and
// nothing proceeds without one.
func TestARestoreCanItselfBeUndone(t *testing.T) {
	svc, store, sessionID, project, records := threeTurnSession(t)
	ctx := context.Background()

	preview := planFor(t, svc, agenthub.RestoreRequest{
		SessionID:       sessionID,
		Mode:            agenthub.RestoreToBeforeTurn,
		PromptMessageID: records[0].PromptMessageID,
	})
	result, err := svc.ApplyRestore(ctx, preview.Token, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.SafetyCheckpointID == "" {
		t.Fatal("no safety snapshot was recorded")
	}
	for _, name := range []string{"one.txt", "two.txt", "three.txt"} {
		if _, err := os.Stat(filepath.Join(project, name)); !os.IsNotExist(err) {
			t.Fatalf("%s survived the restore: %v", name, err)
		}
	}

	safety, err := store.GetCheckpoint(ctx, result.SafetyCheckpointID)
	if err != nil {
		t.Fatal(err)
	}
	if safety.Kind != agenthub.CheckpointRestoreSafety || safety.BeforeTree == "" {
		t.Fatalf("safety row = %+v", safety)
	}

	undo := planFor(t, svc, agenthub.RestoreRequest{
		SessionID:    sessionID,
		Mode:         agenthub.RestoreUndoRestore,
		CheckpointID: result.SafetyCheckpointID,
	})
	if _, err := svc.ApplyRestore(ctx, undo.Token, nil); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"one.txt", "two.txt", "three.txt"} {
		if _, err := os.Stat(filepath.Join(project, name)); err != nil {
			t.Errorf("%s did not come back: %v", name, err)
		}
	}
}

// An agent that could undo its own turns could hide them. This is the person's
// to do, and it is refused at the service, not merely left out of the MCP tool
// list — the two are different guarantees.
func TestAnAgentMayNotRestoreFiles(t *testing.T) {
	svc, _, sessionID, _, records := threeTurnSession(t)
	agentCtx := identity.WithCaller(context.Background(), identity.Agent(sessionID))

	_, err := svc.PlanRestore(agentCtx, agenthub.RestoreRequest{
		SessionID:       sessionID,
		Mode:            agenthub.RestoreUndoTurn,
		PromptMessageID: records[2].PromptMessageID,
	})
	if !errors.Is(err, agenthub.ErrRestoreNotPermitted) {
		t.Fatalf("PlanRestore as an agent = %v, want ErrRestoreNotPermitted", err)
	}

	// And it cannot ride a token a person made either.
	preview := planFor(t, svc, agenthub.RestoreRequest{
		SessionID:       sessionID,
		Mode:            agenthub.RestoreUndoTurn,
		PromptMessageID: records[2].PromptMessageID,
	})
	if _, err := svc.ApplyRestore(agentCtx, preview.Token, nil); !errors.Is(err, agenthub.ErrRestoreNotPermitted) {
		t.Fatalf("ApplyRestore as an agent = %v, want ErrRestoreNotPermitted", err)
	}
}

// A user's own edit since the turn is a conflict: skipped by default, and only
// written when they say so. This is what stops a restore quietly destroying
// work the agent never touched.
func TestAnEditSinceTheTurnIsAConflictUntilForced(t *testing.T) {
	var project string
	svc, store, _, project := checkpointServiceWriting(t, func() {
		writeFile(t, project, "edited.txt", "the agent's version\n")
	})
	ctx := context.Background()
	writeFile(t, project, "edited.txt", "the original\n")
	session, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: project})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: session.ID, Prompt: "go"}); err != nil {
		t.Fatal(err)
	}
	records := waitForCheckpoint(t, store, session.ID, 1)

	// The user's own edit, after the turn, that DevKit never snapshotted.
	writeFile(t, project, "edited.txt", "my own work\n")

	preview := planFor(t, svc, agenthub.RestoreRequest{
		SessionID:       session.ID,
		Mode:            agenthub.RestoreUndoTurn,
		PromptMessageID: records[0].PromptMessageID,
	})
	conflict, ok := conflictOn(preview, "edited.txt")
	if !ok || !conflict.Overridable {
		t.Fatalf("conflicts = %+v, want edited.txt as an overridable conflict", preview.Conflicts)
	}
	if !preview.Empty() {
		t.Fatalf("actions = %+v, want none", preview.Actions)
	}
	if _, err := svc.ApplyRestore(ctx, preview.Token, nil); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, project, "edited.txt"); got != "my own work\n" {
		t.Fatalf("edited.txt = %q, want the user's edit left alone", got)
	}

	forced := planFor(t, svc, agenthub.RestoreRequest{
		SessionID:       session.ID,
		Mode:            agenthub.RestoreUndoTurn,
		PromptMessageID: records[0].PromptMessageID,
	})
	if _, err := svc.ApplyRestore(ctx, forced.Token, []string{"edited.txt"}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, project, "edited.txt"); got != "the original\n" {
		t.Fatalf("edited.txt = %q, want the forced restore", got)
	}
}

// One token, one restore. A second Apply on the same preview is a double-click,
// and a restore is not something to do twice — the second would be planned
// against a project the first one already changed.
func TestARestorePlanIsSingleUse(t *testing.T) {
	svc, _, sessionID, _, records := threeTurnSession(t)
	ctx := context.Background()
	preview := planFor(t, svc, agenthub.RestoreRequest{
		SessionID:       sessionID,
		Mode:            agenthub.RestoreUndoTurn,
		PromptMessageID: records[2].PromptMessageID,
	})
	if _, err := svc.ApplyRestore(ctx, preview.Token, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApplyRestore(ctx, preview.Token, nil); !errors.Is(err, agenthub.ErrRestorePlanStale) {
		t.Fatalf("second apply = %v, want ErrRestorePlanStale", err)
	}
	if _, err := svc.ApplyRestore(ctx, "never-issued", nil); !errors.Is(err, agenthub.ErrRestorePlanStale) {
		t.Fatalf("unknown token = %v, want ErrRestorePlanStale", err)
	}
}

// Files change under a running agent, so a restore during a turn would race its
// writes. Refused rather than queued: the user asked to undo a state that is
// still moving.
func TestARestoreIsRefusedWhileTheSessionIsRunning(t *testing.T) {
	svc, store, sessionID, _, records := threeTurnSession(t)
	ctx := context.Background()
	session, err := store.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateSessionStatus(ctx, session.ID, agenthub.SessionStatusRunning, session.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	_, err = svc.PlanRestore(ctx, agenthub.RestoreRequest{
		SessionID:       sessionID,
		Mode:            agenthub.RestoreUndoTurn,
		PromptMessageID: records[2].PromptMessageID,
	})
	if !errors.Is(err, agenthub.ErrSessionBusy) {
		t.Fatalf("PlanRestore while running = %v, want ErrSessionBusy", err)
	}
}

// A turn whose opening snapshot never finished has nothing to go back to.
// Refused outright rather than approximated against some other tree: that would
// delete files the turn never created.
func TestATurnWithNoOpeningSnapshotCannotBeUndone(t *testing.T) {
	svc, store, sessionID, _, records := threeTurnSession(t)
	ctx := context.Background()

	broken := records[2]
	broken.BeforeTree = ""
	broken.Outcome = agenthub.CheckpointOutcomeBeforeMissing
	if err := store.UpdateCheckpoint(ctx, broken); err != nil {
		t.Fatal(err)
	}
	_, err := svc.PlanRestore(ctx, agenthub.RestoreRequest{
		SessionID:       sessionID,
		Mode:            agenthub.RestoreUndoTurn,
		PromptMessageID: broken.PromptMessageID,
	})
	if !errors.Is(err, agenthub.ErrUnavailable) {
		t.Fatalf("PlanRestore = %v, want ErrUnavailable", err)
	}
}

func readFile(t *testing.T, dir, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
