# TEAM C1: bind preflight and packages to actual harness snapshots

Date: 2026-10-29. Status: **implemented on `feat/team-c1-harness-snapshot` (tasks 0-7, awaiting review; not merged)**. Written against `origin/main` `6d142f04` (#393). The original text below is the plan as prepared; where the implementation differs, `docs/agent-reference/agent-hub-coordination.md` ("Managed harness binding (C1)") is the record. The binding table is update-blocked and insert-only through the service (D5's deliberate absence of a DELETE trigger stands; "append-only" in the text below means that). It authorizes no account/model use, activation, dispatch, push or merge.

Parent: R5 in [remaining acceptance and TEAM handoff](2026-10-27-harness-remaining-acceptance-team-implementation.md). It continues C1 from [Harness Phase 2 and TEAM Slice C](2026-10-26-harness-phase2-team-snapshot-implementation.md) and Slice C of [TEAM harness integration](2026-10-26-team-harness-integration-implementation.md). It does not replay their old tasks: H1-H5a and the ownership/package slices have since landed, and the "current state" below replaces their starting assumptions.

## Goal (the bounded C1 deliverable, verbatim scope from R5)

1. A host-owned intended fresh-child configuration plus the actual child's `HarnessSnapshotRef`, reached through narrow interfaces and composition adapters.
2. Preflight of intended W/V before the child exists, and of the actual child (identity, scope, grants, mode, required groups, snapshot) before enqueue eligibility. A missing group returns configuration-needed. No parent-S inheritance, no automatic self-enable, no account/model change, no shell fallback.
3. A schema-versioned snapshot binding on the immutable package/dispatch inputs, separate from the model profile. Configuration staleness is a different check from the v2 task requirements digest. Rebuild goes through supported new package/attempt semantics; a historical package is never overwritten.
4. Historical turn/call attribution keeps coming from the actual admission snapshot. No new collector, no backfill from current settings.

Stop at one C1 PR. Preflight success authorizes no result acceptance, push or merge. Production dispatch, persisted operation IDs, task assignment, enqueue and recovery are the later slice (E).

## Current state (verified in code at 6d142f04)

### TEAM side: what exists

- `internal/agentwork` is a stdlib-only leaf, enforced by `internal/arch/arch_test.go` (`graph[pkg("agentwork")]` must be empty). It cannot import `internal/harness`; any harness type must be mapped by a composition adapter.
- Ledger and CAS: `ledger.go` (`GroupState`, `WorkItem`, `Attempt`, `CanTransitionAttempt`: `reserved -> session_created -> task_assigned -> prompt_enqueued -> running`), `internal/storage/managed_ledger.go` (`BeginAttempt` fenced by item revision, requires `current_attempt_id IS NULL`; `TransitionAttempt`; `MarkAttemptAmbiguous`).
- Requirements digest v2: `requirements.go` (`RequirementsDigest`), carried as `RequirementsRevision` on item, attempt and package. It deliberately excludes everything harness-related.
- Immutable encrypted packages: `managed_packages.go` (`ManagedPayloadService.BuildContextPackage` needs attempt state `reserved`; `ContextPackage` has no harness or model field; `ManagedPackageSchemaVersion = 1` is inside the AEAD associated data via `PayloadBinding`), `storage/migration_managed_packages.go` migration 53 (`agent_work_packages.attempt_id TEXT NOT NULL UNIQUE`, so one package per attempt, no replacement path), `managed_references.go` / `CheckPackageReferences` (staleness reported without mutation; the model for this slice's `CheckBinding`).
- Leases: `managed_leases.go`, `internal/app/managed_ownership.go` (`ResolveManagedWriter` already validates the child row: workspace, parent equals the group coordinator, not closed, registered folder scope, worktree identity, grants contained in the checkout). The admission seal and release barrier are in `agenthub` (`SealWriterAdmission`, `enterWriterAdmission`).
- Composition: `main.go:1117-1118` calls `app.WireManagedTeams` and `app.WireManagedOwnership(container, managedLedger)`; `Container.ManagedTeams/ManagedLeases/ManagedPayloads` (`internal/app/container.go:170-175`). There is **no dispatcher, no public managed AppService/MCP tool, no `team.*` tool group** (confirmed: no `team.` tool name in `internal/mcp`).
- `agentwork.Handoff` (`handoff.go`) is the existing B7 task handoff; it knows nothing about harness.

### TEAM side: what is missing (found while tracing)

- No harness field anywhere in package, attempt, item or lease. Nothing records which W/V/S a child was meant to run with or did run with.
- **Nothing binds a session to an attempt after `BeginAttempt`.** `agent_work_attempts.session_id` is written only by `BeginAttempt` (managed_ledger.go:458); `TransitionAttempt` changes state only. The child's ID is minted inside `agenthub.CreateSession` (`id := uuid.NewString()`, service.go), so a dispatcher cannot pre-bind it. That session-binding step belongs to slice E. C1's actual-child check therefore **requires `attempt.SessionID` to be already bound** and refuses with `harness_child_unbound` otherwise; C1 must not invent a second way to set it.
- No supported way to start a second attempt: `BeginAttempt` refuses while the item points at an attempt, and release/reassignment stay unavailable until the writer stop proof exists (agent-hub-coordination.md, "Managed writer stop proof"). So "rebuild through new attempt" cannot be executed yet; C1 can only detect and report it.

### Harness side: what exists

- `internal/harness` (stdlib leaf): `SnapshotRef` (schema 1; W/S revisions are `*int64`, never a synthetic 0; `Fingerprint`, `ExecutableDigest`, `FactsComplete`, `Default`), `Snapshot{Ref, Effective, Vendor, Basis}`, `Effective.GroupOn`, `ModelProfileRef` (separate type from harness; **not used by any code today**), `harness.Group(tool)`.
- `internal/harnessconfig.Provider.Snapshot(ctx, SnapshotRequest{SessionID, WorkspaceID, Vendor})`: one read of W, S and V. A fresh child "asks with its own new id, so it cannot see its parent's override". Any resolve failure is `ErrSnapshotUnavailable`, never a default.
- `internal/agenthub`: `HarnessSnapshots` port (`Service.Harness`), `prepareLaunch` / `harnessGate` / `launchKind` (`launchCreate` vs `launchReopen`), the applied reference (`appliedHarness`, `Service.HarnessApplied`, durable `agent_session_harness_applied` migration 55, `previousHarness`), `Service.HarnessStatus` (Desired vs Applied vs `HarnessState`: current / reload_required / new_session_required / not_applied / unavailable / unconfigured; `Serving{Active, Acknowledged, Paused, Pending}`), `Service.HarnessScope`. `CreateSession` gives a fresh uuid, so S for a new child is structurally empty, and inherits mode (`currentMode`, unexported) and `ExtraReadWritePaths` from the parent.
- Attribution is done: `publishHarnessLocked` copies W/S revisions into `runtimeUsage` facts only when the endpoint enforces; turn rows take them at turn start; `noteServedFacts` never relabels a started turn. Existing tests: `TestTurnRowsCarryTheAppliedRevisionsAtTurnStart` (`agenthub/harness_status_test.go`), `TestHistoricalAttributionIsNeverRewrittenByTheHarness` (`app/harness_composition_compat_test.go`). C1 changes no attribution code.
- `app.NewHarnessAssembly` / `ComposeHarness` build the whole graph from narrow ports and can be built in tests with a real `Provider` and fake store (see `internal/app/harness_composition_rig_test.go`).
- **Production is inert**: `main.go` does not call `ComposeHarness`; `Service.Harness` is nil (guard `TestProductionDoesNotWireTheHarnessYet`, `internal/app/harness_public_test.go:744`). `HarnessStatus` then answers `unconfigured`.
- `SnapshotRef` has no group list. Group state must be read from the child's served surface (`servingServed`, unexported) or from `Effective` of a fresh resolve; there is no exported read of served tools.
- Vendor facts: the production `harnessVendors.State` always reports `FactsComplete=false` (`internal/app/harness_config.go:26,56`), so every real snapshot says it describes DevKit's configuration only.

### Error publication

Harness refusals already published in `bridge.keptCodes` (`internal/bridge/errors.go:739-741, 940-953`): `harness.snapshotUnavailable`, `notEnforceable`, `newSessionRequired` and the profile codes. `internal/bridge/mcp_error_codes_test.go` (`TestEveryMCPErrorCodeIsPublishedOrRedactedOnPurpose`) fails if a tool raises an unpublished code.

## Design

### D1. Where each piece lives

| Piece | Package | Constraint |
| --- | --- | --- |
| Value types, pure evaluation, service, store interface | `internal/agentwork` (new `harness_binding.go`, `managed_harness.go`) | stdlib only; no `harness` import; defines its own `HarnessFacts` |
| Persistence | `internal/storage` (migration 56, `managed_harness.go`) | additive; implements `agentwork.ManagedHarnessStore` on `ManagedLedger` |
| Narrow Agent Hub reads (served tools, permission mode) | `internal/agenthub` | generic; no vendor logic |
| Adapter: role->groups table, harness->facts mapping, child checks | `internal/app/managed_harness.go` | the only place that imports both `agentwork` and `harness`; no storage import |
| Capability checks, caller identity, gates | unchanged owners (Tasks, Agent Hub, gateway, MCP) | C1 adds no tool, no AppService method |

### D2. agentwork types (`harness_binding.go`)

```go
const HarnessBindingSchemaVersion = 1 // this record; separate from package schema and from harness.SnapshotSchema
const (HarnessPhaseIntended = "intended"; HarnessPhaseActual = "actual")

// IntendedChild is the host-owned description of the child about to be created.
// It names vendor and role only; model, effort and account stay under the
// existing PlanModelChoice / account policy and are deliberately absent.
type IntendedChild struct{ Vendor, Role string }

// HarnessFacts is the secret-free, host-mapped identity of one resolved
// configuration. Mapped from harness.SnapshotRef by the adapter.
type HarnessFacts struct {
    SourceSchema                       int    // harness.SnapshotSchema as read; unknown => refuse
    Vendor, WorkspaceID                string
    VendorRevision                     int64
    WorkspaceRevision, SessionRevision *int64 // nil = input absent; never 0
    Fingerprint, ExecutableDigest      string
    FactsComplete, Default             bool
}
// HarnessResolution adds the groups that are on (intended) or served now (actual).
type HarnessResolution struct{ Facts HarnessFacts; Groups []string }

type HarnessBinding struct { // one immutable row
    ID string; SchemaVersion int; Phase string
    WorkspaceID, BoardID, ParentTaskID, GroupID, WorkItemID, TaskID, AttemptID, PackageID, Role string
    SessionID string   // actual phase only
    Required  []string // sorted group names the role needed when bound
    Facts     HarnessFacts
    CreatedAt time.Time
}

type HarnessHost interface { // implemented by the app adapter
    RequiredGroups(ctx context.Context, role string) ([]string, error)
    ResolveIntended(ctx context.Context, scope PackageScope, child IntendedChild) (HarnessResolution, error)
    ResolveActual(ctx context.Context, attempt Attempt) (HarnessResolution, error) // checks identity/scope/grants/mode inside
}

type ManagedHarnessStore interface {
    GetManagedAttempt(context.Context, string) (Attempt, error)
    GetCurrentManagedAttempt(context.Context, string) (Attempt, error)
    GetWorkItem(context.Context, string) (WorkItem, error)
    GetWorkGroup(context.Context, string) (GroupState, error)
    GetContextPackage(context.Context, string) (StoredContextPackage, error)
    InsertHarnessBinding(ctx context.Context, b HarnessBinding, expectedAttemptRevision int64) error // ErrHarnessBindingExists on duplicate
    GetHarnessBinding(ctx context.Context, attemptID, phase string) (HarnessBinding, error)          // ErrManagedRecordNotFound when absent
}
```

`HarnessRefusal{Code string; Groups []string}` is the only refusal shape (bounded fixed codes; group names are registry names, never a value). `func ConfigurationNeeded(err error) bool` is true for the codes the person can fix by configuration.

| Code | Meaning | Configuration-needed |
| --- | --- | --- |
| `harness_not_configured` | no harness resolver bound (production today) | yes |
| `harness_unavailable` | resolve/read failed or reference unreadable (retryable, fail closed) | no |
| `harness_schema_unsupported` | unknown binding or source schema | no |
| `harness_override_present` | actual child has an S (intended is W+V only) | yes |
| `harness_required_group_off` + `Groups` | a role-required group is off, not served now, or revoked | yes |
| `harness_config_changed` | actual W/V/fingerprint/executables differ from the bound intended facts | yes |
| `harness_endpoint_not_live` | endpoint not acknowledged, paused, pending, or closed | no |
| `harness_child_scope` / `harness_child_mode` / `harness_child_unbound` | identity, scope, grants, mode refusal; session not bound to the attempt | no |
| `harness_binding_stale` / `harness_binding_missing` | stored binding no longer matches / absent | no |

Check order is fixed (and tested): schema, unbound/scope/mode, unavailable, endpoint live, override present, required group off, config changed. A refusal never falls through to a weaker check.

### D3. Service (`managed_harness.go`)

`ManagedHarnessService{store, host, codec-free}` (no vault needed: binding is metadata only).

- `PreflightIntended(ctx, attemptID, child IntendedChild) (HarnessResolution, error)`: read only. Attempt must be current and `reserved`, group enabled and not paused. Validates required groups against the resolved groups. Call before `CreateSession`.
- `BindIntended(ctx, packageID, child, expectedAttemptRevision) (HarnessBinding, error)`: runs the same preflight, requires the package to belong to that current attempt, inserts the intended binding. Does not touch the package row.
- `BindActual(ctx, attemptID, expectedAttemptRevision) (HarnessBinding, error)`: attempt state `session_created` or `task_assigned`, `SessionID` bound; requires the intended binding; `ResolveActual`; compares with the intended facts; inserts the actual binding. The check does not run for any other attempt state.
- `VerifyForEnqueue(ctx, attemptID) error`: read only; re-runs `ResolveActual` and requires equality with the stored actual binding and required groups still served. The dispatcher calls it at the side-effect boundary, together with its own admission barrier; it never sends or queues anything.
- `CheckBinding(ctx, attemptID) (HarnessBindingStatus, error)`: report-only (`current | stale | missing`, plus `RebuildRequired bool` and the refusal code), shaped like `CheckPackageReferences`. A host failure is an error, not a status.

Comparison (`compareFacts`, pure): vendor, workspace, V revision, W revision (nil==nil), fingerprint, executable digest, `FactsComplete`, `Default` must all be equal; the session ID and `SourceSchema` mismatch rules are separate. Model profile, account, effort and the requirements digest are inputs to nothing here.

Staleness is two independent checks: v2 `RequirementsDigest` through the existing item/attempt/package paths, and the harness binding through `CheckBinding`. Neither feeds the other and neither changes the digest algorithm or `ManagedPackageSchemaVersion`.

**No rebuild in C1.** A stale binding returns `RebuildRequired`. The supported way to refresh is a new attempt, a new package and a new binding, and that is slice E's saga (it needs a supersede/release path that is blocked on the writer stop proof). C1 proves only that nothing is overwritten.

### D4. Binding is a sidecar, not a package change

The package body is sealed at `reserved`, before any child exists, and its schema version is in its AEAD. Rather than bump the package schema and re-seal, C1 adds an append-only sidecar table keyed by `(attempt_id, phase)` that references the package. This keeps `ManagedPackageSchemaVersion = 1`, leaves all stored packages readable, and keeps the intended/actual split natural (the intended binding exists before the child; the actual one after). It also works with the vault locked, because it holds only ids, revisions, digests and group names.

### D5. Storage (migration 56, version to re-check at implementation)

Next free version at base is 56 (`internal/storage/sqlite.go` ends at `m55`); re-read the list before coding, since another branch may take it. Additive `CREATE TABLE IF NOT EXISTS`; no change to existing tables.

```sql
CREATE TABLE IF NOT EXISTS agent_work_harness_bindings (
  id TEXT PRIMARY KEY,
  workspace_id, board_id, parent_task_id, group_id, work_item_id, task_id TEXT NOT NULL,
  attempt_id TEXT NOT NULL, package_id TEXT NOT NULL,
  phase TEXT NOT NULL CHECK(phase IN ('intended','actual')),
  binding_schema INTEGER NOT NULL CHECK(binding_schema > 0),
  source_schema INTEGER NOT NULL CHECK(source_schema > 0),
  role TEXT NOT NULL CHECK(role IN ('coordinator','implementer','reviewer','integrator')),
  session_id TEXT, CHECK((phase = 'actual') = (session_id IS NOT NULL)),
  vendor TEXT NOT NULL, harness_workspace_id TEXT NOT NULL,
  vendor_revision INTEGER NOT NULL CHECK(vendor_revision >= 0),
  workspace_revision INTEGER CHECK(workspace_revision IS NULL OR workspace_revision > 0),
  session_revision INTEGER CHECK(session_revision IS NULL OR session_revision > 0),
  fingerprint TEXT NOT NULL CHECK(length(fingerprint) BETWEEN 1 AND 200),
  executable_digest TEXT NOT NULL CHECK(length(executable_digest) BETWEEN 1 AND 200),
  facts_complete INTEGER NOT NULL CHECK(facts_complete IN (0,1)),
  is_default INTEGER NOT NULL CHECK(is_default IN (0,1)),
  required_groups TEXT NOT NULL CHECK(length(required_groups) <= 1024),
  created_at TEXT NOT NULL,
  UNIQUE(attempt_id, phase),
  FOREIGN KEY(...) -- same composite scope FKs as agent_work_packages (group, item, attempt) plus
  FOREIGN KEY(package_id, workspace_id, group_id, work_item_id, attempt_id)
    REFERENCES agent_work_packages(id, workspace_id, group_id, work_item_id, attempt_id));
CREATE TRIGGER IF NOT EXISTS agent_work_harness_bindings_no_update BEFORE UPDATE ON agent_work_harness_bindings
  BEGIN SELECT RAISE(ABORT, 'harness binding is immutable'); END;
```

Notes: no model, effort, account, command, path, URL, argument or environment column (a test asserts the column list); revisions are nullable and never 0 for W/S; unknown `binding_schema`/`source_schema` on read fails closed (`ErrManagedInvalid`), like the ledger's unknown versions. Deletes are left to the foreign keys and the future manual retention cleanup (no DELETE trigger, so C1 does not block it). `InsertHarnessBinding` runs in one transaction that re-reads and fences on attempt revision, the current attempt pointer, attempt state for the phase, group enabled and not paused, item not tombstoned, and package ownership by that attempt; the duplicate `(attempt_id, phase)` returns `ErrHarnessBindingExists`.

A refused preflight writes nothing. The configuration-needed reason is returned to the caller; **persisting it durably is slice E's operation checkpoint**, because C1 has no operation ID or dispatch record to hang it on.

### D6. Agent Hub reads (small, generic)

- `Service.ServedTools(sessionID string) (tools []string, live bool)`: exported read of the endpoint's last acknowledged surface (`servingServed`), `live=false` when no enforcing endpoint is running. Adapter maps tools to groups with `harness.Group`.
- `Service.PermissionModeOf(ctx, sessionID) (modeID string, known bool)` and a pure `ModeNotWiderThan(parent, child string) (ok, known bool)` over the existing `modeRanks`. Rule: the child's mode must not exceed the coordinator's current mode; if both vendors have no mode concept it passes; if the parent has one and the child's cannot be read it fails closed (`harness_child_mode`).

No change to `CreateSession`, `prepareLaunch`, `harnessGate`, the applied-reference store or attribution.

### D7. Adapter (`internal/app/managed_harness.go`)

`managedHarnessHost` holds the container and the existing `managedOwnershipHost`; it reads `c.AgentHub.Harness` **at call time**, never capturing it at wiring, so wiring order relative to `ComposeHarness` does not matter.

- `RequiredGroups(role)`: a fixed table in this file, validated against the real registry names when resolved (an unknown group name is `harness_unavailable`, not ignored). Default declaration, to be confirmed (see open questions): `coordinator`, `reviewer`: `tasks`, `agenthub`; `implementer`, `integrator`: `tasks`, `agenthub`, `git`. Evidence: a child records branch/result with `tasks.*`, reports with `agenthub.sendMessage`, and `git.push` needs a task-recorded branch (agent-hub-coordination.md). No role requires "every group", and the `harness` propose tool is not required (it is always served).
- `ResolveIntended`: `Harness.Snapshot(ctx, probeID, workspaceID, vendor)` where `probeID` is a throwaway uuid that no session owns, so S is absent by construction and parent S is never read; vendor must be in the registered vendor names; workspace comes from the group, not from a caller. Maps `SnapshotRef` to `HarnessFacts` and `Effective` to the groups that are on. `Harness == nil` returns `harness_not_configured`; any resolver error is `harness_unavailable`. A positively resolved unprofiled default is `Facts.Default=true` with every group on, distinct from both.
- `ResolveActual(attempt)`: (1) `ResolveManagedWriter` for identity/scope/grants (reuse, no copy); (2) mode via D6; (3) `HarnessStatus(child)`: refuse unless `State == current` (a W/V change since open is `reload_required` or `new_session_required` -> `harness_config_changed`), `Applied != nil`, and `Serving.Active && Acknowledged && !Paused && !Pending` (else `harness_endpoint_not_live`); (4) facts come from `Applied` (what the runtime was given), never from `Desired`; (5) groups come from `ServedTools`, so a group revoked after launch is seen.

### D8. Wiring

`WireManagedOwnership` also builds `agentwork.NewManagedHarnessService(store, host)` and sets `Container.ManagedHarness`. The store parameter's interface union gains `agentwork.ManagedHarnessStore`, which `*storage.ManagedLedger` satisfies, so `main.go` does not change (this avoids touching the lines R4 edits). Nothing calls the service in production: no dispatcher, no AppService method, no tool.

## Dependency on R4 (production activation, `feat/harness-activation`)

No unmerged R4 code is required. C1 builds and tests against `internal/harness`, `harnessconfig.Provider`, `NewHarnessAssembly` and `agenthub.HarnessStatus`, all on main. The only coupling is behavioral: until R4 attaches `Service.Harness`, the production adapter answers `harness_not_configured` and no managed work can pass preflight. That is the intended fail-closed outcome and costs nothing because production dispatch does not exist. Items that would need R4 (all excluded from C1): proving the preflight against a real attached production graph, and native evidence that a TEAM child's applied reference is published. The migration number and `main.go` are the two merge-conflict surfaces; C1 avoids the latter by design and re-checks the former at implementation time. If R4 changes the `HarnessSnapshots` or `HarnessStatus` signatures, rebase the adapter and its tests only.

## Task list (small, TDD; each task = one commit)

Run every command with the caps: `systemd-run --user --scope -q -p MemoryMax=5G timeout 1500 env GOMEMLIMIT=2GiB go test -p 1 -count=1 <pkg> -run '<regex>'`. Write each failing test first and see it fail for the stated reason.

**Task 0: re-verify the base.** Fetch, branch from current main, re-read `sqlite.go` migration list (confirm 56 is free), `grep -rn "SessionID" internal/storage/managed_ledger.go` (confirm still no session-binding method), re-read `feat/harness-activation` diff for changes to `agenthub.HarnessStatus`/`HarnessSnapshots`. No code. If a session-binding method now exists, use it and drop `harness_child_unbound` from D2.

**Task 1: facts, refusal and pure comparison (`internal/agentwork`).**
Files: `harness_binding.go`, `harness_binding_test.go`.
Failing tests: `TestCompareFactsIgnoresSessionIDModelAndRequirementsDigest` (equal facts differing only in session id compare equal; adding a model or digest field is impossible: reflect over `HarnessFacts` field names and assert none contains model/effort/account/digest/path/command); `TestCompareFactsNilRevisionIsNotZero` (nil W vs `*0` differ); `TestCompareFactsDetectsWorkspaceVendorFingerprintAndExecutableChange`; `TestRefusalCheckOrderIsFixed` (table: several simultaneous faults return the documented first code); `TestConfigurationNeededOnlyForFixableCodes` (`harness_unavailable` is false, `harness_required_group_off` true); `TestUnknownSourceSchemaFailsClosed`.
Implement the types, constants, `HarnessRefusal`, `ConfigurationNeeded`, `compareFacts`, `checkResolution`.
Verify: `go test -p 1 -count=1 ./internal/agentwork/ -run 'Harness|CompareFacts|Refusal|ConfigurationNeeded'` with the caps.

**Task 2: service with fakes (`internal/agentwork`).**
Files: `managed_harness.go`, `managed_harness_test.go` (fake store and host).
Failing tests: `TestPreflightIntendedMissingGroupReturnsConfigurationNeededAndWritesNothing`; `TestPreflightIntendedDistinguishesNotConfiguredFromResolvedDefault` (`harness_not_configured` vs `Default=true` passing); `TestBindIntendedRefusesAnAttemptThatIsNotReserved`; `TestBindIntendedLeavesThePackageRowUntouched` (store spy: no package write, body hash unchanged); `TestBindActualRequiresBoundSession` (`harness_child_unbound`); `TestBindActualRefusesChildWithOwnOverride` (parent S isolation seen as `harness_override_present` even when every group is served); `TestBindActualDetectsWorkspaceOrVendorChangeBetweenIntendedAndOpen`; `TestBindActualSameModelDifferentHarnessIsConfigChanged` (fake host: same vendor/model, different fingerprint); `TestBindActualRevokedGroupIsRequiredGroupOff`; `TestBindActualConfigReadFailureIsUnavailableNotDefault`; `TestVerifyForEnqueueSendsNothingAndDetectsDrift` (host drifts after bind -> refusal; no store write, no state transition); `TestCheckBindingStaleIsReportedWithoutMutationAndRebuildRequired`; `TestSecondBindForTheSameAttemptNeverReplacesTheFirst` (`ErrHarnessBindingExists`, original returned unchanged); `TestRequirementsDigestAndHarnessStalenessAreIndependent` (requirement change leaves binding current; harness change leaves `RequirementsDigest` equal).
Implement the service per D3.
Verify: `go test -p 1 -count=1 ./internal/agentwork/` with the caps.

**Task 3: storage, migration 56.**
Files: `internal/storage/migration_managed_harness.go`, `managed_harness.go`, `managed_harness_test.go`; one line in `sqlite.go` (add `m56`).
Failing tests: `TestManagedHarnessBindingsMigrationFreshAndUpgradeFrom55` (and idempotent, and data in 52-55 tables survives); `TestHarnessBindingTableHasNoModelAccountOrSecretColumns` (PRAGMA table_info allowlist); `TestHarnessBindingRowIsImmutable` (UPDATE aborts; second insert for `(attempt_id, phase)` is `ErrHarnessBindingExists`); `TestHarnessBindingRejectsCrossScopeAndForeignPackage`; `TestInsertHarnessBindingFencesAttemptRevisionStateAndPause` (stale revision, wrong state for phase, paused/disabled group, tombstoned item each leave no row); `TestHarnessBindingZeroWorkspaceRevisionIsRejected`; `TestHarnessBindingUnknownSchemaFailsClosedOnRead`; `TestPackageRowAndLeaseSurviveBinding` (package `body_hash`/sealed bytes and the lease row identical before/after insert).
Verify: `go test -p 1 -count=1 ./internal/storage/ -run 'ManagedHarness|HarnessBinding'` with the caps.

**Task 4: Agent Hub reads.**
Files: `internal/agenthub/harness_served.go` (+ mode helper next to `mode_inherit.go`), tests.
Failing tests: `TestServedToolsReportsTheAcknowledgedSurfaceNotTheLaunch` (live revoke removes a tool; closed endpoint returns `live=false`); `TestModeNotWiderThanRanksAndFailsClosedOnUnknown`; `TestPermissionModeOfUsesLiveRuntimeThenTranscript`.
Verify: `go test -p 1 -count=1 ./internal/agenthub/ -run 'ServedTools|ModeNotWiderThan|PermissionModeOf'`.

**Task 5: adapter and real-graph scenarios (`internal/app`).**
Files: `managed_harness.go`, `managed_harness_test.go` (build the real `Provider` + `NewHarnessAssembly` rig; reuse the existing harness rig helpers).
Failing tests: `TestIntendedUsesAThrowawaySessionSoParentOverrideIsNeverRead` (the fake store records `GetOverride` ids: only the probe id, never the parent); `TestFreshChildDoesNotInheritParentOverrideThroughCreateSession` (parent has S enabling a group; child created through the real `CreateSession`; actual facts have `SessionRevision == nil`); `TestAbsentRequiredGroupBlocksBeforeAnyPrompt` (W turns `git` off; implementer refused; the fake runtime records zero prompts); `TestWorkspaceEditDuringChildOpenIsConfigChanged`; `TestVendorCustomizationEditDuringChildOpenIsConfigChanged`; `TestSameModelDifferentHarnessIsDistinguishable` and `TestDifferentModelSameHarnessStaysCurrent` (model change via `InitialConfig` does not change binding facts); `TestStaleEndpointIsNotLive` (endpoint closed, paused or pending); `TestRevokedGroupAfterLaunchIsRefusedAtEnqueue`; `TestConfigReadFailureFailsClosedNotDefault` (provider store errors); `TestNilHarnessReportsNotConfigured` (production state); `TestUnknownRoleGroupNameIsUnavailable`; `TestChildScopeAndGrantsReuseTheOwnershipChecks` (child in another workspace or with wider grants is `harness_child_scope`); `TestChildModeWiderThanCoordinatorIsRefused`; `TestRoleRequirementsAreNotEveryGroup` (no role requires all registry groups).
Verify: `go test -p 1 -count=1 ./internal/app/ -run 'ManagedHarness|IntendedUses|FreshChild|RequiredGroup'` with the caps.

**Task 6: wiring and invariants.**
Files: `internal/app/managed_ownership.go` (build and set `c.ManagedHarness`), `container.go` (field), test additions.
Failing tests: `TestWireManagedOwnershipInstallsHarnessBindingWithoutMain` (container field set; `main.go` untouched); `TestManagedHarnessPreservesPackageLeaseAndSeal` (build package, acquire lease, bind intended/actual, then assert package hash, lease generation/revision and writer admission state are unchanged and no `enterWriterAdmission` was taken by the binding calls); `TestProductionStillDoesNotWireTheHarness` is **not edited** and must stay green; `TestNoNewPublicSurfaceForHarnessBinding` (no AppService method or MCP tool name mentions the binding; `internal/bridge` `TestEveryMCPErrorCodeIsPublishedOrRedactedOnPurpose` stays green).
Verify: `go test -p 1 -count=1 ./internal/app/ -run 'WireManagedOwnership|PreservesPackageLeaseAndSeal|NoNewPublicSurface|ProductionStillDoesNot|ProductionDoesNotWire'` and `go test -p 1 -count=1 ./internal/bridge/ -run TestEveryMCPErrorCodeIsPublishedOrRedactedOnPurpose` with the caps.

**Task 7: docs and final gate.**
Files: `docs/agent-reference/agent-hub-coordination.md` (new "Managed harness binding (C1)" section: sidecar design, codes, what is not done), this plan's status line. Keep `AGENTS.md` unchanged and under 10 KiB.
Validation, each with the caps (serial; nothing in parallel with other heavy jobs):
- `go test -p 1 -count=1 ./internal/agentwork/ ./internal/storage/ ./internal/arch/`
- `go test -p 1 -count=1 ./internal/agenthub/ ./internal/app/ ./internal/bridge/`
- `go vet ./internal/agentwork/ ./internal/storage/ ./internal/agenthub/ ./internal/app/`
- `go test -p 1 -count=1 ./internal/... ./cmd/...` (the repository gate; frontend assets must exist for the root composition check; no frontend or binding change is expected, so `bun` checks are only needed if a build artifact is missing)
- repository lint on the diff (`golangci-lint run --new-from-rev=origin/main ./internal/... ./cmd/...`), as CI does.
Open the PR with terminal `gh`, attach it to the current board task through the normal service, and stop for human review. No auto-merge, no force-push.

## Error codes and publication

C1 adds **no MCP tool, no AppService method and no bridge mapping**, so it introduces no public error code. The `harness_*` refusal codes above are Go-level fixed strings consumed by the later dispatcher; they must not be wrapped into any public error in this PR. Before that dispatcher exposes them, it must either map them to published codes or list them in `redactedOnPurpose` (`internal/bridge/mcp_error_codes_test.go`) with a reason; the recommended single public code is `team.configurationNeeded` for the configuration-needed subset (fixed sentence naming only group names), with every other code redacted on purpose. Reused published codes stay as they are (`harness.snapshotUnavailable`, `harness.newSessionRequired`, `harness.notEnforceable`). Task 6's test guards that C1 leaves the code tables untouched.

## Not in C1

Dispatcher, saga, persisted operation IDs, task assignment, session-to-attempt binding, enqueue, recovery or reconciliation; a supersede/new-attempt/release API (so no actual rebuild); result ingestion, acceptance or review; Team/Timeline dashboard; any public `team.*` tool or AppService method; a `team` tool group; writer stop proof or Linux/bwrap work; artifact transport and retention; model-profile binding (C1 only guarantees the harness binding contains and depends on no model/effort/account); changing `harnessconfig`, `harness`, `CreateSession`, prompt delivery or the attribution collectors; enabling or auto-enabling any group, saving a profile, or choosing a model or account for the child; production activation or any change to `main.go`; native/real-vendor evidence (everything runs with the mock runtime and fakes).

## Decisions (made by the coordinator on the owner's explicit delegation, 2026-10-29)

The owner delegated these choices ("decide and finish the plan"). Each takes the stated default:

1. **Role-required groups:** the D7 table as written (`tasks` + `agenthub` for every role, plus `git` for implementer and integrator). It lives in one table in the app adapter so a later change is one edit with its tests.
2. **`FactsComplete=false`:** accepted and recorded; compared for equality; the binding never claims vendor-verified executables.
3. **A child that already has its own S:** refused as `harness_override_present`.
4. **Clearing an unsent attempt for rebuild:** slice E. C1 only reports `RebuildRequired` and never rebuilds or overwrites.
