# Session activity: what a session is doing when no turn of DevKit's is running

Roadmap slice U1 (`docs/superpowers/plans/2026-10-29-agent-hub-ui-adaptation-roadmap.md`), option B, chosen by the owner's
delegation on 2026-10-29. Base `f9dc7b51`. Not implemented, not reviewed.

## 1. Problem

Claude keeps working after DevKit's turn has ended: a background shell task runs on, and when it finishes Claude starts a turn
**of its own** (`async_task_spawned` with `taskType: "turn"`, id `autonomous-turn-…`, ended by `async_task_state_update`
`completed`). DevKit stores the session as `idle` throughout (only `Invoke` writes `running`), so the window shows nothing:
no indicator in the sidebar, a stale preview ("Build chạy nền…" while the agent was already saying "Build xanh"), no Stop that
makes sense, and a tool call of the latest turn that cannot be told from one that was cancelled (the reason #398 settles only
calls of earlier turns). Evidence (owner's data, 2026-10-29): one agent-started turn ran 41 s with 11 tool calls under `idle`;
five such turns had ended, all with `completed` (mean 67 s, longest 207 s); 45 background-task spawns, 40 of them shell tasks.

## 2. Design

### 2.1 Backend: `SessionActivity`, derived, never stored

`bgwork.go` (#396) already folds `async_task_*` updates into a per-session set. It becomes the single source of this fact:

```go
// SessionActivity is what a session's live process is doing that no DevKit turn started.
type SessionActivity struct {
	AgentTurn bool `json:"agentTurn,omitempty"` // the vendor runs a turn of its own (taskType "turn")
	Tasks     int  `json:"tasks,omitempty"`     // running background tasks other than that turn (taskType "shell", others)
}
func (a SessionActivity) Zero() bool { return !a.AgentTurn && a.Tasks == 0 }
```

- `backgroundWork` keeps each task's `taskType` beside its spawn time (the 12 h valve stays). `snapshot(sessionID) SessionActivity`
  counts a `turn` task as `AgentTurn` and every other running task in `Tasks`. `apply` and `forget` report whether the snapshot changed.
- `AgentSession` gets `Activity *SessionActivity` (`json:"activity,omitempty"`, nil when zero). It is **not** stored: `Service.ListSessions`
  and `Service.GetStatus` (the window's reads, `service.go`) fill it from `bgwork.snapshot`; the store's `ListSessions` that idle release and
  others use is untouched. The MCP helper `forAgent` clears it, like `Preview` and `AccountID`: the agent-facing surface does not change
  (test: an agent tool result never carries `activity`).
- **Change notification.** `Service.OnActivityChanged func(sessionID string, a SessionActivity)`, wired in `main.go` to
  `emitAppEvent("agenthub:activity", {sessionId, agentTurn, tasks})`. It must never run while `s.mu` is held (`forget` is called under it), and
  per-session order must hold. Implement with a coalescing notifier: `bgwork` marks the session dirty and signals a single goroutine owned by
  the Service (stopped with the service), which on each wake snapshots every dirty session and calls the hook. A burst of updates collapses to the
  last state; the final state is always delivered; no goroutine per update. Tests use a hook that records calls.
- `forget` (process released, closed, dead, rewound, account switched) clears the activity and notifies, because those tasks died with the process.

### 2.2 Backend: the preview of an agent-started turn

The session preview is written only at the end of a DevKit turn (`writePreview` in `Invoke`), so it stays stale after an agent-started turn.
When a session's `AgentTurn` goes from true to false, write the preview from that turn's last agent message: while `AgentTurn` is true and no
`Invoke` claim is held (`s.invoking`), accumulate the top-level (`origin == ""`) `agent_message_chunk` text of the session in a bounded buffer
(`lastAgentMessage` already exists for `Invoke`; reuse its type), reset it at the spawn, and on completion call the same
`PreviewFromMessage` + `UpdateSessionPreview` path (no second preview path), and write it **before** the activity change is notified. The window
refetches the session row when it sees `agentTurn` go false, the way `handleAgentTurnFinished` does for a turn started elsewhere. If an `Invoke`
claim appears meanwhile, the `Invoke` owns the preview and the buffer is dropped.

### 2.3 The window

State: `useAgentSessionsStore` keeps `activity` per session, from the DTO on every read and from `agenthub:activity` events (an event for
an unknown session is ignored until it is loaded). Pure selectors, with tests:

- `isWorking(session)` = `status === "running"` **or** `activity.agentTurn`. Everything that today reads `status === "running"` for "this session is
  active" reads `isWorking`: the sidebar card avatar and strong text (`AgentSessionCard.tsx`), the group and branch running counts
  (`AgentSessionGroup.tsx`, `SessionBranch.tsx`), the tab status, the **Stop** item (`sidebar/actions.tsx`; `Service.Cancel` already works on any live
  runtime and answers nil when nothing runs), and the composer's running state. Audit every `=== "running"` in `frontend/src/modules/agenthub`
  and decide each; list the decisions in the commit message.
- **Background tasks only** (`tasks > 0`, no agent turn, session idle): no avatar spinner and no "running" count. The card's second line
  shows a quiet "1 background task" (plural-aware), and the session summary popover keeps its existing "Background tasks" section (from the
  transcript). Nothing shouts: the person asked for nothing.
- Live label (`turns.ts`): an agent-started turn reads like any turn ("Thinking", the tool's title, "<name> in the background"); no change needed beyond
  the activity flag making the tab live.
- **Tool calls (the #398 follow-up).** A call of the **latest** turn settles to "didn't finish" when the session is not working (`isWorking` false),
  no `turnInFlight`, `status` is not `awaiting_permission`, and the call has no subagent `origin` (a background subagent's calls are settled only
  by the earlier-turn rule: the session has no signal for them). Update `settleToolState` and its table tests; the cancelled-latest-turn case now
  settles.

### 2.4 Edge cases

- A DevKit turn starts while an agent turn is running (a prompt or peer message arrives): both are visible; `isWorking` is true for either; the
  stored status stays `Invoke`'s business. Nothing in this design changes how `Invoke`, peers or idle release use the status.
- Idle release and the grant release already skip a session with `Tasks > 0` or `AgentTurn` (`bgwork.running`); unchanged.
- A task that never gets an end update: the 12 h valve clears it (also from `snapshot`, so the indicator cannot outlive it). A vendor that sends no
  `async_task_*` (Codex, opencode) shows no activity: nothing is invented.
- Window reload or restart: the DTO carries `activity`, so a reload shows the current state; after a DevKit restart the set is empty (the processes
  are gone).
- Server mode: the same DTO and event; nothing here needs a native window.

## 3. Tests (each guard gets a mutation check)

- `bgwork`: snapshot counts (turn vs shell), changed flag, valve, forget; table tests.
- Service: replay of the owner's sequence as a **synthetic** fixture (bg task spawn; its `completed`; `autonomous-turn-…` spawn; some
  agent message chunks and tool calls; `completed`) through `HandleUpdateForTest`: `Activity.AgentTurn` true between spawn and completion,
  `GetStatus`/`ListSessions` carry it, the hook sees on then off exactly once each, the stored status never changes, the preview equals the
  turn's last message after completion, and an `Invoke` claim during the turn keeps the preview with `Invoke`.
- Coalescing notifier: a burst yields the final state; nothing is called with `s.mu` held (a test hook that tries to take the lock must not deadlock);
  stops with the service (no goroutine leak, `-race`).
- `forAgent` clears `activity` (MCP tool test).
- Frontend: store merge (DTO, event, unknown session), `isWorking`, sidebar card states (agent turn = running look; tasks only = quiet line; none), Stop enabled
  during an agent turn, `settleToolState` latest-turn cases (idle and quiet -> unfinished; agent turn -> live; subagent origin -> live; awaiting -> live).

## 4. Bindings and docs

`AgentSession` gains a field, so regenerate with exactly `wails3 generate bindings -clean=true -ts -i` and revert unrelated drift. Update
`agent-hub-vendors-sessions.md` (activity, notifier, preview), `agent-hub-ui.md` or the sidebar doc (`isWorking`), `agent-hub-transcript-composer.md`
(the settle rule), and mark U1 done in the roadmap.

## 5. Not in this slice

A "Stop" that cancels one background task (the transcript card already has `canStop`); activity for Codex or opencode; persisting activity; changing
`Invoke`, peer delivery or the stored status; the vendor's cron or scheduled wakeups (not reported).

## 6. Limits

Designed from one machine's data and the code, nothing run. The preview of an agent-started turn rests on `agent_message_chunk` updates arriving
for the session while no `Invoke` runs (true in the owner's data: messages at 04:22:45 and 04:23:18). Subagents that run in the background have no
activity of their own here.
