package app

import (
	"testing"

	"devkit/internal/permissions"
	"devkit/internal/tools"
)

func TestNewContainerWiresCoreServices(t *testing.T) {
	container := NewContainer()

	if container.Commands == nil {
		t.Fatal("Commands is nil")
	}
	if container.Modules == nil {
		t.Fatal("Modules is nil")
	}
	if container.Vault == nil {
		t.Fatal("Vault is nil")
	}
	if container.Capabilities == nil {
		t.Fatal("Capabilities is nil")
	}
}

func TestNewContainerDeniesScopeOutsideBridgeGrant(t *testing.T) {
	container := NewContainer()
	decision := container.Permissions.Check(permissions.Request{
		SubjectID:  "builtin.notes",
		Capability: "notes:read",
		Scope:      "profile:unrelated",
	})
	if decision.Allowed {
		t.Fatal("ungranted bridge scope was allowed")
	}
}

func TestRegisterBuiltinsRegistersToolsModule(t *testing.T) {
	container := NewContainer()

	if err := RegisterBuiltins(container); err != nil {
		t.Fatalf("RegisterBuiltins returned error: %v", err)
	}

	if len(container.Modules.List()) != 9 {
		t.Fatalf("module count = %d, want 9", len(container.Modules.List()))
	}
}

func TestNewContainerGrantsAgentHubScopes(t *testing.T) {
	container := NewContainer()
	for _, grant := range []permissions.Grant{
		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "session:list"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "session:abc"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "vendor:list"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:execute", Scope: "session:new"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:execute", Scope: "session:abc"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:execute", Scope: "permission:complete"},
	} {
		decision := container.Permissions.Check(permissions.Request{
			SubjectID: grant.SubjectID, Capability: grant.Capability, Scope: grant.Scope,
		})
		if !decision.Allowed {
			t.Fatalf("agenthub scope %q / %q was denied", grant.Capability, grant.Scope)
		}
	}
}

func TestNewContainerGrantsOnlyExactSyncScopes(t *testing.T) {
	container := NewContainer()
	for _, scope := range []string{"sync:status", "sync:connect", "sync:run", "sync:disconnect", "sync:conflicts", "sync:resolve", "sync:devices"} {
		decision := container.Permissions.Check(permissions.Request{
			SubjectID: "builtin.sync", Capability: "sync:manage", Scope: scope,
		})
		if !decision.Allowed {
			t.Fatalf("sync scope %q was denied", scope)
		}
	}

	for _, scope := range []string{"sync:*", "sync:admin", "*"} {
		decision := container.Permissions.Check(permissions.Request{
			SubjectID: "builtin.sync", Capability: "sync:manage", Scope: scope,
		})
		if decision.Allowed {
			t.Fatalf("ungranted sync scope %q was allowed", scope)
		}
	}
}

// Every tools command must be registered AND granted. These live in different
// files, and the failure of letting them drift is silent in both directions: a
// command registered with no grant is dead on arrival, and a grant with no
// command is an authorisation nobody revokes because nobody sees it.
func TestEveryToolsCommandIsRegisteredAndGranted(t *testing.T) {
	container := NewContainer()
	if err := RegisterBuiltins(container); err != nil {
		t.Fatalf("RegisterBuiltins returned error: %v", err)
	}

	registered := map[string]bool{}
	for _, command := range container.Commands.List() {
		registered[command.ID] = true
	}

	for _, descriptor := range tools.Commands {
		if !registered[descriptor.ID] {
			t.Errorf("command %q is in tools.Commands but was never registered", descriptor.ID)
		}
		decision := container.Permissions.Check(permissions.Request{
			SubjectID:  tools.SubjectID,
			Capability: descriptor.Capability,
			Scope:      "command:" + descriptor.ID,
		})
		if !decision.Allowed {
			t.Errorf("command %q is registered but not granted: %s", descriptor.ID, decision.Reason)
		}
	}
}

// The split is only worth having if it actually withholds something: a util
// command must not be reachable through the hash capability.
func TestUtilCommandsAreNotGrantedUnderTheHashCapability(t *testing.T) {
	container := NewContainer()
	for _, descriptor := range tools.Commands {
		if descriptor.Capability != tools.CapabilityUtil {
			continue
		}
		decision := container.Permissions.Check(permissions.Request{
			SubjectID:  tools.SubjectID,
			Capability: tools.CapabilityHash,
			Scope:      "command:" + descriptor.ID,
		})
		if decision.Allowed {
			t.Errorf("%q is reachable under %s, which defeats the capability split", descriptor.ID, tools.CapabilityHash)
		}
	}
}
