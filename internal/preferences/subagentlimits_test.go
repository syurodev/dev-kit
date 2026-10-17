package preferences

import (
	"errors"
	"testing"
)

func limitOf(n int) CodexSubagentLimits { return CodexSubagentLimits{MaxSubagents: &n} }

func TestNormalizeSubagentLimits(t *testing.T) {
	if got, err := NormalizeSubagentLimits(CodexSubagentLimits{}); err != nil || got.MaxSubagents != nil {
		t.Fatalf("unset = %+v, %v", got, err)
	}
	for _, n := range []int{0, 1, 3, MaxCodexSubagentsCap} {
		if got, err := NormalizeSubagentLimits(limitOf(n)); err != nil || got.MaxSubagents == nil || *got.MaxSubagents != n {
			t.Errorf("%d = %+v, %v", n, got, err)
		}
	}
	for _, n := range []int{-1, MaxCodexSubagentsCap + 1, 1000} {
		if _, err := NormalizeSubagentLimits(limitOf(n)); !errors.Is(err, ErrInvalidSubagentLimits) {
			t.Errorf("%d: err = %v, want ErrInvalidSubagentLimits", n, err)
		}
	}
}

func TestSubagentLimitsAreStoredWithARevision(t *testing.T) {
	svc := NewService(NewMemoryRepository())
	if got, err := svc.AgentSubagentLimits(); err != nil || got.Revision != 0 || got.Codex.MaxSubagents != nil {
		t.Fatalf("unset = %+v, %v", got, err)
	}
	first, err := svc.SetAgentSubagentLimits(AgentSubagentLimits{Codex: limitOf(2)})
	if err != nil || first.Revision != 1 || *first.Codex.MaxSubagents != 2 {
		t.Fatalf("first = %+v, %v", first, err)
	}
	if _, err := svc.SetAgentSubagentLimits(AgentSubagentLimits{Revision: 0, Codex: limitOf(5)}); !errors.Is(err, ErrSubagentLimitsConflict) {
		t.Fatalf("stale write: %v", err)
	}
	// A refused value changes nothing, and does not use up a revision.
	if _, err := svc.SetAgentSubagentLimits(AgentSubagentLimits{Revision: 1, Codex: limitOf(99)}); !errors.Is(err, ErrInvalidSubagentLimits) {
		t.Fatalf("99: %v", err)
	}
	cleared, err := svc.SetAgentSubagentLimits(AgentSubagentLimits{Revision: 1})
	if err != nil || cleared.Revision != 2 || cleared.Codex.MaxSubagents != nil {
		t.Fatalf("cleared = %+v, %v", cleared, err)
	}
	got, _ := svc.AgentSubagentLimits()
	if got.Codex.MaxSubagents != nil {
		t.Fatalf("clearing kept %d", *got.Codex.MaxSubagents)
	}
	// What the caller holds is a copy.
	two, _ := svc.SetAgentSubagentLimits(AgentSubagentLimits{Revision: 2, Codex: limitOf(1)})
	*two.Codex.MaxSubagents = 9
	if again, _ := svc.AgentSubagentLimits(); *again.Codex.MaxSubagents != 1 {
		t.Fatalf("the stored value moved: %d", *again.Codex.MaxSubagents)
	}
}
