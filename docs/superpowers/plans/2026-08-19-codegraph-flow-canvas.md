# CodeGraph Flow Canvas Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace CodeGraph's plain-list flow view with an interactive graph canvas that renders the API Node / Connector / Edge Label components already designed in Figma.

**Architecture:** React Flow (`@xyflow/react`) hosts the canvas. Nodes are ordinary React components, so the API Node card is built once in Tailwind and matches Figma exactly. Connectors are a custom edge type over `getSmoothStepPath`, which produces the rounded elbow the design uses; dashes, arrowheads and the label pill are edge props. Layout is deliberately split in two: `rank.ts` is pure graph theory and knows nothing about pixels, `place.ts` turns ranks into coordinates using the sizes React Flow **measured** from the real DOM. No card dimension is hardcoded anywhere except in the card's own CSS.

**Tech Stack:** React 19, TypeScript, Tailwind v4, `@xyflow/react@12`, `lucide-react`, `bun test`.

---

## Why React Flow, and why layout stays on the client

Both were decided by measurement and by reading the libraries' own docs. Recorded here so they are not re-litigated from intuition.

**Sigma was evaluated first and rejected.** Sigma's built-in edge programs draw straight lines only and have no dash support, so the design's elbow-and-dash connector language cannot come from them — every connector would have to be hand-painted on a custom 2D layer, and the cards would have to be a DOM overlay anyway (baked textures blur when zoomed). That leaves Sigma contributing only a camera and input captors. Measured with an identical method (`bun build --minify`, react/react-dom/zustand external, gzip -9):

| | raw | gzip |
|---|---|---|
| `@xyflow/react@12.11.3` + `@xyflow/system`, d3-zoom/drag/selection, classcat | 173.2 KiB | 57.0 KiB |
| `sigma@3.0.3` + `graphology` | 157.7 KiB | 37.6 KiB |

Sigma is 19.4 KiB gzip smaller, and that is the honest number. But those 57 KiB replace connector routing, canvas painting, pan/zoom, viewport culling, selection and keyboard a11y — several hundred lines that would otherwise be written and maintained here. Both land in a lazy chunk: `check-performance-budget.mjs` only measures `<script>` and `modulepreload` entries in `dist/index.html`, and the CodeGraph module is already lazy via `lazyModuleView`, so neither option touches the budget. Task 11 verifies that rather than trusting it.

Sigma's one real advantage is node count — WebGL handles tens of thousands where DOM does not. That advantage is unreachable in this view: with `onlyRenderVisibleElements` the DOM count is bounded by viewport area divided by card area, which at a readable zoom is well under two hundred. Keep Sigma in mind for a future whole-project map, where it would be used natively with circles and its own edge programs.

**Layout stays on the client for now.** `frontend-performance.mdx` asks for layout computed outside the render loop, not specifically in Go. `rank.ts` is O(V+E) over a neighbourhood of tens of nodes. Pushing it to Go today buys a bridge round-trip and a schema to maintain, and Go would either have to know the card's pixel size or take it as a parameter — both are the wrong coupling. The split into `rank.ts` (no pixels) and `place.ts` (pixels) is the seam: if a whole-project map lands and ranking thousands of nodes becomes a measured bottleneck, `rank.ts` ports to Go unchanged, because by construction it has nothing to port that Go doesn't already know.

## Source of truth

Every visual number below was read off the real Figma components. Open them and compare — do not invent values.

File: `https://www.figma.com/design/3dOzSZBYY7QtzANFTCfEe8/DevKit`, page **14 CodeGraph**.

| Component | Node ID | Variants |
|---|---|---|
| `API Node` | `162:183` | `Layer` = Controller / Service / Repository / Unknown; `State` = Default / Hover / Selected |
| `Edge Label` | `164:49` | `Kind` = Call / DI |
| `Connector` | `167:79` | `Shape` = Straight / Branch; `Style` = Solid / Dashed; boolean `Label` |
| Assembled screen | `150:46` | `CodeGraph / Graph Canvas` inside App Shell `143:115` |

## Verified repo gaps

Checked against the code, not assumed:

1. **`--surface-tinted` and `--icon-secondary` do not exist in `src/index.css`.** They exist in Figma (`#32292f`, `#c6c8c2`) and the card's inset panel and the connector strokes need them.
2. **`CodeGraphGetNeighbors` is bridged in Go and present in the generated Wails bindings, but `src/bridge/codegraph.ts` has no wrapper.** `getCodeGraphApiFlow` returns a linear `steps[]` with no node id and no `layer`, so it cannot drive a graph.
3. **There are no frontend tests yet.** `bun test` is configured in `package.json` but no `*.test.ts` exists. This plan adds the first ones, for the two pure modules only.

## File structure

```
frontend/src/modules/codegraph/graph/
  tokens.ts          layer -> colour/icon mapping, connector constants
  rank.ts            pure graph ranking (tested, no pixels)
  place.ts           ranks + measured sizes -> coordinates (tested)
  ApiNode.tsx        React Flow custom node
  ConnectorEdge.tsx  React Flow custom edge + the Edge Label pill
  LayerLegend.tsx    floating legend panel
  CanvasToolbar.tsx  search / fit / expand
  FlowCanvas.tsx     ReactFlow host, measure -> place -> fitView
frontend/src/bridge/codegraph.ts                   (modify)
frontend/src/index.css                             (modify)
frontend/src/modules/codegraph/CodeGraphView.tsx   (modify)
```

---

## Task 1: Dependency and missing design tokens

**Files:**
- Modify: `frontend/package.json`
- Modify: `frontend/src/index.css`

- [x] **Step 1: Add React Flow**

```bash
cd frontend && bun add @xyflow/react
```

- [x] **Step 2: Verify it resolves**

Run: `cd frontend && bun -e "import { ReactFlow } from '@xyflow/react'; console.log(typeof ReactFlow)"`
Expected: `function`

- [x] **Step 3: Add the two missing tokens**

In `frontend/src/index.css`, inside `:root`, immediately after the `--surface-glass-modal` line:

```css
    --surface-tinted: #32292f;
```

and immediately after the `--text-tertiary` line:

```css
    --icon-secondary: #c6c8c2;
```

Then in the `@theme` block at the top, beside the existing `--color-surface-raised` declarations:

```css
    --color-surface-tinted: var(--surface-tinted);
    --color-icon-secondary: var(--icon-secondary);
```

`--icon-secondary` deliberately holds the same hex as `--text-secondary`. Keep it separate so connector strokes can be retinted without dragging body text along.

- [x] **Step 4: Verify Tailwind emits them**

Run: `cd frontend && bun run build:dev && grep -c 'surface-tinted' dist/assets/*.css`
Expected: build succeeds, count is non-zero.

- [x] **Step 5: Commit**

```bash
git add frontend/package.json frontend/bun.lock frontend/src/index.css
git commit -m "feat(codegraph): add react flow and the surface-tinted/icon-secondary tokens"
```

---

## Task 2: Bridge wrapper for GetNeighbors

**Files:**
- Modify: `frontend/src/bridge/codegraph.ts`

- [x] **Step 1: Confirm the generated binding exists**

Run: `cd frontend && grep -n 'CodeGraphGetNeighbors' bindings/devkit/internal/bridge/*.ts`
Expected: at least one hit. If there is none, regenerate bindings (`cd .. && wails3 generate bindings`) — never hand-write a binding.

- [x] **Step 2: Read the Go signature so the argument order matches**

Run: `grep -n -A3 'func (s \*AppService) CodeGraphGetNeighbors' ../internal/bridge/codegraph.go`
Note the exact parameter order and adapt Step 3 to it. Go is the source of truth here; do not change Go to fit the wrapper.

- [x] **Step 3: Add the types and wrapper**

Append to `frontend/src/bridge/codegraph.ts`, matching the style of the existing wrappers in that file:

```ts
export type CodeGraphNode = {
  id: string;
  label: string;
  kind: string;
  layer: string;
  filePath: string;
  line: number;
  endLine: number;
  summary: string;
};

export type CodeGraphEdge = {
  from: string;
  to: string;
  kind: string;
};

export type CodeGraphSubgraph = {
  nodes: CodeGraphNode[];
  edges: CodeGraphEdge[];
  truncated: boolean;
  continuation?: string;
};

export async function getCodeGraphNeighbors(
  rootId: string,
  depth: number,
  filter?: { layers?: string[]; continuation?: string },
): Promise<CodeGraphSubgraph> {
  const result = await callBridge(() =>
    AppService.CodeGraphGetNeighbors(rootId, depth, filter ?? {}),
  );
  return {
    nodes: result.nodes ?? [],
    edges: result.edges ?? [],
    truncated: result.truncated ?? false,
    continuation: result.continuation,
  };
}
```

- [x] **Step 4: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`
Expected: no errors.

- [x] **Step 5: Commit**

```bash
git add frontend/src/bridge/codegraph.ts
git commit -m "feat(codegraph): expose GetNeighbors to the frontend"
```

---

## Task 3: Layer tokens and connector constants

**Files:**
- Create: `frontend/src/modules/codegraph/graph/tokens.ts`

Note what is deliberately *absent*: no card width, no card height. The card's size is a fact about the card's CSS, and `place.ts` reads it back from React Flow's measurements.

- [x] **Step 1: Write the module**

```ts
import { Globe, Zap, Database, Box, type LucideIcon } from "lucide-react";

/** Matches codegraph.Node.layer as produced by the Java analyzer. */
export type Layer = "controller" | "service" | "repository" | "unknown";

export type LayerVisual = {
  /** CSS custom property name, so every surface reads one source of truth. */
  cssVar: string;
  icon: LucideIcon;
  label: string;
};

export const LAYERS: Record<Layer, LayerVisual> = {
  controller: { cssVar: "--status-info", icon: Globe, label: "CONTROLLER" },
  service: { cssVar: "--status-success", icon: Zap, label: "SERVICE" },
  repository: { cssVar: "--status-warning", icon: Database, label: "REPOSITORY" },
  unknown: { cssVar: "--text-tertiary", icon: Box, label: "UNKNOWN" },
};

export function layerOf(raw: string): Layer {
  return raw in LAYERS ? (raw as Layer) : "unknown";
}

/** Read off Connector (167:79). */
export const EDGE = {
  /** Matches the `a14 14 0 0 0` arc in the Figma Branch path. */
  elbowRadius: 14,
  strokeWidth: 1.5,
  dashArray: "5 5",
} as const;
```

- [x] **Step 2: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`
Expected: no errors.

- [x] **Step 3: Commit**

```bash
git add frontend/src/modules/codegraph/graph/tokens.ts
git commit -m "feat(codegraph): layer visual tokens and connector constants"
```

---

## Task 4: Graph ranking (pure, test-first)

Pure graph theory. No pixels, no React, no DOM. This is the module that ports to Go unchanged if a whole-project map ever needs it.

**Files:**
- Create: `frontend/src/modules/codegraph/graph/rank.ts`
- Test: `frontend/src/modules/codegraph/graph/rank.test.ts`

- [x] **Step 1: Write the failing test**

```ts
import { expect, test } from "bun:test";
import { rankGraph } from "./rank";

const ids = (...v: string[]) => v;

test("ranks nodes by longest path from the root", () => {
  const r = rankGraph({
    rootId: "a",
    nodeIds: ids("a", "b", "c"),
    edges: [
      { from: "a", to: "b" },
      { from: "b", to: "c" },
    ],
  });
  expect(r.get("a")!.rank).toBe(0);
  expect(r.get("b")!.rank).toBe(1);
  expect(r.get("c")!.rank).toBe(2);
});

test("a node reachable by two paths takes the longest rank", () => {
  // a->c directly and a->b->c. If c shared a rank with b, the edge b->c would
  // run sideways, which the connector geometry cannot express.
  const r = rankGraph({
    rootId: "a",
    nodeIds: ids("a", "b", "c"),
    edges: [
      { from: "a", to: "b" },
      { from: "a", to: "c" },
      { from: "b", to: "c" },
    ],
  });
  expect(r.get("c")!.rank).toBe(2);
});

test("siblings get distinct, contiguous orders within a rank", () => {
  const r = rankGraph({
    rootId: "a",
    nodeIds: ids("a", "b", "c"),
    edges: [
      { from: "a", to: "b" },
      { from: "a", to: "c" },
    ],
  });
  const orders = [r.get("b")!.order, r.get("c")!.order].sort();
  expect(orders).toEqual([0, 1]);
  expect(r.get("b")!.rank).toBe(r.get("c")!.rank);
});

test("a cycle terminates instead of hanging", () => {
  const r = rankGraph({
    rootId: "a",
    nodeIds: ids("a", "b"),
    edges: [
      { from: "a", to: "b" },
      { from: "b", to: "a" },
    ],
  });
  expect(r.size).toBe(2);
});

test("nodes unreachable from the root are still ranked", () => {
  const r = rankGraph({ rootId: "a", nodeIds: ids("a", "orphan"), edges: [] });
  expect(r.has("orphan")).toBe(true);
});

test("edges referencing unknown nodes are ignored", () => {
  const r = rankGraph({
    rootId: "a",
    nodeIds: ids("a"),
    edges: [{ from: "a", to: "ghost" }],
  });
  expect(r.size).toBe(1);
});
```

- [x] **Step 2: Run it to confirm it fails**

Run: `cd frontend && bun test src/modules/codegraph/graph/rank.test.ts`
Expected: FAIL — `Cannot find module './rank'`.

- [x] **Step 3: Implement**

```ts
export type RankInput = {
  rootId: string;
  nodeIds: string[];
  edges: { from: string; to: string }[];
};

export type Ranked = {
  /** Distance from the root along the longest path. Row index. */
  rank: number;
  /** Position among the nodes sharing this rank. Column index. */
  order: number;
};

/**
 * Longest-path ranking, then contiguous ordering within each rank.
 *
 * Longest path rather than BFS depth: given `a->c` alongside `a->b->c`, BFS
 * would put b and c in the same rank and the b->c connector would have to run
 * sideways or upward. Longest path guarantees every edge descends by at least
 * one rank, which is what makes the elbow connector always drawable.
 */
export function rankGraph(input: RankInput): Map<string, Ranked> {
  const known = new Set(input.nodeIds);
  const outgoing = new Map<string, string[]>();
  for (const e of input.edges) {
    if (!known.has(e.from) || !known.has(e.to)) continue;
    const list = outgoing.get(e.from);
    if (list) list.push(e.to);
    else outgoing.set(e.from, [e.to]);
  }

  const rank = new Map<string, number>();

  // Iterative DFS carrying an explicit on-path set. Real Java code has mutual
  // recursion, so a back-edge must not be allowed to raise a rank forever.
  const visit = (start: string) => {
    const stack: [string, number][] = [[start, 0]];
    const onPath = new Set<string>();
    while (stack.length) {
      const [id, depth] = stack.pop()!;
      if (onPath.has(id)) continue;
      if ((rank.get(id) ?? -1) >= depth) continue;
      rank.set(id, depth);
      onPath.add(id);
      for (const next of outgoing.get(id) ?? []) {
        if (!onPath.has(next)) stack.push([next, depth + 1]);
      }
      onPath.delete(id);
    }
  };

  if (known.has(input.rootId)) visit(input.rootId);
  for (const id of input.nodeIds) if (!rank.has(id)) visit(id);

  const counters = new Map<number, number>();
  const out = new Map<string, Ranked>();
  for (const id of input.nodeIds) {
    const r = rank.get(id) ?? 0;
    const order = counters.get(r) ?? 0;
    counters.set(r, order + 1);
    out.set(id, { rank: r, order });
  }
  return out;
}
```

- [x] **Step 4: Run the tests**

Run: `cd frontend && bun test src/modules/codegraph/graph/rank.test.ts`
Expected: 6 pass.

- [x] **Step 5: Commit**

```bash
git add frontend/src/modules/codegraph/graph/rank.ts frontend/src/modules/codegraph/graph/rank.test.ts
git commit -m "feat(codegraph): longest-path graph ranking"
```

---

## Task 5: Placement (pure, test-first)

Turns ranks into coordinates using sizes measured off the real DOM. Returns `null` rather than guessing when a size is missing — that is what keeps a card dimension from being hardcoded here.

**Files:**
- Create: `frontend/src/modules/codegraph/graph/place.ts`
- Test: `frontend/src/modules/codegraph/graph/place.test.ts`

- [x] **Step 1: Write the failing test**

```ts
import { expect, test } from "bun:test";
import { place } from "./place";
import type { Ranked } from "./rank";

const ranked = (v: Record<string, Ranked>) => new Map(Object.entries(v));
const sized = (v: Record<string, { width: number; height: number }>) => new Map(Object.entries(v));

test("returns null while any node is still unmeasured", () => {
  const out = place(
    ranked({ a: { rank: 0, order: 0 }, b: { rank: 1, order: 0 } }),
    sized({ a: { width: 268, height: 94 } }),
  );
  expect(out).toBeNull();
});

test("stacks ranks downward using the tallest node in each rank", () => {
  const out = place(
    ranked({ a: { rank: 0, order: 0 }, tall: { rank: 0, order: 1 }, b: { rank: 1, order: 0 } })!,
    sized({
      a: { width: 100, height: 40 },
      tall: { width: 100, height: 120 },
      b: { width: 100, height: 40 },
    }),
  )!;
  expect(out.get("a")!.y).toBe(0);
  expect(out.get("tall")!.y).toBe(0);
  // rank 1 clears the tallest card in rank 0, not the first one
  expect(out.get("b")!.y).toBeGreaterThanOrEqual(120);
});

test("siblings advance by the previous sibling's own width", () => {
  const out = place(
    ranked({ wide: { rank: 0, order: 0 }, next: { rank: 0, order: 1 } })!,
    sized({ wide: { width: 400, height: 40 }, next: { width: 100, height: 40 } }),
  )!;
  expect(out.get("wide")!.x).toBe(0);
  expect(out.get("next")!.x).toBeGreaterThanOrEqual(400);
});

test("the first node of every rank sits on the shared trunk at x=0", () => {
  const out = place(
    ranked({ a: { rank: 0, order: 0 }, b: { rank: 1, order: 0 } })!,
    sized({ a: { width: 268, height: 94 }, b: { width: 268, height: 94 } }),
  )!;
  expect(out.get("a")!.x).toBe(0);
  expect(out.get("b")!.x).toBe(0);
});
```

- [x] **Step 2: Run it to confirm it fails**

Run: `cd frontend && bun test src/modules/codegraph/graph/place.test.ts`
Expected: FAIL — `Cannot find module './place'`.

- [x] **Step 3: Implement**

```ts
import type { Ranked } from "./rank";

export type Size = { width: number; height: number };
export type Position = { x: number; y: number };

/**
 * Vertical gap matches the Connector component's own height (167:79), so the
 * elbow has exactly the room it was drawn with. Horizontal gap is the sibling
 * spacing from the assembled screen (150:46). Both are gaps, not card sizes —
 * the cards report their own dimensions.
 */
const RANK_GAP = 86;
const SIBLING_GAP = 52;

/**
 * Returns null when any node has not been measured yet. Callers keep the graph
 * hidden until this returns a real map; the alternative — a fallback size —
 * would hardcode the card's dimensions in the one module that must not know
 * them, and would flash wrong positions on first paint.
 */
export function place(
  ranks: Map<string, Ranked>,
  sizes: Map<string, Size>,
): Map<string, Position> | null {
  const byRank = new Map<number, string[]>();
  for (const [id, r] of ranks) {
    if (!sizes.has(id)) return null;
    const list = byRank.get(r.rank);
    if (list) list.push(id);
    else byRank.set(r.rank, [id]);
  }

  const out = new Map<string, Position>();
  let y = 0;
  for (const rank of [...byRank.keys()].sort((a, b) => a - b)) {
    const members = byRank.get(rank)!.sort((a, b) => ranks.get(a)!.order - ranks.get(b)!.order);
    let x = 0;
    let tallest = 0;
    for (const id of members) {
      const size = sizes.get(id)!;
      out.set(id, { x, y });
      x += size.width + SIBLING_GAP;
      tallest = Math.max(tallest, size.height);
    }
    y += tallest + RANK_GAP;
  }
  return out;
}
```

- [x] **Step 4: Run the tests**

Run: `cd frontend && bun test src/modules/codegraph/graph/place.test.ts`
Expected: 4 pass.

- [x] **Step 5: Commit**

```bash
git add frontend/src/modules/codegraph/graph/place.ts frontend/src/modules/codegraph/graph/place.test.ts
git commit -m "feat(codegraph): rank-to-coordinate placement from measured sizes"
```

---

## Task 6: ApiNode custom node

Reproduces `API Node` (`162:183`). Open it in Figma and compare before calling this done.

**Files:**
- Create: `frontend/src/modules/codegraph/graph/ApiNode.tsx`

- [x] **Step 1: Write the component**

```tsx
import { Handle, Position, type NodeProps } from "@xyflow/react";
import { LAYERS, layerOf } from "./tokens";
import { cn } from "@/lib/utils";

export type ApiNodeData = {
  layer: string;
  title: string;
  detail: string;
};

/**
 * State uses a neutral border ramp (subtle -> tertiary -> white 2px) rather
 * than the layer colour. `--border-focus` in the design system resolves to the
 * same hex as `--status-info`, so a focus-coloured ring is invisible on a
 * Controller node; and any layer-coloured ring would make "selected" and "which
 * layer" say the same thing. Layer meaning lives in the icon and label only.
 */
export function ApiNode({ data, selected }: NodeProps & { data: ApiNodeData }) {
  const visual = LAYERS[layerOf(data.layer)];
  const Icon = visual.icon;
  const accent = `var(${visual.cssVar})`;

  return (
    <div
      className={cn(
        "group w-[268px] rounded-xl bg-surface-raised p-3",
        "flex flex-col gap-2.5",
        selected
          ? "border-2 border-text-primary shadow-[0_6px_16px_rgba(0,0,0,0.45)]"
          : "border border-border-subtle shadow-[0_1px_3px_rgba(0,0,0,0.18)] hover:border-text-tertiary hover:shadow-[0_4px_12px_rgba(0,0,0,0.35)]",
      )}
    >
      {/* Handles are the anchors the connector geometry attaches to: a straight
          run enters the top, a branch enters the left. Both are invisible —
          this is a read-only view, nobody draws edges by hand. */}
      <Handle id="top" type="target" position={Position.Top} className="!opacity-0" />
      <Handle id="left" type="target" position={Position.Left} className="!opacity-0" />
      <Handle id="bottom" type="source" position={Position.Bottom} className="!opacity-0" />

      <div className="flex items-center gap-2.5">
        <span
          className="flex size-[30px] shrink-0 items-center justify-center rounded-[9px] group-hover:brightness-125"
          style={{ backgroundColor: `color-mix(in srgb, ${accent} 14%, transparent)` }}
        >
          <Icon size={16} strokeWidth={2} style={{ color: accent }} />
        </span>
        <span className="flex min-w-0 flex-col gap-0.5">
          <span
            className="font-mono text-[9px] font-medium tracking-[0.06em]"
            style={{ color: accent }}
          >
            {visual.label}
          </span>
          <span className="truncate text-sm font-semibold text-text-primary">{data.title}</span>
        </span>
      </div>
      <span className="truncate rounded-lg bg-surface-tinted px-2.5 py-[7px] font-mono text-[11px] text-text-secondary">
        {data.detail}
      </span>
    </div>
  );
}
```

- [x] **Step 2: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`
Expected: no errors. If `@/lib/utils` exposes `cn` under another name, match how `src/components/ui/*` imports it.

- [x] **Step 3: Commit**

```bash
git add frontend/src/modules/codegraph/graph/ApiNode.tsx
git commit -m "feat(codegraph): API node custom node component"
```

---

## Task 7: ConnectorEdge custom edge

Reproduces `Connector` (`167:79`) and `Edge Label` (`164:49`).

**Files:**
- Create: `frontend/src/modules/codegraph/graph/ConnectorEdge.tsx`

- [x] **Step 1: Write the edge**

```tsx
import {
  BaseEdge,
  EdgeLabelRenderer,
  getSmoothStepPath,
  type EdgeProps,
} from "@xyflow/react";
import { EDGE } from "./tokens";

export type ConnectorEdgeData = {
  /** codegraph.Edge.kind — "di" renders dashed and labels itself "injects". */
  kind: string;
  /** Labels default off: a sequential call adds no information by saying "calls". */
  showLabel?: boolean;
};

export function ConnectorEdge({ id, data, markerEnd, ...props }: EdgeProps) {
  const kind = (data as ConnectorEdgeData | undefined)?.kind ?? "call";
  const showLabel = (data as ConnectorEdgeData | undefined)?.showLabel ?? false;
  const dashed = kind === "di";

  const [path, labelX, labelY] = getSmoothStepPath({
    ...props,
    borderRadius: EDGE.elbowRadius,
  });

  return (
    <>
      <BaseEdge
        id={id}
        path={path}
        markerEnd={markerEnd}
        style={{
          stroke: "var(--icon-secondary)",
          strokeWidth: EDGE.strokeWidth,
          strokeDasharray: dashed ? EDGE.dashArray : undefined,
        }}
      />
      {showLabel && (
        <EdgeLabelRenderer>
          <div
            className="nodrag nopan pointer-events-none absolute"
            style={{ transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)` }}
          >
            <EdgeLabelPill kind={dashed ? "di" : "call"}>
              {dashed ? "injects" : "calls"}
            </EdgeLabelPill>
          </div>
        </EdgeLabelRenderer>
      )}
    </>
  );
}

/**
 * The DI variant stacks a tint over an opaque base rather than using one
 * low-opacity fill: the canvas has a dot grid behind it, and a translucent chip
 * lets the dots read straight through the label.
 */
export function EdgeLabelPill({ kind, children }: { kind: "call" | "di"; children: string }) {
  const info = "var(--status-info)";
  return (
    <span
      className="rounded-full border px-[9px] py-1 font-mono text-[10px] font-medium tracking-[0.02em] shadow-[0_2px_6px_rgba(0,0,0,0.45)]"
      style={
        kind === "di"
          ? {
              backgroundColor: `color-mix(in srgb, ${info} 20%, var(--surface-raised))`,
              borderColor: `color-mix(in srgb, ${info} 55%, transparent)`,
              color: info,
            }
          : {
              backgroundColor: "var(--surface-raised)",
              borderColor: "var(--border-subtle)",
              color: "var(--text-secondary)",
            }
      }
    >
      {children}
    </span>
  );
}
```

- [x] **Step 2: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`
Expected: no errors.

- [x] **Step 3: Commit**

```bash
git add frontend/src/modules/codegraph/graph/ConnectorEdge.tsx
git commit -m "feat(codegraph): connector edge and edge label pill"
```

---

## Task 8: FlowCanvas host

The load-bearing task: render, measure, place, fit.

**Files:**
- Create: `frontend/src/modules/codegraph/graph/FlowCanvas.tsx`

- [x] **Step 1: Write the host**

```tsx
import { useEffect, useMemo, useState } from "react";
import {
  Background,
  MarkerType,
  ReactFlow,
  ReactFlowProvider,
  useNodesInitialized,
  useReactFlow,
  type Edge,
  type Node,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { ApiNode } from "./ApiNode";
import { ConnectorEdge } from "./ConnectorEdge";
import { rankGraph } from "./rank";
import { place } from "./place";
import type { CodeGraphEdge, CodeGraphNode } from "@/bridge/codegraph";

const nodeTypes = { apiNode: ApiNode };
const edgeTypes = { connector: ConnectorEdge };

export type FlowCanvasProps = {
  rootId: string;
  nodes: CodeGraphNode[];
  edges: CodeGraphEdge[];
  onSelect: (nodeId: string) => void;
};

export function FlowCanvas(props: FlowCanvasProps) {
  return (
    <ReactFlowProvider>
      <Canvas {...props} />
    </ReactFlowProvider>
  );
}

function Canvas({ rootId, nodes, edges, onSelect }: FlowCanvasProps) {
  const { getNodes, setNodes, fitView } = useReactFlow();
  const nodesInitialized = useNodesInitialized();
  const [placed, setPlaced] = useState(false);

  const ranks = useMemo(
    () => rankGraph({ rootId, nodeIds: nodes.map((n) => n.id), edges }),
    [rootId, nodes, edges],
  );

  // Everything starts at the origin and stays invisible until it has been
  // measured and placed — that is what lets place() refuse to guess a size.
  const initialNodes: Node[] = useMemo(
    () =>
      nodes.map((n) => ({
        id: n.id,
        type: "apiNode",
        position: { x: 0, y: 0 },
        data: {
          layer: n.layer,
          title: n.label,
          detail: `${n.filePath.split("/").pop()}:${n.line}`,
        },
      })),
    [nodes],
  );

  const flowEdges: Edge[] = useMemo(
    () =>
      edges.flatMap((e) => {
        const from = ranks.get(e.from);
        const to = ranks.get(e.to);
        if (!from || !to) return [];
        // A target in the same column continues the trunk and enters from the
        // top; anything offset sideways is a branch and enters from the left.
        const sameColumn = from.order === to.order;
        return [
          {
            id: `${e.from}->${e.to}`,
            source: e.from,
            target: e.to,
            sourceHandle: "bottom",
            targetHandle: sameColumn ? "top" : "left",
            type: "connector",
            data: { kind: e.kind, showLabel: e.kind === "di" },
            markerEnd: { type: MarkerType.Arrow, color: "var(--icon-secondary)" },
          },
        ];
      }),
    [edges, ranks],
  );

  useEffect(() => setPlaced(false), [initialNodes, flowEdges]);

  useEffect(() => {
    if (!nodesInitialized || placed) return;
    const sizes = new Map(
      getNodes().flatMap((n) =>
        n.measured?.width && n.measured?.height
          ? [[n.id, { width: n.measured.width, height: n.measured.height }] as const]
          : [],
      ),
    );
    const positions = place(ranks, sizes);
    if (!positions) return;
    setNodes((current) =>
      current.map((n) => ({ ...n, position: positions.get(n.id) ?? n.position })),
    );
    setPlaced(true);
    void fitView({ padding: 0.2, duration: 200 });
  }, [nodesInitialized, placed, ranks, getNodes, setNodes, fitView]);

  return (
    <div className={placed ? "size-full" : "size-full opacity-0"}>
      <ReactFlow
        defaultNodes={initialNodes}
        defaultEdges={flowEdges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        // Read-only view: positions come from the layout, and nobody authors
        // edges by hand, so both editing affordances are off.
        nodesDraggable={false}
        nodesConnectable={false}
        onlyRenderVisibleElements
        onNodeClick={(_, node) => onSelect(node.id)}
        proOptions={{ hideAttribution: false }}
      >
        <Background gap={24} size={2} color="var(--text-tertiary)" />
      </ReactFlow>
    </div>
  );
}
```

- [x] **Step 2: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`
Expected: no errors. If `<Background>`'s props differ, check `node_modules/@xyflow/react/dist/esm/index.d.ts` for `BackgroundProps` and use the declared names — the default variant is already dots, so only `gap`, `size` and `color` should need setting.

- [x] **Step 3: Verify against Figma**

Run the app (`task run:desktop` from the repo root), open a Java project, pick an endpoint. Compare against Figma `150:46`:
- card border ramp matches Default / Hover / Selected
- two edges leaving one node share a vertical trunk before the branch turns right
- only `di` edges are dashed, and only they carry a pill
- the elbow corner radius matches (14)
- no flash of stacked cards at the origin on first render

- [x] **Step 4: Commit**

```bash
git add frontend/src/modules/codegraph/graph/FlowCanvas.tsx
git commit -m "feat(codegraph): flow canvas with measure-then-place layout"
```

---

## Task 9: Legend and toolbar

**Files:**
- Create: `frontend/src/modules/codegraph/graph/LayerLegend.tsx`
- Create: `frontend/src/modules/codegraph/graph/CanvasToolbar.tsx`
- Modify: `frontend/src/modules/codegraph/graph/FlowCanvas.tsx`

- [x] **Step 1: Write the legend**

```tsx
import { LAYERS, type Layer } from "./tokens";

const ORDER: Layer[] = ["controller", "service", "repository", "unknown"];

export function LayerLegend() {
  return (
    <div className="flex flex-col gap-[7px] rounded-[10px] border border-border-subtle bg-surface-raised px-3 py-2.5 shadow-[0_2px_8px_rgba(0,0,0,0.3)]">
      {ORDER.map((layer) => (
        <span key={layer} className="flex items-center gap-2">
          <span
            className="size-2 rounded-full"
            style={{ backgroundColor: `var(${LAYERS[layer].cssVar})` }}
          />
          <span className="font-mono text-[9px] font-medium tracking-[0.06em] text-text-tertiary">
            {LAYERS[layer].label}
          </span>
        </span>
      ))}
    </div>
  );
}
```

Stack it vertically, not horizontally: a horizontal legend runs about 380px wide and collides with the workbench dock at the bottom centre.

- [x] **Step 2: Write the toolbar**

```tsx
import { Search, LocateFixed, Maximize } from "lucide-react";
import { cn } from "@/lib/utils";

export type CanvasToolbarProps = {
  onSearch: () => void;
  onFit: () => void;
  onExpand: () => void;
  fitActive?: boolean;
};

export function CanvasToolbar({ onSearch, onFit, onExpand, fitActive }: CanvasToolbarProps) {
  const buttons = [
    { icon: Search, action: onSearch, active: false, label: "Search nodes" },
    { icon: LocateFixed, action: onFit, active: !!fitActive, label: "Fit to view" },
    { icon: Maximize, action: onExpand, active: false, label: "Expand all" },
  ];
  return (
    <div className="flex gap-0.5 rounded-[9px] border border-border-subtle bg-surface-raised p-[3px]">
      {buttons.map(({ icon: Icon, action, active, label }) => (
        <button
          key={label}
          type="button"
          aria-label={label}
          onClick={action}
          className={cn(
            "flex size-[26px] items-center justify-center rounded-md",
            active && "bg-surface-tinted",
          )}
        >
          <Icon size={14} className="text-icon-secondary" />
        </button>
      ))}
    </div>
  );
}
```

- [x] **Step 3: Mount them as React Flow panels**

Add to the imports in `FlowCanvas.tsx`: `Panel` from `@xyflow/react`, plus the two components. Then inside `<ReactFlow>`, beside `<Background />`:

```tsx
<Panel position="bottom-left">
  <LayerLegend />
</Panel>
<Panel position="top-right">
  <CanvasToolbar
    onSearch={() => {}}
    onFit={() => void fitView({ padding: 0.2, duration: 200 })}
    onExpand={() => {}}
    fitActive
  />
</Panel>
```

`Panel` keeps them pinned to the viewport rather than scrolling with the graph, which is what the Figma screen shows.

- [x] **Step 4: Verify against Figma**

Compare the running canvas to `150:46`. The dot grid must pan and scale with the graph, and the legend must clear the dock.

- [x] **Step 5: Commit**

```bash
git add frontend/src/modules/codegraph/graph/
git commit -m "feat(codegraph): canvas legend and toolbar panels"
```

---

## Task 10: Wire into CodeGraphView

**Files:**
- Modify: `frontend/src/modules/codegraph/CodeGraphView.tsx`

- [x] **Step 1: Add state and imports**

Add to the existing bridge import block:

```tsx
import { getCodeGraphNeighbors, type CodeGraphSubgraph } from "../../bridge/codegraph";
```

and beside the other `useState` calls:

```tsx
const [subgraph, setSubgraph] = useState<CodeGraphSubgraph | null>(null);
const [rootId, setRootId] = useState<string | null>(null);
const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
```

- [x] **Step 2: Load the subgraph when an endpoint is picked**

Replace the body of `viewFlow`:

```tsx
async function viewFlow(endpoint: CodeGraphEndpoint) {
  setError("");
  try {
    const sub = await getCodeGraphNeighbors(endpoint.entryNode, 3);
    setSubgraph(sub);
    setRootId(endpoint.entryNode);
    setSelectedNodeId(endpoint.entryNode);
  } catch (e) {
    setSubgraph(null);
    setError(errorMessage(e));
  }
}
```

- [x] **Step 3: Replace the ordered-list flow panel with the canvas**

In the `activeTab === "endpoints"` branch, replace the `<ol>…</ol>` block and its `else` branch with:

```tsx
{subgraph && rootId ? (
  <div className="flex flex-1 flex-col">
    <span className="px-1 pb-2 font-mono text-[11px] text-text-tertiary">
      {subgraph.nodes.length} nodes · {subgraph.edges.length} edges
    </span>
    <div className="flex-1">
      <FlowCanvas
        rootId={rootId}
        nodes={subgraph.nodes}
        edges={subgraph.edges}
        onSelect={setSelectedNodeId}
      />
    </div>
  </div>
) : (
  <p className="text-sm text-muted-foreground">Select an endpoint to see its flow.</p>
)}
```

with `import { FlowCanvas } from "./graph/FlowCanvas";` added at the top.

`FlowCanvas` needs a parent with a real height — React Flow measures its container. The wrapper above uses `flex-1` inside the existing `flex-1 overflow-hidden` row, which supplies one.

- [x] **Step 4: Typecheck and build**

Run: `cd frontend && bunx tsc --noEmit && bun run build:dev`
Expected: both succeed.

- [x] **Step 5: Verify end to end**

Run the desktop app, open a real Java project, click an endpoint. The graph appears fitted to view, clicking a card selects it and shows the white ring, hovering shows the tertiary border, and the dot grid pans with the graph.

- [x] **Step 6: Commit**

```bash
git add frontend/src/modules/codegraph/CodeGraphView.tsx
git commit -m "feat(codegraph): render the API flow on the react flow canvas"
```

---

## Task 11: Performance budget

`bun run build` runs `check-performance-budget.mjs`, which fails on initial JS over 700 KiB raw / 225 KiB gzip. It measures only `<script>` and `modulepreload` entries in `dist/index.html`, so lazy chunks are not gated. CodeGraph is already lazy through `lazyModuleView` — verify that React Flow inherited that rather than assuming it.

- [x] **Step 1: Run the real build**

Run: `cd frontend && bun run build`
Expected: build succeeds and the budget check passes.

- [x] **Step 2: Confirm React Flow is not in the initial chunk**

Run: `cd frontend && grep -l "xyflow" dist/assets/*.js`
Cross-check those filenames against the `<script>` and `modulepreload` tags in `dist/index.html`.
Expected: none of them appear in `dist/index.html`.

- [x] **Step 3: If one does appear**

Something imports the graph module eagerly. Find it (`grep -rn "graph/FlowCanvas" src/ --include=*.ts*`) and make that import dynamic. Do not raise the budget.

- [x] **Step 4: Commit any fix**

```bash
git add -A frontend/src
git commit -m "perf(codegraph): keep the flow canvas out of the initial bundle"
```

---

## Deliberate limitations

Flagged so they are not mistaken for oversights:

- **Sibling order within a rank is insertion order**, so long edges may cross. A crossing-reduction pass (median heuristic) drops into `rank.ts` without touching anything else, and `place.ts` never needs to know.
- **`rank.ts` runs synchronously on the main thread.** It is O(V+E) over tens of nodes. Move it to a Worker, or to Go, when a measurement says it blocks a frame — the module takes no pixels and returns none, so either move is mechanical.
- **Layout runs once per subgraph.** Re-fetching neighbours for expansion re-runs the whole placement rather than incrementally extending it. Fine at this scale; revisit if expand-in-place lands.
- **The toolbar's search and expand buttons are inert.** They exist because the Figma header shows them; wire them when the behaviours are specified.
- **`hideAttribution` is left false.** React Flow's licence requires keeping the attribution unless you hold a Pro licence. Do not flip it without checking that first.

## Self-review notes

- Every Figma component has a code counterpart: `API Node` → Task 6, `Edge Label` → Task 7 (`EdgeLabelPill`), `Connector` → Task 7 (`ConnectorEdge`), legend/toolbar → Task 9, dot grid → `<Background>` in Task 8, tooltip → reuse the existing shadcn `Tooltip`, not rebuilt.
- Types used in later tasks (`Layer`, `Ranked`, `Size`, `Position`, `ApiNodeData`, `ConnectorEdgeData`, `CodeGraphSubgraph`) are each defined in an earlier task.
- Both verified repo gaps (missing tokens, missing bridge wrapper) are closed before anything depends on them.
- No card dimension appears in `rank.ts`, `place.ts`, or the bridge. The only `268px` in the codebase is the card's own Tailwind width in `ApiNode.tsx`, which `place.ts` reads back through React Flow's measurement.
