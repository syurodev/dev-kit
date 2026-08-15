# Tab strip: per-module collapsible groups

**Date:** 2026-08-15
**Status:** Draft — pending user review
**Builds on:** `docs/superpowers/specs/2026-08-15-workbench-tab-mount-optimization-design.md`
(content-mount bounding, implemented — orthogonal to this spec, see Non-goals)
and `docs/superpowers/specs/2026-08-15-workbench-tab-preview-reorder-design.md`
(preview/pin/drag-reorder, implemented — this spec supersedes only the
drag-reorder mechanics described there, not preview/pin).

## Context

`TabStrip` renders every open tab from every module in one flat row (`Reorder.Group`
over the full `openTabs` array). With several tabs open in one module (e.g. 5
notes) plus tabs in other modules, the strip gets visually noisy — the user
can no longer tell "what do I have open in Database" at a glance once Notes'
tabs dominate the row.

The user asked for tabs of a module that isn't in use to collapse into one
compact element. Two concrete scenarios shaped the final design, both raised
as objections to earlier, simpler versions of this spec:

1. **Peek before switching.** While on Notes, the user wants to open
   Database's collapsed group, see which Database tabs are open, and pick a
   specific one — without that pick being a blind guess ("just open the
   first Database tab").
2. **Future split view.** The user is planning a later feature where two or
   more modules can be simultaneously active (not the existing
   `SplitPanelSlot` metadata panel — a real second content pane). Whatever
   model decides "which module's tabs are expanded" must not hard-code "at
   most one module is ever active," or split view will require rewriting
   this feature.

Both are solved by the same fix: **stop deriving "expanded" from "is this
the active module."** Researched how real products handle exactly this
(tab groups, not per-module tabs, but the same collapse/expand problem) —
see Decisions.

## Goals

1. Tabs group by module in the strip, each group in a stable left-to-right
   position (order of first-open — not disturbed by which module is
   currently active).
2. A group is independently `expanded` or `collapsed`; multiple groups can
   be expanded at once. Nothing in the render logic assumes "only one
   module is ever expanded."
3. A collapsed group renders as one compact chip: module icon + tab-count
   badge (badge omitted when the group has exactly one tab), no text label.
4. Clicking a collapsed chip opens a menu listing that group's open tabs by
   title, plus a "Show all tabs" entry — selecting a tab switches straight
   to it (no intermediate "first tab" guess); "Show all tabs" expands the
   group in place without changing the active tab.
5. Drag-to-reorder keeps working, scoped to tabs within the same expanded
   group (unchanged from the user's own simplification: no cross-group
   dragging).

## Non-goals

- **Split view itself.** Not built here. This spec only avoids modeling
  "expanded" in a way that would need rewriting when it arrives — see
  Architecture's `expandedModuleIds` formula.
- **Cross-module drag-and-drop.** A tab's `moduleId` never changes via drag;
  dragging only reorders a tab among its own group's siblings.
- **Reordering the groups themselves** (e.g. dragging the SSH group to sit
  before the Notes group). Group order is derived (first-open order), not
  user-adjustable in this pass.
- **Bulk-closing a collapsed group's tabs from the chip.** No close button
  on a chip — closing a specific tab still requires it to be visible
  (expand the group, or pick it from the chip's menu, then close normally).
- **Persisting `pinnedModuleIds` (manually-expanded groups) or group order
  across an app restart.** Same "not addressed" status as tab
  list/order/preview persistence generally (existing gap, unrelated to this
  change).
- **Interaction with content-mount bounding
  (`useMountedModuleIds`/`mountedModuleIds` in `Workbench.tsx`).** Left
  exactly as implemented. A group being visually expanded (or pinned) in the
  tab strip does **not** force its content to stay mounted — that stays
  governed independently by the existing grace/cap rule. Confirmed
  explicitly acceptable: the two concerns (strip layout, content mount) are
  orthogonal by design, same as today (a tab can already be visible-but-
  unmounted after the grace window elapses).
- **Pixel-level chip/menu styling.** Follows the existing tab strip's visual
  language (`h-6` tabs, `text-xs`, `rounded-md`, dark theme tokens already
  in use) and the existing `components/ui/dropdown-menu.tsx` defaults —
  exact spacing is an implementation-time call, not spec'd here.

## Decisions validated with user

- **Collapse/expand is per-group state, not derived from the single active
  module.** Directly informed by researching Chrome/Firefox tab groups: in
  both, clicking a group's label toggles that one group's collapsed state
  independently — collapsing one group never forces others open or closed,
  and multiple groups can be expanded simultaneously. Rejected the earlier
  "only the active module's tabs are ever expanded, everything else always
  collapses" model specifically because it hard-codes single-module-active
  and cannot represent split view later without a rewrite.
- **Picking a tab from a collapsed group's menu, not auto-selecting "the
  first tab."** Directly informed by Firefox's tab-group hover preview:
  hovering a collapsed group shows its tab list, and clicking one in that
  list activates it directly — no blind default. Rejected "clicking the
  chip always jumps to that module's first tab," which was the original
  (simpler, but rejected) design.
- **"Show all tabs" as an explicit peek action, separate from picking a
  specific tab.** Lets the user expand a group to look around (e.g. keep
  Database's tabs visible while working in Notes) without that group
  becoming "active" — this is the mechanism that generalizes to split view:
  today it's a manual convenience, later it's also how a second pane's
  module would end up expanded.
- **No cross-group drag, no group reordering.** The user's own
  simplification, offered specifically because letting drag operate across
  groups (or reorder groups themselves) reopens the same
  index-vs-identity fragility problem that motivated moving `reorderTab`
  to an id-based signature in the first place (see Architecture) — scoping
  drag to "within one already-expanded group" sidesteps it entirely: each
  expanded group gets its own independent `Reorder.Group`, so there is
  never a cross-group index to get wrong.
- **Badge omitted at count 1.** The same default already used in the prior
  (superseded) version of this design — kept because most modules
  (Database, SSH, Kafka) normally have exactly one tab, and a permanent "1"
  badge on every collapsed single-tab module would itself be visual noise.
  This is a default, not requested explicitly; flagging it here rather than
  asking again since it was already accepted once.

## Architecture

### `frontend/src/stores/workbenchTabs.ts` — `reorderTab` signature change

```ts
// Before: reorderTab: (fromId: string, toIndex: number) => void;
reorderTab: (fromId: string, beforeId: string | null) => void;
```

Reason for the change: once dragging is scoped to one expanded group's own
`Reorder.Group`, the drag gesture only ever produces a **local** index
within that group's subset of tabs — translating that back into a **global**
splice index into the full `openTabs` array (today's `toIndex` contract) is
exactly the kind of index-space mismatch that's easy to get subtly wrong.
Moving to "move `fromId` to just before `beforeId` (or to the end if
`beforeId` is `null`)" sidesteps the translation entirely: both ids are
globally meaningful regardless of which subset the UI currently has
rendered.

```
reorderTab(fromId, beforeId):
  if fromId === beforeId: no-op
  fromIndex = openTabs.findIndex(id === fromId)
  if fromIndex === -1: no-op
  openTabs' = openTabs.slice()
  [moved] = openTabs'.splice(fromIndex, 1)
  insertAt = beforeId ? openTabs'.findIndex(id === beforeId) : -1
  if insertAt === -1: openTabs'.push(moved)
  else: openTabs'.splice(insertAt, 0, moved)
```

The single existing call site (`Workbench.tsx`'s `<TabStrip onReorder={reorderTab} />`)
needs no change — it's a direct pass-through of the store action reference;
TypeScript enforces both sides agree on the new signature.

### `frontend/src/workbench/tabs/TabStrip.tsx` — grouped rendering

**Derived grouping** (computed each render from the `tabs` prop, no new
prop needed):

```
groups: { moduleId: string; tabs: WorkbenchTab[] }[]
  — walk `tabs` in order; the first time a moduleId is seen, start a new
    group at that position; every subsequent tab with the same moduleId
    appends to its existing group (not a new position). This is what makes
    group left-to-right order "first-open order" and keeps a module's own
    tabs contiguous within its group regardless of how they're
    individually ordered relative to other modules' tabs in the raw array.
```

**New local state:**

```ts
const [pinnedModuleIds, setPinnedModuleIds] = useState<Set<string>>(new Set());
```

**Derived expanded set** (the generalization point for split view):

```ts
const activeModuleId = tabs.find((t) => t.id === activeTabId)?.moduleId;
const expandedModuleIds = new Set(
  activeModuleId ? [...pinnedModuleIds, activeModuleId] : pinnedModuleIds,
);
```

Today `activeModuleId` is a single value. When split view exists, this line
is the *only* place that needs to change — to a union of "active module per
pane" — for every other line in this file (grouping, chip rendering, the
per-group `Reorder.Group`s) to keep working unmodified. Nothing else in this
component may special-case "the" active module; every check must go through
`expandedModuleIds.has(group.moduleId)`.

**Rendering each group:**

- **Expanded** (`expandedModuleIds.has(group.moduleId)`): render exactly
  today's tab markup (title, status dot, preview italics, close button) for
  every tab in `group.tabs`, inside their own `<Reorder.Group values={group.tabs}>`
  scoped to just this group. `onDragEnd` computes the dragged tab's new
  local index within `group.tabs`, reads `group.tabs[newIndex + 1]?.id ?? null`
  as `beforeId`, and calls `onReorder(draggedId, beforeId)`. This is the
  same drag-tracking logic already in `TabStrip` today, just operating on
  a per-group subset instead of the whole array — the pointer-midpoint
  detection, `visualTabs`-mirrors-prop-until-drop pattern, and `layout`
  animation are unchanged in kind, only the array they operate over
  shrinks to one group.
- **Collapsed**: one non-draggable chip button —
  `group.tabs[0].icon ?? getModule(group.moduleId)?.icon ?? Wrench`, plus a
  count badge (`group.tabs.length`) shown only when `group.tabs.length > 1`.
  Wrapped in `DropdownMenu`/`DropdownMenuTrigger` (existing
  `components/ui/dropdown-menu.tsx`, unmodified) — `DropdownMenuContent`
  lists:
  1. A "Show all tabs" `DropdownMenuItem` — `onClick` adds
     `group.moduleId` to `pinnedModuleIds`. Does not call `onSelect`; the
     active tab is untouched.
  2. `DropdownMenuSeparator`.
  3. One `DropdownMenuItem` per tab in `group.tabs`, showing `tab.title` —
     `onClick` calls the existing `onSelect(tab.id)` prop (switches active
     tab; the group ends up expanded automatically through the
     `expandedModuleIds` formula above — no separate "expand" call needed).

**Manually collapsing a pinned (non-active, expanded-by-choice) group:**
each expanded group that is **not** the active module's group gets a small
collapse affordance (e.g. a chevron in the same slot the chip's icon
occupies when collapsed) whose `onClick` removes `group.moduleId` from
`pinnedModuleIds`. The active module's own group has no such control — it
cannot be manually collapsed while active, since that would hide the tab
the user is currently looking at.

## Error handling

No new failure modes. `reorderTab`'s `beforeId` lookup already degrades
safely (falls back to appending at the end) if the id isn't found — covers
the drag-end firing after the target tab was closed mid-drag, same edge
case already called out in the preview/reorder spec's Error handling
section. Grouping and `expandedModuleIds` are synchronous derivations with
no I/O.

## Testing / verification

No frontend unit test runner in this repo (unchanged convention). Verification
is `cd frontend && bun run build` plus a manual QA pass:

1. **Grouping renders correctly.** Open 5 notes (5 Notes tabs), then switch
   to Database. Notes collapses to one chip (icon + "5" badge); Database's
   tab renders normally (no badge, exactly one tab).
2. **Peek without switching.** While on Database, click the Notes chip →
   menu lists all 5 note titles plus "Show all tabs," active tab is still
   Database. Click "Show all tabs" → Notes' 5 tabs expand in place;
   Database is still the active tab and its group is still expanded too
   (two groups expanded simultaneously — this is the split-view-readiness
   check).
3. **Pick a specific tab from the menu.** Collapse Notes again (manual
   collapse chevron), click its chip, pick "Note C" directly from the list
   (not "Show all tabs") → Note C becomes the active tab, Notes' group
   expands with Note C focused — Database's group correctly collapses
   (it's not pinned, and it's no longer active).
4. **Active group can't be manually collapsed.** While Database is active
   and expanded, confirm there's no control that collapses it.
5. **Drag stays within a group.** With Notes expanded (5 tabs), drag Note B
   to sit after Note D — order updates live, matches the existing
   drag-reorder feel from the preview/reorder spec. Confirm no way to drag
   a Notes tab into the Database group's space.
6. **Badge omitted at 1.** Any module with exactly one open tab (Database,
   SSH, Kafka in typical use) shows its chip with no badge when collapsed.
7. **Reorder correctness after the `beforeId` change.** Repeat the original
   preview/reorder spec's drag-reorder QA steps (drag past two neighbors,
   drag the current preview tab) — now scoped to one group — confirm no
   regression from the index → id signature change.

## Out of scope recap

Split view, cross-module drag, group reordering, bulk-close from a chip,
persistence of pin/group state across restart, interaction changes to
content-mount bounding, exact visual styling.
