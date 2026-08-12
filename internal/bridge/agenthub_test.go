package bridge

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/app"
	"devkit/internal/storage"
)

func newBridgeWithAgentHub(t *testing.T) *AppService {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	container := app.NewContainer()
	reg := agenthub.NewRegistry()
	reg.Register(agenthub.NewMockAdapter())
	waiter := agenthub.NewPermissionWaiter(nil)
	svc := agenthub.NewService(reg, agenthub.NewSQLStore(db), nil)
	svc.ResolvePermission = waiter.Resolve
	container.AgentHub = svc
	container.AgentHubPermissions = waiter
	return NewAppService(container)
}

func TestAgentHubListEmpty(t *testing.T) {
	svc := newBridgeWithAgentHub(t)
	list, err := svc.AgentHubListAgents()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("%v", list)
	}
}

func TestAgentHubCreateMock(t *testing.T) {
	svc := newBridgeWithAgentHub(t)
	s, err := svc.AgentHubCreateSession(agenthub.CreateInput{AgentType: "mock", Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if s.ID == "" {
		t.Fatal("missing id")
	}
}

func TestAgentHubCreateAndInvokeMock(t *testing.T) {
	svc := newBridgeWithAgentHub(t)
	sess, err := svc.AgentHubCreateSession(agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	out, err := svc.AgentHubInvoke(context.Background(), agenthub.InvokeInput{
		SessionID: sess.ID,
		Prompt:    "hello",
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if out.Status != "completed" {
		t.Fatalf("status = %q, want completed", out.Status)
	}

	st, err := svc.AgentHubGetStatus(sess.ID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Status != agenthub.SessionStatusIdle {
		t.Fatalf("session status = %q", st.Status)
	}
}

func TestAgentHubListVendorTypes(t *testing.T) {
	svc := newBridgeWithAgentHub(t)
	types, err := svc.AgentHubListVendorTypes()
	if err != nil {
		t.Fatal(err)
	}
	if len(types) != 1 || types[0] != agenthub.AgentTypeMock {
		t.Fatalf("types = %v, want [mock]", types)
	}
}

func TestAgentHubBridgeNilServiceDoesNotPanic(t *testing.T) {
	container := app.NewContainer()
	container.AgentHub = nil
	service := NewAppService(container)

	_, err := service.AgentHubListAgents()
	if err == nil || err.Error() != "internal_error: Agent Hub is not available." {
		t.Fatalf("AgentHubListAgents error = %v, want internal_error unavailable", err)
	}
}

func TestAgentHubResolvePermissionNoPending(t *testing.T) {
	svc := newBridgeWithAgentHub(t)
	err := svc.AgentHubResolvePermission("0123456789abcdef01234567", json.RawMessage(`{"outcome":{"outcome":"cancelled"}}`))
	if err == nil {
		t.Fatal("expected error when no pending permission")
	}
}
