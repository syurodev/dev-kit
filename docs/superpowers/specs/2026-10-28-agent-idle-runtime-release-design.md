# Release the process of idle agent sessions

Date: 2026-10-28. Status: approved in conversation and implemented on `feat/agent-idle-release`.

## Problem

Every open agent session keeps a vendor process (a Claude process is 190-365 MB), its node ACP wrapper, `devkit-mcp`,
and everything the agent started inside the jail (Gradle daemons at 1.3 GB each, Serena, language servers). On the
author's machine ten Claude sessions were open, most of them sub-sessions that had finished one task, and the tree under
`dev-kit` held about 11.5 GB (PSS) next to a 0.5 GB application. Nothing released a runtime short of closing the session.

## Decisions (asked one at a time)

1. **Release the process, keep the session.** The row stays `idle`; the next prompt reopens it through `resumeRuntime`.
   Not a close: a close ends the conversation and cannot be reopened.
2. **Every session, a shorter time for sub-sessions:** 10 minutes with a parent, 30 without; both settable, can be off.
3. **Time only.** No memory-pressure signal (it needs `/proc/meminfo` and a variant per OS); add one later if time alone is not enough.

## Design

`Service.ReleaseIdle(ctx, now, policy)` is one sweep; `StartIdleRelease` runs it every minute and reads the policy each
time. `releaseRuntime` is the teardown half of `CloseSession` (close fence, drop the runtime maps, flush the recorder,
`rt.Close`, close the endpoint) without the half that ends the conversation. See the guide
`docs/agent-reference/agent-hub-vendors-sessions.md` for the list of what is never released and why.

Two points that are easy to get wrong:

- `HarnessLifecycle.SessionClosed` deletes the session's override S, so the release must not call it.
- The runtime leaves `s.runtimes` and a `resuming` barrier goes up in the same critical section as the final check. A
  caller that arrives afterwards waits for the old process to exit before it reopens; a caller that arrived first stamped
  `lastUsed` under the same lock and the check refuses. Without the barrier a reopen could overlap the dying process.

The setting (`preferences.AgentIdleRelease`, 2 to 1440 minutes, an invalid stored value reads as the defaults) is the
window's own: `AgentIdleReleaseGet/Set` are `uiOnly` and the scope `agent_idle_release` is granted to `builtin.settings` only.

## Tests

`internal/agenthub/idle_release_test.go` (each guard has a "not released" case, a reopen after release, a `-race` test of a
sweep against `Invoke`), `internal/preferences/idlerelease_test.go`, `internal/bridge/idle_release_test.go`, and the
frontend validator and editor tests.

## Not done

A memory-pressure trigger, a mark in the rail for a released session, and a measurement of the saving on a real workload.
