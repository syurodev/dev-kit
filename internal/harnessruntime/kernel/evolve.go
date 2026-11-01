package kernel

import (
	"reflect"
)

type RejectCode uint8

const (
	RejectInvalid RejectCode = iota + 1
	RejectConflict
	RejectStale
	RejectInvariant
)

type Rejection struct{ Code RejectCode }

func (r Rejection) Error() string {
	switch r.Code {
	case RejectConflict:
		return "kernel conflict"
	case RejectStale:
		return "kernel stale fact"
	case RejectInvariant:
		return "kernel invariant violation"
	default:
		return "kernel invalid fact"
	}
}

func reject(code RejectCode) error { return Rejection{Code: code} }

// Evolve applies one already-committed batch to a copy of state. Invariants are
// checked once after the entire batch, matching the transaction boundary.
func Evolve(state State, batch Batch) (State, error) {
	batch = cloneBatch(batch)
	if !validID(batch.CommandID) || len(batch.Events) == 0 {
		return State{}, reject(RejectInvalid)
	}
	if prev, ok := state.CommandBatches[batch.CommandID]; ok {
		if reflect.DeepEqual(prev, batch) {
			return cloneState(state), nil
		}
		return State{}, reject(RejectConflict)
	}
	if batch.ProtocolVersion != CurrentProtocolVersion || batch.ExpectedRevision != state.Revision {
		return State{}, reject(RejectStale)
	}
	if err := validateAtomicEventPairs(batch.Events, state.ClosureConfirmed); err != nil {
		return State{}, err
	}
	if err := validateClosureManifest(state, batch); err != nil {
		return State{}, err
	}
	if err := validateExactClosure(state, batch); err != nil {
		return State{}, err
	}
	if err := validateWriter(state, batch); err != nil {
		return State{}, err
	}
	next := cloneState(state)
	for i, ev := range batch.Events {
		if !validID(ev.ID) {
			return State{}, reject(RejectInvalid)
		}
		if prior, ok := next.EventIDs[ev.ID]; ok {
			_ = prior
			return State{}, reject(RejectConflict)
		}
		if ev.ProtocolVersion != CurrentProtocolVersion {
			return State{}, reject(RejectInvalid)
		}
		batch.Events[i] = ev
		if err := applyEvent(&next, ev); err != nil {
			return State{}, err
		}
		next.EventIDs[ev.ID] = cloneEvent(ev)
		next.Revision++
	}
	derive(&next)
	if err := CheckInvariants(next); err != nil {
		return State{}, err
	}
	next.CommandBatches[batch.CommandID] = cloneBatch(batch)
	return next, nil
}

func validateWriter(state State, batch Batch) error {
	var admitted, ownership *Event
	for i := range batch.Events {
		e := &batch.Events[i]
		if e.Kind == EventRunAdmitted {
			admitted = e
		}
		if e.Kind == EventRunOwnershipAcquired {
			ownership = e
		}
	}
	if ownership != nil {
		if !state.Admitted || ownership.PreviousToken != state.AdmissionToken || ownership.PreviousIncarnation != state.Incarnation || batch.WriterToken != ownership.Token || batch.WriterIncarnation != ownership.Incarnation {
			return reject(RejectStale)
		}
		return nil
	}
	if state.Admitted {
		if batch.WriterToken != state.AdmissionToken || batch.WriterIncarnation != state.Incarnation {
			return reject(RejectStale)
		}
		return nil
	}
	if admitted != nil {
		if batch.WriterToken != admitted.Token || batch.WriterIncarnation != admitted.Incarnation {
			return reject(RejectStale)
		}
		return nil
	}
	if batch.WriterToken != 0 || batch.WriterIncarnation != "" {
		return reject(RejectStale)
	}
	return nil
}

func validateAtomicEventPairs(events []Event, closureAlreadyConfirmed bool) error {
	find := func(kind EventKind, tool, attempt, cause ID) bool {
		for _, e := range events {
			if e.Kind == kind && (tool == "" || e.ToolID == tool) && (attempt == "" || e.AttemptID == attempt) && (cause == "" || e.CauseRef == cause) {
				return true
			}
		}
		return false
	}
	for _, e := range events {
		switch e.Kind {
		case EventModelResponseRecorded:
			for _, candidate := range e.Candidates {
				if !find(EventToolCallPlanned, candidate.ID, candidate.AttemptID, "") {
					return reject(RejectInvalid)
				}
			}
		case EventModelRequestInterrupted:
			if !find(EventRunInterrupted, e.AttemptID, "", e.CauseRef) {
				return reject(RejectInvalid)
			}
		case EventToolOutcomeUnknown:
			if !find(EventRunInterrupted, e.ToolID, "", e.CauseRef) {
				return reject(RejectInvalid)
			}
		case EventToolApprovalResolved:
			if e.Disposition == ResultSucceeded && !approvalReadyPair(events, e) {
				return reject(RejectInvalid)
			}
			if (e.Disposition == ResultDenied || e.Disposition == ResultRefused) && !approvalResultPair(events, e) {
				return reject(RejectInvalid)
			}
		case EventToolReconciled:
			found := false
			for _, result := range events {
				if result.Kind == EventToolResultRecorded && result.ToolID == e.ToolID && result.AttemptID == e.AttemptID && result.ReceiptRef == e.Ref && result.Disposition == e.Disposition {
					found = true
					break
				}
			}
			if !found {
				return reject(RejectInvalid)
			}
		case EventToolResultRecorded:
			switch e.Disposition {
			case ResultAbandonedUnknown:
				closure, cleared := false, false
				abandonClose := hasAbandonClose(events, e.ToolID, e.AttemptID, e.CauseRef, e.ReceiptRef)
				for _, candidate := range events {
					closure = closure || (candidate.Kind == EventRunClosureConfirmed && candidate.ReceiptRef == e.ReceiptRef && closureManifestHas(candidate.ClosureTargets, e.ToolID, e.AttemptID, e.CauseRef, ClosureTargetUnknownTool))
					cleared = cleared || (candidate.Kind == EventRunRecoveryCleared && candidate.ToolID == e.ToolID && candidate.AttemptID == e.AttemptID && candidate.CauseRef == e.CauseRef && candidate.RecoveryDisposition == RecoveryDispositionAcceptedUnknown)
				}
				accepted := hasUnknownAcceptanceResultPair(events, e)
				if !accepted && (!closure || !cleared) && !abandonClose {
					return reject(RejectInvalid)
				}
			}
		case EventUnknownAccepted:
			if !e.IsModel && !hasUnknownAcceptanceResultPair(events, e) {
				return reject(RejectInvalid)
			}
		case EventToolAttemptAbandoned:
			if e.Next == NextEnd && !hasAbandonedUnknownResultPair(events, e) {
				return reject(RejectInvalid)
			}
		case EventTimeBudgetWindowClosed:
			elapsed, charge, cancelled := false, false, false
			for _, candidate := range events {
				elapsed = elapsed || (candidate.Kind == EventTimerElapsed && candidate.TimerID == e.TimerID && candidate.Generation == e.Generation)
				charge = charge || (candidate.Kind == EventBudgetObserved && candidate.WindowID == e.WindowID)
				cancelled = cancelled || (candidate.Kind == EventTimerCancelled && candidate.TimerID == e.TimerID && candidate.Generation == e.Generation)
			}
			if (!elapsed || !charge) && !cancelled {
				return reject(RejectInvalid)
			}
		case EventRunClosureConfirmed:
			finishedAbandoned := false
			for _, candidate := range events {
				if candidate.Kind == EventRunFinished && (candidate.Outcome == OutcomeAbandoned || candidate.Outcome == OutcomeAbandonedUnknown) {
					finishedAbandoned = true
					break
				}
			}
			if !finishedAbandoned {
				return reject(RejectInvalid)
			}
			if e.PreviewMode == ClosurePreviewMetadataOnly {
				if !closureManifestEventsMatch(events, e) {
					return reject(RejectInvalid)
				}
				break
			}
			if e.ToolID != "" {
				foundResult, foundClear, foundAbort := false, false, false
				for _, candidate := range events {
					foundResult = foundResult || (candidate.Kind == EventToolResultRecorded && candidate.ToolID == e.ToolID && candidate.AttemptID == e.AttemptID && candidate.ReceiptRef == e.ReceiptRef && candidate.Disposition == ResultAbandonedUnknown)
					foundClear = foundClear || (candidate.Kind == EventRunRecoveryCleared && candidate.ToolID == e.ToolID && candidate.AttemptID == e.AttemptID && candidate.CauseRef == e.CauseRef && candidate.RecoveryDisposition == RecoveryDispositionAcceptedUnknown)
					foundAbort = foundAbort || candidate.Kind == EventStepAborted
				}
				if !foundResult || !foundClear || !foundAbort {
					return reject(RejectInvalid)
				}
			}
		case EventRunRecoveryCleared:
			if e.Accepted {
				result, closure := false, false
				for _, candidate := range events {
					result = result || (candidate.Kind == EventToolResultRecorded && candidate.ToolID == e.ToolID && candidate.AttemptID == e.AttemptID && candidate.Disposition == ResultAbandonedUnknown)
					closure = closure || (candidate.Kind == EventRunClosureConfirmed && (closureManifestHas(candidate.ClosureTargets, e.ToolID, e.AttemptID, e.CauseRef, ClosureTargetUnknownTool) || closureManifestHas(candidate.ClosureTargets, e.InvocationID, e.AttemptID, e.CauseRef, ClosureTargetUnknownModel)))
					closure = closure || (candidate.Kind == EventUnknownAccepted && candidate.IsModel == (e.ToolID == "") && candidate.ToolID == e.ToolID && candidate.InvocationID == e.InvocationID && candidate.AttemptID == e.AttemptID && candidate.CauseRef == e.CauseRef && candidate.ReceiptRef == e.ReceiptRef)
				}
				if e.RecoveryDisposition != RecoveryDispositionAcceptedUnknown || (!result && e.ToolID != "") || !closure {
					return reject(RejectInvalid)
				}
			}
		case EventRunFinished:
			acceptedUnknown := false
			for _, candidate := range events {
				acceptedUnknown = acceptedUnknown || candidate.Kind == EventUnknownAccepted
			}
			if (e.Outcome == OutcomeAbandoned || e.Outcome == OutcomeAbandonedUnknown) && !closureAlreadyConfirmed && !find(EventRunClosureConfirmed, "", "", "") && !acceptedUnknown {
				return reject(RejectInvalid)
			}
		}
	}
	return nil
}

func approvalReadyPair(events []Event, approval Event) bool {
	for _, e := range events {
		if e.Kind == EventToolReadyRecorded && e.ToolID == approval.ToolID && e.Generation == approval.Generation && e.Ref == approval.CauseRef && e.ReceiptRef == approval.ReceiptRef {
			return true
		}
	}
	return false
}
func approvalResultPair(events []Event, approval Event) bool {
	for _, e := range events {
		if e.Kind == EventToolResultRecorded && e.ToolID == approval.ToolID && e.Generation == approval.Generation && e.ReceiptRef == approval.ReceiptRef && e.Disposition == approval.Disposition {
			return true
		}
	}
	return false
}

func hasAbandonClose(events []Event, toolID, attemptID, causeRef, receiptRef ID) bool {
	for _, e := range events {
		if e.Kind == EventToolAttemptAbandoned && e.ToolID == toolID && e.AttemptID == attemptID && e.CauseRef == causeRef && e.ReceiptRef == receiptRef && e.Next == NextEnd {
			return true
		}
	}
	return false
}

func hasAbandonedUnknownResultPair(events []Event, abandon Event) bool {
	for _, event := range events {
		if event.Kind == EventToolResultRecorded && event.ToolID == abandon.ToolID && event.AttemptID == abandon.AttemptID && event.CauseRef == abandon.CauseRef && event.ReceiptRef == abandon.ReceiptRef && event.Disposition == ResultAbandonedUnknown && event.Exposure == ExposureUnknown {
			return true
		}
	}
	return false
}

func hasUnknownAcceptanceResultPair(events []Event, accepted Event) bool {
	var acceptance, result *Event
	if accepted.Kind == EventUnknownAccepted {
		acceptance = &accepted
		for i := range events {
			if events[i].Kind == EventToolResultRecorded {
				result = &events[i]
				break
			}
		}
	} else if accepted.Kind == EventToolResultRecorded {
		result = &accepted
		for i := range events {
			if events[i].Kind == EventUnknownAccepted && !events[i].IsModel {
				acceptance = &events[i]
				break
			}
		}
	}
	if acceptance == nil || result == nil {
		return false
	}
	if acceptance.ToolID == result.ToolID && acceptance.AttemptID == result.AttemptID && acceptance.CauseRef == result.CauseRef && acceptance.ReceiptRef == result.ReceiptRef && result.Disposition == ResultAbandonedUnknown && result.Exposure == ExposureUnknown && validID(result.Ref) && acceptance.Actor == ActorUser && acceptance.Disposition == ResultAbandonedUnknown && validID(acceptance.Ref) && validID(acceptance.ReceiptRef) {
		return true
	}
	return false
}

func applyEvent(s *State, e Event) error {
	if e.Kind == EventRunAccepted {
		if s.Accepted || !validID(e.RunID) || !validID(e.SessionID) || !validID(e.InputRef) {
			return reject(RejectConflict)
		}
		if e.Origin < OriginUser || e.Origin > OriginSystem || (e.Origin == OriginGoal && (!validID(e.GoalID) || e.GoalRound == 0)) || (e.Origin != OriginGoal && (e.GoalID != "" || e.GoalRound != 0)) {
			return reject(RejectInvalid)
		}
		if e.InitialStepLimit == 0 {
			e.InitialStepLimit = 100
		}
		s.RunID, s.SessionID, s.InputRef, s.Accepted = e.RunID, e.SessionID, e.InputRef, true
		s.Origin, s.GoalID, s.GoalRound, s.InitialStepLimit = e.Origin, e.GoalID, e.GoalRound, e.InitialStepLimit
		return nil
	}
	if !s.Accepted || e.RunID != s.RunID || (e.SessionID != "" && e.SessionID != s.SessionID) {
		return reject(RejectStale)
	}
	switch e.Kind {
	case EventRunAdmitted:
		if s.Finished || s.Admitted || e.Token == 0 || !validID(e.Incarnation) {
			return reject(RejectConflict)
		}
		s.Admitted, s.AdmissionToken, s.Incarnation = true, e.Token, e.Incarnation
	case EventRunOwnershipAcquired:
		if !s.Admitted || s.Finished || e.PreviousToken != s.AdmissionToken || e.PreviousIncarnation != s.Incarnation || e.Token <= s.AdmissionToken || !validID(e.Incarnation) || e.Incarnation == s.Incarnation || !validID(e.ReceiptRef) || ownershipReceiptUsed(*s, e.ReceiptRef) {
			return reject(RejectStale)
		}
		s.AdmissionToken, s.Incarnation, s.OwnershipReceipt = e.Token, e.Incarnation, e.ReceiptRef
	case EventStepStarted:
		if !canAdvance(s) || !validID(e.StepID) || s.StepsStarted >= effectiveStepLimit(*s) {
			return reject(RejectConflict)
		}
		if stepByID(s, e.StepID) != nil {
			return reject(RejectConflict)
		}
		idx := uint32(len(s.Steps))
		if e.Generation != uint64(idx+1) {
			return reject(RejectInvalid)
		}
		s.Steps = append(s.Steps, Step{ID: e.StepID, Index: idx, Open: true, Next: NextActionUnknown})
		s.CurrentStep = len(s.Steps) - 1
		s.StepsStarted++
	case EventModelRequestPlanned:
		if !dispatchable(s) || !validID(e.InvocationID) || !validID(e.AttemptID) || !validID(e.Ref) {
			return reject(RejectConflict)
		}
		st := currentStep(s)
		if st == nil || st.ModelID != "" || st.ResponseRef != "" || len(st.ToolIDs) != 0 {
			return reject(RejectConflict)
		}
		if _, exists := s.Models[e.InvocationID]; exists || anyAttemptExists(*s, e.AttemptID) {
			return reject(RejectConflict)
		}
		s.Models[e.InvocationID] = ModelInvocation{ID: e.InvocationID, StepID: st.ID, State: InvocationPlanned, AttemptIDs: []ID{e.AttemptID}}
		s.ModelAttempts[e.AttemptID] = Attempt{ID: e.AttemptID, InvocationID: e.InvocationID, StepID: st.ID, State: AttemptPlanned, Usage: UsageFact{Status: UsageUnknown}}
		st.ModelID = e.InvocationID
	case EventModelDispatchStarted:
		inv, ok := s.Models[e.InvocationID]
		if !ok || !dispatchable(s) || !validID(e.Ref) || !validDispatchFence(s, e) || inv.StepID != currentStepID(*s) {
			return reject(RejectStale)
		}
		att, ok := s.ModelAttempts[e.AttemptID]
		if !ok || att.State != AttemptPlanned || att.InvocationID != inv.ID || att.StepID != inv.StepID || !contains(inv.AttemptIDs, e.AttemptID) || hasActiveAttempt(*s) {
			return reject(RejectConflict)
		}
		att.State, att.Fence, att.Token, att.Incarnation = AttemptRequesting, e.Fence, e.Token, e.Incarnation
		s.ModelAttempts[e.AttemptID] = att
		inv.State = InvocationActive
		s.Models[e.InvocationID] = inv
	case EventModelStreamStarted:
		inv, ok := s.Models[e.InvocationID]
		att, aok := s.ModelAttempts[e.AttemptID]
		if !ok || !aok || !callbackMatches(*s, att, e) || att.State != AttemptRequesting || att.InvocationID != inv.ID || att.StepID != inv.StepID || inv.StepID != currentStepID(*s) {
			return reject(RejectStale)
		}
		att.State = AttemptStreaming
		s.ModelAttempts[e.AttemptID] = att
	case EventModelResponseRecorded:
		inv, ok := s.Models[e.InvocationID]
		att, aok := s.ModelAttempts[e.AttemptID]
		if !ok || !aok || !callbackMatches(*s, att, e) || !isActiveModel(att.State) || att.InvocationID != inv.ID || att.StepID != inv.StepID || inv.StepID != currentStepID(*s) || !validID(e.Ref) || !validUsageFact(e.Usage) || (e.Usage != UsageUnknown && !validID(e.CauseRef)) || inv.ResponseRef != "" {
			return reject(RejectStale)
		}
		att.State, att.TerminalRef, att.Usage = AttemptCompleted, e.Ref, UsageFact{Status: e.Usage, Ref: e.CauseRef}
		s.ModelAttempts[e.AttemptID] = att
		inv.State, inv.SelectedAttempt, inv.ResponseRef, inv.Usage = InvocationCompleted, e.AttemptID, e.Ref, att.Usage
		inv.CandidateIDs = inv.CandidateIDs[:0]
		var lastSource uint32
		firstCandidate := true
		for _, candidate := range e.Candidates {
			if !validID(candidate.ID) || !validID(candidate.AttemptID) || !validID(candidate.ArgsRef) || !validID(candidate.Descriptor) || contains(inv.CandidateIDs, candidate.ID) || anyAttemptExists(*s, candidate.AttemptID) || (!firstCandidate && candidate.SourceIndex <= lastSource) {
				return reject(RejectInvalid)
			}
			firstCandidate, lastSource = false, candidate.SourceIndex
			inv.CandidateIDs = append(inv.CandidateIDs, candidate.ID)
			inv.Candidates = append(inv.Candidates, candidate)
		}
		s.Models[e.InvocationID] = inv
		st := stepByID(s, inv.StepID)
		st.ResponseRef, st.SelectedAttempt = e.Ref, e.AttemptID
		st.Transcript = append(st.Transcript, TranscriptItem{InvocationID: inv.ID, ResponseRef: e.Ref, IsModel: true})
		s.Transcript = append(s.Transcript, TranscriptItem{InvocationID: inv.ID, ResponseRef: e.Ref, IsModel: true})
	case EventModelRequestFailed, EventModelRequestInterrupted:
		inv, ok := s.Models[e.InvocationID]
		att, aok := s.ModelAttempts[e.AttemptID]
		preDispatchFailure := e.Kind == EventModelRequestFailed && att.State == AttemptPlanned && e.Terminal && e.Token == 0 && e.Fence == 0
		recoveryInterrupted := e.Kind == EventModelRequestInterrupted && isActiveModel(att.State) && recoveryCallbackMatches(*s, att.Token, att.Incarnation, att.Fence, e)
		if !ok || !aok || att.InvocationID != inv.ID || att.StepID != inv.StepID || !contains(inv.AttemptIDs, att.ID) || (!preDispatchFailure && !recoveryInterrupted && (!callbackMatches(*s, att, e) || !isActiveModel(att.State))) || !validID(e.Ref) {
			return reject(RejectStale)
		}
		if !validUsageFact(e.Usage) {
			return reject(RejectInvalid)
		}
		att.PartialRef, att.FailureRef, att.Usage = e.CauseRef, e.Ref, UsageFact{Status: e.Usage, Ref: e.ReceiptRef}
		if e.Kind == EventModelRequestFailed {
			att.State = AttemptFailed
			if e.Terminal {
				inv.State, inv.FailureRef = InvocationFailed, e.Ref
			} else {
				inv.State = InvocationActive
			}
		} else {
			att.State = AttemptInterrupted
			att.Exposure = ExposurePossible
			s.Exposures[att.ID] = ExposurePossible
			s.Interruptions[e.CauseRef] = Interruption{ID: e.CauseRef, Kind: InterruptEffectUnknown, TargetID: att.ID}
		}
		att.TerminalRef = e.ID
		s.ModelAttempts[e.AttemptID], s.Models[e.InvocationID] = att, inv
	case EventModelRetryScheduled:
		inv, ok := s.Models[e.InvocationID]
		att, aok := s.ModelAttempts[e.PreviousAttemptID]
		if !ok || !aok || inv.State != InvocationActive || att.InvocationID != inv.ID || att.StepID != inv.StepID || !contains(inv.AttemptIDs, e.PreviousAttemptID) || (att.State != AttemptFailed && att.State != AttemptInterrupted && att.State != AttemptAbandoned) || !validID(e.TimerID) || !validID(e.Ref) {
			return reject(RejectConflict)
		}
		if err := scheduleTimer(s, e, e.PreviousAttemptID); err != nil {
			return err
		}
	case EventModelAttemptPlanned:
		inv, ok := s.Models[e.InvocationID]
		prev, pok := s.ModelAttempts[e.PreviousAttemptID]
		if !ok || !pok || inv.State != InvocationActive || prev.InvocationID != inv.ID || prev.StepID != inv.StepID || !contains(inv.AttemptIDs, e.PreviousAttemptID) || (prev.State != AttemptFailed && prev.State != AttemptInterrupted && prev.State != AttemptAbandoned) || !validID(e.AttemptID) || anyAttemptExists(*s, e.AttemptID) {
			return reject(RejectConflict)
		}
		timer, timerOK := s.Timers[e.TimerID]
		if !timerOK || timer.Purpose != TimerModelRetry || timer.TargetID != e.PreviousAttemptID || !consumeElapsedTimer(s, e.TimerID, e.Generation) {
			return reject(RejectStale)
		}
		if !dispatchable(s) {
			return reject(RejectConflict)
		}
		inv.AttemptIDs = append(inv.AttemptIDs, e.AttemptID)
		s.Models[e.InvocationID] = inv
		s.ModelAttempts[e.AttemptID] = Attempt{ID: e.AttemptID, InvocationID: inv.ID, StepID: inv.StepID, State: AttemptPlanned, Usage: UsageFact{Status: UsageUnknown}}
	case EventModelAttemptCancelled:
		att, ok := s.ModelAttempts[e.AttemptID]
		inv, iok := s.Models[e.InvocationID]
		predispatch := att.State == AttemptPlanned && e.Token == 0 && e.Fence == 0
		if !ok || !iok || att.InvocationID != inv.ID || !contains(inv.AttemptIDs, att.ID) || (!predispatch && (!callbackMatches(*s, att, e) || !isActiveModel(att.State) || !e.Terminal)) || (att.State != AttemptPlanned && !isActiveModel(att.State)) || !validID(e.Ref) {
			return reject(RejectStale)
		}
		att.State, att.TerminalRef = AttemptCancelled, e.Ref
		s.ModelAttempts[e.AttemptID] = att
		inv.State, inv.Disposition = InvocationCancelled, ResultCancelled
		s.Models[e.InvocationID] = inv
	case EventModelAttemptAbandoned:
		inv, ok := s.Models[e.InvocationID]
		att, aok := s.ModelAttempts[e.AttemptID]
		metadataClose := s.ClosureConfirmed && closureModelMatches(*s, e) && e.Next == NextEnd && e.Exposure == ExposureUnknown
		if !ok || !aok || att.InvocationID != inv.ID || !contains(inv.AttemptIDs, att.ID) || (att.State != AttemptInterrupted && att.State != AttemptFailed) || !validID(e.Ref) || !validID(e.ReceiptRef) || e.Exposure != ExposureUnknown || e.Next != NextContinue && !metadataClose {
			return reject(RejectConflict)
		}
		att.State, att.Exposure, att.TerminalRef = AttemptAbandoned, ExposureUnknown, e.Ref
		s.ModelAttempts[e.AttemptID] = att
		s.Exposures[att.ID] = ExposureUnknown
		if metadataClose {
			inv.State, inv.Disposition, inv.DispositionRef = InvocationAbandoned, ResultAbandonedUnknown, e.Ref
		} else {
			resolveInterruptionTarget(s, att.ID, false)
		}
		s.Models[e.InvocationID] = inv
	case EventModelInvocationFailed:
		inv, ok := s.Models[e.InvocationID]
		if !ok || inv.State != InvocationFailed || !validID(e.Ref) {
			return reject(RejectConflict)
		}
		inv.FailureRef, inv.Disposition = e.Ref, ResultFailed
		s.Models[e.InvocationID] = inv
	case EventModelUsageReconciled:
		att, ok := s.ModelAttempts[e.AttemptID]
		inv, iok := s.Models[e.InvocationID]
		if !ok || !iok || att.InvocationID != inv.ID || !contains(inv.AttemptIDs, att.ID) || e.Usage != UsageKnown || !validID(e.Ref) || (att.State != AttemptInterrupted && att.State != AttemptAbandoned && att.State != AttemptFailed && att.State != AttemptCompleted) {
			return reject(RejectConflict)
		}
		att.Usage = UsageFact{Status: UsageKnown, Ref: e.Ref}
		s.ModelAttempts[e.AttemptID] = att
		if inv.SelectedAttempt == att.ID {
			inv.Usage = att.Usage
			s.Models[inv.ID] = inv
		}
	case EventBudgetObserved:
		if !validUsageStatus(e.Usage) || !validID(e.Ref) {
			return reject(RejectInvalid)
		}
		if e.WindowID != "" {
			window, ok := s.Windows[e.WindowID]
			if !ok || !window.Open || contains(window.ObservedRefs, e.Ref) {
				return reject(RejectStale)
			}
			window.ObservedRefs = append(window.ObservedRefs, e.Ref)
			s.Windows[e.WindowID] = window
		}
		if e.AttemptID == "" && e.WindowID != "" {
			break
		}
		if att, ok := s.ModelAttempts[e.AttemptID]; ok {
			inv, exists := s.Models[e.InvocationID]
			if !exists || att.InvocationID != inv.ID || !contains(inv.AttemptIDs, att.ID) {
				return reject(RejectStale)
			}
			att.Usage = UsageFact{Status: e.Usage, Ref: e.Ref}
			s.ModelAttempts[e.AttemptID] = att
			if inv.SelectedAttempt == att.ID {
				inv.Usage = att.Usage
				s.Models[e.InvocationID] = inv
			}
		} else if tool, ok := s.ToolAttempts[e.AttemptID]; ok {
			if e.ToolID == "" || tool.ToolID != e.ToolID || e.InvocationID != "" {
				return reject(RejectStale)
			}
			tool.Usage = UsageFact{Status: e.Usage, Ref: e.Ref}
			s.ToolAttempts[e.AttemptID] = tool
		} else {
			return reject(RejectStale)
		}
	case EventToolCallPlanned:
		inv, ok := s.Models[e.InvocationID]
		st := currentStep(s)
		if !ok || st == nil || inv.State != InvocationCompleted || st.ModelID != inv.ID || st.ResponseRef == "" || !validID(e.ToolID) || !validID(e.AttemptID) || !validID(e.Ref) || !validID(e.CauseRef) || idExists(s.Tools, e.ToolID) || anyAttemptExists(*s, e.AttemptID) || !contains(inv.CandidateIDs, e.ToolID) {
			return reject(RejectConflict)
		}
		candidate, found := candidateByID(s, e.InvocationID, e.ToolID)
		if !found || !validEffectClass(candidate.EffectClass) || candidate.AttemptID != e.AttemptID || candidate.ArgsRef != e.CauseRef || candidate.SourceIndex != e.SourceIndex || candidate.Descriptor != e.Ref {
			return reject(RejectConflict)
		}
		effectClass := candidate.EffectClass
		if effectClass == EffectUnspecified {
			effectClass = EffectSideEffecting
		}
		s.Tools[e.ToolID] = ToolInvocation{ID: e.ToolID, StepID: st.ID, State: ToolPlanned, Descriptor: e.Ref, EffectClass: effectClass, SourceIndex: e.SourceIndex, RawArgsRef: candidate.ArgsRef, ArgsRef: candidate.ArgsRef, AttemptIDs: []ID{e.AttemptID}, Exposure: ExposureNone}
		s.ToolAttempts[e.AttemptID] = ToolAttempt{ID: e.AttemptID, ToolID: e.ToolID, StepID: st.ID, State: AttemptPlanned, ArgsRef: candidate.ArgsRef, Usage: UsageFact{Status: UsageUnknown}}
		st.ToolIDs = append(st.ToolIDs, e.ToolID)
	case EventToolCheckStarted:
		tool, ok := s.Tools[e.ToolID]
		if !ok || !dispatchable(s) || tool.StepID != currentStepID(*s) || !isFrontierTool(*s, tool.ID) || tool.State != ToolPlanned || e.Generation <= tool.CheckGeneration || !validID(e.Ref) {
			return reject(RejectConflict)
		}
		tool.State, tool.CheckGeneration = ToolChecking, e.Generation
		s.Tools[e.ToolID] = tool
	case EventToolCheckReset:
		tool, ok := s.Tools[e.ToolID]
		if !ok || (tool.State != ToolChecking && tool.State != ToolAwaitingApproval && tool.State != ToolReady) || e.Generation <= tool.CheckGeneration || !validID(e.CauseRef) {
			return reject(RejectStale)
		}
		if tool.State == ToolAwaitingApproval {
			blocker, found := blockerForTool(*s, tool.ID, BlockApproval)
			if !found || e.BlockerID != blocker.ID || e.RequestRef != tool.RequestRef || !validID(e.ReceiptRef) {
				return reject(RejectStale)
			}
			delete(s.Blockers, blocker.ID)
		} else if e.BlockerID != "" {
			return reject(RejectInvalid)
		}
		tool.State, tool.CheckGeneration, tool.RequestRef, tool.ApprovalRef, tool.ResolutionRef, tool.PlanArgsRef, tool.PlanArgsGeneration = ToolPlanned, e.Generation, "", "", "", "", 0
		tool.ArgsRef = tool.RawArgsRef
		if len(tool.AttemptIDs) > 0 {
			att := s.ToolAttempts[tool.AttemptIDs[len(tool.AttemptIDs)-1]]
			att.ArgsRef = tool.RawArgsRef
			s.ToolAttempts[att.ID] = att
		}
		s.Tools[e.ToolID] = tool
	case EventToolApprovalRequested:
		tool, ok := s.Tools[e.ToolID]
		if !ok || tool.State != ToolChecking || e.Generation != tool.CheckGeneration || !validID(e.CauseRef) || !validID(e.RequestRef) || !validID(e.BlockerID) {
			return reject(RejectConflict)
		}
		if _, exists := s.Blockers[e.BlockerID]; exists {
			return reject(RejectConflict)
		}
		tool.State, tool.RequestRef, tool.PlanArgsRef, tool.PlanArgsGeneration = ToolAwaitingApproval, e.RequestRef, e.CauseRef, e.Generation
		s.Tools[e.ToolID] = tool
		s.Blockers[e.BlockerID] = Blocker{ID: e.BlockerID, Kind: BlockApproval, Ref: e.RequestRef, TargetID: tool.ID, Revision: e.Revision, Generation: e.Generation}
	case EventToolApprovalResolved:
		tool, ok := s.Tools[e.ToolID]
		if !ok {
			return reject(RejectStale)
		}
		blocker, bok := blockerForTool(*s, tool.ID, BlockApproval)
		if !ok || !bok || tool.State != ToolAwaitingApproval || e.Generation != tool.CheckGeneration || e.RequestRef != tool.RequestRef || e.CauseRef != tool.PlanArgsRef || tool.PlanArgsGeneration != e.Generation || !validID(e.ReceiptRef) {
			return reject(RejectStale)
		}
		delete(s.Blockers, blocker.ID)
		switch e.Disposition {
		case ResultSucceeded:
			tool.ApprovalRef = e.ReceiptRef
			tool.State = ToolChecking
		case ResultDenied, ResultRefused:
			tool.Disposition = e.Disposition
			tool.State = toolStateFor(e.Disposition)
			tool.ResolutionRef = e.ReceiptRef
		default:
			return reject(RejectInvalid)
		}
		s.Tools[e.ToolID] = tool
	case EventToolReadyRecorded:
		tool, ok := s.Tools[e.ToolID]
		if !ok || tool.State != ToolChecking || tool.StepID != currentStepID(*s) || !isFrontierTool(*s, tool.ID) || e.Generation != tool.CheckGeneration || !validID(e.Ref) || len(tool.AttemptIDs) == 0 || (tool.PlanArgsRef != "" && (e.Ref != tool.PlanArgsRef || tool.PlanArgsGeneration != e.Generation)) || (tool.RequestRef != "" && (tool.ApprovalRef == "" || e.ReceiptRef != tool.ApprovalRef)) {
			return reject(RejectConflict)
		}
		tool.PlanArgsRef, tool.PlanArgsGeneration = e.Ref, e.Generation
		tool.State, tool.ArgsRef = ToolReady, e.Ref
		s.Tools[e.ToolID] = tool
		att := s.ToolAttempts[tool.AttemptIDs[len(tool.AttemptIDs)-1]]
		att.ArgsRef = e.Ref
		s.ToolAttempts[att.ID] = att
	case EventToolDispatchStarted:
		tool, ok := s.Tools[e.ToolID]
		if !ok || !dispatchable(s) || tool.StepID != currentStepID(*s) || !isFrontierTool(*s, tool.ID) || tool.State != ToolReady || tool.PlanArgsRef == "" || tool.ArgsRef != tool.PlanArgsRef || tool.PlanArgsGeneration != tool.CheckGeneration || e.Generation != tool.CheckGeneration || tool.RequestRef != "" && e.ReceiptRef != tool.ApprovalRef || !validDispatchFence(s, e) || !validID(e.ReceiptRef) || !validID(e.Ref) || hasActiveAttempt(*s) {
			return reject(RejectStale)
		}
		att := s.ToolAttempts[e.AttemptID]
		if att.ID == "" || att.State != AttemptPlanned || att.ToolID != tool.ID || att.StepID != tool.StepID || !contains(tool.AttemptIDs, att.ID) {
			return reject(RejectConflict)
		}
		att.State, att.Fence, att.Token, att.Incarnation = AttemptRequesting, e.Fence, e.Token, e.Incarnation
		tool.State, tool.SelectedAttempt = ToolDispatching, att.ID
		s.ToolAttempts[att.ID], s.Tools[tool.ID] = att, tool
	case EventToolResultRecorded:
		tool, ok := s.Tools[e.ToolID]
		att, aok := s.ToolAttempts[e.AttemptID]
		acceptedClose := ok && aok && tool.State == ToolUnknown && (att.State == AttemptInterrupted || att.State == AttemptAbandoned) && e.Disposition == ResultAbandonedUnknown && (acceptedUnknownFor(*s, tool.ID) || closureMatchesUnknown(*s, e))
		predispatch := ok && aok && att.State == AttemptPlanned && (tool.State == ToolPlanned || tool.State == ToolChecking || tool.State == ToolReady || tool.State == ToolAwaitingApproval || tool.State == ToolDenied || tool.State == ToolRefused) && e.Token == 0 && e.Fence == 0
		ownerProof := tool.State == ToolPlanned || e.PreviewMode != ClosurePreviewMetadataOnly || validID(e.OwnerEvidenceRef) && e.RequestRef == tool.RequestRef && e.Generation == tool.CheckGeneration
		predispatchCancel := predispatch && e.Disposition == ResultCancelled && e.Terminal && validID(e.ReceiptRef) && ownerProof
		predispatchRefusal := predispatch && (e.Disposition == ResultDenied || e.Disposition == ResultRefused) && validID(e.ReceiptRef) && ((tool.ResolutionRef == "" && tool.Disposition == ResultNone) || (tool.ResolutionRef == e.ReceiptRef && tool.Disposition == e.Disposition && e.Generation == tool.CheckGeneration))
		reconciledResult := ok && aok && tool.State == ToolUnknown && (att.State == AttemptInterrupted || att.State == AttemptAbandoned) && tool.ReconciliationRef != "" && e.ReceiptRef == tool.ReconciliationRef && e.Disposition == tool.ReconciliationDisposition
		if !ok || !aok || att.ToolID != tool.ID || att.StepID != tool.StepID || !contains(tool.AttemptIDs, att.ID) || (!predispatchCancel && !predispatchRefusal && !acceptedClose && !reconciledResult && tool.SelectedAttempt != att.ID) || (!acceptedClose && !predispatchCancel && !predispatchRefusal && !reconciledResult && (!callbackMatchesTool(*s, att, e) || att.State != AttemptRequesting)) || !validID(e.Ref) || !validResultDisposition(e.Disposition) || !validExposure(e.Exposure) || tool.ResultRef != "" {
			return reject(RejectStale)
		}
		switch e.Disposition {
		case ResultAbandonedUnknown:
			if !acceptedUnknownFor(*s, tool.ID) && !closureMatchesUnknown(*s, e) {
				return reject(RejectConflict)
			}
		}
		att.State = AttemptCompleted
		if predispatchCancel || predispatchRefusal {
			if e.Disposition == ResultCancelled {
				att.State = AttemptCancelled
			} else {
				att.State = AttemptFailed
			}
		}
		if acceptedClose {
			att.State = AttemptAbandoned
		}
		att.TerminalRef, att.Exposure, att.ResultRef = e.ID, e.Exposure, e.Ref
		tool.ResultRef, tool.Disposition, tool.Exposure, tool.State = e.Ref, e.Disposition, e.Exposure, toolStateFor(e.Disposition)
		if tool.State == ToolDenied || tool.State == ToolRefused || tool.State == ToolCancelled {
			if blocker, found := blockerForTool(*s, tool.ID, BlockApproval); found {
				delete(s.Blockers, blocker.ID)
			}
		}
		s.ToolAttempts[att.ID], s.Tools[tool.ID] = att, tool
		appendToolTranscript(s, &tool, e.Disposition, e.Ref)
		if reconciledResult {
			resolveInterruptionTarget(s, tool.ID, false)
		}
		if e.Exposure != ExposureNone {
			s.Exposures[tool.ID] = e.Exposure
		}
		s.Tools[tool.ID] = tool
	case EventToolOutcomeUnknown:
		tool, ok := s.Tools[e.ToolID]
		att, aok := s.ToolAttempts[e.AttemptID]
		recoveryUnknown := aok && (att.State == AttemptRequesting || att.State == AttemptStreaming) && recoveryCallbackMatches(*s, att.Token, att.Incarnation, att.Fence, e)
		if !ok || !aok || att.ToolID != tool.ID || att.StepID != tool.StepID || !contains(tool.AttemptIDs, att.ID) || tool.SelectedAttempt != att.ID || (!recoveryUnknown && !callbackMatchesTool(*s, att, e)) || att.State != AttemptRequesting && att.State != AttemptStreaming || !validID(e.CauseRef) || e.Exposure != ExposureUnknown {
			return reject(RejectStale)
		}
		att.State, att.Exposure, att.TerminalRef = AttemptInterrupted, ExposureUnknown, e.CauseRef
		tool.State, tool.Exposure = ToolUnknown, ExposureUnknown
		s.ToolAttempts[att.ID], s.Tools[tool.ID] = att, tool
		s.Exposures[tool.ID] = ExposureUnknown
		s.Interruptions[e.CauseRef] = Interruption{ID: e.CauseRef, Kind: InterruptEffectUnknown, TargetID: tool.ID}
	case EventToolReconciled:
		tool, ok := s.Tools[e.ToolID]
		att, aok := s.ToolAttempts[e.AttemptID]
		if s.Finished || !ok || !aok || att.ToolID != tool.ID || !contains(tool.AttemptIDs, att.ID) || tool.SelectedAttempt != att.ID || att.State != AttemptInterrupted && att.State != AttemptAbandoned || tool.State != ToolUnknown || (e.Disposition != ResultSucceeded && e.Disposition != ResultFailed && e.Disposition != ResultCancelled) || !validID(e.Ref) {
			return reject(RejectConflict)
		}
		tool.ReconciliationRef, tool.ReconciliationDisposition = e.Ref, e.Disposition
		s.Tools[e.ToolID] = tool
	case EventToolAttemptCancelled:
		tool, ok := s.Tools[e.ToolID]
		att, aok := s.ToolAttempts[e.AttemptID]
		if !ok || !aok || att.ToolID != tool.ID || !contains(tool.AttemptIDs, att.ID) || tool.SelectedAttempt != att.ID || !callbackMatchesTool(*s, att, e) || att.State != AttemptRequesting && att.State != AttemptStreaming || !e.Terminal || !validID(e.Ref) {
			return reject(RejectStale)
		}
		att.State, att.TerminalRef = AttemptCancelled, e.Ref
		tool.State, tool.Disposition, tool.ResultRef = ToolCancelled, ResultCancelled, e.Ref
		s.ToolAttempts[att.ID], s.Tools[tool.ID] = att, tool
		appendToolTranscript(s, &tool, ResultCancelled, e.Ref)
		s.Tools[tool.ID] = tool
	case EventToolAttemptAbandoned:
		tool, ok := s.Tools[e.ToolID]
		att, aok := s.ToolAttempts[e.AttemptID]
		if !ok || !aok || att.ToolID != tool.ID || !contains(tool.AttemptIDs, att.ID) || tool.SelectedAttempt != att.ID || tool.State != ToolUnknown || att.State != AttemptInterrupted || e.Exposure != ExposureUnknown || !validID(e.Ref) || !validID(e.ReceiptRef) || (e.Next != NextContinue && e.Next != NextEnd) || e.Next == NextContinue && tool.EffectClass != EffectPureRead {
			return reject(RejectConflict)
		}
		if e.Next == NextEnd {
			intr, found := s.Interruptions[e.CauseRef]
			if !found || intr.TargetID != tool.ID || intr.Resolved || e.Actor != ActorUser || !validID(e.CauseRef) {
				return reject(RejectStale)
			}
			intr.Accepted, intr.Disposition = true, RecoveryDispositionAcceptedUnknown
			s.Interruptions[e.CauseRef] = intr
		} else {
			resolveInterruptionTarget(s, tool.ID, false)
		}
		att.State, att.TerminalRef = AttemptAbandoned, e.Ref
		tool.Exposure = ExposureUnknown
		s.ToolAttempts[att.ID], s.Tools[tool.ID] = att, tool
	case EventToolAttemptPlanned:
		tool, ok := s.Tools[e.ToolID]
		prev, pok := s.ToolAttempts[e.PreviousAttemptID]
		timer, timerOK := s.Timers[e.TimerID]
		if !ok || !pok || prev.ToolID != tool.ID || prev.StepID != tool.StepID || !contains(tool.AttemptIDs, prev.ID) || tool.State != ToolUnknown || prev.State != AttemptInterrupted && prev.State != AttemptAbandoned || !validID(e.AttemptID) || !validID(e.ReceiptRef) || anyAttemptExists(*s, e.AttemptID) || !dispatchable(s) || tool.EffectClass != EffectPureRead || !timerOK || timer.Purpose != TimerToolRetry || timer.TargetID != tool.ID || !consumeElapsedTimer(s, e.TimerID, e.Generation) {
			return reject(RejectConflict)
		}
		tool.AttemptIDs = append(tool.AttemptIDs, e.AttemptID)
		tool.State = ToolPlanned
		s.Tools[tool.ID] = tool
		s.ToolAttempts[e.AttemptID] = ToolAttempt{ID: e.AttemptID, ToolID: tool.ID, StepID: tool.StepID, State: AttemptPlanned, ArgsRef: tool.ArgsRef, Usage: UsageFact{Status: UsageUnknown}, Exposure: ExposureNone}
	case EventRunStopRequested:
		if s.Finished || (e.StopMode != StopTerminal && e.StopMode != StopSuspend) || e.StopReason < StopUserCancel || e.StopReason > StopShutdown || !validActor(e.Actor) || !stopActorCompatible(e.StopReason, e.Actor) || !validID(e.Ref) {
			return reject(RejectConflict)
		}
		if (e.StopReason == StopShutdown) != (e.StopMode == StopSuspend) {
			return reject(RejectInvalid)
		}
		if e.StopReason == StopBudget {
			if e.StopMode != StopTerminal {
				return reject(RejectInvalid)
			}
			if e.BudgetKind < BudgetSteps || e.BudgetKind > BudgetConcurrency {
				return reject(RejectInvalid)
			}
			if e.BudgetKind == BudgetSteps && (e.Actor != ActorUser || !stepBudgetWaiting(*s)) {
				return reject(RejectConflict)
			}
			if e.BudgetKind != BudgetSteps && e.Actor != ActorWorker && e.Actor != ActorSystem && e.Actor != ActorTimer {
				return reject(RejectInvalid)
			}
		}
		intent := StopIntent{Reason: e.StopReason, Mode: e.StopMode, Ref: e.Ref, Actor: e.Actor, BudgetKind: e.BudgetKind}
		if e.StopMode == StopSuspend {
			if e.StopReason != StopShutdown || !validID(e.BlockerID) {
				return reject(RejectInvalid)
			}
			s.Suspended, s.Quiescing = true, e.Terminal
			s.Blockers[e.BlockerID] = Blocker{ID: e.BlockerID, Kind: BlockHostPaused, Ref: e.Ref}
		} else if s.PrimaryStop.Reason == StopReasonNone {
			s.PrimaryStop = intent
			if e.StopReason == StopBudget {
				s.BudgetStopKind, s.BudgetStopActor = e.BudgetKind, e.Actor
			}
		} else {
			s.SecondaryStops = append(s.SecondaryStops, intent)
		}
	case EventRunBlockAdded:
		if s.Finished || !validID(e.BlockerID) || e.BlockKind < BlockApproval || e.BlockKind > BlockRecoveryAdmission || e.BlockKind == BlockStepBudget || !validID(e.Ref) {
			return reject(RejectInvalid)
		}
		if _, ok := s.Blockers[e.BlockerID]; ok {
			return reject(RejectConflict)
		}
		s.Blockers[e.BlockerID] = Blocker{ID: e.BlockerID, Kind: e.BlockKind, Ref: e.Ref, TargetID: e.ToolID, Revision: e.Revision, Generation: e.Generation}
	case EventRunBlockCleared:
		b, ok := s.Blockers[e.BlockerID]
		if !ok || !validID(e.CauseRef) || (e.Revision != 0 && e.Revision != b.Revision) || (e.Generation != 0 && e.Generation != b.Generation) {
			return reject(RejectStale)
		}
		delete(s.Blockers, e.BlockerID)
		if b.Kind == BlockHostPaused {
			s.Suspended = false
			s.Quiescing = false
		}
	case EventRunInterrupted:
		if s.Finished || !validID(e.CauseRef) || e.Interrupt < InterruptEffectUnknown || e.Interrupt > InterruptUsageUnknown || !validID(e.ToolID) {
			return reject(RejectInvalid)
		}
		if prior, ok := s.Interruptions[e.CauseRef]; ok {
			if prior.Kind == e.Interrupt && prior.TargetID == e.ToolID {
				break
			}
			return reject(RejectConflict)
		}
		s.Interruptions[e.CauseRef] = Interruption{ID: e.CauseRef, Kind: e.Interrupt, TargetID: e.ToolID}
	case EventRunRecoveryCleared:
		intr, ok := s.Interruptions[e.CauseRef]
		if !ok || intr.Resolved || !validID(e.Ref) || !validID(e.ReceiptRef) || !validActor(e.Actor) || e.RecoveryDisposition < RecoveryDispositionReconciled || e.RecoveryDisposition > RecoveryDispositionClosedUnavailable {
			return reject(RejectStale)
		}
		if (e.Accepted != (e.RecoveryDisposition == RecoveryDispositionAcceptedUnknown)) || (e.RecoveryDisposition == RecoveryDispositionAcceptedUnknown && e.Actor != ActorUser) || (e.RecoveryDisposition == RecoveryDispositionClosedUnavailable && e.Actor != ActorUser) {
			return reject(RejectInvalid)
		}
		intr.Disposition = e.RecoveryDisposition
		if e.RecoveryDisposition == RecoveryDispositionAcceptedUnknown {
			intr.Accepted = true
		} else {
			intr.Resolved = true
		}
		s.Interruptions[e.CauseRef] = intr
	case EventRunClosureConfirmed:
		causeEvent, causeFound := s.EventIDs[e.CauseEventRef]
		contextInvalid := false
		if s.Admitted {
			expectedRequirement := s.ContextRequirement
			if e.UnavailableReason == UnavailableVaultLocked {
				if expectedRequirement == ContextRequirementNone {
					expectedRequirement = ContextRequirementRevalidationRequired
				}
			} else if e.UnavailableReason == UnavailablePayloadMissing || e.UnavailableReason == UnavailablePayloadCorrupt {
				expectedRequirement = ContextRequirementFreshRequired
			} else {
				return reject(RejectInvalid)
			}
			contextInvalid = e.PreviousContextRevision != s.ContextRevision || e.ContextRevision != e.PreviousContextRevision+1 || e.ContextRevision == 0 || e.ContextRequirement != expectedRequirement
		} else {
			contextInvalid = e.PreviousContextRevision != s.ContextRevision || e.ContextRevision != s.ContextRevision || e.ContextRequirement != s.ContextRequirement
		}
		causeInvalid := !causeFound || causeEvent.RunID != s.RunID || !validClosureCauseEvent(causeEvent.Kind)
		if s.Finished || s.ClosureConfirmed || e.Actor != ActorUser || !validID(e.Ref) || !validID(e.ReceiptRef) || e.Revision != s.Revision || hasActiveAttempt(*s) || e.PreviewMode != ClosurePreviewMetadataOnly || e.PreviewVersion != CurrentClosurePreviewVersion || e.UnavailableReason < UnavailableVaultLocked || e.UnavailableReason > UnavailablePayloadCorrupt || contextInvalid || !validID(e.CauseEventRef) || causeInvalid || e.RecoveryDisposition != RecoveryDispositionUnspecified || e.Accepted || e.ToolID != "" || e.AttemptID != "" {
			return reject(RejectConflict)
		}
		s.ClosureConfirmed, s.ClosureRef = true, e.Ref
		s.ClosureToolID, s.ClosureAttemptID, s.ClosureCauseRef = "", "", e.CauseEventRef
		s.ClosureAcceptedUnknown, s.ClosureReceipt = false, e.ReceiptRef
		s.ClosurePreviewMode, s.ClosurePreviewVersion, s.ClosureUnavailableReason = e.PreviewMode, e.PreviewVersion, e.UnavailableReason
		if e.ClosureTargets == nil {
			s.ClosureTargets = nil
		} else {
			s.ClosureTargets = append(make([]ClosureTargetFact, 0, len(e.ClosureTargets)), e.ClosureTargets...)
		}
		s.ContextRevision, s.ContextRequirement = e.ContextRevision, e.ContextRequirement
		for id, blocker := range s.Blockers {
			if blocker.Kind == BlockRecoveryAdmission || e.UnavailableReason == UnavailableVaultLocked && blocker.Kind == BlockVaultLocked {
				delete(s.Blockers, id)
			}
		}
	case EventUnknownAccepted:
		if e.IsModel {
			intr, ok := s.Interruptions[e.CauseRef]
			inv, invOK := s.Models[e.InvocationID]
			att, attOK := s.ModelAttempts[e.AttemptID]
			if !ok || !invOK || !attOK || att.InvocationID != inv.ID || !contains(inv.AttemptIDs, att.ID) || len(inv.AttemptIDs) == 0 || inv.AttemptIDs[len(inv.AttemptIDs)-1] != att.ID || att.State != AttemptInterrupted || intr.TargetID != att.ID || intr.Resolved || e.Actor != ActorUser || e.Disposition != ResultAbandonedUnknown || e.PreviewMode != ClosurePreviewExact && e.PreviewMode != ClosurePreviewMetadataOnly || e.PreviewVersion != CurrentClosurePreviewVersion || !validID(e.Ref) || !validID(e.ReceiptRef) {
				return reject(RejectStale)
			}
			intr.Accepted = true
			intr.Disposition = RecoveryDispositionAcceptedUnknown
			s.Interruptions[e.CauseRef] = intr
			att.State, att.Exposure, att.TerminalRef = AttemptAbandoned, ExposureUnknown, e.Ref
			s.ModelAttempts[att.ID] = att
			inv.State, inv.Disposition, inv.DispositionRef = InvocationAbandoned, ResultAbandonedUnknown, e.Ref
			s.Models[inv.ID] = inv
			s.Exposures[att.ID] = ExposureUnknown
			break
		}
		intr, ok := s.Interruptions[e.CauseRef]
		tool, toolOK := s.Tools[e.ToolID]
		att, attemptOK := s.ToolAttempts[e.AttemptID]
		if !ok || !toolOK || !attemptOK || att.ToolID != tool.ID || !contains(tool.AttemptIDs, att.ID) || tool.SelectedAttempt != att.ID || tool.State != ToolUnknown || intr.Resolved || intr.TargetID != e.ToolID || e.Actor != ActorUser || e.PreviewMode != ClosurePreviewExact || e.PreviewVersion != CurrentClosurePreviewVersion || !validID(e.Ref) || !validID(e.ReceiptRef) || e.Disposition != ResultAbandonedUnknown {
			return reject(RejectStale)
		}
		intr.Accepted = true
		intr.Disposition = RecoveryDispositionAcceptedUnknown
		s.Interruptions[e.CauseRef] = intr
	case EventStepCompleted:
		st := currentStep(s)
		if st == nil || !st.Open || st.ModelID == "" || st.ResponseRef == "" || hasPendingUnknownClose(*s) || (e.Next != NextContinue && e.Next != NextEnd && e.Next != NextWait) || !validContinuation(e.Continuation) || !stepToolsTerminal(*s, *st) {
			return reject(RejectConflict)
		}
		if continuationNext(e.Continuation) != e.Next {
			return reject(RejectConflict)
		}
		if e.Continuation == ContinuationStepBudget && s.StepsStarted < effectiveStepLimit(*s) {
			return reject(RejectConflict)
		}
		if e.Next == NextContinue && s.StepsStarted >= effectiveStepLimit(*s) {
			return reject(RejectConflict)
		}
		st.Open, st.Next, st.Continuation = false, e.Next, e.Continuation
		inv := s.Models[st.ModelID]
		if inv.ID != "" && inv.State == InvocationCompleted {
			inv.Disposition = ResultSucceeded
			s.Models[inv.ID] = inv
		}
	case EventStepAborted:
		st := currentStep(s)
		if st == nil || !st.Open || hasActiveAttempt(*s) || !validID(e.Ref) {
			return reject(RejectConflict)
		}
		st.Open, st.Aborted, st.Next = false, true, NextEnd
		if st.ModelID != "" {
			inv := s.Models[st.ModelID]
			if inv.State == InvocationCompleted && inv.ResponseRef != "" {
				inv.Disposition = ResultSucceeded
				s.Models[inv.ID] = inv
			}
		}
	case EventRunFinished:
		if s.Finished || !finishAllowed(*s, e.Outcome) || (e.Truncated != (s.CurrentStep >= 0 && s.Steps[s.CurrentStep].Continuation == ContinuationTruncatedEnd)) {
			return reject(RejectConflict)
		}
		s.Finished, s.Outcome, s.Truncated = true, e.Outcome, e.Truncated
	case EventBudgetExtended:
		if s.Finished || s.PrimaryStop.Mode == StopTerminal || e.Actor != ActorUser || e.BudgetKind != BudgetSteps || e.BudgetBy == 0 || !stepBudgetWaiting(*s) {
			return reject(RejectConflict)
		}
		if s.StepBudgetExtended > ^uint64(0)-s.InitialStepLimit || e.BudgetBy > ^uint64(0)-s.InitialStepLimit-s.StepBudgetExtended {
			return reject(RejectInvalid)
		}
		s.StepBudgetExtended += e.BudgetBy
	case EventTimerScheduled:
		target := e.ToolID
		if e.Purpose == TimerRunDeadline {
			target = e.WindowID
		}
		if err := scheduleTimer(s, e, target); err != nil {
			return err
		}
	case EventTimerElapsed:
		if !consumeTimer(s, e.TimerID, e.Generation) || !validID(e.CauseRef) {
			return reject(RejectStale)
		}
		t := s.Timers[e.TimerID]
		t.Elapsed = true
		s.Timers[e.TimerID] = t
	case EventTimerCancelled:
		t, ok := s.Timers[e.TimerID]
		if !ok || !t.Pending || t.Generation != e.Generation || !validID(e.CauseRef) {
			return reject(RejectStale)
		}
		t.Pending = false
		s.Timers[e.TimerID] = t
	case EventTimeBudgetWindowOpened:
		if !s.Admitted || s.Finished || s.PrimaryStop.Mode == StopTerminal || hasOpenWindow(*s) || !validID(e.WindowID) || idExists(s.Windows, e.WindowID) || !validID(e.Incarnation) || e.Incarnation != s.Incarnation || e.Generation == 0 || e.Usage != UsageReserved || !validID(e.TimerID) {
			return reject(RejectConflict)
		}
		s.Windows[e.WindowID] = TimeWindow{ID: e.WindowID, Generation: e.Generation, Incarnation: e.Incarnation, Open: true, Reserved: e.Usage, TimerID: e.TimerID}
	case EventTimeBudgetWindowClosed:
		w, ok := s.Windows[e.WindowID]
		if !ok || !w.Open || w.TimerID != e.TimerID || w.Generation != e.Generation || !validID(e.Ref) {
			return reject(RejectStale)
		}
		w.Open, w.Covered = false, e.Covered
		s.Windows[e.WindowID] = w
	default:
		return reject(RejectInvalid)
	}
	return nil
}
