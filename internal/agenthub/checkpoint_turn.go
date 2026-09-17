// internal/agenthub/checkpoint_turn.go
package agenthub

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"devkit/internal/checkpoint"

	"github.com/google/uuid"
)

// One turn's checkpoint, and working out who changed what.
//
// Attribution is the part that has to stay honest. Three things can write to a
// project during a turn: the agent through a tool call, a sub-session running
// nested inside it, and anything else — a Bash script, an MCP server, an async
// task, or a second session the user has open on the same folder. Only the
// first two leave evidence, so the third is labelled as what it is rather than
// guessed at, with the list of sessions that were running at the time attached
// so the user can make the guess DevKit will not.

type turnCheckpoint struct {
	engine *EngineCheckpoints
	spec   TurnCheckpointSpec

	id         string
	projectKey string
	startedAt  time.Time
	activity   *checkpoint.Handle

	// done closes when the opening snapshot has finished or given up. Reading
	// repo/before/beforeErr is only safe after it.
	done         chan struct{}
	cancelBefore context.CancelFunc
	repo         *checkpoint.Repo
	before       checkpoint.Snapshot
	beforeErr    error
}

func (c *EngineCheckpoints) Begin(ctx context.Context, spec TurnCheckpointSpec) TurnCheckpoint {
	if !c.Enabled() || spec.ProjectPath == "" {
		return noTurnCheckpoint{}
	}
	key, _, err := checkpoint.ProjectKey(spec.ProjectPath)
	if err != nil {
		c.fail(spec.SessionID, err)
		return noTurnCheckpoint{}
	}

	t := &turnCheckpoint{
		engine:     c,
		spec:       spec,
		id:         uuid.NewString(),
		projectKey: key,
		startedAt:  time.Now().UTC(),
		done:       make(chan struct{}),
	}
	c.mu.Lock()
	c.openTurns[t.id] = spec.SessionID
	c.mu.Unlock()
	t.activity = c.engine.Activity().Begin(key, spec.SessionID, checkpoint.KindTurn)

	// WithoutCancel with its own budget: the turn's context is not this
	// snapshot's clock. Cancelling the turn must not leave a half-written index
	// behind, and a turn that takes an hour must not leave a git running for an
	// hour either.
	snapCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), checkpointBeforeBudget)
	t.cancelBefore = cancel
	go func() {
		defer close(t.done)
		repo, err := c.engine.Open(snapCtx, spec.ProjectPath)
		if err != nil {
			t.beforeErr = err
			return
		}
		t.repo = repo
		snap, err := repo.Snapshot(snapCtx, t.id, checkpoint.SlotBefore)
		if err != nil {
			t.beforeErr = err
			return
		}
		t.before = snap
	}()
	return t
}

func (t *turnCheckpoint) ID() string { return t.id }

// Wait joins the opening snapshot. It cannot outrun its budget: the context it
// runs under kills git when the budget is gone, so this returns either with a
// tree or with the reason there is none.
func (t *turnCheckpoint) Wait() {
	<-t.done
	t.cancelBefore()
}

func (t *turnCheckpoint) Finish(ctx context.Context, result TurnResult) {
	t.Wait()
	c := t.engine
	c.mu.Lock()
	delete(c.openTurns, t.id)
	c.mu.Unlock()

	record := CheckpointRecord{
		ID:              t.id,
		SessionID:       t.spec.SessionID,
		ProjectKey:      t.projectKey,
		PromptMessageID: t.spec.PromptMessageID,
		Kind:            CheckpointTurn,
		BeforeTree:      t.before.Tree,
		Status:          CheckpointDone,
		Outcome:         CheckpointOutcomeOK,
		CreatedAt:       t.startedAt,
	}
	if len(t.before.Skipped) > 0 {
		record.Skipped, _ = json.Marshal(t.before.Skipped)
	}

	// The closing snapshot runs here and not in the goroutine below, because the
	// session is about to go idle and idle means another prompt may start. A
	// tree taken after that point would hold the next turn's writes and blame
	// them on this one.
	var (
		after    checkpoint.Snapshot
		afterErr = t.beforeErr
	)
	if t.repo != nil {
		afterCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), checkpointAfterBudget)
		snap, err := t.repo.Snapshot(afterCtx, t.id, checkpoint.SlotAfter)
		cancel()
		afterErr = err
		if err == nil {
			after = snap
			record.AfterTree = snap.Tree
			if len(snap.Skipped) > 0 {
				record.Skipped, _ = json.Marshal(snap.Skipped)
			}
		}
	}

	// Which half is missing decides what the feature can offer. Neither half
	// means the project cannot be checkpointed at all — no git, too large, an
	// unreadable directory — and the row says so rather than pretending to be a
	// checkpoint with nothing in it.
	switch {
	case record.BeforeTree == "" && record.AfterTree == "":
		record.Status = CheckpointFailed
		record.Outcome = CheckpointOutcomeUnavailable
		record.Detail = reasonDetail(firstErr(t.beforeErr, afterErr))
	case record.BeforeTree == "":
		// The turn ran without an opening tree. The row still says what the
		// project looks like now, but there is nothing to go back to.
		record.Outcome = CheckpointOutcomeBeforeMissing
		record.Detail = reasonDetail(t.beforeErr)
	case record.AfterTree == "":
		record.Outcome = CheckpointOutcomeAfterMissing
		record.Detail = reasonDetail(afterErr)
	}

	finishedAt := time.Now().UTC()
	record.FinishedAt = &finishedAt
	t.activity.End()

	// Everything from here is reading and classifying, which nothing is waiting
	// on. The turn is over the moment the tree exists.
	go t.settle(record, after, result, finishedAt)
}

func (t *turnCheckpoint) settle(record CheckpointRecord, after checkpoint.Snapshot, result TurnResult, finishedAt time.Time) {
	c := t.engine
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	record.AgentReport = c.takeReport(t.id)
	if t.repo != nil && record.BeforeTree != "" && record.AfterTree != "" {
		changes, err := t.repo.Diff(ctx, record.BeforeTree, after.Tree)
		if err != nil {
			c.fail(t.spec.SessionID, err)
		} else {
			record.Changed = t.classify(ctx, changes, record.AgentReport, result, finishedAt)
		}
	}
	// Who else was writing in this project while the turn ran. Attached
	// whatever the outcome: it is the context for every unattributed file, and
	// for the warning a restore shows.
	if overlap := c.engine.Activity().Overlap(t.projectKey, t.startedAt, finishedAt, t.spec.SessionID); len(overlap) > 0 {
		record.Overlap, _ = json.Marshal(overlappingSessions(overlap))
	}

	if err := c.store.InsertCheckpoint(ctx, record); err != nil {
		c.fail(t.spec.SessionID, err)
		return
	}
	if c.OnCheckpoint != nil {
		c.OnCheckpoint(t.spec.SessionID, record)
	}
}

// firstErr returns the first non-nil error, for a detail that names the
// original cause rather than whichever failure was noticed last.
func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// classify turns a diff into changed files with a source on each.
func (t *turnCheckpoint) classify(ctx context.Context, changes []checkpoint.Change, report json.RawMessage, result TurnResult, finishedAt time.Time) []ChangedFile {
	agentPaths := reportedPaths(report)
	for _, path := range result.ToolPaths {
		if rel, ok := t.relative(path); ok {
			agentPaths[rel] = true
		}
	}
	childPaths := t.childPaths(ctx, finishedAt)

	out := make([]ChangedFile, 0, len(changes))
	for _, change := range changes {
		file := ChangedFile{
			Path:    change.Path,
			Kind:    string(change.Kind),
			Added:   change.Added,
			Removed: change.Removed,
			Binary:  change.Binary(),
			Source:  SourceUnattributed,
		}
		if file.Binary {
			file.Added, file.Removed = 0, 0
		}
		switch {
		case agentPaths[change.Path]:
			file.Source = SourceAgent
		case childPaths[change.Path] != "":
			file.Source, file.SourceSession = SourceChild, childPaths[change.Path]
		}
		out = append(out, file)
	}
	return out
}

// childPaths maps a path to the sub-session that checkpointed it inside this
// turn. A sub-session runs nested in its parent's turn (the MCP tool blocks
// until it is done), so its own checkpoints fall inside this window.
func (t *turnCheckpoint) childPaths(ctx context.Context, finishedAt time.Time) map[string]string {
	out := map[string]string{}
	if t.engine.sessions == nil {
		return out
	}
	sessions, err := t.engine.sessions.ListSessions(ctx)
	if err != nil {
		return out
	}
	for _, session := range sessions {
		if session.ParentSessionID == nil || *session.ParentSessionID != t.spec.SessionID {
			continue
		}
		records, err := t.engine.store.ListCheckpoints(ctx, session.ID)
		if err != nil {
			continue
		}
		for _, record := range records {
			if record.CreatedAt.Before(t.startedAt) || record.CreatedAt.After(finishedAt) {
				continue
			}
			for _, file := range record.Changed {
				out[file.Path] = session.ID
			}
		}
	}
	return out
}

// relative turns a path a tool call named into a project-relative one. A tool
// call reports absolute paths; the trees are keyed on relative ones.
func (t *turnCheckpoint) relative(path string) (string, bool) {
	if path == "" {
		return "", false
	}
	if !filepath.IsAbs(path) {
		return filepath.ToSlash(filepath.Clean(path)), true
	}
	root := t.spec.ProjectPath
	if t.repo != nil {
		root = t.repo.WorkTree()
	}
	// Both sides resolved, or /tmp and /private/tmp never match on macOS.
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// overlappingSession is what a checkpoint records about another session that
// was writing at the same time.
type overlappingSession struct {
	SessionID string    `json:"sessionId"`
	Kind      string    `json:"kind"`
	Start     time.Time `json:"start"`
	End       time.Time `json:"end,omitempty"`
}

func overlappingSessions(spans []checkpoint.Span) []overlappingSession {
	out := make([]overlappingSession, 0, len(spans))
	for _, span := range spans {
		out = append(out, overlappingSession{
			SessionID: span.Session, Kind: string(span.Kind), Start: span.Start, End: span.End,
		})
	}
	return out
}

// reportedPaths reads the vendor's agentFileChangeReport.
//
// The shape is the vendor's, and a vendor that changes it must not be able to
// make DevKit claim more than it knows: anything unreadable yields an empty
// set, which downgrades files to unattributed rather than mislabelling them.
func reportedPaths(report json.RawMessage) map[string]bool {
	out := map[string]bool{}
	if len(report) == 0 {
		return out
	}
	var body struct {
		Files []struct {
			Path string `json:"path"`
		} `json:"files"`
		Paths []string `json:"paths"`
	}
	if err := json.Unmarshal(report, &body); err != nil {
		return out
	}
	for _, file := range body.Files {
		if rel := strings.TrimPrefix(filepath.ToSlash(file.Path), "./"); rel != "" {
			out[rel] = true
		}
	}
	for _, path := range body.Paths {
		if rel := strings.TrimPrefix(filepath.ToSlash(path), "./"); rel != "" {
			out[rel] = true
		}
	}
	return out
}

// reasonDetail stores why a checkpoint is incomplete, for the UI to explain.
func reasonDetail(err error) json.RawMessage {
	if err == nil {
		return nil
	}
	raw, marshalErr := json.Marshal(struct {
		Reason string `json:"reason"`
	}{Reason: err.Error()})
	if marshalErr != nil {
		return nil
	}
	return raw
}
