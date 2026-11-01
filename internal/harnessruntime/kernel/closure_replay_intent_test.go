package kernel

import "testing"

func TestMetadataClosureAcceptsNilAndEmptyTargetManifests(t *testing.T) {
	for _, tc := range []struct {
		name    string
		targets []ClosureTargetFact
	}{
		{name: "nil"},
		{name: "non-nil empty", targets: make([]ClosureTargetFact, 0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, err := applyBatch(t, EmptyState(), Batch{CommandID: "accept-known-run", Events: []Event{{
				ID: "known-run-event", Kind: EventRunAccepted, RunID: "known-run", SessionID: "session", InputRef: "input-ref", Origin: OriginUser,
			}}})
			if err != nil {
				t.Fatalf("could not create known run: %v", err)
			}
			input := ClosureInput{PreviewRef: "metadata-preview", ReceiptRef: "metadata-receipt", PreviewVersion: CurrentClosurePreviewVersion,
				Revision: state.Revision, Actor: ActorUser, CauseEventRef: "known-run-event", UnavailableReason: UnavailablePayloadMissing,
				ContextRequirement: ContextRequirementNone, Targets: tc.targets}
			cmd := closureCommandFor(state, "empty-manifest-close", CommandConfirmRunClosure, input)
			batch, err := Decide(state, cmd)
			if err != nil {
				t.Fatalf("metadata close rejected empty manifest: %v", err)
			}
			if (batch.Events[0].ClosureTargets == nil) != (tc.targets == nil) {
				t.Fatal("metadata closure header changed the nil/empty target representation")
			}
			next, err := Evolve(state, batch)
			if err != nil {
				t.Fatalf("metadata close failed to evolve: %v", err)
			}
			if !next.Finished || next.Outcome != OutcomeAbandoned || (next.ClosureTargets == nil) != (tc.targets == nil) {
				t.Fatal("empty metadata close did not finish or preserve target representation in state")
			}
		})
	}
}

func TestMetadataClosureReplayRejectsInactiveCommandUnionBranch(t *testing.T) {
	h, input, _ := newMetadataClosureFixture(t)
	batch, err := Decide(h.state, metadataCloseCommand(h, input))
	if err != nil {
		t.Fatalf("valid metadata close rejected: %v", err)
	}
	batch.Intent.Step.StepID = "raw-shaped-inactive-step"
	if _, err := Evolve(h.state, batch); err == nil {
		t.Fatal("metadata closure replay retained an inactive outer command branch")
	}
}
