# One header for Agent Hub's right-hand panel

Roadmap slice U8a (`docs/superpowers/plans/2026-10-29-agent-hub-ui-adaptation-roadmap.md`, section U8). Base `34a7e6d4`. The owner delegated the
choices on 2026-10-29 and approved the Stop semantics below by asking to continue. Not implemented, not reviewed.

## 1. Problem

Agent Hub opens two kinds of panel beside the conversation through `useSplitPanelActions().openPanel(content, { width, onClose, key })`
(`modules/agenthub/session/useAgentHubEffects.tsx`): `SubagentPanel` (`subagents.tsx`) and `AgentActivityPanel` (the follow feed). The shell
draws a header for every panel (the module's icon and the text "Agent Hub", the layout button, close; `components/ui/split-panel.tsx` is the store,
`workbench/pane-slots.tsx` and `workbench/slots/slotModel.ts` draw the slot), and the panel draws a second one inside it (`SubagentPanel`: Bot icon,
the subagent's name, its state, a second close). Two rows of chrome and two close buttons; the module label says only which module the person is
already in, while the useful words (which subagent, its state) sit one row lower.

## 2. Design

### 2.1 The shell takes a header description

`openPanel` options gain an optional `header`:

```ts
type PanelHeader = {
  title: string;               // replaces the module label
  titleHint?: string;          // tooltip: the whole title (the row may cut it), then what it adds (a subagent's task)
  icon?: React.ReactNode;      // replaces the module icon; omitted: the module's own
  status?: React.ReactNode;    // drawn right after the title
  actions?: React.ReactNode;   // drawn before the shell's own buttons
};
```

The shell draws, in its single header: icon, title (truncating, `min-w-0 flex-1`), status, actions, then its own layout and close buttons, unchanged. A panel
that passes no `header` is drawn exactly as today, so the other modules (kafka's message panel, codegraph, tasks, notes) are untouched. The close button
keeps calling the panel's `onClose`. The header is part of the opener's state: reopening with the same `key` and a new `header` updates the header and
keeps the content mounted (same component type, no remount); check the store's `open` path does not remount content when only `header` changes.

### 2.2 The two panels

Both stop drawing their own header and pass a `header`:

- `SubagentPanel`: title = the subagent's name (`"Subagent"` when it has none), `titleHint` = its whole name, then its task, icon = Bot, status = `AgentState` for its state,
  actions = **Stop** (2.3) while it runs.
- `AgentActivityPanel` (the follow feed): title = `Following <session title>` (so the pane says what it is), `titleHint` = the whole title and a line on what the pane shows, status = the session's state (`isWorking`-aware, as the sidebar card reads it), actions =
  its existing stop-following control. Same single-header rule.

### 2.3 Stop on a subagent

There is no per-subagent cancel in DevKit and no per-task stop (`canStop` is typed and carried but nothing consumes it): `Service.Cancel` ends the session's whole
runtime and, with it, every background task. So the control says what it does:

- Shown only while the subagent runs (`isSubagentRunning`) and the session is working (`isWorking`).
- Icon-only (`Square`; a pane can be 320 px wide and the title is what the row is for), label "Stop turn" as `aria-label`, and the tooltip: "Stops the whole
  session's turn, including this subagent and background tasks". The question that follows states the whole-turn effect in words.
- A click asks first, with the repo's existing confirm pattern (find the confirm dialog or popover other destructive controls in agenthub use; no new
  pattern), then calls the same function as the sidebar's "Cancel turn" (`cancelTurn(session)` in `session/useAgentSessionActions.ts`; reuse, do not
  duplicate). After it, the panel's state follows the store like everywhere else.
- Not offered for a finished subagent.

### 2.4 Not in this slice

A stop for one background task or one subagent (needs a backend and a vendor capability); redesigning the shell header; other modules' panels adopting
`header`.

## 3. Tests

- Shell: a panel opened with `header` draws one header row with the title, status and actions, the layout and close buttons present, and **no module label**; a
  panel opened without `header` is unchanged (snapshot of the existing structure); updating only `header` with the same `key` keeps the content mounted.
- `SubagentPanel`: no inner header element (`<header>`/its test id absent), a single close button, title and tooltip, state, Stop shown only while running and
  working, Stop opens the confirm first and calls `cancelTurn` once on confirm, not on dismiss.
- `AgentActivityPanel`: same single-header rule; existing tests stay green.
- Mutations for each new guard.

## 4. Docs

`docs/agent-reference/agent-hub-ui.md` or the transcript doc: the panel header contract (who draws what) and the Stop wording. Mark U8a done in the roadmap.

## 5. Limits

Unit and happy-dom tests only; the look of the single header and its truncation at 420 px are not seen in a native window. The Stop control ends the whole turn:
that is stated in its label, not hidden.
