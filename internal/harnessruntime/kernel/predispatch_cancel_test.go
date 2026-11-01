package kernel

import "testing"

func TestDecideCancelsToolBeforeDispatchFromEveryPreparationState(t *testing.T) {
	for _, stage := range []string{"planned", "checking", "awaiting approval", "ready"} {
		t.Run(stage, func(t *testing.T) {
			h := newTransitionHarness(t)
			h.modelInFlight()
			h.issue(Command{Kind: CommandObserveModelResponse, ModelResponse: ModelResponseInput{
				InvocationID: "model-1", AttemptID: "model-attempt-1", ResponseRef: "model-response-ref",
				Usage: UsageUnknown, Candidates: []ToolCandidate{{ID: "tool-1", AttemptID: "tool-attempt-1", ArgsRef: "raw-args-ref", Descriptor: "descriptor-ref", SourceIndex: 0}},
				Fence: 1, ExecutorToken: 7, ExecutorIncarnation: "writer-inc-1",
			}})
			if stage != "planned" {
				h.issue(Command{Kind: CommandPrepareTool, Tool: ToolInput{ToolID: "tool-1", Generation: 1, Ref: "tool-check"}})
			}
			input := TransitionInput{TargetID: "tool-1", AttemptID: "tool-attempt-1", Ref: "user-cancel-proof", ReceiptRef: "cancel-receipt", Terminal: true}
			if stage == "awaiting approval" {
				h.issue(Command{Kind: CommandRequestToolApproval, Transition: TransitionInput{TargetID: "tool-1", Generation: 1, Ref: "raw-args-ref", RequestRef: "approval-request", BlockerID: "approval-blocker"}})
				input.Generation, input.BlockerID, input.RequestRef = 2, "approval-blocker", "approval-request"
			}
			if stage == "ready" {
				h.issue(Command{Kind: CommandReadyTool, Tool: ToolInput{ToolID: "tool-1", Generation: 1, ArgsRef: "raw-args-ref"}})
			}
			batch := h.issue(Command{Kind: CommandCancelTool, Transition: input})
			wantEvents := 1
			if stage == "awaiting approval" {
				wantEvents = 2
				if batch.Events[0].Kind != EventToolCheckReset || batch.Events[0].BlockerID != "approval-blocker" || batch.Events[0].RequestRef != "approval-request" {
					t.Fatal("approval cancellation did not invalidate the exact outstanding request")
				}
			}
			if len(batch.Events) != wantEvents || batch.Events[len(batch.Events)-1].Kind != EventToolResultRecorded || batch.Events[len(batch.Events)-1].Disposition != ResultCancelled {
				t.Fatalf("pre-dispatch cancellation was not recorded as a cancellation result: %#v", batch.Events)
			}
			tool, attempt := h.state.Tools["tool-1"], h.state.ToolAttempts["tool-attempt-1"]
			if tool.State != ToolCancelled || tool.Disposition != ResultCancelled || attempt.State != AttemptCancelled || tool.ResultRef != "user-cancel-proof" || len(h.state.Blockers) != 0 {
				t.Fatal("pre-dispatch cancellation did not close the tool and retain its cancellation evidence")
			}
			step := h.state.Steps[h.state.CurrentStep]
			if step.ResponseRef != "model-response-ref" || len(step.ToolIDs) != 1 || step.ToolIDs[0] != "tool-1" || len(step.Transcript) != 2 || step.Transcript[0].ResponseRef != "model-response-ref" || step.Transcript[1].ResultRef != "user-cancel-proof" {
				t.Fatal("cancellation changed the canonical model response or source order")
			}
		})
	}
}
