# Agent Hub UI — implementation plan

Design is done (Figma page 17, 26 components). Go core is done (typed
`session/update`, transcript persistence, `AgentHubTranscript`). This plan turns
those into the React surfaces.

## What already exists — do not rebuild

**Go / bridge (landed, tested)**
- `internal/agenthub/update.go` — `ParseSessionUpdate` normalizes a
  `session/update` blob into `SessionUpdate{Kind, Content, ToolCall, Plan,
  Commands, Mode, Usage, MessageID, Raw}`. Never errors; an unmodelled kind
  becomes `UpdateUnknown` with `Raw` intact.
- `internal/agenthub/transcript.go` — `Recorder` persists the transcript,
  coalescing chunks by `(kind, messageId)` and merging `tool_call_update` onto
  the `tool_call` with the same id.
- `AppService.AgentHubTranscript(id)` → `TranscriptEntry[]`, oldest first.
- Event `agenthub:sessionUpdate` now carries **three** fields:
  `{sessionId, update (raw blob), parsed (SessionUpdate)}`.
- Bindings are generated: `frontend/bindings/devkit/internal/agenthub/models.ts`
  has `TranscriptEntry`, `SessionUpdate`, `ToolCall`, `ToolCallContent`,
  `ContentBlock`, `Usage`.

**Frontend**
- `frontend/src/bridge/agenthub.ts` — sessions, invoke, cancel, permissions,
  `onAgentHubSessionUpdate`, `onAgentHubPermission`.
- `frontend/src/components/ui/message.tsx` + `bubble.tsx` +
  `message-scroller.tsx` — the shadcn `Message` anatomy the Figma layers are
  named after (`MessageAvatar`, `MessageContent`, `MessageHeader`, `Bubble` →
  `BubbleContent`, `MessageFooter`).
- `frontend/src/modules/agenthub/AgentHubView.tsx` + `SessionTab.tsx` — today
  `SessionTab` renders only session status. It is the thing being replaced.
- `frontend/src/workbench/AgentCompanion.tsx` + `AgentCompanionSlot.tsx` — the
  one-agent precursor to the rail, with no swarm strip and no parent→child tree.
- The 14 type utilities (`text-body-13`, `text-code-12`, `text-label-11-caps`, …)
  and the Scale tokens in `frontend/src/index.css`.

## Phases

### Phase 1 — data layer (no pixels)
`frontend/src/modules/agenthub/transcript.ts` (+ `.test.ts`).

A reducer that turns `TranscriptEntry[]` (initial load) plus a stream of
`parsed` events into a render model. It must reproduce the Go recorder's
semantics exactly, because a reload has to show what the live view showed:
- chunks coalesce while `kind` **and** `messageId` match;
- `tool_call_update` merges onto the same `toolCallId`, empty field = unchanged;
- unknown kinds are kept, not dropped;
- `usage_update` updates a session-level `usage` (it is not a transcript row).

Deliberately NOT here: any React. This is a pure function plus a small hook
(`useAgentTranscript(sessionId)`) that loads once and subscribes.

Checkpoint: tests green, no component touched.

### Phase 2 — transcript leaves
One component per Figma component, in
`frontend/src/modules/agenthub/transcript/`:
`AgentMessage`, `AgentCodeBlock`, `AgentThinking`, `AgentToolCall`,
`AgentToolDiff`, `AgentToolTerminal`, `AgentFileChip`, `AgentPlan`,
`AgentInvokeResult`, `AgentRateLimit`, `AgentImage`.

Rules:
- No new dependency. Markdown, syntax highlighting and virtualization are all
  **out of scope for this phase** — render text, code as preformatted mono, and
  a plain list. `Agent / Message Rich` defines the eventual markdown language;
  matching it is a later task.
- Width: Figma has a `Width` (Rail/Wide) variant on `Agent / Message` only
  because Figma cannot override `maxWidth` inside an instance. In React use a
  single component with `max-w-*`. Do not port the variant.
- Code and terminal bodies **never wrap** — `overflow-x-auto`, not
  `whitespace-pre-wrap`. Header paths truncate to one line.
- `Agent / Tool Diff` reuses the app's diff language; check
  `frontend/src/components/ui` for an existing diff row before writing one.

Checkpoint: a storybook-less visual check is fine — mount them in
`AgentHubView` behind a dev flag, screenshot, report.

### Phase 3 — surfaces
`Agent Hub / Content` (module pane), `Agent / Rail` (panel), `Agent / Composer`
(3 states: Idle / Streaming / Attached), `Agent / Menu` (Commands + Context),
`Agent / Jump To Latest`, `Agent / New Session`, `Agent / Session Error`.

- Composer `Streaming` swaps send → stop and calls `agentHubCancel`.
- Composer meta band shows turns + cost from `usage_update`, and the vendor +
  version. Cost does **not** go in the sidebar.
- `Agent / Menu` Commands mode is fed by `available_commands_update`.

Checkpoint: Screen A and Screen B reproduced against the Figma screenshots.

### Phase 4 — shell integration
Permission inline card in the rail, `Agent / Permission Toast` when the rail is
closed or another module is active, `Agent / Dock Badge`, and
`agenthub:sessionUpdate` → `setTabStatus` (Module tabs § Scope calls this a
known follow-up; Tasks will be the second consumer).

## Known unknowns — design for absence, not for the happy path

`ToolCallContent` of type `diff` and `terminal` are **modelled from the ACP
spec, never observed**. The live capture
(`internal/agenthub/testdata/claude_session_capture.jsonl`,
claude-agent-acp 0.74) died on `401 OAuth access token has been revoked` before
any tool ran; the only kinds proven so far are `agent_message_chunk`,
`available_commands_update` and `usage_update`.

Consequence for the UI: a tool call with no content must render as a complete,
non-broken row — not as a loading state waiting for a diff that may never come.
`AgentToolDiff` and `AgentToolTerminal` are built, but nothing may reach them.

## Out of scope for all four phases
- Markdown rendering, syntax highlighting, virtualization.
- ACP `terminal/*` (DevKit does not own the agent's terminal yet).
- Transcript retention/pruning — `agent_transcript` grows unbounded today.
- Streaming caret, turn separators, timestamps, attachment remove button.
