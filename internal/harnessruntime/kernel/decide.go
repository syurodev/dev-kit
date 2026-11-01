package kernel

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strconv"
)

const CurrentProtocolVersion uint32 = 1

// Decide turns one closed logical intent plus any typed trusted observations
// into the event batch to persist. It has no event-slice input and cannot run
// effects; Evolve is the separate replay-only reducer.
func Decide(state State, cmd Command) (Batch, error) {
	if !validID(cmd.ID) || !validID(cmd.RunID) || !validID(cmd.SessionID) {
		return Batch{}, reject(RejectInvalid)
	}
	canonical, err := canonicalCommand(cmd)
	if err != nil || !reflect.DeepEqual(canonical, cmd) {
		return Batch{}, reject(RejectInvalid)
	}
	if prior, ok := state.CommandBatches[cmd.ID]; ok {
		if reflect.DeepEqual(prior.Intent, cmd) {
			return cloneBatch(prior), nil
		}
		return Batch{}, reject(RejectConflict)
	}
	events, err := commandEvents(state, cmd)
	if err != nil || len(events) == 0 {
		return Batch{}, reject(RejectInvalid)
	}
	batch := Batch{CommandID: cmd.ID, ExpectedRevision: cmd.ExpectedRevision, WriterToken: cmd.WriterToken, WriterIncarnation: cmd.WriterIncarnation, ProtocolVersion: CurrentProtocolVersion, Intent: canonical, Events: events}
	if cmd.Kind == CommandAcquireOwnership {
		batch.WriterToken, batch.WriterIncarnation = cmd.Transition.ExecutorToken, cmd.Transition.ExecutorIncarnation
		batch.Intent = cmd
	}
	if state.Revision != cmd.ExpectedRevision {
		return Batch{}, reject(RejectStale)
	}
	if err := validateWriter(state, batch); err != nil {
		return Batch{}, err
	}
	if _, err := Evolve(state, batch); err != nil {
		return Batch{}, err
	}
	return batch, nil
}

// canonicalCommand rejects inactive union branches so unused fields cannot be
// smuggled into durable command receipts. Only model response candidates carry
// a slice; copy it before retaining the intent.
func canonicalCommand(c Command) (Command, error) {
	n := Command{ID: c.ID, Kind: c.Kind, ExpectedRevision: c.ExpectedRevision, WriterToken: c.WriterToken, WriterIncarnation: c.WriterIncarnation, RunID: c.RunID, SessionID: c.SessionID}
	switch c.Kind {
	case CommandAcceptRun:
		n.AcceptRun = c.AcceptRun
	case CommandAdmitRun:
		n.Admission = c.Admission
	case CommandStartStep:
		n.Step = c.Step
	case CommandPlanModel:
		n.Model.InvocationID, n.Model.AttemptID, n.Model.RequestRef = c.Model.InvocationID, c.Model.AttemptID, c.Model.RequestRef
	case CommandDispatchModel:
		n.Model.InvocationID, n.Model.AttemptID, n.Model.RequestRef, n.Model.Fence, n.Model.ExecutorToken, n.Model.ExecutorIncarnation = c.Model.InvocationID, c.Model.AttemptID, c.Model.RequestRef, c.Model.Fence, c.Model.ExecutorToken, c.Model.ExecutorIncarnation
	case CommandObserveModelStream:
		n.Model.InvocationID, n.Model.AttemptID, n.Model.Fence, n.Model.ExecutorToken, n.Model.ExecutorIncarnation = c.Model.InvocationID, c.Model.AttemptID, c.Model.Fence, c.Model.ExecutorToken, c.Model.ExecutorIncarnation
	case CommandObserveModelResponse:
		n.ModelResponse = c.ModelResponse
		if c.ModelResponse.Candidates != nil {
			n.ModelResponse.Candidates = append(make([]ToolCandidate, 0, len(c.ModelResponse.Candidates)), c.ModelResponse.Candidates...)
		}
	case CommandPrepareTool:
		n.Tool.ToolID, n.Tool.Generation, n.Tool.Ref = c.Tool.ToolID, c.Tool.Generation, c.Tool.Ref
	case CommandReadyTool:
		n.Tool.ToolID, n.Tool.Generation, n.Tool.ArgsRef, n.Tool.ApprovalRef = c.Tool.ToolID, c.Tool.Generation, c.Tool.ArgsRef, c.Tool.ApprovalRef
	case CommandDispatchTool:
		n.Tool.ToolID, n.Tool.Generation, n.Tool.Ref, n.Tool.ReceiptRef, n.Tool.AttemptID, n.Tool.Fence, n.Tool.ExecutorToken, n.Tool.ExecutorIncarnation = c.Tool.ToolID, c.Tool.Generation, c.Tool.Ref, c.Tool.ReceiptRef, c.Tool.AttemptID, c.Tool.Fence, c.Tool.ExecutorToken, c.Tool.ExecutorIncarnation
	case CommandObserveToolResult:
		n.ToolResult = c.ToolResult
	case CommandFinishStep:
		n.FinishStep = c.FinishStep
	case CommandFinishRun:
		n.FinishRun = c.FinishRun
	case CommandRequestStop:
		n.Stop.Reason, n.Stop.Mode, n.Stop.Ref, n.Stop.Actor = c.Stop.Reason, c.Stop.Mode, c.Stop.Ref, c.Stop.Actor
		if c.Stop.Mode == StopSuspend {
			n.Stop.BlockerID, n.Stop.Quiescing = c.Stop.BlockerID, c.Stop.Quiescing
		}
		if c.Stop.Reason == StopBudget {
			n.Stop.BudgetKind = c.Stop.BudgetKind
		}
	case CommandExtendBudget:
		n.BudgetExtension = c.BudgetExtension
	case CommandAbortStep, CommandObserveModelFailure, CommandCancelModel, CommandAbandonModel, CommandRequestToolApproval, CommandResolveToolApproval, CommandResetToolCheck, CommandCancelTool, CommandObserveUnknownTool, CommandAbandonTool, CommandReconcileTool, CommandPlanToolRetry, CommandScheduleTimer, CommandObserveTimerElapsed, CommandAcquireOwnership, CommandConfirmRunClosure, CommandAcceptUnknownOutcome, CommandAcceptUnknownModelOutcome, CommandReconcileModelUsage, CommandOpenTimeWindow, CommandCloseTimeWindow, CommandObserveBudget, CommandClearRunBlock, CommandClearRecovery, CommandScheduleModelRetry, CommandPlanModelRetry, CommandFailModelInvocation, CommandExpireTimeWindow:
		var err error
		n.Transition, err = canonicalTransition(c.Kind, c.Transition)
		if err != nil {
			return Command{}, err
		}
	default:
		return Command{}, reject(RejectInvalid)
	}
	return n, nil
}

func canonicalTransition(k CommandKind, x TransitionInput) (TransitionInput, error) {
	n := TransitionInput{}
	switch k {
	case CommandAbortStep:
		n.Ref = x.Ref
	case CommandObserveModelFailure:
		n.TargetID, n.AttemptID, n.Ref, n.CauseRef, n.ReceiptRef, n.Fence, n.ExecutorToken, n.ExecutorIncarnation, n.Usage, n.Terminal = x.TargetID, x.AttemptID, x.Ref, x.CauseRef, x.ReceiptRef, x.Fence, x.ExecutorToken, x.ExecutorIncarnation, x.Usage, x.Terminal
	case CommandCancelModel:
		n.TargetID, n.AttemptID, n.Ref, n.Fence, n.ExecutorToken, n.ExecutorIncarnation, n.Terminal = x.TargetID, x.AttemptID, x.Ref, x.Fence, x.ExecutorToken, x.ExecutorIncarnation, x.Terminal
	case CommandCancelTool:
		n.TargetID, n.AttemptID, n.Ref, n.ReceiptRef, n.BlockerID, n.RequestRef, n.Generation, n.Fence, n.ExecutorToken, n.ExecutorIncarnation, n.Terminal = x.TargetID, x.AttemptID, x.Ref, x.ReceiptRef, x.BlockerID, x.RequestRef, x.Generation, x.Fence, x.ExecutorToken, x.ExecutorIncarnation, x.Terminal
	case CommandAbandonModel:
		n.TargetID, n.AttemptID, n.Ref, n.ReceiptRef, n.Exposure, n.Next = x.TargetID, x.AttemptID, x.Ref, x.ReceiptRef, x.Exposure, x.Next
	case CommandRequestToolApproval:
		n.TargetID, n.Generation, n.Ref, n.RequestRef, n.BlockerID = x.TargetID, x.Generation, x.Ref, x.RequestRef, x.BlockerID
	case CommandResolveToolApproval:
		n.TargetID, n.AttemptID, n.Generation, n.Ref, n.RequestRef, n.ReceiptRef, n.ResultRef, n.Disposition = x.TargetID, x.AttemptID, x.Generation, x.Ref, x.RequestRef, x.ReceiptRef, x.ResultRef, x.Disposition
	case CommandResetToolCheck:
		n.TargetID, n.Generation, n.Ref, n.BlockerID, n.RequestRef, n.ReceiptRef = x.TargetID, x.Generation, x.Ref, x.BlockerID, x.RequestRef, x.ReceiptRef
	case CommandObserveUnknownTool:
		n.TargetID, n.AttemptID, n.CauseRef, n.Fence, n.ExecutorToken, n.ExecutorIncarnation, n.Exposure = x.TargetID, x.AttemptID, x.CauseRef, x.Fence, x.ExecutorToken, x.ExecutorIncarnation, x.Exposure
	case CommandAbandonTool:
		n.TargetID, n.AttemptID, n.Ref, n.ReceiptRef, n.Exposure, n.Next = x.TargetID, x.AttemptID, x.Ref, x.ReceiptRef, x.Exposure, x.Next
		if x.Next == NextEnd {
			n.CauseRef, n.ResultRef, n.Actor = x.CauseRef, x.ResultRef, x.Actor
		}
	case CommandReconcileTool:
		n.TargetID, n.AttemptID, n.ReceiptRef, n.ResultRef, n.Disposition, n.Exposure = x.TargetID, x.AttemptID, x.ReceiptRef, x.ResultRef, x.Disposition, x.Exposure
	case CommandPlanToolRetry:
		n.TargetID, n.PreviousAttemptID, n.AttemptID, n.ReceiptRef, n.TimerID, n.Generation = x.TargetID, x.PreviousAttemptID, x.AttemptID, x.ReceiptRef, x.TimerID, x.Generation
	case CommandScheduleModelRetry:
		n.TargetID, n.PreviousAttemptID, n.TimerID, n.BlockerID, n.Ref, n.Generation = x.TargetID, x.PreviousAttemptID, x.TimerID, x.BlockerID, x.Ref, x.Generation
	case CommandPlanModelRetry:
		n.TargetID, n.PreviousAttemptID, n.AttemptID, n.TimerID, n.Generation = x.TargetID, x.PreviousAttemptID, x.AttemptID, x.TimerID, x.Generation
	case CommandFailModelInvocation:
		n.TargetID, n.Ref = x.TargetID, x.Ref
	case CommandScheduleTimer:
		n.TargetID, n.RelatedID, n.TimerID, n.BlockerID, n.Ref, n.Purpose, n.Generation = x.TargetID, x.RelatedID, x.TimerID, x.BlockerID, x.Ref, x.Purpose, x.Generation
	case CommandObserveTimerElapsed:
		n.TimerID, n.Generation, n.Ref = x.TimerID, x.Generation, x.Ref
	case CommandAcquireOwnership:
		n.ExecutorToken, n.ExecutorIncarnation, n.ReceiptRef = x.ExecutorToken, x.ExecutorIncarnation, x.ReceiptRef
		if x.Recoveries != nil {
			n.Recoveries = make([]RecoveryFact, 0, len(x.Recoveries))
			for _, fact := range x.Recoveries {
				f := RecoveryFact{TargetID: fact.TargetID, AttemptID: fact.AttemptID, Tool: fact.Tool, CauseRef: fact.CauseRef, BlockerID: fact.BlockerID, PreviousToken: fact.PreviousToken, PreviousIncarnation: fact.PreviousIncarnation, Fence: fact.Fence}
				if fact.Tool {
					f.Interrupt, f.Exposure = fact.Interrupt, fact.Exposure
				} else {
					f.PartialRef, f.Usage = fact.PartialRef, fact.Usage
				}
				n.Recoveries = append(n.Recoveries, f)
			}
		}
	case CommandConfirmRunClosure:
		n.Ref, n.CauseRef, n.ReceiptRef, n.Actor, n.Revision, n.Outcome, n.Truncated = x.Ref, x.CauseRef, x.ReceiptRef, x.Actor, x.Revision, x.Outcome, x.Truncated
		n.PreviewMode, n.PreviewVersion, n.UnavailableReason = x.PreviewMode, x.PreviewVersion, x.UnavailableReason
		n.PreviousContextRevision, n.ContextRevision, n.ContextRequirement, n.CauseEventRef = x.PreviousContextRevision, x.ContextRevision, x.ContextRequirement, x.CauseEventRef
		n.RecoveryDisposition = x.RecoveryDisposition
		if x.UnknownOutcomes != nil {
			n.UnknownOutcomes = append(make([]UnknownOutcomeFact, 0, len(x.UnknownOutcomes)), x.UnknownOutcomes...)
		}
	case CommandAcceptUnknownOutcome:
		n.TargetID, n.AttemptID, n.Ref, n.CauseRef, n.ReceiptRef, n.Actor, n.Disposition = x.TargetID, x.AttemptID, x.Ref, x.CauseRef, x.ReceiptRef, x.Actor, x.Disposition
		n.ResultRef = x.ResultRef
	case CommandAcceptUnknownModelOutcome:
		n.TargetID, n.AttemptID, n.Ref, n.CauseRef, n.ReceiptRef, n.Actor, n.Disposition = x.TargetID, x.AttemptID, x.Ref, x.CauseRef, x.ReceiptRef, x.Actor, x.Disposition
	case CommandReconcileModelUsage, CommandObserveBudget:
		n.TargetID, n.AttemptID, n.Usage, n.Ref = x.TargetID, x.AttemptID, x.Usage, x.Ref
	case CommandOpenTimeWindow:
		n.WindowID, n.TimerID, n.Ref, n.Generation, n.Usage, n.ExecutorIncarnation = x.WindowID, x.TimerID, x.Ref, x.Generation, x.Usage, x.ExecutorIncarnation
	case CommandCloseTimeWindow:
		n.WindowID, n.TimerID, n.Ref, n.Covered, n.Generation = x.WindowID, x.TimerID, x.Ref, x.Covered, x.Generation
	case CommandExpireTimeWindow:
		n.WindowID, n.TimerID, n.Ref, n.CauseRef, n.Generation, n.Usage, n.BudgetKind, n.Actor, n.BudgetExhausted = x.WindowID, x.TimerID, x.Ref, x.CauseRef, x.Generation, x.Usage, x.BudgetKind, x.Actor, x.BudgetExhausted
	case CommandClearRunBlock:
		n.BlockerID, n.Ref, n.CauseRef, n.Generation, n.Revision = x.BlockerID, x.Ref, x.CauseRef, x.Generation, x.Revision
	case CommandClearRecovery:
		n.Ref, n.ReceiptRef, n.Actor, n.CauseRef, n.RecoveryDisposition = x.Ref, x.ReceiptRef, x.Actor, x.CauseRef, x.RecoveryDisposition
	default:
		return TransitionInput{}, reject(RejectInvalid)
	}
	return n, nil
}

func commandEvents(state State, c Command) ([]Event, error) {
	newEvent := func(n uint32, kind EventKind) Event {
		seed := []byte(string(c.ID) + ":" + strconv.FormatUint(uint64(n), 10))
		sum := sha256.Sum256(seed)
		return Event{ID: ID("e" + hex.EncodeToString(sum[:12])), Kind: kind, ProtocolVersion: CurrentProtocolVersion, RunID: c.RunID, SessionID: c.SessionID}
	}
	var out []Event
	switch c.Kind {
	case CommandAcceptRun:
		e := newEvent(1, EventRunAccepted)
		e.InputRef, e.Origin, e.GoalID, e.GoalRound, e.InitialStepLimit = c.AcceptRun.InputRef, c.AcceptRun.Origin, c.AcceptRun.GoalID, c.AcceptRun.GoalRound, c.AcceptRun.InitialStepLimit
		out = append(out, e)
	case CommandAdmitRun:
		e := newEvent(1, EventRunAdmitted)
		e.Token, e.Incarnation = c.Admission.Token, c.Admission.Incarnation
		out = append(out, e)
	case CommandStartStep:
		e := newEvent(1, EventStepStarted)
		e.StepID, e.Generation = c.Step.StepID, uint64(len(state.Steps)+1)
		out = append(out, e)
	case CommandPlanModel:
		e := newEvent(1, EventModelRequestPlanned)
		e.InvocationID, e.AttemptID, e.Ref = c.Model.InvocationID, c.Model.AttemptID, c.Model.RequestRef
		out = append(out, e)
	case CommandDispatchModel:
		e := newEvent(1, EventModelDispatchStarted)
		e.InvocationID, e.AttemptID, e.Ref = c.Model.InvocationID, c.Model.AttemptID, c.Model.RequestRef
		e.Token, e.Incarnation, e.Fence = c.Model.ExecutorToken, c.Model.ExecutorIncarnation, c.Model.Fence
		out = append(out, e)
	case CommandObserveModelStream:
		e := newEvent(1, EventModelStreamStarted)
		e.InvocationID, e.AttemptID = c.Model.InvocationID, c.Model.AttemptID
		e.Token, e.Incarnation, e.Fence = c.Model.ExecutorToken, c.Model.ExecutorIncarnation, c.Model.Fence
		out = append(out, e)
	case CommandObserveModelResponse:
		r := c.ModelResponse
		e := newEvent(1, EventModelResponseRecorded)
		e.InvocationID, e.AttemptID, e.Ref, e.Usage, e.CauseRef = r.InvocationID, r.AttemptID, r.ResponseRef, r.Usage, r.UsageRef
		e.Token, e.Incarnation, e.Fence = r.ExecutorToken, r.ExecutorIncarnation, r.Fence
		e.Candidates = append([]ToolCandidate(nil), r.Candidates...)
		out = append(out, e)
		n := uint32(2)
		if r.UsageRef != "" {
			budget := newEvent(n, EventBudgetObserved)
			budget.InvocationID, budget.AttemptID, budget.Usage, budget.Ref = r.InvocationID, r.AttemptID, r.Usage, r.UsageRef
			out = append(out, budget)
			n++
		}
		for _, candidate := range r.Candidates {
			planned := newEvent(n, EventToolCallPlanned)
			planned.InvocationID, planned.ToolID, planned.AttemptID = r.InvocationID, candidate.ID, candidate.AttemptID
			planned.Ref, planned.CauseRef, planned.SourceIndex = candidate.Descriptor, candidate.ArgsRef, candidate.SourceIndex
			out = append(out, planned)
			n++
		}
	case CommandPrepareTool:
		e := newEvent(1, EventToolCheckStarted)
		e.ToolID, e.Generation, e.Ref = c.Tool.ToolID, c.Tool.Generation, c.Tool.Ref
		out = append(out, e)
	case CommandReadyTool:
		e := newEvent(1, EventToolReadyRecorded)
		e.ToolID, e.Generation, e.Ref, e.ReceiptRef = c.Tool.ToolID, c.Tool.Generation, c.Tool.ArgsRef, c.Tool.ApprovalRef
		out = append(out, e)
	case CommandDispatchTool:
		e := newEvent(1, EventToolDispatchStarted)
		e.ToolID, e.AttemptID, e.ReceiptRef, e.Ref = c.Tool.ToolID, c.Tool.AttemptID, c.Tool.ReceiptRef, c.Tool.Ref
		e.Generation = c.Tool.Generation
		e.Token, e.Incarnation, e.Fence = c.Tool.ExecutorToken, c.Tool.ExecutorIncarnation, c.Tool.Fence
		out = append(out, e)
	case CommandObserveToolResult:
		r := c.ToolResult
		e := newEvent(1, EventToolResultRecorded)
		e.ToolID, e.AttemptID, e.Ref, e.Disposition, e.Exposure = r.ToolID, r.AttemptID, r.ResultRef, r.Disposition, r.Exposure
		e.Token, e.Incarnation, e.Fence = r.ExecutorToken, r.ExecutorIncarnation, r.Fence
		out = append(out, e)
	case CommandFinishStep:
		e := newEvent(1, EventStepCompleted)
		e.Next, e.Continuation = c.FinishStep.Next, c.FinishStep.Continuation
		out = append(out, e)
	case CommandFinishRun:
		e := newEvent(1, EventRunFinished)
		e.Outcome, e.Truncated = c.FinishRun.Outcome, c.FinishRun.Truncated
		out = append(out, e)
	case CommandRequestStop:
		e := newEvent(1, EventRunStopRequested)
		e.StopReason, e.StopMode, e.Ref, e.BlockerID, e.Terminal, e.Actor, e.BudgetKind = c.Stop.Reason, c.Stop.Mode, c.Stop.Ref, c.Stop.BlockerID, c.Stop.Quiescing, c.Stop.Actor, c.Stop.BudgetKind
		out = append(out, e)
	case CommandExtendBudget:
		e := newEvent(1, EventBudgetExtended)
		e.BudgetKind, e.BudgetBy, e.Actor = c.BudgetExtension.Kind, c.BudgetExtension.By, c.BudgetExtension.Actor
		out = append(out, e)
	case CommandAbortStep:
		e := newEvent(1, EventStepAborted)
		e.Ref = c.Transition.Ref
		out = append(out, e)
	case CommandObserveModelFailure:
		x := c.Transition
		e := newEvent(1, EventModelRequestFailed)
		e.InvocationID, e.AttemptID, e.Ref, e.Usage, e.CauseRef, e.ReceiptRef = x.TargetID, x.AttemptID, x.Ref, x.Usage, x.CauseRef, x.ReceiptRef
		e.Token, e.Incarnation, e.Fence, e.Terminal = x.ExecutorToken, x.ExecutorIncarnation, x.Fence, x.Terminal
		out = append(out, e)
	case CommandCancelModel:
		x := c.Transition
		e := newEvent(1, EventModelAttemptCancelled)
		e.InvocationID, e.AttemptID, e.Ref, e.Terminal = x.TargetID, x.AttemptID, x.Ref, x.Terminal
		e.Token, e.Incarnation, e.Fence = x.ExecutorToken, x.ExecutorIncarnation, x.Fence
		out = append(out, e)
	case CommandAbandonModel:
		x := c.Transition
		e := newEvent(1, EventModelAttemptAbandoned)
		e.InvocationID, e.AttemptID, e.Ref, e.Exposure, e.Next, e.ReceiptRef = x.TargetID, x.AttemptID, x.Ref, x.Exposure, x.Next, x.ReceiptRef
		out = append(out, e)
	case CommandRequestToolApproval:
		x := c.Transition
		e := newEvent(1, EventToolApprovalRequested)
		e.ToolID, e.Generation, e.CauseRef, e.RequestRef, e.BlockerID = x.TargetID, x.Generation, x.Ref, x.RequestRef, x.BlockerID
		out = append(out, e)
	case CommandResolveToolApproval:
		x := c.Transition
		a := newEvent(1, EventToolApprovalResolved)
		a.ToolID, a.Generation, a.RequestRef, a.CauseRef, a.ReceiptRef, a.Disposition = x.TargetID, x.Generation, x.RequestRef, x.Ref, x.ReceiptRef, x.Disposition
		out = append(out, a)
		if x.Disposition == ResultSucceeded {
			e := newEvent(2, EventToolReadyRecorded)
			e.ToolID, e.Generation, e.Ref, e.ReceiptRef = x.TargetID, x.Generation, x.Ref, x.ReceiptRef
			out = append(out, e)
		} else {
			e := newEvent(2, EventToolResultRecorded)
			e.ToolID, e.AttemptID, e.Generation, e.Ref, e.ReceiptRef, e.Disposition = x.TargetID, x.AttemptID, x.Generation, x.ResultRef, x.ReceiptRef, x.Disposition
			out = append(out, e)
		}
	case CommandResetToolCheck:
		x := c.Transition
		e := newEvent(1, EventToolCheckReset)
		e.ToolID, e.Generation, e.CauseRef, e.BlockerID, e.RequestRef, e.ReceiptRef = x.TargetID, x.Generation, x.Ref, x.BlockerID, x.RequestRef, x.ReceiptRef
		out = append(out, e)
	case CommandCancelTool:
		x := c.Transition
		tool, toolOK := state.Tools[x.TargetID]
		attempt, attemptOK := state.ToolAttempts[x.AttemptID]
		if toolOK && attemptOK && attempt.State == AttemptPlanned && (tool.State == ToolPlanned || tool.State == ToolChecking || tool.State == ToolReady || tool.State == ToolAwaitingApproval) {
			var out []Event
			if tool.State == ToolAwaitingApproval {
				blocker, found := blockerForTool(state, tool.ID, BlockApproval)
				if !found || x.BlockerID != blocker.ID || x.RequestRef != tool.RequestRef || x.Generation <= tool.CheckGeneration || !validID(x.ReceiptRef) {
					return nil, reject(RejectStale)
				}
				reset := newEvent(1, EventToolCheckReset)
				reset.ToolID, reset.Generation, reset.CauseRef, reset.BlockerID, reset.RequestRef, reset.ReceiptRef = tool.ID, x.Generation, x.Ref, x.BlockerID, x.RequestRef, x.ReceiptRef
				out = append(out, reset)
			}
			cancelled := newEvent(uint32(len(out)+1), EventToolResultRecorded)
			cancelled.ToolID, cancelled.AttemptID, cancelled.Generation, cancelled.Ref, cancelled.ReceiptRef = tool.ID, attempt.ID, x.Generation, x.Ref, x.ReceiptRef
			cancelled.Disposition, cancelled.Exposure, cancelled.Terminal = ResultCancelled, ExposureNone, true
			out = append(out, cancelled)
			return out, nil
		}
		e := newEvent(1, EventToolAttemptCancelled)
		e.ToolID, e.AttemptID, e.Ref, e.Terminal = x.TargetID, x.AttemptID, x.Ref, x.Terminal
		e.Token, e.Incarnation, e.Fence = x.ExecutorToken, x.ExecutorIncarnation, x.Fence
		out = append(out, e)
	case CommandObserveUnknownTool:
		x := c.Transition
		e := newEvent(1, EventToolOutcomeUnknown)
		e.ToolID, e.AttemptID, e.CauseRef, e.Exposure = x.TargetID, x.AttemptID, x.CauseRef, x.Exposure
		e.Token, e.Incarnation, e.Fence = x.ExecutorToken, x.ExecutorIncarnation, x.Fence
		out = append(out, e)
		r := newEvent(2, EventRunInterrupted)
		r.ToolID, r.CauseRef, r.Interrupt = x.TargetID, x.CauseRef, InterruptEffectUnknown
		out = append(out, r)
	case CommandAbandonTool:
		x := c.Transition
		if x.Next != NextContinue && x.Next != NextEnd {
			return nil, reject(RejectInvalid)
		}
		e := newEvent(1, EventToolAttemptAbandoned)
		e.ToolID, e.AttemptID, e.Ref, e.ReceiptRef, e.Exposure, e.Next = x.TargetID, x.AttemptID, x.Ref, x.ReceiptRef, x.Exposure, x.Next
		if x.Next == NextEnd {
			if x.Actor != ActorUser || !validID(x.CauseRef) || !validID(x.ResultRef) {
				return nil, reject(RejectInvalid)
			}
			e.CauseRef, e.Actor = x.CauseRef, x.Actor
			r := newEvent(2, EventToolResultRecorded)
			r.ToolID, r.AttemptID, r.CauseRef, r.Ref, r.ReceiptRef = x.TargetID, x.AttemptID, x.CauseRef, x.ResultRef, x.ReceiptRef
			r.Disposition, r.Exposure = ResultAbandonedUnknown, ExposureUnknown
			out = append(out, e, r)
			break
		}
		out = append(out, e)
	case CommandReconcileTool:
		x := c.Transition
		e := newEvent(1, EventToolReconciled)
		e.ToolID, e.AttemptID, e.Ref, e.Disposition, e.Exposure = x.TargetID, x.AttemptID, x.ReceiptRef, x.Disposition, x.Exposure
		out = append(out, e)
		r := newEvent(2, EventToolResultRecorded)
		r.ToolID, r.AttemptID, r.Ref, r.ReceiptRef, r.Disposition, r.Exposure = x.TargetID, x.AttemptID, x.ResultRef, x.ReceiptRef, x.Disposition, x.Exposure
		out = append(out, r)
	case CommandPlanToolRetry:
		x := c.Transition
		e := newEvent(1, EventToolAttemptPlanned)
		e.ToolID, e.PreviousAttemptID, e.AttemptID, e.ReceiptRef, e.TimerID, e.Generation = x.TargetID, x.PreviousAttemptID, x.AttemptID, x.ReceiptRef, x.TimerID, x.Generation
		out = append(out, e)
	case CommandScheduleModelRetry:
		x := c.Transition
		e := newEvent(1, EventModelRetryScheduled)
		e.InvocationID, e.PreviousAttemptID, e.TimerID, e.BlockerID, e.Ref, e.Purpose, e.Generation = x.TargetID, x.PreviousAttemptID, x.TimerID, x.BlockerID, x.Ref, TimerModelRetry, x.Generation
		out = append(out, e)
	case CommandPlanModelRetry:
		x := c.Transition
		e := newEvent(1, EventModelAttemptPlanned)
		e.InvocationID, e.PreviousAttemptID, e.AttemptID, e.TimerID, e.Generation = x.TargetID, x.PreviousAttemptID, x.AttemptID, x.TimerID, x.Generation
		out = append(out, e)
	case CommandFailModelInvocation:
		x := c.Transition
		e := newEvent(1, EventModelInvocationFailed)
		e.InvocationID, e.Ref = x.TargetID, x.Ref
		out = append(out, e)
	case CommandScheduleTimer:
		x := c.Transition
		e := newEvent(1, EventTimerScheduled)
		e.TimerID, e.BlockerID, e.Ref, e.Purpose, e.Generation, e.ToolID, e.InvocationID = x.TimerID, x.BlockerID, x.Ref, x.Purpose, x.Generation, x.TargetID, x.RelatedID
		out = append(out, e)
	case CommandObserveTimerElapsed:
		x := c.Transition
		e := newEvent(1, EventTimerElapsed)
		e.TimerID, e.Generation, e.CauseRef = x.TimerID, x.Generation, x.Ref
		out = append(out, e)
	case CommandAcquireOwnership:
		x := c.Transition
		e := newEvent(1, EventRunOwnershipAcquired)
		e.PreviousToken, e.PreviousIncarnation, e.Token, e.Incarnation, e.ReceiptRef = c.WriterToken, c.WriterIncarnation, x.ExecutorToken, x.ExecutorIncarnation, x.ReceiptRef
		out = append(out, e)
		for i, fact := range x.Recoveries {
			n := uint32(2 + i*3)
			if fact.Tool {
				u := newEvent(n, EventToolOutcomeUnknown)
				u.ToolID, u.AttemptID, u.CauseRef, u.Exposure = fact.TargetID, fact.AttemptID, fact.CauseRef, fact.Exposure
				u.Token, u.Incarnation, u.Fence, u.PreviousToken, u.PreviousIncarnation = x.ExecutorToken, x.ExecutorIncarnation, fact.Fence, fact.PreviousToken, fact.PreviousIncarnation
				u.ReceiptRef = x.ReceiptRef
				out = append(out, u)
				r := newEvent(n+1, EventRunInterrupted)
				r.ToolID, r.CauseRef, r.Interrupt = fact.TargetID, fact.CauseRef, fact.Interrupt
				out = append(out, r)
			} else {
				u := newEvent(n, EventModelRequestInterrupted)
				u.InvocationID, u.AttemptID, u.CauseRef, u.Ref, u.Usage = fact.TargetID, fact.AttemptID, fact.CauseRef, fact.PartialRef, fact.Usage
				u.Token, u.Incarnation, u.Fence, u.PreviousToken, u.PreviousIncarnation = x.ExecutorToken, x.ExecutorIncarnation, fact.Fence, fact.PreviousToken, fact.PreviousIncarnation
				u.ReceiptRef = x.ReceiptRef
				out = append(out, u)
				r := newEvent(n+1, EventRunInterrupted)
				r.ToolID, r.CauseRef, r.Interrupt = fact.AttemptID, fact.CauseRef, InterruptEffectUnknown
				out = append(out, r)
			}
			b := newEvent(n+2, EventRunBlockAdded)
			b.BlockerID, b.BlockKind, b.Ref = fact.BlockerID, BlockRecoveryAdmission, x.ReceiptRef
			out = append(out, b)
		}
	case CommandConfirmRunClosure:
		x := c.Transition
		if len(x.UnknownOutcomes) > 1 || (len(x.UnknownOutcomes) > 0 && x.Outcome != OutcomeAbandonedUnknown) {
			return nil, reject(RejectInvalid)
		}
		e := newEvent(1, EventRunClosureConfirmed)
		e.Ref, e.CauseRef, e.ReceiptRef, e.Actor, e.Revision = x.Ref, x.CauseRef, x.ReceiptRef, x.Actor, x.Revision
		e.PreviewMode, e.PreviewVersion, e.UnavailableReason = x.PreviewMode, x.PreviewVersion, x.UnavailableReason
		e.PreviousContextRevision, e.ContextRevision, e.ContextRequirement, e.CauseEventRef = x.PreviousContextRevision, x.ContextRevision, x.ContextRequirement, x.CauseEventRef
		e.RecoveryDisposition = x.RecoveryDisposition
		if len(x.UnknownOutcomes) > 0 {
			e.ToolID, e.AttemptID, e.CauseRef = x.UnknownOutcomes[0].ToolID, x.UnknownOutcomes[0].AttemptID, x.UnknownOutcomes[0].CauseRef
			e.Accepted, e.Disposition, e.RecoveryDisposition = true, ResultAbandonedUnknown, RecoveryDispositionAcceptedUnknown
		} else if x.RecoveryDisposition == RecoveryDispositionAcceptedUnknown {
			e.Accepted = true
		}
		out = append(out, e)
		for i, fact := range x.UnknownOutcomes {
			n := uint32(2 + i*2)
			r := newEvent(n, EventToolResultRecorded)
			r.ToolID, r.AttemptID, r.CauseRef, r.Ref, r.ReceiptRef, r.Disposition, r.Exposure = fact.ToolID, fact.AttemptID, fact.CauseRef, fact.ResultRef, x.ReceiptRef, ResultAbandonedUnknown, ExposureUnknown
			out = append(out, r)
			clear := newEvent(n+1, EventRunRecoveryCleared)
			clear.ToolID, clear.AttemptID, clear.CauseRef, clear.Ref, clear.ReceiptRef, clear.Actor = fact.ToolID, fact.AttemptID, fact.CauseRef, x.ReceiptRef, x.ReceiptRef, ActorUser
			clear.Accepted, clear.Disposition, clear.RecoveryDisposition = true, ResultAbandonedUnknown, RecoveryDispositionAcceptedUnknown
			out = append(out, clear)
		}
		finalOrdinal := uint32(2)
		if len(x.UnknownOutcomes) > 0 {
			abort := newEvent(uint32(2+len(x.UnknownOutcomes)*2), EventStepAborted)
			abort.Ref = x.Ref
			out = append(out, abort)
			finalOrdinal += uint32(1 + len(x.UnknownOutcomes)*2)
		}
		f := newEvent(finalOrdinal, EventRunFinished)
		f.Outcome, f.Truncated = x.Outcome, x.Truncated
		out = append(out, f)
	case CommandAcceptUnknownOutcome:
		x := c.Transition
		e := newEvent(1, EventUnknownAccepted)
		e.ToolID, e.AttemptID, e.Ref, e.CauseRef, e.ReceiptRef, e.Actor, e.Disposition = x.TargetID, x.AttemptID, x.Ref, x.CauseRef, x.ReceiptRef, x.Actor, x.Disposition
		out = append(out, e)
		r := newEvent(2, EventToolResultRecorded)
		r.ToolID, r.AttemptID, r.CauseRef, r.Ref, r.ReceiptRef, r.Disposition, r.Exposure = x.TargetID, x.AttemptID, x.CauseRef, x.ResultRef, x.ReceiptRef, ResultAbandonedUnknown, ExposureUnknown
		out = append(out, r)
	case CommandAcceptUnknownModelOutcome:
		x := c.Transition
		e := newEvent(1, EventUnknownAccepted)
		e.InvocationID, e.AttemptID, e.CauseRef, e.Ref, e.ReceiptRef, e.Actor, e.Disposition, e.IsModel = x.TargetID, x.AttemptID, x.CauseRef, x.Ref, x.ReceiptRef, x.Actor, ResultAbandonedUnknown, true
		out = append(out, e)
	case CommandReconcileModelUsage:
		x := c.Transition
		e := newEvent(1, EventModelUsageReconciled)
		e.InvocationID, e.AttemptID, e.Usage, e.Ref = x.TargetID, x.AttemptID, x.Usage, x.Ref
		out = append(out, e)
	case CommandOpenTimeWindow:
		x := c.Transition
		t := newEvent(1, EventTimerScheduled)
		t.TimerID, t.WindowID, t.Ref, t.Purpose, t.Generation = x.TimerID, x.WindowID, x.Ref, TimerRunDeadline, x.Generation
		out = append(out, t)
		e := newEvent(2, EventTimeBudgetWindowOpened)
		e.WindowID, e.TimerID, e.Generation, e.Incarnation, e.Usage = x.WindowID, x.TimerID, x.Generation, x.ExecutorIncarnation, x.Usage
		out = append(out, e)
	case CommandCloseTimeWindow:
		x := c.Transition
		e := newEvent(1, EventTimeBudgetWindowClosed)
		e.WindowID, e.TimerID, e.Generation, e.Ref, e.Covered = x.WindowID, x.TimerID, x.Generation, x.Ref, x.Covered
		out = append(out, e)
		t := newEvent(2, EventTimerCancelled)
		t.TimerID, t.Generation, t.CauseRef = x.TimerID, x.Generation, x.Ref
		out = append(out, t)
	case CommandExpireTimeWindow:
		x := c.Transition
		t := newEvent(1, EventTimerElapsed)
		t.TimerID, t.Generation, t.CauseRef = x.TimerID, x.Generation, x.CauseRef
		out = append(out, t)
		b := newEvent(2, EventBudgetObserved)
		b.WindowID, b.Usage, b.Ref = x.WindowID, x.Usage, x.Ref
		out = append(out, b)
		w := newEvent(3, EventTimeBudgetWindowClosed)
		w.WindowID, w.TimerID, w.Generation, w.Ref, w.Covered = x.WindowID, x.TimerID, x.Generation, x.CauseRef, true
		out = append(out, w)
		if x.BudgetExhausted {
			stop := newEvent(4, EventRunStopRequested)
			stop.StopReason, stop.StopMode, stop.BudgetKind, stop.Actor, stop.Ref = StopBudget, StopTerminal, x.BudgetKind, x.Actor, x.CauseRef
			out = append(out, stop)
		}
	case CommandObserveBudget:
		x := c.Transition
		e := newEvent(1, EventBudgetObserved)
		e.InvocationID, e.AttemptID, e.Usage, e.Ref = x.TargetID, x.AttemptID, x.Usage, x.Ref
		out = append(out, e)
	case CommandClearRunBlock:
		x := c.Transition
		e := newEvent(1, EventRunBlockCleared)
		e.BlockerID, e.Ref, e.CauseRef, e.Generation, e.Revision = x.BlockerID, x.Ref, x.CauseRef, x.Generation, x.Revision
		out = append(out, e)
	case CommandClearRecovery:
		x := c.Transition
		e := newEvent(1, EventRunRecoveryCleared)
		e.Ref, e.ReceiptRef, e.Actor, e.CauseRef, e.RecoveryDisposition = x.Ref, x.ReceiptRef, x.Actor, x.CauseRef, x.RecoveryDisposition
		out = append(out, e)
	default:
		return nil, reject(RejectInvalid)
	}
	return out, nil
}
