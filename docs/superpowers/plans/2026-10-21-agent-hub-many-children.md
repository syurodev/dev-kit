# Agent Hub rail — many child sessions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A branch of the Agent Hub rail draws at most `max(3, running children)` children (plus the one on screen / pinned) and folds the rest into a `+ N more · Show all` row.

**Architecture:** A pure `visibleChildren` in `sidebar/tree.ts` decides which children are drawn. `SessionBranch.tsx` gets a small `BranchChildren` component (holds the "show all" state, so folding the branch forgets it) and a new `BranchMore.tsx` row. Signals, counts and Needs you are untouched.

**Tech Stack:** React 19, TypeScript, Tailwind, `bun test` (SSR via `renderToString`; no DOM library), lucide-react.

Spec: `docs/superpowers/specs/2026-10-21-agent-hub-many-children-design.md`.

All paths are relative to the repo root. Frontend commands run from `frontend/`. Branch: `feat/agent-hub-many-children-rail` (already created).

---

## File structure

- Modify `frontend/src/modules/agenthub/sidebar/tree.ts` — `MIN_VISIBLE_CHILDREN`, `visibleChildren` (pure).
- Modify `frontend/src/modules/agenthub/sidebar/tree.test.ts` — tests for it.
- Create `frontend/src/modules/agenthub/sidebar/BranchMore.tsx` — the row.
- Modify `frontend/src/modules/agenthub/sidebar/SessionBranch.tsx` — `BranchChildren`, `Guides` row height, `initialShowAll` on the context.
- Modify `frontend/src/modules/agenthub/sidebar/AgentSessionSidebar.test.tsx` — rendering tests.
- Modify `frontend/src/dev/AgentSessionSidebarSection.tsx` — "many children" fixture and rail.
- Modify `AGENTS.md`, the spec — notes.

---

### Task 1: `visibleChildren` (pure rule)

**Files:**
- Modify: `frontend/src/modules/agenthub/sidebar/tree.ts` (after `heldBranches`, ~line 162)
- Test: `frontend/src/modules/agenthub/sidebar/tree.test.ts`

- [ ] **Step 1: Write the failing tests**

In `tree.test.ts`, add `MIN_VISIBLE_CHILDREN` and `visibleChildren` to the import list from `"./tree"`, then append:

```ts
describe("visibleChildren", () => {
  /** The children of one root, newest first (buildForest orders them). */
  const kids = (n: number, over: (i: number) => Partial<AgentSession> = () => ({})) =>
    buildForest([
      session("root", { updatedAt: 1000 }),
      ...Array.from({ length: n }, (_, i) => kid(`k${i}`, "root", { updatedAt: 100 - i, ...over(i) })),
    ])[0].children;
  const ids = (nodes: readonly SessionNode[]) => nodes.map((n) => n.session.id);

  it("keeps the three newest of eleven idle children and hides the rest", () => {
    const { shown, hidden } = visibleChildren(kids(11));
    expect(MIN_VISIBLE_CHILDREN).toBe(3);
    expect(ids(shown)).toEqual(["k0", "k1", "k2"]);
    expect(ids(hidden)).toEqual(["k3", "k4", "k5", "k6", "k7", "k8", "k9", "k10"]);
  });

  it("shows every running child when more than three run", () => {
    const { shown, hidden } = visibleChildren(kids(11, (i) => ({ status: i >= 5 && i < 10 ? "running" : "idle" })));
    expect(ids(shown)).toEqual(["k5", "k6", "k7", "k8", "k9"]);
    expect(hidden).toHaveLength(6);
  });

  it("fills up to three with the newest idle ones when fewer than three run", () => {
    const one = visibleChildren(kids(11, (i) => ({ status: i === 7 ? "running" : "idle" })));
    expect(ids(one.shown)).toEqual(["k0", "k1", "k7"]);
    const two = visibleChildren(kids(5, (i) => ({ status: i === 3 || i === 4 ? "running" : "idle" })));
    expect(ids(two.shown)).toEqual(["k0", "k3", "k4"]);
  });

  it("draws the session on screen or a pinned one though it is old, going past the limit", () => {
    const active = visibleChildren(kids(11), { activeId: "k9" });
    expect(ids(active.shown)).toEqual(["k0", "k1", "k2", "k9"]);
    const pinned = visibleChildren(kids(11), { pinned: new Set(["k8", "k10"]) });
    expect(ids(pinned.shown)).toEqual(["k0", "k1", "k2", "k8", "k10"]);
  });

  it("draws the child above a grandchild that is on screen", () => {
    const children = buildForest([
      session("root", { updatedAt: 1000 }),
      ...Array.from({ length: 6 }, (_, i) => kid(`k${i}`, "root", { updatedAt: 100 - i })),
      kid("deep", "k5", { updatedAt: 1, depth: 2 }),
    ])[0].children;
    expect(ids(visibleChildren(children, { activeId: "deep" }).shown)).toEqual(["k0", "k1", "k2", "k5"]);
  });

  it("hides nothing when there are no more children than the limit", () => {
    for (const n of [0, 1, 3]) {
      const { shown, hidden } = visibleChildren(kids(n));
      expect(shown).toHaveLength(n);
      expect(hidden).toHaveLength(0);
    }
    // Four children, one running: limit is still three, so one is hidden.
    expect(visibleChildren(kids(4, (i) => ({ status: i === 0 ? "running" : "idle" }))).hidden).toHaveLength(1);
    // Four running: limit is four, nothing hidden.
    expect(visibleChildren(kids(4, () => ({ status: "running" }))).hidden).toHaveLength(0);
  });

  it("keeps the input order in both lists", () => {
    const children = kids(8, (i) => ({ status: i === 6 ? "running" : "idle" }));
    const { shown, hidden } = visibleChildren(children);
    expect(ids(shown)).toEqual(["k0", "k1", "k6"]);
    expect(ids(hidden)).toEqual(["k2", "k3", "k4", "k5", "k7"]);
  });
});
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `cd frontend && bun test --isolate src/modules/agenthub/sidebar/tree.test.ts`
Expected: FAIL — `visibleChildren` / `MIN_VISIBLE_CHILDREN` are not exported by `./tree`.

- [ ] **Step 3: Implement**

In `tree.ts`, add after `heldBranches`:

```ts
/** A branch always draws at least this many of its children (more when more are running). */
export const MIN_VISIBLE_CHILDREN = 3;

/**
 * Which children of one node a branch draws and which fold into its "more" row.
 *
 * The limit is the larger of MIN_VISIBLE_CHILDREN and the number of running
 * children. Every running child is drawn, and so is any child that is, or holds
 * in its subtree, the session on screen or a pinned one: those are exceptions
 * and may take the list past the limit. What is left of the limit goes to the
 * newest of the rest (`children` arrives newest first, as buildForest sorts it).
 * Both lists keep that order. Level-agnostic: it takes any node's children.
 */
export function visibleChildren(
  children: readonly SessionNode[],
  options: { activeId?: string; pinned?: ReadonlySet<string> } = {},
): { shown: SessionNode[]; hidden: SessionNode[] } {
  const { activeId, pinned } = options;
  const running = (node: SessionNode) => node.session.status === "running";
  const limit = Math.max(MIN_VISIBLE_CHILDREN, children.filter(running).length);
  if (children.length <= limit) return { shown: [...children], hidden: [] };

  const holds = (node: SessionNode) =>
    flattenNode(node).some((n) => n.session.id === activeId || (pinned?.has(n.session.id) ?? false));
  const chosen = new Set<SessionNode>(children.filter((node) => running(node) || holds(node)));
  for (const node of children) {
    if (chosen.size >= limit) break;
    chosen.add(node);
  }
  return {
    shown: children.filter((node) => chosen.has(node)),
    hidden: children.filter((node) => !chosen.has(node)),
  };
}
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `cd frontend && bun test --isolate src/modules/agenthub/sidebar/tree.test.ts`
Expected: PASS (all tests in the file, old and new).

- [ ] **Step 5: Commit**

```bash
git add frontend/src/modules/agenthub/sidebar/tree.ts frontend/src/modules/agenthub/sidebar/tree.test.ts
git commit -m "Agent Hub rail: decide which children of a branch are drawn

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 2: The "more" row and the branch wiring

**Files:**
- Create: `frontend/src/modules/agenthub/sidebar/BranchMore.tsx`
- Modify: `frontend/src/modules/agenthub/sidebar/SessionBranch.tsx`
- Test: `frontend/src/modules/agenthub/sidebar/AgentSessionSidebar.test.tsx`

- [ ] **Step 1: Write the failing tests**

In `AgentSessionSidebar.test.tsx`:

1. Add imports next to the existing ones:

```tsx
import { SessionBranch, type BranchContext } from "./SessionBranch";
import { buildForest } from "./tree";
```

2. Append a new `describe` at the end of the file:

```tsx
describe("a branch with many children", () => {
  /** A root and n idle children, k0 the newest. */
  const family = (n: number, over: (i: number) => Partial<AgentSession> = () => ({})) => [
    session("root", { updatedAt: 1000 }),
    ...Array.from({ length: n }, (_, i) =>
      session(`k${i}`, { parentSessionId: "root", depth: 1, updatedAt: 100 - i, ...over(i) }),
    ),
  ];
  const ctx = (over: Partial<BranchContext> = {}): BranchContext => ({
    pinned: new Set(),
    folded: new Set(),
    held: new Set(),
    subagents: {},
    peerUnread: {},
    signals: {},
    titles: new Map(),
    now: Date.now(),
    ...over,
  });
  /** One tree on its own, so the state a press would set can be given up front. */
  function renderBranch(sessions: AgentSession[], over: Partial<BranchContext> = {}): string {
    const [root] = buildForest(sessions);
    return renderToString(
      <TooltipProvider>
        <SessionActionsContext.Provider value={NO_ACTIONS}>
          <SessionBranch node={root} first={false} last={false} guided={false} ctx={ctx(over)} />
        </SessionActionsContext.Provider>
      </TooltipProvider>,
    );
  }

  it("draws three of eleven idle children and a row that says how many are folded", () => {
    const html = render({ sessions: family(11) });
    expect(cardIds(html)).toEqual(["root", "k0", "k1", "k2"]);
    const more = tagOf(html, "agent-branch-more");
    expect(more).toContain('aria-expanded="false"');
    expect(more).toContain('aria-label="Show 8 more child sessions"');
    expect(more).toMatch(/aria-controls="[^"]+"/);
    expect(html).toContain("+8 more");
    // The parent still counts every child it started.
    expect(cardHtml(html, "root")).toMatch(/data-testid="agent-branch-count">11</);
    expect(buttonDepth(html)).toBe(1);
  });

  it("draws every running child, and no row when nothing is folded", () => {
    const running = family(8, (i) => ({ status: i >= 2 && i < 7 ? "running" : "idle" }));
    const html = render({ sessions: running });
    expect(cardIds(html)).toEqual(["root", "k2", "k3", "k4", "k5", "k6"]);
    expect(html).toContain("agent-branch-more");
    expect(render({ sessions: family(3) })).not.toContain("agent-branch-more");
  });

  it("draws the session on screen though it is old", () => {
    const html = render({ sessions: family(11), activeSessionId: "k9" });
    expect(cardIds(html)).toEqual(["root", "k0", "k1", "k2", "k9"]);
    expect(tagOf(html, "agent-branch-more")).toContain('aria-label="Show 7 more child sessions"');
  });

  it("draws all of them, with a way back, once shown", () => {
    const html = renderBranch(family(11), { initialShowAll: new Set(["root"]) });
    expect(cardIds(html)).toHaveLength(12);
    const more = tagOf(html, "agent-branch-more");
    expect(more).toContain('aria-expanded="true"');
    expect(more).toContain('aria-label="Show fewer child sessions"');
    expect(html).toContain("Show fewer");
  });

  it("marks a folded child that has a message the person has not seen", () => {
    const html = render({ sessions: family(11), peerUnread: { k8: 1, k9: 2 } });
    const dot = tagOf(html, "agent-branch-more-unread");
    expect(dot).toContain('aria-label="2 folded sessions have new messages"');
    // An unread child that is drawn says so itself, so it is not counted here.
    expect(tagOf(render({ sessions: family(11), peerUnread: { k0: 1 } }), "agent-branch-more-unread")).toBe("");
  });

  it("ends the guide line at the row, and the row has an elbow like its siblings", () => {
    const html = render({ sessions: family(11) });
    // Three children and the row: four guides; the last child is not last any more.
    expect(html.match(/data-testid="agent-branch-guide"/g)).toHaveLength(4);
    const rowAt = html.indexOf('data-testid="agent-branch-more-row"');
    const row = html.slice(rowAt, html.indexOf("</button>", rowAt));
    expect(row.match(/border-left/g)?.length).toBe(1); // elbow only: the line ends here
    expect(html.slice(html.indexOf('data-session-id="k2"'), rowAt).match(/border-left/g)?.length).toBe(2); // k2: line + elbow
  });

  it("folding the parent hides the row with the children", () => {
    const html = render({ sessions: family(11), foldedBranchIds: ["root"] });
    expect(cardIds(html)).toEqual(["root"]);
    expect(html).not.toContain("agent-branch-more");
  });

  it("applies at every level", () => {
    const sessions = [
      ...family(1),
      ...Array.from({ length: 6 }, (_, i) => session(`g${i}`, { parentSessionId: "k0", depth: 2, updatedAt: 50 - i })),
    ];
    const html = render({ sessions });
    expect(cardIds(html)).toEqual(["root", "k0", "g0", "g1", "g2"]);
    expect(tagOf(html, "agent-branch-more")).toContain('aria-label="Show 3 more child sessions"');
  });
});
```

Note: `foldedBranchIds`, `peerUnread`, `activeSessionId` are existing props of `AgentSessionSidebar` (used by the tests above in the file).

- [ ] **Step 2: Run the tests to see them fail**

Run: `cd frontend && bun test --isolate src/modules/agenthub/sidebar/AgentSessionSidebar.test.tsx -t "many children"`
Expected: FAIL — `initialShowAll` is not a `BranchContext` field (type error / ignored), all eleven children are drawn, no `agent-branch-more`.

- [ ] **Step 3: Create `BranchMore.tsx`**

```tsx
import { ChevronDown, MessageSquareShare } from "lucide-react";

import { cn } from "@/lib/utils";

/**
 * The last row of a branch that folds some of its children: how many are
 * folded and the way to draw them all. A button of its own (not inside a
 * card), so it nests no button. The row's own `listitem` and guide are drawn
 * by SessionBranch's BranchChildren, which knows the geometry.
 */
export function BranchMore({
  hidden,
  unread,
  open,
  controls,
  onToggle,
}: {
  /** Children folded away; the count the label reads. */
  hidden: number;
  /** Folded sessions that hold a message the person has not seen. */
  unread: number;
  /** Everything is drawn: the row is the way back. */
  open: boolean;
  /** The id of the list this row opens and closes. */
  controls: string;
  onToggle: () => void;
}) {
  return (
    <button
      type="button"
      data-sidebar-nav=""
      data-testid="agent-branch-more"
      aria-expanded={open}
      aria-controls={controls}
      aria-label={open ? "Show fewer child sessions" : `Show ${hidden} more child sessions`}
      onClick={onToggle}
      className="flex h-7 w-full min-w-0 cursor-pointer items-center gap-2 rounded-md px-2 text-left text-body-12 text-text-tertiary outline-none hover:text-text-secondary focus-visible:ring-2 focus-visible:ring-border-focus/50"
    >
      {open ? (
        <span className="shrink-0">Show fewer</span>
      ) : (
        <>
          <span className="shrink-0 tabular-nums">+{hidden} more</span>
          <span aria-hidden="true" className="shrink-0">
            ·
          </span>
          <span className="shrink-0">Show all</span>
        </>
      )}
      {!open && unread > 0 ? (
        <span
          data-testid="agent-branch-more-unread"
          role="img"
          aria-label={unread === 1 ? "1 folded session has new messages" : `${unread} folded sessions have new messages`}
          className="flex shrink-0 items-center gap-0.5 text-caption-11 text-text-secondary tabular-nums"
        >
          <MessageSquareShare aria-hidden="true" className="size-3" />
          {unread > 9 ? "9+" : unread}
        </span>
      ) : null}
      <ChevronDown
        aria-hidden="true"
        className={cn(
          "ml-auto size-3.5 shrink-0 transition-transform duration-(--motion-fast) ease-(--ease-out-strong) motion-reduce:transition-none",
          open && "rotate-180",
        )}
      />
    </button>
  );
}
```

The unit tests expect the aria-label `"2 folded sessions have new messages"`; the markup above produces exactly that.

- [ ] **Step 4: Rework `SessionBranch.tsx`**

4a. Imports: add `BranchMore` and `flattenNode` / `visibleChildren`:

```tsx
import { BranchMore } from "./BranchMore";
import { branchHint, branchOpen, flattenNode, indented, visibleChildren, type BranchSignals, type SessionNode } from "./tree";
```

4b. Add the row height constant beside the others and an optional context field:

```tsx
const MORE_HEIGHT = 28;
```

In `BranchContext`, add:

```tsx
  /** Branches that start with every child drawn (a test or a capture; a person presses the row). */
  initialShowAll?: ReadonlySet<string>;
```

4c. Give `Guides` a row height (default the card's):

```tsx
function Guides({ first, last, height = CARD_HEIGHT }: { first: boolean; last: boolean; height?: number }) {
```

and in the elbow `height: CARD_HEIGHT / 2 - top,` becomes `height: height / 2 - top,`. The `top` line keeps using `CARD_HEIGHT` and `FACE_BOTTOM` (they describe the parent's card above the first child).

4d. Add `BranchChildren` above `SessionBranch`:

```tsx
/**
 * The nested list of one open branch: the children that are drawn and, when
 * some are folded, the row that draws them all. The "all" choice lives here, so
 * folding the branch (which unmounts this) forgets it.
 */
function BranchChildren({
  node,
  listId,
  indent,
  ctx,
}: {
  node: SessionNode;
  listId: string;
  indent: boolean;
  ctx: BranchContext;
}) {
  const [all, setAll] = React.useState(() => ctx.initialShowAll?.has(node.session.id) ?? false);
  const { shown, hidden } = React.useMemo(
    () => visibleChildren(node.children, { activeId: ctx.activeId, pinned: ctx.pinned }),
    [node.children, ctx.activeId, ctx.pinned],
  );
  const folds = hidden.length > 0;
  const drawn = folds && !all ? shown : node.children;
  const unread = hidden.flatMap(flattenNode).filter((n) => (ctx.peerUnread[n.session.id] ?? 0) > 0).length;

  return (
    <div
      id={listId}
      role="list"
      data-testid="agent-branch-list"
      aria-label={`Sessions started by ${ctx.titles.get(node.session.id) ?? "this session"}`}
      className="relative mt-0.5 flex flex-col gap-0.5"
      style={indent ? { paddingLeft: INDENT } : undefined}
    >
      {drawn.map((child, at) => (
        <SessionBranch
          key={child.session.id}
          node={child}
          first={at === 0}
          last={at === drawn.length - 1 && !folds}
          guided={indent}
          ctx={ctx}
        />
      ))}
      {folds ? (
        <div
          role="listitem"
          aria-level={node.level + 2}
          data-testid="agent-branch-more-row"
          className="relative"
        >
          <BranchMore hidden={hidden.length} unread={unread} open={all} controls={listId} onToggle={() => setAll((v) => !v)} />
          {indent ? <Guides first={false} last height={MORE_HEIGHT} /> : null}
        </div>
      ) : null}
    </div>
  );
}
```

4e. In `SessionBranch`, replace the whole `{open ? (<div id={listId} …>…</div>) : null}` block with:

```tsx
      {open ? <BranchChildren node={node} listId={listId} indent={indent} ctx={ctx} /> : null}
```

`SessionBranch` is referenced recursively from `BranchChildren`; function declarations hoist, so no reordering problem.

- [ ] **Step 5: Run the tests**

Run: `cd frontend && bun test --isolate src/modules/agenthub/sidebar/`
Expected: the new `many children` tests PASS. If a pre-existing test in `AgentSessionSidebar.test.tsx` fails because its fixture has more than three children under one parent, fix the *fixture expectation* (that is the new rule), not the component; none is expected, since the existing `family()` fixtures have two children.

If the guide assertion in "ends the guide line at the row" is off by one (markup order differs from what the test slices), print `html` for that case and adjust the slice offsets only; the invariants are: 4 guides, the row's guide has one `border-left` (elbow), `k2`'s has two.

- [ ] **Step 6: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`
Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/modules/agenthub/sidebar/BranchMore.tsx frontend/src/modules/agenthub/sidebar/SessionBranch.tsx frontend/src/modules/agenthub/sidebar/AgentSessionSidebar.test.tsx
git commit -m "Agent Hub rail: fold the older children of a branch into a more row

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Gallery, notes, full checks

**Files:**
- Modify: `frontend/src/dev/AgentSessionSidebarSection.tsx`
- Modify: `AGENTS.md`
- Modify: `docs/superpowers/specs/2026-10-21-agent-hub-many-children-design.md`

- [ ] **Step 1: Gallery fixture**

In `AgentSessionSidebarSection.tsx`, after the `lone2` line (~line 81) add an orchestrator with eleven finished children and, in a second one, four running:

```tsx
  // Many children: eleven finished ones fold to the newest three and a "+8 more" row.
  s("orch", WEB, "Web-app tài chính cá nhân", 14, { workspaceId: "ws-billing" }),
  ...Array.from({ length: 11 }, (_, i) =>
    s(`orch${i}`, WEB, `PF-${String.fromCharCode(65 + i)} · plan task ${i + 1}`, 20 + i * 30, {
      workspaceId: "ws-billing",
      parentSessionId: "orch",
      depth: 1,
    }),
  ),
  // Four running children: the limit follows them (four), so only the two idle ones fold.
  s("crew", WEB, "Parallel migration crew", 15, { workspaceId: "ws-billing" }),
  ...Array.from({ length: 6 }, (_, i) =>
    s(`crew${i}`, WEB, `Migrate module ${i + 1}`, 16 + i * 10, {
      workspaceId: "ws-billing",
      parentSessionId: "crew",
      depth: 1,
      status: i < 4 ? "running" : "idle",
    }),
  ),
```

Add `orch9: 1` to the `peerUnread` object so the row's unread mark shows: change `const peerUnread: Record<string, number> = { fam2: 2, lone2: 1 };` to `{ fam2: 2, lone2: 1, orch9: 1 }`.

Add one rail to the gallery row, after `rail-branches-folded`:

```tsx
        <div data-testid="rail-many-children"><Rail width={280} initialActive="orch0" /></div>
```

Also extend the doc comment at the top of the file with `a parent with eleven finished children folds all but the newest three` (one clause).

Note: the fixture's `WEB` workspace group is long; the new rail scrolls inside its 160-unit-high frame. That is fine, the point is the tree's height.

- [ ] **Step 2: Spec tweak**

In the spec, under "What stays true for hidden children", replace the sentence beginning `The "more" row carries an unread dot when a hidden child has a peer message` with:

```
- The "more" row carries the unread mark (the card's message icon and a count)
  for folded sessions, **including their descendants** (they are folded too),
  that hold a peer message the person has not seen. A folded running child
  cannot exist (rule 3.1), and waiting/failed ones are marked on the parent
  already.
```

(Keep the surrounding bullets.)

- [ ] **Step 3: AGENTS.md note**

In `AGENTS.md`, in the "Agent Hub" section, directly after the paragraph that ends `Gallery: \`?gallery#agenthub-session-rail\` (the "branches-folded" rail shows the folded dots).`, add:

```
  - **A branch draws `max(3, running children)` children, not all of them** (`tree.ts` `visibleChildren`,
    `BranchMore.tsx`, spec `docs/superpowers/specs/2026-10-21-agent-hub-many-children-design.md`). Every running child
    is drawn, then the newest idle ones up to three; the session on screen, a pinned one, and the child above them
    are drawn even past that. The rest fold into one row, `+N more · Show all` (the unread mark counts folded
    descendants too). Applies at every level; the "all" choice is state of `BranchChildren` and is forgotten when
    the branch is folded. The parent's count, `branchSignals` and Needs you still read every session. Gallery:
    `rail-many-children`.
```

- [ ] **Step 4: Run all frontend checks**

Run:

```bash
cd frontend && bun run test src/modules/agenthub && bun run build
```

Expected: tests PASS; `tsc` and `vite build` succeed. (If `bun run test` rejects a path argument, use `bun test --isolate src/modules/agenthub`.) If the CSS budget check trips, raise the budget per the project's rule (never inline styles).

- [ ] **Step 5: Look at it**

Run: `cd frontend && bun run dev` (Vite only; see `task dev:frontend`, port 9245) and open `http://localhost:9245/?gallery#agenthub-session-rail`. Check `rail-many-children`: the `orch` branch shows three children and `+8 more · Show all` with the message mark; press it → eleven children and `Show fewer`; the guide line ends at the row; `crew` has four running and two idle children, so its limit is four: the four running ones are drawn and the two idle ones fold into `+2 more`. Tab to the row and press Enter/Space. State what you saw, and say so if you could not open a browser.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/dev/AgentSessionSidebarSection.tsx AGENTS.md docs/superpowers/specs/2026-10-21-agent-hub-many-children-design.md
git commit -m "Agent Hub rail: gallery rail and notes for many-children branches

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

## Self-review (done)

- **Spec coverage:** rule and limit → Task 1; row, `Show all`/`Show fewer`, unread mark, guides, every level, fold forgets state → Task 2; hidden children keep signals/count (unchanged code, asserted in Task 2 tests via count and the existing hint tests); gallery and AGENTS note → Task 3; spec tweak for descendants' unread.
- **Placeholders:** none; every step has code or an exact command.
- **Names:** `MIN_VISIBLE_CHILDREN`, `visibleChildren`, `BranchMore`, `BranchChildren`, `initialShowAll`, test ids `agent-branch-more`, `agent-branch-more-row`, `agent-branch-more-unread` are used consistently.
