package kernel

import (
	"reflect"
	"sort"
	"testing"
)

func closureManifestCommand(state State, id ID, targets []ClosureTargetFact) Command {
	return Command{
		ID: id, Kind: CommandConfirmRunClosure,
		ExpectedRevision: state.Revision, WriterToken: state.AdmissionToken, WriterIncarnation: state.Incarnation,
		RunID: state.RunID, SessionID: state.SessionID,
		Closure: ClosureInput{
			PreviewRef: "manifest-preview-ref", ReceiptRef: "manifest-receipt", PreviewVersion: 1,
			Revision: state.Revision, Actor: ActorUser, CauseEventRef: "interrupted",
			UnavailableReason:       UnavailableVaultLocked,
			PreviousContextRevision: state.ContextRevision, ContextRevision: state.ContextRevision + 1,
			ContextRequirement: ContextRequirementRevalidationRequired, Targets: targets,
		},
	}
}

func requireRejectCode(t *testing.T, err error, want RejectCode) {
	t.Helper()
	rejection, ok := err.(Rejection)
	if !ok || rejection.Code != want {
		t.Fatalf("error=%v, want rejection code %d", err, want)
	}
}

func TestClosureManifestCopiesAcrossCommandBatchStateReceiptsAndEvents(t *testing.T) {
	state := traceCRecoverySeed(t)
	inputTargets := []ClosureTargetFact{
		{Kind: ClosureTargetUnknownTool, TargetID: "t1", AttemptID: "ta1", CauseRef: "tool-effect-unknown", CauseEventRef: "interrupted", ResultRef: "unknown-tool-result"},
	}
	cmd := closureManifestCommand(state, "closure-manifest-copy", inputTargets)
	cleanCommand := cmd
	cleanCommand.Closure.Targets = append([]ClosureTargetFact(nil), cmd.Closure.Targets...)
	batch, err := Decide(state, cmd)
	if err != nil {
		t.Fatalf("positive closure command rejected: %v", err)
	}
	if !reflect.DeepEqual(batch.Intent.Closure.Targets, cleanCommand.Closure.Targets) || !reflect.DeepEqual(batch.Events[0].ClosureTargets, cleanCommand.Closure.Targets) {
		t.Fatal("Decide returned a closure intent or event manifest different from its input")
	}
	cleanBatch := cloneBatch(batch)

	// First prove caller-owned input cannot rewrite the returned command receipt
	// or the manifest event before replay.
	inputTargets[0].CauseRef = "caller-mutated-cause"
	cmd.Closure.Targets[0].TargetID = "caller-mutated-target"
	if !reflect.DeepEqual(batch.Intent.Closure.Targets, cleanCommand.Closure.Targets) || !reflect.DeepEqual(batch.Events[0].ClosureTargets, cleanCommand.Closure.Targets) {
		t.Fatal("Decide output aliases caller-owned closure target slices")
	}

	next, err := Evolve(state, batch)
	if err != nil {
		t.Fatalf("replay clean returned closure batch: %v", err)
	}
	storedReceipt, ok := next.CommandBatches[cleanCommand.ID]
	if !ok {
		t.Fatal("successful closure did not retain its command receipt")
	}
	manifestEvent, ok := next.EventIDs[cleanBatch.Events[0].ID]
	if !ok {
		t.Fatal("successful closure did not retain its manifest event")
	}
	if !reflect.DeepEqual(storedReceipt.Intent.Closure.Targets, cleanCommand.Closure.Targets) || !reflect.DeepEqual(storedReceipt.Events[0].ClosureTargets, cleanCommand.Closure.Targets) || !reflect.DeepEqual(manifestEvent.ClosureTargets, cleanCommand.Closure.Targets) {
		t.Fatal("retained closure receipt or event manifest differs from the accepted command")
	}

	// Mutating the returned batch after commit must not rewrite either retained
	// State projection.
	batch.Intent.Closure.Targets[0].ResultRef = "returned-batch-result"
	batch.Intent.Closure.Targets[0].CauseEventRef = "returned-batch-cause-event"
	batch.Events[0].ClosureTargets[0].CauseRef = "returned-batch-cause"
	batch.Events[0].ClosureTargets[0].CauseEventRef = "returned-batch-event-ref"
	if !reflect.DeepEqual(next.CommandBatches[cleanCommand.ID], storedReceipt) || !reflect.DeepEqual(next.EventIDs[cleanBatch.Events[0].ID], manifestEvent) {
		t.Fatal("returned batch aliases retained command receipt or EventIDs manifest")
	}

	// State clones must deep-copy both maps and keep the two manifest copies
	// independent from each other as well as from the source State.
	clonedState := cloneState(next)
	clonedReceipt := clonedState.CommandBatches[cleanCommand.ID]
	clonedReceipt.Intent.Closure.Targets[0].TargetID = "clone-receipt-target"
	clonedReceipt.Intent.Closure.Targets[0].CauseEventRef = "clone-receipt-cause-event"
	clonedReceipt.Events[0].ClosureTargets[0].AttemptID = "clone-receipt-attempt"
	clonedState.CommandBatches[cleanCommand.ID] = clonedReceipt
	clonedEvent := clonedState.EventIDs[cleanBatch.Events[0].ID]
	if !reflect.DeepEqual(clonedEvent.ClosureTargets, manifestEvent.ClosureTargets) {
		t.Fatal("State clone command receipt aliases its EventIDs manifest")
	}
	clonedEvent.ClosureTargets[0].CauseRef = "clone-event-cause"
	clonedEvent.ClosureTargets[0].CauseEventRef = "clone-event-cause-event"
	clonedState.EventIDs[cleanBatch.Events[0].ID] = clonedEvent
	if !reflect.DeepEqual(next.CommandBatches[cleanCommand.ID], storedReceipt) || !reflect.DeepEqual(next.EventIDs[cleanBatch.Events[0].ID], manifestEvent) {
		t.Fatal("State clone manifests alias source State receipts or EventIDs")
	}

	// Exact command retry remains a receipt lookup after revision advances;
	// mutating the returned duplicate cannot rewrite the retained receipt.
	duplicate, err := Decide(next, cleanCommand)
	if err != nil || !reflect.DeepEqual(duplicate, cleanBatch) {
		t.Fatalf("exact closure retry did not return original receipt: %v", err)
	}
	duplicate.Events[0].ClosureTargets[0].TargetID = "mutated-duplicate-target"
	again, err := Decide(next, cleanCommand)
	if err != nil || !reflect.DeepEqual(again, cleanBatch) {
		t.Fatalf("mutating returned duplicate altered retained manifest receipt: %v", err)
	}
}

func TestClosureManifestFailureRollsBackNestedCallerInputs(t *testing.T) {
	state := traceCRecoverySeed(t)
	targets := []ClosureTargetFact{{Kind: ClosureTargetUnknownTool, TargetID: "t1", AttemptID: "ta1", CauseRef: "tool-effect-unknown", CauseEventRef: "interrupted", ResultRef: "unknown-tool-result"}}
	cmd := closureManifestCommand(state, "closure-manifest-rollback", targets)
	batch, err := Decide(state, cmd)
	if err != nil {
		t.Fatalf("positive closure command rejected before rollback case: %v", err)
	}
	malformed := cloneBatch(batch)
	malformed.Events[len(malformed.Events)-1].ID = ""
	stateBefore, batchBefore := cloneState(state), cloneBatch(malformed)
	if _, err := Evolve(state, malformed); err == nil {
		t.Fatal("closure batch with a malformed terminal event was accepted")
	}
	if !reflect.DeepEqual(state, stateBefore) {
		t.Fatal("failed closure replay mutated caller State")
	}
	if !reflect.DeepEqual(malformed, batchBefore) {
		t.Fatal("failed closure replay mutated nested caller batch manifests")
	}
}

func TestClosureManifestReceiptConflictAfterRevisionAdvances(t *testing.T) {
	state := traceCRecoverySeed(t)
	targets := []ClosureTargetFact{{Kind: ClosureTargetUnknownTool, TargetID: "t1", AttemptID: "ta1", CauseRef: "tool-effect-unknown", CauseEventRef: "interrupted", ResultRef: "unknown-tool-result"}}
	original := closureManifestCommand(state, "closure-manifest-conflict", targets)
	batch, err := Decide(state, original)
	if err != nil {
		t.Fatalf("positive closure command rejected: %v", err)
	}
	advanced, err := Evolve(state, batch)
	if err != nil {
		t.Fatalf("apply closure before receipt conflict checks: %v", err)
	}
	duplicate, err := Decide(advanced, original)
	if err != nil || !reflect.DeepEqual(duplicate, batch) {
		t.Fatalf("original command did not return the same receipt after state advanced: %v", err)
	}

	mutations := map[string]func(*Command){
		"target":      func(c *Command) { c.Closure.Targets[0].TargetID = "different-tool" },
		"cause":       func(c *Command) { c.Closure.Targets[0].CauseRef = "different-cause" },
		"cause-event": func(c *Command) { c.Closure.Targets[0].CauseEventRef = "different-event" },
		"receipt":     func(c *Command) { c.Closure.ReceiptRef = "different-receipt" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			changed := original
			changed.Closure.Targets = append([]ClosureTargetFact(nil), original.Closure.Targets...)
			mutate(&changed)
			_, err := Decide(advanced, changed)
			requireRejectCode(t, err, RejectConflict)
		})
	}
}

func TestClosureManifestCanonicalOrderAndDuplicateTupleRules(t *testing.T) {
	unknown := ClosureTargetFact{Kind: ClosureTargetUnknownTool, TargetID: "tool-a", AttemptID: "attempt-a", CauseRef: "cause-a", ResultRef: "result-a"}
	unavailable := ClosureTargetFact{Kind: ClosureTargetUnavailableCause, TargetID: "tool-a", CauseRef: "cause-b"}
	unordered := []ClosureTargetFact{unavailable, unknown}
	canonical := canonicalClosure(ClosureInput{Targets: unordered})
	if !reflect.DeepEqual(canonical.Targets, []ClosureTargetFact{unknown, unavailable}) || !strictClosureOrder(canonical.Targets) {
		t.Fatalf("canonical closure targets are not sorted by the API tuple: %#v", canonical.Targets)
	}
	if !reflect.DeepEqual(unordered, []ClosureTargetFact{unavailable, unknown}) {
		t.Fatal("canonical closure sorting mutated the caller's manifest")
	}
	if strictClosureOrder([]ClosureTargetFact{unknown, unknown}) {
		t.Fatal("duplicate (kind,target,attempt,cause) tuple was accepted")
	}
	if strictClosureOrder(unordered) {
		t.Fatal("unsorted target manifest was accepted")
	}

	state := traceCRecoverySeed(t)
	duplicateCmd := closureManifestCommand(state, "closure-manifest-duplicate", []ClosureTargetFact{unknown, unknown})
	if _, err := Decide(state, duplicateCmd); err == nil {
		t.Fatal("Decide accepted duplicate target tuple")
	} else {
		requireRejectCode(t, err, RejectInvalid)
	}
}

func TestClosureManifestClonesPreserveNilAndEmptySlices(t *testing.T) {
	for name, targets := range map[string][]ClosureTargetFact{"nil": nil, "empty": {}} {
		batch := Batch{
			Intent: Command{Closure: ClosureInput{Targets: targets}},
			Events: []Event{{ClosureTargets: targets}},
		}
		clonedBatch := cloneBatch(batch)
		if (targets == nil) != (clonedBatch.Intent.Closure.Targets == nil) || (targets == nil) != (clonedBatch.Events[0].ClosureTargets == nil) {
			t.Fatalf("cloneBatch changed %s manifest nil/empty representation", name)
		}
		state := EmptyState()
		state.CommandBatches["receipt"] = batch
		state.EventIDs["event"] = batch.Events[0]
		clonedState := cloneState(state)
		if (targets == nil) != (clonedState.CommandBatches["receipt"].Intent.Closure.Targets == nil) || (targets == nil) != (clonedState.CommandBatches["receipt"].Events[0].ClosureTargets == nil) || (targets == nil) != (clonedState.EventIDs["event"].ClosureTargets == nil) {
			t.Fatalf("cloneState changed %s manifest nil/empty representation", name)
		}
	}
}

func TestExactClosureDecideIsDeterministicWithMultipleRecoveryBlockers(t *testing.T) {
	h := newTransitionHarness(t)
	toolID, attemptID := h.toolDispatching()
	unknown := h.issue(Command{Kind: CommandObserveUnknownTool, Transition: TransitionInput{
		TargetID: toolID, AttemptID: attemptID, CauseRef: "deterministic-close-cause", Exposure: ExposureUnknown,
		ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Fence: 2,
	}})
	state, err := applyBatch(t, h.state, Batch{CommandID: "seed-multiple-recovery-blockers", Events: []Event{
		{ID: "recovery-block-z-event", Kind: EventRunBlockAdded, RunID: h.state.RunID, SessionID: h.state.SessionID, BlockerID: "recovery-z", BlockKind: BlockRecoveryAdmission, Ref: "recovery-z-ref"},
		{ID: "recovery-block-a-event", Kind: EventRunBlockAdded, RunID: h.state.RunID, SessionID: h.state.SessionID, BlockerID: "recovery-a", BlockKind: BlockRecoveryAdmission, Ref: "recovery-a-ref"},
	}})
	if err != nil {
		t.Fatalf("seed multiple recovery blockers: %v", err)
	}
	recoveryCount := 0
	for _, blocker := range state.Blockers {
		if blocker.Kind == BlockRecoveryAdmission {
			recoveryCount++
		}
	}
	if recoveryCount < 2 {
		t.Fatalf("fixture has %d recovery blockers, want at least two", recoveryCount)
	}

	cmd := Command{
		ID: "deterministic-uncommitted-close", Kind: CommandAcceptUnknownOutcome,
		ExpectedRevision: state.Revision, WriterToken: state.AdmissionToken, WriterIncarnation: state.Incarnation,
		RunID: state.RunID, SessionID: state.SessionID,
		Closure: ClosureInput{
			PreviewRef: "deterministic-exact-preview", ReceiptRef: "deterministic-exact-receipt",
			PreviewVersion: CurrentClosurePreviewVersion, Revision: state.Revision, Actor: ActorUser,
			Targets: []ClosureTargetFact{{Kind: ClosureTargetUnknownTool, TargetID: toolID, AttemptID: attemptID, CauseRef: "deterministic-close-cause", CauseEventRef: unknown.Events[1].ID, ResultRef: "deterministic-result-ref"}},
		},
	}
	var first Batch
	for i := 0; i < 32; i++ {
		batch, decideErr := Decide(state, cmd)
		if decideErr != nil {
			t.Fatalf("Decide iteration %d: %v", i+1, decideErr)
		}
		if i == 0 {
			first = cloneBatch(batch)
			continue
		}
		if !reflect.DeepEqual(batch, first) {
			t.Fatalf("Decide iteration %d changed the uncommitted batch: %#v vs %#v", i+1, batch, first)
		}
	}
	if _, committed := state.CommandBatches[cmd.ID]; committed {
		t.Fatal("fixture unexpectedly had a committed command receipt")
	}
	cleared := make([]ID, 0, recoveryCount)
	for _, event := range first.Events {
		if event.Kind == EventRunBlockCleared {
			cleared = append(cleared, event.BlockerID)
		}
	}
	if len(cleared) != recoveryCount {
		t.Fatalf("exact closure emitted %d recovery blocker clears, want %d", len(cleared), recoveryCount)
	}
	if !sort.SliceIsSorted(cleared, func(i, j int) bool { return cleared[i] < cleared[j] }) {
		t.Fatalf("recovery blocker clears are not sorted: %v", cleared)
	}
}

func TestExactClosureDecideUsesExplicitCauseEventWithDuplicateObservations(t *testing.T) {
	h, input := newExactClosureFixture(t)
	firstCauseEvent := input.Targets[0].CauseEventRef
	firstCause := h.state.EventIDs[firstCauseEvent]
	const duplicateCauseEvent ID = "exact-unknown-cause-duplicate-event"
	state, err := applyBatch(t, h.state, Batch{CommandID: "duplicate-exact-interruption-observation", Events: []Event{{
		ID: duplicateCauseEvent, Kind: EventRunInterrupted, RunID: h.state.RunID, SessionID: h.state.SessionID,
		CauseRef: firstCause.CauseRef, ToolID: firstCause.ToolID, Interrupt: firstCause.Interrupt,
	}}})
	if err != nil {
		t.Fatalf("seed semantically duplicate exact interruption event: %v", err)
	}
	if state.EventIDs[firstCauseEvent].CauseRef != state.EventIDs[duplicateCauseEvent].CauseRef || state.EventIDs[firstCauseEvent].ToolID != state.EventIDs[duplicateCauseEvent].ToolID {
		t.Fatal("fixture events do not represent the same persisted cause and target")
	}
	input.Revision = state.Revision
	input.Targets[0].CauseEventRef = duplicateCauseEvent
	cmd := closureCommandFor(state, "explicit-exact-duplicate-cause-event", CommandAcceptUnknownOutcome, input)
	var first Batch
	for i := 0; i < 32; i++ {
		batch, decideErr := Decide(state, cmd)
		if decideErr != nil {
			t.Fatalf("exact Decide with explicit duplicate event reference at iteration %d: %v", i+1, decideErr)
		}
		if i == 0 {
			first = cloneBatch(batch)
			continue
		}
		if !reflect.DeepEqual(batch, first) {
			t.Fatalf("exact Decide iteration %d changed batch for stable explicit cause event: %#v vs %#v", i+1, batch, first)
		}
	}
	if first.Intent.Closure.Targets[0].CauseEventRef != duplicateCauseEvent {
		t.Fatal("exact closure receipt lost its explicit per-target cause event binding")
	}
	if _, committed := state.CommandBatches[cmd.ID]; committed {
		t.Fatal("fixture unexpectedly had a committed exact-closure command receipt")
	}
}

func TestMetadataClosureRejectsClosedUnavailableDispositionForUnknownUsage(t *testing.T) {
	h := newTransitionHarness(t)
	invocationID, attemptID := h.modelInFlight()
	recovery := h.issue(Command{Kind: CommandAcquireOwnership, Transition: TransitionInput{
		ExecutorToken: 8, ExecutorIncarnation: "writer-inc-2", ReceiptRef: "unknown-usage-recovery-receipt",
		Recoveries: []RecoveryFact{{TargetID: invocationID, AttemptID: attemptID, CauseRef: "unknown-usage-cause", PartialRef: "unknown-usage-partial", Usage: UsageUnknown, BlockerID: "unknown-usage-blocker", PreviousToken: 7, PreviousIncarnation: "writer-inc-1", Fence: 1}},
	}})
	causeEventRef := recovery.Events[2].ID
	cmd := Command{
		ID: "reject-closed-unavailable-unknown-usage", Kind: CommandConfirmRunClosure,
		ExpectedRevision: h.state.Revision, WriterToken: h.state.AdmissionToken, WriterIncarnation: h.state.Incarnation,
		RunID: h.state.RunID, SessionID: h.state.SessionID,
		Closure: ClosureInput{
			PreviewRef: "unknown-usage-preview", ReceiptRef: "unknown-usage-close-receipt",
			PreviewVersion: CurrentClosurePreviewVersion, Revision: h.state.Revision, Actor: ActorUser,
			CauseEventRef: causeEventRef, UnavailableReason: UnavailableVaultLocked,
			PreviousContextRevision: h.state.ContextRevision, ContextRevision: h.state.ContextRevision + 1,
			ContextRequirement: ContextRequirementRevalidationRequired,
			Targets:            []ClosureTargetFact{{Kind: ClosureTargetUnavailableCause, TargetID: attemptID, CauseRef: "unknown-usage-cause", CauseEventRef: causeEventRef}},
		},
	}
	if h.state.ModelAttempts[attemptID].Usage.Status != UsageUnknown {
		t.Fatal("fixture did not retain unknown model usage")
	}
	if _, err := Decide(h.state, cmd); err == nil {
		t.Fatal("metadata closure accepted closed_unavailable for unknown model usage")
	}
}

func TestOwnershipDecisionCopiesCallerRecoveryFacts(t *testing.T) {
	h := newTransitionHarness(t)
	h.issue(Command{Kind: CommandAcceptRun, AcceptRun: AcceptRunInput{InputRef: "ownership-input", Origin: OriginUser, InitialStepLimit: 3}})
	h.issue(Command{Kind: CommandAdmitRun, Admission: AdmissionInput{Token: 7, Incarnation: "writer-inc-1"}})
	h.issue(Command{Kind: CommandStartStep, Step: StepInput{StepID: "ownership-step"}})
	h.issue(Command{Kind: CommandPlanModel, Model: ModelInput{InvocationID: "ownership-model", AttemptID: "ownership-attempt", RequestRef: "ownership-request"}})
	h.issue(Command{Kind: CommandDispatchModel, Model: ModelInput{InvocationID: "ownership-model", AttemptID: "ownership-attempt", RequestRef: "ownership-dispatch", ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Fence: 5}})
	h.issue(Command{Kind: CommandObserveModelStream, Model: ModelInput{InvocationID: "ownership-model", AttemptID: "ownership-attempt", ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Fence: 5}})

	recoveries := []RecoveryFact{{TargetID: "ownership-model", AttemptID: "ownership-attempt", CauseRef: "ownership-interruption", PartialRef: "ownership-partial", Usage: UsageUnknown, BlockerID: "ownership-recovery-blocker", PreviousToken: 7, PreviousIncarnation: "writer-inc-1", Fence: 5}}
	cmd := Command{
		ID: "ownership-recovery-copy", Kind: CommandAcquireOwnership,
		ExpectedRevision: h.state.Revision, RunID: h.state.RunID, SessionID: h.state.SessionID,
		WriterToken: h.state.AdmissionToken, WriterIncarnation: h.state.Incarnation,
		Transition: TransitionInput{ExecutorToken: 8, ExecutorIncarnation: "writer-inc-2", ReceiptRef: "ownership-recovery-receipt", Recoveries: recoveries},
	}
	originalRecovery := recoveries[0]
	originalCommand := cmd
	originalCommand.Transition.Recoveries = append([]RecoveryFact(nil), recoveries...)
	batch, err := Decide(h.state, cmd)
	if err != nil {
		t.Fatalf("Decide ownership recovery: %v", err)
	}
	if !reflect.DeepEqual(batch.Intent.Transition.Recoveries, []RecoveryFact{originalRecovery}) {
		t.Fatal("ownership batch intent did not retain the canonical recovery fact")
	}
	originalBatch := cloneBatch(batch)

	// Mutating the caller-owned Recoveries backing array after Decide must not
	// rewrite either its retained intent or the effects already derived from it.
	recoveries[0].PartialRef = "caller-mutated-partial"
	recoveries[0].CauseRef = "caller-mutated-cause"
	recoveries[0].BlockerID = "caller-mutated-blocker"
	if !reflect.DeepEqual(batch.Intent.Transition.Recoveries, originalBatch.Intent.Transition.Recoveries) || !reflect.DeepEqual(batch.Events, originalBatch.Events) {
		t.Fatal("ownership batch aliases caller-owned recovery facts or events")
	}

	// Restore the caller's intent to its accepted value, apply the batch, and
	// prove receipt replay remains stable after state advances.
	recoveries[0] = originalRecovery
	next, err := Evolve(h.state, batch)
	if err != nil {
		t.Fatalf("apply ownership recovery batch after caller mutation: %v", err)
	}
	if !reflect.DeepEqual(next.CommandBatches[cmd.ID], originalBatch) {
		t.Fatal("applied ownership receipt differs from the canonical Decide batch")
	}
	retry, err := Decide(next, originalCommand)
	if err != nil || !reflect.DeepEqual(retry, originalBatch) {
		t.Fatalf("ownership retry did not return original receipt: %v", err)
	}
}
