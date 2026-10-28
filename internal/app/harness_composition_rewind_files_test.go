package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/checkpoint"
)

// A rewind of conversation and files whose S changes while the files are being put
// back, with the change committed through the real harness configuration service
// (Prepare and Apply), the real MCP session host and the real coordinator. The
// agenthub package's own tests move S with an epoch-only seam; this is the committed
// path.

// filesRewindRig is a composed graph whose Agent Hub keeps checkpoints, over a
// project whose agent writes one file per turn.
func filesRewindRig(t *testing.T) (*compRig, agenthub.AgentSession, []string, *scriptedVendor) {
	t.Helper()
	git := checkpoint.Locate(context.Background())
	if !git.OK() {
		t.Skip("checkpoint: " + git.Reason)
	}
	r := newCompRig(t, compOptions{})
	raw := agenthub.NewSQLStore(r.db)
	r.agents.SetCheckpoints(agenthub.NewEngineCheckpoints(checkpoint.New(filepath.Join(t.TempDir(), "checkpoints"), git, checkpoint.Limits{}), raw, raw))

	v := r.vendors[agenthub.AgentTypeClaude]
	turn := 0
	v.mu.Lock()
	v.onPrompt = func() {
		turn++
		writeProjectFile(t, r.root, "turn"+strconv.Itoa(turn)+".txt", "from turn "+strconv.Itoa(turn)+"\n")
	}
	v.mu.Unlock()
	writeProjectFile(t, r.root, "kept.txt", "never touched\n")

	sess, prompts := promptedSession(t, r)
	// The checkpoint of a turn is finished after the turn returns.
	deadline := time.Now().Add(15 * time.Second)
	for {
		records, err := raw.ListCheckpoints(context.Background(), sess.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(records) >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("checkpoints = %d, want 2", len(records))
		}
		time.Sleep(20 * time.Millisecond)
	}
	return r, sess, prompts, v
}

func writeProjectFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fileExists(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

func (r *compRig) rewindBoth(sessionID, promptID string) error {
	r.t.Helper()
	preview, err := r.agents.PlanRewind(r.ui(), agenthub.RewindRequest{SessionID: sessionID, PromptMessageID: promptID, Mode: agenthub.RewindBoth})
	if err != nil {
		r.t.Fatal(err)
	}
	_, err = r.agents.ApplyRewind(r.ui(), preview.Token, nil)
	return err
}

// The change lands after the files were put back and before the cut is published
// (OnFilesRestored fires inside the restore, under the rewind's claim). The files are
// restored once, the fork prepared for the old S is discarded, and the launch prepared
// again resolves the committed S: the revoked group is not served, the applied
// reference names the new revision, and the cut, the vendor id and the reference are
// the final attempt's.
func TestACommittedSChangeDuringTheFileRestoreFinishesTheRewind(t *testing.T) {
	r, sess, prompts, v := filesRewindRig(t)
	forks := v.forks.Load()

	var restored atomic.Int32
	var applyErr error
	firstForkServedVault := false
	r.agents.OnFilesRestored = func(string, agenthub.RestoreResult) {
		if restored.Add(1) == 1 {
			// The fork the restore ran beside was prepared for the old S, which enables the group.
			firstForkServedVault = contains(v.LastSpec().DevKit.Tools, "vault.status")
			// A committed S edit through the real service while the rewind holds the
			// session, after its files changed and before its cut.
			_, applyErr = r.apply(sess.ID, nil, []string{"vault"})
		}
	}

	if err := r.rewindBoth(sess.ID, prompts[1]); err != nil {
		t.Fatalf("a rewind whose S committed during the restore must finish: %v", err)
	}
	if applyErr != nil {
		t.Fatalf("the S change committed during the restore must succeed: %v", applyErr)
	}
	if got := restored.Load(); got != 1 {
		t.Fatalf("the files were restored %d times, want once", got)
	}
	if got := v.forks.Load() - forks; got != 2 {
		t.Fatalf("forks = %d, want the first and one re-prepared", got)
	}
	if fileExists(r.root, "turn2.txt") || !fileExists(r.root, "turn1.txt") || !fileExists(r.root, "kept.txt") || r.turns(sess.ID) != 1 {
		t.Fatal("files and conversation must both be rewound")
	}
	if !firstForkServedVault {
		t.Fatal("the discarded fork must have been prepared for the old S, which enabled the group")
	}
	if contains(v.LastSpec().DevKit.Tools, "vault.status") {
		t.Fatal("the launch that won must be prepared from the committed S, which revoked the group")
	}
	if o, st := r.override(sess.ID); o != nil || st.Present || st.Revision != 2 {
		t.Fatalf("the committed revoke stays committed: %+v %+v", o, st)
	}
	view := r.agents.ServingTarget(sess.ID)
	if !view.Active || !view.Acknowledged || view.Pending || view.Serving.Session.Revision != 2 {
		t.Fatalf("the live endpoint must serve the committed S: %+v", view)
	}
	// The stored reference is the final launch's, not the discarded fork's: it carries the
	// fingerprint the live endpoint serves (a revoked S has no session revision to name).
	status, err := r.agents.HarnessStatus(context.Background(), sess.ID)
	if err != nil || status.Applied == nil || status.Applied.Fingerprint != view.Serving.Fingerprint ||
		(status.Applied.Revisions.Session != nil && *status.Applied.Revisions.Session != 2) {
		t.Fatalf("the committed reference must describe the final launch: %+v %v", status, err)
	}
	if pending, err := r.agents.InterruptedRewinds(context.Background()); err != nil || len(pending) != 0 {
		t.Fatalf("a finished rewind leaves no pending safety row: %v %v", pending, err)
	}
}

// S committed on every attempt: the rewind stops inside its budget, says the files were
// restored and the conversation was not, keeps the safety checkpoint, and does not
// restore or cut again. The error that reaches the caller is the partial one.
func TestACommittedSChangeOnEveryAttemptEndsInATruthfulPartialRewind(t *testing.T) {
	r, sess, prompts, v := filesRewindRig(t)
	raw := agenthub.NewSQLStore(r.db)
	forks := v.forks.Load()

	// One committed change after the restore, then one while each re-prepared launch is
	// being forked: the group is revoked, enabled, revoked again.
	var changes, restored atomic.Int32
	var armed atomic.Bool
	change := func() {
		enable, revoke := []string{"vault"}, []string(nil)
		if changes.Add(1)%2 == 1 {
			enable, revoke = nil, []string{"vault"}
		}
		if _, err := r.apply(sess.ID, enable, revoke); err != nil {
			t.Errorf("a committed change: %v", err)
		}
	}
	r.agents.OnFilesRestored = func(string, agenthub.RestoreResult) {
		restored.Add(1)
		armed.Store(true)
		change()
	}
	v.mu.Lock()
	v.onOpen = func() {
		if armed.Load() {
			change()
		}
	}
	v.mu.Unlock()

	err := r.rewindBoth(sess.ID, prompts[1])
	v.mu.Lock()
	v.onOpen = nil
	v.mu.Unlock()

	var partial *agenthub.RewindPartialError
	if !errors.As(err, &partial) || !errors.Is(err, agenthub.ErrRewindFilesRestored) || !errors.Is(err, agenthub.ErrHarnessLaunchStale) {
		t.Fatalf("want a partial rewind caused by a stale launch, got %v", err)
	}
	if got := restored.Load(); got != 1 {
		t.Fatalf("the files were restored %d times, want once", got)
	}
	if got := v.forks.Load() - forks; got != 3 {
		t.Fatalf("forks = %d, want the launch budget of 3 and no more", got)
	}
	if fileExists(r.root, "turn2.txt") || r.turns(sess.ID) != 2 {
		t.Fatal("files restored, conversation not cut")
	}
	if r.agents.ServingTarget(sess.ID).Active {
		t.Fatal("a rewind that gave up must leave nothing staged or serving")
	}
	records, err := raw.ListCheckpoints(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	var safety []agenthub.CheckpointRecord
	for _, rec := range records {
		if rec.Kind == agenthub.CheckpointRewindSafety {
			safety = append(safety, rec)
		}
	}
	if len(safety) != 1 || safety[0].ID != partial.SafetyCheckpointID || safety[0].Status != agenthub.CheckpointFailed || safety[0].BeforeTree == "" {
		t.Fatalf("the one safety row must be kept, settled and restorable: %+v", safety)
	}
}
