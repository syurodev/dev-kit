package kernel

import (
	"sort"
)

func validID(id ID) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, r := range string(id) {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' || r == ':' {
			continue
		}
		return false
	}
	return true
}

func cloneEvents(in []Event) []Event {
	out := make([]Event, len(in))
	for i, ev := range in {
		out[i] = cloneEvent(ev)
	}
	return out
}

func cloneEvent(ev Event) Event {
	if ev.Candidates != nil {
		ev.Candidates = append(make([]ToolCandidate, 0, len(ev.Candidates)), ev.Candidates...)
	}
	return ev
}

func cloneBatch(b Batch) Batch {
	b.Events = cloneEvents(b.Events)
	if b.Intent.ModelResponse.Candidates != nil {
		b.Intent.ModelResponse.Candidates = append(make([]ToolCandidate, 0, len(b.Intent.ModelResponse.Candidates)), b.Intent.ModelResponse.Candidates...)
	}
	if b.Intent.Transition.Recoveries != nil {
		b.Intent.Transition.Recoveries = append(make([]RecoveryFact, 0, len(b.Intent.Transition.Recoveries)), b.Intent.Transition.Recoveries...)
	}
	if b.Intent.Transition.UnknownOutcomes != nil {
		b.Intent.Transition.UnknownOutcomes = append(make([]UnknownOutcomeFact, 0, len(b.Intent.Transition.UnknownOutcomes)), b.Intent.Transition.UnknownOutcomes...)
	}
	return b
}

func cloneState(s State) State {
	n := s
	n.SecondaryStops = append([]StopIntent(nil), s.SecondaryStops...)
	n.Steps = append([]Step(nil), s.Steps...)
	for i := range n.Steps {
		n.Steps[i].ToolIDs = append([]ID(nil), s.Steps[i].ToolIDs...)
		n.Steps[i].CompletedTools = append([]ID(nil), s.Steps[i].CompletedTools...)
		n.Steps[i].Transcript = append([]TranscriptItem(nil), s.Steps[i].Transcript...)
	}
	n.Transcript = append([]TranscriptItem(nil), s.Transcript...)
	n.WaitReasons = append([]BlockKind(nil), s.WaitReasons...)
	n.Windows = make(map[ID]TimeWindow, len(s.Windows))
	for id, w := range s.Windows {
		w.ObservedRefs = append([]ID(nil), w.ObservedRefs...)
		n.Windows[id] = w
	}
	n.Models = make(map[ID]ModelInvocation, len(s.Models))
	for id, v := range s.Models {
		v.AttemptIDs = append([]ID(nil), v.AttemptIDs...)
		v.CandidateIDs = append([]ID(nil), v.CandidateIDs...)
		v.Candidates = append([]ToolCandidate(nil), v.Candidates...)
		n.Models[id] = v
	}
	n.ModelAttempts = cloneMap(s.ModelAttempts)
	n.Tools = make(map[ID]ToolInvocation, len(s.Tools))
	for id, v := range s.Tools {
		v.AttemptIDs = append([]ID(nil), v.AttemptIDs...)
		n.Tools[id] = v
	}
	n.ToolAttempts = cloneMap(s.ToolAttempts)
	n.Blockers = cloneMap(s.Blockers)
	n.Interruptions = cloneMap(s.Interruptions)
	n.Timers = cloneMap(s.Timers)
	n.Exposures = cloneMap(s.Exposures)
	n.EventIDs = make(map[ID]Event, len(s.EventIDs))
	for id, ev := range s.EventIDs {
		n.EventIDs[id] = cloneEvent(ev)
	}
	n.CommandBatches = make(map[ID]Batch, len(s.CommandBatches))
	for id, b := range s.CommandBatches {
		n.CommandBatches[id] = cloneBatch(b)
	}
	derive(&n)
	return n
}

func cloneMap[T any](in map[ID]T) map[ID]T {
	out := make(map[ID]T, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func currentStep(s *State) *Step {
	if s.CurrentStep < 0 || s.CurrentStep >= len(s.Steps) {
		return nil
	}
	return &s.Steps[s.CurrentStep]
}

func currentStepID(s State) ID {
	if s.CurrentStep < 0 || s.CurrentStep >= len(s.Steps) {
		return ""
	}
	return s.Steps[s.CurrentStep].ID
}

func stepByID(s *State, id ID) *Step {
	for i := range s.Steps {
		if s.Steps[i].ID == id {
			return &s.Steps[i]
		}
	}
	return nil
}

func canAdvance(s *State) bool {
	if !s.Admitted || s.Finished || !dispatchable(s) {
		return false
	}
	if s.CurrentStep < 0 {
		return true
	}
	st := s.Steps[s.CurrentStep]
	return !st.Open && (st.Next == NextContinue || st.Next == NextWait && !stepBudgetWaiting(*s))
}

func dispatchable(s *State) bool {
	return s.Admitted && !s.Finished && s.PrimaryStop.Mode != StopTerminal && len(s.Blockers) == 0 && !hasUnresolvedInterruption(*s) && !stepBudgetWaiting(*s)
}

func effectiveStepLimit(s State) uint64 {
	if s.StepBudgetExtended > ^uint64(0)-s.InitialStepLimit {
		return ^uint64(0)
	}
	return s.InitialStepLimit + s.StepBudgetExtended
}

func stepBudgetWaiting(s State) bool {
	if s.PrimaryStop.Mode == StopTerminal || s.CurrentStep < 0 || s.InitialStepLimit == 0 || s.StepsStarted < effectiveStepLimit(s) {
		return false
	}
	st := s.Steps[s.CurrentStep]
	return !st.Open && st.Next == NextWait && st.Continuation == ContinuationStepBudget
}

func validDispatchFence(s *State, e Event) bool {
	return e.Token != 0 && e.Token == s.AdmissionToken && e.Incarnation == s.Incarnation && e.Fence != 0
}

func callbackMatches(s State, a Attempt, e Event) bool {
	return e.Token == a.Token && e.Incarnation == a.Incarnation && e.Fence == a.Fence && e.Token == s.AdmissionToken && e.Incarnation == s.Incarnation && e.Fence != 0
}

func callbackMatchesTool(s State, a ToolAttempt, e Event) bool {
	return e.Token == a.Token && e.Incarnation == a.Incarnation && e.Fence == a.Fence && e.Token == s.AdmissionToken && e.Incarnation == s.Incarnation && e.Fence != 0
}

func recoveryCallbackMatches(s State, oldToken uint64, oldIncarnation ID, oldFence uint64, e Event) bool {
	return s.OwnershipReceipt != "" && validID(e.ReceiptRef) && e.PreviousToken == oldToken && e.PreviousIncarnation == oldIncarnation && e.Token == s.AdmissionToken && e.Incarnation == s.Incarnation && e.Fence == oldFence && oldToken != 0 && oldFence != 0 && validID(oldIncarnation)
}

func ownershipReceiptUsed(s State, receipt ID) bool {
	if receipt == "" {
		return true
	}
	for _, event := range s.EventIDs {
		if event.Kind == EventRunOwnershipAcquired && event.ReceiptRef == receipt {
			return true
		}
	}
	return false
}

func hasActiveAttempt(s State) bool {
	for _, a := range s.ModelAttempts {
		if isActiveModel(a.State) {
			return true
		}
	}
	for _, a := range s.ToolAttempts {
		if a.State == AttemptRequesting || a.State == AttemptStreaming {
			return true
		}
	}
	return false
}

func isActiveModel(s AttemptState) bool { return s == AttemptRequesting || s == AttemptStreaming }

func hasUnresolvedInterruption(s State) bool {
	for _, v := range s.Interruptions {
		if !v.Resolved && !v.Accepted {
			return true
		}
	}
	return false
}

func contains(ids []ID, id ID) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}

func idExists[T any](m map[ID]T, id ID) bool {
	_, ok := m[id]
	return ok
}

func anyAttemptExists(s State, id ID) bool {
	if _, ok := s.ModelAttempts[id]; ok {
		return true
	}
	_, ok := s.ToolAttempts[id]
	return ok
}

func candidateByID(s *State, invocationID, candidateID ID) (ToolCandidate, bool) {
	inv := s.Models[invocationID]
	for _, candidate := range inv.Candidates {
		if candidate.ID == candidateID {
			return candidate, true
		}
	}
	return ToolCandidate{}, false
}

func isFrontierTool(s State, id ID) bool {
	st := currentStep(&s)
	if st == nil {
		return false
	}
	for _, toolID := range st.ToolIDs {
		tool := s.Tools[toolID]
		if tool.State != ToolSucceeded && tool.State != ToolFailed && tool.State != ToolDenied && tool.State != ToolRefused && tool.State != ToolCancelled && tool.State != ToolAbandoned {
			return toolID == id
		}
	}
	return false
}

func blockerForTool(s State, toolID ID, kind BlockKind) (Blocker, bool) {
	for _, blocker := range s.Blockers {
		if blocker.TargetID == toolID && blocker.Kind == kind {
			return blocker, true
		}
	}
	return Blocker{}, false
}

func appendToolTranscript(s *State, tool *ToolInvocation, disposition ResultDisposition, resultRef ID) {
	if tool.ResultRef == "" {
		tool.ResultRef = resultRef
	}
	st := stepByID(s, tool.StepID)
	if st == nil {
		return
	}
	item := TranscriptItem{SourceIndex: tool.SourceIndex, InvocationID: tool.ID, ResponseRef: st.ResponseRef, ResultRef: resultRef, Disposition: disposition}
	st.Transcript = append(st.Transcript, item)
	st.CompletedTools = append(st.CompletedTools, tool.ID)
	s.Transcript = append(s.Transcript, item)
}

func stepToolsTerminal(s State, st Step) bool {
	for _, id := range st.ToolIDs {
		tool, ok := s.Tools[id]
		if !ok || tool.Disposition == ResultNone || tool.ResultRef == "" {
			return false
		}
	}
	return true
}

func validResultDisposition(d ResultDisposition) bool {
	return d == ResultSucceeded || d == ResultFailed || d == ResultDenied || d == ResultRefused || d == ResultCancelled || d == ResultAbandonedUnknown
}

func validUsageStatus(u UsageStatus) bool { return u == UsageKnown || u == UsageReserved }
func validEffectClass(e EffectClass) bool { return e <= EffectSideEffecting }
func validUsageFact(u UsageStatus) bool   { return u <= UsageReserved }
func validActor(a ActorKind) bool         { return a >= ActorUser && a <= ActorSystem }

func validClosureCauseEvent(kind EventKind) bool {
	switch kind {
	case EventRunAccepted, EventRunAdmitted, EventRunBlockAdded, EventRunInterrupted,
		EventModelRequestFailed, EventModelRequestInterrupted, EventToolOutcomeUnknown,
		EventBudgetObserved, EventTimerElapsed, EventTimerCancelled, EventTimeBudgetWindowClosed:
		return true
	default:
		return false
	}
}

func stopActorCompatible(reason StopReason, actor ActorKind) bool {
	switch reason {
	case StopUserCancel:
		return actor == ActorUser
	case StopParentCancel:
		return actor == ActorParent
	case StopBudget:
		return actor == ActorUser || actor == ActorWorker || actor == ActorTimer || actor == ActorSystem
	case StopFatalError:
		return actor == ActorWorker || actor == ActorSystem
	case StopShutdown:
		return actor == ActorSystem
	default:
		return false
	}
}
func validExposure(e Exposure) bool { return e <= ExposureKnown }

func validContinuation(c ContinuationReason) bool {
	return c >= ContinuationToolCalls && c <= ContinuationStepBudget
}

func continuationNext(c ContinuationReason) NextAction {
	switch c {
	case ContinuationToolCalls, ContinuationTruncatedArgs, ContinuationSteerPending, ContinuationTruncatedContinue:
		return NextContinue
	case ContinuationNaturalEnd, ContinuationTruncatedEnd, ContinuationToolTerminate, ContinuationRefused, ContinuationStopIntent, ContinuationBudget:
		return NextEnd
	case ContinuationStepBudget:
		return NextWait
	default:
		return NextActionUnknown
	}
}

func toolStateFor(d ResultDisposition) ToolState {
	switch d {
	case ResultSucceeded:
		return ToolSucceeded
	case ResultFailed:
		return ToolFailed
	case ResultDenied:
		return ToolDenied
	case ResultRefused:
		return ToolRefused
	case ResultCancelled:
		return ToolCancelled
	default:
		return ToolAbandoned
	}
}

func scheduleTimer(s *State, e Event, target ID) error {
	if !validID(e.TimerID) || !validID(e.Ref) || e.Generation == 0 || e.Purpose < TimerModelRetry || e.Purpose > TimerDependencyExpiry {
		return reject(RejectInvalid)
	}
	if _, ok := s.Timers[e.TimerID]; ok {
		return reject(RejectConflict)
	}
	if e.Purpose != TimerRunDeadline && !validID(e.BlockerID) {
		return reject(RejectInvalid)
	}
	s.Timers[e.TimerID] = Timer{ID: e.TimerID, Generation: e.Generation, TargetID: target, BlockerID: e.BlockerID, Purpose: e.Purpose, Pending: true}
	if e.Purpose != TimerRunDeadline {
		if _, exists := s.Blockers[e.BlockerID]; exists {
			return reject(RejectConflict)
		}
		s.Blockers[e.BlockerID] = Blocker{ID: e.BlockerID, Kind: BlockRetryTimer, Ref: e.Ref, TargetID: target, Generation: e.Generation}
	}
	return nil
}

func consumeTimer(s *State, id ID, generation uint64) bool {
	t, ok := s.Timers[id]
	if !ok || !t.Pending || t.Generation != generation {
		return false
	}
	t.Pending = false
	s.Timers[id] = t
	if t.BlockerID != "" {
		delete(s.Blockers, t.BlockerID)
	}
	return true
}

func consumeElapsedTimer(s *State, id ID, generation uint64) bool {
	t, ok := s.Timers[id]
	if !ok || t.Pending || !t.Elapsed || t.Generation != generation {
		return false
	}
	t.Elapsed = false
	s.Timers[id] = t
	return true
}

func hasOpenWindow(s State) bool {
	for _, w := range s.Windows {
		if w.Open {
			return true
		}
	}
	return false
}

func finishAllowed(s State, outcome Outcome) bool {
	if outcome < OutcomeCompleted || outcome > OutcomeAbandonedUnknown || len(s.Blockers) != 0 || hasActiveAttempt(s) || hasOpenWindow(s) {
		return false
	}
	for _, step := range s.Steps {
		if step.Open {
			return false
		}
	}
	for _, timer := range s.Timers {
		if timer.Pending {
			return false
		}
	}
	for _, w := range s.Windows {
		if !w.Covered {
			return false
		}
	}
	for _, model := range s.Models {
		if model.Disposition == ResultNone {
			return false
		}
	}
	unknownForClosure := hasUnknownClosureFact(s)
	if (outcome == OutcomeAbandonedUnknown) != unknownForClosure && (outcome == OutcomeAbandoned || outcome == OutcomeAbandonedUnknown) {
		return false
	}
	if outcome != OutcomeAbandonedUnknown {
		if hasUnresolvedInterruption(s) {
			return false
		}
		for _, tool := range s.Tools {
			if tool.State == ToolUnknown || tool.Disposition == ResultAbandonedUnknown {
				return false
			}
		}
		for _, model := range s.Models {
			if model.Disposition == ResultAbandonedUnknown {
				return false
			}
		}
	}
	for _, tool := range s.Tools {
		if tool.Disposition == ResultNone {
			return false
		}
	}
	if outcome == OutcomeCompleted {
		st := currentStep(&s)
		if s.PrimaryStop.Mode == StopTerminal || st == nil || st.Open || st.Aborted || st.Next != NextEnd || continuationNext(st.Continuation) != NextEnd || st.Continuation == ContinuationBudget || hasUnresolvedInterruption(s) {
			return false
		}
	}
	if outcome == OutcomeAbandonedUnknown {
		if !s.ClosureConfirmed {
			return false
		}
		if !hasUnknownClosureFact(s) {
			return false
		}
		for _, intr := range s.Interruptions {
			if !intr.Resolved && !intr.Accepted {
				return false
			}
		}
	}
	if outcome == OutcomeAbandoned && !s.ClosureConfirmed {
		return false
	}
	if outcome == OutcomeCancelled && s.PrimaryStop.Reason != StopUserCancel && s.PrimaryStop.Reason != StopParentCancel {
		return false
	}
	if outcome == OutcomeBudgetExceeded && s.PrimaryStop.Reason != StopBudget {
		return false
	}
	if outcome == OutcomeBudgetExceeded {
		if s.BudgetStopKind == 0 {
			return false
		}
		st := currentStep(&s)
		if s.BudgetStopKind == BudgetSteps {
			if s.BudgetStopActor != ActorUser || st == nil || st.Continuation != ContinuationStepBudget || st.Next != NextWait {
				return false
			}
		}
	}
	if outcome == OutcomeFailed && s.PrimaryStop.Reason != StopFatalError {
		return false
	}
	return true
}

func hasUnknownClosureFact(s State) bool {
	for _, exposure := range s.Exposures {
		if exposure == ExposureUnknown {
			return true
		}
	}
	for _, attempt := range s.ModelAttempts {
		if attempt.State != AttemptPlanned && (attempt.Usage.Status == UsageUnknown || attempt.Usage.Status == UsageReserved) {
			return true
		}
	}
	return false
}

func derive(s *State) {
	if s.Quiescing && !hasActiveAttempt(*s) {
		s.Quiescing = false
	}
	s.Lifecycle = deriveLifecycle(*s)
	s.Phase = derivePhase(*s)
	reasons := make([]BlockKind, 0, len(s.Blockers))
	seen := map[BlockKind]bool{}
	for _, blocker := range s.Blockers {
		if !seen[blocker.Kind] {
			seen[blocker.Kind] = true
			reasons = append(reasons, blocker.Kind)
		}
	}
	if stepBudgetWaiting(*s) && !seen[BlockStepBudget] {
		reasons = append(reasons, BlockStepBudget)
	}
	sort.Slice(reasons, func(i, j int) bool { return reasons[i] < reasons[j] })
	s.WaitReasons = reasons
}

func deriveLifecycle(s State) Lifecycle {
	if s.Finished {
		return Finished
	}
	if hasUnresolvedInterruption(s) {
		return Interrupted
	}
	if s.PrimaryStop.Mode == StopTerminal {
		return Cancelling
	}
	if !s.Admitted {
		return Queued
	}
	if hasActiveAttempt(s) || s.Quiescing {
		return Running
	}
	if len(s.Blockers) != 0 {
		return Waiting
	}
	if stepBudgetWaiting(s) {
		return Waiting
	}
	return Running
}

func derivePhase(s State) Phase {
	if s.Finished || s.PrimaryStop.Mode == StopTerminal || s.Outcome == OutcomeAbandoned || s.Outcome == OutcomeAbandonedUnknown {
		return PhaseFinalizing
	}
	st := currentStep(&s)
	if st == nil {
		return PhasePreparing
	}
	if !st.Open {
		if st.Next == NextEnd {
			return PhaseFinalizing
		}
		return PhasePreparing
	}
	if st.ModelID == "" {
		return PhasePreparing
	}
	inv := s.Models[st.ModelID]
	if inv.ResponseRef == "" && inv.State != InvocationFailed && inv.State != InvocationCancelled && inv.State != InvocationAbandoned {
		return PhaseCallingModel
	}
	if st.ResponseRef != "" {
		return PhaseProcessingTools
	}
	return PhasePreparing
}

func acceptedUnknownFor(s State, target ID) bool {
	for _, intr := range s.Interruptions {
		if intr.TargetID == target && intr.Accepted {
			return true
		}
	}
	return false
}

func closureMatchesUnknown(s State, e Event) bool {
	return s.ClosureConfirmed && s.ClosureAcceptedUnknown && s.ClosureToolID == e.ToolID && s.ClosureAttemptID == e.AttemptID && s.ClosureReceipt == e.ReceiptRef && e.Disposition == ResultAbandonedUnknown
}

func resolveInterruptionTarget(s *State, target ID, accept bool) {
	for id, intr := range s.Interruptions {
		if intr.TargetID == target {
			if accept {
				intr.Accepted = true
			} else {
				intr.Resolved = true
			}
			s.Interruptions[id] = intr
		}
	}
}
