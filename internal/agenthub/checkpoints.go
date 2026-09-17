// internal/agenthub/checkpoints.go
package agenthub

import (
	"context"
	"encoding/json"
	"log"
	"sort"
	"sync"
	"time"

	"devkit/internal/checkpoint"
)

// Checkpointing a turn.
//
// The engine takes trees; this decides when. Two rules shape everything below:
//
//   - A checkpoint may never delay a prompt. The opening snapshot runs
//     alongside the process resume it would otherwise be added to, and if it
//     is not done by the time the prompt is ready to go, it is killed and the
//     turn runs without one. A user waiting on their agent must not be waiting
//     on git.
//   - The closing snapshot happens before the session goes idle, on every exit
//     path. Idle is the UI's signal that another prompt may be sent, and a
//     second turn starting while the first one's tree is still being written
//     would attribute its own writes to the turn before it.

// Budgets for the two snapshots. The opening one is tight because a turn is
// waiting on it; the closing one is looser because nothing is, beyond the
// session going idle a moment later.
const (
	checkpointBeforeBudget = 5 * time.Second
	checkpointAfterBudget  = 10 * time.Second
)

// Checkpoints is the file-checkpoint engine as Service needs it. A nil
// Checkpoints is a service with the feature off, and every call site is written
// so that costs nothing.
type Checkpoints interface {
	// Enabled reports whether a checkpoint would actually be taken. The adapter
	// asks before declaring the vendor capability that turns on Claude's own
	// file history, so a disabled feature leaves no trace on disk.
	Enabled() bool
	// Baseline warms a project's shadow repository off the turn's path, so the
	// first snapshot a prompt waits for is the 0.04s kind rather than the
	// 0.86s kind. Returns immediately.
	Baseline(projectPath string)
	// Begin opens a turn's checkpoint and starts its opening snapshot. It never
	// returns nil: a disabled or failing engine returns a handle whose ID is
	// empty and whose other methods do nothing.
	Begin(ctx context.Context, spec TurnCheckpointSpec) TurnCheckpoint
	// AttachReport files the vendor's own list of changed files under the
	// requestId the prompt asked it to use. Reports arrive alongside the prompt
	// response and sometimes after it, so this has to work either way.
	// sessionID is the session the update arrived on. A vendor that sends its
	// report without echoing the requestId back still gets filed correctly:
	// only one turn per session is open at a time.
	AttachReport(sessionID, requestID string, report json.RawMessage)
}

// TurnCheckpointSpec names the turn being checkpointed.
type TurnCheckpointSpec struct {
	SessionID       string
	ProjectPath     string
	PromptMessageID string
	// ParentSessionID is set for a sub-session, so its checkpoint can be
	// attributed to the parent turn it ran inside.
	ParentSessionID string
}

// TurnResult is what the turn learned, handed back when it closes.
type TurnResult struct {
	// ToolPaths are the project-relative paths the turn's tool calls named.
	ToolPaths []string
	// Cancelled says the turn was interrupted; the vendor sends no report for
	// one, so its absence is not a fault.
	Cancelled bool
}

// TurnCheckpoint is one turn's checkpoint, from the opening snapshot to the row
// that finally describes it.
type TurnCheckpoint interface {
	// ID is the checkpoint's id, known before the snapshot finishes because it
	// is also the requestId the prompt tells the vendor to tag its report with.
	// Empty when no checkpoint is being taken.
	ID() string
	// Wait joins the opening snapshot, killing it if it has run past its
	// budget. Called just before the prompt goes out.
	Wait()
	// Finish takes the closing snapshot and settles the row. Must be called
	// before the session goes idle.
	Finish(ctx context.Context, result TurnResult)
}

// noCheckpoints is what a Service with the feature off uses, so Invoke never
// branches on nil. A nil check at four call sites is four chances to forget one.
type noCheckpoints struct{}

func (noCheckpoints) Enabled() bool                                { return false }
func (noCheckpoints) Baseline(string)                              {}
func (noCheckpoints) AttachReport(string, string, json.RawMessage) {}
func (noCheckpoints) Begin(context.Context, TurnCheckpointSpec) TurnCheckpoint {
	return noTurnCheckpoint{}
}

type noTurnCheckpoint struct{}

func (noTurnCheckpoint) ID() string                         { return "" }
func (noTurnCheckpoint) Wait()                              {}
func (noTurnCheckpoint) Finish(context.Context, TurnResult) {}

// EngineCheckpoints is the real implementation: internal/checkpoint for the
// trees, the store for the rows.
type EngineCheckpoints struct {
	engine *checkpoint.Engine
	store  CheckpointStore
	// Sessions is how a turn finds the sub-sessions that ran inside it. Only
	// ListSessions is needed, so only that is asked for.
	sessions SessionLister
	// OnCheckpoint fires when a checkpoint is settled, so the UI can fill in a
	// turn header that was drawn while the turn was still running.
	OnCheckpoint func(sessionID string, record CheckpointRecord)
	// OnError reports a checkpoint that could not be taken. Never fatal: the
	// turn is what matters.
	OnError func(sessionID string, err error)

	mu sync.Mutex
	// reports holds a vendor report until the turn that asked for it settles.
	reports map[string]json.RawMessage
	// openTurns maps an open turn's checkpoint id to its session. It is what
	// tells a report arriving during a turn from one arriving after it — a late
	// one has to reopen a row that is already written — and what files a report
	// the vendor sent without echoing the requestId back.
	openTurns map[string]string
	baselined map[string]bool
	// problems is why each project has no checkpoints, if it has none.
	problems map[string]string
}

// SessionLister is the slice of Store that attribution needs.
type SessionLister interface {
	ListSessions(ctx context.Context) ([]AgentSession, error)
}

// NewEngineCheckpoints wires the engine to the store.
func NewEngineCheckpoints(engine *checkpoint.Engine, store CheckpointStore, sessions SessionLister) *EngineCheckpoints {
	return &EngineCheckpoints{
		engine:    engine,
		store:     store,
		sessions:  sessions,
		reports:   map[string]json.RawMessage{},
		openTurns: map[string]string{},
		baselined: map[string]bool{},
		problems:  map[string]string{},
	}
}

func (c *EngineCheckpoints) Enabled() bool {
	return c != nil && c.engine != nil && c.engine.Available()
}

// Engine exposes the engine for the paths that need trees directly — a restore,
// a rewind, maintenance.
func (c *EngineCheckpoints) Engine() *checkpoint.Engine { return c.engine }

func (c *EngineCheckpoints) Baseline(projectPath string) {
	if !c.Enabled() || projectPath == "" {
		return
	}
	c.mu.Lock()
	if c.baselined[projectPath] {
		c.mu.Unlock()
		return
	}
	c.baselined[projectPath] = true
	c.mu.Unlock()

	go func() {
		// Its own context: this outlives whatever call triggered it, and a user
		// who closed the session wizard should not cancel the warm-up that
		// makes their first turn fast.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		repo, err := c.engine.Open(ctx, projectPath)
		if err != nil {
			c.fail("", err)
			return
		}
		// A throwaway id: the tree is discarded, the index it leaves behind is
		// the point, and that is what makes every later snapshot cheap.
		if _, err := repo.Snapshot(ctx, "baseline", checkpoint.SlotBefore); err != nil {
			c.fail("", err)
			return
		}
		_ = repo.DeleteRefs(ctx, "baseline")
	}()
}

// AttachReport files a report under the requestId the prompt asked for.
//
// Measured (spike S7): the report arrives on a session_info_update at the same
// moment as the prompt response, which means either order is possible. One that
// lands after the row is written has to go back and change it, because the row
// is what the turn header reads and a file the agent admits to is not the same
// fact as one nothing accounts for.
func (c *EngineCheckpoints) AttachReport(sessionID, requestID string, report json.RawMessage) {
	if len(report) == 0 {
		return
	}
	copied := append(json.RawMessage(nil), report...)
	c.mu.Lock()
	if requestID == "" {
		requestID = c.openTurnOfLocked(sessionID)
	}
	if requestID == "" {
		c.mu.Unlock()
		return
	}
	_, open := c.openTurns[requestID]
	c.reports[requestID] = copied
	c.mu.Unlock()
	if open {
		return
	}
	c.attachLate(requestID, copied)
}

// attachLate re-attributes a settled checkpoint against a report that arrived
// after it. Best-effort: a checkpoint retention has already swept is simply
// gone, and there is nothing to correct.
func (c *EngineCheckpoints) attachLate(requestID string, report json.RawMessage) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		record, err := c.store.GetCheckpoint(ctx, requestID)
		if err != nil {
			c.takeReport(requestID)
			return
		}
		c.takeReport(requestID)
		record.AgentReport = report
		reported := reportedPaths(report)
		for i := range record.Changed {
			if record.Changed[i].Source == SourceUnattributed && reported[record.Changed[i].Path] {
				record.Changed[i].Source = SourceAgent
			}
		}
		if err := c.store.UpdateCheckpoint(ctx, record); err != nil {
			c.fail(record.SessionID, err)
			return
		}
		if c.OnCheckpoint != nil {
			c.OnCheckpoint(record.SessionID, record)
		}
	}()
}

// openTurnOfLocked finds the one turn a session has in flight. Invoke holds a
// per-session claim for the length of a turn, so there is never more than one.
func (c *EngineCheckpoints) openTurnOfLocked(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	for id, owner := range c.openTurns {
		if owner == sessionID {
			return id
		}
	}
	return ""
}

// takeReport removes and returns a held report.
func (c *EngineCheckpoints) takeReport(requestID string) json.RawMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	report := c.reports[requestID]
	delete(c.reports, requestID)
	return report
}

func (c *EngineCheckpoints) fail(sessionID string, err error) {
	if err == nil {
		return
	}
	if c.OnError != nil {
		c.OnError(sessionID, err)
		return
	}
	log.Printf("agenthub: checkpoint %s: %v", sessionID, err)
}

// Maintain runs the engine's retention pass against the store.
//
// Here rather than in main.go because the engine's view of a checkpoint and the
// store's are deliberately different shapes — the engine knows about trees and
// project keys, the store about sessions and turns — and translating between
// them is this package's job, not the composition root's.
func (c *EngineCheckpoints) Maintain(ctx context.Context) (checkpoint.MaintenanceReport, error) {
	if c == nil || c.engine == nil {
		return checkpoint.MaintenanceReport{}, nil
	}
	return c.engine.Maintain(ctx, checkpointRecords{store: c.store})
}

// checkpointRecords presents the store as the narrow view maintenance needs.
type checkpointRecords struct{ store CheckpointStore }

func (r checkpointRecords) ListCheckpoints(ctx context.Context) ([]checkpoint.Record, error) {
	rows, err := r.store.ListAllCheckpoints(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]checkpoint.Record, 0, len(rows))
	for _, row := range rows {
		out = append(out, checkpoint.Record{
			ID:         row.ID,
			SessionID:  row.SessionID,
			ProjectKey: row.ProjectKey,
			Kind:       string(row.Kind),
			CreatedAt:  row.CreatedAt,
		})
	}
	return out, nil
}

func (r checkpointRecords) DeleteCheckpoints(ctx context.Context, ids []string) error {
	return r.store.DeleteCheckpoints(ctx, ids)
}

// CheckpointAvailability is what Settings shows about file checkpoints.
//
// Two separate facts, deliberately not collapsed into one: whether the machine
// can do this, and whether the user wants it. "Off" with no reason reads as
// broken, and "on" on a machine with no git would be a lie.
type CheckpointAvailability struct {
	// Available says a usable git was found.
	Available  bool   `json:"available"`
	GitPath    string `json:"gitPath,omitempty"`
	GitVersion string `json:"gitVersion,omitempty"`
	// Reason explains an unavailable git, in a sentence that names the fix.
	Reason string `json:"reason,omitempty"`
	// Enabled is the user's switch. A machine can be able and switched off.
	Enabled bool `json:"enabled"`
	// Root is where the shadow repositories live, so a user can see what the
	// feature costs them on disk and delete it if they want to.
	Root string `json:"root"`
	// Problems are the projects a checkpoint could not be taken for, with the
	// reason. Without these a project that is simply too large looks identical
	// to the feature quietly not working.
	Problems []CheckpointProblem `json:"problems,omitempty"`
}

// CheckpointProblem is one project that cannot be checkpointed, and why.
type CheckpointProblem struct {
	Project string `json:"project"`
	Reason  string `json:"reason"`
}

// Status reports what Settings needs to show. Safe on a nil engine.
func (c *EngineCheckpoints) Availability(enabled bool) CheckpointAvailability {
	status := CheckpointAvailability{Enabled: enabled}
	if c == nil || c.engine == nil {
		status.Reason = "File checkpoints are not wired in this build."
		return status
	}
	availability := c.engine.Availability()
	status.Available = availability.OK()
	status.GitPath = availability.Path
	status.GitVersion = availability.Version
	status.Reason = availability.Reason
	status.Root = c.engine.Root()

	c.mu.Lock()
	for project, reason := range c.problems {
		status.Problems = append(status.Problems, CheckpointProblem{Project: project, Reason: reason})
	}
	c.mu.Unlock()
	sort.Slice(status.Problems, func(i, j int) bool { return status.Problems[i].Project < status.Problems[j].Project })
	return status
}

// ApplySettings pushes the user's preference into the running engine.
//
// The mapping from a setting to a limit lives here rather than at the call
// sites, because there are two of them — startup and the moment the user
// changes it — and two copies of the same arithmetic is one chance for them to
// disagree about what "10 MiB" means.
func (c *EngineCheckpoints) ApplySettings(maxFileSizeMiB, retentionDays int, extraExcludes []string) {
	if c == nil || c.engine == nil {
		return
	}
	limits := c.engine.Limits()
	limits.MaxFileBytes = int64(maxFileSizeMiB) << 20
	limits.RetentionDays = retentionDays
	limits.ExtraExcludes = append([]string(nil), extraExcludes...)
	c.engine.SetLimits(limits)
}

// noteProblem remembers why a project could not be checkpointed, and forgets it
// again the moment one succeeds. Per project rather than a running log: what a
// user needs is the current state of their project, not its history of
// failures.
func (c *EngineCheckpoints) noteProblem(projectPath string, err error) {
	if projectPath == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.problems == nil {
		c.problems = map[string]string{}
	}
	if err == nil {
		delete(c.problems, projectPath)
		return
	}
	c.problems[projectPath] = err.Error()
}
