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
  `budgetExempt` tools cannot be overridden. `headTail` keeps both ends of an over-budget
  **plain-text** result under one byte budget across all text blocks, with a marker between
  that says how many bytes lie in the middle and that the ends are not adjacent (marker
  included, never over the budget; UTF-8- and line-safe; the head cut is unchanged). Structured
  results keep their structural shrink and cursor rule, errors and fixed exemptions are never
  cut, and the cut mode comes from the same call-admission snapshot as the size. Measurements
  written from the H5b cutter on carry version `text-utf8-v2` and the algorithm that actually ran
  (`none`, `head`, `headTail`, `structured`, `exempt`, `error`, `unrecorded`); rows written before
  it (`text-utf8-v1`, whose cut mode is the literal `head` whatever happened) are never relabelled
  or backfilled, and a report adds both versions once each. Host text bytes only: nothing about
  tokens, quota or cache savings.
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

### One prepared launch for every runtime (H3b)

`Service.Harness` (`HarnessSnapshots`, nil by default) makes CreateSession, `resumeRuntime`
(Load, reload, dead-runtime and account reopen) and both rewind opens (Open, Fork) derive their
launch from `prepareLaunch`: resolve the snapshot, gate it, open the endpoint, fill the spec.
Nothing is published until the runtime opened.

- **Order.** Snapshot → gate → endpoint (staged with the policy when the endpoint is a
  `PolicyEndpoints`) → spec. The endpoint never exists in a default-on state first. Early tool
  calls are served under the prepared identity once H4 implements `OpenWithPolicy`; until then
  only a snapshot that serves exactly today's tools can launch.
- **Gate.** A profile whose served groups/budgets differ from the default needs an enforcing
  endpoint; without one the launch is refused (`harness.notEnforceable`) before anything opens,
  never run unfiltered. An unresolvable snapshot is refused (`harness.snapshotUnavailable`). A
  reopen that would change the executable set after W moved is refused
  (`harness.newSessionRequired`): executable changes take effect in a new session only. A V-only
  change (edited in Settings) still reloads as it always has.
- **Reload preflight.** `ResumeSession(reload)` runs the same resolution and gate before it
  closes the live runtime, so a refused reload leaves the running session untouched on the
  configuration it opened with.
- **Launch configuration.** For a profiled session the effective deny lists, servers and skill
  folders replace the vendor customization in the `OpenSpec`; with no W and no S the spec keeps
  the vendor customization exactly as before.
- **Publication.** After a successful open the service records `appliedHarness` (reference and
  whether the endpoint enforces it) with the usage facts; W/S revisions reach attribution only
  when the endpoint enforces them. A failed open closes the staged endpoint and leaves the
  previous record. The record survives runtime teardown (account move, reload, crash) and is
  replaced only by the next successful open. It is in memory: it does not survive a restart (H3c).
- **Unchanged.** Admission seals still run first (a sealed start resolves and opens nothing),
  concurrent resumes share one barrier and one preparation, accounts, mode, model and leases are
  untouched.
- No production code sets `Service.Harness` or an enforcing endpoint yet, so nothing changes at
  runtime.

### Durable applied reference and session-override lifecycle (H3c)

- **Applied reference (migration 55).** `agent_session_harness_applied` keeps, per session, the
  `SnapshotRef` of what its runtime was last opened with (ids, revisions, fingerprint,
  executable digest, whether the endpoint enforced it): no definition, path, command, env value,
  prompt or token. It is written **before** a create/resume publishes its runtime; if it cannot
  be written the launch is refused and the staged endpoint closed (a create also removes its
  row). A rewind writes it in the **same transaction** as the vendor session id, transcript
  cut, turn/checkpoint deletions and safety-row completion. A failed reference write rolls back
  that transaction and discards the staged fork; it is never accepted with an older reference.
  A crash after commit but before runtime publication still leaves the fork's reference on disk.
- **What it is for.** After a restart (memory empty) a reload or resume still compares the new
  snapshot with the stored reference: if W moved and the executable digest differs the reopen is
  refused (`harness.newSessionRequired`). DevKit **cannot reconstruct** the old permitted
  executables from a reference, so it refuses instead of restoring from today's different values.
  An unreadable reference refuses the launch (`harness.snapshotUnavailable`).
- **Explicit close.** `CloseSession` commits the status, then calls `HarnessLifecycle.SessionClosed`
  (app adapter → `HarnessStore.ClearOverride`: a tombstone with the next revision, idempotent) and
  keeps the applied reference in memory and on disk: Close parks a conversation, and the next
  prompt can reopen the same session, so its executable guard must survive Close and restart.
  Override cleanup failure is returned as `ErrHarnessCleanupFailed` (the session *is* closed);
  closing again retries and keeps stamps monotonic. Delete closes each session first, then its
  rows and durable references cascade; in-memory references are dropped only after deletion succeeds.
- **Close vs approval.** `HarnessStore.Apply` refuses a closed session inside its transaction, so an
  approval that checked scope before the close cannot commit S after it: either the close
  committed first (the write is refused) or the write committed first (the cleanup removes it).
  Reload, resume, rewind, account moves, crashes and runtime teardown do not call the cleanup.
- Not yet: the read model (H3d), a real `HarnessLifecycle`/`Harness` wiring in the container, and an
  end-to-end close-vs-apply test against the H2c `Apply` (H2c is not in this branch's base).

### Status read model and attribution (H3d)

`Service.HarnessStatus(ctx, sessionID)` is an internal read (no caller authorization; the
transport that exposes it, H5, decides who may ask). It starts nothing and calls no model.

| State | Meaning |
| --- | --- |
| `unconfigured` | no harness provider, or the session has no workspace |
| `unavailable` | the saved configuration cannot be resolved now (applied record still shown) |
| `not_applied` | no runtime has opened under a harness yet |
| `current` | applied equals what the saved configuration resolves to now |
| `reload_required` | saved ≠ applied and a reload would adopt it |
| `new_session_required` | saved ≠ applied and the change brings in a new or altered executable from W |

`Desired` (saved W/S/V resolved now) and `Applied` (what the runtime was opened with) are always
separate references; `Enforced` says the applied endpoint serves the applied policy,
`AppliedFactsComplete` says the vendor's own servers/skills were readable (false → the reference
describes DevKit's configuration, not a verified account of the vendor's), and `Live` is always
false until H4 publishes override changes to the running endpoint. A saved edit therefore reads as
`reload_required`, never as applied. The status agrees with the reopen gate by construction (both
use `SnapshotRef.AdoptsNewExecutables`).

**Attribution.** Turn rows take their `ProfileRevision`/`OverrideRevision` from the runtime facts
captured at turn start, and those carry W/S revisions only when the endpoint enforced the
snapshot. A saved edit mid-session, a failed open or a refused reload changes nothing already
recorded; an unenforced configuration stays NULL; historical rows are never rewritten. The MCP
per-call observer reading the live serving snapshot is H4's.
### Confirm, recheck and commit (H2c)

`Service.Apply(ctx, proposalID)` is the only way a prepared proposal becomes a write. Order:

1. **Take it.** Only the proposing caller can; the proposal is spent whatever happens next
   (approval, denial, stale, DB error, cancellation), so no late callback revives it and a retry
   needs a fresh `Prepare`. Concurrent duplicates: one proceeds, the rest get "not pending".
2. **Fail closed.** No `Confirmer`, `Confirmer.Available()` false (server mode, headless hub, no
   prompt handler) or no `Boundary` → `ErrConfirmUnavailable` before anything is evaluated or
   written, **auto-narrowing included**. The setting only skips the question when someone could
   have answered.
3. **Ask with nothing held.** Existing `interaction.Hub.Confirm` with `Kind=harness`,
   `EnvBlocks=true`: no "always" option, an "always" answer is one allow and is never
   remembered, the D1 scoped `db.query` approval cannot apply (it requires `Tool=db.query`).
   The prompt shows the Go-authored summary; the rationale is appended under an "untrusted
   text" label.
4. **Recheck inside the boundary.** `preferences.Service.HoldAgentCustomization` is the same
   mutex as `SetAgentCustomization`/`SetAgentSandbox`, so the real V writer cannot land between
   the recheck and the commit. Identity, scope, policy, V, W, S and vendor facts are re-read and
   the effective before/after rebuilt; the digest must equal the one shown. W moved →
   `harness.profileConflict`; anything else changed (V, S, vendor facts, policy, scope, registry,
   tier) → `harness.proposalStale`; an executable tier that appears after approval is refused.
5. **Commit once.** `HarnessStore.Apply` with the expected W/S stamps in one short transaction;
   a CAS lost in the gap between the recheck and the commit maps to the same two codes.
6. **Result.** `Result{Profile, Override, Live, ReloadRequired, Commit, Publication}`. Without a
   `Live` port (the default) `Live` is false and the change is *stored*, not applied to any
   runtime. With one, see "Live publication of S (H4c)" below: `Live` is true only when the
   requester's current endpoint acknowledged the exact committed serving identity.

Composition adapters live in `internal/app/harness_config.go` (vendors from preferences with
`FactsComplete=false`, hub confirmer, boundary, directory, validator, default policy) and
`agenthub.Service.HarnessScope` is the trusted session → workspace/vendor read. No production
code builds the service yet: it needs a registry source and a public transport (H4/H5).
`EventAudit`-style records (`Event`) hold outcome, tier, target, ids and revisions only.

### Composition and activation (H4d; activated by R4)

`app.NewHarnessAssembly` / `Attach` compose the graph below and are tested through the real stack; `main.go` does not
call them at H4d; R4 now calls `app.StartHarness` from `main.go`, so production runs the harness. The vendor adapter now reads V for every registered vendor (opencode
included) and refuses others. Gate table, decision, blockers and the H5 handoff:
[Harness composition and activation](harness-composition-activation.md).

### Live publication of S (H4c; active since R4)

Plan: `docs/superpowers/plans/2026-10-27-harness-h4c-live-publication-implementation.md`. A committed
change to S either installs its exact serving policy on the session's **current** endpoint or leaves that
endpoint unable to admit calls. No production `Harness` is constructed yet (H4d), and there is no public
transport, editor or policy setting (H5); everything below is reachable only through the internal service.

- **Three values, never one.** *Launch-applied* is the durable reference and executable baseline of the
  launch (`harnessApplied`, `SetHarnessApplied`); a live S change never writes it, so Close/restart/rewind
  guards (#353) are unchanged. *Serving-applied* is what the current endpoint acknowledged (in memory only,
  `HarnessStatus.Serving`). *Desired* is the saved V/W/S resolved now. `HarnessStatus.State` still compares
  desired with the launch reference, so a live S publish never makes pending W/V drift look current.
- **Pinned launch basis (C1).** `harness.ServingBasis` freezes the launch's registry, V, W and every path
  resolution the launch made (`CanonRecorder`), plus the W and S stamps it read (a tombstone keeps its
  revision). `Snapshot.Basis` is built from the same single `Resolve`, verified by re-resolving, and is
  immutable. `Basis.Resolve(S)` is the only live resolution: the ordinary merge over the pinned V/W and the
  latest durable S. It refuses a result whose executable digest differs, so S can change tool groups only; a
  retargeted symlink, a later saved W or V, and mutated source slices cannot reach a running endpoint.
- **Approval covers the actual effect (C2).** `evaluate` computes the saved effect and, for the requester's
  running session, the pinned-basis effect (`liveEffect`: incarnation, epoch, served identity, pinned V/W
  revisions, before/after fingerprints, per-group availability and result-budget range). The strictest tier of
  the two decides, **before** the `harness.noChange` test: saved W may already allow a group the running
  session's W blocks (a live-only widening must ask) and a revoke may do nothing live. The confirmation shows
  "Saved configuration" and "This running session" as separate sections, never merged. An endpoint that is
  paused, or not serving the stored S, is `harness.notAvailable` rather than a synthetic default. Enabling a
  group adds no pre-approval. At Apply the same evaluation is rebuilt under the coordinator and must equal the
  digest: W moved → `harness.profileConflict`; incarnation, epoch, serving identity or activity moved →
  `harness.proposalStale` (a replacement endpoint with an identical policy still needs a fresh proposal).
- **One barrier (C3).** Per-session `servingCoord` (Agent Hub) serializes every S writer, endpoint staging
  and the session's close. Lock order: V boundary (`HoldAgentCustomization`) → coordinator → host locks
  (`bindMu`, `mu`, the holder's transition lock); `Service.mu` is only taken inside the coordinator, never
  the reverse; nothing waits for a person, vendor or prompt under it. Apply: re-evaluate → **quarantine**
  (admission is one atomic `admissionState`, so a call is admitted under the old policy only if it loaded it
  before the quarantine) → `beginCommit` (a Cancel before it wins and restores) → `Store.Apply` (W+S CAS,
  one transaction) → publish. Handlers already running finish under their entry snapshot; nothing is drained,
  so a proposal that is itself an MCP call cannot deadlock.
- **Discovery changes with enforcement (C4).** `servingHolder.install` runs the registry update while paused:
  tools that must return are re-registered by `Server.registerAll` (the one declaration of the registry; same
  names replace, no handler is duplicated), tools that must go use `RemoveTools`, then policy and admission
  reopen in one store. `tools/list` is refused while paused (no half-updated registry), pagination cursors are
  name-based in SDK v1.7.0 so a cursor issued before a revoke cannot reveal a hidden tool (tested), and policy
  endpoints explicitly set the SDK tools capability so an initially empty endpoint still advertises `listChanged`. The SDK's debounced
  notification is reported as *scheduled*, never as an acknowledgement; "live" does not mean the vendor
  refreshed its tool list (`HarnessServing.DiscoveryReloadRequired`).
- **Durable success, failure, reconciliation (C5).** `store.ErrStoreCommitUnknown` marks the only ambiguous
  case (`tx.Commit` failed); every other store error is a proven rollback and restores the exact paused
  admission (ticket + incarnation must still be current). An unknown outcome is read back (`Expect+1`
  committed, `Expect` rolled back, anything else unknown and the endpoint stays paused). After a commit the
  latest durable S is read on a host context (post-commit cancellation cannot undo it) and installed; any
  failure keeps the endpoint paused and returns `*PublicationError` carrying the committed `Result` (stamps,
  `Commit`, `Publication`). The audit event says `applied` with the publication state. Retrying publication is
  `Service.Reconcile`: one read of the latest durable S under the coordinator, a new ticket that supersedes
  any older operation, install, no retry loop; concurrent calls serialize and the later finds nothing to do;
  it starts no runtime, sends no prompt and cannot revive a closed session (no scope / no endpoint → nothing).
  After a process restart there is no incarnation to resurrect; the next normal launch reads S.
  No new public error code: `harness.snapshotUnavailable` is already in `bridge.keptCodes`.
- **Every opening path is fenced (C6).** `prepareLaunch` (create, resume, reload, dead-runtime recovery,
  account reopen, both rewind opens) reserves the coordinator epoch before it reads S, registers the endpoint
  with the coordinator before the vendor exists (a commit now reaches the staged endpoint like a published
  one). `publishServingLaunch` then holds the coordinator across final epoch/incarnation validation,
  the durable launch write (the cut+reference transaction for rewind), and runtime-map publication.
  No S commit or Close can slip between the check and publication. Vendor handlers/settings and checkpoint
  work run outside this lock. A stale launch is discarded (`agenthub.ErrHarnessLaunchStale`, code
  `harness.snapshotUnavailable`); `prepareLaunch` re-prepares at most 3 times, with no prompt replay or
  new account choice. Close quarantines the endpoint and keeps an opening/commit fence until its status
  and S tombstone cleanup both finish; callbacks cannot reopen from the old S. Failed Close remains fenced
  in the current process until a successful Close retry. Resume's final idle write is also fenced, so a Close
  after publication cannot be undone. Delete drops the coordinator only after rows are gone.
  Reload/resume/rewind/account teardown keep S. Children resolve their own S. Old endpoint operations cannot
  touch a replacement. A policy installed on a staged endpoint reports `PublicationStaged`, never `Live=true`;
  the vendor runtime may still fail or be discarded. The staged/published phase is part of proposal binding.
- **Truthful status, guidance, attribution (C7).** `HarnessStatus.Live` = the current endpoint is open,
  has a successfully published vendor runtime, serves exactly the identity it acknowledged and nothing is pending (point-in-time). `servedFor` (all host
  guidance: peer envelope, workspace context, documents) follows the acknowledged surface, so new turns
  describe what is served; the launch value handed to adapters, the launch permission lists and the launch
  prompt are never mutated and a newly served tool follows the normal permission flow until a reload.
  `Service.ReplacementGuidance` returns the bounded H4b text only for a group the endpoint has acknowledged and
  the launch did not offer, from the whole new served set; otherwise the caller says a reload is required.
  Future turn admissions copy the served S revision (replaced, never mutated, so a started turn keeps its facts;
  a paused or unacknowledged endpoint is not recorded; a tombstone is a NULL revision). Per-call observations
  still use the call-entry snapshot of the endpoint, so a turn's start facts and a call's snapshot can differ
  by design. The shared-memory preamble names the get tool only when `workspace.memory.get` is served;
  user-authored entries, briefs and peer bodies are passed through untouched.
- **Not done / open.** H4d (production construction and activation acceptance) and H5 (public get/propose
  transport, editor, policy setting) are out of scope. Whether Claude, Codex or opencode refresh a tool list
  after `listChanged` is **not measured**; nothing claims it. A rewind-open stale race has the shared code path
  but no dedicated test with a real fork. Vendor-side revocation of a launch pre-approval is not claimed (the
  endpoint refuses by group, the vendor's allow list is as launched).
