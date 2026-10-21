<!-- Research notes behind docs/dev-kit/frontend-performance.mdx (section "Kỹ thuật cực hạn từ các chuyên gia") and docs/dev-kit/smoothness.mdx (section 13). Written 2026-10-21 by research agents. READ = source fetched and read; SNIPPET = search summary only; MEASURED = run on this machine. Kept as-is for traceability; the .mdx files hold the conclusions. Raw probe files mentioned here were not kept in the repo. -->

# Research D — Measurement discipline, startup/bundle cost, memory/GC hygiene, perf testing in CI, anti-pattern catalogue

Date of research: 2026-10-21. Read-only on the repo; this file is the only output.
Convention: **READ** = page fetched and read in this session. **SNIPPET** = only a search-result summary was seen (never treated as proof). Anything marked "(my inference)" is mine, not the source's. Where a fetch failed it is listed in section 6; nothing was filled in from memory.

---

## 0. What the two existing DevKit docs already say (so this file does not repeat it)

`docs/dev-kit/frontend-performance.mdx` (architecture/budgets) already has: workload classes; component split by update frequency; normalized stores + narrow selectors; virtualization thresholds (200 cards / 100 table rows / 300 transcript blocks / 500 tree nodes); bounded bridge responses; realtime batching to one publish per frame; heavy work off the render path; tab lifecycle policy; render-budget table (keystroke < 16 ms, warm tab < 50 ms, scroll no long task > 50 ms); a 7-step profiling procedure (fixture -> production build in Wails/WebKit -> React Profiler + trace -> bridge timing -> fix one thing -> before/after in PR -> budget); a "forbidden optimizations" list; a before/after table for the transcript reducer (median of 3 in one process, deterministic fixture, counters such as identity changes = 20 vs 100 000, DOM sections = 11 vs 5 000).

`docs/dev-kit/smoothness.mdx` already has: first-frame correctness; layout stability; motion tokens; compositor-only animation; WebKitGTK-specific findings (opacity on a big tree builds a layer and paints it in one frame; `inert` toggling restyles the subtree; hidden scroll boxes; `moveBefore` absent) measured with a harness (10 toggles, two passes, p90 frame gap, worst frame, first frame); `perfTrace` + `frameStall` probe behind `DEVKIT_PERF=1` (rAF + 25 ms timer, two clocks distinguish JS from compositor); ratchets (`smoothnessRatchet.test.ts`, `storeSubscriptionRatchet.test.ts`, `moduleBoundaryRatchet.test.ts`, `designSystemConformance.test.ts`); Figma "Keeping Figma Fast" already summarised (20 % margin, p50/p90, 10-min limit); Linear/Notion/VS Code/Apple notes; tool decisions (no web-vitals lib: "20 lines of PerformanceObserver is enough"); the startup-budget script (initial JS <= 2 MiB, CSS <= 320 KiB per the task brief; the older doc says 700 KiB / 225 KiB gzip, so the ceiling has moved).

Gaps this research targets (observations, not criticisms):
1. No written **statistical protocol** (run count, warm-up, median + spread, what to hold constant) — the transcript baseline says "median of 3 in one process", the WebKitGTK harness says "10 toggles, two passes", they differ.
2. No **deterministic counter** layer beyond the transcript fixture: render counts per interaction, DOM node counts, listener balance, heap/leak cycles.
3. Startup budget is a **total**; there is no per-heavy-dependency "must not be in the initial graph" assertion, no waterfall-depth check.
4. Memory/long-session hygiene is only mentioned (transcript cache limit 8). No leak-cycle test, no bounded-cache convention.
5. Anti-pattern ratchets cover store destructuring, `useState([])`+fetch, backdrop-filter, unvirtualized tables, `animate-in` without duration. Many other patterns from this catalogue have zero detector (see section 4).

Repo facts measured today (coarse `grep -l`, production files only, frontend/src; used only to size the proposals in section 4 — these are not claims about correctness):
`new ResizeObserver` 19 files; `getBoundingClientRect` 12; `getComputedStyle` 9; `useLayoutEffect` 20; `offsetHeight|offsetWidth|scrollHeight` 20; `createContext` 17; `useSyncExternalStore` 3; scroll `addEventListener` 3; `will-change` 1; `box-shadow|shadow-` 34; `backdrop-blur|backdrop-filter` 18; `key={i|index|idx}` 32; `:has(` 5; `from "lucide-react"` 286 (named imports); `framer-motion` imports 19 files, `LazyMotion` in 2 (`sidebar-shell.tsx`, `pane-slots.tsx`) while e.g. `VaultStage.tsx` imports `motion`; `WeakRef|FinalizationRegistry` 0; `<Profiler>` already wired in `AppShell.tsx`, `Workbench.tsx`, `DocumentGroups.tsx` (`recordCommit`).

---

## 1. Synthesis

### 1.1 The 15 highest-payoff rules for DevKit (ranked)

Ranking = (probability it prevents or catches a real regression in DevKit) x (cost to adopt), using only what the sources below support.

1. **Gate on deterministic counters first, time second.** Render count per interaction, DOM node count, listener add/remove balance, bytes in the initial graph, number of long frames in a scripted scenario. Use time only as a coarse backstop with a wide margin (Figma uses 20 % on noisy VMs and says it only catches O(n)->O(n^2)-class regressions; Nethercote: wall time outliers were -8.9 %/+9.7 % on a no-op change while instruction counts stayed within +-1.3 %, and 0.3 % changes were detectable only with counts). Sources: #23, #20, #27, #24.
2. **Never accept a single timing.** Every perf claim needs: fixed fixture, N >= 10 runs after >= 1 discarded warm-up, median plus spread (p90/min-max or MAD), and a same-machine same-session A/B. Reassure's defaults are 10 runs + 1 warm-up with outlier removal and a stability check ("same code twice must show ~0 %; < 5 % random is normal, >= 10 % is too high"); tinybench reports rme/MAD; Mozilla alerts compare medians of 12 before/after points. Sources: #27, #29, #24.
3. **Measure on the real engine; treat Chromium as a proxy only.** WebKit has no `longtask` and no Long Animation Frames (LoAF); Chrome/Edge have both; the existing rAF+timer probe is the right instrument on WebKitGTK. Check `PerformanceObserver.supportedEntryTypes` at runtime on the actual WebKitGTK 2.52 / WKWebView / WebView2 before trusting any observer. Sources: #9, #13, #14, #15.
4. **Record the rendering mode with every number.** Headless Chromium defaults to software rendering (SwiftShader); a CI report went from 5+ min / 30 s canvas load to 1.4 min / <1 s only after real GPU access. WebKitGTK has its own software-path pathologies (CSS `filter: blur()` measured 184 ms median between frames for 8 boxes vs 16 ms without, WebKitGTK 2.54.1). A number without the compositing mode is not comparable. Sources: #21, #22, #44.
5. **Observer effect: tracing and DevTools change what is measured.** Chrome's own team: overhead "will never be zero", and a real case had 5-10x slowdown with DevTools open; WebKit's Media/Memory/Allocations timelines are off by default for the same reason; Chrome's selector-stats option is off by default for overhead. Use the lightest instrument that answers the question (counters, `performance.measure`, `console.timeStamp`), then a trace to explain, never to compare against an untraced run. Sources: #18, #15, #8, #6.
6. **Fix the slow render before the re-render; reduce render count by restructuring state before adding memo.** Kent C. Dodds / Abramov / Erikson converge: a re-render that produces no DOM change is usually cheap; "move state down" and "lift content up" come first; React Compiler is on, so manual memo is mostly redundant but compiler bail-outs are invisible unless checked (compiler skips code that breaks the Rules of React rather than failing). Sources: #51, #52, #53, #55.
7. **Startup is a per-dependency budget, not only a total.** Assert that Mermaid, Shiki grammars, Plate, CodeMirror language packs, React Flow, xterm, dagre/ELK, etc. are not reachable from the initial entry graph (Vite manifest `imports` vs `dynamicImports`); the Notion case study found 39 % of vendor and 61 % of app bundle unused after first render; Shiki "full" is 6.4 MB min / ~1.2 MB gzip while the fine-grained path imports only chosen grammars; Motion's `motion` component cannot tree-shake below 43 kB vs ~5 kB initial with `LazyMotion` + `m`. Sources: #35, #39, #40.
8. **Own the module-graph shape.** Avoid barrels in app code (Vite's own perf guide says so; barrel loading costs scale with module count: 10 000 modules 3.1 s, 50 000 modules 48 s in a synthetic test; Next.js `optimizePackageImports` cut lucide-react from 1 583 to 333 modules in dev). In a **production** Vite build tree-shaking handles `import { X } from "lucide-react"` (the lucide page says "tree-shakable"), so for DevKit the barrel cost is mainly **dev server, tests (every `bun test --isolate` file re-loads the graph) and typecheck**, not the shipped bundle (my inference from sources #36, #38, #45c; verify with the build). Sources: #36, #37, #38, #45c.
9. **Keep a flat, shallow module preload graph.** `modulepreload` fetches a module *and lets the engine parse/compile ahead of execution*; nested dynamic imports serialise round trips because dependencies are only discovered after a module loads. Count chunk-dependency depth for the shell route. Sources: #41, #34. Caveat: JavaScriptCore's code-caching story is not documented in anything fetched (see #34), so do not extrapolate V8 numbers (parse+compile are no longer the dominant startup cost in V8 2019 analysis, execution and download are) to WebKit.
10. **Long-lived sessions: bound every cache, pair every listener, test mount/unmount cycles.** Lawson: leaks that grow per interaction (even ~1 kB/interaction) matter for apps open for days; SPAs observed leaking up to 186 MB per interaction; typical sources are window/document listeners, intervals, growing arrays, detached DOM. Add a bun test that mounts/unmounts the big views N times and asserts add/remove listener balance and bounded store/cache sizes; do the heap-snapshot "three snapshot / repeat and reverse" workflow manually before releases. Sources: #48, #47, #49.
11. **Do not use WeakRef/FinalizationRegistry to make a cache "self-cleaning".** MDN: "best avoided"; collection timing is nondeterministic and differs per engine and per version (and DevKit runs three engines). Use explicit LRU / size caps and explicit teardown. Source: #46.
12. **Layout reads are the main JS-side jank source after render count.** Reads of `getBoundingClientRect`, `offsetWidth`, `scrollHeight`, `getComputedStyle` (for width/height/transform/etc.), `focus()`, `scrollIntoView`, `innerText` force style+layout if anything is dirty; alternating read/write in a loop pays once per iteration. 12+20+9 files in DevKit read layout today. Detect with ratchets + a LoAF-based probe on Chromium (LoAF exposes `forcedStyleAndLayoutDuration` per script). Sources: #58, #59, #9.
13. **`useLayoutEffect` + measure + `setState` = two-pass render that blocks paint** (React docs). 20 DevKit files use it; each is a candidate for a render-count test. Source: #57.
14. **CSS cost sources (Blink/WebKit):** selector complexity matters less than *how many elements an invalidation touches*; `:has()` reverses the invalidation direction (a mutation on a descendant walks ancestors) and the Chromium authors themselves list "authors can easily increase complexity" as a risk, advising `:has(> .b)` over `:has(.b)` and `.a:has(.b:hover)` over `.a:has(:hover)`; blur-type paint (box-shadow, filter) costs more than flat fills; each compositor layer costs memory and GPU upload. A real MS case: a `+`/`~` selector on `body` caused a ~2 s hover delay. Detect with Chrome's Selector Stats table and ratchets on `:has(`, `*:has(`, `filter: blur`, `will-change`. Sources: #44, #61-#65, #68-#70.
15. **Perf PRs carry an evidence block (template 1.2) and a counters diff, and the perf CI job has a time cap.** Figma: guardrail checks must finish in < 10 minutes; Mozilla "PerfCompare"/Chromium Pinpoint compare medians over repeated runs. Sources: #23, #24, #26.

### 1.2 Proposed "perf evidence template" (every performance PR)

Grounded in: Figma (p50/p90, hardware vs VM, bisect on real machines), Reassure (runs/warm-up/outliers/stability check), Mozilla (median-of-12 alerts, 2 % default threshold, retriggers), Nethercote (counts over time), mraleph (verify you measure the thing), Akulov/Causal ("medians of 5 runs, before and after"), Chrome docs (DevTools throttling is relative; calibrate), WebKit Timelines doc (heavy timelines off by default).

```
## Perf evidence
Claim:            <one sentence: "<interaction> on <fixture> now does X instead of Y">
Metric(s):        primary = <counter or time>; secondary = <...>; why this metric answers the claim
Scenario:         <scripted steps, e.g. "type 20 chars in note editor, 300 notes loaded, tab warm"> + script path
Fixture:          <name + generator path + size: rows/turns/nodes> (deterministic seed, committed)
Build:            production build? yes/no; commit SHAs of BASE and HEAD; same lockfile
Engine(s):        WebKitGTK x.y (GPU? DMABUF on/off, compositing mode) | WKWebView macOS x | WebView2 x | Chromium x (proxy only)
Machine:          CPU, RAM, GPU/driver, power mode (plugged in), display scale, window size; other load stopped
Instrumentation:  what was on (DEVKIT_PERF=1, Profiler, Performance panel, selector stats, screenshots...) — identical for BASE and HEAD
Held constant:    fixture, window size, data, build flags, engine, rendering mode, throttling, warm/cold state
Runs:             N = __ per variant (>= 10 for timings; deterministic counters: 1-3), warm-up discarded = __, interleaved A/B order = yes/no
Results:          table BASE vs HEAD: median, spread (p90 or min-max or MAD/rme), N, delta %, counters (renders, DOM nodes, long frames, listeners)
Stability check: BASE vs BASE (same code twice) delta = __ % (must be < 5 %; if >= 10 % the numbers are not usable)
Not measured:     <what this does not prove: other engines, cold start, memory, long sessions>
Regression guard: <ratchet/test/benchmark added or why none> + threshold and its reason
```

Rules that go with it: (a) at least one **deterministic counter** accompanies any time claim; (b) a time claim under ~5-10 % without a stability check is rejected; (c) numbers from Vite dev mode are rejected (already in the existing doc); (d) a change that raises a budget needs a named dependency and decision (already in the existing doc); (e) record rendering mode.

### 1.3 Detectors that could be added as ratchet/tests (each tied to an anti-pattern and a source)

All detectors are "count may only go down; test also fails if the number is lower than the baseline" in the style DevKit already uses. Counts are today's coarse file counts (section 0). "Det." = deterministic (no clock).

| # | Detector | Anti-pattern | Source | How | Det. |
|---|---|---|---|---|---|
| D1 | Compiler bail-out ratchet: run the React Compiler transform over `src/` in a test, count components it skips | Manual-memo-free code silently de-optimised; compiler skips code that breaks Rules of React | #55 | Babel plugin in a bun test with a logger/diagnostics collector (the logger API is **not** verified from fetched sources — check the compiler README before building) | yes |
| D2 | `react/jsx-no-constructed-context-values` (and equivalent) as lint, plus a test that every `createContext` provider value in `src/` is a stable reference | Context value churn forces every consumer to re-render (17 `createContext` files) | #51b, #51 | ESLint rule is documented; with the compiler on it may be redundant for compiled components, keep for bail-outs | yes |
| D3 | Render-count scenarios using `<Profiler onRender>` counts (not durations): keystroke in editor / publisher / SQL console re-renders <= K components; tab switch <= K | Re-render storms; state too high | #1, #27, #51 | happy-dom + `<Profiler>` per scenario; assert `commits` and per-id counts | yes |
| D4 | `useLayoutEffect` allow-list ratchet; each entry must be annotated "measures before paint" | Two-pass render blocking paint (20 files) | #57 | regex count + required comment | yes |
| D5 | Forced-layout read ratchet: `getBoundingClientRect|offsetWidth|offsetHeight|scrollHeight|clientHeight|getComputedStyle|innerText|scrollIntoView` in files that also write style in the same function / in `useEffect` / ref callbacks | Layout thrash / forced sync layout (12+20+9 files) | #58, #59 | grep ratchet + manual allow-list; runtime companion: Chromium harness records LoAF `forcedStyleAndLayoutDuration` per scenario (Chromium only) | yes (static) |
| D6 | ResizeObserver inventory: count `new ResizeObserver` (19), require each callback to either not write size-affecting styles or go through `settledRunner`/rAF; runtime: fail a harness scenario on any window `error` event "ResizeObserver loop completed with undelivered notifications" | Feedback loops, multi-frame flicker | #60 | static count + `window.addEventListener("error")` in the e2e harness | yes |
| D7 | `useSyncExternalStore` snapshot contract test: call `getSnapshot()` twice, assert `Object.is`; assert `subscribe` identity stable across renders (3 files; extend to Zustand selector wrappers) | Unstable snapshot -> render loop; inline subscribe -> resubscribe each render | #50 | pure function test | yes |
| D8 | `key={index}`/`key={i}`/`Math.random()` ratchet (32 files) with allow-list for static lists | Lost state + recreated DOM | #56 | grep ratchet, or `react/no-array-index-key` (rule name not verified here) | yes |
| D9 | "Derived state in an effect" ratchet: `useEffect(() => { set... (` whose deps are props/state of the same component, plus existing `useState([])`+fetch | Cascading extra render pass | #54 | regex ratchet (coarse) + review; there may be an ESLint rule in the Rules-of-React plugin — **not verified here** | yes |
| D10 | CSS selector ratchet: disallow `*:has(`, `:has(` without a `>`/`+` anchor, `:has(:hover)`/`:has(:focus)`, `+`/`~` selectors on `body`/`html`/large containers, `:nth-last-child(...)` descendant selectors in app CSS (5 `:has(` files today) | Broad invalidation | #61-#65 | CSS parse test (postcss) over `index.css` + component CSS | yes |
| D11 | Paint-cost ratchet: count `filter: blur`, large-radius `box-shadow` on large surfaces, `backdrop-filter` (existing), permanent `will-change` (1 file) | Blur-class paint, layer explosion | #44, #68, #69, #70 | grep ratchet; WebKitGTK `filter: blur()` slowness is a known open bug | yes |
| D12 | Animated-property check: any `transition`/`animate` on `width|height|top|left|margin|padding|box-shadow` outside the documented panel exceptions | Layout/paint per frame | #70, #68 | CSS/TSX regex ratchet | yes |
| D13 | `content-visibility:auto` must be paired with `contain-intrinsic-size` | Scrollbar jump / layout shift | #67 | CSS test | yes |
| D14 | Initial-graph forbid-list: parse Vite manifest, fail if any module of mermaid / shiki / @codemirror/lang-* / plate / @xyflow / xterm / dagre / elkjs is reachable via static `imports` from the entry; plus **per-chunk** size ceilings for the shell chunk and each heavy lazy chunk | Heavy dependency leaks into startup | #35, #39, #40, #45a | post-build script next to `check-performance-budget.mjs` | yes |
| D15 | Import-depth check: longest chain of *dynamic-import-then-static-import* edges needed for the first usable shell frame <= K | Request waterfalls; late discovery of dependencies | #41 | manifest walk | yes |
| D16 | `motion` vs `m`: count of full `motion.` components in the initial graph (19 files import framer-motion, 2 use `LazyMotion`) | 43 kB non-tree-shakable component vs ~5 kB | #40 | grep ratchet or check manifest for the full feature bundle; `LazyMotion strict` is the documented enforcement | yes |
| D17 | Listener/timer balance test: wrap `addEventListener/removeEventListener/setInterval/clearInterval/requestAnimationFrame` on `window`/`document` in a test, mount+unmount each top-level view 20x, assert zero net registrations | Listener / timer leaks in long sessions | #48, #47, #49 | happy-dom spy; for AbortSignal-based cleanup see #47 | yes |
| D18 | Bounded-cache convention: module-scope `new Map()/new Set()/[]` that grow need a `// bounded: <how>` comment or a named `createBoundedCache`; ratchet the unannotated count (166 files use `new Map/new Set`, most are local; scope the regex to module scope) | Unbounded growth | #48 | AST scan (ts-morph/regex) | yes |
| D19 | Store/transcript growth test: push N synthetic events, assert retained entries <= configured cap (extends the existing `TRANSCRIPT_CACHE_LIMIT = 8` idea to all stores) | Closures/arrays retaining large transcripts | #48 | bun test | yes |
| D20 | DOM-node ceiling per scenario (generalise the transcript SSR window test): render list/table/tree/graph views with the standard fixture and assert node count <= ceiling | Unvirtualized collection, large DOM slows rendering and interactions | #11, #59 | SSR or happy-dom count | yes |
| D21 | Frame-stall e2e (Chromium + WebKitGTK MiniBrowser where possible): scripted scenario, N >= 10 runs, assert p90 frame gap and number of frames > 50 ms; BASE vs HEAD comparison with a wide margin | Scroll/switch jank | #23, #15, existing `frameStall.ts` | extend the existing hand-run harness into a CI-callable script that prints the evidence template JSON | no (time) |
| D22 | Font check: every `@font-face` has `font-display`, woff2 only, size budget for bundled fontsource files | Late font swap / large font payload | #43 | CSS + file-size test | yes |
| D23 | Bench-quality lint for `*.bench.ts`: require warm-up, require result consumption (`do_not_optimize`-style), print rme/MAD, forbid single-run | Misleading microbenchmarks | #19, #29 | convention + helper wrapper | yes |
| D24 | Platform feature probe test in the app (log once under `DEVKIT_PERF=1`): `PerformanceObserver.supportedEntryTypes`, `typeof scheduler?.yield`, `CSS.supports("selector(:has(*))")` | Assuming an API exists on WebKitGTK | #13, #14, #75 | runtime log; paste into PR evidence | n/a |

---

## 2. Source entries

Format per entry: title — author/role — URL — date — READ/SNIPPET — claims — **Rule for DevKit** — caveats — confidence.

### A. Measurement

**#1. React reference: `<Profiler>`** — React team (react.dev) — https://react.dev/reference/react/Profiler — date not shown — **READ**
- `onRender(id, phase, actualDuration, baseDuration, startTime, commitTime)`; `phase` is mount / update / nested-update; `actualDuration` = time rendering the profiled subtree for this update and "indicates how well the subtree makes use of memoization"; `baseDuration` estimates worst-case (no memo) cost; `commitTime` is shared across profilers in one commit.
- "Profiling adds some additional overhead, so it is disabled in the production build by default"; need a special profiling build. "Each use adds some CPU and memory overhead".
- **Rule:** Use `<Profiler>` for **counts** (commits per scenario, per-id) and for `actualDuration` relative to `baseDuration`; DevKit already mounts it at 5 shell sites via `recordCommit` — keep those behind a flag and make a test that asserts the number of commits per scripted interaction. Do not leave Profilers in the normal production build (overhead, and they are inert there anyway).
- **Caveat:** measures React's render phase only; an expensive layout effect or layout work is outside it (see #74). **Confidence: high.**

**#2. React Performance tracks (profiling builds)** — React team — https://react.dev/reference/dev-tools/react-performance-tracks — date not shown — **READ**
- Scheduler track (Blocking / Transition / Suspense / Idle) with phases Update -> Render -> Commit (incl. layout effects) -> Remaining Effects; "Cascading updates" (update scheduled during render) are flagged; Components track is a flamegraph; effects < 0.05 ms hidden.
- Profiling builds are not production builds: import `react-dom/profiling` instead of `react-dom/client` (alias at build time). Profiling builds enable only Scheduler tracks by default; the Components track includes only `<Profiler>`-wrapped subtrees unless the DevTools extension is active. Tracks show in browsers supporting the extensibility APIs.
- **Rule:** Add a `?perf-profiling` / env-flagged Vite alias of `react-dom/client` -> `react-dom/profiling` so a **production-shaped** build can be traced; PR evidence says which build it was. Look for "Cascading update" entries when hunting effect->setState chains (#54).
- **Caveat:** whether the tracks render in WebKitGTK's inspector is **not documented** here; the tracks are a Chromium Performance-panel feature in the page's wording ("browsers that support extensibility APIs"). **Confidence: high (facts), low (WebKit availability).**

**#3. Introducing the React Profiler** — Brian Vaughn (React team) — https://legacy.reactjs.org/blog/2018/09/10/introducing-the-react-profiler.html — 2018-09-10 — **READ**
- Commit bars (height/colour = duration), flame chart (bar width = last render time; colour = time in the selected commit; grey = did not render), ranked chart (slowest first; parent time includes children); comparing a component across commits "may also provide a hint" why it rendered; `react-dom/profiling` supports profiling outside DEV.
- **Rule:** Read the ranked chart first for "which component is slow", the commit list for "how many commits". The "record why each component rendered" checkbox is the DevTools-side "why did this render" signal (search summaries of third-party guides agree, see #4) — turn it on when attributing re-renders.
- **Caveat:** old article (2018); UI details changed. **Confidence: medium.**

**#4. DevTools "Record why each component rendered"** — third-party guides (Pluralsight, DebugBear, oneuptime) — e.g. https://www.debugbear.com/blog/react-devtools — date unknown — **SNIPPET**
- Summary only: gear icon in Profiler tab -> checkbox; reasons such as "props changed", "parent rendered", "first mount".
- **Rule:** use it, but quote the reason from your own profile in a PR, not from this summary. **Confidence: low** (no primary source read; the primary React docs page for the DevTools was not fetched).

**#5. MDN: User Timing API** — MDN contributors — https://developer.mozilla.org/en-US/docs/Web/API/Performance_API/User_timing — date not shown — **READ**
- `performance.mark(name, {startTime, detail})`; `performance.measure(name, startMark, endMark)` or `{start, end, detail}` (e.g. using an event's `timeStamp` as start); prefer `PerformanceObserver` over `getEntries*` (observer receives entries passively; getEntries only returns what exists at call time); `clearMarks/clearMeasures`.
- **Rule:** measure interactions from `event.timeStamp` (input time), not from the handler start, so input delay is included; always `clearMarks/clearMeasures` in long sessions (entries accumulate in the performance buffer — buffer limits are **not** stated on the page; LoAF page says its buffer holds 200 entries, #9); use a single naming scheme (`devkit:<module>:<scenario>`) so `perfTrace.ts` output is greppable.
- **Caveat:** MDN page gives no naming/buffer guidance. **Confidence: high.**

**#6. Chrome DevTools performance extensibility API** — Chrome DevTools docs — https://developer.chrome.com/docs/devtools/performance/extension — date not shown — **READ**
- `console.timeStamp(label, start?, end?, trackName?, trackGroup?, color?)` is "designed for minimal runtime overhead, making it suitable for instrumenting hot paths and production builds", does not add to the performance timeline; `performance.measure` with `detail.devtools = {dataType:"track-entry", track, trackGroup, color, properties, tooltipText}` draws custom tracks; custom data only appears if "Show custom tracks" is enabled.
- **Rule:** emit DevKit scenario spans as `performance.measure` with `devtools.track = "DevKit"` (so a Chromium trace of the web UI shows them) and use `console.timeStamp` in hot paths. Keep one helper (`lib/perfTrace.ts` already calls `performance.measure`) so the instrumentation is identical in BASE/HEAD.
- **Caveat:** Chromium DevTools only; WebKit Timelines will not draw these tracks (it lists marks/measures only through its own mechanisms; not verified). **Confidence: high.**

**#7. Chrome DevTools: Performance panel reference** — Chrome DevTools docs — https://developer.chrome.com/docs/devtools/performance/reference — date not shown — **READ**
- CPU throttling is **relative to your machine** ("2x slowdown" = half speed) and DevTools "can't truly simulate mobile CPU architecture"; there is a **Calibrate** option for custom presets; Memory checkbox adds heap/memory charts; Timings track shows marks/measures with `detail` and stack traces; "advanced paint instrumentation" enables Layers tab and Paint Profiler.
- **Rule:** if throttling is used, record the multiplier **and** that it is relative to a named machine; DevKit is a desktop app, so prefer no throttling on a named reference machine (plus a second, slower machine) over a simulated 4x; never compare numbers across machines.
- **Caveat:** the page does not say how much overhead screenshots or the profiler add (not covered). **Confidence: high.**

**#8. Chrome DevTools: CSS selector stats** — Chrome DevTools docs — https://developer.chrome.com/docs/devtools/performance/selector-stats — date not shown — **READ**
- Enable "Enable CSS selector stats" in Capture settings (off by default because it adds overhead). Columns: Elapsed (ms), Match Attempts, Match Count, % of slow-path non-matches. Prioritise selectors with high elapsed time, many attempts, low match-count-to-attempts, high slow-path %. Re-record and see whether Recalculate Style falls.
- **Rule:** when a trace shows long "Recalculate Style", open selector stats on the same scenario; log the top 5 selectors in the PR. Blink-only (no WebKit equivalent found).
- **Confidence: high.**

**#9. Long Animation Frames API** — Chrome for Developers — https://developer.chrome.com/docs/web-platform/long-animation-frames — date not shown — **READ**
- A long animation frame is one whose rendering update is delayed beyond **50 ms**; unlike Long Tasks it measures frames (many small tasks can add to a long frame) and has script attribution (`scripts`, reported for > 5 ms), `blockingDuration`, `renderStart`, `styleAndLayoutStart`, and per-script `forcedStyleAndLayoutDuration`. Supported in Chrome/Edge 123+; **Firefox and Safari support neither LoAF nor Long Tasks** (per the page). Observe with `PerformanceObserver` `type:'long-animation-frame', buffered:true`; the performance buffer holds only 200 entries.
- **Rule:** use LoAF **only in the Chromium harness** (it is the instrument that attributes forced layout to a script); on WebKit keep the rAF+timer probe. Feature-detect via `PerformanceObserver.supportedEntryTypes.includes('long-animation-frame')`.
- **Caveat:** the page is dated to its fetch time; WebKitGTK 2.52 support must be checked at runtime (page says no Safari support). **Confidence: high.**

**#10. web.dev: Interaction to Next Paint (INP)** — web.dev / Google — https://web.dev/articles/inp — date not shown — **READ**
- INP uses Event Timing; reports "the longest interaction observed, ignoring outliers"; thresholds at the 75th percentile of field loads: good <= 200 ms, poor > 500 ms; an interaction = input delay + processing + presentation delay; for > 50 interactions one highest is ignored per 50; JS-side the 98th percentile of interactions; scrolling/hovering not observed; **event entries under 104 ms are not reported by default**.
- **Rule:** use the **three-phase decomposition** as the vocabulary for DevKit's interaction budgets (the "keystroke < 16 ms" budget is processing + presentation, input delay is main-thread occupancy by other work like streams/polls) and report the **p98/worst of N interactions**, not the mean. For lab harness use `durationThreshold: 16` (MDN, #13).
- **Caveat:** INP is a field metric for web pages; DevKit has no field data, so use it as methodology. **Confidence: high.**

**#11. web.dev: Optimize INP** — Jeremy Wagner, Philip Walton, Barry Pollard — https://web.dev/articles/optimize-inp — published 2023-05-19, updated 2025-09-02 — **READ**
- Do little work in event callbacks; yield with `setTimeout` between tasks ("yielding indiscriminately beats not yielding"); apply only the visual update needed for the next frame (e.g. rAF + setTimeout), defer the rest; avoid layout thrashing (shown as a red triangle on Recalculate Style/Layout in the Performance panel); **minimise DOM size** and use `content-visibility` for presentation delay; large client-side HTML rendering blocks the next frame; test flows **during load** when the main thread is busiest.
- **Rule:** keystroke handlers update the field synchronously and defer search/derivation (`useDeferredValue` / `startTransition` already in smoothness doc §6.5); include "interact during startup/indexing/stream" scenarios in the harness.
- **Confidence: high.**

**#12. web.dev: Debug performance in the field** — Philip Walton (Google) — https://web.dev/articles/debug-performance-in-the-field — last updated 2024-10-06 — **READ**
- web-vitals attribution build (v3+) gives INP `interactionTarget`, `interactionType`, `interactionTime`, `loadState`, `inputDelay`, `processingDuration`, `presentationDelay`; CLS `largestShiftTarget`; LoAF is mentioned as a supplementary INP signal; lab tools "better for identifying and fixing issues", field "more accurate"; Lighthouse misses interaction problems.
- **Rule:** DevKit has no field channel; if a **local, opt-in** one is ever built (the repo keeps a "no web-vitals library" decision), log the same attribution fields (target, type, load state, three phases) from `PerformanceObserver` so a user bug report can carry them.
- **Caveat:** none of this applies to WebKitGTK unless the observers exist (check `supportedEntryTypes`). **Confidence: high.**

**#13. MDN: PerformanceEventTiming** — MDN contributors — https://developer.mozilla.org/en-US/docs/Web/API/PerformanceEventTiming — date not shown — **READ**
- Default exposed when duration >= **104 ms** (first multiple of 8 above 100 ms; durations rounded to 8 ms); `durationThreshold` minimum **16 ms**; `processingStart - startTime` = input delay; `interactionId` groups events. The fetched extract showed "Baseline 2025, Newly available... Since December 2025 ... works across the latest devices and browser versions"; the per-browser table was **not** in the extract.
- **Rule:** in the harness observe `{type:"event", durationThreshold:16, buffered:true}`; log `supportedEntryTypes` on each target engine and record it in the evidence block; do not assume WebKitGTK 2.52 has it until logged.
- **Caveat:** the Baseline note appears to mean the feature is available in the latest browsers including Safari, but the extract did not give per-engine versions, so this is **unconfirmed for WebKitGTK**. **Confidence: medium.**

**#14. MDN: `PerformanceObserver.supportedEntryTypes`** — MDN — https://developer.mozilla.org/en-US/docs/Web/API/PerformanceObserver/supportedEntryTypes_static — date not shown — **READ**
- Returns the entry types the UA supports (Chrome 129 example lists `event`, `first-input`, `long-animation-frame`, `longtask`, `layout-shift`, `mark`, `measure`, ...); the list "varies per browser and is evolving"; differs between main thread and workers.
- **Rule:** the probe belongs in `perfTrace.ts` startup output (D24) so every pasted log states what the engine can observe.
- **Confidence: high.**

**#15. WebKit Web Inspector Reference: Timelines tab** — WebKit project — https://webkit.org/web-inspector/timelines-tab/ — date not shown — **READ**
- Timelines: Screenshots, Network Requests, **Layout & Rendering** (style invalidations/recalcs, layouts, composites, paints with the JS that triggered them), Media & Animations, **JavaScript & Events** (sampled call trees), CPU, Memory, JavaScript Allocations. Media & Animations, Memory and JS Allocations are **off by default because they can impact page performance**. JS Allocations takes heap snapshots at start, end and every 10 s. **Frames view** groups by frame with JS/Layout/Paint/Other and 30/60 FPS reference lines. JIT deopts, breakpoints, coverage and type profiler are disabled during recording "so the measured performance is more realistic". `console.profile/profileEnd` and `console.takeHeapSnapshot` exist. Sampling interval **not stated**.
- **Rule:** the WebKit-side recipe for the Wails/WKWebView/WebKitGTK app is Timelines -> Layout & Rendering + JavaScript & Events + CPU (leave Media/Memory/Allocations off for timing runs; use them in a separate memory run). Use Frames view for the "frame gap" claims. This is the engine-native counterpart of Chrome's Performance panel.
- **Caveat:** whether the same panel is available through WebKitGTK remote inspector in the DevKit build is not established here (#16). **Confidence: high.**

**#16. WebKitGTK remote Web Inspector** — Igalia/WebKit docs — search results only (https://people.igalia.com/aperez/Documentation/wpe-webkit/remote-inspector.html, https://trac.webkit.org/wiki/RemoteInspectorGTKandWPE) — date n/a — **SNIPPET** (the trac page fetched as a "moved" stub with no content)
- Summary: set `WEBKIT_INSPECTOR_SERVER=<addr:port>`, the app must enable `enable-developer-extras`, connect with an `inspector://` client (MiniBrowser/Epiphany); Chrome is not a client; match client/server WebKit versions.
- **Rule:** if DevKit's GTK build can enable developer extras, that is the path to Timelines on the real engine; the existing doc says only "if there is a Web Inspector". Verify the exact steps in the WebKitGTK manual before documenting. **Confidence: low.**

**#17. Chrome DevTools throttling is not accurate** — Ivan Akulov (performance consultant) — https://3perf.com/blog/chrome-throttling/ — 2026-04-14 (updated 2026-04-15) — **READ**
- DevTools throttles per request not per packet: bandwidth split equally across requests, no TCP slow start/DNS/handshake, no packet loss; a 20 kB image took ~12 s under "3G" in DevTools vs 300-500 ms on WebPageTest; Google's docs call it "a decent approximation" but "not a sufficient model".
- **Rule:** network throttling is irrelevant to DevKit (local Go bridge), but the *lesson* applies to CPU throttling too: use it only for relative comparisons, record it, and prefer a real slow machine for acceptance. For the bridge, add latency in the Go side if a slow-bridge scenario is needed, not browser throttling.
- **Caveat:** about networks, not CPU; one client case for the WebPageTest comparison. **Confidence: high (for network), medium (extension to CPU is my inference).**

**#18. How we sped up Chrome DevTools stack traces by 10x** — Benedikt Meurer (Chrome/V8 engineer) — https://developer.chrome.com/blog/faster-stack-traces/ — last updated 2021-05-06 — **READ**
- "After all, observing something changes it"; DevTools overhead "will never" be zero: breakpoints, stack traces, and capturing a performance trace all affect speed; a real report had **5-10x** slowdown with DevTools open; ~90 % of renderer time was symbolizing stack frames, reduced to ~15 % after the first round of fixes.
- **Rule:** the evidence template requires stating which instrumentation was active, identical for BASE and HEAD; headline timings come from the **lightest** instrumentation (marks/counters), traces are for explanation. Re-measure after a trace-driven fix **without** the trace.
- **Caveat:** the 5-10x case was a specific bug, now fixed; do not read it as typical overhead. **Confidence: high.**

**#19. The black cat of microbenchmarks** — mraleph (V8 engineer, author of IRHydra; page shows no name) — https://mrale.ph/blog/2014/02/23/the-black-cat-of-microbenchmarks.html — 2014-02-23 — **READ**
- A benchmarked operation can be dead-code-eliminated entirely; inlined bodies disappear while loop-invariant guards are hoisted; wrapping your own loop inside Benchmark.js's loop is nonsensical; without checking, you may be "measuring weather instead of performance"; inspect the optimizer's output; sometimes algorithmic work is a better use of time.
- **Rule:** every microbenchmark (reducer, markdown, `groupTurns`) must **consume its result** and fail loudly if timings are implausibly small; prefer macro benchmarks (real reducer over the committed fixture) over micro-loops; prefer counters (identity changes, allocations avoided) for claims about "work".
- **Caveat:** V8-specific (JavaScriptCore has different JIT tiers; bun test runs JSC). Warm-up is not discussed in the article. **Confidence: high (principle), medium (transfer to JSC).**

**#20. What is perf.rust-lang.org measuring and why is `instructions:u` the default?** — Nicholas Nethercote (Rust compiler performance lead) — https://internals.rust-lang.org/t/what-is-perf-rust-lang-org-measuring-and-why-is-instructions-u-the-default/9815 — 2019-04-14 — **READ**
- Wall time is "the ultimate metric" but "has high variance, which makes it very hard to use in practice"; small changes under ~1 % are hard to detect with wall time; in his no-op-change example wall-time outliers were -8.9 %/+9.7 % while instruction counts had outliers +1.3 %/-1.1 % with most between -0.2 % and +0.2 %; changes as small as 0.3 % are reliably detectable with instruction counts; counts can diverge from time (cache misses, branch mispredicts) but "significant divergence is extremely rare in practice". A dissenter prefers cycles.
- **Rule:** the JS analogue of "instruction count" is a **work counter**: renders, reducer ops, allocations, nodes created, bytes parsed. Gate CI on those and keep wall time informational. (Instruction counting for JS is not a thing in bun/JSC to my knowledge — not verified.)
- **Caveat:** compiler workloads are deterministic, single-threaded batch jobs; UI workloads involve the compositor, GPU and OS scheduling, which counters do not capture (hence the second layer of real-machine frame measurement). **Confidence: high.**

**#21. CI GPU tests post** — Dave Snider (designer/developer; personal blog) — https://davesnider.com/posts/gputests — 2026-02-23 — **READ** (practitioner)
- Headless Chromium by default used **SwiftShader** (software) even on a GPU runner; headed under xvfb used llvmpipe (still software); root cause was missing NVIDIA kernel modules; before: 5+ min runs, ~30 s 3D canvas load per test; after: <1 s canvas load, 1.4 min suite; fallback llvmpipe 1.6 min; GPU runners cost ~3-4x per minute.
- **Rule:** Chromium CI timings from a headless runner without a GPU are a **software-rendering** measurement; label them `rendering=software` and never compare them to a desktop GPU run. DevKit's own WebKitGTK harness ran "on a headless compositor using the GPU" — keep that in the evidence block.
- **Caveat:** one practitioner anecdote with a 3D canvas workload; DOM-only apps are less sensitive. **Confidence: medium.**

**#22. Playwright headless vs headed / Chromium headless rendering** — mixed (Chromium commit, Playwright issue, vendor blogs) — https://github.com/microsoft/playwright/issues/33566 , https://chromium.googlesource.com/chromium/src.git/+/d99a9664c873fc38e226e33bc92da974fb852da3 — dates n/a — **SNIPPET**
- Summaries: headless moved to SwiftShader on most platforms (not macOS); since Playwright 1.57 headed = full Chrome, headless = chrome-headless-shell (separate implementations, GPU/font behaviour can differ); Playwright says new headless mode "can be faster or slower depending on the suite".
- **Rule:** pin the browser binary and mode in the harness and print `chrome://gpu`-equivalent info; do not mix headed/headless runs in one comparison. **Confidence: low (search summaries; verify before relying).**

### B. Perf regression testing

**#23. Keeping Figma Fast** — Figma engineering blog (author not shown in extract) — https://www.figma.com/blog/keeping-figma-fast/ — system went live Oct 2022; post date not in extract — **READ**
- Any guardrail check must finish in **under 10 minutes**; dozens of stress scenarios in parallel; runs in **GPU-enabled VMs with headless Chromium on every PR**; VM noise handled by a **20 % pass margin** that "catches only large regressions such as O(n) -> O(n^2)"; physical laptop lab (older MacBooks, old Windows laptops, Chromebooks) on a slower queue used for **bisecting** subtle regressions, day-over-day comparison, hot-path changes; metrics include FPS at **p50 and p90**; every run produces an HTML report and optional CPU profile; a p90 regression on specific machines was found by a manual run of a few tests within about 10 runs; **disabled tests produce no alert**.
- **Rule:** two tiers — (1) PR tier: deterministic counters + frame-stall scenario with a **wide** time margin (start at 20-25 % on CI hardware); (2) lab tier: named reference machines (one WebKitGTK Linux, one macOS, one Windows) run nightly/on demand for bisect. Cap the PR tier at 10 min; track "disabled/skipped perf tests" as a visible count (a disabled test is silent).
- **Caveat:** Figma's workloads are canvas/WebAssembly heavy; the specific 20 % was chosen for their VM noise, calibrate with a BASE-vs-BASE experiment (see #27). **Confidence: high.**

**#24. Mozilla perf docs and sheriffing FAQ** — Mozilla Performance team — https://wiki.mozilla.org/Performance_sheriffing/Alert_FAQ , https://firefox-source-docs.mozilla.org/testing/perfdocs/perftest-in-a-nutshell.html , plus SNIPPET of https://m.wiki.mozilla.org/Special:MobileDiff/1216062 and arXiv 2606.18377 — dates n/a — **READ** (Alert FAQ, perftest docs), **SNIPPET** (the "12 future points / t-test" details)
- READ: default alert threshold **2 %**, per-test overrides (Dromaeo 5 %); noisy tests may be **removed**; some tests treat higher as better (a "reverse" list) so direction must be recorded per metric; verify patches via **try runs** because CI hardware differs from local; small (~2-3 %) changes are hard to judge; sheriffs run retriggers for small/noisy alerts; current compare tool "PerfCompare" (compare with a base, up to 3 revisions) and the alerts view has a **confidence** value and flags multimodal distributions.
- SNIPPET (needs verification): alert = original point + 12 future points, median of previous 12 vs future 12, t-test confidence; Perfherder uses t-test-based change-point detection (arXiv 2026).
- **Rule:** store per-metric **direction** and **threshold** with each benchmark; support **retrigger** (re-run N more) as a first-class CI action before declaring a regression; mark chronically noisy scenarios "informational" rather than letting them gate; record bimodal results (cold/warm GPU) explicitly.
- **Caveat:** Firefox infrastructure is far larger than DevKit's; only the *process* transfers. **Confidence: medium-high.**

**#25. Speedometer 3.0** — Apple WebKit team (joint Blink/Gecko/WebKit project) — https://www.webkit.org/blog/15131/speedometer-3-0-the-best-way-yet-to-measure-browser-performance — 2024-03 (per search results) — **READ**
- Each simulated action's **sync time** (script in a rAF callback) **plus async time** (a second rAF schedules a zero-delay timer; time to that timer, which captures rendering because rendering happens after rAF and before the next timer) is summed; geometric mean across workloads; workloads include TodoMVC (6 simple-DOM + 6 complex-DOM variants), Next.js/Nuxt news, canvas/SVG charts, CodeMirror and TipTap editors; "complex DOM" versions embed the app in deeply nested DOM and many CSS rules. The article gives no iteration count/statistics.
- **Rule:** measure an interaction as **handler time + time until the frame after next rAF/timer** — this is precisely what DevKit's frameStall rAF/timer probe can do; use "simple vs complex DOM" fixtures (the app under a realistic shell, not a bare component) because the same component costs more inside a heavy DOM.
- **Caveat:** the article is inconsistent about the final score formula. **Confidence: high.**

**#26. Chromium: addressing performance regressions / bisects** — Chromium docs — https://chromium.googlesource.com/chromium/src/+/refs/heads/main/docs/speed/addressing_performance_regressions.md , https://chromium.googlesource.com/chromium/src/+/da170c2267b2201954efe1c8fa5d2d89ab09f7bf/docs/speed/bisects.md — dates n/a — **SNIPPET** (both fetches failed: 503)
- Summary: Pinpoint bisects a CL range because perf tests are too slow to run on every revision; graph per commit of **median with min/max bars**; story filter to one test case to cut time; advise running a **perf try job** on lab machines matching the waterfall.
- **Rule:** adopt the same graph shape (median + min/max per commit) in any perf dashboard DevKit builds; keep a "story filter" (run one scenario) flag in the harness. **Confidence: low** until read in full.

**#27. Reassure — React render regression testing** — Callstack (docs + README) — https://oss.callstack.com/reassure/docs/introduction , https://github.com/callstack/reassure — dates n/a — **READ**
- Measures **render duration and render count** via `React.Profiler`; default **10 runs + 1 warm-up** (discarded), outliers removed by default; compares current vs a baseline run on the **same machine one after another**; "statistical analysis" decides significance (method not named); report buckets: significant duration changes, non-significant, render-count changes, added/removed scenarios; **stability check**: same code twice should give ~0 %, random changes < 5 % normal, >= 10 % too high; raising runs to 20/50/100 is a last resort; "CI agent should not run other work during measurement"; mock I/O.
- **Rule:** adopt this structure wholesale for D3 (render counts) and as the model for the evidence template: baseline run + current run in one CI job, warm-up, outlier removal, a stability job that runs BASE twice, report render-count changes as **hard** failures and duration changes as **soft**.
- **Caveat:** Reassure targets Jest + RTL; DevKit uses bun test (the Profiler part is reproducible by hand). Its significance test is unspecified. **Confidence: high.**

**#28. Turbopack benchmark suite + CodSpeed on Next.js** — Vercel (turbopack-bench README) — https://github.com/vercel/next.js/blob/canary/turbopack/crates/turbopack-bench/README.md ; CodSpeed reports SNIPPET https://codspeed.io/vercel/next.js — dates n/a — README **READ**, CodSpeed **SNIPPET**
- README: scenarios run in a **real headless browser**; default 1 000 modules, scalable via env (`TURBOPACK_BENCH_COUNTS=5000`); measures startup (cold start -> rendered), hydration (-> interactive), HMR-to-eval and **HMR-to-commit measured with a `useEffect` hook**; each scenario tries to capture "**at least 10 samples**"; published numbers come from "a controlled environment"; no statistics or CI thresholds described. CodSpeed (snippet): per-PR comparison vs base with regressions flagged.
- **Rule:** parameterise fixtures by size (1k/5k) so complexity regressions show up as a slope, not a point; use a `useEffect`-based "committed" marker only as a coarse end-of-update signal (effect timing is not paint — see #74).
- **Caveat:** HMR-to-commit via `useEffect` is not the paint time. **Confidence: medium.**

**#29. Bench frameworks: tinybench, mitata, Vitest bench, Bun** —
- *tinybench README* — https://github.com/tinylibs/tinybench — **READ**: `warmupTime/warmupIterations`; reports average, median (p50), `rme` (relative margin of error), `mad` (median absolute deviation), sample count; default timestamp `performance.now()`; `subtractTimerOverhead` exists but clamping at zero biases results and calibration returns 0 if the timer is too coarse; warnings `zero-dominated`, `low-distinct`, `zero-mad` flag saturated timers; async overhead is not calibrated (sub-microsecond async samples stay over-measured). (README extract did not show p75/p99.)
- *mitata README* — https://github.com/evanwashere/mitata — **READ**: runs GC once after warmup by default (`.gc('once'|'inner'|false)`, needs Bun or `--expose-gc`); flags likely dead-code-eliminated benchmarks with "!" and provides `do_not_optimize(value)`; computes parameters **outside** the loop to avoid loop-invariant code motion (no guarantee on recompute order/count); output includes avg/min/max/p75/p99; "use dedicated hardware"; concurrent benchmarks have more variance; hardware counters need `perf_event_paranoid <= 2` and may not exist in VMs; close Instruments on macOS.
- *Vitest benchmarking* — https://vitest.dev/guide/features.html#benchmarking — **READ**: "via Tinybench", `bench.compare(...)` runs several benchmarks and shows them side by side; the page documents no options, baseline semantics or caveats.
- *Bun benchmarking* — https://bun.sh/docs/project/benchmarking — **READ**: recommends **mitata** for microbenchmarks, `hyperfine` for scripts/CLI; `Bun.gc(true)` forces sync GC ("objects not freed immediately is normal, never freed is a leak"); `bun --cpu-prof` writes `.cpuprofile`; `--heap-prof` V8-format snapshot; `Bun.generateHeapSnapshot()` can be read by Safari/WebKit GTK DevTools; the page does not mention `bun test` benchmark support.
- **Rule:** DevKit tests run on **JavaScriptCore via bun**, the same engine family as WKWebView/WebKitGTK (not V8 in Chromium/WebView2) — so bun-based micro/macro benchmarks are **more** representative of Safari/WebKit than of Chromium, but not of the web view's layout/paint. Use mitata (or tinybench) with `do_not_optimize`, GC-after-warm-up, report p50 + p75/p99 or rme/MAD; reject results where the tool prints the DCE warning or `zero-dominated`.
- **Caveat:** none of these tools measure layout/paint; they are for the JS side only. **Confidence: high.**

**#30. Perfetto** — Google/Android — https://perfetto.dev/docs/ — date n/a — **READ**
- Open-source tracing suite; **default tracing system of Chromium**; opens Chrome JSON traces, Firefox profiler JSON, `perf`, macOS Instruments traces; has a **SQL trace-processor library** and UI query engine for programmatic analysis.
- **Rule:** when a Chromium trace is saved as evidence, analyse it with SQL (long frames, `RunTask` > 50 ms, layout counts) instead of eyeballing screenshots, and keep the .json trace as a CI artifact. For WebKit there is no equivalent found; export Timelines (⌘S) per #15.
- **Caveat:** not WebKit-aware. **Confidence: high.**

**#31. size-limit** — Andrey Sitnik (ai) — https://github.com/ai/size-limit — date n/a — **READ**
- Budgets on size **and time**: `time` plugin estimates CPU throttle by comparing the machine to a low-priced Android phone and runs headless Chrome to measure compile+execute; "calculates the real cost" including dependencies and polyfills; recommends limits of **current size + 25 %** for apps and ~1 kB for small libs; GitHub Action posts size changes on PRs; the README warns time measurements "depend on available resources and might be unstable".
- **Rule:** DevKit's `check-performance-budget.mjs` is already this for bytes; keep **bytes** as a hard gate (stable) and any "execute time of the initial chunk" as informational only. Use the "+25 % over measured" convention when setting new per-chunk ceilings so they do not become silent slack.
- **Caveat:** repo page 403'd on npm; README read from GitHub. **Confidence: high (bytes), medium (time).**

### C. Startup and bundle

**#32. The cost of JavaScript in 2019** — V8 team blog (authors not shown in extract) — https://v8.dev/blog/cost-of-javascript-2019 — 2019 — **READ**
- "The dominant costs of processing scripts are now download and CPU execution time"; JS processing is 10-30 % of V8 time during page load on a high-end desktop; V8 streams parse/compile on a worker thread once >= 30 kB seen; code caching applies if the first two visits are within **72 hours**; external scripts eligible at >= 1 kB; split bundles > ~50-100 kB; `JSON.parse` for large (~10 kB+) data objects is 1.7x faster than a literal in V8 and faster in all major engines; "not all engines implement script streaming".
- **Rule:** for DevKit (local files, no download), startup cost = **execution of module top-level code + parse** — so measure `performance.now()` at first meaningful render and the sum of module-evaluation work, and keep top-level side effects out of lazily-loaded modules (registries, `createContext`, heavy `new Map()` seeding). Convert large static JSON-like tables (icon/language maps) to `JSON.parse` strings only after measuring on JSC.
- **Caveat:** V8/Chrome-specific; 2019 numbers on phones. **Confidence: medium (transfer to JSC).**

**#33. Code caching for JavaScript developers** — V8 team (blog) — https://v8.dev/blog/code-caching-for-devs — date n/a — **READ**
- Cold/warm/hot runs; cache is produced after execution and holds only functions compiled by then; a 200 response clears the cache, a 304 keeps it; changing a URL creates a cold entry; split stable library code from changing app code (caching is per script); scripts < 1 KiB not cached; inline scripts' caches depend on the whole document; verify via `chrome://tracing` `v8` category (`v8.compile`, `cacheProduceOptions`/`cacheConsumeOptions`, sizes in bytes).
- **Rule:** chunk **naming/hashing** matters for repeat-launch speed in a V8 web view (WebView2): keep vendor chunks (React, Radix, TanStack) stable across builds with content-hash splitting so an app update only invalidates changed chunks; but this advice is documented only for Chrome/V8 — treat it as WebView2-only until JSC's behaviour is verified (#34).
- **Caveat:** the page does not state the 72-hour figure (that came from #32). **Confidence: high (V8), n/a (JSC).**

**#34. A New Bytecode Format for JavaScriptCore** — Tadeu Zagallo (WebKit/JSC engineer) — https://webkit.org/blog/9329/a-new-bytecode-format-for-javascriptcore/ — 2019-06-21 — **READ**
- New format uses ~**50 %** less memory for bytecode, ~**10 %** overall for JS-heavy sites (bytecode was **20 %** of memory); designed to be "easily cacheable on disk"; "the caching API for the new bytecode is already underway".
- **Rule:** do **not** assume a V8-style persistent code cache for WKWebView/WebKitGTK; the 2019 post says only that caching was *underway*. Treat every cold launch as cold: the startup budget protects parse+execute, not a cache hit.
- **Caveat:** the post says nothing about whether/when on-disk caching shipped or applies to a custom WKWebView/WebKitGTK embedding (a 2009 mailing-list snippet said JSC found codegen < 2 % of runtime — SNIPPET, not read). **Confidence: low for "no cache today", high for what the post says.**

**#35. Case study: Analyzing Notion app performance** — Ivan Akulov — https://3perf.com/blog/notion/ — 2020-05-19 — **READ**
- On a Nexus 5: executing vendor+app bundles took 4.9 s of which compilation 1.6 s (~32 %); initialisation ran **1 100+ modules** (~3.3 s); coverage showed **39 %** of the vendor bundle and **61 %** of the app bundle unused after first render; large pieces: moment with all locales 227 KB, react-dom 111 KB, libphonenumber metadata 81 KB, lodash 71 KB; core-js bundled three times; nine API requests before render (~10 % estimated gain by removing); deferring third-party scripts rendered content ~1 s earlier. Estimates are "very rough"; the author did not change Notion.
- **Rule:** run **Coverage** (Chrome DevTools "start coverage", or WebKit equivalent if any) on the production build at first-usable-frame and record unused-JS % per chunk in the budget report; look for **duplicate packages** and locale/metadata bundles; sequential bridge calls before first paint are the DevKit analogue of the nine API calls (existing doc 8.2 measured them parallel: keep that measurement in the evidence).
- **Caveat:** 2020, mobile CPU; method (coverage) is the durable part. **Confidence: high.**

**#36. Speeding up the JavaScript ecosystem - the barrel file debacle** — Marvin Hagemeister (Preact core team) — https://marvinh.dev/blog/speeding-up-javascript-ecosystem-part-7/ — 2023-10-08 — **READ**
- Barrel files chain-load nearly every file; module loading is not lazy (all imported modules load, even unused); synthetic empty-module project on an M1 Air: 500 modules 0.15 s, 1 000: 0.31 s, 10 000: 3.12 s, 25 000: 16.81 s, 50 000: 48.44 s; Jest rebuilds the module graph per test file so the cost multiplies (100 files x 4 in parallel: 10 000 modules -> 1:18 min); recommendation: remove barrels, "60-80 % faster" tasks; a few barrels are fine, a barrel in every folder is the problem. The article does not discuss production tree-shaking or icon libraries.
- **Rule:** DevKit tests run each file in its own global (`bun test --isolate`), so module-graph size is paid **per test file**; add a test-time metric "modules loaded by `bun test` for file X" to the evidence for dev-experience changes and ratchet barrel files (`index.ts` that only re-exports) in `src/`.
- **Caveat:** synthetic numbers; production bundlers tree-shake (my inference; see #38/#45c). **Confidence: high (dev/test cost), n/a (prod).**

**#37. Vite: Performance guide** — Vite docs — https://vite.dev/guide/performance — date n/a — **READ**
- Avoid barrel files (import individual APIs); use explicit import extensions (one example needed 6 filesystem checks); import large SVGs as strings/URLs, not components; `server.warmup` for hot files; avoid long work in plugin `buildStart/config/configResolved`; browser extensions and "Disable Cache" slow dev; official plugins do less work in dev (regex vs full parse); `vite --profile` + speedscope, `vite --debug plugin-transform`.
- **Rule:** the existing "measure on production build, not Vite dev" holds doubly: dev behaviour is explicitly different (plugins do less). For build-time questions use `vite build --profile`.
- **Confidence: high.**

**#38. How we optimized package imports in Next.js** — Shu Ding (Vercel) — https://vercel.com/blog/how-we-optimized-package-imports-in-next-js — 2023-10-13 — **READ**
- `lucide-react` 1 583 -> 333 modules (dev boot 5.8 s -> 3 s); `@material-ui/icons` 11 738 -> 632 modules (10.2 s -> 2.9 s); `@mui/material` 2 225 -> 735 (7.1 -> 2.9 s); `next build` ~28 % faster on a page using lucide + headlessui; local server start ~10 % faster; cold starts up to 40 % (with other changes); popular React packages take **200-800 ms just to import**; tree-shaking is a bundler feature and fails if a library is externalised; depends on `sideEffects` in package.json.
- **Rule:** DevKit imports lucide by name from the barrel in 286 files; in a Vite **production** build that is tree-shaken, but in **dev and `bun test` each file loads the whole lucide barrel (1 583 modules)** — measure how long `import "lucide-react"` takes under bun (`bun --cpu-prof`) before deciding; if material, a path-import alias or a local icon registry (`components/icons.ts` re-exporting ~100 used icons) removes the cost. Never add `import * as Icons`/dynamic icon lookup by string without a bounded map (it defeats tree-shaking — standard consequence, **not stated** in the fetched lucide page).
- **Caveat:** Next-specific mechanism; the module counts are for a specific lucide version. **Confidence: high (numbers), medium (applicability to bun/Vite).**

**#39. Shiki: bundles** — Shiki docs — https://shiki.style/guide/bundles — date n/a — **READ**
- `shiki/bundle/full` ~6.4 MB minified (~1.2 MB gzip), `web` ~3.8 MB (~695 KB gzip), including async chunks; fine-grained: `shiki/core` with only chosen themes/languages via dynamic imports; "the main entry lazy-loads grammars" but unused grammar files can still enlarge dist when bundled for browsers/workers; WASM Oniguruma engine inlined as base64 (sizes of WASM vs the JS regex engine not given); recommends fine-grained bundles for performance and memory.
- **Rule:** the highlighter in the transcript/notes must be created from `shiki/core` with an explicit language allow-list loaded on demand, behind a lazy boundary; detector D14 asserts shiki is not in the initial graph and a per-chunk ceiling exists for the highlighter chunk. Measure highlight time on the standard fixture (the existing doc lists "syntax highlight many blocks" as heavy work).
- **Caveat:** whether DevKit already does this is outside this research; sizes are for the current Shiki. **Confidence: high.**

**#40. Motion: Reduce bundle size** — Motion docs — https://motion.dev/docs/react-reduce-bundle-size — date n/a — **READ**
- `motion` component "can't be tree-shaken below **43 kB**"; `LazyMotion` + `m` brings initial render to just over **5 kB**; `domAnimation` adds 24 kB (animations, variants, exit, tap/hover/focus), `domMax` 38 kB (adds drag/pan and layout animations); `strict` mode throws if a `motion` component is used inside; features can be loaded async via dynamic import; sizes from Rollup builds.
- **Rule:** DevKit uses `LazyMotion` in 2 files but imports `motion` in others (e.g. `VaultStage.tsx` imports `motion` — in the initial path for the vault gate). Decide per file: if the vault stage is in the initial graph, the 43 kB is on the startup path; the shared-element `layoutId`/layout animations need `domMax` (38 kB) loaded async. Enable `strict` on `LazyMotion` providers, add D16.
- **Caveat:** whether motion from different files ends up in one chunk is a build fact, check the manifest. **Confidence: high.**

**#41. modulepreload** — web.dev (Google; extract shows 2024-11-23 update) — https://web.dev/articles/modulepreload — 2024-11-23 — **READ**
- `modulepreload` fetches a module and lets the engine parse and compile it ahead of execution (plain `preload` just caches); a browser must wait for a module to load to discover its dependencies, so nested imports are sequential round trips; spec allows but does not require recursive dependency fetching; recommended approach: preload the module and a **flat list** of its dependencies; support: Chrome 66, Edge 79, Firefox 115, Safari 17; module performance "still being improved", bundle into chunks meanwhile.
- **Rule:** Vite already emits `modulepreload` for the entry's static dependency chunks; make sure **shell-route** lazy chunks that are always needed after unlock (e.g. the workbench/title bar) are in that list or are preloaded at idle after first paint; add D15 (dependency depth).
- **Caveat:** local-file loading (custom scheme) changes the cost model (no network); the "waterfall" cost is then disk/IPC latency, not RTT — measure before investing. **Confidence: high.**

**#42. React Flow: Performance** — xyflow docs — https://reactflow.dev/learn/advanced-use/performance — date n/a — **READ**
- Main cause of slowness: unnecessary re-renders from frequent state updates (node dragging); memoize custom node/edge components (or define outside), wrap callbacks in `useCallback`, objects/arrays (e.g. `defaultEdgeOptions`, `snapGrid`) in `useMemo`; **do not read the full `nodes` array in components** (re-renders on any node change) — keep derived data (e.g. selected IDs) in a separate store field; collapse large trees (toggle `hidden`); simplify CSS — "animations, shadows, and gradients" are named as likely to hurt; the page did not mention `onlyRenderVisibleElements`.
- **Rule:** for the CodeGraph Flow/outline canvases: node/edge types are module-level constants; no component subscribes to `nodes`; ban `box-shadow` on nodes at high count (ratchet D11); keep CSS animation off nodes.
- **Confidence: high.**

**#43. Best practices for fonts** — Katie Hempenius & Barry Pollard (web.dev) — https://web.dev/articles/font-best-practices — updated 2022-10-04 — **READ**
- `preload` helps for critical fonts but competes with other resources and ignores `unicode-range` (use one format); WOFF2 ~30 % smaller than WOFF; variable fonts are usually larger than one single-style file and pay off with many weights; `font-display`: `optional` gives no shift but maybe no web font, `swap` can shift, `block` delays text; `size-adjust` reduces layout shift; icon fonts shift more than SVG; Latin fonts have ~100-1 000 glyphs, CJK > 10 000.
- **Rule:** DevKit bundles Geist/Geist Mono/Inter via fontsource and loads them synchronously before first render (smoothness doc 3.6): add D22 (woff2 only, `font-display` declared, total font bytes in the budget, subset where Vietnamese/Latin only are needed — the docs are in Vietnamese, so keep `vietnamese` subsets). A variable font may be *larger* than the two weights actually used: compare sizes.
- **Caveat:** web-delivery advice; local fonts have no network cost, so the only costs are parse/decode and layout shift. **Confidence: medium.**

**#44. WebKit Bugzilla 240865: [GTK] Webpages completely slow down when CSS blur filter is in use** — reporter Caden Mitchell, with WebKitGTK engineer comments — https://www2.webkit.org/show_bug.cgi?id=240865 — filed 2022-05-24; a 2026 comment tests WebKitGTK 2.54.1 — **READ**
- Over ten seconds per frame for a 60 px blur in 2022; CPU blur path slow vs macOS Accelerate; a 2026 test: **8 blurred boxes median 184 ms between frames vs 16 ms** with no filter; workarounds: force GPU compositing (`will-change: transform` / `translateZ(0)`), a 2026 tester says `will-change: filter` avoids the cost; status NEW (open); fixes reduced but did not remove the cost.
- **Rule:** `filter: blur()` (and probably large-radius shadows) on WebKitGTK is a documented hazard — add `filter: blur` to the paint-cost ratchet (D11) and require a measurement on WebKitGTK for any use; `backdrop-filter` is already ratcheted.
- **Caveat:** about `filter: blur`, not `backdrop-filter`; a search for a primary WebKit write-up on `backdrop-filter` cost found only practitioner posts (SNIPPET: "re-composites everything behind the element every frame", unverified). **Confidence: high (bug facts), low (generalisation).**

**#45a. CodeMirror 6 `@codemirror/language-data`** — CodeMirror forum/search summaries — https://discuss.codemirror.net/t/module-name-in-language-data/8672 — date n/a — **SNIPPET**
- Summary: a `languages` array of `LanguageDescription` objects, each with a dynamic `import()` loader; `matchFilename`/`matchLanguageName` find a description before loading; the descriptions carry default config.
- **Rule:** use `LanguageDescription.load()` on demand and verify in the manifest that no `@codemirror/lang-*` is in the initial graph (D14). **Confidence: low (not read at primary source; verify at codemirror.net).**

**#45b. Mermaid usage docs** — Mermaid — https://mermaid.js.org/config/usage.html — date n/a — **READ** (limited)
- `startOnLoad:false` + explicit `mermaid.run()/render(id, def)`; `mermaid.parse()` validates without rendering; a Tiny build is ~half the full size and omits Mindmap, Architecture, KaTeX, lazy loading and ELK layout.
- **Rule:** Mermaid stays behind a dynamic import (already the case per AGENTS.md) and renders one at a time; `parse()` first to avoid rendering invalid sources. The "Tiny" build is an option **if** its omitted diagram types are not needed (not evaluated).
- **Confidence: medium.**

**#45c. Lucide React guide** — Lucide — https://lucide.dev/guide/packages/lucide-react — date n/a — **READ** (limited)
- States the package is "tree-shakable — only the icons you import are included in your final bundle"; no statement about dynamic imports or sizes on that page.
- **Rule:** supports the inference in #38 that production bundles are fine with named imports; verify in the build output rather than from docs. **Confidence: medium.**

### D. Memory and GC

**#46. MDN: WeakRef** — MDN (quoting the TC39 proposal) — https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/WeakRef — date n/a — **READ**
- "Correct use of `WeakRef` takes careful thought, and it's best avoided if possible"; engines differ and vary by version and situation; cleanup "may happen much later than expected, or not at all"; sources of variability include generational/incremental/concurrent GC and "engine may hold references to things that look unreachable (e.g. in closures, inline caches)"; a target is not reclaimed until the end of the current job; `deref()` may never return `undefined`.
- **Rule:** no `WeakRef`/`FinalizationRegistry` in DevKit for correctness or for bounding memory (0 uses today: keep a ratchet at 0 or require a reasoned comment). A WeakMap keyed by DOM nodes/objects for **metadata** is fine because it holds no value alive past its key.
- **Caveat:** says nothing about WeakMap. **Confidence: high.**

**#47. MDN: AbortSignal** — MDN — https://developer.mozilla.org/en-US/docs/Web/API/AbortSignal — date n/a — **READ**
- An `AbortController` signal can be GC'd when signal+controller are unreachable even with abort listeners; signals from `AbortSignal.any()`/`timeout()` are kept alive by pending abort listeners and a long-lived signal **retains the listener and everything it references**; `{once:true}` removes a listener only when the event fires; remove abort listeners in `finally`; don't create combined signals when the global one suffices.
- **Rule:** in bridge calls and agent streams that combine a global/session signal with a per-call signal, always remove the abort listener in `finally` (and don't use `AbortSignal.any` on a never-aborting session signal per call): this is the exact leak shape for long sessions (D17). The page did **not** document the `signal` option of `addEventListener` (do not cite it from here).
- **Confidence: high.**

**#48. Memory leaks: the forgotten side of web performance** — Nolan Lawson (browser/web performance engineer; author of fuite) — https://nolanlawson.com/2022/01/05/memory-leaks-the-forgotten-side-of-web-performance/ — 2022-01-05 (from URL path) — **READ**
- Typical leak sources: `addEventListener` without removal (including on `window`, `document`, `body`, persistent header/footer), `setInterval` never cleared, DOM nodes added and not removed, arrays/collections that grow (one entry per interaction); public SPAs leaking up to **186 MB per interaction** (his example: 5 MB); a ~1 kB leak per interaction from a routing library may not be worth fixing; leaks compound when a bigger object attaches to an existing leak (5 -> 50 MB); users mask leaks by refreshing; heap snapshots are "often a mess" and tooling "not really up to the task"; WeakMap/circular-reference and DevTools-console-itself caveats; **fuite** (Puppeteer-based) detects leaking listeners, DOM nodes and collections, later versions give stack traces for `Array.push`; fixes are often one line but finding them can take days; "prioritize leaks that grow over time". WeakRef is not discussed in the article.
- **Rule:** a leak test is **repeat an action N times and assert a counter returns to baseline** (listeners, nodes, store entries, cache sizes); do it for each module's open/close, tab open/close, session open/close, workspace switch. Prioritise by growth rate per interaction x frequency (a keystroke leak beats a once-a-day leak). Run `fuite` (or equivalent) against the production server-mode build manually before releases (server mode works headless per AGENTS.md; Agent Hub is unavailable there).
- **Caveat:** web-page SPAs; DevKit is a long-lived desktop shell where a day-long session is normal — raises the importance of 1-kB/interaction leaks. **Confidence: high.**

**#49. Chrome DevTools: Heap snapshots** — Chrome DevTools docs — https://developer.chrome.com/docs/devtools/memory-problems/heap-snapshots — date n/a — **READ**
- Taking a snapshot starts with GC; views Summary / Comparison / Containment / Statistics; **shallow** vs **retained** size; **distance** = shortest path to root; leak workflow: snapshot, do operation, do the reverse (repeat a few times), snapshot, **Comparison** view — objects remaining after the reverse operation suggest a leak; "Objects retained by detached nodes" filter finds detached-DOM retention; caveats: native getters/numbers not captured, C++ objects show as `InternalNode`, `(compiled code)` changes without app changes.
- **Rule:** the manual pre-release leak workflow is exactly this three-step comparison, performed on the production (profiling) build with named scenarios; WebKit's JavaScript Allocations timeline offers snapshot comparison and shortest path to root (#15) for the real engine.
- **Caveat:** Edge's "Detached elements" tool page 404'd (not read). **Confidence: high.**

### E. Anti-pattern catalogue (each with detection)

**#50. React: `useSyncExternalStore`** — React team — https://react.dev/reference/react/useSyncExternalStore — date n/a — **READ**
- `getSnapshot` must return an immutable, cached value (repeated calls must be `Object.is`-equal while the store is unchanged); returning a fresh object each call -> "The result of getSnapshot should be cached" and an infinite loop; a non-stable `subscribe` function causes **re-subscription on every render**; during a Transition React calls `getSnapshot` a second time before applying DOM changes and, if it differs, restarts the update **as blocking**.
- **Rule:** D7. For any custom store hook (3 files use uSES): a unit test that calls `getSnapshot()` twice, and `subscribe` defined at module scope or `useCallback`ed. For Zustand, the same failure mode exists with selectors that build objects; the Zustand doc fetch failed (404), so cite the React doc, not Zustand's.
- **Confidence: high.**

**#51. Blogged Answers: A (Mostly) Complete Guide to React Rendering Behavior** — Mark Erikson (Redux maintainer) — https://blog.isquaredsoftware.com/2020/05/blogged-answers-a-mostly-complete-guide-to-react-rendering-behavior/ — 2020-05-17, updated Oct 2022 (React 18) — **READ**
- New context `value` forces **every consumer** to re-render and consumers cannot skip it; a parent render recursively renders children regardless of props; `memo` uses shallow equality, so new object/function props defeat it; Profiler shows what rendered and why; "Rendering is not always bad — it is how React determines whether DOM changes are needed. Wasted renders still add up."
- **Rule:** (a) context value churn is the one that cannot be fixed at the consumer — D2; (b) default stance: do not chase renders that produce no DOM change unless the profile shows they are expensive (existing "forbidden: blanket memo" rule); (c) with React Compiler, object/function props are auto-memoised only in compiled components (D1).
- **Caveat:** pre-Compiler article. **Confidence: high.**

**#51b. ESLint rule `react/jsx-no-constructed-context-values`** — eslint-plugin-react maintainers — https://github.com/jsx-eslint/eslint-plugin-react/blob/master/docs/rules/jsx-no-constructed-context-values.md — date n/a — **READ**
- Flags non-stable values (inline objects, functions defined in the component) as a `Context.Provider` `value`; when `value` changes React re-renders the context's children and all consumers, and spends time scanning the tree for consumers; fix with `useMemo`/`useCallback`; primitives are accepted.
- **Rule:** enable it (or an equivalent test) for the 17 `createContext` files; with React Compiler on, a compiled provider's object literal is memoised automatically, so the lint matters most where the compiler bails out (#55).
- **Caveat:** the lint is syntactic (it does not see a value rebuilt in a custom hook). **Confidence: high.**

**#52. Fix the slow render before you fix the re-render** — Kent C. Dodds — https://kentcdodds.com/blog/fix-the-slow-render-before-you-fix-the-re-render — 2019-09-09 — **READ**
- Render = call the component, reconcile = diff, commit = DOM; commit is typically the slowest and "the slow part of the DOM is layout"; unnecessary re-renders are "usually cheap"; if the app freezes the cause is more likely a **slow render**; reducing re-renders while leaving a slow render tends to complicate code; workflow: Performance tab -> perform the janky interaction -> find the slowest part -> fix -> profile again -> then React profiler -> only then unnecessary re-renders. No numbers in the article.
- **Rule:** the evidence template requires the **top self-time frame** of the slow interaction from a real trace before a memo/split PR is accepted (implements the existing doc's "render count alone is not a conclusion").
- **Confidence: high.**

**#53. Before you memo()** — Dan Abramov (React team) — https://overreacted.io/before-you-memo/ — 2021-02-23 — **READ**
- Two techniques: **move state down** (extract the stateful part into its own component) and **lift content up** (pass stable subtrees as `children` so a state change in the wrapper does not re-render them); "they don't replace `memo` or `useMemo`, but they're often good to try first"; "where it's not enough, use the Profiler and sprinkle those memo's"; the patterns are about code structure rather than performance per se.
- **Rule:** prefer these two refactors for provider/shell problems (workbench + title bar + module view all under one state) — they need no memo and survive compiler bail-outs.
- **Confidence: high.**

**#54. You Might Not Need an Effect** — React team — https://react.dev/learn/you-might-not-need-an-effect — date n/a — **READ**
- Derived state set in an effect "does an entire render pass with a stale value, then immediately re-renders" — compute during render; chains of effects re-render the component and children between each `set`; reset state by **`key`** not by an effect; adjust a part of state on a prop change by storing an **ID** and deriving the selection; expensive derivations use `useMemo`; user-triggered work belongs in event handlers.
- **Rule:** D9 ratchet; also the effect->setState->effect chains show up as "Cascading update" in the Scheduler track (#2). Selections stored as IDs (already DevKit's `selectedId` convention).
- **Caveat:** an ESLint rule specifically for this was not verified here. **Confidence: high.**

**#55. React Compiler: Debugging and troubleshooting** — React team — https://react.dev/learn/react-compiler/debugging — date n/a — **READ**
- Verify a component is optimised by the **✨ badge in React DevTools**; `"use no memo"` isolates compiler issues; the compiler "safely skips optimization" for code that might break the Rules of React rather than fail; most issues are runtime issues from Rules-of-React violations the compiler cannot detect; memoization-for-correctness (effects depending on referential equality) is a common breaking pattern. The page does not name the ESLint rule.
- **Rule:** since the compiler silently skips, **count skipped components** (D1) and keep the list in CI output; investigate any hot component (editor, transcript row, grid cell) with no ✨.
- **Caveat:** the mechanism to enumerate skipped components in a test is not documented in the fetched page. **Confidence: high (facts), low (implementation of D1).**

**#56. Rendering lists: rules of keys** — React team — https://react.dev/learn/rendering-lists — date n/a — **READ**
- Index as key "often leads to subtle and confusing bugs" when items insert/delete/reorder; `key={Math.random()}` makes keys never match -> **all components and DOM recreated every render**, "slow", and **user input lost**; keys must not be generated while rendering; `key` is not a prop.
- **Rule:** D8; also check `key` on list rows that contain inputs/editors/terminals.
- **Confidence: high.**

**#57. React: `useLayoutEffect`** — React team — https://react.dev/reference/react/useLayoutEffect — date n/a — **READ**
- "can hurt performance. Prefer `useEffect` when possible"; its code and all state updates scheduled from it **block the browser from repainting**; a state update inside it makes React run all remaining Effects immediately; the measure-then-`setState` pattern renders **in two passes and blocks the browser**; object/function deps created in the component make effects re-run more than needed.
- **Rule:** D4 + D3: each of the 20 files gets an annotation ("measures before paint: X") and a render-count test; replace measure-then-setState with CSS (container queries, `grid`, `anchor`) where possible, or with a ref + direct style write.
- **Confidence: high.**

**#58. What forces layout/reflow. The comprehensive list.** — Paul Irish (Chrome DevRel) — https://gist.github.com/paulirish/5d52fb081b3570c81e3a — original date not shown, update Apr 2020 and activity to Oct 2026 — **READ**
- List of forcing reads: `offset*`, `client*`, `scroll*` (reads), `getClientRects`, `getBoundingClientRect`, `scrollIntoView(IfNeeded)`, `focus()`, `innerText`, `computedRole/Name`, `window.scrollX/Y`, `visualViewport.*`, `elementFromPoint`, `select()`, mouse `layerX/offsetX`; `getComputedStyle` forces layout only for some cases (shadow trees, viewport-dependent media queries, or properties such as `height/width/top/…/transform/translate/rotate/scale/grid*`; margin/padding only when fixed); SVG `getBBox`, `getComputedTextLength`; costs only if something was dirtied since the last layout; derived from Blink source — WebKit and Gecko "mostly consistent"; a 2025 comment says `window.innerHeight` did not force reflow in Chromium 133.
- **Rule:** D5. The distinction "layout is forced only if dirty" implies the real fix is to **order work: all reads, then all writes** in a frame (rAF) — DevKit's `settledRunner` already defers resize-driven reads.
- **Caveat:** Blink-based; unverified comments are in the gist. **Confidence: high (list), medium (cross-engine).**

**#59. Avoid large, complex layouts and layout thrashing** — web.dev — https://web.dev/articles/avoid-large-complex-layouts-and-layout-thrashing — date n/a — **READ**
- Layout is "almost always scoped to the entire document"; larger DOMs cost more (not tightly coupled); batch **reads then writes**; in a loop alternating read/write forces layout "on every single iteration"; example: layout > 28 ms per frame with 1 618 elements (5 needing layout) against a 16 ms budget; detect via Timeline/DevTools "Forced Reflow" insight; field attribution through LoAF `forcedStyleAndLayoutDuration`.
- **Rule:** D5 + the LoAF-based Chromium probe; measure forced layout with LoAF, not by guessing from the call site.
- **Confidence: high.**

**#60. MDN: ResizeObserver — observation errors** — MDN — https://developer.mozilla.org/en-US/docs/Web/API/ResizeObserver — date n/a — **READ**
- Notifications run before paint; if a callback changes size, style+layout re-run; to avoid infinite loops the UA processes only elements **deeper** in the DOM per iteration, defers others to the next paint and fires `window` `error` "ResizeObserver loop completed with undelivered notifications"; the error only prevents lock-up, not the loop itself (a callback growing `width` forever repeats every frame); a settling loop may show "a flash of broken layout" across several frames; fixes: do intentional resizing in `requestAnimationFrame`, or skip when the size already equals the expected one (WeakMap of expected sizes).
- **Rule:** D6; in every RO callback that writes a size, either guard with expected size or rAF. Add a window `error` listener to the harness and fail on that message.
- **Confidence: high.**

**#61. Reduce the scope and complexity of style calculations** — web.dev — https://web.dev/articles/reduce-the-scope-and-complexity-of-style-calculations — date n/a — **READ**
- Costly selectors such as `.box:nth-last-child(-n+1) .title` (the browser must check surrounding elements) -> use a specific class; worst-case cost scales with elements x selectors, and the page ranks **reducing invalidated elements** above selector complexity; "in modern browsers this tends to be less of an issue"; Blink: "roughly half" of computed-style time is selector matching (Rune Lillesveen); example recalc just over 25 ms for just over 900 elements; measure in Performance panel (Recalculate Style shows elements affected, duration) with Selector stats.
- **Rule:** the metric to put in evidence is **"elements affected" and duration of Recalculate Style** per interaction, not selector style opinions; D10.
- **Caveat:** older article; page does not discuss `:has()`. **Confidence: high.**

**#62. How Blink invalidates styles when `:has()` is in use** — Byungwoo Lee (Igalia, implemented Chrome's `:has()`) — https://blogs.igalia.com/blee/posts/2023/05/31/how-blink-invalidates-styles-when-has-in-use.html — 2023-05-31 — **READ**
- ~4 % of Chrome page loads use `:has()` (May 2023); for `.a:has(.b)`, adding `b` on a descendant requires **walking up ancestors** to find `.a` (the reverse of `.a .b`); only classes appearing inside the `:has()` argument trigger ancestor traversal; flags `AffectedBySubjectHas` / `AncestorsAffectedByHas` limit work; performance depends on (1) elements traversed, (2) cost of checking whether a mutation relates to an element, (3) elements marked for recalculation unnecessarily; the approach is "not perfect, but better than recalculating everything"; a 19 531-element tree was used for tests (charts only, no numbers in text). The article gives no explicit authoring advice.
- **Rule:** treat each `:has()` as a **tracked cost**: only on small, shallow subjects; narrow arguments (my inference from the flags mechanism). Detect by D10 and by a trace with a class toggle at the deepest level.
- **Confidence: high (mechanism), medium (advice).**

**#63. Intent to Ship: `:has()` pseudo-class** — Byungwoo Lee on blink-dev — https://groups.google.com/a/chromium.org/g/blink-dev/c/bRsbl3wLuyk/m/mt3iSKNHBQAJ — 2022-05-16 — **READ**
- Ergonomics risk wording: "Authors can easily increase complexity of selector query or style invalidation by using ':has()' pseudo classes"; processing time scales roughly **linearly** with element count and memory roughly **logarithmically**; mitigations (caching `:has()` results within a query, affected-by/dynamic-restyle flags, disallowing `:is()`, `:where()`, `:has()` inside `:has()`, a bloom filter like WebKit's); advice: **`.a:has(> .b)` over `.a:has(.b)`**, and for `:hover`/`:focus` use a compound: **`.a:has(.b:hover)` over `.a:has(:hover)`**.
- **Rule:** D10 encodes both rewrites; any `:has(:hover|:focus|:focus-within|:checked)` must be anchored.
- **Caveat:** Chromium; WebKit implemented it with its own caching/filtering (#64). **Confidence: high.**

**#64. Using `:has()` as a CSS parent selector and much more** — Jen Simmons (WebKit Developer Relations) — https://webkit.org/blog/13096/css-has-pseudo-class/ — 2022-08-18 — **READ**
- WebKit built "novel `:has`-specific caching and filtering optimizations", chose a fully powerful implementation instead of a restricted one, and reports "fantastic performance" with large DOM trees and many `:has()` selectors (supported by a linked tweet, no numbers in the article); no invalidation mechanics, no guidance on expensive patterns; notes form invalidation was not supported in some situations (correctness).
- **Rule:** do not assume WebKit is slow or fast on `:has()`; the engine claims are unquantified. **Measure on WebKitGTK** (a 2026 WebKit bug titled "[:has() perf] Include full selector context to :has() invalidation selectors" was marked fixed, per a search snippet only).
- **Confidence: medium.**

**#65. Style Recalculation Secrets They Don't Want You To Know** — Patrick Brosset (Microsoft Edge, DevTools) — https://noti.st/patrickbrosset/NIyBLK/slides — CSS Day, June 2023 — **READ**
- Recalc is usually not the bottleneck but can be slow on large pages; browsers use **invalidation sets** (built at load/when stylesheets change; conservative — false positives allowed, misses are bugs); selectors match **right to left**; sibling (`+`/`~`) and `:nth-child` combinations can force whole-subtree recalcs; a real case: a ~**2 s hover delay** from a `+`/`~` selector on `body` invalidating all children; a slow case showed 400 ms+ recalc over ~5 000 elements; large web app example: 100+ stylesheets, 10 000+ rules, 2 000-6 000 nodes; advice: identify the slow scenario, measure, improve, measure again.
- **Rule:** D10 includes "no sibling combinators or `:nth-*` on high-level containers".
- **Caveat:** slides (the text is terse); the page didn't cover DevTools selector stats. **Confidence: medium.**

**#66. CSS container queries cost** — CSSWG thread / posts — e.g. https://lists.w3.org/Archives/Public/public-css-archive/2022Jun/0423.html — 2022 — **SNIPPET**
- Summary: the cost depends on the implementation approach and how authors write queries; concerns apply to **style queries** resolution, not all sites; Gecko/Chromium filter selector targets first then narrow containers, WebKit does the reverse; naming containers narrows the search. No Blink numbers found.
- **Rule:** use **named** containers for container queries; ratchet unnamed `container-type` on broad ancestors. **Confidence: low** (search-result summary only; not a primary performance measurement).

**#67. web.dev: `content-visibility`** — Una Kravets, Jeremy Wagner, Vladimir Levin — https://web.dev/articles/content-visibility — published 2020-08-05, updated 2025-09-23 — **READ**
- `content-visibility: auto` gave a **7x** rendering boost (232 ms -> 30 ms) in the example; offscreen elements get size containment so need `contain-intrinsic-size` (the `auto` keyword remembers the last size); off-screen content stays in the accessibility tree; calling DOM APIs that force rendering on skipped subtrees removes the benefit; support: Chrome/Edge 85, Firefox 125, Safari 18 (Baseline 2025-09-15).
- **Rule:** D13; the existing note "content-visibility is not virtualisation" holds (DOM nodes persist). Test a scenario where a layout read (e.g. `scrollIntoView`) targets content under `content-visibility:auto` — it forces rendering of the skipped subtree.
- **Caveat:** WebKitGTK 2.52's Safari-18-equivalent support is not stated. **Confidence: high.**

**#68. Stick to compositor-only properties and manage layer count** — web.dev — https://web.dev/articles/stick-to-compositor-only-properties-and-manage-layer-count — date n/a — **READ**
- Only `transform` and `opacity` are compositor-only; promote with `will-change: transform` "within reason, don't overdo it"; each layer needs memory and management, textures must be uploaded to the GPU; promoting everything is a "layer explosion"; compositing target ~4-5 ms during scrolling/transitions; inspect with Layers tab (reasons for layer creation) after enabling paint profiler.
- **Rule:** D11/D12; and per the existing smoothness doc, WebKitGTK builds a **layer for the whole pane** when `opacity` animates over a large tree: this page does not say so, the DevKit measurement (smoothness 3.8) does.
- **Caveat:** no per-layer byte numbers. **Confidence: high.**

**#69. Simplify paint complexity and reduce paint areas** — web.dev — https://web.dev/articles/simplify-paint-complexity-and-reduce-paint-areas — date n/a — **READ**
- "Anything that involves a blur (like a shadow, for example) is going to take longer to paint than drawing a red box"; `background:red` vs `box-shadow` look similar but differ in cost; Rendering tab "paint flashing" shows repainted areas; Paint Profiler for complexity; 10 ms per-frame budget for paint is "often too short"; the page does not mention `filter` or gradients.
- **Rule:** D11; use **paint flashing** (Chromium) or WebKit's Layout & Rendering timeline (paint records with sizes) to prove a change reduces repainted area.
- **Confidence: high.**

**#70. MDN: CSS performance optimization** — MDN — https://developer.mozilla.org/en-US/docs/Learn_web_development/Extensions/Performance/CSS — date n/a — **READ**
- `will-change` "is intended to be used as a last resort ... should not be used to anticipate performance problems"; properties triggering reflow include dimensions, position, margin, `align-*`, `flex`, and **`box-shadow`** (listed among geometry-changing effects); preferred animation properties: transforms, `opacity`, **`filter`** (listed as not reflowing/repainting, which conflicts with the WebKitGTK blur bug #44 and DevKit's smoothness doc — treat MDN's claim as engine-optimistic); "larger animations are more costly"; `contain`/`content-visibility` limit recalculation; `body *` selectors and overly specific selectors cost; `:has()` "requires careful use"; no numbers.
- **Rule:** D12; do not accept "filter is cheap" from docs for the WebKitGTK path without a measurement.
- **Caveat:** guidance page without benchmarks; internally in tension with #44. **Confidence: medium.**

**#71. TanStack Query: Render optimizations** — TanStack — https://tanstack.com/query/latest/docs/framework/react/guides/render-optimizations — date n/a — **READ**
- Structural sharing preserves object references when data hasn't changed (works for JSON-compatible data; `structuralSharing:false` or a custom function to change it); the top-level result objects are new each render but `data` is "as stable as possible"; **tracked properties** (Proxy) re-render only for properties the component reads — **object rest destructuring reads every property and disables it** (a lint rule exists); `select` limits re-renders to a transformation of data and re-runs only if its function reference or `data` changes (inline functions rerun every render); no quantitative claims.
- **Rule:** ban `const {data, ...rest} = useQuery()` (D-lint: add to the store-subscription ratchet family); put `select` functions at module scope or `useCallback`; set `notifyOnChangeProps` only with evidence. DevKit uses `staleTime` defaults — keep gating tab queries as described in smoothness §11.
- **Confidence: high.**

**#72. TanStack Virtual: React adapter** — TanStack — https://tanstack.com/virtual/latest/docs/framework/react/react-virtual — date n/a — **READ**
- `useFlushSync` (default true) uses `flushSync` during scroll for accuracy, "may hurt performance"; consider `false` for rapid scrolling on lower-end devices, non-critical lists, or to avoid React 19's warning about `flushSync` during rendering; `directDomUpdates` (default false) skips React re-renders for scroll-only updates (needs absolute-positioned items and `containerRef`); mode `'transform'` smoother but creates a stacking context interfering with `position: fixed` descendants; set `directDomUpdates` once at mount. `overscan`, `getItemKey`, stable-reference guidance **not** in the extract.
- **Rule:** the transcript/table virtualizers are exactly where scroll-time re-renders cost; evaluate `useFlushSync:false` and `directDomUpdates` against the standard transcript fixture with frame-gap p90 (DevKit's `useVirtualRows` uses padding rows rather than absolute items, so `directDomUpdates` is **not** applicable there as documented).
- **Caveat:** newer options than the installed version might not exist; check the installed version. **Confidence: medium.**

**#73. Case study: Making React interactions in Causal 4x faster** — Ivan Akulov — https://3perf.com/blog/causal/ — 2022-11-24 — **READ**
- Method: Chrome Performance recording of one update, counting `performSyncWorkOnRoot` calls (1 325), flame chart to callers, React Profiler "Record why each component rendered", logpoints and `console.time`; JS cost dropped almost 4x (medians of 5 runs, before/after); fixes: remove an extra render (setState inside effect) ~15-20 %; skip no-op renders with a shallow equality check (1 568 renders per cell update) ~15-30 %; `useEffect` depending on a whole object -> depend on a joined sorted ID list ~5-10 %; deep-compare of a ~90 ms memoized `context` prop -> identity check ~3-5 %; anti-patterns: large memoized context deep-compared every render, deep derived values hard to memoize, `useSelector`+`useCallback` regenerating callbacks (read `getState()` inside callbacks instead); **what failed**: moving protobuf parsing to a Web Worker (comlink) — serialising large data cost more than parsing; loading only visible rows cut parsing from 500-1 500 ms to 1-5 ms.
- **Rule:** the best reusable pattern is "**count renders per interaction first** (1 325 -> N), then attribute them"; and for workers: measure structured-clone/serialisation cost before moving data across (same for the Go bridge: JSON payload size). Report **estimated savings per change** in PRs as Akulov does.
- **Caveat:** AG Grid-specific fixes; "medians of 5 runs" is a smaller N than the template's 10. **Confidence: high.**

**#74. How to monitor React render performance** — Ivan Akulov — https://3perf.com/blog/react-monitoring/ — 2022-06-06, updated 2024-03-12 — **READ**
- Argues `<Profiler>` is a poor fit for **interaction** monitoring: it measures only React's render phase and "would miss an expensive `useLayoutEffect` or extra layout work"; measure key interactions (start at `performance.now()`, end in an `afterFrame` callback so style/layout/paint are included), long tasks (> 50 ms), slow event handlers (>= 50 ms), synthetic INP; caveats: rAF/setTimeout/promise callbacks can end a measurement before paint; `useEffect` can fire early; with `useTransition`/`useDeferredValue` `afterFrame` fires too early; field observer fields vary by browser (Chrome 102 details); synthetic tests were "too noisy", compare against the **average of the last 4-40 runs**; overhead not quantified.
- **Rule:** the interaction helper: `t0 = event.timeStamp` -> `requestAnimationFrame(() => setTimeout(end))` (same shape as Speedometer's sync+async, #25); report as `performance.measure`. Keep `<Profiler>` for counts only.
- **Caveat:** Chrome-centric details. **Confidence: high.**

**#75. Optimize long tasks** — Jeremy Wagner & Brendan Kenny (web.dev) — https://web.dev/articles/optimize-long-tasks — published 2022-09-30, updated 2024-12-19 — **READ**
- Long task > 50 ms; after five nested `setTimeout` calls the browser clamps to a minimum 5 ms; `scheduler.yield()` supported in Chrome/Edge 129 and Firefox 142, **Safari does not**; `isInputPending()` (Chrome 87 only) is **no longer recommended**; yield before user-facing work, batch short jobs and yield when a ~50 ms deadline passes, always `await` the yield, avoid `forEach` for this.
- **Rule:** on WebKit there is **no `scheduler.yield`** — DevKit's chunked work (markdown pass, index progress, large store hydrations) needs a `MessageChannel`/`setTimeout` fallback and `startTransition`; gate any `scheduler.yield()` use with feature detection (D24).
- **Caveat:** WebKitGTK 2.52 support of `scheduler.yield` unverified here. **Confidence: high.**

---

## 3. Cross-cutting observations (derived from the entries above)

- **Three layers of evidence, in order of decreasing reliability:** (1) deterministic work counters (#20, #27, #73), (2) controlled-machine frame/long-task statistics with N >= 10 and median+spread (#23, #25, #24), (3) traces/profiles for **explanation only** (#18, #15, #2). DevKit has layer 1 for the transcript only, layer 2 as a hand-run harness, layer 3 abundantly.
- **WebKit is not Chromium:** no LoAF/long-task (#9), no `scheduler.yield` (#75), heavy Timelines off by default (#15), CSS `filter: blur` slow path on WebKitGTK (#44), unknown persistent code cache (#34). Everything in the Chrome-docs half of this catalogue (selector stats, paint flashing, LoAF, extensibility tracks) is Chromium-harness-only; use it to **find** things, then re-measure on the real engine.
- **bun test = JSC**: microbenchmarks in the test runner are closer to Safari's JS engine than to Chromium's V8, but give no layout/paint information (#29).
- **DevKit-specific amplifiers:** long sessions (leaks matter, #48), `--isolate` per test file (barrel/module-graph costs multiply, #36), local fonts/bridge (network-RTT advice does not apply, #17/#41/#43), Compiler on (manual memo advice shifts to "verify the compiler didn't bail out", #55).

## 4. What was not verified / not found

- No primary-source read for: DevKit-relevant `:has()` WebKit invalidation cost numbers; container-query cost in Blink/WebKit (snippet only, #66); `backdrop-filter` cost (practitioner posts only, not cited as fact); JSC persistent code caching status; React team's own benchmark/CI setup (search returned Reassure, not React's repo); TanStack's own perf CI; Vercel's CI regression thresholds (only the turbopack-bench README and CodSpeed snippets).
- Event Timing / `PerformanceObserver` entry-type support in WebKitGTK 2.52 specifically: unknown (must be logged on the real engine, D24).
- Whether WebKitGTK's remote inspector in DevKit's build exposes Timelines (#16).

## 5. Methodology notes

- Searched with WebSearch, then read pages with WebFetch (summaries were produced by the fetch tool's model on the page text; quotes in this file are the tool's extraction of the page text and numbers were copied from those extractions). Pages where the extraction lacked a date are marked "date n/a".
- "Confidence" rates how well the entry's claims are supported by the page text read and how transferable they are to DevKit's engines, not the quality of the author.

## 6. Failed or unusable fetches (nothing invented to fill them)

- https://nolanlawson.com/2022/01/06/... and `/fixing-memory-leaks-in-web-applications/` : 404 (the 2022-01-05 "Memory leaks: the forgotten side of web performance" post **was** read; the separate "fixing memory leaks" post was not found).
- https://blog.nethercote.net/... : DNS failure (the rust-internals thread #20 was read instead).
- https://zustand.docs.pmnd.rs/hooks/use-shallow and .../migrations/migrating-to-v5 : 404.
- https://learn.microsoft.com/.../detached-elements-tool : 404.
- https://web.dev/articles/long-animation-frames : 404 (the developer.chrome.com page was read).
- https://wiki.mozilla.org/... Alert FAQ was read but lacked the 12-point/t-test text; that detail is SNIPPET only.
- https://chromium.googlesource.com/.../addressing_performance_regressions.md and bisects.md : 503.
- https://docs.codspeed.io/instruments/cpu-simulation -> redirect then 404 at the new URL.
- https://trac.webkit.org/wiki/RemoteInspectorGTKandWPE : "moved" stub only.
- https://www.npmjs.com/package/size-limit : 403 (GitHub README read instead).
