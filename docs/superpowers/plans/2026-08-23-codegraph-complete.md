# CodeGraph Complete Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Finish CodeGraph — bring every feature the design doc and the Figma file describe up to working UI, except the parts where DevKit calls into a running agent session, which stay as explicit TODOs.

**Architecture:** The Go core is already ~85% done: service, SQLite store, MCP tools, and Wails bridge exist for indexing, endpoints, neighbors, files, snippets, blast radius, data-flow trace and the whole plan lifecycle. The gap is almost entirely frontend, plus four specific Go additions named below. Work proceeds bottom-up per feature — Go read method → Wails bridge → `frontend/src/bridge/*.ts` wrapper → component — so each phase ends with something usable rather than a half-wired layer.

**Tech Stack:** Go 1.x + Wails v3, SQLite (`modernc.org/sqlite`), React 19 + TypeScript + Tailwind v4, Zustand, `@xyflow/react`, CodeMirror 6, Radix (`radix-ui`), `bun test` for frontend unit tests, `go test` for Go.

---

## Phase Independence

Five phases. **Each is independently shippable and can be run as its own execution session.** They are ordered by value and by dependency: Phase 1 unblocks the def-use work already merged; Phase 3 depends on Phase 2's plan bridge; Phases 4 and 5 depend on nothing but Phase 1's bridge conventions.

| Phase | Subsystem | Depends on |
|---|---|---|
| 1 | Node inspector + data flow | — |
| 2 | Plan-first flow | — (Phase 1 for the popover shell) |
| 3 | Plan node comments (storage + UI) | Phase 2 |
| 4 | Blast radius + index freshness | — |
| 5 | Terminal in the bottom slot | — |

If you only have budget for one, run Phase 1.

---

## What already exists (do not rebuild)

Verified by reading the tree on 2026-08-23:

**Go — complete, tested, bridged, and exposed over MCP:**
`OpenProject`, `ListEndpoints`, `GetApiFlow`, `SearchSymbol`, `GetBlastRadius`, `WaitForIndexReady`, `GetNeighbors`, `ListFiles`, `ReadFile`, `WriteFile`, `GetCodeSnippet`, `TraceDataFlow`, `StartTerminal`, and the terminal registry's `Write`/`Resize`/`Close`.

**Go — complete in the service and over MCP, but with NO Wails bridge:**
`ProposeFlow`, `GetPlannedFlow`, `GetPlanProgress`, `ApprovePlan`, `RelinkPlanNode` (`internal/codegraph/service.go:1005-1131`). MCP tools live in `internal/mcp/tools_codegraph_plan.go`.

**Frontend — working today:**
`CodeGraphView.tsx` (project picker, re-index, endpoint list → `FlowCanvas`, file tree → CodeMirror editor with Ctrl+S), `FlowCanvas`/`ApiNode`/`ConnectorEdge`/`LayerLegend`, `NodeSnippetPanel` in a SplitPanel, `codegraphStore.ts`.

**Frontend — missing entirely:**
Everything about data flow, plans, comments, blast radius, and the terminal.

---

## The four Go additions this plan needs

Everything else on the Go side is already done. These four are genuinely absent:

1. **`ListFields`** (Phase 1) — `TraceDataFlow(symbolID, fieldName)` requires a field name up front, and nothing in the API can produce one. Without this the UI can only ask the user to type a field name blind.
2. **Plan Wails bridge** (Phase 2) — the service methods exist; `internal/bridge/codegraph.go` never exposes them.
3. **Comment storage** (Phase 3) — zero code exists. `grep -rn Comment internal/` returns nothing.
4. **Terminal output emit + index-updated event** (Phases 4, 5) — `main.go:201` installs a callback that silently drops every byte the PTY produces, and a finished re-index tells the frontend nothing.

---

## File Structure

**Phase 1**
- Modify: `internal/codegraph/codegraph.go` — add `FieldRef`, extend the `Store` interface
- Modify: `internal/codegraph/store.go` — `ListFields` query
- Modify: `internal/codegraph/service.go` — `ListFields` + `nilStore` stub
- Modify: `internal/bridge/codegraph.go` — `CodeGraphListFields`
- Modify: `internal/mcp/tools_codegraph.go` + `internal/mcp/server.go` — `codegraph.listFields` (agent parity)
- Modify: `frontend/src/bridge/codegraph.ts` — `listCodeGraphFields`
- Modify: `frontend/src/components/ui/tabs.tsx` — `TabsCount` badge
- Create: `frontend/src/modules/codegraph/inspector/NodeInspector.tsx` — popover shell + tab routing
- Create: `frontend/src/modules/codegraph/inspector/DataFlowTab.tsx`
- Create: `frontend/src/modules/codegraph/inspector/dataFlowRows.ts` — pure trace→rows transform
- Create: `frontend/src/modules/codegraph/inspector/dataFlowRows.test.ts`
- Modify: `frontend/src/modules/codegraph/CodeGraphView.tsx`, `codegraphStore.ts`

**Phase 2**
- Modify: `internal/bridge/codegraph.go` — four plan methods
- Create: `frontend/src/bridge/codegraphPlan.ts`
- Create: `frontend/src/modules/codegraph/plan/PlanNode.tsx`
- Create: `frontend/src/modules/codegraph/plan/planStatus.ts` + `planStatus.test.ts`
- Create: `frontend/src/modules/codegraph/plan/PlanBar.tsx`
- Modify: `CodeGraphView.tsx`, `codegraphStore.ts`, `graph/FlowCanvas.tsx`

**Phase 3**
- Modify: `internal/codegraph/codegraph.go`, `store.go`, `service.go`
- Create: `internal/codegraph/comments.go` + `comments_test.go`
- Modify: `internal/bridge/codegraph.go`, `internal/mcp/tools_codegraph_plan.go`
- Create: `frontend/src/modules/codegraph/inspector/CommentsTab.tsx`
- Modify: `frontend/src/bridge/codegraphPlan.ts`

**Phase 4**
- Modify: `frontend/src/bridge/codegraph.ts`, `internal/codegraph/service.go`, `main.go`
- Create: `frontend/src/modules/codegraph/BlastRadiusPanel.tsx`

**Phase 5**
- Modify: `main.go`
- Create: `frontend/src/bridge/terminal.ts`
- Create: `frontend/src/modules/codegraph/terminal/TerminalPane.tsx`
- Modify: `frontend/package.json`, `CodeGraphView.tsx`

---

# Phase 1 — Node inspector and data flow

The def-use trace shipped with no way to reach it. This phase gives it a UI, and replaces the SplitPanel snippet flow with the tabbed popover from the Figma file (`Node Detail`, `Node Data Flow`, `Node Comments` — the Comments tab lands in Phase 3).

### Task 1: `ListFields` — Go store

`TraceDataFlow` needs a field name and nothing can produce one. One query serves both node kinds: `class_id` matches when the selected node is a class, `node_id` matches when it is a method, so the caller never has to parse a node ID.

**Files:**
- Modify: `internal/codegraph/codegraph.go` (add `FieldRef` near `FieldAccess` at line 176; add to the `Store` interface at line ~285)
- Modify: `internal/codegraph/store.go` (after `GetFieldAccesses`, line ~365)
- Test: `internal/codegraph/store_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/codegraph/store_test.go`:

```go
func TestStoreListFieldsByClassAndByMethod(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	result := sampleResult()
	result.FieldAccesses = []FieldAccess{
		{ClassID: "n1", NodeID: "n1", FieldName: "total", Kind: "write", Line: 12},
		{ClassID: "n1", NodeID: "n2", FieldName: "total", Kind: "read", Line: 22},
		{ClassID: "n1", NodeID: "n2", FieldName: "status", Kind: "write", Line: 25},
	}
	if err := store.ReplaceIndex(ctx, result); err != nil {
		t.Fatalf("ReplaceIndex: %v", err)
	}

	// By class: every field the class has, with both accesses of "total" counted.
	byClass, err := store.ListFields(ctx, "n1")
	if err != nil {
		t.Fatalf("ListFields: %v", err)
	}
	if len(byClass) != 2 {
		t.Fatalf("byClass = %+v, want 2 fields", byClass)
	}
	if byClass[0].FieldName != "status" || byClass[1].FieldName != "total" {
		t.Fatalf("byClass = %+v, want alphabetical order", byClass)
	}
	if byClass[1].Reads != 1 || byClass[1].Writes != 1 {
		t.Fatalf("total = %+v, want 1 read 1 write", byClass[1])
	}
	if byClass[0].ClassID != "n1" {
		t.Fatalf("status.ClassID = %q, want n1 — the trace needs it to call TraceDataFlow", byClass[0].ClassID)
	}

	// By method: only the fields THAT METHOD touches. n2 never touches n1's
	// "total" as a write, but it does read it, so both fields appear; n1 the
	// method touches only "total".
	byMethod, err := store.ListFields(ctx, "n2")
	if err != nil {
		t.Fatalf("ListFields: %v", err)
	}
	if len(byMethod) != 2 {
		t.Fatalf("byMethod = %+v, want 2", byMethod)
	}
	if byMethod[0].Reads != 0 || byMethod[0].Writes != 1 {
		t.Fatalf("status seen from n2 = %+v, want 0 read 1 write", byMethod[0])
	}
}

func TestStoreListFieldsNoAccesses(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.ReplaceIndex(ctx, sampleResult()); err != nil {
		t.Fatalf("ReplaceIndex: %v", err)
	}

	fields, err := store.ListFields(ctx, "n1")
	if err != nil {
		t.Fatalf("ListFields: %v", err)
	}
	if len(fields) != 0 {
		t.Fatalf("expected no fields, got %+v", fields)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/codegraph/ -run TestStoreListFields -v`
Expected: FAIL — `store.ListFields undefined (type *sqliteStore has no field or method ListFields)`

- [ ] **Step 3: Add the `FieldRef` type**

In `internal/codegraph/codegraph.go`, immediately after the `FieldAccess` struct (ends line 182):

```go
// FieldRef is one field a node touches, with how often — enough for a picker
// to list the fields worth tracing without the caller guessing a name. Reads
// and Writes are counted over whatever scope was asked for: every method on
// the class when ListFields was given a class, or the one method when it was
// given a method.
type FieldRef struct {
	// ClassID is the class that declares the field. Pass it straight back to
	// TraceDataFlow — it is what that method means by symbolID.
	ClassID   string `json:"classId"`
	FieldName string `json:"fieldName"`
	Reads     int    `json:"reads"`
	Writes    int    `json:"writes"`
}
```

Add to the `Store` interface, directly under `GetFieldAccesses`:

```go
	ListFields(ctx context.Context, nodeID string) ([]FieldRef, error)
```

- [ ] **Step 4: Implement the store method**

In `internal/codegraph/store.go`, after `GetFieldAccesses`:

```go
// ListFields returns every field reachable from one node, for a field picker.
//
// nodeID may be either a class or a method and the caller does not have to
// know which: field_accesses stores both the declaring class and the accessing
// method, so one OR matches whichever it is. A class yields all of its fields;
// a method yields only the fields it itself touches.
func (s *sqliteStore) ListFields(ctx context.Context, nodeID string) ([]FieldRef, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT class_id,
		       field_name,
		       SUM(CASE WHEN kind = 'read' THEN 1 ELSE 0 END),
		       SUM(CASE WHEN kind = 'write' THEN 1 ELSE 0 END)
		  FROM field_accesses
		 WHERE class_id = ? OR node_id = ?
		 GROUP BY class_id, field_name
		 ORDER BY field_name`, nodeID, nodeID)
	if err != nil {
		return nil, fmt.Errorf("codegraph: ListFields: %w", err)
	}
	defer rows.Close()

	var out []FieldRef
	for rows.Next() {
		var f FieldRef
		if err := rows.Scan(&f.ClassID, &f.FieldName, &f.Reads, &f.Writes); err != nil {
			return nil, fmt.Errorf("codegraph: scan field ref: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/codegraph/ -run TestStoreListFields -v`
Expected: PASS (both tests)

- [ ] **Step 6: Commit**

```bash
git add internal/codegraph/codegraph.go internal/codegraph/store.go internal/codegraph/store_test.go
git commit -m "feat(codegraph): list the fields a node touches, so a trace can be started"
```

---

### Task 2: `ListFields` — service, nilStore, Wails bridge, MCP

**Files:**
- Modify: `internal/codegraph/service.go` (near `TraceDataFlow`, line ~319; `nilStore` stubs at line ~1150)
- Modify: `internal/bridge/codegraph.go`
- Modify: `internal/mcp/server.go` (the `CodeGraphAPI` interface, line ~113) and `internal/mcp/tools_codegraph.go`
- Test: `internal/codegraph/service_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/codegraph/service_test.go`:

```go
func TestServiceListFieldsRequiresActiveProject(t *testing.T) {
	svc := NewService(nil, nil)
	if _, err := svc.ListFields(context.Background(), "n1"); !errors.Is(err, ErrNoActiveProject) {
		t.Fatalf("err = %v, want ErrNoActiveProject", err)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/codegraph/ -run TestServiceListFields -v`
Expected: FAIL — `svc.ListFields undefined`

- [ ] **Step 3: Implement the service method**

In `internal/codegraph/service.go`, directly above `TraceDataFlow`:

```go
// ListFields returns the fields one node touches — the entry point for a data
// flow trace, since TraceDataFlow needs a field name the caller cannot guess.
func (s *Service) ListFields(ctx context.Context, nodeID string) ([]FieldRef, error) {
	store, err := s.activeStore()
	if err != nil {
		return nil, err
	}
	return store.ListFields(ctx, nodeID)
}
```

And with the other `nilStore` stubs (~line 1150):

```go
func (nilStore) ListFields(context.Context, string) ([]FieldRef, error) {
	return nil, ErrNoActiveProject
}
```

- [ ] **Step 4: Run it to verify it passes**

Run: `go test ./internal/codegraph/ -v`
Expected: PASS, whole package

- [ ] **Step 5: Add the Wails bridge method**

In `internal/bridge/codegraph.go`, after `CodeGraphTraceDataFlow` (line ~162), matching the surrounding `s.call` shape exactly:

```go
func (s *AppService) CodeGraphListFields(nodeID string) ([]codegraph.FieldRef, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:read", "project:active", map[string]any{
		"action":  "list_fields",
		"node_id": nodeID,
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.ListFields(ctx, nodeID)
	})
	if err != nil {
		return nil, err
	}
	return result.([]codegraph.FieldRef), nil
}
```

- [ ] **Step 6: Add the MCP tool**

An agent hits the same wall a human does. In `internal/mcp/server.go`, add to the `CodeGraphAPI` interface next to `TraceDataFlow`:

```go
	ListFields(ctx context.Context, nodeID string) ([]codegraph.FieldRef, error)
```

In `internal/mcp/tools_codegraph.go`, register alongside `codegraph.traceDataFlow`:

```go
	addTool(s, server, &sdk.Tool{
		Name:        "codegraph.listFields",
		Description: "List the fields a class or method touches, with read/write counts. Use this to find a field name for codegraph.traceDataFlow.",
	}, s.toolCodeGraphListFields)
```

and the handler, next to `toolCodeGraphTraceDataFlow`:

```go
type codeGraphListFieldsInput struct {
	NodeID string `json:"nodeId" jsonschema:"the class or method node id"`
}

func (s *Server) toolCodeGraphListFields(ctx context.Context, _ *sdk.CallToolRequest, in codeGraphListFieldsInput) (*sdk.CallToolResult, any, error) {
	return jsonResult(func() (any, error) {
		return s.deps.CodeGraph.ListFields(ctx, in.NodeID)
	})
}
```

> Match the existing handler's return helper exactly — read `toolCodeGraphTraceDataFlow` at `internal/mcp/tools_codegraph.go:199` and copy its shape rather than the sketch above if they differ. Its fake in `internal/mcp/tools_codegraph_test.go` (`fakeCodeGraphAPI`) must also gain a `ListFields` method or the package will not compile.

- [ ] **Step 7: Verify the whole backend builds and passes**

Run: `go build ./... && go test ./internal/codegraph/ ./internal/bridge/ ./internal/mcp/`
Expected: PASS, no build errors

- [ ] **Step 8: Regenerate the Wails bindings**

Run: `wails3 generate bindings -clean=true -ts -i`
Expected: `frontend/bindings/devkit/internal/codegraph/models.ts` gains `FieldRef`, `appservice.ts` gains `CodeGraphListFields`

> The bindings have gone stale before — that is exactly how `depth`/`targetId`/`unresolved` nearly got lost. Confirm the new symbols are actually in the output before moving on: `grep -n "FieldRef\|CodeGraphListFields" frontend/bindings/devkit/internal/**/*.ts`

- [ ] **Step 9: Commit**

```bash
git add internal/ frontend/bindings/
git commit -m "feat(codegraph): expose ListFields over the bridge and MCP"
```

---

### Task 3: `TabsCount` — the badge the Figma trigger has and the code does not

The Figma `Tabs / Trigger` component carries `HasCount`/`Count` properties; `frontend/src/components/ui/tabs.tsx` has no equivalent. The Comments tab needs it in Phase 3, and adding it here keeps Phase 1 from hand-rolling a span.

**Files:**
- Modify: `frontend/src/components/ui/tabs.tsx`

- [ ] **Step 1: Add the component**

Add above the `export {` line:

```tsx
function TabsCount({ className, ...props }: React.ComponentProps<"span">) {
  return (
    <span
      data-slot="tabs-count"
      className={cn(
        "inline-flex h-4 min-w-4 items-center justify-center rounded-full px-1",
        "bg-foreground/10 text-[10px] font-medium tabular-nums text-foreground/70",
        "group-data-active/tabs-trigger:bg-foreground/15 group-data-active/tabs-trigger:text-foreground",
        className
      )}
      {...props}
    />
  )
}
```

For the `group-data-active/tabs-trigger:` selectors to resolve, `TabsTrigger` needs the matching group name. In `TabsTrigger`'s `cn(...)`, change the leading `"relative inline-flex ...` to start with `"group/tabs-trigger relative inline-flex ...` — one word, nothing else on that line changes.

Extend the export:

```tsx
export { Tabs, TabsList, TabsTrigger, TabsCount, TabsContent, tabsListVariants }
```

- [ ] **Step 2: Verify it type-checks**

Run: `cd frontend && npx tsc --noEmit`
Expected: clean

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/ui/tabs.tsx
git commit -m "feat(ui): tabs triggers can carry a count badge"
```

---

### Task 4: `dataFlowRows` — the trace→rows transform, tested on its own

The rendering decisions (indent, which wall label, whether a row is clickable) are pure functions of the trace. Keeping them out of the component is what makes them testable without a DOM.

**Files:**
- Create: `frontend/src/modules/codegraph/inspector/dataFlowRows.ts`
- Test: `frontend/src/modules/codegraph/inspector/dataFlowRows.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
import { describe, expect, it } from "bun:test";
import { toDataFlowRows, WALL_LABELS, type DataFlowRow } from "./dataFlowRows";
import type { CodeGraphDataFlowStep } from "@/bridge/codegraph";

// flatMap, not filter: it narrows the discriminated union without depending on
// TypeScript inferring a type predicate from the callback.
const headers = (rows: DataFlowRow[]) => rows.flatMap((r) => (r.type === "header" ? [r] : []));
const events = (rows: DataFlowRow[]) => rows.flatMap((r) => (r.type === "event" ? [r] : []));

const step = (over: Partial<CodeGraphDataFlowStep>): CodeGraphDataFlowStep => ({
  nodeId: "n1",
  label: "OrderService.process",
  filePath: "OrderService.java",
  line: 10,
  kind: "read",
  depth: 0,
  targetId: "",
  unresolved: "",
  ...over,
});

describe("toDataFlowRows", () => {
  it("groups consecutive steps in the same method under one header", () => {
    const rows = toDataFlowRows([
      step({ nodeId: "n1", line: 10, kind: "write" }),
      step({ nodeId: "n1", line: 12, kind: "read" }),
      step({ nodeId: "n2", label: "Repo.save", line: 40, kind: "read" }),
    ]);
    expect(headers(rows)).toHaveLength(2);
    expect(headers(rows)[0].label).toBe("OrderService.process");
    expect(events(rows)).toHaveLength(3);
  });

  it("re-opens a header when the same method is revisited at a new depth", () => {
    const rows = toDataFlowRows([
      step({ nodeId: "n1", line: 10, depth: 0 }),
      step({ nodeId: "n2", label: "B.f", line: 20, depth: 1 }),
      step({ nodeId: "n1", line: 14, depth: 0 }),
    ]);
    expect(headers(rows)).toHaveLength(3);
  });

  it("marks a resolved call as navigable and an unresolved one as a wall", () => {
    const rows = toDataFlowRows([
      step({ kind: "call", targetId: "n2", line: 11 }),
      step({ kind: "call", targetId: "", unresolved: "library", line: 13 }),
    ]);
    const [first, second] = events(rows);
    expect(first.targetId).toBe("n2");
    expect(first.wall).toBe("");
    expect(second.targetId).toBe("");
    expect(second.wall).toBe(WALL_LABELS.library);
  });

  it("indents by depth", () => {
    const rows = toDataFlowRows([step({ depth: 3 })]);
    expect(rows[0].depth).toBe(3);
  });

  it("returns nothing for an empty trace", () => {
    expect(toDataFlowRows([])).toEqual([]);
  });
});
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd frontend && bun test src/modules/codegraph/inspector/dataFlowRows.test.ts`
Expected: FAIL — cannot resolve `./dataFlowRows`

- [ ] **Step 3: Implement**

```ts
import type { CodeGraphDataFlowStep } from "@/bridge/codegraph";

/**
 * Human wording for each wall the trace can hit. The strings say what was lost
 * and why, because a trace that just stops is indistinguishable from a trace
 * that finished — see codegraph.mdx § "Ba bức tường".
 */
export const WALL_LABELS = {
  library: "library — bytecode, no body to read",
  interface: "interface — implementation not knowable statically",
  unresolved: "unresolved — the solver could not resolve this call",
  depth: "depth limit reached",
  cycle: "cycle — already on this path",
} as const;

export type DataFlowRow =
  | { type: "header"; key: string; nodeId: string; label: string; filePath: string; depth: number }
  | {
      type: "event";
      key: string;
      nodeId: string;
      line: number;
      kind: "read" | "write" | "call";
      depth: number;
      /** Non-empty only for a resolved call — the node to jump to. */
      targetId: string;
      /** Non-empty when this step hit a wall; already human-readable. */
      wall: string;
    };

/**
 * Flattens a trace into rows, inserting a header whenever the method changes.
 *
 * The header re-opens on revisit rather than merging: the same method reached
 * again after a deeper call is a different point in the walk, and collapsing
 * the two would imply an ordering the trace does not claim.
 */
export function toDataFlowRows(steps: CodeGraphDataFlowStep[]): DataFlowRow[] {
  const rows: DataFlowRow[] = [];
  let openMethod = "";
  let openDepth = -1;

  steps.forEach((s, i) => {
    if (s.nodeId !== openMethod || s.depth !== openDepth) {
      rows.push({
        type: "header",
        key: `h${i}`,
        nodeId: s.nodeId,
        label: s.label,
        filePath: s.filePath,
        depth: s.depth,
      });
      openMethod = s.nodeId;
      openDepth = s.depth;
    }
    rows.push({
      type: "event",
      key: `e${i}`,
      nodeId: s.nodeId,
      line: s.line,
      kind: s.kind,
      depth: s.depth,
      targetId: s.unresolved === "" ? s.targetId : "",
      wall: s.unresolved === "" ? "" : (WALL_LABELS[s.unresolved] ?? s.unresolved),
    });
  });

  return rows;
}
```

- [ ] **Step 4: Run it to verify it passes**

Run: `cd frontend && bun test src/modules/codegraph/inspector/dataFlowRows.test.ts`
Expected: PASS, 5 tests

- [ ] **Step 5: Commit**

```bash
git add frontend/src/modules/codegraph/inspector/
git commit -m "feat(codegraph): flatten a data-flow trace into renderable rows"
```

---

### Task 5: `DataFlowTab` — field picker plus trace

**Files:**
- Modify: `frontend/src/bridge/codegraph.ts` (add `listCodeGraphFields` beside `traceCodeGraphDataFlow`)
- Create: `frontend/src/modules/codegraph/inspector/DataFlowTab.tsx`

- [ ] **Step 1: Add the bridge wrapper**

Append to `frontend/src/bridge/codegraph.ts`, following the file's existing shape:

```ts
/** One field a node touches. `classId` is what TraceDataFlow calls symbolId. */
export type CodeGraphFieldRef = {
  classId: string;
  fieldName: string;
  reads: number;
  writes: number;
};

export async function listCodeGraphFields(nodeId: string): Promise<CodeGraphFieldRef[]> {
  return callBridge(async () => (await AppService.CodeGraphListFields(nodeId)) ?? []);
}
```

- [ ] **Step 2: Build the tab**

```tsx
import { useEffect, useState } from "react";
import { ArrowDownToLine, ArrowUpFromLine, PhoneOutgoing } from "lucide-react";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { cn } from "@/lib/utils";
import {
  listCodeGraphFields,
  traceCodeGraphDataFlow,
  type CodeGraphFieldRef,
  type CodeGraphDataFlowTrace,
} from "@/bridge/codegraph";
import { errorMessage } from "@/bridge/errors";
import { shortLabel } from "../graph/labels";
import { toDataFlowRows } from "./dataFlowRows";

const KIND_ICON = {
  read: ArrowUpFromLine,
  write: ArrowDownToLine,
  call: PhoneOutgoing,
} as const;

/**
 * Two states in one tab, matching the Figma `Node Data Flow` variants: a node
 * whose class touches no field at all (NoAccess), and a picked field's trace
 * (Traced). The picker is not optional — TraceDataFlow needs a field name and
 * there is no sensible default to guess.
 */
export function DataFlowTab({ nodeId, onJump }: { nodeId: string; onJump: (id: string) => void }) {
  const [fields, setFields] = useState<CodeGraphFieldRef[] | null>(null);
  const [picked, setPicked] = useState<CodeGraphFieldRef | null>(null);
  const [trace, setTrace] = useState<CodeGraphDataFlowTrace | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    let live = true;
    setFields(null);
    setPicked(null);
    setTrace(null);
    setError("");
    listCodeGraphFields(nodeId)
      .then((f) => live && setFields(f))
      .catch((e) => live && setError(errorMessage(e)));
    return () => {
      live = false;
    };
  }, [nodeId]);

  useEffect(() => {
    if (!picked) return;
    let live = true;
    setTrace(null);
    traceCodeGraphDataFlow(picked.classId, picked.fieldName)
      .then((t) => live && setTrace(t))
      .catch((e) => live && setError(errorMessage(e)));
    return () => {
      live = false;
    };
  }, [picked]);

  if (error) return <p className="p-3 text-xs text-destructive">{error}</p>;
  if (!fields) return <p className="p-3 text-xs text-muted-foreground">Loading fields…</p>;

  if (fields.length === 0) {
    return (
      <p className="p-3 text-xs text-muted-foreground">
        This node touches no fields, so there is nothing to trace.
      </p>
    );
  }

  const rows = trace ? toDataFlowRows(trace.steps) : [];

  return (
    <div className="flex min-h-0 flex-col gap-2">
      <div className="flex flex-wrap gap-1 px-3 pt-2">
        {fields.map((f) => (
          <Button
            key={`${f.classId}.${f.fieldName}`}
            size="sm"
            variant={picked?.fieldName === f.fieldName ? "secondary" : "ghost"}
            className="h-6 gap-1 px-2 font-mono text-[11px]"
            onClick={() => setPicked(f)}
          >
            {f.fieldName}
            <span className="text-text-tertiary">
              {f.reads}r/{f.writes}w
            </span>
          </Button>
        ))}
      </div>

      {!picked && (
        <p className="px-3 pb-3 text-xs text-muted-foreground">
          Pick a field to trace it through the call graph.
        </p>
      )}

      {picked && !trace && <p className="px-3 pb-3 text-xs text-muted-foreground">Tracing…</p>}

      {trace && (
        <>
          <p className="px-3 text-[11px] text-text-tertiary">
            Ordered by <span className="font-mono">{trace.orderedBy}</span> — line order within each
            method, not execution order.
          </p>
          <ScrollArea className="min-h-0 flex-1">
            <ul className="flex flex-col gap-0.5 px-3 pb-3">
              {rows.map((row) =>
                row.type === "header" ? (
                  <li
                    key={row.key}
                    style={{ paddingLeft: row.depth * 12 }}
                    className="mt-1.5 flex items-baseline gap-1.5 text-xs font-medium text-text-primary"
                  >
                    <span className="truncate">{shortLabel(row.label)}</span>
                    <span className="truncate font-mono text-[10px] text-text-tertiary">
                      {row.filePath}
                    </span>
                  </li>
                ) : (
                  <li key={row.key} style={{ paddingLeft: row.depth * 12 + 10 }}>
                    <button
                      type="button"
                      disabled={!row.targetId}
                      onClick={() => row.targetId && onJump(row.targetId)}
                      className={cn(
                        "flex w-full items-center gap-1.5 rounded px-1 py-0.5 text-left font-mono text-[11px]",
                        row.targetId
                          ? "text-text-secondary hover:bg-surface-raised"
                          : "cursor-default text-text-tertiary",
                      )}
                    >
                      {(() => {
                        const Icon = KIND_ICON[row.kind];
                        return <Icon size={11} className="shrink-0" />;
                      })()}
                      <span className="shrink-0 tabular-nums">:{row.line}</span>
                      <span className="truncate">{row.wall || row.kind}</span>
                    </button>
                  </li>
                ),
              )}
            </ul>
          </ScrollArea>
        </>
      )}
    </div>
  );
}
```

- [ ] **Step 3: Type-check**

Run: `cd frontend && npx tsc --noEmit`
Expected: clean. If `shortLabel` is not exported from `graph/labels.ts`, read that file and use whatever it does export — do not add a second label helper.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/bridge/codegraph.ts frontend/src/modules/codegraph/inspector/DataFlowTab.tsx
git commit -m "feat(codegraph): trace a field from the node inspector"
```

---

### Task 6: `NodeInspector` — the tabbed surface, anchored to the node

The Figma design (`Node Detail`, `Node Data Flow`) is a popover floating beside the clicked node, not a side panel. `@xyflow/react` ships `NodeToolbar` for exactly this: content positioned relative to a node, with no manual coordinate maths and no re-implementation of pan/zoom tracking. Use it rather than Radix `Popover` + `PopoverAnchor`, which would need the flow-to-screen transform wired by hand.

**Files:**
- Create: `frontend/src/modules/codegraph/inspector/NodeInspector.tsx`
- Modify: `frontend/src/modules/codegraph/graph/FlowCanvas.tsx`

- [ ] **Step 1: Build the inspector**

```tsx
import { X } from "lucide-react";
import { NodeToolbar, Position } from "@xyflow/react";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { NodeSnippetPanel } from "../NodeSnippetPanel";
import { DataFlowTab } from "./DataFlowTab";
import type { CodeGraphCodeSnippet } from "@/bridge/codegraph";

export type NodeInspectorProps = {
  nodeId: string;
  snippet: CodeGraphCodeSnippet | null;
  snippetLoading: boolean;
  snippetError: string;
  onEdit?: () => void;
  onJump: (nodeId: string) => void;
  onClose: () => void;
};

/**
 * One node's inspector: source on the Detail tab, the field trace on Data
 * flow. Anchored to the node via NodeToolbar so it tracks pan and zoom for
 * free. The Comments tab arrives in Phase 3.
 *
 * Tabs use the shared component with the `line` variant, not `default`: this
 * surface is `bg-surface-raised`, and `--muted` — what the default variant
 * fills its track with — resolves to that same colour, so the segmented track
 * is invisible here. `line` needs no track.
 */
export function NodeInspector(props: NodeInspectorProps) {
  const { nodeId, snippet, snippetLoading, snippetError, onEdit, onJump, onClose } = props;

  return (
    <NodeToolbar nodeId={nodeId} isVisible position={Position.Right} offset={16}>
      <div className="flex h-[420px] w-105 flex-col overflow-hidden rounded-xl border border-border-subtle bg-surface-raised shadow-[0_12px_32px_rgba(0,0,0,0.45)]">
        <Tabs defaultValue="detail" className="flex min-h-0 flex-1 flex-col gap-0">
          <div className="flex items-center gap-2 border-b border-border-subtle px-3 py-2">
            <TabsList variant="line" className="flex-1">
              <TabsTrigger value="detail">Detail</TabsTrigger>
              <TabsTrigger value="dataflow">Data flow</TabsTrigger>
            </TabsList>
            <Button
              variant="ghost"
              size="icon"
              className="size-6 shrink-0"
              aria-label="Close inspector"
              onClick={onClose}
            >
              <X size={14} />
            </Button>
          </div>

          <TabsContent value="detail" className="min-h-0 overflow-auto">
            <NodeSnippetPanel
              snippet={snippet}
              loading={snippetLoading}
              error={snippetError}
              onEdit={onEdit}
            />
          </TabsContent>

          <TabsContent value="dataflow" className="flex min-h-0 flex-col">
            <DataFlowTab nodeId={nodeId} onJump={onJump} />
          </TabsContent>
        </Tabs>
      </div>
    </NodeToolbar>
  );
}
```

- [ ] **Step 2: Render it from the canvas**

`NodeToolbar` only works inside the `ReactFlow` provider, so the inspector is a child of the canvas, not of `CodeGraphView`. In `FlowCanvas.tsx`, add an `inspector?: React.ReactNode` prop to `FlowCanvasProps` and render `{inspector}` as a child of `<ReactFlow>`, next to the existing `<Background>`/`<Controls>` children.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/modules/codegraph/
git commit -m "feat(codegraph): node inspector with Detail and Data flow tabs"
```

---

### Task 7: Retire the SplitPanel snippet flow

`previewNode` and the `useEffect` that owns the SplitPanel become the inspector's state. The SplitPanel itself stays in the app — other modules use it — this only stops CodeGraph from driving it.

**Files:**
- Modify: `frontend/src/modules/codegraph/codegraphStore.ts`
- Modify: `frontend/src/modules/codegraph/CodeGraphView.tsx`

- [ ] **Step 1: Rename the store field**

In `codegraphStore.ts`, replace `snippetOpen: boolean` with:

```ts
  /** The node whose inspector is open, or null. Replaces snippetOpen: the
   *  inspector is anchored to a specific node, so "which node" IS the
   *  open/closed state. */
  inspectorNodeId: string | null;
```

Update the initial value to `inspectorNodeId: null` and leave `snippet`/`snippetLoading`/`snippetError` alone — the Detail tab still uses all three.

- [ ] **Step 2: Rewrite `previewNode`**

In `CodeGraphView.tsx`, replace the whole `previewNode` function:

```ts
/** Click a node → open its inspector, anchored to that node on the canvas. */
async function inspectNode(nodeId: string) {
  const { subgraph } = snapshot();
  const node = subgraph?.nodes.find((n) => n.id === nodeId);
  if (!node) return;
  set({ inspectorNodeId: nodeId, snippet: null, snippetError: "" });
  // A node with no source yet (a plan node still not_implemented) has an empty
  // filePath — let the Detail tab show its own empty state rather than asking
  // Go for a file that does not exist.
  if (!node.filePath) {
    set({ snippetLoading: false });
    return;
  }
  set({ snippetLoading: true });
  try {
    const snippet = await getCodeGraphCodeSnippet(node.filePath, node.line, node.endLine);
    set({ snippet, snippetLoading: false });
  } catch (e) {
    set({ snippetError: errorMessage(e), snippetLoading: false });
  }
}
```

- [ ] **Step 3: Delete the SplitPanel effect and pass the inspector to the canvas**

Remove the `useSplitPanel` import, the `const { openPanel, closePanel } = useSplitPanel();` line, and the entire `useEffect` that calls `openPanel`. Replace the four `snippetOpen`-derived selectors with `const inspectorNodeId = useCodeGraphStore((s) => s.inspectorNodeId);`.

Pass the inspector into `FlowCanvas`:

```tsx
                <FlowCanvas
                  key={rootId}
                  rootId={rootId}
                  nodes={subgraph.nodes}
                  edges={subgraph.edges}
                  onSelect={(nodeId) => void inspectNode(nodeId)}
                  inspector={
                    inspectorNodeId && (
                      <NodeInspector
                        nodeId={inspectorNodeId}
                        snippet={snippet}
                        snippetLoading={snippetLoading}
                        snippetError={snippetError}
                        onEdit={
                          snippet
                            ? () => {
                                void openFile(snippet.filePath);
                                set({ activeTab: "files", inspectorNodeId: null, snippet: null });
                              }
                            : undefined
                        }
                        onJump={(id) => void inspectNode(id)}
                        onClose={() => set({ inspectorNodeId: null, snippet: null, snippetError: "" })}
                      />
                    )
                  }
                />
```

- [ ] **Step 4: Verify**

Run: `cd frontend && npx tsc --noEmit && bun test`
Expected: clean, all tests pass. `grep -rn "snippetOpen" frontend/src/` must return nothing.

- [ ] **Step 5: Manual check**

Open a Java project, pick an endpoint, click a node. The inspector should appear beside the node, follow it while panning and zooming, show source on Detail, and — on a node whose class has fields — list them on Data flow and render a trace with indentation and wall labels.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/modules/codegraph/
git commit -m "refactor(codegraph): the node inspector replaces the SplitPanel preview"
```

---

# Phase 2 — Plan-first flow

The whole plan lifecycle exists in Go and over MCP and has never been reachable from the UI. This phase adds the bridge and the canvas.

**Boundary to preserve:** `ProposeFlow` is deliberately **not** bridged. An agent proposes a plan over MCP; a human approves it. Adding a UI "propose" button would erase that split. See codegraph.mdx § "Ranh giới: agent chỉ write được metadata plan".

### Task 8: Wails bridge for the plan lifecycle

**Files:**
- Modify: `internal/bridge/codegraph.go`

- [ ] **Step 1: Add four methods**

Capability is `codegraph:plan` per codegraph.mdx § "Bridge & MCP mới" — not `codegraph:read`. Append to `internal/bridge/codegraph.go`:

```go
func (s *AppService) CodeGraphGetPlannedFlow() (codegraph.Plan, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:plan", "project:active", map[string]any{
		"action": "get_planned_flow",
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.GetPlannedFlow(ctx)
	})
	if err != nil {
		return codegraph.Plan{}, err
	}
	return result.(codegraph.Plan), nil
}

func (s *AppService) CodeGraphGetPlanProgress() (codegraph.PlanProgress, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:plan", "project:active", map[string]any{
		"action": "get_plan_progress",
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.GetPlanProgress(ctx)
	})
	if err != nil {
		return codegraph.PlanProgress{}, err
	}
	return result.(codegraph.PlanProgress), nil
}

// CodeGraphApprovePlan is human-only on purpose — approving a plan is a
// person's decision, like KafkaConnect or SSHTrustHostKey. It has no MCP tool.
func (s *AppService) CodeGraphApprovePlan() error {
	_, err := s.call(context.Background(), "builtin.codegraph", "codegraph:plan", "project:active", map[string]any{
		"action": "approve_plan",
	}, func(ctx context.Context) (any, error) {
		return nil, s.container.CodeGraph.ApprovePlan(ctx)
	})
	return err
}

func (s *AppService) CodeGraphRelinkPlanNode(planNodeID, fqn string) error {
	_, err := s.call(context.Background(), "builtin.codegraph", "codegraph:plan", "project:active", map[string]any{
		"action":       "relink_plan_node",
		"plan_node_id": planNodeID,
		"fqn":          fqn,
	}, func(ctx context.Context) (any, error) {
		return nil, s.container.CodeGraph.RelinkPlanNode(ctx, planNodeID, fqn)
	})
	return err
}
```

- [ ] **Step 2: Check the capability is declared**

Run: `grep -rn "codegraph:plan" internal/`
Expected: the capability appears in whatever registry the other `codegraph:*` capabilities are declared in. If it does not, add it there the same way `codegraph:terminal` is declared — find that with `grep -rn "codegraph:terminal" internal/ --include=*.go` and copy the pattern. A bridge call against an undeclared capability fails at runtime, not at compile time.

- [ ] **Step 3: Build and regenerate bindings**

Run: `go build ./... && wails3 generate bindings -clean=true -ts -i`
Expected: `appservice.ts` gains all four; `models.ts` gains `Plan`, `PlanNode`, `PlanEdge`, `PlanProgress`, `PlanNodeProgress`

- [ ] **Step 4: Commit**

```bash
git add internal/bridge/codegraph.go frontend/bindings/
git commit -m "feat(codegraph): bridge the plan lifecycle, minus proposeFlow"
```

---

### Task 9: Frontend plan bridge

**Files:**
- Create: `frontend/src/bridge/codegraphPlan.ts`

- [ ] **Step 1: Write it**

```ts
import { callBridge } from "./client";
import { AppService } from "../../bindings/devkit/internal/bridge";

export type PlanNodeStatus =
  | "not_implemented"
  | "pending_enhancement"
  | "implemented_matching"
  | "implemented_diverged";

export type PlanAggregate = "not_started" | "in_progress" | "complete";

export type CodeGraphPlanNode = {
  planNodeId: string;
  label: string;
  description: string;
  fqn: string;
  kind: "class" | "method";
  status: PlanNodeStatus;
  /** The indexed node the FQN resolved to; empty when not_implemented. */
  realNodeId: string;
};

export type CodeGraphPlanEdge = {
  from: string;
  to: string;
  /** Plan-time documentation for the human reviewing the canvas. Never
   *  machine-verified against real code. */
  condition: string;
};

export type CodeGraphPlanProgress = {
  description: string;
  state: "draft" | "approved";
  aggregate: PlanAggregate;
  nodes: CodeGraphPlanNode[];
};

// PlanNodeProgress embeds PlanNode in Go. encoding/json inlines an embedded
// struct's fields, but confirm the Wails generator did the same before trusting
// the cast below: `grep -n "PlanNodeProgress" -A 12
// frontend/bindings/devkit/internal/codegraph/models.ts`. If the generator
// nested it under a key instead, map the fields explicitly rather than casting.
export async function getCodeGraphPlanProgress(): Promise<CodeGraphPlanProgress> {
  const p = await callBridge(() => AppService.CodeGraphGetPlanProgress());
  return {
    description: p.description,
    state: p.state as "draft" | "approved",
    aggregate: p.aggregate as PlanAggregate,
    nodes: (p.nodes ?? []) as CodeGraphPlanNode[],
  };
}

/** Edges are only on the plan itself — PlanProgress carries nodes alone. */
export async function getCodeGraphPlanEdges(): Promise<CodeGraphPlanEdge[]> {
  const plan = await callBridge(() => AppService.CodeGraphGetPlannedFlow());
  return (plan.edges ?? []) as CodeGraphPlanEdge[];
}

export async function approveCodeGraphPlan(): Promise<void> {
  return callBridge(() => AppService.CodeGraphApprovePlan());
}

export async function relinkCodeGraphPlanNode(planNodeId: string, fqn: string): Promise<void> {
  return callBridge(() => AppService.CodeGraphRelinkPlanNode(planNodeId, fqn));
}
```

- [ ] **Step 2: Type-check and commit**

Run: `cd frontend && npx tsc --noEmit`

```bash
git add frontend/src/bridge/codegraphPlan.ts
git commit -m "feat(codegraph): frontend bridge for the plan lifecycle"
```

---

### Task 10: `planStatus` — status → visual treatment, tested

**Constraint that drives this whole task:** the four layer accents already own `status/info`, `status/success`, `status/warning` and `text/tertiary`. Status therefore **cannot** be encoded by hue — a green plan node would be indistinguishable from a Repository node. Status uses border style and a badge; layer keeps the colour. Verify the exact treatments against the Figma `Plan Node` component (page **14 CodeGraph**, node `232:268`) before finalising the strings.

**Files:**
- Create: `frontend/src/modules/codegraph/plan/planStatus.ts`
- Test: `frontend/src/modules/codegraph/plan/planStatus.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
import { describe, expect, it } from "bun:test";
import { PLAN_STATUS, planStatusOrder, aggregateLabel } from "./planStatus";
import type { PlanNodeStatus } from "@/bridge/codegraphPlan";

const ALL: PlanNodeStatus[] = [
  "not_implemented",
  "pending_enhancement",
  "implemented_matching",
  "implemented_diverged",
];

describe("PLAN_STATUS", () => {
  it("covers every status the backend can return", () => {
    for (const s of ALL) {
      expect(PLAN_STATUS[s]).toBeDefined();
      expect(PLAN_STATUS[s].label.length).toBeGreaterThan(0);
    }
  });

  it("never encodes status with a layer accent colour", () => {
    // The four layer accents are status/info, status/success, status/warning
    // and text/tertiary. A plan node using any of them for STATUS would say
    // the same thing twice, in conflict with the layer icon.
    const banned = ["status-info", "status-success", "status-warning", "text-tertiary"];
    for (const s of ALL) {
      const styles = `${PLAN_STATUS[s].border} ${PLAN_STATUS[s].badge}`;
      for (const token of banned) {
        expect(styles).not.toContain(token);
      }
    }
  });

  it("sorts diverged before matching so problems surface first", () => {
    const sorted = [...ALL].sort((a, b) => planStatusOrder(a) - planStatusOrder(b));
    expect(sorted[0]).toBe("implemented_diverged");
    expect(sorted[sorted.length - 1]).toBe("implemented_matching");
  });
});

describe("aggregateLabel", () => {
  it("names all three aggregates", () => {
    expect(aggregateLabel("not_started")).toBe("Not started");
    expect(aggregateLabel("in_progress")).toBe("In progress");
    expect(aggregateLabel("complete")).toBe("Complete");
  });
});
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd frontend && bun test src/modules/codegraph/plan/planStatus.test.ts`
Expected: FAIL — cannot resolve `./planStatus`

- [ ] **Step 3: Implement**

```ts
import type { PlanAggregate, PlanNodeStatus } from "@/bridge/codegraphPlan";

/**
 * Visual treatment per plan-node status.
 *
 * Deliberately achromatic. The node's layer already spends the four accent
 * colours (status/info, status/success, status/warning, text/tertiary), so
 * status is carried by border style and a neutral badge instead — otherwise a
 * "done" colour and a "Repository" colour would collide on the same card.
 */
export const PLAN_STATUS: Record<
  PlanNodeStatus,
  { label: string; border: string; badge: string; hint: string }
> = {
  not_implemented: {
    label: "Planned",
    border: "border-dashed border-foreground/25",
    badge: "bg-foreground/5 text-foreground/50",
    hint: "No code matches this FQN yet.",
  },
  pending_enhancement: {
    label: "Needs change",
    border: "border-dashed border-foreground/50",
    badge: "bg-foreground/10 text-foreground/70",
    hint: "The method exists but does not yet call what the plan says it should.",
  },
  implemented_matching: {
    label: "Done",
    border: "border-solid border-foreground/20",
    badge: "bg-foreground/10 text-foreground/60",
    hint: "Code matches the plan.",
  },
  implemented_diverged: {
    label: "Diverged",
    border: "border-solid border-2 border-destructive",
    badge: "bg-destructive/15 text-destructive",
    hint: "Code exists but its calls do not match the planned edges.",
  },
};

/** Sort key: what needs attention first. */
export function planStatusOrder(status: PlanNodeStatus): number {
  return {
    implemented_diverged: 0,
    pending_enhancement: 1,
    not_implemented: 2,
    implemented_matching: 3,
  }[status];
}

export function aggregateLabel(aggregate: PlanAggregate): string {
  return { not_started: "Not started", in_progress: "In progress", complete: "Complete" }[
    aggregate
  ];
}
```

> `destructive` is not one of the four layer accents, so the banned-token test passes and the diverged state still reads as a problem.

- [ ] **Step 4: Run it to verify it passes**

Run: `cd frontend && bun test src/modules/codegraph/plan/planStatus.test.ts`
Expected: PASS, 4 tests

- [ ] **Step 5: Commit**

```bash
git add frontend/src/modules/codegraph/plan/
git commit -m "feat(codegraph): plan node status treatments, achromatic by necessity"
```

---

### Task 11: `PlanNode` canvas node

**Files:**
- Create: `frontend/src/modules/codegraph/plan/PlanNode.tsx`
- Modify: `frontend/src/modules/codegraph/graph/FlowCanvas.tsx` (register the node type)

- [ ] **Step 1: Build the node**

Mirrors `graph/ApiNode.tsx` — same layer icon, same size, same handles — plus the status border and badge.

```tsx
import { Handle, Position, type NodeProps } from "@xyflow/react";
import { LAYERS, layerOf } from "../graph/tokens";
import { PLAN_STATUS } from "./planStatus";
import { cn } from "@/lib/utils";
import type { PlanNodeStatus } from "@/bridge/codegraphPlan";

export type PlanNodeData = {
  layer: string;
  title: string;
  fqn: string;
  status: PlanNodeStatus;
  commentCount: number;
};

export function PlanNode({ data, selected }: NodeProps & { data: PlanNodeData }) {
  const visual = LAYERS[layerOf(data.layer)];
  const Icon = visual.icon;
  const accent = `var(${visual.cssVar})`;
  const status = PLAN_STATUS[data.status];

  return (
    <div
      className={cn(
        "group w-67 rounded-xl bg-surface-raised p-3",
        "flex flex-col gap-2.5 border",
        status.border,
        selected && "shadow-[0_6px_16px_rgba(0,0,0,0.45)]",
      )}
      title={status.hint}
    >
      <Handle id="top" type="target" position={Position.Top} className="!opacity-0" />
      <Handle id="left" type="target" position={Position.Left} className="!opacity-0" />
      <Handle id="bottom" type="source" position={Position.Bottom} className="!opacity-0" />

      <div className="flex items-center gap-2.5">
        <span
          className="flex size-7.5 shrink-0 items-center justify-center rounded-[9px]"
          style={{ backgroundColor: `color-mix(in srgb, ${accent} 14%, transparent)` }}
        >
          <Icon size={16} strokeWidth={2} style={{ color: accent }} />
        </span>
        <span className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span
            className="font-mono text-[9px] font-medium tracking-[0.06em]"
            style={{ color: accent }}
          >
            {visual.label}
          </span>
          <span className="truncate text-sm font-semibold text-text-primary">{data.title}</span>
        </span>
        {data.commentCount > 0 && (
          <span className="shrink-0 rounded-full bg-foreground/10 px-1.5 text-[10px] tabular-nums text-foreground/70">
            {data.commentCount}
          </span>
        )}
      </div>

      <div className="flex items-center gap-2">
        <span
          className={cn("rounded px-1.5 py-0.5 text-[10px] font-medium", status.badge)}
        >
          {status.label}
        </span>
        <span
          title={data.fqn}
          className="min-w-0 flex-1 truncate font-mono text-[10px] text-text-tertiary"
        >
          {data.fqn}
        </span>
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Register the type**

In `FlowCanvas.tsx`, change `const nodeTypes = { apiNode: ApiNode };` to include `planNode: PlanNode`. The object must stay module-level — recreating `nodeTypes` per render makes React Flow remount every node on every render.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/modules/codegraph/
git commit -m "feat(codegraph): plan node renders its status without stealing a layer colour"
```

---

### Task 12: Plan mode in the view

**Files:**
- Modify: `frontend/src/modules/codegraph/codegraphStore.ts`
- Create: `frontend/src/modules/codegraph/plan/PlanBar.tsx`
- Modify: `frontend/src/modules/codegraph/CodeGraphView.tsx`
- Modify: `frontend/src/modules/codegraph/CodeGraphSidebar.tsx`

- [ ] **Step 1: Extend the store**

Add to `CodeGraphState`, with the others:

```ts
  plan: CodeGraphPlanProgress | null;
  planEdges: CodeGraphPlanEdge[];
  planError: string;
```

Initial values `null`, `[]`, `""`. Extend the sidebar's `activeTab` union from `"endpoints" | "files"` to `"endpoints" | "files" | "plan"` in both the store and `CodeGraphSidebarProps`.

- [ ] **Step 2: Loader**

In `CodeGraphView.tsx`, beside the other module-level async helpers:

```ts
async function loadPlan() {
  set({ planError: "" });
  try {
    const [plan, planEdges] = await Promise.all([
      getCodeGraphPlanProgress(),
      getCodeGraphPlanEdges(),
    ]);
    set({ plan, planEdges });
  } catch (e) {
    // No plan yet is the normal state for most projects, not an error worth
    // shouting about — the empty state below says what to do instead.
    set({ plan: null, planEdges: [], planError: errorMessage(e) });
  }
}
```

- [ ] **Step 3: `PlanBar`**

```tsx
import { Button } from "@/components/ui/button";
import { aggregateLabel } from "./planStatus";
import type { CodeGraphPlanProgress } from "@/bridge/codegraphPlan";

export function PlanBar({
  plan,
  onApprove,
  onRefresh,
  busy,
}: {
  plan: CodeGraphPlanProgress;
  onApprove: () => void;
  onRefresh: () => void;
  busy: boolean;
}) {
  const done = plan.nodes.filter((n) => n.status === "implemented_matching").length;

  return (
    <div className="flex items-center gap-2 border-b border-border-subtle px-3 py-1.5">
      <span className="text-xs font-medium text-text-primary">{aggregateLabel(plan.aggregate)}</span>
      <span className="font-mono text-[11px] text-text-tertiary">
        {done}/{plan.nodes.length} nodes
      </span>
      {plan.state === "draft" ? (
        <Button size="sm" className="ml-auto h-6 px-2 text-xs" disabled={busy} onClick={onApprove}>
          Approve plan
        </Button>
      ) : (
        <span className="ml-auto text-[11px] text-text-tertiary">approved</span>
      )}
      <Button
        size="sm"
        variant="secondary"
        className="h-6 px-2 text-xs"
        disabled={busy}
        onClick={onRefresh}
      >
        Refresh
      </Button>
    </div>
  );
}
```

- [ ] **Step 4: Sidebar tab + canvas branch**

Add a third `Plan` button next to Endpoints/Files in `CodeGraphSidebar.tsx`, listing `plan.nodes` sorted by `planStatusOrder`, each row showing the status badge and FQN. In `CodeGraphView.tsx` add an `activeTab === "plan"` branch that renders `PlanBar` above a `FlowCanvas` fed from `plan.nodes` mapped to `type: "planNode"` and `planEdges` mapped to edges with the `condition` as the edge label.

Call `void loadPlan()` when the user switches to the Plan tab, and after `reindex` completes — the diff is recomputed on read, so a fresh index means fresh statuses.

- [ ] **Step 5: Verify**

Run: `cd frontend && npx tsc --noEmit && bun test`

Manual: with no plan, the tab shows an empty state naming `codegraph.proposeFlow` as the way one gets created. With a plan proposed over MCP, nodes render with statuses, `Approve plan` flips draft→approved and then disappears.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/modules/codegraph/
git commit -m "feat(codegraph): plan canvas with per-node status and approval"
```

---

### Task 13: Relink from the inspector

The escape hatch for when the agent named a class differently from the plan.

**Files:**
- Modify: `frontend/src/modules/codegraph/inspector/NodeInspector.tsx`

- [ ] **Step 1: Add an optional relink affordance**

Add optional props `planNodeId?: string` and `onRelink?: (fqn: string) => void`. When `planNodeId` is set, the Detail tab shows the plan node's FQN with an inline edit that calls `onRelink` on submit. When it is not set — every non-plan node — nothing changes.

Wire `onRelink` in `CodeGraphView.tsx` to `relinkCodeGraphPlanNode(planNodeId, fqn)` followed by `void loadPlan()`.

- [ ] **Step 2: Verify and commit**

Run: `cd frontend && npx tsc --noEmit`

```bash
git add frontend/src/modules/codegraph/
git commit -m "feat(codegraph): repoint a plan node at a different FQN"
```

---

# Phase 3 — Plan node comments (storage and UI)

Zero code exists for this: `grep -rn "Comment" internal/ --include=*.go` returns nothing. This phase builds the storage, the two write paths, and the thread UI.

> **The agent-calling half stays a TODO.** Per codegraph.mdx § "Gửi vào đúng phiên đang chạy", a comment written while an `AgentSession` is implementing the plan must go straight into that session as a scoped `session/prompt`, and the UI must distinguish `idle`/`running` (Live) from `awaiting_permission` (Blocked), `error`, and no-session (queued). **Task 17 stubs that boundary and does not implement it.** Everything else in this phase works without it: comments are stored, shown, and handed to the agent on its next `getPlannedFlow`.

### Task 14: Comment storage

**Files:**
- Create: `internal/codegraph/comments.go`
- Modify: `internal/codegraph/codegraph.go` (type + `Store` interface), `internal/codegraph/store.go` (schema + clear list)
- Test: `internal/codegraph/comments_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/codegraph/comments_test.go
package codegraph

import (
	"context"
	"testing"
)

func TestStoreAddAndListComments(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := store.AddComment(ctx, PlanComment{
		PlanNodeID: "pn-001", CommentID: "c1", Author: "user", Body: "use a queue here", CreatedAt: 100,
	}); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if err := store.AddComment(ctx, PlanComment{
		PlanNodeID: "pn-001", CommentID: "c2", Author: "agent", Body: "ok, switching", CreatedAt: 200, SessionID: "s1",
	}); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if err := store.AddComment(ctx, PlanComment{
		PlanNodeID: "pn-002", CommentID: "c3", Author: "user", Body: "unrelated", CreatedAt: 300,
	}); err != nil {
		t.Fatalf("AddComment: %v", err)
	}

	thread, err := store.ListComments(ctx, "pn-001")
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(thread) != 2 {
		t.Fatalf("thread = %+v, want 2", thread)
	}
	if thread[0].CommentID != "c1" || thread[1].CommentID != "c2" {
		t.Fatalf("thread = %+v, want oldest first", thread)
	}
	if thread[1].Author != "agent" || thread[1].SessionID != "s1" {
		t.Fatalf("reply = %+v, want author agent in session s1", thread[1])
	}

	counts, err := store.CountComments(ctx)
	if err != nil {
		t.Fatalf("CountComments: %v", err)
	}
	if counts["pn-001"] != 2 || counts["pn-002"] != 1 {
		t.Fatalf("counts = %+v", counts)
	}
}

// A new plan replaces the old one, and its threads go with it — comments do
// NOT migrate to a same-FQN node in the new plan (codegraph.mdx § Non-goals).
func TestStoreSavePlanClearsComments(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := store.AddComment(ctx, PlanComment{
		PlanNodeID: "pn-001", CommentID: "c1", Author: "user", Body: "x", CreatedAt: 1,
	}); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if err := store.SavePlan(ctx, Plan{
		Description: "next",
		State:       PlanStateDraft,
		Nodes:       []PlanNode{{PlanNodeID: "pn-001", Label: "A", FQN: "com.A", Kind: "class"}},
	}); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}

	thread, err := store.ListComments(ctx, "pn-001")
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(thread) != 0 {
		t.Fatalf("thread = %+v, want empty after a new plan replaced the old one", thread)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/codegraph/ -run TestStore.*Comment -v`
Expected: FAIL — `undefined: PlanComment`

- [ ] **Step 3: Add the type**

In `internal/codegraph/codegraph.go`, next to the other plan types:

```go
// PlanComment is one message on a plan node's thread. Author is "user" or
// "agent". SessionID records which agent session the comment was delivered
// into, for reading the thread back later — it is not used for routing.
//
// Threads are read-only history: there is no resolve, reopen, edit or delete
// (codegraph.mdx § Non-goals). A thread dies with the plan it belongs to.
type PlanComment struct {
	PlanNodeID string `json:"planNodeId"`
	CommentID  string `json:"commentId"`
	Author     string `json:"author"`
	Body       string `json:"body"`
	CreatedAt  int64  `json:"createdAt"` // unix seconds
	SessionID  string `json:"sessionId"`
}
```

Add to the `Store` interface, under `SetPlanNodeFQN`:

```go
	AddComment(ctx context.Context, c PlanComment) error
	ListComments(ctx context.Context, planNodeID string) ([]PlanComment, error)
	CountComments(ctx context.Context) (map[string]int, error)
```

- [ ] **Step 4: Add the schema**

In `internal/codegraph/store.go`, in the same `CREATE TABLE` block as the other tables (near line 43):

```sql
CREATE TABLE IF NOT EXISTS plan_node_comments (
	plan_node_id TEXT NOT NULL,
	comment_id   TEXT NOT NULL PRIMARY KEY,
	author       TEXT NOT NULL,
	body         TEXT NOT NULL,
	created_at   INTEGER NOT NULL,
	session_id   TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_plan_node_comments ON plan_node_comments(plan_node_id, created_at);
```

> `project_id` from the doc's sketch is dropped on purpose: each project already gets its own SQLite file, exactly as for `nodes`, `edges` and `plan_nodes`. A column that is the same value in every row of every database buys nothing.

- [ ] **Step 5: Implement the three methods**

Create `internal/codegraph/comments.go`:

```go
// internal/codegraph/comments.go
package codegraph

import (
	"context"
	"fmt"
)

func (s *sqliteStore) AddComment(ctx context.Context, c PlanComment) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO plan_node_comments (plan_node_id, comment_id, author, body, created_at, session_id)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		c.PlanNodeID, c.CommentID, c.Author, c.Body, c.CreatedAt, c.SessionID)
	if err != nil {
		return fmt.Errorf("codegraph: AddComment: %w", err)
	}
	return nil
}

// ListComments returns one node's thread, oldest first.
func (s *sqliteStore) ListComments(ctx context.Context, planNodeID string) ([]PlanComment, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT plan_node_id, comment_id, author, body, created_at, session_id
		   FROM plan_node_comments WHERE plan_node_id = ? ORDER BY created_at, comment_id`,
		planNodeID)
	if err != nil {
		return nil, fmt.Errorf("codegraph: ListComments: %w", err)
	}
	defer rows.Close()

	var out []PlanComment
	for rows.Next() {
		var c PlanComment
		if err := rows.Scan(&c.PlanNodeID, &c.CommentID, &c.Author, &c.Body, &c.CreatedAt, &c.SessionID); err != nil {
			return nil, fmt.Errorf("codegraph: scan comment: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CountComments returns the thread length per plan node, for the canvas badge.
// One query rather than one per node — the canvas renders every node at once.
func (s *sqliteStore) CountComments(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT plan_node_id, COUNT(*) FROM plan_node_comments GROUP BY plan_node_id`)
	if err != nil {
		return nil, fmt.Errorf("codegraph: CountComments: %w", err)
	}
	defer rows.Close()

	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, fmt.Errorf("codegraph: scan comment count: %w", err)
		}
		out[id] = n
	}
	return out, rows.Err()
}
```

- [ ] **Step 6: Clear comments when a new plan replaces the old**

In `internal/codegraph/planstore.go`, inside `SavePlan`, alongside wherever it already clears `plan_nodes`/`plan_edges`, add a delete of `plan_node_comments`. It must be inside the same transaction — read the existing function and follow its shape rather than adding a bare `Exec` outside it.

- [ ] **Step 7: Add the `nilStore` stubs**

```go
func (nilStore) AddComment(context.Context, PlanComment) error { return ErrNoActiveProject }
func (nilStore) ListComments(context.Context, string) ([]PlanComment, error) {
	return nil, ErrNoActiveProject
}
func (nilStore) CountComments(context.Context) (map[string]int, error) {
	return nil, ErrNoActiveProject
}
```

- [ ] **Step 8: Run the tests**

Run: `go test ./internal/codegraph/ -v`
Expected: PASS, whole package including the two new tests

- [ ] **Step 9: Commit**

```bash
git add internal/codegraph/
git commit -m "feat(codegraph): store plan node comment threads"
```

---

### Task 15: Comment service methods

**Files:**
- Modify: `internal/codegraph/service.go`
- Test: `internal/codegraph/comments_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestServiceCommentPlanNodeStoresAndStamps(t *testing.T) {
	svc, _ := newTestService(t) // follow service_test.go's existing helper
	ctx := context.Background()

	if err := svc.CommentPlanNode(ctx, "pn-001", "make it idempotent"); err != nil {
		t.Fatalf("CommentPlanNode: %v", err)
	}
	thread, err := svc.ListPlanNodeComments(ctx, "pn-001")
	if err != nil {
		t.Fatalf("ListPlanNodeComments: %v", err)
	}
	if len(thread) != 1 {
		t.Fatalf("thread = %+v, want 1", thread)
	}
	if thread[0].Author != "user" {
		t.Fatalf("author = %q, want user", thread[0].Author)
	}
	if thread[0].CommentID == "" || thread[0].CreatedAt == 0 {
		t.Fatalf("comment = %+v, want a generated id and timestamp", thread[0])
	}
}

func TestServiceReplyPlanNodeIsAttributedToTheAgent(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	if err := svc.ReplyPlanNode(ctx, "pn-001", "done", "sess-7"); err != nil {
		t.Fatalf("ReplyPlanNode: %v", err)
	}
	thread, _ := svc.ListPlanNodeComments(ctx, "pn-001")
	if thread[0].Author != "agent" || thread[0].SessionID != "sess-7" {
		t.Fatalf("reply = %+v, want author agent in session sess-7", thread[0])
	}
}

func TestServiceCommentPlanNodeRejectsEmptyBody(t *testing.T) {
	svc, _ := newTestService(t)
	if err := svc.CommentPlanNode(context.Background(), "pn-001", "   "); err == nil {
		t.Fatal("expected an error for a whitespace-only comment")
	}
}
```

> `newTestService` is a sketch — read `internal/codegraph/service_test.go` and use whatever helper that file already has for a service backed by a real store.

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/codegraph/ -run TestService.*PlanNode -v`
Expected: FAIL — undefined methods

- [ ] **Step 3: Implement**

Append to `internal/codegraph/comments.go`:

```go
// CommentPlanNode records a human comment on a plan node.
//
// The comment is agent-readable by design: it is returned with the plan on the
// next getPlannedFlow, so it steers implementation rather than being a private
// note (codegraph.mdx § "Comment agent đọc được").
//
// TODO(agent delivery): when an AgentSession is implementing this plan, the
// comment must ALSO go straight into that session as a scoped session/prompt
// rather than waiting for the next getPlannedFlow. See Task 17 — deliberately
// not implemented here; storing always happens either way.
func (s *Service) CommentPlanNode(ctx context.Context, planNodeID, body string) error {
	body = strings.TrimSpace(body)
	if body == "" {
		return fmt.Errorf("codegraph: comment body is empty")
	}
	store, err := s.activeStore()
	if err != nil {
		return err
	}
	return store.AddComment(ctx, PlanComment{
		PlanNodeID: planNodeID,
		CommentID:  newCommentID(),
		Author:     "user",
		Body:       body,
		CreatedAt:  time.Now().Unix(),
	})
}

// ReplyPlanNode records an agent reply. This is an MCP write, and it stays
// inside the plan-metadata boundary: text only, no code touched, no effect
// until a person reads it.
func (s *Service) ReplyPlanNode(ctx context.Context, planNodeID, body, sessionID string) error {
	body = strings.TrimSpace(body)
	if body == "" {
		return fmt.Errorf("codegraph: reply body is empty")
	}
	store, err := s.activeStore()
	if err != nil {
		return err
	}
	return store.AddComment(ctx, PlanComment{
		PlanNodeID: planNodeID,
		CommentID:  newCommentID(),
		Author:     "agent",
		Body:       body,
		CreatedAt:  time.Now().Unix(),
		SessionID:  sessionID,
	})
}

func (s *Service) ListPlanNodeComments(ctx context.Context, planNodeID string) ([]PlanComment, error) {
	store, err := s.activeStore()
	if err != nil {
		return nil, err
	}
	return store.ListComments(ctx, planNodeID)
}

func (s *Service) CountPlanNodeComments(ctx context.Context) (map[string]int, error) {
	store, err := s.activeStore()
	if err != nil {
		return nil, err
	}
	return store.CountComments(ctx)
}

func newCommentID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failing is not recoverable and not worth a second id
		// scheme; a timestamp collision would corrupt the thread's identity.
		panic(fmt.Sprintf("codegraph: rand: %v", err))
	}
	return hex.EncodeToString(b[:])
}
```

Add `crypto/rand`, `encoding/hex`, `strings`, `time` to the file's imports.

- [ ] **Step 4: Run it to verify it passes**

Run: `go test ./internal/codegraph/ -v`

- [ ] **Step 5: Commit**

```bash
git add internal/codegraph/
git commit -m "feat(codegraph): comment and reply on plan nodes"
```

---

### Task 16: Comment bridge and MCP tool

**Files:**
- Modify: `internal/bridge/codegraph.go`, `internal/mcp/server.go`, `internal/mcp/tools_codegraph_plan.go`

- [ ] **Step 1: Bridge (human write + reads)**

```go
func (s *AppService) CodeGraphCommentPlanNode(planNodeID, body string) error {
	_, err := s.call(context.Background(), "builtin.codegraph", "codegraph:plan", "project:active", map[string]any{
		"action":       "comment_plan_node",
		"plan_node_id": planNodeID,
	}, func(ctx context.Context) (any, error) {
		return nil, s.container.CodeGraph.CommentPlanNode(ctx, planNodeID, body)
	})
	return err
}

func (s *AppService) CodeGraphListPlanNodeComments(planNodeID string) ([]codegraph.PlanComment, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:plan", "project:active", map[string]any{
		"action":       "list_plan_node_comments",
		"plan_node_id": planNodeID,
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.ListPlanNodeComments(ctx, planNodeID)
	})
	if err != nil {
		return nil, err
	}
	return result.([]codegraph.PlanComment), nil
}

func (s *AppService) CodeGraphCountPlanNodeComments() (map[string]int, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:plan", "project:active", map[string]any{
		"action": "count_plan_node_comments",
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.CountPlanNodeComments(ctx)
	})
	if err != nil {
		return nil, err
	}
	return result.(map[string]int), nil
}
```

The body is deliberately absent from the audit payload — comment text is content, not a parameter worth logging on every call. Check how the other bridge methods treat free text (`CodeGraphWriteFile` omits `content` for the same reason) and stay consistent.

- [ ] **Step 2: MCP — the agent's reply tool**

In `internal/mcp/tools_codegraph_plan.go`, register `codegraph.replyPlanNode` (effect `execute`) following the shape of `codegraph.relinkPlanNode` right above it, taking `planNodeId`, `body` and `sessionId`. Add `ReplyPlanNode` to the `CodeGraphAPI` interface in `internal/mcp/server.go` and to `fakeCodeGraphAPI` in `internal/mcp/tools_codegraph_test.go`.

- [ ] **Step 3: MCP — hand threads to the agent with the plan**

`getPlannedFlow` must return each node's comments, per codegraph.mdx: "Comment agent đọc được — trả kèm trong getPlannedFlow". `plannedFlowResult` (see `internal/mcp/tools_codegraph_plan_test.go:48`) is already a wrapper around the plan, so attach a `comments` map there rather than changing the `Plan` struct — `Plan` is what `SavePlan`/`GetPlan` round-trip, and widening it would ripple through every existing plan test for no gain.

- [ ] **Step 4: Verify and regenerate bindings**

Run: `go build ./... && go test ./internal/... && wails3 generate bindings -clean=true -ts -i`

- [ ] **Step 5: Commit**

```bash
git add internal/ frontend/bindings/
git commit -m "feat(codegraph): comments reach the agent, replies reach the human"
```

---

### Task 17: TODO — deliver a comment into the running agent session

**This task is deliberately not implemented.** It is written down so the boundary is explicit rather than forgotten.

- [ ] **Step 1: Record the boundary in code**

Add to `internal/codegraph/comments.go`, below `CommentPlanNode`:

```go
// TODO(agent session delivery): CommentPlanNode currently only stores.
//
// codegraph.mdx § "Gửi vào đúng phiên đang chạy, không phải hàng đợi" requires
// that when an AgentSession is implementing this plan, the comment is
// delivered into THAT session as a scoped session/prompt — same conversation
// as the SplitPanel chat, not a side channel — and that the UI distinguishes
// four session states, because the awaiting_permission case is the one where
// the user presses send and nothing happens:
//
//	idle, running          -> Live: delivered immediately as a prompt
//	awaiting_permission    -> Blocked: queued behind the permission prompt;
//	                          the user must resolve it in Agent Hub first
//	error                  -> not delivered; waits for the next getPlannedFlow
//	done, or no session    -> stored; handed over on the next getPlannedFlow
//
// Doing this needs a link from a plan to the AgentSession implementing it,
// which does not exist yet — nothing today records "this session is working on
// this plan". That link is the real first step, not the prompt call.
```

- [ ] **Step 2: Give the UI an honest placeholder**

The Comments tab (Task 18) shows the **None / queued** state unconditionally: "Saved. The agent will see this the next time it reads the plan." That is currently true in every case. Do not show a Live badge that the backend cannot honour.

- [ ] **Step 3: Commit**

```bash
git add internal/codegraph/comments.go
git commit -m "docs(codegraph): name what comment delivery into a live session still needs"
```

---

### Task 18: Comments tab

**Files:**
- Modify: `frontend/src/bridge/codegraphPlan.ts`, `frontend/src/modules/codegraph/inspector/NodeInspector.tsx`
- Create: `frontend/src/modules/codegraph/inspector/CommentsTab.tsx`

- [ ] **Step 1: Bridge wrappers**

Add `listCodeGraphPlanNodeComments`, `commentCodeGraphPlanNode` and `countCodeGraphPlanNodeComments` to `codegraphPlan.ts`, following the file's existing `callBridge` shape, with:

```ts
export type CodeGraphPlanComment = {
  planNodeId: string;
  commentId: string;
  author: "user" | "agent";
  body: string;
  createdAt: number;
  sessionId: string;
};
```

- [ ] **Step 2: Build the tab**

```tsx
import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { cn } from "@/lib/utils";
import {
  commentCodeGraphPlanNode,
  listCodeGraphPlanNodeComments,
  type CodeGraphPlanComment,
} from "@/bridge/codegraphPlan";
import { errorMessage } from "@/bridge/errors";

/**
 * One plan node's comment thread.
 *
 * The banner is not decoration. A comment here is returned to the agent with
 * the plan and steers what gets built, so the UI has to say that outright —
 * letting a user believe they are writing a private note would make the
 * feature actively misleading (codegraph.mdx § "Comment agent đọc được").
 */
export function CommentsTab({
  planNodeId,
  onCountChange,
}: {
  planNodeId: string;
  onCountChange?: (n: number) => void;
}) {
  const [thread, setThread] = useState<CodeGraphPlanComment[] | null>(null);
  const [draft, setDraft] = useState("");
  const [sending, setSending] = useState(false);
  const [error, setError] = useState("");

  async function reload() {
    try {
      const next = await listCodeGraphPlanNodeComments(planNodeId);
      setThread(next);
      onCountChange?.(next.length);
    } catch (e) {
      setError(errorMessage(e));
    }
  }

  useEffect(() => {
    setThread(null);
    setError("");
    void reload();
    // reload closes over planNodeId only; onCountChange is called, not read.
  }, [planNodeId]);

  async function send() {
    const body = draft.trim();
    if (!body) return;
    setSending(true);
    setError("");
    try {
      await commentCodeGraphPlanNode(planNodeId, body);
      setDraft("");
      await reload();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setSending(false);
    }
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <p className="border-b border-border-subtle px-3 py-1.5 text-[11px] text-text-tertiary">
        The agent reads this and it steers the implementation — not a private note.
      </p>

      <ScrollArea className="min-h-0 flex-1">
        <ul className="flex flex-col gap-2 p-3">
          {thread?.map((c) => (
            <li
              key={c.commentId}
              className={cn(
                "rounded-lg px-2.5 py-1.5 text-xs",
                c.author === "agent"
                  ? "bg-surface-tinted text-text-secondary"
                  : "bg-foreground/5 text-text-primary",
              )}
            >
              <span className="mb-0.5 block font-mono text-[10px] text-text-tertiary">
                {c.author}
              </span>
              <span className="whitespace-pre-wrap">{c.body}</span>
            </li>
          ))}
          {thread?.length === 0 && (
            <li className="text-xs text-muted-foreground">No comments on this node yet.</li>
          )}
          {!thread && <li className="text-xs text-muted-foreground">Loading…</li>}
        </ul>
      </ScrollArea>

      <div className="flex flex-col gap-1.5 border-t border-border-subtle p-3">
        <textarea
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          rows={3}
          placeholder="Tell the agent what to do differently here…"
          className="w-full resize-none rounded-md border border-border-subtle bg-surface-tinted px-2.5 py-1.5 text-xs text-text-primary placeholder:text-text-tertiary focus-visible:border-ring focus-visible:outline-none"
        />
        <div className="flex items-center gap-2">
          <Button
            size="sm"
            className="h-6 px-2 text-xs"
            disabled={sending || !draft.trim()}
            onClick={() => void send()}
          >
            {sending ? "Saving…" : "Send"}
          </Button>
          {/* Task 17: delivery into a live session is not built. Saying "sent
              to the agent" here would be a lie in every current case. */}
          <span className="text-[11px] text-text-tertiary">
            Saved. The agent sees this next time it reads the plan.
          </span>
        </div>
        {error && <p className="text-[11px] text-destructive">{error}</p>}
      </div>
    </div>
  );
}
```

- [ ] **Step 3: Add the tab to the inspector**

Add a third `TabsTrigger` rendered only when `planNodeId` is set, with the count badge:

```tsx
<TabsTrigger value="comments">
  Comments
  {commentCount > 0 && <TabsCount>{commentCount}</TabsCount>}
</TabsTrigger>
```

- [ ] **Step 4: Feed counts to the canvas**

Call `countCodeGraphPlanNodeComments()` in `loadPlan()` and pass each node's count into `PlanNodeData.commentCount`, which Task 11 already renders.

- [ ] **Step 5: Verify and commit**

Run: `cd frontend && npx tsc --noEmit && bun test`

```bash
git add frontend/src/
git commit -m "feat(codegraph): comment threads on plan nodes"
```

---

# Phase 4 — Blast radius and index freshness

Two backend features that have been fully built and never surfaced.

### Task 19: Blast radius in the Files tab

`GetBlastRadius(filePath) -> []string` is **file-scoped**, so it belongs beside the file tree, not in the node inspector. Putting it on a node would mean answering a file question with a method selected.

**Files:**
- Modify: `frontend/src/bridge/codegraph.ts`
- Create: `frontend/src/modules/codegraph/BlastRadiusPanel.tsx`
- Modify: `frontend/src/modules/codegraph/CodeGraphView.tsx`

- [ ] **Step 1: Bridge wrappers**

```ts
/** Every file that transitively depends on this one — what a change here can break. */
export async function getCodeGraphBlastRadius(filePath: string): Promise<string[]> {
  return callBridge(async () => (await AppService.CodeGraphGetBlastRadius(filePath)) ?? []);
}

/** Blocks until no re-index is pending; resolves to the index generation. */
export async function waitForCodeGraphIndexReady(): Promise<number> {
  return callBridge(() => AppService.CodeGraphWaitForIndexReady());
}
```

- [ ] **Step 2: The panel**

```tsx
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { getCodeGraphBlastRadius } from "@/bridge/codegraph";
import { errorMessage } from "@/bridge/errors";

/**
 * What a change to the open file can break. File-scoped by nature — the
 * backend answers per file, so this lives beside the file tree rather than in
 * the node inspector.
 */
export function BlastRadiusPanel({
  filePath,
  onOpenFile,
}: {
  filePath: string;
  onOpenFile: (path: string) => void;
}) {
  const [files, setFiles] = useState<string[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function load() {
    setBusy(true);
    setError("");
    try {
      setFiles(await getCodeGraphBlastRadius(filePath));
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="border-t border-border-subtle px-3 py-1.5">
      <div className="flex items-center gap-2">
        <Button
          size="sm"
          variant="secondary"
          className="h-6 px-2 text-xs"
          disabled={busy}
          onClick={() => void load()}
        >
          {busy ? "Checking…" : "Impact"}
        </Button>
        {/* An empty list and a failed call look identical if both render as
            nothing, so the no-dependents case says so in words. */}
        {files?.length === 0 && (
          <span className="text-[11px] text-text-tertiary">
            Nothing else depends on this file.
          </span>
        )}
        {files && files.length > 0 && (
          <span className="font-mono text-[11px] text-status-warning">
            {files.length} dependent file(s)
          </span>
        )}
        {error && <span className="text-[11px] text-destructive">{error}</span>}
      </div>

      {files && files.length > 0 && (
        <ul className="mt-1 max-h-32 overflow-y-auto">
          {files.map((f) => (
            <li key={f}>
              <button
                type="button"
                onClick={() => onOpenFile(f)}
                className="w-full truncate rounded px-1 py-0.5 text-left font-mono text-[11px] text-text-secondary hover:bg-surface-raised"
              >
                {f}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
```

Render it in `CodeGraphView`'s `activeTab === "files"` branch, below the editor, when `selectedFile` is set. Key it on `selectedFile` so switching files clears the previous result rather than showing a stale impact list.

- [ ] **Step 3: Verify and commit**

Run: `cd frontend && npx tsc --noEmit`

```bash
git add frontend/src/
git commit -m "feat(codegraph): show what a file change can break"
```

---

### Task 20: Tell the frontend when the index changed

`runReindex` fires on a debounced fsnotify trigger and the UI learns nothing — endpoint lists and plan statuses go stale with no way to know. Per codegraph.mdx § "Diff tính lại tự động sau mỗi incremental re-index", the frontend should not need a button.

**Files:**
- Modify: `internal/codegraph/service.go`, `main.go`
- Modify: `frontend/src/bridge/codegraph.ts`, `frontend/src/modules/codegraph/CodeGraphView.tsx`

- [ ] **Step 1: Add a callback to the service**

Mirror the existing `terminal.NewRegistry(func(...))` and agenthub callback pattern rather than importing Wails into `internal/codegraph` — the package has no application dependency today and should keep it that way.

```go
// OnIndexUpdated is called after every successful (re-)index, on the
// background goroutine that performed it. Non-blocking implementations only:
// a slow callback stalls the watch loop. nil means nobody is listening.
OnIndexUpdated func(generation int)
```

Call it at the end of `runReindex` on success only.

- [ ] **Step 2: Wire it in `main.go`**

Beside the other emitters (`emitAgentHubEvent`, ~line 389):

```go
container.CodeGraph.OnIndexUpdated = func(generation int) {
	emitAgentHubEvent("codegraph:indexUpdated", map[string]any{"generation": generation})
}
```

> `emitAgentHubEvent` is misnamed for this use — it is a generic "emit if the app exists" helper. Rename it to `emitAppEvent` and update its three existing call sites in the same commit; do not add a second copy of the same four lines.

- [ ] **Step 3: Subscribe on the frontend**

In `frontend/src/bridge/codegraph.ts`, following `agenthub.ts:242`'s exact `Events.On` shape:

```ts
const INDEX_UPDATED_EVENT = "codegraph:indexUpdated";

/** Returns an unsubscribe function, like the agenthub subscriptions. */
export function onCodeGraphIndexUpdated(handler: (generation: number) => void): () => void {
  return Events.On(INDEX_UPDATED_EVENT, (ev) => {
    handler(Number(ev.data?.generation ?? 0));
  });
}
```

In `CodeGraphView`, subscribe in a `useEffect` and on each event refresh whatever the user is currently looking at: endpoints, the open subgraph, and the plan if the Plan tab is active. Do not refresh the file editor — that would discard unsaved edits.

- [ ] **Step 4: Verify**

Run: `go build ./... && go test ./internal/codegraph/ && cd frontend && npx tsc --noEmit`

Manual: with a project open, edit a `.java` file outside DevKit. Within ~1s the endpoint count updates without any click.

- [ ] **Step 5: Commit**

```bash
git add internal/ main.go frontend/src/
git commit -m "feat(codegraph): the UI follows the index instead of going stale"
```

---

# Phase 5 — Terminal in the bottom slot

The bottom slot ships with no tenant, and the terminal backend works end to end except that its output goes nowhere.

### Task 21: Emit PTY output

**Files:**
- Modify: `main.go` (the callback at line 201)

- [ ] **Step 1: Replace the TODO**

```go
terminalRegistry := terminal.NewRegistry(func(sessionID string, chunk []byte) {
	// Wails' Emit is non-blocking, which is what readLoop needs — a slow
	// consumer must never stall the PTY reader. The chunk crosses as a
	// base64 string: encoding/json renders []byte that way, which is also
	// the only lossless option here, since PTY output is arbitrary bytes and
	// a multibyte rune can straddle two chunks.
	emitAppEvent("terminal:output", map[string]any{
		"sessionId": sessionID,
		"chunk":     chunk,
	})
})
```

> **If Phase 4 has not run**, the helper is still called `emitAgentHubEvent` —
> use that name here and leave the rename to Task 20. The phases are
> independent, and this is the one symbol they share.

- [ ] **Step 2: Verify the wire shape before building any UI**

Run the app, start a terminal session, and log the raw event payload in the browser console. Confirm `chunk` arrives as a base64 string, not an array of numbers. If it arrives as an array, decode accordingly in Task 22 — do not guess, this is exactly the assumption the original TODO refused to make.

- [ ] **Step 3: Commit**

```bash
git add main.go
git commit -m "feat(terminal): PTY output reaches the frontend"
```

---

### Task 22: Terminal bridge

**Files:**
- Create: `frontend/src/bridge/terminal.ts`

- [ ] **Step 1: Write it**

```ts
import { Events } from "@wailsio/runtime";
import { callBridge } from "./client";
import { AppService } from "../../bindings/devkit/internal/bridge";

const OUTPUT_EVENT = "terminal:output";

export async function startCodeGraphTerminal(rows: number, cols: number): Promise<string> {
  return callBridge(() => AppService.CodeGraphStartTerminal(rows, cols));
}

export async function writeTerminal(sessionId: string, data: string): Promise<void> {
  return callBridge(() => AppService.TerminalWrite(sessionId, [...new TextEncoder().encode(data)]));
}

export async function resizeTerminal(sessionId: string, rows: number, cols: number): Promise<void> {
  return callBridge(() => AppService.TerminalResize(sessionId, rows, cols));
}

export async function closeTerminal(sessionId: string): Promise<void> {
  return callBridge(() => AppService.TerminalClose(sessionId));
}

/** Subscribes to one session's output. Returns an unsubscribe function. */
export function onTerminalOutput(
  sessionId: string,
  handler: (chunk: Uint8Array) => void,
): () => void {
  return Events.On(OUTPUT_EVENT, (ev) => {
    if (ev.data?.sessionId !== sessionId) return;
    const raw = ev.data.chunk;
    handler(
      typeof raw === "string"
        ? Uint8Array.from(atob(raw), (c) => c.charCodeAt(0))
        : Uint8Array.from(raw ?? []),
    );
  });
}
```

> `AppService.TerminalWrite` takes a Go `[]byte`. Check the generated binding's TypeScript signature before assuming the array-of-numbers form above — if the generator produced `string`, pass the text directly and drop the `TextEncoder`.

- [ ] **Step 2: Type-check and commit**

Run: `cd frontend && npx tsc --noEmit`

```bash
git add frontend/src/bridge/terminal.ts
git commit -m "feat(terminal): frontend bridge"
```

---

### Task 23: Terminal pane in the bottom slot

**Files:**
- Modify: `frontend/package.json`
- Create: `frontend/src/modules/codegraph/terminal/TerminalPane.tsx`
- Modify: `frontend/src/modules/codegraph/CodeGraphView.tsx`

- [ ] **Step 1: Add the dependency**

Run: `cd frontend && bun add @xterm/xterm @xterm/addon-fit`

- [ ] **Step 2: Build the pane**

```tsx
import { useEffect, useRef } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import {
  closeTerminal,
  onTerminalOutput,
  resizeTerminal,
  startCodeGraphTerminal,
  writeTerminal,
} from "@/bridge/terminal";

/**
 * A shell rooted at the open project, in the bottom slot.
 *
 * Everything lives in one effect with an empty dep array: the xterm instance,
 * the PTY session and the subscription have exactly the same lifetime, and
 * splitting them across effects is how you end up with two shells or a write
 * landing after dispose. The `disposed` flag guards the async gap between
 * mount and the session id arriving.
 */
export function TerminalPane() {
  const hostRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;

    let disposed = false;
    let sessionId = "";
    let offOutput: (() => void) | undefined;

    const term = new Terminal({
      fontFamily: "var(--font-mono, monospace)",
      fontSize: 12,
      convertEol: true,
      cursorBlink: true,
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(host);
    fit.fit();

    void (async () => {
      try {
        const id = await startCodeGraphTerminal(term.rows, term.cols);
        if (disposed) {
          // Unmounted while starting — close the shell we just opened rather
          // than leaking a PTY nobody is reading.
          void closeTerminal(id);
          return;
        }
        sessionId = id;
        offOutput = onTerminalOutput(id, (chunk) => term.write(chunk));
        term.onData((data) => void writeTerminal(id, data));
      } catch (e) {
        term.write(`\r\n\x1b[31m${String(e)}\x1b[0m\r\n`);
      }
    })();

    const observer = new ResizeObserver(() => {
      fit.fit();
      if (sessionId) void resizeTerminal(sessionId, term.rows, term.cols);
    });
    observer.observe(host);

    return () => {
      disposed = true;
      observer.disconnect();
      offOutput?.();
      if (sessionId) void closeTerminal(sessionId);
      term.dispose();
    };
  }, []);

  return <div ref={hostRef} className="h-full w-full bg-surface-base p-2" />;
}
```

- [ ] **Step 3: Register it as the bottom-slot tenant**

In `CodeGraphView`, call `useBottomContent(<TerminalPane />)` — the same hook shape `useSidebarContent` uses. It must be a stable element: build it with `useMemo` or hoist it, or every render remounts the terminal and kills the shell.

- [ ] **Step 4: Verify**

Manual: open a Java project, open the bottom slot from the title-bar toggle. A shell prompt appears in the project directory. `ls` works, `mvn -v` works, resizing the slot reflows the shell, switching modules and back does not spawn a second shell.

- [ ] **Step 5: Commit**

```bash
git add frontend/
git commit -m "feat(codegraph): a project shell in the bottom slot"
```

---

## What this plan deliberately leaves undone

| Left out | Why |
|---|---|
| Delivering a comment into a live agent session, and the Live/Blocked/Error session states | The user asked for it to stay a TODO. Task 17 records exactly what it needs, starting with the missing plan→session link. |
| `CodeGraphProposeFlow` on the bridge | Plans come from agents over MCP; humans approve. A UI propose button would erase that boundary. |
| Resolve/reopen on comment threads, comments on edges, comments surviving a new plan | Named non-goals in codegraph.mdx § Non-goals. |
| Re-verifying an edge `condition` against real code | Plan-time documentation for human review, never a machine assertion — codegraph.mdx § "Điều kiện trên cạnh". |
| `searchSymbol` UI | The bridge and MCP tool exist and work. No design calls for it yet; adding a second search box beside the sidebar filter would need a reason. |
| Packaged-build analyzer jar resolution | `main.go` resolves the jar relative to the working directory and says so. Out of scope here; it is a packaging decision, not a CodeGraph one. |
