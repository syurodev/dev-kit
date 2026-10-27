package agenthub_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/checkpoint"
	"devkit/internal/storage"
)

// forkHookAdapter runs a hook while the rewind's fork is opening: the moment S
// can commit between the launch's reservation and its publication.
type forkHookAdapter struct {
	replyingAdapter
	onFork func()
}

func (a forkHookAdapter) Fork(ctx context.Context, spec agenthub.OpenSpec, src, messageID string) (agenthub.SessionRuntime, error) {
	if a.onFork != nil {
		a.onFork()
	}
	return a.replyingAdapter.Fork(ctx, spec, src, messageID)
}

// A rewind of conversation and files whose launch goes stale while the fork opens
// (an S commit) must refuse before it touches a file: the files change only for a
// rewind that can still commit, so a refusal that says "nothing was cut" is true of
// the project folder too. Real git; real SQL; the S commit is a coordinator epoch
// move at the fork.
func TestAFilesRewindWhoseLaunchGoesStaleChangesNoFile(t *testing.T) {
	git := checkpoint.Locate(context.Background())
	if !git.OK() {
		t.Skip("checkpoint: " + git.Reason)
	}
	ctx := context.Background()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = storage.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO workspaces (id, name, root) VALUES ('w1', 'w1', '')"); err != nil {
		t.Fatal(err)
	}
	store := agenthub.NewSQLStore(db)
	project := t.TempDir()
	turn := 0
	var session agenthub.AgentSession
	var svc *agenthub.Service
	stale := false
	adapter := forkHookAdapter{
		replyingAdapter: replyingAdapter{MockAdapter: agenthub.NewMockAdapter(), closed: &atomic.Bool{}, writeDuringTurn: func() {
			turn++
			writeFile(t, project, "turn"+itoa(turn)+".txt", "from turn "+itoa(turn)+"\n")
		}},
	}
	adapter.onFork = func() {
		if !stale {
			return
		}
		stale = false
		_ = svc.CoordinateServing(ctx, session.ID, func(txn *agenthub.ServingTxn) error { txn.MarkPending(); return nil })
	}
	reg := agenthub.NewRegistry()
	reg.Register(adapter)
	svc = agenthub.NewService(reg, store, func(string, json.RawMessage) {})
	svc.SetCheckpoints(agenthub.NewEngineCheckpoints(checkpoint.New(filepath.Join(t.TempDir(), "checkpoints"), git, checkpoint.Limits{}), store, store))
	svc.MatchingWorkspaces = func(string) ([]string, error) { return []string{"w1"}, nil }
	ev := &events{}
	svc.MCPEndpoints = &policyEndpoints{plainEndpoints: plainEndpoints{ev: ev}}
	fh := &fakeHarness{ev: ev}
	fh.set(workspaceServer(t, false))
	svc.Harness = fh

	writeFile(t, project, "kept.txt", "never touched\n")
	session, err = svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: project, WorkspaceID: "w1"})
	if err != nil {
		t.Fatal(err)
	}
	var prompts []string
	for i := range 2 {
		if _, err = svc.Invoke(ctx, agenthub.InvokeInput{SessionID: session.ID, Prompt: "prompt " + itoa(i+1)}); err != nil {
			t.Fatal(err)
		}
		prompts = append(prompts, waitForCheckpoint(t, store, session.ID, i+1)[i].PromptMessageID)
	}
	if _, err = os.Stat(filepath.Join(project, "turn2.txt")); err != nil {
		t.Fatal("the second turn's file should exist before the rewind")
	}

	preview, err := svc.PlanRewind(ctx, agenthub.RewindRequest{SessionID: session.ID, PromptMessageID: prompts[1], Mode: agenthub.RewindBoth})
	if err != nil {
		t.Fatal(err)
	}
	stale = true
	if _, err = svc.ApplyRewind(ctx, preview.Token, nil); !errors.Is(err, agenthub.ErrHarnessLaunchStale) {
		t.Fatalf("rewind = %v, want a stale launch refusal", err)
	}

	if _, err := os.Stat(filepath.Join(project, "turn2.txt")); err != nil {
		t.Fatalf("a refused rewind removed a file from the project: %v", err)
	}
	if got := readFile(t, project, "turn1.txt"); got != "from turn 1\n" {
		t.Fatalf("turn1.txt = %q", got)
	}
	if texts := transcriptTexts(t, store, session.ID); len(texts) != 2 {
		t.Fatalf("a refused rewind cut the conversation: %v", texts)
	}

	// The retry is a whole new rewind and completes.
	preview, err = svc.PlanRewind(ctx, agenthub.RewindRequest{SessionID: session.ID, PromptMessageID: prompts[1], Mode: agenthub.RewindBoth})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ApplyRewind(ctx, preview.Token, nil); err != nil {
		t.Fatalf("the retry after a stale launch: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, "turn2.txt")); !os.IsNotExist(err) {
		t.Fatalf("the completed rewind left the dropped turn's file: %v", err)
	}
	if texts := transcriptTexts(t, store, session.ID); len(texts) != 1 {
		t.Fatalf("the completed rewind did not cut the conversation: %v", texts)
	}
}
