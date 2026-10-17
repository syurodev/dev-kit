package bridge

import (
	"context"
	"strings"
	"testing"

	"devkit/internal/identity"
	"devkit/internal/preferences"
)

// The limits steer what an agent spends, so they are the window's: read and write
// go through grants the UI holds, check the revision, refuse nonsense in words,
// and refuse every other caller.
func TestSubagentLimitsBridgeIsUIOnlyAndChecksRevision(t *testing.T) {
	svc := newBridgeWithAgentHub(t)
	svc.container.Preferences = preferences.NewService(preferences.NewMemoryRepository())
	ctx := context.Background()

	got, err := svc.AgentHubGetSubagentLimits(ctx)
	if err != nil || got.Revision != 0 || got.CodexMaxSubagents != nil {
		t.Fatalf("unset = %+v, %v", got, err)
	}
	two := 2
	saved, err := svc.AgentHubSetSubagentLimits(ctx, 0, &two)
	if err != nil || saved.Revision != 1 || saved.CodexMaxSubagents == nil || *saved.CodexMaxSubagents != 2 {
		t.Fatalf("saved = %+v, %v", saved, err)
	}
	if _, err := svc.AgentHubSetSubagentLimits(ctx, 0, &two); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("stale revision: %v", err)
	}
	many := 40
	if _, err := svc.AgentHubSetSubagentLimits(ctx, 1, &many); err == nil || !strings.Contains(err.Error(), "at most") {
		t.Fatalf("40 subagents: %v", err)
	}
	cleared, err := svc.AgentHubSetSubagentLimits(ctx, 1, nil)
	if err != nil || cleared.CodexMaxSubagents != nil || cleared.Revision != 2 {
		t.Fatalf("cleared = %+v, %v", cleared, err)
	}

	agent := identity.WithCaller(ctx, identity.Agent("11111111-1111-1111-1111-111111111111"))
	if _, err := svc.AgentHubGetSubagentLimits(agent); err == nil {
		t.Fatal("an agent read the limits through the window's method")
	}
	if _, err := svc.AgentHubSetSubagentLimits(agent, 2, &two); err == nil {
		t.Fatal("an agent changed the limits")
	}
}
