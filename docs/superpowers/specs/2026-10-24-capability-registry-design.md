# Capability registry: declare each operation once

Task OPT-8a83b7 (A1). Status: **design, not built**. Nothing in this document has been implemented.
Everything under "Today" was read from the code on 2026-10-24 (branch `opt/a1-capability-spec`, cut from `main`).

## 1. Problem

A DevKit operation, such as "read a note", is written down in up to four places that nothing ties together:

| Where | What it says | Example for `notes.get` |
| --- | --- | --- |
| `internal/bridge/<module>.go` | subject, capability, scope, audit metadata, handler | `AppService.NotesGet`: `builtin.notes` / `notes:read` / `"note:"+recordID(id)` / `{action: get, note_id}` → `Notes.Get(id)` |
| `internal/mcp/tools_<module>.go` | **the same** subject, capability, scope, metadata, handler, plus name, description, annotations, input DTO | `toolNotesGet`: the same strings, the same `recordID`, the same `Notes.Get(in.ID)`. `recordID` is even copied: `mcp/tools_notes.go:272` says it "mirrors bridge.recordID" |
| `internal/app/container.go` | the grants that let the call through | `{builtin.notes, notes:read, note:*}` (one of 150 hand-written grants) |
| tests | that the three above agree | `TestNewContainerGrants*` (8 of them in `internal/app/app_test.go`, one in `bridge/interaction_test.go`) |

Measured on this branch:

- **425** exported `AppService` methods (all bound by Wails), **384** `s.call(...)` gateway call sites in `internal/bridge`,
  **151** `sdk.AddTool` registrations in `internal/mcp`, **150** default grants in `app.NewContainerWithStores`.
- Notes: 21 bridge methods, 11 MCP tools. All 11 tools repeat a bridge method word for word. The other 10 bridge
  methods are UI-only, and the reason is written as a comment in two places (`bridge/notes.go:136`, `mcp/tools_notes.go:46`).

Here is how the copies drift, in cases that already happened (all from AGENTS.md):

- `git.fetch` / `git.pull` asked for a scope no grant covered. Every agent call was refused, and the MCP tests still
  passed because their fake `Caller` skips the policy. `TestNewContainerGrantsAgentGit*` exists for that reason.
- The window's Agent Hub pre-session reads (`catalog:*`, `environment:*`) had no grant. The draft picker said
  "Models unavailable" for weeks.
- UI-only is enforced three ways: by leaving the MCP tool out, by `bridge.uiOnly` (refuses a stamped non-UI caller), and
  by `agenthub.requireWindow`. A method is UI-only only because somebody remembered to do one of them.
- Confirmation runs in different places. `db.query` asks the person **before** the gateway checks the policy, so the
  person can approve a call that the policy then refuses, and the refusal is audited after the prompt. `api.*` writes
  ask **inside** the gateway handler (`mcp/tools_database.go:151` vs `confirmAPIWrite`).

What must not change: `capabilities.Gateway.Call` (`internal/capabilities/gateway.go:60`) stays the **only** place where
authorization is decided and audited. Vault / vaultitem / sync boundaries and every rule in `internal/arch` stay as strong
as they are, or get stronger.

## 2. Goal and non-goals

**Goal.** Each operation is declared once, as a Go value. These are derived from that value:

1. the Wails bridge method (same name, arguments and return type, so the TS bindings do not change),
2. the MCP tool (name, description, annotations, input schema, agent result shape),
3. the default grant(s),
4. the audit metadata,
5. the contract tests that the copies above can no longer drift from.

**Non-goals** are listed in §11. The most important ones: no change to the permission model (grants stay per
subject, not per caller), no new authentication for server mode, no renamed MCP tool, and no frontend rewrite.

## 3. Today, in one picture

```
 webview ──Wails $Call.ByID──▶ bridge.AppService.NotesGet(id)
                                   │  callerContext: unstamped ⇒ identity.UI()
                                   ▼
                              s.call(subject, capability, scope, meta, handler) ─┐
                                                                                 ▼
 agent ──MCP──▶ mcp.toolNotesGet(in)                              capabilities.Gateway.Call
                  │ withCaller: agent:<id> / user-terminal         policy.Check → audit.Record
                  ▼                                                → handler(ctx)
             deps.Caller.Call(...)  ── bridge.NewCaller ──▶ s.call ──┘    (bridge.publicErrorFor
                                                                           maps errors for both)
```

Two facts matter for the design:

- **MCP already goes through the bridge's gateway wrapper.** `main.go:1104` wires `mcp.Deps.Caller` as `bridge.NewCaller(appService)`,
  so error publishing (`publicErrorFor`, `keptCodes`) is already shared. Only the declaration is duplicated, not the
  pipeline.
- **The policy does not know who is calling.** `permissions.Policy.Check` matches subject, capability and scope only.
  Agents reach fewer operations only because fewer operations are registered as MCP tools. The registry makes this
  explicit (§5.3) without changing the policy model.

## 4. The declaration

### 4.1 Shape

A new package, `internal/ops`, holds one generic type plus the module declarations:

```go
// Op is one operation DevKit offers, to the window, to agents or to both.
type Op[In, Out any] struct {
	Name     string // MCP tool name and audit identity: "notes.get". Never renamed (agents' allowlists name it).
	Bridge   string // AppService method name: "NotesGet". Explicit, because Wails IDs are hashed from it (§6).

	Subject    string                                          // "builtin.notes"
	Capability string                                          // "notes:read"
	Scope      func(ctx context.Context, in In) (Scope, error) // request scope + audit metadata, see 4.3
	Grant      string                                          // the default grant pattern: "note:*" (see 7)

	Risk     Risk     // Read | Write | Destructive | External
	Confirm  Confirm  // policy + who it applies to, see 8
	Exposure Exposure // UI | Agent | Both, + Reason when not Both, see 5
	Desktop  string   // non-empty = refused in server mode, with this reason (dialogs, file paths, prompts)

	Guards []Guard // agent-side preconditions run after the policy and before confirmation, see 5.2
	Target *Target // active-target resolution for agent ops (database/ssh/kafka), see 5.2

	Agent AgentSurface // description, schema hints, result shape, budget exemption, pre-approved, see 5.4

	Handle func(ctx context.Context, d Deps, in In) (Out, error)
}

type Scope struct {
	Value string         // what the gateway checks: "note:<24-hex>" or "note:invalid"
	Meta  map[string]any // audit metadata beyond action/origin; ids and counts only
}
```

`Deps` is a struct of narrow interfaces, the same shape as `mcp.Deps` today (`NotesAPI`, `DatabaseAPI`, …). The
existing interfaces move from `internal/mcp` to `internal/ops`, and `mcp.Deps` embeds them. Each module file
(`ops/notes.go`, `ops/git.go`, …) exports its list:

```go
var NotesGet = Op[NotesIDIn, notes.Note]{
	Name: "notes.get", Bridge: "NotesGet",
	Subject: "builtin.notes", Capability: "notes:read", Grant: "note:*",
	Scope:    func(_ context.Context, in NotesIDIn) (Scope, error) { return RecordScope("note", "note_id", in.ID), nil },
	Risk:     Read,
	Exposure: Both,
	Agent:    AgentSurface{Description: "Get a note by id (decrypts body; requires unlocked vault)"},
	Handle:   func(_ context.Context, d Deps, in NotesIDIn) (notes.Note, error) { return d.Notes.Get(in.ID) },
}

func Notes() []Operation { return []Operation{NotesList, NotesSearch, NotesGet /* … */} }
```

`Operation` is the non-generic interface every `Op[In, Out]` satisfies (name, subject, capability, grant, exposure,
reflect types of In/Out, …). Adapters and tests iterate over it. Typed calls use the generic value.

### 4.2 One pipeline: `ops.Invoke`

Every adapter calls the same function:

```go
func Invoke[In, Out any](ctx context.Context, rt Runtime, op Op[In, Out], in In) (Out, error)
```

`Runtime` carries `Deps`, the `Caller` (today's `bridge.Caller`, so `publicErrorFor` stays where it is), the
`interaction.Hub`, the env confirmer and the server-mode flag. The order is fixed. It is the same for every operation,
and it is what §1 showed is missing today:

1. **Caller already stamped** by the adapter (bridge: `identity.UI()`; MCP: `agent:<id>` / `user-terminal`). `Invoke` does not guess.
2. **Desktop check**: in server mode, an op with `Desktop != ""` fails with the published "desktop app only" error.
   This replaces today's per-method `inServerMode` checks, such as `errAgentAccountsDesktopOnly`.
3. **Target resolution** (agent ops with `Target`): `EnsureActive`. This may ask the person to *select* a profile.
   Selecting is not confirming. The result is the exact profile id and its env tag, and the handler runs against that
   id (the race comment in `GateActiveMCP` still holds).
4. **Scope**: `op.Scope(ctx, in)` → `Scope{Value, Meta}`.
5. **Gateway**: `Caller.Call(CallRequest{Subject, Capability, Scope, Metadata, Operation: op.Name, Exposure: op.Exposure, Handler: …})`.
   The gateway checks exposure (§5.3) and the policy, records the audit event, then runs the handler. The handler
   holds the remaining steps, so they only run once authorization has passed:
   1. **Guards** (task ownership, granted folders, …).
   2. **Confirmation** (§8). It uses the env tag from step 3, or from the op's own resolver.
   3. **`op.Handle`**.
6. The adapter **shapes the result**. The bridge returns `Out` as it is. MCP applies `Agent.Shape` (compact shapes) and
   the existing `withResultBudget` middleware (§5.4).

So for `db.query`, confirmation moves from before the policy check to after it. This changes what the person sees in
one way: a call the policy would refuse no longer prompts first. I consider that a fix, not a regression.

### 4.3 Scope resolvers and audit metadata

Scope strings are never built inline. Small helpers do it, so the sanitising is written once:

- `RecordScope(kind, metaKey, id)`: `kind + ":" + recordID(id)`, with `{metaKey: recordID(id)}`. This is the one `recordID`
  (24 hex or `"invalid"`). The copy in `internal/mcp` is deleted.
- `FixedScope("note:list")`, `CommandScope(...)` (today's `commandID`), `ProfileScope(...)`, `RepoScope(...)`.

Audit metadata is assembled by `Invoke` and nowhere else: `action` (default: the part of `Name` after the module, e.g.
`get`; it can be overridden so the existing strings stay byte-identical), `origin` (`"mcp"` from the MCP adapter, as
`mcp.meta` adds today; absent from the bridge, as today), plus `Scope.Meta`. `audit.RedactMap` still runs inside the
gateway. The rule "ids and counts, never text" (note titles, SQL, queries, PR bodies) becomes a test instead of a
convention (§9, canary test).

## 5. Exposure and agent-only concepts

### 5.1 Exposure

| Value | Bridge method | MCP tool | Reason required |
| --- | --- | --- | --- |
| `Both` | yes | yes | no |
| `UI` | yes | no | **yes**. Examples: "irreversible: empties the trash", "overwrites the note (version restore)", "edits what agents may spend (model profiles)", "carries the master password" |
| `Agent` | no | yes | **yes**. Examples: `codex.hookEvent` ("lifecycle hook from an external Codex CLI"), `agenthub.sendMessage` ("a session speaks to a session") |

"UI-only" means *the person's window*. Remote-human (Telegram) callers are not in this enum. They keep their own
boundary (`telegramcontrol`) and are out of scope (§11).

### 5.2 Agent-only concepts are fields, not handler code

| Concept today | Where it lives today | In the registry |
| --- | --- | --- |
| **Active target** (db/ssh/kafka sticky profile, selection prompt) | `mcp/active_*.go`, `GateActiveMCP` | `Target: &Target{Kind: interaction.KindDatabase}`. `Invoke` step 3. The env tag it returns feeds confirmation. A UI op names its profile in its input and has no `Target` |
| **Task ownership** (push / `pr.*` writes only on a branch recorded on a task the session holds) | `TaskBranches`, `prAgentOwns` | `Guards: []Guard{AgentHoldsBranch(func(in) string)}`. A guard declares which caller kinds it applies to (agents only here). Its refusal is a published error code |
| **Granted folders** (git writes only in `granted` folders; reads anywhere in the workspace) | `mcp/git_repo.go` | input field `repo` + `RepoScope` resolver + `Guards: {GrantedFolder}` on writes. The resolver turns `repo` into the folder for both adapters (the window passes its open repository) |
| **Session-scoped data** (`pdf.*`, `workspace.memory.*` by the session's workspace) | each handler | a resolver reads `identity.Caller.SessionID`. A terminal caller must pass `sessionId` / `workspaceId`. Unchanged behaviour, declared once |
| **Result budget** (24 KB cut, compact shapes, `budgetExempt`) | `mcp/result_budget.go` middleware + per-tool shapes | the middleware stays global and unchanged. `Agent.Shape func(Out) any` holds the compact shape. `Agent.BudgetExempt string` (a reason) replaces the `budgetExempt` map. The tool-definition budget tests run over the registry |
| **Pre-approved / gate-owned tools** | `Server.PreApprovedTools`, `gateOwnedTool` | `Agent.PreApproved string` (reason), and `GateOwned` derived from `Confirm != Never`. Both lists are computed from the registry |
| **Timeouts an agent needs because it has no Stop button** (`agentQueryTimeout`) | inline in handlers | `Agent.Timeout`, applied by the MCP adapter only |

When the window and an agent need different inputs or outputs for the same thing (`DatabaseQuery` takes a profile
and a page size; `db.query` takes SQL and uses the active target), they are **two ops that share one handler
function**. A contract test then checks that twins share subject and capability. The registry does not force one
op per concept. It forces each *surface* to be declared once, and the parts they share to be shared, not copied.

### 5.3 Enforcing exposure at the gateway

Today an agent cannot reach a UI-only method only because no tool calls it. That holds as long as nobody reaches
an `AppService` method from an MCP handler, and nothing checks it. The registry adds the check where authorization
already happens:

- `capabilities.CallRequest` gains `Operation string` and `Exposure`. It is a plain value, so the gateway stays
  free of domain imports.
- `Gateway.Call` refuses a `UI` op for a caller whose kind is not `ui`, and an `Agent` op for a `ui` caller. The
  refusal is recorded like a policy denial and wrapped in `ErrPermissionDenied` with reason `"not exposed to <kind>"`.
- Callers without a stamp keep today's meaning: `bridge.callerContext` stamps them as UI. `uiOnly` and
  `requireWindow` stay in place as defence in depth until their module is migrated, and are then deleted. The
  exception is `agenthub.requireWindow`: it guards service entry points that are not only reached through the bridge.

This is the only change to `internal/capabilities`, and it adds a check without removing one.

### 5.4 The MCP adapter

`mcp.RegisterOps(ms, rt, ops...)` replaces each `registerX`:

- `sdk.Tool{Name, Description}`, annotations from `Risk` (`Read` → `ReadOnlyHint`, `Destructive` → `DestructiveHint`,
  `External` → `OpenWorldHint`). The input schema is inferred from `In` (the SDK already does this from `json` /
  `jsonschema` tags, so existing names such as `folder_id` stay). The handler is
  `Invoke(ctx, rt, op, in)` → `Agent.Shape` → `structured`.
- Per-identity server instances (`sdkServerFor`) and the two middlewares (`withCaller`, `withResultBudget`) are unchanged.
- Tools that do not fit yet stay hand-registered. The ratchet test (§9) lists them.

## 6. The bridge adapter and TS binding compatibility

Wails v3.0.0-beta.32 binds every exported method of the bound service by reflection. The binding ID is
`FNV-32a("devkit/internal/bridge.AppService.<Method>")` (`internal/generator/collect/service.go:222`,
`pkg/application/bindings.go:251`). Arguments are positional, and a leading `context.Context` is not part of the TS
signature (`service.go:276-282`). So the frontend keeps working unchanged if and only if:

1. the bound type stays `bridge.AppService` in package `devkit/internal/bridge`,
2. the method name stays the same,
3. the non-context parameters keep their order and Go types,
4. the return types stay the same Go types, so `models.ts` is unchanged.

Go cannot add methods at run time, so each bridge method stays a written method. It becomes a one-line shim:

```go
func (s *AppService) NotesGet(ctx context.Context, id string) (notes.Note, error) {
	return ops.Invoke(s.uiCtx(ctx), s.rt, ops.NotesGet, ops.NotesIDIn{ID: id})
}
```

Adding `ctx context.Context` to methods that lack it today (all of `bridge/notes.go` uses `context.Background()`) leaves
the ID and the TS signature unchanged. It also gives these calls Wails' cancellation for free.

**Recommendation: handwritten shims plus a reflection test, not code generation, at least for the pilot.** The shims
are trivial, review is easy, and Go doc comments on them still become TS doc comments. The test (§9) fails when an op
with UI exposure has no shim, when a shim's `Bridge` name and method name disagree, or when an `AppService` method is
neither a shim nor on the legacy list. If after two modules the shims prove to be busywork, a `go generate` step
(`cmd/opsgen`, reading the registry's reflect types) can write `bridge/zz_ops.go`. That is open question Q4.

**Verification for every migrated module:** regenerate bindings (`wails3 generate bindings -clean=true -ts -i`). The diff
of `frontend/bindings/devkit/internal/bridge/appservice.ts` must contain no changed `ByID` number, parameter list or
return type. Doc comments may move. Then `bun run build` and the module's frontend tests.

## 7. Grants

- Each op declares the grant **pattern** it needs (`Grant: "note:*"`). Grants are not derived as `"*"` from the scope
  resolver, because a grant is a security decision and must stay readable in review.
- `ops.Grants(all...)` returns the de-duplicated `(subject, capability, grant)` triples. `app.NewContainerWithStores`
  builds the policy from `toolGrants` + `ops.Grants(...)` + the legacy list. The legacy list shrinks module by module
  and must end empty. The tools module already does this (`tools.Commands` → grants), and the registry generalises it.
- A grant that is not also a request scope (for example `note:new`) is covered because `"note:*"` matches it. The
  test in §9 runs each op's resolver and checks the result against the real policy, so a too-narrow pattern fails.
- Grants stay per subject, not per caller. The window and agents share `builtin.notes`. Narrowing what an agent can do
  is the job of exposure, guards and confirmation (§5, §8). Moving to per-caller grants is a larger change and out of
  scope (§11).

## 8. Confirmation policy and the person's decision D1

**D1 (the person's decision, an input to this design):** unattended agent work is allowed, except where the
target's environment tag blocks automatic action (for example `prod`). There the person is asked every time.

The field:

```go
type Confirm struct {
	Policy  ConfirmPolicy // Never | EnvTag | AskOnce | Always | Custom
	Callers []identity.Kind // who it applies to; default: agent + user-terminal
	Summary func(in any) string // the exact text shown (today's ConfirmRequest.Summary)
	Kind    interaction.Kind
	Custom  func(ctx context.Context, d Deps, in any) (ConfirmRequest, bool, error) // Policy == Custom only
}
```

| Policy | Behaviour | Prompt offers allow-always | Today's examples |
| --- | --- | --- | --- |
| `Never` | proceeds | – | reads, notes writes, tasks writes |
| `EnvTag` | **D1 default for target-bound ops.** Proceeds unless the env tag `RequiresConfirmation` (including fail-closed for an unknown tag). In that case it asks every time | no | `GateProfileMCP` (`db.explain`, `db.schema.get`, `*.testConnection`) |
| `AskOnce` | asks, offers allow-always. An env tag that blocks wins and asks every time | yes, unless env blocks | `GateActiveMCP` (`db.query`, `ssh.runCommand`, `kafka.*`), `pr.comment|reply`, credential-free `api.*` writes |
| `Always` | asks every time, whatever the tag | no | `pr.create|update|merge|close|deleteBranch`, credential-carrying `api.*` writes, a child's model above its parent's, a cross-workspace peer message |
| `Custom` | the op computes whether and what to ask | per request | `tasks` terminal transition (board `verify` gate), `api.sendAdhoc` with a secret |

The policy is computed in one function (`ops.decide(policy, envBlocks, caller)`), which calls `interaction.Hub.Confirm`.
The Hub's semantics do not change: an env block disables allow-always, and headless fails fast.

**How D1 enters the code:**

1. A **new** op that touches a tagged target defaults to `EnvTag`. `AskOnce` needs a written reason in the
   declaration (a contract test checks this).
2. **Existing** `AskOnce` ops (`db.query`, `ssh.runCommand`, `kafka.publishMessage`, …) contradict D1 on non-prod
   tags: today they ask once per caller and tool. Changing them changes what the person sees, so this spec does not
   do it silently. Recommendation: switch each one to `EnvTag` in the PR that migrates its module, and say so in that
   PR (Q1).
3. `Always` ops are a separate class. They are irreversible or leave the machine regardless of any env tag (a merge,
   a deleted branch, a credential sent to an agent-chosen host, money spent on a model). Recommendation: D1 does not
   relax them (Q2).
4. The person's own window keeps its prompts (prod `git push`, guarded commits, prod `api.send`, task gates). These
   are ops with `Callers: {ui}` or `Custom`, unchanged (Q3).

Audit: today a declined confirmation is not a gateway event. The gateway records "allowed" and the handler then
returns `ErrConfirmationDenied`. Recording `declined` / `confirmed` as a second gateway event would make the audit
log say what the person decided. It is a small addition to `CallRequest` (a confirmation hook run by the gateway) but
not needed for the pilot (Q6).

## 9. Contract tests derived from the registry

These replace the hand-written drift tests. Each one iterates over `ops.All()`:

1. **Every op is granted** (`TestEveryOpIsGrantedByTheRealPolicy`): run `op.Scope` on a zero input and on a canary input,
   then `NewContainer().Permissions.Check`. This replaces `TestNewContainerGrants*`, `mcp.PullRequestGrants`,
   `GitSyncGrant`, `AgentHubModelsGrant`, …, and the fake-`Caller` blind spot that let `git.fetch` ship refused.
2. **No orphan grants** (after full migration): every default grant is the `Grant` of some op or of `tools.Commands`.
3. **Bridge surface matches** (`TestBridgeShimsMatchTheRegistry`): every UI/Both op has an `AppService` method named
   `op.Bridge` whose non-context parameters and returns match the shim's mapping. Every exported `AppService` method is
   a shim or on `legacyBridgeMethods`, a list that may only shrink (a ratchet, like `moduleBoundaryRatchet`).
   `TestBoundSurfaceCarriesNoFuncs` stays.
4. **MCP surface matches**: every Agent/Both op is a registered tool. No UI op is. Every tool not built from an op is
   on `legacyTools` (it may only shrink). The tool-definition budget (`TestToolDefinitionsStayWithinBudget`) and
   `TestGateOwnedToolsStillSayTheyAsk` read the registry.
5. **Exposure is enforced**: for every UI op, `Invoke` with an agent caller returns `ErrPermissionDenied` *from the
   real gateway*, and the audit sink saw a denial.
6. **Reasons are written**: `UI`/`Agent` exposure, `Desktop`, `AskOnce`, `Agent.BudgetExempt` and `Agent.PreApproved`
   each need a non-empty reason.
7. **Risk ⇒ confirmation**: a `Destructive` or `External` op with `Confirm: Never` must name what makes it safe
   (`notes.delete`: "reversible for 30 days from the trash").
8. **Audit canary**: fill every string field of `In` with a canary (`"CANARY-…"`), run the resolver, and check the canary
   appears in neither `Scope.Value` nor `Scope.Meta`. Free text (titles, SQL, queries, bodies) can then never reach a
   scope or the audit log by accident.
9. **Twins agree**: ops declared as twins share subject and capability.
10. **Error codes published**: an op may list the error codes it raises. `TestEveryMCPErrorCodeIsPublishedOrRedactedOnPurpose`
    can read that list instead of scanning source.
11. `go test ./internal/arch/` gains: `ops` may not import `storage`, `syncstore`, `sync`, `crypto`, `app`, `bridge`
    or `mcp`. `capabilities` may not import `ops`. Only `bridge`, `mcp`, `main` (and a future adapter) import `ops`.

## 10. Adapters: server mode and a future non-Wails UI

The registry is the API. An adapter is whatever turns a transport's calls into `ops.Invoke` with a stamped caller.

**Wails desktop (today).** `bridge.AppService` shims. Caller: UI.

**Server mode (today, `-tags server`).** It is already the same `AppService` served by Wails over loopback HTTP
(`POST /wails/runtime`). It stays the Wails adapter with `Runtime.ServerMode = true`. The registry adds one thing:
`Desktop` on an op is refused uniformly in step 2 of `Invoke` instead of by ad-hoc checks. The headless Hub
(`main.go:981`) still makes every prompt fail fast. What server mode does **not** get: authentication. Its callers are
still stamped UI, so any local process is "the window". That is today's accepted risk and stays out of scope (§11).
A dedicated HTTP adapter (JSON-RPC over loopback with a per-launch token, built from the registry, no Wails) becomes
possible and small, but this spec does not build it.

**A future native UI, for example MyGo.** I do not know MyGo's architecture, so this depends on Q8:

- *If the UI is written in Go in the same process*, the adapter is direct typed calls: `ops.Invoke(identity.WithCaller(ctx, identity.UI()), rt, ops.NotesGet, in)`.
  No JSON, no bindings, no `AppService`. The registry is everything the UI needs, and grants, audit, exposure and
  confirmation come for free.
- *If it keeps a webview with the current TS frontend*, the adapter serves the same IDs and arguments as Wails'
  `$Call.ByID`, or ships its own generated bindings from the registry's reflect types. Either way it is one package.

Not covered by the registry, and therefore real work for any UI switch (§11): Wails **events** (`notes:changed`,
`apiclient:changed`, `agentaccounts:changed`, `prwatch:changed`, …), native dialogs (`saveFileDialog`,
`openDirDialog`, `openFilesDialog` on `AppService`), window/menu/theme calls in `main.go`, and file drop. An inventory
of `Event.Emit` call sites should come before a switch decision. A later spec could put events behind a small
`EventBus` interface, but that is not part of A1.

## 11. Out of scope

- Per-caller grants, or any change to `permissions.Policy` matching.
- Authentication for server mode, or closing its "every caller is UI" gap.
- Renaming any MCP tool or input field, or changing any bridge method name or signature beyond adding `ctx`.
- Frontend changes. TS bindings must come out unchanged (§6).
- Events, dialogs, menus, window management, file drop (§10).
- Telegram / remote-human control (`telegramcontrol`). It keeps its own boundary.
- Behaviour changes to confirmation in modules other than the pilot. D1 lands per module (§8, Q1).
- Methods that do not reach the gateway today (for example `ListModules`). During migration they become ops with
  `Ungated: "<reason>"` and appear on a list, but their behaviour does not change here.
- Vault unlock/setup internals, vaultitem, sync and crypto. The registry sits above the domain services and imports
  none of the forbidden packages (§9.11).

## 12. Migration plan

Each step is its own PR, keeps `go test ./internal/... ./cmd/...`, `go test ./internal/arch/`, `bun run build` and
`bun run test` green, and ends with the bindings diff check from §6.

**Step 0: the machinery (no module moves).** `internal/ops` (`Op`, `Operation`, `Invoke`, `Runtime`, scope helpers,
`decide`), `CallRequest.Operation/Exposure` + the gateway exposure check (§5.3, with its own tests), the arch rules,
and the contract tests with empty legacy lists computed from today's code (so they pass from day one, and every
later PR only removes entries).

**Step 1: notes (pilot).** 21 bridge methods, 11 tools, 9 grants. It is a good pilot because it has every shape except
targets and guards: reads, writes, a reversible destructive op, 10 UI-only methods with reasons already written, and a
vault dependency (locked → `vault_locked`).

- 11 `Both` ops: list, search, get, create, update, delete, set_organization, folder.list/create/update/delete.
- 10 `UI` ops with reasons: listTrash / restore / purge / emptyTrash ("irreversible, reachable only from the Trash
  screen", from `bridge/notes.go:136`); checkpoint / listVersions / getVersion / restoreVersion ("restoring overwrites
  the note; an agent should not roll one back", from `bridge/notes.go:170`); listBacklinks / resolveLinks ("render
  helpers, not requested for agents", Q5).
- Keep byte-identical: action strings, scope strings, MCP input JSON names (`folder_id`), descriptions, annotations.
  Bridge shims gain `ctx`.
- Delete: `mcp/tools_notes.go` handlers and `registerNotes`, the `recordID` copy in `internal/mcp`, the 9 notes grants
  from the hand-written list, and the duplicated UI-only comments (the reasons now live in the declarations).
- Done when: bindings diff empty, every test in §9 runs over notes, and a manual check in server mode (Notes view and
  `notes.*` from a terminal MCP client) behaves as before.

**Step 2: workspace memory** (8 / 8). Session-scoped resolver, CAS errors, and "no body unless `includeBody`" as `Agent.Shape`.

**Step 3: tasks** (23 / 17). First `Guards` (task ownership) and the first `Custom` confirmation (board gate).

**Step 4: api client** (29 / 14). `Custom` confirmation (`Service.Carries`), `Always` for environments and deletes,
`ForAgent` masking as `Agent.Shape`, `Desktop` for import/export.

**Step 5: database, ssh, kafka** (40+12+12 / 9+4+7). `Target` and env-tag confirmation. **D1 lands here** for
`db.query`, `ssh.runCommand` and `kafka.*` if the person answers Q1 "yes".

**Step 6: git and pull requests** (34+3 / 21+11). Granted folders, push-class grants, `Always` for `pr.*` writes, history refusals.

**Step 7: codegraph** (23+22+3+8 / 22). Result budgets, `budgetExempt` (`codegraph.readFile`), paged defaults.

**Step 8: agent hub, accounts, preferences** (47+12+13+… / 12+3). Mostly `UI` ops with reasons. `Desktop` replaces
`errAgentAccountsDesktopOnly`. `uiOnly` is deleted.

**Step 9: the remainder** (sync, vault, updates, terminal, telegram, uistate, workfolder, mcp_install, interaction,
environments, search, tools). Almost all `UI`. Bulk-convert, then empty the legacy lists and turn the ratchets into
"must be empty".

The pilot's size (21 methods) and the totals (425 / 151) suggest steps 2–9 are mechanical except 3, 5 and 6. Those
three each need a reviewer who knows the module's gate.

## 13. Open questions for the person

1. **D1 vs "ask once" tools.** `db.query`, `ssh.runCommand`, `kafka.*`, `pr.comment|reply` and credential-free `api.*`
   writes ask once per caller and tool today, even on a dev tag. D1 says unattended work on non-blocking tags is
   allowed. Switch them to `EnvTag` when their module migrates? *Recommendation: yes, each in its own PR, stated in
   the PR description.*
2. **D1 vs "always" tools.** `pr.create|update|merge|close|deleteBranch`, credential-carrying `api.*` writes, a child's
   model above its parent's, and cross-workspace messages ask every time regardless of tag. Keep them outside D1?
   *Recommendation: yes. They are irreversible or leave the machine, and an env tag does not describe them.*
3. **The window's own prompts** (prod push, guarded commits, prod `api.send`, task gates) stay as they are?
   *Recommendation: yes.*
4. **Bridge shims: handwritten or generated?** *Recommendation: handwritten plus the reflection test for the pilot.
   Revisit after step 3.*
5. **Notes' UI-only reads** (`listBacklinks`, `resolveLinks`, `listVersions`, `getVersion`): keep them UI-only, or expose the
   reads to agents? *Recommendation: no change in the pilot. The pilot should change nothing the person can see.*
6. **Audit the confirmation outcome** as a gateway event (`confirmed` / `declined`)? *Recommendation: yes, as a follow-up
   after the pilot.*
7. **Server mode**: accept that it stays unauthenticated ("every caller is the window") under the registry, with
   `Desktop` ops refused there? *Recommendation: yes for A1. A token-authenticated HTTP adapter is a separate decision.*
8. **MyGo**: will the native UI render in Go in-process, or keep the webview and the TS frontend? This decides whether
   that adapter is direct typed calls or a bindings-compatible transport (§10). It does not change the registry.
