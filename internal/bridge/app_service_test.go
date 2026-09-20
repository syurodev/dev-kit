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
	"devkit/internal/tools"
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

	if len(moduleList) != 13 {
		t.Fatalf("len(modules) = %d, want 13", len(moduleList))
	}
	wantCapabilities := map[string][]string{
		// tools:util joins tools:hash rather than replacing it — tools.mdx
		// splits the pure-function group from hashing, and technical contracts
		// counts an added capability with an untouched grant as compatible.
		"tools":     {"tools:hash", "tools:util"},
		"notes":     {"notes:read", "notes:write"},
		"database":  {"db:query"},
		"ssh":       {"ssh:connect", "ssh:forward"},
		"kafka":     {"kafka:connect", "kafka:produce"},
		"sync":      {"sync:manage"},
		"settings":  {"env:read", "env:write", "preferences:read", "preferences:write"},
		"agenthub":  {"agenthub:read", "agenthub:execute"},
		"codegraph": {"codegraph:index", "codegraph:read", "codegraph:toolchain"},
		// accounts:reveal is declared but never granted to the MCP subject —
		// the manifest says the capability exists, container.go decides who
		// holds it.
		"accounts": {"accounts:read", "accounts:write", "accounts:reveal", "accounts:use"},
		"tasks":    {"tasks:read", "tasks:write", "tasks:admin"},
		// git:push is its own capability because it leaves the machine, and
		// there is no git:push:force anywhere.
		"git":       {"git:read", "git:write", "git:push"},
		"apiclient": {"api:read", "api:write", "api:send"},
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
	if !found["tools"] || !found["notes"] || !found["database"] || !found["ssh"] || !found["kafka"] || !found["sync"] || !found["settings"] || !found["agenthub"] || !found["codegraph"] {
		t.Fatalf("modules = %+v, want tools, notes, database, ssh, kafka, sync, settings, agenthub and codegraph", moduleList)
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
	if _, err := service.DBQuery(context.Background(), hostileProfileID, rawSQL, 0, ""); err == nil {
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

// Every util command must reach its service through the bridge. This is the
// only place the untyped payload from the webview becomes a typed request, and
// a command whose payload decodes to the wrong shape fails at the handler with
// nothing on screen to explain it.
func TestAppServiceExecutesEveryUtilCommand(t *testing.T) {
	service := newTestService(t)
	tests := []struct {
		id      string
		payload map[string]any
		want    string
	}{
		{id: tools.CommandBase64Encode, payload: map[string]any{"input": "hello"}, want: "aGVsbG8="},
		{id: tools.CommandBase64Decode, payload: map[string]any{"input": "aGVsbG8="}, want: "hello"},
		{id: tools.CommandURLEncode, payload: map[string]any{"input": "a b"}, want: "a+b"},
		{id: tools.CommandURLDecode, payload: map[string]any{"input": "a+b"}, want: "a b"},
		{id: tools.CommandJSONFormat, payload: map[string]any{"input": `{"a":1}`}, want: "{\n  \"a\": 1\n}"},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			result, err := service.ExecuteCommand(context.Background(), ExecuteCommandRequest{
				ID: tc.id, Payload: tc.payload,
			})
			if err != nil {
				t.Fatalf("ExecuteCommand(%q): %v", tc.id, err)
			}
			util, ok := result.(tools.UtilResult)
			if !ok {
				t.Fatalf("ExecuteCommand(%q) returned %T, want tools.UtilResult", tc.id, result)
			}
			if util.Output != tc.want {
				t.Errorf("ExecuteCommand(%q) output = %q, want %q", tc.id, util.Output, tc.want)
			}
		})
	}
}

func TestAppServiceGeneratesUUID(t *testing.T) {
	service := newTestService(t)
	for _, version := range []string{"", "v4", "v7"} {
		result, err := service.ExecuteCommand(context.Background(), ExecuteCommandRequest{
			ID: tools.CommandUUIDGenerate, Payload: map[string]any{"version": version},
		})
		if err != nil {
			t.Fatalf("ExecuteCommand(uuid %q): %v", version, err)
		}
		util, ok := result.(tools.UtilResult)
		if !ok || len(util.Output) != 36 {
			t.Fatalf("ExecuteCommand(uuid %q) returned %#v, want a 36-character UUID", version, result)
		}
	}
}

// An id the tools table does not know must be refused before it reaches the
// registry. Previously every command was authorised as tools:hash, so anything
// that got registered later would have run under a grant meant for hashing.
func TestAppServiceRefusesCommandsOutsideTheToolsTable(t *testing.T) {
	service := newTestService(t)
	for _, id := range []string{"notes.create", "tools.base64Encode", "", "tools.nope"} {
		if _, err := service.ExecuteCommand(context.Background(), ExecuteCommandRequest{ID: id}); err == nil {
			t.Errorf("ExecuteCommand(%q) succeeded, want a refusal", id)
		}
	}
}

// A missing key must read as empty rather than the string "<nil>", which is
// what fmt.Sprint gives for a nil interface — an algorithm named "<nil>" would
// reach the service and be reported as unsupported, blaming the wrong thing.
func TestDecodeCommandPayloadTreatsMissingKeysAsEmpty(t *testing.T) {
	request := ExecuteCommandRequest{ID: tools.CommandHash, Payload: map[string]any{"input": "x"}}
	decoded, ok := decodeCommandPayload(request).(tools.HashRequest)
	if !ok {
		t.Fatalf("decodeCommandPayload returned %T, want tools.HashRequest", decodeCommandPayload(request))
	}
	if decoded.Algorithm != "" {
		t.Errorf("Algorithm = %q, want empty", decoded.Algorithm)
	}
	nilValued := ExecuteCommandRequest{ID: tools.CommandURLEncode, Payload: map[string]any{"input": nil}}
	util, ok := decodeCommandPayload(nilValued).(tools.UtilRequest)
	if !ok || util.Input != "" {
		t.Errorf("decodeCommandPayload with a nil value = %#v, want an empty Input", decodeCommandPayload(nilValued))
	}
}
