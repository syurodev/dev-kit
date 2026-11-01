package kernel

import (
	"reflect"
	"sort"
)

func closureEvents(state State, c Command, event func(uint32, EventKind) Event) ([]Event, error) {
	x := c.Closure
	metadata := c.Kind == CommandConfirmRunClosure
	if !validID(x.PreviewRef) || !validID(x.ReceiptRef) || x.PreviewVersion != CurrentClosurePreviewVersion || x.Revision != state.Revision || x.Actor != ActorUser || (!metadata && len(x.Targets) == 0) {
		return nil, reject(RejectInvalid)
	}
	if len(x.Targets) > 1 && !strictClosureOrder(x.Targets) {
		return nil, reject(RejectInvalid)
	}
	previewMode := ClosurePreviewExact
	if metadata {
		previewMode = ClosurePreviewMetadataOnly
		if x.UnavailableReason < UnavailableVaultLocked || x.UnavailableReason > UnavailablePayloadCorrupt || !validID(x.CauseEventRef) {
			return nil, reject(RejectInvalid)
		}
	} else if x.UnavailableReason != UnavailableNone || x.CauseEventRef != "" || x.ContextRequirement != ContextRequirementNone || x.PreviousContextRevision != 0 || x.ContextRevision != 0 {
		return nil, reject(RejectInvalid)
	}
	if hasActiveAttempt(state) {
		return nil, reject(RejectConflict)
	}
	out := make([]Event, 0, len(x.Targets)*3+3)
	ordinal := uint32(1)
	if metadata {
		header := event(ordinal, EventRunClosureConfirmed)
		header.Ref, header.ReceiptRef, header.Actor, header.Revision = x.PreviewRef, x.ReceiptRef, ActorUser, x.Revision
		header.PreviewMode, header.PreviewVersion, header.UnavailableReason = ClosurePreviewMetadataOnly, x.PreviewVersion, x.UnavailableReason
		header.PreviousContextRevision, header.ContextRevision, header.ContextRequirement, header.CauseEventRef = x.PreviousContextRevision, x.ContextRevision, x.ContextRequirement, x.CauseEventRef
		if x.Targets != nil {
			header.ClosureTargets = append(make([]ClosureTargetFact, 0, len(x.Targets)), x.Targets...)
		}
		out, ordinal = append(out, header), ordinal+1
	}
	unknown := false
	for _, target := range x.Targets {
		if target.OwnerEvidenceRef != "" && target.OwnerEvidenceRef == x.ReceiptRef {
			return nil, reject(RejectInvalid)
		}
		if err := validateClosureTarget(state, target, metadata); err != nil {
			return nil, err
		}
		switch target.Kind {
		case ClosureTargetUnknownTool:
			unknown = true
			if !metadata {
				a := event(ordinal, EventUnknownAccepted)
				a.ToolID, a.AttemptID, a.CauseRef, a.Ref, a.ReceiptRef, a.Actor, a.Disposition = target.TargetID, target.AttemptID, target.CauseRef, x.PreviewRef, x.ReceiptRef, ActorUser, ResultAbandonedUnknown
				a.ResultRef = target.ResultRef
				a.PreviewMode, a.PreviewVersion, a.Revision = previewMode, x.PreviewVersion, x.Revision
				out, ordinal = append(out, a), ordinal+1
			}
			r := event(ordinal, EventToolResultRecorded)
			r.ToolID, r.AttemptID, r.CauseRef, r.Ref, r.ReceiptRef, r.Disposition, r.Exposure = target.TargetID, target.AttemptID, target.CauseRef, target.ResultRef, x.ReceiptRef, ResultAbandonedUnknown, ExposureUnknown
			out, ordinal = append(out, r), ordinal+1
			out = append(out, closureRecoveryClear(event(ordinal, EventRunRecoveryCleared), target, x, RecoveryDispositionAcceptedUnknown))
			ordinal++
		case ClosureTargetUnknownModel:
			unknown = true
			if metadata {
				a := event(ordinal, EventModelAttemptAbandoned)
				a.InvocationID, a.AttemptID, a.CauseRef, a.Ref, a.ReceiptRef, a.Exposure, a.Next = target.TargetID, target.AttemptID, target.CauseRef, x.PreviewRef, x.ReceiptRef, ExposureUnknown, NextEnd
				out, ordinal = append(out, a), ordinal+1
			} else {
				a := event(ordinal, EventUnknownAccepted)
				a.InvocationID, a.AttemptID, a.CauseRef, a.Ref, a.ReceiptRef, a.Actor, a.Disposition, a.IsModel = target.TargetID, target.AttemptID, target.CauseRef, x.PreviewRef, x.ReceiptRef, ActorUser, ResultAbandonedUnknown, true
				a.PreviewMode, a.PreviewVersion, a.Revision = previewMode, x.PreviewVersion, x.Revision
				out, ordinal = append(out, a), ordinal+1
			}
			out = append(out, closureRecoveryClear(event(ordinal, EventRunRecoveryCleared), target, x, RecoveryDispositionAcceptedUnknown))
			ordinal++
		case ClosureTargetUnavailableCause:
			out = append(out, closureRecoveryClear(event(ordinal, EventRunRecoveryCleared), target, x, RecoveryDispositionClosedUnavailable))
			ordinal++
		case ClosureTargetPlannedTool:
			r := event(ordinal, EventToolResultRecorded)
			r.ToolID, r.AttemptID, r.Ref, r.ReceiptRef, r.Disposition, r.Exposure, r.Terminal = target.TargetID, target.AttemptID, x.ReceiptRef, x.ReceiptRef, ResultCancelled, ExposureNone, true
			r.OwnerEvidenceRef, r.RequestRef, r.Generation = target.OwnerEvidenceRef, target.RequestRef, target.Generation
			r.PreviewMode, r.PreviewVersion = ClosurePreviewMetadataOnly, x.PreviewVersion
			out, ordinal = append(out, r), ordinal+1
		case ClosureTargetPlannedModel:
			r := event(ordinal, EventModelAttemptCancelled)
			r.InvocationID, r.AttemptID, r.Ref, r.ReceiptRef, r.Terminal = target.TargetID, target.AttemptID, x.ReceiptRef, x.ReceiptRef, true
			out, ordinal = append(out, r), ordinal+1
		}
	}
	blockerIDs := make([]ID, 0, len(state.Blockers))
	for id := range state.Blockers {
		blockerIDs = append(blockerIDs, id)
	}
	sort.Slice(blockerIDs, func(i, j int) bool { return blockerIDs[i] < blockerIDs[j] })
	for _, id := range blockerIDs {
		blocker := state.Blockers[id]
		if metadata || blocker.Kind != BlockRecoveryAdmission {
			continue
		}
		clear := event(ordinal, EventRunBlockCleared)
		clear.BlockerID, clear.CauseRef, clear.Ref = id, x.PreviewRef, x.ReceiptRef
		clear.Revision, clear.Generation = blocker.Revision, blocker.Generation
		out, ordinal = append(out, clear), ordinal+1
	}
	if state.CurrentStep >= 0 && state.Steps[state.CurrentStep].Open {
		a := event(ordinal, EventStepAborted)
		a.Ref = x.ReceiptRef
		out, ordinal = append(out, a), ordinal+1
	}
	f := event(ordinal, EventRunFinished)
	unknown = unknown || hasUnknownClosureFact(state)
	if unknown {
		f.Outcome = OutcomeAbandonedUnknown
	} else {
		f.Outcome = OutcomeAbandoned
	}
	out = append(out, f)
	return out, nil
}

func closureRecoveryClear(e Event, target ClosureTargetFact, input ClosureInput, disposition RecoveryDisposition) Event {
	e.ToolID, e.AttemptID, e.CauseRef = "", target.AttemptID, target.CauseRef
	if target.Kind == ClosureTargetUnknownTool {
		e.ToolID = target.TargetID
	}
	if target.Kind == ClosureTargetUnknownModel {
		e.InvocationID = target.TargetID
	}
	e.Ref, e.ReceiptRef, e.Actor = input.PreviewRef, input.ReceiptRef, ActorUser
	e.RecoveryDisposition, e.Accepted = disposition, disposition == RecoveryDispositionAcceptedUnknown
	return e
}

func validateClosureTarget(s State, f ClosureTargetFact, metadata bool) error {
	if !validID(f.TargetID) {
		return reject(RejectInvalid)
	}
	if f.Kind != ClosureTargetPlannedTool && (f.OwnerEvidenceRef != "" || f.RequestRef != "" || f.Generation != 0) {
		return reject(RejectInvalid)
	}
	switch f.Kind {
	case ClosureTargetUnknownTool:
		tool, ok := s.Tools[f.TargetID]
		att, aok := s.ToolAttempts[f.AttemptID]
		intr, iok := s.Interruptions[f.CauseRef]
		cause, cok := s.EventIDs[f.CauseEventRef]
		if !ok || !aok || !iok || !cok || cause.Kind != EventRunInterrupted || cause.CauseRef != f.CauseRef || cause.ToolID != tool.ID || cause.RunID != s.RunID || tool.State != ToolUnknown || tool.SelectedAttempt != att.ID || att.ToolID != tool.ID || att.State != AttemptInterrupted && att.State != AttemptAbandoned || intr.TargetID != tool.ID || intr.Resolved || !validID(f.AttemptID) || !validID(f.CauseRef) || !validID(f.CauseEventRef) || !validID(f.ResultRef) {
			return reject(RejectStale)
		}
	case ClosureTargetUnknownModel:
		inv, ok := s.Models[f.TargetID]
		att, aok := s.ModelAttempts[f.AttemptID]
		intr, iok := s.Interruptions[f.CauseRef]
		cause, cok := s.EventIDs[f.CauseEventRef]
		if !ok || !aok || !iok || !cok || cause.Kind != EventRunInterrupted || cause.CauseRef != f.CauseRef || cause.ToolID != att.ID || cause.RunID != s.RunID || len(inv.AttemptIDs) == 0 || inv.AttemptIDs[len(inv.AttemptIDs)-1] != att.ID || att.InvocationID != inv.ID || att.State != AttemptInterrupted || intr.TargetID != att.ID || intr.Resolved || !validID(f.AttemptID) || !validID(f.CauseRef) || !validID(f.CauseEventRef) || f.ResultRef != "" {
			return reject(RejectStale)
		}
	case ClosureTargetUnavailableCause:
		if !metadata || !validPersistedClosureCause(s, f) || f.AttemptID != "" || f.ResultRef != "" {
			return reject(RejectStale)
		}
	case ClosureTargetPlannedTool:
		tool, ok := s.Tools[f.TargetID]
		att, aok := s.ToolAttempts[f.AttemptID]
		needsOwnerProof := ok && tool.State != ToolPlanned
		proofInvalid := false
		if needsOwnerProof {
			proofInvalid = (tool.State != ToolChecking && tool.State != ToolAwaitingApproval && tool.State != ToolReady) || !validID(f.OwnerEvidenceRef) || f.OwnerEvidenceRef == f.RequestRef || f.RequestRef != tool.RequestRef || f.Generation != tool.CheckGeneration
		} else {
			proofInvalid = f.OwnerEvidenceRef != "" || f.RequestRef != "" || f.Generation != 0
		}
		if !metadata || !ok || !aok || tool.State == ToolDispatching || tool.State == ToolUnknown || att.State != AttemptPlanned || att.ToolID != tool.ID || !contains(tool.AttemptIDs, att.ID) || f.CauseRef != "" || f.CauseEventRef != "" || f.ResultRef != "" || proofInvalid {
			return reject(RejectStale)
		}
	case ClosureTargetPlannedModel:
		inv, ok := s.Models[f.TargetID]
		att, aok := s.ModelAttempts[f.AttemptID]
		if !metadata || !ok || !aok || att.State != AttemptPlanned || att.InvocationID != inv.ID || !contains(inv.AttemptIDs, att.ID) || f.CauseRef != "" || f.CauseEventRef != "" || f.ResultRef != "" {
			return reject(RejectStale)
		}
	default:
		return reject(RejectInvalid)
	}
	return nil
}

func validPersistedClosureCause(s State, f ClosureTargetFact) bool {
	if !validID(f.CauseEventRef) {
		return false
	}
	event, eventOK := s.EventIDs[f.CauseEventRef]
	if !eventOK || event.RunID != s.RunID || !validClosureCauseEvent(event.Kind) || event.CauseRef != f.CauseRef && event.ID != f.CauseRef {
		return false
	}
	if intr, ok := s.Interruptions[f.CauseRef]; ok {
		return intr.Kind == InterruptPayloadUnavailable && !intr.Resolved && intr.TargetID == f.TargetID && event.Kind == EventRunInterrupted && event.CauseRef == f.CauseRef && event.Interrupt == InterruptPayloadUnavailable
	}
	if event.Kind == EventRunInterrupted && event.Interrupt != InterruptPayloadUnavailable {
		return false
	}
	if event.Kind == EventToolOutcomeUnknown || event.Kind == EventModelRequestInterrupted {
		return false
	}
	return event.ToolID == "" || event.ToolID == f.TargetID || event.InvocationID == f.TargetID || event.AttemptID == f.TargetID
}

func strictClosureOrder(targets []ClosureTargetFact) bool {
	causes := map[ID]bool{}
	type targetKey struct {
		kind            ClosureTargetKind
		target, attempt ID
	}
	targetsSeen := map[targetKey]bool{}
	for _, f := range targets {
		key := targetKey{f.Kind, f.TargetID, f.AttemptID}
		if targetsSeen[key] {
			return false
		}
		targetsSeen[key] = true
		if f.CauseRef != "" {
			if causes[f.CauseRef] {
				return false
			}
			causes[f.CauseRef] = true
		}
	}
	for i := 1; i < len(targets); i++ {
		p, n := targets[i-1], targets[i]
		if p.Kind > n.Kind || p.Kind == n.Kind && (p.TargetID > n.TargetID || p.TargetID == n.TargetID && (p.AttemptID > n.AttemptID || p.AttemptID == n.AttemptID && p.CauseRef >= n.CauseRef)) {
			return false
		}
	}
	return true
}

func validateClosureManifest(state State, batch Batch) error {
	events := batch.Events
	headerFound := false
	for _, header := range events {
		if header.Kind != EventRunClosureConfirmed || header.PreviewMode != ClosurePreviewMetadataOnly {
			continue
		}
		headerFound = true
		intent := batch.Intent.Closure
		if !canonicalReplayIntent(batch) || batch.Intent.Kind != CommandConfirmRunClosure || batch.Intent.ID != batch.CommandID || batch.Intent.ExpectedRevision != state.Revision || batch.Intent.RunID != state.RunID || batch.Intent.SessionID != state.SessionID || batch.Intent.WriterToken != batch.WriterToken || batch.Intent.WriterIncarnation != batch.WriterIncarnation || !reflect.DeepEqual(header.ClosureTargets, intent.Targets) || header.Ref != intent.PreviewRef || header.ReceiptRef != intent.ReceiptRef || header.PreviewVersion != intent.PreviewVersion || header.Revision != intent.Revision || header.Actor != intent.Actor || header.CauseEventRef != intent.CauseEventRef || header.UnavailableReason != intent.UnavailableReason || header.PreviousContextRevision != intent.PreviousContextRevision || header.ContextRevision != intent.ContextRevision || header.ContextRequirement != intent.ContextRequirement {
			return reject(RejectInvalid)
		}
		if events[0].ID != header.ID || events[len(events)-1].Kind != EventRunFinished || (events[len(events)-1].Outcome != OutcomeAbandoned && events[len(events)-1].Outcome != OutcomeAbandonedUnknown) || header.Actor != ActorUser || !validID(header.Ref) || !validID(header.ReceiptRef) || header.PreviewVersion != CurrentClosurePreviewVersion || !strictClosureOrder(header.ClosureTargets) || header.UnavailableReason < UnavailableVaultLocked || header.UnavailableReason > UnavailablePayloadCorrupt {
			return reject(RejectInvalid)
		}
		for _, e := range events {
			if e.Kind == EventRunBlockCleared {
				return reject(RejectInvalid)
			}
		}
		if state.CurrentStep >= 0 && state.Steps[state.CurrentStep].Open {
			if len(events) < 3 || events[len(events)-2].Kind != EventStepAborted {
				return reject(RejectInvalid)
			}
		} else if len(events) >= 2 && events[len(events)-2].Kind == EventStepAborted {
			return reject(RejectInvalid)
		}
		for _, target := range header.ClosureTargets {
			if err := validateClosureTarget(state, target, true); err != nil {
				return err
			}
		}
		if !closureTargetsCoverControl(state, header.ClosureTargets) {
			return reject(RejectInvalid)
		}
		if !closureTargetsCoverUnknownAccounting(state, header.ClosureTargets) {
			return reject(RejectConflict)
		}
		if !closureManifestEventsMatch(events, header) || !closureManifestOrderMatches(events, header) {
			return reject(RejectInvalid)
		}
		break
	}
	if batch.Intent.Kind == CommandConfirmRunClosure && !headerFound {
		return reject(RejectInvalid)
	}
	return nil
}

func canonicalReplayIntent(batch Batch) bool {
	canonical, err := canonicalCommand(batch.Intent)
	return err == nil && reflect.DeepEqual(canonical, batch.Intent)
}

// validateExactClosure makes user acceptance a complete, replay-verifiable
// terminal transaction. No older single-event acceptance frame is accepted.
func validateExactClosure(state State, batch Batch) error {
	events := batch.Events
	accepted := make([]Event, 0)
	for _, e := range events {
		if e.Kind == EventUnknownAccepted {
			accepted = append(accepted, e)
		}
	}
	if len(accepted) == 0 {
		if batch.Intent.Kind == CommandAcceptUnknownOutcome {
			return reject(RejectInvalid)
		}
		return nil
	}
	input := batch.Intent.Closure
	if !canonicalReplayIntent(batch) || batch.Intent.Kind != CommandAcceptUnknownOutcome || batch.Intent.ID != batch.CommandID || batch.Intent.ExpectedRevision != state.Revision || batch.Intent.RunID != state.RunID || batch.Intent.SessionID != state.SessionID || batch.Intent.WriterToken != batch.WriterToken || batch.Intent.WriterIncarnation != batch.WriterIncarnation || input.Revision != state.Revision || input.PreviewVersion != CurrentClosurePreviewVersion || input.Actor != ActorUser || !validID(input.PreviewRef) || !validID(input.ReceiptRef) || input.CauseEventRef != "" || input.UnavailableReason != UnavailableNone || input.PreviousContextRevision != 0 || input.ContextRevision != 0 || input.ContextRequirement != ContextRequirementNone || !reflect.DeepEqual(input.Targets, canonicalClosure(input).Targets) || len(input.Targets) != len(accepted) {
		return reject(RejectInvalid)
	}
	if state.Finished || state.ClosureConfirmed || hasActiveAttempt(state) || events[len(events)-1].Kind != EventRunFinished || events[len(events)-1].Outcome != OutcomeAbandonedUnknown {
		return reject(RejectInvalid)
	}
	if state.CurrentStep >= 0 && state.Steps[state.CurrentStep].Open {
		if len(events) < 2 || events[len(events)-2].Kind != EventStepAborted {
			return reject(RejectInvalid)
		}
	} else if len(events) >= 2 && events[len(events)-2].Kind == EventStepAborted {
		return reject(RejectInvalid)
	}
	facts := make([]ClosureTargetFact, 0, len(accepted))
	want := make([]EventKind, 0, len(events))
	firstPreview, firstReceipt := accepted[0].Ref, accepted[0].ReceiptRef
	for i, a := range accepted {
		if a.PreviewMode != ClosurePreviewExact || a.PreviewVersion != CurrentClosurePreviewVersion || a.Revision != state.Revision || a.Actor != ActorUser || !validID(a.Ref) || !validID(a.ReceiptRef) || a.Ref != firstPreview || a.ReceiptRef != firstReceipt || a.Disposition != ResultAbandonedUnknown {
			return reject(RejectInvalid)
		}
		fact := ClosureTargetFact{AttemptID: a.AttemptID, CauseRef: a.CauseRef}
		if i >= len(input.Targets) {
			return reject(RejectInvalid)
		}
		fact.CauseEventRef = input.Targets[i].CauseEventRef
		if a.IsModel {
			fact.Kind, fact.TargetID = ClosureTargetUnknownModel, a.InvocationID
		} else {
			fact.Kind, fact.TargetID = ClosureTargetUnknownTool, a.ToolID
			for _, candidate := range events {
				if candidate.Kind == EventToolResultRecorded && candidate.ToolID == fact.TargetID && candidate.AttemptID == fact.AttemptID && candidate.CauseRef == fact.CauseRef && candidate.Ref == a.ResultRef && candidate.ReceiptRef == a.ReceiptRef && candidate.Disposition == ResultAbandonedUnknown && candidate.Exposure == ExposureUnknown {
					fact.ResultRef = candidate.Ref
					break
				}
			}
		}
		if err := validateClosureTarget(state, fact, false); err != nil {
			return err
		}
		wantFact := input.Targets[i]
		if err := validateClosureTarget(state, wantFact, false); err != nil {
			return err
		}
		if fact.Kind != wantFact.Kind || fact.TargetID != wantFact.TargetID || fact.AttemptID != wantFact.AttemptID || fact.CauseRef != wantFact.CauseRef || fact.CauseEventRef != wantFact.CauseEventRef || fact.ResultRef != wantFact.ResultRef || a.Ref != input.PreviewRef || a.ReceiptRef != input.ReceiptRef || a.ResultRef != wantFact.ResultRef {
			return reject(RejectInvalid)
		}
		facts = append(facts, fact)
		want = append(want, EventUnknownAccepted)
		if fact.Kind == ClosureTargetUnknownTool {
			foundResult := false
			for _, e := range events {
				if e.Kind == EventToolResultRecorded && e.ToolID == fact.TargetID && e.AttemptID == fact.AttemptID && e.CauseRef == fact.CauseRef && e.ReceiptRef == a.ReceiptRef && e.Disposition == ResultAbandonedUnknown && e.Exposure == ExposureUnknown && validID(e.Ref) {
					foundResult = true
				}
			}
			if !foundResult {
				return reject(RejectInvalid)
			}
			want = append(want, EventToolResultRecorded)
		}
		foundClear := false
		for _, e := range events {
			if e.Kind == EventRunRecoveryCleared && e.AttemptID == fact.AttemptID && e.CauseRef == fact.CauseRef && e.ReceiptRef == a.ReceiptRef && e.RecoveryDisposition == RecoveryDispositionAcceptedUnknown && e.Accepted && (fact.Kind == ClosureTargetUnknownTool && e.ToolID == fact.TargetID || fact.Kind == ClosureTargetUnknownModel && e.InvocationID == fact.TargetID) {
				foundClear = true
			}
		}
		if !foundClear {
			return reject(RejectInvalid)
		}
		want = append(want, EventRunRecoveryCleared)
	}
	if !strictClosureOrder(facts) {
		return reject(RejectInvalid)
	}
	if !closureTargetsCoverUnknownAccounting(state, facts) {
		return reject(RejectConflict)
	}
	for _, intr := range state.Interruptions {
		if intr.Resolved || intr.Accepted {
			continue
		}
		if intr.Kind != InterruptEffectUnknown {
			return reject(RejectConflict)
		}
		covered := false
		for _, fact := range facts {
			covered = covered || fact.CauseRef == intr.ID
		}
		if !covered {
			return reject(RejectConflict)
		}
	}
	clearIDs := make([]ID, 0)
	for id, blocker := range state.Blockers {
		if blocker.Kind == BlockRecoveryAdmission {
			clearIDs = append(clearIDs, id)
		}
	}
	sort.Slice(clearIDs, func(i, j int) bool { return clearIDs[i] < clearIDs[j] })
	actualClearIDs := make([]ID, 0)
	for _, e := range events {
		if e.Kind == EventRunBlockCleared {
			actualClearIDs = append(actualClearIDs, e.BlockerID)
			want = append(want, e.Kind)
		}
	}
	if len(clearIDs) != len(actualClearIDs) {
		return reject(RejectInvalid)
	}
	for i := range clearIDs {
		if clearIDs[i] != actualClearIDs[i] {
			return reject(RejectInvalid)
		}
	}
	if state.CurrentStep >= 0 && state.Steps[state.CurrentStep].Open {
		want = append(want, EventStepAborted)
	}
	want = append(want, EventRunFinished)
	if len(events) != len(want) {
		return reject(RejectInvalid)
	}
	for i := range want {
		if events[i].Kind != want[i] {
			return reject(RejectInvalid)
		}
	}
	for _, e := range events {
		if e.Kind == EventRunBlockCleared {
			if state.Blockers[e.BlockerID].Kind != BlockRecoveryAdmission {
				return reject(RejectInvalid)
			}
		}
	}
	return nil
}

func closureTargetsCoverControl(s State, targets []ClosureTargetFact) bool {
	for _, intr := range s.Interruptions {
		if intr.Resolved || intr.Accepted {
			continue
		}
		covered := false
		for _, target := range targets {
			if target.CauseRef != intr.ID {
				continue
			}
			switch intr.Kind {
			case InterruptEffectUnknown:
				covered = covered || target.Kind == ClosureTargetUnknownTool && target.TargetID == intr.TargetID || target.Kind == ClosureTargetUnknownModel && target.AttemptID == intr.TargetID
			case InterruptPayloadUnavailable:
				covered = covered || target.Kind == ClosureTargetUnavailableCause && target.TargetID == intr.TargetID
			}
		}
		if !covered {
			return false
		}
	}
	for _, tool := range s.Tools {
		if tool.State != ToolPlanned && tool.State != ToolChecking && tool.State != ToolAwaitingApproval && tool.State != ToolReady {
			continue
		}
		if len(tool.AttemptIDs) == 0 {
			return false
		}
		attempt := tool.AttemptIDs[len(tool.AttemptIDs)-1]
		covered := false
		for _, target := range targets {
			covered = covered || target.Kind == ClosureTargetPlannedTool && target.TargetID == tool.ID && target.AttemptID == attempt
		}
		if !covered {
			return false
		}
	}
	for _, inv := range s.Models {
		if inv.State != InvocationPlanned {
			continue
		}
		if len(inv.AttemptIDs) == 0 {
			return false
		}
		attempt := inv.AttemptIDs[len(inv.AttemptIDs)-1]
		covered := false
		for _, target := range targets {
			covered = covered || target.Kind == ClosureTargetPlannedModel && target.TargetID == inv.ID && target.AttemptID == attempt
		}
		if !covered {
			return false
		}
	}
	return true
}

func closureTargetsCoverUnknownAccounting(s State, targets []ClosureTargetFact) bool {
	for _, att := range s.ModelAttempts {
		if att.Token == 0 || att.Usage.Status != UsageUnknown && att.Usage.Status != UsageReserved {
			continue
		}
		if hasAcceptedUnknownInterruptionFor(s, att.ID) {
			continue
		}
		covered := false
		for _, target := range targets {
			covered = covered || target.Kind == ClosureTargetUnknownModel && target.AttemptID == att.ID
		}
		if !covered {
			return false
		}
	}
	for _, att := range s.ToolAttempts {
		if att.Exposure != ExposureUnknown {
			continue
		}
		if tool, ok := s.Tools[att.ToolID]; ok && hasAcceptedUnknownInterruptionFor(s, tool.ID) {
			continue
		}
		covered := false
		for _, target := range targets {
			covered = covered || target.Kind == ClosureTargetUnknownTool && target.AttemptID == att.ID
		}
		if !covered {
			return false
		}
	}
	return true
}

func hasAcceptedUnknownInterruptionFor(s State, target ID) bool {
	for _, intr := range s.Interruptions {
		if intr.TargetID == target && intr.Accepted && intr.Disposition == RecoveryDispositionAcceptedUnknown {
			return true
		}
	}
	return false
}

func closureManifestHas(targets []ClosureTargetFact, target, attempt, cause ID, kind ClosureTargetKind) bool {
	for _, fact := range targets {
		if fact.Kind == kind && fact.TargetID == target && fact.AttemptID == attempt && fact.CauseRef == cause {
			return true
		}
	}
	return false
}

func closureManifestEventsMatch(events []Event, header Event) bool {
	used := 0
	for _, fact := range header.ClosureTargets {
		count := 0
		for _, e := range events {
			if e.Kind == EventRunClosureConfirmed {
				continue
			}
			switch fact.Kind {
			case ClosureTargetUnknownTool:
				if e.Kind == EventToolResultRecorded && e.ToolID == fact.TargetID && e.AttemptID == fact.AttemptID && e.CauseRef == fact.CauseRef && e.Ref == fact.ResultRef && e.ReceiptRef == header.ReceiptRef && e.Disposition == ResultAbandonedUnknown {
					count++
				}
				if e.Kind == EventRunRecoveryCleared && e.ToolID == fact.TargetID && e.AttemptID == fact.AttemptID && e.CauseRef == fact.CauseRef && e.RecoveryDisposition == RecoveryDispositionAcceptedUnknown {
					count++
				}
			case ClosureTargetUnknownModel:
				if e.Kind == EventModelAttemptAbandoned && e.InvocationID == fact.TargetID && e.AttemptID == fact.AttemptID && e.CauseRef == fact.CauseRef && e.ReceiptRef == header.ReceiptRef && e.Next == NextEnd {
					count++
				}
				if e.Kind == EventRunRecoveryCleared && e.InvocationID == fact.TargetID && e.AttemptID == fact.AttemptID && e.CauseRef == fact.CauseRef && e.RecoveryDisposition == RecoveryDispositionAcceptedUnknown {
					count++
				}
			case ClosureTargetUnavailableCause:
				if e.Kind == EventRunRecoveryCleared && e.AttemptID == fact.AttemptID && e.CauseRef == fact.CauseRef && e.RecoveryDisposition == RecoveryDispositionClosedUnavailable {
					count++
				}
			case ClosureTargetPlannedTool:
				if e.Kind == EventToolResultRecorded && e.ToolID == fact.TargetID && e.AttemptID == fact.AttemptID && e.Disposition == ResultCancelled && e.Terminal && e.ReceiptRef == header.ReceiptRef && e.OwnerEvidenceRef == fact.OwnerEvidenceRef && e.RequestRef == fact.RequestRef && e.Generation == fact.Generation {
					count++
				}
			case ClosureTargetPlannedModel:
				if e.Kind == EventModelAttemptCancelled && e.InvocationID == fact.TargetID && e.AttemptID == fact.AttemptID && e.Terminal && e.ReceiptRef == header.ReceiptRef {
					count++
				}
			}
		}
		want := 1
		if fact.Kind == ClosureTargetUnknownTool {
			want = 2
		}
		if fact.Kind == ClosureTargetUnknownModel {
			want = 2
		}
		if count != want {
			return false
		}
		used += want
	}
	actual := 0
	for _, e := range events {
		switch e.Kind {
		case EventUnknownAccepted, EventToolResultRecorded, EventRunRecoveryCleared, EventModelAttemptCancelled, EventModelAttemptAbandoned:
			actual++
		}
	}
	return actual == used
}

func closureManifestOrderMatches(events []Event, header Event) bool {
	want := []EventKind{EventRunClosureConfirmed}
	for _, fact := range header.ClosureTargets {
		switch fact.Kind {
		case ClosureTargetUnknownTool:
			want = append(want, EventToolResultRecorded, EventRunRecoveryCleared)
		case ClosureTargetUnknownModel:
			want = append(want, EventModelAttemptAbandoned, EventRunRecoveryCleared)
		case ClosureTargetUnavailableCause:
			want = append(want, EventRunRecoveryCleared)
		case ClosureTargetPlannedTool:
			want = append(want, EventToolResultRecorded)
		case ClosureTargetPlannedModel:
			want = append(want, EventModelAttemptCancelled)
		}
	}
	for _, e := range events {
		if e.Kind == EventStepAborted || e.Kind == EventRunFinished {
			want = append(want, e.Kind)
		}
	}
	actual := make([]EventKind, 0, len(events))
	for _, e := range events {
		if e.Kind == EventRunBlockCleared {
			continue
		}
		actual = append(actual, e.Kind)
		if e.Kind == EventStepAborted || e.Kind == EventRunFinished {
			continue
		}
	}
	if len(want) != len(actual) {
		return false
	}
	for i := range want {
		if actual[i] != want[i] {
			return false
		}
	}
	return true
}
