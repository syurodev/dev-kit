# Workbench shell — Phase 5 (first slice)

**Date:** 2026-08-12
**Status:** Draft — pending user review
**Roadmap:** `base-doc/content/projects/dev-kit/roadmap.mdx` — Phase 5 Workbench UI ("apply `dev-kit-app/DESIGN.md` to whole app; UI must stay flexible enough to add vendors, tabs/sessions, Sync conflict panes, device list without layout rewrites")

## Context

`dev-kit-app` is a Wails desktop app ("DevKit") with 8 built-in modules (Tools,
Notes, Database, SSH, Kafka, AgentHub, Sync, Settings) behind a vault-gated
shell. The UI currently renders as unstyled HTML (`workbench/ActivityBar.tsx`,
plain `app.css`) — functional but not on the `DESIGN.md` design system
(Raycast-like dark canvas: surface ladder, hairline borders, Inter + ss03,
command-palette chrome).

This spec covers **only the app shell** — sidebar navigation and the
container that hosts the active module's view. It does not redesign the
content of any individual module (Database, Notes, SSH, Kafka, Sync,
Settings) — those keep functioning exactly as they do today, just rendered
inside the new shell.

## Goals

1. Apply `DESIGN.md` tokens to the app shell (canvas, surface ladder,
   hairline borders, Inter + ss03, no drop shadows).
2. Sidebar must show **cross-module status** — a module can have background
   activity (e.g. AgentHub sessions running while the user is on Database)
   that's visible without switching modules. This was the core requirement
   driving the design: AgentHub tab status must be visible from the sidebar
   regardless of which module is active.
3. Reuse existing shadcn/ui primitives already vendored in
   `frontend/src/components/ui/` instead of hand-rolled CSS — the app already
   has `sidebar.tsx`, `command.tsx`, `collapsible.tsx`, `dialog.tsx` etc.
   installed and unused by the current shell.

## Non-goals (this pass)

- VaultGate redesign (separate full-screen lock UI, own future spec).
- Per-module content redesign (Database schema browser, AgentHub chat
  transcript styling, Notes editor, etc.) — modules mount unchanged inside
  the new content container.
- New backend/bridge changes — this is frontend-only.
- Setting up a frontend test runner (none exists today — see Verification).

## Decisions validated with user (visual brainstorming)

- **Two-layer shell**: an outer canvas frame contains the sidebar; the
  active module's content renders in a separate rounded "card" inset with a
  gap from the frame — not a single flat plane split by a hairline. This
  matches shadcn's `Sidebar variant="inset"` + `SidebarInset` pattern
  exactly (confirmed by reading `components/ui/sidebar.tsx`): `SidebarInset`
  already applies `m-2 ml-0 rounded-xl` when the sibling `Sidebar` has
  `variant="inset"`.
- **Sidebar is a fixed labeled list** (icon + name per module), not an
  icon-only collapsed rail — confirmed after comparing both.
- **Icon-heavy chrome**: every module row, quick action, and tree/table row
  gets a `lucide-react` icon (already a dependency). Status is communicated
  by **color dots**, not icons — icon-coding status was explicitly rejected
  (color reads faster than icon shape for state).
- **Accordion sub-tree for AgentHub**: the AgentHub sidebar row expands into
  one sub-row per open session, each with a colored status dot (running =
  `accent-blue`, awaiting permission = `accent-yellow`, idle = `stone`,
  error = `accent-red`) and a badge count on the parent row. This maps to
  `SidebarMenuSub`/`SidebarMenuSubItem`/`SidebarMenuBadge` wrapped in a
  `Collapsible`.
- **AgentHub's content view drops its own tab-strip** — the sidebar
  sub-tree already switches between sessions, so a second horizontal
  tab-strip inside the content card would duplicate the same control.
- Active sidebar row: `surface-card` background + a 2px `accent-blue` left
  bar. No purple, no glow/shadow (`DESIGN.md` forbids drop shadows on chrome
  — elevation comes from the surface ladder and the inset gap only).
- **Command Palette gets restyled in this pass** (not deferred) because it's
  `DESIGN.md`'s signature component and is now directly reachable from the
  new header's ⌘K trigger — leaving it unstyled would be the most visually
  jarring inconsistency in the new shell.
- **Cross-module AgentHub status uses Zustand** (installed by user during
  this session, `zustand@^5.0.14`) rather than React Context — user's
  explicit choice.
- **Sidebar stays collapsible** (`collapsible="icon"`, for narrow Wails
  window widths) — but shadcn's stock `SidebarMenuBadge` and `SidebarMenuSub`
  both ship with `group-data-[collapsible=icon]:hidden` baked into their
  default className, which would silently drop AgentHub status the moment
  the sidebar collapses. Fixed by composing from the two public extension
  points `sidebar.tsx` already exposes, instead of forking the file — see
  "Collapsed-mode status" below.

## Architecture

### Data flow — AgentHub session status

Today, `AgentHubView.tsx` owns `tabs: TabState[]` as local `useState` and
subscribes to `onAgentHubSessionUpdate`/`onAgentHubPermission` in a
`useEffect`. Because `Workbench.tsx` only mounts the active module's view
(`{ActiveView ? <ActiveView /> : ...}`), this state — and the subscription
— disappears the moment the user switches away from AgentHub. The sidebar
can't show live status for a module that isn't mounted.

Fix: lift the **session list + status** (not the transcript — that stays
local to `AgentHubView`) into a Zustand store that's created once and lives
for the app's lifetime, independent of which module is mounted.

```ts
// frontend/src/stores/agentSessions.ts
type AgentSessionsState = {
  sessions: AgentSession[]; // id, agentType, status, title, ...
  setSessions: (sessions: AgentSession[]) => void;
  upsertSession: (session: AgentSession) => void;
  removeSession: (id: string) => void;
};
```

- The event subscription (`onAgentHubSessionUpdate`, `onAgentHubPermission`)
  moves to a single place that's always mounted — a small
  `AgentSessionsSubscriber` component (no UI, just wires bridge events into
  the store) rendered once in `Workbench.tsx`, sibling to the sidebar and
  content card, not remounted on module switch.
- `AgentHubView.tsx` reads `sessions` from the store instead of local state
  for the tab list; keeps local state for per-tab `lines`/`prompt` (not
  needed outside the view, no reason to lift).
- `Sidebar` reads the same store to render the AgentHub badge count + status
  sub-tree, with no dependency on whether AgentHub is the active module.

### Component changes

| File | Change |
|---|---|
| `frontend/src/stores/agentSessions.ts` | **New.** Zustand store described above. |
| `frontend/src/workbench/AgentSessionsSubscriber.tsx` | **New.** Wires `onAgentHubSessionUpdate`/`onAgentHubPermission` into the store. Rendered once in `Workbench.tsx`. |
| `frontend/src/modules/agenthub/AgentHubView.tsx` | Read session list from the store instead of local `useState`; drop the now-redundant horizontal tab-strip UI (sidebar owns tab switching). |
| `frontend/src/workbench/ActivityBar.tsx` | **Deleted**, replaced by `AppSidebar.tsx`. |
| `frontend/src/workbench/AppSidebar.tsx` | **New.** Built from shadcn primitives: `SidebarProvider` (wraps the whole shell in `Workbench.tsx`), `Sidebar variant="inset" collapsible="icon"`, `SidebarHeader` (brand), `SidebarGroup`/`SidebarMenu`/`SidebarMenuItem`/`SidebarMenuButton` (one per module, icon + label, `isActive` prop, `tooltip` prop carrying rich status content on the AgentHub row), `SidebarMenuBadge` (count, `className` override so it survives collapse — see "Collapsed-mode status" below), `Collapsible` + `SidebarMenuSub`/`SidebarMenuSubItem`/`SidebarMenuSubButton` (AgentHub sessions only, expanded state), `SidebarFooter` (static "Vault unlocked" badge — see note below). |
| `frontend/src/workbench/Workbench.tsx` | Rewritten around `SidebarProvider` → `AppSidebar` + `SidebarInset`. `SidebarInset` becomes the content card; header row inside it (module icon + title + ⌘K trigger) replaces `workbench-header`. |
| `frontend/src/workbench/CommandPalette.tsx` | Rewritten on top of `CommandDialog`/`CommandInput`/`CommandList`/`CommandGroup`/`CommandItem` from `components/ui/command.tsx` instead of hand-rolled divs. Same `open`/`commands`/`onClose` props — internal only. |
| `frontend/src/index.css` | Remap `:root` CSS variables (`--background`, `--card`, `--popover`, `--border`, `--muted`, `--accent`, `--sidebar*`, `--radius`) to `DESIGN.md` token values. App is dark-only (`DESIGN.md`: "Don't introduce a light mode") — no theme toggle exists in the app today (`next-themes` is an unused transitive import in `sonner.tsx` only), so the dark palette becomes the single `:root`, not a `.dark` class variant. |
| `frontend/src/app.css` | Trimmed to just the remaining unstyled-module leftovers (`.tools-view`, `.notes-*`, etc.) now that shell rules move to the component-level Tailwind classes; no shell-related rules remain here. |

### Collapsed-mode status (sidebar `state="collapsed"`)

Two of `sidebar.tsx`'s primitives hide status by default when the sidebar is
icon-collapsed: `SidebarMenuBadge` (`group-data-[collapsible=icon]:hidden`)
and `SidebarMenuSub` (same). Rather than editing `sidebar.tsx` itself
(fragile — the next `shadcn add sidebar` re-sync would blow the edit away
and there'd be no diff signal), `AppSidebar.tsx` composes two things the
component already exposes as public extension points:

1. **`SidebarMenuButton`'s `tooltip` prop.** The component internally sets
   `hidden={state !== "collapsed"}` on the tooltip content — i.e. it is
   *built* to only appear once collapsed, which is exactly the gap we need
   to fill. `tooltip.children` accepts any `ReactNode`, so the AgentHub row
   passes a small list (icon + name + status dot per session) instead of a
   plain string. Hovering the collapsed Agent Hub icon shows the same
   information the expanded sub-tree shows.
2. **Overriding `SidebarMenuBadge`'s `className`.** Every shadcn primitive
   in this codebase renders `cn(defaultClasses, className)` — `cn` is
   `twMerge(clsx(...))` (`frontend/src/lib/utils.ts`), and `twMerge` resolves
   conflicting utilities *within the same arbitrary variant*, so passing
   `className="group-data-[collapsible=icon]:flex group-data-[collapsible=icon]:size-2 group-data-[collapsible=icon]:p-0 group-data-[collapsible=icon]:right-0.5 group-data-[collapsible=icon]:top-0.5"`
   on the AgentHub row's badge cancels the component's own
   `group-data-[collapsible=icon]:hidden` and repositions the badge into a
   small corner dot on the icon (count text hidden via `text-[0px]` at that
   size — the dot's color alone carries "something needs attention"; hover
   for the tooltip to see what). This is the same public `className` prop
   every other override in this spec already relies on — not a fork.

Known ceiling: this couples to `sidebar.tsx`'s *current* default class
strings for these two components. If a future `shadcn add sidebar` changes
them, the override might stop canceling correctly — low risk here since
nothing in this repo auto-re-syncs vendored shadcn files, but worth a
comment at the override site pointing back to this note.

### Module → icon mapping (`lucide-react`)

| Module | Icon |
|---|---|
| Tools | `Wrench` |
| Notes | `FileText` |
| Database | `Database` |
| SSH | `Terminal` |
| Kafka | `Workflow` |
| Agent Hub | `Bot` |
| Sync | `RefreshCw` |
| Settings | `Settings` |

### Note: vault status in the sidebar footer

`VaultGate.tsx` conditionally renders `children` (i.e. `Workbench`) only
once `status === "unlocked"`; there's no `lockVault` bridge call today, so
the vault cannot become locked again while `Workbench` is mounted. The
footer badge is therefore a **static** "Vault unlocked" indicator, not a
live-tracked status — there is nothing to react to yet. If a re-lock action
is added later, this becomes a real subscription; not built now (YAGNI).

### Status color vocabulary (reused wherever a status dot appears)

| State | Token |
|---|---|
| running | `accent-blue` (`#57c1ff`) |
| awaiting permission | `accent-yellow` (`#ffc533`) |
| idle | `stone` (`#434345`) |
| error | `accent-red` (`#ff6161`) |

## Error / loading / empty states

- Existing metadata-parity warning (`hasMetadataWarning`) and loading state
  in `Workbench.tsx` keep their logic, restyled as a small inline banner in
  the content card header using existing `alert.tsx`.
- Sidebar with zero registered modules: unchanged fallback message, styled
  in the empty content card.
- AgentHub badge/sub-tree simply doesn't render when `sessions` is empty —
  no separate empty state needed since the parent row still functions as a
  plain nav item.

## Verification

No frontend test runner exists in `frontend/package.json` (no vitest/jest
configured). This pass does not introduce one — verification is manual:
run the Wails dev server (`task dev` / `wails3 dev`, whichever `Taskfile.yml`
exposes) and visually confirm:

- Sidebar renders all 8 modules with icons, switches active module correctly.
- Opening 2+ AgentHub sessions and switching to Database still shows their
  status dots/badge in the sidebar, **expanded and collapsed**: collapse the
  sidebar and confirm the AgentHub icon still shows a corner status dot and
  the hover tooltip lists all open sessions with status.
- Command palette (⌘K) opens styled on-brand, filters/selects commands.
- Sidebar footer shows the "Vault unlocked" badge (static — see note above).
