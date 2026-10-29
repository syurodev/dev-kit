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
//	 6. restore the files, once
//	 7. one transaction: new vendor id, cut transcript, drop turns and
//	    checkpoints, safety row done, applied reference
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
//
// Files and database are not one transaction, and this is not pretending they
// are. Once step 6 has changed the files nothing automatic puts them back, so
// step 7 is finished forward instead of given up:
//
//   - The files are restored once and the safety row is written once. A retry
//     never restores or snapshots again.
//   - Step 7 refused only because S moved (the coordinator's reason, read at the
//     refused publication) is finished forward: the abandoned fork and its endpoint
//     are discarded, the same rewind target is prepared and forked again from the
//     same source conversation by the ordinary rules (the launch gate included), and
//     step 7 is attempted again. The preparations of the whole rewind, the first
//     one included, share one budget of launchAttempts; vendor fallback candidates
//     are a separate, finite loop inside each preparation's fork.
//   - Every other refusal ends the rewind: a Close (in progress, failed or
//     finished), a lost or replaced endpoint, a deleted session, a cancelled
//     caller, a gate refusal, a fork or store failure. Nothing here is retried as
//     if S had moved.
//   - A step 7 that failed while committing is ambiguous. The durable facts (the
//     session's vendor id, the safety row) are read back first: a cut that did
//     commit is published and finished as a success; one that did not is a proven
//     non-commit; one that cannot be read back deletes nothing and is reported
//     as unknown.
//
// After the files changed, a rewind that does not finish says so: RewindPartialError
// when the conversation was proven not rewound, RewindUnknownError when that could
// not be proven. Both keep the safety snapshot; neither is ever retried by the
// host. The cleanup they need runs on a bounded context of the host's own, so a
// caller that has gone away cannot strand it.

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

	// Before the live runtime is touched, as a reload does: a fork the launch gate
	// would refuse (a new workspace executable, an unresolvable snapshot) must not
	// be discovered after step 2 has closed the running session, which a reopen
	// would then be refused by the same gate. The launch repeats the check.
	if err := s.preflightHarness(ctx, session); err != nil {
		return RewindResult{}, err
	}

	rewinder := &rewindRun{service: s, session: session, held: held, forker: forker, overwrite: overwrite, launchBudget: launchAttempts}
	return rewinder.run(ctx)
}

// rewindCleanupTimeout bounds the cleanup a rewind owes after its caller is gone:
// settling the safety row, discarding a fork, reading the durable facts back.
const rewindCleanupTimeout = 15 * time.Second

// hostContext is a context for work the rewind owes whether or not the caller is
// still there. It keeps the caller's values (identity, tracing) but not its
// cancellation, and it is bounded.
func hostContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), rewindCleanupTimeout)
}

// rewindRun carries the state of one rewind through its steps, so each step can
// read what the ones before it produced without a chain of parameters.
type rewindRun struct {
	service   *Service
	session   AgentSession
	held      heldRewindPlan
	forker    RuntimeForker
	overwrite []string

	// launchBudget is the number of launch preparations this rewind may still
	// make, shared by its first launch and every finish-forward round.
	launchBudget int

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
	s.forgetBackgroundWork(r.session.ID)
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
	// 6. The files. Whether the fork's launch can still be published is asked
	// first: a launch that went stale while the fork opened (S committed) is
	// refused here, with the project untouched, rather than at step 7 after the
	// files were already put back. A commit that lands during the restore itself
	// still makes step 7 refuse; the files are then restored, the conversation
	// uncut, the safety checkpoint is the way back, and a new rewind completes.
	var files *RestoreResult
	if r.held.restoreToken != "" {
		if err := s.checkServingLaunch(r.session.ID, r.launch); err != nil {
			r.discardFork(ctx)
			r.abandonSafety(ctx, err)
			return RewindResult{}, err
		}
		applied, err := s.applyRestoreHeld(ctx, r.held.restoreToken, r.session, r.overwrite)
		if err != nil && checkpoint.Changed(err) {
			// The restore stopped part-way: the cut was never attempted, but files
			// may have changed, so this is not a refusal. The fork is not wanted
			// and is discarded. The safety row stays pending, not failed: it is the
			// one record that a rewind did not finish, and InterruptedRewinds lists
			// pending rows only. Its snapshot is the way back.
			r.discardFork(ctx)
			return RewindResult{}, &RewindPartialError{Cause: err, SafetyCheckpointID: r.safetyID, RestoreIncomplete: true}
		}
		if err != nil {
			// The restore changed nothing (it was refused before its first write) and
			// nothing has been committed: close the fork and delete it, so the
			// vendor is not left holding a session nothing points at.
			r.discardFork(ctx)
			r.abandonSafety(ctx, err)
			return RewindResult{}, err
		}
		files = &applied
	}

	// 7. The one write. Everything or nothing.
	//
	// Nothing was restored (a conversation-only rewind): a refusal changed
	// nothing, and says so. Files restored: the files are now ahead of the
	// conversation, so a refusal that is only S having moved is finished forward
	// (below) and anything else says exactly what state it leaves.
	published := true
	for {
		err := r.commitCut(ctx)
		if err == nil {
			break
		}
		outcome := r.cutOutcome(ctx, err)
		if outcome == cutCommitted {
			// The transaction landed and only its acknowledgement was lost. The cut,
			// the vendor id and the reference are durable, so this rewind succeeded;
			// what is left is the in-memory publication the failed call skipped.
			published = r.publishCommitted()
			break
		}
		if outcome == cutUnknown {
			// Neither proven. The fork may be the durable winner: close it, delete
			// nothing (not it, not the source), and keep the safety row pending so a
			// restart still reports a rewind that did not finish.
			r.closeFork(ctx)
			if files == nil {
				return RewindResult{}, fmt.Errorf("%w: %v", ErrRewindCommitUnknown, err)
			}
			return RewindResult{}, &RewindUnknownError{Cause: err, SafetyCheckpointID: r.safetyID}
		}
		if files == nil {
			r.discardFork(ctx)
			r.abandonSafety(ctx, err)
			// Proven not committed: the failure is reported as what it was, without the
			// commit sentinel, which would say the opposite.
			if errors.Is(err, ErrRewindCommitUnknown) {
				return RewindResult{}, fmt.Errorf("agenthub: the rewind was not committed: %v", err)
			}
			return RewindResult{}, err
		}
		if !r.canFinishForward(ctx, err) {
			r.discardFork(ctx)
			r.abandonSafety(ctx, err)
			return RewindResult{}, &RewindPartialError{Cause: err, SafetyCheckpointID: r.safetyID}
		}
		// S moved after the files were put back. The files, the rewind target, the
		// cut and the safety checkpoint stay as they are; the fork and its endpoint
		// were prepared for the old S, so both are discarded and a new launch is
		// prepared by the ordinary rules, gate included, against the same session
		// reference. Nothing of the discarded endpoint is carried over.
		r.discardFork(ctx)
		if err := r.openForked(ctx); err != nil {
			r.abandonSafety(ctx, err)
			return RewindResult{}, &RewindPartialError{Cause: err, SafetyCheckpointID: r.safetyID}
		}
	}

	// 8. Vendor settings and callbacks run outside the coordinator. The cut and
	// reference committed atomically; a crash still leaves the restart guard.
	// The cut is durable, so what follows is owed whether or not the caller is still
	// there: on the host's own bounded context, as the cleanup before the cut is.
	if published {
		s.attachUpdates(r.session.ID, r.newRuntime)
		hctx, cancel := hostContext(ctx)
		s.reapplySessionSettings(hctx, r.session.ID, r.newRuntime)
		cancel()
	}

	// 9. Tell the UI. Its transcript is now wrong in a way no event it already
	// listens to would explain.
	if s.OnSessionRewound != nil {
		s.OnSessionRewound(r.session.ID, r.held.preview.PromptText)
	}

	// 10. The old vendor session is unreachable now. Best effort: a vendor that
	// refuses is holding a file, not blocking the user. With no live fork to ask
	// (a committed cut that could not be published) it stays, rather than being
	// deleted through anything but the winner.
	if published {
		r.deleteOldVendorSession(ctx)
	}

	return RewindResult{
		SessionID:  r.session.ID,
		PromptText: r.held.preview.PromptText,
		Files:      files,
	}, nil
}

// commitCut is step 7 for the launch and fork the run currently holds: the
// transcript cut, the turn and checkpoint rows, the safety row and the fork's
// applied reference in one transaction, published under the session's epoch.
func (r *rewindRun) commitCut(ctx context.Context) error {
	s := r.service
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
	} else if s.Harness != nil {
		// Attached, and this fork has no snapshot (no workspace): no reference
		// belongs to it. Without a harness (a rolled-back build) the stored
		// reference is left alone: deleting it would erase the baseline the
		// reopen gate needs once the harness is back (O-2b).
		spec.HarnessClear = true
	}
	// Keep the rewind transaction (including its applied reference) and runtime
	// publication under the same epoch/Close boundary as create and resume.
	return s.publishServingLaunch(r.session.ID, r.launch, func() error {
		if err := s.store.RewindSession(ctx, spec); err != nil {
			return err
		}
		r.publishRuntime()
		return nil
	})
}

// publishRuntime puts the fork's runtime, usage facts, applied reference and
// endpoint into the service's maps. It runs under the coordinator, from
// publishServingLaunch, and only after the cut is durable.
func (r *rewindRun) publishRuntime() {
	s := r.service
	s.mu.Lock()
	s.runtimes[r.session.ID] = r.newRuntime
	s.markUsedLocked(r.session.ID)
	s.runtimeUsage[r.session.ID] = r.usageFacts
	s.publishHarnessLocked(r.session.ID, r.launch)
	s.sessionAccount[r.session.ID] = r.usageFacts.AccountID
	s.endpoints[r.session.ID] = r.closeNew
	s.forgetWorkspaceContextLocked(r.session.ID)
	s.mu.Unlock()
}

// cutOutcome is what a failed commitCut proves about the transaction.
type cutOutcome int

const (
	// cutNotCommitted: nothing was written. Every refusal before the commit (the
	// publication fence, a failed statement) rolled the whole cut back.
	cutNotCommitted cutOutcome = iota
	// cutCommitted: the cut, the vendor id and the reference are durable.
	cutCommitted
	// cutUnknown: the commit failed and the durable facts could not settle it.
	cutUnknown
)

// cutOutcome decides what to believe about a failed commitCut. Only a failure while
// committing is ambiguous; it is settled from what is durable: the session's vendor
// id (the same transaction wrote it), else the safety row (marked done by the same
// transaction). Whatever cannot be read, or reads as something else, is unknown.
func (r *rewindRun) cutOutcome(ctx context.Context, err error) cutOutcome {
	if !errors.Is(err, ErrRewindCommitUnknown) {
		return cutNotCommitted
	}
	hctx, cancel := hostContext(ctx)
	defer cancel()
	s := r.service
	if next := r.newRuntime.ACPSessionID(); next != "" && next != r.session.ACPSessionID {
		current, gerr := s.store.GetSession(hctx, r.session.ID)
		switch {
		case gerr != nil:
			return cutUnknown
		case current.ACPSessionID == next:
			return cutCommitted
		case current.ACPSessionID == r.session.ACPSessionID:
			return cutNotCommitted
		}
		return cutUnknown
	}
	if r.safetyID == "" {
		return cutUnknown
	}
	record, gerr := s.store.GetCheckpoint(hctx, r.safetyID)
	switch {
	case gerr != nil:
		return cutUnknown
	case record.Status == CheckpointDone:
		return cutCommitted
	case record.Status == CheckpointPending:
		return cutNotCommitted
	}
	return cutUnknown
}

// publishCommitted publishes the runtime of a cut that is proven durable, under the
// same fence as any publication but without writing the cut again. It reports
// whether the fork became the session's live runtime. When the fence refuses (S
// moved, a Close began) the cut is still durable and the session simply has no
// live runtime: the fork is closed, never deleted, and the next prompt resumes from
// the durable vendor id.
func (r *rewindRun) publishCommitted() bool {
	err := r.service.publishServingLaunch(r.session.ID, r.launch, func() error {
		r.publishRuntime()
		return nil
	})
	if err == nil {
		return true
	}
	log.Printf("agenthub: rewind %s: the cut is committed but its runtime could not be published (%v); the session resumes from the stored conversation", r.session.ID, err)
	r.closeFork(context.Background())
	return false
}

// canFinishForward says whether a refused publication, after the files were
// restored, may be answered with another launch: only when S moved and nothing else
// did (the coordinator decided that at the refusal), the session is still open, the
// caller is still there and the rewind has budget left. Every other refusal is
// final: a Close in progress, failed or finished, a lost or replaced endpoint, a
// deleted session, a cancelled caller, a store or fork failure.
func (r *rewindRun) canFinishForward(ctx context.Context, err error) bool {
	var stale *launchStaleError
	if !errors.As(err, &stale) || stale.reason != launchStaleEpoch || ctx.Err() != nil || r.launchBudget <= 0 {
		return false
	}
	// Belt and braces: the lifecycle generation already excludes a Close; this is
	// the durable word on it, and on a deleted session.
	current, gerr := r.service.store.GetSession(ctx, r.session.ID)
	return gerr == nil && current.Status != SessionStatusClosed
}

// ErrRewindFilesRestored is a rewind that put the files back and then could not
// cut the conversation. The project folder has changed; the conversation has not.
var ErrRewindFilesRestored = errors.New("agenthub.rewindFilesRestored")

// RewindPartialError is that outcome, and it is only returned when the cut was
// proven not to have committed. Cause is why the cut could not be published (still
// matches ErrHarnessLaunchStale, ErrSessionClosed and so on). Rewinding the same
// message again finishes the job (the files already match, the restore is a
// no-op); SafetyCheckpointID names the snapshot taken before the files changed,
// which is kept, for undoing the restore instead. The host never repeats the
// restore by itself.
type RewindPartialError struct {
	Cause              error
	SafetyCheckpointID string
	// RestoreIncomplete is set when the file restore itself stopped part-way: some
	// files may have been put back and others not, so the outcome must not say the
	// files were put back.
	RestoreIncomplete bool
}

func (e *RewindPartialError) Error() string {
	if e.RestoreIncomplete {
		return "agenthub: the file restore stopped part-way and the conversation was not rewound: " + e.Cause.Error()
	}
	return "agenthub: the files were restored but the conversation was not rewound: " + e.Cause.Error()
}
func (e *RewindPartialError) Unwrap() error        { return e.Cause }
func (e *RewindPartialError) Is(target error) bool { return target == ErrRewindFilesRestored }

// ErrRewindOutcomeUnknown is a rewind that restored the files and then could not
// prove whether the cut committed: the store failed while committing and the
// durable facts could not be read back. Neither "rewound" nor "not rewound" is
// true of the conversation as far as this process can tell.
var ErrRewindOutcomeUnknown = errors.New("agenthub.rewindOutcomeUnknown")

// RewindUnknownError is that outcome. It never matches ErrRewindFilesRestored:
// that sentence says the conversation was not rewound, which was not proven.
// Nothing is deleted on this path (not the fork, which may be the durable
// winner, and not the old vendor session), and the safety row stays pending so
// the next start still reports a rewind that did not finish.
type RewindUnknownError struct {
	Cause              error
	SafetyCheckpointID string
}

func (e *RewindUnknownError) Error() string {
	return "agenthub: the files were restored and it is not known whether the conversation was rewound: " + e.Cause.Error()
}
func (e *RewindUnknownError) Unwrap() error        { return e.Cause }
func (e *RewindUnknownError) Is(target error) bool { return target == ErrRewindOutcomeUnknown }

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
//
// It runs on the host's own bounded context, not the caller's: a caller that
// cancelled is exactly when the row would otherwise stay "in flight" forever. When
// the row cannot be read or written the failure is reported and logged, never
// swallowed, and the row is left as it is (pending) rather than claimed settled.
func (r *rewindRun) abandonSafety(ctx context.Context, cause error) {
	if r.safetyID == "" {
		return
	}
	hctx, cancel := hostContext(ctx)
	defer cancel()
	record, err := r.service.store.GetCheckpoint(hctx, r.safetyID)
	if err != nil {
		r.reportBookkeeping("reading the safety row", err)
		return
	}
	finishedAt := time.Now().UTC()
	record.Status = CheckpointFailed
	record.FinishedAt = &finishedAt
	record.Detail = reasonDetail(cause)
	if err := r.service.store.UpdateCheckpoint(hctx, record); err != nil {
		r.reportBookkeeping("settling the safety row", err)
	}
}

// reportBookkeeping surfaces a failure of the rewind's own records. The log line
// names the step and the safety row, never a path or the cause's payload beyond
// the store's own error text.
func (r *rewindRun) reportBookkeeping(what string, err error) {
	log.Printf("agenthub: rewind %s: %s %s: %v", r.session.ID, what, r.safetyID, err)
	if r.service.OnRecordError != nil {
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
	launch, err := s.prepareLaunchWithin(ctx, &spec, r.session.ID, r.session.WorkspaceID, r.session.AgentType, previous, launchReopen, &r.launchBudget)
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
//
// Only a fork that is proven not to be the durable winner is deleted. Where the
// cut's outcome is unknown, closeFork is used instead. Both run on the host's own
// bounded context: the caller may be gone, and the fork and its endpoint are owed
// their cleanup regardless.
func (r *rewindRun) discardFork(ctx context.Context) {
	if r.newRuntime == nil {
		return
	}
	hctx, cancel := hostContext(ctx)
	defer cancel()
	if deleter, ok := r.newRuntime.(RuntimeDeleter); ok {
		if err := deleter.Delete(hctx); err != nil {
			log.Printf("agenthub: rewind %s: deleting the abandoned fork: %v", r.session.ID, err)
		}
	}
	r.closeFork(ctx)
}

// closeFork shuts the fork's runtime and releases its endpoint without asking the
// vendor to forget the conversation: the vendor session survives a Close, which is
// what makes this safe for a fork that might be the durable winner.
func (r *rewindRun) closeFork(ctx context.Context) {
	if r.newRuntime != nil {
		hctx, cancel := hostContext(ctx)
		if err := r.newRuntime.Close(hctx); err != nil {
			log.Printf("agenthub: rewind %s: closing the abandoned fork: %v", r.session.ID, err)
		}
		cancel()
	}
	if r.closeNew != nil {
		r.closeNew()
		r.closeNew = nil
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
	// The cut is durable: the deletion is owed whether or not the caller is still there.
	hctx, cancel := hostContext(ctx)
	defer cancel()
	if err := deleter.DeleteSession(hctx, r.session.ACPSessionID); err != nil {
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
