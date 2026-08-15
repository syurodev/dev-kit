# Keyboard Tab Quick-Switcher Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `⌘⇧E` (macOS) / `Ctrl⇧E` (Windows/Linux) opens a small MRU-ordered tab-switcher popup; holding `E` (OS key-repeat) or pressing it again escalates to a larger popup; releasing either modifier cancels; `Enter` on the highlighted tab switches to it.

**Architecture:** `useWorkbenchTabsStore` gains a `tabMru: string[]` field (index 0 = most recent), updated via a small `pushMru` helper at every point `activeTabId` changes (`openTab`'s three branches, `setActiveTab`), and pruned in `closeTab`. A new `TabSwitcher.tsx` component reuses `components/ui/command.tsx` (the same cmdk-backed primitive `CommandPalette.tsx` already uses) for overlay chrome, arrow-key navigation, and `Enter`-to-select — populated from the store's `openTabs`/`tabMru` instead of module commands. `Workbench.tsx` owns a `window`-level `keydown`/`keyup` listener implementing the escalating-chord detection and renders `<TabSwitcher>` as a new sibling of the existing `<CommandPalette>`.

**Tech Stack deviation from the skill default:** no frontend unit test runner in this repo — verification is `cd frontend && bun run build` (tsc + vite) plus a manual QA pass (final task), and this one specifically needs the real Wails window rather than a browser tab, since global keydown capture is exactly the kind of thing this session already found behaves differently there.

**Builds on:** `docs/superpowers/specs/2026-08-15-tab-quick-switcher-design.md` (the approved design this plan implements). Independent of `docs/superpowers/plans/2026-08-15-titlebar-tabstrip-redesign.md` — this plan's `Workbench.tsx` edits (state declarations near the top, JSX near `<CommandPalette>` at the end) touch regions of the file untouched by that plan's header restructuring, so the two can land in either order.

---

## File Structure

Create:
- `frontend/src/workbench/TabSwitcher.tsx` — the popup itself.

Modify:
- `frontend/src/stores/workbenchTabs.ts` — add `tabMru` tracking.
- `frontend/src/workbench/Workbench.tsx` — shortcut detection + render the new component.

---

### Task 1: MRU tracking in the tabs store

**Files:**
- Modify: `frontend/src/stores/workbenchTabs.ts`

- [x] **Step 1: Add the `pushMru` helper and the `tabMru` field**

Find:

```ts
  const suffix = keys.map((key) => `${key}=${String(params![key])}`).join("&");
  return `${moduleId}:${viewId}:${suffix}`;
}

type WorkbenchTabsState = {
  openTabs: WorkbenchTab[];
  activeTabId: string;
  openTab: (input: OpenTabInput) => string;
  closeTab: (id: string) => void;
  setActiveTab: (id: string) => void;
  setTabStatus: (id: string, status: TabStatus) => void;
  pinTab: (id: string) => void;
  reorderTab: (fromId: string, beforeId: string | null) => void;
};
```

Replace:

```ts
  const suffix = keys.map((key) => `${key}=${String(params![key])}`).join("&");
  return `${moduleId}:${viewId}:${suffix}`;
}

/** Moves `id` to the front of an MRU list, deduping any earlier entry. */
function pushMru(mru: string[], id: string): string[] {
  return [id, ...mru.filter((existing) => existing !== id)];
}

type WorkbenchTabsState = {
  openTabs: WorkbenchTab[];
  activeTabId: string;
  /** Most-recently-active tab ids, index 0 = most recent. Drives the
   * keyboard quick-switcher's ordering — independent of openTabs' own
   * (drag-reorderable, module-clustered) display order. */
  tabMru: string[];
  openTab: (input: OpenTabInput) => string;
  closeTab: (id: string) => void;
  setActiveTab: (id: string) => void;
  setTabStatus: (id: string, status: TabStatus) => void;
  pinTab: (id: string) => void;
  reorderTab: (fromId: string, beforeId: string | null) => void;
};
```

- [x] **Step 2: Initialize `tabMru`**

Find:

```ts
export const useWorkbenchTabsStore = create<WorkbenchTabsState>((set, get) => ({
  openTabs: [],
  activeTabId: "",
```

Replace:

```ts
export const useWorkbenchTabsStore = create<WorkbenchTabsState>((set, get) => ({
  openTabs: [],
  activeTabId: "",
  tabMru: [],
```

- [x] **Step 3: Update `tabMru` in `openTab`'s existing-tab branches**

Find:

```ts
        const openTabs = state.openTabs.slice();
        openTabs[existingIndex] = { ...existing, preview: false };
        set({ openTabs, activeTabId: id });
        return id;
      }
      set({ activeTabId: id });
      return id;
    }
```

Replace:

```ts
        const openTabs = state.openTabs.slice();
        openTabs[existingIndex] = { ...existing, preview: false };
        set({ openTabs, activeTabId: id, tabMru: pushMru(state.tabMru, id) });
        return id;
      }
      set({ activeTabId: id, tabMru: pushMru(state.tabMru, id) });
      return id;
    }
```

- [x] **Step 4: Update `tabMru` in `openTab`'s new-tab branches**

Find:

```ts
    if (input.preview) {
      const previewIndex = state.openTabs.findIndex((t) => t.preview);
      if (previewIndex !== -1) {
        // Replace the current preview tab in place — same array index —
        // so the strip doesn't jump when browsing via single-click.
        const openTabs = state.openTabs.slice();
        openTabs[previewIndex] = tab;
        set({ openTabs, activeTabId: id });
        return id;
      }
    }

    set((s) => ({ openTabs: [...s.openTabs, tab], activeTabId: id }));
    return id;
  },
```

Replace:

```ts
    if (input.preview) {
      const previewIndex = state.openTabs.findIndex((t) => t.preview);
      if (previewIndex !== -1) {
        // Replace the current preview tab in place — same array index —
        // so the strip doesn't jump when browsing via single-click.
        const openTabs = state.openTabs.slice();
        openTabs[previewIndex] = tab;
        set({ openTabs, activeTabId: id, tabMru: pushMru(state.tabMru, id) });
        return id;
      }
    }

    set((s) => ({
      openTabs: [...s.openTabs, tab],
      activeTabId: id,
      tabMru: pushMru(s.tabMru, id),
    }));
    return id;
  },
```

- [x] **Step 5: Prune closed tabs from `tabMru`**

Find:

```ts
  closeTab: (id) => {
    set((state) => {
      const index = state.openTabs.findIndex((tab) => tab.id === id);
      if (index === -1) {
        return state;
      }
      const openTabs = state.openTabs.filter((tab) => tab.id !== id);
      if (state.activeTabId !== id) {
        return { openTabs };
      }
      // Closing the active tab focuses its former left neighbor, matching
      // VS Code — falling back to the new first tab if we closed index 0.
      const fallback = openTabs[index - 1] ?? openTabs[0];
      return { openTabs, activeTabId: fallback?.id ?? "" };
    });
  },
```

Replace:

```ts
  closeTab: (id) => {
    set((state) => {
      const index = state.openTabs.findIndex((tab) => tab.id === id);
      if (index === -1) {
        return state;
      }
      const openTabs = state.openTabs.filter((tab) => tab.id !== id);
      const tabMru = state.tabMru.filter((existing) => existing !== id);
      if (state.activeTabId !== id) {
        return { openTabs, tabMru };
      }
      // Closing the active tab focuses its former left neighbor, matching
      // VS Code — falling back to the new first tab if we closed index 0.
      const fallback = openTabs[index - 1] ?? openTabs[0];
      return { openTabs, tabMru, activeTabId: fallback?.id ?? "" };
    });
  },
```

- [x] **Step 6: Update `tabMru` in `setActiveTab`**

Find:

```ts
  setActiveTab: (id) => set({ activeTabId: id }),
```

Replace:

```ts
  setActiveTab: (id) =>
    set((state) => ({ activeTabId: id, tabMru: pushMru(state.tabMru, id) })),
```

- [x] **Step 7: Typecheck**

Run: `cd frontend && bun run build`
Expected: succeeds.

- [x] **Step 8: Commit**

```bash
git add frontend/src/stores/workbenchTabs.ts
git commit -m "feat(workbench): track tab activation order for the quick-switcher"
```

---

### Task 2: `TabSwitcher.tsx`

**Files:**
- Create: `frontend/src/workbench/TabSwitcher.tsx`

- [x] **Step 1: Write the file**

```tsx
import { Wrench } from "lucide-react";
import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { getModule } from "../modules/registry";
import { useWorkbenchTabsStore } from "../stores/workbenchTabs";

type TabSwitcherProps = {
  open: boolean;
  size: "small" | "large";
  onClose: () => void;
};

/**
 * Keyboard-driven tab switcher (⌘⇧E / Ctrl⇧E — see Workbench.tsx's
 * keydown/keyup handling for the escalating-chord detection). MRU order,
 * not the tab strip's own left-to-right/module-clustered order, since the
 * whole point is jumping back to whatever was just being looked at.
 *
 * No CommandInput (unlike CommandPalette): releasing either modifier key
 * closes this popup (see Workbench.tsx), which means there's never a
 * window where the user could actually type a search term without also
 * closing it — showing a search box that can't practically be used would
 * be misleading. Arrow keys + Enter over CommandList/CommandItem work
 * without an Input present; cmdk focuses the first item automatically.
 */
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
    // the MRU-known ones rather than dropped from the list entirely.
    ...openTabs
      .map((tab) => tab.id)
      .filter((id) => id !== activeTabId && !tabMru.includes(id)),
  ];

  return (
    <CommandDialog
      open={open}
      onOpenChange={(next) => {
        if (!next) {
          onClose();
        }
      }}
      title="Switch tab"
      description="Jump to a recently used tab"
      className={size === "large" ? "sm:max-w-2xl" : "sm:max-w-md"}
    >
      <CommandList>
        <CommandEmpty>No other tabs are open.</CommandEmpty>
        <CommandGroup heading="Recent tabs">
          {orderedIds.map((id) => {
            const tab = byId.get(id);
            if (!tab) {
              return null;
            }
            const ModuleIcon = tab.icon ?? getModule(tab.moduleId)?.icon ?? Wrench;
            const moduleTitle = getModule(tab.moduleId)?.title ?? tab.moduleId;
            return (
              <CommandItem
                key={id}
                value={`${tab.title} ${moduleTitle}`}
                onSelect={() => {
                  setActiveTab(id);
                  onClose();
                }}
              >
                <ModuleIcon className="size-3.5 shrink-0 opacity-70" />
                <span className="truncate">{tab.title}</span>
                {size === "large" ? (
                  <span className="ml-auto shrink-0 text-xs text-muted-foreground">
                    {moduleTitle}
                  </span>
                ) : null}
              </CommandItem>
            );
          })}
        </CommandGroup>
      </CommandList>
    </CommandDialog>
  );
}
```

- [x] **Step 2: Typecheck**

Run: `cd frontend && bun run build`
Expected: succeeds — this component has no consumers yet (Task 3 wires it
in), so this step just proves it's internally well-typed against the
`tabMru` field Task 1 added.

- [x] **Step 3: Commit**

```bash
git add frontend/src/workbench/TabSwitcher.tsx
git commit -m "feat(workbench): add TabSwitcher popup component"
```

---

### Task 3: Wire the shortcut into `Workbench.tsx`

**Files:**
- Modify: `frontend/src/workbench/Workbench.tsx`

- [x] **Step 1: Import `TabSwitcher`**

Find:

```ts
import { CommandPalette } from "./CommandPalette";
import { InteractionHost } from "./InteractionHost";
```

Replace:

```ts
import { CommandPalette } from "./CommandPalette";
import { TabSwitcher } from "./TabSwitcher";
import { InteractionHost } from "./InteractionHost";
```

- [x] **Step 2: Add switcher state and the keydown/keyup effect**

Find:

```ts
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [isFullscreen, setIsFullscreen] = useState(false);
```

Replace:

```ts
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [switcher, setSwitcher] = useState<"closed" | "small" | "large">("closed");
  const [isFullscreen, setIsFullscreen] = useState(false);
```

Find:

```ts
  useEffect(() => {
    let mounted = true;
    listModules()
```

Replace:

```ts
  useEffect(() => {
    // navigator.platform is deprecated but still the simplest reliable
    // Mac/non-Mac check available without pulling in a UA-parsing
    // dependency for one boolean.
    const isMac = navigator.platform.toUpperCase().includes("MAC");

    function isTriggerModifier(event: KeyboardEvent): boolean {
      return (isMac ? event.metaKey : event.ctrlKey) && event.shiftKey;
    }

    function handleKeyDown(event: KeyboardEvent) {
      if (event.key.toLowerCase() !== "e" || !isTriggerModifier(event)) {
        return;
      }
      event.preventDefault();
      // First press (not already open) -> small popup. Any further "e"
      // while already open -> large — whether it's the OS's native
      // key-repeat from holding the key down, or a second discrete press
      // (the fallback for users with OS key-repeat disabled), both arrive
      // here as another keydown and take the same branch on purpose.
      setSwitcher((current) => (current === "closed" ? "small" : "large"));
    }

    function handleKeyUp(event: KeyboardEvent) {
      const isModifierKey =
        event.key === "Meta" || event.key === "Control" || event.key === "Shift";
      // Releasing either required modifier ends the session — cancel, not
      // commit (Enter is the only thing that switches tabs; see
      // TabSwitcher.tsx and the design spec for why this diverges from
      // ⌘Tab's release-to-commit).
      if (isModifierKey && !isTriggerModifier(event)) {
        setSwitcher("closed");
      }
    }

    window.addEventListener("keydown", handleKeyDown);
    window.addEventListener("keyup", handleKeyUp);
    return () => {
      window.removeEventListener("keydown", handleKeyDown);
      window.removeEventListener("keyup", handleKeyUp);
    };
  }, []);

  useEffect(() => {
    let mounted = true;
    listModules()
```

- [x] **Step 3: Render `TabSwitcher`**

Find:

```tsx
        <CommandPalette
          open={paletteOpen}
          commands={commands}
          onClose={() => setPaletteOpen(false)}
        />
        <InteractionHost />
```

Replace:

```tsx
        <CommandPalette
          open={paletteOpen}
          commands={commands}
          onClose={() => setPaletteOpen(false)}
        />
        <TabSwitcher
          open={switcher !== "closed"}
          size={switcher === "large" ? "large" : "small"}
          onClose={() => setSwitcher("closed")}
        />
        <InteractionHost />
```

- [x] **Step 4: Typecheck**

Run: `cd frontend && bun run build`
Expected: succeeds.

- [x] **Step 5: Commit**

```bash
git add frontend/src/workbench/Workbench.tsx
git commit -m "feat(workbench): wire up the ⌘⇧E/Ctrl⇧E tab quick-switcher shortcut"
```

---

### Task 4: Manual QA

No code changes. Start the app and, in the running app (not a browser tab
— global keydown/keyup capture needs verifying against the real Wails
window):

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
   modifiers held), press `E` again → same escalation to the large popup.
   If possible, retest with OS key-repeat disabled in system settings to
   confirm this path works independently of step 4.
6. **MRU ordering.** Switch between tabs A, B, C via clicking (not the
   switcher) in that order, so C is active. Open the switcher — B should
   be first in the list, not A, and not the tab strip's left-to-right
   order.
7. **No collision with existing shortcuts.** `⌘K` still opens Commands,
   `⌘,` still opens Settings.
8. **Closing a tab prunes it.** Open several tabs, close one that isn't
   active, open the switcher — the closed tab doesn't appear.
