- **Sessions message each other without blocking** (`internal/agenthub/peer.go`, MCP `agenthub.sendMessage` /
  `agenthub.listPeers`; spec `docs/superpowers/specs/2026-10-16-agent-hub-peer-messages-design.md`). The answer is at
  once `delivery` = `started` (idle: an ordinary `invoke` turn), `steered` (running: Claude `_session/steering` / Codex
  `turn/steer`, the `TurnSteerer` interface), `queued` (in-memory; drained when a turn ends) or `refused` + `code`.
  Reach = own workspace, sub-sessions, parents and siblings — a child reaches the other children of its parent
  (`PeerSibling`, hop cap `MaxSiblingHops` = 4, general `MaxPeerHops` = 6; the refusal and the envelope tell it to report
  to its parent) but not a child of another parent (`PeerOtherBranch`, `not_found`: the parent passes it on; the DevKit
  prompt's "Report to your parent" says so); another workspace is `not_found` unless the person approves the exact
  text (the tool asks, `CrossWorkspaceApproved` is never a wire field); a closed session is never woken. The row is a
  `user_message_chunk` with `source` (`MessageSource`, stored payload + `_devkit.source`), drawn by `PeerMessage` and
  never as the person's bubble; the model gets an envelope (`peerEnvelope`) saying it is not the person's, framed by
  lines carrying the message id. Loop/storm guards: hop count (**per pair of sessions**, below; the person's prompt
  resets it; cap 6), 5/min per pair, 20/min per target. `peerState.chain` is keyed by the unordered pair `{A,B}` (`peerPair`),
  not by session: it holds the hop of the last message either sent the other, who sent it and when. A message A→B is
  step 1 with no exchange in the last 15 min (`peerChainDecay`); one step deeper than the last when B spoke last (an
  answer, `replyTo` or not); the *same* step when A spoke last (a second progress report is not a conversation, nobody
  answered); `replyTo` naming a message B sent A can only raise it. So what either says to a third session never moves
  the pair — a parent hearing from three children counts each child apart (before: one shared `chain[parent]`, and the
  parent's next message to a child was "step 7") — and never resets it either, so interleaving other messages is no way
  past the cap. The person's prompt to a session (`resetChain`), closing/deleting it (`forget`) and the terminal sending
  to it end every pair containing it. **A message to the sender's own PARENT is never refused by the hop cap** (it is
  still recorded in its own pair, and stays under the 5/min pair and 20/min target limits); the cap and its `loop`
  refusal stay for parent→child, sibling↔sibling (4) and every other pair, so a parent↔child ping-pong stops on the
  parent's side. The test rig's `hold` starts its turn as the terminal, because a window prompt would end the pair. Tests: `peer_hops_test.go`. Not run with real
  models: the tests drive the mock runtime. The window mirrors its own prompt queue to Go
  (`AgentHubSetPromptQueue`) so the person's prompts go first and a pause is honoured. `TestNewContainerGrantsTheMessagingTools`
  checks the real grants. Audit: `OnPeerMessage` (never the text). Not durable across a restart.

- **A parent controls the sessions below it** (`internal/mcp/tools_agenthub.go`; beside `cancelAgent`, `getSessionStatus`,
  `invokeAgent`, `sendMessage`). `agenthub.resumeSession` (`Service.ResumeChild`: reopens a session's process without a prompt,
  never reloads, refuses itself/outside its tree with `ErrNotDescendant` and a closed session with `ErrSessionClosed` — only the
  person reopens one that was closed; `ResumeSession` stays UI-only), `agenthub.closeSession` (`CloseSession`; an agent cannot close
  itself) and `agenthub.readSession` (`ReadSessionMessages`: newest user/agent messages, no tool calls, ≤ 20 per read, 3000 runes
  each, `beforeSeq` pages back, every answer says the text is data). Same `session:<id>` scopes and descendant rule as the rest
  (`TestNewContainerGrantsTheChildControlTools`); only `readSession` is pre-approved. The UI's Resume on the "interrupted" notice
  now dismisses the notice and toasts, because it used to succeed silently (the row's `updated_at` moved, nothing on screen did).
- **A sub-session cannot request more folders** (`workspace.requestWrite` answers `workspace.grantRefused`); its parent
  asks, and children created afterwards inherit. A child already holds a copy of what its parent had when it was created.
- **A parent knows why a child stopped, and is not left to resume it blindly** (`internal/agenthub/stop.go`, `stop_service.go`,
  migration 43 `agent_sessions.stop`). Evidence: a child whose provider returned 503 eleven times read as `status: error`;
  the parent called `agenthub.resumeSession` over and over into the same failure.
  - **`AgentSession.Stop`** (`SessionStop`: `kind`, `message`, `note`, `retryable`, `retryAfter`, `since`, `attempts`) is written
    when a turn ends without completing (`Service.recordStop`, from `invoke`'s error branch) and cleared when the next turn
    starts; a completed turn leaves none. Kinds: `provider_unavailable`, `rate_limited`, `usage_limit`, `auth_required`,
    `process_exited`, `interrupted_by_restart` (`RecoverStaleSessions` writes it), `cancelled`, `timeout`, `failed`.
    `StopFromError` decides: an adapter's `WithStopHint(err, StopHint{Kind, Attempts, RetryAfter, Retryable})` first (errors.Is/As
    see through it; opencode's `turnRefusal` sets one from its last retry notice — HTTP status 429 / 5xx and the attempt it
    announced, 11 in the recorded outage — and ignores `nextRetryAt`, a retry the ended turn will not make), then the sentinels (`ErrAgentUsageLimit`, `ErrAgentAuthRequired`, `ErrProcessFailed`,
    `ErrCancelled`, the turn limit), then the words of the vendor's sentence (`429`/rate limit → `rate_limited`;
    `5xx`/unavailable/overloaded/gateway/upstream → `provider_unavailable`, even under `ErrProcessFailed`, since opencode
    reported a provider failure as a dead process). `retryable` is false for provider, rate-limit, usage-limit and sign-in
    stops; a usage limit's `retryAfter` is read from "try again at 3:10 PM" / "in 2 hours" (next occurrence in the machine's zone;
    a sentence that says neither leaves it empty — never a guess). `message` is the vendor's sentence from a `VendorError`,
    one line, ≤ 300 runes, with `note` saying it is third-party text. Heuristics are on text: a vendor that changes its wording
    falls back to `failed` (retryable), not to a wrong confident kind.
  - **Agents see it**: `forAgent` keeps `Stop` (and still blanks `Preview`) in `agenthub.getSessionStatus` / `resumeSession` /
    `createAgentSession`; `listPeers` carries `stop: {kind, retryable, retryAfter}` only. Shape tests:
    `TestAgentHubStatusCarriesTheStopButNotThePreview`, `TestUpdateSessionStopRoundTripsAndClears`.
  - **The parent is told once** (`notifyParentOfStop`): when a child's turn ends in a stop that is not `cancelled` /
    `interrupted_by_restart`, its parent gets a message with `MessageSource.Kind = "devkit"` (`SessionID` = the child, drawn as
    "DevKit notice about …" by `PeerMessage`). It goes through `deliverPeer` (started / steered / queued) but **not through
    `SendMessage`**, so it is not a hop of any exchange and the hop and rate caps neither count nor refuse it; the envelope says
    DevKit wrote it, not the person and not the child, and asks for no reply. Deduplicated per failure (`peerState.stopNoticed`:
    child → kind + sentence; a completed turn or a different failure resets it; a refused delivery frees it). Not sent when
    the parent itself invoked the child and waits (the error is its call's result), on a cancel, or when the parent is closed.
    Not durable across a restart, like the rest of peer state.
  - **`agenthub.resumeSession` is not a retry loop**: `ResumeChild(ctx, id, force)` returns `*ResumeRefusedError`
    (`ErrResumeRefused`) when the stored stop is non-retryable or its `retryAfter` is in the future; the MCP tool turns it into
    the published code `agenthub.resumeRefused` (in `bridge.keptCodes`) with `Sentence()` — kind, since, the vendor's text
    marked third-party, when to try again, `force=true`. `force: true` overrides; `interrupted_by_restart`, `cancelled`,
    `process_exited`, `timeout` and `failed` resume as before. Sending a message to a stopped child is not guarded (the prompt
    tells the agent to read the status first). The DevKit prompt's "Controlling a child" carries the rule
    (`TestPromptTellsAParentToReadTheStopBeforeResuming`).
  - **Not verified against the real vendors**: the 503 / usage-limit sentences come from stored examples (opencode
    `{type:'provider.internal', status:503, message:'Upstream request failed: Endpoint is unavailable.'}`, Codex's "You've hit
    your usage limit. Try again at 3:10 PM."), driven through the mock runtime (`FAIL_PROVIDER_503`, `FAIL_USAGE_LIMIT`
    markers). A real provider's wording, Claude Code's own error text, and whether a woken parent behaves well on the notice were
    not exercised.
- **A parent briefs a child in a few lines, and delegates code through a task** (`devkit_prompt.go` "Briefing a child",
  "Delegating through the task board", "Report to your parent"; `TestPromptTeachesShortBriefsAndDelegationThroughTheBoard`;
  `agenthub.createAgentSession`'s `taskId`, `internal/mcp/tools_agenthub_task.go`). Evidence: a Codex parent's brief to an opencode
  child opened with the child's parent id, retold what the person had said earlier and which model the child used, and ended with
  "report to your parent by sendMessage" — all of which DevKit already gives the child (the `peerEnvelope` names the sender and
  session, the prompt says to report, the model is a parameter, the mode is inherited). The prompt used to say only "objective,
  scope, constraints, completion criterion", which invites padding and names nothing to leave out; Claude Code's own Agent tool
  carries that guidance, DevKit had none. Now: what a brief holds, what it leaves out, "a few lines, not a page"; a child's report
  is a claim to check.
  - **When the workspace has a board (`tasks.board.list`) and the job changes code or needs acceptance, the brief is a task's
    description**: the parent creates it (`tasks.create`, a subtask of the one it holds, or `tasks.createFromPlan`; order by
    `blockedBy`) and starts the child with **`createAgentSession` + `taskId`**. DevKit then, in this order: checks the task can be
    given away *before* the session exists (`tasks.Service.Assignable`: in the parent's board scope, not finished or archived —
    `ErrNotAssignable` — and not held by another live session — `ErrClaimed`; the parent's own hold may be handed over), creates the
    session titled with the task's title when none was given, **claims the task for it** (`Service.Assign`: the holder is the child,
    the board's first active status, the pick in the history is `agent:<child>` so the task is in the child's tray), and sends the
    child the task as its first message from the parent (`taskBrief`: key, title, id, "already claimed for you", the description up
    to 8000 characters, the rest "is on the task"). The parent writes no message. If the claim is lost in the meantime the new
    session is closed again; if only the message fails the child keeps the task and the answer says `delivery` so the parent can
    send it. The answer carries `task {id,key,title}` and `delivery`, never the description. `Assign` unlike `Claim` never
    reopens a finished task (`Claim` always did). Only an agent caller can pass `taskId` (a terminal is refused).
  - The child records its branch, writes blockers/decisions/result as `tasks.comment.add` and sends one short message as the
    doorbell; the parent reads the task and comments, checks, and moves it to a terminal status — the board's gate runs then. One
    task and branch per code-editing child is also what keeps them from overwriting each other (`git.push` only pushes a branch
    recorded on a task the session holds). A read-only question or a board-less workspace is a short message, no task. A session
    that already exists is told the task's **id** by `sendMessage` and claims it itself: the key (`WEB-abc123`) is a label — the
    first 6 characters of the id with the board prefix — and no tool takes it (the first version of this prompt told the child to
    claim "the key"; `tasks.claim` takes the id).
  - **Only a prompt rule for the rest**: nothing stops a child closing its own task (the gate is the same whoever asks), nothing
    tells the parent when a task's comments change (the message is the doorbell), and a board's "review" status is an `active` one,
    so the rule cannot say "move to review" — it says "leave it for the parent to close". Grants: the new steps ask for exactly what
    `tasks.claim` and `agenthub.sendMessage` ask (`tasks:write` on `task:<id>`, `agenthub:execute` on `session:<id>`), so no grant
    was added. Not run with any real model or against the native app: the tests use fakes for the hub and the board
    (`tools_agenthub_task_test.go`) and the real tasks service (`TestAssign…`, and `Assign`/`Assignable` are in the scope table
    `everyScopedMethod`).

## Child tool groups: inheritance and requests

Spec: `docs/superpowers/specs/2026-10-30-child-tool-group-requests-design.md` (see its "As built"). Code: `internal/agenthub/group_request*.go`
(ledger, state machine, SQL; migration 57 `agent_group_requests`, `agent_session_group_locks`), `internal/harnessconfig/delegate.go`
(`ApplyDelegated`), `internal/app/harness_group_requests.go` (the only place that joins them), `internal/mcp/tools_harness_groups.go`.

- **Creation.** `agenthub.createAgentSession` takes optional `toolGroups`. Omitted: a snapshot of the parent's stored S groups. Given: each must
  be a group the parent serves now (`agenthub.groupNotGrantable` otherwise, nothing created). It adds to the workspace profile and cannot
  narrow below it (S is on-only). Written after creation and before the first message; the answer's `toolGroups` says `granted` or
  `not_granted` and, for a group not yet confirmed live, says so.
- **Requests.** `harness.group.request {groups, route, reason}` (route `parent` returns at once, `user` waits for the person),
  `harness.group.requestStatus`, and for the direct parent `agenthub.listGroupRequests` / `agenthub.resolveGroupRequest {requestId, revision,
  decision}` (approve | deny | forward). States: `pending_parent`, `pending_user`, `approved`, `denied`, `expired` (30 min on the parent
  route, checked lazily), `invalidated` (session closed, parent closed for the parent route, **every open request at start**), `superseded`.
  Every move is a compare-and-swap on the row's `revision`; forwarding changes the same row, so there is one approval. The reason is the
  child's text (400 runes): shown only as untrusted, never in audit or metadata. Peer notices are doorbells only.
- **Authority.** Only the direct parent recorded on the child's row now may resolve, and it may approve only groups it serves (confirmed
  endpoint) that are not locked for that child; anything else is deny or forward. `ApplyDelegated` replaces only the person's question; the
  evaluation, tier, V boundary, coordinator, compare-and-swap, publication and audit are the same as `Apply`, and its patch can only be
  `EnableGroups` inside `Grantable`. Audit events carry `Grantor` and `RequestID`.
- **Locks.** Written only by the person (window): "Decline and block" in the question, or a confirmed Revoke (the revoke and its lock are one
  SQL transaction, `OverrideWrite.Lock`). No agent path adds a locked group (request, parent approval, creation, `harness.profile.propose`)
  and the check is made where S is written: `OverrideWrite.Forbid` is read inside `HarnessStore.Apply`'s transaction, so a block written while
  a question was open, or just before a commit, is not bypassed. Only the window lifts one (session card, `HarnessSetGroupLock`). A plain
  decline closes that request only. A lock does not hide a group the workspace profile serves.
- **One open request per child and group, at most four open (migration 58).** SQL-enforced (`agent_group_request_open_groups`, a quota trigger);
  an identical set returns the existing request, an overlapping different set is `agenthub.groupRequestOverlap`, nothing is merged.
- **Boundary revalidation.** `Delegation.Grantable` is re-read inside the commit (V boundary → child's coordinator → grantor's coordinator, read
  only → SQL): the grantor must still be the child's open direct parent and serve the group through a tool that follows its group
  (`harness.ServedWhateverItsGroup` tools prove nothing about their group). With `taskId`, a child whose groups are not confirmed live is closed
  and the task is not claimed or sent (`agenthub.childGroupsNotReady`).
- **Fenced write.** The grantor (still the open direct parent) and the request (still open at the revision read, in the state read) are checked
  **inside the SQL transaction that writes S** (`harness.WriteFence`, `HarnessStore.Apply`), which also moves the request to `approved`; a lost
  fence writes nothing (`agenthub.groupRequestResolved`). Linearization point: that commit (one SQLite connection). A grant committed before the
  parent closed stays (snapshot, no cascade revoke). Lock order: request mutex → V boundary → child's coordinator → grantor's coordinator (read) → SQL.
  A `taskId` child that must be abandoned is closed on `WithoutCancel` + 5 s; if that fails, `agenthub.childNotClosed` names the still-open child and
  its checkout is kept.
- **Integration gap.** C1 (`CheckActual`) refuses a managed child with an S (`harness_override_present`) while grants write S; managed dispatch
  is not open and the preflight is not bypassed. Decide the rule before managed children get groups.
- **Honesty.** `Live` only from an acknowledged endpoint; a saved-but-unpublished change is a failure response with its outcome (like propose).
  Enabling a group never changes grants, workspace scope, sandbox, model gates or prod/unknown-environment confirmation, and a missing-tool
  error never creates a request.
- **Not verified.** Scripted vendors only; no real Claude/Codex/opencode session, no native window, no `tools/list_changed` measurement.

## Managed writer stop proof and admission barrier

`OPT-b7e5f6` (plan slice B). Release of a managed writer lease asks the host for a fresh quiescence check
(`managedOwnershipHost.CheckManagedWriterQuiescence`), and the only source is the Agent Hub:

- **Evidence is the kernel's reap, not a request.** `sandbox.Proc` closes `Exited()` only when `Wait` returned; `Kill`, a signal, a
  `Close` that returned, a timeout, idle/closed status, a missing map entry and an expired lease are not evidence. `acpRuntime.Close`
  now also reaps the process it killed (and kills one whose serve loop ended on its own), so the exit becomes observable.
- **Containment.** A reaped supervisor proves the *jail* is empty only if the backend says so: bwrap (`--unshare-pid`, Linux) sets
  `ContainsDescendants`; Seatbelt, Landlock and unsandboxed runs do not, so on macOS the proof stays `Isolated=false` and release stays
  unavailable. The bwrap claim is reasoning about PID-namespace semantics plus `TestBwrapReapedSupervisorLeavesNoDescendant`, which
  skips unless run on Linux with bwrap; it has not been run on the development (macOS) host.
- **Every incarnation, this process only.** `Service.WriterStopProof(session)` covers every runtime this process opened for the session
  (create, resume/load, rewind fork), including a failed open (unknown for the epoch, sticky) and a runtime kind with no witness
  (`unsupported_runtime`; Codex has none). It carries the process `Epoch`/`EpochStart`; the host also requires the lease to have been
  created after `EpochStart`, so a restart never turns an earlier process's writer into a released one. A restart leaves the lease
  pinned until a person resolves it. Reasons are bounded codes (`runtime_live`, `exit_unobserved`, `no_descendant_containment`,
  `lease_predates_process`, …) appended to `ErrManagedQuiescenceUnknown`; no pid, path or payload.
- **Release seal.** `ManagedLeaseService.Release` takes the host's optional `ManagedReleaseBarrier` *before* the stop proof and holds it
  until the release is committed or refused (`defer end()`); the app host implements it with `Service.SealWriterAdmission`, which
  refuses every new admission for the session — including one nested inside an admission that registered before the seal — and waits
  for registered ones to leave. A positive proof therefore cannot be overtaken by a start that passed an earlier fence check, and a
  failed release never leaves the session sealed. A hub that cannot seal makes release unavailable (`no_admission_barrier`).
- **Every start path is admitted.** The single place a process is started for an existing session (`resumeRuntime`, reached by Invoke,
  Steer, resume, settings calls and account moves) now registers an admission and rechecks the fence at the start, as do `ApplyRewind`
  (it restores files and forks a runtime) and `ApplyRestore` (it writes files). A new session cannot hold a lease yet and is not
  fenced.
- **Bounded table.** The proof table keeps only each process's exit channel and containment flag (no runtime reference), identifies the
  live runtime by that channel, and drops reaped entries whenever a runtime opens, so closing and reopening a session does not
  accumulate runtimes.
- **Pause vs start.** Invoke, Steer and resume register an admission (`enterWriterAdmission`), recheck the managed-writer fence while
  registered, and Invoke/Steer recheck once more immediately before the prompt leaves. A pause flips its durable state and then calls
  `Service.DrainWriterAdmission(ctx, sessionID)`; anything that checked earlier is either sent or refused when it returns. A prompt
  already handed to the runtime is *sent* and is stopped through the existing cancellation policy; the slot is released just before
  `Prompt`, so that last step counts as sent. No SQLite transaction is held while waiting on a runtime. No public pause API calls
  the drain yet (`ManagedService.ConfigureGroup` has no caller); the dispatcher/pause owner must call flip → drain.
- Other processes that write the same worktree outside the Agent Hub (an unmanaged session, a person) are outside this proof; the
  single-writer resource slot is what excludes other *managed* sessions.

## Managed package references (host-selected, bound, re-checkable)

`OPT-2e96ed` (plan slice D, internal part). A context package's `References` are chosen by the host and bound to something
immutable; no public transport or MCP tool exposes them yet.

- **Selection** (`managedOwnershipHost.selectDependencyReferences`): the task's `blockedBy` tasks that the *same group* actively
  tracks, each as `{task, id, Revision = that task's current requirements digest (v2)}`. A blocker the group does not track is board
  ordering, not an input, and is left out; a tracked dependency whose task vanished fails the build (typed
  `ErrManagedReferenceMissing`) instead of being dropped. Nothing the agent, a caller or coordinator prose names is consulted, and no
  transcript, comment or memory body is read. Artifacts are never auto-selected (they are agent claims until results are reviewed).
- **Resolution** (`PackageHost.ResolvePackageReference`): the host re-derives a reference's canonical form from its own state and
  never trusts the revision it was handed. Task refs must be a blocker of the item and tracked in the same group; managed-artifact refs
  (`Revision v<schema>.g<generation>`, `Digest` = sealed-body hash, required) must belong to the same group and to a work item that is
  a declared dependency. `document` and unknown kinds are `ErrManagedReferenceUnsupported`; wrong workspace/group/non-dependency is
  `…Foreign`; unknown ids `…Missing`; a changed revision/hash `…Stale`. A locked vault is `ErrManagedPayloadLocked`.
- **Build**: references are sorted (kind, id), duplicates rejected, each re-resolved and required to equal what was selected, over the
  existing 100-reference bound (refused, not truncated). A refused build stores nothing and does not consume the attempt.
- **Staleness without mutation**: `ManagedPayloadService.CheckPackageReferences(packageID)` reports each stored reference as
  `current|stale|missing|foreign|unsupported` (ids and states only). A dispatcher uses it to rebuild an *unsent* package; the stored
  package stays immutable history. A host/vault failure is an error, never a state. Task-content staleness (requirements digest) and
  reference staleness are separate checks.
- **Not done**: *decisions.* There is no durable decision record yet (they belong with result ingestion and `team.decision.resolve`,
  `OPT-9324dc`/`OPT-f3831a`), so `PackageSource.Decisions` stays empty; inventing one from comments or transcripts would break the
  isolation rule. Also not done: explicit artifact selection, document references, budget-aware read transport, retention cleanup.
  `ErrManagedNotFound` in storage is now an alias of `agentwork.ErrManagedRecordNotFound` so hosts can classify absence.

## Managed harness binding (C1)

`TEAM C1` (plan `docs/superpowers/plans/2026-10-29-team-c1-harness-snapshot-implementation.md`). Internal only: no
dispatcher, AppService method, MCP tool, `team.*` group or public error code uses it, `main.go` is unchanged, and production
Agent Hub still has no harness attached (`TestProductionDoesNotWireTheHarnessYet`), so every preflight answers
`harness_not_configured` today. The goal is that a managed child's preflight and package are tied to the harness
configuration it was meant to run with and the one it actually runs with, and that a mismatch is reported, never papered over.

- **Sidecar, not a package change.** `agent_work_harness_bindings` (migration 56) is update-blocked and insert-only through the service (no DELETE trigger by design, so a later manual retention cleanup is not blocked) and keyed by
  `(attempt_id, phase)`, phase `intended` (before the child exists, attempt `reserved`) or `actual` (after, `session_created` or
  `task_assigned`, session bound). It references the sealed package by composite FK; `ManagedPackageSchemaVersion` stays 1, the
  package row, lease row and admission barrier are never touched, and an UPDATE aborts (`harness binding is immutable`). Only ids,
  revisions, digests, fingerprints and group names are stored; a test pins the exact column list (no model, effort, account, path,
  command, URL or environment). A second row for the same attempt and phase is `ErrHarnessBindingExists` and never replaces the
  first. Unknown binding or source schema fails closed on write and read. The binding is separate from the model profile and from
  the v2 requirements digest: neither feeds the other, and neither changes the digest algorithm.
- **Pieces.** `internal/agentwork` (`harness_binding.go`, `managed_harness.go`): stdlib-only types, the fixed check order and
  `ManagedHarnessService` (`PreflightIntended`, `BindIntended`, `BindActual`, `VerifyForEnqueue`, `CheckBinding`).
  `internal/storage/managed_harness.go`: `ManagedLedger.InsertHarnessBinding` (one transaction fenced on attempt revision, current
  attempt pointer, phase state, enabled unpaused group, live item, package ownership and, for `actual`, the intended row and the
  attempt's own session id) and `GetHarnessBinding`. `internal/agenthub`: `ServedTools` (the endpoint's last acknowledged surface,
  `live=false` without an enforcing endpoint), `PermissionModeOf`, pure `ModeNotWiderThan`. `internal/app/managed_harness.go`: the
  only importer of both `agentwork` and `harness`, the role-to-groups table (`managedRoleGroups`; one edit point) and the
  `HarnessHost` adapter; `WireManagedOwnership` builds `Container.ManagedHarness` (the store union gained
  `agentwork.ManagedHarnessStore`, which the ledger already satisfies).
- **Intended** resolves W and V through a throwaway session id that no session owns, so S is absent by construction and the
  coordinator's S is never read. **Actual** reuses `ResolveManagedWriter` (identity, workspace, parent equals the coordinator,
  folder scope, worktree, grants), requires the child's mode not wider than its coordinator's now, reads the *applied* reference
  (never Desired) and the served surface from the endpoint. A child with an S of its own (even one committed live) is
  `harness_override_present`; there is no parent-S inheritance, auto-enable, account or model change, or shell fallback.
- **Refusals** are fixed `HarnessRefusal` codes (`harness_not_configured`, `_unavailable`, `_schema_unsupported`,
  `_override_present`, `_required_group_off` + group names, `_config_changed`, `_endpoint_not_live`, `_child_scope`, `_child_mode`,
  `_child_unbound`, `_binding_stale`, `_binding_missing`) checked in a fixed order. `ConfigurationNeeded` is true only for the
  four the person can fix by configuration. The cause of a host failure is never carried. They are not public error codes; the
  later dispatcher must map them (recommended single `team.configurationNeeded`) or list them in `redactedOnPurpose`.
- **Staleness without rebuild.** `CheckBinding` reports `current|stale|missing` plus `RebuildRequired` and a code without
  mutating anything. A rebuild is a new attempt, package and binding; nothing here supersedes or overwrites, because the release
  path waits for the writer stop proof. A refused preflight writes nothing; persisting the reason is slice E's operation checkpoint.
- **Session binding is not here.** `BeginAttempt` is the only writer of `agent_work_attempts.session_id`, and the child's id is
  minted inside `CreateSession`, so `BindActual` requires the session to be bound already (`harness_child_unbound` otherwise).
  Binding it, task assignment, enqueue, recovery and the dispatcher are slice E. `VerifyForEnqueue` is read only and is meant to
  run at the side-effect boundary together with the dispatcher's own admission barrier.
- **Limits.** Production vendor facts are `FactsComplete=false`, so a binding never claims vendor-verified executables (compared
  for equality, recorded as is). Tests use the mock runtime and fakes for the ledger store; not run against real vendors or a
  production graph.
- **Check order is the service's.** The adapter only hands the service the facts: once a saved W, V or S has drifted from what the
  child was opened with (reload or new session required) it reports what the saved configuration resolves to now, so an
  override (including an S saved but not yet live), then a group that is off, then a changed configuration are named in the
  fixed order. Endpoint-not-live (paused, pending, closed) comes before them, mode and scope before that.
- **Mode.** "No mode concept" (a live runtime without modes) passes; a mode that cannot be read (store error, or no live runtime and
  nothing recorded) is `harness_child_mode`, never a pass (`Service.ReadPermissionMode`). A closed child of a vendor that records
  no mode therefore reads as `harness_child_mode`, not `harness_endpoint_not_live`. A cancelled context is returned as itself, never
  as a scope or mode refusal.
- **Known limits.** The binding's workspace id is checked against the group/item/attempt rows by their composite keys, but the
  storage layer does not separately tie `harness_workspace_id` (the facts' workspace) to the attempt's workspace; the host's
  intended resolve takes the workspace from the group. A typed-nil store or host handed to `NewManagedHarnessService` is not
  detected (only an untyped nil host reads as not configured); the app wiring never passes one.
