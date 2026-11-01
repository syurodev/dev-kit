package kernel

import (
	"fmt"
	"reflect"
	"testing"
)

// transitionHarness applies state changes only through typed Commands.
type transitionHarness struct {
	t      *testing.T
	state  State
	number int
}

func newTransitionHarness(t *testing.T) *transitionHarness {
	t.Helper()
	return &transitionHarness{t: t, state: EmptyState()}
}

func (h *transitionHarness) issue(cmd Command) Batch {
	h.t.Helper()
	h.number++
	if cmd.ID == "" {
		cmd.ID = ID(fmt.Sprintf("typed-command-%d", h.number))
	}
	cmd.ExpectedRevision = h.state.Revision
	if cmd.RunID == "" {
		cmd.RunID = "approval-run"
	}
	if cmd.SessionID == "" {
		cmd.SessionID = "approval-session"
	}
	if h.state.Admitted {
		cmd.WriterToken, cmd.WriterIncarnation = h.state.AdmissionToken, h.state.Incarnation
	} else if cmd.Kind == CommandAdmitRun {
		cmd.WriterToken, cmd.WriterIncarnation = cmd.Admission.Token, cmd.Admission.Incarnation
	}
	batch, err := Decide(h.state, cmd)
	if err != nil {
		h.t.Fatalf("Decide(%d): %v", cmd.Kind, err)
	}
	next, err := Evolve(h.state, batch)
	if err != nil {
		h.t.Fatalf("Evolve(%d): %v", cmd.Kind, err)
	}
	h.state = next
	return batch
}

func (h *transitionHarness) modelInFlight() (invocationID, attemptID ID) {
	h.t.Helper()
	h.issue(Command{Kind: CommandAcceptRun, AcceptRun: AcceptRunInput{
		InputRef: "protected-input-ref", Origin: OriginUser, InitialStepLimit: 4, RetryBudgetLimit: 5, ModelRetryPolicyVersion: 1,
	}})
	h.issue(Command{Kind: CommandAdmitRun, Admission: AdmissionInput{Token: 7, Incarnation: "writer-inc-1"}})
	h.issue(Command{Kind: CommandStartStep, Step: StepInput{StepID: "step-1"}})
	h.issue(Command{Kind: CommandPlanModel, Model: ModelInput{
		InvocationID: "model-1", AttemptID: "model-attempt-1", RequestRef: "model-request-ref",
	}})
	h.issue(Command{Kind: CommandDispatchModel, Model: ModelInput{
		InvocationID: "model-1", AttemptID: "model-attempt-1", RequestRef: "model-dispatch-ref",
		ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Fence: 1,
	}})
	h.issue(Command{Kind: CommandObserveModelStream, Model: ModelInput{
		InvocationID: "model-1", AttemptID: "model-attempt-1",
		ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Fence: 1,
	}})
	return "model-1", "model-attempt-1"
}

func (h *transitionHarness) toolAwaitingApproval() (toolID, attemptID, blockerID ID) {
	return h.toolAwaitingApprovalWithEffect(EffectSideEffecting)
}

func (h *transitionHarness) toolAwaitingApprovalWithEffect(effect EffectClass) (toolID, attemptID, blockerID ID) {
	h.t.Helper()
	h.modelInFlight()
	h.issue(Command{Kind: CommandObserveModelResponse, ModelResponse: ModelResponseInput{
		InvocationID: "model-1", AttemptID: "model-attempt-1", ResponseRef: "model-response-ref",
		Usage: UsageKnown, UsageRef: "usage-ref", Fence: 1, ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1",
		Candidates: []ToolCandidate{{ID: "tool-1", AttemptID: "tool-attempt-1", ArgsRef: "args-protected-ref", Descriptor: "descriptor-ref", SourceIndex: 0, EffectClass: effect}},
	}})
	h.issue(Command{Kind: CommandPrepareTool, Tool: ToolInput{ToolID: "tool-1", Generation: 1, Ref: "tool-check-ref"}})
	toolID, attemptID, blockerID = "tool-1", "tool-attempt-1", "approval-blocker-1"
	h.issue(Command{Kind: CommandRequestToolApproval, Transition: TransitionInput{
		TargetID: toolID, Generation: 1, Ref: "args-protected-ref", RequestRef: "approval-request-ref", BlockerID: blockerID,
	}})
	return toolID, attemptID, blockerID
}

func (h *transitionHarness) toolDispatching() (toolID, attemptID ID) {
	return h.toolDispatchingWithEffect(EffectSideEffecting)
}

func (h *transitionHarness) toolDispatchingWithEffect(effect EffectClass) (toolID, attemptID ID) {
	h.t.Helper()
	toolID, attemptID, _ = h.toolAwaitingApprovalWithEffect(effect)
	h.issue(Command{Kind: CommandResolveToolApproval, Transition: TransitionInput{
		TargetID: toolID, AttemptID: attemptID, Generation: 1, RequestRef: "approval-request-ref",
		Ref: "args-protected-ref", ReceiptRef: "approval-receipt-ref", Disposition: ResultSucceeded,
	}})
	h.issue(Command{Kind: CommandDispatchTool, Tool: ToolInput{
		ToolID: toolID, AttemptID: attemptID, Generation: 1,
		ReceiptRef: "approval-receipt-ref", Ref: "tool-dispatch-ref",
		ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Fence: 2,
	}})
	return toolID, attemptID
}

func TestDecideResolveToolApprovalProducesAtomicTypedTransitions(t *testing.T) {
	for _, disposition := range []ResultDisposition{ResultSucceeded, ResultDenied, ResultRefused} {
		t.Run(fmt.Sprintf("disposition-%d", disposition), func(t *testing.T) {
			h := newTransitionHarness(t)
			toolID, attemptID, blockerID := h.toolAwaitingApproval()
			resultRef, receiptRef := ID("approval-result-ref"), ID("approval-receipt-ref")
			cmd := Command{
				ID: "resolve-approval", Kind: CommandResolveToolApproval,
				RunID: h.state.RunID, SessionID: h.state.SessionID,
				ExpectedRevision: h.state.Revision, WriterToken: h.state.AdmissionToken, WriterIncarnation: h.state.Incarnation,
				Transition: TransitionInput{
					TargetID: toolID, AttemptID: attemptID, Generation: 1,
					RequestRef: "approval-request-ref", Ref: "args-protected-ref",
					ResultRef: resultRef, ReceiptRef: receiptRef, Disposition: disposition,
				},
			}
			batch := h.issue(cmd)
			if len(batch.Events) != 2 || batch.Events[0].Kind != EventToolApprovalResolved {
				t.Fatalf("approval command emitted unexpected batch: %#v", batch.Events)
			}
			approval, outcome := batch.Events[0], batch.Events[1]
			if approval.ToolID != toolID || approval.Generation != 1 || approval.RequestRef != "approval-request-ref" || approval.CauseRef != "args-protected-ref" || approval.ReceiptRef != receiptRef || approval.Disposition != disposition {
				t.Fatal("approval fact lost its request, input, receipt, or disposition refs")
			}
			if disposition == ResultSucceeded {
				if outcome.Kind != EventToolReadyRecorded || outcome.Ref != "args-protected-ref" || outcome.ReceiptRef != receiptRef {
					t.Fatal("approval success did not atomically make the exact checked args ready")
				}
				if h.state.Tools[toolID].State != ToolReady || h.state.Tools[toolID].ApprovalRef != receiptRef || h.state.Tools[toolID].ArgsRef != "args-protected-ref" {
					t.Fatal("approval success projection is inconsistent")
				}
			} else {
				if outcome.Kind != EventToolResultRecorded || outcome.AttemptID != attemptID || outcome.Ref != resultRef || outcome.ReceiptRef != receiptRef || outcome.Disposition != disposition || outcome.Generation != 1 {
					t.Fatal("approval denial/refusal did not atomically record terminal result and provenance")
				}
				want := ToolDenied
				if disposition == ResultRefused {
					want = ToolRefused
				}
				if h.state.Tools[toolID].State != want || h.state.Tools[toolID].ResultRef != resultRef || h.state.Tools[toolID].ResolutionRef != receiptRef {
					t.Fatal("approval denial/refusal projection is inconsistent")
				}
			}
			if _, blocked := h.state.Blockers[blockerID]; blocked {
				t.Fatal("approval resolution left its blocker active")
			}
			// An exact retry returns the original receipt even after the state advances.
			retry, err := Decide(h.state, cmd)
			if err != nil || !reflect.DeepEqual(retry, batch) {
				t.Fatalf("exact approval command retry changed receipt: err=%v", err)
			}
		})
	}
}

func TestDecideResetToolCheckInvalidatesPendingApproval(t *testing.T) {
	h := newTransitionHarness(t)
	toolID, _, blockerID := h.toolAwaitingApproval()
	batch := h.issue(Command{Kind: CommandResetToolCheck, Transition: TransitionInput{
		TargetID: toolID, Generation: 2, Ref: "reset-check-ref", BlockerID: blockerID,
		RequestRef: "approval-request-ref", ReceiptRef: "reset-receipt-ref",
	}})
	if len(batch.Events) != 1 || batch.Events[0].Kind != EventToolCheckReset {
		t.Fatalf("reset command emitted unexpected batch: %#v", batch.Events)
	}
	event := batch.Events[0]
	if event.ToolID != toolID || event.Generation != 2 || event.CauseRef != "reset-check-ref" || event.BlockerID != blockerID || event.RequestRef != "approval-request-ref" || event.ReceiptRef != "reset-receipt-ref" {
		t.Fatal("reset did not retain explicit invalidation provenance")
	}
	tool := h.state.Tools[toolID]
	if tool.State != ToolPlanned || tool.CheckGeneration != 2 || tool.RequestRef != "" || tool.ApprovalRef != "" || tool.ArgsRef != "args-protected-ref" {
		t.Fatal("reset did not return the tool to a clean planned state")
	}
	if _, blocked := h.state.Blockers[blockerID]; blocked {
		t.Fatal("reset left the stale approval blocker active")
	}
}

func TestDecideModelFailureAndCancellationUseAttemptProvenance(t *testing.T) {
	t.Run("failure", func(t *testing.T) {
		h := newTransitionHarness(t)
		invocationID, attemptID := h.modelInFlight()
		batch := h.issue(Command{Kind: CommandObserveModelFailure, Transition: TransitionInput{
			TargetID: invocationID, AttemptID: attemptID, Ref: "failure-ref", CauseRef: "partial-response-ref",
			Usage: UsageKnown, ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Fence: 1, FailureStage: ModelFailureStream, FailureClass: ModelFailureServer, RetryDisposition: RetryDispositionRetry,
		}})
		if len(batch.Events) != 1 || batch.Events[0].Kind != EventModelRequestFailed || batch.Events[0].AttemptID != attemptID || batch.Events[0].InvocationID != invocationID || batch.Events[0].Ref != "failure-ref" || batch.Events[0].CauseRef != "partial-response-ref" {
			t.Fatal("model failure did not retain attempt and protected-ref provenance")
		}
		if h.state.ModelAttempts[attemptID].State != AttemptFailed || h.state.Models[invocationID].State != InvocationActive {
			t.Fatal("nonterminal model failure projection is inconsistent")
		}
	})
	t.Run("cancel then abort", func(t *testing.T) {
		h := newTransitionHarness(t)
		invocationID, attemptID := h.modelInFlight()
		stop := h.issue(Command{Kind: CommandRequestStop, Stop: StopInput{
			Reason: StopShutdown, Mode: StopSuspend, Ref: "shutdown-ref", BlockerID: "host-paused-blocker", Quiescing: true, Actor: ActorSystem,
		}})
		if stop.Events[0].Kind != EventRunStopRequested || !h.state.Suspended || !h.state.Quiescing || h.state.Lifecycle != Running {
			t.Fatal("typed shutdown did not enter quiescing suspension")
		}
		cancel := h.issue(Command{Kind: CommandCancelModel, Transition: TransitionInput{
			TargetID: invocationID, AttemptID: attemptID, Ref: "model-cancel-ref", Terminal: true,
			ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Fence: 1,
		}})
		if cancel.Events[0].Kind != EventModelAttemptCancelled || h.state.ModelAttempts[attemptID].State != AttemptCancelled || h.state.Models[invocationID].State != InvocationCancelled || h.state.Lifecycle != Waiting || !hasWaitReason(h.state, BlockHostPaused) {
			t.Fatal("model cancellation did not acknowledge quiescence and expose host-paused wait")
		}
		aborted := h.issue(Command{Kind: CommandAbortStep, Transition: TransitionInput{Ref: "step-abort-ref"}})
		if len(aborted.Events) != 1 || aborted.Events[0].Kind != EventStepAborted || aborted.Events[0].Ref != "step-abort-ref" || h.state.Steps[h.state.CurrentStep].Open || h.state.Lifecycle != Waiting || !hasWaitReason(h.state, BlockHostPaused) {
			t.Fatal("step was not aborted after typed cancellation evidence")
		}
	})
}

func TestDecideCancelAndUnknownToolOutcomesAreTyped(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		h := newTransitionHarness(t)
		toolID, attemptID := h.toolDispatching()
		batch := h.issue(Command{Kind: CommandCancelTool, Transition: TransitionInput{
			TargetID: toolID, AttemptID: attemptID, Ref: "tool-cancel-ref", Terminal: true,
			ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Fence: 2,
		}})
		if len(batch.Events) != 1 || batch.Events[0].Kind != EventToolAttemptCancelled || h.state.Tools[toolID].State != ToolCancelled || h.state.ToolAttempts[attemptID].State != AttemptCancelled {
			t.Fatal("tool cancellation did not record terminal attempt evidence")
		}
	})
	t.Run("outcome unknown", func(t *testing.T) {
		h := newTransitionHarness(t)
		toolID, attemptID := h.toolDispatching()
		batch := h.issue(Command{Kind: CommandObserveUnknownTool, Transition: TransitionInput{
			TargetID: toolID, AttemptID: attemptID, CauseRef: "tool-timeout-ref", Exposure: ExposureUnknown,
			ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Fence: 2,
		}})
		if len(batch.Events) != 2 || batch.Events[0].Kind != EventToolOutcomeUnknown || batch.Events[1].Kind != EventRunInterrupted || batch.Events[0].CauseRef != "tool-timeout-ref" || batch.Events[1].CauseRef != "tool-timeout-ref" {
			t.Fatal("unknown tool outcome was not paired with interruption evidence")
		}
		if h.state.Tools[toolID].State != ToolUnknown || h.state.ToolAttempts[attemptID].State != AttemptInterrupted || h.state.Exposures[toolID] != ExposureUnknown {
			t.Fatal("unknown tool outcome projection lost exposure uncertainty")
		}
	})
}

func TestDecidePreDispatchCancellationUsesCancellationEvidenceInCanonicalOrder(t *testing.T) {
	for _, stage := range []string{"planned", "checking", "awaiting approval", "ready"} {
		t.Run(stage, func(t *testing.T) {
			h := newTransitionHarness(t)
			h.modelInFlight()
			h.issue(Command{Kind: CommandObserveModelResponse, ModelResponse: ModelResponseInput{
				InvocationID: "model-1", AttemptID: "model-attempt-1", ResponseRef: "model-response-ref", Usage: UsageKnown, UsageRef: "model-usage",
				Candidates: []ToolCandidate{{ID: "tool-1", AttemptID: "tool-attempt-1", ArgsRef: "raw-args-ref", Descriptor: "tool-descriptor", SourceIndex: 0, EffectClass: EffectSideEffecting}},
				Fence:      1, ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1",
			}})
			if stage != "planned" {
				h.issue(Command{Kind: CommandPrepareTool, Tool: ToolInput{ToolID: "tool-1", Generation: 1, Ref: "check-ref"}})
			}
			transition := TransitionInput{TargetID: "tool-1", AttemptID: "tool-attempt-1", Ref: "user-cancel-proof", ReceiptRef: "cancel-receipt", Terminal: true}
			if stage == "awaiting approval" {
				h.issue(Command{Kind: CommandRequestToolApproval, Transition: TransitionInput{TargetID: "tool-1", Generation: 1, Ref: "raw-args-ref", RequestRef: "approval-request", BlockerID: "approval-blocker"}})
				transition.Generation, transition.BlockerID, transition.RequestRef = 2, "approval-blocker", "approval-request"
				bad := Command{ID: "bad-cancel-proof", Kind: CommandCancelTool, RunID: h.state.RunID, SessionID: h.state.SessionID, ExpectedRevision: h.state.Revision, WriterToken: h.state.AdmissionToken, WriterIncarnation: h.state.Incarnation, Transition: transition}
				bad.Transition.BlockerID = "wrong-blocker"
				if _, err := Decide(h.state, bad); err == nil {
					t.Fatal("cancellation with mismatched approval blocker unexpectedly passed")
				}
				if _, ok := h.state.Blockers["approval-blocker"]; !ok {
					t.Fatal("failed cancellation proof mutated the pending approval blocker")
				}
			}
			if stage == "ready" {
				h.issue(Command{Kind: CommandReadyTool, Tool: ToolInput{ToolID: "tool-1", Generation: 1, ArgsRef: "raw-args-ref"}})
			}
			batch := h.issue(Command{Kind: CommandCancelTool, Transition: transition})
			wantKinds := []EventKind{EventToolResultRecorded}
			if stage == "awaiting approval" {
				wantKinds = []EventKind{EventToolCheckReset, EventToolResultRecorded}
			}
			if len(batch.Events) != len(wantKinds) {
				t.Fatalf("cancellation emitted %d events, want %d: %#v", len(batch.Events), len(wantKinds), batch.Events)
			}
			for i, kind := range wantKinds {
				if batch.Events[i].Kind != kind {
					t.Fatalf("cancellation event %d is %v, want %v", i, batch.Events[i].Kind, kind)
				}
			}
			result := batch.Events[len(batch.Events)-1]
			if result.ToolID != "tool-1" || result.AttemptID != "tool-attempt-1" || result.Ref != "user-cancel-proof" || result.ReceiptRef != "cancel-receipt" || result.Disposition != ResultCancelled || result.Terminal != true || result.Exposure != ExposureNone || result.Token != 0 || result.Fence != 0 {
				t.Fatal("pre-dispatch cancellation did not preserve exact cancellation proof without inventing execution evidence")
			}
			if result.Disposition == ResultDenied || result.Disposition == ResultRefused {
				t.Fatal("pre-dispatch user cancellation was misclassified as denial or refusal")
			}
			if stage == "awaiting approval" && (batch.Events[0].Generation != 2 || batch.Events[0].BlockerID != "approval-blocker" || batch.Events[0].RequestRef != "approval-request" || batch.Events[0].ReceiptRef != "cancel-receipt") {
				t.Fatal("approval reset did not invalidate the exact blocker/request with cancellation receipt")
			}
			tool, attempt := h.state.Tools["tool-1"], h.state.ToolAttempts["tool-attempt-1"]
			if tool.State != ToolCancelled || tool.Disposition != ResultCancelled || tool.ResultRef != "user-cancel-proof" || attempt.State != AttemptCancelled || len(h.state.Blockers) != 0 {
				t.Fatal("typed pre-dispatch cancellation did not close the tool or clear only its approval blocker")
			}
			step := h.state.Steps[h.state.CurrentStep]
			if step.ResponseRef != "model-response-ref" || len(step.Transcript) != 2 || step.Transcript[0].IsModel != true || step.Transcript[0].ResponseRef != "model-response-ref" || step.Transcript[1].ResultRef != "user-cancel-proof" {
				t.Fatal("cancellation did not preserve the model response and append its ordered terminal fact")
			}
		})
	}
}

func TestDecideOwnershipRecoveryCarriesOldAttemptEvidenceAtomically(t *testing.T) {
	h := newTransitionHarness(t)
	invocationID, attemptID := h.modelInFlight()
	oldToken, oldIncarnation := h.state.AdmissionToken, h.state.Incarnation
	batch := h.issue(Command{Kind: CommandAcquireOwnership, Transition: TransitionInput{
		ExecutorToken: 8, ExecutorIncarnation: "writer-inc-2", ReceiptRef: "ownership-receipt-ref",
		Recoveries: []RecoveryFact{{
			TargetID: invocationID, AttemptID: attemptID, CauseRef: "recovery-cause-ref",
			PartialRef: "partial-response-ref", Usage: UsageUnknown,
			BlockerID: "recovery-blocker-id", PreviousToken: oldToken, PreviousIncarnation: oldIncarnation, Fence: 1,
		}},
	}})
	if batch.WriterToken != 8 || batch.WriterIncarnation != "writer-inc-2" {
		t.Fatal("ownership batch was not written by the new owner")
	}
	if len(batch.Events) != 4 {
		t.Fatalf("recovery command emitted %d facts, want ownership plus interrupt/block facts", len(batch.Events))
	}
	owner, interrupted, cause, blocker := batch.Events[0], batch.Events[1], batch.Events[2], batch.Events[3]
	if owner.Kind != EventRunOwnershipAcquired || owner.PreviousToken != oldToken || owner.PreviousIncarnation != oldIncarnation || owner.Token != 8 || owner.Incarnation != "writer-inc-2" || owner.ReceiptRef != "ownership-receipt-ref" {
		t.Fatal("ownership fact lost old owner evidence or new ownership receipt")
	}
	if interrupted.Kind != EventModelRequestInterrupted || interrupted.InvocationID != invocationID || interrupted.AttemptID != attemptID || interrupted.PreviousToken != oldToken || interrupted.PreviousIncarnation != oldIncarnation || interrupted.Token != 8 || interrupted.Incarnation != "writer-inc-2" || interrupted.Fence != 1 || interrupted.ReceiptRef != "ownership-receipt-ref" || interrupted.Ref != "partial-response-ref" {
		t.Fatal("recovery fact did not bind interruption to the previous attempt and new ownership")
	}
	if cause.Kind != EventRunInterrupted || cause.ToolID != attemptID || cause.CauseRef != "recovery-cause-ref" || cause.Interrupt != InterruptEffectUnknown {
		t.Fatalf("recovery interruption pair lost its cause or attempt: %#v", cause)
	}
	if blocker.Kind != EventRunBlockAdded || blocker.BlockerID != "recovery-blocker-id" || blocker.BlockKind != BlockRecoveryAdmission || blocker.Ref != "ownership-receipt-ref" {
		t.Fatal("recovery admission blocker was not included atomically")
	}
	if h.state.AdmissionToken != 8 || h.state.Incarnation != "writer-inc-2" || h.state.ModelAttempts[attemptID].State != AttemptInterrupted || h.state.Interruptions["recovery-cause-ref"].TargetID != attemptID {
		t.Fatal("recovered state does not reflect ownership and ambiguous attempt")
	}
	if _, ok := h.state.Blockers["recovery-blocker-id"]; !ok {
		t.Fatal("recovery blocker was not retained in the projection")
	}
}

func TestDecideRetryPreservesPreviousModelAttemptAndTimerPair(t *testing.T) {
	h := newTransitionHarness(t)
	invocationID, oldAttemptID := h.modelInFlight()
	h.issue(Command{Kind: CommandObserveModelFailure, Transition: TransitionInput{
		TargetID: invocationID, AttemptID: oldAttemptID, Ref: "model-failure-ref", CauseRef: "partial-model-response-ref",
		Usage: UsageUnknown, ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Fence: 1, FailureStage: ModelFailureStream, FailureClass: ModelFailureServer, RetryDisposition: RetryDispositionRetry,
	}})
	scheduled := h.issue(Command{Kind: CommandScheduleModelRetry, Transition: TransitionInput{
		TargetID: invocationID, PreviousAttemptID: oldAttemptID, AttemptID: "model-attempt-2", TimerID: "retry-timer-id", BlockerID: "retry-blocker-id",
		Ref: "retry-policy-ref", Generation: 1, RetryOrdinal: 1, RetryLimit: 5, PolicyVersion: 1, DeadlineUnixMilli: 1000,
	}})
	if len(scheduled.Events) != 3 || scheduled.Events[0].Kind != EventModelRetryScheduled || scheduled.Events[1].Kind != EventModelAttemptPlanned || scheduled.Events[2].Kind != EventTimerScheduled || scheduled.Events[0].PreviousAttemptID != oldAttemptID || scheduled.Events[0].InvocationID != invocationID || scheduled.Events[0].TimerID != "retry-timer-id" {
		t.Fatal("retry schedule was not an atomic intent/attempt/timer batch")
	}
	elapsed := h.issue(Command{Kind: CommandObserveTimerElapsed, Transition: TransitionInput{
		TimerID: "retry-timer-id", Generation: 1, Ref: "retry-elapsed-ref", ObservedUnixMilli: 1000,
	}})
	if len(elapsed.Events) != 1 || elapsed.Events[0].Kind != EventTimerElapsed || elapsed.Events[0].CauseRef != "retry-elapsed-ref" {
		t.Fatal("timer elapsed observation did not match the scheduled retry")
	}
	if len(h.state.Models[invocationID].AttemptIDs) != 2 || h.state.Models[invocationID].AttemptIDs[0] != oldAttemptID || h.state.Models[invocationID].AttemptIDs[1] != "model-attempt-2" || h.state.ModelAttempts[oldAttemptID].State != AttemptFailed || h.state.ModelAttempts["model-attempt-2"].State != AttemptPlanned {
		t.Fatal("retry projection overwrote or detached the prior failed attempt")
	}
	if h.state.Timers["retry-timer-id"].Pending || !h.state.Timers["retry-timer-id"].Elapsed {
		t.Fatal("elapsed retry timer must remain available until dispatch")
	}
	if _, exists := h.state.Blockers["retry-blocker-id"]; exists {
		t.Fatal("retry timer blocker remained after typed plan")
	}
	h.issue(Command{Kind: CommandDispatchModel, Model: ModelInput{InvocationID: invocationID, AttemptID: "model-attempt-2", RequestRef: "model-retry-dispatch", ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Fence: 2}})
	h.issue(Command{Kind: CommandObserveModelStream, Model: ModelInput{InvocationID: invocationID, AttemptID: "model-attempt-2", ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Fence: 2}})
	h.issue(Command{Kind: CommandObserveModelResponse, ModelResponse: ModelResponseInput{InvocationID: invocationID, AttemptID: "model-attempt-2", ResponseRef: "model-retry-response", Usage: UsageKnown, UsageRef: "selected-usage-ref", Fence: 2, ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1"}})
	h.issue(Command{Kind: CommandFinishStep, FinishStep: FinishStepInput{Next: NextEnd, Continuation: ContinuationNaturalEnd}})
	h.issue(Command{Kind: CommandFinishRun, FinishRun: FinishRunInput{Outcome: OutcomeCompleted}})
	selectedBefore := h.state.Models[invocationID]
	transcriptBefore := append([]TranscriptItem(nil), h.state.Transcript...)
	lateOldUsage := h.issue(Command{Kind: CommandReconcileModelUsage, Transition: TransitionInput{TargetID: invocationID, AttemptID: oldAttemptID, Usage: UsageKnown, Ref: "late-old-attempt-usage"}})
	if len(lateOldUsage.Events) != 1 || lateOldUsage.Events[0].Kind != EventModelUsageReconciled || h.state.ModelAttempts[oldAttemptID].Usage != (UsageFact{Status: UsageKnown, Ref: "late-old-attempt-usage"}) {
		t.Fatal("late usage for the historical failed attempt was not retained on that attempt")
	}
	selectedAfterOldUsage := h.state.Models[invocationID]
	if selectedAfterOldUsage.Usage != selectedBefore.Usage || selectedAfterOldUsage.SelectedAttempt != selectedBefore.SelectedAttempt || selectedAfterOldUsage.ResponseRef != selectedBefore.ResponseRef || !reflect.DeepEqual(h.state.Transcript, transcriptBefore) {
		t.Fatal("historical attempt reconciliation changed the selected response or transcript projection")
	}
	selectedUsage := h.issue(Command{Kind: CommandReconcileModelUsage, Transition: TransitionInput{TargetID: invocationID, AttemptID: "model-attempt-2", Usage: UsageKnown, Ref: "selected-attempt-reconciled-usage"}})
	if len(selectedUsage.Events) != 1 || selectedUsage.Events[0].Kind != EventModelUsageReconciled || h.state.Models[invocationID].Usage != (UsageFact{Status: UsageKnown, Ref: "selected-attempt-reconciled-usage"}) || h.state.Models[invocationID].SelectedAttempt != "model-attempt-2" || h.state.Models[invocationID].ResponseRef != "model-retry-response" || !reflect.DeepEqual(h.state.Transcript, transcriptBefore) {
		t.Fatal("selected attempt reconciliation did not update only invocation usage projection")
	}
}

func TestDecideTimeWindowOpensAndClosesWithDeadlineTimer(t *testing.T) {
	h := newTransitionHarness(t)
	h.issue(Command{Kind: CommandAcceptRun, AcceptRun: AcceptRunInput{InputRef: "protected-input-ref", Origin: OriginUser}})
	h.issue(Command{Kind: CommandAdmitRun, Admission: AdmissionInput{Token: 7, Incarnation: "writer-inc-1"}})
	opened := h.issue(Command{Kind: CommandOpenTimeWindow, Transition: TransitionInput{
		WindowID: "budget-window-id", TimerID: "budget-window-timer", Ref: "window-open-ref", Generation: 1,
		Usage: UsageReserved, ExecutorIncarnation: "writer-inc-1",
	}})
	if len(opened.Events) != 2 || opened.Events[0].Kind != EventTimerScheduled || opened.Events[1].Kind != EventTimeBudgetWindowOpened {
		t.Fatalf("opening did not atomically create its deadline timer: %#v", opened.Events)
	}
	if opened.Events[0].TimerID != "budget-window-timer" || opened.Events[0].WindowID != "budget-window-id" || opened.Events[0].Purpose != TimerRunDeadline || opened.Events[1].TimerID != "budget-window-timer" || opened.Events[1].Usage != UsageReserved {
		t.Fatal("window and deadline timer are not paired")
	}
	window := h.state.Windows["budget-window-id"]
	if !window.Open || window.TimerID != "budget-window-timer" || window.Generation != 1 || h.state.Timers["budget-window-timer"].TargetID != "budget-window-id" {
		t.Fatal("open window projection lost its timer relationship")
	}
	closed := h.issue(Command{Kind: CommandCloseTimeWindow, Transition: TransitionInput{
		WindowID: "budget-window-id", TimerID: "budget-window-timer", Ref: "window-close-ref", Generation: 1, Covered: true,
	}})
	if len(closed.Events) != 2 || closed.Events[0].Kind != EventTimeBudgetWindowClosed || closed.Events[1].Kind != EventTimerCancelled || closed.Events[1].CauseRef != "window-close-ref" {
		t.Fatalf("closing did not atomically close window and cancel timer: %#v", closed.Events)
	}
	window = h.state.Windows["budget-window-id"]
	if window.Open || !window.Covered || h.state.Timers["budget-window-timer"].Pending {
		t.Fatal("closed window or timer projection remains active")
	}
	closure := h.issue(closureCommandFor(h.state, "closure-ref", CommandConfirmRunClosure, ClosureInput{
		PreviewRef: "closure-preview", ReceiptRef: "closure-receipt", Actor: ActorUser, Revision: h.state.Revision,
		PreviewVersion: CurrentClosurePreviewVersion, UnavailableReason: UnavailablePayloadMissing,
		PreviousContextRevision: h.state.ContextRevision, ContextRevision: h.state.ContextRevision + 1,
		ContextRequirement: ContextRequirementFreshRequired, CauseEventRef: closed.Events[1].ID,
	}))
	if len(closure.Events) != 2 || closure.Events[0].Kind != EventRunClosureConfirmed || closure.Events[1].Kind != EventRunFinished || closure.Events[1].Outcome != OutcomeAbandoned || !h.state.Finished || !h.state.ClosureConfirmed {
		t.Fatal("closure command did not produce one atomic terminal batch")
	}
	if closure.Events[0].PreviewMode != ClosurePreviewMetadataOnly || closure.Events[0].PreviewVersion != CurrentClosurePreviewVersion || closure.Events[0].UnavailableReason != UnavailablePayloadMissing || closure.Events[0].ContextRequirement != ContextRequirementFreshRequired || closure.Events[0].CauseEventRef != closed.Events[1].ID || len(closure.Events[0].ClosureTargets) != 0 {
		t.Fatal("closure confirmation omitted its versioned metadata-only preview and unavailable-context evidence")
	}
}

func TestDecideOwnershipRecoveryCarriesExplicitOldAttemptAndBlockerFacts(t *testing.T) {
	h := newTransitionHarness(t)
	h.issue(Command{Kind: CommandAcceptRun, AcceptRun: AcceptRunInput{InputRef: "input", Origin: OriginUser, InitialStepLimit: 3}})
	h.issue(Command{Kind: CommandAdmitRun, Admission: AdmissionInput{Token: 7, Incarnation: "writer-inc-1"}})
	h.issue(Command{Kind: CommandStartStep, Step: StepInput{StepID: "step-1"}})
	h.issue(Command{Kind: CommandPlanModel, Model: ModelInput{InvocationID: "model-1", AttemptID: "attempt-1", RequestRef: "request"}})
	h.issue(Command{Kind: CommandDispatchModel, Model: ModelInput{InvocationID: "model-1", AttemptID: "attempt-1", RequestRef: "dispatch", ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Fence: 5}})
	h.issue(Command{Kind: CommandObserveModelStream, Model: ModelInput{InvocationID: "model-1", AttemptID: "attempt-1", ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Fence: 5}})
	cmd := Command{Kind: CommandAcquireOwnership, Transition: TransitionInput{ExecutorToken: 8, ExecutorIncarnation: "writer-inc-2", ReceiptRef: "recovery-receipt", Recoveries: []RecoveryFact{{TargetID: "model-1", AttemptID: "attempt-1", CauseRef: "interrupted-cause", PartialRef: "partial-ref", Usage: UsageUnknown, BlockerID: "recovery-blocker", PreviousToken: 7, PreviousIncarnation: "writer-inc-1", Fence: 5}}}}
	batch := h.issue(cmd)
	if len(batch.Events) != 4 || batch.Events[0].Kind != EventRunOwnershipAcquired || batch.Events[1].Kind != EventModelRequestInterrupted || batch.Events[2].Kind != EventRunInterrupted || batch.Events[3].Kind != EventRunBlockAdded {
		t.Fatalf("recovery batch omitted explicit disposition/blocker: %#v", batch.Events)
	}
	if h.state.AdmissionToken != 8 || h.state.ModelAttempts["attempt-1"].State != AttemptInterrupted || h.state.Blockers["recovery-blocker"].Kind != BlockRecoveryAdmission {
		t.Fatal("recovery batch did not project the new writer and stale-work blocker")
	}
}

func TestDecideBudgetExtensionIsTypedAndUserScoped(t *testing.T) {
	h := newTransitionHarness(t)
	h.issue(Command{Kind: CommandAcceptRun, AcceptRun: AcceptRunInput{InputRef: "input", Origin: OriginUser, InitialStepLimit: 1}})
	h.issue(Command{Kind: CommandAdmitRun, Admission: AdmissionInput{Token: 7, Incarnation: "writer-inc-1"}})
	h.issue(Command{Kind: CommandStartStep, Step: StepInput{StepID: "step-1"}})
	h.issue(Command{Kind: CommandPlanModel, Model: ModelInput{InvocationID: "model-1", AttemptID: "attempt-1", RequestRef: "request"}})
	h.issue(Command{Kind: CommandDispatchModel, Model: ModelInput{InvocationID: "model-1", AttemptID: "attempt-1", RequestRef: "dispatch", ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Fence: 1}})
	h.issue(Command{Kind: CommandObserveModelStream, Model: ModelInput{InvocationID: "model-1", AttemptID: "attempt-1", ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Fence: 1}})
	h.issue(Command{Kind: CommandObserveModelResponse, ModelResponse: ModelResponseInput{InvocationID: "model-1", AttemptID: "attempt-1", ResponseRef: "response", Usage: UsageKnown, UsageRef: "usage", Fence: 1, ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1"}})
	h.issue(Command{Kind: CommandFinishStep, FinishStep: FinishStepInput{Next: NextWait, Continuation: ContinuationStepBudget}})
	if h.state.Lifecycle != Waiting || !hasWaitReason(h.state, BlockStepBudget) {
		t.Fatal("step budget exhaustion did not project an explicit wait")
	}
	batch := h.issue(Command{Kind: CommandExtendBudget, BudgetExtension: BudgetExtensionInput{Kind: BudgetSteps, By: 2, Actor: ActorUser}})
	if len(batch.Events) != 1 || batch.Events[0].Kind != EventBudgetExtended || batch.Events[0].BudgetKind != BudgetSteps || batch.Events[0].BudgetBy != 2 || h.state.StepBudgetExtended != 2 || h.state.Lifecycle != Running {
		t.Fatal("typed user extension did not clear the exact step budget wait")
	}
}

func TestDecideExpiredRunDeadlineChargesWithoutAttemptAndStops(t *testing.T) {
	h := newTransitionHarness(t)
	h.issue(Command{Kind: CommandAcceptRun, AcceptRun: AcceptRunInput{InputRef: "input", Origin: OriginUser}})
	h.issue(Command{Kind: CommandAdmitRun, Admission: AdmissionInput{Token: 7, Incarnation: "writer-inc-1"}})
	h.issue(Command{Kind: CommandOpenTimeWindow, Transition: TransitionInput{WindowID: "window-1", TimerID: "deadline-1", Ref: "window-open", Generation: 1, Usage: UsageReserved, ExecutorIncarnation: "writer-inc-1"}})
	batch := h.issue(Command{Kind: CommandExpireTimeWindow, Transition: TransitionInput{
		WindowID: "window-1", TimerID: "deadline-1", Generation: 1, Ref: "charge-proof", CauseRef: "deadline-expiry-proof",
		Usage: UsageKnown, BudgetExhausted: true, BudgetKind: BudgetTime, Actor: ActorTimer,
	}})
	if len(batch.Events) != 4 || batch.Events[0].Kind != EventTimerElapsed || batch.Events[1].Kind != EventBudgetObserved || batch.Events[1].WindowID != "window-1" || batch.Events[1].AttemptID != "" || batch.Events[1].Usage != UsageKnown || batch.Events[2].Kind != EventTimeBudgetWindowClosed || batch.Events[3].Kind != EventRunStopRequested {
		t.Fatalf("deadline expiry did not atomically charge/close/stop without an attempt: %#v", batch.Events)
	}
	if h.state.Windows["window-1"].Open || !h.state.Windows["window-1"].Covered || h.state.Timers["deadline-1"].Pending || h.state.PrimaryStop.Reason != StopBudget || h.state.BudgetStopKind != BudgetTime {
		t.Fatal("deadline charge or terminal budget stop was not projected")
	}
	finished := h.issue(Command{Kind: CommandFinishRun, FinishRun: FinishRunInput{Outcome: OutcomeBudgetExceeded}})
	if len(finished.Events) != 1 || !h.state.Finished || h.state.Outcome != OutcomeBudgetExceeded || len(h.state.Steps) != 0 || len(h.state.Tools) != 0 || len(h.state.Models) != 0 {
		t.Fatal("pre-work time budget exhaustion did not finish cleanly without fabricated work")
	}
}

func TestDecidePureReadUnknownCanRetryAndFinishWithoutErasingHistory(t *testing.T) {
	h := newTransitionHarness(t)
	toolID, oldAttempt := h.toolDispatchingWithEffect(EffectPureRead)
	h.issue(Command{Kind: CommandObserveUnknownTool, Transition: TransitionInput{TargetID: toolID, AttemptID: oldAttempt, CauseRef: "timeout-cause", Exposure: ExposureUnknown, ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Fence: 2}})
	abandoned := h.issue(Command{Kind: CommandAbandonTool, Transition: TransitionInput{TargetID: toolID, AttemptID: oldAttempt, Ref: "abandon-ref", ReceiptRef: "abandon-receipt", Exposure: ExposureUnknown, Next: NextContinue}})
	if len(abandoned.Events) != 1 || h.state.ToolAttempts[oldAttempt].State != AttemptAbandoned || !h.state.Interruptions["timeout-cause"].Resolved {
		t.Fatal("pure-read abandonment failed to retain old attempt and resolve interruption")
	}
	timer := h.issue(Command{Kind: CommandScheduleTimer, Transition: TransitionInput{TargetID: toolID, TimerID: "tool-retry-timer", BlockerID: "tool-retry-blocker", Ref: "retry-policy", Purpose: TimerToolRetry, Generation: 2}})
	if len(timer.Events) != 1 || timer.Events[0].Kind != EventTimerScheduled || h.state.Lifecycle != Waiting {
		t.Fatal("tool retry did not schedule its typed waiting timer")
	}
	h.issue(Command{Kind: CommandObserveTimerElapsed, Transition: TransitionInput{TimerID: "tool-retry-timer", Generation: 2, Ref: "retry-elapsed"}})
	planned := h.issue(Command{Kind: CommandPlanToolRetry, Transition: TransitionInput{TargetID: toolID, PreviousAttemptID: oldAttempt, AttemptID: "tool-attempt-2", ReceiptRef: "retry-approval-receipt", TimerID: "tool-retry-timer", Generation: 2}})
	if len(planned.Events) != 1 || planned.Events[0].Kind != EventToolAttemptPlanned || planned.Events[0].PreviousAttemptID != oldAttempt || planned.Events[0].AttemptID != "tool-attempt-2" {
		t.Fatal("typed pure-read retry plan lost prior attempt provenance")
	}
	h.issue(Command{Kind: CommandPrepareTool, Tool: ToolInput{ToolID: toolID, Generation: 2, Ref: "second-check"}})
	h.issue(Command{Kind: CommandRequestToolApproval, Transition: TransitionInput{TargetID: toolID, Generation: 2, Ref: "args-protected-ref", RequestRef: "retry-approval-request", BlockerID: "retry-approval-blocker"}})
	h.issue(Command{Kind: CommandResolveToolApproval, Transition: TransitionInput{TargetID: toolID, AttemptID: oldAttempt, Generation: 2, RequestRef: "retry-approval-request", Ref: "args-protected-ref", ReceiptRef: "retry-approval-receipt", Disposition: ResultSucceeded}})
	h.issue(Command{Kind: CommandDispatchTool, Tool: ToolInput{ToolID: toolID, AttemptID: "tool-attempt-2", Generation: 2, ReceiptRef: "retry-approval-receipt", Ref: "second-dispatch", ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Fence: 3}})
	h.issue(Command{Kind: CommandObserveToolResult, ToolResult: ToolResultInput{ToolID: toolID, AttemptID: "tool-attempt-2", ResultRef: "second-result", Disposition: ResultSucceeded, Exposure: ExposureKnown, ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Fence: 3}})
	h.issue(Command{Kind: CommandFinishStep, FinishStep: FinishStepInput{Next: NextEnd, Continuation: ContinuationNaturalEnd}})
	h.issue(Command{Kind: CommandFinishRun, FinishRun: FinishRunInput{Outcome: OutcomeCompleted}})
	if h.state.ToolAttempts[oldAttempt].State != AttemptAbandoned || h.state.ToolAttempts[oldAttempt].Exposure != ExposureUnknown || h.state.ToolAttempts["tool-attempt-2"].State != AttemptCompleted || h.state.Tools[toolID].Disposition != ResultSucceeded || h.state.Outcome != OutcomeCompleted {
		t.Fatal("successful retry erased historical unknown exposure or failed to finish")
	}
}

func TestDecideClearsRecoveryBlockAndInterruptionWithSeparateTypedProofs(t *testing.T) {
	h := newTransitionHarness(t)
	invocationID, attemptID := h.modelInFlight()
	h.issue(Command{Kind: CommandAcquireOwnership, Transition: TransitionInput{ExecutorToken: 8, ExecutorIncarnation: "writer-inc-2", ReceiptRef: "owner-receipt", Recoveries: []RecoveryFact{{TargetID: invocationID, AttemptID: attemptID, CauseRef: "recovery-cause", PartialRef: "partial", Usage: UsageUnknown, BlockerID: "recovery-blocker", PreviousToken: 7, PreviousIncarnation: "writer-inc-1", Fence: 1}}}})
	clearedBlock := h.issue(Command{Kind: CommandClearRunBlock, Transition: TransitionInput{BlockerID: "recovery-blocker", CauseRef: "block-clear-proof", Ref: "block-clear-receipt"}})
	if len(clearedBlock.Events) != 1 || clearedBlock.Events[0].Kind != EventRunBlockCleared {
		t.Fatal("recovery blocker was not cleared through its typed command")
	}
	clearedRecovery := h.issue(Command{Kind: CommandClearRecovery, Transition: TransitionInput{CauseRef: "recovery-cause", Ref: "resolution-proof", ReceiptRef: "resolution-receipt", Actor: ActorUser, RecoveryDisposition: RecoveryDispositionReconciled}})
	if len(clearedRecovery.Events) != 1 || clearedRecovery.Events[0].Kind != EventRunRecoveryCleared || clearedRecovery.Events[0].RecoveryDisposition != RecoveryDispositionReconciled || !h.state.Interruptions["recovery-cause"].Resolved || len(h.state.Blockers) != 0 {
		t.Fatal("recovery interruption did not require and retain a separate resolution proof")
	}
}

func TestDecideAbandonModelAndObserveAttemptUsageThroughTypedCommands(t *testing.T) {
	h := newTransitionHarness(t)
	invocationID, attemptID := h.modelInFlight()
	h.issue(Command{Kind: CommandAcquireOwnership, Transition: TransitionInput{ExecutorToken: 8, ExecutorIncarnation: "writer-inc-2", ReceiptRef: "owner-receipt", Recoveries: []RecoveryFact{{TargetID: invocationID, AttemptID: attemptID, CauseRef: "model-interruption", PartialRef: "partial-ref", Usage: UsageUnknown, BlockerID: "recovery-blocker", PreviousToken: 7, PreviousIncarnation: "writer-inc-1", Fence: 1}}}})
	h.issue(Command{Kind: CommandClearRunBlock, Transition: TransitionInput{BlockerID: "recovery-blocker", CauseRef: "block-clear", Ref: "block-clear-ref"}})
	abandoned := h.issue(Command{Kind: CommandAbandonModel, Transition: TransitionInput{TargetID: invocationID, AttemptID: attemptID, Ref: "model-abandon-ref", ReceiptRef: "model-abandon-receipt", Exposure: ExposureUnknown, Next: NextContinue}})
	if len(abandoned.Events) != 1 || abandoned.Events[0].Kind != EventModelAttemptAbandoned || h.state.ModelAttempts[attemptID].State != AttemptAbandoned || !h.state.Interruptions["model-interruption"].Resolved {
		t.Fatal("model abandonment did not resolve the exact interruption and preserve the attempt")
	}
	observed := h.issue(Command{Kind: CommandObserveBudget, Transition: TransitionInput{TargetID: invocationID, AttemptID: attemptID, Usage: UsageKnown, Ref: "attempt-usage-proof"}})
	if len(observed.Events) != 1 || observed.Events[0].Kind != EventBudgetObserved || observed.Events[0].InvocationID != invocationID || observed.Events[0].AttemptID != attemptID || h.state.ModelAttempts[attemptID].Usage != (UsageFact{Status: UsageKnown, Ref: "attempt-usage-proof"}) {
		t.Fatal("typed budget observation did not attach usage to the historical model attempt")
	}
}

func TestDecideTerminalModelFailureAndInvocationFailureAreSeparateFacts(t *testing.T) {
	h := newTransitionHarness(t)
	invocationID, attemptID := h.modelInFlight()
	failed := h.issue(Command{Kind: CommandObserveModelFailure, Transition: TransitionInput{TargetID: invocationID, AttemptID: attemptID, Ref: "terminal-attempt-failure", CauseRef: "partial-body", Usage: UsageUnknown, ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Fence: 1, Terminal: true, FailureStage: ModelFailureStream, FailureClass: ModelFailureServer, RetryDisposition: RetryDispositionFatal}})
	if len(failed.Events) != 1 || h.state.Models[invocationID].State != InvocationFailed {
		t.Fatal("terminal transport failure did not close the invocation")
	}
	annotated := h.issue(Command{Kind: CommandFailModelInvocation, Transition: TransitionInput{TargetID: invocationID, Ref: "invocation-failure-proof"}})
	if len(annotated.Events) != 1 || annotated.Events[0].Kind != EventModelInvocationFailed || h.state.Models[invocationID].FailureRef != "invocation-failure-proof" || h.state.Models[invocationID].Disposition != ResultFailed {
		t.Fatal("invocation failure command did not record the separate terminal disposition")
	}
}

func TestDecideToolReconciliationAndUnknownOutcomeClosure(t *testing.T) {
	t.Run("reconcile known result", func(t *testing.T) {
		h := newTransitionHarness(t)
		toolID, attemptID := h.toolDispatching()
		h.issue(Command{Kind: CommandObserveUnknownTool, Transition: TransitionInput{TargetID: toolID, AttemptID: attemptID, CauseRef: "timeout", Exposure: ExposureUnknown, ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Fence: 2}})
		batch := h.issue(Command{Kind: CommandReconcileTool, Transition: TransitionInput{TargetID: toolID, AttemptID: attemptID, ReceiptRef: "reconciliation-proof", ResultRef: "reconciled-result", Disposition: ResultSucceeded, Exposure: ExposureKnown}})
		if len(batch.Events) != 2 || batch.Events[0].Kind != EventToolReconciled || batch.Events[1].Kind != EventToolResultRecorded || !h.state.Interruptions["timeout"].Resolved || h.state.Tools[toolID].Disposition != ResultSucceeded {
			t.Fatal("typed reconciliation failed to pair its decision/result and clear uncertainty")
		}
	})
	t.Run("user accepts unknown and closes", func(t *testing.T) {
		h := newTransitionHarness(t)
		toolID, attemptID := h.toolDispatching()
		transcript := append([]TranscriptItem(nil), h.state.Transcript...)
		unknown := h.issue(Command{Kind: CommandObserveUnknownTool, Transition: TransitionInput{TargetID: toolID, AttemptID: attemptID, CauseRef: "unknown-close-cause", Exposure: ExposureUnknown, ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Fence: 2}})
		accepted := h.issue(closureCommandFor(h.state, "unknown-close", CommandAcceptUnknownOutcome, ClosureInput{
			PreviewRef: "user-acceptance-proof", ReceiptRef: "user-acceptance-receipt", Revision: h.state.Revision, Actor: ActorUser, PreviewVersion: CurrentClosurePreviewVersion,
			Targets: []ClosureTargetFact{{Kind: ClosureTargetUnknownTool, TargetID: toolID, AttemptID: attemptID, CauseRef: "unknown-close-cause", CauseEventRef: unknown.Events[1].ID, ResultRef: "abandoned-result"}},
		}))
		if len(accepted.Events) != 5 || accepted.Events[0].Kind != EventUnknownAccepted || accepted.Events[1].Kind != EventToolResultRecorded || accepted.Events[1].Disposition != ResultAbandonedUnknown || !h.state.Interruptions["unknown-close-cause"].Accepted || h.state.Tools[toolID].Disposition != ResultAbandonedUnknown || h.state.ToolAttempts[attemptID].State != AttemptAbandoned {
			t.Fatal("unknown acceptance did not atomically pair user decision with its explicit terminal result")
		}
		if !h.state.Finished || h.state.Outcome != OutcomeAbandonedUnknown || h.state.Exposures[toolID] != ExposureUnknown || h.state.ToolAttempts[attemptID].State != AttemptAbandoned || len(h.state.Transcript) != len(transcript)+1 || !reflect.DeepEqual(h.state.Transcript[:len(transcript)], transcript) || h.state.Transcript[len(transcript)].ResultRef != "abandoned-result" || !h.state.Steps[h.state.CurrentStep].Aborted || h.state.Models[h.state.Steps[h.state.CurrentStep].ModelID].Disposition != ResultSucceeded {
			t.Fatalf("user closure did not preserve model history and explicit unknown exposure: finished=%v outcome=%v exposure=%v transcript=%#v step=%#v", h.state.Finished, h.state.Outcome, h.state.Exposures[toolID], h.state.Transcript, h.state.Steps[h.state.CurrentStep])
		}
	})
}

func TestDecideClosureCanAtomicallyResolveAcceptedUnknownOutcome(t *testing.T) {
	h := newTransitionHarness(t)
	toolID, attemptID := h.toolDispatching()
	unknown := h.issue(Command{Kind: CommandObserveUnknownTool, Transition: TransitionInput{TargetID: toolID, AttemptID: attemptID, CauseRef: "closure-interruption", Exposure: ExposureUnknown, ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Fence: 2}})
	batch := h.issue(closureCommandFor(h.state, "closure-ack-ref", CommandAcceptUnknownOutcome, ClosureInput{
		PreviewRef: "closure-preview", ReceiptRef: "closure-receipt", Actor: ActorUser, Revision: h.state.Revision, PreviewVersion: CurrentClosurePreviewVersion,
		Targets: []ClosureTargetFact{{Kind: ClosureTargetUnknownTool, TargetID: toolID, AttemptID: attemptID, CauseRef: "closure-interruption", CauseEventRef: unknown.Events[1].ID, ResultRef: "closure-result-ref"}},
	}))
	want := []EventKind{EventUnknownAccepted, EventToolResultRecorded, EventRunRecoveryCleared, EventStepAborted, EventRunFinished}
	if len(batch.Events) != len(want) {
		t.Fatalf("unknown closure batch has %d events, want %d: %#v", len(batch.Events), len(want), batch.Events)
	}
	for i, kind := range want {
		if batch.Events[i].Kind != kind {
			t.Fatalf("unknown closure event %d is %v, want %v", i, batch.Events[i].Kind, kind)
		}
	}
	if batch.Events[0].PreviewMode != ClosurePreviewExact || batch.Events[0].PreviewVersion != CurrentClosurePreviewVersion || batch.Events[0].Ref != "closure-preview" || batch.Events[1].AttemptID != attemptID || batch.Events[1].Ref != "closure-result-ref" || batch.Events[1].Disposition != ResultAbandonedUnknown || batch.Events[2].CauseRef != "closure-interruption" || batch.Events[2].RecoveryDisposition != RecoveryDispositionAcceptedUnknown || !h.state.Interruptions["closure-interruption"].Accepted || !h.state.Steps[h.state.CurrentStep].Aborted || h.state.Outcome != OutcomeAbandonedUnknown || h.state.ToolAttempts[attemptID].State != AttemptAbandoned || h.state.Models[h.state.Steps[h.state.CurrentStep].ModelID].Disposition != ResultSucceeded {
		t.Fatalf("atomic unknown closure lost exact attempt, interruption, or aborted-step provenance: events=%#v intr=%#v step=%#v outcome=%v attempt=%#v model=%#v", batch.Events, h.state.Interruptions["closure-interruption"], h.state.Steps[h.state.CurrentStep], h.state.Outcome, h.state.ToolAttempts[attemptID], h.state.Models[h.state.Steps[h.state.CurrentStep].ModelID])
	}
}

func TestDecideAbortAfterResponsePreservesModelAndTerminalToolTranscript(t *testing.T) {
	h := newTransitionHarness(t)
	toolID, attemptID := h.toolDispatching()
	responseRef := h.state.Steps[h.state.CurrentStep].ResponseRef
	h.issue(Command{Kind: CommandCancelTool, Transition: TransitionInput{TargetID: toolID, AttemptID: attemptID, Ref: "cancelled-tool-ref", Terminal: true, ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Fence: 2}})
	transcript := append([]TranscriptItem(nil), h.state.Transcript...)
	aborted := h.issue(Command{Kind: CommandAbortStep, Transition: TransitionInput{Ref: "abort-after-response"}})
	step := h.state.Steps[h.state.CurrentStep]
	if len(aborted.Events) != 1 || aborted.Events[0].Kind != EventStepAborted || !step.Aborted || step.Open || step.ResponseRef != responseRef || h.state.Models[step.ModelID].ResponseRef != responseRef || !reflect.DeepEqual(h.state.Transcript, transcript) {
		t.Fatal("aborting after response erased committed model/terminal-tool evidence or marked normal completion")
	}
}

func TestDecideModelUnknownAcceptanceAndPostFinishUsageReconciliation(t *testing.T) {
	t.Run("accept unknown", func(t *testing.T) {
		h := newTransitionHarness(t)
		invocationID, attemptID := h.modelInFlight()
		recovery := h.issue(Command{Kind: CommandAcquireOwnership, Transition: TransitionInput{ExecutorToken: 8, ExecutorIncarnation: "writer-inc-2", ReceiptRef: "model-recovery-receipt", Recoveries: []RecoveryFact{{TargetID: invocationID, AttemptID: attemptID, CauseRef: "model-unknown-cause", PartialRef: "partial-response", Usage: UsageUnknown, BlockerID: "model-recovery-blocker", PreviousToken: 7, PreviousIncarnation: "writer-inc-1", Fence: 1}}}})
		causeEvent := recovery.Events[2].ID
		cmd := closureCommandFor(h.state, "model-user-acceptance", CommandAcceptUnknownOutcome, ClosureInput{PreviewRef: "model-preview", ReceiptRef: "model-user-receipt", PreviewVersion: CurrentClosurePreviewVersion, Revision: h.state.Revision, Actor: ActorUser, Targets: []ClosureTargetFact{{Kind: ClosureTargetUnknownModel, TargetID: invocationID, AttemptID: attemptID, CauseRef: "model-unknown-cause", CauseEventRef: causeEvent}}})
		batch := h.issue(cmd)
		if batch.Events[0].Kind != EventUnknownAccepted || !batch.Events[0].IsModel || !h.state.Finished || h.state.Outcome != OutcomeAbandonedUnknown || h.state.Models[invocationID].State != InvocationAbandoned || h.state.Exposures[attemptID] != ExposureUnknown {
			t.Fatal("typed model unknown acceptance lost user acknowledgement or exposure state")
		}
	})
	t.Run("reconcile usage after finish", func(t *testing.T) {
		h := newTransitionHarness(t)
		invocationID, attemptID := h.modelInFlight()
		h.issue(Command{Kind: CommandObserveModelFailure, Transition: TransitionInput{TargetID: invocationID, AttemptID: attemptID, Ref: "model-failed", CauseRef: "partial", Usage: UsageUnknown, ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Fence: 1, Terminal: true, FailureStage: ModelFailureStream, FailureClass: ModelFailureServer, RetryDisposition: RetryDispositionFatal}})
		h.issue(Command{Kind: CommandFailModelInvocation, Transition: TransitionInput{TargetID: invocationID, Ref: "invocation-failed"}})
		h.issue(Command{Kind: CommandAbortStep, Transition: TransitionInput{Ref: "failed-step-abort"}})
		h.issue(Command{Kind: CommandRequestStop, Stop: StopInput{Reason: StopFatalError, Mode: StopTerminal, Ref: "fatal-stop", Actor: ActorSystem}})
		h.issue(Command{Kind: CommandFinishRun, FinishRun: FinishRunInput{Outcome: OutcomeFailed}})
		beforeOutcome, beforeTranscript := h.state.Outcome, append([]TranscriptItem(nil), h.state.Transcript...)
		batch := h.issue(Command{Kind: CommandReconcileModelUsage, Transition: TransitionInput{TargetID: invocationID, AttemptID: attemptID, Usage: UsageKnown, Ref: "late-usage-proof"}})
		if len(batch.Events) != 1 || batch.Events[0].Kind != EventModelUsageReconciled || !h.state.Finished || h.state.Outcome != beforeOutcome || !reflect.DeepEqual(h.state.Transcript, beforeTranscript) || h.state.ModelAttempts[attemptID].Usage != (UsageFact{Status: UsageKnown, Ref: "late-usage-proof"}) {
			t.Fatal("late usage reconciliation changed terminal outcome/transcript or failed to update attempt fact")
		}
	})
}
