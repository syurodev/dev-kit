// internal/agenthub/checkpoint_restore.go
package agenthub

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"devkit/internal/checkpoint"
	"devkit/internal/identity"

	"github.com/google/uuid"
)

// Putting a project's files back.
//
// Two shapes, and they differ only in which paths are in scope:
//
//	undo      the paths this turn changed, back to how they were before it
//	toBefore  every path any turn since has changed, back to before this one
//
// Both aim at the same tree — the opening snapshot of the named turn — and both
// check against the same expectation: the newest full tree DevKit holds for
// this project, which carries every path's last known content. Anything that
// has moved since is a conflict, skipped unless the user says otherwise.
//
// This is the user's to do, never the agent's. It is not an MCP tool and the
// call refuses an agent caller outright: an agent that could undo its own turns
// could hide them, and the point of the feature is that the person can see and
// reverse what happened.

// RestoreMode is which of the three restores is being asked for.
type RestoreMode string

const (
	// RestoreUndoTurn reverses one turn, leaving later turns alone.
	RestoreUndoTurn RestoreMode = "undo"
	// RestoreToBeforeTurn rewinds the project's files to before a turn,
	// reversing everything since.
	RestoreToBeforeTurn RestoreMode = "toBefore"
	// RestoreUndoRestore puts back what a restore itself changed, from the
	// safety snapshot it took first.
	RestoreUndoRestore RestoreMode = "undoRestore"
)

// RestoreRequest names what to restore.
type RestoreRequest struct {
	SessionID string      `json:"sessionId"`
	Mode      RestoreMode `json:"mode"`
	// PromptMessageID names the turn, for undo and toBefore.
	PromptMessageID string `json:"promptMessageId,omitempty"`
	// CheckpointID names the safety row, for undoRestore.
	CheckpointID string `json:"checkpointId,omitempty"`
}

// RestoreAction is one path a restore would change.
type RestoreAction struct {
	Path string `json:"path"`
	// Kind is "write" or "delete".
	Kind string `json:"kind"`
}

// RestoreConflict is one path a restore will not change, and why.
type RestoreConflict struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
	// Overridable says the user may force past it. False for a path no tree
	// holds the content of, and for one whose directory is now a symlink.
	Overridable bool `json:"overridable"`
}

// RestorePreview is the read-only answer to "what would this restore do".
type RestorePreview struct {
	// Token is what ApplyRestore takes. Opaque, single-use, and checked again
	// at apply time: the dialog the user read described a particular state of
	// the project, and a plan that went stale must not be carried out.
	Token     string            `json:"token"`
	SessionID string            `json:"sessionId"`
	Mode      RestoreMode       `json:"mode"`
	Actions   []RestoreAction   `json:"actions"`
	Conflicts []RestoreConflict `json:"conflicts"`
	// Warnings are things the user should know before agreeing: another
	// session writing in the same project, a turn recorded on one side only.
	Warnings []string `json:"warnings"`
}

// Empty reports whether the restore would change nothing.
func (p RestorePreview) Empty() bool { return len(p.Actions) == 0 }

// RestoreResult is what a restore actually did.
type RestoreResult struct {
	Written []string `json:"written"`
	Deleted []string `json:"deleted"`
	// SafetyCheckpointID is the snapshot taken before the restore, which is
	// what "Undo restore" goes back to.
	SafetyCheckpointID string `json:"safetyCheckpointId"`
}

// restorePlanTTL bounds how long a preview stays usable. Long enough for a
// dialog, short enough that a plan left open overnight is re-made against a
// project that has moved on.
const restorePlanTTL = 15 * time.Minute

// heldRestorePlan is a preview waiting for the user to agree to it.
type heldRestorePlan struct {
	request    RestoreRequest
	projectID  string
	enginePlan checkpoint.RestorePlan
	preview    RestorePreview
	madeAt     time.Time
}

// restorePlans holds previews between PlanRestore and ApplyRestore. Server-side
// because the token must carry no authority of its own: a client that could
// hand back a tree hash could name any tree.
type restorePlans struct {
	mu    sync.Mutex
	plans map[string]heldRestorePlan
}

func (r *restorePlans) put(token string, plan heldRestorePlan) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.plans == nil {
		r.plans = map[string]heldRestorePlan{}
	}
	now := time.Now()
	for id, held := range r.plans {
		if now.Sub(held.madeAt) > restorePlanTTL {
			delete(r.plans, id)
		}
	}
	r.plans[token] = plan
}

// take removes and returns a plan. Single use: a second Apply on one token is a
// double-click, and a restore is not something to do twice.
func (r *restorePlans) take(token string) (heldRestorePlan, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	held, ok := r.plans[token]
	if !ok {
		return heldRestorePlan{}, false
	}
	delete(r.plans, token)
	if time.Since(held.madeAt) > restorePlanTTL {
		return heldRestorePlan{}, false
	}
	return held, true
}

// PlanRestore works out what a restore would do, without touching anything.
func (s *Service) PlanRestore(ctx context.Context, req RestoreRequest) (RestorePreview, error) {
	if err := refuseAgentCaller(ctx); err != nil {
		return RestorePreview{}, err
	}
	session, err := s.store.GetSession(ctx, req.SessionID)
	if err != nil {
		return RestorePreview{}, err
	}
	if err := s.restorableNow(session); err != nil {
		return RestorePreview{}, err
	}

	engine, ok := s.currentCheckpoints().(*EngineCheckpoints)
	if !ok || !engine.Enabled() {
		return RestorePreview{}, ErrUnavailable
	}
	target, scope, protected, warnings, err := s.restoreScope(ctx, req)
	if err != nil {
		return RestorePreview{}, err
	}
	// What DevKit last saw of the whole project. One row, not the session's
	// history: a snapshot is a full tree rather than a delta, so the newest one
	// already carries every path's last known content.
	expected, found, err := s.store.NewestCheckpointTree(ctx, req.SessionID)
	if err != nil {
		return RestorePreview{}, err
	}
	if !found {
		warnings = append(warnings,
			"DevKit has no complete snapshot of this project, so every file will look changed.")
	}

	repo, err := engine.engine.Open(ctx, session.ProjectPath)
	if err != nil {
		return RestorePreview{}, err
	}
	enginePlan, err := repo.PlanRestore(ctx, target, expected, scope, protected)
	if err != nil {
		return RestorePreview{}, err
	}

	// Anything else writing in this project right now could be undone by this,
	// or could undo it. Named rather than refused: it is the user's project and
	// their call, but not one to make without being told.
	projectKey, _, err := checkpoint.ProjectKey(session.ProjectPath)
	if err != nil {
		return RestorePreview{}, err
	}
	for _, span := range engine.engine.Activity().RunningIn(projectKey, req.SessionID) {
		warnings = append(warnings, fmt.Sprintf(
			"Another session is running in this project right now (%s). Its changes may be overwritten.", span.Session))
	}

	preview := RestorePreview{
		Token:     uuid.NewString(),
		SessionID: req.SessionID,
		Mode:      req.Mode,
		Actions:   make([]RestoreAction, 0, len(enginePlan.Actions)),
		Conflicts: make([]RestoreConflict, 0, len(enginePlan.Conflicts)),
		Warnings:  warnings,
	}
	for _, action := range enginePlan.Actions {
		preview.Actions = append(preview.Actions, RestoreAction{Path: action.Path, Kind: string(action.Kind)})
	}
	for _, conflict := range enginePlan.Conflicts {
		preview.Conflicts = append(preview.Conflicts, RestoreConflict{
			Path:        conflict.Path,
			Kind:        string(conflict.Kind),
			Reason:      string(conflict.Reason),
			Overridable: conflict.Reason.Overridable(),
		})
	}
	s.restores.put(preview.Token, heldRestorePlan{
		request:    req,
		projectID:  projectKey,
		enginePlan: enginePlan,
		preview:    preview,
		madeAt:     time.Now(),
	})
	return preview, nil
}

// ApplyRestore carries out a plan.
//
// overwrite names the conflicts the user chose to force; only the overridable
// ones are honoured. A safety snapshot is always taken first, whatever the
// plan, so the restore itself is never the one change that cannot be undone.
func (s *Service) ApplyRestore(ctx context.Context, token string, overwrite []string) (RestoreResult, error) {
	if err := refuseAgentCaller(ctx); err != nil {
		return RestoreResult{}, err
	}
	held, ok := s.restores.take(token)
	if !ok {
		return RestoreResult{}, ErrRestorePlanStale
	}
	session, err := s.store.GetSession(ctx, held.request.SessionID)
	if err != nil {
		return RestoreResult{}, err
	}
	if err := s.restorableNow(session); err != nil {
		return RestoreResult{}, err
	}

	// Hold the session for the length of the restore, the same claim a turn
	// takes. Without it a prompt could start while files are being written and
	// the turn's opening snapshot would catch the restore half-done.
	s.mu.Lock()
	if _, busy := s.invoking[session.ID]; busy {
		s.mu.Unlock()
		return RestoreResult{}, ErrSessionBusy
	}
	s.invoking[session.ID] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.invoking, session.ID)
		s.mu.Unlock()
	}()

	return s.applyRestoreClaimed(ctx, held, session, overwrite)
}

// applyRestoreHeld runs a restore whose plan is already in hand and whose
// session is already claimed — what a rewind needs at its file step. Split out
// rather than re-entering ApplyRestore, which would deadlock on the claim the
// rewind is holding, and re-checking it there would be a lie: the whole point
// of the rewind's claim is that nothing else may touch this session.
func (s *Service) applyRestoreHeld(ctx context.Context, token string, session AgentSession, overwrite []string) (RestoreResult, error) {
	held, ok := s.restores.take(token)
	if !ok {
		return RestoreResult{}, ErrRestorePlanStale
	}
	return s.applyRestoreClaimed(ctx, held, session, overwrite)
}

// applyRestoreClaimed is the restore itself. Callers have already authorised it
// and taken the session's claim.
func (s *Service) applyRestoreClaimed(ctx context.Context, held heldRestorePlan, session AgentSession, overwrite []string) (RestoreResult, error) {
	engine, ok := s.currentCheckpoints().(*EngineCheckpoints)
	if !ok || !engine.Enabled() {
		return RestoreResult{}, ErrUnavailable
	}
	repo, err := engine.engine.Open(ctx, session.ProjectPath)
	if err != nil {
		return RestoreResult{}, err
	}
	activity := engine.engine.Activity().Begin(held.projectID, session.ID, checkpoint.KindRestore)
	defer activity.End()

	// The safety snapshot, before anything is written. Its id is the row, and
	// the row is what "Undo restore" reads.
	safetyID := uuid.NewString()
	safetyBefore, err := repo.Snapshot(ctx, safetyID, checkpoint.SlotBefore)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("checkpoint: refusing to restore without a way back: %w", err)
	}
	safety := CheckpointRecord{
		ID:              safetyID,
		SessionID:       session.ID,
		ProjectKey:      held.projectID,
		PromptMessageID: held.request.PromptMessageID,
		Kind:            CheckpointRestoreSafety,
		BeforeTree:      safetyBefore.Tree,
		Status:          CheckpointPending,
		Outcome:         CheckpointOutcomeOK,
		CreatedAt:       time.Now().UTC(),
	}
	if err := s.store.InsertCheckpoint(ctx, safety); err != nil {
		return RestoreResult{}, err
	}

	applied, applyErr := repo.ApplyRestore(ctx, held.enginePlan, overwrite)

	// The row is settled either way. A restore that failed part-way is exactly
	// when a way back matters, so the snapshot has to stay usable rather than
	// be rolled back with the operation.
	safetyAfter, snapErr := repo.Snapshot(ctx, safetyID, checkpoint.SlotAfter)
	if snapErr == nil {
		safety.AfterTree = safetyAfter.Tree
	} else {
		safety.Outcome = CheckpointOutcomeAfterMissing
		safety.Detail = reasonDetail(snapErr)
	}
	finishedAt := time.Now().UTC()
	safety.FinishedAt = &finishedAt
	safety.Status = CheckpointDone
	if applyErr != nil {
		safety.Status = CheckpointFailed
		safety.Detail = reasonDetail(applyErr)
	}
	if err := s.store.UpdateCheckpoint(ctx, safety); err != nil && s.OnRecordError != nil {
		s.OnRecordError(session.ID, err)
	}
	if applyErr != nil {
		return RestoreResult{SafetyCheckpointID: safetyID}, applyErr
	}

	result := RestoreResult{
		Written:            applied.Written,
		Deleted:            applied.Deleted,
		SafetyCheckpointID: safetyID,
	}
	if s.OnFilesRestored != nil {
		s.OnFilesRestored(session.ID, result)
	}
	return result, nil
}

// refuseAgentCaller keeps the restore in the person's hands.
func refuseAgentCaller(ctx context.Context) error {
	if caller, _ := identity.FromContext(ctx); caller.IsAgent() {
		return ErrRestoreNotPermitted
	}
	return nil
}

// restorableNow refuses a session that is mid-turn. Files change under a
// running agent, and a restore during one would race its writes.
func (s *Service) restorableNow(session AgentSession) error {
	switch session.Status {
	case SessionStatusRunning, SessionStatusAwaitingPermission:
		return ErrSessionBusy
	}
	s.mu.Lock()
	_, busy := s.invoking[session.ID]
	s.mu.Unlock()
	if busy {
		return ErrSessionBusy
	}
	return nil
}

// restoreScope works out the target tree, which paths are in scope, which are
// off limits, and what the user should be told.
//
// The reads are bounded by what is being reversed rather than by how long the
// session has run: the safety row is fetched by id, and "this turn and every
// one after it" is a query from that turn's own moment.
func (s *Service) restoreScope(ctx context.Context, req RestoreRequest) (target string, scope, protected []string, warnings []string, err error) {
	if req.Mode == RestoreUndoRestore {
		safety, err := s.store.GetCheckpoint(ctx, req.CheckpointID)
		if err != nil {
			return "", nil, nil, nil, err
		}
		if safety.SessionID != req.SessionID || !safety.IsSafety() {
			return "", nil, nil, nil, ErrCheckpointNotFound
		}
		if safety.BeforeTree == "" {
			return "", nil, nil, nil, fmt.Errorf("%w: the safety snapshot is missing", ErrUnavailable)
		}
		// Every path, because a restore's own scope is already the narrow one:
		// going back to the safety tree undoes exactly what it wrote.
		return safety.BeforeTree, nil, skippedPaths(safety), nil, nil
	}

	turn, err := s.findTurnCheckpoint(ctx, req.SessionID, req.PromptMessageID)
	if err != nil {
		return "", nil, nil, nil, err
	}
	if turn.BeforeTree == "" {
		// Nothing to go back to. Refused rather than approximated: a restore
		// against a tree that does not describe this turn's starting point
		// would delete files it never created.
		return "", nil, nil, nil, fmt.Errorf("%w: this turn has no opening snapshot", ErrUnavailable)
	}
	target = turn.BeforeTree

	switch req.Mode {
	case RestoreUndoTurn:
		scope = changedPathsOf(turn)
		protected = skippedPaths(turn)
	case RestoreToBeforeTurn:
		// This turn and every one after it: the union of what they changed is
		// what has to move to put the project back to before this turn.
		later, err := s.store.ListCheckpointsFrom(ctx, req.SessionID, turn.CreatedAt)
		if err != nil {
			return "", nil, nil, nil, err
		}
		seen := map[string]bool{}
		for _, record := range later {
			if record.Kind != CheckpointTurn {
				continue
			}
			for _, path := range changedPathsOf(record) {
				if !seen[path] {
					seen[path] = true
					scope = append(scope, path)
				}
			}
			protected = append(protected, skippedPaths(record)...)
			if record.Outcome != CheckpointOutcomeOK {
				warnings = append(warnings, fmt.Sprintf(
					"One of the turns being reversed was only partly recorded (%s), so some of its changes may remain.",
					record.Outcome))
			}
		}
		sort.Strings(scope)
	default:
		return "", nil, nil, nil, fmt.Errorf("%w: unknown restore mode %q", ErrUnsupported, req.Mode)
	}
	if turn.Outcome == CheckpointOutcomeBeforeMissing || turn.Outcome == CheckpointOutcomeAfterMissing {
		warnings = append(warnings, partialTurnWarning(turn.Outcome))
	}
	return target, scope, protected, warnings, nil
}

// findTurnCheckpoint looks one turn up by the prompt that started it.
func (s *Service) findTurnCheckpoint(ctx context.Context, sessionID, promptMessageID string) (CheckpointRecord, error) {
	records, err := s.store.ListCheckpoints(ctx, sessionID)
	if err != nil {
		return CheckpointRecord{}, err
	}
	turn, ok := CheckpointForPrompt(records, promptMessageID)
	if !ok {
		return CheckpointRecord{}, ErrCheckpointNotFound
	}
	return turn, nil
}

func partialTurnWarning(outcome string) string {
	if outcome == CheckpointOutcomeAfterMissing {
		return "This turn's closing snapshot did not finish, so what it changed is only partly known."
	}
	return "This turn's opening snapshot did not finish, so it cannot be fully undone."
}

func changedPathsOf(record CheckpointRecord) []string {
	out := make([]string, 0, len(record.Changed))
	for _, file := range record.Changed {
		out = append(out, file.Path)
	}
	return out
}

// skippedPaths are the paths a snapshot left out, which a restore must never
// touch: no tree holds their content, so acting on one would either delete a
// file nobody checkpointed or write a stale version of it.
func skippedPaths(record CheckpointRecord) []string {
	if len(record.Skipped) == 0 {
		return nil
	}
	var skips []checkpoint.Skip
	if err := json.Unmarshal(record.Skipped, &skips); err != nil {
		return nil
	}
	out := make([]string, 0, len(skips))
	for _, skip := range skips {
		out = append(out, skip.Path)
	}
	return out
}

// IsSafety reports whether this record is the snapshot a restore or a rewind
// took before it acted.
func (c CheckpointRecord) IsSafety() bool {
	return c.Kind == CheckpointRestoreSafety || c.Kind == CheckpointRewindSafety
}
