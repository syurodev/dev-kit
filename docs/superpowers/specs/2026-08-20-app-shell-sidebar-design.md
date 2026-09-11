# App shell sidebar — `useSidebarContent` primitive + CodeGraph as first consumer

**Date:** 2026-08-20
**Status:** Draft — pending user review
**Supersedes:** nothing shipped. Corrects one stale claim in
`base-doc/content/projects/dev-kit/sidebar.mdx` (§ Resize — see Decisions).

## Context

`frontend/src/workbench/Workbench.tsx` (the real, running app shell) has a
header, a `content-row` (`WorkbenchViews` + `SplitPanelSlot`), and a `Dock`.
There is no left-hand sidebar column at all — confirmed by reading the file,
not assumed. Two modules have already built their own ad hoc version of "a
nav column next to content" independently: `NotesDashboard.tsx` uses the
vendored shadcn `Sidebar` primitive (`components/ui/sidebar.tsx`) entirely
inside its own view; `CodeGraphView.tsx` hand-rolls one with a raw
`<ul className="w-1/3 border-r">`. Neither goes through the shell.

`base-doc/content/projects/dev-kit/sidebar.mdx` already specifies a shell-level
Sidebar primitive in detail, cross-referencing four other pages
(`ui-state.mdx`, `title-bar.mdx`, `vault.mdx`, `shell-primitives.mdx`), all
marked **"Design only — chưa có runtime."** This spec is that runtime, scoped
down in two places (see Decisions) and correcting one factual error the doc
made about existing code.

The real Figma App Shell component (`107:137`, file `3dOzSZBYY7QtzANFTCfEe8`)
confirms the shape: variant axes `Panel=Closed/Open × Sidebar=Closed/Open`.
With `Sidebar=Open`, `content-row` splits into `sidebar-surface` (320px,
fixed) + `content-surface` (fills remainder); with `Sidebar=Closed`,
`content-surface` alone fills the row. The toggle is a titlebar button.

## Goals

1. A shell-level Sidebar primitive — `SidebarProvider` + `SidebarSurface` +
   `useSidebarContent()` — hosted by `Workbench.tsx`, structurally mirroring
   the already-implemented `SplitPanelProvider`/`SplitPanelSlot`/
   `useSplitPanel()` (`components/ui/split-panel.tsx`).
2. A global open/closed toggle, titlebar button, state persisted across app
   restarts through a new generic `ui_state` persistence subsystem (Go table
   + bridge + Zustand `persist` adapter) — the first real consumer of a
   mechanism `ui-state.mdx` designs as reusable for future preferences.
3. Drag-to-resize (240–480px, default 320px), dynamically clamped against
   `SplitPanel`'s own width when both are open, with a hard floor for the
   center content area (480px) — mirroring `SplitPanelSlot`'s real,
   hand-rolled pointer-resize implementation, not a new dependency.
4. `CodeGraphView` becomes the first real consumer: its existing
   Endpoints/Files browser moves from inline JSX into the sidebar via
   `useSidebarContent`, proving the primitive end-to-end.

## Non-goals

- **Migrating `NotesDashboard`'s sidebar.** It already works
  (`collapsible="icon"`, folder list, badge counts, drag-drop) on the
  vendored shadcn `Sidebar` component, entirely inside its own view. Moving
  it to the new shell primitive is real, working-code-touching scope with no
  functional payoff for this spec's goal (proving the shell mechanism) —
  explicit user decision, left for later.
- **The title-bar redesign** (`title-bar.mdx`: 3 fixed zones, no `TabStrip`,
  single active module, sidebar toggle "right after traffic lights"). That
  page itself says **"chưa có runtime — Workbench.tsx hiện tại vẫn chạy theo
  module-tabs.mdx."** It is a separate, larger rewrite that removes the
  currently-working `TabStrip`/⌘⇧E switcher from the header. Explicit user
  decision: build against the **current** title bar, toggle placed
  pragmatically at the end of the existing right-hand icon cluster. When the
  title-bar redesign eventually lands, the toggle moves — that's a change to
  this spec's placement only, not to `useSidebarContent`'s contract.
- **CodeGraph's search box.** The Figma CodeGraph sidebar mock (page
  `14 CodeGraph`, built earlier this session) shows a "Search files or
  endpoints…" input; the current `CodeGraphView.tsx` has no search logic at
  all (`searchCodeGraphSymbol` exists on the bridge, unused). Wiring it is
  visual-parity work beyond "prove the mechanism," not requested — left as a
  follow-up.
- **Migrating `theme` or tab state onto `ui_state`.** `ui-state.mdx` names
  them as plausible *future* consumers of the generic table; this spec only
  builds the table and wires its first real consumer (sidebar open/closed).

## Decisions validated with user

1. **Scope: shell mechanism + wire CodeGraph only**, not also migrating
   Notes. (Three-option `AskUserQuestion`; user picked this over "also
   migrate Notes" and "empty mechanism, no consumer.")
2. **API shape: push-based `useSidebarContent`**, per `sidebar.mdx`, not the
   `ModuleManifest.sidebar` field this spec's first draft proposed. Directly
   reverses that earlier draft — the doc is the more authoritative, already-
   written source of intent, and the user confirmed following it over the
   draft.
3. **Toggle placement: current title bar**, pragmatic end-of-right-cluster
   position, not the redesigned "right after traffic lights" position that
   depends on unbuilt `title-bar.mdx` work.
4. **Persistence: through backend (`ui_state`), not session-only.** User
   explicitly chose real cross-restart persistence over an ephemeral
   `useState` default, after being told this means building the generic
   `ui_state` subsystem `ui-state.mdx` specs (new Go package, new bridge
   subject, new Zustand storage adapter) rather than reusing the existing
   `preferences`/theme table (which is intentionally per-field-typed at the
   bridge layer, not generic key-value — reusing it would mean growing its
   contract into something `ui_state` already exists to be).

Correction to `sidebar.mdx` itself, not a decision requiring a choice: the
doc's § Resize claims Sidebar should "dùng lại `react-resizable-panels`
(...cùng lib SplitPanel dùng)." Read against the real code
(`components/ui/split-panel.tsx`): `SplitPanelSlot` does **not** use that
library — its resize is hand-rolled `pointerdown`/`pointermove`/`pointerup`
with manual clamping, animated via `framer-motion`. `react-resizable-panels`
is a dependency in `package.json` but unused by the one existing precedent
this feature is supposed to match. Building Sidebar's resize against a
different library than its sibling primitive uses, one row over in the same
`content-row`, would be a real inconsistency for no benefit. This spec
mirrors the real `SplitPanelSlot` implementation and includes a one-line
fix to `sidebar.mdx`'s § Resize section.

Also resolved, not asked (mechanical, not a fork): `main.go`'s
`WebviewWindowOptions` sets no `MinWidth`/`MinHeight` at all — `sidebar.mdx`'s
own flagged "open question" about window min-width isn't unconfirmed, it's
answered: there is no floor today. The dynamic resize clamp (§ Resize below)
needs one to avoid nonsensical widths, so this spec adds it.

## Architecture

### 1. `ui_state` persistence (prerequisite, Go + bridge + frontend adapter)

Mirrors the existing `preferences`/theme implementation shape exactly
(`internal/preferences/{preferences,memory,sqlrepository}.go` and
`internal/bridge/preferences.go`), generic instead of theme-typed:

```go
// internal/uistate/uistate.go
package uistate

type Repository interface {
    Get(key string) (value string, found bool, err error)
    Set(key, value string) error
}

type Service struct{ repo Repository }

func NewService(repo Repository) *Service { return &Service{repo: repo} }
func (s *Service) Get(key string) (string, bool, error) { return s.repo.Get(key) }
func (s *Service) Set(key, value string) error          { return s.repo.Set(key, value) }
```

`MemoryRepository` (ephemeral containers) and `SQLRepository` (real,
`ui_state(key TEXT PRIMARY KEY, value TEXT, updated_at)`) mirror
`internal/preferences/{memory,sqlrepository}.go` line for line, table name
from a new `storage.TableUIState = "ui_state"` constant, migration entry
appended in `internal/storage/sqlite.go` next to the existing "create
env_tags" / "create app_preferences" migrations (same `CREATE TABLE IF NOT
EXISTS` idempotent pattern already used there).

Bridge, mirroring `internal/bridge/preferences.go`'s `s.call(...)` shape
exactly, subject/capability/scope from `ui-state.mdx`'s own table:

```go
func (s *AppService) UIStateGet(key string) (string, error)   // ui:read,  key:<key>
func (s *AppService) UIStateSet(key, value string) error       // ui:write, key:<key>
```

No native-apply-and-rollback step (unlike `PreferencesSetTheme`'s
`applyTheme` callback) — `ui_state` is plain UI preference with no native
window/appearance side effect to keep in sync.

Container wiring mirrors `main.go:82`'s
`container.Preferences = preferences.NewService(preferences.NewSQLRepository(db))`
exactly: `container.UIState = uistate.NewService(uistate.NewSQLRepository(db))`
after `NewContainerWithStores`.

Frontend adapter (`bridge/uistate.ts`), a single implementation of Zustand's
`PersistStorage` interface — every future `ui_state` consumer reuses this,
none writes its own:

```ts
export const uiStateStorage: PersistStorage<unknown> = {
  getItem: async (key) => (await callBridge(() => AppService.UIStateGet(key))) || null,
  setItem: debounce(async (key, value) => {
    await callBridge(() => AppService.UIStateSet(key, JSON.stringify(value)));
  }, 400),
  removeItem: async (key) => { await callBridge(() => AppService.UIStateSet(key, "")); },
};
```

`getItem` never debounces (`ui-state.mdx` is explicit: reads happen once at
store init). `setItem` debounces ~300–500ms so a resize drag doesn't hammer
the bridge — the doc's own explicit requirement.

### 2. `useSidebarContent` primitive (shell, frontend-only)

Mirrors `SplitPanelProvider`/`SplitPanelSlot`'s file shape
(`components/ui/split-panel.tsx`) but with the register/show split
`sidebar.mdx` calls out as the one real difference from `SplitPanel`:
registering content and showing the sidebar are independent — a module
registers whenever it's mounted, regardless of whether the shell currently
has the sidebar open.

That independence creates one thing `SplitPanel` never had to solve:
`SplitPanel`'s content is exclusive by construction (`openPanel`/`closePanel`
is a single slot, at most one caller matters at a time). Sidebar's own
"hide, don't destroy" performance rule (§ Performance below) means more than
one module's view can be mounted-but-hidden simultaneously, each still
calling `useSidebarContent`. Last-write-wins would let a hidden module's
unrelated state change silently clobber the active module's sidebar the
moment its effect re-runs. So content is keyed by module id, and the shell
renders only the active module's entry:

```ts
// components/ui/sidebar.tsx (new primitive, not the vendored shadcn one —
// that stays as NotesDashboard's private implementation, untouched)

const SidebarModuleIdContext = createContext<string | null>(null);

type SidebarShellState = {
  content: Map<string, ReactNode>;
  setContent: (moduleId: string, node: ReactNode | null) => void;
};

export function useSidebarContent(content: ReactNode | null) {
  const moduleId = useContext(SidebarModuleIdContext);
  const setContent = useSidebarShellContext().setContent;
  useEffect(() => {
    if (!moduleId) return;
    setContent(moduleId, content);
    return () => setContent(moduleId, null);
  }, [moduleId, content, setContent]);
}
```

`SidebarModuleIdContext` is provided once, where `WorkbenchViews` already
knows `tab.moduleId` for each mounted subtree (`Workbench.tsx`'s existing
per-tab render loop) — callers never pass an id, matching the zero-
boilerplate ergonomics of `useModuleAction`/`useSplitPanel`.

`SidebarSurface` (the rendered column, `Workbench.tsx`'s new flex sibling to
the left of `WorkbenchViews`) reads `content.get(activeModuleId)` — the
same `activeTab.moduleId` `Workbench.tsx` already computes today.

**Open/closed is a separate, global concern**, not part of this context:

```ts
// stores/sidebarShell.ts
export const useSidebarOpenStore = create(
  persist<{ open: boolean; setOpen: (open: boolean) => void }>(
    (set) => ({ open: false, setOpen: (open) => set({ open }) }),
    { name: "sidebar", storage: uiStateStorage },
  ),
);
```

Default `false` (closed) — matches the Figma App Shell component's own
`defaultVariant`, `Panel=Closed, Sidebar=Closed`.

### 3. Resize (mirrors `SplitPanelSlot`'s real pointer-resize code)

`SidebarSurface` reuses the exact pattern read out of
`components/ui/split-panel.tsx` — `pointerdown` capturing start
x/width, global `pointermove`/`pointerup` listeners, `Math.min/max` clamp,
`framer-motion` width animation, cursor/select-none body styling during
drag. Because the sidebar's max width depends on `SplitPanel`'s *current*
width and vice versa (the doc's own cross-clamp formula), the two widths
must live somewhere both resize handlers can read live — today
`SplitPanelSlot` owns its width as fully local `useState`, invisible to a
sibling. This spec lifts both widths into `Workbench.tsx` (the one place
that already renders both siblings) and extracts the shared pointer-resize
math into one hook both call:

```ts
// workbench/useResizableWidth.ts
function useResizableWidth(opts: {
  width: number;
  onWidthChange: (w: number) => void;
  min: number;
  max: number;
  sign: 1 | -1; // sidebar drags right-to-grow (+1), split panel left-to-grow (-1)
}): { isResizing: boolean; onPointerDown: (e: React.PointerEvent) => void }
```

`SplitPanelSlot` is refactored to call this hook instead of its inlined
version — same behavior, same constants, one implementation. `min`/`max`
passed in are computed once in `Workbench.tsx` from the live pair:

```
maxSidebarWidth    = min(480, windowWidth - splitPanelWidth - 480)
maxSplitPanelWidth = min(720, windowWidth - sidebarWidth    - 480)
```

`MIN_SIDEBAR_WIDTH`/`MAX_SIDEBAR_WIDTH` = 240/480, default 320 — per
`sidebar.mdx`. `SplitPanel`'s own 320/720 (`MIN_PANEL_WIDTH`/
`MAX_PANEL_WIDTH` — already real constants in `split-panel.tsx`) are
unchanged. `GAP_WIDTH` (4px) is reused as-is for the sidebar's own gutter —
one source of truth, per the file's own documented gotcha about that
constant drifting from a hardcoded Tailwind class in the past.

`main.go`'s `WebviewWindowOptions` gets `MinWidth`/`MinHeight` added —
currently absent entirely. Worst case both panels open at their floor:
240 (sidebar) + 4 (gutter) + 480 (content floor) + 4 (gutter) + 320
(split panel) = 1048, plus the outer `mx-1`/`mb-1` margins (8px) ≈ 1056.
`MinWidth: 1080` (small safety margin for borders/icons), `MinHeight: 700`
(current default content comfortably fits; not derived from any panel math,
kept close to today's `760` starting height). This resolves `sidebar.mdx`'s
own flagged open question with a real number instead of leaving it TBD.

### 4. Vault gating

Free, not new code. `VaultGate` already wraps all of `Workbench`
(`internal/vault`'s doc, confirmed) — `Workbench.tsx`, and therefore
`SidebarProvider`/the titlebar toggle, simply doesn't exist in the tree
before `unlocked`. No special-case check needed anywhere in this spec.

### 5. Performance (mount lifecycle)

`sidebar.mdx` asks for lazy-register-on-first-open + hide-not-destroy.
`WorkbenchViews` already implements exactly that policy for main content
(`mountedModuleIds`, MRU-capped at 3, 2-minute grace,
`className={tab.id === activeTabId ? "contents" : "hidden"}` rather than
unmounting). Because `useSidebarContent` is called from *inside* a module's
own view component — the same subtree `WorkbenchViews` already keeps warm
under that policy — sidebar content inherits it for free: no second cache,
no separate eviction policy to design or keep in sync with the first. This
is an explicit reading of `sidebar.mdx`'s one-line "ẩn chứ không huỷ" as
"the same policy WorkbenchViews already has," not a new unbounded-retention
mechanism — flagging this interpretation plainly since the doc's own
wording doesn't rule out something bigger.

`SidebarSurface` chrome (resize handle, toggle-driven width animation) is
its own `memo`'d component, isolated from whichever module's content is
currently rendered inside it — mirrors `WorkbenchViews`/`TabStrip`'s
existing separation, named directly in `sidebar.mdx`.

`VirtualList`/`VirtualTree` (new `@tanstack/react-virtual`-based helpers) are
**out of scope for this spec**: CodeGraph's endpoint/file lists are small,
nothing here needs virtualization yet, and `sidebar.mdx` itself frames them
as opt-in, added "khi thật sự cần" — building them now with no real consumer
would be exactly the premature-abstraction case the sidebar doc's own
performance section warns against for every *other* future helper shape.

### 6. `CodeGraphView` as first consumer

No new Zustand store, no state-sharing problem — the corrected API shape
(§2) removes the need for one. `CodeGraphSidebar` becomes a plain
presentational component (props + callbacks, no state of its own);
`CodeGraphView` keeps every piece of state and every async handler it
already has (`projectPath`, `busy`, `status`, `error`, `activeTab`,
`endpoints`, `files`, `selectedFile`, `fileContent`, `fileDirty`,
`pickAndOpenProject`, `reindex`, `viewFlow`, `openFile`, `saveCurrentFile` —
all unchanged), and pushes the sidebar's JSX through the hook:

```tsx
export function CodeGraphView() {
  // ...all existing state and handlers, unchanged...

  useSidebarContent(
    <CodeGraphSidebar
      projectPath={projectPath} busy={busy} status={status} error={error}
      endpoints={endpoints} activeTab={activeTab} onTabChange={setActiveTab}
      files={files} selectedFile={selectedFile}
      onOpenProject={pickAndOpenProject}
      onReindex={() => reindex(projectPath)}
      onSelectEndpoint={viewFlow}
      onSelectFile={openFile}
    />,
  );

  return (
    <div className="flex h-full flex-col overflow-hidden">
      {activeTab === "endpoints"
        ? (subgraph && rootId ? <FlowCanvas ... /> : <EmptyFlowState />)
        : (selectedFile ? <CodeEditor ... /> : <EmptyFileState />)}
      {fileError && <p className="text-sm text-destructive">{fileError}</p>}
    </div>
  );
}
```

`CodeGraphSidebar.tsx` (new file) owns exactly the JSX currently at
`CodeGraphView.tsx:172–194` (project row, status/error, Endpoints/Files
toggle) and `:198–213`/`:237–239` (the endpoint `<ul>` and `<FileTree>`),
moved verbatim as markup, driven entirely by props. This mirrors
`NoteEditor`'s existing pattern of pushing `<NoteMetaPanel />` into
`SplitPanel` from inside its own render — same shape, continuous
registration instead of an open/close pair.

## Error handling

- `UIStateGet`/`UIStateSet` failures: `uiStateStorage.getItem` returning a
  rejected promise falls back to Zustand `persist`'s own default-state
  behavior (sidebar opens closed, same as first run) — no new error UI.
  `setItem` failures are swallowed after the debounce fires (matches
  `preferences`' own `saveThemePreference` catch-and-log precedent) — a
  failed persist of "sidebar is open" is not worth surfacing to the user.
- Resize clamp math never produces a negative or NaN width: both `min`/`max`
  are derived with `Math.min`/`Math.max` against the live window size, and
  the new `MinWidth`/`MinHeight` on the native window (§3) makes sure the
  window itself can never get small enough to break the formula.
- A module that never calls `useSidebarContent` (every module but
  CodeGraph, initially) simply has no entry in the content map — `content.get(activeModuleId)` returns `undefined`, `SidebarSurface` renders nothing
  inside the column while still showing the resize/toggle chrome, exactly
  like `SplitPanel` today when no module has opened it.

## Testing / verification

- Go: unit tests for `uistate.Service` mirroring
  `internal/preferences/preferences_test.go` exactly (round-trip, missing
  key returns `found=false`), plus a bridge test mirroring
  `internal/bridge/preferences_test.go`'s round-trip test (no native-apply
  variant needed — nothing to mock).
- Frontend: no existing test harness for hooks/Zustand stores in this repo
  (`bun test` currently only covers pure modules — `rank.ts`/`place.ts` from
  the CodeGraph flow-canvas work). `useSidebarContent`'s module-id-keyed
  map is worth one narrow pure-logic test (`content.set`/`get`/`delete`
  behavior) if the map logic is extracted from the hook into a small
  standalone function — matches this session's established pattern of
  keeping pure logic separately testable from the React binding around it.
- Manual, against Figma `107:137`: open CodeGraph, confirm toggle opens/
  closes the column at 320px default, confirm drag-resize respects 240–480
  and visually matches `sidebar-surface`'s Figma width, confirm dragging
  `SplitPanel` open at the same time clamps both correctly, restart the app
  and confirm the open/closed state survived.

## Out of scope recap

- Migrating `NotesDashboard`'s sidebar to this primitive.
- The title-bar redesign (`title-bar.mdx`) — toggle stays in its pragmatic
  current-title-bar position until that separate project lands.
- CodeGraph's search box.
- Migrating `theme`/tab state onto `ui_state`.
- `VirtualList`/`VirtualTree` — no consumer needs them yet.
