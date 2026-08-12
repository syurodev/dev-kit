package agenthub_test

import (
	"errors"
	"testing"

	"devkit/internal/agenthub"
)

func strPtr(s string) *string { return &s }

func TestDepthLimit(t *testing.T) {
	err := agenthub.CheckDepth(3, 3) // parentDepth=3, max=3 → child would be 4
	if !errors.Is(err, agenthub.ErrDepthLimitExceeded) {
		t.Fatalf("got %v", err)
	}
}

func TestCycleDetected(t *testing.T) {
	// A parent of B parent of C; invoking A from C must fail
	sessions := map[string]agenthub.AgentSession{
		"a": {ID: "a"},
		"b": {ID: "b", ParentSessionID: strPtr("a")},
		"c": {ID: "c", ParentSessionID: strPtr("b")},
	}
	err := agenthub.CheckCycle(sessions, "c", "a")
	if !errors.Is(err, agenthub.ErrCycleDetected) {
		t.Fatalf("got %v", err)
	}
}

func TestNoCycleWhenTargetUnrelated(t *testing.T) {
	sessions := map[string]agenthub.AgentSession{
		"a": {ID: "a"},
		"b": {ID: "b"},
	}
	if err := agenthub.CheckCycle(sessions, "a", "b"); err != nil {
		t.Fatal(err)
	}
}

func TestCycleSelfInvoke(t *testing.T) {
	sessions := map[string]agenthub.AgentSession{
		"a": {ID: "a"},
	}
	err := agenthub.CheckCycle(sessions, "a", "a")
	if !errors.Is(err, agenthub.ErrCycleDetected) {
		t.Fatalf("got %v", err)
	}
}
