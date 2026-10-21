<!-- Research notes behind docs/dev-kit/frontend-performance.mdx (section "Kỹ thuật cực hạn từ các chuyên gia") and docs/dev-kit/smoothness.mdx (section 13). Written 2026-10-21 by research agents. READ = source fetched and read; SNIPPET = search summary only; MEASURED = run on this machine. Kept as-is for traceability; the .mdx files hold the conclusions. Raw probe files mentioned here were not kept in the repo. -->

# Cluster A: React rendering, re-renders, state ownership and fine-grained reactivity

Research date: 2026-10-21. Target: DevKit (React 19.2.x with React Compiler 1.0 via babel-plugin-react-compiler, Zustand 5 + useShallow, TanStack Query 5, TanStack Virtual, Radix, framer-motion 13, Vite, WebKitGTK 2.52 / WKWebView).

How to read the evidence tags:
- READ = I fetched the page and extracted claims from the fetched text. (The fetch tool summarises the page, so quotes are short and numbers are only the ones the page gave.)
- SNIPPET = only seen in a search result summary. Not relied on for rules; flagged.
- FAILED = fetch returned 404/403/405; nothing invented.
- "Local observation" = something I grep'd/read in /home/ptvu3/Downloads/dev-kit to ground a rule. Not part of any source.

Existing docs already say (do not duplicate): `docs/dev-kit/frontend-performance.mdx` (Vietnamese) already mandates: split components by update frequency; state ownership tiers; normalized store with `ids` + `byId` and per-entity subscription; virtualize past thresholds; bridge payload limits; stream buffer outside React flushed once per frame with permission events bypassing the batch; `startTransition` never around controlled inputs; no blanket memo; Activity cleanup hazard for Notes/Agent Hub; a profiling procedure (production build, React Profiler + WebKit trace, bridge timings); a forbidden-optimisations list; transcript baseline numbers (1000 tokens -> 1 publish/frame, `AgentTurn` identity changes 100000 -> 20, markdown parse 49.99 -> 3.07 ms, 5000 turns -> 11 DOM sections). `docs/dev-kit/smoothness.mdx` already says: first-frame correctness, layout stability, only animate transform/opacity, WebKitGTK opacity-layer and `inert` restyle findings, `useShallow` ratchet (6.1), `useVirtualRows` windowing, `<Activity>` in DatabaseView, `startTransition` used in 1 place, `useDeferredValue` for rail search, `frameStall.ts` probe (rAF + 25 ms timer to separate JS from compositor), WebKit has no `longtask`.
What those docs do NOT contain (this file's added value): the React-team's exact semantics for Activity / transitions / deferred value / uSES (and the traps), compiler coverage verification and its documented limits, why Context cannot be fixed with selectors (RFC #119 history), what signals buy and what cannot transfer to React, TanStack Virtual's `useFlushSync` / `directDomUpdates` (a lever the app is not using), Query structural-sharing cost, measured/independent numbers on the compiler, and a source-quality grading of react-scan / Million / WDYR.

---------------------------------------------------------------------------------------------------

# SYNTHESIS

## Top rules for DevKit, ranked by expected payoff against the measured pain (main-thread React commits + forced style recalcs while scrolling/streaming)

Every rule names the entries (E#) that support it. "Inference" means I derived it from the sources plus local code, it is not stated by a source; verify before relying on it.

1. **Stop committing React on every scroll-driven range change in virtualized lists; try `useFlushSync: false` and, where item layout allows, `directDomUpdates`.**
   - TanStack React adapter defaults `useFlushSync = true`: every `onChange` with `sync` set calls `flushSync(rerender)`; React's own docs say flushSync "can significantly hurt performance". The adapter docs themselves list "performance on lower-end devices during rapid scrolling" as a reason to disable it. `directDomUpdates` writes positions straight to the DOM and only re-renders when the visible index range or `isScrolling` changes. (E51, E22, E2)
   - Local observation: installed `@tanstack/react-virtual` 3.14.9 (virtual-core 3.17.7) contains both options (I read `dist/esm/index.js` lines 1-110); `modules/agenthub/surfaces/TranscriptStream.tsx` calls `useVirtualizer({...})` without either; `components/ui/virtual-rows.tsx`, `tree-view.tsx` and `codegraph/sidebar/SearchTab.tsx` also use it; `tree-view.tsx` has its own `flushSync(() => setPins(next))`.
   - How to verify: A/B in the real WebKitGTK window with `DEVKIT_PERF=1` (frameStall) and `<Profiler>` commit counts per 1000 px of scroll; keep the change only if commits/scroll drop and no visible blank rows or scroll jumps. `directDomUpdates` requires `position:absolute` items and a `containerRef` container, so it does NOT fit `virtual-rows.tsx` (spacer `<tr>` design); it may fit the transcript and the tree. Caveat: dynamic-height transcripts rely on `measureElement`; test jump-to-bottom and scroll anchoring.
2. **Keep the streaming path to "one store publish per frame, then re-render only the tail".** Settled blocks must keep referentially stable props so memoization (compiler or `memo`) skips them. Split markdown into blocks and memoize each block by its raw text; throttle the producer. (E52 AI SDK recipe, E4 Erikson, E7 Kent, E10 compiler docs, E49 Nadia, E20 memo)
   - DevKit already batches per frame and parses incrementally (existing doc). The unverified gap is whether every settled block/turn element is actually compiled (check the React DevTools badge) and receives stable props. A single always-new object/array/function prop (documented in `memo` docs and shown in Nadia's real-app test) re-renders the whole subtree.
   - How to verify: Profiler "Why did this render?" on a settled turn while a later turn streams; expect zero renders of earlier turns.
3. **Subscribe per entity with primitive (or `useShallow`-stable) selector outputs; never carry hot state through Context.** Context re-renders every consumer on any value change; the React team never found a viable context-selector API (RFC #119 still open; 2025 comment: "never found a viable API design"). External stores with selectors are the supported route. (E5 Erikson context, E3 Erikson/Redux selectors, E40 RFC 119, E27 Zustand, E37 Daishi compiler-era)
   - Rule: selectors must return stable references or primitives; `useShallow` only for small arrays/objects of primitives; derive lists inside the store (cached) not in the selector; a per-row component takes an `id` and selects its own record.
   - How to verify: the existing `storeSubscriptionRatchet`; add a dev assertion (react-scan or Profiler) that streaming one session does not render another session's rail rows.
4. **Move state down / lift content up before any memoization.** Structural fixes (colocate, `children` as props, split by update frequency) work with or without the compiler and also reduce the compiler's cache work. (E1 Abramov, E6-E7 Kent, E48 Nadia guide, E20 memo docs)
   - Rule: keystroke/hover/drag/token-tick state belongs in the smallest leaf; a parent that owns a fast-changing value passes `children`/slots in, never re-creates them.
5. **Treat compiler coverage as something to verify per hot component, not as a global guarantee.** The compiler skips what it cannot prove safe, only memoizes components/hooks (not plain functions), does not share memoization across components, and cannot stop Context-driven re-renders. In a 15k-line real app, of 9 noticeable re-render cases 2 were fully fixed, 5 partly, 2 not at all; bail-outs were caused by an external library and by non-memoized object props from React Query. (E10, E11, E12, E49 Nadia, E50 Callstack)
   - Rule: for the 10-20 hottest components (transcript turn, rail row, grid row/cell, tab strip item) confirm the compiled badge in React DevTools, keep `eslint-plugin-react-hooks` compiler rules at error, never delete existing `useMemo`/`useCallback`/`memo` without measuring (docs say removing can change output), and keep manual `useMemo`/`useCallback` where a value is an Effect dependency.
6. **Hidden-but-mounted views must do zero work, and Activity is not free of caveats.** Hidden Activity: `display:none`, Effects destroyed, children STILL re-render on new props at lower priority, state/DOM kept. `frozenWhileHidden` (local) bails the whole subtree out. (E14, E13)
   - Inference to verify: external-store subscriptions made via `useSyncExternalStore` are set up in an effect, so they should be torn down while an Activity is hidden and re-synced on show; confirm with a Profiler capture of a hidden DatabaseView while another tab streams.
   - Rule: use Activity only where Effect cleanup on hide is safe (not Notes autosave / Agent listeners, as the existing doc already says); everything else keep `frozenWhileHidden`-style freezing.
7. **Use `useDeferredValue` / `useTransition` for non-urgent derived rendering, with the correct preconditions.** Deferred value only helps if the heavy child is memoized (compiler counts) and the deferred value is a primitive or created outside render; transitions cannot drive text inputs, must wrap the set call synchronously, and a store update delivered through `useSyncExternalStore` cannot be a transition (React docs; Daishi). (E16, E17, E15, E38)
   - Consequence: Zustand-driven updates are always urgent. To deprioritise a heavy view of streaming data, feed it a deferred copy (`useDeferredValue(selectedValue)`) rather than expecting the store write to be a transition. `startTransition` is used in one place in the app today (smoothness doc, local grep agrees); candidate sites: opening heavy modules, large table set, search filters.
8. **Take the hottest leaf out of React: ref + subscribe + direct DOM write.** For things that change every frame and have no React semantics (streaming tail text cursor, token counters, scroll-linked offsets) update a DOM node directly; React stays the owner of structure. Evidence of the pattern in the ecosystem: Motion values ("without triggering a React re-render", batched to next frame), TanStack `directDomUpdates`, Preact Signals JSX text binding, Lexical's own reconciler outside React. (E47 Motion, E51 TanStack, E30/E31 Preact, E53 Lexical)
   - Cautions: selection/copy/find-in-page and a11y on directly-written text; must be idempotent under StrictMode; keep the canonical content in the store so a remount repaints it; do not do this where text is long-lived (use normal render once settled).
9. **Measure the right thing.** Re-render counts are a proxy; commit duration, main-thread blocking and style/layout time are the metric. Use a production (or `react-dom/profiling`) build, 4x CPU throttle for relative numbers, `<Profiler>` `actualDuration` vs `baseDuration`, and the React 19.2 Performance Tracks where a Chromium trace is available; dev-mode numbers are unreliable. (E18, E19, E21, E26, E9, E6)
   - WebKit caveat: the Performance Tracks are documented for Chrome DevTools. I found no source saying Web Inspector in WebKitGTK shows them; `perfTrace`/`frameStall` plus `<Profiler onRender>` remain the in-WebView tools. Use a Chromium build of the same bundle for the React tracks, then confirm on WebKit.
10. **No effects for derived state or event relays; use `useEffectEvent` for effect-owned callbacks that read latest props.** Effect chains cause an extra render per link ("three unnecessary re-renders of the tree below" in the docs' worst case); notifying a parent from an Effect adds a render pass. `useEffectEvent` stops stream/IPC listeners from re-subscribing on every prop change. (E23, E24)
11. **Do not use `flushSync` in app code; if a library uses it, see rule 1.** It may force Suspense fallbacks, run pending effects, and flush updates outside the callback. (E22, E51)
12. **Remember render cost is O(view size).** Windowing is the only structural fix when the view is big; memoization only prunes. Signals-style systems skip the intermediate tree, memoization cannot ("`useMemo()` can't skip component layers"). So the order is: virtualize > colocate > memoize. (E2, E34, E41)
13. **TanStack Query: keep the defaults, but turn structural sharing off for huge payloads.** Structural sharing keeps unchanged item references and tracked props avoid `isFetching` re-renders; it runs on the `queryFn` result and again on `select`, and "can become a bottleneck with very large datasets" (so grid result sets: `structuralSharing: false` and a stable `select`). (E46)
14. **Do not adopt a signals/observable library across the app; borrow the mechanism where it fits.** Signals-in-React bindings are partial (text-node binding bypasses the component; everything else still re-renders), depend on a Babel transform or per-component hook, and some (Legend `use$`) were incompatible with the Compiler. Zustand selectors + per-row components + direct-DOM leaves capture most of the benefit with no new model. (E30, E31, E36, E28, E27, E37)

## Myths and outdated advice (what older posts say that is wrong or obsolete now)

- "Wrap everything in `memo` / `useCallback`." Obsolete for compiled code; docs: "When you enable React Compiler, you typically don't need `React.memo` anymore." Manual memo remains for Effect deps, non-compiled code, and as a measured escape hatch. (E10, E20)
- "Memoize the Context value and consumers will be fine." It stops provider-ancestor re-renders only; any value change still re-renders every consumer, "can not be prevented with memoization directly" (Nadia), and React never shipped context selectors. (E5, E48, E40)
- "Re-render = bug / re-renders are the cost." Kent: slow renders matter more than re-render counts; Dan/Comeau: re-rendering is cheap for simple components; TkDodo: "Re-renders are a good thing". Fix the slow render first. (E6, E1, E9, E46)
- "Props changing is what causes a re-render." State change causes it; a child re-renders because its parent did. Props only matter for memoized children (shallow compare). (E9, E48, E20)
- "`useDeferredValue` makes the render faster / debounce replacement." It does not speed any render, needs `memo` on the heavy child, has no fixed delay, is interruptible, and always returns the new value inside a transition. (E16)
- "Transitions fix input lag for controlled inputs / store updates." Transitions cannot control text inputs and updates after `await` need re-wrapping; external store updates through `useSyncExternalStore` cannot be marked non-blocking. (E17, E15, E38)
- "`useSyncExternalStore` is just a better subscribe." It trades concurrency for consistency: sync, tearing-free, de-opts transitions/Suspense. Jotai deliberately does not use it for that reason. (E15, E38, E39)
- "The compiler is experimental / React Forget is unreleased." Erikson's 2020 guide (updated 2022) says React Forget was unreleased; 1.0 shipped 2025-10-07 and is stable, used at Meta. (E4, E10)
- "Zustand `equalityFn` / `createWithEqualityFn` for shallow compare." In v5 the doc-recommended route is `useShallow`; selectors that return new arrays/objects need it. (E27) (I could not read the v5 migration page, so the v4 API details are not verified here.)
- "`useSelector` / hooks cannot stop parent-caused renders, use `connect`." Redux-specific 2020 caveat; irrelevant to Zustand + compiler, kept only for the principle that a subscription hook cannot prevent renders caused by its parent. (E4)
- "Virtual DOM is pure overhead, signals always faster." The 2018 post and the 2023 Hevery post contain no benchmarks; Harris himself concludes the vdom is "usually fast enough". Vendor benchmark numbers (Million.js "70% faster than React", paper "133-300%") are synthetic or vendor-run. (E29, E34, E43)
- "Dev-mode Profiler numbers tell you the truth." Dev builds, StrictMode double render and the dev Profiler distort; the docs say to build for production and throttle CPU. (E9, E21, E26)
- "Use index as key / the reconciler will sort it out." Keys tell React items are "conceptually the same"; wrong keys keep stale state and cause updates in place. (E2)
- "Resetting state with an effect is fine." It renders once with stale state; use `key`. (E23)
- "Debounce typed input to fix slowness." Kent: a worse UX; defer the derived work instead. (E7, E16)

---------------------------------------------------------------------------------------------------

# ENTRIES

Format: Title | Author, role | URL | Date | Evidence | Claims | Rule for DevKit | Caveats | Confidence

## Part 1. Rendering model, state ownership (people and posts)

### E1. "Before You memo()"
- Author: Dan Abramov (React core team at the time; creator of Redux). URL: https://overreacted.io/before-you-memo/ . Date: 2021-02-23. Evidence: READ.
- Claims: (1) Two refactors fix many slow renders without memo: "move state down" (put the state in the child that uses it) and "lift content up" (wrapper owns state, receives the rest as `children`; React skips `children` because the element is identical). (2) "They don't replace `memo` or `useMemo`, but they're often good to try first." (3) The main benefit is data flow clarity, "don't really have anything to do with performance, per se". (4) Dev builds can be slower "in extreme cases even by an order of magnitude". (5) Only number: the demo's `ExpensiveTree` busy-waits 100 ms per render; no measured before/after.
- Rule: before touching memoization, check whether the fast-changing state can be moved into a leaf or the slow subtree passed as `children`/slots. In DevKit: a module root that holds draft/hover/drag state next to a grid or transcript is the smell.
- Caveats: pre-Compiler; the pattern still works (compiler memoizes JSX but your structure decides what is even re-evaluated). No measurements, opinion plus demo.
- Confidence: high (principle), n/a (numbers).

### E2. "React as a UI Runtime"
- Author: Dan Abramov. URL: https://overreacted.io/react-as-a-ui-runtime/ . Date: 2019-02-02. Evidence: READ.
- Claims: (1) A parent's setState reconciles its whole child subtree by default because React cannot know whether the child is affected; "React rendering is O(view size) rather than O(model size)". (2) React "intentionally doesn't memoize components by default" because comparing props often costs more. (3) Windowing "significantly cut the view size" is the fix for expensive big views. (4) Updates in one event handler are batched into one render. (5) Keys mark items "conceptually the same"; without them reordered same-type items update in place, causing slow updates and stale input values. (6) Calling the dependency-array skip "often a premature optimization".
- Rule: size of the mounted view is the first-order cost; virtualise before memoize; stable semantic keys always.
- Caveats: 2019, pre-hooks-era details (batching now automatic in 18+ including async). No numbers.
- Confidence: high.

### E3. Deriving Data with Selectors (Redux docs, maintained by Mark Erikson)
- Author: Redux maintainers (Mark Erikson). URL: https://redux.js.org/usage/deriving-data-selectors . Date: living docs (mentions Reselect 5, Dec 2023). Evidence: READ.
- Claims: (1) "Selectors used with `useSelector` will be re-run after every dispatched action, regardless of what section of the Redux root state was actually updated." (2) A selector that always returns a new reference (`filter`/`map`) forces a re-render every time. (3) Do NOT memoize direct lookups (`state.todos[id]`), SHOULD memoize derivations returning new references; memoize derived totals only if used widely/long lists. (4) "Adding a separate selector function for every single field is not a good idea!" (5) Per-item selectors `selectTodoById(state, id)` with the id passed from props. (6) Reselect 5 caches per distinct argument set (weakMapMemoize); v4 had cache size 1 so components evicted each other.
- Rule: translate to Zustand: selectors are cheap but run on every store change, so keep them O(1) lookups; derived collections are computed once in the store (or a cached selector), not in the hook; per-row subscription takes `id`.
- Caveats: Redux/Reselect API specifics do not transfer; Zustand 5 gives infinite loops when a selector returns new objects (see E27).
- Confidence: high.

### E4. A (Mostly) Complete Guide to React Rendering Behavior
- Author: Mark Erikson (Redux maintainer; React-Redux). URL: https://blog.isquaredsoftware.com/2020/05/blogged-answers-a-mostly-complete-guide-to-react-rendering-behavior/ . Date: 2020-05-17, last updated 2022-10. Evidence: READ.
- Claims: (1) "when a parent component renders, React will recursively render all child components inside of it"; children re-render even if props are unchanged. (2) "rendering" is not "updating the DOM"; a render can change nothing. (3) `memo` is "completely useless if the props passed to your component are always different". (4) A provider whose value gets a new reference re-renders every consumer; wrapping children in `React.memo` limits cascades. (5) `useSelector` is a hook so it cannot stop parent-caused renders; `connect` could. (6) Returning the same reference from a state update makes React bail out. (7) Render-phase setState loops cut at 50 attempts.
- Rule: the mental model for reviews: a render is a function call tree, a commit is DOM work; measure both. Used as the reference when explaining why a store subscription hook cannot protect a component whose parent re-renders.
- Caveats: dated; calls React Forget unreleased (now stable, see E10); pre-compiler memo advice partly obsolete. No performance numbers.
- Confidence: high (behaviour), medium (advice).

### E5. Why React Context is Not a "State Management" Tool
- Author: Mark Erikson. URL: https://blog.isquaredsoftware.com/2021/01/context-redux-differences/ . Date: 2021-01-18. Evidence: READ.
- Claims: (1) A new provider value re-renders every consumer, even if it reads part of the data. (2) Quoting Sebastian Markbage: Context "is ready to be used for low frequency unlikely updates (like locale/theme)", "not ready to be used as a replacement for all Flux-like state propagation". (3) Splitting contexts / adding selector components is "just reinventing React-Redux, poorly". (4) React-Redux passes only the store instance through Context and components subscribe to slices. No benchmarks.
- Rule: Context in DevKit only for low-frequency values (theme, workspace id, services); anything that changes per token/tick/keystroke lives in an external store with selectors, and context may carry the store handle only.
- Caveats: opinion from a Redux maintainer (interested party); the mechanism claims match React docs (`memo` page: context changes re-render even memoized consumers).
- Confidence: high.

### E6. Fix the slow render before you fix the re-render
- Author: Kent C. Dodds (educator, ex-PayPal). URL: https://kentcdodds.com/blog/fix-the-slow-render-before-you-fix-the-re-render . Date: 2019-09-09. Evidence: READ.
- Claims: (1) A re-render is React calling your function; it need not touch the DOM. (2) Slow renders (expensive render-phase work) cause freezes more than re-render counts do; "It doesn't matter if 100% of your renders are necessary." (3) Reducing re-render counts while leaving slow renders adds code complexity. (4) Procedure: pick the janky interaction, record a browser performance profile, fix the slowest work, re-profile, only then address re-renders. No timings in the article.
- Rule: in DevKit profiles, rank by self time of render functions and by style/layout in the same task before counting renders; matches the `frameStall` "JS vs compositor" split.
- Caveats: 2019; opinion plus procedure; compiler lowers the cost of avoidable re-renders but not of slow render bodies.
- Confidence: high.

### E7. State Colocation will make your React app faster
- Author: Kent C. Dodds. URL: https://kentcdodds.com/blog/state-colocation-will-make-your-react-app-faster . Date: 2019-09-23. Evidence: READ.
- Claims: "One of the leading causes to slow React applications is global state" that changes often; "Keep your state as close to where it's used as possible"; memo/useMemo/useCallback help but add complexity and still require walking the tree; debouncing input is sometimes needed but gives worse UX; context providers need not be at the root; only truly global state belongs in Redux. Demo uses a 200 ms artificial delay; no benchmarks.
- Rule: mirrors the existing "state ownership tiers" table; adds the review question "does this value really need to be in a store, or can the leaf own it".
- Caveats: opinion + demo; "global state is the cause" is true only if consumers subscribe broadly (E3, E27).
- Confidence: high.

### E8. Application State Management with React
- Author: Kent C. Dodds. URL: https://kentcdodds.com/blog/application-state-management-with-react . Date: 2020-07-21. Evidence: READ.
- Claims: split providers per logical domain; "Don't reach for context too soon!"; all state is either server cache or UI state and they should not be mixed (treat server data as a cache, e.g. react-query); keep state close to where needed (per-page providers help code splitting).
- Rule: supports TanStack Query for bridge-backed data and Zustand only for UI/session state; matches existing doc.
- Caveats: opinion; 2020 pre-React-18.
- Confidence: medium-high.

### E9. Why React Re-Renders
- Author: Josh W. Comeau (educator). URL: https://www.joshwcomeau.com/react/why-react-re-renders/ . Date: 2022-08-16, updated 2025-12-03. Evidence: READ.
- Claims: every re-render starts with a state change, cascades to descendants; "props have nothing to do with re-renders" for unmemoized children; React prefers re-rendering too often over stale UI; memoizing everything is counterproductive (his source for that is Dan's posts, not measurement); context consumers re-render; dev-mode Profiler ms "isn't trustworthy"; test on low-end hardware. No numbers.
- Rule: use as the newcomer-facing explanation in the doc; the "dev timings untrustworthy" line supports measuring production only.
- Caveats: tutorial; no benchmarks.
- Confidence: medium-high.

## Part 2. React team and React 19.x official documentation

### E10. React Compiler: introduction (what it memoizes and does not)
- Author: React team (react.dev). URL: https://react.dev/learn/react-compiler/introduction . Date: living (post-1.0). Evidence: READ.
- Claims: (1) Handles cascading re-renders and expensive calculations "inside components and hooks"; "automatically applies the equivalent of manual memoization... sometimes referred to as 'fine-grained reactivity'". (2) Handles the inline-arrow-function pitfall. (3) Does NOT memoize plain functions that are not components/hooks ("only memoizes React components and hooks, not every function"). (4) Memoization is NOT shared across components/hooks. (5) Works as long as code follows the Rules of React; "still an optional addition". (6) `useMemo`/`useCallback` "can continue to be used... as an escape hatch", main use controlling Effect dependencies; for existing code leave memoization in place ("removing it can change compilation output"). (7) Profile before adding memoization for expensive functions.
- Rule: keep compiler on; do not rip out existing manual memo; hand-memoize (or module-cache) expensive pure functions shared by several components (e.g. markdown parse, syntax highlight, row formatters) because the compiler will not share them; keep `useMemo` for values used as effect deps.
- Caveats: official but self-reported; "fine-grained reactivity" here means memoization granularity, not signals (contrast E34).
- Confidence: high.

### E11. React Compiler 1.0 announcement
- Author: Lauren Tan, Joe Savona, Mofei Zhang (React team/Meta). URL: https://react.dev/blog/2025/10/07/react-compiler-1 . Date: 2025-10-07. Evidence: READ.
- Claims: stable, "battle tested on major apps at Meta". Measured (Meta Quest Store only): initial loads and cross-page navigations improved "by up to 12%", certain interactions "more than 2.5x faster", memory neutral; "your mileage may vary". Compiler can memoize after an early return (manual cannot). Lint rules (`set-state-in-render`, `set-state-in-effect`, `refs`) ship in `eslint-plugin-react-hooks` recommended presets and do not need the compiler installed. Pin exact version when test coverage is weak. Works with React 17+; Expo 54+ and create-vite templates enable it.
- Rule: run the compiler-powered ESLint rules at error severity for `src/`; pin `babel-plugin-react-compiler` exactly (DevKit has `^1.0.0`, a caret range, which the post advises against when coverage is weak); treat "up to 12% / 2.5x" as a single-app vendor number, not a prediction.
- Caveats: first-party numbers, one product, no methodology; Meta's code is written for the compiler.
- Confidence: high (facts), low (generalisation of numbers).

### E12. Debugging the compiler / `"use no memo"` / incremental adoption
- Author: React team. URLs: https://react.dev/learn/react-compiler/debugging , https://react.dev/reference/react-compiler/directives/use-no-memo , https://react.dev/learn/react-compiler/incremental-adoption . Date: living. Evidence: READ.
- Claims: the compiler "safely skips optimization rather than risk changing your app's behavior"; verify with the React DevTools ✨ badge and ESLint; problems that appear after enabling are usually code relying on referential equality for correctness (effects, dep arrays, reference checks); isolate with `"use no memo"` then remove manual memo; the directive is "a temporary debugging tool, not a permanent solution" and must be first in the function body; `compilationMode: 'annotation'` + `"use memo"`, directory `overrides`, and `gating` exist for rollout.
- Rule: add a dev checklist item "hot component shows ✨"; annotate every `"use no memo"` with a reason and a ratchet (count must only go down); a quick audit target is any component that is a streaming/list hot path.
- Caveats: the debugging page says nothing about measuring runtime impact; "bail out" is not a term the pages use. The pages do not list every bail-out reason.
- Confidence: high.

### E13. React 19.2 release post
- Author: The React Team. URL: https://react.dev/blog/2025/10/01/react-19-2 . Date: 2025-10-01. Evidence: READ.
- Claims: `<Activity>` hidden "hides the children, unmounts effects, and defers all updates until React has nothing left to work on", so you can "pre-render and keep rendering hidden parts of the app without impacting the performance of anything visible"; `useEffectEvent` splits event logic out of Effects; Chrome DevTools "Scheduler" and "Components" tracks; Effect Events must not be in the dep array and "You don't need to wrap everything in useEffectEvent"; SSR Suspense reveal batching (CWV-driven); Node streams recommended over Web Streams in Node; `useId` prefix changed to `_r_`. No performance benchmarks given.
- Rule: baseline for the 19.2 features DevKit can use; the tracks require a profiling/dev build (E19).
- Caveats: the SSR parts are irrelevant to a desktop app.
- Confidence: high.

### E14. `<Activity>` reference
- Author: React team. URL: https://react.dev/reference/react/Activity . Date: living. Evidence: READ.
- Claims: hidden = `display:none` + Effects destroyed (subscriptions cleaned up); shown again = state restored and Effects re-created; DOM is preserved (textarea draft survives); "While hidden, children still re-render in response to new props, albeit at a lower priority"; a boundary that starts hidden pre-renders at low priority without mounting Effects; data prefetch works only via Suspense (not inside Effects); hidden text-only children render nothing; media (`video`, `audio`, `iframe`) keeps playing unless paused in an Effect cleanup (use `useLayoutEffect` cleanup); "conceptually, you should think of 'hidden' Activities as being unmounted"; StrictMode surfaces missing cleanups.
- Rule: Activity is a state-keeper, not a render-suppressor: its children still render on prop changes. For tabs kept mounted in DevKit, pair Activity with props that do not change while hidden, or keep `frozenWhileHidden`. Never put non-idempotent Effects (autosave flush, IPC listeners owning transcript) inside one (already in existing doc).
- Caveats: the page does not say how external-store subscriptions behave; my "uSES unsubscribes while hidden" is an inference from "destroys their Effects" (verify with a trace). No numbers on hidden-render cost.
- Confidence: high (documented behaviour), low (store-subscription inference).

### E15. `useSyncExternalStore`
- Author: React team. URL: https://react.dev/reference/react/useSyncExternalStore . Date: living. Evidence: READ.
- Claims: `getSnapshot` must return the same value while the store is unchanged ("The result of `getSnapshot` should be cached"); snapshot must be immutable; compares with `Object.is`; an unstable `subscribe` identity re-subscribes every render; a store mutation during a non-blocking Transition makes React call `getSnapshot` again and "restart the update from scratch... as a blocking update" so every component shows one store version; store changes cannot be marked as transitions and therefore "will trigger the nearest Suspense fallback"; prefer `useState`/`useReducer` when possible.
- Rule: custom stores (and anything wrapped manually) must return cached snapshots and a module-level `subscribe`; heavy views that must not block should read a deferred copy of the selected value (E16); do not suspend render on a store value.
- Caveats: the page does not use the word "tearing"; see E38/E39 for that.
- Confidence: high.

### E16. `useDeferredValue`
- Author: React team. URL: https://react.dev/reference/react/useDeferredValue . Date: living. Evidence: READ.
- Claims: first render with the old value, then a background render with the new one; the background render is interruptible and restarts if the value changes again; no fixed delay; the optimisation "requires `SlowList` to be wrapped in `memo`"; "does not make re-rendering of the SlowList faster", it stops it blocking keystrokes; inside a Transition it always returns the new value; pass primitives or objects created outside render; Effects do not fire until the background render commits; does not prevent extra network requests; without `initialValue` it does not defer the initial render.
- Rule: use for rail/search filters, grid filters and big derived lists fed from a store selection; ensure the consumer is compiled/memoized; do not defer controlled inputs.
- Caveats: with the compiler "wrapped in memo" is satisfied automatically only if the component is compiled and its other props are stable (E49).
- Confidence: high.

### E17. `useTransition`
- Author: React team. URL: https://react.dev/reference/react/useTransition . Date: living. Evidence: READ.
- Claims: transition updates are non-urgent and interruptible (a second click wins); "Transition updates can't be used to control text inputs"; the function must be synchronous and `set` calls after `await` must be re-wrapped in another `startTransition`; transitions avoid hiding already-shown content but will not wait on a nested Suspense boundary; multiple ongoing transitions are currently batched together; `startTransition` has a stable identity.
- Rule: wrap module activation, big collection swaps and filter application; never the controlled input or permission decisions (matches existing doc); wrap post-`await` sets explicitly.
- Caveats: only urgent-vs-not scheduling; it does not reduce the work.
- Confidence: high.

### E18. `<Profiler>` API
- Author: React team. URL: https://react.dev/reference/react/Profiler . Date: living. Evidence: READ.
- Claims: `onRender(id, phase, actualDuration, baseDuration, startTime, commitTime)`; `actualDuration` shows how well the subtree uses memoization (it should drop after mount); `baseDuration` estimates the worst-case unoptimised cost; disabled in production builds by default; each `<Profiler>` adds CPU/memory overhead, use "only when necessary".
- Rule: gate a `<Profiler>` around TranscriptStream, a rail group and a grid body behind `?perf`/`devkitPerf` (using the profiling build) and log `actualDuration/baseDuration` per commit; a low ratio means memoization works, a ratio near 1 means it is not.
- Caveats: needs `react-dom/profiling` in a production-like bundle.
- Confidence: high.

### E19. React Performance Tracks
- Author: React team. URL: https://react.dev/reference/dev-tools/react-performance-tracks . Date: living (19.2). Evidence: READ.
- Claims: Scheduler track has four lanes (Blocking, Transition, Suspense, Idle) and shows Update/Render/Commit/Remaining Effects phases and "Cascading update" (an update scheduled during render makes React possibly discard work and restart); Components track is a flamegraph of render and effect durations; on by default in dev builds, `react-dom/profiling` needed for production-like builds (only Scheduler tracks by default there); effects under 0.05 ms are hidden unless they triggered an update; dev builds show "potential changes in props" on a render entry; overhead means off in production.
- Rule: capture streaming and scrolling in a Chromium build of the profiling bundle and look for Blocking-lane commits per frame and "Cascading update" markers (effects that set state); fix those first.
- Caveats: documented for Chrome DevTools; I found no source confirming WebKit Web Inspector displays them. The page does not define "Blocked" labels.
- Confidence: high (content), low (WebKit applicability).

### E20. `memo` reference (incl. compiler note)
- Author: React team. URL: https://react.dev/reference/react/memo . Date: living. Evidence: READ.
- Claims: `memo` is a performance optimisation, "If your code doesn't work without it, find the underlying problem"; props compared with `Object.is`, so a new object/array/function every render defeats it ("a single value that's 'always new' is enough to break memoization for an entire component"); memoized components still re-render on own state or context change; "When you enable React Compiler, you typically don't need `React.memo` anymore"; the compiler also memoizes intermediate values and expensive computations; recommended principles: accept JSX as `children`, keep state local, keep rendering pure, avoid state-updating Effects, trim Effect deps.
- Rule: use the page's principles as the PR checklist; pass primitives (ids) not objects to list rows; do not hand-add `memo` to compiled code.
- Caveats: none significant.
- Confidence: high.

### E21. `useMemo` reference (expense test and cache caveat)
- Author: React team. URL: https://react.dev/reference/react/useMemo . Date: living. Evidence: READ.
- Claims: a calculation is probably not expensive "unless you're creating or looping over thousands of objects"; measure with `console.time`, "1ms or more" summed per interaction is the memoize threshold; test on a throttled CPU; dev/StrictMode doubles render so use production; `useMemo` does not speed the first render; React may discard the cache (HMR, suspended initial mount, future virtualized-list features); the compiler "reduces the need for manual useMemo calls".
- Rule: put the 1 ms / 4x-throttle test in the module-author checklist; never use `useMemo` for correctness.
- Caveats: guideline, not a law.
- Confidence: high.

### E22. `flushSync` reference
- Author: React team. URL: https://react.dev/reference/react-dom/flushSync (the first URL I tried, /reference/react/flushSync, returned 404). Date: living. Evidence: READ.
- Claims: "can significantly hurt performance. Use sparingly"; "may force pending Suspense boundaries to show their fallback"; "may run pending Effects and synchronously apply any updates they contain"; "may flush updates outside the callback when necessary"; no-op (warning) inside render/effects/lifecycle; use only for third-party integrations needing the DOM synchronously; avoid in Effects (move to an event, microtask is worse).
- Rule: grep-gate `flushSync` (local: only `components/tree-view.tsx` calls it directly; the virtualizer adapter calls it for scroll, see E51); each use needs a comment with the measured reason.
- Caveats: none.
- Confidence: high.

### E23. You Might Not Need an Effect
- Author: React team. URL: https://react.dev/learn/you-might-not-need-an-effect . Date: living. Evidence: READ.
- Claims: derive from props/state during render ("it does an entire render pass with a stale value"); reset state with `key`, not an Effect; subscribe to external stores with `useSyncExternalStore`; chains of Effects re-render between each `set` (worst case "three unnecessary re-renders of the tree below"); notifying a parent from an Effect adds a render pass, update both in one event; `useMemo` "won't make the first render faster".
- Rule: lint for `set-state-in-effect` (E11); in review, any Effect whose job is "when A changes set B" is a bug candidate; transcript/rail components should compute display values in render.
- Caveats: none.
- Confidence: high.

### E24. `useEffectEvent`
- Author: React team. URL: https://react.dev/reference/react/useEffectEvent . Date: living (stable in 19.2). Evidence: READ.
- Claims: Effect Events read the latest props/state without re-synchronising the Effect; only callable from Effects/Effect Events; do not pass to children, do not put in dep arrays; identity intentionally unstable; "Do not use useEffectEvent to avoid specifying dependencies" (hides bugs).
- Rule: bridge-event and stream subscribers (`agenthub:*`) should subscribe once and read latest session/selection via an Effect Event instead of re-subscribing when callbacks change; do not use it as a lint silencer.
- Caveats: not for child callbacks; use compiled functions or `useCallback` there.
- Confidence: high.

### E25. Rules of React
- Author: React team. URL: https://react.dev/reference/rules . Date: living. Evidence: READ.
- Claims: components/hooks must be pure and idempotent; props, state and hook arguments are immutable; values are immutable after being passed to JSX; React calls components; rules of Hooks. The page does not say which rules the compiler depends on; it says following them "allows React to automatically optimize your code".
- Rule: any "mutable ref + subscribe + direct DOM write" escape (rule 8) lives in effects/event handlers, never in render, so the compiler's assumptions hold; run StrictMode in dev.
- Caveats: the compiler-dependence statement in my synthesis comes from E10/E12, not from this page.
- Confidence: high.

### E26. Introducing the React Profiler
- Author: Brian Vaughn (React core team, DevTools author). URL: https://legacy.reactjs.org/blog/2018/09/10/introducing-the-react-profiler.html . Date: 2018-09-10. Evidence: READ.
- Claims: Profiler groups data per commit (render phase vs commit phase), flame chart (width = last render time, colour = time in this commit), ranked chart, component chart (a component's renders across commits, example `List` rendering 11 times), interactions; dev timings differ from production, use the `react-dom/profiling` bundle.
- Rule: learning/reference for how to read the chart; use commit-by-commit stepping while streaming to see which components render in every commit.
- Caveats: 2018 UI; "Why did this render" is a later DevTools feature the article does not describe. Interactions tracing API was experimental and removed later (I did not verify removal).
- Confidence: medium-high.

### E27. Zustand: Prevent rerenders with `useShallow`
- Author: pmndrs/Zustand maintainers (Daishi Kato and others). URL: https://zustand.docs.pmnd.rs/learn/guides/prevent-rerenders-with-use-shallow . Date: living (v5 docs). Evidence: READ (guide); the v5 migration page returned 404 on two URLs (FAILED).
- Claims: a selector re-renders when its output changes by `Object.is`; a computed selector such as `Object.keys(state)` re-renders even when only values changed; wrap in `useShallow` to compare shallowly. Search snippet (not read): v5 selectors that return fresh arrays/objects can loop infinitely.
- Rule: `useShallow` is still needed under the compiler (the compiler memoizes components, it does not alter how a store subscription decides to schedule a render; inference supported by E37); keep the ratchet at 0 destructure-whole-store.
- Caveats: `useShallow` compares one level; nested objects in the result defeat it.
- Confidence: high (docs), medium (compiler-interaction inference).

### E28. Valtio getting started (proxy render optimisation)
- Author: pmndrs/Daishi Kato. URL: https://valtio.dev/docs/introduction/getting-started . Date: living. Evidence: READ (thin).
- Claims: "Valtio shines at render optimization"; `useSnapshot` re-renders the component when the properties it uses change; mutate the proxy, not the snapshot; nested proxies are advanced.
- Rule: only a reference point for per-property tracking; do not introduce (Daishi himself says render optimisation by proxy is conceptually at odds with compile-time memoization, E37).
- Caveats: page gives no mechanism or numbers.
- Confidence: medium.

## Part 3. Fine-grained reactivity and signals

### E29. "Virtual DOM is pure overhead"
- Author: Rich Harris (Svelte creator). URL: https://svelte.dev/blog/virtual-dom-is-pure-overhead . Date: 2018-12-27. Evidence: READ. (This is the origin of the phrase, not Ryan Carniato.)
- Claims: vdom work is in addition to real DOM updates, so it only beats a naive full re-render; vdom is "usually fast enough"; `shouldComponentUpdate`-style opt-outs show the original promise failed; Fiber chunks work but does not reduce it; hooks encourage unnecessary work per render ("death by a thousand cuts"); a compiler can emit direct updates. No benchmarks.
- Rule: opinion/argument only; supports the "per-leaf direct DOM write" idea for hot leaves but not a framework change.
- Caveats: 2018, pre-compiler, no data, from a competing framework's author.
- Confidence: medium (argument), low (as evidence of speed).

### E30. "Introducing Signals" (Preact)
- Author: Marvin Hagemeister and Jason Miller (Preact maintainers). URL: https://preactjs.com/blog/introducing-signals/ . Date: 2022-09-06. Evidence: READ.
- Claims: pass the signal (a stable object holding the value) instead of the value so intermediate components are skipped and only components reading `.value` update; signals can be dropped into JSX where a string goes and update that text directly; Context only updates on the whole value so granular updates are impossible; memo/useMemo are fragile; adds "1.6kB". No benchmark numbers.
- Rule: the one transferable idea is "stable handle, leaf subscribes". In React it maps to: pass a store handle/id, not the changing value, through the tree; leaf reads via selector.
- Caveats: Preact's renderer, not React's; no numbers; vendor blog.
- Confidence: high (mechanism), low (performance claims).

### E31. `@preact/signals-react` README
- Author: Preact team. URL: https://github.com/preactjs/signals/blob/main/packages/react/README.md . Date: living. Evidence: READ.
- Claims: needs the Babel transform (`@preact/signals-react-transform`) or `useSignals()` in each component; passing the signal itself into JSX binds the DOM text node so the surrounding component does not re-render; signals as DOM attributes unsupported in that version; render-prop / getter reads can be missed by the transform (`mode: all` or move the read into its own component); SSR does not track.
- Rule: signals in React reduce renders only for text-node bindings; anything else still goes through React. Not adopting; use as precedent for the leaf-binding pattern (rule 8).
- Caveats: the README does not discuss React versions, concurrent rendering or StrictMode, and I did not check compiler interplay. Treat compatibility with React Compiler 1.0 as unverified.
- Confidence: medium.

### E32. "A Hands-on Introduction to Fine-Grained Reactivity"
- Author: Ryan Carniato (Solid creator). URL: https://dev.to/ryansolid/a-hands-on-introduction-to-fine-grained-reactivity-3ndf . Date: 2021-02-09, edited 2021-07-20. Evidence: READ.
- Claims: signals (getter/setter/value), reactions (effects) auto-subscribe when they read a signal, derivations (memos) are guaranteed in sync; dependencies are rebuilt on every run (so dynamic dependencies work); execution is synchronous and glitch-free, "a hybrid of push and pull"; compiler-based libraries (Svelte, Marko) analyse statically, with less re-execution control but less subscription overhead.
- Rule: conceptual grounding for "subscription granularity = the unit of work". It explains why a store selector (a tracked read of one field) is the closest React analogue.
- Caveats: Solid-specific, no benchmarks.
- Confidence: high (mechanism).

### E33. "Thinking Locally with Signals"
- Author: Ryan Carniato. URL: https://dev.to/this-is-learning/thinking-locally-with-signals-3b7h . Date: 2023-10-13. Evidence: READ.
- Claims: signals are finer-grained than vdom re-renders and guard updates at a smaller scope; signals alone do not remove over-notification from parents ("the impact is generally much smaller", not eliminated); progression "Components -> Hooks -> Signals" only if React's principles (local reasoning, unidirectional flow) survive; the consumer that owns/passes state should decide what is reactive.
- Rule: counter-evidence to over-claiming signals: even Solid's creator says parent re-calls still happen. Do not sell signals as free.
- Caveats: opinion essay; no data.
- Confidence: medium.

### E34. "React Compiler will not solve prop drilling"
- Author: Misko Hevery (Qwik/Angular creator). URL: https://www.builder.io/blog/react-compiler-will-not-solve-prop-drilling . Date: 2023-03-02. Evidence: READ.
- Claims: with a memoizing compiler, state and its setter still need to be passed down (or via context) and "React still needs to render all components between" the owner and the consumer; "`useMemo()` can't skip component layers", it only prunes branches; signals update only the consumer, existing on "a plane independent from the component tree". No benchmarks.
- Rule: justification for putting hot state in an external store read at the leaf, not threaded through intermediate components; also for per-row subscription.
- Caveats: written when the compiler was "React Forget" (unreleased); a competitor's author; no data. Compiler 1.0 memoizes JSX so intermediate components can bail out cheaply, but still get visited.
- Confidence: medium (conceptual).

### E35. "Super Charging Fine-Grained Reactive Performance"
- Author: Milo (milomg; Reactively, Solid contributor). URL: https://dev.to/modderme123/super-charging-fine-grained-reactive-performance-47ph . Date: 2022-12-01, edited 2024-12-17. Evidence: READ.
- Claims: compares dirty-propagation algorithms (graph colouring in Reactively, version numbers in Preact Signals, two-pass counting in MobX, queue + colouring in Solid); charts of updated reactive elements per ms on an M1 (values only in images, not extractable); qualitative ranking only.
- Rule: none directly; relevant only if DevKit ever writes its own derived-state graph; prefer a library.
- Caveats: micro-benchmark of reactive graphs, not UI rendering; no numbers recoverable.
- Confidence: low (for DevKit use).

### E36. Legend-State v3: "Fast" and React API
- Author: Jay Meistrich (Legend). URLs: https://legendapp.com/open-source/state/v3/intro/fast/ , https://legendapp.com/open-source/state/v3/react/react-api/ . Date: living. Evidence: READ.
- Claims (vendor): proxy observables with listeners per node, shallow listeners so parents do not re-render on child change, array optimisations that re-render only changed rows (`optimized` For prop reuses React nodes and can break animations), results charts from the krausest benchmark (images only, no numbers in text); `useValue` re-renders only when its returned value changes; `observer` wraps with a Proxy and one hook; `<Memo>` renders an observable field without re-rendering the parent; `use$` "was not compatible with React Compiler", use `useValue`.
- Rule: a documented example of what fine-grained React bindings look like and of a compiler incompatibility; the per-row re-render idea is replicable with Zustand selectors by id.
- Caveats: vendor claims, no independent benchmark found; benchmark is krausest (synthetic table).
- Confidence: low-medium.

### E37. "Thoughts on State Management Libraries in the React Compiler Era"
- Author: Daishi Kato (creator of Zustand, Jotai, Valtio). URL: https://blog.axlight.com/posts/thoughts-on-state-management-libraries-in-the-react-compiler-era/ . Date: 2025-01-07. Evidence: READ.
- Claims: the compiler gives automatic memoization so manual `useMemo`/`useCallback`/`memo` are not needed; "the compiler doesn't optimize re-renders yet" for Context (if it did, the main problem global-state libraries solve "may disappear"); Zustand's selector render optimisation is a main benefit but external state is the bigger one; Valtio's proxy-based runtime optimisation conceptually conflicts with build-time compilation (Valtio v2 works with the compiler); nothing changes soon.
- Rule: selector-based stores remain justified under the compiler; keep Zustand + selectors; do not expect the compiler to remove store-driven re-renders.
- Caveats: library-author opinion; early 2025; 1.0 compiler now stable, context re-render optimisation still not announced (per the RFC thread, E40).
- Confidence: medium-high.

### E38. "Why useSyncExternalStore Is Not Used in Jotai"
- Author: Daishi Kato. URL: https://dev.to/dai_shi/why-usesyncexternalstore-is-not-used-in-jotai-23h9 . Date: 2023-11-05 (orig. 2023-10-28). Evidence: READ.
- Claims: uSES prevents tearing but its "Sync" means it is incompatible with time slicing, "a de-opt"; Zustand (uSES-based) does not show the expected pending state under `useTransition` and shows the Suspense fallback instead; Jotai prioritises transition behaviour over avoiding temporary tearing; he calls uSES "not a silver bullet".
- Rule: in a Zustand app, assume store-driven updates are urgent and un-interruptible; use `useDeferredValue` on the selected value for heavy children (rule 7).
- Caveats: blog opinion by a maintainer of the alternative approach; React docs (E15) independently confirm the blocking-update behaviour.
- Confidence: high (behaviour), medium (priority judgement).

### E39. "Testing global state libraries with React 18 alpha concurrent rendering" + tearing definition
- Authors: Daishi Kato (2021-06-11, READ: https://dev.to/dai_shi/testing-global-state-libraries-with-react-18-alpha-concurrent-rendering-2a2d) and Rick Hanlon (React team), reactwg/react-18 discussion #69 (2021-07-07, READ: https://github.com/reactwg/react-18/discussions/69). Evidence: READ both.
- Claims: tearing = the UI "has shown multiple values for the same state", possible because concurrent rendering yields mid-render; Daishi: tearing and branching are "subtle behaviors and would not be required in most moderate apps"; only React state passes branching (module-state libraries do not); his results table is an image (not extractable) and he warns "Tests can be wrong". Hanlon's page does not mention uSES.
- Rule: DevKit's stores are module state; accept that transitions will not branch them; no action needed beyond rule 7.
- Caveats: 2021 alpha-era; Hanlon page content as fetched is short.
- Confidence: medium.

### E40. RFC #119 "Context selectors"
- Author: Josh Gnoff (gnoff, React contributor); thread with Sebastian Markbage, Joe Savona, Mark Erikson. URL: https://github.com/reactjs/rfcs/pull/119 . Date: opened 2019-07-08; comments through 2025-08. Evidence: READ.
- Claims: proposed `useContextSelector` (author's testing: about 40 ms with `useContext` vs 4 ms with the selector hook); still open; Erikson summarising in Aug 2025: the team "never found a viable API design for the original context selectors concept"; Joe Savona said the Compiler "essentially" covers the `useMemo` use case, context selectors "can still do too much work", the direction is stores with internal subscription tracking ("highly likely to change"), and asked for benchmarks, "actual data, not opinions". Userland: `dai-shi/use-context-selector`.
- Rule: authoritative reason not to wait for React to fix Context fan-out; hot state belongs in a store (rule 3).
- Caveats: the 40 vs 4 ms is one author's local test; the RFC page is a thread, statuses as of fetch.
- Confidence: high (status), low (numbers).

### E41. Vue "Rendering Mechanism" (compiler-informed vdom) and Vapor Mode 3.6 RC notes
- Author: Vue team (Evan You and contributors). URLs: https://vuejs.org/guide/extras/rendering-mechanism.html (READ, living), https://github.com/vuejs/core/releases/tag/v3.6.0-rc.1 (READ). Evidence: READ.
- Claims: Vue's compiler hoists static nodes, emits patch flags (bitmask of what changed) and flattens a block tree so updates traverse only dynamic descendants; Vapor Mode (3.6 RC) drops the vdom for template-only SFCs, `@vue/reactivity` refactored on alien-signals with "significantly improves performance and memory usage" (no numbers in the notes; a Telegram summary of Evan You's talk gave 14% memory reduction, SNIPPET only), "the same level of performance as Solid and Svelte 5" per third-party benchmarks (unnamed). Limits: no Options API, no `v-memo`; mixing vdom and Vapor has edge cases and pulls in the vdom runtime.
- Rule: shows the two industrial routes: (a) compile-time hints let the vdom skip static work (closest to React Compiler), (b) abandon the vdom. React can only do (a); do not plan around (b).
- Caveats: Vue-specific; vendor-reported; Vapor status is RC.
- Confidence: medium.

## Part 4. Measurement and tooling authors

### E42. react-scan
- Author: Aiden Bai (Million Software). URL: https://raw.githubusercontent.com/aidenybai/react-scan/main/README.md . Date: living. Evidence: READ (README via raw; a separate search summary SNIPPET said it can track "unnecessary renders" = no DOM change, `trackUnnecessaryRenders` off by default; that is not in the README I read).
- Claims: "automatically detects performance issues"; "Requires no code changes"; highlights renders (props compared by reference); `dangerouslyForceRunInProduction` default false and "not recommended"; credits React DevTools, WDYR, Million Lint; recommends a successor "React Doctor" for coding agents. No numbers, no accuracy evidence, no overhead figures.
- Rule: fine as a visual re-render finder in a dev/Chromium build; do not treat render flashes as cost (E6); do not leave it in the packaged app.
- Caveats: marketing README; the unnecessary-render logic is the maintainers' DOM-change heuristic (snippet only) and its overhead is acknowledged there.
- Confidence: medium (as a dev aid), low (as evidence).

### E43. Million.js / "Virtual DOM: Back in Block" / SAC '23 paper
- Author: Aiden Bai. URLs: https://arxiv.org/abs/2202.08409 (READ), talk page (SNIPPET: https://portal.gitnation.org/contents/virtual-dom-back-in-block). Date: arXiv 2022-02-17, v5 2023-01-01; ACM SAC 2023. Evidence: READ abstract; talk SNIPPET.
- Claims: paper abstract: compiler-augmented vdom, "133% to 300% faster" rendering, "2347% faster" load, real app loaded "35.11% faster" after migrating from React (informal user study); benchmark libraries not named in the abstract. Talk (snippet): "30% faster than Preact and over 70% faster than React" on synthetic benchmarks; v2 release called the 70% figure a local benchmark with a pending PR.
- Rule: do not adopt; treat numbers as synthetic/vendor. The block-vdom idea (skip static, diff only dynamic holes) is what React Compiler 1.0 approximates in-tree.
- Caveats: single author, own project; React in those comparisons was the 2022 pre-compiler React; React Compiler 1.0 postdates it. Marketing vs measured: headline percentages are marketing-grade.
- Confidence: low.

### E44. why-did-you-render
- Author: welldone-software. URL: https://github.com/welldone-software/why-did-you-render . Date: living. Evidence: READ.
- Claims: patches React to notify about "potentially avoidable re-renders" (equal props/state); latest tested with React 19 only; hooks tracking on by default; "should NEVER be used in production", "significantly slows down React"; author: "not tested with React Compiler at all" and "completely incompatible" with it.
- Rule: do not use in DevKit (the compiler is on); use the Profiler and DevTools "why did this render" instead.
- Caveats: maintainer statement, not tested.
- Confidence: high.

### E45. React Doctor (Million)
- Author: Million Software. URL: SNIPPET only (mintlify mirrors, https://mintlify.com/millionco/react-doctor); the react-scan README names it as the successor. Evidence: SNIPPET.
- Claims (unverified): CLI with 60+ lint rules plus dead-code scan, 0-100 score. Mirrors disagree on config format.
- Rule: none; optionally trial in CI as a lint-only signal, not as a performance measure.
- Caveats: not read from the primary repo.
- Confidence: low.

### E46. TanStack Query render optimisations
- Author: Dominik Dorfmeister, "TkDodo" (TanStack Query maintainer). URL: https://tkdodo.eu/blog/react-query-render-optimizations . Date: 2021-03-20, updated 2023-10-21. Evidence: READ.
- Claims: "Re-renders are a good thing"; default defaults are strong; a component reading only `data` re-renders on `isFetching` toggles unless `notifyOnChangeProps` is set; tracked properties are on by default from v4 (rest-destructuring defeats tracking; fields read only in effects are not tracked); structural sharing keeps unchanged object references so `select`-based per-item hooks re-render only when their item changes; it runs twice with selectors, "can become a bottleneck with very large datasets", only for JSON-serialisable data, disable with `structuralSharing: false`.
- Rule: never destructure with rest from a query result; use `select` for per-row slices; for DB result grids and big transcript pages set `structuralSharing: false` and pass stable references; confirm the per-query cost in a profile.
- Caveats: v3/v4-era text updated through 2023; v5 behaviour should be checked in current docs before relying on specific option names.
- Confidence: medium-high.

### E47. Motion values (framer-motion / Motion)
- Author: Motion team (Matt Perry). URL: https://motion.dev/docs/react-motion-value . Date: living. Evidence: READ.
- Claims: changes to a motion value update the DOM "without triggering a React re-render", set many times but rendered batched to the next frame; `useTransform` derives values; use motion values instead of React state for visual properties.
- Rule: any pointer-/scroll-/progress-linked visual (tab drag, panel resize veil `--veil`, scroll indicators) should drive a motion value, not state; DevKit already does this for some (smoothness doc: `lib/panelMotion`).
- Caveats: library docs; no numbers.
- Confidence: high.

### E48. React re-renders guide: everything, all at once
- Author: Nadia Makarevich (Developer Way, author of "Advanced React"). URL: https://www.developerway.com/posts/react-re-renders-guide . Date: 2022-08-02. Evidence: READ.
- Claims: Context value change re-renders every consumer, "can not be prevented with memoization directly"; split data and API providers, split independent chunks; `memo` only works if all non-primitive props are memoized, and memoizing a parent does not help if children/props are new each render; "children as props" and "components as props" patterns skip re-renders; unmemoized children re-render with the parent regardless of props. No numbers.
- Rule: use as the recipe set for review (split providers, children-as-props, primitive props).
- Caveats: pre-compiler (the compiler automates the memo parts; the context and composition parts stand).
- Confidence: high.

### E49. How React Compiler Performs on Real Code
- Author: Nadia Makarevich. URL: https://developerway.com/posts/how-react-compiler-performs-on-real-code . Date: 2024-12-04. Evidence: READ.
- Claims: app of about 15k lines TypeScript; "Successfully compiled 361 out of 363 components"; Lighthouse production mobile profile with 4x CPU slowdown, 5 runs averaged; initial load "pretty much identical"; interactions: theme toggle blocking time 280 ms -> 0, card list 130 ms -> 90 ms (and -> 0 after one manual fix; INP "more than halved"); of 9 noticeable re-render cases 2 fully fixed, 5 partly, 2 not at all; not fixed: object props from React Query changing identity on key change, and an external search library that made the compiler bail; conclusion: for most apps enabling it is enough, apps needing max performance still need manual memo and re-render knowledge.
- Rule: independent evidence that the compiler is helpful but incomplete; (1) pass primitives to memoized children, (2) check bail-outs on hot paths (E12), (3) expect stable-identity bugs at library boundaries (Query, Radix, framer).
- Caveats: one app, one author, simulated 4x throttle on Lighthouse; component counts outside DevKit's scale.
- Confidence: medium-high.

### E50. Sanity Studio and Wakelet compiler reports; Callstack
- Authors: Sanity (changelog 3.65.0, 2024-11-26), Wakelet (via secondary reports), Callstack (Zuzanna Kumorek, 2024-08-29). URLs: Sanity https://www.sanity.io/changelog/839f4b18-788b-4e83-b5d9-8cb838a5be9e (SNIPPET), Callstack https://callstack.com/blog/optimizing-performance-with-the-react-compiler (READ), InfoQ https://www.infoq.com/news/2025/12/react-compiler-meta (FAILED 405).
- Claims: Sanity (snippet): 1,231 of 1,411 components compiled, "20%-30% overall reduction in render time and latency", no method given; Wakelet (snippet): INP 275 -> 240 ms (about 15%), about 30% on Radix dropdowns, LCP 2.6 -> 2.4 s; Callstack (read): used Reassure over 50 runs but published no numbers in text and ends with a sales pitch; code that breaks the rules is skipped.
- Rule: use as sanity-check ranges only (single-digit to 30% on interactions in real apps), not as budget numbers.
- Caveats: company self-reports; Sanity/Wakelet not read at source.
- Confidence: low-medium.

## Part 5. Hot-path libraries and "avoid React in the hot path"

### E51. TanStack Virtual: React adapter and virtualizer options
- Author: Tanner Linsley / TanStack maintainers. URLs: https://tanstack.com/virtual/latest/docs/framework/react/react-virtual (READ), https://tanstack.com/virtual/latest/docs/api/virtualizer (READ; the page as fetched did not describe `useFlushSync`), local source `frontend/node_modules/@tanstack/react-virtual/dist/esm/index.js` (READ). Date: living; installed 3.14.9. Evidence: READ.
- Claims: `useFlushSync` (default true) "will use flushSync from react-dom to ensure synchronous rendering during scroll events"; disable it for React 19 (warning "flushSync was called from inside a lifecycle method"), "performance on lower-end devices during rapid scrolling", non-critical lists; `useFlushSync: false` lets React batch naturally. `directDomUpdates` (default false) "skips React re-renders for scroll-only updates" by writing positions and container size to the DOM; re-renders only when the visible range or `isScrolling` changes (confirmed in source); requires absolutely positioned items (and `top:0;left:0` in transform mode), no main-axis styles in the item, and `containerRef` on the inner container; set once at mount; `'transform'` mode creates a stacking context that can break `position: fixed` descendants. `getItemKey` should be memoized; `overscan` default 1, more overscan costs render time; `useAnimationFrameWithResizeObserver` adds roughly 16 ms of delay, use only if measured; `shouldAdjustScrollPositionOnItemSizeChange`, `scrollMargin` for content above the list.
- Rule: see synthesis rule 1. Concretely for the transcript: (a) try `useFlushSync: false` in `TranscriptStream`; (b) evaluate `directDomUpdates` for it (dynamic heights use `measureElement`; verify jump-to-latest and scroll anchoring); (c) memoize `getItemKey` (already stable per the existing doc); (d) keep overscan small and measure.
- Caveats: no numbers published for either flag; `directDomUpdates` is new to this version and I found no independent report on it; the tables use spacer rows so it does not apply to `useVirtualRows`.
- Confidence: high (documented behaviour and source), low (benefit size).

### E52. AI SDK cookbook: Markdown chatbot with memoization; Streamdown
- Author: Vercel AI SDK team. URLs: https://v4.ai-sdk.dev/cookbook/next/markdown-chatbot-with-memoization (READ), https://ai-sdk.dev/cookbook/next/markdown-chatbot-with-memoization (READ; now only describes Streamdown), https://github.com/vercel/streamdown (READ). Date: v4-era recipe (2024-25), current page updated. Evidence: READ.
- Claims: problem stated as "the entire conversation history is re-rendered with each new token"; fix: `marked.lexer(markdown)` splits into block raw strings, each block rendered by a `memo` component whose comparator checks only content equality, `useMemo` keyed on content avoids re-parsing, block keys `${id}-block_${index}`, producer throttled with `experimental_throttle: 50` (ms). Current page: Streamdown "memoizes Markdown blocks so unchanged blocks can skip re-rendering", handles incomplete markdown (`remend`); Streamdown README has no numbers.
- Rule: DevKit already groups and parses incrementally; confirm that rendered settled blocks have no props that change per token (e.g. an inline `onClick`, a new `components` map, a `settled` flag derived from the last block) and that block keys are stable; consider a 30-50 ms floor between publishes for text-only tokens (the per-frame flush is ~16 ms).
- Caveats: cookbook is a recipe, no measurements; the 50 ms is the SDK's choice, not derived; Streamdown claims are unquantified.
- Confidence: medium-high (technique), low (numbers).

### E53. Lexical (Meta): design and intro docs
- Author: Meta (Dominic Gannaway and team; docs by Lexical maintainers). URLs: https://lexical.dev/docs/design , https://lexical.dev/docs/intro , https://lexical.dev/docs/concepts/editor-state . Date: living. Evidence: READ.
- Claims: framework-agnostic core with React bindings in `@lexical/react`; double-buffered state: "never more than two editor states in play", a "pending" state built in the background then swapped; editor state is immutable once committed (enables undo/redo); updates in the same tick are batched and the DOM reconciler patches only the DOM of changed nodes; MutationObserver reverts or interprets outside DOM edits; `discrete: true` forces an immediate commit. The docs make no benchmark claims and do not state "why not React" for performance (my search for a primary statement found none).
- Rule: model for DevKit's hottest editable/streaming surfaces: own the state outside React, mutate a pending copy, reconcile only dirty nodes in the same tick, expose snapshots to React for the rest of the UI. This is the same shape as the existing per-frame transcript buffer.
- Caveats: the performance reasoning is inferred from architecture, not stated; I did not find a Meta post measuring React vs Lexical.
- Confidence: medium.

### E54. Jotai performance guide
- Author: Daishi Kato / pmndrs. URL: https://jotai.org/docs/guides/performance . Date: living. Evidence: READ.
- Claims: each atom re-renders only when its own value changes; `selectAtom`, `focusAtom` and `splitAtom` give per-part and per-list-item subscriptions; for an object that changes almost every second the extra atoms add overhead without benefit.
- Rule: the "per-row atom" idea is `useStore(s => s.byId[id])`; do not split fast-changing, jointly-changing fields into separate subscriptions (the token buffer: one record per session, not one subscription per token).
- Caveats: guide itself says it is incomplete.
- Confidence: medium.

### E55. Solid docs: fine-grained reactivity
- Author: Solid team. URLs: https://docs.solidjs.com/advanced-concepts/fine-grained-reactivity , https://docs.solidjs.com/concepts/components/basics . Date: living. Evidence: READ.
- Claims: "A Solid component is only run once"; updates re-run only the reactive reads, targeting the specific attribute; tracking is synchronous (reads in `setTimeout` are not tracked); contrasts with React re-executing the whole component for a single attribute change.
- Rule: explains the unit-of-work difference to the team; do not port Solid idioms.
- Caveats: vendor docs; no numbers.
- Confidence: high (behaviour).

### E56. React issue #35151 "flushSync flickers briefly (19.2)"
- Author: community report (Eliav2); no maintainer response. URL: https://github.com/facebook/react/issues/35151 . Date: opened 2025-11-15, closed 2026-08-01 by the reporter as "not a React bug" (two copies of react-dom in the demo). Evidence: READ.
- Claims: reported production-only flicker with flushSync in 19.2.0 versus 19.1; reporter later attributed it to a duplicated react-dom install and retracted.
- Rule: no change; kept only as a warning that flushSync behaviour reports exist and that a duplicate `react-dom` in a bundle makes flushSync silently ineffective. Run `bun pm ls react-dom` (or equivalent) in CI to assert one copy.
- Caveats: unconfirmed by maintainers; do not cite as a React bug.
- Confidence: low.

---------------------------------------------------------------------------------------------------

# WHAT CAN AND CANNOT TRANSFER FROM FINE-GRAINED REACTIVITY TO REACT 19

Can transfer (supported by E30-E34, E37, E40, E47, E51, E54):
- Stable handle, subscribing leaf: pass ids/store handles down; leaf selects its own slice (per-row subscriptions). Zustand selectors already implement this.
- Direct DOM writes for a few extreme leaves (motion values, `directDomUpdates`, Preact text binding precedent).
- Subscription granularity as the unit of work; keep derived collections out of selectors.
- Compile-time static hoisting/memoization (Vue's, and React Compiler's analogue).

Cannot transfer:
- "Components run once": React re-executes component functions; the compiler caches outputs but React still visits the tree (E34, E55).
- Automatic dependency tracking inside render without a Babel transform or per-component hook (E31); not compiler-safe in at least one library (E36).
- Transition/branch semantics for module-state stores (E38, E39); uSES updates are urgent.
- Skipping the intermediate components: memoization prunes, it does not skip layers (E34).
- A Vapor-style opt-out of the vdom (E41).

# SOURCES THAT FAILED OR WERE NOT READ (nothing was invented for them)
- react.dev/reference/react/flushSync: 404 (the correct page is /reference/react-dom/flushSync, read as E22).
- Zustand v5 migration page (two URLs, 404) and the pmndrs raw markdown (404): v4->v5 API claims unverified; only the `useShallow` guide was read.
- ryansolid.medium.com VDOM post (403), dev.to "fundamentals of fine-grained reactivity" (404), InfoQ compiler-at-Meta article (405), npm `million` page (403), Sanity "react-compiler-and-sanity-studio" blog (404).
- Ryan Carniato's "Reports of the Virtual DOM's Death are Greatly Exaggerated": only seen as a title in a third-party link list; not read. "Virtual DOM is pure overhead" is Rich Harris's post (E29), not Carniato's.
- Million.js talk, Sanity changelog numbers, Wakelet figures, React Doctor, Evan You's Vue.js Nation talk: SNIPPET only.
- Lexical: no primary source found that states why Lexical avoids React state for performance; E53 is architecture only.
- Sebastian Markbage on signals: no primary source found; only the quote relayed by Erikson (E5).

# LOCAL OBSERVATIONS USED (from the repo, read-only; not research claims)
- `frontend/package.json`: react/react-dom ^19.2.8, babel-plugin-react-compiler ^1.0.0 (caret range), @tanstack/react-virtual ^3.14.9, @tanstack/react-query ^5.102.8, zustand ^5.0.14, framer-motion ^13.1.0; `vite.config.ts` loads `babel-plugin-react-compiler`.
- `@tanstack/react-virtual` 3.14.9 index.js: `useFlushSync = true` default, `directDomUpdates = false` default, `flushSync(rerender)` when `useFlushSync && sync`.
- `modules/agenthub/surfaces/TranscriptStream.tsx` `useVirtualizer({count, getScrollElement, getItemKey, scrollMargin, estimateSize, overscan, paddingStart, initialRect})`, no `useFlushSync`/`directDomUpdates`.
- `components/tree-view.tsx` imports `flushSync` and calls it when `sync` for pins.
- Transitions/deferral in non-test source: `React.startTransition` once (`components/ui/inline-combobox.tsx`); `useDeferredValue` in `modules/tasks/TaskPicker.tsx`, `modules/settings/KeyboardTab.tsx`, `modules/tasks/TasksBoardView.tsx`. No `useTransition`.
- React's `<Activity mode=...>` is NOT found anywhere in `frontend/src` (my grep matched only lucide `<Activity />` icons and a comment). `docs/dev-kit/smoothness.mdx` section 11 says DatabaseView uses `<Activity>`; `modules/database/DatabaseView.tsx` has no such usage now, so that doc line looks stale (worth confirming with the owner). `lib/frozenWhileHidden.ts` documents why Activity is not used for the Agent companion (display:none loses scroll positions).
- `useSyncExternalStore` direct uses: `agenthub/summary/summaryFold.ts`, `components/ui/split-panel.tsx`, `lib/panelMotion.ts` (3 hooks).
