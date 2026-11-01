// Package kernel retry-batch guards for the durable model attempt protocol.
package kernel

import "reflect"

func validateRetryBatch(state State, batch Batch) error {
	hasRetryFact := false
	if len(batch.Events) > 0 {
		for _, event := range batch.Events {
			if event.Kind == EventModelRetryScheduled || event.Kind == EventTimerScheduled && event.Purpose == TimerModelRetry || event.Kind == EventModelAttemptPlanned {
				hasRetryFact = true
			}
		}
	}
	if batch.Intent.Kind == CommandScheduleModelRetry || hasRetryFact {
		if !hasRetryFact || batch.Intent.Kind != CommandScheduleModelRetry || len(batch.Events) != 3 || batch.Events[0].Kind != EventModelRetryScheduled || batch.Events[1].Kind != EventModelAttemptPlanned || batch.Events[2].Kind != EventTimerScheduled ||
			!canonicalRetryIntent(state, batch, CommandScheduleModelRetry, false) {
			return reject(RejectInvalid)
		}
	}
	for i, e := range batch.Events {
		if e.Kind != EventModelRetryScheduled {
			continue
		}
		if i+2 >= len(batch.Events) {
			return reject(RejectInvalid)
		}
		p, timer := batch.Events[i+1], batch.Events[i+2]
		if p.Kind != EventModelAttemptPlanned || timer.Kind != EventTimerScheduled ||
			p.InvocationID != e.InvocationID || p.PreviousAttemptID != e.PreviousAttemptID || p.AttemptID != e.AttemptID || p.TimerID != e.TimerID || p.Generation != e.Generation ||
			timer.InvocationID != e.InvocationID || timer.TimerID != e.TimerID || timer.BlockerID != e.BlockerID || timer.Ref != e.Ref || timer.Purpose != TimerModelRetry || timer.Generation != e.Generation || timer.DeadlineUnixMilli != e.DeadlineUnixMilli {
			return reject(RejectInvalid)
		}
	}
	cancelRetry := false
	for _, event := range batch.Events {
		if event.Kind == EventModelAttemptCancelled {
			attempt := state.ModelAttempts[event.AttemptID]
			cancelRetry = cancelRetry || attempt.RetryTimerID != ""
		}
	}
	stopIntent := batch.Intent.Kind == CommandRequestStop && batch.Intent.Stop.Mode == StopTerminal
	plannedRetry := false
	for _, attempt := range state.ModelAttempts {
		plannedRetry = plannedRetry || attempt.State == AttemptPlanned && attempt.RetryTimerID != ""
	}
	terminalStop := false
	for _, event := range batch.Events {
		terminalStop = terminalStop || event.Kind == EventRunStopRequested && event.StopMode == StopTerminal
	}
	if plannedRetry && (terminalStop || stopIntent || cancelRetry) {
		if !terminalStop || !cancelRetry || !canonicalRetryIntent(state, batch, CommandRequestStop, false) {
			return reject(RejectInvalid)
		}
	} else if cancelRetry && batch.Intent.Kind != CommandRequestStop {
		return reject(RejectInvalid)
	}
	for i, event := range batch.Events {
		if event.Kind == EventModelAttemptPlanned && (i != 1 || batch.Events[0].Kind != EventModelRetryScheduled || batch.Events[2].Kind != EventTimerScheduled) {
			return reject(RejectInvalid)
		}
		if event.Kind == EventTimerScheduled && event.Purpose == TimerModelRetry && (i != 2 || batch.Events[0].Kind != EventModelRetryScheduled || batch.Events[1].Kind != EventModelAttemptPlanned) {
			return reject(RejectInvalid)
		}
	}
	for _, event := range batch.Events {
		if event.Kind == EventModelAttemptCancelled {
			if attempt, ok := state.ModelAttempts[event.AttemptID]; ok && attempt.RetryTimerID != "" {
				found := false
				for _, candidate := range batch.Events {
					found = found || candidate.Kind == EventTimerCancelled && candidate.TimerID == attempt.RetryTimerID && candidate.Generation == attempt.RetryGeneration && candidate.CauseRef == event.Ref
				}
				if !found {
					return reject(RejectInvalid)
				}
			}
		}
		if event.Kind == EventTimerCancelled {
			if timer, ok := state.Timers[event.TimerID]; ok && timer.Purpose == TimerModelRetry {
				found := false
				for _, candidate := range batch.Events {
					if candidate.Kind == EventModelAttemptCancelled {
						a := state.ModelAttempts[candidate.AttemptID]
						found = found || a.RetryTimerID == event.TimerID && candidate.Ref == event.CauseRef
					}
				}
				if !found {
					return reject(RejectInvalid)
				}
			}
		}
	}
	return nil
}

func validateRetryRecoveryOwnership(state State, batch Batch) error {
	needed := false
	for _, attempt := range state.ModelAttempts {
		needed = needed || attempt.State == AttemptPlanned && attempt.RetryTimerID != ""
	}
	ownershipIntent := batch.Intent.Kind == CommandAcquireOwnership
	ownershipFact := false
	blockFact := false
	for _, event := range batch.Events {
		ownershipFact = ownershipFact || event.Kind == EventRunOwnershipAcquired
		blockFact = blockFact || event.Kind == EventRunBlockAdded && event.BlockKind == BlockRecoveryAdmission
	}
	if !ownershipIntent && !ownershipFact && !blockFact {
		return nil
	}
	if !needed {
		if ownershipIntent {
			if !ownershipFact || !canonicalRetryIntent(state, batch, CommandAcquireOwnership, true) {
				return reject(RejectInvalid)
			}
		}
		return nil
	}
	if !ownershipIntent && (ownershipFact || blockFact) {
		return reject(RejectInvalid)
	}
	if ownershipIntent && !ownershipFact {
		return reject(RejectInvalid)
	}
	hasBlock := false
	for _, blocker := range state.Blockers {
		hasBlock = hasBlock || blocker.Kind == BlockRecoveryAdmission
	}
	if hasBlock && (batch.Intent.Transition.BlockerID != "" || batch.Intent.Transition.Ref != "") {
		return reject(RejectInvalid)
	}
	if !hasBlock && !blockFact {
		return reject(RejectInvalid)
	}
	if hasBlock && blockFact {
		return reject(RejectInvalid)
	}
	if !canonicalRetryIntent(state, batch, CommandAcquireOwnership, true) {
		return reject(RejectInvalid)
	}
	return nil
}

func canonicalRetryIntent(state State, batch Batch, kind CommandKind, ownership bool) bool {
	if batch.Intent.Kind != kind || batch.CommandID != batch.Intent.ID || batch.ExpectedRevision != batch.Intent.ExpectedRevision {
		return false
	}
	canonical, err := canonicalCommand(batch.Intent)
	if err != nil || !reflect.DeepEqual(canonical, batch.Intent) {
		return false
	}
	if ownership {
		if batch.WriterToken != batch.Intent.Transition.ExecutorToken || batch.WriterIncarnation != batch.Intent.Transition.ExecutorIncarnation {
			return false
		}
	} else if batch.WriterToken != batch.Intent.WriterToken || batch.WriterIncarnation != batch.Intent.WriterIncarnation {
		return false
	}
	expected, err := commandEvents(state, batch.Intent)
	return err == nil && reflect.DeepEqual(expected, batch.Events)
}
