package kernel

import (
	"reflect"
	"testing"
)

// traceCRecoverySeed reproduces Trace B's post-restart R2 state with typed
// opaque refs. TA1 is unknown under the new owner; both vault and recovery
// admission dependencies remain open.
func traceCRecoverySeed(t *testing.T) State {
	t.Helper()
	s := EmptyState()
	s.Accepted, s.Admitted, s.RunID, s.SessionID = true, true, "r2", "s2"
	s.AdmissionToken, s.Incarnation = 7, "old-inc"
	s.Models["m1"] = ModelInvocation{ID: "m1", StepID: "step1", State: InvocationCompleted, AttemptIDs: []ID{"a1"}, ResponseRef: "response-ref", CandidateIDs: []ID{"t1"}, Candidates: []ToolCandidate{{ID: "t1", AttemptID: "ta1", ArgsRef: "protected-args-ref", Descriptor: "descriptor-ref", SourceIndex: 0}}}
	s.ModelAttempts["a1"] = Attempt{ID: "a1", InvocationID: "m1", StepID: "step1", State: AttemptCompleted}
	s.Tools["t1"] = ToolInvocation{ID: "t1", StepID: "step1", State: ToolDispatching, Descriptor: "descriptor-ref", ArgsRef: "protected-args-ref", AttemptIDs: []ID{"ta1"}, SelectedAttempt: "ta1", SourceIndex: 0, EffectClass: EffectSideEffecting}
	s.ToolAttempts["ta1"] = ToolAttempt{ID: "ta1", ToolID: "t1", StepID: "step1", State: AttemptRequesting, Token: 7, Incarnation: "old-inc", Fence: 14}
	response := TranscriptItem{InvocationID: "m1", ResponseRef: "response-ref", IsModel: true}
	s.Steps = []Step{{ID: "step1", Open: true, ModelID: "m1", ResponseRef: "response-ref", ToolIDs: []ID{"t1"}, Transcript: []TranscriptItem{response}}}
	s.Transcript = []TranscriptItem{response}
	s.CurrentStep = 0
	derive(&s)
	traceB, err := applyBatch(t, s, Batch{CommandID: "trace-b-recovery", Events: []Event{
		{ID: "owner", Kind: EventRunOwnershipAcquired, RunID: "r2", SessionID: "s2", PreviousToken: 7, PreviousIncarnation: "old-inc", Token: 8, Incarnation: "new-inc", ReceiptRef: "ownership-receipt"},
		{ID: "vault-block", Kind: EventRunBlockAdded, RunID: "r2", SessionID: "s2", BlockerID: "vault-blocker", BlockKind: BlockVaultLocked, Ref: "vault-observation"},
		{ID: "recovery-block", Kind: EventRunBlockAdded, RunID: "r2", SessionID: "s2", BlockerID: "recovery-blocker", BlockKind: BlockRecoveryAdmission, Ref: "recovery-receipt"},
		{ID: "unknown", Kind: EventToolOutcomeUnknown, RunID: "r2", SessionID: "s2", ToolID: "t1", AttemptID: "ta1", CauseRef: "tool-effect-unknown", Exposure: ExposureUnknown, PreviousToken: 7, PreviousIncarnation: "old-inc", Token: 8, Incarnation: "new-inc", Fence: 14, ReceiptRef: "ownership-receipt"},
		{ID: "interrupted", Kind: EventRunInterrupted, RunID: "r2", SessionID: "s2", CauseRef: "tool-effect-unknown", ToolID: "t1", Interrupt: InterruptEffectUnknown},
	}})
	if err != nil {
		t.Fatalf("Trace B recovery seed rejected: %v", err)
	}
	return traceB
}

func TestTraceCMetadataClosureReplaysExactFiveEventBatch(t *testing.T) {
	state := traceCRecoverySeed(t)
	if state.AdmissionToken != 8 || state.Incarnation != "new-inc" || state.Tools["t1"].State != ToolUnknown || state.ToolAttempts["ta1"].Fence != 14 || state.Exposures["t1"] != ExposureUnknown || len(state.Blockers) != 2 {
		t.Fatalf("Trace B seed lost recovery facts: token=%d incarnation=%s tool=%d fence=%d exposure=%d blockers=%d", state.AdmissionToken, state.Incarnation, state.Tools["t1"].State, state.ToolAttempts["ta1"].Fence, state.Exposures["t1"], len(state.Blockers))
	}
	responseHistory := append([]TranscriptItem(nil), state.Transcript...)

	// This is Trace C's atomic metadata-only close. RunClosureConfirmed is the
	// trusted user authorization for abandoning this exact unknown effect; the
	// recovery clear records the typed accepted-unknown disposition.
	batch := Batch{CommandID: "trace-c-close", WriterToken: state.AdmissionToken, WriterIncarnation: state.Incarnation, Events: []Event{
		{ID: "close-confirmed", Kind: EventRunClosureConfirmed, RunID: state.RunID, SessionID: state.SessionID, Ref: "metadata-preview-ref", ReceiptRef: "closure-receipt", Actor: ActorUser, Revision: state.Revision, ToolID: "t1", AttemptID: "ta1", CauseRef: "tool-effect-unknown", Accepted: true, Disposition: ResultAbandonedUnknown, RecoveryDisposition: RecoveryDispositionAcceptedUnknown, PreviewMode: ClosurePreviewMetadataOnly, PreviewVersion: 1, UnavailableReason: UnavailableVaultLocked, PreviousContextRevision: state.ContextRevision, ContextRevision: state.ContextRevision + 1, ContextRequirement: ContextRequirementRevalidationRequired, CauseEventRef: "interrupted"},
		{ID: "tool-abandoned-unknown", Kind: EventToolResultRecorded, RunID: state.RunID, SessionID: state.SessionID, ToolID: "t1", AttemptID: "ta1", CauseRef: "tool-effect-unknown", Ref: "unknown-disposition-ref", ReceiptRef: "closure-receipt", Disposition: ResultAbandonedUnknown, Exposure: ExposureUnknown, Token: 8, Incarnation: "new-inc", Fence: 14},
		{ID: "recovery-accepted-unknown", Kind: EventRunRecoveryCleared, RunID: state.RunID, SessionID: state.SessionID, ToolID: "t1", AttemptID: "ta1", CauseRef: "tool-effect-unknown", Ref: "closure-receipt", ReceiptRef: "closure-receipt", Actor: ActorUser, Accepted: true, Disposition: ResultAbandonedUnknown, RecoveryDisposition: RecoveryDispositionAcceptedUnknown},
		{ID: "step-unavailable-close", Kind: EventStepAborted, RunID: state.RunID, SessionID: state.SessionID, Ref: "unavailable-close-ref", CauseRef: "payload-unavailable-cause"},
		{ID: "run-abandoned-unknown", Kind: EventRunFinished, RunID: state.RunID, SessionID: state.SessionID, Outcome: OutcomeAbandonedUnknown},
	}}
	wantKinds := []EventKind{EventRunClosureConfirmed, EventToolResultRecorded, EventRunRecoveryCleared, EventStepAborted, EventRunFinished}
	if len(batch.Events) != len(wantKinds) {
		t.Fatalf("Trace C batch has %d events, want exactly %d", len(batch.Events), len(wantKinds))
	}
	for i, want := range wantKinds {
		if batch.Events[i].Kind != want {
			t.Fatalf("Trace C event %d kind=%d, want %d", i+1, batch.Events[i].Kind, want)
		}
	}
	closure, toolResult, recoveryClear, stepAbort, runFinish := batch.Events[0], batch.Events[1], batch.Events[2], batch.Events[3], batch.Events[4]
	if closure.Ref != "metadata-preview-ref" || closure.Revision != state.Revision || closure.ToolID != "t1" || closure.AttemptID != "ta1" || closure.CauseRef != "tool-effect-unknown" || closure.Disposition != ResultAbandonedUnknown || !closure.Accepted || closure.PreviewMode != ClosurePreviewMetadataOnly || closure.PreviewVersion != 1 || closure.UnavailableReason != UnavailableVaultLocked || closure.PreviousContextRevision != state.ContextRevision || closure.ContextRevision != state.ContextRevision+1 || closure.ContextRequirement != ContextRequirementRevalidationRequired || closure.CauseEventRef != "interrupted" {
		t.Fatal("Trace C closure lost metadata preview, exact unknown target, revision, or disposition facts")
	}
	if toolResult.ToolID != "t1" || toolResult.AttemptID != "ta1" || toolResult.CauseRef != "tool-effect-unknown" || toolResult.Disposition != ResultAbandonedUnknown || toolResult.Exposure != ExposureUnknown {
		t.Fatal("Trace C result lost exact unknown attempt disposition")
	}
	if recoveryClear.ToolID != "t1" || recoveryClear.AttemptID != "ta1" || recoveryClear.CauseRef != "tool-effect-unknown" || recoveryClear.RecoveryDisposition != RecoveryDispositionAcceptedUnknown {
		t.Fatal("Trace C recovery clear lost accepted-unknown disposition")
	}
	if stepAbort.Ref != "unavailable-close-ref" || runFinish.Outcome != OutcomeAbandonedUnknown {
		t.Fatal("Trace C did not preserve unavailable-close reason and abandoned-unknown outcome")
	}
	closed, err := applyBatch(t, state, batch)
	if err != nil {
		diagnostic := cloneState(state)
		for i, event := range batch.Events {
			if eventErr := applyEvent(&diagnostic, event); eventErr != nil {
				t.Fatalf("Trace C five-event close batch rejected at event %d/%d: %v (batch: %v)", i+1, len(batch.Events), eventErr, err)
			}
			diagnostic.Revision++
		}
		t.Fatalf("Trace C five-event close batch rejected by projection invariants: %v", err)
	}
	if !closed.Finished || closed.Outcome != OutcomeAbandonedUnknown || !closed.ClosureConfirmed {
		t.Fatal("Trace C did not finish through metadata-only closure")
	}
	if len(closed.EventIDs)-len(state.EventIDs) != 5 {
		t.Fatalf("Trace C close was not one exact five-event transaction: events=%d", len(closed.EventIDs)-len(state.EventIDs))
	}
	if closed.ToolAttempts["ta1"].State != AttemptAbandoned || closed.ToolAttempts["ta1"].Fence != 14 || closed.Exposures["t1"] != ExposureUnknown {
		t.Fatal("Trace C erased the original unknown attempt or treated it as a successful effect")
	}
	if closed.Tools["t1"].Disposition != ResultAbandonedUnknown || closed.Tools["t1"].State == ToolSucceeded {
		t.Fatal("Trace C projected a fake tool success")
	}
	if !reflect.DeepEqual(closed.Transcript[:len(responseHistory)], responseHistory) {
		t.Fatal("Trace C close changed prior response history")
	}
	if closed.AdmissionToken != 8 || closed.Incarnation != "new-inc" {
		t.Fatal("Trace C close changed the current writer fence")
	}
	if closed.ClosurePreviewMode != ClosurePreviewMetadataOnly || closed.ClosurePreviewVersion != 1 || closed.ClosureUnavailableReason != UnavailableVaultLocked || closed.ContextRevision != state.ContextRevision+1 || closed.ContextRequirement != ContextRequirementRevalidationRequired {
		t.Fatal("Trace C did not project metadata-only vault-locked context revalidation")
	}
	interruption := closed.Interruptions["tool-effect-unknown"]
	if interruption.Disposition != RecoveryDispositionAcceptedUnknown || !interruption.Accepted || interruption.Resolved {
		t.Fatal("Trace C did not retain typed accepted-unknown cause disposition")
	}
	if len(closed.Blockers) != 0 {
		t.Fatalf("Trace C left recovery dependencies after closure: %#v", closed.Blockers)
	}
}
