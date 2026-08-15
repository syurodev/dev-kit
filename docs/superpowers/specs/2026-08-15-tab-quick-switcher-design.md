# Keyboard tab quick-switcher (small → large escalating popup)

**Date:** 2026-08-15
**Status:** Draft — pending user review
**Companion spec:** `docs/superpowers/specs/2026-08-15-titlebar-tabstrip-redesign-design.md`
(the tab strip's own visual redesign). Independent of that spec — this one
doesn't render inside the tab strip at all, it's a modal overlay triggered
by a global shortcut, and doesn't care whether the strip uses fisheye
widths, colors, or anything else.

## Context

Even with the tab-strip redesign (companion spec) making the strip itself
easier to scan, mouse-driven switching is inherently slower than keyboard
switching for anyone who works primarily from the keyboard. The user wants
a dedicated shortcut that opens a switcher popup, modeled loosely on
macOS's own `⌘Tab` app switcher (hold the modifier, tap the trigger key,
release to act) but adapted: a first tap opens a compact popup; holding the
trigger key down (or tapping it again, for users with OS key-repeat
disabled) escalates to a larger, roomier popup with more detail.

## Goals

1. **Shortcut**: `⌘⇧E` on macOS, `Ctrl⇧E` on Windows/Linux. Doesn't collide
   with anything already wired in this app (`⌘K` Commands, `⌘,` Settings).
2. **First `E` keydown** (modifiers held) opens a small popup: a scrollable
   list of open tabs, most-recently-active first, arrow keys move the
   highlighted row, `Enter` switches to the highlighted tab and closes the
   popup, `Escape` closes without switching.
3. **Holding `E` down** (the OS's native key-repeat firing further
   `keydown` events for the same physical press) escalates the already-open
   small popup to a larger variant showing the same list with more room per
   row (module name, more of the title visible, bigger click targets) —
   without losing the current highlight/selection.
4. **A second, discrete `E` keydown** (not a repeat — i.e. the user
   physically released and pressed `E` again while still holding the
   modifiers) also escalates, identically to holding — this is the
   fallback for users who've disabled OS key-repeat (an accessibility
   setting), who would otherwise have no way to reach the large popup.
5. **Releasing either modifier key** (`⌘`/`Ctrl` or `Shift`) closes the
   popup **without** switching tabs — closing is a cancel, not a commit;
   `Enter` is the only action that actually switches tabs (see Decisions
   for why this diverges from `⌘Tab`'s release-to-commit).
6. **Most-recently-active ordering.** The list isn't the tab strip's
   left-to-right visual order — it's MRU order (last-active tab first,
   excluding the currently-active tab itself, which doesn't need to be in
   its own switcher list), so tapping the shortcut and immediately hitting
   `Enter` toggles between the two most recently used tabs — the single
   most common reason to reach for a quick-switcher at all.

## Non-goals

- **Fuzzy search / typing to filter.** Not requested. The underlying
  `components/ui/command.tsx` (cmdk) happens to support typeahead
  filtering for free since it's being reused for its list/keyboard-nav/
  overlay chrome — if it turns out useful in practice that's a bonus, not
  a designed requirement, and nothing here is built assuming typing is the
  primary interaction (arrow keys + Enter are).
- **Release-to-commit (the literal `⌘Tab` behavior).** Explicitly
  diverges — see Decisions.
- **Any change to the tab strip itself, `TabStrip.tsx`, or how tabs render
  there.** This is a fully separate overlay component.
- **Customizing the shortcut.** Hardcoded to `⌘⇧E`/`Ctrl⇧E`; no settings UI.
- **Cross-window or global-OS-level shortcut registration.** Scoped to
  this window's own keydown/keyup handling, same as every other shortcut
  already in this app (`⌘K`, `⌘,`).

## Decisions validated with user

- **Escalate on hold OR discrete second tap, not hold-only.** The user
  asked specifically whether OS key-repeat alone could drive the
  escalation (simpler than a manual double-tap timer) — confirmed yes,
  *and* asked to also support a second discrete press as a fallback, since
  users with key-repeat disabled (a real, not-hypothetical OS accessibility
  setting) would otherwise be permanently stuck on the small popup with no
  way to reach the large one.
- **Reuse `components/ui/command.tsx` (cmdk-backed) for the popup itself,
  not a bespoke list/overlay.** Not explicitly requested, but this
  component already provides everything the popup needs — overlay chrome,
  arrow-key navigation, `Enter`-to-select, `Escape`-to-close — and
  `CommandPalette.tsx` already proves the exact usage pattern (`CommandDialog`
  with a controlled `open`/`onOpenChange`) in this codebase. Building new
  list-navigation/keyboard-handling code instead would duplicate what this
  already does correctly. The small vs. large popup is the *same*
  component with a different `className` (width/row-height), not two
  separate implementations.
- **`Enter` commits, releasing modifiers cancels — diverging from `⌘Tab`'s
  release-to-commit.** `⌘Tab` can get away with release-to-commit because
  there's nothing else to do with it — it's a blind cycle with icons only.
  This popup has a visible, clickable, arrow-navigable list where `Enter`
  is already the documented commit action (goal 2, stated by the user
  outright: "dùng các phím mủi tên và enter để chọn tab"). Making release
  *also* commit would mean the popup can be dismissed via `Escape` (no
  switch) but not via letting go of the keys (switches to whatever's
  highlighted, possibly not what was intended) — two different keys
  producing two different outcomes for "close the popup" is a sharper
  footgun than just always requiring `Enter` to act. Flagging this
  explicitly since it wasn't asked in these exact words — it's the
  logical consequence of goal 2's own wording, not a new idea.
- **MRU order, tracked in the store, not derived from tab-strip position.**
  The tab strip's left-to-right order (companion spec: clustered by
  module) has nothing to do with recency of use, and a quick-switcher's
  entire value proposition is jumping to something you were just looking
  at — matches `⌘Tab`/VS Code `Ctrl+Tab`'s own convention.

## Architecture

### `frontend/src/stores/workbenchTabs.ts` — MRU tracking

New state field and a pure helper, updated at every point `activeTabId`
changes:

```ts
type WorkbenchTabsState = {
  ...
  tabMru: string[]; // most-recently-active tab ids, index 0 = most recent
  ...
};

function pushMru(mru: string[], id: string): string[] {
  return [id, ...mru.filter((existing) => existing !== id)];
}
```

Every existing `set()` call that changes `activeTabId` (there are four:
`openTab`'s promote-in-place branch, its preview-replace branch, its
append branch, and `setActiveTab`) also sets `tabMru: pushMru(state.tabMru, id)`
alongside it. `closeTab` additionally filters the closed id out of
`tabMru` (`tabMru: state.tabMru.filter((existing) => existing !== id)`),
same shape as how it already filters `openTabs`.

### New: `frontend/src/workbench/TabSwitcher.tsx`

Sibling to the existing `CommandPalette.tsx`, following its exact
established shape (a `CommandDialog` wrapping `CommandInput`/`CommandList`/
`CommandGroup`/`CommandItem`), reading tabs directly from the store rather
than via props (this overlay is triggered globally, not scoped to
`Workbench`'s own prop-drilling the way `CommandPalette`'s `commands` list
is):

```tsx
type TabSwitcherProps = {
  open: boolean;
  size: "small" | "large";
  onClose: () => void;
};

export function TabSwitcher({ open, size, onClose }: TabSwitcherProps) {
  const openTabs = useWorkbenchTabsStore((state) => state.openTabs);
  const activeTabId = useWorkbenchTabsStore((state) => state.activeTabId);
  const tabMru = useWorkbenchTabsStore((state) => state.tabMru);
  const setActiveTab = useWorkbenchTabsStore((state) => state.setActiveTab);

  const byId = new Map(openTabs.map((tab) => [tab.id, tab]));
  const orderedIds = [
    ...tabMru.filter((id) => id !== activeTabId && byId.has(id)),
    // Tabs never activated yet (no MRU entry — e.g. opened via preview and
    // never focused) fall back to their openTabs position, appended after
    // the MRU-known ones rather than lost from the list entirely.
    ...openTabs.map((tab) => tab.id).filter((id) => id !== activeTabId && !tabMru.includes(id)),
  ];

  return (
    <CommandDialog
      open={open}
      onOpenChange={(next) => { if (!next) onClose(); }}
      title="Switch tab"
      description="Jump to a recently used tab"
      className={size === "large" ? "sm:max-w-2xl" : "sm:max-w-md"}
    >
      <CommandInput placeholder="Switch tab..." />
      <CommandList>
        <CommandEmpty>No other tabs are open.</CommandEmpty>
        <CommandGroup heading="Recent tabs">
          {orderedIds.map((id) => {
            const tab = byId.get(id);
            if (!tab) return null;
            return (
              <CommandItem
                key={id}
                value={tab.title}
                onSelect={() => { setActiveTab(id); onClose(); }}
              >
                {/* small: icon + title only. large: also the module's
                    title as a trailing hint — exact row layout/spacing is
                    implementation-time, not spec'd pixel-by-pixel here. */}
                {tab.title}
              </CommandItem>
            );
          })}
        </CommandGroup>
      </CommandList>
    </CommandDialog>
  );
}
```

### `frontend/src/workbench/Workbench.tsx` — shortcut detection

New local state: `const [switcher, setSwitcher] = useState<"closed" | "small" | "large">("closed");`

A `useEffect` registering `window`-level `keydown`/`keyup` listeners
(mirrors how fullscreen state is already wired via `Events.On` in this
same component — same "global listener owned by `Workbench`, torn down in
the effect's cleanup" shape):

```
isTriggerModifier(event) = (isMac ? event.metaKey : event.ctrlKey) && event.shiftKey
isMac = navigator.platform-or-userAgentData check, computed once

on keydown:
  if event.key !== "e" (case-insensitive) or !isTriggerModifier(event): return
  event.preventDefault()  // avoid any browser/OS default for the combo
  if switcher === "closed":
    setSwitcher("small")
  else:
    // any further "e" while the popup is already open — whether a held
    // repeat (event.repeat === true) or a discrete second physical press
    // — escalates. No branch needed distinguishing the two: goal 3 and
    // goal 4 resolve to the exact same code path.
    setSwitcher("large")

on keyup:
  if switcher === "closed": return
  if event.key is one of the trigger modifiers ("Meta"/"Control", "Shift")
  and isTriggerModifier is now false:
    setSwitcher("closed")  // cancel, same as TabSwitcher's onClose — no
                            // tab switch, matches Decisions above
```

`<TabSwitcher open={switcher !== "closed"} size={switcher === "large" ?
"large" : "small"} onClose={() => setSwitcher("closed")} />` renders as a
sibling of the existing `<CommandPalette />` near the end of `Workbench`'s
JSX.

## Error handling

No new failure modes beyond one real edge case worth naming: if the user
releases modifiers and re-presses the whole chord fast enough that a
stray `keyup` for `Shift` arrives *after* a new `keydown` for `E` (key
event ordering isn't strictly guaranteed across a fast chord re-press),
the effect could see `switcher` flip to `"closed"` right after being set —
this only produces a slightly-too-eager close, not a stuck/incorrect
state, and self-corrects on the next full re-press. Not worth adding
timing-fudge logic for; call it out if manual QA finds it actually
reproducible in practice (Testing item 6).

## Testing / verification

No frontend unit test runner in this repo (unchanged convention).
Verification is `cd frontend && bun run build` plus a manual QA pass in
the running app (not just a browser tab — global keydown/keyup capture
needs verifying against the real Wails window, same caveat as the rest of
this session's work):

1. **Basic open/select.** With 3+ tabs open, press `⌘⇧E` (or `Ctrl⇧E`) →
   small popup opens showing the other open tabs, most-recent first.
   Arrow down, `Enter` → switches to that tab, popup closes.
2. **Cancel via Escape.** Open the popup, press `Escape` → closes, active
   tab unchanged.
3. **Cancel via release.** Open the popup, release `Shift` (or `⌘`/`Ctrl`)
   without pressing `Enter` → closes, active tab unchanged.
4. **Escalate via hold.** Press and hold `⌘⇧E` — small popup opens, then
   (once OS key-repeat kicks in) upgrades to the large popup without
   losing the current arrow-key highlight.
5. **Escalate via second tap.** Press `⌘⇧E`, release just `E` (keep
   modifiers held), press `E` again → same escalation to the large popup,
   even with OS key-repeat off (test with key-repeat disabled in system
   settings if possible, to actually exercise the fallback path).
6. **MRU ordering.** Switch between tabs A, B, C via clicking (not the
   switcher) in that order, so C is active. Open the switcher — B should
   be first in the list (most recently active before C), not A, and not
   the tab strip's left-to-right order.
7. **No collision with existing shortcuts.** `⌘K` still opens Commands,
   `⌘,` still opens Settings, neither is affected by the new listener.
