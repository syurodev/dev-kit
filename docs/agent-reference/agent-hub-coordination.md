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
