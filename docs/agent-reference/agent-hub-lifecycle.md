- **The window learns of a session someone else created** (`Service.OnSessionCreated` → `agenthub:sessionCreated`, frontend
  `adoptCreatedSession` in `AgentSessionsSubscriber`). The session list is read once at start; before this a child started
  by `agenthub.createAgentSession` (or from the terminal) never reached the rail and its tab said "Session not available"
  until the app restarted. The event carries only the id; the window reads the row (`AgentHubGetStatus`) and upserts it.
  Sessions the window creates itself (UI / remote human callers) do not fire it.

- **Codex and opencode sessions are named by DevKit** (`internal/agenthub/session_title.go`). Claude Code names its session after the
  first exchange (`session_info_update`); measured on the person's real database, no Codex or opencode session ever got a title
  from its agent (Codex's own thread title is the first message — DevKit's context block; opencode never sent one), so they read
  "Session 43ceeb81" for ever. After a turn the session's first prompt gives a title (first line that says something, ≤ 60 runes; a
  slash command, path or `@` reference gives none; a peer's message never does) through `AdoptSessionTitle`, so a name an agent or
  the person set is never replaced and Claude is left alone (`vendorNamesSessions`). `BackfillSessionTitles` does the same once at
  startup for older rows from their first user message, keeping the row's time so the rail does not reorder. The window takes the
  title from the row when a turn it did not send ends. A vendor title that arrives after the fallback does not replace it.

- **The rail learns when a turn starts, not only when it ends** (`Service.OnTurnStarted` → `agenthub:turnStarted`, frontend
  `handleAgentTurnStarted`). A turn an agent's `sendMessage` or the terminal started set the row `running` in Go, but the window
  heard nothing until `agenthub:turnFinished`, so a working child showed as idle (no running arc) in the rail. Turns this window
  sent are left to the call that sent them (`turnInFlightBySession`), and a session waiting for permission is not set back to running.

**Not in server mode**: `main.go` registers `agenthub.ServerModeAdapter` stubs there, and
every session fails with `ErrUnavailableInServerMode` — the jail shares the host loopback
and server mode's bridge has no authentication, so a session could approve itself.

```bash
go test ./internal/acp/ ./internal/agenthub/ ./internal/bridge/ ./internal/mcp/ -count=1
# Codex in the real jail, signed out (no quota): DEVKIT_CODEX_COMMAND is the native
# binary, e.g. <data>/runtimes/codex/0.147.0/node_modules/@openai/codex-<platform>/vendor/<triple>/bin/codex
DEVKIT_CODEX_COMMAND=/abs/path/to/codex go test ./internal/agenthub/ -run CodexRealBinary -v
```

