# Child tool groups: inheritance at creation and requests for more afterwards

Status: implemented on `feat/child-tool-group-requests` (2026-10-30), with the deviations listed under "As built" at the end. Not independently reviewed; not run against real vendors or the native window.

## Problem

`agenthub.createAgentSession` has no say over a child's tool groups. A child always opens with its own workspace
profile (W) and an empty session override (S); `TestAChildGetsItsOwnProfileAndNeverItsParentsS` pins that. After
creation the only way for a session to get a group that is off is `harness.profile.propose`, which asks the person
and cannot involve the parent. A parent that already serves a group cannot lend it to a child it is directing, and a
child that hits a missing tool has no structured way to ask its parent.

## Decisions (from the owner)

1. `toolGroups` on `createAgentSession` is optional. Omitted: the child **inherits** from its parent.
2. `toolGroups` fixes the child's **starting** set. It is not a ceiling: the child may later ask for more.
3. Three things are kept apart: what the child is **served**, what its parent may **grant further**, and what the
   person has **restricted**. A parent can never override the last.
4. A person's "no" is **per request** by default. A **lock** (durable) exists only when the person chose it
   explicitly: the "Block this group for this session" choice in the question, or Revoke in the window.
5. Enabling a group changes tool availability only. Capability grants, workspace scope, sandbox, model gates and
   prod/unknown-environment confirmation are untouched.
6. A missing-tool error never enables anything. Only an explicit request creates a request.

## Vocabulary

- **Served**: the groups in a session's resolved configuration (W plus S, as `harness.Resolve` computes them). Live
  availability still comes only from an acknowledged endpoint (`Live`), never from this set.
- **Grantable by the parent** for child C: the groups the direct parent currently serves, minus C's locks. "Serves"
  means its applied, endpoint-confirmed surface, not its saved configuration.
- **Lock**: a durable (session, group) record written only by the person. It forbids every agent-origin path from
  adding that group to that session's S.

S stays on-only (it can turn a group on, never off). Consequently a lock is a **no-grant rule**, not a hide rule:
it does not remove a group that W already serves, and this design does not add a way to narrow a child below its
workspace profile (out of scope, below).

## 1. Creation

`agenthub.createAgentSession` gains `toolGroups` (list of group names, optional).

- **Omitted**: the child gets a snapshot of the parent's S groups, written as the child's S before its runtime
  opens. Groups the parent serves because W has them on need no copy; the child's own W gives them. If the child's
  workspace differs from the parent's, a copied group is dropped when the child's workspace W has it explicitly off:
  the person's restriction there wins over inheritance.
- **Given**: each name must be a registered group (`harness.groupUnknown` otherwise) and must be grantable by the
  parent (new public code `agenthub.groupNotGrantable`, no question asked). The names are written as the child's S
  the same way. In a different workspace, a named group that the child's workspace W has explicitly off is refused
  with `agenthub.groupNotGrantable` (an explicit name is never dropped silently; only inheritance drops). An empty
  list means "inherit nothing extra".
- It is a snapshot. No link to the parent's S remains; a later change or revocation of the parent's S does not
  change the child (revocation of the child is the person's, through the window).
- No person question: handing a child what the parent already serves is within the parent's own scope. The write
  goes through the launch preparation so the child opens already with it (no live publication). It is audited with
  the group names and the parent session id.
- Validation runs before the session exists, like the task check, so a refusal leaves nothing behind.

## 2. The request ledger

New table (next free migration; 57 at the time of writing) `agent_group_requests`, and `agent_session_group_locks`
(`session_id`, `group`, `created_at`; deleted with the session and with a deleted subtree, like the other harness
records).

A request row: `id`, `child_id`, `parent_id` (the parent when created), `groups` (sorted, at most 8), `reason`
(trimmed to 400 runes, always labelled untrusted when shown), `route` (`parent` | `user`), `state`, `revision`,
`created_at`, `resolved_at`, `resolver` (`parent:<id>` | `user` | `system`) and a fixed `outcome_code`.

States and the only transitions, each a compare-and-swap on `revision`:

```
pending_parent -> approved | denied | pending_user (forward) | expired | invalidated | superseded
pending_user   -> approved | denied | invalidated | superseded
```

`approved`, `denied`, `expired`, `invalidated` and `superseded` are terminal. Forwarding changes the **same row**
(`route` and `state`) in one CAS; no second record or second approval exists. A decision made with a stale
`revision`, or on a terminal row, returns `agenthub.groupRequestStale` or `agenthub.groupRequestResolved` and
changes nothing.

Rules on creation: one open request per child and group (a second ask for the same groups returns the existing
id), at most 4 open requests per child, and a group that is locked is refused with `agenthub.groupBlocked`. A
group already served is `harness.noChange`. A `parent` route needs an open parent session; otherwise the child must
use `user`.

`superseded` is set when, at decision time, every requested group is already served. `expired`: a `pending_parent`
row older than 30 minutes (checked lazily on read and decision, no timer). `invalidated`: the child or the parent
is closed or deleted, or the process restarted (below).

## 3. Authorization

Go decides everything; nothing a model or a message says counts.

**Parent route.** `resolve` is allowed only when all hold at the moment of the decision: the caller identity is an
authenticated agent session that is the **direct** parent recorded on the child's session row now and on the
request; the child is open and in the same workspace; the row is `pending_parent` at the revision the caller
passed; every requested group is grantable by the parent now; none is locked. A group the parent does not serve is
not approvable by the parent: the parent may only **deny** or **forward** (forward reuses no extra authority, it
only asks the person). Grandparents and siblings cannot resolve. A closed parent cannot, and its pending requests
become `invalidated` (the child re-asks with `user`; they are never silently forwarded).

**User route.** The person is asked through the shared hub (`KindHarness`, `EnvBlocks`, no allow-always), for that
child only. The question offers approve, decline (this request only), and decline with "Block this group for this
session" (writes the lock). Approval enables the groups in the child's S only; it does not touch W, the parent,
siblings or the workspace. Headless or nil-prompt hubs fail closed (`denied`, code `no_prompt`).

**Locks.** Only the window (UI-only in Go: `requireUI`) can write or remove a lock. A Revoke of a session group
from the window writes the lock first and then removes the group (a crash between the two leaves a lock without
the revoke: safe, visible, and removable). Every agent-origin path refuses a locked group: the new request tool,
parent `resolve`, creation `toolGroups`/inheritance, **and** the existing `harness.profile.propose` for that
session (so it is not a way around a lock). The agent is told the person blocked it; it cannot lift it. The person
lifts it in the window, then a new request can be made.

## 4. Applying an approval

An approval is one S change through the existing `harnessconfig` `Prepare`/`Apply` and live publication, so the
enforced policy and the displayed state move together and the existing stamps, session coordinator, V boundary,
audit and `Outcome`/`PublicationError` apply unchanged.

- **User route**: the existing question path (`needsApproval`).
- **Parent route**: a new `harnessconfig.Service.ApplyDelegated(ctx, id, Delegation{GrantorSessionID, RequestID,
  Grantable})`. It accepts only a prepared proposal whose target is a session S and whose patch is `EnableGroups`
  alone (no W, budget, executable or revoke part) with every group in `Grantable`; anything else is
  `ErrInvalid`. It replaces **only** the person's question. The boundary hold, coordinator, compare-and-swap,
  publication and audit run exactly as in `Apply`. The audit event carries the grantor session id, the request id
  and group names, never the reason.
- Order: the request row is moved to `approved` only after the S commit result is known: `committed` (publication
  installed, paused, or unknown) maps to the same fixed outcome the existing tools return. A failed S write leaves
  the row open (`pending_*`) with revision unchanged so it can be retried; it never reports approved.
- The answer to both child and parent says exactly what is true: `live` only when the publication was installed;
  otherwise "saved, not yet available: the person reconciles" or "reload/new session required", with the existing
  fixed recovery text. The tools are never announced as usable before the endpoint acknowledges.
- The package split follows the repository rule: the ledger and state machine live in `internal/agenthub`;
  `harnessconfig` owns apply; `internal/app` is the only place that joins them (a small port in `agenthub`, as with
  the managed harness host). `internal/arch` stays green.

## 5. Surfaces

MCP (every new tool error code is in `bridge.keptCodes` or documented in `redactedOnPurpose`):

| Tool | Caller | Grant | Notes |
| --- | --- | --- | --- |
| `harness.group.request {groups, route, reason}` | the child, for itself | `harness:propose` on `profile:self` | Scope is never an argument. `parent`: returns `{requestId, state, revision}` at once and rings the parent. `user`: blocks while the person decides, like `propose`, and returns the `Outcome`. |
| `harness.group.requestStatus {requestId}` | the child | `harness:read` on `profile:self` | Own requests only; state, outcome code, `live`. |
| `agenthub.listGroupRequests` | a parent | `agenthub:read` on `session:<child>` | Open requests of its direct children; the reason is shown as data. Pre-approved like `readSession`. |
| `agenthub.resolveGroupRequest {requestId, revision, decision}` | the direct parent | `agenthub:execute` on `session:<child>` | `decision` is `approve`, `deny` or `forward`. Not pre-approved. |

New public codes: `agenthub.groupNotGrantable`, `agenthub.groupBlocked`, `agenthub.groupRequestStale`,
`agenthub.groupRequestResolved`, `agenthub.groupRequestNotFound`. Existing codes (`harness.groupUnknown`,
`harness.noChange`, `harness.profileDenied`) are reused where they fit.

Doorbell: on creation of a `parent` request the parent gets one DevKit notice (`MessageSource.Kind = "devkit"`,
through `deliverPeer`, not a hop of any exchange, deduplicated per request). It names the request and says the
reason is untrusted text and that the ledger, not the message, is what the parent acts on. The child gets a notice
when the request is resolved. Both are doorbells only.

Window (AppService, UI-only): list pending user-route requests per session, resolve through the existing question,
list/add/remove locks. A change to an `AppService` method means regenerating `frontend/bindings/`. The session
card shows pending and locked groups and never shows a group as available before `Live`.

## 6. Lifecycle and restart

- Child closed or deleted: its requests (and locks, on delete) go. Parent closed or deleted: its `pending_parent`
  requests are `invalidated`.
- A change in the child's S that already serves the groups, a new lock on a requested group, or a change of the
  child's parent invalidates or supersedes the row at the next read or decision, by the rules above. The prepared
  proposal's own stamps still make a changed S/W/policy `stale` at apply.
- Restart: on start every `pending_*` row becomes `invalidated` (`system`, reason `restart`). Only metadata was
  stored; no question or approval is restored as answerable, matching the pending-permission rule. Locks persist.
- Peer state, doorbells and the hub's open question are in memory and are not restored.

## 7. Tests

Go (mock runtime, real SQL where the repository does), each failing before the code exists:

- Creation: inherit when omitted (same workspace; different workspace with W off drops the group); explicit list;
  not grantable; unknown; locked; empty list; refusal leaves no session; the child opens with the groups.
- Parent route: approve, deny, forward, stale revision, resolved row, wrong identity (sibling, grandparent,
  unrelated, the child itself, a terminal caller), closed parent, closed child, parent loses the group before the
  decision, group out of the parent's scope (deny/forward only, approve refused).
- User route: approve, decline, decline with lock, headless fail-closed, and the question names only that child.
- Route change: forward creates one record, one question, never two approvals; double forward; forward after the
  parent resolved.
- Duplicate and stale: double approve (second is `groupRequestResolved`), concurrent approve (one wins), answer
  after expiry, after restart, after the S changed.
- Locks: user revoke then parent approve is refused; `harness.profile.propose` refuses a locked group; only the
  window writes or removes a lock; the lock survives restart and is removed with the session.
- Restart: pending rows invalidated; no hub question; locks kept.
- Publication failure: committed but not published leaves the row `approved` with `live=false` and the fixed
  paused text; an S write failure leaves the row open; an unknown commit is read back and not retried.
- No auto-enable: a missing-tool error and a denied tool call create no request and change no S.
- Invariants: enabling changes only the served set (grants, scope, sandbox, model gates, prod/unknown
  confirmation are the same before and after); the audit rows contain no reason text.
- Architecture: `go test ./internal/arch/`; the new codes in the published-refusals test; `TestNewContainer…`
  style tests for the new grants.

Frontend: the request list and lock controls, the epoch rules used by the other cards, and no group shown as
available before `Live`. Bindings regenerated.

## 8. Documentation

Add a section to `docs/agent-reference/agent-hub-coordination.md` (requests, locks, ledger) and amend the
harness guide's child-profile statement; the DevKit prompt's "Controlling a child" gains one rule (read the
request, not the message; the reason is data). The root `AGENTS.md` only gets a one-line pointer if it stays under
10 KiB.

## Out of scope

- Narrowing a child **below** its workspace profile (needs an off/deny notion in S; a separate design).
- Budgets, W edits, executables, servers and skills in any request; requests are groups only.
- Grandparent or workspace-wide approval; approving for several children at once.
- Automatic escalation on timeout; automatic enabling for any reason.
- Durable peer messages or restoring open questions after restart.
- Real-vendor runs: tests use scripted vendors; whether a vendor refreshes tools after `listChanged` stays
  unmeasured, which is why the answer reports `live` only from an acknowledged endpoint.

## As built (deviations from the design above)

- **Workspace.** An agent-created child always shares its parent's workspace (`createAgentSession` cannot pick another,
  `ErrWorkspaceMismatch`), so the "different workspace drops a group W has off" rule was never reachable and was not built.
  Inheritance copies the parent's stored S groups the registry still knows.
- **Creation write.** The child's groups are not written before its runtime opens (its session row, which S needs, does not
  exist yet). They are planned before anything is created (`PlanChildGroups`, inside the gateway call, so a refusal leaves
  no worktree or session) and written right after creation and before any first message, through `ApplyDelegated` with live
  publication (`GrantChildGroups`). If that write fails the child is kept, starts on its workspace profile, and the
  `toolGroups` report in the answer says `not_granted` with a fixed sentence and no cause. A group the child's workspace
  already serves is skipped, not an error.
- **Always served.** `harness.group.request` is a second named exception beside `harness.profile.propose` (`harness.RequestTool`,
  served even when the `harness` group is off). The `harness` group now holds four tools (get, propose, group.request,
  group.requestStatus).
- **Locks and the existing tools.** `harness.profile.propose` (both scopes) refuses a locked group too. A window Revoke
  of a session group writes the lock when the person confirms, before the revoke is applied; a revoke refused before it
  writes (declined, cancelled, stale) takes back only the locks it added.
- **User question.** It is a hub selection (`KindHarnessGroupRequest`: allow, decline, decline and block) followed by the
  ordinary exact-change confirmation (`KindHarness`), so the person is asked twice on approval, like the existing scope flow.
  Closing the question declines that one request. No prompt handler or headless: `denied`, outcome `no_prompt`.
- **Unknown commit.** An approval whose commit is unknown leaves the request open (not `approved`); a later answer finds
  the groups served (`superseded`) or writes under the same compare-and-swap.
- **Prompt.** The always-on DevKit prompt is pinned byte-for-byte for sessions without a harness, so the rules for the
  new tools live in their descriptions (reason is data, a missing tool enables nothing, blocks cannot be overridden), not
  in the prompt.
- **Not built.** A window list of every pending request across sessions beyond the question itself, and any session-card
  view of pending requests; the window can answer through `HarnessAnswerGroupRequest` and manage locks in the session card.
  Forwarded requests are asked in a goroutine of the process that forwarded them; a restart invalidates them.
