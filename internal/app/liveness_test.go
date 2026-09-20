package app

import (
	"context"
	"errors"
	"testing"

	"devkit/internal/agenthub"
)

type stubSessions map[string]agenthub.SessionStatus

func (s stubSessions) GetStatus(_ context.Context, id string) (agenthub.AgentSession, error) {
	status, ok := s[id]
	if !ok {
		return agenthub.AgentSession{}, errors.New("no such session")
	}
	return agenthub.AgentSession{ID: id, Status: status}, nil
}

// A claim is kept so an agent can reconnect to its own work, and can be taken
// over precisely when the holder is gone. Reading "gone" as alive would make
// the claim permanent, which is the failure this closes.
func TestAgentLivenessReadsTheSessionStatus(t *testing.T) {
	live := agentLiveness{hub: stubSessions{
		"running": agenthub.SessionStatusRunning,
		"idle":    agenthub.SessionStatusIdle,
		"closed":  agenthub.SessionStatusClosed,
		"failed":  agenthub.SessionStatusError,
	}}
	for id, want := range map[string]bool{
		"running": true,
		// Idle is alive: the agent is between turns, not gone.
		"idle":   true,
		"closed": false,
		"failed": false,
		// A session the hub cannot find is gone, not alive.
		"vanished": false,
		"":         false,
	} {
		if got := live.Alive(id); got != want {
			t.Errorf("Alive(%q) = %v, want %v", id, got, want)
		}
	}
}

// A nil hub must not report everything alive — that is what made every
// takeover impossible before this was wired.
func TestAgentLivenessWithoutAHubReportsNothingAlive(t *testing.T) {
	if (agentLiveness{}).Alive("anything") {
		t.Fatal("a nil hub reported a session alive")
	}
}
