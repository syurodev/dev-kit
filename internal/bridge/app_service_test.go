package bridge

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"devkit/internal/app"
	"devkit/internal/capabilities"
	"devkit/internal/modules"
	"devkit/internal/permissions"
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
	moduleList := service.ListModules()

	if len(moduleList) != 6 {
		t.Fatalf("len(modules) = %d, want 6", len(moduleList))
	}
	wantCapabilities := map[string][]string{
		"tools":    {"tools:hash"},
		"notes":    {"notes:read", "notes:write"},
		"database": {"db:query"},
		"ssh":      {"ssh:connect", "ssh:forward"},
		"sync":     {"sync:manage"},
		"settings": {"env:read", "env:write"},
	}
	found := map[string]bool{}
	for _, m := range moduleList {
		found[m.ID] = true
		if m.Version != "1.0.0" {
			t.Errorf("module %q version = %q, want 1.0.0", m.ID, m.Version)
		}
		if m.APIVersion != modules.CurrentAPIVersion {
			t.Errorf("module %q api version = %q, want %q", m.ID, m.APIVersion, modules.CurrentAPIVersion)
		}
		want, ok := wantCapabilities[m.ID]
		if !ok {
			t.Errorf("unexpected module %q", m.ID)
			continue
		}
		if len(m.Capabilities) == 0 {
			t.Errorf("module %q capabilities are empty", m.ID)
		}
		if got, wantJSON := string(mustJSON(t, m.Capabilities)), string(mustJSON(t, want)); got != wantJSON {
			t.Errorf("module %q capabilities = %s, want %s", m.ID, got, wantJSON)
		}
	}
	if !found["tools"] || !found["notes"] || !found["database"] || !found["ssh"] || !found["sync"] || !found["settings"] {
		t.Fatalf("modules = %+v, want tools, notes, database, ssh, sync and settings", moduleList)
	}
}

func TestAppServiceModuleDTOJSONExposesOnlyPublicMetadata(t *testing.T) {
	service := newTestService(t)
	payload, err := json.Marshal(service.ListModules())
	if err != nil {
		t.Fatalf("json.Marshal(ListModules()) returned error: %v", err)
	}

	serialized := string(payload)
	for _, expected := range []string{`"version":"1.0.0"`, `"api_version":"1"`, `"capabilities":[`} {
		if !strings.Contains(serialized, expected) {
			t.Fatalf("ListModules JSON = %s, want it to contain %s", serialized, expected)
		}
	}
	for _, forbidden := range []string{"handler", "secret", "grant"} {
		if strings.Contains(strings.ToLower(serialized), forbidden) {
			t.Fatalf("ListModules JSON exposed forbidden metadata %q: %s", forbidden, serialized)
		}
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal(%T) returned error: %v", value, err)
	}
	return payload
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
