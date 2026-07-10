# MCP Agent Integration & Security Controls

## 1. Overview

This document describes how DevKit exposes its capabilities to AI agents (Claude,
Cursor, Copilot, etc.) via the Model Context Protocol (MCP), and how the security
control model is designed to keep that surface safe.

### Position in the Stack

MCP Agent is a **new transport into the existing Capability Gateway** — it does not
replace or bypass the existing security boundary.

```
React Frontend  ──► Wails Bridge   ──► Capability Gateway ──► Core Services
Internal Module ──► Extension API  ──► Capability Gateway ──► Core Services
AI Agent        ──► MCP Adapter    ──► Capability Gateway ──► Core Services
                    (new)              (existing, reused)
```

The Capability Gateway does not know whether the caller is a frontend user or an AI
agent. It only sees subject identity, capability, and scope. All existing security
guarantees remain in place.

### Design Principles

1. **MCP is a transport, not a trust boundary.** The Capability Gateway is the trust
   boundary. MCP Adapter is a protocol translator that sits in front of it.
2. **Least privilege by default.** The global default mode is `read_only`. Operators
   must explicitly grant write capabilities.
3. **Module-grouped exposure.** Tools are grouped by module. A new tool added to a
   module automatically inherits the module's policy — no config update needed.
4. **Defense-in-depth.** Three enforcement layers stack on top of each other.
   Passing layer 1 does not skip layer 2 or layer 3.
5. **Auditable by construction.** Every tool call is recorded in the audit log
   regardless of outcome.

---

## 2. Tool Exposure — Module-Grouped

Tools are exposed grouped by module, not as a flat list.

**Advantages over a flat list:**

| Concern | Flat list | Module-grouped |
|---|---|---|
| Config size | Grows with every tool | Stays small |
| New tool added to module | Must update policy manually | Inherits module policy |
| Mental model | Must know individual tool names | "Enable the vault module" |
| Mapping to codebase | No natural mapping | Maps to `internal/modules` |

### Tool Naming Convention

```
<module>.<action><Resource>

vault.listItems        vault.getItem
vault.createItem       vault.deleteItem

notes.search           notes.getNote
notes.createNote       notes.deleteNote

db.listConnections     db.executeQuery
db.getSchema

ssh.listProfiles       ssh.runCommand

tools.hash             tools.encode
tools.decode
```

### Tool Definition

Every MCP tool **must declare its `Effect`**. This is the foundation of the
read-only enforcement mode: the policy engine can block write/delete/execute tools
without needing an explicit deny list.

```go
type MCPToolDefinition struct {
    ID                  ToolID
    Module              ModuleID
    Name                string
    Description         string
    InputSchema         JSONSchema
    Effect              ToolEffect           // required
    RequiredPermissions []Permission
}

type ToolEffect string

const (
    EffectRead    ToolEffect = "read"    // does not change state
    EffectWrite   ToolEffect = "write"   // creates or modifies data
    EffectDelete  ToolEffect = "delete"  // removes data
    EffectExecute ToolEffect = "execute" // runs a query, command, or SSH operation
    EffectSystem  ToolEffect = "system"  // changes app state (lock vault, etc.)
)
```

Effect annotations by built-in module:

| Tool | Effect |
|---|---|
| `vault.listItems`, `vault.getItem` | `read` |
| `vault.createItem`, `vault.updateItem` | `write` |
| `vault.deleteItem` | `delete` |
| `notes.search`, `notes.getNote` | `read` |
| `notes.createNote`, `notes.updateNote` | `write` |
| `notes.deleteNote` | `delete` |
| `db.listConnections`, `db.getSchema` | `read` |
| `db.executeQuery` | `execute` (may be INSERT/UPDATE) |
| `ssh.listProfiles` | `read` |
| `ssh.runCommand` | `execute` |
| `tools.hash`, `tools.encode`, `tools.decode` | `read` |
| `system.lockVault` | `system` |

---

## 3. Security Model — Three Enforcement Layers

```
Layer 1 — MODULE     enable / disable entire modules
    │
Layer 2 — EFFECT     read-only mode blocks by effect class
    │
Layer 3 — TOOL       block specific tools within a module
```

Layer 1 handles the most common case with one line of config. Layers 2 and 3 are
available for fine-grained control when needed.

Beyond these three layers, two additional controls apply:

- **Data Scope** — limits which data the agent can see (vault scope, note tags, DB
  connection IDs).
- **Confirmation Gate** — requires human approval before executing dangerous effects.

---

## 4. Agent Policy

An `AgentPolicy` is attached to a session at connection time and is immutable for
the duration of that session.

```go
type AgentPolicy struct {
    AgentID string
    Mode    AgentMode // global fallback mode for all modules

    // Layer 1: module-level control
    EnabledModules  []ModuleID // empty = all modules allowed
    DisabledModules []ModuleID

    // Layers 2 + 3: per-module fine-grained control
    ModulePolicies map[ModuleID]ModulePolicy

    // Data access boundaries
    DataScope AgentDataScope

    // Human-in-the-loop
    RequireConfirmation ConfirmationPolicy

    // Session lifetime
    SessionTTL time.Duration

    // Metadata
    CreatedAt time.Time
    CreatedBy string // "user" | "config"
}

type AgentMode string

const (
    AgentModeReadOnly  AgentMode = "read_only"  // only EffectRead allowed
    AgentModeReadWrite AgentMode = "read_write" // all effects allowed
    AgentModeCustom    AgentMode = "custom"     // only AllowedTools list
)

type ModulePolicy struct {
    Mode         AgentMode // overrides global mode for this module
    DeniedTools  []ToolID  // always denied within this module
    AllowedTools []ToolID  // only used when Mode = custom
}
```

---

## 5. Policy Enforcement Flow

Every tool call passes through all layers in order before reaching the Capability
Gateway.

```
AI Agent calls: vault.deleteItem
        │
        ▼ Layer 1: Module check
        │  Is "vault" in DisabledModules?               → DENY
        │  Is "vault" absent from EnabledModules (when set)? → DENY
        │  Pass → continue
        │
        ▼ Layer 2: Effect check
        │  Global mode = read_only AND effect != read?  → DENY
        │  Module mode = read_only AND effect != read?  → DENY
        │  Pass → continue
        │
        ▼ Layer 3: Tool check
        │  Tool in ModulePolicy.DeniedTools?            → DENY
        │  Mode = custom AND tool not in AllowedTools?  → DENY
        │  Pass → continue
        │
        ▼ Data Scope check
        │  Requested item outside vault_scope?          → DENY
        │  Pass → continue
        │
        ▼ Confirmation Gate
        │  Effect in ConfirmEffects?                    → Wait for user confirm
        │  User denies or timeout?                      → DENY
        │  Pass → continue
        │
        ▼ Capability Gateway (existing Layer 2)
           Permission granted for subject?              → check
           Vault unlocked?                              → check
           Input payload valid?                         → check
           → CALL Core Service
```

---

## 6. Data Scope Constraints

Data scope limits which data the agent can access, independent of which tools it
can call.

```go
type AgentDataScope struct {
    // Vault: agent only sees items matching this scope pattern
    // examples: "secret:notes/*" | "secret:connection/pg/*" | "*"
    VaultScope string

    // Notes: agent only sees notes with these tags
    // empty = all notes visible
    NotesTags []string

    // DB: agent may only query these connection profiles
    AllowedDBConnections []string // connection IDs; empty = all

    // SSH: agent may only use these SSH profiles
    AllowedSSHProfiles []string // profile IDs; empty = all
}
```

Recommended defaults for a coding assistant agent:

```toml
[mcp.default_data_scope]
vault_scope = "secret:notes/*"
notes_tags  = ["ai-accessible"]
allowed_db_connections = []
allowed_ssh_profiles   = []
```

---

## 7. Confirmation Gate

Some operations require human approval before execution, even when the agent has
the required permission.

```go
type ConfirmationPolicy struct {
    // Effects that always require confirmation
    ConfirmEffects []ToolEffect

    // Tools that always require confirmation regardless of effect
    AlwaysConfirm []ToolID

    // How long to wait for user response
    ConfirmTimeout time.Duration // default: 30s

    // Behavior when no UI is available (headless mode)
    // Recommended: Deny
    HeadlessBehavior HeadlessBehavior // Deny | Allow
}
```

When confirmation is needed, the MCP server emits a Wails event. The desktop UI
shows a dialog:

```
┌──────────────────────────────────────────────────┐
│ 🤖 Agent requests permission to execute:         │
│                                                  │
│   Tool:    vault.deleteItem                      │
│   Effect:  DELETE                                │
│   Agent:   claude-desktop                        │
│                                                  │
│   [Allow]                           [Deny]       │
└──────────────────────────────────────────────────┘
```

If the app is running headless (server mode), `HeadlessBehavior = Deny` is
recommended.

---

## 8. Session Management

### Session Lifecycle

```
Agent connects to MCP server
    │
    ▼
Load AgentPolicy from config (or vault settings)
    │
    ▼
Create AgentSession {
    sessionId:      generated
    agentId:        from config key
    policy:         AgentPolicy (immutable)
    startedAt:      time.Now()
    lastActivityAt: time.Now()
}
    │
    ▼
Agent calls tools → lastActivityAt updated on each call
    │
    ├── TTL exceeded → session expired → all calls DENY
    ├── Agent disconnects → session terminated
    └── User revokes → session killed immediately
    │
    ▼
All session calls logged to audit
```

### Session Revocation

The user can revoke a running session from the UI under
**Settings → AI Agents → Active Sessions**. Revocation is immediate and generates
an audit event `agent_session_revoked`.

---

## 9. Audit Trail

Every tool call from an agent is recorded regardless of outcome.

**Successful call:**

```json
{
  "event": "agent_tool_call",
  "sessionId": "sess_abc123",
  "agentId": "claude-desktop",
  "tool": "vault.getItem",
  "module": "vault",
  "effect": "read",
  "policyDecision": "allowed",
  "gatewayDecision": "allowed",
  "duration_ms": 12,
  "timestamp": "2026-07-11T00:10:00Z"
}
```

**Denied by policy:**

```json
{
  "event": "agent_tool_denied",
  "sessionId": "sess_abc123",
  "agentId": "claude-desktop",
  "tool": "vault.deleteItem",
  "module": "vault",
  "effect": "delete",
  "denyLayer": "policy",
  "denyReason": "policy.module_read_only",
  "timestamp": "2026-07-11T00:10:05Z"
}
```

**Redaction rule:** tool call arguments must not be logged. Only the tool ID,
effect, and decision are recorded. This follows the same redaction rules as the
existing Capability Gateway audit events.

---

## 10. Configuration Reference

### Global MCP settings

```toml
[mcp]
enabled              = true
default_mode         = "read_only"   # safest default
session_ttl          = "1h"
require_vault_unlock = true          # MCP server does not start if vault is locked

[mcp.confirmation]
confirm_effects   = ["delete", "execute", "system"]
confirm_timeout   = "30s"
headless_behavior = "deny"           # deny when no UI is available

[mcp.default_data_scope]
vault_scope            = "secret:notes/*"
notes_tags             = ["ai-accessible"]
allowed_db_connections = []
allowed_ssh_profiles   = []
```

### Per-agent configuration examples

```toml
# Coding assistant: read-only, limited to annotated notes
[mcp.agents.claude-desktop]
mode = "read_only"

[mcp.agents.claude-desktop.data_scope]
notes_tags  = ["ai-accessible", "code-context"]
vault_scope = "secret:notes/*"

# ──────────────────────────────────────────────────────────────────────────────

# Dev automation agent: write access to notes and tools, no SSH or DB
[mcp.agents.cursor]
disabled_modules = ["ssh", "db"]

[mcp.agents.cursor.modules.vault]
mode = "read_only"

[mcp.agents.cursor.modules.notes]
mode         = "read_write"
denied_tools = ["notes.deleteNote"]

[mcp.agents.cursor.modules.tools]
mode = "read_write"

# ──────────────────────────────────────────────────────────────────────────────

# Admin agent: full access with confirmation on destructive operations
[mcp.agents.admin-agent]
mode            = "read_write"
enabled_modules = ["vault", "notes", "db", "tools"]

[mcp.agents.admin-agent.confirmation]
confirm_effects = ["delete", "execute", "system"]
```

### Security option quick reference

| Goal | Config |
|---|---|
| Lock all write/delete/execute tools | `mode = "read_only"` |
| Disable a module entirely | `disabled_modules = ["ssh", "db"]` |
| Read-only one module | `modules.vault.mode = "read_only"` |
| Block a specific tool | `modules.notes.denied_tools = ["notes.deleteNote"]` |
| Whitelist only certain tools | `modules.X.mode = "custom"` + `allowed_tools = [...]` |
| Limit which vault items agent sees | `data_scope.vault_scope = "secret:notes/*"` |
| Limit which notes agent sees | `data_scope.notes_tags = ["ai-accessible"]` |
| Require confirm before delete | `confirmation.confirm_effects = ["delete"]` |
| Auto-expire session | `session_ttl = "1h"` |
| Kill a running session | UI: Settings → AI Agents → Active Sessions → Revoke |

---

## 11. Implementation Plan

### New package: `internal/mcp`

```text
internal/mcp/
├── server.go          MCP server (stdio / SSE transport)
├── session.go         AgentSession store and TTL management
├── policy.go          AgentPolicy, ModulePolicy types
├── enforcement.go     Three-layer policy enforcement
├── confirmation.go    Confirmation gate + Wails event bridge
├── audit.go           Agent-specific audit event types
├── config.go          Config loading from vault settings
└── tool_registry.go   MCP tool registry (module-grouped)
```

### Changes to existing packages

```text
internal/commands/
└── registry.go        Add Effect field to CommandDefinition / MCPToolDefinition

internal/modules/
└── registry.go        Expose module-grouped MCPToolDefinition list

internal/audit/
└── events.go          Add agent_tool_call, agent_tool_denied, agent_session_revoked

internal/app/
└── app.go             Wire MCP server into composition root
```

### Frontend (future)

```text
frontend/src/modules/settings/
└── agents/
    ├── AgentList.tsx          per-agent config editor
    └── ActiveSessions.tsx     active session list with Revoke action
```

### Implementation order

```
1. Add Effect field to CommandDefinition
2. Define AgentPolicy, ModulePolicy, AgentDataScope types
3. Implement three-layer enforcement (module → effect → tool)
4. Implement AgentSession store with TTL
5. MCP stdio server and tool routing
6. Audit event types for agent calls
7. Config loading from vault settings
8. Confirmation gate + Wails event emit
9. UI: Active Sessions panel + per-agent config editor
```

---

## 12. Key Risks

1. **Tool Effect mis-annotation.** If a tool that mutates state is incorrectly
   annotated as `read`, read-only mode will not block it. Every tool definition
   must be reviewed at PR time.

2. **Data scope bypass via chained reads.** An agent with `vault_scope =
   "secret:notes/*"` might infer secrets from notes content. Data classification
   of notes content matters.

3. **Confirmation fatigue.** If too many operations require confirmation, users will
   start approving without reading. Confirmation should be reserved for genuinely
   dangerous effects (`delete`, `execute`, `system`).

4. **Headless mode without deny.** If `headless_behavior = "allow"` is used,
   destructive operations proceed without any human review. Default must be `deny`.

5. **Session leak.** If session TTL is too long or sessions are not cleaned up after
   disconnect, stale sessions accumulate. Implement session GC and emit
   `agent_session_expired` events.

---

## 13. Open Questions

1. Should the MCP server support SSE (HTTP streaming) in addition to stdio, to
   support web-based agents?
2. Should per-agent policies be stored in vault settings (encrypted) or in a plain
   config file?
3. Should the confirmation gate support a timeout-then-allow mode for non-sensitive
   effects when the user is known to be away?
4. Should agent identity be verified cryptographically (signed session token), or is
   a local config key sufficient for the first version?
