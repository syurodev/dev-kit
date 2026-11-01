package kernel

import (
	"reflect"
	"testing"
)

func TestEvolveRejectsMalformedNestedBatchWithoutMutatingInputs(t *testing.T) {
	h := newTransitionHarness(t)
	h.modelInFlight()
	state := h.state

	// The first event is valid and advances a cloned state. The second one is
	// rejected after carrying deeply nested candidate slices in its payload.
	input := Batch{
		CommandID: "malformed-nested", ExpectedRevision: state.Revision,
		WriterToken: state.AdmissionToken, WriterIncarnation: state.Incarnation,
		ProtocolVersion: CurrentProtocolVersion,
		Events: []Event{
			{ID: "first-valid", Kind: EventRunBlockAdded, ProtocolVersion: CurrentProtocolVersion, RunID: state.RunID, SessionID: state.SessionID, BlockerID: "blocker-1", Ref: "block-ref", BlockKind: BlockApproval},
			{ID: "bad-enum", Kind: EventBudgetObserved, ProtocolVersion: CurrentProtocolVersion, RunID: state.RunID, SessionID: state.SessionID, Ref: "usage-ref", Usage: UsageStatus(255), Candidates: []ToolCandidate{{ID: "candidate-1", AttemptID: "candidate-attempt-1", ArgsRef: "args-ref", Descriptor: "descriptor-ref"}}},
		},
	}
	stateBefore, batchBefore := cloneState(state), cloneBatch(input)
	if _, err := Evolve(state, input); err == nil {
		t.Fatal("malformed enum was accepted")
	}
	if !reflect.DeepEqual(state, stateBefore) {
		t.Fatal("failed Evolve mutated caller state")
	}
	if !reflect.DeepEqual(input, batchBefore) {
		t.Fatal("failed Evolve mutated nested caller batch data")
	}
}

func TestDecideBatchCopiesDoNotAliasReceiptOrCallerCommand(t *testing.T) {
	h := newTransitionHarness(t)
	h.modelInFlight()
	candidates := []ToolCandidate{{ID: "tool-ref-1", AttemptID: "tool-attempt-ref-1", ArgsRef: "args-ref-1", Descriptor: "descriptor-ref-1"}}
	cmd := Command{ID: "response-alias", Kind: CommandObserveModelResponse, RunID: h.state.RunID, SessionID: h.state.SessionID,
		ExpectedRevision: h.state.Revision, WriterToken: h.state.AdmissionToken, WriterIncarnation: h.state.Incarnation,
		ModelResponse: ModelResponseInput{InvocationID: "model-1", AttemptID: "model-attempt-1", ResponseRef: "response-ref", Usage: UsageKnown, UsageRef: "usage-ref", Fence: 1, ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Candidates: candidates}}
	batch, err := Decide(h.state, cmd)
	if err != nil {
		t.Fatal(err)
	}
	// Mutating both input and output slices must not affect the receipt produced
	// by applying the original batch.
	candidates[0].ID = "caller-mutated-candidate"
	cmd.ModelResponse.Candidates[0].ArgsRef = "caller-mutated-args"
	if batch.Intent.ModelResponse.Candidates[0].ID != "tool-ref-1" || batch.Events[0].Candidates[0].ArgsRef != "args-ref-1" {
		t.Fatal("Decide output aliases caller command slices")
	}
	batch.Events[0].Candidates[0].Descriptor = "returned-batch-mutated"
	batch.Intent.ModelResponse.Candidates[0].AttemptID = "returned-intent-mutated"
	next, err := Evolve(h.state, batch)
	if err == nil {
		t.Fatal("mutated batch unexpectedly remained valid")
	}
	// Decide itself has no retained mutable state. Obtain a fresh canonical
	// intent and ensure Evolve's retained receipt is isolated from its caller.
	cmd.ModelResponse.Candidates = []ToolCandidate{{ID: "tool-ref-1", AttemptID: "tool-attempt-ref-1", ArgsRef: "args-ref-1", Descriptor: "descriptor-ref-1"}}
	batch, err = Decide(h.state, cmd)
	if err != nil {
		t.Fatal(err)
	}
	next, err = Evolve(h.state, batch)
	if err != nil {
		t.Fatal(err)
	}
	stored := cloneBatch(next.CommandBatches[cmd.ID])
	batch.Events[0].Candidates[0].ID = "mutated-after-evolve"
	batch.Intent.ModelResponse.Candidates[0].ID = "mutated-intent-after-evolve"
	if !reflect.DeepEqual(next.CommandBatches[cmd.ID], stored) {
		t.Fatal("returned batch aliases retained command receipt")
	}
}

func TestDecideDuplicateOriginalCommandSurvivesLaterStateAndReturnedMutation(t *testing.T) {
	h := newTransitionHarness(t)
	cmd := Command{ID: "early-accept", Kind: CommandAcceptRun, RunID: "duplicate-run", SessionID: "duplicate-session",
		AcceptRun: AcceptRunInput{InputRef: "input-ref", Origin: OriginUser, InitialStepLimit: 5}}
	cmd.ExpectedRevision = h.state.Revision
	original, err := Decide(h.state, cmd)
	if err != nil {
		t.Fatal(err)
	}
	h.state, err = Evolve(h.state, original)
	if err != nil {
		t.Fatal(err)
	}
	h.issue(Command{RunID: cmd.RunID, SessionID: cmd.SessionID, Kind: CommandAdmitRun, Admission: AdmissionInput{Token: 7, Incarnation: "writer-inc-1"}})
	h.issue(Command{RunID: cmd.RunID, SessionID: cmd.SessionID, Kind: CommandStartStep, Step: StepInput{StepID: "later-step"}})

	dup, err := Decide(h.state, cmd)
	if err != nil || !reflect.DeepEqual(dup, original) {
		t.Fatalf("duplicate command did not return original batch after advancement: err=%v", err)
	}
	dup.Events[0].InputRef = "mutated-retry"
	again, err := Decide(h.state, cmd)
	if err != nil || !reflect.DeepEqual(again, original) {
		t.Fatalf("mutating duplicate response altered retained batch: err=%v", err)
	}
}

func TestDecideRejectsInactiveTypedUnionFields(t *testing.T) {
	for name, cmd := range map[string]Command{
		"model plan executor fields": {ID: "inactive-model-plan", Kind: CommandPlanModel, RunID: "r", SessionID: "s", Model: ModelInput{
			InvocationID: "model-id", AttemptID: "attempt-id", RequestRef: "request-ref", ExecutorToken: 7, ExecutorIncarnation: "writer-inc", Fence: 9,
		}},
		"tool prepare attempt fields": {ID: "inactive-tool-prepare", Kind: CommandPrepareTool, RunID: "r", SessionID: "s", Tool: ToolInput{
			ToolID: "tool-id", Generation: 1, Ref: "check-ref", AttemptID: "attempt-id", ArgsRef: "args-ref", ApprovalRef: "approval-ref", ReceiptRef: "receipt-ref",
		}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Decide(EmptyState(), cmd); err == nil {
				t.Fatal("Decide accepted inactive union fields")
			}
		})
	}
}

func TestCloneBatchPreservesNilAndEmptySliceSemantics(t *testing.T) {
	for name, candidates := range map[string][]ToolCandidate{"nil": nil, "empty": {}} {
		got := cloneBatch(Batch{Intent: Command{ModelResponse: ModelResponseInput{Candidates: candidates}}})
		if (candidates == nil) != (got.Intent.ModelResponse.Candidates == nil) {
			t.Fatalf("%s candidates changed nil/empty representation", name)
		}
	}
	for name, recoveries := range map[string][]RecoveryFact{"nil": nil, "empty": {}} {
		got := cloneBatch(Batch{Intent: Command{Transition: TransitionInput{Recoveries: recoveries}}})
		if (recoveries == nil) != (got.Intent.Transition.Recoveries == nil) {
			t.Fatalf("%s recoveries changed nil/empty representation", name)
		}
	}
	for name, outcomes := range map[string][]UnknownOutcomeFact{"nil": nil, "empty": {}} {
		got := cloneBatch(Batch{Intent: Command{Transition: TransitionInput{UnknownOutcomes: outcomes}}})
		if (outcomes == nil) != (got.Intent.Transition.UnknownOutcomes == nil) {
			t.Fatalf("%s unknown outcomes changed nil/empty representation", name)
		}
	}
	for name, candidates := range map[string][]ToolCandidate{"nil": nil, "empty": {}} {
		got := cloneBatch(Batch{Events: []Event{{Candidates: candidates}}})
		if (candidates == nil) != (got.Events[0].Candidates == nil) {
			t.Fatalf("%s event candidates changed nil/empty representation", name)
		}
	}
	outcomes := []UnknownOutcomeFact{{ToolID: "tool-ref", AttemptID: "attempt-ref", CauseRef: "cause-ref", ResultRef: "result-ref"}}
	cloned := cloneBatch(Batch{Intent: Command{Transition: TransitionInput{UnknownOutcomes: outcomes}}})
	outcomes[0].CauseRef = "mutated-cause"
	if cloned.Intent.Transition.UnknownOutcomes[0].CauseRef != "cause-ref" {
		t.Fatal("cloned unknown outcome aliases the caller slice")
	}
}

func TestEvolveUnknownPersistedEventScalarsNeverPanic(t *testing.T) {
	// These are valid states created through the reducer, not forged projections.
	states := []State{EmptyState()}
	accepted, err := applyBatch(t, EmptyState(), Batch{CommandID: "scalar-seed-accept", Events: []Event{
		{ID: "scalar-seed-event", Kind: EventRunAccepted, RunID: "scalar-run", SessionID: "scalar-session", InputRef: "input-ref", Origin: OriginUser},
	}})
	if err != nil {
		t.Fatal(err)
	}
	states = append(states, accepted)
	h := newTransitionHarness(t)
	h.modelInFlight()
	states = append(states, h.state)
	for _, state := range states {
		for kind := uint8(0); kind < 80; kind++ {
			batch := Batch{CommandID: ID("scalar-command"), ExpectedRevision: state.Revision, ProtocolVersion: CurrentProtocolVersion,
				WriterToken: state.AdmissionToken, WriterIncarnation: state.Incarnation,
				Events: []Event{{ID: "scalar-event", Kind: EventKind(kind), ProtocolVersion: CurrentProtocolVersion, RunID: state.RunID, SessionID: state.SessionID,
					Origin: RunOrigin(kind), Usage: UsageStatus(kind), Next: NextAction(kind), Continuation: ContinuationReason(kind),
					StopReason: StopReason(kind), StopMode: StopMode(kind), Outcome: Outcome(kind), Disposition: ResultDisposition(kind),
					Exposure: Exposure(kind), Interrupt: InterruptionKind(kind), BudgetKind: BudgetKind(kind), Actor: ActorKind(kind),
					Purpose: TimerPurpose(kind), BlockKind: BlockKind(kind)}}}
			func() {
				defer func() {
					if recovered := recover(); recovered != nil {
						t.Fatalf("Evolve panicked for persisted event scalar %d: %v", kind, recovered)
					}
				}()
				_, _ = Evolve(state, batch)
			}()
		}
	}
}
