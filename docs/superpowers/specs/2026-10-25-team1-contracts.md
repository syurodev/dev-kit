# TEAM1 contracts: managed groups, work items, attempts, packages, results, events

Status: approved policy decisions and intended TEAM1 interface proposal, reconciled 2026-10-25. Task: OPT-76c3b6 (TEAM-S1). Parent design:
[2026-10-24-agent-hub-team-orchestration-design.md](2026-10-24-agent-hub-team-orchestration-design.md)
(§4–13, §16, §18). The policy decisions below were approved by the person on
2026-10-25 and are recorded in the comments of OPT-76c3b6; this document turns them
into interfaces the TEAM1 implementation tasks can code against. This document describes
the approved policy and intended contracts; it is not evidence that the later capabilities
or UI described here exist. Current implementation includes the metadata ledger and scoped host composition
described in the managed-team implementation plan; managed execution and UI remain future work.

Names (`team.*`, table names) are proposals. The original source branch is unchanged.
The repository already has a stdlib-only `internal/agentwork` leaf and metadata ledger;
the source proposal for a separate `internal/workgroup` package is reconciled to that
existing boundary. Do not create a parallel package, ledger or outbox.

## 1. What is decided, and where it lands

| Decision (approved) | Contract consequence |
| --- | --- |
| Workflow mapping is stored per group: ready = `todo`, running = `in_progress`, awaiting_review = `review`, accepted = `done`; `wont_do` is never accepted | `Group.StatusMap` (§4.1). The board OPT has two `active` statuses, so a category alone cannot name the review column |
| A group is enabled by an explicit future opt-in action from the parent task UI. Off by default. Enabling grants no push, merge, background run or approval | `team.group.enable` is UI-only (§10). No code path enables a group as a side effect |
| Requirements digest = description (which carries the acceptance criteria today), `blockedBy`, folder/branch scope. Progress, evidence, comments and status do not change it. `updatedAt` is never a CAS token | `RequirementsDigest` (§5) |
| Only the current coordinator or the person (in the UI) accepts. Workers and reviewers never close a task by any route. Policy that cannot be read keeps the task waiting. A person's override is audited with a reason and is never shown as a review pass | `tasks.TerminalPolicy` (§9) |
| Retention: only ended groups are cleaned, and only by hand. Deleting a task or group while evidence is referenced leaves a tombstone. Starting values 30 days and 200 MB per group, adjustable | `Retention` (§7); numbers are config, not constants |
| Verified target v1: a worktree of its own (B10), committed revisions only; no host-verified label for a dirty tree; the verify command is configured by the person in the UI and an agent never names command or cwd | `Revision` and `Evidence` (§4.5, §8) |
| Review and integration are proposed by default; the person turns auto-dispatch of reviewer and repair rounds (max 2) on per group; integration never starts by itself; push and merge keep their gates | `Group.Policy` (§4.1) |
| 30–60k coordinator context, 3 workers per group, 2 per account and 10 s / 30 s batching are tunable starting values pending OPT-400ff3; auto-repair remains an explicit per-group opt-in and is capped at 2 rounds | `Limits` (§4.1) |
| Seven defaults of design §16: Agent Hub + Tasks plus a Go layer, no outside framework; opt-in per group, same workspace; coordinator gets aggregates; roles reuse, sessions are per task; reviews and evidence tied to a revision, closing and remote effects through today's gates; bounded concurrency, rounds and recovery, no patrol agent; Team/Timeline in one group document on the shared shell, no model call to draw progress | Constraints on every section below |

## 2. Packages and boundaries

The existing stdlib-only leaf package, `internal/agentwork`, is the domain boundary for
shared DTOs, validation, the requirements digest and the existing handoff sequence. Future
TEAM1 types/state validation may extend this package without adding a parallel package;
it imports no other internal package.

| Package | May import `agentwork` | `agentwork` must not import | Owns |
| --- | --- | --- | --- |
| `agentwork` | – | `tasks`, `agenthub`, `git`, `storage`, `syncstore`, `crypto`, `vaultitem`, `bridge`, `mcp`, `app`, `ops` | Shared DTOs, state validation, handoff and versioned requirements digest; future managed-domain types stay in this leaf |
| `tasks` | no | – | Tasks, boards, claim and close gate. Declares `TerminalPolicy` itself (§9) and never imports Agent Hub or `agentwork` |
| `agenthub` | no (reached through adapters) | – | Sessions, runtimes, the durable outbox, `startPeerTurn`/`invoke` |
| `ops` / `mcp` | yes, through `Deps` interfaces | – | Operation declarations (§10) and the thin adapters |
| `app` | yes | – | Future composition responsibility: build the SQL store, codec and adapters; wire `tasks.TerminalPolicy`; fan out `tasks.Service.OnChanged` if a safe cache is introduced. The current metadata foundation has no cache or OnChanged fan-out and recomputes requirements at use. |

`internal/arch` keeps the `agentwork` stdlib-leaf boundary enforced; architecture tests
stay green as the existing code is extended.

Interfaces `agentwork` declares and the composition root implements:

```go
type TaskReader interface {            // adapter over tasks.Service, caller = the coordinator's identity
    Get(ctx context.Context, taskID string) (TaskView, error)   // id, boardID, title, description, blockedBy, status, category, branch, deleted/archived
    Children(ctx context.Context, parentID string) ([]string, error)
}
type SessionHost interface {           // adapter over agenthub
    Create(ctx context.Context, spec ChildSpec, opID string) (SessionRef, error)   // idempotent by opID
    Enqueue(ctx context.Context, s SessionRef, prompt string, opID string) (Delivery, error)
    State(ctx context.Context, s SessionRef) (SessionState, error)                 // alive / dead / unknown, pending question, queue paused, closed
    Stop(ctx context.Context, s SessionRef, reason string) error                   // the standard cancel
}
type Codec interface {                 // adapter over the vault
    Seal(keyID string, plaintext []byte) ([]byte, error)
    Open(keyID string, sealed []byte) ([]byte, error)   // ErrLocked while the vault is locked
}
type Clock interface{ Now() time.Time }
type IDs interface{ New() string }     // 24 hex, like tasks
```

`SessionHost` is the only way `agentwork` reaches a vendor. It cannot spawn a process,
choose a model or account, or write outside the folders the host grants; those stay in
Agent Hub's existing guards (design §12).

## 3. Identifiers and revisions

- Every id is an opaque 24-hex string from `IDs`. Agents see ids, never paths.
- `schemaVersion` (integer, starts at 1) is stored on every durable record and every
  package/result/event payload. A reader that meets a higher version than it knows refuses the
  record with a typed error and leaves it untouched.
- `Group.PolicyRevision` increments on any change to `Group.Policy`, `StatusMap` or
  `Limits`. A queued dispatch keeps the revision it was reserved under; a mismatch at
  claim time sends it back to `ready`.
- A work item has `RequirementsDigest` (§5) and a monotonically increasing
  `Generation` per task (§7). A result carries both.

## 4. Records

Plaintext columns are ids, states, timestamps, counters and hashes. Text a person or
agent wrote (briefs, summaries, evidence descriptions, limitations) is sealed with the
`Codec` under the key ids in §11, so the lock stops payload reads (design §11.1).

### 4.1 Group

```go
type Group struct {
    ID, WorkspaceID, ParentTaskID, BoardID string
    Coordinator   SessionRef      // the one current coordinator; changes only by an explicit transfer
    StatusMap     StatusMap       // ready, running, awaitingReview, accepted -> board status ids
    Policy        Policy          // AutoReview, AutoRepair bool; MaxRepairRounds int (default 2); AutoIntegrate is always false in v1
    Limits        Limits          // MaxWorkersPerGroup (3), MaxPerAccount (2), BatchWindow (10s), BatchMax (30s)
    Enabled, Paused bool          // both persisted; Paused survives restart
    PolicyRevision int64
    CreatedAt, UpdatedAt time.Time
    EndedAt       *time.Time      // set when the group is closed; the only state retention may touch
}
```

`StatusMap` stores explicit board status IDs for ready, running, awaiting-review and
accepted. Validate each ID against the selected board; a category alone cannot distinguish
multiple active statuses. A deleted or renamed mapping needs configuration repair and never
silently becomes accepted. `wont_do` is never accepted.

Invariants: one workspace; a task belongs to at most one non-ended group; `Enabled`
false means no dispatch, no wake, reads still work; deleting the parent task ends the
group (§8).

### 4.2 WorkItem

One row per task the group manages, plus linked review and integration items.

```go
type WorkItem struct {
    ID, GroupID, TaskID string
    Kind          ItemKind         // implement | review | integrate | report
    LinkedTo      string           // item id: a review/integrate item points at the item it covers
    State         ItemState        // §6.1
    Wait          WaitReason       // none | blocked | paused | user | limit | vault_locked | stale_requirements
    Digest        string           // RequirementsDigest the item was last reconciled to
    ActiveAttempt string           // empty or the attempt id
    CurrentResult string           // empty or the accepted-for-review result id
    UpdatedAt     time.Time
}
```

### 4.3 Attempt

```go
type Attempt struct {
    ID, ItemID string
    Generation   int64            // fencing token, §7
    Role         Role             // implementer | reviewer | integrator
    Session      SessionRef       // empty until session_created
    Step         DispatchStep     // reserved | session_created | task_assigned | prompt_enqueued | running
    OpID         string           // idempotency key of the current step
    Worktree     string           // worktree id granted to this attempt; empty for read-only roles
    Base         string           // commit the attempt started from
    PackageID    string
    End          AttemptEnd       // none | succeeded | failed | cancelled | interrupted | superseded
    Lease        Lease            // §7
    StartedAt, EndedAt *time.Time
}
```

An attempt that ends does not end its item. A retry is a new attempt with a new
generation; the old one stays as history.

### 4.4 ContextPackage

Immutable once written. Built by `agentwork` from `TaskReader` and the group, never from a
transcript.

```go
type ContextPackage struct {
    ID, ItemID, AttemptID string
    SchemaVersion int
    TaskID        string
    Digest        string           // requirements digest it was built from
    Role          Role
    Objective, Acceptance string   // sealed
    Decisions     []Decision       // kind: requirement | proposal | assumption; sealed text
    InputRefs     []InputRef       // artifact id, task key, document+revision, file hint; ids and hashes only
    Output        OutputContract   // result outcomes allowed, evidence kinds required, who receives it, when to escalate
    Budget        Budget           // soft limits; never truncates instructions, tool pairs or sole evidence
    Profile       string           // model profile name as the person configured it; never chosen here
}
```

Host-supplied, not taken from the package or the agent: workspace, writable folders,
mode, parent relation, grants (design §7.1). The package carries no transcript, no comment
history, no memory bodies; it carries references to them.

### 4.5 Revision, Evidence, Result

```go
type Revision struct {
    RepositoryID string           // workspace folder id
    Base, Head   string           // full commit SHAs; no short names, no branch names
    Tree         string           // tree SHA, so a dirty tree can be refused rather than labelled
    Clean        bool
}

type Evidence struct {
    Kind     EvidenceKind         // agent_claim | host_verify | reviewer_verdict | forge_ci
    Artifact string               // artifact id (§7); empty only for forge_ci
    Revision Revision             // the revision it speaks about
    Source   string               // host command id / reviewer attempt / CI check id; never a free command line
    At       time.Time
}

type Result struct {
    ID, ItemID, AttemptID string
    Generation   int64
    SchemaVersion int
    Digest       string           // requirements digest the worker saw
    Outcome      ResultOutcome    // ready_for_review | blocked | failed. A worker can never write accepted
    Summary      string           // sealed, bounded
    Revision     Revision
    Evidence     []Evidence
    Limitations  []string         // sealed
    DecisionNeeded *Decision
    Stale        StaleReason      // none | generation | digest | head | superseded; set by the host, never by the worker
}
```

A schema-valid result is still an agent claim (design §8). `Evidence.Kind` is assigned by
the host adapter that produced it, so an agent cannot label its own claim `host_verify`.

### 4.6 Event

```go
type Event struct {
    ID, DedupeKey string            // DedupeKey = group + kind + subject + revision
    GroupID, ItemID, AttemptID string
    Kind      EventKind           // §6.3
    Recipient SessionRef          // the coordinator (or reviewer/worker for routed findings)
    Revision  int64               // of the subject, so a late event cannot overwrite a newer one
    PayloadRef string             // artifact or sealed row; the event itself carries no text
    State     EventState          // queued | dispatching | delivered | acked | expired | ambiguous
    CreatedAt time.Time
}
```

Events ride the delivery substrate B5 built (claim, revalidate, `startPeerTurn`); they
do not get a second outbox or drain loop. TEAM1 defines the type; the inbox and wake policy
are OPT-04271c and OPT-e4ff6c.

## 5. Requirements digest

The canonical requirements digest is v2:

`sha256(canonicalJSON{workspaceID, boardID, taskID, description, sorted(blockedBy), folderScope, branch})`

Description carries today's acceptance criteria. The host resolves workspace, board, task,
folder scope and branch; caller-provided values are not authority. Blocked task IDs and folder
scope are canonical sorted sets. The hash prefix is `devkit-managed-requirements-v2\0` and
the returned identifier is `sha256:v2:<hex>`.

- Not in the digest: title, status, priority, assignee, labels, comments, history,
  `sessionId`, `updatedAt`, evidence, progress, role contract, separately represented
  acceptance fields, and dependency requirements/result revision bindings. Editing these
  does not make a result stale. Dependency-input bindings belong to later immutable context
  and result records.
- The host recomputes from its current snapshot at use. Existing v1 snapshots must reconcile
  to v2; old v1 values are not silently accepted as current.
- A changed digest stales the package and any result built on it, keeps both as history and
  returns the item to `ready` after the coordinator reconciles scope. Dispatch of the old
  package never happens.

## 6. State machines

### 6.1 Item state

| From | Event | Guard | To | Side effect |
| --- | --- | --- | --- | --- |
| planned | all `blockedBy` accepted or terminal, group enabled and not paused | digest computable | ready | – |
| ready | claim by scheduler | capacity (§4.1 Limits), profile/mode check by host, writer slot free (§7) | dispatching | attempt created at `reserved` |
| dispatching | saga reaches `running` (§8) | – | running | board status = `running` |
| dispatching | saga fails before `prompt_enqueued` | operation compensated | ready | attempt `failed` or `cancelled` |
| running | result `ready_for_review` accepted by host | generation and digest current | awaiting_review | board status = `awaitingReview`; review item proposed or queued per `Policy` |
| running | result `blocked` | – | waiting (`Wait=user` or `blocked`) | event to coordinator once |
| running | result `failed` or process dead without result | bounded retry left | ready, else waiting | attempt `failed`/`interrupted`; event only when retries are spent |
| awaiting_review | reviewer verdict `changes_requested` | rounds < `MaxRepairRounds` and `AutoRepair` | running | findings routed to the worker; new attempt, new generation |
| awaiting_review | reviewer verdict `accept` | verdict revision = current head and digest | awaiting_acceptance | – |
| awaiting_acceptance | acceptance by coordinator or person | `TerminalPolicy` allows (§9) and the board gate passes | accepted | `accepted` recorded **after** the terminal transition succeeds |
| any non-terminal | head moves, digest changes, task deleted, group ended | – | stale / cancelled | old results kept as history |
| any | vault locked, group paused, user queue present | – | `Wait` set, state unchanged | no dispatch, no wake |

`wont_do` (or any status mapped to no `accepted` column) ends the item as `cancelled`, never
`accepted`. Review passing does not close the task.

### 6.2 Attempt end states

`succeeded` (a current result was recorded), `failed`, `cancelled` (stop by the person or by
group end), `interrupted` (process gone with no result, or an ambiguous dispatch), `superseded`
(a newer generation exists). An attempt's terminal label does not prove its process stopped.
Ownership releases only after the host confirms process/turn quiescence or isolates the old
worktree; until then the old attempt keeps ownership regardless of its recorded end state.

### 6.3 Event kinds

`item_ready_for_review`, `item_blocked`, `item_failed`, `decision_needed`, `review_verdict`,
`group_digest`, `limit_parked`, `escalation`. Progress and tool activity are **not** events: they
update the ledger and the UI only and never wake a model (design §10).

## 7. Ownership, leases and artifacts

- Exactly one active implementation attempt per task and one writer per worktree. Reviews and
  integration are separate items with their own attempt, owner, branch and worktree; worker push
  rights never transfer to an integrator.
- `Lease{Holder SessionRef, Generation int64, ExpiresAt time.Time, HeartbeatAt time.Time}`.
  The heartbeat is the host's liveness check of the process, never a model call.
- Claim is an atomic compare-and-set on `(item, ActiveAttempt == "")` that increments
  `Generation`. Two schedulers cannot both win.
- An expired lease does not authorise a second writer. The old process must be confirmed
  stopped or its worktree isolated; otherwise the item waits `blocked` for the person.
- A result or event carrying an older generation is stored as history and changes no state.
- A change of session or account never releases ownership by itself.
- **Artifacts**: opaque id, scope (group/item), SHA-256 of the sealed content, size, producer
  attempt, revision, kind, retention class. Read by id through the service with workspace and
  task scope checked; ranges and pages only; never a path. Bytes live sealed in the database,
  not in a plaintext staging directory.
- **Retention**: manual cleanup only, limited to ended groups, configured by the person.
  Starting values are 30 days and 200 MB per group. Active or referenced records are not
  automatically deleted. Deleting a task or group leaves a tombstone while references remain.

## 8. Dispatch saga and recovery

The saga extends the B7 handoff and B5 delivery code; it is not a second one. Today
`agentwork.Handoff` has no operation ids. The additive change is an optional interface
`agentwork.Idempotent` that an `Operations` value may also implement, giving `Handoff` an
operation id per step; existing callers keep working.

| Step recorded | Operation | Recovery on restart or retry |
| --- | --- | --- |
| reserved | attempt row written with `OpID`, capacity and writer slot taken | not started: release; started: continue |
| session_created | `SessionHost.Create(opID)` | same `opID` returns the same session; never create a second child because an ack was lost |
| task_assigned | assign the Task to the session (existing claim, ownership check) | lost the race: keep the winner's assignment, close the unused session, attempt `cancelled` |
| prompt_enqueued | `SessionHost.Enqueue(opID)` through the shared outbox | retry with the same `opID`; the outbox row is the idempotency record |
| running | session state confirmed | – |

If a crash may have happened after the vendor took the prompt but before the receipt, the
attempt becomes `interrupted` and reconciles against the session's state before anything is
resent. Blind replay of a task with side effects is forbidden. Pause, vault lock, task deletion
and a stale digest each stop an **unsent** dispatch; a sent one is stopped through the standard
cancel only on the person's request or at group end.

## 9. Acceptance and the terminal-transition policy

Declared in `tasks` and implemented by the composition root, so `tasks` imports neither Agent
Hub nor `agentwork`:

```go
// in package tasks
type TerminalRequest struct {
    TaskID, BoardID, To string
    Caller              Caller      // agent session id, or the person
    Override            string      // reason text from the UI, empty otherwise
}
type TerminalDecision struct {
    Allow  bool
    Reason string                   // shown to the caller on refusal
    Managed bool                    // the task belongs to an enabled group
}
type TerminalPolicy interface {
    Check(ctx context.Context, req TerminalRequest) (TerminalDecision, error)
}
func (s *Service) SetTerminalPolicy(p TerminalPolicy)
```

- `Transition` asks the policy first for **every** caller, before the existing gate. Today
  only agent callers go through the gate (`category == terminal && caller.IsAgent()`); the person
  moves a task directly. That stays: for the person, `Check` always allows, and for a managed
  task the move is recorded as accepted-by-person, with `Override` captured when the
  evidence is not current. It is audited and never rendered as a review pass.
- For an agent caller on a managed task, `Allow` is true only when the caller is the group's
  current coordinator and the result, review and requirements revisions are all current.
- A policy error, or a managed task whose policy cannot be read, **refuses**; it never falls
  back to the unmanaged path.
- The board gate (verify command or confirmation) still decides after a policy `Allow`. The
  ledger writes `accepted` only after the terminal transition returned success.
- Contract test (OPT-f3831a): enumerate every `*tasks.Service` method that can move a task into a
  terminal-category status and fail if one bypasses `Check`. Today that is `Transition` alone:
  `Create` and the claim write only the first open and the first active status, and `SetFields`,
  `Update` and `Archive` do not write status.

## 10. Capability exposure

Declared in `internal/ops` like every operation (`Name`, `Subject`, `Capability`, `Scope`,
`Grant`, `Risk`, `Confirm`, `Exposure`), with only what the product needs. Initial list:

| Operation | Exposure | Risk | Notes |
| --- | --- | --- | --- |
| `team.group.enable` / `disable` / `pause` / `resume` | UI | Write | Future opt-in action from the parent task UI; policy, limits and status mapping are UI-only too |
| `team.group.get`, `team.group.list` | Both | Read | Summary counts, pending decision refs, paged history; scoped by workspace and caller |
| `team.item.get` | Both | Read | One item with its attempts and current result; no transcript or artifact bodies |
| `team.artifact.read` | Agent (scoped) / UI | Read | By id, range or page; checks scope; refuses while locked |
| `team.result.submit` | Agent | Write | Worker only; the host validates ids, size, scope, generation, digest and revision (§4.5) |
| `team.decision.resolve` | UI | Write | The person answers a pending decision |
| `team.accept` | UI | Destructive | Goes through `TerminalPolicy` and the board gate |

Every new error code is published in `bridge.keptCodes` or documented in `redactedOnPurpose`.
Tool definitions and results keep the compact budgets; details load on demand. No operation
here grants send, push, merge or a background run.

## 11. Storage, migration and codec ownership

- Migration 49 is already the metadata-only ledger foundation in `internal/storage`; later
  schema work takes its version at implementation time. Table names are constants owned by the
  composition root and passed to the store, as for accounts: `agentwork` does not import
  `storage`; its SQL store takes a `*sql.DB` and table names.
- Tables for TEAM1: `agent_work_groups`, `agent_work_items`, `agent_work_attempts` (migration 49, metadata only);
  packages, results and artifacts are later-phase additions. `work_events` arrives with OPT-04271c. Events reuse B5's claim
  state vocabulary.
- Plaintext: ids, states, enums, digests, generations, timestamps, hashes, sizes. Sealed with
  `Codec`: package text, result summary and limitations, decision text, artifact bytes.
- Key ids are distinct on purpose, as for tasks (`task-body` cannot open `task-comment-body`):
  `team-package`, `team-result`, `team-decision`, `team-artifact`.
- One SQL statement or transaction at a time, never held across a session read or vendor call
  (`MaxOpenConns(1)`). A ledger change and its outbox row share a transaction **within the
  store that owns them**; the saga does not assume Tasks, the session host and the vendor commit
  together.
- This execution ledger is local, not synced in v1. Tasks remain the synced work source; the
  ledger is never copied into a second task board.
- Vault locked: no payload read, no dispatch, `Wait=vault_locked`. A locked read returns the typed
  locked error, never an empty value.

## 12. Failure and recovery

| Situation | Behaviour |
| --- | --- |
| App restarts | Load groups, pause state, leases, outbox; call no model because the app started; reconcile session liveness before taking a lease again |
| Process alive after lease expiry | Ownership kept; second writer refused |
| Process gone, no result | Attempt `interrupted`; bounded retry; no inferred success |
| Dispatch crash between steps | Resume from the recorded step with the same operation id (§8) |
| Ambiguous prompt acceptance | `interrupted`, reconcile, never resend blindly |
| Vault locked | Items wait `vault_locked`; nothing is read or sent |
| Task edited (here or by sync) | Digest recomputed at use; old package/result stale, kept |
| Head of a branch moves | Result, review and evidence for the old head stale |
| Task deleted or synced to terminal elsewhere | Unsent dispatch cancelled; item reconciled, ledger not forced |
| Duplicate or late event | Dedupe key and revision; older revision changes nothing |
| Group paused or ended | No new dispatch or wake; ended groups cancel unsent dispatch |
| Policy unreadable | Terminal transition refused |
| Usage limit | Item parks with `Wait=limit`; slot released, writer ownership kept while a process may write |

## 13. Verification the later tasks must carry

| Task | Must prove |
| --- | --- |
| OPT-12698e persistence | Migration and reopen; v2 digest invalidation on identity, description, blockedBy, folder scope and branch edits, but not title/progress; deletion and terminal-sync reconcile; lock blocks payload reads and dispatch; architecture test for `agentwork` |
| OPT-b7e5f6 leases | Two schedulers cannot claim one item; stale token; process alive after expiry; cancellation and restart |
| OPT-2e96ed packages | No transcript, comments or memory bodies by default; traversal and scope leaks rejected; locked reads refused; soft budgets keep instructions and sole evidence |
| OPT-1518fd dispatch | Fault injection between every step: no duplicate child, no stolen assignment; same `opID` on retry; pause, lock, deletion and stale digest block unsent work |
| OPT-9324dc results | Stale generation, foreign artifact, malformed or oversized output, repeated submission, worker cannot write `accepted`, dirty tree not labelled host-verified |
| OPT-59dfcb API | Real container grants; ordered deltas and resync; no bodies or secrets in events or audit; published error codes; compact budgets |
| OPT-f3831a acceptance | §9 contract test; worker/reviewer refusal; unreadable policy fails closed; override audited; custom status names |

## 14. Not decided here

- Numeric defaults (§1) are starting values; OPT-400ff3 sets the final ones.
- Transport details of `team.*` operations beyond §10, the Team/Timeline view model and
  everything under TEAM2 to TEAM4.
- Snapshot-based verified targets, auto-integration, cross-workspace groups and execution-ledger
  sync stay out of v1.
- Whether the Tasks board later gets a structured acceptance field. If it does, it joins the
  digest by version bump (§5).
