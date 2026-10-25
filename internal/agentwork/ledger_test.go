package agentwork

import (
	"errors"
	"testing"
)

func TestManagedAttemptTransitionContract(t *testing.T) {
	for _, edge := range [][2]string{{"reserved", "session_created"}, {"session_created", "task_assigned"}, {"task_assigned", "prompt_enqueued"}, {"prompt_enqueued", "running"}, {"running", "awaiting_review"}, {"running", "interrupted_unknown"}} {
		if err := CanTransitionAttempt(edge[0], edge[1]); err != nil {
			t.Errorf("%s -> %s: %v", edge[0], edge[1], err)
		}
	}
	for _, edge := range [][2]string{{"interrupted_unknown", "running"}, {"awaiting_review", "running"}, {"reserved", "accepted"}, {"running", "done"}, {"bogus", "bogus"}, {"running", "bogus"}} {
		if err := CanTransitionAttempt(edge[0], edge[1]); !errors.Is(err, ErrInvalidTransition) {
			t.Errorf("%s -> %s error=%v", edge[0], edge[1], err)
		}
	}
}

func TestManagedWorkScopeRequiresDistinctTaskAndParent(t *testing.T) {
	if err := ValidateScope("ws", "board", "parent", "task"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range [][4]string{{"", "board", "parent", "task"}, {"ws", "", "parent", "task"}, {"ws", "board", "", "task"}, {"ws", "board", "parent", ""}, {"ws", "board", "same", "same"}} {
		if err := ValidateScope(tc[0], tc[1], tc[2], tc[3]); err == nil {
			t.Errorf("ValidateScope(%q) accepted invalid scope", tc)
		}
	}
}
