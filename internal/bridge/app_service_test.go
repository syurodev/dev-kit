package bridge

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"changeme/internal/app"
	"changeme/internal/capabilities"
	"changeme/internal/permissions"
)

func newTestService(t *testing.T) *AppService {
	t.Helper()
	container := app.NewContainer()
	if err := app.RegisterBuiltins(container); err != nil {
		t.Fatalf("RegisterBuiltins returned error: %v", err)
	}
	return NewAppService(container)
}

func TestAppServiceListsModules(t *testing.T) {
	service := newTestService(t)
	modules := service.ListModules()

	if len(modules) != 4 {
		t.Fatalf("len(modules) = %d, want 4", len(modules))
	}
	found := map[string]bool{}
	for _, m := range modules {
		found[m.ID] = true
	}
	if !found["tools"] || !found["notes"] || !found["database"] || !found["ssh"] {
		t.Fatalf("modules = %+v, want tools, notes, database and ssh", modules)
	}
}

func TestAppServiceExecutesHashCommand(t *testing.T) {
	service := newTestService(t)
	result, err := service.ExecuteCommand(context.Background(), ExecuteCommandRequest{
		ID: "tools.hash",
		Payload: map[string]any{
			"algorithm": "sha256",
			"input":     "hello",
		},
	})
	if err != nil {
		t.Fatalf("ExecuteCommand returned error: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
}

func TestAppServiceAuditsAllowedAndDeniedSensitiveCallsWithoutSerializingInputs(t *testing.T) {
	container := app.NewContainer()
	if err := app.RegisterBuiltins(container); err != nil {
		t.Fatalf("RegisterBuiltins returned error: %v", err)
	}
	service := NewAppService(container)

	const secretInput = "hash-input-must-not-reach-audit"
	if _, err := service.ExecuteCommand(context.Background(), ExecuteCommandRequest{
		ID: "tools.hash",
		Payload: map[string]any{
			"algorithm": "sha256",
			"input":     secretInput,
		},
	}); err != nil {
		t.Fatalf("allowed ExecuteCommand returned error: %v", err)
	}

	container.Permissions = permissions.NewPolicy([]permissions.Grant{
		{SubjectID: "builtin.tools", Capability: "tools:hash", Scope: "command:tools.hash"},
	})
	container.Capabilities = capabilities.NewGateway(container.Permissions, container.Audit)

	const hostileProfileID = "postgres://admin:password@internal-db/devkit"
	const rawSQL = "SELECT password FROM credentials; DROP TABLE audit_events"
	if _, err := service.DBQuery(context.Background(), hostileProfileID, rawSQL); err == nil {
		t.Fatal("denied DBQuery returned nil error")
	}

	events := container.Audit.Events()
	if len(events) != 2 {
		t.Fatalf("len(audit events) = %d, want 2", len(events))
	}
	if events[0].Outcome != "allowed" || events[1].Outcome != "denied" {
		t.Fatalf("audit outcomes = [%q %q], want [allowed denied]", events[0].Outcome, events[1].Outcome)
	}

	serialized, err := json.Marshal(events)
	if err != nil {
		t.Fatalf("json.Marshal(audit events): %v", err)
	}
	for _, forbidden := range []string{secretInput, hostileProfileID, rawSQL} {
		if strings.Contains(string(serialized), forbidden) {
			t.Fatalf("serialized audit events leaked %q: %s", forbidden, serialized)
		}
	}
}
