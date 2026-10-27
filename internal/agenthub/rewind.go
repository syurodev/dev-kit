// internal/agenthub/rewind.go
package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"devkit/internal/checkpoint"

	"github.com/google/uuid"
)

// Rewinding a session.
//
// A rewind is the only operation in Agent Hub that changes two things at once —
// the conversation the vendor holds and the files on disk — and neither half
// can be rolled back by repeating it. So it is a sequence with one ordering,
// written out here rather than implied by where the calls happen to sit:
//
//	 1. claim the session, re-check the plan
//	 2. close the old runtime and its MCP endpoint — nothing writes after this
//	 3. flush the recorder, then drop its state
//	 4. write the safety row, pending: the log that says a rewind was started
//	 5. new endpoint, fork the vendor session
//	 6. restore the files
//	 7. one transaction: new vendor id, cut transcript, drop turns and
//	    checkpoints, safety row done
//	 8. publish the runtime, re-apply mode and model
//	 9. tell the UI
//	10. best-effort delete of the old vendor session; release the claim
//
// Steps 2 and 3 come before anything is written because a live runtime keeps
// recording: an async task or a subagent still running would append rows to a
// transcript being cut out from under it. Step 4 comes before step 5 because
// from there on a crash leaves the files in an unknown state and the row is the
// only thing that will say so. Step 7 is one transaction because a session
// pointed at a forked conversation whose transcript still holds the dropped
// messages answers out of a history the user cannot see.

// RewindMode is how much of a turn to take back.
type RewindMode string

const (
	// RewindBoth takes back the conversation and the files.
	RewindBoth RewindMode = "both"
	// RewindConversation takes back only what the agent remembers.
	RewindConversation RewindMode = "conversation"
	// RewindCode takes back only the files. Identical to a restore, and
	// answered by the restore path.
	RewindCode RewindMode = "code"
)

// RewindRequest names what to rewind.
type RewindRequest struct {
	SessionID       string     `json:"sessionId"`
	PromptMessageID string     `json:"promptMessageId"`
	Mode            RewindMode `json:"mode"`
}

// RewindPreview is the read-only answer to "what would this rewind do".
type RewindPreview struct {
	Token     string     `json:"token"`
	SessionID string     `json:"sessionId"`
	Mode      RewindMode `json:"mode"`
	// PromptText is the message being rewound, for the composer when the user
	// asked to edit and re-run it.
	PromptText string `json:"promptText"`
	// ExtraTurnsDropped is how many turns beyond the one named also go,
	// because they left no message the vendor can branch at — a turn that
	// failed or was cancelled before the agent replied.
	ExtraTurnsDropped int `json:"extraTurnsDropped"`
	// StartsFresh says there is nothing before this turn to branch from, so the
	// session starts a new conversation rather than a branch of this one.
	StartsFresh bool `json:"startsFresh"`
	// Files is the file half, when the mode includes it. Nil when it does not,
	// or when this session has no checkpoint for the turn.
	Files *RestorePreview `json:"files,omitempty"`
	// Warnings are things to say before the user agrees.
	Warnings []string `json:"warnings"`
}

// RewindResult is what a rewind did.
type RewindResult struct {
	SessionID string `json:"sessionId"`
	// PromptText is the message that was rewound, so the composer can offer it
	// back.
	PromptText string `json:"promptText"`
	// Files is the file half's outcome, when there was one.
	Files *RestoreResult `json:"files,omitempty"`
}

// heldRewindPlan is a preview waiting for the user to agree to it.
type heldRewindPlan struct {
	request RewindRequest
	cut     rewindCut
	// restoreToken is the file half's own plan, held by the restore machinery.
	restoreToken string
	preview      RewindPreview
	madeAt       time.Time
}

type rewindPlans struct {
	mu    sync.Mutex
	plans map[string]heldRewindPlan
}

func (r *rewindPlans) put(token string, plan heldRewindPlan) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.plans == nil {
		r.plans = map[string]heldRewindPlan{}
	}
	now := time.Now()
	for id, held := range r.plans {
		if now.Sub(held.madeAt) > restorePlanTTL {
			delete(r.plans, id)
		}
	}
	r.plans[token] = plan
}

func (r *rewindPlans) take(token string) (heldRewindPlan, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	held, ok := r.plans[token]
	if !ok {
		return heldRewindPlan{}, false
	}
	delete(r.plans, token)
	if time.Since(held.madeAt) > restorePlanTTL {
		return heldRewindPlan{}, false
	}
	return held, true
}

// PlanRewind works out what a rewind would do, without touching anything.
func (s *Service) PlanRewind(ctx context.Context, req RewindRequest) (RewindPreview, error) {
	if err := refuseAgentCaller(ctx); err != nil {
		return RewindPreview{}, err
	}
	if req.Mode == "" {
		req.Mode = RewindBoth
	}
	session, err := s.store.GetSession(ctx, req.SessionID)
	if err != nil {
		return RewindPreview{}, err
	}
	if err := s.restorableNow(session); err != nil {
		return RewindPreview{}, err
	}

	preview := RewindPreview{Token: uuid.NewString(), SessionID: req.SessionID, Mode: req.Mode}
	held := heldRewindPlan{request: req, preview: preview, madeAt: time.Now()}

	if req.Mode != RewindCode {
		adapter, ok := s.reg.Get(session.AgentType)
		if !ok {
			return RewindPreview{}, ErrVendorUnknown
		}
		if _, ok := adapter.(RuntimeForker); !ok {
			// The two halves are independent, so a vendor that cannot branch
			// still allows a file-only rewind. Saying which is unavailable is
			// the difference between "not supported" and "not supported yet".
			return RewindPreview{}, fmt.Errorf("%w: %s cannot rewind a conversation", ErrUnsupported, session.AgentType)
		}
		cut, err := planRewindCut(ctx, s.store, req.SessionID, req.PromptMessageID)
		if err != nil {
			return RewindPreview{}, err
		}
		held.cut = cut
		preview.PromptText = cut.PromptText
		preview.ExtraTurnsDropped = cut.ExtraTurnsDropped
		preview.StartsFresh = cut.ForkMessageID == ""
		if cut.ExtraTurnsDropped > 0 {
			preview.Warnings = append(preview.Warnings, fmt.Sprintf(
				"%d earlier %s also removed: the agent never replied to %s, so there is no point in the conversation to go back to.",
				cut.ExtraTurnsDropped,
				plural(cut.ExtraTurnsDropped, "message is", "messages are"),
				plural(cut.ExtraTurnsDropped, "it", "them")))
		}
		preview.Warnings = append(preview.Warnings,
			"Rewinding a conversation cannot be undone: the old one is deleted from the agent.")
	}

	if req.Mode != RewindConversation {
		files, err := s.PlanRestore(ctx, RestoreRequest{
			SessionID:       req.SessionID,
			Mode:            RestoreToBeforeTurn,
			PromptMessageID: req.PromptMessageID,
		})
		switch {
		case err == nil:
			preview.Files = &files
			held.restoreToken = files.Token
			preview.Warnings = append(preview.Warnings, files.Warnings...)
		case req.Mode == RewindCode:
			// Asked for the files and only the files; a failure there is the
			// whole answer.
			return RewindPreview{}, err
		default:
			// Asked for both, and the file half cannot be done. The
			// conversation half still can, and saying so beats refusing.
			preview.Warnings = append(preview.Warnings,
				"Files will not be restored: "+err.Error())
		}
	}

	held.preview = preview
	s.rewinds.put(preview.Token, held)
	return preview, nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// ApplyRewind carries out a plan, in the order at the top of this file.
func (s *Service) ApplyRewind(ctx context.Context, token string, overwrite []string) (RewindResult, error) {
	if err := refuseAgentCaller(ctx); err != nil {
		return RewindResult{}, err
	}
	held, ok := s.rewinds.take(token)
	if !ok {
		return RewindResult{}, ErrRestorePlanStale
	}
	// A rewind restores files and starts a forked runtime on the session's
	// worktree: both are writer actions, so the managed-writer fence applies
	// before either, and a release that holds the barrier refuses it.
	leaveAdmission, err := s.enterWriterAdmission(ctx, held.request.SessionID)
	if err != nil {
		return RewindResult{}, err
	}
	defer leaveAdmission()
	// Code only is a restore wearing a different name; there is no reason for
	// it to take the conversation apart to do a thing the restore path already
	// does correctly.
	if held.request.Mode == RewindCode {
		files, err := s.ApplyRestore(ctx, held.restoreToken, overwrite)
		if err != nil {
			return RewindResult{}, err
		}
		return RewindResult{SessionID: held.request.SessionID, PromptText: held.preview.PromptText, Files: &files}, nil
	}

	session, err := s.store.GetSession(ctx, held.request.SessionID)
	if err != nil {
		return RewindResult{}, err
	}
	if err := s.restorableNow(session); err != nil {
		return RewindResult{}, err
	}
	adapter, ok := s.reg.Get(session.AgentType)
	if !ok {
		return RewindResult{}, ErrVendorUnknown
	}
	forker, ok := adapter.(RuntimeForker)
	if !ok {
		return RewindResult{}, ErrUnsupported
	}

	// 1. Claim the session. Everything below assumes nothing else is prompting.
	s.mu.Lock()
	if _, busy := s.invoking[session.ID]; busy {
		s.mu.Unlock()
		return RewindResult{}, ErrSessionBusy
	}
	s.invoking[session.ID] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.invoking, session.ID)
		s.mu.Unlock()
	}()

	rewinder := &rewindRun{service: s, session: session, held: held, forker: forker, overwrite: overwrite}
	return rewinder.run(ctx)
}

// rewindRun carries the state of one rewind through its steps, so each step can
// read what the ones before it produced without a chain of parameters.
type rewindRun struct {
	service   *Service
	session   AgentSession
	held      heldRewindPlan
	forker    RuntimeForker
	overwrite []string

	usageFacts RuntimeUsageFacts
	launch     harnessLaunch
	oldRuntime SessionRuntime
	newRuntime SessionRuntime
	closeNew   func()
	safetyID   string
}

func (r *rewindRun) run(ctx context.Context) (RewindResult, error) {
	s := r.service

	// 2. Close the old runtime and its endpoint. An async task or a subagent
	// still running would keep appending rows to a transcript about to be cut.
	s.mu.Lock()
	r.oldRuntime = s.runtimes[r.session.ID]
	delete(s.runtimes, r.session.ID)
	delete(s.runtimeUsage, r.session.ID)
	// The conversation is about to be cut back: the block may be in the part
	// that goes.
	s.forgetWorkspaceContextLocked(r.session.ID)
	closeOldEndpoint := s.endpoints[r.session.ID]
	delete(s.endpoints, r.session.ID)
	s.mu.Unlock()

	// 3. Land what the recorder is holding, then forget it. Flushing after the
	// cut would write rows below a transcript that no longer has room for them.
	if err := s.recorder.Flush(ctx, r.session.ID); err != nil && s.OnRecordError != nil {
		s.OnRecordError(r.session.ID, err)
	}
	if r.oldRuntime != nil {
		if err := r.oldRuntime.Close(ctx); err != nil {
			log.Printf("agenthub: rewind %s: closing the old runtime: %v", r.session.ID, err)
		}
	}
	if closeOldEndpoint != nil {
		closeOldEndpoint()
	}
	s.recorder.Drop(r.session.ID)

	// 4. The safety row, pending. From here a crash leaves the files in an
	// unknown state, and this row is the only thing that will say so.
	if r.held.restoreToken != "" {
		if err := r.writeSafetyRow(ctx); err != nil {
			return RewindResult{}, err
		}
	}

	// 5. A new endpoint and the fork.
	if err := r.openForked(ctx); err != nil {
		r.abandonSafety(ctx, err)
		return RewindResult{}, err
	}
	// 6. The files.
	var files *RestoreResult
	if r.held.restoreToken != "" {
		applied, err := s.applyRestoreHeld(ctx, r.held.restoreToken, r.session, r.overwrite)
		if err != nil {
			// Nothing has been committed: close the fork and delete it, so the
			// vendor is not left holding a session nothing points at.
			r.discardFork(ctx)
			r.abandonSafety(ctx, err)
			return RewindResult{}, err
		}
		files = &applied
	}

	// 7. The one write. Everything or nothing.
	spec := RewindSpec{
		SessionID:               r.session.ID,
		ACPSessionID:            r.newRuntime.ACPSessionID(),
		CutSeq:                  r.held.cut.CutSeq,
		DroppedPromptMessageIDs: r.held.cut.DroppedPrompts,
		SafetyCheckpointID:      r.safetyID,
		At:                      time.Now().UTC(),
	}
	if r.launch.snapshot != nil {
		ref := r.launch.snapshot.Ref.Clone()
		spec.HarnessRef, spec.HarnessEnforced = &ref, r.launch.enforced
	}
	// Keep the rewind transaction (including its applied reference) and runtime
	// publication under the same epoch/Close boundary as create and resume.
	if err := s.publishServingLaunch(r.session.ID, r.launch, func() error {
		if err := s.store.RewindSession(ctx, spec); err != nil {
			return err
		}
		s.mu.Lock()
		s.runtimes[r.session.ID] = r.newRuntime
		s.runtimeUsage[r.session.ID] = r.usageFacts
		s.publishHarnessLocked(r.session.ID, r.launch)
		s.sessionAccount[r.session.ID] = r.usageFacts.AccountID
		s.endpoints[r.session.ID] = r.closeNew
		s.forgetWorkspaceContextLocked(r.session.ID)
		s.mu.Unlock()
		return nil
	}); err != nil {
		r.discardFork(ctx)
		r.abandonSafety(ctx, err)
		return RewindResult{}, err
	}

	// 8. Vendor settings and callbacks run outside the coordinator. The cut and
	// reference committed atomically; a crash still leaves the restart guard.

	s.attachUpdates(r.session.ID, r.newRuntime)
	s.reapplySessionSettings(ctx, r.session.ID, r.newRuntime)

	// 9. Tell the UI. Its transcript is now wrong in a way no event it already
	// listens to would explain.
	if s.OnSessionRewound != nil {
		s.OnSessionRewound(r.session.ID, r.held.preview.PromptText)
	}

	// 10. The old vendor session is unreachable now. Best effort: a vendor that
	// refuses is holding a file, not blocking the user.
	r.deleteOldVendorSession(ctx)

	return RewindResult{
		SessionID:  r.session.ID,
		PromptText: r.held.preview.PromptText,
		Files:      files,
	}, nil
}

func (r *rewindRun) writeSafetyRow(ctx context.Context) error {
	s := r.service
	engine, ok := s.currentCheckpoints().(*EngineCheckpoints)
	if !ok || !engine.Enabled() {
		return nil
	}
	key, _, err := checkpoint.ProjectKey(r.session.ProjectPath)
	if err != nil {
		return err
	}
	repo, err := engine.engine.Open(ctx, r.session.ProjectPath)
	if err != nil {
		return err
	}
	id := uuid.NewString()
	before, err := repo.Snapshot(ctx, id, checkpoint.SlotBefore)
	if err != nil {
		return fmt.Errorf("agenthub: refusing to rewind without a way back: %w", err)
	}
	r.safetyID = id
	return s.store.InsertCheckpoint(ctx, CheckpointRecord{
		ID:              id,
		SessionID:       r.session.ID,
		ProjectKey:      key,
		PromptMessageID: r.held.request.PromptMessageID,
		Kind:            CheckpointRewindSafety,
		BeforeTree:      before.Tree,
		// Pending until the cut commits. A row still pending after a restart is
		// a rewind that died part-way, and the banner that offers to put the
		// files back reads exactly this.
		Status:    CheckpointPending,
		Outcome:   CheckpointOutcomeOK,
		CreatedAt: time.Now().UTC(),
	})
}

// abandonSafety marks the safety row failed when the rewind gives up before the
// cut. The snapshot stays usable — a rewind that failed half-way is when a way
// back matters most — but the row stops claiming to be in flight.
func (r *rewindRun) abandonSafety(ctx context.Context, cause error) {
	if r.safetyID == "" {
		return
	}
	record, err := r.service.store.GetCheckpoint(ctx, r.safetyID)
	if err != nil {
		return
	}
	finishedAt := time.Now().UTC()
	record.Status = CheckpointFailed
	record.FinishedAt = &finishedAt
	record.Detail = reasonDetail(cause)
	if err := r.service.store.UpdateCheckpoint(ctx, record); err != nil && r.service.OnRecordError != nil {
		r.service.OnRecordError(r.session.ID, err)
	}
}

func (r *rewindRun) openForked(ctx context.Context) error {
	s := r.service
	spec := sessionOpenSpec(r.session)
	spec.SessionTempRoot = s.SessionTempRoot
	previous, err := s.previousHarness(ctx, r.session.ID)
	if err != nil {
		return err
	}
	launch, err := s.prepareLaunch(ctx, &spec, r.session.ID, r.session.WorkspaceID, r.session.AgentType, previous)
	if err != nil {
		return err
	}
	r.launch = launch
	r.closeNew = launch.closeEndpoint
	spec.TempOwner = s.SessionTempOwner
	if err := prepareSessionTemp(&spec, false); err != nil {
		r.closeNew()
		return err
	}
	s.applySandboxExtras(&spec)
	s.applyHarnessLaunch(&spec, launch, r.session.AgentType)
	resolvedAccount := s.fillAccountSpec(ctx, &spec, r.session)
	s.recordSessionAccount(r.session.ID, resolvedAccount)
	r.usageFacts = factsForSpec(r.session, spec, resolvedAccount)
	s.applySubagents(&spec, r.session.AgentType)

	// Nothing before this turn to branch from: the session starts a fresh
	// conversation rather than a branch of one.
	if r.held.cut.ForkMessageID == "" {
		adapter, _ := s.reg.Get(r.session.AgentType)
		rt, err := s.openTracked(r.session.ID, func() (SessionRuntime, error) { return adapter.Open(ctx, spec) })
		if err != nil {
			r.closeNew()
			return err
		}
		r.newRuntime = rt
		return nil
	}

	// The vendor decides what it can branch at. Measurement says the last
	// assistant message works; when it does not, the candidates before it are
	// tried in turn rather than the rewind failing on the vendor's first no.
	var lastErr error
	for _, messageID := range append([]string{r.held.cut.ForkMessageID}, r.held.cut.Fallbacks...) {
		rt, err := s.openTracked(r.session.ID, func() (SessionRuntime, error) {
			return r.forker.Fork(ctx, spec, r.session.ACPSessionID, messageID)
		})
		if err == nil {
			r.newRuntime = rt
			return nil
		}
		lastErr = err
		if errors.Is(err, ErrAgentAuthRequired) || errors.Is(err, ErrAgentNotInstalled) {
			// Not a refusal of this branch point; trying an earlier one would
			// fail the same way and spawn a process each time.
			break
		}
		log.Printf("agenthub: rewind %s: fork at %s refused: %v", r.session.ID, messageID, err)
	}
	r.closeNew()
	return lastErr
}

// discardFork closes a fork the rewind is not going to use and asks the vendor
// to forget it, so a failed rewind does not leave a session nothing points at.
func (r *rewindRun) discardFork(ctx context.Context) {
	if r.newRuntime == nil {
		return
	}
	if deleter, ok := r.newRuntime.(RuntimeDeleter); ok {
		if err := deleter.Delete(ctx); err != nil {
			log.Printf("agenthub: rewind %s: deleting the abandoned fork: %v", r.session.ID, err)
		}
	}
	if err := r.newRuntime.Close(ctx); err != nil {
		log.Printf("agenthub: rewind %s: closing the abandoned fork: %v", r.session.ID, err)
	}
	if r.closeNew != nil {
		r.closeNew()
	}
	r.newRuntime = nil
}

// deleteOldVendorSession drops the conversation the rewind left behind.
//
// Best effort, and last: the rewind is already committed, so a vendor that
// refuses has left a file on disk rather than a session anyone can reach.
// Measured (spike S6): session/delete works on a session this process never
// loaded, which is what makes doing it from the forked runtime possible at all.
func (r *rewindRun) deleteOldVendorSession(ctx context.Context) {
	if r.session.ACPSessionID == "" {
		return
	}
	deleter, ok := r.newRuntime.(VendorSessionDeleter)
	if !ok {
		return
	}
	if err := deleter.DeleteSession(ctx, r.session.ACPSessionID); err != nil {
		log.Printf("agenthub: rewind %s: the old vendor session %s could not be deleted: %v",
			r.session.ID, r.session.ACPSessionID, err)
	}
}

// storedUpdate reads a transcript row back into the update it was written from.
func storedUpdate(entry TranscriptEntry) SessionUpdate {
	var update SessionUpdate
	if len(entry.Payload) == 0 {
		return update
	}
	if err := json.Unmarshal(entry.Payload, &update); err != nil {
		return SessionUpdate{}
	}
	return update
}

// configOptionValue reads a stored option back as the string the setter takes.
// The vendor sends the current value as arbitrary JSON; only a scalar can be
// sent back, and anything else is left as it is rather than guessed at.
func configOptionValue(option ConfigOption) string {
	if len(option.CurrentValue) == 0 {
		return ""
	}
	var value any
	if err := json.Unmarshal(option.CurrentValue, &value); err != nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	default:
		return ""
	}
}

// reapplySessionSettings puts the mode and the config options back on a freshly
// forked runtime.
//
// Measured (spike S2): a forked session comes up in the vendor's default mode,
// whatever the source was set to. Without this a rewind silently moves a
// session out of plan mode, or back to a model the user did not choose — a
// change they never asked for, made by an operation about something else.
func (s *Service) reapplySessionSettings(ctx context.Context, sessionID string, rt SessionRuntime) {
	configurator, ok := rt.(RuntimeConfigurator)
	if !ok {
		return
	}
	// The stored payload is the normalized SessionUpdate, not the wire frame
	// the agent sent: the recorder marshals what it parsed. Running it back
	// through ParseSessionUpdate reads it as an unknown kind with no fields,
	// which is a silent no-op rather than an error — so it is unmarshalled
	// directly, the way recordSessionState reads its own rows.
	if entry, found, err := s.store.FindTranscriptKind(ctx, sessionID, UpdateCurrentMode); err == nil && found {
		if update := storedUpdate(entry); update.Mode != "" {
			if err := configurator.SetMode(ctx, update.Mode); err != nil {
				log.Printf("agenthub: rewind %s: re-applying mode %s: %v", sessionID, update.Mode, err)
			}
		}
	}
	if entry, found, err := s.store.FindTranscriptKind(ctx, sessionID, UpdateConfigOptions); err == nil && found {
		update := storedUpdate(entry)
		for _, option := range update.ConfigOptions {
			value := configOptionValue(option)
			if option.ID == "" || value == "" {
				continue
			}
			if err := configurator.SetConfigOption(ctx, option.ID, value); err != nil {
				log.Printf("agenthub: rewind %s: re-applying %s=%s: %v", sessionID, option.ID, value, err)
			}
		}
	}
}

// InterruptedRewinds returns the rewinds that never finished: a safety row
// still pending when the app starts is one whose process died between taking
// the snapshot and committing the cut.
//
// Nothing is reverted automatically. The files may be half-restored, half-cut
// or untouched depending on where it stopped, and only the person can say which
// of those they want — so this reports, and the banner it feeds offers.
func (s *Service) InterruptedRewinds(ctx context.Context) ([]CheckpointSummary, error) {
	records, err := s.store.ListInterruptedRewinds(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]CheckpointSummary, 0, len(records))
	for _, record := range records {
		out = append(out, record.Summary())
	}
	return out, nil
}

// DismissInterruptedRewind marks a pending safety row done without touching any
// files. The user has looked at the banner and said the project is fine.
func (s *Service) DismissInterruptedRewind(ctx context.Context, checkpointID string) error {
	if err := refuseAgentCaller(ctx); err != nil {
		return err
	}
	record, err := s.store.GetCheckpoint(ctx, checkpointID)
	if err != nil {
		return err
	}
	if record.Kind != CheckpointRewindSafety || record.Status != CheckpointPending {
		return ErrCheckpointNotFound
	}
	finishedAt := time.Now().UTC()
	record.Status = CheckpointDone
	record.FinishedAt = &finishedAt
	return s.store.UpdateCheckpoint(ctx, record)
}
