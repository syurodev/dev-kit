package agenthub_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"devkit/internal/acp"
	"devkit/internal/agenthub"
	"devkit/internal/checkpoint"
	"devkit/internal/identity"
	"devkit/internal/storage"
)

// Rewinding a session. The sequence is what is pinned: which step happens
// before which, and what is left behind when one of them fails.

// replyingAdapter is the mock plus an agent that actually says something, so a
// transcript has assistant messages with ids to branch at — the mock's own
// reply carries none, and a rewind has nothing to work with without them.
type replyingAdapter struct {
	*agenthub.MockAdapter
	writeDuringTurn func()
	// closed is handed to the runtimes Open makes — the ones a rewind replaces.
	closed *atomic.Bool
}

func (a replyingAdapter) Open(ctx context.Context, spec agenthub.OpenSpec) (agenthub.SessionRuntime, error) {
	rt, err := a.MockAdapter.Open(ctx, spec)
	if err != nil {
		return nil, err
	}
	return &replyingRuntime{SessionRuntime: rt, write: a.writeDuringTurn, closed: a.closed}, nil
}

func (a replyingAdapter) Fork(ctx context.Context, spec agenthub.OpenSpec, src, messageID string) (agenthub.SessionRuntime, error) {
	rt, err := a.MockAdapter.Fork(ctx, spec, src, messageID)
	if err != nil {
		return nil, err
	}
	// The fork gets its own flag: the test is about the runtime being replaced,
	// not about every runtime the adapter ever made.
	return &replyingRuntime{SessionRuntime: rt, write: a.writeDuringTurn}, nil
}

type replyingRuntime struct {
	agenthub.SessionRuntime
	write    func()
	handlers agenthub.RuntimeHandlers
	replies  int
	// closed is set when this runtime is shut down, so a test can ask whether
	// it had been by the time a later step ran.
	closed *atomic.Bool
}

func (r *replyingRuntime) Close(ctx context.Context) error {
	if r.closed != nil {
		r.closed.Store(true)
	}
	return r.SessionRuntime.Close(ctx)
}

func (r *replyingRuntime) Unwrap() agenthub.SessionRuntime { return r.SessionRuntime }

// Forwarding the optional interfaces by hand, because embedding an interface
// does not carry the methods the concrete value has beyond it: a wrapper that
// did not do this would silently take away a runtime's ability to be
// configured or to delete a vendor session, and the rewind would look like it
// had simply chosen not to.
func (r *replyingRuntime) DeleteSession(ctx context.Context, acpSessionID string) error {
	if deleter, ok := r.SessionRuntime.(agenthub.VendorSessionDeleter); ok {
		return deleter.DeleteSession(ctx, acpSessionID)
	}
	return nil
}

func (r *replyingRuntime) Delete(ctx context.Context) error {
	if deleter, ok := r.SessionRuntime.(agenthub.RuntimeDeleter); ok {
		return deleter.Delete(ctx)
	}
	return nil
}

func (r *replyingRuntime) SetMode(ctx context.Context, modeID string) error {
	if configurator, ok := r.SessionRuntime.(agenthub.RuntimeConfigurator); ok {
		return configurator.SetMode(ctx, modeID)
	}
	return nil
}

func (r *replyingRuntime) SetConfigOption(ctx context.Context, optionID, value string) error {
	if configurator, ok := r.SessionRuntime.(agenthub.RuntimeConfigurator); ok {
		return configurator.SetConfigOption(ctx, optionID, value)
	}
	return nil
}

func (r *replyingRuntime) SetHandlers(h agenthub.RuntimeHandlers) {
	r.handlers = h
	r.SessionRuntime.SetHandlers(h)
}

func (r *replyingRuntime) Prompt(ctx context.Context, prompt []acp.ContentBlock, meta json.RawMessage) (agenthub.PromptOutcome, error) {
	if r.write != nil {
		r.write()
	}
	r.replies++
	// An assistant message with a vendor message id — what a fork point is.
	if r.handlers.OnUpdate != nil {
		update := `{"sessionUpdate":"agent_message_chunk","messageId":"msg_` +
			r.SessionRuntime.ACPSessionID() + `_` + itoa(r.replies) + `","content":{"type":"text","text":"reply"}}`
		r.handlers.OnUpdate(json.RawMessage(update), nil)
	}
	return r.SessionRuntime.Prompt(ctx, prompt, meta)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var out []byte
	for n > 0 {
		out = append([]byte{byte('0' + n%10)}, out...)
		n /= 10
	}
	return string(out)
}

// rewindHarness is a session with three turns, each replied to and each
// changing one file, against a real git.
func rewindHarness(t *testing.T) (*agenthub.Service, agenthub.Store, *agenthub.MockAdapter, string, string, []string) {
	svc, store, mock, sessionID, project, prompts, _ := rewindHarnessWith(t, nil)
	return svc, store, mock, sessionID, project, prompts
}

// rewindHarnessWith lets a test wrap the store — the only way to stand inside
// the rewind's sequence and ask what had already happened.
func rewindHarnessWith(t *testing.T, wrap func(agenthub.Store) agenthub.Store) (*agenthub.Service, agenthub.Store, *agenthub.MockAdapter, string, string, []string, *atomic.Bool) {
	t.Helper()
	git := checkpoint.Locate(context.Background())
	if !git.OK() {
		t.Skip("checkpoint: " + git.Reason)
	}
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	var store agenthub.Store = agenthub.NewSQLStore(db)
	if wrap != nil {
		store = wrap(store)
	}
	project := t.TempDir()
	turn := 0
	mock := agenthub.NewMockAdapter()
	closed := &atomic.Bool{}
	reg := agenthub.NewRegistry()
	reg.Register(replyingAdapter{MockAdapter: mock, closed: closed, writeDuringTurn: func() {
		turn++
		writeFile(t, project, "turn"+itoa(turn)+".txt", "from turn "+itoa(turn)+"\n")
	}})
	svc := agenthub.NewService(reg, store, func(string, json.RawMessage) {})
	engine := checkpoint.New(filepath.Join(t.TempDir(), "checkpoints"), git, checkpoint.Limits{})
	svc.SetCheckpoints(agenthub.NewEngineCheckpoints(engine, store, store))

	ctx := context.Background()
	writeFile(t, project, "kept.txt", "never touched\n")
	session, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: project})
	if err != nil {
		t.Fatal(err)
	}
	var prompts []string
	for i := range 3 {
		if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: session.ID, Prompt: "prompt " + itoa(i+1)}); err != nil {
			t.Fatal(err)
		}
		records := waitForCheckpoint(t, store, session.ID, i+1)
		prompts = append(prompts, records[i].PromptMessageID)
	}
	return svc, store, mock, session.ID, project, prompts, closed
}

func transcriptTexts(t *testing.T, store agenthub.Store, sessionID string) []string {
	t.Helper()
	entries, err := store.ListTranscript(context.Background(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, entry := range entries {
		if entry.Kind == agenthub.UpdateUserMessageChunk {
			out = append(out, entry.Text)
		}
	}
	return out
}

// The whole thing, end to end: the conversation is cut, the vendor session is
// replaced by a branch of it, the files go back, and the turns that no longer
// exist take their rows with them.
func TestRewindCutsTheConversationAndTheFiles(t *testing.T) {
	svc, store, mock, sessionID, project, prompts := rewindHarness(t)
	ctx := context.Background()

	before, err := store.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}

	preview, err := svc.PlanRewind(ctx, agenthub.RewindRequest{
		SessionID:       sessionID,
		PromptMessageID: prompts[1],
		Mode:            agenthub.RewindBoth,
	})
	if err != nil {
		t.Fatalf("PlanRewind: %v", err)
	}
	if preview.PromptText != "prompt 2" {
		t.Fatalf("promptText = %q, want the message being rewound", preview.PromptText)
	}
	if preview.Files == nil {
		t.Fatal("the file half was not planned")
	}

	result, err := svc.ApplyRewind(ctx, preview.Token, nil)
	if err != nil {
		t.Fatalf("ApplyRewind: %v", err)
	}

	// The conversation: prompts 2 and 3 are gone, 1 remains.
	if got := transcriptTexts(t, store, sessionID); len(got) != 1 || got[0] != "prompt 1" {
		t.Fatalf("prompts left = %v, want only the first", got)
	}
	// The vendor session: a branch, not the one the rewind left behind.
	after, err := store.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if after.ACPSessionID == before.ACPSessionID || after.ACPSessionID == "" {
		t.Fatalf("acp session = %q, was %q; want a new one", after.ACPSessionID, before.ACPSessionID)
	}
	forks := mock.Forks()
	if len(forks) != 1 || forks[0].SourceACPSessionID != before.ACPSessionID {
		t.Fatalf("forks = %+v, want one from %s", forks, before.ACPSessionID)
	}
	// Branched at the last thing the agent said before the rewound turn.
	if forks[0].MessageID != "msg_"+before.ACPSessionID+"_1" {
		t.Errorf("branched at %q, want the reply to prompt 1", forks[0].MessageID)
	}
	// And the old conversation is asked to go.
	if deleted := mock.Deleted(); len(deleted) != 1 || deleted[0] != before.ACPSessionID {
		t.Errorf("deleted = %v, want the old vendor session", deleted)
	}

	// The files: turns 2 and 3 undone, turn 1 and the untouched file left.
	for _, name := range []string{"turn2.txt", "turn3.txt"} {
		if _, err := os.Stat(filepath.Join(project, name)); !os.IsNotExist(err) {
			t.Errorf("%s survived: %v", name, err)
		}
	}
	for _, name := range []string{"turn1.txt", "kept.txt"} {
		if _, err := os.Stat(filepath.Join(project, name)); err != nil {
			t.Errorf("%s was removed: %v", name, err)
		}
	}
	if result.Files == nil {
		t.Error("the result does not say what happened to the files")
	}

	// The rows of the turns that no longer exist go with them: a cost or a
	// checkpoint attributed to a message nobody can see is unreachable clutter.
	turns, err := store.ListTurns(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 || turns[0].PromptMessageID != prompts[0] {
		t.Fatalf("turns = %d, want only the first", len(turns))
	}
	records, err := store.ListCheckpoints(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if record.Kind == agenthub.CheckpointTurn && record.PromptMessageID != prompts[0] {
			t.Errorf("a checkpoint for a cut turn survived: %+v", record)
		}
	}
}

// Rewinding the first turn has nothing to branch from. A fresh session, not a
// refusal: the user asked to go back to the beginning, and the beginning is a
// conversation with nothing in it.
func TestRewindingTheFirstTurnStartsAFreshConversation(t *testing.T) {
	svc, store, mock, sessionID, _, prompts := rewindHarness(t)
	ctx := context.Background()

	preview, err := svc.PlanRewind(ctx, agenthub.RewindRequest{
		SessionID:       sessionID,
		PromptMessageID: prompts[0],
		Mode:            agenthub.RewindConversation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !preview.StartsFresh {
		t.Fatal("preview does not say the session starts over")
	}
	if _, err := svc.ApplyRewind(ctx, preview.Token, nil); err != nil {
		t.Fatal(err)
	}
	if got := transcriptTexts(t, store, sessionID); len(got) != 0 {
		t.Fatalf("prompts left = %v, want none", got)
	}
	if forks := mock.Forks(); len(forks) != 0 {
		t.Fatalf("forks = %+v, want none: there was nothing to branch", forks)
	}
}

// Conversation only leaves the files alone. The two halves are independent —
// that is why the user can ask for either.
func TestRewindingTheConversationOnlyLeavesFilesAlone(t *testing.T) {
	svc, _, _, sessionID, project, prompts := rewindHarness(t)
	ctx := context.Background()

	preview, err := svc.PlanRewind(ctx, agenthub.RewindRequest{
		SessionID:       sessionID,
		PromptMessageID: prompts[1],
		Mode:            agenthub.RewindConversation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Files != nil {
		t.Fatal("a conversation-only rewind planned a file restore")
	}
	if _, err := svc.ApplyRewind(ctx, preview.Token, nil); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"turn1.txt", "turn2.txt", "turn3.txt"} {
		if _, err := os.Stat(filepath.Join(project, name)); err != nil {
			t.Errorf("%s was removed by a conversation-only rewind: %v", name, err)
		}
	}
}

// A fork that fails must leave the session exactly as it was. The conversation
// has not been cut, so a session now pointing at nothing would be worse than
// the rewind simply not happening.
func TestAFailedForkLeavesTheSessionIntact(t *testing.T) {
	svc, store, mock, sessionID, project, prompts := rewindHarness(t)
	ctx := context.Background()
	before, err := store.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}

	preview, err := svc.PlanRewind(ctx, agenthub.RewindRequest{
		SessionID:       sessionID,
		PromptMessageID: prompts[1],
		Mode:            agenthub.RewindBoth,
	})
	if err != nil {
		t.Fatal(err)
	}
	mock.FailNextFork(errors.New("the vendor refused"))

	if _, err := svc.ApplyRewind(ctx, preview.Token, nil); err == nil {
		t.Fatal("expected the rewind to fail with its fork")
	}

	if got := transcriptTexts(t, store, sessionID); len(got) != 3 {
		t.Fatalf("prompts = %v, want all three still there", got)
	}
	after, err := store.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if after.ACPSessionID != before.ACPSessionID {
		t.Fatalf("acp session = %q, want it unchanged at %q", after.ACPSessionID, before.ACPSessionID)
	}
	// The files are the half that runs after the fork, so they must be untouched.
	for _, name := range []string{"turn1.txt", "turn2.txt", "turn3.txt"} {
		if _, err := os.Stat(filepath.Join(project, name)); err != nil {
			t.Errorf("%s was removed by a rewind that never got past its fork: %v", name, err)
		}
	}
	// And the safety row says a rewind was attempted and did not finish, rather
	// than still claiming to be in flight.
	records, err := store.ListCheckpoints(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	var safety int
	for _, record := range records {
		if record.Kind != agenthub.CheckpointRewindSafety {
			continue
		}
		safety++
		if record.Status != agenthub.CheckpointFailed {
			t.Errorf("safety row status = %q, want failed", record.Status)
		}
	}
	if safety != 1 {
		t.Fatalf("safety rows = %d, want one", safety)
	}
	// Nothing is left pending, so no banner offers to recover a rewind that
	// never touched anything.
	pending, err := store.ListInterruptedRewinds(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("interrupted rewinds = %+v, want none", pending)
	}
}

// A rewind is the person's, like a restore, and for the same reason: an agent
// that could erase its own turns could erase the evidence of them.
func TestAnAgentMayNotRewind(t *testing.T) {
	svc, _, _, sessionID, _, prompts := rewindHarness(t)
	agentCtx := identity.WithCaller(context.Background(), identity.Agent(sessionID))
	_, err := svc.PlanRewind(agentCtx, agenthub.RewindRequest{
		SessionID:       sessionID,
		PromptMessageID: prompts[1],
		Mode:            agenthub.RewindBoth,
	})
	if !errors.Is(err, agenthub.ErrRestoreNotPermitted) {
		t.Fatalf("PlanRewind as an agent = %v, want ErrRestoreNotPermitted", err)
	}

	preview, err := svc.PlanRewind(context.Background(), agenthub.RewindRequest{
		SessionID:       sessionID,
		PromptMessageID: prompts[1],
		Mode:            agenthub.RewindBoth,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApplyRewind(agentCtx, preview.Token, nil); !errors.Is(err, agenthub.ErrRestoreNotPermitted) {
		t.Fatalf("ApplyRewind as an agent = %v, want ErrRestoreNotPermitted", err)
	}
}

// The session-level rows describe the session, not the conversation: they are
// true of the branch too. Usage is not — it is the running cost of messages
// that no longer exist.
func TestRewindKeepsSettingsRowsAndDropsUsage(t *testing.T) {
	svc, store, _, sessionID, _, prompts := rewindHarness(t)
	ctx := context.Background()

	for i, raw := range []string{
		`{"sessionUpdate":"current_mode_update","currentModeId":"plan","availableModes":[{"id":"plan","name":"Plan"}]}`,
		`{"sessionUpdate":"usage_update","usage":{"totalTokens":1234}}`,
	} {
		update := agenthub.ParseSessionUpdate(json.RawMessage(raw))
		payload, err := json.Marshal(update)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.AppendTranscript(ctx, agenthub.TranscriptEntry{
			ID:        "row-" + itoa(i),
			SessionID: sessionID,
			Kind:      update.Kind,
			Payload:   payload,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}

	preview, err := svc.PlanRewind(ctx, agenthub.RewindRequest{
		SessionID:       sessionID,
		PromptMessageID: prompts[0],
		Mode:            agenthub.RewindConversation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApplyRewind(ctx, preview.Token, nil); err != nil {
		t.Fatal(err)
	}

	entries, err := store.ListTranscript(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[agenthub.UpdateKind]int{}
	seqs := map[int64]bool{}
	for _, entry := range entries {
		kinds[entry.Kind]++
		if seqs[entry.Seq] {
			t.Fatalf("two rows share seq %d, which the unique index forbids", entry.Seq)
		}
		seqs[entry.Seq] = true
	}
	if kinds[agenthub.UpdateCurrentMode] != 1 {
		t.Errorf("mode rows = %d, want the session's mode kept", kinds[agenthub.UpdateCurrentMode])
	}
	if kinds[agenthub.UpdateUsage] != 0 {
		t.Errorf("usage rows = %d, want none: it is the cost of messages that are gone", kinds[agenthub.UpdateUsage])
	}

	// The kept row was renumbered into the space the cut freed, so the next
	// append does not collide with it.
	if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sessionID, Prompt: "after the rewind"}); err != nil {
		t.Fatalf("a prompt after the rewind failed: %v", err)
	}
}

// One token, one rewind — the same rule a restore has, for the stronger reason:
// the second would branch a conversation the first already replaced.
func TestARewindPlanIsSingleUse(t *testing.T) {
	svc, _, _, sessionID, _, prompts := rewindHarness(t)
	ctx := context.Background()
	preview, err := svc.PlanRewind(ctx, agenthub.RewindRequest{
		SessionID:       sessionID,
		PromptMessageID: prompts[2],
		Mode:            agenthub.RewindConversation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApplyRewind(ctx, preview.Token, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApplyRewind(ctx, preview.Token, nil); !errors.Is(err, agenthub.ErrRestorePlanStale) {
		t.Fatalf("second apply = %v, want ErrRestorePlanStale", err)
	}
}

// Measured (spike S2): a forked session comes up in the vendor's default mode,
// whatever the source was set to. Without putting it back, a rewind silently
// moves a session out of plan mode — a change the user never asked for, made by
// an operation about something else entirely.
func TestRewindPutsTheSessionsModeAndModelBack(t *testing.T) {
	svc, store, mock, sessionID, _, prompts := rewindHarness(t)
	ctx := context.Background()

	if err := svc.SetMode(ctx, sessionID, "plan"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetConfigOption(ctx, sessionID, "model", "opus"); err != nil {
		t.Fatal(err)
	}
	// The agent's own acknowledgement, recorded the way the recorder records
	// it: the stored payload is the normalized update, not the wire frame.
	for i, raw := range []string{
		`{"sessionUpdate":"current_mode_update","currentModeId":"plan","availableModes":[{"id":"plan","name":"Plan"}]}`,
		`{"sessionUpdate":"config_option_update","options":[{"id":"model","name":"Model","currentValue":"opus"}]}`,
	} {
		update := agenthub.ParseSessionUpdate(json.RawMessage(raw))
		payload, err := json.Marshal(update)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.AppendTranscript(ctx, agenthub.TranscriptEntry{
			ID:        "state-" + itoa(i),
			SessionID: sessionID,
			Kind:      update.Kind,
			Payload:   payload,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}

	preview, err := svc.PlanRewind(ctx, agenthub.RewindRequest{
		SessionID:       sessionID,
		PromptMessageID: prompts[1],
		Mode:            agenthub.RewindConversation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApplyRewind(ctx, preview.Token, nil); err != nil {
		t.Fatal(err)
	}

	forked := mock.LastRuntime()
	if forked == nil {
		t.Fatal("no forked runtime")
	}
	if got := forked.SetModeValue(); got != "plan" {
		t.Errorf("mode after the rewind = %q, want plan put back", got)
	}
	if got := forked.SetOptionValue("model"); got != "opus" {
		t.Errorf("model after the rewind = %q, want opus put back", got)
	}
}

// A turn the agent never replied to leaves no message the vendor can branch at,
// so it goes too. The preview says how many, because finding out afterwards
// that a rewind took an extra message is finding out too late.
func TestARewindAlsoDropsTurnsWithNoReply(t *testing.T) {
	svc, store, _, sessionID, _, prompts := rewindHarness(t)
	ctx := context.Background()

	// A prompt with no reply, the way a cancelled or failed turn leaves one.
	if err := store.AppendTranscript(ctx, agenthub.TranscriptEntry{
		ID:        "no-reply",
		SessionID: sessionID,
		Kind:      agenthub.UpdateUserMessageChunk,
		Text:      "the turn that failed",
		Payload:   json.RawMessage(`{"kind":"user_message_chunk","messageId":"p-failed"}`),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	// And the turn the user actually names, after it.
	if err := store.AppendTranscript(ctx, agenthub.TranscriptEntry{
		ID:        "named",
		SessionID: sessionID,
		Kind:      agenthub.UpdateUserMessageChunk,
		Text:      "the one I am rewinding",
		Payload:   json.RawMessage(`{"kind":"user_message_chunk","messageId":"p-named"}`),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	preview, err := svc.PlanRewind(ctx, agenthub.RewindRequest{
		SessionID:       sessionID,
		PromptMessageID: "p-named",
		Mode:            agenthub.RewindConversation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if preview.ExtraTurnsDropped != 1 {
		t.Fatalf("extraTurnsDropped = %d, want 1", preview.ExtraTurnsDropped)
	}
	var warned bool
	for _, warning := range preview.Warnings {
		if len(warning) > 0 && warning[0] == '1' {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("warnings = %v, want one naming the extra message", preview.Warnings)
	}

	if _, err := svc.ApplyRewind(ctx, preview.Token, nil); err != nil {
		t.Fatal(err)
	}
	texts := transcriptTexts(t, store, sessionID)
	for _, text := range texts {
		if text == "the turn that failed" || text == "the one I am rewinding" {
			t.Fatalf("prompts left = %v, want both cut", texts)
		}
	}
	// And it stopped where there was something to branch at, not further back.
	if len(texts) != 3 {
		t.Fatalf("prompts left = %v, want the three replied-to turns", texts)
	}
	_ = prompts
}

// rewindOrderProbe stands inside the rewind's one write and reports what had
// already happened by the time it ran.
type rewindOrderProbe struct {
	agenthub.Store
	closedAtCut *atomic.Bool
	watch       *atomic.Bool
}

func (p *rewindOrderProbe) RewindSession(ctx context.Context, spec agenthub.RewindSpec) error {
	p.closedAtCut.Store(p.watch.Load())
	return p.Store.RewindSession(ctx, spec)
}

// The old runtime is closed before anything is written, because a live runtime
// keeps recording: an async task or a subagent still running would append rows
// to a transcript being cut out from under it, below a cut that has already
// been decided.
func TestTheOldRuntimeIsClosedBeforeTheCutCommits(t *testing.T) {
	var closedAtCut atomic.Bool
	probe := &rewindOrderProbe{closedAtCut: &closedAtCut}
	svc, store, _, sessionID, _, prompts, closed := rewindHarnessWith(t, func(inner agenthub.Store) agenthub.Store {
		probe.Store = inner
		return probe
	})
	// Wired after the harness, because the flag the runtimes set only exists
	// once they have been made.
	probe.watch = closed
	ctx := context.Background()

	preview, err := svc.PlanRewind(ctx, agenthub.RewindRequest{
		SessionID:       sessionID,
		PromptMessageID: prompts[1],
		Mode:            agenthub.RewindConversation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if closed.Load() {
		t.Fatal("planning a rewind closed the session's runtime")
	}
	if _, err := svc.ApplyRewind(ctx, preview.Token, nil); err != nil {
		t.Fatal(err)
	}
	if !closedAtCut.Load() {
		t.Fatal("the transcript was cut while the old runtime was still running")
	}
	_ = store
}

// The branch point is the newest thing the agent said before the rewound turn,
// not merely something it once said. Taking an older one would silently drop
// the turns in between — a rewind of the third message that quietly took the
// second as well.
func TestTheBranchPointIsTheNewestMessageBeforeTheTurn(t *testing.T) {
	svc, store, mock, sessionID, _, prompts := rewindHarness(t)
	ctx := context.Background()
	before, err := store.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}

	// Rewinding the third turn: there are two candidates behind it, so which
	// one is chosen is now observable.
	preview, err := svc.PlanRewind(ctx, agenthub.RewindRequest{
		SessionID:       sessionID,
		PromptMessageID: prompts[2],
		Mode:            agenthub.RewindConversation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApplyRewind(ctx, preview.Token, nil); err != nil {
		t.Fatal(err)
	}

	forks := mock.Forks()
	if len(forks) != 1 {
		t.Fatalf("forks = %+v, want one", forks)
	}
	if want := "msg_" + before.ACPSessionID + "_2"; forks[0].MessageID != want {
		t.Fatalf("branched at %q, want %q — the reply to the message before the one rewound",
			forks[0].MessageID, want)
	}
	if got := transcriptTexts(t, store, sessionID); len(got) != 2 {
		t.Fatalf("prompts left = %v, want the first two", got)
	}
}

// forkCandidateLimitForTest mirrors the engine's own candidate limit; the point
// of the test below is that exactly this many useless rows do not exhaust it.
const forkCandidateLimitForTest = 5

// A subagent's message is not a branch point: the vendor keeps it in the
// subagent's own session, so asking the parent to fork at it names a message
// that session has never had.
//
// The filter runs in SQL, so the limit counts candidates rather than rows — a
// session whose last hundred assistant messages all came from subagents must
// not hand back a hundred rows for the caller to discard.
func TestASubagentMessageIsNotABranchPoint(t *testing.T) {
	svc, store, mock, sessionID, _, prompts := rewindHarness(t)
	ctx := context.Background()
	before, err := store.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}

	// Five assistant messages carrying no vendor message id — a streamed reply
	// whose rows never got one. They sit between the branch point and the
	// prompt, so a query that did not filter them would spend its whole limit
	// on rows the caller must discard and find no branch point at all.
	for i := range forkCandidateLimitForTest {
		if err := store.AppendTranscript(ctx, agenthub.TranscriptEntry{
			ID:        "idless-" + itoa(i),
			SessionID: sessionID,
			Kind:      agenthub.UpdateAgentMessageChunk,
			Text:      "no id on this one",
			Payload:   json.RawMessage(`{"kind":"agent_message_chunk"}`),
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}

	// What a subagent's reply looks like in the parent's transcript: its own
	// message id, tagged with the session it really came from. It lands after
	// the reply to prompt 2, so a filter that ignored `origin` would branch
	// here instead.
	subagent := agenthub.SessionUpdate{
		Kind:      agenthub.UpdateAgentMessageChunk,
		MessageID: "msg_from_a_subagent",
		Origin:    "vendor-session-of-the-child",
	}
	payload, err := json.Marshal(subagent)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AppendTranscript(ctx, agenthub.TranscriptEntry{
		ID:        "subagent-reply",
		SessionID: sessionID,
		Kind:      agenthub.UpdateAgentMessageChunk,
		Text:      "the subagent said this",
		Payload:   payload,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	// And the prompt the user will rewind, after it.
	if err := store.AppendTranscript(ctx, agenthub.TranscriptEntry{
		ID:        "after-subagent",
		SessionID: sessionID,
		Kind:      agenthub.UpdateUserMessageChunk,
		Text:      "the one I am rewinding",
		Payload:   json.RawMessage(`{"kind":"user_message_chunk","messageId":"p-after-subagent"}`),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	preview, err := svc.PlanRewind(ctx, agenthub.RewindRequest{
		SessionID:       sessionID,
		PromptMessageID: "p-after-subagent",
		Mode:            agenthub.RewindConversation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApplyRewind(ctx, preview.Token, nil); err != nil {
		t.Fatal(err)
	}

	forks := mock.Forks()
	if len(forks) != 1 {
		t.Fatalf("forks = %+v, want one", forks)
	}
	if forks[0].MessageID == "msg_from_a_subagent" {
		t.Fatal("branched at a subagent's message, which the parent session does not have")
	}
	if want := "msg_" + before.ACPSessionID + "_3"; forks[0].MessageID != want {
		t.Fatalf("branched at %q, want %q — the last thing the main thread said", forks[0].MessageID, want)
	}
	_ = prompts
}
