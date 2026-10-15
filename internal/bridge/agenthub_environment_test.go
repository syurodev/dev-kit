package bridge

import (
	"context"
	"devkit/internal/agenthub"
	"devkit/internal/identity"
	"devkit/internal/preferences"
	"testing"
)

func TestAgentHubEnvironmentDoesNotResumeAClosedSession(t *testing.T) {
	svc := newBridgeWithAgentHub(t)
	session, err := svc.AgentHubCreateSession(agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.AgentHubCloseSession(context.Background(), session.ID); err != nil {
		t.Fatal(err)
	}
	report, err := svc.AgentHubEnvironment(context.Background(), "ignored", session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if report.Scope != "next" || report.Agent != "mock" {
		t.Fatalf("report = %+v", report)
	}
	status, err := svc.AgentHubGetStatus(session.ID)
	if err != nil || status.Status != agenthub.SessionStatusClosed {
		t.Fatalf("inspection resumed the session: %+v, %v", status, err)
	}
}

func TestCustomizationBridgeIsUIOnlyAndChecksRevision(t *testing.T) {
	svc := newBridgeWithAgentHub(t)
	svc.container.Preferences = preferences.NewService(preferences.NewMemoryRepository())
	ctx := context.Background()
	value, err := svc.AgentHubSetCustomization(ctx, "codex", preferences.AgentCustomization{Servers: []preferences.AgentMCPConfig{{Name: "docs", URL: "https://example.com/mcp", Enabled: true}}})
	if err != nil || value.Revision != 1 {
		t.Fatal(value, err)
	}
	read, err := svc.AgentHubGetCustomization(ctx, "codex")
	if err != nil || len(read.Servers) != 1 {
		t.Fatal(read, err)
	}
	if _, err := svc.AgentHubSetCustomization(ctx, "codex", preferences.AgentCustomization{}); err == nil {
		t.Fatal("stale update accepted")
	}
	callerCtx := identity.WithCaller(ctx, identity.Agent("11111111-1111-1111-1111-111111111111"))
	if _, err := svc.AgentHubGetCustomization(callerCtx, "codex"); err == nil {
		t.Fatal("agent read customization")
	}
	if _, err := svc.AgentHubSetCustomization(callerCtx, "codex", value); err == nil {
		t.Fatal("agent modified customization")
	}
	if err := svc.AgentHubResumeSession(callerCtx, "11111111-1111-1111-1111-111111111111", true); err == nil {
		t.Fatal("agent used UI recovery")
	}
}
