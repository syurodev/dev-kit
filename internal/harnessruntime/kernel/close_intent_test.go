package kernel

import (
	"reflect"
	"strconv"
	"testing"
)

func closeIntentCommand(s State, id ID, kind CommandKind, input TransitionInput) Command {
	return Command{
		ID: id, Kind: kind, RunID: s.RunID, SessionID: s.SessionID,
		ExpectedRevision: s.Revision, WriterToken: s.AdmissionToken, WriterIncarnation: s.Incarnation,
		Transition: input,
	}
}

func requireCloseIntentReject(t *testing.T, s State, cmd Command) {
	t.Helper()
	if _, err := Decide(s, cmd); err == nil {
		t.Fatalf("Decide(%s) unexpectedly accepted close intent", cmd.ID)
	}
}

func requireUnknownCloseBlocksContinuation(t *testing.T, h *transitionHarness) {
	t.Helper()
	if !hasPendingUnknownClose(h.state) || dispatchable(&h.state) || canAdvance(&h.state) {
		t.Fatal("unknown close intent did not block dispatch and advancement")
	}

	// Prove this exact continuation command is valid in an otherwise equivalent
	// pre-close run; the pending-close state below is the reason it must reject.
	control := newTransitionHarness(t)
	toolID, attemptID := control.toolDispatching()
	control.issue(Command{Kind: CommandObserveToolResult, ToolResult: ToolResultInput{
		ToolID: toolID, AttemptID: attemptID, ResultRef: "successful-tool-result",
		Disposition: ResultSucceeded, Exposure: ExposureKnown,
		ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Fence: 2,
	}})
	control.issue(Command{ID: "close-pending-continue", Kind: CommandFinishStep, FinishStep: FinishStepInput{Next: NextContinue, Continuation: ContinuationToolCalls}})

	for _, cmd := range []Command{
		{Kind: CommandStartStep, Step: StepInput{StepID: "step-after-unknown-close"}},
		{Kind: CommandPlanModel, Model: ModelInput{InvocationID: "model-after-unknown-close", AttemptID: "attempt-after-unknown-close", RequestRef: "request-ref"}},
		{Kind: CommandFinishStep, FinishStep: FinishStepInput{Next: NextContinue, Continuation: ContinuationToolCalls}},
	} {
		if cmd.Kind == CommandFinishStep {
			cmd.ID = "close-pending-continue"
		} else {
			cmd.ID = ID("rejected-after-unknown-close-" + strconv.Itoa(int(cmd.Kind)))
		}
		if !validID(cmd.ID) {
			t.Fatalf("generated invalid command ID %q", cmd.ID)
		}
		cmd.ExpectedRevision = h.state.Revision
		cmd.RunID, cmd.SessionID = h.state.RunID, h.state.SessionID
		cmd.WriterToken, cmd.WriterIncarnation = h.state.AdmissionToken, h.state.Incarnation
		requireCloseIntentReject(t, h.state, cmd)
	}
}

func TestUnknownAcceptanceCloseIntentBlocksContinuationUntilAbortAndClosure(t *testing.T) {
	h := newTransitionHarness(t)
	toolID, attemptID := h.toolDispatching()
	unknown := h.issue(Command{Kind: CommandObserveUnknownTool, Transition: TransitionInput{
		TargetID: toolID, AttemptID: attemptID, CauseRef: "close-interruption",
		Exposure: ExposureUnknown, ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Fence: 2,
	}})
	preAccept := cloneState(h.state)
	acceptIntent := closeIntentCommand(h.state, "accept-unknown-close", CommandAcceptUnknownOutcome, TransitionInput{
		TargetID: toolID, AttemptID: attemptID, CauseRef: "close-interruption", Ref: "user-acceptance-proof",
		ReceiptRef: "user-acceptance-receipt", ResultRef: "abandoned-result", Actor: ActorUser,
		Disposition: ResultAbandonedUnknown,
	})
	accepted := h.issue(acceptIntent)
	if len(unknown.Events) != 2 || unknown.Events[1].Kind != EventRunInterrupted || unknown.Events[1].CauseRef != "close-interruption" || len(accepted.Events) != 2 || accepted.Events[0].Kind != EventUnknownAccepted || accepted.Events[1].Kind != EventToolResultRecorded || accepted.Events[1].Disposition != ResultAbandonedUnknown {
		t.Fatalf("acceptance did not persist explicit unknown close facts: %#v", accepted.Events)
	}
	for name, mutate := range map[string]func(*Event){
		"receipt":     func(e *Event) { e.ReceiptRef = "other-receipt" },
		"cause":       func(e *Event) { e.CauseRef = "other-cause" },
		"disposition": func(e *Event) { e.Disposition = ResultSucceeded },
		"exposure":    func(e *Event) { e.Exposure = ExposureKnown },
	} {
		t.Run("acceptance-result-mismatch-"+name, func(t *testing.T) {
			candidate, err := Decide(preAccept, acceptIntent)
			if err != nil {
				t.Fatalf("valid acceptance setup rejected: %v", err)
			}
			mutate(&candidate.Events[1])
			if _, err := Evolve(preAccept, candidate); err == nil {
				t.Fatal("reducer accepted a result that did not match unknown-acceptance authority")
			}
		})
	}
	requireUnknownCloseBlocksContinuation(t, h)

	h.issue(Command{Kind: CommandAbortStep, Transition: TransitionInput{Ref: "abort-after-unknown-acceptance"}})
	closed := h.issue(Command{Kind: CommandConfirmRunClosure, Transition: TransitionInput{
		Ref: "closure-ref", ReceiptRef: "closure-proof", Actor: ActorUser, Revision: h.state.Revision,
		Outcome: OutcomeAbandonedUnknown, PreviewMode: ClosurePreviewMetadataOnly, PreviewVersion: 1,
		UnavailableReason: UnavailablePayloadMissing, PreviousContextRevision: h.state.ContextRevision,
		ContextRevision: h.state.ContextRevision + 1, ContextRequirement: ContextRequirementFreshRequired,
		CauseRef: "close-interruption", CauseEventRef: unknown.Events[1].ID,
		RecoveryDisposition: RecoveryDispositionClosedUnavailable,
	}})
	if len(closed.Events) != 2 || !h.state.Finished || h.state.Outcome != OutcomeAbandonedUnknown || !h.state.Steps[h.state.CurrentStep].Aborted || h.state.Tools[toolID].Disposition != ResultAbandonedUnknown || h.state.Exposures[toolID] != ExposureUnknown {
		t.Fatalf("abort and user closure did not preserve accepted unknown outcome: events=%#v state=%#v", closed.Events, h.state)
	}
}

func TestToolAbandonNextEndRequiresUserReceiptAndExactInterruption(t *testing.T) {
	for name, facts := range map[string]struct {
		actor   ActorKind
		receipt ID
		cause   ID
	}{
		"non-user":        {ActorSystem, "close-receipt", "close-interruption"},
		"missing-receipt": {ActorUser, "", "close-interruption"},
		"wrong-cause":     {ActorUser, "close-receipt", "different-interruption"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newTransitionHarness(t)
			toolID, attemptID := h.toolDispatching()
			h.issue(Command{Kind: CommandObserveUnknownTool, Transition: TransitionInput{
				TargetID: toolID, AttemptID: attemptID, CauseRef: "close-interruption", Exposure: ExposureUnknown,
				ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Fence: 2,
			}})
			before := cloneState(h.state)
			cmd := closeIntentCommand(h.state, "rejected-close-command", CommandAbandonTool, TransitionInput{
				TargetID: toolID, AttemptID: attemptID, Ref: "abandon-proof", ReceiptRef: facts.receipt,
				Exposure: ExposureUnknown, Next: NextEnd, CauseRef: facts.cause,
				ResultRef: "abandoned-result", Actor: facts.actor,
			})
			requireCloseIntentReject(t, h.state, cmd)
			if !reflect.DeepEqual(h.state, before) {
				t.Fatal("rejected close intent mutated the state")
			}
		})
	}
}

func TestToolAbandonNextEndPersistsPairedAuthorityAndBlocksContinuation(t *testing.T) {
	h := newTransitionHarness(t)
	toolID, attemptID := h.toolDispatching()
	unknown := h.issue(Command{Kind: CommandObserveUnknownTool, Transition: TransitionInput{
		TargetID: toolID, AttemptID: attemptID, CauseRef: "close-interruption", Exposure: ExposureUnknown,
		ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Fence: 2,
	}})
	cmd := closeIntentCommand(h.state, "abandon-next-end", CommandAbandonTool, TransitionInput{
		TargetID: toolID, AttemptID: attemptID, Ref: "abandon-proof", ReceiptRef: "user-receipt",
		Exposure: ExposureUnknown, Next: NextEnd, CauseRef: "close-interruption", ResultRef: "abandoned-result", Actor: ActorUser,
	})
	batch := h.issue(cmd)
	if len(batch.Events) != 2 || batch.Events[0].Kind != EventToolAttemptAbandoned || batch.Events[0].Next != NextEnd || batch.Events[1].Kind != EventToolResultRecorded || batch.Events[1].Disposition != ResultAbandonedUnknown || batch.Events[1].Exposure != ExposureUnknown || batch.Events[1].CauseRef != "close-interruption" || batch.Events[1].ReceiptRef != "user-receipt" {
		t.Fatalf("NextEnd did not atomically persist matching abandonment/result authority: %#v", batch.Events)
	}
	if !h.state.Interruptions["close-interruption"].Accepted || h.state.Tools[toolID].Disposition != ResultAbandonedUnknown || h.state.ToolAttempts[attemptID].State != AttemptAbandoned || h.state.Exposures[toolID] != ExposureUnknown {
		t.Fatal("NextEnd close did not preserve accepted interruption and unknown exposure")
	}
	requireUnknownCloseBlocksContinuation(t, h)

	// Replaying the exact receipt is idempotent; changing either authority fact
	// under the same command ID is a conflict.
	replay, err := Decide(h.state, batch.Intent)
	if err != nil || !reflect.DeepEqual(replay, batch) {
		t.Fatalf("exact close command replay was not stable: err=%v", err)
	}
	for name, mutate := range map[string]func(*Command){
		"cause":   func(c *Command) { c.Transition.CauseRef = "different-interruption" },
		"receipt": func(c *Command) { c.Transition.ReceiptRef = "different-receipt" },
	} {
		t.Run("replay-mismatch-"+name, func(t *testing.T) {
			changed := batch.Intent
			mutate(&changed)
			if _, err := Decide(h.state, changed); err == nil {
				t.Fatal("same command ID accepted changed close authority")
			}
		})
	}

	// The reducer independently requires the paired result to bind the exact
	// cause, receipt, disposition, and exposure; a lone authority event cannot
	// self-authorize a close.
	for name, mutate := range map[string]func(*Event){
		"cause":       func(e *Event) { e.CauseRef = "other-cause" },
		"receipt":     func(e *Event) { e.ReceiptRef = "other-receipt" },
		"disposition": func(e *Event) { e.Disposition = ResultSucceeded },
		"exposure":    func(e *Event) { e.Exposure = ExposureKnown },
	} {
		t.Run("result-pair-mismatch-"+name, func(t *testing.T) {
			// Recreate the pre-command state by replaying the command history up
			// to the interruption; use a fresh harness to avoid testing against a
			// state that already contains the close events.
			fresh := newTransitionHarness(t)
			id, attempt := fresh.toolDispatching()
			fresh.issue(Command{Kind: CommandObserveUnknownTool, Transition: TransitionInput{TargetID: id, AttemptID: attempt, CauseRef: "close-interruption", Exposure: ExposureUnknown, ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Fence: 2}})
			valid := closeIntentCommand(fresh.state, "tampered-close", CommandAbandonTool, TransitionInput{TargetID: id, AttemptID: attempt, Ref: "abandon-proof", ReceiptRef: "user-receipt", Exposure: ExposureUnknown, Next: NextEnd, CauseRef: "close-interruption", ResultRef: "abandoned-result", Actor: ActorUser})
			candidate, err := Decide(fresh.state, valid)
			if err != nil {
				t.Fatalf("valid close setup rejected: %v", err)
			}
			mutate(&candidate.Events[1])
			if _, err := Evolve(fresh.state, candidate); err == nil {
				t.Fatal("reducer accepted mismatched abandoned-unknown result pair")
			}
		})
	}
	t.Run("missing-result", func(t *testing.T) {
		fresh := newTransitionHarness(t)
		id, attempt := fresh.toolDispatching()
		fresh.issue(Command{Kind: CommandObserveUnknownTool, Transition: TransitionInput{TargetID: id, AttemptID: attempt, CauseRef: "close-interruption", Exposure: ExposureUnknown, ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Fence: 2}})
		valid := closeIntentCommand(fresh.state, "unpaired-close", CommandAbandonTool, TransitionInput{TargetID: id, AttemptID: attempt, Ref: "abandon-proof", ReceiptRef: "user-receipt", Exposure: ExposureUnknown, Next: NextEnd, CauseRef: "close-interruption", ResultRef: "abandoned-result", Actor: ActorUser})
		candidate, err := Decide(fresh.state, valid)
		if err != nil {
			t.Fatalf("valid close setup rejected: %v", err)
		}
		candidate.Events = candidate.Events[:1]
		if _, err := Evolve(fresh.state, candidate); err == nil {
			t.Fatal("reducer accepted NextEnd abandonment without its result pair")
		}
	})

	h.issue(Command{Kind: CommandAbortStep, Transition: TransitionInput{Ref: "abort-after-abandon-next-end"}})
	closed := h.issue(Command{Kind: CommandConfirmRunClosure, Transition: TransitionInput{
		Ref: "closure-ref", ReceiptRef: "closure-proof", Actor: ActorUser, Revision: h.state.Revision,
		Outcome: OutcomeAbandonedUnknown, PreviewMode: ClosurePreviewMetadataOnly, PreviewVersion: 1,
		UnavailableReason: UnavailablePayloadMissing, PreviousContextRevision: h.state.ContextRevision,
		ContextRevision: h.state.ContextRevision + 1, ContextRequirement: ContextRequirementFreshRequired,
		CauseRef: "close-interruption", CauseEventRef: unknown.Events[1].ID,
		RecoveryDisposition: RecoveryDispositionClosedUnavailable,
	}})
	if len(closed.Events) != 2 || !h.state.Finished || h.state.Outcome != OutcomeAbandonedUnknown || !h.state.Steps[h.state.CurrentStep].Aborted || h.state.Exposures[toolID] != ExposureUnknown {
		t.Fatalf("abort and user closure did not preserve NextEnd unknown outcome: %#v", closed.Events)
	}
}
