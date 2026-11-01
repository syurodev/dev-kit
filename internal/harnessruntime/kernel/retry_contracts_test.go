package kernel

import (
	"reflect"
	"testing"
)

func retryReadyHarness(t *testing.T) (*transitionHarness, ID, ID) {
	t.Helper()
	h := newTransitionHarness(t)
	h.issue(Command{Kind: CommandAcceptRun, AcceptRun: AcceptRunInput{
		InputRef: "protected-input-ref", Origin: OriginUser, InitialStepLimit: 4,
		RetryBudgetLimit: 2, ModelRetryPolicyVersion: 7,
	}})
	h.issue(Command{Kind: CommandAdmitRun, Admission: AdmissionInput{Token: 7, Incarnation: "retry-inc-1"}})
	h.issue(Command{Kind: CommandStartStep, Step: StepInput{StepID: "retry-step-1"}})
	h.issue(Command{Kind: CommandPlanModel, Model: ModelInput{InvocationID: "retry-invocation", AttemptID: "retry-attempt-1", RequestRef: "protected-request-ref"}})
	h.issue(Command{Kind: CommandDispatchModel, Model: ModelInput{InvocationID: "retry-invocation", AttemptID: "retry-attempt-1", RequestRef: "dispatch-ref", ExecutorToken: 7, ExecutorIncarnation: "retry-inc-1", Fence: 1}})
	h.issue(Command{Kind: CommandObserveModelStream, Model: ModelInput{InvocationID: "retry-invocation", AttemptID: "retry-attempt-1", ExecutorToken: 7, ExecutorIncarnation: "retry-inc-1", Fence: 1}})
	h.issue(Command{Kind: CommandObserveModelFailure, Transition: TransitionInput{
		TargetID: "retry-invocation", AttemptID: "retry-attempt-1", Ref: "failure-ref", CauseRef: "partial-ref", Usage: UsageReserved, ReceiptRef: "usage-reservation-ref",
		ExecutorToken: 7, ExecutorIncarnation: "retry-inc-1", Fence: 1,
		FailureStage: ModelFailureStream, FailureClass: ModelFailureRateLimit, RetryDisposition: RetryDispositionRetry,
	}})
	return h, "retry-invocation", "retry-attempt-1"
}

func retryStreamingHarness(t *testing.T) (*transitionHarness, ID, ID) {
	t.Helper()
	h := newTransitionHarness(t)
	h.issue(Command{Kind: CommandAcceptRun, AcceptRun: AcceptRunInput{InputRef: "stream-input", Origin: OriginUser, RetryBudgetLimit: 2, ModelRetryPolicyVersion: 7}})
	h.issue(Command{Kind: CommandAdmitRun, Admission: AdmissionInput{Token: 7, Incarnation: "retry-inc-1"}})
	h.issue(Command{Kind: CommandStartStep, Step: StepInput{StepID: "stream-step"}})
	h.issue(Command{Kind: CommandPlanModel, Model: ModelInput{InvocationID: "stream-inv", AttemptID: "stream-attempt", RequestRef: "stream-request"}})
	h.issue(Command{Kind: CommandDispatchModel, Model: ModelInput{InvocationID: "stream-inv", AttemptID: "stream-attempt", RequestRef: "stream-dispatch", ExecutorToken: 7, ExecutorIncarnation: "retry-inc-1", Fence: 1}})
	h.issue(Command{Kind: CommandObserveModelStream, Model: ModelInput{InvocationID: "stream-inv", AttemptID: "stream-attempt", ExecutorToken: 7, ExecutorIncarnation: "retry-inc-1", Fence: 1}})
	return h, "stream-inv", "stream-attempt"
}

func decideRetryWithoutMutation(h *transitionHarness, cmd Command) error {
	h.t.Helper()
	h.number++
	if cmd.ID == "" {
		cmd.ID = ID("retry-rejected-command")
	}
	cmd.ExpectedRevision = h.state.Revision
	cmd.RunID, cmd.SessionID = h.state.RunID, h.state.SessionID
	if h.state.Admitted {
		cmd.WriterToken, cmd.WriterIncarnation = h.state.AdmissionToken, h.state.Incarnation
	}
	_, err := Decide(h.state, cmd)
	return err
}

func retryScheduleCommand(inv, prev, attempt ID, ordinal uint32) Command {
	return Command{Kind: CommandScheduleModelRetry, Transition: TransitionInput{
		TargetID: inv, PreviousAttemptID: prev, AttemptID: attempt, TimerID: ID("timer-" + string(attempt)), BlockerID: ID("blocker-" + string(attempt)),
		Ref: "retry-policy-ref", Generation: 1, RetryOrdinal: ordinal, RetryLimit: 2, PolicyVersion: 7, DeadlineUnixMilli: 1_800_000_000_000,
	}}
}

func TestRetryContractAtomicPlanChargeAndDispatchFence(t *testing.T) {
	h, invID, oldID := retryReadyHarness(t)
	before := h.state
	scheduled := h.issue(retryScheduleCommand(invID, oldID, "retry-attempt-2", 1))
	if len(scheduled.Events) != 3 || scheduled.Events[0].Kind != EventModelRetryScheduled || scheduled.Events[1].Kind != EventModelAttemptPlanned || scheduled.Events[2].Kind != EventTimerScheduled {
		t.Fatalf("retry intent must commit as ordered scheduled/planned/timer triple, got %#v", scheduled.Events)
	}
	inv, old, next := h.state.Models[invID], h.state.ModelAttempts[oldID], h.state.ModelAttempts["retry-attempt-2"]
	if inv.RetryCount != 1 || inv.RetryLimit != 2 || inv.RetryPolicyVersion != 7 || len(inv.AttemptIDs) != 2 || next.State != AttemptPlanned || next.Usage.Status != UsageUnknown || next.Token != 0 || next.RetryTimerID != "timer-retry-attempt-2" || next.RetryGeneration != 1 || old.State != AttemptFailed || old.Usage != (UsageFact{Status: UsageReserved, Ref: "usage-reservation-ref"}) {
		t.Fatal("retry scheduling did not charge once, preserve the prior reservation, and create a planned unreserved attempt")
	}
	if h.state.Lifecycle != Waiting || !h.state.Timers["timer-retry-attempt-2"].Pending || h.state.Timers["timer-retry-attempt-2"].DeadlineUnixMilli != 1_800_000_000_000 || h.state.Blockers["blocker-retry-attempt-2"].Kind != BlockRetryTimer {
		t.Fatal("retry schedule did not persist the timer wait")
	}

	// Exact command replay remains a receipt lookup; changing its payload under the same ID is a conflict.
	replayed, err := Decide(h.state, scheduled.Intent)
	if err != nil || !reflect.DeepEqual(replayed, scheduled) {
		t.Fatalf("exact retry command replay changed its receipt: %v", err)
	}
	conflict := scheduled.Intent
	conflict.Transition.DeadlineUnixMilli++
	if _, err := Decide(h.state, conflict); err == nil {
		t.Fatal("same retry command ID accepted a different deadline")
	}

	// A batch with the right events in a different order is not a valid replay.
	badOrder := scheduled
	badOrder.CommandID = "retry-wrong-order"
	badOrder.Intent.ID = badOrder.CommandID
	badOrder.ExpectedRevision = before.Revision
	badOrder.Events = append([]Event(nil), scheduled.Events...)
	badOrder.Events[0], badOrder.Events[1] = badOrder.Events[1], badOrder.Events[0]
	if _, err := Evolve(before, scheduled); err != nil {
		t.Fatalf("valid retry batch did not establish the positive replay control: %v", err)
	}
	if _, err := Evolve(before, badOrder); err == nil {
		t.Fatal("replay accepted an incorrectly ordered retry batch")
	}
	missingTimer := scheduled
	missingTimer.CommandID, missingTimer.Intent.ID = "retry-missing-timer", "retry-missing-timer"
	missingTimer.Events = append([]Event(nil), scheduled.Events[:2]...)
	if _, err := Evolve(before, missingTimer); err == nil {
		t.Fatal("replay accepted a scheduled/planned retry without its durable timer frame")
	}
	extraEvent := scheduled
	extraEvent.CommandID, extraEvent.Intent.ID = "retry-with-extra-event", "retry-with-extra-event"
	extra := Event{ID: "extra-retry-event", Kind: EventRunBlockAdded, ProtocolVersion: CurrentProtocolVersion, RunID: before.RunID, SessionID: before.SessionID, BlockerID: "unrelated-blocker", BlockKind: BlockHostPaused, Ref: "extra-ref"}
	extraEvent.Events = append(append([]Event(nil), scheduled.Events...), extra)
	if _, err := Evolve(before, extraEvent); err == nil {
		t.Fatal("replay accepted an extra event alongside the canonical retry triple")
	}
	nonRetryFrames := scheduled
	nonRetryFrames.CommandID, nonRetryFrames.Intent.ID = "retry-intent-without-retry-events", "retry-intent-without-retry-events"
	nonRetryFrames.Events = append([]Event(nil), scheduled.Events...)
	for i := range nonRetryFrames.Events {
		nonRetryFrames.Events[i].Kind = EventRunBlockAdded
		nonRetryFrames.Events[i].BlockerID = ID("unrelated-blocker-" + string(rune('1'+i)))
		nonRetryFrames.Events[i].BlockKind = BlockHostPaused
		nonRetryFrames.Events[i].Ref = "unrelated-block-ref"
		nonRetryFrames.Events[i].InvocationID, nonRetryFrames.Events[i].AttemptID = "", ""
		nonRetryFrames.Events[i].PreviousAttemptID, nonRetryFrames.Events[i].TimerID = "", ""
		nonRetryFrames.Events[i].Purpose, nonRetryFrames.Events[i].DeadlineUnixMilli = 0, 0
	}
	if _, err := Evolve(before, nonRetryFrames); err == nil {
		t.Fatal("replay accepted an unrelated event frame under a retry-schedule intent")
	}
	orphanPlan := scheduled
	orphanPlan.CommandID, orphanPlan.Intent.ID = "orphan-retry-plan", "orphan-retry-plan"
	orphanPlan.Events = []Event{scheduled.Events[1]}
	if _, err := Evolve(before, orphanPlan); err == nil {
		t.Fatal("replay accepted an orphan planned retry attempt without its schedule fact")
	}
	orphanTimer := scheduled
	orphanTimer.CommandID, orphanTimer.Intent.ID = "orphan-retry-timer", "orphan-retry-timer"
	orphanTimer.Events = []Event{scheduled.Events[2]}
	if _, err := Evolve(before, orphanTimer); err == nil {
		t.Fatal("replay accepted a model retry timer without its schedule and planned attempt")
	}

	// Crash/recovery replay keeps the already charged planned attempt and does not schedule a second retry.
	recovered, err := Evolve(before, scheduled)
	if err != nil || !reflect.DeepEqual(recovered, h.state) || recovered.Models[invID].RetryCount != 1 || recovered.ModelAttempts["retry-attempt-2"].State != AttemptPlanned {
		t.Fatalf("replaying the durable retry batch did not reproduce the charged waiting state: %v", err)
	}

	if err := decideRetryWithoutMutation(h, Command{Kind: CommandObserveTimerElapsed, Transition: TransitionInput{TimerID: "timer-retry-attempt-2", Generation: 1, Ref: "early-fire", ObservedUnixMilli: 1_799_999_999_999}}); err == nil {
		t.Fatal("early timer firing was accepted")
	}
	if h.state.Models[invID].RetryCount != 1 || h.state.ModelAttempts["retry-attempt-2"].State != AttemptPlanned {
		t.Fatal("rejected early timer changed retry projection")
	}

	h.issue(Command{Kind: CommandObserveTimerElapsed, Transition: TransitionInput{TimerID: "timer-retry-attempt-2", Generation: 1, Ref: "elapsed-proof", ObservedUnixMilli: 1_800_000_000_000}})
	if h.state.ModelAttempts["retry-attempt-2"].State != AttemptPlanned || h.state.Models[invID].RetryCount != 1 {
		t.Fatal("timer elapsed created or charged a new attempt")
	}
	h.issue(Command{Kind: CommandDispatchModel, Model: ModelInput{InvocationID: invID, AttemptID: "retry-attempt-2", RequestRef: "retry-dispatch", ExecutorToken: 7, ExecutorIncarnation: "retry-inc-1", Fence: 2}})
	if a := h.state.ModelAttempts["retry-attempt-2"]; a.State != AttemptRequesting || a.Usage.Status != UsageReserved || a.Token != 7 || a.Fence != 2 {
		t.Fatal("dispatch did not establish the attempt fence and usage reservation")
	}
}

func TestRetryContractReducerBindsCanonicalCommandIntentToEvents(t *testing.T) {
	h, invID, prevID := retryReadyHarness(t)
	before := h.state
	valid := h.issue(retryScheduleCommand(invID, prevID, "retry-attempt-2", 1))
	if _, err := Evolve(before, valid); err != nil {
		t.Fatalf("valid retry command batch was not a positive control: %v", err)
	}
	mutations := []struct {
		name   string
		mutate func(*Batch)
	}{
		{name: "command kind", mutate: func(b *Batch) { b.Intent.Kind = CommandPlanModelRetry }},
		{name: "deadline", mutate: func(b *Batch) { b.Intent.Transition.DeadlineUnixMilli++ }},
		{name: "unused command field", mutate: func(b *Batch) { b.Intent.Transition.CauseRef = "unmodeled-payload" }},
		{name: "intent ID", mutate: func(b *Batch) { b.Intent.ID = "different-command" }},
		{name: "missing intent", mutate: func(b *Batch) { b.Intent = Command{} }},
		{name: "unused event field", mutate: func(b *Batch) { b.Events[0].Actor = ActorModel }},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			bad := valid
			bad.Events = append([]Event(nil), valid.Events...)
			tc.mutate(&bad)
			if _, err := Evolve(before, bad); err == nil {
				t.Fatal("replay accepted an event batch that no longer matches its canonical command intent")
			}
		})
	}

	// A direct replay-only command/event frame cannot invent a retry attempt with no previous attempt.
	planCmd := Command{ID: "orphan-plan-command", Kind: CommandPlanModelRetry, ExpectedRevision: before.Revision, WriterToken: before.AdmissionToken, WriterIncarnation: before.Incarnation, RunID: before.RunID, SessionID: before.SessionID, Transition: TransitionInput{TargetID: invID, AttemptID: "orphan-planned-attempt", TimerID: "orphan-timer", Generation: 1}}
	planEvents, err := commandEvents(before, planCmd)
	if err != nil {
		t.Fatalf("construct orphan plan control: %v", err)
	}
	orphan := Batch{CommandID: planCmd.ID, ExpectedRevision: before.Revision, WriterToken: before.AdmissionToken, WriterIncarnation: before.Incarnation, ProtocolVersion: CurrentProtocolVersion, Intent: planCmd, Events: planEvents}
	if len(orphan.Events) != 1 || orphan.Events[0].Kind != EventModelAttemptPlanned || orphan.Events[0].PreviousAttemptID != "" {
		t.Fatal("orphan plan test did not construct the targeted zero-previous-attempt event")
	}
	if _, err := Evolve(before, orphan); err == nil {
		t.Fatal("replay accepted a retry attempt plan with zero PreviousAttemptID")
	}
}

func TestRetryContractRejectsInvalidOrdinalLimitAndCrossInvocationAttempt(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*Command)
	}{
		{name: "skipped ordinal", edit: func(c *Command) { c.Transition.RetryOrdinal = 2 }},
		{name: "above configured limit", edit: func(c *Command) { c.Transition.RetryOrdinal, c.Transition.RetryLimit = 3, 3 }},
		{name: "wrong policy version", edit: func(c *Command) { c.Transition.PolicyVersion = 8 }},
		{name: "missing deadline", edit: func(c *Command) { c.Transition.DeadlineUnixMilli = 0 }},
		{name: "duplicate attempt", edit: func(c *Command) { c.Transition.AttemptID = "retry-attempt-1" }},
		{name: "wrong prior attempt", edit: func(c *Command) { c.Transition.PreviousAttemptID = "unknown-attempt" }},
		{name: "noncanonical raw metadata", edit: func(c *Command) { c.Transition.CauseRef = "unmodeled-payload" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, invID, oldID := retryReadyHarness(t)
			cmd := retryScheduleCommand(invID, oldID, "retry-attempt-2", 1)
			tc.edit(&cmd)
			before := h.state
			if err := decideRetryWithoutMutation(h, cmd); err == nil {
				t.Fatal("invalid retry schedule was accepted")
			}
			if !reflect.DeepEqual(h.state, before) {
				t.Fatal("rejected retry schedule mutated state")
			}
		})
	}
}

func TestRetryContractFailureMustBeTypedAndPreDispatchFailureCannotRetry(t *testing.T) {
	beforeStart := newTransitionHarness(t)
	beforeStart.issue(Command{Kind: CommandAcceptRun, AcceptRun: AcceptRunInput{InputRef: "input-ref", Origin: OriginUser, RetryBudgetLimit: 2, ModelRetryPolicyVersion: 7}})
	beforeStart.issue(Command{Kind: CommandAdmitRun, Admission: AdmissionInput{Token: 7, Incarnation: "retry-inc-1"}})
	beforeStart.issue(Command{Kind: CommandStartStep, Step: StepInput{StepID: "before-start-step"}})
	beforeStart.issue(Command{Kind: CommandPlanModel, Model: ModelInput{InvocationID: "before-start-inv", AttemptID: "before-start-attempt", RequestRef: "request-ref"}})
	beforeStart.issue(Command{Kind: CommandDispatchModel, Model: ModelInput{InvocationID: "before-start-inv", AttemptID: "before-start-attempt", RequestRef: "dispatch-ref", ExecutorToken: 7, ExecutorIncarnation: "retry-inc-1", Fence: 1}})
	beforeStart.issue(Command{Kind: CommandObserveModelFailure, Transition: TransitionInput{
		TargetID: "before-start-inv", AttemptID: "before-start-attempt", Ref: "before-start-failure", Usage: UsageReserved, ReceiptRef: "reservation-ref",
		ExecutorToken: 7, ExecutorIncarnation: "retry-inc-1", Fence: 1, FailureStage: ModelFailureBeforeStart,
		FailureClass: ModelFailureTimeout, RetryDisposition: RetryDispositionRetry,
	}})
	beforeStart.issue(retryScheduleCommand("before-start-inv", "before-start-attempt", "before-start-retry", 1))
	if beforeStart.state.ModelAttempts["before-start-attempt"].FailureStage != ModelFailureBeforeStart || beforeStart.state.RetriesUsed != 1 {
		t.Fatal("retryable failure before stream start was not retained and charged")
	}

	pre := newTransitionHarness(t)
	pre.issue(Command{Kind: CommandAcceptRun, AcceptRun: AcceptRunInput{InputRef: "input-ref", Origin: OriginUser, RetryBudgetLimit: 2, ModelRetryPolicyVersion: 7}})
	pre.issue(Command{Kind: CommandAdmitRun, Admission: AdmissionInput{Token: 7, Incarnation: "retry-inc-1"}})
	pre.issue(Command{Kind: CommandStartStep, Step: StepInput{StepID: "step-1"}})
	pre.issue(Command{Kind: CommandPlanModel, Model: ModelInput{InvocationID: "inv", AttemptID: "attempt", RequestRef: "request"}})
	pre.issue(Command{Kind: CommandObserveModelFailure, Transition: TransitionInput{
		TargetID: "inv", AttemptID: "attempt", Ref: "pre-failure", Usage: UsageUnknown, Terminal: true,
		FailureStage: ModelFailurePreDispatch, FailureClass: ModelFailureAuth, RetryDisposition: RetryDispositionFatal,
	}})
	if err := decideRetryWithoutMutation(pre, retryScheduleCommand("inv", "attempt", "attempt-2", 1)); err == nil {
		t.Fatal("fatal pre-dispatch failure was accepted for retry")
	}
	pre.issue(Command{Kind: CommandAbandonModel, Transition: TransitionInput{TargetID: "inv", AttemptID: "attempt", Ref: "fatal-abandon-proof", ReceiptRef: "fatal-abandon-policy", Exposure: ExposureUnknown, Next: NextContinue}})
	fatalAbandonRetry := retryScheduleCommand("inv", "attempt", "fatal-abandon-retry", 1)
	fatalAbandonRetry.Transition.Ref = "fatal-abandon-policy"
	if err := decideRetryWithoutMutation(pre, fatalAbandonRetry); err == nil {
		t.Fatal("known fatal failure bypassed retry disposition through abandonment")
	}
}

func TestRetryContractUnsupportedFailureClassesFailClosedOnEligibleAttempt(t *testing.T) {
	control, invID, attemptID := retryStreamingHarness(t)
	before := control.state
	valid := control.issue(Command{Kind: CommandObserveModelFailure, Transition: TransitionInput{
		TargetID: invID, AttemptID: attemptID, Ref: "known-server-failure", Usage: UsageUnknown,
		ExecutorToken: 7, ExecutorIncarnation: "retry-inc-1", Fence: 1,
		FailureStage: ModelFailureProtocol, FailureClass: ModelFailureServer, RetryDisposition: RetryDispositionRetry,
	}})
	if len(valid.Events) != 1 || control.state.ModelAttempts[attemptID].FailureClass != ModelFailureServer || control.state.ModelAttempts[attemptID].RetryDisposition != RetryDispositionRetry {
		t.Fatal("eligible retryable failure was not a positive classification control")
	}
	if _, err := Evolve(before, valid); err != nil {
		t.Fatalf("valid retryable failure did not replay: %v", err)
	}
	for _, tc := range []struct {
		name        string
		class       ModelFailureClass
		disposition RetryDisposition
	}{
		{name: "refusal", class: ModelFailureRefusal, disposition: RetryDispositionResult},
		{name: "context overflow", class: ModelFailureContextOverflow, disposition: RetryDispositionFatal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, inv, attempt := retryStreamingHarness(t)
			cmd := valid.Intent
			cmd.ID, cmd.Transition.TargetID, cmd.Transition.AttemptID = ID("unsupported-"+tc.name), inv, attempt
			cmd.Transition.Ref = ID("unsupported-failure-" + tc.name)
			cmd.Transition.FailureClass, cmd.Transition.RetryDisposition = tc.class, tc.disposition
			if err := decideRetryWithoutMutation(h, cmd); err == nil {
				t.Fatalf("unsupported %s classification passed on an eligible streaming attempt", tc.name)
			}
		})
	}
}

func TestRetryContractInterruptedAttemptNeedsExplicitAbandonmentAndPolicy(t *testing.T) {
	h := newTransitionHarness(t)
	invID, attemptID := h.modelInFlight()
	h.issue(Command{Kind: CommandAcquireOwnership, Transition: TransitionInput{
		ExecutorToken: 8, ExecutorIncarnation: "retry-inc-2", ReceiptRef: "recovery-receipt",
		Recoveries: []RecoveryFact{{TargetID: invID, AttemptID: attemptID, CauseRef: "interruption-cause", PartialRef: "partial-ref", Usage: UsageUnknown, BlockerID: "recovery-blocker", PreviousToken: 7, PreviousIncarnation: "writer-inc-1", Fence: 1}},
	}})
	h.issue(Command{Kind: CommandClearRunBlock, Transition: TransitionInput{BlockerID: "recovery-blocker", CauseRef: "block-clear-cause", Ref: "block-clear-proof"}})
	h.issue(Command{Kind: CommandClearRecovery, Transition: TransitionInput{CauseRef: "interruption-cause", Ref: "recovery-resolution", ReceiptRef: "resolution-receipt", Actor: ActorUser, RecoveryDisposition: RecoveryDispositionReconciled}})
	if h.state.ModelAttempts[attemptID].State != AttemptInterrupted {
		t.Fatal("recovery unexpectedly converted the interrupted attempt into a known retryable failure")
	}
	if err := decideRetryWithoutMutation(h, retryScheduleCommand(invID, attemptID, "retry-after-interrupt", 1)); err == nil {
		t.Fatal("interrupted unknown attempt retried without explicit abandonment policy")
	}
	h.issue(Command{Kind: CommandAbandonModel, Transition: TransitionInput{TargetID: invID, AttemptID: attemptID, Ref: "abandon-proof", ReceiptRef: "abandon-receipt", Exposure: ExposureUnknown, Next: NextContinue}})
	withoutPolicy := retryScheduleCommand(invID, attemptID, "retry-after-abandon", 1)
	withoutPolicy.Transition.RetryLimit, withoutPolicy.Transition.PolicyVersion = 5, 1
	withoutPolicy.Transition.Ref = ""
	if err := decideRetryWithoutMutation(h, withoutPolicy); err == nil {
		t.Fatal("abandoned unknown attempt retried without explicit retry policy reference")
	}
	withPolicyCommand := retryScheduleCommand(invID, attemptID, "retry-after-abandon", 1)
	withPolicyCommand.Transition.RetryLimit, withPolicyCommand.Transition.PolicyVersion = 5, 1
	withPolicyCommand.Transition.Ref = "abandon-receipt"
	inv, abandoned := h.state.Models[invID], h.state.ModelAttempts[attemptID]
	if inv.State != InvocationActive || abandoned.State != AttemptAbandoned || len(inv.AttemptIDs) == 0 || inv.AttemptIDs[len(inv.AttemptIDs)-1] != attemptID || h.state.RetriesUsed >= h.state.RetryBudgetLimit || len(h.state.Blockers) != 0 {
		t.Fatalf("abandon retry positive control is not eligible: inv=%#v attempt=%#v blockers=%#v", inv, abandoned, h.state.Blockers)
	}
	withPolicy := h.issue(withPolicyCommand)
	if len(withPolicy.Events) != 3 || withPolicy.Events[0].Kind != EventModelRetryScheduled || h.state.RetriesUsed != 1 || h.state.ModelAttempts["retry-after-abandon"].State != AttemptPlanned {
		t.Fatal("explicitly abandoned attempt with policy reference did not schedule a new planned retry")
	}
}

func TestRetryContractBudgetIsRunScopedAcrossInvocations(t *testing.T) {
	h, invID, prevID := retryReadyHarness(t)
	h.issue(retryScheduleCommand(invID, prevID, "retry-attempt-2", 1))
	if h.state.RetriesUsed != 1 {
		t.Fatalf("retry was not charged at scheduling time: used=%d", h.state.RetriesUsed)
	}
	h.issue(Command{Kind: CommandObserveTimerElapsed, Transition: TransitionInput{TimerID: "timer-retry-attempt-2", Generation: 1, Ref: "elapsed-1", ObservedUnixMilli: 1_800_000_000_000}})
	h.issue(Command{Kind: CommandDispatchModel, Model: ModelInput{InvocationID: invID, AttemptID: "retry-attempt-2", RequestRef: "dispatch-2", ExecutorToken: 7, ExecutorIncarnation: "retry-inc-1", Fence: 2}})
	h.issue(Command{Kind: CommandObserveModelResponse, ModelResponse: ModelResponseInput{InvocationID: invID, AttemptID: "retry-attempt-2", ResponseRef: "response-2", Usage: UsageKnown, UsageRef: "usage-2", ExecutorToken: 7, ExecutorIncarnation: "retry-inc-1", Fence: 2}})
	h.issue(Command{Kind: CommandFinishStep, FinishStep: FinishStepInput{Next: NextContinue, Continuation: ContinuationSteerPending}})
	h.issue(Command{Kind: CommandStartStep, Step: StepInput{StepID: "retry-step-2"}})
	secondInv, secondAttempt := ID("retry-invocation-2"), ID("retry-attempt-3")
	h.issue(Command{Kind: CommandPlanModel, Model: ModelInput{InvocationID: secondInv, AttemptID: secondAttempt, RequestRef: "request-3"}})
	h.issue(Command{Kind: CommandDispatchModel, Model: ModelInput{InvocationID: secondInv, AttemptID: secondAttempt, RequestRef: "dispatch-3", ExecutorToken: 7, ExecutorIncarnation: "retry-inc-1", Fence: 3}})
	h.issue(Command{Kind: CommandObserveModelStream, Model: ModelInput{InvocationID: secondInv, AttemptID: secondAttempt, ExecutorToken: 7, ExecutorIncarnation: "retry-inc-1", Fence: 3}})
	h.issue(Command{Kind: CommandObserveModelFailure, Transition: TransitionInput{
		TargetID: secondInv, AttemptID: secondAttempt, Ref: "failure-3", CauseRef: "partial-3", Usage: UsageUnknown,
		ExecutorToken: 7, ExecutorIncarnation: "retry-inc-1", Fence: 3, FailureStage: ModelFailureStream,
		FailureClass: ModelFailureServer, RetryDisposition: RetryDispositionRetry,
	}})
	// The second invocation gets the legitimate remaining run-level retry.
	secondRetry := retryScheduleCommand(secondInv, secondAttempt, "retry-attempt-4", 1)
	h.issue(secondRetry)
	if h.state.RetriesUsed != 2 || h.state.Models[secondInv].RetryCount != 1 {
		t.Fatal("remaining run retry budget was not available to a later invocation")
	}
	// A third invocation cannot reset the exhausted run-level budget.
	h.issue(Command{Kind: CommandObserveTimerElapsed, Transition: TransitionInput{TimerID: "timer-retry-attempt-4", Generation: 1, Ref: "elapsed-4", ObservedUnixMilli: 1_800_000_000_000}})
	h.issue(Command{Kind: CommandDispatchModel, Model: ModelInput{InvocationID: secondInv, AttemptID: "retry-attempt-4", RequestRef: "dispatch-4", ExecutorToken: 7, ExecutorIncarnation: "retry-inc-1", Fence: 4}})
	h.issue(Command{Kind: CommandObserveModelResponse, ModelResponse: ModelResponseInput{InvocationID: secondInv, AttemptID: "retry-attempt-4", ResponseRef: "response-4", Usage: UsageKnown, UsageRef: "usage-4", ExecutorToken: 7, ExecutorIncarnation: "retry-inc-1", Fence: 4}})
	h.issue(Command{Kind: CommandFinishStep, FinishStep: FinishStepInput{Next: NextContinue, Continuation: ContinuationSteerPending}})
	h.issue(Command{Kind: CommandStartStep, Step: StepInput{StepID: "retry-step-3"}})
	thirdInv, thirdAttempt := ID("retry-invocation-3"), ID("retry-attempt-5")
	h.issue(Command{Kind: CommandPlanModel, Model: ModelInput{InvocationID: thirdInv, AttemptID: thirdAttempt, RequestRef: "request-5"}})
	h.issue(Command{Kind: CommandDispatchModel, Model: ModelInput{InvocationID: thirdInv, AttemptID: thirdAttempt, RequestRef: "dispatch-5", ExecutorToken: 7, ExecutorIncarnation: "retry-inc-1", Fence: 5}})
	h.issue(Command{Kind: CommandObserveModelStream, Model: ModelInput{InvocationID: thirdInv, AttemptID: thirdAttempt, ExecutorToken: 7, ExecutorIncarnation: "retry-inc-1", Fence: 5}})
	h.issue(Command{Kind: CommandObserveModelFailure, Transition: TransitionInput{
		TargetID: thirdInv, AttemptID: thirdAttempt, Ref: "failure-5", CauseRef: "partial-5", Usage: UsageUnknown,
		ExecutorToken: 7, ExecutorIncarnation: "retry-inc-1", Fence: 5, FailureStage: ModelFailureStream,
		FailureClass: ModelFailureServer, RetryDisposition: RetryDispositionRetry,
	}})
	if err := decideRetryWithoutMutation(h, retryScheduleCommand(thirdInv, thirdAttempt, "retry-attempt-6", 1)); err == nil {
		t.Fatal("new invocation reset the fully spent run-level retry budget")
	}
	if err := decideRetryWithoutMutation(h, retryScheduleCommand(thirdInv, "retry-attempt-4", "retry-attempt-cross", 1)); err == nil {
		t.Fatal("retry accepted a previous attempt owned by another invocation")
	}
	if h.state.RetriesUsed != 2 || h.state.Models[thirdInv].RetryCount != 0 {
		t.Fatal("rejected over-budget retry changed run or invocation accounting")
	}
}

func TestRetryContractStopWhileWaitingCancelsPlannedAttemptAndTimer(t *testing.T) {
	h, invID, prevID := retryReadyHarness(t)
	h.issue(retryScheduleCommand(invID, prevID, "retry-attempt-2", 1))
	if !h.state.Timers["timer-retry-attempt-2"].Pending || h.state.Blockers["blocker-retry-attempt-2"].Kind != BlockRetryTimer {
		t.Fatal("test setup is missing a pending timer and retry blocker")
	}
	if err := decideRetryWithoutMutation(h, Command{Kind: CommandClearRunBlock, Transition: TransitionInput{BlockerID: "blocker-retry-attempt-2", CauseRef: "manual-clear-cause", Ref: "manual-clear-proof"}}); err == nil {
		t.Fatal("retry timer blocker was manually cleared while its timer was pending")
	}
	if h.state.RetriesUsed != 1 {
		t.Fatal("scheduled retry was not charged before the wait")
	}
	stopped := h.issue(Command{Kind: CommandRequestStop, Stop: StopInput{Reason: StopUserCancel, Mode: StopTerminal, Ref: "user-stop-ref", Actor: ActorUser}})
	if len(stopped.Events) != 3 || stopped.Events[0].Kind != EventRunStopRequested || stopped.Events[1].Kind != EventModelAttemptCancelled || stopped.Events[2].Kind != EventTimerCancelled {
		t.Fatalf("stop while waiting did not atomically cancel attempt and timer: %#v", stopped.Events)
	}
	cancelledAttempt, cancelledTimer := h.state.ModelAttempts["retry-attempt-2"], h.state.Timers["timer-retry-attempt-2"]
	if cancelledAttempt.State != AttemptCancelled || cancelledAttempt.RetryTimerID != "timer-retry-attempt-2" || !cancelledAttempt.RetryCancelled || cancelledTimer.Pending || !cancelledTimer.Cancelled || h.state.Blockers["blocker-retry-attempt-2"].ID != "" || h.state.RetriesUsed != 1 || cancelledAttempt.Usage.Status != UsageUnknown {
		t.Fatal("stop did not cancel the planned retry wait while preserving the one-time retry charge and unknown usage")
	}
	if err := decideRetryWithoutMutation(h, Command{Kind: CommandObserveTimerElapsed, Transition: TransitionInput{TimerID: "timer-retry-attempt-2", Generation: 1, Ref: "late-fire", ObservedUnixMilli: 1_900_000_000_000}}); err == nil {
		t.Fatal("cancelled retry timer was allowed to revive the attempt")
	}
}

func TestRetryContractTerminalStopEventCannotOmitRetryCancellationPair(t *testing.T) {
	h, invID, prevID := retryReadyHarness(t)
	h.issue(retryScheduleCommand(invID, prevID, "retry-attempt-2", 1))
	state := h.state
	cmd := Command{ID: "retry-stop-without-cancel", Kind: CommandRequestStop, ExpectedRevision: state.Revision, WriterToken: state.AdmissionToken, WriterIncarnation: state.Incarnation, RunID: state.RunID, SessionID: state.SessionID, Stop: StopInput{Reason: StopUserCancel, Mode: StopTerminal, Ref: "stop-ref", Actor: ActorUser}}
	complete, err := Decide(state, cmd)
	if err != nil {
		t.Fatalf("valid terminal stop batch was not a positive control: %v", err)
	}
	if _, err := Evolve(state, complete); err != nil {
		t.Fatalf("valid terminal stop batch did not replay: %v", err)
	}
	unusedStopField := complete
	unusedStopField.Intent.Transition.Ref = "unmodeled-stop-payload"
	if _, err := Evolve(state, unusedStopField); err == nil {
		t.Fatal("replay accepted terminal stop intent with an unused Transition field")
	}
	missingCancel := complete
	missingCancel.Events = append([]Event(nil), complete.Events[:1]...)
	if _, err := Evolve(state, missingCancel); err == nil {
		t.Fatal("replay accepted terminal stop for retry wait without cancelling its planned attempt and timer")
	}
}

func TestRetryContractStopIntentCannotCarryUnrelatedTimerElapsedEvents(t *testing.T) {
	h, invID, prevID := retryReadyHarness(t)
	h.issue(retryScheduleCommand(invID, prevID, "retry-attempt-2", 1))
	h.issue(Command{Kind: CommandScheduleTimer, Transition: TransitionInput{TargetID: "dependency-target", TimerID: "unrelated-timer", BlockerID: "unrelated-blocker", Ref: "timer-schedule-proof", Purpose: TimerDependencyExpiry, Generation: 1}})
	beforeElapsed := h.state
	elapsed := h.issue(Command{Kind: CommandObserveTimerElapsed, Transition: TransitionInput{TimerID: "unrelated-timer", Generation: 1, Ref: "unrelated-elapsed-proof"}})
	if len(elapsed.Events) != 1 || elapsed.Events[0].Kind != EventTimerElapsed {
		t.Fatal("test setup did not produce a valid unrelated TimerElapsed event")
	}
	if _, err := Evolve(beforeElapsed, elapsed); err != nil {
		t.Fatalf("valid unrelated timer event was not a positive control: %v", err)
	}
	stopIntentOnly := elapsed
	stopIntentOnly.Intent.Kind = CommandRequestStop
	stopIntentOnly.Intent.Transition = TransitionInput{}
	stopIntentOnly.Intent.Stop = StopInput{Reason: StopUserCancel, Mode: StopTerminal, Ref: "stop-intent-ref", Actor: ActorUser}
	for _, event := range stopIntentOnly.Events {
		if event.Kind == EventRunStopRequested {
			t.Fatal("test setup unexpectedly included a stop event")
		}
	}
	if _, err := Evolve(beforeElapsed, stopIntentOnly); err == nil {
		t.Fatal("replay accepted a stop intent backed only by unrelated timer elapsed events")
	}
}

func TestRetryContractTerminalStopAfterElapsedCancelsBeforeDispatch(t *testing.T) {
	h, invID, prevID := retryReadyHarness(t)
	h.issue(retryScheduleCommand(invID, prevID, "retry-attempt-2", 1))
	h.issue(Command{Kind: CommandObserveTimerElapsed, Transition: TransitionInput{TimerID: "timer-retry-attempt-2", Generation: 1, Ref: "elapsed-proof", ObservedUnixMilli: 1_800_000_000_000}})
	if h.state.ModelAttempts["retry-attempt-2"].State != AttemptPlanned || !h.state.Timers["timer-retry-attempt-2"].Elapsed {
		t.Fatal("timer elapsed did not leave the retry planned and awaiting dispatch")
	}
	stop := h.issue(Command{Kind: CommandRequestStop, Stop: StopInput{Reason: StopUserCancel, Mode: StopTerminal, Ref: "stop-after-elapsed", Actor: ActorUser}})
	if len(stop.Events) != 3 || stop.Events[1].Kind != EventModelAttemptCancelled || stop.Events[2].Kind != EventTimerCancelled || h.state.ModelAttempts["retry-attempt-2"].State != AttemptCancelled || h.state.Timers["timer-retry-attempt-2"].Pending || h.state.Blockers["blocker-retry-attempt-2"].ID != "" {
		t.Fatal("terminal stop failed to cancel the elapsed but undispatched retry")
	}
}

func TestRetryContractSuspendPreservesRetryWait(t *testing.T) {
	h, invID, prevID := retryReadyHarness(t)
	h.issue(retryScheduleCommand(invID, prevID, "retry-attempt-2", 1))
	stop := h.issue(Command{Kind: CommandRequestStop, Stop: StopInput{Reason: StopShutdown, Mode: StopSuspend, Ref: "shutdown-suspend", BlockerID: "host-pause", Actor: ActorSystem}})
	if len(stop.Events) != 1 || stop.Events[0].Kind != EventRunStopRequested || h.state.ModelAttempts["retry-attempt-2"].State != AttemptPlanned || !h.state.Timers["timer-retry-attempt-2"].Pending || h.state.Blockers["blocker-retry-attempt-2"].Kind != BlockRetryTimer || h.state.Blockers["host-pause"].Kind != BlockHostPaused || h.state.RetriesUsed != 1 {
		t.Fatal("suspend did not preserve the retry wait while recording the host pause")
	}
}

func TestRetryContractOwnershipRecoveryGatesElapsedRetryUntilAdmissionClear(t *testing.T) {
	h, invID, prevID := retryReadyHarness(t)
	h.issue(retryScheduleCommand(invID, prevID, "retry-attempt-2", 1))
	preOwnership := h.state
	owned := h.issue(Command{Kind: CommandAcquireOwnership, Transition: TransitionInput{
		ExecutorToken: 8, ExecutorIncarnation: "retry-inc-2", ReceiptRef: "ownership-receipt",
		BlockerID: "recovery-admission", Ref: "recovery-admission-proof",
	}})
	if len(owned.Events) != 2 || owned.Events[0].Kind != EventRunOwnershipAcquired || owned.Events[1].Kind != EventRunBlockAdded || owned.Events[1].BlockKind != BlockRecoveryAdmission {
		t.Fatalf("ownership change did not install recovery admission: %#v", owned.Events)
	}
	if _, err := Evolve(preOwnership, owned); err != nil {
		t.Fatalf("valid ownership recovery batch was not a positive control: %v", err)
	}
	orphanRecovery := owned
	orphanRecovery.Events = append([]Event(nil), owned.Events[1:]...)
	if _, err := Evolve(preOwnership, orphanRecovery); err == nil {
		t.Fatal("replay accepted recovery admission events under an ownership intent with no ownership event")
	}
	beforeSecondOwnership := h.state
	secondOwnership := h.issue(Command{Kind: CommandAcquireOwnership, Transition: TransitionInput{ExecutorToken: 9, ExecutorIncarnation: "retry-inc-3", ReceiptRef: "second-ownership-receipt"}})
	if len(secondOwnership.Events) != 1 || secondOwnership.Events[0].Kind != EventRunOwnershipAcquired || h.state.Blockers["recovery-admission"].Kind != BlockRecoveryAdmission {
		t.Fatal("second ownership recovery did not preserve the existing admission blocker")
	}
	if _, err := Evolve(beforeSecondOwnership, secondOwnership); err != nil {
		t.Fatalf("second ownership batch was not a positive control: %v", err)
	}
	modifiedSecondOwnership := secondOwnership
	modifiedSecondOwnership.Intent.Transition.ReceiptRef = "tampered-second-ownership-receipt"
	if _, err := Evolve(beforeSecondOwnership, modifiedSecondOwnership); err == nil {
		t.Fatal("replay accepted a modified ownership intent while recovery admission was already active")
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Batch)
	}{
		{name: "blocker ID", mutate: func(b *Batch) { b.Intent.Transition.BlockerID = "unused-second-blocker" }},
		{name: "blocker ref", mutate: func(b *Batch) { b.Intent.Transition.Ref = "unused-second-block-ref" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := secondOwnership
			tc.mutate(&bad)
			if _, err := Evolve(beforeSecondOwnership, bad); err == nil {
				t.Fatalf("replay accepted second ownership intent with an unused %s", tc.name)
			}
		})
	}
	if h.state.RetriesUsed != 1 || h.state.ModelAttempts["retry-attempt-2"].State != AttemptPlanned || !h.state.Timers["timer-retry-attempt-2"].Pending {
		t.Fatal("ownership recovery changed the charged retry or planned attempt")
	}
	h.issue(Command{Kind: CommandObserveTimerElapsed, Transition: TransitionInput{TimerID: "timer-retry-attempt-2", Generation: 1, Ref: "overdue-timer-proof", ObservedUnixMilli: 1_900_000_000_000}})
	if h.state.ModelAttempts["retry-attempt-2"].State != AttemptPlanned || h.state.RetriesUsed != 1 || h.state.Blockers["recovery-admission"].Kind != BlockRecoveryAdmission {
		t.Fatal("elapsed retry timer bypassed recovery admission or charged another retry")
	}
	blockedDispatch := Command{Kind: CommandDispatchModel, Model: ModelInput{InvocationID: invID, AttemptID: "retry-attempt-2", RequestRef: "retry-dispatch", ExecutorToken: 9, ExecutorIncarnation: "retry-inc-3", Fence: 2}}
	if err := decideRetryWithoutMutation(h, blockedDispatch); err == nil {
		t.Fatal("elapsed timer dispatched the planned attempt before recovery admission cleared")
	}
	h.issue(Command{Kind: CommandClearRunBlock, Transition: TransitionInput{BlockerID: "recovery-admission", CauseRef: "admission-clear-cause", Ref: "fresh-admission-clear-proof"}})
	h.issue(blockedDispatch)
	if h.state.RetriesUsed != 1 || h.state.Models[invID].RetryCount != 1 || h.state.ModelAttempts["retry-attempt-2"].State != AttemptRequesting || h.state.ModelAttempts["retry-attempt-2"].Token != 9 {
		t.Fatal("authorized recovery clear did not dispatch the existing planned attempt without recharging")
	}
}
