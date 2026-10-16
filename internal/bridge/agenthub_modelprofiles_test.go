package bridge

import (
	"context"
	"strings"
	"testing"

	"devkit/internal/identity"
	"devkit/internal/preferences"
)

// The profiles steer what an agent spends, so they are the window's to edit:
// read and write go through grants the UI holds, check the revision, say what
// is wrong with a list in words, and refuse every other caller.
func TestModelProfilesBridgeIsUIOnlyAndChecksRevision(t *testing.T) {
	svc := newBridgeWithAgentHub(t)
	svc.container.Preferences = preferences.NewService(preferences.NewMemoryRepository())
	ctx := context.Background()

	got, err := svc.AgentHubGetModelProfiles(ctx)
	if err != nil || len(got.Profiles) != 0 || got.Revision != 0 {
		t.Fatalf("empty list = %+v, %v", got, err)
	}
	saved, err := svc.AgentHubSetModelProfiles(ctx, 0, []preferences.AgentModelProfile{
		{Name: "Code review", Description: "reading a lot of code", Vendor: "mock", Model: "m-small", Effort: "low"},
	})
	if err != nil || saved.Revision != 1 || len(saved.Profiles) != 1 || saved.Profiles[0].ID != "code-review" {
		t.Fatalf("saved = %+v, %v", saved, err)
	}
	if _, err := svc.AgentHubSetModelProfiles(ctx, 0, nil); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("stale revision: %v", err)
	}
	_, err = svc.AgentHubSetModelProfiles(ctx, 1, []preferences.AgentModelProfile{{Name: "A", Vendor: "mock", Model: "x"}, {Name: "a", Vendor: "mock", Model: "y"}})
	if err == nil || !strings.Contains(err.Error(), "two profiles are named") {
		t.Fatalf("duplicate names: %v", err)
	}
	checked, err := svc.AgentHubCheckModelProfiles(ctx)
	if err != nil || len(checked) != 1 || checked[0].Status == "" || checked[0].Status == "ok" {
		// The mock vendor has no model list to check against, which a profile
		// for it must say rather than pass.
		t.Fatalf("checked = %+v, %v", checked, err)
	}

	agent := identity.WithCaller(ctx, identity.Agent("11111111-1111-1111-1111-111111111111"))
	if _, err := svc.AgentHubGetModelProfiles(agent); err == nil {
		t.Fatal("an agent read the profiles through the window's method")
	}
	if _, err := svc.AgentHubSetModelProfiles(agent, 1, nil); err == nil {
		t.Fatal("an agent changed the profiles")
	}
	if _, err := svc.AgentHubCheckModelProfiles(agent); err == nil {
		t.Fatal("an agent used the window's check")
	}
}
