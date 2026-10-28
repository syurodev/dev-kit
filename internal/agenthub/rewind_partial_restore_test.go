package agenthub_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"devkit/internal/agenthub"
)

// A restore that fails part-way has changed files and has not cut the conversation. The
// rewind says exactly that (the partial outcome) and keeps its safety row pending, so
// the interrupted-rewind list still names it. Only a restore that provably wrote nothing
// is an ordinary refusal.

// partialRestoreRig is a rig whose second turn deleted kept.txt and created locked/a.txt.
// Rewinding that turn writes kept.txt back (a write) and deletes locked/a.txt and
// turn2.txt (deletes). Locking the directory makes the first delete fail after the write
// landed: a restore that stops in the middle.
func partialRestoreRig(t *testing.T) *filesRig {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("a read-only directory does not stop root")
	}
	r, _ := newFilesRig(t, func(project string, turn int) {
		if turn != 2 {
			return
		}
		if err := os.Remove(filepath.Join(project, "kept.txt")); err != nil {
			t.Error(err)
		}
		writeFile(t, project, "locked/a.txt", "made by turn 2\n")
	})
	return r
}

func lockDir(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Error(err)
		return
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
}

func TestARestoreThatFailsAfterWritingIsAPartialRewindAndKeepsTheSafetyRowPending(t *testing.T) {
	r := partialRestoreRig(t)
	ctx := context.Background()
	opens, closes := r.counts()
	r.store.onRestore = func(int) { lockDir(t, filepath.Join(r.project, "locked")) }

	_, err := r.rewind(ctx)

	var partial *agenthub.RewindPartialError
	if !errors.As(err, &partial) || !errors.Is(err, agenthub.ErrRewindFilesRestored) || errors.Is(err, agenthub.ErrRewindOutcomeUnknown) {
		t.Fatalf("a restore that wrote some files and then failed must be the partial outcome, got %v", err)
	}
	if !partial.RestoreIncomplete {
		t.Fatal("the restore did not finish: the outcome must not claim the files were put back")
	}
	if !r.exists("kept.txt") || !r.exists("locked/a.txt") {
		t.Fatal("the scenario must have written kept.txt and failed before deleting locked/a.txt")
	}
	if r.messages() != 2 {
		t.Fatal("the conversation must not be cut")
	}
	if _, cuts := r.store.counts(); cuts != 0 {
		t.Fatalf("the cut was attempted %d times after a failed restore", cuts)
	}
	// The way back: the rewind's own safety row stays pending, so a restart (and the list
	// the banner reads) still reports a rewind that did not finish, with its tree kept.
	row := r.safetyRow()
	if row.Status != agenthub.CheckpointPending || row.BeforeTree == "" || row.ID != partial.SafetyCheckpointID {
		t.Fatalf("the safety row must stay pending with its tree: %+v (error names %s)", row, partial.SafetyCheckpointID)
	}
	listed, lerr := r.svc.InterruptedRewinds(ctx)
	if lerr != nil || len(listed) != 1 || listed[0].ID != row.ID {
		t.Fatalf("the interrupted rewinds must list the row: %v %v", listed, lerr)
	}
	// The fork is not wanted: closed and forgotten, its endpoint released, nothing serving.
	if closed, deleted, _ := r.probes.fork(1).state(); !closed || !deleted {
		t.Fatalf("the abandoned fork: closed=%v deleted=%v, want both", closed, deleted)
	}
	do, dc := r.endpointDelta(opens, closes)
	if do != 1 || dc != 2 {
		t.Fatalf("endpoint opens=%d closes=%d, want the staged one and the old one closed (1 and 2)", do, dc)
	}
	if r.svc.ServingTarget(r.session.ID).Active || r.rewound.Load() != 0 {
		t.Fatal("nothing is published and nobody is told the conversation was rewound")
	}
}

// A restore that is refused before it writes anything changed nothing, and stays the
// plain refusal it always was: not the partial outcome, and the row is settled as failed.
func TestARestoreRefusedBeforeItWritesIsAPlainRefusal(t *testing.T) {
	r, _ := newFilesRig(t)
	ctx := context.Background()
	// The project moves between the plan and the apply: the restore sees the drift and
	// writes nothing.
	r.store.onRestore = func(int) {
		writeFile(t, r.project, "turn2.txt", "edited after the plan\n")
	}

	_, err := r.rewind(ctx)
	if err == nil {
		t.Fatal("a stale plan must be refused")
	}
	var partial *agenthub.RewindPartialError
	var unknown *agenthub.RewindUnknownError
	if errors.As(err, &partial) || errors.As(err, &unknown) || errors.Is(err, agenthub.ErrRewindFilesRestored) {
		t.Fatalf("a restore that wrote nothing must stay a plain refusal, got %v", err)
	}
	if row := r.safetyRow(); row.Status != agenthub.CheckpointFailed {
		t.Fatalf("the row of a rewind that changed nothing is settled: %+v", row)
	}
	if listed, lerr := r.svc.InterruptedRewinds(ctx); lerr != nil || len(listed) != 0 {
		t.Fatalf("nothing changed, so nothing is interrupted: %v %v", listed, lerr)
	}
	if r.messages() != 2 || !r.exists("kept.txt") || !r.exists("turn2.txt") {
		t.Fatal("neither the conversation nor the files may have moved")
	}
}
