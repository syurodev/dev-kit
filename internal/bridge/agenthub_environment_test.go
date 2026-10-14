package bridge

import (
	"context"
	"devkit/internal/agenthub"
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
