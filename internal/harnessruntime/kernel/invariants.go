package kernel

// CheckInvariants checks SM-01 through SM-12 against one complete transaction
// state. It never inspects an intermediate prefix inside a batch.
func CheckInvariants(s State) error {
	fail := func() error { return reject(RejectInvariant) }

	// SM-01: one active model/tool attempt at the execution frontier.
	active := 0
	for _, a := range s.ModelAttempts {
		if isActiveModel(a.State) {
			active++
			if a.Token == 0 || a.Fence == 0 || a.Incarnation == "" || a.Token != s.AdmissionToken || a.Incarnation != s.Incarnation {
				return fail()
			}
		}
	}
	for _, a := range s.ToolAttempts {
		if a.State == AttemptRequesting || a.State == AttemptStreaming {
			active++
			if a.Token == 0 || a.Fence == 0 || a.Incarnation == "" || a.Token != s.AdmissionToken || a.Incarnation != s.Incarnation {
				return fail()
			}
		}
	}
	if active > 1 {
		return fail()
	}
	if s.Lifecycle != deriveLifecycle(s) {
		return fail()
	}

	// SM-02: approval wait and the exact tool/check generation are bijective.
	awaiting := map[ID]ToolInvocation{}
	for id, tool := range s.Tools {
		if id != tool.ID {
			return fail()
		}
		if tool.State == ToolAwaitingApproval {
			awaiting[id] = tool
			count := 0
			for _, b := range s.Blockers {
				if b.Kind == BlockApproval && b.TargetID == id && b.Ref == tool.RequestRef && b.Generation == tool.CheckGeneration {
					count++
				}
			}
			if count != 1 || tool.RequestRef == "" {
				return fail()
			}
		}
	}
	for _, b := range s.Blockers {
		if b.Kind == BlockApproval {
			tool, ok := awaiting[b.TargetID]
			if !ok || b.Ref != tool.RequestRef || b.Generation != tool.CheckGeneration {
				return fail()
			}
		}
	}

	// SM-03: approval alone is Waiting when there is no higher-precedence fact.
	if s.Accepted && s.Admitted && !s.Finished && !hasUnresolvedInterruption(s) && s.PrimaryStop.Mode != StopTerminal && !hasActiveAttempt(s) {
		if len(awaiting) > 0 && (s.Lifecycle != Waiting || !hasWaitReason(s, BlockApproval)) {
			return fail()
		}
	}

	// SM-04: phase is a projection of the committed frontier.
	if s.Phase != derivePhase(s) {
		return fail()
	}

	// SM-05: finished runs have terminal dispositions, closed/covered windows,
	// no known live work, and no pending timer/dependency authority.
	if s.Finished {
		if len(s.Blockers) != 0 || hasActiveAttempt(s) || hasOpenWindow(s) {
			return fail()
		}
		for _, step := range s.Steps {
			if step.Open {
				return fail()
			}
		}
		for _, t := range s.Timers {
			if t.Pending {
				return fail()
			}
		}
		for _, w := range s.Windows {
			if !w.Covered {
				return fail()
			}
		}
		for _, inv := range s.Models {
			if inv.Disposition == ResultNone {
				return fail()
			}
		}
		for _, tool := range s.Tools {
			if tool.Disposition == ResultNone {
				return fail()
			}
		}
	}
	// Active execution-time accounting is paired transactionally with exactly
	// one live tranche deadline; closed windows cannot leave runnable timers.
	for id, w := range s.Windows {
		if id != w.ID || len(w.ObservedRefs) != len(uniqueIDs(w.ObservedRefs)) {
			return fail()
		}
		timer, ok := s.Timers[w.TimerID]
		if !ok || timer.Purpose != TimerRunDeadline || timer.TargetID != w.ID || timer.Generation != w.Generation {
			return fail()
		}
		if w.Open != timer.Pending {
			return fail()
		}
	}

	// SM-06: interruption outranks cancelling; terminal intent is never erased.
	if hasUnresolvedInterruption(s) && !s.Finished && s.Lifecycle != Interrupted {
		return fail()
	}
	if s.PrimaryStop.Mode == StopTerminal && s.PrimaryStop.Reason == StopReasonNone {
		return fail()
	}

	// SM-07: completed steps have one canonical pair per candidate, in source order;
	// a following model can only belong to the next closed step.
	for _, st := range s.Steps {
		if !st.Open && !st.Aborted && st.ResponseRef != "" {
			if len(st.CompletedTools) != len(st.ToolIDs) {
				return fail()
			}
			for i, toolID := range st.ToolIDs {
				if st.CompletedTools[i] != toolID || transcriptPairCount(st.Transcript, toolID) != 1 || transcriptPairCount(s.Transcript, toolID) != 1 {
					return fail()
				}
			}
		}
		if !st.Open && st.Next == NextContinue && !stepToolsTerminal(s, st) {
			return fail()
		}
		if st.ResponseRef != "" {
			inv := s.Models[st.ModelID]
			if inv.ID == "" || inv.ResponseRef != st.ResponseRef || len(inv.Candidates) != len(st.ToolIDs) {
				return fail()
			}
			var source uint32
			firstCandidate := true
			for _, candidate := range inv.Candidates {
				if !contains(st.ToolIDs, candidate.ID) {
					return fail()
				}
				if !firstCandidate && candidate.SourceIndex <= source {
					return fail()
				}
				firstCandidate, source = false, candidate.SourceIndex
			}
		}
		if len(st.CompletedTools) != len(uniqueIDs(st.CompletedTools)) {
			return fail()
		}
		last := uint32(0)
		first := true
		for _, toolID := range st.CompletedTools {
			tool := s.Tools[toolID]
			if !first && tool.SourceIndex < last {
				return fail()
			}
			first, last = false, tool.SourceIndex
			if transcriptPairCount(st.Transcript, toolID) != 1 || transcriptPairCount(s.Transcript, toolID) != 1 {
				return fail()
			}
		}
	}
	if s.CurrentStep > 0 {
		for i := 0; i < s.CurrentStep; i++ {
			historicalBudgetWait := s.Steps[i].Next == NextWait && s.Steps[i].Continuation == ContinuationStepBudget && s.StepBudgetExtended > 0
			if s.Steps[i].Open || (s.Steps[i].Next != NextContinue && !historicalBudgetWait) {
				return fail()
			}
		}
	}

	// SM-08: each retry is a new attempt under one invocation; old attempt facts
	// remain addressable and cannot be replaced by a later attempt.
	for id, inv := range s.Models {
		if id != inv.ID || len(inv.AttemptIDs) == 0 || len(inv.AttemptIDs) != len(uniqueIDs(inv.AttemptIDs)) {
			return fail()
		}
		for _, attemptID := range inv.AttemptIDs {
			a, ok := s.ModelAttempts[attemptID]
			if !ok || a.ID != attemptID || a.InvocationID != inv.ID || a.StepID != inv.StepID {
				return fail()
			}
		}
	}
	for id, tool := range s.Tools {
		if id != tool.ID || len(tool.AttemptIDs) == 0 || len(tool.AttemptIDs) != len(uniqueIDs(tool.AttemptIDs)) {
			return fail()
		}
		if tool.SelectedAttempt != "" && !contains(tool.AttemptIDs, tool.SelectedAttempt) {
			return fail()
		}
		if tool.State == ToolSucceeded || tool.State == ToolFailed || tool.State == ToolDenied || tool.State == ToolRefused || tool.State == ToolCancelled || tool.State == ToolAbandoned {
			acceptedUnknown := tool.State == ToolAbandoned && tool.Disposition == ResultAbandonedUnknown && tool.ResultRef == "" && tool.ResolutionRef != "" && acceptedUnknownFor(s, tool.ID)
			if !acceptedUnknown && (tool.Disposition == ResultNone || tool.ResultRef == "" || tool.State != toolStateFor(tool.Disposition)) {
				return fail()
			}
		} else if tool.Disposition != ResultNone || tool.ResultRef != "" {
			return fail()
		}
		for _, attemptID := range tool.AttemptIDs {
			a, ok := s.ToolAttempts[attemptID]
			if !ok || a.ID != attemptID || a.ToolID != tool.ID || a.StepID != tool.StepID {
				return fail()
			}
		}
	}
	for id, a := range s.ModelAttempts {
		if id != a.ID || (a.Exposure == ExposureUnknown && s.Exposures[id] != ExposureUnknown) {
			return fail()
		}
	}

	// SM-09: every live callback target is fenced to current session ownership.
	// Applied command IDs make an exact duplicate idempotent; a conflicting reuse
	// is rejected by Evolve before it can append another result.
	for _, a := range s.ModelAttempts {
		if isActiveModel(a.State) && (a.Token != s.AdmissionToken || a.Incarnation != s.Incarnation) {
			return fail()
		}
	}
	for _, a := range s.ToolAttempts {
		if (a.State == AttemptRequesting || a.State == AttemptStreaming) && (a.Token != s.AdmissionToken || a.Incarnation != s.Incarnation) {
			return fail()
		}
	}

	// SM-11: accepting unknown to close preserves an explicit exposure fact.
	if s.Finished && s.Outcome == OutcomeAbandonedUnknown {
		if !hasUnknownClosureFact(s) {
			return fail()
		}
	}

	// SM-12: replaying an accepted but unadmitted run cannot create execution.
	if s.Accepted && !s.Admitted && !s.Finished {
		if s.Lifecycle != Queued || len(s.Steps) != 0 || hasActiveAttempt(s) || len(s.Windows) != 0 {
			return fail()
		}
		for _, a := range s.ModelAttempts {
			if a.State != AttemptPlanned {
				return fail()
			}
		}
		for _, a := range s.ToolAttempts {
			if a.State != AttemptPlanned {
				return fail()
			}
		}
	}

	return nil
}

func hasWaitReason(s State, kind BlockKind) bool {
	for _, reason := range s.WaitReasons {
		if reason == kind {
			return true
		}
	}
	return false
}

func uniqueIDs(in []ID) []ID {
	seen := map[ID]bool{}
	out := make([]ID, 0, len(in))
	for _, id := range in {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func transcriptPairCount(items []TranscriptItem, invocation ID) int {
	count := 0
	for _, item := range items {
		if !item.IsModel && item.InvocationID == invocation && item.ResultRef != "" {
			count++
		}
	}
	return count
}
