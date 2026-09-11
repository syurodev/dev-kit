# Shared components — reconcile with Figma

Pages 03–07 were **inventoried** in the design-system Phase 3 report and their
**typography and radius** were fixed in Phase 4. Nothing else was ever compared
against Figma: no structure, no states, no missing components. Agent Hub is
built on top of these, so every drift here is inherited by the newest surfaces.

## Verified inventory — 41 component sets

Node ids below were read from the file on 2026-09-06, not copied from the
earlier report (which cited a section id for the slots and missed six
components).

### 03 Button & Icon Button
| Component | Node | Variants | First variant |
|---|---|---|---|
| `Button` | `27:54` | 24 — Variant × State × Size | 112×32, r=8 |
| `Icon Button` | `27:85` | 6 — **Tone** × State × Size | 32×32, r=8 |
| `Type=Secondary, State=Default, Size=LG` | `27:103` | 1 | 106×40, r=10 |
| `Type=Primary, State=Default, Size=LG` | `27:109` | 1 | 120×40, r=10 |

### 04 Input & Selection
| Component | Node | Variants | First variant |
|---|---|---|---|
| `Input` | `28:62` | 8 — **Type** (Text/Password) × State | 360×86 (Label + Field 360×40 r=8 + Helper) |
| `Checkbox` | `28:78` | 4 — State | row 220×32, Control 18×18 r=6 |
| `Tooltip` | `28:79` | 1 | 132×32 |
| `Tabs / List` | `318:385` | 2 — Variant | 300×32 |
| `Tabs / Trigger` | `317:380` | 4 — Variant × State | 36×25 |
| `Select / Trigger` | `378:61` | 4 — State | 220×33, **r=8** |
| `Select / Option` | `378:70` | 2 — Selected | 208×31 |
| `Select / Menu` | `378:71` | 1 | 220×142 |
| `Tag / Chip` | `378:2674` | 1 | 74×20 |
| `Tag Input` | `378:2698` | 2 — State | 320×32 |
| `Switch` | `570:59` | 3 — State | **32×18** |

### 05 Feedback & Progress
`Alert` `29:39` (4, Type) · `Step Progress` `29:76` (4, Step) ·
`Loading / Vault status` `29:77`

### 06 Security Patterns
`Recovery Key Field` `30:51` (3, State) · `Security Notice` `30:68` (2, Type) ·
`Vault Shell` `30:69` (960×540)

### 07 App Shell
| Component | Node | Variants |
|---|---|---|
| `App Shell` | `107:137` | 8 — Panel × Sidebar × Bottom, 1440×900 |
| `Titlebar` | `732:2170` | 4 — Mode × State, 1440×40 |
| `Titlebar Action` | `674:47115` | 3 — State, 28×28 |
| `Titlebar / Module Actions` | `678:1020` | 1 |
| `Titlebar / App Actions` | `677:986` | 1 |
| `System Status` | `616:932` | 3 — State |
| `Tab Strip` | `449:325` | 1, 1106×48 |
| `Tab Pill` | `448:343` | 3 — State |
| `Dock` | `208:438` | 2 — State |
| `Shell / Resize Handle` | `699:1104` | 4 — Orientation × State |
| `Shell / Command Palette` | `719:1475` | 2 — State |
| `Shell / Toast` | `727:1682` | 5 — Tone |
| `Shell / Content State` | `735:2169` | 2 — State |
| `Shell / Menu Item` | `816:2423` | 4 — Tone × State |
| `_Slot / App Content` | `705:47988` | 2 — Mode (Bleed/Padded) |
| `_Slot / Sidebar Content` | `116:1303` | 1 |
| `_Slot / Panel Content` | `105:6` | 1 |
| `_Slot / Bottom Content` | `203:250` | 1 |
| `Shell / Scroll Ruler` | `700:1100` | **spec only — never instance** |

## Known gaps carried over from the Phase 3 report
- **`Icon Button` has no code of its own.** Figma gives it a `Tone` property
  (Neutral / Primary); the codebase folds it into `size="icon"` on `Button`,
  losing tone entirely.
- **`Tag Input` and `Tag / Chip` have no code at all.**
- **`Input` has a `Type=Password` variant** with a built-in reveal toggle;
  `input.tsx` is a bare `<input>`.
- **`Select / Menu`** was never inventoried, so never compared.
- `Tabs`, `Select`, `Switch`, `Checkbox` had only their type and radius touched.

## Order — additive first, blast radius last
1. **Missing components** (`Tag / Chip`, `Tag Input`, `Icon Button` tone).
   Nothing existing changes, so nothing existing can break.
2. **Form controls** — `Input` (incl. Password), `Checkbox`, `Switch`,
   `Select` trigger/option/menu, `Tabs` list/trigger, `Tooltip`.
3. **Feedback & security** — `Alert`, `Step Progress`, `Loading`,
   `Recovery Key Field`, `Security Notice`, `Vault Shell`.
4. **App Shell** — every slot, the titlebar family, tab strip, dock, resize
   handle, command palette, toast, content state, menu item. Largest blast
   radius: these render on pages 07/12/13/14/15/16/17 at once.

## Window transparency — resolved 2026-09-06: the window is now SOLID
This section originally said the transparent window was deliberate and must not
be "fixed". Running the built app settled it the other way: on a busy desktop,
module content sits on the user's wallpaper and every surface colour is judged
against whatever is behind the window instead of against the palette.

`main.go` now defaults to a solid window. `DEVKIT_TRANSLUCENT_WINDOW=1` restores
the old vibrancy path (macOS `MacBackdropTranslucent`, Windows 11 22H2+ Mica) so
the two can still be compared; the old `DEVKIT_OPAQUE_WINDOW` flag is gone.

The glass tiers are unaffected — they tint over `--background-app` rather than
the desktop, which is what they are specified against anyway.

What step 4 **must** verify is which tier each surface uses: only
`glass/rail`, `glass/panel`, `glass/float` and `surface/glass-modal` are
translucent. `content-surface` is `surface/base` and must be **opaque** — if the
desktop is visible behind module content, that is a real bug.

## How to look at a component without the desktop app

`App.tsx` wraps everything in `VaultGate`, which calls `vaultStatus()` over the
Wails bridge — in a plain browser that never resolves, so the workbench and
every component inside it are unreachable. That is why four phases shipped on
`renderToString` + `toContain`, which cannot see overlap, overflow, or a class
that was merged away.

`frontend/src/dev/ComponentGallery.tsx` is mounted **before** the gate at
`?gallery=1`, lazy-loaded so it stays out of the initial bundle:

```bash
cd dev-kit-app/frontend && bun run dev   # or reuse a running wails3 dev on 9245
# then open http://localhost:9245/?gallery=1
```

Static props only — bridge-backed data is unavailable there by design, which is
also what makes it a fair comparison against a Figma node. **Extend it with one
`<Spec>` per component you reconcile**, labelled with the node id you opened.
That is where the screenshots come from.

## Out of scope
- Pages 08 Diff and 15 Database (frozen).
- Any Go change.
- New dependencies.
- Agent Hub module code — it consumes these, it does not define them.
