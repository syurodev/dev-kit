package agenthub

const MaxDepth = 3

func CheckDepth(parentDepth, max int) error {
	if parentDepth+1 > max {
		return ErrDepthLimitExceeded
	}
	return nil
}

// CheckCycle fails if targetID is equal to callerID or appears in caller's ancestor chain
// when interpreting "caller invokes target" — also fail if target's ancestors include caller
// (A→B→A). Implement: walk from target up parents; if hit callerID → cycle; walk from caller
// up; if hit targetID → cycle.
func CheckCycle(byID map[string]AgentSession, callerID, targetID string) error {
	if callerID == targetID {
		return ErrCycleDetected
	}
	if ancestorContains(byID, targetID, callerID) {
		return ErrCycleDetected
	}
	if ancestorContains(byID, callerID, targetID) {
		return ErrCycleDetected
	}
	return nil
}

// ancestorContains walks from startID up ParentSessionID links and reports whether needle appears.
func ancestorContains(byID map[string]AgentSession, startID, needle string) bool {
	seen := make(map[string]struct{})
	cur := startID
	for {
		if _, ok := seen[cur]; ok {
			return false
		}
		seen[cur] = struct{}{}
		s, ok := byID[cur]
		if !ok || s.ParentSessionID == nil {
			return false
		}
		parent := *s.ParentSessionID
		if parent == needle {
			return true
		}
		cur = parent
	}
}
