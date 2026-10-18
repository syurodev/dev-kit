package app

import (
	"testing"

	"devkit/internal/mcp"
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

	if len(container.Modules.List()) != 13 {
		t.Fatalf("module count = %d, want 13", len(container.Modules.List()))
	}
}

func TestNewContainerGrantsAgentHubScopes(t *testing.T) {
	container := NewContainer()
	for _, grant := range []permissions.Grant{
		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "session:list"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "session:abc"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "vendor:list"},
		// The window's reads before a session exists. Without these the draft
		// picker and Settings → Agents were refused at the gateway.
		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "catalog:claude"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "catalog:codex"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "environment:claude"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "environment:codex"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:execute", Scope: "session:new"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:execute", Scope: "session:abc"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:execute", Scope: "permission:complete"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "permission:list"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:execute", Scope: "elicitation:respond"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "elicitation:list"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "runtime:claude-acp"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:execute", Scope: "runtime:claude-acp"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "auth:claude"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:execute", Scope: "auth:claude"},
		// Codex signs in inside the app: status, browser or code, cancel, sign out.
		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "auth:codex"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:execute", Scope: "auth:codex"},
		{SubjectID: "builtin.agenthub", Capability: "terminal:io", Scope: "session:abc"},
	} {
		decision := container.Permissions.Check(permissions.Request{
			SubjectID: grant.SubjectID, Capability: grant.Capability, Scope: grant.Scope,
		})
		if !decision.Allowed {
			t.Fatalf("agenthub scope %q / %q was denied", grant.Capability, grant.Scope)
		}
	}
}

// The MCP fakes call the handler without asking the policy, so only the real
// grants can say whether an agent's fetch or pull is allowed. They were not:
// git.fetch and git.pull asked for git:write on "remote:origin" and the repo:*
// grant never covered it, so every call was refused.
func TestNewContainerGrantsAgentGitSync(t *testing.T) {
	container := NewContainer()
	subject, capability, scope := mcp.GitSyncGrant()
	decision := container.Permissions.Check(permissions.Request{SubjectID: subject, Capability: capability, Scope: scope})
	if !decision.Allowed {
		t.Fatalf("git.fetch / git.pull ask for %s %q on %q, which no default grant covers", subject, capability, scope)
	}
	// Pushing stays its own capability; a fetch grant must not widen it.
	if container.Permissions.Check(permissions.Request{SubjectID: subject, Capability: "git:push", Scope: "repo:sync"}).Allowed {
		t.Fatal("git:push must stay limited to remote:* scopes")
	}
}

// Every pr.* tool asks the gateway for one of these. The tests in internal/mcp
// call the handlers without the policy, so only the grants the container really
// installs can say whether an agent's pr.get or pr.create would be refused.
func TestNewContainerGrantsEveryPullRequestToolItsCapability(t *testing.T) {
	container := NewContainer()
	for _, grant := range mcp.PullRequestGrants() {
		decision := container.Permissions.Check(permissions.Request{SubjectID: grant[0], Capability: grant[1], Scope: grant[2]})
		if !decision.Allowed {
			t.Errorf("pr.* asks for %s %q on %q, which no default grant covers", grant[0], grant[1], grant[2])
		}
	}
	// The window reads and attaches under the same subject.
	for _, grant := range [][3]string{{"builtin.git", "git:read", "repo:pr"}, {"builtin.git", "git:write", "repo:pr"}} {
		if !container.Permissions.Check(permissions.Request{SubjectID: grant[0], Capability: grant[1], Scope: grant[2]}).Allowed {
			t.Errorf("the window's %v is not granted", grant)
		}
	}
	// A write is push-class: a git:write grant must not stand in for it.
	if container.Permissions.Check(permissions.Request{SubjectID: "builtin.git", Capability: "git:push", Scope: "repo:pr"}).Allowed {
		t.Error("git:push must stay limited to remote:* scopes")
	}
}

// Same lesson as git.fetch: the MCP tests call the handlers without the policy,
// so only the container's real grants say whether an agent's listPeers or
// sendMessage would be refused.
func TestNewContainerGrantsTheMessagingTools(t *testing.T) {
	container := NewContainer()
	for _, grant := range mcp.PeerMessageGrants() {
		decision := container.Permissions.Check(permissions.Request{SubjectID: grant[0], Capability: grant[1], Scope: grant[2]})
		if !decision.Allowed {
			t.Errorf("the messaging tools ask for %s %q on %q, which no default grant covers", grant[0], grant[1], grant[2])
		}
	}
}

// resumeSession, closeSession and readSession call through session:<id>; the
// MCP tests have no policy, so the real grants decide whether an agent is refused.
func TestNewContainerGrantsTheChildControlTools(t *testing.T) {
	container := NewContainer()
	id := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	for _, grant := range [][3]string{
		{"builtin.agenthub", "agenthub:execute", "session:" + id},
		{"builtin.agenthub", "agenthub:read", "session:" + id},
	} {
		if !container.Permissions.Check(permissions.Request{SubjectID: grant[0], Capability: grant[1], Scope: grant[2]}).Allowed {
			t.Errorf("a child-control tool asks for %s %q on %q, which no default grant covers", grant[0], grant[1], grant[2])
		}
	}
}

// agenthub.listModels and agenthub.listModelProfiles read through scopes of
// their own. The MCP tests call the handlers with no policy, so only the
// container's real grants can say whether an agent's call would be refused.
func TestNewContainerGrantsTheModelReadTools(t *testing.T) {
	container := NewContainer()
	for _, grant := range [][3]string{mcp.AgentHubModelsGrant, mcp.AgentHubProfilesGrant} {
		decision := container.Permissions.Check(permissions.Request{SubjectID: grant[0], Capability: grant[1], Scope: grant[2]})
		if !decision.Allowed {
			t.Errorf("a model tool asks for %s %q on %q, which no default grant covers", grant[0], grant[1], grant[2])
		}
	}
	// Reading the lists must not stand in for starting a session or for
	// editing the profiles: those are other capabilities.
	if container.Permissions.Check(permissions.Request{SubjectID: "builtin.agenthub", Capability: "agenthub:execute", Scope: "profiles:list"}).Allowed {
		t.Error("agenthub:execute must not be granted on profiles:list")
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
