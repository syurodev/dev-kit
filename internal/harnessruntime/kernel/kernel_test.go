package kernel

import (
	"reflect"
	"testing"
)

func applyBatch(t *testing.T, state State, batch Batch) (State, error) {
	t.Helper()
	batch.ExpectedRevision = state.Revision
	batch.ProtocolVersion = CurrentProtocolVersion
	for i := range batch.Events {
		batch.Events[i].ProtocolVersion = CurrentProtocolVersion
	}
	if state.Admitted {
		batch.WriterToken, batch.WriterIncarnation = state.AdmissionToken, state.Incarnation
		for _, event := range batch.Events {
			if event.Kind == EventRunOwnershipAcquired {
				batch.WriterToken, batch.WriterIncarnation = event.Token, event.Incarnation
			}
		}
	} else {
		for _, event := range batch.Events {
			if event.Kind == EventRunAdmitted {
				batch.WriterToken, batch.WriterIncarnation = event.Token, event.Incarnation
			}
		}
	}
	return Evolve(state, batch)
}

func ev(id ID, kind EventKind) Event { return Event{ID: id, Kind: kind, RunID: "r1", SessionID: "s1"} }

func TestTraceA22ModelToolModelFinishAndReplay(t *testing.T) {
	state := EmptyState()
	steps := []Batch{
		{CommandID: "c01", Events: []Event{{ID: "e01", Kind: EventRunAccepted, RunID: "r1", SessionID: "s1", InputRef: "input", Origin: OriginUser}}},
		{CommandID: "c02", Events: []Event{
			{ID: "e02", Kind: EventRunAdmitted, RunID: "r1", SessionID: "s1", Token: 7, Incarnation: "inc1"},
			{ID: "e03", Kind: EventStepStarted, RunID: "r1", SessionID: "s1", StepID: "step1", Generation: 1},
			{ID: "e04", Kind: EventModelRequestPlanned, RunID: "r1", SessionID: "s1", InvocationID: "m1", AttemptID: "a1", Ref: "request1"},
		}},
		{CommandID: "c05", Events: []Event{{ID: "e05", Kind: EventModelDispatchStarted, RunID: "r1", SessionID: "s1", InvocationID: "m1", AttemptID: "a1", Ref: "dispatch1", Token: 7, Incarnation: "inc1", Fence: 1}}},
		{CommandID: "c06", Events: []Event{{ID: "e06", Kind: EventModelStreamStarted, RunID: "r1", SessionID: "s1", InvocationID: "m1", AttemptID: "a1", Token: 7, Incarnation: "inc1", Fence: 1}}},
		{CommandID: "c07", Events: []Event{
			{ID: "e07", Kind: EventModelResponseRecorded, RunID: "r1", SessionID: "s1", InvocationID: "m1", AttemptID: "a1", Ref: "response1", Usage: UsageKnown, CauseRef: "usage1", Token: 7, Incarnation: "inc1", Fence: 1, Candidates: []ToolCandidate{{ID: "tool1", AttemptID: "ta1", ArgsRef: "args1", Descriptor: "descriptor1", SourceIndex: 0}}},
			{ID: "e08", Kind: EventBudgetObserved, RunID: "r1", SessionID: "s1", InvocationID: "m1", AttemptID: "a1", Ref: "usage2", Usage: UsageKnown},
			{ID: "e09", Kind: EventToolCallPlanned, RunID: "r1", SessionID: "s1", InvocationID: "m1", ToolID: "tool1", AttemptID: "ta1", Ref: "descriptor1", CauseRef: "args1", SourceIndex: 0},
		}},
		{CommandID: "c10", Events: []Event{{ID: "e10", Kind: EventToolCheckStarted, RunID: "r1", SessionID: "s1", ToolID: "tool1", Generation: 1, Ref: "check1"}}},
		{CommandID: "c11", Events: []Event{{ID: "e11", Kind: EventToolReadyRecorded, RunID: "r1", SessionID: "s1", ToolID: "tool1", Generation: 1, Ref: "args1"}}},
		{CommandID: "c12", Events: []Event{{ID: "e12", Kind: EventToolDispatchStarted, RunID: "r1", SessionID: "s1", ToolID: "tool1", AttemptID: "ta1", ReceiptRef: "grant1", Ref: "dispatch2", Generation: 1, Token: 7, Incarnation: "inc1", Fence: 2}}},
		{CommandID: "c13", Events: []Event{
			{ID: "e13", Kind: EventToolResultRecorded, RunID: "r1", SessionID: "s1", ToolID: "tool1", AttemptID: "ta1", Ref: "result1", Disposition: ResultSucceeded, Token: 7, Incarnation: "inc1", Fence: 2},
			{ID: "e14", Kind: EventStepCompleted, RunID: "r1", SessionID: "s1", Next: NextContinue, Continuation: ContinuationToolCalls},
			{ID: "e15", Kind: EventStepStarted, RunID: "r1", SessionID: "s1", StepID: "step2", Generation: 2},
			{ID: "e16", Kind: EventModelRequestPlanned, RunID: "r1", SessionID: "s1", InvocationID: "m2", AttemptID: "a2", Ref: "request2"},
		}},
		{CommandID: "c17", Events: []Event{{ID: "e17", Kind: EventModelDispatchStarted, RunID: "r1", SessionID: "s1", InvocationID: "m2", AttemptID: "a2", Ref: "dispatch3", Token: 7, Incarnation: "inc1", Fence: 3}}},
		{CommandID: "c18", Events: []Event{{ID: "e18", Kind: EventModelStreamStarted, RunID: "r1", SessionID: "s1", InvocationID: "m2", AttemptID: "a2", Token: 7, Incarnation: "inc1", Fence: 3}}},
		{CommandID: "c19", Events: []Event{
			{ID: "e19", Kind: EventModelResponseRecorded, RunID: "r1", SessionID: "s1", InvocationID: "m2", AttemptID: "a2", Ref: "response2", Usage: UsageKnown, CauseRef: "usage3", Token: 7, Incarnation: "inc1", Fence: 3},
			{ID: "e20", Kind: EventBudgetObserved, RunID: "r1", SessionID: "s1", InvocationID: "m2", AttemptID: "a2", Ref: "usage4", Usage: UsageKnown},
			{ID: "e21", Kind: EventStepCompleted, RunID: "r1", SessionID: "s1", Next: NextEnd, Continuation: ContinuationNaturalEnd},
			{ID: "e22", Kind: EventRunFinished, RunID: "r1", SessionID: "s1", Outcome: OutcomeCompleted},
		}},
	}
	count := 0
	for _, batch := range steps {
		var err error
		state, err = applyBatch(t, state, batch)
		if err != nil {
			t.Fatalf("%s: %v", batch.CommandID, err)
		}
		count += len(batch.Events)
	}
	if count != 22 || state.Lifecycle != Finished || len(state.Transcript) != 3 {
		t.Fatalf("trace projection mismatch: count=%d lifecycle=%d transcript=%d", count, state.Lifecycle, len(state.Transcript))
	}
	replayed := EmptyState()
	for _, batch := range steps {
		var err error
		replayed, err = applyBatch(t, replayed, batch)
		if err != nil {
			t.Fatalf("replay %s: %v", batch.CommandID, err)
		}
	}
	if !reflect.DeepEqual(state, replayed) {
		t.Fatal("replay changed the resulting state")
	}
	dup, err := Evolve(state, state.CommandBatches[steps[len(steps)-1].CommandID])
	if err != nil || !reflect.DeepEqual(state, dup) {
		v, w := reflect.ValueOf(state), reflect.ValueOf(dup)
		for i := 0; i < v.NumField(); i++ {
			if !reflect.DeepEqual(v.Field(i).Interface(), w.Field(i).Interface()) {
				t.Logf("different field: %s", reflect.TypeOf(state).Field(i).Name)
			}
		}
		t.Fatalf("exact batch retry was not idempotent (err=%v commandEqual=%v)", err, reflect.DeepEqual(state.CommandBatches[steps[len(steps)-1].CommandID], steps[len(steps)-1]))
	}
}

func TestDecideDrivesTraceAAndReplayConsumesItsBatches(t *testing.T) {
	state := EmptyState()
	var batches []Batch
	issue := func(c Command) {
		t.Helper()
		c.ExpectedRevision = state.Revision
		if state.Admitted {
			c.WriterToken, c.WriterIncarnation = state.AdmissionToken, state.Incarnation
		} else if c.Kind == CommandAdmitRun {
			c.WriterToken, c.WriterIncarnation = c.Admission.Token, c.Admission.Incarnation
		}
		batch, err := Decide(state, c)
		if err != nil {
			t.Fatalf("decide %d: %v", c.Kind, err)
		}
		state, err = Evolve(state, batch)
		if err != nil {
			t.Fatalf("evolve %d: %v", c.Kind, err)
		}
		batches = append(batches, batch)
	}
	issue(Command{ID: "a01", Kind: CommandAcceptRun, RunID: "r1", SessionID: "s1", AcceptRun: AcceptRunInput{InputRef: "input", Origin: OriginUser, InitialStepLimit: 100}})
	issue(Command{ID: "a02", Kind: CommandAdmitRun, RunID: "r1", SessionID: "s1", Admission: AdmissionInput{Token: 7, Incarnation: "inc1"}})
	issue(Command{ID: "a03", Kind: CommandStartStep, RunID: "r1", SessionID: "s1", Step: StepInput{StepID: "step1"}})
	issue(Command{ID: "a04", Kind: CommandPlanModel, RunID: "r1", SessionID: "s1", Model: ModelInput{InvocationID: "m1", AttemptID: "a1", RequestRef: "request1"}})
	issue(Command{ID: "a05", Kind: CommandDispatchModel, RunID: "r1", SessionID: "s1", Model: ModelInput{InvocationID: "m1", AttemptID: "a1", RequestRef: "dispatch1", ExecutorToken: 7, ExecutorIncarnation: "inc1", Fence: 1}})
	issue(Command{ID: "a06", Kind: CommandObserveModelStream, RunID: "r1", SessionID: "s1", Model: ModelInput{InvocationID: "m1", AttemptID: "a1", ExecutorToken: 7, ExecutorIncarnation: "inc1", Fence: 1}})
	issue(Command{ID: "a07", Kind: CommandObserveModelResponse, RunID: "r1", SessionID: "s1", ModelResponse: ModelResponseInput{InvocationID: "m1", AttemptID: "a1", ResponseRef: "response1", Usage: UsageKnown, UsageRef: "usage1", Fence: 1, ExecutorToken: 7, ExecutorIncarnation: "inc1", Candidates: []ToolCandidate{{ID: "tool1", AttemptID: "ta1", ArgsRef: "args1", Descriptor: "descriptor1", SourceIndex: 0}}}})
	issue(Command{ID: "a10", Kind: CommandPrepareTool, RunID: "r1", SessionID: "s1", Tool: ToolInput{ToolID: "tool1", Generation: 1, Ref: "check1"}})
	issue(Command{ID: "a11", Kind: CommandReadyTool, RunID: "r1", SessionID: "s1", Tool: ToolInput{ToolID: "tool1", Generation: 1, ArgsRef: "args1"}})
	issue(Command{ID: "a12", Kind: CommandDispatchTool, RunID: "r1", SessionID: "s1", Tool: ToolInput{ToolID: "tool1", AttemptID: "ta1", ReceiptRef: "grant1", Ref: "dispatch2", Generation: 1, ExecutorToken: 7, ExecutorIncarnation: "inc1", Fence: 2}})
	issue(Command{ID: "a13", Kind: CommandObserveToolResult, RunID: "r1", SessionID: "s1", ToolResult: ToolResultInput{ToolID: "tool1", AttemptID: "ta1", ResultRef: "result1", Disposition: ResultSucceeded, ExecutorToken: 7, ExecutorIncarnation: "inc1", Fence: 2}})
	issue(Command{ID: "a14", Kind: CommandFinishStep, RunID: "r1", SessionID: "s1", FinishStep: FinishStepInput{Next: NextContinue, Continuation: ContinuationToolCalls}})
	issue(Command{ID: "a15", Kind: CommandStartStep, RunID: "r1", SessionID: "s1", Step: StepInput{StepID: "step2"}})
	issue(Command{ID: "a16", Kind: CommandPlanModel, RunID: "r1", SessionID: "s1", Model: ModelInput{InvocationID: "m2", AttemptID: "a2", RequestRef: "request2"}})
	issue(Command{ID: "a17", Kind: CommandDispatchModel, RunID: "r1", SessionID: "s1", Model: ModelInput{InvocationID: "m2", AttemptID: "a2", RequestRef: "dispatch3", ExecutorToken: 7, ExecutorIncarnation: "inc1", Fence: 3}})
	issue(Command{ID: "a18", Kind: CommandObserveModelStream, RunID: "r1", SessionID: "s1", Model: ModelInput{InvocationID: "m2", AttemptID: "a2", ExecutorToken: 7, ExecutorIncarnation: "inc1", Fence: 3}})
	issue(Command{ID: "a19", Kind: CommandObserveModelResponse, RunID: "r1", SessionID: "s1", ModelResponse: ModelResponseInput{InvocationID: "m2", AttemptID: "a2", ResponseRef: "response2", Usage: UsageKnown, UsageRef: "usage3", Fence: 3, ExecutorToken: 7, ExecutorIncarnation: "inc1"}})
	issue(Command{ID: "a21", Kind: CommandFinishStep, RunID: "r1", SessionID: "s1", FinishStep: FinishStepInput{Next: NextEnd, Continuation: ContinuationNaturalEnd}})
	issue(Command{ID: "a22", Kind: CommandFinishRun, RunID: "r1", SessionID: "s1", FinishRun: FinishRunInput{Outcome: OutcomeCompleted}})
	var eventCount int
	for _, b := range batches {
		eventCount += len(b.Events)
	}
	if eventCount != 22 || state.Lifecycle != Finished || len(state.Transcript) != 3 {
		t.Fatalf("command trace facts mismatch: events=%d lifecycle=%d transcript=%d", eventCount, state.Lifecycle, len(state.Transcript))
	}
	if again, err := Decide(state, Command{ID: batches[len(batches)-1].CommandID, Kind: CommandFinishRun, RunID: "r1", SessionID: "s1", ExpectedRevision: batches[len(batches)-1].ExpectedRevision, WriterToken: 7, WriterIncarnation: "inc1", FinishRun: FinishRunInput{Outcome: OutcomeCompleted}}); err != nil || !reflect.DeepEqual(again, batches[len(batches)-1]) {
		t.Fatal("command retry did not return its original batch")
	}
	replayed := EmptyState()
	for _, batch := range batches {
		var err error
		replayed, err = Evolve(replayed, batch)
		if err != nil {
			t.Fatalf("replay %s: %v", batch.CommandID, err)
		}
	}
	if !reflect.DeepEqual(state, replayed) {
		t.Fatal("replay diverged from command-decided state")
	}
}

func TestKernelRejectsInvalidTransitions(t *testing.T) {
	tests := []struct {
		name  string
		batch Batch
	}{
		{"model dispatch before admission", Batch{CommandID: "c1", Events: []Event{{ID: "e1", Kind: EventRunAccepted, RunID: "r", SessionID: "s", InputRef: "i", Origin: OriginUser}, {ID: "e2", Kind: EventModelDispatchStarted, RunID: "r", SessionID: "s", InvocationID: "m", AttemptID: "a", Token: 1, Incarnation: "inc", Fence: 1, Ref: "d"}}}},
		{"malformed id", Batch{CommandID: "c1", Events: []Event{{ID: "bad id", Kind: EventRunAccepted, RunID: "r", SessionID: "s", InputRef: "i", Origin: OriginUser}}}},
		{"unknown continuation", Batch{CommandID: "c1", Events: []Event{{ID: "e1", Kind: EventRunAccepted, RunID: "r", SessionID: "s", InputRef: "i", Origin: OriginGoal}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := applyBatch(t, EmptyState(), tt.batch); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestEventIdentityVersionAndExpectedRevisionGuards(t *testing.T) {
	state, err := applyBatch(t, EmptyState(), Batch{CommandID: "first", Events: []Event{{ID: "same", Kind: EventRunAccepted, RunID: "r", SessionID: "s", InputRef: "i", Origin: OriginUser}}})
	if err != nil {
		t.Fatal(err)
	}
	if state.EventIDs["same"].ProtocolVersion != CurrentProtocolVersion {
		t.Fatal("event protocol version was not preserved")
	}
	if _, err := applyBatch(t, state, Batch{CommandID: "second", Events: []Event{{ID: "same", Kind: EventRunAccepted, RunID: "r", SessionID: "s", InputRef: "i", Origin: OriginUser}}}); err == nil {
		t.Fatal("same event ID was accepted under a second command")
	}
	if _, err := Evolve(state, Batch{CommandID: "bad-version", ExpectedRevision: state.Revision, ProtocolVersion: CurrentProtocolVersion + 1, Events: []Event{{ID: "v2", Kind: EventRunAdmitted}}}); err == nil {
		t.Fatal("unsupported batch protocol version was accepted")
	}
	if _, err := Evolve(state, Batch{CommandID: "missing-event-version", ExpectedRevision: state.Revision, ProtocolVersion: CurrentProtocolVersion, Events: []Event{{ID: "v2", Kind: EventRunAdmitted}}}); err == nil {
		t.Fatal("missing event protocol version was accepted")
	}
	if _, err := Evolve(state, Batch{CommandID: "bad-event-version", ExpectedRevision: state.Revision, ProtocolVersion: CurrentProtocolVersion, Events: []Event{{ID: "v2", Kind: EventRunAdmitted, ProtocolVersion: CurrentProtocolVersion + 1}}}); err == nil {
		t.Fatal("unsupported event protocol version was accepted")
	}
	if _, err := Evolve(state, Batch{CommandID: "stale-revision", ExpectedRevision: 0, ProtocolVersion: CurrentProtocolVersion, Events: []Event{{ID: "stale", Kind: EventRunBlockAdded}}}); err == nil {
		t.Fatal("stale expected revision was accepted")
	}
}

func TestReplayFailureDoesNotMutateCallerBatch(t *testing.T) {
	state := EmptyState()
	input := Batch{CommandID: "failed-input", ExpectedRevision: 0, ProtocolVersion: CurrentProtocolVersion, Events: []Event{
		{ID: "valid-first", Kind: EventRunAccepted, ProtocolVersion: CurrentProtocolVersion, RunID: "r", SessionID: "s", InputRef: "input", Origin: OriginUser},
		{ID: "bad-second", Kind: EventRunAdmitted, ProtocolVersion: CurrentProtocolVersion + 1, RunID: "r", SessionID: "s", Token: 7, Incarnation: "inc"},
	}}
	before := cloneBatch(input)
	if _, err := Evolve(state, input); err == nil {
		t.Fatal("unsupported event version was accepted")
	}
	if !reflect.DeepEqual(input, before) {
		t.Fatal("failed replay mutated caller batch")
	}
	missing := Batch{CommandID: "missing-version", ExpectedRevision: 0, ProtocolVersion: CurrentProtocolVersion, Events: []Event{{ID: "no-version", Kind: EventRunAccepted, RunID: "r", SessionID: "s", InputRef: "input", Origin: OriginUser}}}
	if _, err := Evolve(state, missing); err == nil {
		t.Fatal("missing event version was accepted")
	}
}

func TestCanonicalCommandPreservesEmptyActiveSlicesAndRejectsInactivePayloads(t *testing.T) {
	for _, candidates := range [][]ToolCandidate{nil, {}} {
		cmd := Command{ID: "canonical", Kind: CommandObserveModelResponse, RunID: "r", SessionID: "s", ModelResponse: ModelResponseInput{Candidates: candidates}}
		got, err := canonicalCommand(cmd)
		if err != nil {
			t.Fatalf("valid response slice rejected: %v", err)
		}
		if (candidates == nil) != (got.ModelResponse.Candidates == nil) {
			t.Fatal("canonicalization changed nil/empty candidate semantics")
		}
	}
	cmd := Command{ID: "canonical-owner", Kind: CommandAcquireOwnership, RunID: "r", SessionID: "s", Transition: TransitionInput{Recoveries: []RecoveryFact{}}}
	got, err := canonicalCommand(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if got.Transition.Recoveries == nil {
		t.Fatal("canonicalization changed nonnil empty recovery semantics")
	}
	bad := Command{ID: "inactive-payload", Kind: CommandAbortStep, RunID: "r", SessionID: "s", Transition: TransitionInput{Ref: "abort", ResultRef: "unused"}}
	normalized, err := canonicalCommand(bad)
	if err != nil || reflect.DeepEqual(normalized, bad) {
		t.Fatal("inactive transition field was retained")
	}
}

func TestModelResponseUsageUnknownIsExplicitAndKnownRequiresRef(t *testing.T) {
	h := newTransitionHarness(t)
	h.modelInFlight()
	batch := h.issue(Command{Kind: CommandObserveModelResponse, ModelResponse: ModelResponseInput{InvocationID: "model-1", AttemptID: "model-attempt-1", ResponseRef: "response-no-usage", Usage: UsageUnknown, Fence: 1, ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1"}})
	if batch.Events[0].Usage != UsageUnknown || h.state.Models["model-1"].Usage.Status != UsageUnknown {
		t.Fatal("unknown provider usage was not preserved explicitly")
	}
	h2 := newTransitionHarness(t)
	h2.modelInFlight()
	cmd := Command{ID: "known-no-ref", Kind: CommandObserveModelResponse, RunID: h2.state.RunID, SessionID: h2.state.SessionID, ExpectedRevision: h2.state.Revision, WriterToken: h2.state.AdmissionToken, WriterIncarnation: h2.state.Incarnation, ModelResponse: ModelResponseInput{InvocationID: "model-1", AttemptID: "model-attempt-1", ResponseRef: "response-known-no-ref", Usage: UsageKnown, Fence: 1, ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1"}}
	if _, err := Decide(h2.state, cmd); err == nil {
		t.Fatal("known model usage without a protected accounting ref was accepted")
	}
}

func TestDecideRejectsUnsupportedKindAndStaleWriter(t *testing.T) {
	state := EmptyState()
	state, err := applyBatch(t, state, Batch{CommandID: "accept", Events: []Event{{ID: "accept-event", Kind: EventRunAccepted, RunID: "r", SessionID: "s", InputRef: "i", Origin: OriginUser}}})
	if err != nil {
		t.Fatal(err)
	}
	unsupported := Command{ID: "unsupported", Kind: 255, RunID: "r", SessionID: "s", ExpectedRevision: state.Revision}
	if _, err := Decide(state, unsupported); err == nil {
		t.Fatal("unsupported command kind was accepted")
	}
	admit := Command{ID: "admit", Kind: CommandAdmitRun, RunID: "r", SessionID: "s", ExpectedRevision: state.Revision, WriterToken: 1, WriterIncarnation: "inc", Admission: AdmissionInput{Token: 1, Incarnation: "inc"}}
	batch, err := Decide(state, admit)
	if err != nil {
		t.Fatal(err)
	}
	state, err = Evolve(state, batch)
	if err != nil {
		t.Fatal(err)
	}
	cmd := Command{ID: "step", Kind: CommandStartStep, RunID: "r", SessionID: "s", ExpectedRevision: state.Revision, WriterToken: 0, WriterIncarnation: "old", Step: StepInput{StepID: "s1"}}
	if _, err := Decide(state, cmd); err == nil {
		t.Fatal("stale command writer was accepted")
	}
}

func TestDecideStartStepRetryUsesStoredIntentAfterStateAdvances(t *testing.T) {
	s := EmptyState()
	var err error
	accept := Command{ID: "start-retry-accept", Kind: CommandAcceptRun, RunID: "r", SessionID: "s", AcceptRun: AcceptRunInput{InputRef: "input", Origin: OriginUser}}
	b, err := Decide(s, accept)
	if err != nil {
		t.Fatal(err)
	}
	s, err = Evolve(s, b)
	if err != nil {
		t.Fatal(err)
	}
	admit := Command{ID: "start-retry-admit", Kind: CommandAdmitRun, RunID: "r", SessionID: "s", ExpectedRevision: s.Revision, WriterToken: 7, WriterIncarnation: "inc", Admission: AdmissionInput{Token: 7, Incarnation: "inc"}}
	b, err = Decide(s, admit)
	if err != nil {
		t.Fatal(err)
	}
	s, err = Evolve(s, b)
	if err != nil {
		t.Fatal(err)
	}
	start := Command{ID: "start-retry-step", Kind: CommandStartStep, RunID: "r", SessionID: "s", ExpectedRevision: s.Revision, WriterToken: 7, WriterIncarnation: "inc", Step: StepInput{StepID: "step1"}}
	first, err := Decide(s, start)
	if err != nil {
		t.Fatal(err)
	}
	s, err = Evolve(s, first)
	if err != nil {
		t.Fatal(err)
	}
	advanced, err := applyBatch(t, s, Batch{CommandID: "start-retry-ownership", Events: []Event{{ID: "start-retry-ownership-e", Kind: EventRunOwnershipAcquired, RunID: "r", SessionID: "s", PreviousToken: 7, PreviousIncarnation: "inc", Token: 8, Incarnation: "inc2", ReceiptRef: "ownership-proof"}}})
	if err != nil {
		t.Fatal(err)
	}
	retry, err := Decide(advanced, start)
	if err != nil || !reflect.DeepEqual(retry, first) {
		t.Fatalf("retry did not return original batch after progress: err=%v", err)
	}
	conflict := start
	conflict.Step.StepID = "different"
	if _, err := Decide(advanced, conflict); err == nil {
		t.Fatal("reused command ID with different intent was accepted")
	}
}

func TestToolResultDispositionUpdatesLogicalStateAndAdvancesFrontier(t *testing.T) {
	for _, outcome := range []ResultDisposition{ResultSucceeded, ResultFailed, ResultDenied, ResultRefused, ResultCancelled} {
		t.Run(string(rune('0'+outcome)), func(t *testing.T) {
			s := dispatchingToolState(t)
			e := Event{ID: "result-" + ID(string(rune('0'+outcome))), Kind: EventToolResultRecorded, RunID: "r", SessionID: "s", ToolID: "tool", AttemptID: "ta", Ref: "result", Disposition: outcome, Token: 7, Incarnation: "inc", Fence: 1}
			s, err := applyBatch(t, s, Batch{CommandID: "tool-result-" + ID(string(rune('0'+outcome))), Events: []Event{e}})
			if err != nil {
				t.Fatal(err)
			}
			if s.Tools["tool"].State != toolStateFor(outcome) || s.Tools["tool"].Disposition != outcome || s.Tools["tool"].ResultRef != "result" {
				t.Fatalf("tool terminal projection mismatch: state=%d disposition=%d", s.Tools["tool"].State, s.Tools["tool"].Disposition)
			}
			s, err = applyBatch(t, s, Batch{CommandID: "close-step-" + ID(string(rune('0'+outcome))), Events: []Event{{ID: "close-step-" + ID(string(rune('0'+outcome))), Kind: EventStepCompleted, RunID: "r", SessionID: "s", Next: NextContinue, Continuation: ContinuationToolCalls}}})
			if err != nil {
				t.Fatal(err)
			}
			s, err = applyBatch(t, s, Batch{CommandID: "advance-" + ID(string(rune('0'+outcome))), Events: []Event{{ID: "advance-" + ID(string(rune('0'+outcome))), Kind: EventStepStarted, RunID: "r", SessionID: "s", StepID: "next", Generation: 2}}})
			if err != nil {
				t.Fatalf("terminal result did not advance frontier: %v", err)
			}
		})
	}
}

func TestToolApprovalBindsValidatedArgsAndResetInvalidatesIt(t *testing.T) {
	s := plannedToolState()
	var err error
	s, err = applyBatch(t, s, Batch{CommandID: "check", Events: []Event{{ID: "check-event", Kind: EventToolCheckStarted, RunID: "r", SessionID: "s", ToolID: "tool", Generation: 1, Ref: "check"}}})
	if err != nil {
		t.Fatal(err)
	}
	awaiting, err := applyBatch(t, s, Batch{CommandID: "approval-request", Events: []Event{{ID: "approval-request-event", Kind: EventToolApprovalRequested, RunID: "r", SessionID: "s", ToolID: "tool", Generation: 1, CauseRef: "canonical-args-v1", RequestRef: "owner-request", BlockerID: "approval"}}})
	if err != nil {
		t.Fatal(err)
	}
	resetWait, err := applyBatch(t, awaiting, Batch{CommandID: "reset-awaiting", Events: []Event{{ID: "reset-awaiting-event", Kind: EventToolCheckReset, RunID: "r", SessionID: "s", ToolID: "tool", Generation: 2, CauseRef: "owner-invalidation", BlockerID: "approval", RequestRef: "owner-request", ReceiptRef: "invalidate-receipt"}}})
	if err != nil {
		t.Fatalf("awaiting approval reset failed: %v", err)
	}
	if resetWait.Tools["tool"].State != ToolPlanned || len(resetWait.Blockers) != 0 {
		t.Fatal("awaiting approval reset retained a dependency")
	}
	wrong := Batch{CommandID: "approval-wrong-args", Events: []Event{
		{ID: "approval-resolved-wrong", Kind: EventToolApprovalResolved, RunID: "r", SessionID: "s", ToolID: "tool", Generation: 1, RequestRef: "owner-request", CauseRef: "canonical-args-v1", ReceiptRef: "approval-receipt", Disposition: ResultSucceeded},
		{ID: "ready-wrong", Kind: EventToolReadyRecorded, RunID: "r", SessionID: "s", ToolID: "tool", Generation: 1, Ref: "canonical-args-v2", ReceiptRef: "approval-receipt"},
	}}
	if _, err := applyBatch(t, awaiting, wrong); err == nil {
		t.Fatal("approved plan was replaced with different validated args")
	}
	approved, err := applyBatch(t, awaiting, Batch{CommandID: "approval-valid-args", Events: []Event{
		{ID: "approval-resolved", Kind: EventToolApprovalResolved, RunID: "r", SessionID: "s", ToolID: "tool", Generation: 1, RequestRef: "owner-request", CauseRef: "canonical-args-v1", ReceiptRef: "approval-receipt", Disposition: ResultSucceeded},
		{ID: "ready-valid", Kind: EventToolReadyRecorded, RunID: "r", SessionID: "s", ToolID: "tool", Generation: 1, Ref: "canonical-args-v1", ReceiptRef: "approval-receipt"},
	}})
	if err != nil {
		t.Fatalf("validated canonical args were not ready: %v", err)
	}
	tool := approved.Tools["tool"]
	if tool.RawArgsRef != "raw-args" || tool.ArgsRef != "canonical-args-v1" || tool.PlanArgsRef != "canonical-args-v1" {
		t.Fatalf("raw and validated argument references were conflated: %#v", tool)
	}
	reset, err := applyBatch(t, approved, Batch{CommandID: "reset-check", Events: []Event{{ID: "reset-check-event", Kind: EventToolCheckReset, RunID: "r", SessionID: "s", ToolID: "tool", Generation: 2, CauseRef: "owner-invalidation"}}})
	if err != nil {
		t.Fatal(err)
	}
	if reset.Tools["tool"].State != ToolPlanned || reset.Tools["tool"].ApprovalRef != "" || reset.Tools["tool"].PlanArgsRef != "" {
		t.Fatal("reset retained stale approval or plan args")
	}
	reset, err = applyBatch(t, reset, Batch{CommandID: "recheck", Events: []Event{{ID: "recheck-event", Kind: EventToolCheckStarted, RunID: "r", SessionID: "s", ToolID: "tool", Generation: 3, Ref: "check-again"}}})
	if err != nil {
		t.Fatal(err)
	}
	reset, err = applyBatch(t, reset, Batch{CommandID: "ready-new-plan", Events: []Event{{ID: "ready-new-plan-event", Kind: EventToolReadyRecorded, RunID: "r", SessionID: "s", ToolID: "tool", Generation: 3, Ref: "canonical-args-v2"}}})
	if err != nil {
		t.Fatalf("new generation did not accept new validated args: %v", err)
	}
}

func TestPredispatchDenialAndApprovalCancellationAreCanonical(t *testing.T) {
	denied := plannedToolState()
	var err error
	denied, err = applyBatch(t, denied, Batch{CommandID: "check-for-denial", Events: []Event{{ID: "check-for-denial-event", Kind: EventToolCheckStarted, RunID: "r", SessionID: "s", ToolID: "tool", Generation: 1, Ref: "check"}}})
	if err != nil {
		t.Fatal(err)
	}
	denied, err = applyBatch(t, denied, Batch{CommandID: "pre-dispatch-denial", Events: []Event{{ID: "pre-dispatch-denial-event", Kind: EventToolResultRecorded, RunID: "r", SessionID: "s", ToolID: "tool", AttemptID: "ta", Ref: "denial-result", ReceiptRef: "owner-denial", Disposition: ResultDenied}}})
	if err != nil {
		t.Fatalf("pre-dispatch denial rejected: %v", err)
	}
	if denied.Tools["tool"].State != ToolDenied || denied.Tools["tool"].ResultRef != "denial-result" {
		t.Fatal("pre-dispatch denial was not projected canonically")
	}

	waiting, err := applyBatch(t, plannedToolState(), Batch{CommandID: "check-for-cancel", Events: []Event{{ID: "check-for-cancel-event", Kind: EventToolCheckStarted, RunID: "r", SessionID: "s", ToolID: "tool", Generation: 1, Ref: "check"}}})
	if err != nil {
		t.Fatal(err)
	}
	waiting, err = applyBatch(t, waiting, Batch{CommandID: "approval-for-cancel", Events: []Event{{ID: "approval-for-cancel-event", Kind: EventToolApprovalRequested, RunID: "r", SessionID: "s", ToolID: "tool", Generation: 1, CauseRef: "canonical-args", RequestRef: "owner-request", BlockerID: "approval"}}})
	if err != nil {
		t.Fatal(err)
	}
	waiting, err = applyBatch(t, waiting, Batch{CommandID: "cancel-approval", Events: []Event{{ID: "cancel-approval-event", Kind: EventToolResultRecorded, RunID: "r", SessionID: "s", ToolID: "tool", AttemptID: "ta", Ref: "cancelled", ReceiptRef: "cancel-evidence", Disposition: ResultCancelled, Terminal: true}}})
	if err != nil {
		t.Fatalf("approval wait cancellation rejected: %v", err)
	}
	if waiting.Tools["tool"].State != ToolCancelled || len(waiting.Blockers) != 0 {
		t.Fatal("approval cancellation left a wait or nonterminal tool")
	}
}

func TestBudgetStopSecondaryCannotReplacePrimaryKind(t *testing.T) {
	s := EmptyState()
	var err error
	s, err = applyBatch(t, s, Batch{CommandID: "budget-run-accepted", Events: []Event{{ID: "budget-run-accepted-event", Kind: EventRunAccepted, RunID: "r", SessionID: "s", InputRef: "input", Origin: OriginUser}}})
	if err != nil {
		t.Fatal(err)
	}
	s, err = applyBatch(t, s, Batch{CommandID: "budget-run-admitted", Events: []Event{{ID: "budget-run-admitted-event", Kind: EventRunAdmitted, RunID: "r", SessionID: "s", Token: 1, Incarnation: "inc"}}})
	if err != nil {
		t.Fatal(err)
	}
	s, err = applyBatch(t, s, Batch{CommandID: "primary-budget", Events: []Event{{ID: "primary-budget-event", Kind: EventRunStopRequested, RunID: "r", SessionID: "s", StopReason: StopBudget, StopMode: StopTerminal, BudgetKind: BudgetModelTokens, Actor: ActorSystem, Ref: "primary"}}})
	if err != nil {
		t.Fatal(err)
	}
	s, err = applyBatch(t, s, Batch{CommandID: "secondary-budget", Events: []Event{{ID: "secondary-budget-event", Kind: EventRunStopRequested, RunID: "r", SessionID: "s", StopReason: StopBudget, StopMode: StopTerminal, BudgetKind: BudgetTime, Actor: ActorTimer, Ref: "secondary"}}})
	if err != nil {
		t.Fatal(err)
	}
	if s.BudgetStopKind != BudgetModelTokens || s.BudgetStopActor != ActorSystem || len(s.SecondaryStops) != 1 {
		t.Fatal("secondary budget overwrote primary budget provenance")
	}
}

func TestAbandonedFinishRequiresExplicitClosureAndClosedStep(t *testing.T) {
	s := makeBudgetCompletionState()
	model := s.Models["m"]
	model.Disposition = ResultSucceeded
	s.Models["m"] = model
	if _, err := applyBatch(t, s, Batch{CommandID: "abandoned-without-close", Events: []Event{{ID: "abandoned-without-close-event", Kind: EventRunFinished, RunID: "budget-run", SessionID: "s", Outcome: OutcomeAbandoned}}}); err == nil {
		t.Fatal("abandoned run finished without explicit closure fact")
	}
	closure := Event{ID: "closure", Kind: EventRunClosureConfirmed, RunID: "budget-run", SessionID: "s", Ref: "close-preview", ReceiptRef: "close-receipt", Actor: ActorUser, Revision: s.Revision}
	if _, err := applyBatch(t, s, Batch{CommandID: "open-step-close", Events: []Event{closure, {ID: "close-run", Kind: EventRunFinished, RunID: "budget-run", SessionID: "s", Outcome: OutcomeAbandoned}}}); err == nil {
		t.Fatal("run with open step finished by metadata closure")
	}
}

func TestNonStepBudgetCanFinishFromPreparationAfterEffectsQuiesce(t *testing.T) {
	s := EmptyState()
	var err error
	s, err = applyBatch(t, s, Batch{CommandID: "prep-accept", Events: []Event{{ID: "prep-accept-e", Kind: EventRunAccepted, RunID: "prep", SessionID: "s", InputRef: "input", Origin: OriginUser}}})
	if err != nil {
		t.Fatal(err)
	}
	s, err = applyBatch(t, s, Batch{CommandID: "prep-admit", Events: []Event{{ID: "prep-admit-e", Kind: EventRunAdmitted, RunID: "prep", SessionID: "s", Token: 1, Incarnation: "inc"}}})
	if err != nil {
		t.Fatal(err)
	}
	s, err = applyBatch(t, s, Batch{CommandID: "prep-budget-stop", Events: []Event{{ID: "prep-budget-stop-e", Kind: EventRunStopRequested, RunID: "prep", SessionID: "s", StopReason: StopBudget, StopMode: StopTerminal, BudgetKind: BudgetTime, Actor: ActorTimer, Ref: "time-limit"}}})
	if err != nil {
		t.Fatal(err)
	}
	s, err = applyBatch(t, s, Batch{CommandID: "prep-budget-finish", Events: []Event{{ID: "prep-budget-finish-e", Kind: EventRunFinished, RunID: "prep", SessionID: "s", Outcome: OutcomeBudgetExceeded}}})
	if err != nil || !s.Finished {
		t.Fatalf("pre-step terminal budget did not finish: finished=%v err=%v", s.Finished, err)
	}
}

func TestBudgetFinishAfterActiveModelQuiescenceAndStepAbort(t *testing.T) {
	s := EmptyState()
	s.Accepted, s.Admitted, s.RunID, s.SessionID = true, true, "active-budget", "s"
	s.AdmissionToken, s.Incarnation, s.InitialStepLimit = 9, "inc", 100
	s.Steps, s.CurrentStep, s.StepsStarted = []Step{{ID: "step", Open: true, ModelID: "m"}}, 0, 1
	s.Models["m"] = ModelInvocation{ID: "m", StepID: "step", State: InvocationActive, AttemptIDs: []ID{"a"}}
	s.ModelAttempts["a"] = Attempt{ID: "a", InvocationID: "m", StepID: "step", State: AttemptRequesting, Token: 9, Incarnation: "inc", Fence: 2}
	derive(&s)
	var err error
	s, err = applyBatch(t, s, Batch{CommandID: "active-token-stop", Events: []Event{{ID: "active-token-stop-e", Kind: EventRunStopRequested, RunID: "active-budget", SessionID: "s", StopReason: StopBudget, StopMode: StopTerminal, BudgetKind: BudgetModelTokens, Actor: ActorSystem, Ref: "token-limit"}}})
	if err != nil {
		t.Fatal(err)
	}
	s, err = applyBatch(t, s, Batch{CommandID: "active-model-quiesced", Events: []Event{{ID: "active-model-quiesced-e", Kind: EventModelAttemptCancelled, RunID: "active-budget", SessionID: "s", InvocationID: "m", AttemptID: "a", Ref: "quiescence-proof", Token: 9, Incarnation: "inc", Fence: 2, Terminal: true}}})
	if err != nil {
		t.Fatal(err)
	}
	s, err = applyBatch(t, s, Batch{CommandID: "active-step-aborted", Events: []Event{{ID: "active-step-aborted-e", Kind: EventStepAborted, RunID: "active-budget", SessionID: "s", Ref: "step-abort"}}})
	if err != nil {
		t.Fatal(err)
	}
	s, err = applyBatch(t, s, Batch{CommandID: "active-budget-finish", Events: []Event{{ID: "active-budget-finish-e", Kind: EventRunFinished, RunID: "active-budget", SessionID: "s", Outcome: OutcomeBudgetExceeded}}})
	if err != nil || !s.Finished {
		t.Fatalf("budget did not finish after quiescence/abort: finished=%v err=%v", s.Finished, err)
	}
}

func TestOtherTerminalBudgetOverridesStepBudgetWait(t *testing.T) {
	s := makeStepBudgetRun(t)
	var err error
	s, err = applyBatch(t, s, Batch{CommandID: "terminal-other-budget", Events: []Event{{ID: "terminal-other-budget-e", Kind: EventRunStopRequested, RunID: "budget-run", SessionID: "s", StopReason: StopBudget, StopMode: StopTerminal, BudgetKind: BudgetTime, Actor: ActorTimer, Ref: "time-limit"}}})
	if err != nil {
		t.Fatal(err)
	}
	s, err = applyBatch(t, s, Batch{CommandID: "finish-other-budget", Events: []Event{{ID: "finish-other-budget-e", Kind: EventRunFinished, RunID: "budget-run", SessionID: "s", Outcome: OutcomeBudgetExceeded}}})
	if err != nil || !s.Finished || s.BudgetStopKind != BudgetTime {
		t.Fatalf("other budget could not end step-budget wait: finished=%v kind=%d err=%v", s.Finished, s.BudgetStopKind, err)
	}
}

func TestPureReadToolRetryPreservesUnknownAttemptProvenance(t *testing.T) {
	s := plannedToolState()
	tool := s.Tools["tool"]
	tool.EffectClass = EffectPureRead
	s.Tools["tool"] = tool
	s, err := applyBatch(t, s, Batch{CommandID: "tool-check-retry", Events: []Event{{ID: "tool-check-retry-e", Kind: EventToolCheckStarted, RunID: "r", SessionID: "s", ToolID: "tool", Generation: 1, Ref: "check"}}})
	if err != nil {
		t.Fatal(err)
	}
	s, err = applyBatch(t, s, Batch{CommandID: "tool-ready-retry", Events: []Event{{ID: "tool-ready-retry-e", Kind: EventToolReadyRecorded, RunID: "r", SessionID: "s", ToolID: "tool", Generation: 1, Ref: "args"}}})
	if err != nil {
		t.Fatal(err)
	}
	s, err = applyBatch(t, s, Batch{CommandID: "tool-dispatch-retry", Events: []Event{{ID: "tool-dispatch-retry-e", Kind: EventToolDispatchStarted, RunID: "r", SessionID: "s", ToolID: "tool", AttemptID: "ta", ReceiptRef: "grant", Ref: "dispatch", Generation: 1, Token: 7, Incarnation: "inc", Fence: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	s, err = applyBatch(t, s, Batch{CommandID: "tool-unknown-retry", Events: []Event{
		{ID: "tool-unknown-retry-e", Kind: EventToolOutcomeUnknown, RunID: "r", SessionID: "s", ToolID: "tool", AttemptID: "ta", CauseRef: "unknown-cause", Exposure: ExposureUnknown, Token: 7, Incarnation: "inc", Fence: 1},
		{ID: "run-unknown-retry-e", Kind: EventRunInterrupted, RunID: "r", SessionID: "s", CauseRef: "unknown-cause", ToolID: "tool", Interrupt: InterruptEffectUnknown},
	}})
	if err != nil {
		t.Fatal(err)
	}
	s, err = applyBatch(t, s, Batch{CommandID: "abandon-for-retry", Events: []Event{{ID: "abandon-for-retry-e", Kind: EventToolAttemptAbandoned, RunID: "r", SessionID: "s", ToolID: "tool", AttemptID: "ta", Ref: "abandon-ref", ReceiptRef: "retry-policy", Exposure: ExposureUnknown, Next: NextContinue}}})
	if err != nil {
		t.Fatal(err)
	}
	s, err = applyBatch(t, s, Batch{CommandID: "retry-timer", Events: []Event{{ID: "retry-timer-e", Kind: EventTimerScheduled, RunID: "r", SessionID: "s", TimerID: "retry-timer", BlockerID: "retry-block", Ref: "retry-policy", Purpose: TimerToolRetry, Generation: 2, ToolID: "tool"}}})
	if err != nil {
		t.Fatal(err)
	}
	s, err = applyBatch(t, s, Batch{CommandID: "retry-timer-elapsed", Events: []Event{{ID: "retry-timer-elapsed-e", Kind: EventTimerElapsed, RunID: "r", SessionID: "s", TimerID: "retry-timer", Generation: 2, CauseRef: "timer-proof"}}})
	if err != nil {
		t.Fatal(err)
	}
	s, err = applyBatch(t, s, Batch{CommandID: "new-tool-attempt", Events: []Event{{ID: "new-tool-attempt-e", Kind: EventToolAttemptPlanned, RunID: "r", SessionID: "s", ToolID: "tool", PreviousAttemptID: "ta", AttemptID: "ta2", ReceiptRef: "retry-policy", TimerID: "retry-timer", Generation: 2}}})
	if err != nil {
		t.Fatalf("pure-read retry rejected: %v", err)
	}
	if s.ToolAttempts["ta"].State != AttemptAbandoned || s.ToolAttempts["ta"].Exposure != ExposureUnknown || s.Exposures["tool"] != ExposureUnknown || s.ToolAttempts["ta2"].State != AttemptPlanned {
		t.Fatal("retry replaced or cleared old unknown attempt facts")
	}
}

func TestUnknownSideEffectToolCannotRetry(t *testing.T) {
	// Default/unspecified effect classes are classified as side-effecting on plan.
	s := dispatchingToolState(t)
	s, err := applyBatch(t, s, Batch{CommandID: "sideeffect-unknown", Events: []Event{
		{ID: "sideeffect-unknown-e", Kind: EventToolOutcomeUnknown, RunID: "r", SessionID: "s", ToolID: "tool", AttemptID: "ta", CauseRef: "unknown-cause", Exposure: ExposureUnknown, Token: 7, Incarnation: "inc", Fence: 1},
		{ID: "sideeffect-run-interrupted-e", Kind: EventRunInterrupted, RunID: "r", SessionID: "s", CauseRef: "unknown-cause", ToolID: "tool", Interrupt: InterruptEffectUnknown},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := applyBatch(t, s, Batch{CommandID: "sideeffect-retry-abandon", Events: []Event{{ID: "sideeffect-retry-abandon-e", Kind: EventToolAttemptAbandoned, RunID: "r", SessionID: "s", ToolID: "tool", AttemptID: "ta", Ref: "abandon-ref", ReceiptRef: "retry-policy", Exposure: ExposureUnknown, Next: NextContinue}}}); err == nil {
		t.Fatal("unknown side effect was made retryable")
	}
}

func TestModelRetryKeepsOldUsageAndUnknownAttempt(t *testing.T) {
	s := EmptyState()
	s.Accepted, s.Admitted, s.RunID, s.SessionID = true, true, "r", "s"
	s.AdmissionToken, s.Incarnation, s.InitialStepLimit = 7, "inc", 100
	s.RetryBudgetLimit, s.ModelRetryPolicyVersion = 2, 1
	s.Steps, s.CurrentStep, s.StepsStarted = []Step{{ID: "step", Open: true, ModelID: "m"}}, 0, 1
	s.Models["m"] = ModelInvocation{ID: "m", StepID: "step", State: InvocationActive, AttemptIDs: []ID{"a"}}
	s.ModelAttempts["a"] = Attempt{ID: "a", InvocationID: "m", StepID: "step", State: AttemptRequesting, Token: 7, Incarnation: "inc", Fence: 1}
	derive(&s)
	var err error
	s, err = applyBatch(t, s, Batch{CommandID: "model-interrupted", Events: []Event{
		{ID: "model-interrupted-e", Kind: EventModelRequestInterrupted, RunID: "r", SessionID: "s", InvocationID: "m", AttemptID: "a", Ref: "unknown", CauseRef: "model-cause", Usage: UsageReserved, ReceiptRef: "usage-reservation", Token: 7, Incarnation: "inc", Fence: 1},
		{ID: "model-run-interrupted-e", Kind: EventRunInterrupted, RunID: "r", SessionID: "s", CauseRef: "model-cause", ToolID: "a", Interrupt: InterruptEffectUnknown},
	}})
	if err != nil {
		t.Fatal(err)
	}
	s, err = applyBatch(t, s, Batch{CommandID: "model-abandon-retry", Events: []Event{{ID: "model-abandon-retry-e", Kind: EventModelAttemptAbandoned, RunID: "r", SessionID: "s", InvocationID: "m", AttemptID: "a", Ref: "policy-abandon", ReceiptRef: "retry-policy", Exposure: ExposureUnknown, Next: NextContinue}}})
	if err != nil {
		t.Fatal(err)
	}
	retryCommand := Command{ID: "model-retry-command", Kind: CommandScheduleModelRetry, RunID: "r", SessionID: "s", ExpectedRevision: s.Revision, WriterToken: 7, WriterIncarnation: "inc", Transition: TransitionInput{TargetID: "m", PreviousAttemptID: "a", AttemptID: "a2", TimerID: "model-timer", BlockerID: "model-retry-block", Ref: "retry-policy", Generation: 2, RetryOrdinal: 1, RetryLimit: 2, PolicyVersion: 1, DeadlineUnixMilli: 1000}}
	retryBatch, decideErr := Decide(s, retryCommand)
	if decideErr != nil {
		t.Fatal(decideErr)
	}
	s, err = Evolve(s, retryBatch)
	if err != nil {
		t.Fatal(err)
	}
	if s.ModelAttempts["a"].State != AttemptAbandoned || s.ModelAttempts["a"].Usage.Ref != "usage-reservation" || s.Exposures["a"] != ExposureUnknown || s.ModelAttempts["a2"].State != AttemptPlanned {
		t.Fatal("model retry replaced old uncertainty/accounting facts")
	}
}

func plannedToolState() State {
	s := EmptyState()
	s.Accepted, s.Admitted, s.RunID, s.SessionID = true, true, "r", "s"
	s.AdmissionToken, s.Incarnation, s.InitialStepLimit = 7, "inc", 100
	s.Steps = []Step{{ID: "step", Open: true, ModelID: "m", ResponseRef: "response", ToolIDs: []ID{"tool"}}}
	s.CurrentStep, s.StepsStarted = 0, 1
	s.Models["m"] = ModelInvocation{ID: "m", StepID: "step", State: InvocationCompleted, AttemptIDs: []ID{"ma"}, ResponseRef: "response", CandidateIDs: []ID{"tool"}, Candidates: []ToolCandidate{{ID: "tool", AttemptID: "ta", ArgsRef: "raw-args", Descriptor: "desc", SourceIndex: 0}}}
	s.ModelAttempts["ma"] = Attempt{ID: "ma", InvocationID: "m", StepID: "step", State: AttemptCompleted}
	s.Tools["tool"] = ToolInvocation{ID: "tool", StepID: "step", State: ToolPlanned, Descriptor: "desc", EffectClass: EffectSideEffecting, SourceIndex: 0, RawArgsRef: "raw-args", ArgsRef: "raw-args", AttemptIDs: []ID{"ta"}}
	s.ToolAttempts["ta"] = ToolAttempt{ID: "ta", ToolID: "tool", StepID: "step", State: AttemptPlanned, ArgsRef: "raw-args"}
	derive(&s)
	return s
}

func dispatchingToolState(t *testing.T) State {
	t.Helper()
	s := plannedToolState()
	var err error
	s, err = applyBatch(t, s, Batch{CommandID: "check-dispatch", Events: []Event{{ID: "check-dispatch-e", Kind: EventToolCheckStarted, RunID: "r", SessionID: "s", ToolID: "tool", Generation: 1, Ref: "check"}}})
	if err != nil {
		t.Fatal(err)
	}
	s, err = applyBatch(t, s, Batch{CommandID: "ready-dispatch", Events: []Event{{ID: "ready-dispatch-e", Kind: EventToolReadyRecorded, RunID: "r", SessionID: "s", ToolID: "tool", Generation: 1, Ref: "validated-args"}}})
	if err != nil {
		t.Fatal(err)
	}
	s, err = applyBatch(t, s, Batch{CommandID: "start-dispatch", Events: []Event{{ID: "start-dispatch-e", Kind: EventToolDispatchStarted, RunID: "r", SessionID: "s", ToolID: "tool", AttemptID: "ta", ReceiptRef: "grant", Ref: "dispatch", Generation: 1, Token: 7, Incarnation: "inc", Fence: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestModelCallbackCannotCrossInvocationOwnership(t *testing.T) {
	s := EmptyState()
	s.Accepted, s.Admitted, s.RunID, s.SessionID = true, true, "r", "s"
	s.AdmissionToken, s.Incarnation = 7, "inc"
	s.Steps = []Step{{ID: "step1", Open: true, ModelID: "m1"}}
	s.CurrentStep, s.StepsStarted = 0, 1
	s.Models["m1"] = ModelInvocation{ID: "m1", StepID: "step1", State: InvocationActive, AttemptIDs: []ID{"a1"}}
	s.Models["m2"] = ModelInvocation{ID: "m2", StepID: "step2", State: InvocationActive, AttemptIDs: []ID{"a2"}}
	s.ModelAttempts["a1"] = Attempt{ID: "a1", InvocationID: "m1", StepID: "step1", State: AttemptRequesting, Token: 7, Incarnation: "inc", Fence: 1}
	s.ModelAttempts["a2"] = Attempt{ID: "a2", InvocationID: "m2", StepID: "step2", State: AttemptRequesting, Token: 7, Incarnation: "inc", Fence: 2}
	derive(&s)
	response := Event{ID: "cross-response", Kind: EventModelResponseRecorded, RunID: "r", SessionID: "s", InvocationID: "m1", AttemptID: "a2", Ref: "response", Usage: UsageKnown, Token: 7, Incarnation: "inc", Fence: 2}
	if _, err := applyBatch(t, s, Batch{CommandID: "cross", Events: []Event{response}}); err == nil {
		t.Fatal("callback for invocation m2 completed m1")
	}
}

func TestStopAndBlockGateNewDispatch(t *testing.T) {
	state := EmptyState()
	accepted := Batch{CommandID: "c1", Events: []Event{{ID: "e1", Kind: EventRunAccepted, RunID: "r", SessionID: "s", InputRef: "i", Origin: OriginUser}}}
	var err error
	state, err = applyBatch(t, state, accepted)
	if err != nil {
		t.Fatal(err)
	}
	state, err = applyBatch(t, state, Batch{CommandID: "c2", Events: []Event{{ID: "e2", Kind: EventRunAdmitted, RunID: "r", SessionID: "s", Token: 2, Incarnation: "inc"}, {ID: "e3", Kind: EventRunBlockAdded, RunID: "r", SessionID: "s", BlockerID: "b", BlockKind: BlockVaultLocked, Ref: "lock"}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = applyBatch(t, state, Batch{CommandID: "c3", Events: []Event{{ID: "e4", Kind: EventStepStarted, RunID: "r", SessionID: "s", StepID: "step", Generation: 1}}})
	if err == nil {
		t.Fatal("dispatch frontier advanced with unresolved blocker")
	}
	state, err = applyBatch(t, state, Batch{CommandID: "c4", Events: []Event{{ID: "e5", Kind: EventRunBlockCleared, RunID: "r", SessionID: "s", BlockerID: "b", CauseRef: "unlocked"}}})
	if err != nil {
		t.Fatal(err)
	}
	state, err = applyBatch(t, state, Batch{CommandID: "c5", Events: []Event{{ID: "e6", Kind: EventRunStopRequested, RunID: "r", SessionID: "s", StopMode: StopTerminal, StopReason: StopUserCancel, Actor: ActorUser, Ref: "cancel"}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = applyBatch(t, state, Batch{CommandID: "c6", Events: []Event{{ID: "e7", Kind: EventStepStarted, RunID: "r", SessionID: "s", StepID: "step2", Generation: 1}}})
	if err == nil {
		t.Fatal("dispatch frontier advanced after terminal stop")
	}
}

func TestSM01ActiveAttemptCardinalityValidAndInvalid(t *testing.T) {
	s := EmptyState()
	s.Admitted, s.AdmissionToken, s.Incarnation = true, 4, "inc"
	s.Models["m"] = ModelInvocation{ID: "m", StepID: "step", State: InvocationActive, AttemptIDs: []ID{"a1"}}
	s.ModelAttempts["a1"] = Attempt{ID: "a1", InvocationID: "m", StepID: "step", State: AttemptRequesting, Token: 4, Incarnation: "inc", Fence: 1}
	derive(&s)
	if err := CheckInvariants(s); err != nil {
		t.Fatalf("valid one active attempt: %v", err)
	}
	s.Tools["tool"] = ToolInvocation{ID: "tool", StepID: "step", State: ToolDispatching, AttemptIDs: []ID{"ta"}}
	s.ToolAttempts["ta"] = ToolAttempt{ID: "ta", ToolID: "tool", StepID: "step", State: AttemptStreaming, Token: 4, Incarnation: "inc", Fence: 2}
	derive(&s)
	if CheckInvariants(s) == nil {
		t.Fatal("accepted two active attempts")
	}
}

func TestSM02ApprovalDependencyPairValidAndInvalid(t *testing.T) {
	s := approvalWaitState()
	if err := CheckInvariants(s); err != nil {
		t.Fatalf("valid approval wait: %v", err)
	}
	s.Blockers["orphan"] = Blocker{ID: "orphan", Kind: BlockApproval, TargetID: "missing", Ref: "req", Generation: 1}
	derive(&s)
	if CheckInvariants(s) == nil {
		t.Fatal("accepted unmatched approval dependency")
	}
}

func TestSM03ApprovalWaitProjectionValidAndInvalid(t *testing.T) {
	s := approvalWaitState()
	if err := CheckInvariants(s); err != nil {
		t.Fatalf("valid waiting projection: %v", err)
	}
	s.Lifecycle = Running
	if CheckInvariants(s) == nil {
		t.Fatal("accepted approval wait without Waiting lifecycle")
	}
}

func TestSM04PhaseProjectionValidAndInvalid(t *testing.T) {
	s := EmptyState()
	derive(&s)
	if err := CheckInvariants(s); err != nil {
		t.Fatalf("valid phase: %v", err)
	}
	s.Phase = PhaseCallingModel
	if CheckInvariants(s) == nil {
		t.Fatal("accepted mismatched phase")
	}
}

func TestSM05FinishDispositionValidAndInvalid(t *testing.T) {
	s := terminalControlState()
	if err := CheckInvariants(s); err != nil {
		t.Fatalf("valid finish: %v", err)
	}
	inv := s.Models["m"]
	inv.Disposition = ResultNone
	s.Models["m"] = inv
	if CheckInvariants(s) == nil {
		t.Fatal("accepted finished run with nonterminal model invocation")
	}
	open := terminalControlState()
	open.Steps[0].Open = true
	if CheckInvariants(open) == nil {
		t.Fatal("accepted finished run with an open step")
	}
}

func TestExtendedRunCanReachAnotherStepBudgetWait(t *testing.T) {
	waiting := makeStepBudgetRun(t)
	extended, err := applyBatch(t, waiting, Batch{CommandID: "extend-first-wait", Events: []Event{{ID: "extend-first-wait-e", Kind: EventBudgetExtended, RunID: "budget-run", SessionID: "s", BudgetKind: BudgetSteps, BudgetBy: 1, Actor: ActorUser}}})
	if err != nil {
		t.Fatal(err)
	}
	started, err := applyBatch(t, extended, Batch{CommandID: "second-step-start", Events: []Event{{ID: "second-step-start-e", Kind: EventStepStarted, RunID: "budget-run", SessionID: "s", StepID: "step2", Generation: 2}}})
	if err != nil {
		t.Fatal(err)
	}
	started.Models["m2"] = ModelInvocation{ID: "m2", StepID: "step2", State: InvocationCompleted, AttemptIDs: []ID{"a2"}, ResponseRef: "response2"}
	started.ModelAttempts["a2"] = Attempt{ID: "a2", InvocationID: "m2", StepID: "step2", State: AttemptCompleted}
	step := started.Steps[1]
	step.ModelID = "m2"
	step.ResponseRef = "response2"
	started.Steps[1] = step
	derive(&started)
	wait2, err := applyBatch(t, started, Batch{CommandID: "second-step-wait", Events: []Event{{ID: "second-step-wait-e", Kind: EventStepCompleted, RunID: "budget-run", SessionID: "s", Next: NextWait, Continuation: ContinuationStepBudget}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckInvariants(wait2); err != nil {
		t.Fatalf("historical step-budget wait invalid after another wait: %v", err)
	}
}

func TestSM06InterruptionPrecedenceValidAndInvalid(t *testing.T) {
	s := EmptyState()
	s.Accepted, s.Admitted, s.AdmissionToken, s.Incarnation = true, true, 3, "inc"
	derive(&s)
	if err := CheckInvariants(s); err != nil {
		t.Fatalf("valid admitted state: %v", err)
	}
	s.Interruptions["cause"] = Interruption{ID: "cause", Kind: InterruptEffectUnknown, TargetID: "attempt"}
	derive(&s)
	if err := CheckInvariants(s); err != nil {
		t.Fatalf("valid interrupted precedence: %v", err)
	}
	s.Lifecycle = Cancelling
	if CheckInvariants(s) == nil {
		t.Fatal("accepted cancellation over unresolved interruption")
	}
}

func TestSM07CanonicalToolPairOrderValidAndInvalid(t *testing.T) {
	s := terminalControlState()
	if err := CheckInvariants(s); err != nil {
		t.Fatalf("valid canonical pairs: %v", err)
	}
	st := s.Steps[0]
	st.CompletedTools = append(st.CompletedTools, "tool1")
	s.Steps[0] = st
	if CheckInvariants(s) == nil {
		t.Fatal("accepted duplicate canonical tool pair")
	}
}

func TestSM08RetryProvenanceValidAndInvalid(t *testing.T) {
	s := terminalControlState()
	if err := CheckInvariants(s); err != nil {
		t.Fatalf("valid attempt provenance: %v", err)
	}
	delete(s.ModelAttempts, "a")
	if CheckInvariants(s) == nil {
		t.Fatal("accepted invocation with missing attempt provenance")
	}
}

func TestSM09CurrentOwnershipFenceValidAndInvalid(t *testing.T) {
	s := EmptyState()
	s.Admitted, s.AdmissionToken, s.Incarnation = true, 8, "inc2"
	s.Models["m"] = ModelInvocation{ID: "m", StepID: "step", State: InvocationActive, AttemptIDs: []ID{"a"}}
	s.ModelAttempts["a"] = Attempt{ID: "a", InvocationID: "m", StepID: "step", State: AttemptRequesting, Token: 8, Incarnation: "inc2", Fence: 1}
	derive(&s)
	if err := CheckInvariants(s); err != nil {
		t.Fatalf("valid current fence: %v", err)
	}
	a := s.ModelAttempts["a"]
	a.Token = 7
	s.ModelAttempts["a"] = a
	derive(&s)
	if CheckInvariants(s) == nil {
		t.Fatal("accepted stale ownership token")
	}
}

func TestSM10StopAndWaitDispatchGateValidAndInvalid(t *testing.T) {
	state := EmptyState()
	var err error
	state, err = applyBatch(t, state, Batch{CommandID: "c", Events: []Event{{ID: "e", Kind: EventRunAccepted, RunID: "r", SessionID: "s", InputRef: "i", Origin: OriginUser}, {ID: "e2", Kind: EventRunAdmitted, RunID: "r", SessionID: "s", Token: 1, Incarnation: "inc"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = applyBatch(t, state, Batch{CommandID: "c2", Events: []Event{{ID: "e3", Kind: EventStepStarted, RunID: "r", SessionID: "s", StepID: "step", Generation: 1}}}); err != nil {
		t.Fatalf("valid runnable step: %v", err)
	}
	state, err = applyBatch(t, state, Batch{CommandID: "c3", Events: []Event{{ID: "e4", Kind: EventRunStopRequested, RunID: "r", SessionID: "s", StopMode: StopTerminal, StopReason: StopUserCancel, Actor: ActorUser, Ref: "stop"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := applyBatch(t, state, Batch{CommandID: "c4", Events: []Event{{ID: "e5", Kind: EventStepStarted, RunID: "r", SessionID: "s", StepID: "step2", Generation: 2}}}); err == nil {
		t.Fatal("accepted step after stop")
	}
	model := EmptyState()
	model.Accepted, model.Admitted, model.RunID, model.SessionID, model.AdmissionToken, model.Incarnation = true, true, "r", "s", 1, "inc"
	model.Steps = []Step{{ID: "step", Open: true}}
	model.CurrentStep, model.StepsStarted = 0, 1
	model.Models["m"] = ModelInvocation{ID: "m", StepID: "step", State: InvocationPlanned, AttemptIDs: []ID{"a"}}
	model.ModelAttempts["a"] = Attempt{ID: "a", InvocationID: "m", StepID: "step", State: AttemptPlanned}
	derive(&model)
	model, err = applyBatch(t, model, Batch{CommandID: "model-block", Events: []Event{{ID: "model-block-e", Kind: EventRunBlockAdded, RunID: "r", SessionID: "s", BlockerID: "dependency", BlockKind: BlockDependency, Ref: "dependency-ref"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = applyBatch(t, model, Batch{CommandID: "model-after-block", Events: []Event{{ID: "model-after-block-e", Kind: EventModelDispatchStarted, RunID: "r", SessionID: "s", InvocationID: "m", AttemptID: "a", Ref: "dispatch", Token: 1, Incarnation: "inc", Fence: 1}}}); err == nil {
		t.Fatal("model dispatch passed a live dependency blocker")
	}
	tool := plannedToolState()
	tool, err = applyBatch(t, tool, Batch{CommandID: "tool-block", Events: []Event{{ID: "tool-block-e", Kind: EventRunBlockAdded, RunID: "r", SessionID: "s", BlockerID: "dependency", BlockKind: BlockDependency, Ref: "dependency-ref"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = applyBatch(t, tool, Batch{CommandID: "tool-after-block", Events: []Event{{ID: "tool-after-block-e", Kind: EventToolCheckStarted, RunID: "r", SessionID: "s", ToolID: "tool", Generation: 1, Ref: "check"}}}); err == nil {
		t.Fatal("tool preparation passed a live dependency blocker")
	}
}

func TestStopReasonModePairing(t *testing.T) {
	state, err := applyBatch(t, EmptyState(), Batch{CommandID: "stop-mode-accept", Events: []Event{{ID: "stop-mode-accept-e", Kind: EventRunAccepted, RunID: "r", SessionID: "s", InputRef: "input", Origin: OriginUser}, {ID: "stop-mode-admit-e", Kind: EventRunAdmitted, RunID: "r", SessionID: "s", Token: 1, Incarnation: "inc"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = applyBatch(t, state, Batch{CommandID: "shutdown-terminal", Events: []Event{{ID: "shutdown-terminal-e", Kind: EventRunStopRequested, RunID: "r", SessionID: "s", StopReason: StopShutdown, StopMode: StopTerminal, Ref: "shutdown"}}}); err == nil {
		t.Fatal("shutdown was accepted as terminal instead of suspend")
	}
	if _, err = applyBatch(t, state, Batch{CommandID: "wrong-user-actor", Events: []Event{{ID: "wrong-user-actor-e", Kind: EventRunStopRequested, RunID: "r", SessionID: "s", StopReason: StopUserCancel, StopMode: StopTerminal, Actor: ActorParent, Ref: "cancel"}}}); err == nil {
		t.Fatal("user cancellation accepted parent provenance")
	}
	if _, err = applyBatch(t, state, Batch{CommandID: "user-suspend", Events: []Event{{ID: "user-suspend-e", Kind: EventRunStopRequested, RunID: "r", SessionID: "s", StopReason: StopUserCancel, StopMode: StopSuspend, Ref: "suspend", BlockerID: "paused"}}}); err == nil {
		t.Fatal("user cancellation was accepted as suspend")
	}
	paused, err := applyBatch(t, state, Batch{CommandID: "shutdown-suspend", Events: []Event{{ID: "shutdown-suspend-e", Kind: EventRunStopRequested, RunID: "r", SessionID: "s", StopReason: StopShutdown, StopMode: StopSuspend, Actor: ActorSystem, Ref: "shutdown", BlockerID: "paused"}}})
	if err != nil || !paused.Suspended {
		t.Fatalf("valid shutdown suspend rejected: %v", err)
	}
}

func TestSM11UnknownExposurePreservedValidAndInvalid(t *testing.T) {
	s := EmptyState()
	s.Finished, s.Outcome = true, OutcomeAbandonedUnknown
	s.Exposures["ta"] = ExposureUnknown
	derive(&s)
	if err := CheckInvariants(s); err != nil {
		t.Fatalf("valid unknown exposure: %v", err)
	}
	delete(s.Exposures, "ta")
	if CheckInvariants(s) == nil {
		t.Fatal("accepted abandoned_unknown without exposure")
	}
}

func TestResolvedPureReadUnknownProvenanceDoesNotBlockLaterSuccess(t *testing.T) {
	s := terminalControlState()
	s.Exposures["old-read-attempt"] = ExposureUnknown
	derive(&s)
	if err := CheckInvariants(s); err != nil {
		t.Fatalf("historical unknown read provenance blocked later success: %v", err)
	}
	unknown := s.Tools["tool"]
	unknown.State = ToolUnknown
	s.Tools["tool"] = unknown
	derive(&s)
	if finishAllowed(s, OutcomeCancelled) {
		t.Fatal("terminal cancellation hid a current unknown tool outcome")
	}
}

func TestClosureDistinguishesUnknownUsageFromKnownUsage(t *testing.T) {
	s := terminalControlState()
	s.ClosureConfirmed = true
	a := s.ModelAttempts["a"]
	a.Usage = UsageFact{Status: UsageUnknown}
	a.Token = 1 // dispatch evidence: this is unknown accounting, not predispatch cancellation.
	s.ModelAttempts["a"] = a
	if finishAllowed(s, OutcomeAbandoned) {
		t.Fatal("metadata-only abandoned close hid dispatched model usage uncertainty")
	}
	if !finishAllowed(s, OutcomeAbandonedUnknown) {
		t.Fatal("usage-only uncertainty could not close as abandoned_unknown")
	}
	known := terminalControlState()
	known.ClosureConfirmed = true
	a = known.ModelAttempts["a"]
	a.Usage = UsageFact{Status: UsageKnown, Ref: "usage-receipt"}
	known.ModelAttempts["a"] = a
	if !finishAllowed(known, OutcomeAbandoned) {
		t.Fatal("known usage was incorrectly forced into abandoned_unknown")
	}
}

func TestSM12UnadmittedReplayValidAndInvalid(t *testing.T) {
	s := EmptyState()
	s.Accepted, s.RunID = true, "r"
	derive(&s)
	if err := CheckInvariants(s); err != nil {
		t.Fatalf("valid queued replay: %v", err)
	}
	s.Steps = append(s.Steps, Step{ID: "unexpected", Open: true})
	if CheckInvariants(s) == nil {
		t.Fatal("accepted unadmitted run with execution step")
	}
}

func TestRecoveryOwnershipThenExplicitUnknownFactsAreAtomic(t *testing.T) {
	s := EmptyState()
	s.Accepted, s.Admitted, s.RunID, s.SessionID = true, true, "r", "s"
	s.AdmissionToken, s.Incarnation = 4, "oldinc"
	s.Models["m"] = ModelInvocation{ID: "m", StepID: "step", State: InvocationActive, AttemptIDs: []ID{"a"}}
	s.ModelAttempts["a"] = Attempt{ID: "a", InvocationID: "m", StepID: "step", State: AttemptStreaming, Token: 4, Incarnation: "oldinc", Fence: 9}
	s.Steps = []Step{{ID: "step", Open: true, ModelID: "m"}}
	s.CurrentStep = 0
	derive(&s)
	ownership := Event{ID: "owner", Kind: EventRunOwnershipAcquired, RunID: "r", SessionID: "s", PreviousToken: 4, PreviousIncarnation: "oldinc", Token: 5, Incarnation: "newinc", ReceiptRef: "recovery-receipt"}
	partial, err := applyBatch(t, s, Batch{CommandID: "partial", Events: []Event{ownership}})
	if err == nil {
		t.Fatal("accepted owner change without explicit stale-attempt disposition")
	}
	if partial.RunID != "" {
		t.Fatal("rejected batch mutated the source state")
	}
	batch := Batch{CommandID: "recover", Events: []Event{
		ownership,
		{ID: "model-unknown", Kind: EventModelRequestInterrupted, RunID: "r", SessionID: "s", InvocationID: "m", AttemptID: "a", Ref: "unknown-ref", CauseRef: "model-cause", Usage: UsageUnknown, PreviousToken: 4, PreviousIncarnation: "oldinc", Token: 5, Incarnation: "newinc", Fence: 9, ReceiptRef: "recovery-receipt"},
		{ID: "run-interrupted", Kind: EventRunInterrupted, RunID: "r", SessionID: "s", CauseRef: "model-cause", ToolID: "a", Interrupt: InterruptEffectUnknown},
		{ID: "recovery-block", Kind: EventRunBlockAdded, RunID: "r", SessionID: "s", BlockerID: "recovery", BlockKind: BlockRecoveryAdmission, Ref: "recovery-receipt"},
	}}
	got, err := applyBatch(t, s, batch)
	if err != nil {
		t.Fatalf("atomic recovery batch rejected: %v", err)
	}
	if got.AdmissionToken != 5 || got.Incarnation != "newinc" || got.ModelAttempts["a"].State != AttemptInterrupted || got.Exposures["a"] != ExposurePossible || got.Lifecycle != Interrupted || len(got.Interruptions) != 1 {
		t.Fatalf("recovery facts not projected: token=%d incarnation=%q attempt=%d lifecycle=%d interruptions=%d", got.AdmissionToken, got.Incarnation, got.ModelAttempts["a"].State, got.Lifecycle, len(got.Interruptions))
	}
}

func TestTraceBRecoveryBatchFencesToolAndKeepsUnknownExposure(t *testing.T) {
	s := EmptyState()
	s.Accepted, s.Admitted, s.RunID, s.SessionID = true, true, "r2", "s2"
	s.AdmissionToken, s.Incarnation = 7, "oldinc"
	s.Models["m1"] = ModelInvocation{ID: "m1", StepID: "step1", State: InvocationCompleted, AttemptIDs: []ID{"a1"}, ResponseRef: "response", CandidateIDs: []ID{"t1"}, Candidates: []ToolCandidate{{ID: "t1", AttemptID: "ta1", ArgsRef: "args", Descriptor: "desc", SourceIndex: 0}}}
	s.ModelAttempts["a1"] = Attempt{ID: "a1", InvocationID: "m1", StepID: "step1", State: AttemptCompleted}
	s.Tools["t1"] = ToolInvocation{ID: "t1", StepID: "step1", State: ToolDispatching, Descriptor: "desc", ArgsRef: "args", AttemptIDs: []ID{"ta1"}, SelectedAttempt: "ta1", SourceIndex: 0}
	s.ToolAttempts["ta1"] = ToolAttempt{ID: "ta1", ToolID: "t1", StepID: "step1", State: AttemptRequesting, Token: 7, Incarnation: "oldinc", Fence: 14}
	s.Steps = []Step{{ID: "step1", Open: true, ModelID: "m1", ResponseRef: "response", ToolIDs: []ID{"t1"}}}
	s.CurrentStep = 0
	derive(&s)
	batch := Batch{CommandID: "trace-b-recovery", Events: []Event{
		{ID: "owner", Kind: EventRunOwnershipAcquired, RunID: "r2", SessionID: "s2", PreviousToken: 7, PreviousIncarnation: "oldinc", Token: 8, Incarnation: "newinc", ReceiptRef: "ownership-receipt"},
		{ID: "vault", Kind: EventRunBlockAdded, RunID: "r2", SessionID: "s2", BlockerID: "vault", BlockKind: BlockVaultLocked, Ref: "vault-observation"},
		{ID: "recovery", Kind: EventRunBlockAdded, RunID: "r2", SessionID: "s2", BlockerID: "recovery", BlockKind: BlockRecoveryAdmission, Ref: "recovery-receipt"},
		{ID: "unknown", Kind: EventToolOutcomeUnknown, RunID: "r2", SessionID: "s2", ToolID: "t1", AttemptID: "ta1", CauseRef: "tool-effect-unknown", Exposure: ExposureUnknown, PreviousToken: 7, PreviousIncarnation: "oldinc", Token: 8, Incarnation: "newinc", Fence: 14, ReceiptRef: "ownership-receipt"},
		{ID: "interrupted", Kind: EventRunInterrupted, RunID: "r2", SessionID: "s2", CauseRef: "tool-effect-unknown", ToolID: "t1", Interrupt: InterruptEffectUnknown},
	}}
	got, err := applyBatch(t, s, batch)
	if err != nil {
		t.Fatalf("Trace B recovery batch rejected: %v", err)
	}
	if got.Lifecycle != Interrupted || got.AdmissionToken != 8 || got.Tools["t1"].State != ToolUnknown || got.ToolAttempts["ta1"].Fence != 14 || got.Exposures["t1"] != ExposureUnknown || len(got.Blockers) != 2 || len(got.Interruptions) != 1 {
		t.Fatalf("Trace B recovery facts lost: lifecycle=%d token=%d tool=%d fence=%d exposure=%d blockers=%d", got.Lifecycle, got.AdmissionToken, got.Tools["t1"].State, got.ToolAttempts["ta1"].Fence, got.Exposures["t1"], len(got.Blockers))
	}
	stale := ev("stale-callback", EventToolResultRecorded)
	stale.ToolID, stale.AttemptID, stale.Ref, stale.Disposition = "t1", "ta1", "late-result", ResultSucceeded
	stale.Token, stale.Incarnation, stale.Fence = 7, "oldinc", 14
	if _, err := applyBatch(t, got, Batch{CommandID: "late", Events: []Event{stale}}); err == nil {
		t.Fatal("old callback crossed the new ownership fence")
	}
}

func TestStepBudgetWaitExtendsSameRunAndUserMayClose(t *testing.T) {
	waiting := makeStepBudgetRun(t)
	if waiting.Lifecycle != Waiting || !hasWaitReason(waiting, BlockStepBudget) || waiting.Finished || waiting.StepsStarted != 1 {
		t.Fatalf("step budget did not create persistent wait: lifecycle=%d reasons=%v finished=%v steps=%d", waiting.Lifecycle, waiting.WaitReasons, waiting.Finished, waiting.StepsStarted)
	}
	if _, err := applyBatch(t, waiting, Batch{CommandID: "blocked-step", Events: []Event{{ID: "blocked-step-event", Kind: EventStepStarted, RunID: "budget-run", SessionID: "s", StepID: "step2", Generation: 2}}}); err == nil {
		t.Fatal("step started while step budget wait was unresolved")
	}
	if _, err := applyBatch(t, waiting, Batch{CommandID: "peer-extension", Events: []Event{{ID: "peer-extension-event", Kind: EventBudgetExtended, RunID: "budget-run", SessionID: "s", BudgetKind: BudgetSteps, BudgetBy: 2, Actor: ActorParent}}}); err == nil {
		t.Fatal("non-user extended step budget")
	}
	extended, err := applyBatch(t, waiting, Batch{CommandID: "user-extension", Events: []Event{{ID: "user-extension-event", Kind: EventBudgetExtended, RunID: "budget-run", SessionID: "s", BudgetKind: BudgetSteps, BudgetBy: 2, Actor: ActorUser}}})
	if err != nil {
		t.Fatalf("user extension rejected: %v", err)
	}
	if extended.Finished || extended.StepsStarted != 1 || extended.StepBudgetExtended != 2 || extended.Lifecycle != Running || hasWaitReason(extended, BlockStepBudget) {
		t.Fatalf("extension did not resume same run/count: lifecycle=%d finished=%v steps=%d extension=%d reasons=%v", extended.Lifecycle, extended.Finished, extended.StepsStarted, extended.StepBudgetExtended, extended.WaitReasons)
	}
	if _, err := applyBatch(t, extended, Batch{CommandID: "next-step", Events: []Event{{ID: "next-step-event", Kind: EventStepStarted, RunID: "budget-run", SessionID: "s", StepID: "step2", Generation: 2}}}); err != nil {
		t.Fatalf("same run could not continue after extension: %v", err)
	}

	closed := makeStepBudgetRun(t)
	closed, err = applyBatch(t, closed, Batch{CommandID: "user-budget-stop", Events: []Event{{ID: "user-budget-stop-event", Kind: EventRunStopRequested, RunID: "budget-run", SessionID: "s", StopReason: StopBudget, StopMode: StopTerminal, BudgetKind: BudgetSteps, Actor: ActorUser, Ref: "budget-stop"}}})
	if err != nil {
		t.Fatalf("user step-budget stop rejected: %v", err)
	}
	closed, err = applyBatch(t, closed, Batch{CommandID: "budget-finish", Events: []Event{{ID: "budget-finish-event", Kind: EventRunFinished, RunID: "budget-run", SessionID: "s", Outcome: OutcomeBudgetExceeded}}})
	if err != nil || !closed.Finished || closed.Outcome != OutcomeBudgetExceeded {
		t.Fatalf("step-budget user close failed: finished=%v outcome=%d err=%v", closed.Finished, closed.Outcome, err)
	}
}

func TestOtherBudgetsRemainTerminal(t *testing.T) {
	s := makeBudgetCompletionState()
	var err error
	s, err = applyBatch(t, s, Batch{CommandID: "other-budget-stop", Events: []Event{{ID: "other-budget-stop-event", Kind: EventRunStopRequested, RunID: "budget-run", SessionID: "s", StopReason: StopBudget, StopMode: StopTerminal, BudgetKind: BudgetModelTokens, Actor: ActorSystem, Ref: "tokens-exhausted"}}})
	if err != nil {
		t.Fatal(err)
	}
	s, err = applyBatch(t, s, Batch{CommandID: "other-budget-step", Events: []Event{{ID: "other-budget-step-event", Kind: EventStepCompleted, RunID: "budget-run", SessionID: "s", Next: NextEnd, Continuation: ContinuationBudget}}})
	if err != nil {
		t.Fatal(err)
	}
	s, err = applyBatch(t, s, Batch{CommandID: "other-budget-finish", Events: []Event{{ID: "other-budget-finish-event", Kind: EventRunFinished, RunID: "budget-run", SessionID: "s", Outcome: OutcomeBudgetExceeded}}})
	if err != nil || !s.Finished || s.BudgetStopKind != BudgetModelTokens {
		t.Fatalf("non-step budget did not finish terminally: finished=%v kind=%d err=%v", s.Finished, s.BudgetStopKind, err)
	}
}

func makeStepBudgetRun(t *testing.T) State {
	t.Helper()
	s := EmptyState()
	s.Accepted, s.Admitted, s.RunID, s.SessionID = true, true, "budget-run", "s"
	s.AdmissionToken, s.Incarnation, s.InitialStepLimit = 1, "inc", 1
	derive(&s)
	started, err := applyBatch(t, s, Batch{CommandID: "budget-step-start", Events: []Event{{ID: "budget-step-start-event", Kind: EventStepStarted, RunID: "budget-run", SessionID: "s", StepID: "step1", Generation: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	started.Models["m"] = ModelInvocation{ID: "m", StepID: "step1", State: InvocationCompleted, ResponseRef: "response", AttemptIDs: []ID{"a"}}
	started.ModelAttempts["a"] = Attempt{ID: "a", InvocationID: "m", StepID: "step1", State: AttemptCompleted}
	st := started.Steps[0]
	st.ModelID, st.ResponseRef, st.Open = "m", "response", true
	started.Steps[0] = st
	derive(&started)
	completed, err := applyBatch(t, started, Batch{CommandID: "budget-step-complete", Events: []Event{{ID: "budget-step-complete-event", Kind: EventStepCompleted, RunID: "budget-run", SessionID: "s", Next: NextWait, Continuation: ContinuationStepBudget}}})
	if err != nil {
		t.Fatal(err)
	}
	return completed
}

func makeBudgetCompletionState() State {
	s := EmptyState()
	s.Accepted, s.Admitted, s.RunID, s.SessionID = true, true, "budget-run", "s"
	s.AdmissionToken, s.Incarnation, s.InitialStepLimit = 1, "inc", 1
	s.Models["m"] = ModelInvocation{ID: "m", StepID: "step1", State: InvocationCompleted, AttemptIDs: []ID{"a"}, ResponseRef: "response"}
	s.ModelAttempts["a"] = Attempt{ID: "a", InvocationID: "m", StepID: "step1", State: AttemptCompleted}
	s.Steps = []Step{{ID: "step1", Index: 0, Open: true, ModelID: "m", ResponseRef: "response"}}
	s.CurrentStep, s.StepsStarted = 0, 1
	derive(&s)
	return s
}

func approvalWaitState() State {
	s := EmptyState()
	s.Accepted, s.Admitted, s.RunID, s.SessionID = true, true, "r", "s"
	s.Tools["tool"] = ToolInvocation{ID: "tool", StepID: "step", State: ToolAwaitingApproval, RequestRef: "request", CheckGeneration: 2, AttemptIDs: []ID{"ta"}}
	s.ToolAttempts["ta"] = ToolAttempt{ID: "ta", ToolID: "tool", StepID: "step", State: AttemptPlanned}
	s.Blockers["approval"] = Blocker{ID: "approval", Kind: BlockApproval, TargetID: "tool", Ref: "request", Generation: 2}
	derive(&s)
	return s
}

func terminalControlState() State {
	s := EmptyState()
	s.Accepted, s.Admitted, s.Finished, s.Outcome = true, true, true, OutcomeCompleted
	s.Models["m"] = ModelInvocation{ID: "m", StepID: "step", State: InvocationCompleted, Disposition: ResultSucceeded, ResponseRef: "resp", AttemptIDs: []ID{"a"}, Candidates: []ToolCandidate{{ID: "tool", AttemptID: "ta", ArgsRef: "args", Descriptor: "desc"}}}
	s.ModelAttempts["a"] = Attempt{ID: "a", InvocationID: "m", StepID: "step", State: AttemptCompleted}
	s.Tools["tool"] = ToolInvocation{ID: "tool", State: ToolSucceeded, StepID: "step", SourceIndex: 0, AttemptIDs: []ID{"ta"}, Disposition: ResultSucceeded, ResultRef: "result"}
	s.ToolAttempts["ta"] = ToolAttempt{ID: "ta", ToolID: "tool", StepID: "step", State: AttemptCompleted}
	modelItem := TranscriptItem{InvocationID: "m", ResponseRef: "resp", IsModel: true}
	toolItem := TranscriptItem{InvocationID: "tool", ResponseRef: "resp", ResultRef: "result", Disposition: ResultSucceeded}
	s.Steps = []Step{{ID: "step", Open: false, Next: NextEnd, ModelID: "m", ResponseRef: "resp", ToolIDs: []ID{"tool"}, CompletedTools: []ID{"tool"}, Transcript: []TranscriptItem{modelItem, toolItem}}}
	s.CurrentStep = 0
	s.Transcript = []TranscriptItem{modelItem, toolItem}
	derive(&s)
	return s
}

func TestKernelCopiesCallerOwnedSlices(t *testing.T) {
	candidates := []ToolCandidate{{ID: "t", ArgsRef: "args", Descriptor: "d"}}
	input := Event{ID: "e", Kind: EventRunAccepted, RunID: "r", SessionID: "s", InputRef: "i", Origin: OriginUser, Candidates: candidates}
	batch := Batch{CommandID: "c", Events: []Event{input}}
	state, err := applyBatch(t, EmptyState(), batch)
	if err != nil {
		t.Fatal(err)
	}
	candidates[0].ID = "mutated"
	batch.Events[0].ID = "mutated"
	if state.RunID != "r" || state.EventIDs["e"].ID != "e" {
		t.Fatal("caller mutation changed committed state")
	}
}

func TestCloneWindowsDoesNotAliasSource(t *testing.T) {
	s := EmptyState()
	s.Windows["w"] = TimeWindow{ID: "w", ObservedRefs: []ID{"usage1"}}
	copy := cloneState(s)
	w := copy.Windows["w"]
	w.ObservedRefs[0] = "changed"
	copy.Windows["w"] = w
	if s.Windows["w"].ObservedRefs[0] != "usage1" {
		t.Fatal("cloning state changed source window map or references")
	}
}

func TestFailedBatchLeavesSourceWindowAndAttemptMapsUntouched(t *testing.T) {
	s := EmptyState()
	s.Accepted, s.Admitted, s.RunID, s.SessionID = true, true, "r", "s"
	s.AdmissionToken, s.Incarnation = 7, "inc"
	s.Models["m"] = ModelInvocation{ID: "m", StepID: "step", State: InvocationActive, AttemptIDs: []ID{"a"}}
	s.ModelAttempts["a"] = Attempt{ID: "a", InvocationID: "m", StepID: "step", State: AttemptCompleted, Usage: UsageFact{Status: UsageKnown, Ref: "old-usage"}}
	s.Timers["timer"] = Timer{ID: "timer", Generation: 1, TargetID: "window", Purpose: TimerRunDeadline, Pending: true}
	s.Windows["window"] = TimeWindow{ID: "window", TimerID: "timer", Generation: 1, Incarnation: "inc", Open: true, Reserved: UsageReserved}
	_, err := applyBatch(t, s, Batch{CommandID: "fail-after-window-write", Events: []Event{
		{ID: "observe-first", Kind: EventBudgetObserved, RunID: "r", SessionID: "s", InvocationID: "m", AttemptID: "a", WindowID: "window", Ref: "new-usage", Usage: UsageKnown},
		{ID: "observe-invalid", Kind: EventBudgetObserved, RunID: "r", SessionID: "s", InvocationID: "m", AttemptID: "a", WindowID: "window", Ref: "", Usage: UsageUnknown},
	}})
	if err == nil {
		t.Fatal("invalid batch unexpectedly committed")
	}
	if len(s.Windows["window"].ObservedRefs) != 0 || s.ModelAttempts["a"].Usage.Ref != "old-usage" || s.Revision != 0 {
		t.Fatal("failed batch mutated source state")
	}
}

func TestPhaseProjectionForCommittedStepBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name string
		step Step
		want Phase
	}{
		{"calling model", Step{Open: true, ModelID: "m"}, PhaseCallingModel},
		{"processing tools", Step{Open: true, ModelID: "m", ResponseRef: "resp"}, PhaseProcessingTools},
		{"closed continue", Step{Open: false, Next: NextContinue, ModelID: "m", ResponseRef: "resp"}, PhasePreparing},
		{"step budget wait", Step{Open: false, Next: NextWait, Continuation: ContinuationStepBudget, ModelID: "m", ResponseRef: "resp"}, PhasePreparing},
		{"closed end", Step{Open: false, Next: NextEnd, ModelID: "m", ResponseRef: "resp"}, PhaseFinalizing},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := EmptyState()
			s.Steps = []Step{tc.step}
			s.CurrentStep = 0
			if tc.step.ModelID != "" {
				s.Models[tc.step.ModelID] = ModelInvocation{ID: tc.step.ModelID, State: InvocationCompleted, ResponseRef: tc.step.ResponseRef}
			}
			if got := derivePhase(s); got != tc.want {
				t.Fatalf("phase=%d want=%d", got, tc.want)
			}
		})
	}
}

func TestCommittedStepCloseProjectsPreparingThenFinalizing(t *testing.T) {
	s := EmptyState()
	s.Accepted, s.Admitted, s.RunID, s.SessionID = true, true, "phase-run", "s"
	s.AdmissionToken, s.Incarnation, s.InitialStepLimit = 3, "inc", 100
	s.Models["m1"] = ModelInvocation{ID: "m1", StepID: "s1", State: InvocationCompleted, AttemptIDs: []ID{"a1"}, ResponseRef: "resp1"}
	s.ModelAttempts["a1"] = Attempt{ID: "a1", InvocationID: "m1", StepID: "s1", State: AttemptCompleted}
	s.Steps = []Step{{ID: "s1", Open: true, ModelID: "m1", ResponseRef: "resp1"}}
	s.CurrentStep, s.StepsStarted = 0, 1
	derive(&s)
	var err error
	s, err = applyBatch(t, s, Batch{CommandID: "close-continue", Events: []Event{{ID: "close-continue-event", Kind: EventStepCompleted, RunID: "phase-run", SessionID: "s", Next: NextContinue, Continuation: ContinuationToolCalls}}})
	if err != nil {
		t.Fatal(err)
	}
	if s.Phase != PhasePreparing {
		t.Fatalf("closed continue phase=%d want preparing", s.Phase)
	}
	s, err = applyBatch(t, s, Batch{CommandID: "start-next", Events: []Event{{ID: "start-next-event", Kind: EventStepStarted, RunID: "phase-run", SessionID: "s", StepID: "s2", Generation: 2}}})
	if err != nil {
		t.Fatal(err)
	}
	s, err = applyBatch(t, s, Batch{CommandID: "plan-next", Events: []Event{{ID: "plan-next-event", Kind: EventModelRequestPlanned, RunID: "phase-run", SessionID: "s", InvocationID: "m2", AttemptID: "a2", Ref: "req2"}}})
	if err != nil {
		t.Fatal(err)
	}
	s, err = applyBatch(t, s, Batch{CommandID: "dispatch-next", Events: []Event{{ID: "dispatch-next-event", Kind: EventModelDispatchStarted, RunID: "phase-run", SessionID: "s", InvocationID: "m2", AttemptID: "a2", Ref: "dispatch", Token: 3, Incarnation: "inc", Fence: 2}}})
	if err != nil {
		t.Fatal(err)
	}
	s, err = applyBatch(t, s, Batch{CommandID: "response-next", Events: []Event{{ID: "response-next-event", Kind: EventModelResponseRecorded, RunID: "phase-run", SessionID: "s", InvocationID: "m2", AttemptID: "a2", Ref: "resp2", CauseRef: "usage-ref2", Usage: UsageKnown, Token: 3, Incarnation: "inc", Fence: 2}}})
	if err != nil {
		t.Fatal(err)
	}
	s, err = applyBatch(t, s, Batch{CommandID: "close-end", Events: []Event{{ID: "close-end-event", Kind: EventStepCompleted, RunID: "phase-run", SessionID: "s", Next: NextEnd, Continuation: ContinuationNaturalEnd}}})
	if err != nil {
		t.Fatal(err)
	}
	if s.Phase != PhaseFinalizing || s.Finished {
		t.Fatalf("closed end before finish phase=%d finished=%v", s.Phase, s.Finished)
	}
}
