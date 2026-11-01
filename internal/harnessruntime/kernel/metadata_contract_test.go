package kernel

import "testing"

func TestClosureRequiresTypedMetadataPreviewContract(t *testing.T) {
	base := traceCRecoverySeed(t)
	validClosure := Event{
		ID: "closure", Kind: EventRunClosureConfirmed, RunID: base.RunID, SessionID: base.SessionID,
		Ref: "preview", ReceiptRef: "receipt", Actor: ActorUser, Revision: base.Revision,
		PreviewMode: ClosurePreviewMetadataOnly, PreviewVersion: 1, UnavailableReason: UnavailableVaultLocked,
		PreviousContextRevision: base.ContextRevision, ContextRevision: base.ContextRevision + 1,
		ContextRequirement: ContextRequirementRevalidationRequired, CauseEventRef: "interrupted",
		ClosureTargets: []ClosureTargetFact{{Kind: ClosureTargetUnknownTool, TargetID: "t1", AttemptID: "ta1", CauseRef: "tool-effect-unknown", CauseEventRef: "interrupted", ResultRef: "unknown-result"}},
	}
	validBatch := func(closure Event) Batch {
		intent := Command{ID: "metadata-closure-test", Kind: CommandConfirmRunClosure, RunID: base.RunID, SessionID: base.SessionID, ExpectedRevision: base.Revision, WriterToken: base.AdmissionToken, WriterIncarnation: base.Incarnation, Closure: ClosureInput{
			PreviewRef: closure.Ref, ReceiptRef: closure.ReceiptRef, PreviewVersion: closure.PreviewVersion, Revision: closure.Revision,
			Actor: closure.Actor, CauseEventRef: closure.CauseEventRef, UnavailableReason: closure.UnavailableReason,
			PreviousContextRevision: closure.PreviousContextRevision, ContextRevision: closure.ContextRevision, ContextRequirement: closure.ContextRequirement,
			Targets: append([]ClosureTargetFact(nil), closure.ClosureTargets...),
		}}
		return Batch{CommandID: "metadata-closure-test", ExpectedRevision: base.Revision, WriterToken: base.AdmissionToken, WriterIncarnation: base.Incarnation, ProtocolVersion: CurrentProtocolVersion, Intent: intent, Events: []Event{
			closure,
			{ID: "result", Kind: EventToolResultRecorded, RunID: base.RunID, SessionID: base.SessionID, ToolID: "t1", AttemptID: "ta1", CauseRef: "tool-effect-unknown", Ref: "unknown-result", ReceiptRef: "receipt", Disposition: ResultAbandonedUnknown, Exposure: ExposureUnknown, Token: base.AdmissionToken, Incarnation: base.Incarnation, Fence: 14},
			{ID: "clear", Kind: EventRunRecoveryCleared, RunID: base.RunID, SessionID: base.SessionID, ToolID: "t1", AttemptID: "ta1", CauseRef: "tool-effect-unknown", Ref: "receipt", ReceiptRef: "receipt", Actor: ActorUser, Accepted: true, RecoveryDisposition: RecoveryDispositionAcceptedUnknown},
			{ID: "abort", Kind: EventStepAborted, RunID: base.RunID, SessionID: base.SessionID, Ref: "step-abort"},
			{ID: "finish", Kind: EventRunFinished, RunID: base.RunID, SessionID: base.SessionID, Outcome: OutcomeAbandonedUnknown},
		}}
	}
	if _, err := applyBatch(t, base, validBatch(validClosure)); err != nil {
		t.Fatalf("positive metadata-only closure fixture is invalid: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*Event)
	}{
		{name: "missing preview version", mutate: func(e *Event) { e.PreviewVersion = 0 }},
		{name: "unspecified preview mode", mutate: func(e *Event) { e.PreviewMode = ClosurePreviewUnspecified }},
		{name: "unavailable context mismatch", mutate: func(e *Event) { e.ContextRequirement = ContextRequirementFreshRequired }},
		{name: "missing cause event", mutate: func(e *Event) { e.CauseEventRef = "" }},
		{name: "unexpected accepted flag", mutate: func(e *Event) { e.Accepted = true }},
		{name: "stale prior context revision", mutate: func(e *Event) { e.PreviousContextRevision, e.ContextRevision = 1, 2 }},
		{name: "context revision rollback", mutate: func(e *Event) { e.ContextRevision = e.PreviousContextRevision }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			closure := validClosure
			tc.mutate(&closure)
			_, err := applyBatch(t, base, validBatch(closure))
			if err == nil {
				t.Fatal("malformed metadata-only closure contract was accepted")
			}
		})
	}
}

func TestRecoveryClearRejectsUnknownActorAndRequiresTypedDisposition(t *testing.T) {
	base := EmptyState()
	base.Accepted, base.RunID, base.SessionID = true, "run", "session"
	base.Interruptions["interruption"] = Interruption{ID: "interruption", Kind: InterruptEffectUnknown, TargetID: "attempt"}
	cases := []struct {
		name  string
		actor ActorKind
		disp  RecoveryDisposition
	}{
		{name: "unknown actor", actor: ActorUnknown, disp: RecoveryDispositionReconciled},
		{name: "missing disposition", actor: ActorRecovery, disp: RecoveryDispositionUnspecified},
		{name: "accepted unknown requires user", actor: ActorRecovery, disp: RecoveryDispositionAcceptedUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := applyBatch(t, base, Batch{CommandID: "invalid-recovery-clear", Events: []Event{{
				ID: "clear", Kind: EventRunRecoveryCleared, RunID: "run", SessionID: "session",
				CauseRef: "interruption", Ref: "resolution", ReceiptRef: "receipt", Actor: tc.actor,
				RecoveryDisposition: tc.disp, Accepted: tc.disp == RecoveryDispositionAcceptedUnknown,
			}}})
			if err == nil {
				t.Fatal("recovery clear without valid actor/disposition was accepted")
			}
		})
	}
}

func TestMetadataClosureCauseReferenceSupportsQueuedAndKnownRunCauses(t *testing.T) {
	queued, err := applyBatch(t, EmptyState(), Batch{CommandID: "queued-accept", Events: []Event{{
		ID: "queued-accepted", Kind: EventRunAccepted, RunID: "queued", SessionID: "session", InputRef: "input", Origin: OriginUser,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	queuedClosure := Event{
		ID: "queued-close", Kind: EventRunClosureConfirmed, RunID: queued.RunID, SessionID: queued.SessionID,
		Ref: "preview", ReceiptRef: "receipt", Actor: ActorUser, Revision: queued.Revision,
		PreviewMode: ClosurePreviewMetadataOnly, PreviewVersion: 1, UnavailableReason: UnavailablePayloadMissing,
		ContextRequirement: ContextRequirementNone, CauseEventRef: "queued-accepted",
	}
	queuedInput := ClosureInput{PreviewRef: queuedClosure.Ref, ReceiptRef: queuedClosure.ReceiptRef, PreviewVersion: queuedClosure.PreviewVersion, Revision: queuedClosure.Revision, Actor: queuedClosure.Actor, CauseEventRef: queuedClosure.CauseEventRef, UnavailableReason: queuedClosure.UnavailableReason, ContextRequirement: queuedClosure.ContextRequirement}
	if _, err = applyBatch(t, queued, Batch{CommandID: "queued-close-command", ExpectedRevision: queued.Revision, ProtocolVersion: CurrentProtocolVersion, Intent: Command{ID: "queued-close-command", Kind: CommandConfirmRunClosure, RunID: queued.RunID, SessionID: queued.SessionID, ExpectedRevision: queued.Revision, Closure: queuedInput}, Events: []Event{
		queuedClosure,
		{ID: "queued-close-finish", Kind: EventRunFinished, RunID: queued.RunID, SessionID: queued.SessionID, Outcome: OutcomeAbandoned},
	}}); err != nil {
		t.Fatalf("queued metadata-only close should preserve session context requirement: %v", err)
	}

	admitted, err := applyBatch(t, EmptyState(), Batch{CommandID: "admit-known", Events: []Event{
		{ID: "known-accepted", Kind: EventRunAccepted, RunID: "known", SessionID: "session", InputRef: "input", Origin: OriginUser},
		{ID: "known-admitted", Kind: EventRunAdmitted, RunID: "known", SessionID: "session", Token: 1, Incarnation: "writer"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	knownClosure := Event{
		ID: "known-close", Kind: EventRunClosureConfirmed, RunID: admitted.RunID, SessionID: admitted.SessionID,
		Ref: "preview", ReceiptRef: "receipt", Actor: ActorUser, Revision: admitted.Revision,
		PreviewMode: ClosurePreviewMetadataOnly, PreviewVersion: 1, UnavailableReason: UnavailablePayloadCorrupt,
		PreviousContextRevision: admitted.ContextRevision, ContextRevision: admitted.ContextRevision + 1,
		ContextRequirement: ContextRequirementFreshRequired, CauseEventRef: "known-accepted",
	}
	knownInput := ClosureInput{PreviewRef: knownClosure.Ref, ReceiptRef: knownClosure.ReceiptRef, PreviewVersion: knownClosure.PreviewVersion, Revision: knownClosure.Revision, Actor: knownClosure.Actor, CauseEventRef: knownClosure.CauseEventRef, UnavailableReason: knownClosure.UnavailableReason, PreviousContextRevision: knownClosure.PreviousContextRevision, ContextRevision: knownClosure.ContextRevision, ContextRequirement: knownClosure.ContextRequirement}
	if _, err = applyBatch(t, admitted, Batch{CommandID: "known-close-command", ExpectedRevision: admitted.Revision, WriterToken: admitted.AdmissionToken, WriterIncarnation: admitted.Incarnation, ProtocolVersion: CurrentProtocolVersion, Intent: Command{ID: "known-close-command", Kind: CommandConfirmRunClosure, RunID: admitted.RunID, SessionID: admitted.SessionID, ExpectedRevision: admitted.Revision, WriterToken: admitted.AdmissionToken, WriterIncarnation: admitted.Incarnation, Closure: knownInput}, Events: []Event{
		knownClosure,
		{ID: "known-close-finish", Kind: EventRunFinished, RunID: admitted.RunID, SessionID: admitted.SessionID, Outcome: OutcomeAbandoned},
	}}); err != nil {
		t.Fatalf("admitted closure with an input cause but no interruption should pass: %v", err)
	}

	priorFresh := cloneState(admitted)
	priorFresh.ContextRevision, priorFresh.ContextRequirement = 7, ContextRequirementFreshRequired
	lockedClose := knownClosure
	lockedClose.ID, lockedClose.Revision = "locked-after-fresh", priorFresh.Revision
	lockedClose.PreviousContextRevision, lockedClose.ContextRevision = 7, 8
	lockedClose.UnavailableReason = UnavailableVaultLocked
	lockedClose.ContextRequirement = ContextRequirementFreshRequired
	lockedInput := ClosureInput{PreviewRef: lockedClose.Ref, ReceiptRef: lockedClose.ReceiptRef, PreviewVersion: lockedClose.PreviewVersion, Revision: lockedClose.Revision, Actor: lockedClose.Actor, CauseEventRef: lockedClose.CauseEventRef, UnavailableReason: lockedClose.UnavailableReason, PreviousContextRevision: lockedClose.PreviousContextRevision, ContextRevision: lockedClose.ContextRevision, ContextRequirement: lockedClose.ContextRequirement}
	lockedIntent := Command{ID: "locked-after-fresh-command", Kind: CommandConfirmRunClosure, RunID: priorFresh.RunID, SessionID: priorFresh.SessionID, ExpectedRevision: priorFresh.Revision, WriterToken: priorFresh.AdmissionToken, WriterIncarnation: priorFresh.Incarnation, Closure: lockedInput}
	if _, err = applyBatch(t, priorFresh, Batch{CommandID: "locked-after-fresh-command", ExpectedRevision: priorFresh.Revision, WriterToken: priorFresh.AdmissionToken, WriterIncarnation: priorFresh.Incarnation, ProtocolVersion: CurrentProtocolVersion, Intent: lockedIntent, Events: []Event{
		lockedClose,
		{ID: "locked-after-fresh-finish", Kind: EventRunFinished, RunID: priorFresh.RunID, SessionID: priorFresh.SessionID, Outcome: OutcomeAbandoned},
	}}); err != nil {
		t.Fatalf("vault-locked close must preserve a prior fresh-required context: %v", err)
	}
	lockedClose.ContextRequirement = ContextRequirementRevalidationRequired
	lockedIntent.ID, lockedIntent.Closure.ContextRequirement = "invalid-downgrade-command", lockedClose.ContextRequirement
	if _, err = applyBatch(t, priorFresh, Batch{CommandID: "invalid-downgrade-command", ExpectedRevision: priorFresh.Revision, WriterToken: priorFresh.AdmissionToken, WriterIncarnation: priorFresh.Incarnation, ProtocolVersion: CurrentProtocolVersion, Intent: lockedIntent, Events: []Event{
		lockedClose,
		{ID: "invalid-downgrade-finish", Kind: EventRunFinished, RunID: priorFresh.RunID, SessionID: priorFresh.SessionID, Outcome: OutcomeAbandoned},
	}}); err == nil {
		t.Fatal("vault-locked close downgraded an existing fresh-required context")
	}
}
