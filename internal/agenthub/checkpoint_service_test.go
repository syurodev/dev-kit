package agenthub_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"devkit/internal/acp"
	"devkit/internal/agenthub"
	"devkit/internal/checkpoint"
	"devkit/internal/storage"
)

// What a turn's checkpoint has to get right, against a real git and the mock
// adapter. The engine's own behaviour is pinned in internal/checkpoint; these
// are the joins — the order things happen in around a prompt, and whether the
// right thing ends up in the row.

// duringTurn is the mock adapter with a hook that runs inside the prompt —
// after the opening snapshot has been joined and before the prompt returns.
// That is exactly where an agent's writes land, and nothing else in the test
// harness can put them there.
type duringTurn struct {
	*agenthub.MockAdapter
	write func()
}

func (a duringTurn) Open(ctx context.Context, spec agenthub.OpenSpec) (agenthub.SessionRuntime, error) {
	rt, err := a.MockAdapter.Open(ctx, spec)
	if err != nil {
		return nil, err
	}
	return writingRuntime{SessionRuntime: rt, write: a.write, mu: &sync.Mutex{}, held: &agenthub.RuntimeHandlers{}}, nil
}

type writingRuntime struct {
	agenthub.SessionRuntime
	write func()
	mu    *sync.Mutex
	held  *agenthub.RuntimeHandlers
}

// SetHandlers keeps a copy on the way through, so a test can push an update
// into the turn the way the vendor would.
func (r writingRuntime) SetHandlers(h agenthub.RuntimeHandlers) {
	r.mu.Lock()
	*r.held = h
	r.mu.Unlock()
	r.SessionRuntime.SetHandlers(h)
}

func (r writingRuntime) emit(update string) {
	r.mu.Lock()
	handler := r.held.OnUpdate
	r.mu.Unlock()
	if handler != nil {
		handler(json.RawMessage(update), nil)
	}
}

// Unwrap lets MockPromptMetas see the mock through this wrapper.
func (r writingRuntime) Unwrap() agenthub.SessionRuntime { return r.SessionRuntime }

func (r writingRuntime) Prompt(ctx context.Context, prompt []acp.ContentBlock, meta json.RawMessage) (agenthub.PromptOutcome, error) {
	if r.write != nil {
		r.write()
	}
	if emitting != nil {
		r.emit(emitting())
	}
	return r.SessionRuntime.Prompt(ctx, prompt, meta)
}

// emitting is what one test pushes into its turn. Package-level because the
// adapter is registered before the test knows what it wants to send.
var emitting func() string

func checkpointService(t *testing.T) (*agenthub.Service, agenthub.Store, *agenthub.EngineCheckpoints, string) {
	t.Helper()
	return checkpointServiceWriting(t, nil)
}

// checkpointServiceWriting builds the same service with a hook that runs inside
// every prompt.
func checkpointServiceWriting(t *testing.T, write func()) (*agenthub.Service, agenthub.Store, *agenthub.EngineCheckpoints, string) {
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
	store := agenthub.NewSQLStore(db)
	reg := agenthub.NewRegistry()
	reg.Register(duringTurn{MockAdapter: agenthub.NewMockAdapter(), write: write})
	svc := agenthub.NewService(reg, store, func(string, json.RawMessage) {})

	engine := checkpoint.New(filepath.Join(t.TempDir(), "checkpoints"), git, checkpoint.Limits{})
	cps := agenthub.NewEngineCheckpoints(engine, store, store)
	svc.SetCheckpoints(cps)
	return svc, store, cps, t.TempDir()
}

// waitForCheckpoint polls for the settled row. The diff and the row write
// happen off the turn's path on purpose, so a test that read immediately would
// be testing the timing rather than the result.
func waitForCheckpoint(t *testing.T, store agenthub.Store, sessionID string, want int) []agenthub.CheckpointRecord {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		records, err := store.ListCheckpoints(context.Background(), sessionID)
		if err != nil {
			t.Fatal(err)
		}
		if len(records) >= want {
			return records
		}
		if time.Now().After(deadline) {
			t.Fatalf("checkpoints = %d, want %d", len(records), want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func changedPaths(record agenthub.CheckpointRecord) map[string]agenthub.ChangedFile {
	out := map[string]agenthub.ChangedFile{}
	for _, file := range record.Changed {
		out[file.Path] = file
	}
	return out
}

// The whole point: a turn writes down what the project looked like on either
// side of it, and the turn's own prompt message id is what ties the two ends
// together.
func TestTurnRecordsACheckpointKeyedOnThePrompt(t *testing.T) {
	var project string
	svc, store, _, project := checkpointServiceWriting(t, func() {
		writeFile(t, project, "a.txt", "two\n")
		writeFile(t, project, "new.txt", "fresh\n")
	})
	ctx := context.Background()
	writeFile(t, project, "a.txt", "one\n")

	session, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: project})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: session.ID, Prompt: "hello"}); err != nil {
		t.Fatalf("invoke: %v", err)
	}

	records := waitForCheckpoint(t, store, session.ID, 1)
	record := records[0]
	if record.Kind != agenthub.CheckpointTurn || record.Status != agenthub.CheckpointDone {
		t.Fatalf("record = %+v", record)
	}
	if record.Outcome != agenthub.CheckpointOutcomeOK {
		t.Fatalf("outcome = %q, want ok (detail %s)", record.Outcome, record.Detail)
	}
	if record.BeforeTree == "" || record.AfterTree == "" || record.BeforeTree == record.AfterTree {
		t.Fatalf("trees = %q / %q", record.BeforeTree, record.AfterTree)
	}

	turns, err := store.ListTurns(ctx, session.ID)
	if err != nil || len(turns) != 1 {
		t.Fatalf("turns = %v, %v", turns, err)
	}
	if record.PromptMessageID == "" || record.PromptMessageID != turns[0].PromptMessageID {
		t.Fatalf("checkpoint prompt id %q, turn prompt id %q", record.PromptMessageID, turns[0].PromptMessageID)
	}

	changed := changedPaths(record)
	if got, ok := changed["a.txt"]; !ok || got.Kind != "modified" {
		t.Errorf("a.txt = %+v", got)
	}
	if got, ok := changed["new.txt"]; !ok || got.Kind != "added" {
		t.Errorf("new.txt = %+v", got)
	}
}

// idleProbe fires the moment the session's row is set back to idle.
//
// Idle is the UI's signal that another prompt may be sent, so it is the exact
// instant the closing snapshot has to already be behind. Hooking the status
// write is the only way to stand between the two: a defer around the snapshot
// still runs before Invoke returns, so nothing the caller can observe would
// tell the two orderings apart.
type idleProbe struct {
	agenthub.Store
	mu     sync.Mutex
	onIdle func()
	fired  bool
}

func (s *idleProbe) UpdateSessionStatus(ctx context.Context, id string, status agenthub.SessionStatus, at time.Time) error {
	if status == agenthub.SessionStatusIdle {
		s.mu.Lock()
		hook := s.onIdle
		if hook != nil && !s.fired {
			s.fired = true
			s.mu.Unlock()
			hook()
		} else {
			s.mu.Unlock()
		}
	}
	return s.Store.UpdateSessionStatus(ctx, id, status, at)
}

// The closing snapshot has to be taken before the session goes idle. Idle means
// another prompt may start, and a tree taken after that point would hold the
// next turn's writes and blame them on this one.
func TestClosingSnapshotHappensBeforeTheSessionGoesIdle(t *testing.T) {
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
	project := t.TempDir()
	probe := &idleProbe{Store: agenthub.NewSQLStore(db)}
	reg := agenthub.NewRegistry()
	reg.Register(duringTurn{MockAdapter: agenthub.NewMockAdapter(), write: func() {
		writeFile(t, project, "during.txt", "in the turn\n")
	}})
	svc := agenthub.NewService(reg, probe, func(string, json.RawMessage) {})
	engine := checkpoint.New(filepath.Join(t.TempDir(), "checkpoints"), git, checkpoint.Limits{})
	svc.SetCheckpoints(agenthub.NewEngineCheckpoints(engine, probe, probe))

	ctx := context.Background()
	writeFile(t, project, "a.txt", "one\n")
	session, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: project})
	if err != nil {
		t.Fatal(err)
	}
	// What the next prompt would write, the instant the UI is told it may send
	// one. It must not land in this turn's checkpoint.
	probe.onIdle = func() { writeFile(t, project, "next-turn.txt", "the turn after\n") }

	if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: session.ID, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}

	records := waitForCheckpoint(t, probe, session.ID, 1)
	changed := changedPaths(records[0])
	if _, ok := changed["during.txt"]; !ok {
		t.Fatalf("the turn's own write is missing: %+v", records[0].Changed)
	}
	if _, ok := changed["next-turn.txt"]; ok {
		t.Fatal("a write from the moment the session went idle was attributed to this turn")
	}
}

// The prompt has to carry the checkpoint's id, or the vendor's report comes
// back tagged with nothing and cannot be filed.
func TestPromptCarriesTheFileChangeReportRequest(t *testing.T) {
	svc, store, _, project := checkpointService(t)
	ctx := context.Background()

	session, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: project})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: session.ID, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	rt, ok := svc.RuntimeForTest(session.ID)
	if !ok {
		t.Fatal("no runtime")
	}
	metas := agenthub.MockPromptMetas(rt)
	if len(metas) != 1 {
		t.Fatalf("prompt metas = %d, want 1", len(metas))
	}
	var body struct {
		JetBrains struct {
			Air struct {
				Request struct {
					Version   int    `json:"version"`
					RequestID string `json:"requestId"`
				} `json:"agentFileChangeReportRequest"`
			} `json:"air"`
		} `json:"jetbrains"`
	}
	if err := json.Unmarshal(metas[0], &body); err != nil {
		t.Fatalf("meta = %s: %v", metas[0], err)
	}
	request := body.JetBrains.Air.Request
	if request.Version != 1 || request.RequestID == "" {
		t.Fatalf("request = %+v, want version 1 and an id", request)
	}

	records := waitForCheckpoint(t, store, session.ID, 1)
	if records[0].ID != request.RequestID {
		t.Fatalf("requestId %q is not the checkpoint id %q", request.RequestID, records[0].ID)
	}
}

// With checkpoints off the prompt must carry no request at all: asking for the
// report switches on the vendor's own file history under ~/.claude, and a user
// who turned the feature off should get none.
func TestPromptCarriesNoRequestWhenCheckpointsAreOff(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	session, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: session.ID, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	rt, _ := svc.RuntimeForTest(session.ID)
	for _, meta := range agenthub.MockPromptMetas(rt) {
		if len(meta) != 0 {
			t.Fatalf("prompt carried _meta %s with checkpoints off", meta)
		}
	}
	if svc.CheckpointsEnabled() {
		t.Fatal("checkpoints report themselves enabled with no engine")
	}
}

// A file the vendor names is the agent's doing; one nothing names is not, and
// saying so is the difference between a useful label and a guess.
func TestChangedFilesCarryTheirSource(t *testing.T) {
	var (
		project   string
		sessionID string
		cps       *agenthub.EngineCheckpoints
	)
	svc, store, cps, project := checkpointServiceWriting(t, func() {
		writeFile(t, project, "by-agent.txt", "two\n")
		writeFile(t, project, "by-script.txt", "two\n")
		// The vendor's report, as it arrives mid-turn — with no requestId, the
		// way a vendor that does not echo it back would send. One turn per
		// session is open, so it still files correctly.
		cps.AttachReport(sessionID, "", json.RawMessage(`{"files":[{"path":"by-agent.txt"}]}`))
	})
	ctx := context.Background()
	writeFile(t, project, "by-agent.txt", "one\n")
	writeFile(t, project, "by-script.txt", "one\n")

	session, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: project})
	if err != nil {
		t.Fatal(err)
	}
	sessionID = session.ID
	if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: session.ID, Prompt: "go"}); err != nil {
		t.Fatal(err)
	}

	records := waitForCheckpoint(t, store, session.ID, 1)
	changed := changedPaths(records[0])
	if got := changed["by-agent.txt"].Source; got != agenthub.SourceAgent {
		t.Errorf("by-agent.txt source = %q, want agent", got)
	}
	if got := changed["by-script.txt"].Source; got != agenthub.SourceUnattributed {
		t.Errorf("by-script.txt source = %q, want unattributed", got)
	}
}

// The report and the prompt response land at the same instant (spike S7), so
// one arriving after the row is written is not an edge case. It has to go back
// and correct the row, or a file the agent admitted to stays labelled as
// nobody's.
func TestALateReportReattributesASettledCheckpoint(t *testing.T) {
	var project string
	svc, store, cps, project := checkpointServiceWriting(t, func() {
		writeFile(t, project, "late.txt", "two\n")
	})
	ctx := context.Background()
	writeFile(t, project, "late.txt", "one\n")

	session, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: project})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: session.ID, Prompt: "go"}); err != nil {
		t.Fatal(err)
	}
	records := waitForCheckpoint(t, store, session.ID, 1)
	if got := changedPaths(records[0])["late.txt"].Source; got != agenthub.SourceUnattributed {
		t.Fatalf("before the report, late.txt = %q, want unattributed", got)
	}

	cps.AttachReport(session.ID, records[0].ID, json.RawMessage(`{"files":[{"path":"late.txt"}]}`))

	deadline := time.Now().Add(10 * time.Second)
	for {
		record, err := store.GetCheckpoint(ctx, records[0].ID)
		if err != nil {
			t.Fatal(err)
		}
		if changedPaths(record)["late.txt"].Source == agenthub.SourceAgent {
			if len(record.AgentReport) == 0 {
				t.Fatal("the report was applied but not stored")
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the late report never reattributed the file: %+v", record.Changed)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A project too large to snapshot must not fail the turn. The prompt is what
// the user asked for; the checkpoint is a convenience.
func TestATurnStillRunsWhenTheProjectCannotBeSnapshotted(t *testing.T) {
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
	store := agenthub.NewSQLStore(db)
	reg := agenthub.NewRegistry()
	reg.Register(agenthub.NewMockAdapter())
	svc := agenthub.NewService(reg, store, func(string, json.RawMessage) {})

	project := t.TempDir()
	for _, name := range []string{"a", "b", "c", "d"} {
		writeFile(t, project, name+".txt", name)
	}
	engine := checkpoint.New(filepath.Join(t.TempDir(), "checkpoints"), git, checkpoint.Limits{MaxFiles: 2})
	svc.SetCheckpoints(agenthub.NewEngineCheckpoints(engine, store, store))

	session, err := svc.CreateSession(context.Background(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: project})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: session.ID, Prompt: "hello"})
	if err != nil {
		t.Fatalf("the turn failed because its checkpoint did: %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed", result.Status)
	}

	records := waitForCheckpoint(t, store, session.ID, 1)
	if records[0].Status != agenthub.CheckpointFailed || records[0].Outcome != agenthub.CheckpointOutcomeUnavailable {
		t.Fatalf("record = %+v, want failed/unavailable", records[0])
	}
	if !strings.Contains(string(records[0].Detail), "too large") {
		t.Errorf("detail = %s, want the reason to reach the UI", records[0].Detail)
	}
}

// The opening snapshot runs alongside the process resume, and the prompt does
// not wait for it past its budget. Without the join, a turn could go out before
// its own baseline was taken and the diff would include the agent's own writes
// as if they were there beforehand.
func TestOpeningSnapshotIsJoinedBeforeThePrompt(t *testing.T) {
	var project string
	svc, store, cps, project := checkpointServiceWriting(t, func() {
		writeFile(t, project, "a.txt", "during the turn\n")
	})
	ctx := context.Background()
	writeFile(t, project, "a.txt", "before the turn\n")

	session, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: project})
	if err != nil {
		t.Fatal(err)
	}
	// Changed between session creation and the prompt: the opening tree must
	// hold this version, not the one the baseline saw.
	writeFile(t, project, "a.txt", "just before the prompt\n")
	if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: session.ID, Prompt: "go"}); err != nil {
		t.Fatal(err)
	}

	records := waitForCheckpoint(t, store, session.ID, 1)
	changed := changedPaths(records[0])
	file, ok := changed["a.txt"]
	if !ok {
		t.Fatalf("the turn's diff is %+v, want a.txt", records[0].Changed)
	}
	// One line replaced by one line. Had the opening tree been the baseline's
	// rather than the one taken at the prompt, the diff would still name a.txt
	// — so the content is what proves which tree it was.
	body, err := engineFileContent(t, cps, project, records[0].BeforeTree, "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if body != "just before the prompt\n" {
		t.Fatalf("opening tree holds %q, want the version from the moment of the prompt", body)
	}
	if file.Added != 1 || file.Removed != 1 {
		t.Errorf("a.txt = +%d -%d, want +1 -1", file.Added, file.Removed)
	}
}

// engineFileContent reads one path out of a tree, through a second engine over
// the same shadow directory — the test asks git what the checkpoint stored
// rather than trusting the row.
func engineFileContent(t *testing.T, cps *agenthub.EngineCheckpoints, project, tree, path string) (string, error) {
	t.Helper()
	repo, err := cps.Engine().Open(context.Background(), project)
	if err != nil {
		return "", err
	}
	raw, err := repo.FileContent(context.Background(), tree, path)
	return string(raw), err
}

// A tool call that names where it worked is the other half of attribution, and
// the half that works when the vendor sends no report at all — a turn cancelled
// before it finished gets none. The path a tool call reports is absolute; the
// trees are keyed on project-relative paths, so the two have to be reconciled
// or every file the agent edited reads as nobody's.
func TestATurnAttributesAFileAToolCallNamed(t *testing.T) {
	var project string
	svc, store, _, project := checkpointServiceWriting(t, func() {
		writeFile(t, project, "edited.ts", "two\n")
		writeFile(t, project, "scripted.ts", "two\n")
	})
	t.Cleanup(func() { emitting = nil })
	emitting = func() string {
		// Absolute, the way a tool call reports it, and only for the file the
		// agent actually edited.
		return `{"sessionUpdate":"tool_call","toolCallId":"c1","title":"Edit","status":"completed",` +
			`"locations":[{"path":"` + filepath.ToSlash(filepath.Join(project, "edited.ts")) + `"}]}`
	}

	ctx := context.Background()
	writeFile(t, project, "edited.ts", "one\n")
	writeFile(t, project, "scripted.ts", "one\n")
	session, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: project})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: session.ID, Prompt: "go"}); err != nil {
		t.Fatal(err)
	}

	records := waitForCheckpoint(t, store, session.ID, 1)
	changed := changedPaths(records[0])
	if got := changed["edited.ts"].Source; got != agenthub.SourceAgent {
		t.Errorf("edited.ts source = %q, want agent (the tool call named it)", got)
	}
	if got := changed["scripted.ts"].Source; got != agenthub.SourceUnattributed {
		t.Errorf("scripted.ts source = %q, want unattributed (nothing named it)", got)
	}
}
