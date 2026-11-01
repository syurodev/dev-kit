package kernel

import (
	"reflect"
	"testing"
)

func closureCommandFor(s State, id ID, kind CommandKind, input ClosureInput) Command {
	return Command{
		ID: id, Kind: kind, RunID: s.RunID, SessionID: s.SessionID,
		ExpectedRevision: s.Revision, WriterToken: s.AdmissionToken, WriterIncarnation: s.Incarnation,
		Closure: input,
	}
}

func TestExactClosureAcceptsToolAndModelUnknownWithoutMetadataMutation(t *testing.T) {
	t.Run("tool result", func(t *testing.T) {
		h := newTransitionHarness(t)
		toolID, attemptID := h.toolDispatching()
		unknown := h.issue(Command{Kind: CommandObserveUnknownTool, Transition: TransitionInput{
			TargetID: toolID, AttemptID: attemptID, CauseRef: "tool-close-cause", Exposure: ExposureUnknown,
			ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Fence: 2,
		}})
		beforeTranscript := append([]TranscriptItem(nil), h.state.Transcript...)
		beforeContextRevision, beforeContextRequirement := h.state.ContextRevision, h.state.ContextRequirement
		input := ClosureInput{PreviewRef: "exact-preview", ReceiptRef: "exact-receipt", PreviewVersion: 1, Revision: h.state.Revision, Actor: ActorUser,
			Targets: []ClosureTargetFact{{Kind: ClosureTargetUnknownTool, TargetID: toolID, AttemptID: attemptID, CauseRef: "tool-close-cause", CauseEventRef: unknown.Events[1].ID, ResultRef: "tool-abandoned-result"}}}
		batch := h.issue(closureCommandFor(h.state, "exact-tool-close", CommandAcceptUnknownOutcome, input))
		want := []EventKind{EventUnknownAccepted, EventToolResultRecorded, EventRunRecoveryCleared, EventStepAborted, EventRunFinished}
		if len(batch.Events) != len(want) {
			t.Fatalf("exact tool closure emitted %d events, want %d: %#v", len(batch.Events), len(want), batch.Events)
		}
		for i, kind := range want {
			if batch.Events[i].Kind != kind {
				t.Fatalf("exact tool closure event %d is %v, want %v", i, batch.Events[i].Kind, kind)
			}
		}
		if batch.Events[0].Ref != "exact-preview" || batch.Events[0].ReceiptRef != "exact-receipt" || batch.Events[0].Actor != ActorUser || batch.Events[0].PreviewMode != ClosurePreviewExact || batch.Events[0].PreviewVersion != 1 || batch.Events[0].Revision != input.Revision || batch.Events[1].Ref != "tool-abandoned-result" || batch.Events[1].Disposition != ResultAbandonedUnknown || batch.Events[1].Exposure != ExposureUnknown || batch.Events[2].RecoveryDisposition != RecoveryDispositionAcceptedUnknown || h.state.ClosureConfirmed || h.state.ContextRevision != beforeContextRevision || h.state.ContextRequirement != beforeContextRequirement || !h.state.Finished || h.state.Outcome != OutcomeAbandonedUnknown || h.state.Exposures[toolID] != ExposureUnknown || h.state.ToolAttempts[attemptID].State != AttemptAbandoned || !h.state.Interruptions["tool-close-cause"].Accepted || len(h.state.Transcript) != len(beforeTranscript)+1 || !reflect.DeepEqual(h.state.Transcript[:len(beforeTranscript)], beforeTranscript) {
			t.Fatalf("exact tool acceptance changed metadata or lost unknown provenance: events=%#v state=%#v", batch.Events, h.state)
		}
		if len(unknown.Events) != 2 || unknown.Events[1].Kind != EventRunInterrupted || unknown.Events[1].CauseRef != "tool-close-cause" {
			t.Fatal("exact closure was not bound to the persisted interruption evidence")
		}
		replay, err := Decide(h.state, batch.Intent)
		if err != nil || !reflect.DeepEqual(replay, batch) {
			t.Fatalf("exact closure receipt replay was unstable: err=%v", err)
		}
		changedReceipt := batch.Intent
		changedReceipt.Closure.ReceiptRef = "different-user-receipt"
		if _, err := Decide(h.state, changedReceipt); err == nil {
			t.Fatal("same exact-closure command ID accepted a changed receipt")
		}
	})

	t.Run("model attempt with unknown usage", func(t *testing.T) {
		h := newTransitionHarness(t)
		invocationID, attemptID := h.modelInFlight()
		recovery := h.issue(Command{Kind: CommandAcquireOwnership, Transition: TransitionInput{
			ExecutorToken: 8, ExecutorIncarnation: "writer-inc-2", ReceiptRef: "recovery-receipt",
			Recoveries: []RecoveryFact{{TargetID: invocationID, AttemptID: attemptID, CauseRef: "model-close-cause", PartialRef: "partial-response", Usage: UsageUnknown, BlockerID: "recovery-blocker", PreviousToken: 7, PreviousIncarnation: "writer-inc-1", Fence: 1}},
		}})
		beforeContextRevision, beforeContextRequirement := h.state.ContextRevision, h.state.ContextRequirement
		input := ClosureInput{PreviewRef: "model-exact-preview", ReceiptRef: "model-exact-receipt", PreviewVersion: 1, Revision: h.state.Revision, Actor: ActorUser,
			Targets: []ClosureTargetFact{{Kind: ClosureTargetUnknownModel, TargetID: invocationID, AttemptID: attemptID, CauseRef: "model-close-cause", CauseEventRef: recovery.Events[2].ID}}}
		batch := h.issue(closureCommandFor(h.state, "exact-model-close", CommandAcceptUnknownOutcome, input))
		want := []EventKind{EventUnknownAccepted, EventRunRecoveryCleared, EventRunBlockCleared, EventStepAborted, EventRunFinished}
		if len(batch.Events) != len(want) {
			t.Fatalf("exact model closure emitted %d events, want %d: %#v", len(batch.Events), len(want), batch.Events)
		}
		for i, kind := range want {
			if batch.Events[i].Kind != kind {
				t.Fatalf("exact model closure event %d is %v, want %v", i, batch.Events[i].Kind, kind)
			}
		}
		if !batch.Events[0].IsModel || batch.Events[0].PreviewMode != ClosurePreviewExact || batch.Events[0].PreviewVersion != 1 || batch.Events[0].Revision != input.Revision || batch.Events[0].InvocationID != invocationID || batch.Events[0].AttemptID != attemptID || batch.Events[0].CauseRef != "model-close-cause" || batch.Events[0].Ref != "model-exact-preview" || batch.Events[0].ReceiptRef != "model-exact-receipt" || batch.Events[2].BlockerID != "recovery-blocker" || !h.state.Finished || h.state.Outcome != OutcomeAbandonedUnknown || h.state.ClosureConfirmed || h.state.ContextRevision != beforeContextRevision || h.state.ContextRequirement != beforeContextRequirement || h.state.ModelAttempts[attemptID].Usage.Status != UsageUnknown || h.state.ModelAttempts[attemptID].State != AttemptAbandoned || h.state.Exposures[attemptID] != ExposureUnknown || !h.state.Interruptions["model-close-cause"].Accepted {
			t.Fatalf("model closure lost unknown usage/exposure or changed unavailable-context metadata: events=%#v attempt=%#v", batch.Events, h.state.ModelAttempts[attemptID])
		}
	})
}

func newMetadataClosureFixture(t *testing.T) (*transitionHarness, ClosureInput, ID) {
	t.Helper()
	h := newTransitionHarness(t)
	invocationID, _ := h.modelInFlight()
	h.issue(Command{Kind: CommandObserveModelResponse, ModelResponse: ModelResponseInput{
		InvocationID: invocationID, AttemptID: "model-attempt-1", ResponseRef: "response-ref", Usage: UsageKnown, UsageRef: "usage-ref",
		Fence: 1, ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1",
		Candidates: []ToolCandidate{
			{ID: "tool-1", AttemptID: "tool-attempt-1", ArgsRef: "args-one", Descriptor: "descriptor-one", SourceIndex: 0, EffectClass: EffectSideEffecting},
			{ID: "tool-2", AttemptID: "tool-attempt-2", ArgsRef: "args-two", Descriptor: "descriptor-two", SourceIndex: 1, EffectClass: EffectSideEffecting},
		},
	}})
	h.issue(Command{Kind: CommandPrepareTool, Tool: ToolInput{ToolID: "tool-1", Generation: 1, Ref: "check-ref"}})
	h.issue(Command{Kind: CommandRequestToolApproval, Transition: TransitionInput{TargetID: "tool-1", Generation: 1, Ref: "args-one", RequestRef: "approval-request", BlockerID: "approval-blocker"}})
	h.issue(Command{Kind: CommandResolveToolApproval, Transition: TransitionInput{TargetID: "tool-1", AttemptID: "tool-attempt-1", Generation: 1, RequestRef: "approval-request", Ref: "args-one", ReceiptRef: "approval-receipt", Disposition: ResultSucceeded}})
	h.issue(Command{Kind: CommandDispatchTool, Tool: ToolInput{ToolID: "tool-1", AttemptID: "tool-attempt-1", Generation: 1, ReceiptRef: "approval-receipt", Ref: "dispatch-ref", ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Fence: 2}})
	unknown := h.issue(Command{Kind: CommandObserveUnknownTool, Transition: TransitionInput{TargetID: "tool-1", AttemptID: "tool-attempt-1", CauseRef: "effect-unknown-cause", Exposure: ExposureUnknown, ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Fence: 2}})

	// The host observation is persisted as its own typed interruption fact. It
	// represents unavailable protected context and deliberately carries no text.
	e := Event{ID: "payload-unavailable-event", Kind: EventRunInterrupted, ProtocolVersion: CurrentProtocolVersion, RunID: h.state.RunID, SessionID: h.state.SessionID, ToolID: "context-target", CauseRef: "payload-unavailable-cause", Interrupt: InterruptPayloadUnavailable}
	batch := Batch{CommandID: "seed-payload-unavailable", ExpectedRevision: h.state.Revision, WriterToken: h.state.AdmissionToken, WriterIncarnation: h.state.Incarnation, ProtocolVersion: CurrentProtocolVersion, Events: []Event{e}}
	next, err := Evolve(h.state, batch)
	if err != nil {
		t.Fatalf("could not seed persisted payload-unavailable observation: %v", err)
	}
	h.state = next
	input := ClosureInput{
		PreviewRef: "metadata-preview", ReceiptRef: "metadata-receipt", PreviewVersion: 1,
		Revision: h.state.Revision, Actor: ActorUser, CauseEventRef: e.ID,
		UnavailableReason: UnavailablePayloadMissing, PreviousContextRevision: h.state.ContextRevision,
		ContextRevision: h.state.ContextRevision + 1, ContextRequirement: ContextRequirementFreshRequired,
		Targets: []ClosureTargetFact{
			{Kind: ClosureTargetUnknownTool, TargetID: "tool-1", AttemptID: "tool-attempt-1", CauseRef: "effect-unknown-cause", CauseEventRef: unknown.Events[1].ID, ResultRef: "unknown-result"},
			{Kind: ClosureTargetUnavailableCause, TargetID: "context-target", CauseRef: "payload-unavailable-cause", CauseEventRef: e.ID},
			{Kind: ClosureTargetPlannedTool, TargetID: "tool-2", AttemptID: "tool-attempt-2"},
		},
	}
	return h, input, e.ID
}

func metadataCloseCommand(h *transitionHarness, input ClosureInput) Command {
	return closureCommandFor(h.state, "metadata-manifest-close", CommandConfirmRunClosure, input)
}

func requireMetadataPositiveControl(t *testing.T, h *transitionHarness, input ClosureInput) Batch {
	t.Helper()
	cmd := metadataCloseCommand(h, input)
	batch, err := Decide(h.state, cmd)
	if err != nil {
		t.Fatalf("valid metadata manifest positive control rejected: %v", err)
	}
	if _, err := Evolve(h.state, batch); err != nil {
		t.Fatalf("valid metadata manifest positive control failed to evolve: %v", err)
	}
	return batch
}

func TestMetadataClosureBindsExactManifestAndUnavailableContext(t *testing.T) {
	h, input, causeEventID := newMetadataClosureFixture(t)
	beforeContext, beforeTranscript := h.state.ContextRevision, append([]TranscriptItem(nil), h.state.Transcript...)
	if !validID(input.PreviewRef) || !validID(input.ReceiptRef) || !validID(input.CauseEventRef) || !validID(causeEventID) {
		t.Fatal("metadata closure fixture contains an invalid preview, receipt, or cause event ID")
	}
	for _, target := range input.Targets {
		valid := validID(target.TargetID)
		switch target.Kind {
		case ClosureTargetUnknownTool, ClosureTargetUnknownModel:
			valid = valid && validID(target.AttemptID) && validID(target.CauseRef) && validID(target.CauseEventRef)
			if target.Kind == ClosureTargetUnknownTool {
				valid = valid && validID(target.ResultRef)
			}
		case ClosureTargetUnavailableCause:
			valid = valid && validID(target.CauseRef) && validID(target.CauseEventRef)
		case ClosureTargetPlannedTool, ClosureTargetPlannedModel:
			valid = valid && validID(target.AttemptID)
		}
		if !valid {
			t.Fatalf("fixture manifest contains invalid identifiers: %#v", target)
		}
	}
	valid := requireMetadataPositiveControl(t, h, input)
	command := h.issue(metadataCloseCommand(h, input))
	want := []EventKind{EventRunClosureConfirmed, EventToolResultRecorded, EventRunRecoveryCleared, EventRunRecoveryCleared, EventToolResultRecorded, EventStepAborted, EventRunFinished}
	if len(command.Events) != len(want) {
		t.Fatalf("metadata closure emitted %d events, want %d: %#v", len(command.Events), len(want), command.Events)
	}
	for i, kind := range want {
		if command.Events[i].Kind != kind {
			t.Fatalf("metadata closure event %d is %v, want %v", i, command.Events[i].Kind, kind)
		}
	}
	header := command.Events[0]
	if !reflect.DeepEqual(header.ClosureTargets, input.Targets) || header.PreviewMode != ClosurePreviewMetadataOnly || header.PreviewVersion != input.PreviewVersion || header.Revision != input.Revision || header.Actor != ActorUser || header.ReceiptRef != input.ReceiptRef || header.Ref != input.PreviewRef || header.CauseEventRef != causeEventID || header.UnavailableReason != UnavailablePayloadMissing {
		t.Fatalf("closure header did not preserve exact manifest and preview receipt: %#v", header)
	}
	for _, event := range command.Events {
		if event.Kind == EventUnknownAccepted {
			t.Fatal("metadata-only close fabricated exact-preview UnknownAccepted evidence")
		}
	}
	if command.Events[3].RecoveryDisposition != RecoveryDispositionClosedUnavailable || command.Events[4].Disposition != ResultCancelled || !command.Events[4].Terminal || command.Events[len(command.Events)-1].Outcome != OutcomeAbandonedUnknown {
		t.Fatalf("closure did not separately close unavailable cause, cancel planned work, and retain unknown outcome: %#v", command.Events)
	}
	if !h.state.Finished || h.state.Outcome != OutcomeAbandonedUnknown || !h.state.ClosureConfirmed || h.state.ContextRevision != beforeContext+1 || h.state.ContextRequirement != ContextRequirementFreshRequired || !h.state.Interruptions["payload-unavailable-cause"].Resolved || !h.state.Interruptions["effect-unknown-cause"].Accepted || h.state.Exposures["tool-1"] != ExposureUnknown || h.state.Tools["tool-1"].Disposition != ResultAbandonedUnknown || h.state.Tools["tool-2"].Disposition != ResultCancelled || h.state.ToolAttempts["tool-attempt-2"].State != AttemptCancelled || !h.state.Steps[h.state.CurrentStep].Aborted {
		t.Fatalf("metadata close lost provenance or failed to cancel predispatch work: state=%#v", h.state)
	}
	if !reflect.DeepEqual(h.state.Transcript[:len(beforeTranscript)], beforeTranscript) || h.state.Models["model-1"].ResponseRef != "response-ref" || valid.Events[0].ClosureTargets[1].TargetID != "context-target" || hasActiveAttempt(h.state) {
		t.Fatal("metadata close changed old transcript or exact preview manifest")
	}
	if h.state.ContextRequirement == ContextRequirementNone {
		t.Fatal("missing protected payload did not require fresh context")
	}
	replay, err := Decide(h.state, command.Intent)
	if err != nil || !reflect.DeepEqual(replay, command) {
		t.Fatalf("metadata close receipt replay was unstable: err=%v", err)
	}
	changedReceipt := command.Intent
	changedReceipt.Closure.ReceiptRef = "different-metadata-receipt"
	if _, err := Decide(h.state, changedReceipt); err == nil {
		t.Fatal("same metadata-close command ID accepted a changed receipt")
	}
}

func TestMetadataClosureRejectsIncompleteOrMismatchedManifest(t *testing.T) {
	cases := map[string]func(*ClosureInput){
		"omitted-targets": func(in *ClosureInput) { in.Targets = nil },
		"omitted-cause":   func(in *ClosureInput) { in.Targets = []ClosureTargetFact{in.Targets[0], in.Targets[2]} },
		"extra-target": func(in *ClosureInput) {
			in.Targets = append(in.Targets, ClosureTargetFact{Kind: ClosureTargetPlannedTool, TargetID: "not-in-run", AttemptID: "not-in-run-attempt"})
		},
		"duplicate-target":            func(in *ClosureInput) { in.Targets = append(in.Targets, in.Targets[0]) },
		"wrong-target":                func(in *ClosureInput) { in.Targets[0].TargetID = "other-tool" },
		"wrong-cause":                 func(in *ClosureInput) { in.Targets[0].CauseRef = "other-interruption" },
		"missing-receipt":             func(in *ClosureInput) { in.ReceiptRef = "" },
		"missing-preview-ref":         func(in *ClosureInput) { in.PreviewRef = "" },
		"zero-preview-version":        func(in *ClosureInput) { in.PreviewVersion = 0 },
		"unsupported-preview-version": func(in *ClosureInput) { in.PreviewVersion = CurrentClosurePreviewVersion + 1 },
		"wrong-revision":              func(in *ClosureInput) { in.Revision++ },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			h, input, _ := newMetadataClosureFixture(t)
			_ = requireMetadataPositiveControl(t, h, input)
			mutate(&input)
			cmd := metadataCloseCommand(h, input)
			if !validID(cmd.ID) || !validID(cmd.Closure.PreviewRef) && name != "missing-preview-ref" {
				t.Fatalf("test constructed invalid command fixture: %#v", cmd)
			}
			if _, err := Decide(h.state, cmd); err == nil {
				t.Fatal("closure accepted an omitted, duplicate, extra, or mismatched manifest")
			}
		})
	}
}

func newExactClosureFixture(t *testing.T) (*transitionHarness, ClosureInput) {
	t.Helper()
	h := newTransitionHarness(t)
	toolID, attemptID := h.toolDispatching()
	unknown := h.issue(Command{Kind: CommandObserveUnknownTool, Transition: TransitionInput{
		TargetID: toolID, AttemptID: attemptID, CauseRef: "exact-unknown-cause", Exposure: ExposureUnknown,
		ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1", Fence: 2,
	}})
	return h, ClosureInput{
		PreviewRef: "exact-preview", ReceiptRef: "exact-receipt", PreviewVersion: 1, Revision: h.state.Revision, Actor: ActorUser,
		Targets: []ClosureTargetFact{{Kind: ClosureTargetUnknownTool, TargetID: toolID, AttemptID: attemptID, CauseRef: "exact-unknown-cause", CauseEventRef: unknown.Events[1].ID, ResultRef: "exact-abandoned-result"}},
	}
}

func TestExactClosureRejectsMissingAndMismatchedAuthority(t *testing.T) {
	cases := map[string]func(*ClosureInput){
		"omitted-targets": func(in *ClosureInput) { in.Targets = nil },
		"extra-target": func(in *ClosureInput) {
			in.Targets = append(in.Targets, ClosureTargetFact{Kind: ClosureTargetUnknownTool, TargetID: "missing-tool", AttemptID: "missing-attempt", CauseRef: "missing-cause", CauseEventRef: "missing-event", ResultRef: "missing-result"})
		},
		"duplicate-target":       func(in *ClosureInput) { in.Targets = append(in.Targets, in.Targets[0]) },
		"wrong-target":           func(in *ClosureInput) { in.Targets[0].TargetID = "other-tool" },
		"wrong-cause":            func(in *ClosureInput) { in.Targets[0].CauseRef = "other-cause" },
		"wrong-cause-event":      func(in *ClosureInput) { in.Targets[0].CauseEventRef = "other-event" },
		"missing-receipt":        func(in *ClosureInput) { in.ReceiptRef = "" },
		"missing-preview-ref":    func(in *ClosureInput) { in.PreviewRef = "" },
		"zero-preview-version":   func(in *ClosureInput) { in.PreviewVersion = 0 },
		"stale-preview-revision": func(in *ClosureInput) { in.Revision++ },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			h, input := newExactClosureFixture(t)
			control := closureCommandFor(h.state, "exact-close-command", CommandAcceptUnknownOutcome, input)
			batch, err := Decide(h.state, control)
			if err != nil {
				t.Fatalf("valid exact-preview control rejected: %v", err)
			}
			if _, err := Evolve(h.state, batch); err != nil {
				t.Fatalf("valid exact-preview control did not evolve: %v", err)
			}
			mutate(&input)
			cmd := closureCommandFor(h.state, "exact-close-command", CommandAcceptUnknownOutcome, input)
			if !validID(cmd.ID) {
				t.Fatalf("test built invalid command ID %q", cmd.ID)
			}
			if _, err := Decide(h.state, cmd); err == nil {
				t.Fatal("exact closure accepted omitted, extra, duplicate, or mismatched authority")
			}
		})
	}
}

func TestExactClosureReplayRejectsAlteredOrPartialTerminalBatch(t *testing.T) {
	cases := map[string]func(*Batch){
		"missing-preview-version": func(b *Batch) { b.Events[0].PreviewVersion = 0 },
		"preview-version":         func(b *Batch) { b.Events[0].PreviewVersion = CurrentClosurePreviewVersion + 1 },
		"preview-ref":             func(b *Batch) { b.Events[0].Ref = "other-preview" },
		"receipt":                 func(b *Batch) { b.Events[0].ReceiptRef = "other-receipt" },
		"revision":                func(b *Batch) { b.Events[0].Revision++ },
		"missing-abort":           func(b *Batch) { b.Events = append(b.Events[:3], b.Events[4:]...) },
		"missing-finish":          func(b *Batch) { b.Events = b.Events[:len(b.Events)-1] },
		"wrong-result-ref":        func(b *Batch) { b.Events[1].Ref = "other-result" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			h, input := newExactClosureFixture(t)
			batch, err := Decide(h.state, closureCommandFor(h.state, "replay-close", CommandAcceptUnknownOutcome, input))
			if err != nil {
				t.Fatalf("valid exact control rejected: %v", err)
			}
			mutate(&batch)
			if _, err := Evolve(h.state, batch); err == nil {
				t.Fatal("replay accepted an altered or incomplete exact-preview terminal batch")
			}
		})
	}
}

func TestExactClosureReplayRejectsInactiveIntentWithOriginalEvents(t *testing.T) {
	cases := map[string]func(*Batch){
		"model-response-branch": func(b *Batch) { b.Intent.ModelResponse.ResponseRef = "inactive-response" },
		"transition-branch":     func(b *Batch) { b.Intent.Transition.Ref = "inactive-transition-ref" },
		"unavailable-reason":    func(b *Batch) { b.Intent.Closure.UnavailableReason = UnavailablePayloadMissing },
		"cause-event-ref":       func(b *Batch) { b.Intent.Closure.CauseEventRef = "inactive-cause-event" },
		"previous-context-revision": func(b *Batch) {
			b.Intent.Closure.PreviousContextRevision = 1
		},
		"context-revision":    func(b *Batch) { b.Intent.Closure.ContextRevision = 1 },
		"context-requirement": func(b *Batch) { b.Intent.Closure.ContextRequirement = ContextRequirementFreshRequired },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			h, input := newExactClosureFixture(t)
			batch, err := Decide(h.state, closureCommandFor(h.state, "inactive-intent-replay", CommandAcceptUnknownOutcome, input))
			if err != nil {
				t.Fatalf("valid exact closure control rejected: %v", err)
			}
			if _, err := Evolve(h.state, batch); err != nil {
				t.Fatalf("original exact closure batch failed before tampering: %v", err)
			}
			originalEvents := append([]Event(nil), batch.Events...)
			mutate(&batch)
			if !reflect.DeepEqual(batch.Events, originalEvents) {
				t.Fatal("intent-only mutation unexpectedly changed durable events")
			}
			if _, err := Evolve(h.state, batch); err == nil {
				t.Fatal("replay accepted an inactive outer or metadata field in exact closure intent")
			}
		})
	}
}

func TestMetadataClosureRejectsTamperedEnvelopeAndContextAtEvolve(t *testing.T) {
	cases := map[string]func(*Batch){
		"preview-mode":        func(b *Batch) { b.Events[0].PreviewMode = ClosurePreviewExact },
		"missing-version":     func(b *Batch) { b.Events[0].PreviewVersion = 0 },
		"future-version":      func(b *Batch) { b.Events[0].PreviewVersion = CurrentClosurePreviewVersion + 1 },
		"preview-ref":         func(b *Batch) { b.Events[0].Ref = "different-preview" },
		"receipt":             func(b *Batch) { b.Events[0].ReceiptRef = "different-receipt" },
		"revision":            func(b *Batch) { b.Events[0].Revision++ },
		"cause-event":         func(b *Batch) { b.Events[0].CauseEventRef = "different-cause-event" },
		"context-revision":    func(b *Batch) { b.Events[0].ContextRevision++ },
		"context-requirement": func(b *Batch) { b.Events[0].ContextRequirement = ContextRequirementRevalidationRequired },
		"missing-abort":       func(b *Batch) { b.Events = append(b.Events[:len(b.Events)-2], b.Events[len(b.Events)-1]) },
		"missing-finish":      func(b *Batch) { b.Events = b.Events[:len(b.Events)-1] },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			h, input, _ := newMetadataClosureFixture(t)
			batch, err := Decide(h.state, metadataCloseCommand(h, input))
			if err != nil {
				t.Fatalf("valid metadata closure control rejected: %v", err)
			}
			if _, err := Evolve(h.state, batch); err != nil {
				t.Fatalf("valid metadata closure control did not evolve: %v", err)
			}
			mutate(&batch)
			if _, err := Evolve(h.state, batch); err == nil {
				t.Fatal("metadata close replay accepted altered preview, context, or terminal envelope")
			}
		})
	}
}

func TestExactClosureRejectsInactiveOwnerFieldsAtDecideAndReplay(t *testing.T) {
	t.Run("decide", func(t *testing.T) {
		h, input := newExactClosureFixture(t)
		input.Targets[0].OwnerEvidenceRef = "raw-payload-owner-evidence"
		input.Targets[0].RequestRef = "raw-payload-request"
		input.Targets[0].Generation = ^uint64(0)
		if _, err := Decide(h.state, closureCommandFor(h.state, "inactive-owner-fields", CommandAcceptUnknownOutcome, input)); err == nil {
			t.Fatal("exact closure accepted inactive owner fields")
		}
	})
	t.Run("replay", func(t *testing.T) {
		h, input := newExactClosureFixture(t)
		batch, err := Decide(h.state, closureCommandFor(h.state, "inactive-owner-replay", CommandAcceptUnknownOutcome, input))
		if err != nil {
			t.Fatalf("valid exact closure rejected: %v", err)
		}
		batch.Intent.Closure.Targets[0].OwnerEvidenceRef = "raw-payload-owner-evidence"
		batch.Intent.Closure.Targets[0].RequestRef = "raw-payload-request"
		batch.Intent.Closure.Targets[0].Generation = ^uint64(0)
		if _, err := Evolve(h.state, batch); err == nil {
			t.Fatal("exact closure replay accepted inactive owner fields")
		}
	})
}

func newUnknownModelMetadataClosureFixture(t *testing.T) (*transitionHarness, ClosureInput, int) {
	t.Helper()
	h := newTransitionHarness(t)
	invocationID, attemptID := h.modelInFlight()
	recovery := h.issue(Command{Kind: CommandAcquireOwnership, Transition: TransitionInput{
		ExecutorToken: 8, ExecutorIncarnation: "writer-inc-2", ReceiptRef: "model-recovery-receipt",
		Recoveries: []RecoveryFact{{TargetID: invocationID, AttemptID: attemptID, CauseRef: "model-unknown-cause", PartialRef: "partial-response", Usage: UsageUnknown, BlockerID: "model-recovery-blocker", PreviousToken: 7, PreviousIncarnation: "writer-inc-1", Fence: 1}},
	}})
	input := ClosureInput{
		PreviewRef: "metadata-model-preview", ReceiptRef: "metadata-model-receipt", PreviewVersion: CurrentClosurePreviewVersion,
		Revision: h.state.Revision, Actor: ActorUser, CauseEventRef: recovery.Events[2].ID, UnavailableReason: UnavailablePayloadMissing,
		PreviousContextRevision: h.state.ContextRevision, ContextRevision: h.state.ContextRevision + 1, ContextRequirement: ContextRequirementFreshRequired,
		Targets: []ClosureTargetFact{{Kind: ClosureTargetUnknownModel, TargetID: invocationID, AttemptID: attemptID, CauseRef: "model-unknown-cause", CauseEventRef: recovery.Events[2].ID}},
	}
	return h, input, 0
}

func newPlannedModelMetadataClosureFixture(t *testing.T) (*transitionHarness, ClosureInput, int) {
	t.Helper()
	h := newTransitionHarness(t)
	h.issue(Command{Kind: CommandAcceptRun, AcceptRun: AcceptRunInput{InputRef: "input-ref", Origin: OriginUser}})
	h.issue(Command{Kind: CommandAdmitRun, Admission: AdmissionInput{Token: 7, Incarnation: "writer-inc-1"}})
	h.issue(Command{Kind: CommandStartStep, Step: StepInput{StepID: "step-1"}})
	h.issue(Command{Kind: CommandPlanModel, Model: ModelInput{InvocationID: "planned-model", AttemptID: "planned-model-attempt", RequestRef: "planned-model-request"}})
	e := Event{ID: "planned-model-payload-cause-event", Kind: EventRunInterrupted, ProtocolVersion: CurrentProtocolVersion, RunID: h.state.RunID, SessionID: h.state.SessionID, ToolID: "context-target", CauseRef: "planned-model-payload-cause", Interrupt: InterruptPayloadUnavailable}
	next, err := Evolve(h.state, Batch{CommandID: "planned-model-payload-cause", ExpectedRevision: h.state.Revision, WriterToken: h.state.AdmissionToken, WriterIncarnation: h.state.Incarnation, ProtocolVersion: CurrentProtocolVersion, Events: []Event{e}})
	if err != nil {
		t.Fatalf("could not seed planned-model unavailable cause: %v", err)
	}
	h.state = next
	input := ClosureInput{
		PreviewRef: "planned-model-preview", ReceiptRef: "planned-model-receipt", PreviewVersion: CurrentClosurePreviewVersion,
		Revision: h.state.Revision, Actor: ActorUser, CauseEventRef: e.ID, UnavailableReason: UnavailablePayloadMissing,
		PreviousContextRevision: h.state.ContextRevision, ContextRevision: h.state.ContextRevision + 1, ContextRequirement: ContextRequirementFreshRequired,
		Targets: []ClosureTargetFact{
			{Kind: ClosureTargetUnavailableCause, TargetID: "context-target", CauseRef: "planned-model-payload-cause", CauseEventRef: e.ID},
			{Kind: ClosureTargetPlannedModel, TargetID: "planned-model", AttemptID: "planned-model-attempt"},
		},
	}
	return h, input, 1
}

func TestClosureTargetKindsRejectInactiveOwnerFields(t *testing.T) {
	builders := map[string]func(*testing.T) (*transitionHarness, ClosureInput, int){
		"unknown tool": func(t *testing.T) (*transitionHarness, ClosureInput, int) {
			h, in, _ := newMetadataClosureFixture(t)
			return h, in, 0
		},
		"unknown model": newUnknownModelMetadataClosureFixture,
		"unavailable cause": func(t *testing.T) (*transitionHarness, ClosureInput, int) {
			h, in, _ := newMetadataClosureFixture(t)
			return h, in, 1
		},
		"planned model": newPlannedModelMetadataClosureFixture,
	}
	mutations := map[string]func(*ClosureTargetFact){
		"owner evidence": func(f *ClosureTargetFact) { f.OwnerEvidenceRef = "inactive-owner-evidence" },
		"request ref":    func(f *ClosureTargetFact) { f.RequestRef = "inactive-request-ref" },
		"generation":     func(f *ClosureTargetFact) { f.Generation = 9 },
	}
	for kindName, build := range builders {
		for fieldName, mutate := range mutations {
			t.Run(kindName+"/"+fieldName, func(t *testing.T) {
				h, input, targetIndex := build(t)
				valid := metadataCloseCommand(h, input)
				positive, err := Decide(h.state, valid)
				if err != nil {
					t.Fatalf("valid %s closure control rejected: %v", kindName, err)
				}
				if _, err := Evolve(h.state, positive); err != nil {
					t.Fatalf("valid %s closure control did not evolve: %v", kindName, err)
				}

				changed := input
				changed.Targets = append([]ClosureTargetFact(nil), input.Targets...)
				mutate(&changed.Targets[targetIndex])
				cmd := metadataCloseCommand(h, changed)
				if !validID(cmd.ID) || !validID(changed.Targets[targetIndex].TargetID) {
					t.Fatalf("inactive-field test constructed invalid IDs for %s", kindName)
				}
				if _, err := Decide(h.state, cmd); err == nil {
					t.Fatalf("%s target accepted inactive %s at Decide", kindName, fieldName)
				}

				intentTampered := positive
				intentTampered.Intent.Closure.Targets[targetIndex] = changed.Targets[targetIndex]
				if _, err := Evolve(h.state, intentTampered); err == nil {
					t.Fatalf("%s target inactive %s leaked into Evolve command receipt", kindName, fieldName)
				}
				headerTampered := positive
				headerTampered.Events[0].ClosureTargets[targetIndex] = changed.Targets[targetIndex]
				if _, err := Evolve(h.state, headerTampered); err == nil {
					t.Fatalf("%s target inactive %s leaked into closure header", kindName, fieldName)
				}
			})
		}
	}
}

func TestMetadataClosureRejectsUnavailableDispositionForEffectCause(t *testing.T) {
	h, input, _ := newMetadataClosureFixture(t)
	input.Targets = []ClosureTargetFact{{Kind: ClosureTargetUnavailableCause, TargetID: "tool-1", CauseRef: "effect-unknown-cause", CauseEventRef: input.Targets[0].CauseEventRef}}
	if _, err := Decide(h.state, metadataCloseCommand(h, input)); err == nil {
		t.Fatal("metadata-only unavailable disposition accepted an effect-unknown cause")
	}
}

func TestMetadataClosureRejectsDuplicateCauseAcrossDispositionKinds(t *testing.T) {
	h, input, _ := newMetadataClosureFixture(t)
	input.Targets = append(input.Targets, ClosureTargetFact{Kind: ClosureTargetUnavailableCause, TargetID: "context-target", CauseRef: input.Targets[0].CauseRef, CauseEventRef: input.Targets[0].CauseEventRef})
	// Keep the manifest sorted so the rejection specifically covers the duplicate
	// cause trying to receive both accepted_unknown and closed_unavailable.
	input = canonicalClosure(input)
	if _, err := Decide(h.state, metadataCloseCommand(h, input)); err == nil {
		t.Fatal("metadata-only close assigned one persisted cause two incompatible dispositions")
	}
}

func TestMetadataClosureRequiresOwnerInvalidationForPreparedApproval(t *testing.T) {
	h := newTransitionHarness(t)
	toolID, attemptID, _ := h.toolAwaitingApproval()
	cause := ID("")
	for id, ev := range h.state.EventIDs {
		if ev.Kind == EventRunAccepted {
			cause = id
			break
		}
	}
	input := ClosureInput{PreviewRef: "metadata-preview", ReceiptRef: "user-close-receipt", PreviewVersion: CurrentClosurePreviewVersion,
		Revision: h.state.Revision, Actor: ActorUser, CauseEventRef: cause, UnavailableReason: UnavailablePayloadMissing,
		PreviousContextRevision: h.state.ContextRevision, ContextRevision: h.state.ContextRevision + 1, ContextRequirement: ContextRequirementFreshRequired,
		Targets: []ClosureTargetFact{{Kind: ClosureTargetPlannedTool, TargetID: toolID, AttemptID: attemptID, OwnerEvidenceRef: "owner-invalidation-receipt", RequestRef: h.state.Tools[toolID].RequestRef, Generation: h.state.Tools[toolID].CheckGeneration}}}
	batch := h.issue(closureCommandFor(h.state, "owner-invalidation-close", CommandConfirmRunClosure, input))
	var cancelled Event
	for _, ev := range batch.Events {
		if ev.Kind == EventToolResultRecorded {
			cancelled = ev
		}
	}
	if cancelled.OwnerEvidenceRef != "owner-invalidation-receipt" || cancelled.RequestRef != input.Targets[0].RequestRef || cancelled.Generation != input.Targets[0].Generation || h.state.Tools[toolID].Disposition != ResultCancelled || len(h.state.Blockers) != 0 {
		t.Fatal("prepared approval close omitted exact owner invalidation evidence or retained its blocker")
	}

	h = newTransitionHarness(t)
	toolID, attemptID, _ = h.toolAwaitingApproval()
	for id, ev := range h.state.EventIDs {
		if ev.Kind == EventRunAccepted {
			cause = id
			break
		}
	}
	input.Revision, input.PreviousContextRevision, input.ContextRevision = h.state.Revision, h.state.ContextRevision, h.state.ContextRevision+1
	input.Targets = []ClosureTargetFact{{Kind: ClosureTargetPlannedTool, TargetID: toolID, AttemptID: attemptID, OwnerEvidenceRef: "owner-invalidation-receipt", RequestRef: h.state.Tools[toolID].RequestRef, Generation: h.state.Tools[toolID].CheckGeneration + 1}}
	if _, err := Decide(h.state, closureCommandFor(h.state, "stale-owner-invalidation", CommandConfirmRunClosure, input)); err == nil {
		t.Fatal("prepared approval close accepted a stale check generation")
	}
}
