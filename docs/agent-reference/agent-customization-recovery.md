### Agent customization and recovery
- **Settings → Agents is its own nav group of seven one-topic pages**, not one tab
  (`frontend/src/modules/settings/settingsNavigation.ts` is the model, `AgentsPages.tsx` the pages):
  Runtimes (`agents-runtimes`, + Codex observed sessions), Models & subagents (`agents-models`),
  Accounts & limits (`agents-accounts`, several logins per vendor and failover, see **Accounts UI**),
  Skills & MCP (`agents-skills`, the tools/skills/MCP report and `AgentCustomization` overrides,
  `AgentEnvironmentPanel`), Safety (`agents-safety`, sandbox + file checkpoints), Notifications & remote
  (`agents-notifications`, + Telegram) and DevKit as MCP (`agents-mcp`). Runtimes and Skills & MCP carry a shadcn
  `Tabs` Claude | Codex | opencode switch (Accounts & limits has Claude | Codex only) whose last choice is shared and kept in `localStorage`
  (`agentVendorChoice.ts`, guarded); the other pages are vendor-neutral. The old ids `agents`, `remote` and `mcp`
  stay valid for callers (`requestSettings("agents")`): `resolveSettingsTab` maps them to Runtimes, Notifications &
  remote and DevKit as MCP. A new agent setting is a panel on the page whose topic it is, plus keywords on that
  page's `SETTINGS_GROUPS` entry — not a new stacked section.
- Settings → Agents → Tools, skills and MCP manages local imports and per-vendor
  overrides (`preferences.AgentCustomization`, in the existing sandbox preference
  JSON). Dedicated UI-only get/set bridge methods use revision/CAS. General sandbox
  saves preserve this map; they cannot overwrite it from a stale Settings form.
- Imported skill folders are read-only links, never copied or deleted at source.
  Codex uses native skill config (both canonical file and folder forms for runtime
  compatibility); Claude uses a temporary local plugin cleaned up with its process.
  MCP definitions support stdio and HTTP, with environment variable names only.
- Claude's inherited toggles deny skill/MCP tool invocation, not MCP startup.
  Vendor source deny rules remain authoritative. Local source updates are applied
  by Refresh + Reload; no remote installer/update command runs automatically.
- Resume/Reload is UI-only and reuses the persisted native conversation ID without
  replaying a prompt. Reload reserves the session against Invoke and resume while
  closing its idle runtime. A missing native conversation stays an explicit error.
  Startup recovery adds a durable interruption notice; model/effort/mode are restored
  before attaching live handlers. User-requested Retry turn remains separate.


## Effective harness configuration (`internal/harness`)

`internal/harness` is the one definition of a session's effective configuration (tool
groups, result budgets, deny lists, servers, skill folders). It is a stdlib-only leaf
(`internal/arch` enforces it): the registry, the vendor customization (V), the vendor's own
environment facts, the workspace profile (W), the session override (S) and path
canonicalization are all injected, so nothing else may re-implement the merge. H1 ships the
contract only; no service calls it yet and behavior is unchanged.

- `Resolve(Inputs)` returns an `Effective` or a typed `*harness.Error`. An invalid input is
  rejected, never skipped: a failure means "do not open", not "fall back to unfiltered".
  With no W and no S every group is on at the 24 KiB default (`mcp.ResultBudget` is pinned
  equal to `harness.DefaultResultBudget`).
- Absent vs present-empty: nil vs empty non-nil with `omitzero`. W and S carry a schema
  version and a revision; a missing W or S is a nil revision, never 0.
- Groups: absent is on, W can turn one off, S can only turn one on; unknown groups are
  `harness.groupUnknown`. `harness.profile.propose` is the one tool served with its group off.
- Budgets: tool, then group, then default, each of size and cut separately; 2–64 KiB;
  `budgetExempt` tools cannot be overridden. `headTail` is rejected (`harness.budgetInvalid`)
  until phase 3 implements it.
- Deny lists are V ∪ W. Servers are keyed by name, W replaces V, a duplicate or a name the
  vendor environment already defines is rejected. Skill folders are V ∪ W by canonical path.
- `Classify(before, after)` compares two effective values: executable > widening > narrowing.
  A deny entry whose removal exposes a vendor-defined server or skill, or an imported skill
  folder, is *executable* (a denied imported skill is not in the executable set); a
  cut-mode-only change is neutral in the narrowing tier; budgets compare per tool on the
  effective value (deleting a 16 KiB override widens).
- `Effective.Fingerprint` (`sha256:v1:`) covers the V/W/S revisions, the registry's tool names
  and exemptions, the vendor-defined servers/skills (name, command, args, URL, env names,
  sorted) and the canonical targets of imported skill folders and denied skills, so re-pointing
  a symlink moves it. Nil and empty args/env names are one value (storage drops empty lists).
  No env values, no definition sizes.
- Known gaps for later slices: `EnvironmentReport` entries carry no command/arguments, so
  the host must add them before a vendor-defined server can be fingerprinted (spike 9); a stored
  profile naming a tool or group that a later build removed makes `Resolve` fail, so H2/H5 need
  a repair path.

### Harness profile storage (H2a)

`storage.HarnessStore` (migration 54) persists W (`workspace_harness_profiles`) and S
(`session_harness_overrides`). It is internal: the future harness service and composition
call it; no UI, bridge or MCP write path exists yet.

- **Tokens.** `harness.Stamp{Revision, Present}`. Every write, a delete included, bumps the
  revision and a delete leaves a tombstone, so "absent at 3" differs from "absent at 0" and a
  delete/recreate never repeats a token. Revision 0 = never existed. A present record's
  `Profile.Revision`/`Override.Revision` is the column; an absent one is a nil H1 input, never 0.
- **Writes.** `Apply(StoreWrite)` compares each record's expected revision inside one
  transaction (insert-if-absent from 0, CAS afterwards). `*harness.ConflictError` names the
  record that moved (`profile` → `harness.profileConflict`, `override` → stale proposal). W+S
  commit together or not at all; a failure after W rolls W back (tested with a SQL trigger).
- **Scope.** The workspace must exist and the session's `workspace_id` must equal it. S stores
  the workspace it was written under; reading it under another workspace is `ErrStoreScope`,
  so an override never follows a session that moved. Deleting the session or workspace
  cascades; closing or reloading a runtime does not touch S (H3 owns that hook).
- **Reads.** A stored document with an unknown schema, invalid JSON, an unknown field or
  trailing data is `ErrStoreCorrupt`, never "no profile" (that would turn every group on).
  Absent vs present-empty fields survive the store; S groups are sorted and de-duplicated.

### Harness proposals (H2b, `internal/harnessconfig`)

`harnessconfig.Service.Prepare` turns a request into an exact, host-authored effective diff
and keeps it pending in memory. It writes nothing, asks nobody and has no public transport.
It reaches storage, sessions, preferences, policy and validation only through ports, and
imports only `internal/harness` and `internal/identity` (architecture-tested).

- **Identity is the caller's, not the request's.** The caller comes from `identity` on the
  context: an agent targets its own authenticated session's workspace (a request naming another
  workspace or session is denied); the UI names a workspace and, optionally, a session that must
  belong to it; a terminal CLI, a remote person or no caller is denied. Scope is re-read from
  the directory port on every evaluation.
- **One resolver.** Candidates are built on deep copies of W and S, then `harness.Resolve`
  and `harness.Classify` run for the before and the after, per vendor (a workspace change is
  judged for every vendor, a session change for the session's vendor). The patch itself is never
  classified. Sets are sorted and de-duplicated first, so equal intents bind identically.
- **Facts fail closed.** A patch that edits deny lists, servers or skill folders needs the
  vendor's own server/skill facts; if the host reports them incomplete (today's
  `EnvironmentReport` has no commands) or unreadable, `harness.factsUnavailable`, never an
  empty environment. A group- or budget-only patch needs none.
- **Executable proposals are unavailable.** The prompt cannot yet show the exact command,
  arguments, resolved paths and env names, so an executable-tier proposal is refused
  (`harness.profileDenied` when the UI opt-in is off, `harness.notAvailable` when on). A
  truncated summary is not an approval. H5 supplies the structured view.
- **Binding.** The evaluation holds caller, session, workspace, target, normalized patch,
  rationale, policy revision, W/S stamps, per-vendor V revision, fingerprints before and after,
  tier and the canonical changes; its digest must be reproduced by a fresh evaluation. The
  proposal ID is a random lookup key held in memory for the proposing caller; it is not an
  approval, nothing accepts it from anyone else, restart drops it, and there are at most 32.
- **Tiers.** Narrowing asks unless the UI-only auto-narrowing setting is on; widening always
  asks. The agent's rationale is cleaned, bounded and shown labelled untrusted; it never
  influences target, tier or summary.

### Runtime snapshots (H3a)

`harness.Snapshot` is the resolved effective configuration plus the identity it was resolved
for; `harnessconfig.Provider.Snapshot` produces it from the same store/vendor/registry ports
and the single `harness.Resolve`. Nothing in Agent Hub calls it yet (H3b).

- **Two references, never one.** `harness.ModelProfileRef` (vendor/model/effort) selects a
  model; `harness.SnapshotRef` identifies the harness. The same model can run under different
  snapshots and a snapshot never changes the model.
- **SnapshotRef** holds schema, workspace/session/vendor ids, the V/W/S revisions (W and S nil
  when they did not exist, never 0), the effective fingerprint, an `ExecutableDigest` (what could
  run or load; order- and revision-independent, follows resolved symlink targets),
  `FactsComplete` and `Default`. It carries no path, command, argument, instruction, env value
  or endpoint token; a digest is never a permission.
- **Default vs unavailable.** No W and no S is a positively resolved `Default` snapshot.
  Anything that cannot be resolved (corrupt W or S, vendor read error, no registry, a profile
  naming a removed group) is `harness.snapshotUnavailable`; the launch must be refused, never
  run with unfiltered defaults. A present-but-empty W is not `Default`, although it serves the
  same tools.
- **Scope.** The request comes from Agent Hub's own session row. A fresh child asks with its own
  new id, so it cannot see its parent's S; an S written under another workspace does not follow
  the session and is not applied.
- **Facts.** When the host reports the vendor's own servers and skills as incomplete they are not
  used at all and `FactsComplete=false`: the snapshot describes DevKit's configuration, not a
  verified account of what the vendor runs. Do not present it as a fully checked TEAM
  configuration.
- **Detached.** `Snapshot.Clone`/`Effective.Clone` share no memory; the provider returns a clone,
  so a later change to a request, a candidate or a store row cannot alter an opening or live
  snapshot. `Effective.ServingIsDefault` says whether the MCP server would serve exactly what it
  serves today, i.e. whether anything needs the server to enforce the configuration.
