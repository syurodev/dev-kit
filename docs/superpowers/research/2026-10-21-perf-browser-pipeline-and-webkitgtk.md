<!-- Research notes behind docs/dev-kit/frontend-performance.mdx (section "Kỹ thuật cực hạn từ các chuyên gia") and docs/dev-kit/smoothness.mdx (section 13). Written 2026-10-21 by research agents. READ = source fetched and read; SNIPPET = search summary only; MEASURED = run on this machine. Kept as-is for traceability; the .mdx files hold the conclusions. Raw probe files mentioned here were not kept in the repo. -->

# Research B: browser rendering pipeline, scheduling and WebKit / WebKitGTK

Date of research: 2026-10-21. Target: WebKitGTK 2.52.6 (Ubuntu 26.04 package), Wayland, Iris Xe, DMABUF on, DPR 2.
Existing docs not duplicated: `docs/dev-kit/smoothness.mdx`, `docs/dev-kit/frontend-performance.mdx` (both read first; where this file extends or contradicts them it says so).

Legend. READ = fetched and read the page (or a clearly stated part of it). SNIPPET = only a search-result summary, nothing fetched. MEASURED = I ran it myself in the real engines on this machine (WebKitGTK 2.52.6 `MiniBrowser` from `webkitgtk-6.0`, Wayland, DPR 2; Google Chrome 155 headless, DPR 1). Raw data and probe pages: `(scratchpad, not kept)` (probe.html, probe3.html, srv3.py, result-*.json, r3-webkitgtk/, r3-chrome/, r3n/).

---------------------------------------------------------------------------------------------------

## 0. Synthesis

### 0.1 The ten to fifteen highest-payoff rules for DevKit (ranked)

1. **Never write an inherited property (CSS custom property, `inert`, `visibility`, `pointer-events`) on `:root` or any big ancestor from an effect or per frame, and never read it back with `getComputedStyle(...).getPropertyValue(...)` in an effect.**
   Evidence (MEASURED, 20 000 elements): one write of `--x` on `<html>` + `getComputedStyle(html).getPropertyValue('--x')` costs **53 ms median on WebKitGTK 2.52.6 vs 27 ms on Chrome 155**; with a forced layout instead, 134 ms vs 72 ms; writing a variable that nothing reads costs the same (53 ms), because the whole inherited subtree is restyled. Writing on a small element, or on `:root` with a registered `@property` whose `inherits: false`, costs about 0 ms in both engines. A merged real-world PR reports 77 ms per root write on a 42k-element document and 0.5 ms scoped (sim PR #5738). This is exactly the `getComputedStyle().getPropertyValue("--header-inset")` hot spot.
   Do: write the variable on the element that consumes it (or declare `@property --x { inherits:false }`); keep the number in JS (ResizeObserver result or a module constant) instead of round-tripping through computed style; no reads of layout/computed values inside effects that run per commit.
   Verify: Selector Stats / Recalculate Style in Chrome trace; Web Inspector Layout & Rendering timeline "Style invalidation / recalculation" rows on WebKitGTK; the micro-bench in `probe.html`.

2. **On WebKitGTK 2.52, do not put `contain: layout|paint|content|strict`, `overflow: hidden`/`auto` (these make independent formatting contexts) or `content-visibility: auto` on hundreds of sibling elements.** MEASURED, 4000 sibling cards: forced layout after editing ONE card = **3 ms plain, 266 ms `contain:layout`, 333 ms `contain:layout style paint`, 352 ms `contain:paint`, 318 ms `overflow:hidden`, 377-508 ms `content-visibility:auto`** (Chrome: 1-2 ms for all). With `content-visibility:auto` a full-range scroll sweep ran at **414-516 ms per frame** (38 of 39 frames over 20 ms) on WebKitGTK and at 16.7 ms on Chrome. The cost is super-linear in sibling count (`contain:layout`: N=1000 -> 7 ms, N=2000 -> 46 ms, N=4000 -> 266 ms; `content-visibility:auto` fine at N<=300, p90 26 ms at N=1000). The Safari 27.0 notes list the matching fix ("`contain: layout` caused significantly slower forced layouts when all siblings created their own formatting context", 171545381), so 2.52 does not have it. `contain: style` and `isolation: isolate` were fine.
   Do: virtualise long lists (windowing) instead of relying on `content-visibility`; keep contained siblings under about 300 per container; use `contain` only on a few big panes. This **contradicts the spirit of `smoothness.mdx` 6.4** ("content-visibility: auto for below-the-fold areas ... still used on Notes card, agent line, message scroller"): audit those on WebKitGTK with the real counts.

3. **Budget main-thread work for WebKitGTK at about half of Chrome's speed, and aim for 8-10 ms of JS+style+layout per frame, not 16 ms.** MEASURED: style+layout over the same 20k-element tree is 1.9-2.0x slower on WebKitGTK 2.52 than Chrome 155; this matches the project's 1.7-2.1x observations. The classic pipeline note says browser overhead leaves about 10 ms of the 16.66 ms (web.dev, READ). Do: define per-frame work budgets in `frameStall` terms against WebKitGTK numbers, not Chrome numbers.

4. **Do not assume modern scheduling APIs exist in WebKitGTK: use a small portable yield helper.** MEASURED in 2.52.6: `scheduler`, `scheduler.yield`, `scheduler.postTask`, `navigator.scheduling.isInputPending`, `requestIdleCallback`, `long-animation-frame` and `longtask` entries are all absent (Chrome 155 has every one). `PerformanceObserver` entry types present: event, first-input, largest-contentful-paint, mark, measure, navigation, paint, resource. Do: yield with a `MessageChannel` macrotask (what React's scheduler does, to avoid the 4 ms `setTimeout` clamp, READ), in slices of at most about 5-8 ms, and `rAF` for visual commits; feature-detect `scheduler.yield` only as a Chrome/Windows-WebView2 fast path. Do not build on `isInputPending` (Chrome is even deprecating its own advice, see entry 7).

5. **Keep heavy non-DOM work (markdown parse, syntax highlight, diff, JSON pretty-print, search) off the main thread, with messages under about 100 KiB (10 KiB if it feeds an animation).** Surma's measurements (READ): structured clone cost tracks JSON size; 100 KiB stays inside the 100 ms budget even on the slowest phones; 10 KiB is safe per frame. Transferables are size-independent. On WebKitGTK 2.52.6 (MEASURED): `Worker` and `OffscreenCanvas` 2D context in a worker work; **`OffscreenCanvas.getContext('webgl')` in a worker returns nothing**; `SharedArrayBuffer` is undefined (not cross-origin isolated). So workers = yes, GPU canvas in workers = no, shared memory = unverified (needs COOP/COEP from the Wails asset handler; not tested).

6. **Layers: treat `will-change`, `opacity` animation and `backdrop-filter` on large subtrees as paid features on WebKitGTK; keep them transient and few.** MEASURED: `will-change: transform` on every one of 4000 cards made a scroll sweep 49 ms per frame (every frame over 20 ms) vs 17 ms; at 1000 cards it was still 17 ms, so the cliff is in total layer area, not count of tens. MDN: set `will-change` just before and remove after; do not leave it in stylesheets. Chrome docs: each layer costs GPU texture memory and CPU-GPU bandwidth (READ). WebKit bug 271340 (READ): `backdrop-filter` makes the document element a backdrop root, which adds a full-screen compositing layer unless the document element has an opaque background; fixed on trunk in March 2024 (fix not tied to a release number in the bug) - give `html` an opaque background anyway. Confirms and extends `smoothness.mdx` 4.2 and 7.1-7.3.

7. **Find and delete `background-attachment: fixed` and similar scroll-synchronised paints.** WebKit bug 221738 (READ, status REOPENED Aug 2025): a page that scrolled at 12-18 fps on a 4K Skia WebKitGTK ran "nearly smooth" and CPU dropped from about 150% to 15% when `background-attachment: fixed` was disabled in the inspector. Cheap grep, large downside.

8. **Observers and per-element listeners: keep the observed count small on WebKitGTK 2.52.** Safari 27.0 notes (READ) list fixes for `IntersectionObserver` "slows down when observing many elements, caused by O(n^2) iteration" (172727210) and `ResizeObserver` "callbacks no longer become progressively slower over time" (172718139). 2.52 does not have them. Do: one shared observer per concern, observe only mounted (virtualised) rows, `disconnect()` on unmount, never observe every row of a long list.

9. **Prefer class selectors and small restyle scopes; avoid attribute selectors and deep invalidation.** Nolan Lawson (READ): 1000 components x 10 rules, style recalc on Safari 15.5: classes 80 ms, attributes **802 ms**, unscoped 857 ms (Chrome 102: 277/419/820; Firefox 45/54/190); Safari 16 "largely closed" the attribute gap (his Oct 2022 note). Blink spends about half of style calc matching selectors (web.dev, READ). Do: use Selector Stats in Chrome to find hot selectors, but remember the same CSS is 2x slower in WebKitGTK; shrink the invalidated subtree (rule 1) before tuning selectors.

10. **Measure on the real engine with the right tools, and bake a frame-time harness into CI-like local runs.** WebKitGTK: remote Web Inspector via `WEBKIT_INSPECTOR_SERVER`/`WEBKIT_INSPECTOR_HTTP_SERVER` plus `enable-developer-extras` (Wails v3 has `DevToolsEnabled`/`OpenDevTools`); Timelines -> Layout & Rendering, JavaScript & Events, CPU, and the **Frames view (colour-coded JavaScript/Layout/Paint/Other with 30 and 60 fps lines)**; the Layers tab (compositing reasons, per-layer memory, paint count); Sysprof marks for WebKit's rendering pipeline. Harness primitives that exist on WebKitGTK 2.52: `performance.mark/measure`, Event Timing (`event`, `first-input`), rAF interval stats, 10 ms timer lateness (already in `frameStall.ts`). Timer resolution on WebKitGTK is whole milliseconds (MEASURED values were integers), so aggregate many iterations.

11. **Never rely on `overflow-anchor` / scroll anchoring on 2.52: it does not exist there.** MEASURED `CSS.supports('overflow-anchor','none')` = false on 2.52.6; BCD says Safari 27; WebKit's Safari 27.0 post says "Safari 27.0 adds scroll anchoring". Anything that inserts above the viewport (streaming transcript, lazy measured rows) needs explicit scroll compensation in JS (same frame as the layout read/write batch). Chrome has it by default and masks bugs here.

12. **Batch DOM reads before writes, per frame; treat every layout-metric read in an effect as a bug until proven cheap.** Irish list (READ): `offsetWidth/Height`, `getBoundingClientRect`, `scrollTop` and friends, `innerText`, `focus()`, `scrollIntoView`, `getComputedStyle` of layout-dependent properties force style+layout. web.dev example: 28 ms of layout per frame from one read-write loop. FastDOM-style `measure()/mutate()` (READ) or a single measure phase per frame. In Chrome, LoAF `forcedStyleAndLayoutDuration` finds these in the field; on WebKitGTK use the timeline's "Layout" record, which shows the JS that triggered it.

13. **Budget DPR 2 paints: WebKitGTK at fractional display scale paints at 2x, so every full-viewport repaint costs 4x pixels.** Igalia: HiDPI is faster through GPU scaling but not free (2.46 post, READ); bugs 250998 and 280920 (READ, open) report order-of-magnitude slower scrolling at 4K/2x on Intel. MEASURED: MiniBrowser DPR = 2 on this machine. Do: avoid large shadows/gradients/blur behind scrolling content; test every perf gate at DPR 2 (not 1) because Chrome at DPR 1 flatters. (Not tested: whether forcing `GDK_SCALE=1` or a different scale changes cost. Run that as an experiment before recommending it.)

14. **Plan for the engine moving under us: WebKitGTK 2.54 (Sept 2026) ships a new Skia compositor and "damage in composition", and removes the Cairo option.** Ubuntu 26.04 still ships 2.52.6. Igalia's own numbers are for a Raspberry Pi 4, not Intel (filters/clip/mask tests 3-4x faster, MotionMark about 35%), so they are not predictions for DevKit. Do: add a "WebKitGTK version" field to every perf report; re-run the harness when a 2.54 package is available; use `WEBKIT_USE_SKIA_FOR_COMPOSITION=0` (2.54 only) to bisect compositor regressions.

15. **Streaming and publish cadence: keep one React publish per frame, but cap the work inside that frame, not the number of publishes.** The Chrome profile (96% busy, JS 5.5 s of 7.4 s) shows the problem is work per commit, not commit count. Rules 1, 12 and virtualisation cap per-commit cost; rule 4 supplies a yield for the unavoidable rest.

### 0.2 Browser feature support matrix: WebKitGTK 2.52.6 vs Chrome

"V" = verified by running a probe in the real engine today (WebKitGTK 2.52.6 `MiniBrowser`; Chrome 155 headless). "BCD" = from MDN browser-compat-data (READ), which describes Safari, not WebKitGTK. "U" = not verified in WebKitGTK. UA string on WebKitGTK reads `Version/60.5 Safari/605.1.15`: never sniff Safari versions from it.

| Feature | WebKitGTK 2.52.6 | Chrome 155 | Notes |
|---|---|---|---|
| `content-visibility: auto/hidden` | **V: supported** (CSS.supports true) but **V: pathological with many siblings** (rule 2) | V: supported, fast | BCD: Safari 18 partial for `auto` (find-in-page), full in 26 |
| `contain` (`layout paint style strict content`) | V: supported; **V: slow forced layout with many siblings** | V: fine | BCD Safari 15.4; Safari 27 fixes the sibling slowdown |
| `contain-intrinsic-size` incl. `auto <len>` | V: supported | V | BCD Safari 17 |
| `scheduler.yield()` | **V: absent** | V: present | BCD: Safari not supported; Firefox 142 |
| `scheduler.postTask()` | **V: absent** | V | BCD same |
| `isInputPending()` | **V: absent** | V (Chrome-only API) | BCD: Firefox, Safari not supported |
| `requestIdleCallback` | **V: absent** | V | BCD: Safari preview behind flag |
| OffscreenCanvas in workers | **V: 2D yes, WebGL no** (`getContext('webgl')` falsy in a worker) | V: both | BCD main-thread WebGL Safari 17; worker WebGL not in this data |
| `SharedArrayBuffer` | V: undefined (page not cross-origin isolated); **U: with COOP+COEP via Wails assets** | V: undefined under same conditions (no headers) | BCD: Safari 15.2 with isolation |
| Long Animation Frames (`long-animation-frame`) | **V: absent** | V | BCD: Chrome 123 only |
| Long Tasks (`longtask`) | **V: absent** | V | |
| Event Timing (`event`, `first-input`) | **V: present** | V | BCD Safari 26.2; INP-style measurement possible |
| `overflow-anchor` / scroll anchoring | **V: absent** (CSS.supports false) | V | BCD Safari 27 |
| `@starting-style` | V: `CSSStartingStyleRule` present | V | BCD Safari 17.5 |
| View Transitions (`startViewTransition`) | V: present | V | BCD Safari 18; snapshot bugs fixed in 27 |
| `inert` | V: present | V | |
| `scrollbar-gutter` | V: supported | V | BCD Safari 18.2; no effect with overlay scrollbars |
| `scrollbar-color` | V: supported | V | added in WebKitGTK 2.52.3 release notes (READ) |
| `backdrop-filter`, `will-change`, `animation-timeline: scroll()` | V: supported | V | perf caveats in rules 6 |
| `Element.moveBefore` | **V: absent** | V | matches the existing note in smoothness.mdx 3.8 |
| `onscrollend` | V: present | V | 2.52.0 release notes fix `scrollend` timing |
| `CSS.registerProperty` / `@property` | V: present (registered `inherits:false` var write cost 0) | V | |
| `structuredClone`, `ResizeObserver` | V: present | V | |
| Timer / `performance.now()` resolution | V: whole ms in observed output | sub-ms | Surma (READ): Safari/Firefox clamp to 1 ms |

---------------------------------------------------------------------------------------------------

## 1. Rendering pipeline, layout and compositing (engine-neutral)

### 1.1 "What forces layout / reflow. The comprehensive list."
- Author/role: Paul Irish, Chrome DevRel (gist). URL: https://gist.github.com/paulirish/5d52fb081b3570c81e3a
- Date: body "Updated slightly April 2020"; gist last active 2026-10-11. READ.
- Claims: (a) any API that synchronously returns layout metrics forces style recalc and usually layout: `offsetTop/Left/Width/Height/Parent`, `client*`, `getClientRects`, `getBoundingClientRect`, `scrollTo/By/IntoView`, `scrollTop/Left/Width/Height`, `focus()`, `innerText`, `elementFromPoint`; (b) `getComputedStyle()` "will typically force style recalc" and forces layout when the element is in a shadow tree, when viewport media queries exist, or for properties like height/width/top/transform/grid; (c) "if layout is forced, style must be recalculated first"; (d) reflow only costs when style or layout is invalid; (e) the data comes from Blink source; Tony Gentilcore's 2011 WebKit list "generally aligns", modern WebKit "mostly consistent". Comments: `window.innerHeight` did not reflow in Chromium 133.
- Rule for DevKit: grep effects and layout effects for these APIs; one measure phase then one write phase per frame; `getComputedStyle(...).getPropertyValue("--x")` is a style flush even though it is not a layout read.
- Caveats: Blink-sourced; WebKit "mostly" the same; individual entries drift (innerHeight). My own MEASURED forced-style number (rule 1) is the WebKitGTK-specific confirmation.
- Confidence: high.

### 1.2 "Avoid large, complex layouts and layout thrashing"
- Author/role: web.dev / Chrome DevRel (author not shown in fetched text). URL: https://web.dev/articles/avoid-large-complex-layouts-and-layout-thrashing
- Date: published 2015-03-20, updated 2025-05-07. READ.
- Claims: layout cost scales with elements needing layout and their complexity, and is usually document-scoped; reading `offsetHeight` after a style write forces layout early; looping read-write-read-write "thrashes"; example trace had 28 ms of layout per frame vs a 16 ms budget (1618 elements, 5 needing layout); fix is read once, then write; DevTools has a Forced Reflow insight and LoAF exposes `forcedStyleAndLayoutDuration`.
- Rule: batch reads first; use Chrome LoAF/Forced Reflow insight to find offenders in a scenario run, then fix and re-check on WebKitGTK.
- Caveats: detection tooling is Chrome-only (LoAF absent in WebKitGTK, MEASURED).
- Confidence: high.

### 1.3 "Rendering performance" (pixel pipeline)
- Author/role: web.dev / Chrome DevRel (Paul Lewis lineage; author not shown). URL: https://web.dev/articles/rendering-performance
- Date: not shown. READ.
- Claims: pipeline = JavaScript -> Style -> Layout -> Paint -> Composite; geometry changes take the full path, paint-only the shorter one, `transform`/`opacity` only composite ("the cheapest and most desirable pathway"); 60 Hz = 16.66 ms per frame but "all of your work needs to be completed inside 10 milliseconds" because of browser overhead; 100 ms is the oft-cited response figure.
- Rule: per-frame budget 10 ms (halve again for WebKitGTK, rule 3); keep animation properties on the composite-only path.
- Caveats: the 10 ms figure is a rule of thumb, not measured in the article.
- Confidence: high.

### 1.4 "FLIP your animations"
- Author/role: Paul Lewis (then Chrome DevRel), aerotwist.com. URL: https://aerotwist.com/blog/flip-your-animations/
- Date: 2015-02-11, updated 2022-05-17. READ.
- Claims: FLIP = First, Last, Invert, Play: measure start and end, apply an inverse `transform`/`opacity`, then release it with a transition; "a window of 100ms after someone interacts ... where you're able to do work without them noticing"; 60 fps only matters while things move; precompute in idle/response window; transform scaling can distort content.
- Rule: for layout-changing UI (reordering tabs, expanding rows), measure once, animate with transform only; do not animate `height`/`width` of big trees (already in smoothness.mdx 4.2).
- Caveats: FLIP needs a layout read (`getBoundingClientRect`) before the animation: do it once, in a measure phase. Large scaled subtrees can force layers (rule 6).
- Confidence: high.

### 1.5 "Stick to compositor-only properties and manage layer count"
- Author/role: Paul Lewis, web.dev. URL: https://web.dev/articles/stick-to-compositor-only-properties-and-manage-layer-count
- Date: 2015-03-20 (as shown). READ.
- Claims: only `transform` and `opacity` can be handled by the compositor alone; promote with `will-change: transform` (fallback `translateZ(0)`); "Do not promote elements unnecessarily"; promoting everything (`*`) is a bad practice; each layer costs memory and management, and textures cross the CPU-GPU boundary; target about 4-5 ms of compositing time; the Paint profiler shows layers and why each was created. No per-layer memory figure is given.
- Rule: explicit layers only for the 1-3 elements that really animate; verify with the WebKit Layers tab (entry 4.12) not Chrome's.
- Caveats: 2015; compositing is better now but the principle holds; WebKitGTK threshold of pain is MEASURED in rule 6.
- Confidence: high.

### 1.6 `will-change`
- Author/role: MDN contributors. URL: https://developer.mozilla.org/en-US/docs/Web/CSS/will-change
- Date: current MDN. READ (first 100k chars).
- Claims: "Use `will-change` as a last resort ... Don't use it to anticipate performance problems"; apply to small, nested elements, never `<body>`; set it before the change, remove after; in stylesheets it implies "always a few moments away from changing" so the browser may keep the optimisation in memory too long; values that create stacking contexts (e.g. opacity) create them up front; excessive use "will result in excessive memory use".
- Rule: set via JS around an animation (as in MDN's example), never in global CSS; confirm DevKit's existing `will-change` uses (smoothness.mdx 7.2 says "needs review").
- Caveats: containing-block side effects were not in the fetched part of the page.
- Confidence: high.

### 1.7 RenderingNG: architecture and data structures
- Author/role: Chris Harrelson et al., Chromium. URLs: https://developer.chrome.com/docs/chromium/renderingng-architecture , https://developer.chrome.com/docs/chromium/renderingng-data-structures
- Date: architecture page last updated 2021-07-26. READ.
- Claims: 12 stages: animate, style, layout, pre-paint, scroll, paint, commit, layerize, raster/decode/paint worklets, activate, aggregate, draw; main thread vs compositor thread vs Viz (GPU) process; scroll and visual-effect animations can skip layout/pre-paint/paint and run on the compositor thread; "Separating the main and compositor threads is critically important for performance isolation"; four property trees (transform, clip, effect, scroll); layerization trades re-raster cost vs GPU memory and avoids merging chunks whose property-tree state is expected to change on the compositor.
- Rule: for a Chrome finding, map stage -> WebKit stage (table 4.13); the compositor-only property set is the same idea in WebKit but the cost of a new layer differs (Skia tiles, rule 6).
- Caveats: Blink-only architecture; no performance numbers in these pages.
- Confidence: high (for Blink); medium when carried over to WebKit.

### 1.8 "Reduce the scope and complexity of style calculations"
- Author/role: web.dev / Chrome DevRel. URL: https://web.dev/articles/reduce-the-scope-and-complexity-of-style-calculations
- Date: 2015-03-20. READ.
- Claims: worst-case cost = elements x selectors; roughly half of Blink's time in computed style is selector matching, the other half is constructing the computed style (credit: Rune Lillesveen); reducing the number of invalidated elements is often more important than simplifying selectors; example recalculation 25 ms for just over 900 elements.
- Rule: scope first (rule 1), selectors second; tune with Selector Stats (entry 1.9).
- Caveats: Blink figures from 2015; do not generalise to WebKit.
- Confidence: medium-high.

### 1.9 Chrome DevTools "Analyze CSS selector performance during Recalculate Style events" (Selector Stats)
- Author/role: Chrome DevTools docs. URL: https://developer.chrome.com/docs/devtools/performance/selector-stats
- Date: not shown. READ.
- Claims: off by default (overhead); columns Elapsed, Match Attempts, Match Count, % slow-path non-matches; prioritise high elapsed, many attempts with low match count, high slow-path share; re-record to confirm.
- Rule: use it on the scenario that shows the forced recalc; do not tune from it alone (Lawson, entry 3.4: custom properties or stylesheet count can be the real cost).
- Caveats: Chrome-only; no WebKit equivalent found.
- Confidence: high.

### 1.10 "Benchmarking the performance of CSS @property" (inheritance cost)
- Author/role: Bramus (Bram Van Damme), Chrome DevRel, web.dev blog. URL: https://web.dev/blog/at-property-performance
- Date: 2024-10-02. READ.
- Claims (Chromium, M1 Pro, about 7000 elements): changing an inherited property on `:root` (unregistered custom property): 256 runs/s (about 3.9 ms); registered `inherits:true`: 252 runs/s (about 3.96 ms); registering adds about 0.06 ms; `inherits:false` custom property: about 4.67 microseconds per run (848% faster); `accent-color` 6.13 ms; registering 25 000 properties cost about 32 ms once at initial style recalc. Quote: "If you can register your custom property with `inherits: false`, you definitely should."
- Rule: register `--header-inset`-style variables with `inherits: false` when children do not need them, or write them on the consumer. MEASURED on WebKitGTK 2.52.6: root write with a registered non-inheriting property = 0 ms (probe.html "rootWriteRegisteredNoInherit").
- Caveats: Chromium-only benchmarks; WebKitGTK confirmed by my probe, not by the article. Custom property animation is main-thread in Chromium (crbug 1411864).
- Confidence: high.

### 1.11 PR "Workflow canvas resize: scoping CSS-variable writes" (simstudioai/sim #5738)
- Author/role: waleedlatif1 (application developer); merged to `staging` 2026-07-17. URL: https://github.com/simstudioai/sim/pull/5738
- READ (self-reported, one app).
- Claims: dragging panels wrote a CSS variable to `document.documentElement`; with about 42k elements each write recalculated the whole tree at about 77 ms regardless of whether anything read the variable; fix writes the variable on the consuming element during the drag and commits to `:root` and the store on release; per-write subtree recalc about 0.5 ms (about 150x less); panel frame p95/max 91.6/108 ms -> 8.4/8.8 ms; terminal recalcStyle 3899 -> 16 ms over a 50-move drag; traces captured with CDP + Playwright, rAF frame timing.
- Rule: same as rule 1; this is the closest published precedent to DevKit's `--header-inset` and panel-drag variables. Note DevKit's `shellDrag` already handles resize cost (smoothness.mdx 6.7); the variable-write scope is the missing piece.
- Caveats: self-reported, near-empty canvas, Chrome tooling. My MEASURED WebKitGTK table confirms the mechanism on this engine.
- Confidence: medium-high.

### 1.12 FastDOM
- Author/role: Wilson Page, library README. URL: https://github.com/wilsonpage/fastdom
- Date: copyright 2016; no release/commit date visible in the capture. READ.
- Claims: batches DOM reads (`measure`) before writes (`mutate`) at the next rAF (about 600 bytes gz); `fastdom-strict` throws when sensitive properties are read/written in the wrong phase (recommended for dev only); no benchmarks given; maintenance status not established from the page.
- Rule: do not add the dependency; copy the idea: one `measure` queue and one `mutate` queue per frame inside DevKit's own frame scheduler, with a dev-only strict mode that warns on reads in the mutate phase.
- Caveats: legacy library, unclear maintenance.
- Confidence: medium.

---------------------------------------------------------------------------------------------------

## 2. Event loop, scheduling, off-main-thread

### 2.1 HTML Standard: event loop processing model and "update the rendering"
- Author/role: WHATWG. URL: https://html.spec.whatwg.org/multipage/webappapis.html#event-loop-processing-model
- Date: living standard (read 2026-10-21). READ (chars 200 000-300 000).
- Claims: per rendering opportunity the order is: reveal/resize/scroll/media-query/Web Animations steps, **rAF callbacks**, then **style recalculation and layout (loop that also handles `content-visibility:auto` proximity checks and ResizeObserver)**, focus fixup, view transitions, **IntersectionObserver**, record timing and paint, update the rendering, top-layer removals. Rendering only happens if the user agent has a rendering opportunity (able to present).
- Rule: ResizeObserver callbacks run after layout and may force another layout (loop); IntersectionObserver runs late: do not use either to drive layout-affecting writes per frame; rAF callbacks run before style/layout, so reads there are forced reads.
- Caveats: spec order; WebKit followed it for rAF only since 2019 (entry 2.3).
- Confidence: high.

### 2.2 "Tasks, microtasks, queues and schedules" (+ "In the Loop" talk)
- Author/role: Jake Archibald, then Chrome DevRel. URL: https://jakearchibald.com/2015/tasks-microtasks-queues-and-schedules/ ; talk transposed by a third party at https://www.andreaverlicchi.eu/blog/jake-archibald-in-the-loop-jsconf-asia-talk-transposed/ (the YouTube page itself returned no transcript).
- Date: 2015-08-17 (post). Talk: JSConf.Asia 2018 (third-party text, not the speaker's own). READ (both).
- Claims: browser "may render updates" between tasks, never between microtasks; a microtask loop blocks rendering like `while(true)`; rendering is skipped if nothing changed or the tab is hidden and is synced to the display; rAF callbacks run inside the render steps before style/layout; a `setTimeout` loop yields between iterations, a `while` loop does not; promise callbacks are microtasks; in 2015 Safari 8 ran some promise callbacks as tasks (WebKit bug 147933, since fixed).
- Rule: yielding must cross a task boundary (macrotask or `scheduler.yield`); `await`ing already-resolved promises does not yield to rendering; microtask-heavy store flushes (Zustand/React) can starve a frame.
- Caveats: Safari findings are from 2015.
- Confidence: high for the model, medium for third-party transposition.

### 2.3 "Accurately measuring layout on the web"
- Author/role: Nolan Lawson (ex-Microsoft Edge, Mozilla; Pinafore). URL: https://nolanlawson.com/2018/09/25/accurately-measuring-layout-on-the-web/
- Date: 2018-09-25; updates Aug 2019 and Oct 2022. READ.
- Claims: to time rendering from userland put a mark before the JS and another after style/layout/paint; use `requestAnimationFrame` then `setTimeout` (rAF + timeout) as the cross-browser "after frame" callback; per spec rAF fires before style/layout (Chrome, Firefox, Edge 18+); Safari fired it after style/layout and **changed in 2019 to match**; double rAF over-counts idle; microtask fires too early; `requestIdleCallback` can be delayed arbitrarily on a busy thread; forced layout moves style/layout cost into JS without changing total cost.
- Rule: the harness for DevKit (appendix A) uses rAF -> `setTimeout(0)`/MessageChannel for "frame done", not microtasks or rIC. MEASURED: WebKitGTK 2.52.6 rAF idle interval median 17 ms, max 23 ms (Chrome 16.7).
- Caveats: the "after frame" point is an approximation; paint/composite are not measurable from JS.
- Confidence: high.

### 2.4 "Is postMessage slow?"
- Author/role: Surma, Chrome DevRel. URL: https://surma.dev/things/is-postmessage-slow/
- Date: 2019-07-15 (2018 MacBook Pro, Pixel 3XL, Nokia 2). READ.
- Claims: `postMessage` = serialise on sender (blocking) + deserialise on receiver (Chrome and Safari defer until `.data` is read; Firefox before dispatch); transfer time tracks JSON size; up to about 100 KiB stays inside the 100 ms budget even on the slowest devices, about 10 KiB is safe for 16 ms animations; JSON.stringify/parse vs structured clone: no clear winner; Transferable ArrayBuffers are size-independent; patches/chunking for big state; SAB was off everywhere but desktop Chrome then; "`postMessage()` does have a cost, but not the extent that it makes off-main-thread architectures unviable". Timer clamp: Chrome desktop 5 us, Firefox and Safari 1 ms.
- Rule: worker contracts: results as patches or chunks, under 100 KiB per message; big text goes as a transferred buffer.
- Caveats: 2019 hardware and engines; the ratios hold, absolutes drift.
- Confidence: high.

### 2.5 "When should you be using Web Workers?"
- Author/role: Surma. URL: https://surma.dev/things/when-workers/
- Date: 2019-06-14. READ.
- Claims: the main thread also runs layout, paint and input, so long JS = jank; low-end devices have stagnated and "the median is falling"; PROXX on a Nokia 2 froze for up to about 6.6 s on the main thread but its event handler ran in about 48 ms with the logic in a worker; recommends an off-main-thread default (actor-style, UI thread forwards messages), chunking as partial measure; Comlink is about 1.2 KiB gz and wraps `postMessage`.
- Rule: DevKit already moves domain logic to Go; the rest (markdown/highlight/Mermaid/diff/JSON) is the worker candidate set (matches frontend-performance.mdx section 6). Use Comlink-style RPC only if the message protocol grows.
- Caveats: "overworked and underpaid" phrasing is not in the page text I received; I only claim the thesis summarised there ("we should start using workers. A lot.").
- Confidence: high.

### 2.6 "Optimize long tasks"
- Author/role: web.dev / Chrome DevRel (author not shown in fetched text). URL: https://web.dev/articles/optimize-long-tasks
- Date: 2022-09-30, updated 2024-12-19. READ.
- Claims: long task = over 50 ms, blocking time = duration - 50; `setTimeout(fn,0)` goes to the back of the queue and nested timers clamp to 5 ms after 5 levels; `scheduler.yield()` continues ahead of other queued tasks; **"We no longer recommend using [isInputPending]"** (can wrongly return false; animations and UI updates also need yielding); fallback: `scheduler.yield` else `setTimeout`; batch jobs and yield only when 50 ms elapsed since last yield; page badges: `scheduler.yield` Chrome 129/Edge 129/Firefox 142/Safari no; `isInputPending` Chrome 87/Edge 87 only.
- Rule: yield helper in rule 4; deadline slices of 5-8 ms for DevKit (not 50) because WebKitGTK is 2x slower and the target is frame-level smoothness, not INP.
- Caveats: the 50 ms figure is an INP/long-task threshold, not a frame budget.
- Confidence: high.

### 2.7 "Use `scheduler.yield()` to break up long tasks"
- Author/role: Brendan Kenny, Chrome DevRel, developer.chrome.com. URL: https://developer.chrome.com/blog/use-scheduler-yield
- Date: 2025-03-06. READ.
- Claims: after `await scheduler.yield()` the continuation runs ahead of other already-queued tasks, whereas a `setTimeout` continuation waits behind them (example: second half waits for a slow third-party job); inside a background `postTask` it still runs behind default/high priority tasks; fallback is `setTimeout` or skipping the yield; `scheduler-polyfill` covers `postTask`/`yield` without priority inheritance; prose says "only available in Chromium-based browsers", table lists Firefox 142 (internally inconsistent page).
- Rule: use `scheduler.yield` when present (Windows WebView2) and a MessageChannel yield otherwise; do not depend on the priority-continuation behaviour for correctness.
- Caveats: MEASURED: absent in WebKitGTK 2.52.6; BCD (READ): Safari false, Firefox 142, Chrome 129; I found no WebKit statement of intent in the fetched sources.
- Confidence: high.

### 2.8 BCD: `Scheduler`, `Scheduling`, `PerformanceLongAnimationFrameTiming`, `PerformanceLongTaskTiming`, `PerformanceEventTiming`
- Author/role: MDN browser-compat-data (GitHub raw JSON). URLs: https://raw.githubusercontent.com/mdn/browser-compat-data/main/api/Scheduler.json (and the sibling files).
- Date: fetched 2026-10-21. READ.
- Claims: `Scheduler`/`postTask` Chrome 94, Firefox 142, Safari false; `yield` Chrome 129, Firefox 142, Safari false; `isInputPending` Chrome 87 only; LoAF Chrome 123 only (`paintTime`/`presentationTime` Chrome 145); long tasks Chrome 58 only; Event Timing Chrome 76, Firefox 89, Safari 26.2 (`interactionId`: Safari 26.2).
- Rule: matrix 0.2; confirmed by running in WebKitGTK.
- Caveats: BCD describes Safari, not WebKitGTK; WebKitGTK may lag or lead (here: Event Timing present in 2.52.6, consistent with Safari 26.2).
- Confidence: high.

### 2.9 "Faster input events with Facebook's first browser API contribution" (isInputPending)
- Author/role: Engineering at Meta (Facebook). URL: https://engineering.fb.com/2019/04/22/developer-tools/isinputpending-api/
- Date: 2019-04-22, updated 2020-11-19. READ.
- Claims: input queueing delay was among the biggest contributors to slow interactions; the trade-off between big JS blocks (delay input) and frequent yielding (slows load); `navigator.scheduling.isInputPending()` cheaply reads Chrome's compositor-side input queue; origin trial in Chrome 74, shipped Chrome 87; no quantitative results published in the post.
- Rule: do not build on it; web.dev now recommends against (entry 2.6). The relevant idea (input beats background work) is already DevKit policy (smoothness.mdx Figma section).
- Caveats: Chrome-only (V: absent in WebKitGTK).
- Confidence: high.

### 2.10 React scheduler source (`packages/scheduler`)
- Author/role: React team, Meta. URL: https://raw.githubusercontent.com/facebook/react/main/packages/scheduler/src/forks/Scheduler.js
- Date: main branch, read 2026-10-21. READ.
- Claims: scheduling mechanism order: `setImmediate` (Node/IE), else **`MessageChannel` "because of the 4ms setTimeout clamping"**, else `setTimeout`; yields by elapsed time (`frameInterval`, default from a flag, "yields multiple times per frame", does not align with frames); `requestPaint()` makes it yield when a paint is needed; **no use of `isInputPending`**.
- Rule: DevKit's own yield helper can be the same 10 lines; React's concurrent features (`startTransition`, `useDeferredValue`) already use this path on WebKitGTK.
- Caveats: the default yield interval is a flag value not shown in the file.
- Confidence: high.

### 2.11 "Idle Until Urgent"
- Author/role: Philip Walton, Google engineer. URL: https://philipwalton.com/articles/idle-until-urgent/
- Date: 2018-09-20. READ.
- Claims: defer non-critical init to idle time but run synchronously if needed first (`IdleValue`, `IdleQueue`); real numbers: p99 FID 254 -> 85 ms (-67%), main bundle one 233 ms task -> longest 37 ms; `setTimeout` fallback where `requestIdleCallback` is missing (it was Chrome/Firefox only).
- Rule: on WebKitGTK `requestIdleCallback` is absent (V), so idle work needs a timeout/MessageChannel fallback; keep "idle until urgent" for non-critical module init (e.g. settings panes, MCP status).
- Caveats: 2018 numbers (FID); pattern still valid.
- Confidence: high.

### 2.12 INP and input delay (web.dev)
- Author/role: web.dev. URLs: https://web.dev/articles/inp ; https://web.dev/articles/optimize-input-delay
- Date: INP 2022-05-06, updated 2025-09-02; input delay updated 2023-05-09. READ.
- Claims: INP = latency of click/tap/key interactions through next paint = input delay + processing + presentation delay; good <= 200 ms, poor > 500 ms at p75; scroll/hover/zoom not counted; input delay comes from recurring timers, long tasks and overlapping interactions; fixes: fewer timers, short tasks, debounce + `AbortController`, prefer CSS animations. The fetched INP page does not state browser support.
- Rule: DevKit's targets are tighter than INP (frame level); use INP-style Event Timing (present in WebKitGTK, V) to compare interaction cost between engines.
- Caveats: not a frame metric.
- Confidence: high.

### 2.13 Long Animation Frames API
- Author/role: Chrome DevRel docs. URL: https://developer.chrome.com/docs/web-platform/long-animation-frames
- Date: updated 2024-10-14. READ.
- Claims: LoAF flags frames whose rendering update is delayed beyond 50 ms; fields `scripts` (>5 ms), `blockingDuration`, `renderStart`, `styleAndLayoutStart`, `firstUIEventTimestamp`, and per script `forcedStyleAndLayoutDuration`; observe with `PerformanceObserver({type:'long-animation-frame', buffered:true})`; unlike Long Tasks it captures many small tasks adding up and gives script attribution; Chrome/Edge 123+, not Firefox/Safari.
- Rule: use LoAF for the Chrome/WebView2 runs of the same scenario to attribute JS vs style/layout; it does not exist in WebKitGTK (V), so there the Web Inspector Frames view and the rAF/timer harness take its place.
- Caveats: Chrome-only; thresholds are 50 ms, not 16.7 ms.
- Confidence: high.

### 2.14 Alex Russell: "The Performance Inequality Gap, 2026"
- Author/role: Alex Russell, Infrequently Noted (ex-Chrome, now independent). URL: https://infrequently.org/2025/11/performance-inequality-gap-2026/
- Date: 2025-11-24 (updated 2025-11-28). READ.
- Claims: test devices Galaxy A24 4G (Helio G99) and a Celeron N4500 laptop (HP 14); budgets (critical path, two connections) 3 s: 2.0 MiB total JS-light / 1.2 MiB JS-heavy with about 0.3-0.62 MiB JS; low-end single-core performance still "9x slower than contemporary iPhones", budget-phone CPUs "have not meaningfully improved since 2022"; desktops/laptops are 15-18% of web-capable sales, about 80% of "desktop" are battery laptops/tablets; Edge fleet about 30% low-end, 60% medium, 10% high.
- Rule: DevKit is a desktop app on a local bridge, so network budgets do not apply, but the CPU argument does: test on a low-end Linux laptop-class machine, not only the dev box; a Celeron-class CPU is the right "low" device.
- Caveats: web-delivery oriented; no WebKitGTK data.
- Confidence: high (for the CPU argument), medium for transfer to desktop apps.

### 2.15 Nolan Lawson on CSS cost (style scoping vs shadow DOM; "my talk on CSS runtime performance")
- Author/role: Nolan Lawson. URLs: https://nolanlawson.com/2022/06/22/style-scoping-versus-shadow-dom-which-is-fastest/ ; https://nolanlawson.com/2023/01/17/my-talk-on-css-runtime-performance/
- Date: 2022-06-22 (updates to 2022-10-08); 2023-01-17. READ.
- Claims: 1000 components x 10 elements x 10 rules, median style recalc on M1: Chrome 102 / Firefox 101 / Safari 15.5: scoped classes 277 / 45 / 80 ms; scoped attributes 419 / 54 / **802**; shadow DOM 57 / 67 / 82; unscoped 820 / 190 / 857; concatenating `<style>` into one sheet cut Chrome scoped times to about 44 ms; on a 2014 Mac Mini Chrome separate sheets 717-1070 ms vs about 190 ms concatenated, Safari attributes about 2850 ms; Safari 16 "largely closed" the attribute gap; the 2023 post says to measure with Selector Stats and not avoid `:has` or container queries. His 2026 posts that surfaced in search were opinion pieces (AI and frontend), not performance data.
- Rule: prefer class selectors, one concatenated stylesheet; do not chase selectors blindly.
- Caveats: engine versions are old (Safari 15.5); WebKitGTK 2.52 is newer than Safari 16, so treat attribute selectors as "suspect, measure" not "forbidden".
- Confidence: medium-high.
- Not found: the "Pinafore" and "innerHTML tricks" posts the brief asked for did not come up in two searches; I make no claim from memory about them.

### 2.16 Addy Osmani on main-thread scheduling
- Not found. One search for his work on `scheduler.yield`/`postTask`/long tasks returned only web.dev and Chrome blog pages, no Osmani-authored article. No claim made.

---------------------------------------------------------------------------------------------------

## 3. CSS containment, visibility and scroll anchoring

### 3.1 "content-visibility: the new CSS property that boosts your rendering performance"
- Author/role: web.dev / Chrome DevRel (author not shown in fetched text). URL: https://web.dev/articles/content-visibility
- Date: Baseline "Newly available" 2025-09-15 as shown. READ.
- Claims: `auto` gives layout, style and paint containment and, off-screen, size containment plus no painting/hit-testing; demo "7x rendering performance boost on initial load" (232 ms -> 30 ms in the travel-blog example) and "reduction of 50% or more" expected; `contain-intrinsic-size: auto <len>` remembers the last rendered size; without a suitable intrinsic size the scrollbar jitters; forcing layout in skipped subtrees undermines the benefit; off-screen content stays in the accessibility tree and find-in-page; support Chrome 85, Firefox 125, Safari 18.
- Rule: valid in Chrome and WebView2; **on WebKitGTK 2.52 the same CSS made 4000-sibling lists catastrophically slower** (rule 2: forced layout 377-508 ms, scroll sweep 414-516 ms/frame, vs Chrome mount 69 ms and 16.7 ms frames). Replace with virtualisation; if kept, cap siblings (about 300 fine, 1000 degrades) and add a WebKitGTK perf gate.
- Caveats: article numbers are Chrome. Safari 18 -> 26 had partial `auto` support (find-in-page) per BCD; the Safari 27.0 notes list further content-visibility fixes.
- Confidence: high for Chrome; high for the WebKitGTK counter-evidence because MEASURED.

### 3.2 MDN: CSS containment and `content-visibility`
- Author/role: MDN contributors. URLs: https://developer.mozilla.org/en-US/docs/Web/CSS/CSS_containment/Using_CSS_containment ; https://developer.mozilla.org/en-US/docs/Web/CSS/content-visibility
- Date: current MDN. READ (first 100k chars each).
- Claims: `layout` => independent formatting context, containing block for absolute/fixed descendants, stacking context; `paint` clips to the padding box and lets the browser skip off-screen painting; `size` alone gives little benefit and needs `contain-intrinsic-size`; `style` only scopes counters (and quotes in newer engines); `content` = layout+paint+style ("safe to apply widely"); `strict` adds size; `container-type` applies style+layout containment automatically; `content-visibility:auto` = `contain: content` plus skipping when not relevant (viewport, focus, selection, top layer); `hidden` removes content from find-in-page and focus; `contentvisibilityautostatechange` event.
- Rule: `contain: content` is "safe" semantically (stacking context and containing-block side effects), **not safe performance-wise on WebKitGTK 2.52 when applied to many siblings** (rule 2). `container-type` (container queries) implies layout containment: apply to few containers.
- Caveats: MDN states the benefit as general; my measurements show an engine-specific exception.
- Confidence: high (definitions).

### 3.3 BCD: `content-visibility`, `contain`, `contain-intrinsic-size`, `overflow-anchor`, `scrollbar-gutter`, `will-change`
- Author/role: MDN browser-compat-data. URLs under https://raw.githubusercontent.com/mdn/browser-compat-data/main/css/properties/ . READ.
- Claims: `content-visibility: auto` Chrome 85, Firefox 125, **Safari 26 (partial from 18: skipped content not findable)**; `hidden`/`visible` Safari 18; `contain` Safari 15.4 (`style` full only in 27 for quotes); `contain-intrinsic-size` Safari 17; **`overflow-anchor` Chrome 56, Firefox 66, Safari 27**; `scrollbar-gutter` Safari 18.2; `will-change` Safari 9.1; `@starting-style` Safari 17.5; View Transitions Safari 18; `inert` Safari 15.5; OffscreenCanvas Safari 16.4 (WebGL context 17); SharedArrayBuffer Safari 15.2; COOP Safari 15.2.
- Rule: use as a prior, then probe WebKitGTK (matrix 0.2). The BCD Safari 27 value for `overflow-anchor` agreed with my CSS.supports probe on 2.52.6.
- Caveats: BCD is about Safari; the WebKitGTK port can differ (e.g. `scrollbar-color` shipped in 2.52.3).
- Confidence: high.

### 3.4 WebKit: "WebKit Features for Safari 27.0" (scroll anchoring, containment and perf fixes)
- Author/role: WebKit team blog (webkit.org). URL: https://webkit.org/blog/18325/webkit-features-for-safari-27-0/
- Date: Safari 27.0 (post id 18325, Sept 2026); date not shown in fetched text. READ (chars 0-100 000 and 100 000-147 528).
- Claims: **adds scroll anchoring** (`overflow-anchor` default `auto`); resolved `contain: layout` slow forced layout with sibling formatting contexts (171545381); `IntersectionObserver` O(n^2) fix (172727210); `ResizeObserver` progressive slowdown fix (172718139); fixes for blur filters with large radii (172480480), `:where/:is` selector performance (114904007), `:has()` DOM-manipulation freezes (138431700), dynamic `<img>` insertion (166201075), `<select>` `innerHTML` O(n^2) (174244946); "a 2D canvas element unnecessarily forced a compositing layer" (172864747); gradient rendering rework (174880197); rAF sub-ms timestamps in cross-origin-isolated contexts (176967366); scroll-anchoring crash/jump fixes; Web Inspector Timelines fixes.
- Rule: everything in this list is **missing from WebKitGTK 2.52.6 unless Igalia backported it** (I found no backport evidence: unverified). Write DevKit for the older behaviour.
- Caveats: bug numbers are Apple Radar-style ids, no repro details; "present in 2.52" is inferred from release timing (2.52.0 was March 2026, 27.0 is Sept 2026), not verified for each item. My MEASURED sibling-containment slowdown on 2.52.6 matches the described bug.
- Confidence: high that these were bugs in earlier WebKit; medium that each applies to 2.52.6.

### 3.5 WebKit: "WebKit Features in Safari 18.0", "26.0", "26.4"
- Author/role: WebKit team. URLs: https://webkit.org/blog/16301/webkit-features-in-safari-18-0/ ; https://webkit.org/blog/17333/webkit-features-in-safari-26-0/ ; https://webkit.org/?p=17862 (26.4)
- Date: not shown for these. READ.
- Claims: 18.0: `scrollbar-gutter` has "zero effect" with overlay scrollbars; cross-document View Transitions in 18.2; fixes for transform animations jumping; 26.0: fixes for `content-visibility:auto` findability (141237620), geometry inside `content-visibility:hidden` subtrees, hit testing in skipped subtrees, images in scroll containers load when near the viewport (118706766); `will-change: view-transition-name` creates a stacking context; 26.4: scroll-driven animations "run on the compositor thread, separate from the main thread", "improved drop-shadow and blur effects rendering performance" (169472992), "`animateTransform` animations on hidden elements were triggering full-page rendering updates each frame" (159647563), Layers tab shows real composited layer snapshots.
- Rule: scroll-driven animations (`animation-timeline`) are a viable main-thread-free option on WebKit (V: `animation-timeline: scroll()` supported on 2.52.6) but verify they are compositor-driven on WebKitGTK before relying on it; `scrollbar-gutter` is a no-op where scrollbars are overlay (GNOME overlay scrollbars) - DevKit's note about overlay scrollbars on Linux (`quietsScrollBoxes`) is consistent.
- Caveats: Safari release notes; WebKitGTK 2.52 timing relative to 26.4 is unverified (both March 2026).
- Confidence: medium-high.

### 3.6 `overflow-anchor` / scroll anchoring
- Author/role: MDN guide URL https://developer.mozilla.org/en-US/docs/Web/CSS/CSS_scroll_anchoring/Guide_to_scroll_anchoring returned 404; no MDN prose read.
- What is verified instead: BCD (READ): Chrome 56, Firefox 66, Safari 27; WebKit Safari 27.0 post (READ): "adds scroll anchoring"; MEASURED: `CSS.supports('overflow-anchor','none')` false on WebKitGTK 2.52.6, true on Chrome 155. Safari 27.0 also lists scroll-anchoring fixes (blank pages after dynamic load, negative offsets, jumps), i.e. the first implementation had bugs.
- Rule: rule 11. When 2.54+ (probably includes it) arrives, re-test streaming transcript: Chrome-style anchoring may start fighting DevKit's own compensation; plan `overflow-anchor: none` on the transcript scroller as the explicit contract in both engines.
- Confidence: high (support facts), low (what 2.54 includes: not verified).

---------------------------------------------------------------------------------------------------

## 4. WebKit / WebKitGTK specific

### 4.1 "Graphics improvements in WebKitGTK and WPEWebKit 2.46" (Skia replaces Cairo)
- Author/role: Carlos Garcia Campos, Igalia (WebKitGTK/WPE graphics lead). URL: https://blogs.igalia.com/carlosgc/?p=1029
- Date: 2024-09-27. READ.
- Claims: Skia replaces Cairo as the 2D renderer; GPU rendering is the default, CPU (threaded, as Cairo) stays; dirty-region buffers are rendered as textures on the main thread and the compositor waits on fences; "scaling with a device scale factor above 1 is faster because it runs on the GPU"; **all CSS filters use Skia and are accelerated when possible, "sites using blur filters are no longer slow"**; new setting `WebKitSettings:enable-2d-canvas-acceleration`; `willReadFrequently` forces unaccelerated canvas; damage propagation is experimental; `webkit://gpu` shows the DMA-BUF format; initial Sysprof marks; embedded GPUs are weak so CPU rendering with threads is often better there.
- Rule: on Intel Iris Xe the GPU path is the right one; do not set CPU-render variables in production; `webkit://gpu` is the first place to check which path runs.
- Caveats: no benchmark numbers; no env var names in this post.
- Confidence: high.

### 4.2 "Graphics improvements in WebKitGTK and WPEWebKit after the switch to Skia"
- Author/role: Carlos Garcia Campos, Igalia. URL: https://blogs.igalia.com/carlosgc/?p=1041
- Date: 2025-04-21. READ.
- Claims: explicit fencing for the DMA-BUF renderer unblocks the web process sooner; MSAA (4 samples) in WPE only; accelerated canvas texture passed to the compositor without a copy; **GPU layer painting moved off the main thread (commit 287060)**, 1 worker, later 2 GPU workers on 4+ cores; hybrid CPU+GPU workers; display lists replaced by `SkPicture` (recorded once on main, replayed on workers); layer dirty regions limited to 32 and merged; "record once per layer" instead of per tile (costly with shadows/filters); MotionMark on a Raspberry Pi 4 (July 2024 -> April 2025): paths 375 -> 4256, canvas arcs 140 -> 828, multiply 501 -> 684, leaves 319 -> 471; damage tracking exists but off by default; DMA-BUF tile textures and vblank sync planned.
- Rule: shadows/filters on a large layer are re-recorded for each dirty region, so avoid big blurred/shadowed layers that repaint every frame; threaded GPU painting means paint can overlap JS, so a frame can be "JS-free but paint-heavy" - the existing `frameStall` split ("timer on time, frame late => paint/compositor") is the right test.
- Caveats: numbers are RPi4/WPE, not Intel/GTK; the post names no env vars.
- Confidence: high for architecture, low for transferring numbers.

### 4.3 "Highlights of WebKitGTK 2.50" and WPE 2.50 release post
- Author/role: WebKitGTK and WPE teams (Igalia). URLs: https://webkitgtk.org/2025/11/26/webkitgtk-2.50.html ; https://wpewebkit.org/blog/2025-11-27-wpewebkit-2.50.html
- Date: 2025-11-26/27. READ.
- Claims: threaded rendering now records with `SkPictureRecorder` instead of the non-thread-safe WebCore DisplayList (fixes glitches, allows multithreaded replay); experimental hybrid mode (GPU workers first, CPU when busy); **damage propagation to the system compositor (added 2.48 off by default) is now enabled**; Sysprof marks and counters (frame rate, memory); `WPEScreenSyncObserver` for display-sync pacing (WPE platform API).
- Rule: 2.50/2.52 already include these; nothing to enable. If frame pacing looks wrong on Wayland (fractional scale), damage propagation is one variable to bisect (no documented env var found; unverified).
- Caveats: no scrolling or env var content.
- Confidence: high.

### 4.4 WebKitGTK 2.52.x release notes
- Author/role: WebKitGTK maintainers. URLs: https://www.webkitgtk.org/releases/webkitgtk-2.52.0.tar.xz.news , .../webkitgtk-2.52.2... (via https://webkitgtk.org/2026/04/13/webkitgtk2.52.2-released.html), .../2.52.3..., .../2.52.4...
- Date: 2.52.0 March 2026; 2.52.2 2026-04-13; 2.52.3/2.52.4 later in 2026. READ.
- Claims: 2.52.0: smoother touch scrolling for small movements, `scrollend` emitted correctly after scroll animations; 2.52.2: "Improve handling of real-time threads", "Fix scrollbar rendering glitches visible in some GPU configurations"; 2.52.3: adds `scrollbar-color`; 2.52.4: "Fix painting scrollbars when their width changes", `webkit://gpu` works without libGL/libOpenGL. Nothing on layout/compositing performance. (A search-engine summary of a Fedora update also mentions "faster tile-based rendering" and "improved accelerated 2D canvas performance" between 2.50 and 2.52: SNIPPET only, not verified.)
- Rule: 2.52.6 has all of the above; no 2.52 notes mention fixes for the containment/IntersectionObserver issues in section 3.4.
- Caveats: notes are terse; 2.52.5/2.52.6 notes not found.
- Confidence: high for what is listed; medium for absence claims.

### 4.5 "Skia compositor for WPE WebKit and WebKitGTK" (2.54)
- Author/role: Carlos Garcia Campos, Igalia. URL: https://blogs.igalia.com/carlosgc/2026/09/21/skia-compositor-for-wpe-webkit-and-webkitgtk/
- Date: 2026-09-21. READ.
- Claims: replaces TextureMapper (2010, OpenGL ES, own shaders) with `SkiaCompositingLayer` (Skia Ganesh) in WebKitGTK and WPE **2.54, enabled by default**; goal was modernisation, not benchmark gains; on Raspberry Pi 4: composition suite about 45% faster with GPU rendering, MotionMark 1.3.1 about 35% faster, filters/clipping/mask tests about 3-4x faster; deferred display lists cost about 29% in Suits and 14% in Leaves at 15 fps; filters become `SkColorFilter` without an intermediate surface (blur and drop shadow still need one); blend modes now supported; damage handled to repaint only changed regions; `WEBKIT_USE_SKIA_FOR_COMPOSITION=0` switches back to TextureMapper in 2.54.
- Rule: 2.52.6 (DevKit's current runtime) is on TextureMapper; the compositor is a variable in our "WebKitGTK is 2x slower" gap. Re-run the harness on 2.54 as soon as packaged; do not tell users to upgrade on the strength of RPi numbers alone.
- Caveats: the post does not mention backdrop-filter or scrolling; Raspberry Pi numbers.
- Confidence: high.

### 4.6 WebKitGTK 2.54.0 changes (Fedora advisories, SNIPPET only)
- Source: search-result summaries of Fedora 45 `webkitgtk` update pages (https://linuxsecurity.com/advisories/fedora/webkitgtk-fedora-2026-40db9b80a2). SNIPPET only, not fetched.
- Claims (unverified): 2.54.0 dated 2026-09-16; switches the web-process compositor from TextureMapper to Skia; improved damage tracking now also limits composited areas; GPU atlas creation and replay substitution for batched raster image uploads; cairo 2D option removed; 2.53.1 notes also mention removing cairo.
- Rule: treat as pointers for later verification, not facts.
- Confidence: low.

### 4.7 Environment variables reference (WPE/WebKitGTK)
- Author/role: Adrian Perez de Castro, Igalia. URL: https://people.igalia.com/aperez/Documentation/wpe-webkit/environment-variables.html
- Date: not shown. READ.
- Claims: `WEBKIT_SKIA_CPU_PAINTING_THREADS` (default: half the cores, max 8); `WEBKIT_SKIA_GPU_PAINTING_THREADS` (default 2 if more than 3 cores else 1; `0` disables GPU tile rendering); `WEBKIT_SKIA_ENABLE_CPU_RENDERING` (disallow GPU rendering; setting threads to 0 alone may still allow GPU filters); `WEBKIT_SKIA_USE_LINEAR_TILE_TEXTURES` (may degrade performance); `WPE_DMABUF_BUFFER_FORMAT`, `WPE_DRM_*` (DRM backend); `WEBKIT_DEBUG`; `WEBKIT_INSPECTOR_SERVER`, `WEBKIT_INSPECTOR_HTTP_SERVER`; "behaviour may change between versions". No `WEBKIT_SHOW_FPS` or accelerated-compositing entries there. Related: bug 293012/2.48.3 fixed a crash with `WEBKIT_SKIA_GPU_PAINTING_THREADS=0` (SNIPPET).
- Rule: A/B experiment set for the harness: `WEBKIT_SKIA_GPU_PAINTING_THREADS=1|2|4`, `WEBKIT_SKIA_CPU_PAINTING_THREADS=...`, `WEBKIT_SKIA_ENABLE_CPU_RENDERING=1` (control), `WEBKIT_DISABLE_DMABUF_RENDERER=1` (control; also in `native_webview_linux.go`). Report each as an experiment, not a recommendation; DevKit as an embedder can `Setenv` before GTK starts (it already does for DMABUF).
- Caveats: unstable by the doc's own admission.
- Confidence: high that these exist; low that any is a win on Iris Xe (nothing measured).

### 4.8 DMABUF renderer and its failure modes (Tauri guide, Bugzilla 303811, 259858)
- Source: search-result summaries of https://v2.tauri.app/develop/debug/linux-graphics/ , https://bugs.webkit.org/show_bug.cgi?id=303811 , https://bugs.webkit.org/show_bug.cgi?id=259858. SNIPPET only.
- Claims: `WEBKIT_DISABLE_DMABUF_RENDERER=1` is the common fix for blank windows (mostly NVIDIA) at the cost of the faster path; it is read once at startup; a Bugzilla report says with DMABUF disabled `backdrop-filter: blur()` does not render (RTX 50 setup); `WEBKIT_DISABLE_COMPOSITING_MODE=1` is the last resort.
- Rule: DevKit sets the DMABUF-off variable before GTK comes up (per `native_webview_linux.go`, read locally) and docs say it is on for Intel; if a user reports missing blur or low fps, check which path they run.
- Caveats: not fetched; treat as leads.
- Confidence: low-medium.

### 4.9 WebKit Bugzilla: GTK scrolling and compositing performance bugs
- Author/role: WebKit community (reporter Jeff Fortin; Carlos Garcia Campos, Michael Catanzaro for WebKitGTK). URLs: https://bugs.webkit.org/show_bug.cgi?id=221738 , https://bugs.webkit.org/show_bug.cgi?id=250998 , https://bugs.webkit.org/show_bug.cgi?id=280920 , https://bugs.webkit.org/show_bug.cgi?id=271232 , https://bugs.webkit.org/show_bug.cgi?id=271340
- Date: 221738 reported 2021-02-10, last modified 2025-08-23 (REOPENED); 250998 reported 2023-01-22 (NEW); 280920 reported 2024-10-04 (NEW); 271232 fixed 2024-05-14; 271340 fixed 2024-03-27 (commit 276749@main). READ (all five).
- Claims:
  - 221738: janky scrolling only with async scrolling + accelerated compositing; **disabling `background-attachment: fixed` in the inspector cut CPU about 150% -> 15% and made scrolling nearly smooth** (Aug 2025, Skia, 4K, frame rate 12-18 fps before).
  - 250998: scrolling Apple pages at 4K/2x about 0.5 fps on Intel, Xorg and Wayland, never root-caused; Catanzaro: web process doing heavy OpenGL work.
  - 280920: MiniBrowser on Skia, 4K HiDPI, Intel Kabylake, Wayland, slow scrolling over a large banner; no developer comments, no root cause.
  - 271232: blurred background image behind scrolled content made scrolling stutter on both Cairo and Skia; fixed "with Skia now that blurs are done on the GPU".
  - 271340: `backdrop-filter` makes the nearest suitable ancestor a backdrop root; usually the document element, which adds **an extra full-screen compositing layer** unless it has an opaque background; fixed on trunk.
- Rule: (a) grep for `background-attachment: fixed`; (b) set an opaque background on `html`; (c) HiDPI + Intel + big scrolling surfaces are the known weak combination in WebKitGTK (this is DevKit's exact setup); (d) none of these is "solved", so do not assume a newer version fixes scrolling jank.
- Caveats: individual user reports; two are open without analysis.
- Confidence: medium-high.

### 4.10 Remote Web Inspector for WebKitGTK/WPE
- Author/role: Igalia docs. URL: https://people.igalia.com/aperez/Documentation/wpe-webkit/remote-inspector.html
- Date: not shown. READ.
- Claims: set `WEBKIT_INSPECTOR_SERVER=host:port` (or `WEBKIT_INSPECTOR_HTTP_SERVER`) and enable `WebKitSettings:enable-developer-extras` in the app; connect from a WebKitGTK client with `inspector://host:port` (client version should be equal or newer); with the HTTP server variable the inspector UI can be loaded with `http://` in a browser, and "Chromium and derivatives tend to work well".
- Local fact (READ in Wails v3.0.0-beta.32 source in the module cache, the version DevKit's `go.mod` pins): `WebviewWindowOptions.DevToolsEnabled` exists; Linux code toggles `webkit_settings_set_enable_developer_extras` and `openDevTools` calls `webkit_web_inspector_show`; the GPU policy option has only `Always`/`Never` in WebKitGTK 6 (`ON_DEMAND` removed). DevKit's repo code (grep) does not set `DevToolsEnabled` or the GPU policy.
- Rule: perf builds should enable developer extras and `WEBKIT_INSPECTOR_HTTP_SERVER=127.0.0.1:<port>` so a Chrome tab can host the WebKit Inspector against the real app; keep it out of release builds (Bind loopback only).
- Caveats: older mailing-list reports of a blank inspector window (SNIPPET, 2019); the HTTP-hosted inspector in Chrome is "tends to work", not guaranteed.
- Confidence: medium-high.

### 4.11 WebKit Web Inspector: Timelines tab
- Author/role: WebKit Web Inspector docs. URL: https://webkit.org/web-inspector/timelines-tab/
- Date: not shown. READ.
- Claims: timelines: Screenshots, Network Requests, **Layout & Rendering** (style invalidations and recalculations, layout, paint, composite, with sizes and triggering JS), Media & Animations, **JavaScript & Events** (sampling into call trees), **CPU** (usage per thread, counts of listeners/timers), Memory, JavaScript Allocations; the **Frames view** groups activity by frame with 30 fps (about 33 ms) and 60 fps (about 16 ms) lines, colour-coded JavaScript/Layout/Paint/Other; recording disables breakpoints, code coverage, type profiler and JIT deoptimisation to stay realistic; export/import with Cmd+S; `console.profile`/`profileEnd` start and stop recording.
- Rule: standard capture for a WebKitGTK investigation: Layout & Rendering + JavaScript & Events + CPU + Frames, 10-30 s per scenario, saved as `.json` next to the Chrome trace of the same scenario. The Frames colour split answers "JS vs layout vs paint" without LoAF.
- Caveats: the page does not describe GPU/raster time on Linux; the Web Inspector measures the web process, not the GPU/compositor process.
- Confidence: high.

### 4.12 WebKit Web Inspector: Layers tab
- Author/role: WebKit Web Inspector docs. URL: https://webkit.org/web-inspector/layers-tab/
- Date: not shown. READ.
- Claims: live 3D layer-tree view; per-layer paint count and memory cost; per-layer dimensions and "one or more reasons it was created"; "Reveal in Layers Tab" from Elements; toggles "Show compositing borders" and "Enable paint flashing" (layers flash red when repainted). The 26.4 notes say the tab now shows composited layer snapshots.
- Rule: before and after any change that touches opacity, `will-change`, filters, `inert` or scroll containers, take a Layers snapshot and compare layer count and memory; check that a big pane did not become one huge layer.
- Caveats: this inspects the DOM-level layer tree; WebKitGTK's Skia tiles are a separate layer of the system.
- Confidence: high.

### 4.13 Chrome DevTools Performance reference (for mapping Chrome findings to WebKit)
- Author/role: Chrome DevTools docs. URL: https://developer.chrome.com/docs/devtools/performance/reference
- Date: not shown. READ.
- Claims: tracks CPU/NET/FPS/HEAP, Main (flame chart), Frames (green on time, yellow partially presented, red dropped), Interactions, Layout shifts, Animations, Timings, GPU, Thread Pool (raster); event categories Scripting (yellow), Rendering (purple), Loading, Painting; long tasks flagged with a red triangle. The page does not define Recalculate Style, Layout, Pre-Paint, Commit or Layerize.
- Mapping (my inference from entries 4.11 and 1.7, not verified side by side): Chrome "Recalculate Style" = WebKit "Style Invalidation / Style Recalculation" records; "Layout" = "Layout"; "Paint" = "Paint"; "Composite Layers" = "Composite"; Chrome Frames track = WebKit Frames view; Chrome LoAF/long task = no WebKit equivalent; Chrome Raster/GPU threads = Sysprof, not the Web Inspector.
- Chromium tracing categories: I could not read an authoritative page (the Chromium `debug-janks.md` returned HTTP 503 twice; the `about:tracing` how-to page names no categories). Only snippet-level knowledge: PipelineReporter events in the renderer give per-begin-frame overview, long `FramePresented` = dropped frames, long `WaitForSwap` = GPU-side delay (SNIPPET, search summary of the Chromium doc). No category list claimed.
- Confidence: medium.

### 4.14 Sysprof marks for WebKit
- Author/role: Georges Stavracas (GNOME/Endless; WebKit contributor). URLs: https://feaneron.com/2024/07/12/profiling-a-web-engine/ ; https://feaneron.com/2025/09/08/marks-and-counters-in-sysprof/ ; Igalia periodical https://blogs.igalia.com/webkit/blog/2026/wip-73/ (2026-08-17)
- Date: 2024-07-12; 2025-09-08; 2026-08-17. READ.
- Claims: WebKit embeds libsysprof-capture and reports its tracing macros as Sysprof marks (JavaScript events, layout and rendering events, resources); marks now span DRM vblank, compositor, GTK, WebKit and Mesa so frame timing can be correlated "from the kernel driver through the compositor, GTK, and WebKit's UI and Web processes"; Sysprof can filter the call graph by mark; WebKit counters appear in the counters view; a `webkit-sysprof` toolkit landed in 2026-08 to dump/summarise/plot `.syscap` captures (dump, summary, delta-time histograms). Neither post gives recording commands or env vars.
- Rule: for "paint or compositor" stalls (timer on time, frame late) Sysprof is the only tool that sees the GPU/compositor side on Linux; use it when the Web Inspector shows the web process idle. Needs a Sysprof-enabled WebKit build (stated for stable "once shipped"; whether Ubuntu's 2.52.6 is built with it: unverified).
- Caveats: how to start a capture of the app is not in the fetched pages.
- Confidence: medium.

### 4.15 Wails/GTK facts from the local Wails v3 source (beta.32)
- Source: local files under `~/go/pkg/mod/github.com/wailsapp/wails/v3@v3.0.0-beta.32/pkg/application/` (`linux_cgo.go`, `application_linux.go`). READ.
- Claims: Wails sets `WEBKIT_DISABLE_DMABUF_RENDERER=1` itself only when an NVIDIA GPU is detected and the variable is unset; it sets `webkit_settings_set_hardware_acceleration_policy` to `ALWAYS` by default (`NEVER` selectable via `WebviewGpuPolicyNever`; in WebKitGTK 6 `ON_DEMAND` was removed); developer extras and `webkit_web_inspector_show` are exposed.
- Rule: embedders control only the policy (always/never), env vars before start, developer extras; there is no Wails knob for tile size, painting threads or compositor mode other than environment variables.
- Confidence: high.

### 4.16 My probes of WebKitGTK 2.52.6 (MEASURED)
- Method: `/usr/lib/x86_64-linux-gnu/webkitgtk-6.0/MiniBrowser` (2.52.6, Ubuntu 26.04), Wayland session, window 1000x700, DPR 2; Chrome 155 headless, DPR 1; the page POSTs JSON to a local Python server. Each result is the median of 12-20 in-page repetitions; one run per variant (no repeat across sessions). Timer resolution on WebKitGTK was whole milliseconds.
- Results are in rules 1-3, 5, 6 and tables A.1-A.3 below. Biggest findings: (1) inherited-property writes cost 2x Chrome and scale with tree size, not with consumers; (2) containment/BFC/content-visibility on thousands of siblings is 100x worse than plain on 2.52.6 and normal on Chrome; (3) the scheduling and LoAF API gaps; (4) no scroll anchoring.
- Caveats: MiniBrowser, not the Wails app (no Wails shell CSS, no GTK frameless window, no React); synthetic DOM; GPU path assumed (not confirmed via `webkit://gpu`); not repeated on another GPU; a stray `pkill -x chrome` in my run script may have closed other Chrome windows on this machine (I did not check whether any were open).
- Confidence: high for the relative effects (large, consistent), medium for absolute milliseconds.

---------------------------------------------------------------------------------------------------

## Appendix A: measurement tables and the harness recipe

### A.1 Inherited-property writes (20 000 trivial elements, `padding-left: calc(var(--x,0)*1px)` on each)

| Operation (median ms, 20 reps) | WebKitGTK 2.52.6 | Chrome 155 |
|---|---|---|
| `root.style.setProperty('--x')` + `getComputedStyle(root).getPropertyValue('--x')` | 53 (p90 62) | 27.2 (p90 45.2) |
| same write + `offsetHeight` (forced layout) | 134 (p90 143) | 72.4 (p90 84.8) |
| write of an UNUSED var on root + `offsetHeight` | 53 | 26.5 |
| write on a small leaf element + `getComputedStyle` | 0 | 0 |
| write on root of a registered `inherits:false` property + `offsetHeight` | 0 | 0 |
| `getComputedStyle(root).getPropertyValue` with nothing dirty | 0 | 0.1 |
| idle rAF interval median / max | 17 / 23 | 16.7 / 16.8 |
| idle 10 ms timer lateness median / max | 2 / 2 | 0.2 / 0.6 |

### A.2 Siblings with CSS containment (4000 cards, 8 `<p>` each; forced layout after editing one card; Chrome in parentheses)

| Variant | edit-one forced layout ms | append-one ms | scroll sweep median / p90 ms per frame (frames >20 ms of 39) | mount ms |
|---|---|---|---|---|
| plain | 3 (1.3) | 4 (1.4) | 17 / 17 (0) (16.7) | 493 (358) |
| `contain: layout` | **266** (2.2) | 271 (1.8) | 17 / 17 (1) | 1085 (351) |
| `contain: paint` | **352** (1.6) | 417 | 17 / 17 (0) | 1227 (375) |
| `contain: layout style paint` | **333** (1.4) | 367 | 17 / 17 (0) | 1103 (404) |
| `contain: style` | 3 (1.1) | 3 | 17 / 17 (1) | 564 (421) |
| `isolation: isolate` | 3 (1.5) | 5 | 17 / 18 (1) | 1223 (404) |
| `overflow: hidden` | **318** (1.3) | 319 | 17 / 17 (1) | 1133 (397) |
| `content-visibility:auto` + `contain-intrinsic-size: 0 175px` | **508** (1.1) | 482 | **516 / 598 (38)** (16.7) | 1472 (**69**) |
| `content-visibility:auto` + `contain-intrinsic-size: auto 175px` | **377** (1.1) | 427 | **414 / 493 (38)** (16.7) | 1346 (79) |
| `will-change: transform` per card | 4 (1.8) | 6 | **49 / 51 (39 of 39)** (16.7) | 610 (365) |

Scaling (WebKitGTK, forced layout after one-card edit, ms): `contain:layout` N=100: 1; 300: 2; 1000: 7; 2000: 46; 4000: 266. Plain: 1 / 1 / 1 / 2 / 3. `content-visibility:auto` (fixed intrinsic size): N=100 sweep p90 17; 300: 17; 1000: 26 (7 of 39 frames over 20 ms), edit 12 ms. `will-change` N=100-1000: no sweep penalty (17 ms), 4000: 49 ms.

Interpretation limits: cards are 600 px wide, about 175 px tall, text only; the Safari 27 note says the bug depends on "all siblings creating their own formatting context" (which `overflow:hidden`, `contain:layout|paint` and cv:auto all do); `contain: style` and `isolation` do not, matching my results.

### A.3 Reproducible frame-time harness (what to build, using only APIs present in WebKitGTK 2.52)

1. Scenario driver in page: scripted actions (stream N tokens/s into the transcript, toggle pane, full-range scroll sweep in 40-120 steps, mount module) so runs are repeatable.
2. Per-frame sampling: `requestAnimationFrame` timestamps -> intervals; report median, p90, p99, max, and count over 20 ms / 33 ms (the project's 29% figure is "over 20 ms").
3. Second clock: a 10 ms `setTimeout` loop recording lateness (JS/layout holding the main thread) as `frameStall.ts` already does: late timer => main thread; on-time timer but late frame => paint/compositor/GPU (then use Sysprof).
4. "After frame" mark: rAF then `setTimeout(0)` or MessageChannel (Lawson), with `performance.mark/measure` around each step.
5. Event Timing: `PerformanceObserver({type:'event', durationThreshold:16, buffered:true})` (present in WebKitGTK) for input-to-next-paint style numbers.
6. Chrome-only extras on the same scenario: LoAF (with `forcedStyleAndLayoutDuration`), `longtask`, Selector Stats trace.
7. WebKitGTK-only extras: remote Web Inspector Timelines (Layout & Rendering, JavaScript & Events, CPU, Frames) and Layers tab; `webkit://gpu`; Sysprof marks.
8. Environment capture in every report: WebKitGTK/WebView2/WKWebView version, GPU path, DPR, display scale, window size, `WEBKIT_*` variables set.
9. Aggregation over many iterations because timer resolution on WebKitGTK is 1 ms.
10. Store p50/p90/max per engine; gate on ratios (WebKitGTK/Chrome) and absolute p90, because absolute numbers differ by machine.
The probe pages in the scratchpad (`probe.html`, `probe3.html`, `srv3.py`) are working examples of items 1-4 and 9 and can be turned into the harness skeleton.

### A.4 Things I could not verify (explicit)
- Whether any of the Safari 27 fixes (sibling containment, IntersectionObserver, ResizeObserver) were backported into 2.52.6: not found in the 2.52.x release notes I read; the sibling-containment slowdown reproduces on 2.52.6.
- Whether 2.54 includes scroll anchoring or the containment fix: not checked.
- SharedArrayBuffer under COOP/COEP in a Wails app: not tested.
- Whether WebKitGTK's `requestIdleCallback`/`scheduler` flags can be enabled by an embedder: not tested (BCD says Safari has `requestIdleCallback` behind a flag).
- Whether Ubuntu's WebKitGTK is built with Sysprof marks, and how to start a Sysprof capture of the app: not found in fetched pages.
- Any effect of `WEBKIT_SKIA_*` variables, display scale or `GDK_SCALE` on DevKit's frame times: nothing measured.
- Addy Osmani material, Nolan Lawson's Pinafore/innerHTML posts, the Chromium tracing category list, MDN scroll-anchoring guide: not found or fetch failed (404/503), no claims made.
- Pre-existing project measurements (Chrome 96% busy, WebKitGTK 29% frames over 20 ms) were taken as given; I did not reproduce them.
