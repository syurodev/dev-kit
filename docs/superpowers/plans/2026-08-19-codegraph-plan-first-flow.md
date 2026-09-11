# CodeGraph Plan-First Flow Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement ["Plan-first flow: vẽ trước, agent implement, auto-diff tiến độ"](base-doc `content/projects/dev-kit/codegraph.mdx:605-823`) — the inverse of this module's normal direction: an agent proposes a draft flow of not-yet-existing classes/methods, a human approves it, the agent implements it, and DevKit automatically diffs real indexed code against the approved plan to report per-node progress.

**Architecture:** Two new tables (`plan_nodes`, `plan_edges`) in the same per-project SQLite index, one active plan per project (a new `ProposeFlow` replaces any existing plan wholesale, mirroring `ReplaceIndex`'s existing "swap, don't merge" contract). The heart of the feature is a **pure status-computation function** — no DB access, exhaustively unit-testable — that maps (plan nodes, plan edges, resolved real nodes, real out-edges) → per-node status. Everything else is thin glue.

**Key simplification vs. the doc's literal SQL: per-node `status` is computed on read, never stored.** The design doc's schema sketch shows `plan_nodes(..., status)` as a column, plus a separate requirement that statuses auto-update after every incremental re-index. Storing it would require recompute-and-write machinery hooked into the re-index path, with a whole class of staleness bugs when that hook is missed. Computing it fresh inside `GetPlanProgress` satisfies "auto-updates after re-index" **for free and exactly** (the next read simply sees the new index) with zero extra machinery — and it is the same principle the doc already applies one paragraph later to the aggregate status ("Trạng thái tổng thể của plan — tính động, không lưu riêng"). This plan extends that stated principle consistently to per-node status. Consequence: `2026-08-19-codegraph-auto-reindex.md` needs **no changes at all** to satisfy the "diff recalculates automatically after re-index" requirement.

**Tech Stack:** Go only — no new dependencies.

---

## FQN matching rule (grounded, not invented)

Plan nodes carry an intended FQN; matching resolves it against the real index. Verified against `2026-08-19-codegraph-java-analyzer.md:34` — the analyzer emits **two** name forms per method node:

- `Node.Label` = `com.acme.refund.RefundService.process` — plain dotted FQN, no parameter types.
- `Node.ID` = `com.acme.refund.RefundService.process(com.acme.refund.RefundRequest)` — includes parameter types, unique across overloads.
- For **class** nodes, `ID` and `Label` are both the plain qualified name.

This maps exactly onto the doc's overload rule ("FQN kèm luôn danh sách kiểu tham số nếu plan có cung cấp... plan không cung cấp kiểu tham số thì so theo tên method thôi"):

> **If the plan's FQN contains `(`, match it against `nodes.id`; otherwise match against `nodes.label`.**

A label match can hit more than one row when overloads exist (two overloads share a label) — resolution takes the lowest `id` in sorted order for determinism, and this is exactly the "accepted v1 overload ambiguity risk" the doc already names.

---

## `implemented_diverged` — a literal reading, flagged

The doc defines it as: FQN matches, every planned out-edge is satisfied, **and there is at least one real out-edge not in the plan**. Implemented literally here.

**Honest consequence the implementer should know:** a plan node pointing at a *pre-existing* method that already calls several other things will report `implemented_diverged` essentially always, since all of that method's existing calls are "not in the plan". The doc is explicit that this state is informational and non-blocking ("không phải lỗi... không chặn gì, không coi là fail"), so this is faithful rather than broken — but if it proves too noisy against a real project, the intended narrowing is a one-line change in `planNodeStatus`: only count an extra real edge as divergence when its **target is also a plan node** (i.e. the plan had an opinion about that target and reality disagreed). Left literal here because deviating would be redesigning, not implementing.

---

## Aggregate status — one gap in the spec, filled explicitly

The doc's aggregate table defines `complete` (no `not_implemented`/`pending_enhancement` remain), `not_started` (all nodes `not_implemented`), and `in_progress` (≥1 unfinished **and** ≥1 finished). Read strictly, a plan whose nodes are *all* `pending_enhancement` matches none of the three. Filled by making `in_progress` the fallback: anything that is neither `complete` nor `not_started` is `in_progress`. Noted here rather than silently chosen.

---

## File Structure

Create:
- `internal/codegraph/plan.go` — plan types + the pure `planNodeStatus`/`aggregateStatus` functions.
- `internal/codegraph/plan_test.go` — exhaustive pure-function tests (no DB).
- `internal/codegraph/planstore.go` — `sqliteStore`'s plan persistence + FQN resolution.
- `internal/bridge/plan.go` — the 5 bridge methods.
- `internal/bridge/plan_test.go` — tests for them.
- `internal/mcp/tools_codegraph_plan.go` — the 4 MCP tools.
- `internal/mcp/tools_codegraph_plan_test.go` — tests for them.

Modify:
- `internal/codegraph/codegraph.go` — plan errors, extend `Store` interface.
- `internal/codegraph/store.go` — `plan_nodes`/`plan_edges` schema.
- `internal/codegraph/service.go` — 5 Service methods, `nilStore` stubs.
- `internal/codegraph/service_test.go` — Service-level integration tests.
- `internal/app/container.go` — `codegraph:plan` capability grants.
- `internal/mcp/server.go` — extend `CodeGraphAPI`.
- `internal/mcp/tools_codegraph_test.go` — extend `fakeCodeGraphAPI`.

---

## Task 1: Plan types and the pure status functions

**Files:**
- Create: `internal/codegraph/plan.go`
- Create: `internal/codegraph/plan_test.go`

This is the algorithmic core. Written first, in pure form, with no database anywhere near it.

- [ ] **Step 1: Write the failing tests**

```go
// internal/codegraph/plan_test.go
package codegraph

import "testing"

// resolvedFrom builds the planNodeID -> real Node map the way the Service
// does, but by hand so these tests stay pure (no store, no SQLite).
func resolvedFrom(pairs map[string]string) map[string]Node {
	out := make(map[string]Node, len(pairs))
	for planNodeID, realID := range pairs {
		out[planNodeID] = Node{ID: realID, Label: realID}
	}
	return out
}

func TestPlanNodeStatusNotImplementedWhenFQNUnresolved(t *testing.T) {
	got := planNodeStatus("pn-1", nil, map[string]Node{}, map[string][]string{})
	if got != PlanStatusNotImplemented {
		t.Fatalf("status = %q, want %q", got, PlanStatusNotImplemented)
	}
}

func TestPlanNodeStatusMatchingWhenNoOutEdgesPlanned(t *testing.T) {
	// A plan node referencing an existing method that the plan asks nothing
	// more of — matching immediately, per the doc's "Node tham chiếu method
	// cũ chỉ implemented_matching ngay lập tức nếu plan không yêu cầu thêm
	// cạnh nào từ nó".
	resolved := resolvedFrom(map[string]string{"pn-1": "com.acme.A.foo"})
	got := planNodeStatus("pn-1", nil, resolved, map[string][]string{})
	if got != PlanStatusImplementedMatching {
		t.Fatalf("status = %q, want %q", got, PlanStatusImplementedMatching)
	}
}

func TestPlanNodeStatusPendingEnhancementWhenPlannedEdgeMissing(t *testing.T) {
	// pn-1 exists in real code, pn-2 exists too, but the real call from
	// A.foo -> B.bar that the plan requires has not been written yet.
	edges := []PlanEdge{{From: "pn-1", To: "pn-2"}}
	resolved := resolvedFrom(map[string]string{"pn-1": "com.acme.A.foo", "pn-2": "com.acme.B.bar"})
	got := planNodeStatus("pn-1", edges, resolved, map[string][]string{})
	if got != PlanStatusPendingEnhancement {
		t.Fatalf("status = %q, want %q", got, PlanStatusPendingEnhancement)
	}
}

func TestPlanNodeStatusPendingEnhancementWhenEdgeTargetNotImplemented(t *testing.T) {
	// The planned target itself does not exist yet, so the edge cannot be
	// satisfied — per the doc, edges need no status of their own, this is
	// inferred from the two endpoint nodes.
	edges := []PlanEdge{{From: "pn-1", To: "pn-2"}}
	resolved := resolvedFrom(map[string]string{"pn-1": "com.acme.A.foo"})
	got := planNodeStatus("pn-1", edges, resolved, map[string][]string{})
	if got != PlanStatusPendingEnhancement {
		t.Fatalf("status = %q, want %q", got, PlanStatusPendingEnhancement)
	}
}

func TestPlanNodeStatusMatchingWhenPlannedEdgeSatisfied(t *testing.T) {
	edges := []PlanEdge{{From: "pn-1", To: "pn-2"}}
	resolved := resolvedFrom(map[string]string{"pn-1": "com.acme.A.foo", "pn-2": "com.acme.B.bar"})
	realOut := map[string][]string{"com.acme.A.foo": {"com.acme.B.bar"}}
	got := planNodeStatus("pn-1", edges, resolved, realOut)
	if got != PlanStatusImplementedMatching {
		t.Fatalf("status = %q, want %q", got, PlanStatusImplementedMatching)
	}
}

func TestPlanNodeStatusDivergedOnExtraRealEdge(t *testing.T) {
	edges := []PlanEdge{{From: "pn-1", To: "pn-2"}}
	resolved := resolvedFrom(map[string]string{"pn-1": "com.acme.A.foo", "pn-2": "com.acme.B.bar"})
	realOut := map[string][]string{"com.acme.A.foo": {"com.acme.B.bar", "com.acme.Logger.debug"}}
	got := planNodeStatus("pn-1", edges, resolved, realOut)
	if got != PlanStatusImplementedDiverged {
		t.Fatalf("status = %q, want %q", got, PlanStatusImplementedDiverged)
	}
}

func TestPlanNodeStatusMissingEdgeWinsOverExtraEdge(t *testing.T) {
	// One planned edge satisfied, one not, plus an unplanned extra. The
	// unfinished work is the more important signal — pending_enhancement,
	// not diverged.
	edges := []PlanEdge{{From: "pn-1", To: "pn-2"}, {From: "pn-1", To: "pn-3"}}
	resolved := resolvedFrom(map[string]string{
		"pn-1": "com.acme.A.foo", "pn-2": "com.acme.B.bar", "pn-3": "com.acme.C.baz",
	})
	realOut := map[string][]string{"com.acme.A.foo": {"com.acme.B.bar", "com.acme.Logger.debug"}}
	got := planNodeStatus("pn-1", edges, resolved, realOut)
	if got != PlanStatusPendingEnhancement {
		t.Fatalf("status = %q, want %q", got, PlanStatusPendingEnhancement)
	}
}

func TestPlanNodeStatusIgnoresEdgesFromOtherNodes(t *testing.T) {
	// An unsatisfied edge out of pn-9 must not drag pn-1 down.
	edges := []PlanEdge{{From: "pn-9", To: "pn-2"}}
	resolved := resolvedFrom(map[string]string{"pn-1": "com.acme.A.foo"})
	got := planNodeStatus("pn-1", edges, resolved, map[string][]string{})
	if got != PlanStatusImplementedMatching {
		t.Fatalf("status = %q, want %q", got, PlanStatusImplementedMatching)
	}
}

func TestAggregateStatus(t *testing.T) {
	cases := []struct {
		name     string
		statuses []string
		want     string
	}{
		{"all not implemented", []string{PlanStatusNotImplemented, PlanStatusNotImplemented}, PlanAggregateNotStarted},
		{"all matching", []string{PlanStatusImplementedMatching, PlanStatusImplementedMatching}, PlanAggregateComplete},
		{"matching and diverged", []string{PlanStatusImplementedMatching, PlanStatusImplementedDiverged}, PlanAggregateComplete},
		{"mixed done and not", []string{PlanStatusImplementedMatching, PlanStatusNotImplemented}, PlanAggregateInProgress},
		{"pending plus done", []string{PlanStatusPendingEnhancement, PlanStatusImplementedMatching}, PlanAggregateInProgress},
		// Spec gap, filled by making in_progress the fallback: every node is
		// pending_enhancement, so it is neither complete nor not_started.
		{"all pending enhancement", []string{PlanStatusPendingEnhancement, PlanStatusPendingEnhancement}, PlanAggregateInProgress},
		// Also a fallback case: nothing finished, but not everything is
		// untouched either.
		{"not implemented plus pending", []string{PlanStatusNotImplemented, PlanStatusPendingEnhancement}, PlanAggregateInProgress},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := aggregateStatus(tc.statuses); got != tc.want {
				t.Fatalf("aggregateStatus(%v) = %q, want %q", tc.statuses, got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run to confirm they fail**

```bash
go test ./internal/codegraph/... -run "TestPlanNodeStatus|TestAggregateStatus" -v
```

Expected: FAIL — compile error, nothing in this task exists yet.

- [ ] **Step 3: Implement**

```go
// internal/codegraph/plan.go
package codegraph

// Per-plan-node status values. See base-doc codegraph.mdx "4 trạng thái mỗi
// plan node" — these are computed on read, never stored (see this plan's
// header for why).
const (
	PlanStatusNotImplemented      = "not_implemented"
	PlanStatusPendingEnhancement  = "pending_enhancement"
	PlanStatusImplementedMatching = "implemented_matching"
	PlanStatusImplementedDiverged = "implemented_diverged"
)

// Whole-plan status values, derived from the set of per-node statuses.
const (
	PlanAggregateNotStarted = "not_started"
	PlanAggregateInProgress = "in_progress"
	PlanAggregateComplete   = "complete"
)

// Plan draft/approved lifecycle. A draft plan is inert — it exists to be
// reviewed on the canvas; approval is a human-only bridge action.
const (
	PlanStateDraft    = "draft"
	PlanStateApproved = "approved"
)

// PlanNode is one intended class or method in a proposed flow. FQN is the
// fully-qualified name the agent commits to creating; matching resolves it
// against the real index (see this plan's "FQN matching rule").
type PlanNode struct {
	PlanNodeID  string `json:"planNodeId"`
	Label       string `json:"label"`
	Description string `json:"description"`
	FQN         string `json:"fqn"`
	Kind        string `json:"kind"` // "class" | "method"
}

// PlanEdge is one intended call between two plan nodes. Condition is
// plan-time documentation written by the proposing agent for the human
// reviewing the canvas — it is never machine-verified against real code
// (see the doc's "Điều kiện trên cạnh" section).
type PlanEdge struct {
	From      string `json:"from"` // PlanNode.PlanNodeID
	To        string `json:"to"`   // PlanNode.PlanNodeID
	Condition string `json:"condition"`
}

// Plan is the single active plan for a project.
type Plan struct {
	Description string     `json:"description"`
	State       string     `json:"state"` // "draft" | "approved"
	Nodes       []PlanNode `json:"nodes"`
	Edges       []PlanEdge `json:"edges"`
}

// PlanNodeProgress is one plan node plus its freshly computed status.
type PlanNodeProgress struct {
	PlanNode
	Status string `json:"status"`
	// RealNodeID is the indexed node the FQN resolved to, empty when the
	// status is not_implemented. Lets a caller jump straight to the real
	// code (e.g. via GetCodeSnippet) without re-resolving the FQN itself.
	RealNodeID string `json:"realNodeId"`
}

// PlanProgress is GetPlanProgress's result: every node's status plus the
// derived whole-plan status.
type PlanProgress struct {
	Description string             `json:"description"`
	State       string             `json:"state"`
	Aggregate   string             `json:"aggregate"`
	Nodes       []PlanNodeProgress `json:"nodes"`
}

// planNodeStatus computes one plan node's status from fully resolved inputs.
// Pure by design — no store access — so every branch of the doc's 4-state
// table is unit-testable without a database.
//
//   - resolved maps a plan node ID to the real indexed Node its FQN resolved
//     to; a missing key means that FQN matched nothing in the index.
//   - realOutTargets maps a real node ID to the real node IDs it calls.
func planNodeStatus(planNodeID string, planEdges []PlanEdge, resolved map[string]Node, realOutTargets map[string][]string) string {
	real, ok := resolved[planNodeID]
	if !ok {
		return PlanStatusNotImplemented
	}

	realTargets := make(map[string]bool, len(realOutTargets[real.ID]))
	for _, t := range realOutTargets[real.ID] {
		realTargets[t] = true
	}

	plannedAndPresent := make(map[string]bool)
	for _, e := range planEdges {
		if e.From != planNodeID {
			continue
		}
		target, targetResolved := resolved[e.To]
		if !targetResolved || !realTargets[target.ID] {
			// A planned call that reality does not have yet. The method
			// itself exists (FQN resolved) but the plan asks more of it.
			return PlanStatusPendingEnhancement
		}
		plannedAndPresent[target.ID] = true
	}

	for target := range realTargets {
		if !plannedAndPresent[target] {
			return PlanStatusImplementedDiverged
		}
	}
	return PlanStatusImplementedMatching
}

// aggregateStatus derives the whole-plan status from its nodes' statuses.
// in_progress is the deliberate fallback for combinations the design doc's
// table does not name (e.g. every node pending_enhancement) — see this
// plan's header.
func aggregateStatus(statuses []string) string {
	allNotImplemented := true
	anyUnfinished := false
	for _, s := range statuses {
		if s != PlanStatusNotImplemented {
			allNotImplemented = false
		}
		if s == PlanStatusNotImplemented || s == PlanStatusPendingEnhancement {
			anyUnfinished = true
		}
	}
	switch {
	case !anyUnfinished:
		return PlanAggregateComplete
	case allNotImplemented:
		return PlanAggregateNotStarted
	default:
		return PlanAggregateInProgress
	}
}
```

- [ ] **Step 4: Run the tests**

```bash
go test ./internal/codegraph/... -run "TestPlanNodeStatus|TestAggregateStatus" -v
```

Expected: all 9 pass (8 `planNodeStatus` cases + the 7 `aggregateStatus` subtests).

- [ ] **Step 5: Commit**

```bash
git add internal/codegraph/plan.go internal/codegraph/plan_test.go
git commit -m "feat(codegraph): plan types and pure status-computation functions"
```

---

## Task 2: Plan storage

**Files:**
- Modify: `internal/codegraph/store.go`
- Modify: `internal/codegraph/codegraph.go`
- Create: `internal/codegraph/planstore.go`
- Modify: `internal/codegraph/service.go` (`nilStore` stubs)
- Modify: `internal/codegraph/store_test.go`

The doc's schema sketch includes a `project_id` column on both tables. Dropped here for the same reason `nodes`/`edges`/`endpoints` have never had one: each project already gets its own SQLite file (`ProjectDBPath`), so a project discriminator inside that file is dead weight. Also dropped: the `status` column — see this plan's header.

- [ ] **Step 1: Write the failing tests**

Add to `internal/codegraph/store_test.go`:

```go
func samplePlan() Plan {
	return Plan{
		Description: "Refund flow",
		State:       PlanStateDraft,
		Nodes: []PlanNode{
			{PlanNodeID: "pn-1", Label: "Nhận yêu cầu hoàn tiền", Description: "controller entry", FQN: "com.acme.refund.RefundController.create", Kind: "method"},
			{PlanNodeID: "pn-2", Label: "Xử lý hoàn tiền", Description: "core logic", FQN: "com.acme.refund.RefundService.process", Kind: "method"},
		},
		Edges: []PlanEdge{
			{From: "pn-1", To: "pn-2", Condition: "chỉ khi type == REFUND"},
		},
	}
}

func TestStoreSavePlanThenGetPlan(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := store.SavePlan(ctx, samplePlan()); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}

	got, err := store.GetPlan(ctx)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.Description != "Refund flow" || got.State != PlanStateDraft {
		t.Fatalf("plan = %+v", got)
	}
	if len(got.Nodes) != 2 || got.Nodes[0].PlanNodeID != "pn-1" || got.Nodes[1].FQN != "com.acme.refund.RefundService.process" {
		t.Fatalf("nodes = %+v", got.Nodes)
	}
	if len(got.Edges) != 1 || got.Edges[0].Condition != "chỉ khi type == REFUND" {
		t.Fatalf("edges = %+v", got.Edges)
	}
}

func TestStoreGetPlanWhenNoneFails(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.GetPlan(context.Background()); !errors.Is(err, ErrNoPlan) {
		t.Fatalf("GetPlan = %v, want ErrNoPlan", err)
	}
}

func TestStoreSavePlanReplacesPrevious(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.SavePlan(ctx, samplePlan()); err != nil {
		t.Fatalf("first SavePlan: %v", err)
	}

	second := Plan{
		Description: "Something else",
		State:       PlanStateDraft,
		Nodes:       []PlanNode{{PlanNodeID: "pn-9", Label: "Only node", FQN: "com.acme.Other.run", Kind: "method"}},
	}
	if err := store.SavePlan(ctx, second); err != nil {
		t.Fatalf("second SavePlan: %v", err)
	}

	got, err := store.GetPlan(ctx)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.Description != "Something else" || len(got.Nodes) != 1 || got.Nodes[0].PlanNodeID != "pn-9" {
		t.Fatalf("plan = %+v", got)
	}
	if len(got.Edges) != 0 {
		t.Fatalf("expected the previous plan's edges cleared, got %+v", got.Edges)
	}
}

func TestStoreSetPlanState(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.SavePlan(ctx, samplePlan()); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}

	if err := store.SetPlanState(ctx, PlanStateApproved); err != nil {
		t.Fatalf("SetPlanState: %v", err)
	}
	got, err := store.GetPlan(ctx)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.State != PlanStateApproved {
		t.Fatalf("state = %q, want approved", got.State)
	}
}

func TestStoreSetPlanNodeFQN(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.SavePlan(ctx, samplePlan()); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}

	if err := store.SetPlanNodeFQN(ctx, "pn-2", "com.acme.refund.RefundService.handle"); err != nil {
		t.Fatalf("SetPlanNodeFQN: %v", err)
	}
	got, err := store.GetPlan(ctx)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.Nodes[1].FQN != "com.acme.refund.RefundService.handle" {
		t.Fatalf("fqn = %q", got.Nodes[1].FQN)
	}

	if err := store.SetPlanNodeFQN(ctx, "does-not-exist", "com.acme.X.y"); !errors.Is(err, ErrPlanNodeNotFound) {
		t.Fatalf("SetPlanNodeFQN on unknown node = %v, want ErrPlanNodeNotFound", err)
	}
}

func TestStoreResolveFQNPrefersIDWhenParamsGiven(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	result := AnalysisResult{
		Nodes: []Node{
			{ID: "com.acme.A.foo(java.lang.String)", Label: "com.acme.A.foo", Kind: "method", Layer: "service", FilePath: "A.java", Line: 1, EndLine: 2},
			{ID: "com.acme.A.foo(int)", Label: "com.acme.A.foo", Kind: "method", Layer: "service", FilePath: "A.java", Line: 4, EndLine: 5},
		},
	}
	if err := store.ReplaceIndex(ctx, result); err != nil {
		t.Fatalf("ReplaceIndex: %v", err)
	}

	// With parameter types: exact id match, unambiguous.
	node, ok, err := store.ResolveFQN(ctx, "com.acme.A.foo(int)")
	if err != nil {
		t.Fatalf("ResolveFQN: %v", err)
	}
	if !ok || node.ID != "com.acme.A.foo(int)" {
		t.Fatalf("node = %+v, ok = %v", node, ok)
	}

	// Without parameter types: label match, lowest id wins for determinism.
	node, ok, err = store.ResolveFQN(ctx, "com.acme.A.foo")
	if err != nil {
		t.Fatalf("ResolveFQN: %v", err)
	}
	if !ok || node.ID != "com.acme.A.foo(int)" {
		t.Fatalf("expected the lowest-sorted overload id, got %+v", node)
	}

	if _, ok, err = store.ResolveFQN(ctx, "com.acme.Nope.bar"); err != nil || ok {
		t.Fatalf("expected no match, got ok=%v err=%v", ok, err)
	}
}
```

- [ ] **Step 2: Run to confirm they fail**

```bash
go test ./internal/codegraph/... -run "TestStoreSavePlan|TestStoreGetPlan|TestStoreSetPlan|TestStoreResolveFQN" -v
```

Expected: FAIL — compile error, none of these methods exist yet.

- [ ] **Step 3: Add the errors**

In `internal/codegraph/codegraph.go`, add to the `var (...)` block:

```go
	// ErrNoPlan is returned when a plan operation runs against a project
	// that has no active plan (none proposed yet, per "1 plan active tại 1
	// thời điểm").
	ErrNoPlan = errors.New("codegraph: no active plan")
	// ErrPlanNodeNotFound is returned by RelinkPlanNode for a plan node ID
	// that is not part of the active plan.
	ErrPlanNodeNotFound = errors.New("codegraph: plan node not found")
	// ErrInvalidPlan is returned by ProposeFlow when the submitted plan is
	// internally inconsistent (no nodes, duplicate node IDs, or an edge
	// referencing an unknown node).
	ErrInvalidPlan = errors.New("codegraph: invalid plan")
)
```

- [ ] **Step 4: Add the schema**

In `internal/codegraph/store.go`, extend the `schema` constant:

```go
CREATE TABLE IF NOT EXISTS plan_meta (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	description TEXT NOT NULL,
	state TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS plan_nodes (
	plan_node_id TEXT PRIMARY KEY,
	label TEXT NOT NULL,
	description TEXT NOT NULL,
	fqn TEXT NOT NULL,
	kind TEXT NOT NULL,
	ordinal INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS plan_edges (
	from_plan_node_id TEXT NOT NULL,
	to_plan_node_id TEXT NOT NULL,
	condition TEXT NOT NULL
);
```

`plan_meta`'s `CHECK (id = 1)` enforces the doc's "1 plan active tại 1 thời điểm" at the schema level rather than by convention. `ordinal` preserves the agent's submitted node order so the canvas and every response render nodes in the order they were proposed, not an arbitrary one.

- [ ] **Step 5: Implement the store methods**

```go
// internal/codegraph/planstore.go
package codegraph

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// SavePlan replaces the project's single active plan wholesale — same
// "swap, don't merge" contract as ReplaceIndex, matching the doc's "đề xuất
// plan mới trong lúc plan cũ chưa xong thì thay thế".
func (s *sqliteStore) SavePlan(ctx context.Context, plan Plan) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("codegraph: begin save plan: %w", err)
	}
	defer tx.Rollback()

	for _, table := range []string{"plan_meta", "plan_nodes", "plan_edges"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			return fmt.Errorf("codegraph: clear %s: %w", table, err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO plan_meta (id, description, state) VALUES (1, ?, ?)`,
		plan.Description, plan.State); err != nil {
		return fmt.Errorf("codegraph: insert plan meta: %w", err)
	}
	for i, n := range plan.Nodes {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO plan_nodes (plan_node_id, label, description, fqn, kind, ordinal) VALUES (?, ?, ?, ?, ?, ?)`,
			n.PlanNodeID, n.Label, n.Description, n.FQN, n.Kind, i); err != nil {
			return fmt.Errorf("codegraph: insert plan node %q: %w", n.PlanNodeID, err)
		}
	}
	for _, e := range plan.Edges {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO plan_edges (from_plan_node_id, to_plan_node_id, condition) VALUES (?, ?, ?)`,
			e.From, e.To, e.Condition); err != nil {
			return fmt.Errorf("codegraph: insert plan edge %q->%q: %w", e.From, e.To, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("codegraph: commit save plan: %w", err)
	}
	return nil
}

func (s *sqliteStore) GetPlan(ctx context.Context) (Plan, error) {
	var plan Plan
	err := s.db.QueryRowContext(ctx, `SELECT description, state FROM plan_meta WHERE id = 1`).
		Scan(&plan.Description, &plan.State)
	if err == sql.ErrNoRows {
		return Plan{}, ErrNoPlan
	}
	if err != nil {
		return Plan{}, fmt.Errorf("codegraph: GetPlan: %w", err)
	}

	nodeRows, err := s.db.QueryContext(ctx,
		`SELECT plan_node_id, label, description, fqn, kind FROM plan_nodes ORDER BY ordinal`)
	if err != nil {
		return Plan{}, fmt.Errorf("codegraph: GetPlan nodes: %w", err)
	}
	defer nodeRows.Close()
	for nodeRows.Next() {
		var n PlanNode
		if err := nodeRows.Scan(&n.PlanNodeID, &n.Label, &n.Description, &n.FQN, &n.Kind); err != nil {
			return Plan{}, fmt.Errorf("codegraph: scan plan node: %w", err)
		}
		plan.Nodes = append(plan.Nodes, n)
	}
	if err := nodeRows.Err(); err != nil {
		return Plan{}, err
	}

	edgeRows, err := s.db.QueryContext(ctx,
		`SELECT from_plan_node_id, to_plan_node_id, condition FROM plan_edges ORDER BY from_plan_node_id, to_plan_node_id`)
	if err != nil {
		return Plan{}, fmt.Errorf("codegraph: GetPlan edges: %w", err)
	}
	defer edgeRows.Close()
	for edgeRows.Next() {
		var e PlanEdge
		if err := edgeRows.Scan(&e.From, &e.To, &e.Condition); err != nil {
			return Plan{}, fmt.Errorf("codegraph: scan plan edge: %w", err)
		}
		plan.Edges = append(plan.Edges, e)
	}
	return plan, edgeRows.Err()
}

func (s *sqliteStore) SetPlanState(ctx context.Context, state string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE plan_meta SET state = ? WHERE id = 1`, state)
	if err != nil {
		return fmt.Errorf("codegraph: SetPlanState: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("codegraph: SetPlanState: %w", err)
	}
	if affected == 0 {
		return ErrNoPlan
	}
	return nil
}

func (s *sqliteStore) SetPlanNodeFQN(ctx context.Context, planNodeID, fqn string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE plan_nodes SET fqn = ? WHERE plan_node_id = ?`, fqn, planNodeID)
	if err != nil {
		return fmt.Errorf("codegraph: SetPlanNodeFQN: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("codegraph: SetPlanNodeFQN: %w", err)
	}
	if affected == 0 {
		return ErrPlanNodeNotFound
	}
	return nil
}

// ResolveFQN finds the indexed node a plan FQN refers to. An FQN carrying
// parameter types (it contains "(") matches nodes.id exactly; a plain dotted
// FQN matches nodes.label. A label can match several overloads — the lowest
// id wins so repeated calls are deterministic, which is exactly the v1
// overload ambiguity the design doc already accepts.
func (s *sqliteStore) ResolveFQN(ctx context.Context, fqn string) (Node, bool, error) {
	column := "label"
	if strings.Contains(fqn, "(") {
		column = "id"
	}
	var n Node
	err := s.db.QueryRowContext(ctx,
		`SELECT id, label, kind, layer, file_path, line, end_line FROM nodes WHERE `+column+` = ? ORDER BY id LIMIT 1`,
		fqn,
	).Scan(&n.ID, &n.Label, &n.Kind, &n.Layer, &n.FilePath, &n.Line, &n.EndLine)
	if err == sql.ErrNoRows {
		return Node{}, false, nil
	}
	if err != nil {
		return Node{}, false, fmt.Errorf("codegraph: ResolveFQN: %w", err)
	}
	return n, true, nil
}
```

`column` is interpolated into the SQL string rather than passed as a placeholder because SQLite (like every SQL engine) does not accept a bound parameter in a column position. It is safe here: its value is one of exactly two hardcoded literals chosen by a local `strings.Contains` check, never derived from `fqn` itself — the FQN stays a bound parameter.

- [ ] **Step 6: Extend the `Store` interface**

In `internal/codegraph/codegraph.go`:

```go
	SavePlan(ctx context.Context, plan Plan) error
	GetPlan(ctx context.Context) (Plan, error)
	SetPlanState(ctx context.Context, state string) error
	SetPlanNodeFQN(ctx context.Context, planNodeID, fqn string) error
	ResolveFQN(ctx context.Context, fqn string) (Node, bool, error)
```

- [ ] **Step 7: Add the `nilStore` stubs**

In `internal/codegraph/service.go`:

```go
func (nilStore) SavePlan(context.Context, Plan) error          { return ErrNoActiveProject }
func (nilStore) GetPlan(context.Context) (Plan, error)         { return Plan{}, ErrNoActiveProject }
func (nilStore) SetPlanState(context.Context, string) error    { return ErrNoActiveProject }
func (nilStore) SetPlanNodeFQN(context.Context, string, string) error { return ErrNoActiveProject }
func (nilStore) ResolveFQN(context.Context, string) (Node, bool, error) {
	return Node{}, false, ErrNoActiveProject
}
```

- [ ] **Step 8: Run the tests**

```bash
go test ./internal/codegraph/... -run "TestStoreSavePlan|TestStoreGetPlan|TestStoreSetPlan|TestStoreResolveFQN" -v
go build ./...
```

Expected: all 6 pass; build confirms both `Store` implementations are complete again.

- [ ] **Step 9: Full package regression**

```bash
go test ./internal/codegraph/... -race
```

- [ ] **Step 10: Commit**

```bash
git add internal/codegraph/plan.go internal/codegraph/planstore.go internal/codegraph/store.go internal/codegraph/store_test.go internal/codegraph/codegraph.go internal/codegraph/service.go
git commit -m "feat(codegraph): plan_nodes/plan_edges storage and FQN resolution"
```

---

## Task 3: `Service.ProposeFlow`

**Files:**
- Modify: `internal/codegraph/service.go`
- Modify: `internal/codegraph/service_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/codegraph/service_test.go`:

```go
func planFixtureAnalysis() AnalysisResult {
	// Real code that already contains RefundController.create calling
	// RefundService.process — used to exercise the "plan is already
	// complete the moment it is proposed" case.
	return AnalysisResult{
		Nodes: []Node{
			{ID: "com.acme.RefundController.create(com.acme.Req)", Label: "com.acme.RefundController.create", Kind: "method", Layer: "controller", FilePath: "C.java", Line: 1, EndLine: 5},
			{ID: "com.acme.RefundService.process(com.acme.Req)", Label: "com.acme.RefundService.process", Kind: "method", Layer: "service", FilePath: "S.java", Line: 1, EndLine: 5},
		},
		Edges: []Edge{
			{From: "com.acme.RefundController.create(com.acme.Req)", To: "com.acme.RefundService.process(com.acme.Req)", Kind: "call"},
		},
	}
}

func TestServiceProposeFlowStoresDraftAndReportsProgress(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: AnalysisResult{}})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	progress, err := svc.ProposeFlow(context.Background(), Plan{
		Description: "Refund flow",
		Nodes: []PlanNode{
			{PlanNodeID: "pn-1", Label: "Entry", FQN: "com.acme.RefundController.create", Kind: "method"},
			{PlanNodeID: "pn-2", Label: "Core", FQN: "com.acme.RefundService.process", Kind: "method"},
		},
		Edges: []PlanEdge{{From: "pn-1", To: "pn-2", Condition: "always"}},
	})
	if err != nil {
		t.Fatalf("ProposeFlow: %v", err)
	}
	if progress.State != PlanStateDraft {
		t.Fatalf("state = %q, want draft", progress.State)
	}
	// Index is empty, so nothing resolves.
	if progress.Aggregate != PlanAggregateNotStarted {
		t.Fatalf("aggregate = %q, want not_started", progress.Aggregate)
	}
	if len(progress.Nodes) != 2 || progress.Nodes[0].Status != PlanStatusNotImplemented {
		t.Fatalf("nodes = %+v", progress.Nodes)
	}
}

func TestServiceProposeFlowCompleteImmediatelyWhenCodeAlreadyMatches(t *testing.T) {
	// The doc's explicit special case: every planned method already exists
	// and the flow already matches reality, so the plan is complete the
	// moment it is proposed — no approve/implement cycle needed.
	svc := newTestService(t, fakeAnalyzer{result: planFixtureAnalysis()})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	progress, err := svc.ProposeFlow(context.Background(), Plan{
		Description: "Already built",
		Nodes: []PlanNode{
			{PlanNodeID: "pn-1", Label: "Entry", FQN: "com.acme.RefundController.create", Kind: "method"},
			{PlanNodeID: "pn-2", Label: "Core", FQN: "com.acme.RefundService.process", Kind: "method"},
		},
		Edges: []PlanEdge{{From: "pn-1", To: "pn-2"}},
	})
	if err != nil {
		t.Fatalf("ProposeFlow: %v", err)
	}
	if progress.Aggregate != PlanAggregateComplete {
		t.Fatalf("aggregate = %q, want complete; nodes = %+v", progress.Aggregate, progress.Nodes)
	}
	if progress.Nodes[0].RealNodeID != "com.acme.RefundController.create(com.acme.Req)" {
		t.Fatalf("RealNodeID = %q", progress.Nodes[0].RealNodeID)
	}
}

func TestServiceProposeFlowReplacesPreviousPlan(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: AnalysisResult{}})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	first := Plan{Description: "First", Nodes: []PlanNode{{PlanNodeID: "pn-1", Label: "A", FQN: "com.acme.A.a", Kind: "method"}}}
	if _, err := svc.ProposeFlow(context.Background(), first); err != nil {
		t.Fatalf("first ProposeFlow: %v", err)
	}

	second := Plan{Description: "Second", Nodes: []PlanNode{{PlanNodeID: "pn-9", Label: "B", FQN: "com.acme.B.b", Kind: "method"}}}
	if _, err := svc.ProposeFlow(context.Background(), second); err != nil {
		t.Fatalf("second ProposeFlow: %v", err)
	}

	flow, err := svc.GetPlannedFlow(context.Background())
	if err != nil {
		t.Fatalf("GetPlannedFlow: %v", err)
	}
	if flow.Description != "Second" || len(flow.Nodes) != 1 || flow.Nodes[0].PlanNodeID != "pn-9" {
		t.Fatalf("plan = %+v", flow)
	}
}

func TestServiceProposeFlowRejectsInvalidPlans(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: AnalysisResult{}})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	cases := []struct {
		name string
		plan Plan
	}{
		{"no nodes", Plan{Description: "empty"}},
		{"duplicate node id", Plan{Description: "dup", Nodes: []PlanNode{
			{PlanNodeID: "pn-1", Label: "A", FQN: "com.acme.A.a", Kind: "method"},
			{PlanNodeID: "pn-1", Label: "B", FQN: "com.acme.B.b", Kind: "method"},
		}}},
		{"node without fqn", Plan{Description: "nofqn", Nodes: []PlanNode{
			{PlanNodeID: "pn-1", Label: "A", Kind: "method"},
		}}},
		{"edge to unknown node", Plan{Description: "badedge", Nodes: []PlanNode{
			{PlanNodeID: "pn-1", Label: "A", FQN: "com.acme.A.a", Kind: "method"},
		}, Edges: []PlanEdge{{From: "pn-1", To: "pn-404"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.ProposeFlow(context.Background(), tc.plan); !errors.Is(err, ErrInvalidPlan) {
				t.Fatalf("ProposeFlow = %v, want ErrInvalidPlan", err)
			}
		})
	}
}

func TestServiceProposeFlowBeforeOpenProjectFails(t *testing.T) {
	svc := NewService(nilStore{}, fakeAnalyzer{})
	plan := Plan{Description: "x", Nodes: []PlanNode{{PlanNodeID: "pn-1", Label: "A", FQN: "com.acme.A.a", Kind: "method"}}}
	if _, err := svc.ProposeFlow(context.Background(), plan); !errors.Is(err, ErrNoActiveProject) {
		t.Fatalf("expected ErrNoActiveProject, got %v", err)
	}
}
```

- [ ] **Step 2: Run to confirm they fail**

```bash
go test ./internal/codegraph/... -run TestServiceProposeFlow -v
```

Expected: FAIL — compile error, `ProposeFlow`/`GetPlannedFlow` do not exist yet.

- [ ] **Step 3: Implement**

In `internal/codegraph/service.go`, add:

```go
// validatePlan rejects internally inconsistent plans at the boundary, the
// same way AnalysisResult.Validate rejects bad analyzer output — a bad plan
// caught here beats a confusing failure three layers later.
func validatePlan(plan Plan) error {
	if len(plan.Nodes) == 0 {
		return fmt.Errorf("%w: plan has no nodes", ErrInvalidPlan)
	}
	ids := make(map[string]bool, len(plan.Nodes))
	for _, n := range plan.Nodes {
		if n.PlanNodeID == "" {
			return fmt.Errorf("%w: plan node with empty id", ErrInvalidPlan)
		}
		if ids[n.PlanNodeID] {
			return fmt.Errorf("%w: duplicate plan node id %q", ErrInvalidPlan, n.PlanNodeID)
		}
		if n.FQN == "" {
			return fmt.Errorf("%w: plan node %q has no fqn", ErrInvalidPlan, n.PlanNodeID)
		}
		ids[n.PlanNodeID] = true
	}
	for _, e := range plan.Edges {
		if !ids[e.From] {
			return fmt.Errorf("%w: edge references unknown plan node %q", ErrInvalidPlan, e.From)
		}
		if !ids[e.To] {
			return fmt.Errorf("%w: edge references unknown plan node %q", ErrInvalidPlan, e.To)
		}
	}
	return nil
}

// ProposeFlow replaces the project's active plan with a new draft and
// returns its progress immediately. The returned progress can already be
// `complete` when every planned method exists and the flow already matches
// real code — the doc's explicit special case, where no approve/implement
// cycle is needed at all.
func (s *Service) ProposeFlow(ctx context.Context, plan Plan) (PlanProgress, error) {
	store, err := s.activeStore()
	if err != nil {
		return PlanProgress{}, err
	}
	if err := validatePlan(plan); err != nil {
		return PlanProgress{}, err
	}

	plan.State = PlanStateDraft
	if err := store.SavePlan(ctx, plan); err != nil {
		return PlanProgress{}, err
	}
	return computeProgress(ctx, store, plan)
}

// GetPlannedFlow returns the active plan as proposed, without computing
// statuses — the shape an implementing agent needs (intended FQNs, edges,
// conditions), not a progress report.
func (s *Service) GetPlannedFlow(ctx context.Context) (Plan, error) {
	store, err := s.activeStore()
	if err != nil {
		return Plan{}, err
	}
	return store.GetPlan(ctx)
}

// computeProgress resolves every plan node's FQN against the current index,
// collects each resolved node's real out-edges, and runs the pure status
// functions over the result. Statuses are always computed here, never
// stored — so any re-index (including the background one) is reflected on
// the very next call with no recompute hook anywhere.
func computeProgress(ctx context.Context, store Store, plan Plan) (PlanProgress, error) {
	resolved := make(map[string]Node, len(plan.Nodes))
	realOutTargets := make(map[string][]string, len(plan.Nodes))
	for _, n := range plan.Nodes {
		real, ok, err := store.ResolveFQN(ctx, n.FQN)
		if err != nil {
			return PlanProgress{}, err
		}
		if !ok {
			continue
		}
		resolved[n.PlanNodeID] = real
		if _, seen := realOutTargets[real.ID]; seen {
			continue // two plan nodes resolved to the same real node
		}
		edges, err := store.GetOutEdges(ctx, real.ID)
		if err != nil {
			return PlanProgress{}, err
		}
		targets := make([]string, 0, len(edges))
		for _, e := range edges {
			if e.Kind != "call" {
				continue
			}
			targets = append(targets, e.To)
		}
		realOutTargets[real.ID] = targets
	}

	progress := PlanProgress{
		Description: plan.Description,
		State:       plan.State,
		Nodes:       make([]PlanNodeProgress, 0, len(plan.Nodes)),
	}
	statuses := make([]string, 0, len(plan.Nodes))
	for _, n := range plan.Nodes {
		status := planNodeStatus(n.PlanNodeID, plan.Edges, resolved, realOutTargets)
		statuses = append(statuses, status)
		progress.Nodes = append(progress.Nodes, PlanNodeProgress{
			PlanNode:   n,
			Status:     status,
			RealNodeID: resolved[n.PlanNodeID].ID,
		})
	}
	progress.Aggregate = aggregateStatus(statuses)
	return progress, nil
}
```

- [ ] **Step 4: Run the tests**

```bash
go test ./internal/codegraph/... -run TestServiceProposeFlow -v
```

Expected: all pass (5 top-level tests, one with 4 subtests).

- [ ] **Step 5: Commit**

```bash
git add internal/codegraph/service.go internal/codegraph/service_test.go
git commit -m "feat(codegraph): Service.ProposeFlow and GetPlannedFlow"
```

---

## Task 4: `GetPlanProgress`, `ApprovePlan`, `RelinkPlanNode`

**Files:**
- Modify: `internal/codegraph/service.go`
- Modify: `internal/codegraph/service_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestServiceGetPlanProgressReflectsNewlyIndexedCode(t *testing.T) {
	// The heart of "auto-diff": propose against an empty index (nothing
	// implemented), then re-index with the code written, and the very next
	// GetPlanProgress call reports it — no recompute hook, no manual
	// refresh, because status is computed on read.
	analyzer := &swappableAnalyzer{result: AnalysisResult{}}
	svc := newTestService(t, analyzer)
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	plan := Plan{
		Description: "Refund flow",
		Nodes: []PlanNode{
			{PlanNodeID: "pn-1", Label: "Entry", FQN: "com.acme.RefundController.create", Kind: "method"},
			{PlanNodeID: "pn-2", Label: "Core", FQN: "com.acme.RefundService.process", Kind: "method"},
		},
		Edges: []PlanEdge{{From: "pn-1", To: "pn-2"}},
	}
	before, err := svc.ProposeFlow(context.Background(), plan)
	if err != nil {
		t.Fatalf("ProposeFlow: %v", err)
	}
	if before.Aggregate != PlanAggregateNotStarted {
		t.Fatalf("before = %q, want not_started", before.Aggregate)
	}

	// The agent writes the code; a re-index picks it up.
	analyzer.result = planFixtureAnalysis()
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("re-OpenProject: %v", err)
	}

	// This call succeeding at all is load-bearing: ReplaceIndex clears the
	// index tables but must NOT touch plan_meta/plan_nodes/plan_edges, or a
	// routine background re-index would silently destroy the user's approved
	// plan. A wiped plan surfaces here as ErrNoPlan, not a wrong status.
	after, err := svc.GetPlanProgress(context.Background())
	if err != nil {
		t.Fatalf("GetPlanProgress: %v", err)
	}
	if after.Aggregate != PlanAggregateComplete {
		t.Fatalf("after = %q, want complete; nodes = %+v", after.Aggregate, after.Nodes)
	}
}

func TestServiceApprovePlan(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: AnalysisResult{}})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	plan := Plan{Description: "x", Nodes: []PlanNode{{PlanNodeID: "pn-1", Label: "A", FQN: "com.acme.A.a", Kind: "method"}}}
	if _, err := svc.ProposeFlow(context.Background(), plan); err != nil {
		t.Fatalf("ProposeFlow: %v", err)
	}

	if err := svc.ApprovePlan(context.Background()); err != nil {
		t.Fatalf("ApprovePlan: %v", err)
	}
	progress, err := svc.GetPlanProgress(context.Background())
	if err != nil {
		t.Fatalf("GetPlanProgress: %v", err)
	}
	if progress.State != PlanStateApproved {
		t.Fatalf("state = %q, want approved", progress.State)
	}
}

func TestServiceApprovePlanWithoutPlanFails(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: AnalysisResult{}})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	if err := svc.ApprovePlan(context.Background()); !errors.Is(err, ErrNoPlan) {
		t.Fatalf("ApprovePlan = %v, want ErrNoPlan", err)
	}
}

func TestServiceRelinkPlanNodeRestoresMatchAfterRename(t *testing.T) {
	// The documented escape hatch for the FQN-matching tradeoff: the real
	// method exists under a different name than planned, so the node reads
	// not_implemented until the plan is pointed at the new name.
	analysis := AnalysisResult{
		Nodes: []Node{
			{ID: "com.acme.RefundService.handle(com.acme.Req)", Label: "com.acme.RefundService.handle", Kind: "method", Layer: "service", FilePath: "S.java", Line: 1, EndLine: 5},
		},
	}
	svc := newTestService(t, fakeAnalyzer{result: analysis})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	plan := Plan{Description: "x", Nodes: []PlanNode{
		{PlanNodeID: "pn-1", Label: "Core", FQN: "com.acme.RefundService.process", Kind: "method"},
	}}
	progress, err := svc.ProposeFlow(context.Background(), plan)
	if err != nil {
		t.Fatalf("ProposeFlow: %v", err)
	}
	if progress.Nodes[0].Status != PlanStatusNotImplemented {
		t.Fatalf("status = %q, want not_implemented before relink", progress.Nodes[0].Status)
	}

	if err := svc.RelinkPlanNode(context.Background(), "pn-1", "com.acme.RefundService.handle"); err != nil {
		t.Fatalf("RelinkPlanNode: %v", err)
	}

	progress, err = svc.GetPlanProgress(context.Background())
	if err != nil {
		t.Fatalf("GetPlanProgress: %v", err)
	}
	if progress.Nodes[0].Status != PlanStatusImplementedMatching {
		t.Fatalf("status = %q, want implemented_matching after relink", progress.Nodes[0].Status)
	}
}

func TestServiceRelinkUnknownPlanNodeFails(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: AnalysisResult{}})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	plan := Plan{Description: "x", Nodes: []PlanNode{{PlanNodeID: "pn-1", Label: "A", FQN: "com.acme.A.a", Kind: "method"}}}
	if _, err := svc.ProposeFlow(context.Background(), plan); err != nil {
		t.Fatalf("ProposeFlow: %v", err)
	}
	if err := svc.RelinkPlanNode(context.Background(), "pn-404", "com.acme.X.y"); !errors.Is(err, ErrPlanNodeNotFound) {
		t.Fatalf("RelinkPlanNode = %v, want ErrPlanNodeNotFound", err)
	}
}

func TestServiceGetPlanProgressWithoutPlanFails(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: AnalysisResult{}})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	if _, err := svc.GetPlanProgress(context.Background()); !errors.Is(err, ErrNoPlan) {
		t.Fatalf("GetPlanProgress = %v, want ErrNoPlan", err)
	}
}

// swappableAnalyzer lets a test change what the next OpenProject indexes,
// simulating "the agent wrote the code, then it got re-indexed".
type swappableAnalyzer struct {
	result AnalysisResult
}

func (a *swappableAnalyzer) Analyze(ctx context.Context, projectPath string) (AnalysisResult, error) {
	return a.result, nil
}
```

- [ ] **Step 2: Run to confirm they fail**

```bash
go test ./internal/codegraph/... -run "TestServiceGetPlanProgress|TestServiceApprovePlan|TestServiceRelink" -v
```

Expected: FAIL — compile error, the three methods do not exist yet.

- [ ] **Step 3: Implement**

In `internal/codegraph/service.go`:

```go
// GetPlanProgress returns per-node status plus the derived whole-plan
// status. Statuses are computed fresh against the current index on every
// call, so progress reflects the latest re-index automatically.
func (s *Service) GetPlanProgress(ctx context.Context) (PlanProgress, error) {
	store, err := s.activeStore()
	if err != nil {
		return PlanProgress{}, err
	}
	plan, err := store.GetPlan(ctx)
	if err != nil {
		return PlanProgress{}, err
	}
	return computeProgress(ctx, store, plan)
}

// ApprovePlan moves the active plan from draft to approved. Human-only by
// design — the bridge exposes it, MCP deliberately does not (see the doc's
// Bridge & MCP table: approving a plan is always a human decision).
func (s *Service) ApprovePlan(ctx context.Context) error {
	store, err := s.activeStore()
	if err != nil {
		return err
	}
	return store.SetPlanState(ctx, PlanStateApproved)
}

// RelinkPlanNode repoints one plan node at a different FQN — the escape
// hatch for the deliberate "matching by FQN breaks on rename" tradeoff.
// Explicit by design: DevKit never guesses that a new name is a renamed old
// one (see the doc's non-goals).
func (s *Service) RelinkPlanNode(ctx context.Context, planNodeID, fqn string) error {
	store, err := s.activeStore()
	if err != nil {
		return err
	}
	if fqn == "" {
		return fmt.Errorf("%w: relink requires a non-empty fqn", ErrInvalidPlan)
	}
	return store.SetPlanNodeFQN(ctx, planNodeID, fqn)
}
```

- [ ] **Step 4: Run the tests**

```bash
go test ./internal/codegraph/... -run "TestServiceGetPlanProgress|TestServiceApprovePlan|TestServiceRelink" -v
```

Expected: all 6 pass.

- [ ] **Step 5: Full package regression**

```bash
go test ./internal/codegraph/... -race
```

- [ ] **Step 6: Commit**

```bash
git add internal/codegraph/service.go internal/codegraph/service_test.go
git commit -m "feat(codegraph): GetPlanProgress, ApprovePlan, RelinkPlanNode"
```

---

## Task 5: Capability grants

**Files:**
- Modify: `internal/app/container.go`

- [ ] **Step 1: Add the grant**

In `internal/app/container.go`, next to the existing `builtin.codegraph` grants:

```go
		{SubjectID: "builtin.codegraph", Capability: "codegraph:plan", Scope: "plan:active"},
```

All five plan methods operate on the one active plan, so a single scope covers them — unlike `codegraph:fs`, which needed a per-file scope because each call names a different file.

- [ ] **Step 2: Build**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/app/container.go
git commit -m "feat(app): grant codegraph:plan capability"
```

---

## Task 6: Bridge methods

**Files:**
- Create: `internal/bridge/plan.go`
- Create: `internal/bridge/plan_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/bridge/plan_test.go
package bridge

import (
	"context"
	"errors"
	"testing"

	"devkit/internal/codegraph"
)

func newPlanTestService(t *testing.T, analysis codegraph.AnalysisResult) *AppService {
	t.Helper()
	s := newTestService(t)
	s.container.CodeGraph = codegraph.NewService(codegraph.NewNilStore(), fakeCodeGraphAnalyzer{result: analysis})
	if err := s.container.CodeGraph.SetProjectPath(t.TempDir() + "/index.db"); err != nil {
		t.Fatalf("SetProjectPath: %v", err)
	}
	if err := s.container.CodeGraph.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	return s
}

func TestCodeGraphProposeApproveAndProgress(t *testing.T) {
	s := newPlanTestService(t, codegraph.AnalysisResult{})

	plan := codegraph.Plan{
		Description: "Refund flow",
		Nodes: []codegraph.PlanNode{
			{PlanNodeID: "pn-1", Label: "Entry", FQN: "com.acme.A.create", Kind: "method"},
		},
	}
	progress, err := s.CodeGraphProposeFlow(plan)
	if err != nil {
		t.Fatalf("CodeGraphProposeFlow: %v", err)
	}
	if progress.State != codegraph.PlanStateDraft || progress.Aggregate != codegraph.PlanAggregateNotStarted {
		t.Fatalf("progress = %+v", progress)
	}

	flow, err := s.CodeGraphGetPlannedFlow()
	if err != nil {
		t.Fatalf("CodeGraphGetPlannedFlow: %v", err)
	}
	if len(flow.Nodes) != 1 || flow.Nodes[0].FQN != "com.acme.A.create" {
		t.Fatalf("flow = %+v", flow)
	}

	if err := s.CodeGraphApprovePlan(); err != nil {
		t.Fatalf("CodeGraphApprovePlan: %v", err)
	}
	progress, err = s.CodeGraphGetPlanProgress()
	if err != nil {
		t.Fatalf("CodeGraphGetPlanProgress: %v", err)
	}
	if progress.State != codegraph.PlanStateApproved {
		t.Fatalf("state = %q, want approved", progress.State)
	}
}

func TestCodeGraphRelinkPlanNodeBridge(t *testing.T) {
	s := newPlanTestService(t, codegraph.AnalysisResult{})
	plan := codegraph.Plan{
		Description: "x",
		Nodes:       []codegraph.PlanNode{{PlanNodeID: "pn-1", Label: "A", FQN: "com.acme.A.a", Kind: "method"}},
	}
	if _, err := s.CodeGraphProposeFlow(plan); err != nil {
		t.Fatalf("CodeGraphProposeFlow: %v", err)
	}

	if err := s.CodeGraphRelinkPlanNode("pn-1", "com.acme.A.b"); err != nil {
		t.Fatalf("CodeGraphRelinkPlanNode: %v", err)
	}
	flow, err := s.CodeGraphGetPlannedFlow()
	if err != nil {
		t.Fatalf("CodeGraphGetPlannedFlow: %v", err)
	}
	if flow.Nodes[0].FQN != "com.acme.A.b" {
		t.Fatalf("fqn = %q", flow.Nodes[0].FQN)
	}
}

func TestCodeGraphGetPlanProgressWithoutPlanFails(t *testing.T) {
	s := newPlanTestService(t, codegraph.AnalysisResult{})
	if _, err := s.CodeGraphGetPlanProgress(); !errors.Is(err, codegraph.ErrNoPlan) {
		t.Fatalf("expected ErrNoPlan, got %v", err)
	}
}
```

- [ ] **Step 2: Run to confirm they fail**

```bash
go test ./internal/bridge/... -run "TestCodeGraphPropose|TestCodeGraphRelink|TestCodeGraphGetPlanProgress" -v
```

Expected: FAIL — compile error, the five bridge methods do not exist yet.

- [ ] **Step 3: Implement**

```go
// internal/bridge/plan.go
package bridge

import (
	"context"

	"devkit/internal/codegraph"
)

func (s *AppService) CodeGraphProposeFlow(plan codegraph.Plan) (codegraph.PlanProgress, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:plan", "plan:active", map[string]any{
		"action": "propose_flow",
		"nodes":  len(plan.Nodes),
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.ProposeFlow(ctx, plan)
	})
	if err != nil {
		return codegraph.PlanProgress{}, err
	}
	return result.(codegraph.PlanProgress), nil
}

// CodeGraphApprovePlan is human-only — exposed on the bridge for the UI,
// deliberately absent from the MCP tool surface.
func (s *AppService) CodeGraphApprovePlan() error {
	_, err := s.call(context.Background(), "builtin.codegraph", "codegraph:plan", "plan:active", map[string]any{
		"action": "approve_plan",
	}, func(ctx context.Context) (any, error) {
		return nil, s.container.CodeGraph.ApprovePlan(ctx)
	})
	return err
}

func (s *AppService) CodeGraphGetPlannedFlow() (codegraph.Plan, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:plan", "plan:active", map[string]any{
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
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:plan", "plan:active", map[string]any{
		"action": "get_plan_progress",
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.GetPlanProgress(ctx)
	})
	if err != nil {
		return codegraph.PlanProgress{}, err
	}
	return result.(codegraph.PlanProgress), nil
}

func (s *AppService) CodeGraphRelinkPlanNode(planNodeID, fqn string) error {
	_, err := s.call(context.Background(), "builtin.codegraph", "codegraph:plan", "plan:active", map[string]any{
		"action":       "relink_plan_node",
		"plan_node_id": planNodeID,
		"fqn":          fqn,
	}, func(ctx context.Context) (any, error) {
		return nil, s.container.CodeGraph.RelinkPlanNode(ctx, planNodeID, fqn)
	})
	return err
}
```

- [ ] **Step 4: Run the tests**

```bash
go test ./internal/bridge/... -run TestCodeGraph -v
```

Expected: all pass, including every pre-existing `TestCodeGraph*` test.

- [ ] **Step 5: Commit**

```bash
git add internal/bridge/plan.go internal/bridge/plan_test.go
git commit -m "feat(codegraph): bridge methods for plan-first flow"
```

---

## Task 7: MCP tools

**Files:**
- Modify: `internal/mcp/server.go`
- Create: `internal/mcp/tools_codegraph_plan.go`
- Create: `internal/mcp/tools_codegraph_plan_test.go`
- Modify: `internal/mcp/tools_codegraph_test.go`

Four tools, not five: `approvePlan` is deliberately **not** exposed, per the doc's Bridge & MCP table. `proposeFlow` and `relinkPlanNode` are the one sanctioned exception to "agents never write via MCP" in all of DevKit — justified because a draft plan is inert metadata until a human approves it, and neither call touches source code.

- [ ] **Step 1: Write the failing tests**

```go
// internal/mcp/tools_codegraph_plan_test.go
package mcp

import (
	"context"
	"testing"

	"devkit/internal/codegraph"
)

func TestToolCodeGraphProposeFlow(t *testing.T) {
	deps := Deps{
		Caller: &fakeCaller{},
		CodeGraph: fakeCodeGraphAPI{planProgress: codegraph.PlanProgress{
			State:     codegraph.PlanStateDraft,
			Aggregate: codegraph.PlanAggregateNotStarted,
			Nodes:     []codegraph.PlanNodeProgress{{Status: codegraph.PlanStatusNotImplemented}},
		}},
	}
	s := NewServer(deps)
	in := codeGraphProposeFlowInput{
		Description: "Refund flow",
		Nodes:       []codegraph.PlanNode{{PlanNodeID: "pn-1", Label: "Entry", FQN: "com.acme.A.create", Kind: "method"}},
	}
	_, v, err := s.toolCodeGraphProposeFlow(context.Background(), nil, in)
	if err != nil {
		t.Fatalf("toolCodeGraphProposeFlow: %v", err)
	}
	progress, ok := v.(codegraph.PlanProgress)
	if !ok || progress.Aggregate != codegraph.PlanAggregateNotStarted {
		t.Fatalf("unexpected result: %#v", v)
	}
}

func TestToolCodeGraphGetPlannedFlowRemindsAgentOfFQNs(t *testing.T) {
	deps := Deps{
		Caller: &fakeCaller{},
		CodeGraph: fakeCodeGraphAPI{plan: codegraph.Plan{
			Description: "Refund flow",
			Nodes:       []codegraph.PlanNode{{PlanNodeID: "pn-1", FQN: "com.acme.A.create", Kind: "method"}},
		}},
	}
	s := NewServer(deps)
	_, v, err := s.toolCodeGraphGetPlannedFlow(context.Background(), nil, struct{}{})
	if err != nil {
		t.Fatalf("toolCodeGraphGetPlannedFlow: %v", err)
	}
	out, ok := v.(plannedFlowResult)
	if !ok {
		t.Fatalf("unexpected result type: %#v", v)
	}
	if out.Instruction == "" {
		t.Fatal("expected a non-empty instruction reminding the agent to use the planned FQNs exactly")
	}
	if len(out.Plan.Nodes) != 1 || out.Plan.Nodes[0].FQN != "com.acme.A.create" {
		t.Fatalf("plan = %+v", out.Plan)
	}
}

func TestToolCodeGraphGetPlanProgress(t *testing.T) {
	deps := Deps{
		Caller:    &fakeCaller{},
		CodeGraph: fakeCodeGraphAPI{planProgress: codegraph.PlanProgress{Aggregate: codegraph.PlanAggregateComplete}},
	}
	s := NewServer(deps)
	_, v, err := s.toolCodeGraphGetPlanProgress(context.Background(), nil, struct{}{})
	if err != nil {
		t.Fatalf("toolCodeGraphGetPlanProgress: %v", err)
	}
	progress, ok := v.(codegraph.PlanProgress)
	if !ok || progress.Aggregate != codegraph.PlanAggregateComplete {
		t.Fatalf("unexpected result: %#v", v)
	}
}

func TestToolCodeGraphRelinkPlanNode(t *testing.T) {
	s := NewServer(Deps{Caller: &fakeCaller{}, CodeGraph: fakeCodeGraphAPI{}})
	in := codeGraphRelinkPlanNodeInput{PlanNodeID: "pn-1", FQN: "com.acme.A.b"}
	if _, _, err := s.toolCodeGraphRelinkPlanNode(context.Background(), nil, in); err != nil {
		t.Fatalf("toolCodeGraphRelinkPlanNode: %v", err)
	}
}
```

Extend `fakeCodeGraphAPI` in `internal/mcp/tools_codegraph_test.go` with the fields and methods the new interface requires (add these alongside whatever fields earlier plans in this sequence already added):

```go
	plan         codegraph.Plan
	planProgress codegraph.PlanProgress
```

```go
func (f fakeCodeGraphAPI) ProposeFlow(ctx context.Context, plan codegraph.Plan) (codegraph.PlanProgress, error) {
	return f.planProgress, f.err
}
func (f fakeCodeGraphAPI) GetPlannedFlow(ctx context.Context) (codegraph.Plan, error) {
	return f.plan, f.err
}
func (f fakeCodeGraphAPI) GetPlanProgress(ctx context.Context) (codegraph.PlanProgress, error) {
	return f.planProgress, f.err
}
func (f fakeCodeGraphAPI) RelinkPlanNode(ctx context.Context, planNodeID, fqn string) error {
	return f.err
}
```

- [ ] **Step 2: Run to confirm they fail**

```bash
go test ./internal/mcp/... -run "TestToolCodeGraphPropose|TestToolCodeGraphGetPlan|TestToolCodeGraphRelink" -v
```

Expected: FAIL — compile error, none of the handlers or input types exist yet.

- [ ] **Step 3: Extend the interface**

In `internal/mcp/server.go`, add to `CodeGraphAPI` (alongside whatever earlier plans in this sequence already added — `ApprovePlan` is deliberately absent):

```go
	ProposeFlow(ctx context.Context, plan codegraph.Plan) (codegraph.PlanProgress, error)
	GetPlannedFlow(ctx context.Context) (codegraph.Plan, error)
	GetPlanProgress(ctx context.Context) (codegraph.PlanProgress, error)
	RelinkPlanNode(ctx context.Context, planNodeID, fqn string) error
```

- [ ] **Step 4: Implement the tools**

```go
// internal/mcp/tools_codegraph_plan.go
package mcp

import (
	"context"

	"devkit/internal/codegraph"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// planFQNInstruction is returned with every getPlannedFlow response. The
// design doc requires the reminder to travel with the data on every call
// rather than relying on the agent to remember the rule from a previous
// turn — FQN matching is the entire basis of progress tracking, and a
// differently-named method silently reads as "not implemented".
const planFQNInstruction = "Implement each node using EXACTLY the package, class, and method name given in its fqn field. " +
	"Progress is tracked by matching these names against the real index — a different name is reported as not implemented. " +
	"If you must use a different name, call codegraph.relinkPlanNode to repoint the plan node first."

func (s *Server) registerCodeGraphPlan() {
	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "codegraph.proposeFlow",
		Description: "Propose a draft flow of intended classes/methods for a new feature, for a human to review and approve",
	}, s.toolCodeGraphProposeFlow)

	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "codegraph.getPlannedFlow",
		Description: "Get the active plan's nodes (with the exact FQNs to create), edges, and conditions",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolCodeGraphGetPlannedFlow)

	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "codegraph.getPlanProgress",
		Description: "Get per-node implementation status and the overall plan status, diffed against the current index",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolCodeGraphGetPlanProgress)

	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "codegraph.relinkPlanNode",
		Description: "Repoint one plan node at a different fully-qualified name, after a deliberate rename",
	}, s.toolCodeGraphRelinkPlanNode)
}

type codeGraphProposeFlowInput struct {
	Description string                `json:"description"`
	Nodes       []codegraph.PlanNode  `json:"nodes"`
	Edges       []codegraph.PlanEdge  `json:"edges"`
}

func (s *Server) toolCodeGraphProposeFlow(ctx context.Context, _ *sdk.CallToolRequest, in codeGraphProposeFlowInput) (*sdk.CallToolResult, any, error) {
	plan := codegraph.Plan{Description: in.Description, Nodes: in.Nodes, Edges: in.Edges}
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:plan", "plan:active", meta("propose_flow", map[string]any{"nodes": len(in.Nodes)}), func(context.Context) (any, error) {
		return s.deps.CodeGraph.ProposeFlow(ctx, plan)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}

// plannedFlowResult pairs the plan with the naming instruction, so the
// reminder cannot be separated from the data it applies to.
type plannedFlowResult struct {
	Plan        codegraph.Plan `json:"plan"`
	Instruction string         `json:"instruction"`
}

func (s *Server) toolCodeGraphGetPlannedFlow(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:plan", "plan:active", meta("get_planned_flow", nil), func(context.Context) (any, error) {
		plan, err := s.deps.CodeGraph.GetPlannedFlow(ctx)
		if err != nil {
			return nil, err
		}
		return plannedFlowResult{Plan: plan, Instruction: planFQNInstruction}, nil
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}

func (s *Server) toolCodeGraphGetPlanProgress(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:plan", "plan:active", meta("get_plan_progress", nil), func(context.Context) (any, error) {
		return s.deps.CodeGraph.GetPlanProgress(ctx)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}

type codeGraphRelinkPlanNodeInput struct {
	PlanNodeID string `json:"planNodeId"`
	FQN        string `json:"fqn"`
}

func (s *Server) toolCodeGraphRelinkPlanNode(ctx context.Context, _ *sdk.CallToolRequest, in codeGraphRelinkPlanNodeInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:plan", "plan:active", meta("relink_plan_node", map[string]any{"plan_node_id": in.PlanNodeID, "fqn": in.FQN}), func(context.Context) (any, error) {
		return nil, s.deps.CodeGraph.RelinkPlanNode(ctx, in.PlanNodeID, in.FQN)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}
```

- [ ] **Step 5: Register the new tool group**

In `internal/mcp/server.go`, `NewServer`, next to the existing `s.registerCodeGraph()` call:

```go
	s.registerCodeGraphPlan()
```

- [ ] **Step 6: Run the tests**

```bash
go test ./internal/mcp/... -run TestToolCodeGraph -v
```

Expected: all pass, including every pre-existing `TestToolCodeGraph*` test.

- [ ] **Step 7: Commit**

```bash
git add internal/mcp/server.go internal/mcp/tools_codegraph_plan.go internal/mcp/tools_codegraph_plan_test.go internal/mcp/tools_codegraph_test.go
git commit -m "feat(codegraph): expose plan-first flow tools via MCP (approvePlan stays human-only)"
```

---

## Task 8: Full regression

**Files:** none (verification only)

- [ ] **Step 1: Standard check**

```bash
go test ./internal/... ./cmd/... -count=1
go build ./...
```

- [ ] **Step 2: Race**

```bash
go test ./internal/... ./cmd/... -race -count=1
```

Expected: everything passes.

- [ ] **Step 3: Confirm `approvePlan` is genuinely not reachable over MCP**

```bash
grep -rn "approvePlan\|ApprovePlan" internal/mcp/
```

Expected: **no output**. `ApprovePlan` must exist only on `Service` and `AppService`, never on `CodeGraphAPI` or any registered tool — a human-only decision, per the design doc. If this grep prints anything, remove it before considering the plan done.

---

## What this plan does NOT do

**No canvas UI** — no dashed-line rendering of draft nodes, no on-canvas edge re-wiring, no approve button. That is frontend work belonging with the deferred Graph UI canvas plan; every method here is exercised end-to-end by tests and usable over MCP today. **No stored per-node `status` column and no re-index hook** — see this plan's header; status is derived on read, which satisfies the doc's "auto-updates after re-index" requirement exactly, and means `2026-08-19-codegraph-auto-reindex.md` needs no changes. **No fuzzy FQN matching, no automatic rename detection, no multi-plan history or versioning, no agent auto-resume** — all four are explicit non-goals in the design doc. **No machine verification of edge `condition` text** — deliberately, per the doc's "Điều kiện trên cạnh" section: conditions are plan-time documentation for the human reviewing the canvas, and the auto-diff checks only that wiring exists, never that a condition string matches real code.

With this plan, every backend phase of CodeGraph has a written plan. What remains across the whole module is frontend and the analyzer itself: the Java analyzer CLI (`2026-08-19-codegraph-java-analyzer.md`, written but unimplemented), the CodeGraph frontend module, the file-editing panel, the Graph UI canvas, and the Terminal `xterm.js` component.
