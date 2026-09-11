# Database module — frontend

Self-contained brief. You do not need the design conversations that produced it.

## The three sources, and which wins

- **Behaviour:** `base-doc/content/projects/dev-kit/database.mdx`. If this brief
  and that page disagree, the page wins — say so rather than choosing silently.
- **Visuals:** Figma file `3dOzSZBYY7QtzANFTCfEe8`, page **15 Database**
  (`507:2`). 26 screens and 33 components, listed at the end.
- **Contract:** the Go bridge. Every screen renders data some bridge method
  already returns. **If a screen seems to need data no method provides, stop and
  report it.** Do not invent an endpoint, and do not fake the data to make a
  screen look finished.

The Go core is complete. This is frontend only — no Go changes except
regenerating bindings.

---

## F0 · Prerequisites — do this first, it blocks everything

**The generated bindings are stale.** `frontend/bindings/` exposes 9 database
methods; the bridge has 24. Regenerate:

```
task generate:bindings
```

(`wails3` is on PATH; the task lives in `build/Taskfile.yml`.)

Then extend `frontend/src/bridge/database.ts`, which currently wraps only those
9. It is the typed adapter layer every view calls — views must never import
`bindings/` directly. Methods to add: `DBListSchemas`, `DBListTables`,
`DBListColumns`, `DBListForeignKeys`, `DBIndexSchema`, `DBGetSchemaCache`,
`DBListQueryHistory`, `DBSaveQuery`, `DBListSavedQueries`, `DBDeleteSavedQuery`,
`DBListActivity`, `DBCancelQuery`, `DBTerminateSession`, `DBExplain`,
`DBExplainAnalyze`.

**`DatabaseView.tsx` is 465 lines of scaffolding, not a foundation.** It was
written to prove the bridge worked, and its connection-options panel and
safe-mode banners pre-empt screens 17 and 18 with ad-hoc markup. Replace it;
do not extend it.

## F1 · Module shell

The workbench shell owns the titlebar and tab strip — modules supply content
only. Build the sidebar (`DB / Sidebar Content` 509:2) and the content area.

Sidebar has connections (grouped in folders, with a per-engine brand icon) and a
schema tree.

> **Folders are not in `database.mdx`.** They were designed in Figma and never
> given a backend contract. Do not add one. Either derive grouping from a
> `uistate` key, or ship v1 flat and say so. Whatever you choose, grouping must
> stay cosmetic — it can never change how a profile connects.

## F2 · Query console — the daily path

Screens **01**, **13** (error), **14** (running), **18** (read-only rejected),
**21** (MySQL placeholder).

- CodeMirror 6 is already a dependency, with SQL dialects. Pick the dialect from
  the profile's `engine`.
- **Never rewrite SQL between engines.** Show the engine badge and its
  placeholder style (`$1` vs `?`); the driver's own error is the teacher.
- Errors render the engine's own text — SQLSTATE, the `LINE`/caret position, the
  server's `HINT`. Where DevKit adds advice of its own, label it as DevKit's, as
  screen 21 does. Do not merge the two voices.
- The 30s cap is DevKit's, not the server's; screen 14 shows elapsed against it.
- Results cap at 200 rows and say so.

## F3 · Schema tree and cache

`DBGetSchemaCache` returns `{namespace, indexed_at}` where namespace is
`schema → table → columns` — already the shape `@codemirror/lang-sql` wants, so
feed it to the editor without transforming. Show `indexed_at` as freshness and
offer a manual refresh (`DBIndexSchema`). An empty cache indexes itself; do not
add a first-run button.

## F4 · Connection lifecycle

Screens **04**, **05**, **07**, **08**, **17**, **19**, **24**, **25**, **26**.

- **05 New connection** — the `Input` master was fixed so fields fill their
  column; if a field overflows again, the instance is overriding the master.
- **17 Connection options** — `env`, `ssh_profile_id` and `read_only` in one
  dialog, because one bridge method changes all three. The `prod` warning is not
  decoration: changing that tag changes agent-confirmation policy.
- **18/26** — a read-only rejection comes from the database, not DevKit; a
  mid-session vault lock keeps the result on screen because it is already in
  memory. Neither wording is interchangeable with a generic error.
- **19/25** — both fail closed and explain why. Do not soften them into retries.

## F5 · History, saved queries, export

Screens **03**, **20**, **23**.

- History is per-device and capped at 50; saved queries sync. Do not present
  them as one list with a filter — they are different storage for a reason.
- Both error when the vault is locked. Render the error; an empty list would
  read as "you have never run a query here".
- **Export already works.** `saveExportFile` sends bytes to Go, which opens the
  native dialog and writes. Do not reintroduce a Blob download — `<a download>`
  does nothing in a WKWebView. `serializeCSV`/`serializeJSON` in
  `modules/database/export.ts` are correct; reuse them.
- JSON exports every value as a string. Screen 20 says so; keep that.

## F6 · Live activity and query plans

Screens **10**, **11**, **12**, **15**, **16**, **22**.

- Sort by duration descending — the longest-running thing is why the screen was
  opened.
- `blocked_by` is Postgres-only and comes back empty on MySQL. Render the
  blocking relationship when present; do not draw an empty connector.
- **15 (nothing running) is the common case**, not the exception. **16** is for
  an engine that reports no sessions — remove the refresh and filter controls
  there rather than leaving buttons that can never do anything.
- Cancel and terminate are separate actions with different consequences. Both
  confirm on `env=prod`. Safe mode does **not** cover them — screen 12 says so
  and that line must survive.
- **22** — bars are self time, not inclusive, and estimate-vs-actual gaps beyond
  10× are the finding. `DBExplain` plans; `DBExplainAnalyze` runs the statement
  inside a rolled-back transaction and follows the env policy.

## F7 · ERD

Screen **02**. `@xyflow/react` is already a dependency. Declared foreign keys
(`DBListForeignKeys`) and inferred relationships are **two separate lists** and
must stay visually distinct — an inferred edge presented as a real constraint is
a lie about the schema.

---

## Rules for the whole job

- **No new dependencies.** CodeMirror, xyflow, Recharts, Radix and the shadcn
  components are all present.
- **Keep it out of the initial bundle.** Module views load through
  `modules/registry.ts` `lazyModuleView`. `bun run build` enforces a budget;
  breaking it fails the build.
- **Use the design system, not one-off markup.** `Button` has all seven shadcn
  variants including `destructive`; `Switch`, `Input`, `Select`, `Tabs`,
  `Badge` exist. If Figma shows something with no component, say so before
  hand-rolling it.
- **Tokens, never literal colours.** Everything resolves from `index.css`; both
  themes must work. A hardcoded hex is a bug.
- Run `bun test` and `bun run build` before reporting.

## Testing rules

**A negative test must be shown to fail.** This project has twice shipped a test
whose name promised a guarantee its body never checked. When you assert that
something is absent, disabled, or refused, temporarily make it present and watch
the test fail, then restore — and say in your report that you did.

## Suggested order

F0 → F1 → F2 → F3 → F4 → F5 → F6 → F7. F2 is the daily path and worth getting
right before breadth; F7 is the only one nothing else depends on.

## Figma node ids

Screens: 01 `513:90` · 02 `518:612` · 03 `518:923` · 04 `518:1362` ·
05 `518:1584` · 06 `518:1784` · 07 `522:2090` · 08 `522:2233` · 09 `533:2794` ·
10 `544:3333` · 11 `554:3819` · 12 `554:4327` · 13 `568:5108` · 14 `568:5510` ·
15 `568:5904` · 16 `568:6305` · 17 `572:5891` · 18 `572:6416` · 19 `572:6815` ·
20 `574:6856` · 21 `574:7378` · 22 `575:7651` · 23 `578:8647` · 24 `578:9226` ·
25 `578:9741` · 26 `578:10133`

Key components: `DB / Sidebar Content` 509:2 · `DB / Console Content` 511:75 ·
`DB / ERD Content` 515:551 · `DB / Profile Row` 507:29 · `DB / Folder Row`
529:2496 · `DB / Schema Tree Row` 508:26 · `DB / Engine Badge` 528:2454 ·
`DB / Titlebar Actions` 513:77 · `DB / Cell Inspector` 578:8538

## Notes

- Linux cross-compile fails inside Wails' own GTK cgo files. Pre-existing.
- `base-doc` may carry unrelated uncommitted changes. Leave them alone.
