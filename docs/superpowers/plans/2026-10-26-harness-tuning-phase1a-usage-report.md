# Token Usage Report (Phase 1a) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show, under Settings → Agents → Accounts & limits, what a vendor's turns spent in tokens, by workspace, day, model and session, read correctly from the data DevKit already stores.

**Architecture:** A pure function re-derives each turn's usage from its raw vendor meta with the adapter's current `NormalizeUsage` (the stored structured fields hold false zeros for older turns). A pure aggregator groups the rows and preserves the number of known values for each metric. An optional `TurnUsageSource` on the SQL store lists the rows, and `Service.UsageReport` ties them together for the window only. A UI-only bridge method returns numbers and ids; the page names workspaces from the list it already has and draws the report with the shared `DataTable`.

**Tech Stack:** Go (`internal/agenthub`, `internal/bridge`, `internal/app`), SQLite, Wails v3 bindings, React + TypeScript + Tailwind, `bun test`.

**Spec:** `docs/superpowers/specs/2026-10-26-subscription-harness-tuning-design.md`, section 3 (Phase 1). This plan is **1a**: items 1, 2, 4, 5. Item 3 (account id and profile revision on each turn, tool result sizes) is plan 1b and is not here.

**Scope of the plan:** Claude only has data (opencode has 44 turns and Codex 1, with no usable usage); other vendors show "No token data yet". Nothing here is an MCP tool and nothing writes data.

---

## Reporting contract

- Every metric carries its sum **and coverage**: `known.<metric>` is the number of turns with a known value, including a genuine 0.
  A missing field contributes neither a value nor coverage. `TurnsWithoutData` counts turns with no known metric; it does not
  describe coverage of individual fields. Preserve the spec's existing normalizer limitation for plain input/output fields.
- UI cells show `—` for no known values, the sum for complete coverage, and `<sum> (partial)` for incomplete coverage.
  Model rows carry coverage for total only; their fresh input, cache and output cells show `—`.
- Percentages require complete coverage of both operands over the relevant population. Otherwise show `—`; never divide sums
  from different known subsets. With a known zero denominator also show `—`.
- Fresh input is visible beside cache read/write and output. A model-split warning says the split is incomplete or differs;
  it never infers helper models from a mismatch alone.
- The usage query explicitly enables refetch on window focus, overriding the application's disabled default. An integration
  test uses the real QueryClient defaults and a focus event to verify a stale active query reads again.

## Implementation status — 2026-10-26

Phase 1a is implemented in the current checkout. The existing branch and the earlier plan edits were preserved; branch creation
and commit steps below were not executed. The navigation test was checked after implementation, not as a separate red step.

Verified:
- Go: `go test ./internal/agenthub/ ./internal/bridge/ ./internal/app/ ./internal/arch/ ./internal/mcp/ -count=1` passed.
  After the server-mode decoder fix, the Agent Hub and architecture suites passed again.
- `go vet ./internal/agenthub/ ./internal/bridge/ ./internal/app/`, formatting and `git diff --check` passed.
- Frontend: `bun test --isolate` passed all 5,041 tests on the final run; production build and performance budgets passed.
  An earlier run under concurrent build/test load hit two existing countdown assertions in `surfaces.test.tsx` (expected 60s,
  rendered 57s). That file passed independently, and the complete rerun passed without changing those tests.
- A read-only backup of the local database matched independent SQL: 513 Claude turns, 960,683,040 total tokens,
  932,811,339 cache-read tokens. The live file was opened read-only and the temporary backup was removed.
- The report component was checked in a browser preview at wide and 520px panel widths, including unknown model metrics and
  the Codex no-data state. Horizontal scrolling stays inside each table. The native desktop Settings flow was not exercised.
- Wails bindings were regenerated and `go build -tags server` passed.

Implementation adjustment: `ForServerMode` retains only each vendor's pure usage-normalization function. Without it the
server stub made historical reports unknown. Execution and sign-in stay refused; a regression test covers parsing and refusal.

## Known traps (found while verifying this plan against the repository)

1. In `package agenthub_test` the helper name `itoa` already exists. Use `strconv.Itoa`.
2. `agent_sessions.workspace_id` is a foreign key. A test that gives a session a workspace id must first insert that workspace (`INSERT INTO workspaces (id, name, root) VALUES (...)`), or `InsertSession` fails with `FOREIGN KEY constraint failed`.
3. `internal/bridge/ops_contract_test.go` (`TestBridgeShimsMatchTheRegistry`) fails for any new `AppService` method that is not an op or listed in `internal/bridge/testdata/legacy_ops_methods.json`. Add the method name to that list, keeping it sorted.
4. Regenerate bindings with `wails3 generate bindings -clean=true -ts -i` from the repository root. Without `-ts` it writes `.js` files and deletes the committed `.ts` ones.
5. `frontend/src/smoothnessRatchet.test.ts` counts files that contain `<table` and `.map(` without a virtualizer, and the baseline must never go up. Draw tables with the shared `DataTable` (`@/components/ui/data-table/DataTable`), not a raw `<table>`.
6. A fresh git worktree has no `frontend/dist` (it is git-ignored and embedded by `main.go`), so `go test ./internal/arch/` fails with `pattern all:frontend/dist: no matching files found`. Run `mkdir -p frontend/dist && touch frontend/dist/.keep` there.
7. Editor diagnostics from a worktree outside the workspace ("not included in your workspace", "undefined: ...") are noise. Trust `go vet` and `go test`.

---

## File structure

| File | Responsibility |
| --- | --- |
| `internal/agenthub/turn_usage.go` | `TurnUsage`, `DeriveTurnUsage`: what one turn spent, read from raw meta |
| `internal/agenthub/usage_report.go` | `TurnUsageRow`, `UsageCoverage`, `UsageTotals`, `UsageReport`, `BuildUsageReport`: grouping and sums, pure |
| `internal/agenthub/adapter_servermode.go`, `adapter_servermode_test.go` | Preserve the pure usage decoder for historical reports while refusing execution |
| `internal/agenthub/usage_source.go` | `TurnUsageSource` (optional store interface), `SQLStore.TurnUsageRows`, `Service.UsageReport` |
| `internal/bridge/agenthub_usage.go` | UI-only `AppService.AgentUsageReport` and its DTOs |
| `internal/app/container.go` | the `usage:*` read grant |
| `internal/bridge/testdata/legacy_ops_methods.json` | one added method name |
| `frontend/bindings/devkit/internal/bridge/*` | regenerated |
| `frontend/src/bridge/agentUsage.ts` | typed wrapper over the binding |
| `frontend/src/modules/agenthub/usage/usageModel.ts` | pure formatting and row building |
| `frontend/src/modules/agenthub/usage/UsageReportTable.tsx` | presentational report |
| `frontend/src/modules/agenthub/usage/useAgentUsage.ts` | react-query hooks |
| `frontend/src/modules/agenthub/usage/AgentUsageSetting.tsx` | connected panel |
| `frontend/src/modules/settings/AgentsPages.tsx`, `settingsNavigation.ts` | mount and search keywords |
| `docs/agent-reference/agent-hub-accounts.md` | one bullet |

---

### Task 0: Branch and baseline

**Files:** none

- [ ] **Step 1: Create the branch from main**

```bash
git switch -c feat/agent-usage-report main
```

- [x] **Step 2: Confirm the embed directory exists (needed by `go test ./internal/arch/`)**

Run: `ls frontend/dist >/dev/null 2>&1 || (mkdir -p frontend/dist && touch frontend/dist/.keep)`
Expected: no output.

- [x] **Step 3: Baseline**

Run: `go test ./internal/agenthub/ ./internal/bridge/ ./internal/app/ ./internal/arch/ -count=1`
Expected: four lines starting with `ok`.

---

### Task 1: Read a turn's usage

**Files:**
- Create: `internal/agenthub/turn_usage.go`
- Test: `internal/agenthub/turn_usage_test.go`

- [x] **Step 1: Write the failing test**

`internal/agenthub/turn_usage_test.go`:

```go
package agenthub_test

import (
	"encoding/json"
	"testing"

	"devkit/internal/agenthub"
)

// claudeMeta is the raw meta of a real Claude turn: the numbers sit under
// quota.token_count, with a per-model split of the same totals.
const claudeMeta = `{"quota":{"token_count":{"totalTokens":2440835,"inputTokens":8,"cachedInputTokens":1832413,"cachedWriteTokens":601633,"outputTokens":6781,"reasoningOutputTokens":0},"model_usage":[{"model":"claude-opus-5-5","token_count":{"totalTokens":2440835,"inputTokens":8,"cachedInputTokens":1832413,"cachedWriteTokens":601633,"outputTokens":6781,"reasoningOutputTokens":0}}]}}`

func usageJSONOf(t *testing.T, structured agenthub.NormalizedUsage, raw string) json.RawMessage {
	t.Helper()
	if raw != "" {
		structured.RawMeta = json.RawMessage(raw)
	}
	out, err := json.Marshal(structured)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func wantInt(t *testing.T, name string, got *int, want int) {
	t.Helper()
	if got == nil {
		t.Errorf("%s: unknown, want %d", name, want)
		return
	}
	if *got != want {
		t.Errorf("%s = %d, want %d", name, *got, want)
	}
}

func wantUnknown(t *testing.T, name string, got *int) {
	t.Helper()
	if got != nil {
		t.Errorf("%s = %d, want unknown", name, *got)
	}
}

// A row stored while the normalizer did not understand the meta carries 0 for
// input and output next to the real numbers in the raw meta. The raw meta wins.
func TestDeriveTurnUsageIgnoresAFalseZeroNextToRealRawNumbers(t *testing.T) {
	adapter := agenthub.NewClaudeAdapter(agenthub.ClaudeOptions{})
	stored := usageJSONOf(t, agenthub.NormalizedUsage{InputTokens: 0, OutputTokens: 0}, claudeMeta)
	got := agenthub.DeriveTurnUsage(adapter, stored, json.RawMessage(claudeMeta))
	wantInt(t, "total", got.Total, 2440835)
	wantInt(t, "fresh input", got.FreshInput, 8)
	wantInt(t, "output", got.Output, 6781)
	wantInt(t, "cache read", got.CacheRead, 1832413)
	wantInt(t, "cache write", got.CacheWrite, 601633)
	wantInt(t, "reasoning", got.Reasoning, 0)
	if len(got.Models) != 1 || got.Models[0].Model != "claude-opus-5-5" || got.Models[0].Total != 2440835 || got.SplitPartial {
		t.Errorf("models = %+v partial=%v", got.Models, got.SplitPartial)
	}
}

// A 0 the raw meta really says is a known 0, not a missing number.
func TestDeriveTurnUsageKeepsARealZero(t *testing.T) {
	adapter := agenthub.NewClaudeAdapter(agenthub.ClaudeOptions{})
	raw := `{"quota":{"token_count":{"totalTokens":0,"inputTokens":0,"cachedInputTokens":0,"cachedWriteTokens":0,"outputTokens":0,"reasoningOutputTokens":0}}}`
	got := agenthub.DeriveTurnUsage(adapter, usageJSONOf(t, agenthub.NormalizedUsage{}, raw), json.RawMessage(raw))
	wantInt(t, "total", got.Total, 0)
	wantInt(t, "fresh input", got.FreshInput, 0)
	wantInt(t, "output", got.Output, 0)
	wantInt(t, "cache read", got.CacheRead, 0)
}

// A field the raw meta does not have is unknown, and the fields it has stay known.
func TestDeriveTurnUsageLeavesAnAbsentFieldUnknown(t *testing.T) {
	adapter := agenthub.NewClaudeAdapter(agenthub.ClaudeOptions{})
	raw := `{"quota":{"token_count":{"totalTokens":100,"inputTokens":5,"outputTokens":7}}}`
	got := agenthub.DeriveTurnUsage(adapter, usageJSONOf(t, agenthub.NormalizedUsage{}, raw), json.RawMessage(raw))
	wantInt(t, "total", got.Total, 100)
	wantInt(t, "fresh input", got.FreshInput, 5)
	wantInt(t, "output", got.Output, 7)
	wantUnknown(t, "cache read", got.CacheRead)
	wantUnknown(t, "cache write", got.CacheWrite)
	wantUnknown(t, "reasoning", got.Reasoning)
}

// A meta that says nothing about tokens is no data, not a turn that cost 0.
func TestDeriveTurnUsageTreatsAMetaWithoutTokensAsUnknown(t *testing.T) {
	adapter := agenthub.NewClaudeAdapter(agenthub.ClaudeOptions{})
	got := agenthub.DeriveTurnUsage(adapter, usageJSONOf(t, agenthub.NormalizedUsage{}, `{}`), json.RawMessage(`{}`))
	wantUnknown(t, "total", got.Total)
	wantUnknown(t, "fresh input", got.FreshInput)
	wantUnknown(t, "output", got.Output)
}

// With no raw meta in the row at all, the stored fields are all there is.
func TestDeriveTurnUsageFallsBackToStoredFieldsOnlyWithoutRawMeta(t *testing.T) {
	adapter := agenthub.NewClaudeAdapter(agenthub.ClaudeOptions{})
	total, read, write := 5394506, 5379544, 11505
	stored := usageJSONOf(t, agenthub.NormalizedUsage{
		InputTokens: 12, OutputTokens: 3, TotalTokens: &total, CachedInputTokens: &read, CachedWriteTokens: &write,
	}, "")
	got := agenthub.DeriveTurnUsage(adapter, stored, nil)
	wantInt(t, "total", got.Total, 5394506)
	wantInt(t, "fresh input", got.FreshInput, 12)
	wantInt(t, "cache read", got.CacheRead, 5379544)

	zero := usageJSONOf(t, agenthub.NormalizedUsage{}, "")
	none := agenthub.DeriveTurnUsage(adapter, zero, nil)
	wantUnknown(t, "stored zeros without raw: total", none.Total)
	wantUnknown(t, "stored zeros without raw: fresh input", none.FreshInput)
}

// The raw meta is also kept inside the stored usage, for rows whose own raw column is empty.
func TestDeriveTurnUsageReadsTheRawMetaKeptInsideTheStoredUsage(t *testing.T) {
	adapter := agenthub.NewClaudeAdapter(agenthub.ClaudeOptions{})
	stored := usageJSONOf(t, agenthub.NormalizedUsage{}, claudeMeta)
	got := agenthub.DeriveTurnUsage(adapter, stored, nil)
	wantInt(t, "total", got.Total, 2440835)
	wantInt(t, "output", got.Output, 6781)
}

// A malformed, larger or uncomparable split must be marked partial.
func TestDeriveTurnUsageMarksUnreadableOrUncomparableSplits(t *testing.T) {
	adapter := agenthub.NewClaudeAdapter(agenthub.ClaudeOptions{})
	for _, raw := range []string{
		`{"quota":{"token_count":{"totalTokens":20},"model_usage":[{"model":"a","token_count":{}}]}}`,
		`{"quota":{"token_count":{"totalTokens":20},"model_usage":[{"model":"a","token_count":{"totalTokens":30}}]}}`,
		`{"quota":{"model_usage":[{"model":"a","token_count":{"totalTokens":10}}]}}`,
	} {
		if got := agenthub.DeriveTurnUsage(adapter, nil, json.RawMessage(raw)); !got.SplitPartial {
			t.Errorf("unreadable, larger or uncomparable split must be partial: %s", raw)
		}
	}
}

func TestDeriveTurnUsageMarksASplitThatDoesNotAddUpAsPartial(t *testing.T) {
	adapter := agenthub.NewClaudeAdapter(agenthub.ClaudeOptions{})
	raw := `{"quota":{"token_count":{"totalTokens":20,"inputTokens":1,"outputTokens":1},"model_usage":[{"model":"a","token_count":{"totalTokens":10}}]}}`
	got := agenthub.DeriveTurnUsage(adapter, usageJSONOf(t, agenthub.NormalizedUsage{}, raw), json.RawMessage(raw))
	if len(got.Models) != 1 || !got.SplitPartial {
		t.Errorf("models = %+v partial=%v", got.Models, got.SplitPartial)
	}
	wantInt(t, "total", got.Total, 20)
}
```

- [x] **Step 2: Run it to verify it fails**

Run: `go test ./internal/agenthub/ -run TestDeriveTurnUsage -count=1`
Expected: FAIL to build, with `undefined: agenthub.DeriveTurnUsage`.

- [x] **Step 3: Write the implementation**

`internal/agenthub/turn_usage.go`:

```go
package agenthub

import (
	"bytes"
	"encoding/json"
)

// TurnUsage is what one turn spent. A nil field means the vendor's meta did
// not say, which is not the same as 0: a report counts such a turn as "no
// data" and never adds it as 0.
type TurnUsage struct {
	FreshInput *int
	CacheRead  *int
	CacheWrite *int
	Output     *int
	Reasoning  *int
	Total      *int
	// Models is the per-model split of the same totals. It is never added to
	// the fields above.
	Models []ModelTurnUsage
	// SplitPartial is set for unequal totals, unreadable entries, or a nonempty
	// split that cannot be compared because the turn total is unknown.
	SplitPartial bool
}

// ModelTurnUsage is one model's share of a turn's total.
type ModelTurnUsage struct {
	Model string
	Total int
}

func (u TurnUsage) known() bool {
	return u.FreshInput != nil || u.CacheRead != nil || u.CacheWrite != nil ||
		u.Output != nil || u.Reasoning != nil || u.Total != nil
}

// DeriveTurnUsage reads what a turn spent.
//
// The stored structured fields are not trusted: NormalizedUsage.InputTokens
// and OutputTokens are plain numbers that json.Marshal always writes, so a row
// stored while the normalizer did not understand the vendor's meta holds 0
// beside real numbers in the raw meta. The raw meta (the row's own column, else
// the copy kept inside the stored usage) is therefore normalized again with the
// adapter's current NormalizeUsage. The stored fields are used only when the
// row has no raw meta at all.
func DeriveTurnUsage(adapter VendorAdapter, usageJSON, rawMeta json.RawMessage) TurnUsage {
	meta := rawMeta
	if len(meta) == 0 {
		meta = metaKeptInUsage(usageJSON)
	}
	if len(meta) > 0 {
		n, err := adapter.NormalizeUsage(meta)
		if err != nil {
			return TurnUsage{}
		}
		u := usageFromNormalized(n)
		u.Models, u.SplitPartial = modelSplit(meta, u.Total)
		return u
	}
	var stored NormalizedUsage
	if len(usageJSON) == 0 || json.Unmarshal(usageJSON, &stored) != nil {
		return TurnUsage{}
	}
	return usageFromNormalized(stored)
}

// usageFromNormalized decides what a normalizer's answer knows. InputTokens and
// OutputTokens cannot say "absent", so an answer is known only if anything in it
// is: a pointer field, or a non-zero number. A vendor that reported nothing
// gives all zeros and nil pointers, and that is "no data".
func usageFromNormalized(n NormalizedUsage) TurnUsage {
	known := n.TotalTokens != nil || n.CachedInputTokens != nil || n.CachedWriteTokens != nil ||
		n.ReasoningOutputTokens != nil || n.InputTokens != 0 || n.OutputTokens != 0
	if !known {
		return TurnUsage{}
	}
	in, out := n.InputTokens, n.OutputTokens
	return TurnUsage{
		FreshInput: &in,
		CacheRead:  n.CachedInputTokens,
		CacheWrite: n.CachedWriteTokens,
		Output:     &out,
		Reasoning:  n.ReasoningOutputTokens,
		Total:      n.TotalTokens,
	}
}

func metaKeptInUsage(usageJSON json.RawMessage) json.RawMessage {
	var kept struct {
		RawMeta json.RawMessage
	}
	if len(usageJSON) == 0 || json.Unmarshal(usageJSON, &kept) != nil {
		return nil
	}
	if len(kept.RawMeta) == 0 || bytes.Equal(bytes.TrimSpace(kept.RawMeta), []byte("null")) {
		return nil
	}
	return kept.RawMeta
}

// modelSplit reads quota.model_usage, Claude's per-model breakdown of the turn
// total. An entry without a model or a total is skipped and makes the split partial.
func modelSplit(meta json.RawMessage, total *int) ([]ModelTurnUsage, bool) {
	var root struct {
		Quota *struct {
			ModelUsage []struct {
				Model      string `json:"model"`
				TokenCount *struct {
					TotalTokens *int `json:"totalTokens"`
				} `json:"token_count"`
			} `json:"model_usage"`
		} `json:"quota"`
	}
	if json.Unmarshal(meta, &root) != nil || root.Quota == nil {
		return nil, false
	}
	var out []ModelTurnUsage
	partial := false
	sum := 0
	for _, m := range root.Quota.ModelUsage {
		if m.Model == "" || m.TokenCount == nil || m.TokenCount.TotalTokens == nil {
			partial = true
			continue
		}
		out = append(out, ModelTurnUsage{Model: m.Model, Total: *m.TokenCount.TotalTokens})
		sum += *m.TokenCount.TotalTokens
	}
	if len(out) == 0 {
		return nil, partial
	}
	if total == nil || sum != *total {
		partial = true
	}
	return out, partial
}
```

- [x] **Step 4: Run the tests to verify they pass**

Run: `gofmt -l internal/agenthub && go test ./internal/agenthub/ -run TestDeriveTurnUsage -count=1 -v`
Expected: `gofmt` prints nothing; all `TestDeriveTurnUsage` cases pass; `ok  	devkit/internal/agenthub`.

- [ ] **Step 5: Commit**

```bash
git add internal/agenthub/turn_usage.go internal/agenthub/turn_usage_test.go
git commit -m "feat(agenthub): read a turn's usage from its raw vendor meta

The stored InputTokens and OutputTokens are always serialized, so turns stored
while the normalizer did not understand the meta hold a false 0 beside real
numbers in the raw meta. Re-derive usage from the raw meta with the adapter's
current NormalizeUsage and treat a turn with nothing in it as unknown, not 0."
```

---

### Task 2: Group turns into a report

**Files:**
- Create: `internal/agenthub/usage_report.go`
- Test: `internal/agenthub/usage_report_test.go`

- [x] **Step 1: Write the failing test**

`internal/agenthub/usage_report_test.go`:

```go
package agenthub_test

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"devkit/internal/agenthub"
)

func claudeRow(turn, session, workspace string, at time.Time, total, in, read, write, out int, model string) agenthub.TurnUsageRow {
	raw := json.RawMessage(
		`{"quota":{"token_count":{"totalTokens":` + strconv.Itoa(total) + `,"inputTokens":` + strconv.Itoa(in) +
			`,"cachedInputTokens":` + strconv.Itoa(read) + `,"cachedWriteTokens":` + strconv.Itoa(write) +
			`,"outputTokens":` + strconv.Itoa(out) + `},"model_usage":[{"model":"` + model +
			`","token_count":{"totalTokens":` + strconv.Itoa(total) + `}}]}}`)
	return agenthub.TurnUsageRow{
		TurnID: turn, SessionID: session, WorkspaceID: workspace, CreatedAt: at,
		UsageJSON: json.RawMessage(`{"CostUSD":0,"InputTokens":0,"OutputTokens":0}`), RawMeta: raw,
	}
}

func TestBuildUsageReportGroupsAndSums(t *testing.T) {
	adapter := agenthub.NewClaudeAdapter(agenthub.ClaudeOptions{})
	d1 := time.Date(2026, 10, 25, 23, 59, 0, 0, time.UTC)
	d2 := time.Date(2026, 10, 26, 0, 1, 0, 0, time.UTC)
	rows := []agenthub.TurnUsageRow{
		claudeRow("t1", "s1", "w1", d1, 1000, 1, 900, 50, 49, "opus"),
		claudeRow("t2", "s1", "w1", d2, 3000, 2, 2800, 100, 98, "opus"),
		claudeRow("t3", "s2", "w2", d2, 500, 3, 400, 10, 87, "sonnet"),
		// no tokens in the meta at all: counted, never added as 0
		{TurnID: "t4", SessionID: "s2", WorkspaceID: "w2", CreatedAt: d2, UsageJSON: json.RawMessage(`{"InputTokens":0,"OutputTokens":0}`), RawMeta: json.RawMessage(`{}`)},
	}
	rep := agenthub.BuildUsageReport(agenthub.AgentTypeClaude, adapter, rows)

	if rep.Totals.Turns != 4 || rep.Totals.TurnsWithoutData != 1 {
		t.Errorf("turns = %d without data = %d, want 4 and 1", rep.Totals.Turns, rep.Totals.TurnsWithoutData)
	}
	if rep.Totals.Total != 4500 || rep.Totals.CacheRead != 4100 || rep.Totals.CacheWrite != 160 || rep.Totals.FreshInput != 6 || rep.Totals.Output != 234 {
		t.Errorf("totals = %+v", rep.Totals)
	}
	if len(rep.ByWorkspace) != 2 || rep.ByWorkspace[0].Key != "w1" || rep.ByWorkspace[0].Total != 4000 || rep.ByWorkspace[1].Key != "w2" || rep.ByWorkspace[1].TurnsWithoutData != 1 {
		t.Errorf("by workspace = %+v", rep.ByWorkspace)
	}
	// Days are UTC and oldest first.
	if len(rep.ByDay) != 2 || rep.ByDay[0].Key != "2026-10-25" || rep.ByDay[0].Total != 1000 || rep.ByDay[1].Key != "2026-10-26" || rep.ByDay[1].Total != 3500 {
		t.Errorf("by day = %+v", rep.ByDay)
	}
	if len(rep.ByModel) != 2 || rep.ByModel[0].Key != "opus" || rep.ByModel[0].Total != 4000 || rep.ByModel[0].Turns != 2 || rep.ByModel[1].Key != "sonnet" {
		t.Errorf("by model = %+v", rep.ByModel)
	}
	if len(rep.TopSessions) != 2 || rep.TopSessions[0].SessionID != "s1" || rep.TopSessions[0].Total != 4000 || rep.TopSessions[0].WorkspaceID != "w1" {
		t.Errorf("top sessions = %+v", rep.TopSessions)
	}
}

func TestBuildUsageReportKeepsCoverageOfMissingAndZeroMetrics(t *testing.T) {
	adapter := agenthub.NewClaudeAdapter(agenthub.ClaudeOptions{})
	at := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	rows := []agenthub.TurnUsageRow{
		// A legacy shape knows input and output, but no total or cache metrics.
		{SessionID: "s", CreatedAt: at, RawMeta: json.RawMessage(`{"claudeCode":{"inputTokens":5,"outputTokens":7}}`)},
		claudeRow("zero", "s", "", at, 0, 0, 0, 0, 0, "opus"),
		{SessionID: "s", CreatedAt: at, RawMeta: json.RawMessage(`{}`)},
	}
	rep := agenthub.BuildUsageReport(agenthub.AgentTypeClaude, adapter, rows)
	if rep.Totals.Turns != 3 || rep.Totals.TurnsWithoutData != 1 || rep.Totals.Total != 0 {
		t.Fatalf("totals = %+v", rep.Totals)
	}
	want := agenthub.UsageCoverage{FreshInput: 2, Output: 2, CacheRead: 1, CacheWrite: 1, Total: 1}
	if rep.Totals.Known != want || rep.ByDay[0].Known != want || rep.ByWorkspace[0].Known != want || rep.TopSessions[0].Known != want {
		t.Errorf("coverage lost in an aggregate: %+v", rep)
	}
	model := rep.ByModel[0]
	if model.Known != (agenthub.UsageCoverage{Total: 1}) || model.Total != 0 {
		t.Errorf("model total is known zero; other model metrics are unknown: %+v", model)
	}
}

func TestBuildUsageReportOfNothingHasEmptyListsNotNil(t *testing.T) {
	rep := agenthub.BuildUsageReport(agenthub.AgentTypeClaude, agenthub.NewClaudeAdapter(agenthub.ClaudeOptions{}), nil)
	if rep.Totals.Turns != 0 || rep.ByWorkspace == nil || rep.ByDay == nil || rep.ByModel == nil || rep.TopSessions == nil {
		t.Errorf("report = %+v", rep)
	}
}

func TestBuildUsageReportKeepsOnlyTheHeaviestSessions(t *testing.T) {
	adapter := agenthub.NewClaudeAdapter(agenthub.ClaudeOptions{})
	at := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	var rows []agenthub.TurnUsageRow
	for i := 1; i <= 15; i++ {
		rows = append(rows, claudeRow("t"+strconv.Itoa(i), "s"+strconv.Itoa(i), "w", at, i*100, 1, i*90, 0, 1, "opus"))
	}
	rep := agenthub.BuildUsageReport(agenthub.AgentTypeClaude, adapter, rows)
	if len(rep.TopSessions) != 10 || rep.TopSessions[0].SessionID != "s15" || rep.TopSessions[9].SessionID != "s6" {
		t.Errorf("top sessions = %+v", rep.TopSessions)
	}
}

// A split can list a helper model besides the main one, so it adds up to more than the turn total.
func TestBuildUsageReportCountsTurnsWhoseModelSplitIsNotTheTurnTotal(t *testing.T) {
	adapter := agenthub.NewClaudeAdapter(agenthub.ClaudeOptions{})
	at := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	withHelper := agenthub.TurnUsageRow{
		TurnID: "t1", SessionID: "s1", WorkspaceID: "w", CreatedAt: at,
		RawMeta: json.RawMessage(`{"quota":{"token_count":{"totalTokens":1000,"inputTokens":1,"outputTokens":1},"model_usage":[{"model":"opus","token_count":{"totalTokens":1000}},{"model":"haiku","token_count":{"totalTokens":9}}]}}`),
	}
	plain := claudeRow("t2", "s1", "w", at, 500, 1, 400, 0, 1, "opus")
	rep := agenthub.BuildUsageReport(agenthub.AgentTypeClaude, adapter, []agenthub.TurnUsageRow{withHelper, plain})
	if rep.PartialSplitTurns != 1 {
		t.Errorf("PartialSplitTurns = %d, want 1", rep.PartialSplitTurns)
	}
	if rep.Totals.Total != 1500 || len(rep.ByModel) != 2 || rep.ByModel[0].Key != "opus" || rep.ByModel[0].Total != 1500 || rep.ByModel[1].Key != "haiku" || rep.ByModel[1].Total != 9 {
		t.Errorf("totals = %+v by model = %+v", rep.Totals, rep.ByModel)
	}
}
```

- [x] **Step 2: Run it to verify it fails**

Run: `go test ./internal/agenthub/ -run TestBuildUsageReport -count=1`
Expected: FAIL to build, with `undefined: agenthub.TurnUsageRow` and `undefined: agenthub.BuildUsageReport`.

- [x] **Step 3: Write the implementation**

`internal/agenthub/usage_report.go`:

```go
package agenthub

import (
	"sort"
	"time"
)

// topSessionsLimit is how many sessions a report lists, heaviest first.
const topSessionsLimit = 10

// TurnUsageRow is one stored turn with the session facts a report groups by.
// It carries no prompt, no message and no title: only what the turn recorded
// about its own cost.
type TurnUsageRow struct {
	TurnID      string
	SessionID   string
	WorkspaceID string
	CreatedAt   time.Time
	UsageJSON   []byte
	RawMeta     []byte
}

// UsageCoverage counts known values, including genuine zeros, per metric.
// Counts are per turn. Model groups populate Total only.
type UsageCoverage struct {
	FreshInput int
	CacheRead  int
	CacheWrite int
	Output     int
	Total      int
}

// UsageTotals are sums plus coverage. Missing values never become measured zeros.
// A turn whose usage is wholly unknown adds to Turns and TurnsWithoutData only.
type UsageTotals struct {
	Known            UsageCoverage
	Turns            int
	TurnsWithoutData int
	FreshInput       int64
	CacheRead        int64
	CacheWrite       int64
	Output           int64
	Total            int64
}

// UsageGroup is the totals of one workspace, one day or one model. For a model
// only Turns, Total and Known.Total are filled: this report reads model totals only.
type UsageGroup struct {
	Key string
	UsageTotals
}

// UsageSessionGroup is the totals of one session.
type UsageSessionGroup struct {
	SessionID   string
	WorkspaceID string
	UsageTotals
}

// UsageReport is what a vendor's turns spent, in tokens. Tokens are not a share
// of a subscription limit: how a limit counts cached reads is not known.
type UsageReport struct {
	Vendor      AgentType
	Totals      UsageTotals
	ByWorkspace []UsageGroup
	ByDay       []UsageGroup
	ByModel     []UsageGroup
	TopSessions []UsageSessionGroup
	// PartialSplitTurns counts incomplete, unequal or uncomparable splits.
	// A mismatch alone does not establish why it differs.
	PartialSplitTurns int
}

func addMetric(sum *int64, known *int, value *int) {
	if value == nil {
		return
	}
	*sum += int64(*value)
	*known += 1
}

func (t *UsageTotals) add(u TurnUsage) {
	t.Turns++
	if !u.known() {
		t.TurnsWithoutData++
		return
	}
	addMetric(&t.FreshInput, &t.Known.FreshInput, u.FreshInput)
	addMetric(&t.CacheRead, &t.Known.CacheRead, u.CacheRead)
	addMetric(&t.CacheWrite, &t.Known.CacheWrite, u.CacheWrite)
	addMetric(&t.Output, &t.Known.Output, u.Output)
	addMetric(&t.Total, &t.Known.Total, u.Total)
}

type sessionTotals struct {
	workspace string
	UsageTotals
}

// BuildUsageReport groups rows by workspace, UTC day, model and session. Usage is
// read with DeriveTurnUsage, so rows stored with false zeros still report the
// numbers in their raw meta.
func BuildUsageReport(vendor AgentType, adapter VendorAdapter, rows []TurnUsageRow) UsageReport {
	rep := UsageReport{Vendor: vendor}
	byWorkspace := map[string]*UsageTotals{}
	byDay := map[string]*UsageTotals{}
	byModel := map[string]*UsageTotals{}
	bySession := map[string]*sessionTotals{}
	totalsOf := func(m map[string]*UsageTotals, key string) *UsageTotals {
		t, ok := m[key]
		if !ok {
			t = &UsageTotals{}
			m[key] = t
		}
		return t
	}
	for _, row := range rows {
		u := DeriveTurnUsage(adapter, row.UsageJSON, row.RawMeta)
		rep.Totals.add(u)
		totalsOf(byWorkspace, row.WorkspaceID).add(u)
		totalsOf(byDay, row.CreatedAt.UTC().Format("2006-01-02")).add(u)
		s, ok := bySession[row.SessionID]
		if !ok {
			s = &sessionTotals{workspace: row.WorkspaceID}
			bySession[row.SessionID] = s
		}
		s.add(u)
		if u.SplitPartial {
			rep.PartialSplitTurns++
		}
		for _, m := range u.Models {
			t := totalsOf(byModel, m.Model)
			t.Turns++
			t.Known.Total++
			t.Total += int64(m.Total)
		}
	}
	rep.ByWorkspace = groupsOf(byWorkspace, heaviestFirst)
	rep.ByModel = groupsOf(byModel, heaviestFirst)
	rep.ByDay = groupsOf(byDay, oldestFirst)
	rep.TopSessions = topSessions(bySession)
	return rep
}

func heaviestFirst(a, b UsageGroup) bool {
	if a.Total != b.Total {
		return a.Total > b.Total
	}
	return a.Key < b.Key
}

func oldestFirst(a, b UsageGroup) bool { return a.Key < b.Key }

func groupsOf(m map[string]*UsageTotals, less func(a, b UsageGroup) bool) []UsageGroup {
	out := make([]UsageGroup, 0, len(m))
	for key, t := range m {
		out = append(out, UsageGroup{Key: key, UsageTotals: *t})
	}
	sort.Slice(out, func(i, j int) bool { return less(out[i], out[j]) })
	return out
}

func topSessions(m map[string]*sessionTotals) []UsageSessionGroup {
	out := make([]UsageSessionGroup, 0, len(m))
	for id, s := range m {
		out = append(out, UsageSessionGroup{SessionID: id, WorkspaceID: s.workspace, UsageTotals: s.UsageTotals})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Total != out[j].Total {
			return out[i].Total > out[j].Total
		}
		return out[i].SessionID < out[j].SessionID
	})
	if len(out) > topSessionsLimit {
		out = out[:topSessionsLimit]
	}
	return out
}
```

- [x] **Step 4: Run the tests to verify they pass**

Run: `gofmt -l internal/agenthub && go vet ./internal/agenthub/ && go test ./internal/agenthub/ -run 'TestBuildUsageReport|TestDeriveTurnUsage' -count=1 -v`
Expected: `gofmt` and `go vet` print nothing; all matching usage tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/agenthub/usage_report.go internal/agenthub/usage_report_test.go
git commit -m "feat(agenthub): group what turns spent by workspace, day, model and session

A turn with no token data counts in Turns and TurnsWithoutData and adds
nothing else. The per-model split can include helper models that the turn
total leaves out, so the report counts those turns as PartialSplitTurns and
keeps model totals out of the overall total."
```

---

### Task 3: List the rows and serve the report

**Files:**
- Create: `internal/agenthub/usage_source.go`
- Test: `internal/agenthub/usage_source_test.go`

- [x] **Step 1: Write the failing test**

The helper `newUsageDB` inserts workspaces `w1` and `w2` first (trap 2).

`internal/agenthub/usage_source_test.go`:

```go
package agenthub_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/identity"
	"devkit/internal/storage"
)

// newUsageDB opens a migrated database holding workspaces w1 and w2: a session's
// workspace_id is a foreign key, so the rows must exist before a session names them.
func newUsageDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"w1", "w2"} {
		if _, err := db.Exec("INSERT INTO workspaces (id, name, root) VALUES (?, ?, '')", id, id); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func usageRowsFixture(t *testing.T) (agenthub.Store, time.Time) {
	t.Helper()
	store := agenthub.NewSQLStore(newUsageDB(t))
	ctx := context.Background()
	at := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

	claude := sampleSession("s-claude")
	claude.AgentType = agenthub.AgentTypeClaude
	claude.WorkspaceID = "w1"
	mock := sampleSession("s-mock")
	mock.WorkspaceID = "w2"
	for _, s := range []agenthub.AgentSession{claude, mock} {
		if err := store.InsertSession(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	raw := json.RawMessage(`{"quota":{"token_count":{"totalTokens":900,"inputTokens":4,"cachedInputTokens":800,"cachedWriteTokens":60,"outputTokens":36}}}`)
	turns := []agenthub.TurnRecord{
		{ID: "t1", SessionID: "s-claude", UsageJSON: json.RawMessage(`{"InputTokens":0,"OutputTokens":0}`), RawMeta: raw, CreatedAt: at},
		{ID: "t2", SessionID: "s-mock", UsageJSON: json.RawMessage(`{"InputTokens":1,"OutputTokens":1}`), CreatedAt: at},
	}
	for _, turn := range turns {
		if err := store.InsertTurn(ctx, turn); err != nil {
			t.Fatal(err)
		}
	}
	return store, at
}

func TestTurnUsageRowsListsOnlyTheVendorsTurnsWithTheirWorkspace(t *testing.T) {
	store, at := usageRowsFixture(t)
	source, ok := store.(agenthub.TurnUsageSource)
	if !ok {
		t.Fatal("the SQL store must be a TurnUsageSource")
	}
	rows, err := source.TurnUsageRows(context.Background(), agenthub.AgentTypeClaude)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].TurnID != "t1" || rows[0].SessionID != "s-claude" || rows[0].WorkspaceID != "w1" || !rows[0].CreatedAt.Equal(at) {
		t.Fatalf("rows = %+v", rows)
	}
	if len(rows[0].RawMeta) == 0 || len(rows[0].UsageJSON) == 0 {
		t.Errorf("the row must carry both stored parts: %+v", rows[0])
	}
}

func newUsageService(t *testing.T) *agenthub.Service {
	t.Helper()
	store := agenthub.NewSQLStore(newUsageDB(t))
	ctx := context.Background()
	s := sampleSession("s-claude")
	s.AgentType = agenthub.AgentTypeClaude
	s.WorkspaceID = "w1"
	if err := store.InsertSession(ctx, s); err != nil {
		t.Fatal(err)
	}
	raw := json.RawMessage(`{"quota":{"token_count":{"totalTokens":900,"inputTokens":4,"cachedInputTokens":800,"cachedWriteTokens":60,"outputTokens":36}}}`)
	if err := store.InsertTurn(ctx, agenthub.TurnRecord{ID: "t1", SessionID: "s-claude", UsageJSON: json.RawMessage(`{}`), RawMeta: raw, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	reg := agenthub.NewRegistry()
	reg.Register(agenthub.NewClaudeAdapter(agenthub.ClaudeOptions{}))
	return agenthub.NewService(reg, store, nil)
}

func TestServiceUsageReportReadsTheStoredTurns(t *testing.T) {
	svc := newUsageService(t)
	rep, err := svc.UsageReport(context.Background(), agenthub.AgentTypeClaude)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Totals.Turns != 1 || rep.Totals.Total != 900 || rep.Totals.CacheRead != 800 || len(rep.ByWorkspace) != 1 || rep.ByWorkspace[0].Key != "w1" {
		t.Errorf("report = %+v", rep)
	}
}

func TestServiceUsageReportIsTheWindowsAndNeedsAKnownVendor(t *testing.T) {
	svc := newUsageService(t)
	agent := identity.WithCaller(context.Background(), identity.Agent("11111111-1111-1111-1111-111111111111"))
	if _, err := svc.UsageReport(agent, agenthub.AgentTypeClaude); !errors.Is(err, agenthub.ErrPermissionDenied) {
		t.Errorf("an agent caller got %v, want ErrPermissionDenied", err)
	}
	if _, err := svc.UsageReport(identity.WithCaller(context.Background(), identity.UI()), agenthub.AgentTypeClaude); err != nil {
		t.Errorf("the window got %v", err)
	}
	if _, err := svc.UsageReport(context.Background(), agenthub.AgentType("nope")); !errors.Is(err, agenthub.ErrVendorUnknown) {
		t.Errorf("unknown vendor got %v, want ErrVendorUnknown", err)
	}
}
```

- [x] **Step 2: Run it to verify it fails**

Run: `go test ./internal/agenthub/ -run 'TestTurnUsageRows|TestServiceUsageReport' -count=1`
Expected: FAIL to build, with `undefined: agenthub.TurnUsageSource`.

- [x] **Step 3: Write the implementation**

`internal/agenthub/usage_source.go`:

```go
package agenthub

import (
	"context"
	"database/sql"
	"errors"
)

// ErrUsageUnavailable: the store cannot list what turns spent. Every shipped
// store can; this is for a custom one.
var ErrUsageUnavailable = errors.New("agenthub.usageUnavailable")

// TurnUsageSource is the part of a Store that lists what a vendor's turns spent.
// It is optional, like SessionLoader: the Service asks for it and reports
// ErrUsageUnavailable when a store has none.
type TurnUsageSource interface {
	TurnUsageRows(ctx context.Context, vendor AgentType) ([]TurnUsageRow, error)
}

var _ TurnUsageSource = (*SQLStore)(nil)

// TurnUsageRows lists every turn of a vendor's sessions, oldest first, with the
// session's workspace. It reads no message and no title.
func (st *SQLStore) TurnUsageRows(ctx context.Context, vendor AgentType) ([]TurnUsageRow, error) {
	rows, err := st.db.QueryContext(ctx, `
		SELECT t.id, t.session_id, COALESCE(s.workspace_id, ''), t.created_at, t.usage_json, t.raw_meta
		FROM `+tableAgentTurns+` t
		JOIN `+tableAgentSessions+` s ON s.id = t.session_id
		WHERE s.agent_type = ?
		ORDER BY t.created_at ASC, t.id ASC`, string(vendor))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]TurnUsageRow, 0)
	for rows.Next() {
		var r TurnUsageRow
		var usageJSON, rawMeta sql.NullString
		var createdAt string
		if err := rows.Scan(&r.TurnID, &r.SessionID, &r.WorkspaceID, &createdAt, &usageJSON, &rawMeta); err != nil {
			return nil, err
		}
		if usageJSON.Valid {
			r.UsageJSON = []byte(usageJSON.String)
		}
		if rawMeta.Valid {
			r.RawMeta = []byte(rawMeta.String)
		}
		if r.CreatedAt, err = parseStoreTime(createdAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UsageReport reports what a vendor's turns spent, in tokens. Window only, like
// every Settings read of the hub's own data.
func (s *Service) UsageReport(ctx context.Context, vendor AgentType) (UsageReport, error) {
	if err := requireWindow(ctx); err != nil {
		return UsageReport{}, err
	}
	adapter, ok := s.reg.Get(vendor)
	if !ok {
		return UsageReport{}, ErrVendorUnknown
	}
	source, ok := s.store.(TurnUsageSource)
	if !ok {
		return UsageReport{}, ErrUsageUnavailable
	}
	rows, err := source.TurnUsageRows(ctx, vendor)
	if err != nil {
		return UsageReport{}, err
	}
	return BuildUsageReport(vendor, adapter, rows), nil
}
```

- [x] **Step 4: Run the tests to verify they pass**

Run: `gofmt -l internal/agenthub && go vet ./internal/agenthub/ && go test ./internal/agenthub/ -run 'TestTurnUsageRows|TestServiceUsageReport|TestBuildUsageReport|TestDeriveTurnUsage' -count=1 -v`
Expected: all matching usage tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/agenthub/usage_source.go internal/agenthub/usage_source_test.go
git commit -m "feat(agenthub): list a vendor's turn usage and serve the report to the window

TurnUsageSource is an optional part of the store, like SessionLoader. The
Service refuses a caller that is not the window, as the account methods do."
```

---

### Task 4: Check the report against the real database

This is the done-criterion of the spec: the report must equal a plain SQL sum of the raw meta.

**Files:**
- Create: `internal/agenthub/usage_real_db_test.go`

- [x] **Step 1: Add the opt-in test**

It runs only when `DEVKIT_USAGE_DB` names a database file, and it must never point at the live file.

`internal/agenthub/usage_real_db_test.go`:

```go
package agenthub_test

import (
	"context"
	"os"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/storage"
)

// TestUsageReportOnARealDatabase prints the report of a copy of a real DevKit
// database, so its totals can be compared with a hand-written SQL sum. It runs
// only when DEVKIT_USAGE_DB names a database file; make a copy first
// (sqlite3 <db> ".backup /tmp/usage.db") and never point it at the live file.
func TestUsageReportOnARealDatabase(t *testing.T) {
	path := os.Getenv("DEVKIT_USAGE_DB")
	if path == "" {
		t.Skip("set DEVKIT_USAGE_DB to a copy of a DevKit database")
	}
	db, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	reg := agenthub.NewRegistry()
	reg.Register(agenthub.NewClaudeAdapter(agenthub.ClaudeOptions{}))
	svc := agenthub.NewService(reg, agenthub.NewSQLStore(db), nil)
	rep, err := svc.UsageReport(context.Background(), agenthub.AgentTypeClaude)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("turns=%d withoutData=%d total=%d cacheRead=%d cacheWrite=%d freshInput=%d output=%d",
		rep.Totals.Turns, rep.Totals.TurnsWithoutData, rep.Totals.Total, rep.Totals.CacheRead,
		rep.Totals.CacheWrite, rep.Totals.FreshInput, rep.Totals.Output)
	t.Logf("partialSplitTurns=%d", rep.PartialSplitTurns)
	for _, g := range rep.ByWorkspace {
		t.Logf("workspace %q turns=%d total=%d", g.Key, g.Turns, g.Total)
	}
	for _, g := range rep.ByModel {
		t.Logf("model %q turns=%d total=%d", g.Key, g.Turns, g.Total)
	}
}
```

- [x] **Step 2: Run it on a copy of your database**

```bash
sqlite3 ~/.config/DevKit/devkit.db ".backup /tmp/usage.db"
DEVKIT_USAGE_DB=/tmp/usage.db go test ./internal/agenthub/ -run TestUsageReportOnARealDatabase -count=1 -v
```

Expected on the author's database on 2026-10-26 (yours will differ, compare the two outputs of this step and the next, not these numbers):

```
usage_real_db_test.go:33: turns=512 withoutData=0 total=960683040 cacheRead=932811339 cacheWrite=24759563 freshInput=6906 output=3105232
usage_real_db_test.go:36: partialSplitTurns=95
```

- [x] **Step 3: Compare with a plain SQL sum**

```bash
sqlite3 /tmp/usage.db "select count(*), sum(json_extract(t.raw_meta,'\$.quota.token_count.totalTokens')), sum(json_extract(t.raw_meta,'\$.quota.token_count.cachedInputTokens')) from agent_turns t join agent_sessions s on s.id = t.session_id where s.agent_type = 'claude'"
```

Expected: the same turn count, total and cache-read numbers as step 2 (the author's database: `512|960683040|932811339`). If they differ, stop and find out why before going on.

- [ ] **Step 4: Commit**

```bash
git add internal/agenthub/usage_real_db_test.go
git commit -m "test(agenthub): print the usage report of a copy of a real database

Opt-in through DEVKIT_USAGE_DB, so the report's totals can be checked against
a hand-written SQL sum."
```

---

### Task 5: The window's bridge method and its grant

**Files:**
- Create: `internal/bridge/agenthub_usage.go`
- Test: `internal/bridge/agenthub_usage_test.go`
- Modify: `internal/app/container.go`, `internal/app/app_test.go`, `internal/bridge/testdata/legacy_ops_methods.json`

- [x] **Step 1: Write the failing tests**

`internal/bridge/agenthub_usage_test.go`:

```go
package bridge

import (
	"context"
	"testing"

	"devkit/internal/identity"
)

func TestAgentUsageReportIsUIOnlyAndNeverPermissionDeniedForTheWindow(t *testing.T) {
	svc := newBridgeWithAgentAccounts(t)
	agent := identity.WithCaller(context.Background(), identity.Agent("11111111-1111-1111-1111-111111111111"))
	terminalCaller := identity.WithCaller(context.Background(), identity.UserTerminal())

	for who, caller := range map[string]context.Context{"agent": agent, "terminal": terminalCaller} {
		_, err := svc.AgentUsageReport(caller, "mock")
		if err == nil {
			t.Errorf("a %s caller must be refused", who)
			continue
		}
		if publicCode(err) != "permission_denied" || err.Error() != "permission_denied: Permission denied." {
			t.Errorf("a %s caller got %v, want the window-only refusal", who, err)
		}
	}

	// The window: the real gateway must have the grant (a missing one is permission_denied).
	report, err := svc.AgentUsageReport(context.Background(), "mock")
	assertNotPermissionDenied(t, "window usage report", err)
	if err != nil {
		t.Fatal(err)
	}
	if report.Vendor != "mock" || report.Totals.Turns != 0 {
		t.Errorf("report = %+v", report)
	}
	// Lists reach the window as [], never null.
	if report.ByWorkspace == nil || report.ByDay == nil || report.ByModel == nil || report.TopSessions == nil {
		t.Errorf("a list is null: %+v", report)
	}
}

func TestAgentUsageReportRefusesAnInvalidVendorWithoutAnError(t *testing.T) {
	svc := newBridgeWithAgentAccounts(t)
	if _, err := svc.AgentUsageReport(context.Background(), "../etc"); err == nil {
		t.Error("a vendor that is not an opaque id must be refused")
	}
}
```

Append this test to `internal/app/app_test.go`:

`internal/app/app_test.go`:

```diff
--- a/internal/app/app_test.go
+++ b/internal/app/app_test.go
@@ -265,3 +265,15 @@ func TestNewContainerGrantsAgentAccounts(t *testing.T) {
 		}
 	}
 }
+
+func TestNewContainerGrantsAgentUsageReport(t *testing.T) {
+	container := NewContainer()
+	grant := permissions.Grant{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "usage:report"}
+	if !container.Permissions.Check(permissions.Request(grant)).Allowed {
+		t.Errorf("%s %s on %s was denied", grant.SubjectID, grant.Capability, grant.Scope)
+	}
+	write := permissions.Grant{SubjectID: "builtin.agenthub", Capability: "agenthub:execute", Scope: "usage:report"}
+	if container.Permissions.Check(permissions.Request(write)).Allowed {
+		t.Errorf("the usage report must be a read: %s on %s was allowed", write.Capability, write.Scope)
+	}
+}
```

- [x] **Step 2: Run them to verify they fail**

Run: `go test ./internal/bridge/ -run TestAgentUsageReport -count=1 && go test ./internal/app/ -run TestNewContainerGrantsAgentUsageReport -count=1`
Expected: the bridge package FAILs to build with `svc.AgentUsageReport undefined`; the app test FAILs with `... was denied`.

- [x] **Step 3: Write the bridge method**

`internal/bridge/agenthub_usage.go`:

```go
package bridge

import (
	"context"

	"devkit/internal/agenthub"
)

// Token usage (Settings → Agents → Accounts & limits). The window's alone, like the
// account methods: no MCP tool uses the usage:* scope, the call refuses a caller that
// is not the window, and the Service refuses again. It answers with numbers and ids
// only: no prompt, no message, no title, no path. Audit metadata carries the vendor id.

// AgentUsageTotalsInfo is a sum of tokens. A turn with no token data counts in
// Turns and TurnsWithoutData and adds nothing else.
type AgentUsageTotalsInfo struct {
	Known            map[string]int `json:"known"`
	Turns            int            `json:"turns"`
	TurnsWithoutData int            `json:"turnsWithoutData"`
	FreshInput       int64          `json:"freshInput"`
	CacheRead        int64          `json:"cacheRead"`
	CacheWrite       int64          `json:"cacheWrite"`
	Output           int64          `json:"output"`
	Total            int64          `json:"total"`
}

// AgentUsageGroupInfo is one workspace id, UTC day (YYYY-MM-DD) or model.
type AgentUsageGroupInfo struct {
	Key    string               `json:"key"`
	Totals AgentUsageTotalsInfo `json:"totals"`
}

// AgentUsageSessionInfo is one session's totals.
type AgentUsageSessionInfo struct {
	SessionID   string               `json:"sessionId"`
	WorkspaceID string               `json:"workspaceId"`
	Totals      AgentUsageTotalsInfo `json:"totals"`
}

// AgentUsageReportInfo is what a vendor's turns spent, in tokens. It is not a share
// of a subscription limit. Every list is non-null. Workspace ids are named by the
// window from the workspace list it already has.
type AgentUsageReportInfo struct {
	Vendor            string                  `json:"vendor"`
	Totals            AgentUsageTotalsInfo    `json:"totals"`
	ByWorkspace       []AgentUsageGroupInfo   `json:"byWorkspace"`
	ByDay             []AgentUsageGroupInfo   `json:"byDay"`
	ByModel           []AgentUsageGroupInfo   `json:"byModel"`
	TopSessions       []AgentUsageSessionInfo `json:"topSessions"`
	PartialSplitTurns int                     `json:"partialSplitTurns"`
}

func usageTotalsInfo(t agenthub.UsageTotals) AgentUsageTotalsInfo {
	return AgentUsageTotalsInfo{
		Known: map[string]int{
			"freshInput": t.Known.FreshInput, "cacheRead": t.Known.CacheRead,
			"cacheWrite": t.Known.CacheWrite, "output": t.Known.Output, "total": t.Known.Total,
		},
		Turns: t.Turns, TurnsWithoutData: t.TurnsWithoutData, FreshInput: t.FreshInput,
		CacheRead: t.CacheRead, CacheWrite: t.CacheWrite, Output: t.Output, Total: t.Total,
	}
}

func usageGroupsInfo(groups []agenthub.UsageGroup) []AgentUsageGroupInfo {
	out := make([]AgentUsageGroupInfo, 0, len(groups))
	for _, g := range groups {
		out = append(out, AgentUsageGroupInfo{Key: g.Key, Totals: usageTotalsInfo(g.UsageTotals)})
	}
	return out
}

func usageReportInfo(r agenthub.UsageReport) AgentUsageReportInfo {
	sessions := make([]AgentUsageSessionInfo, 0, len(r.TopSessions))
	for _, s := range r.TopSessions {
		sessions = append(sessions, AgentUsageSessionInfo{SessionID: s.SessionID, WorkspaceID: s.WorkspaceID, Totals: usageTotalsInfo(s.UsageTotals)})
	}
	return AgentUsageReportInfo{
		Vendor:            string(r.Vendor),
		Totals:            usageTotalsInfo(r.Totals),
		ByWorkspace:       usageGroupsInfo(r.ByWorkspace),
		ByDay:             usageGroupsInfo(r.ByDay),
		ByModel:           usageGroupsInfo(r.ByModel),
		TopSessions:       sessions,
		PartialSplitTurns: r.PartialSplitTurns,
	}
}

// AgentUsageReport reports what a vendor's turns spent, in tokens.
func (s *AppService) AgentUsageReport(ctx context.Context, vendor string) (AgentUsageReportInfo, error) {
	v := agentRuntimeID(vendor)
	result, err := s.agentAccountsCall(ctx, "agenthub:read", "usage:report", map[string]any{"action": "agent_usage_report", "vendor": v}, func(c context.Context, svc *agenthub.Service) (any, error) {
		report, err := svc.UsageReport(c, agenthub.AgentType(v))
		if err != nil {
			return nil, err
		}
		return usageReportInfo(report), nil
	})
	if err != nil {
		return AgentUsageReportInfo{}, err
	}
	return result.(AgentUsageReportInfo), nil
}
```

- [x] **Step 4: Add the read grant**

`internal/app/container.go`:

```diff
--- a/internal/app/container.go
+++ b/internal/app/container.go
@@ -367,6 +367,8 @@ func NewContainerWithStores(
 		// uses these scopes and every bridge method refuses a caller that is not the window.
 		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "account:*"},
 		{SubjectID: "builtin.agenthub", Capability: "agenthub:execute", Scope: "account:*"},
+		// Token usage report (same page). A read of numbers and ids; UI only, no MCP tool uses usage:*.
+		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "usage:*"},
 		{SubjectID: "builtin.agenthub", Capability: "terminal:io", Scope: "session:*"},
 		{SubjectID: "builtin.vault", Capability: "vault:manage", Scope: "vault"},
 		{SubjectID: "builtin.search", Capability: "vault:search", Scope: "item:search"},
```

- [x] **Step 5: List the method (trap 3)**

`internal/bridge/testdata/legacy_ops_methods.json`:

```diff
--- a/internal/bridge/testdata/legacy_ops_methods.json
+++ b/internal/bridge/testdata/legacy_ops_methods.json
@@ -105,6 +105,7 @@
   "AgentRuntimeRemove",
   "AgentRuntimeSetClaudeBinary",
   "AgentRuntimeStatus",
+  "AgentUsageReport",
   "ApiCancelSend",
   "ApiClearHistory",
   "ApiCollections",
```

- [x] **Step 6: Run the tests to verify they pass**

Run: `gofmt -l internal/bridge internal/app && go vet ./internal/bridge/ ./internal/app/ && go test ./internal/bridge/ ./internal/app/ ./internal/arch/ -count=1`
Expected: `gofmt` and `go vet` print nothing; three lines starting with `ok`.

- [ ] **Step 7: Commit**

```bash
git add internal/bridge/agenthub_usage.go internal/bridge/agenthub_usage_test.go internal/bridge/testdata/legacy_ops_methods.json internal/app/container.go internal/app/app_test.go
git commit -m "feat(bridge): AgentUsageReport, the window's read of token usage

UI-only like the account methods (no MCP tool uses usage:*), numbers and ids
only. Workspace ids are named by the window from the list it already has."
```

---

### Task 6: Regenerate the bindings

**Files:**
- Modify (generated): `frontend/bindings/devkit/internal/bridge/appservice.ts`, `index.ts`, `models.ts`

- [x] **Step 1: Generate (trap 4)**

Run: `wails3 generate bindings -clean=true -ts -i`
Expected: an `INFO Processed: ... 1 Service, 4xx Methods ...` line and no error.

- [x] **Step 2: Check the diff is only the new method and its four types**

Run: `git status --short frontend/bindings && git diff --stat -- frontend/bindings`
Expected: exactly the three files above are modified, the new method, DTOs and per-metric coverage map, with no unrelated changes. `appservice.ts` gains `AgentUsageReport(vendor: string)`; `models.ts` gains `AgentUsageGroupInfo`, `AgentUsageReportInfo`, `AgentUsageSessionInfo`, `AgentUsageTotalsInfo`, with the list fields typed `T[] | null`.

- [ ] **Step 3: Commit**

```bash
git add frontend/bindings
git commit -m "chore(bindings): regenerate for AgentUsageReport"
```

---

### Task 7: The typed wrapper and the pure model

**Files:**
- Create: `frontend/src/bridge/agentUsage.ts`, `frontend/src/modules/agenthub/usage/usageModel.ts`
- Test: `frontend/src/bridge/agentUsage.test.ts`, `frontend/src/modules/agenthub/usage/usageModel.test.ts`

- [x] **Step 1: Write the failing tests**

`frontend/src/modules/agenthub/usage/usageModel.test.ts`:

```ts
import { describe, expect, test } from "bun:test";
import type { UsageGroup, UsageReport, UsageTotals } from "@/bridge/agentUsage";
import {
  QUOTA_NOTE,
  cacheReadShare,
  completeTotal,
  dataNotes,
  formatMetric,
  formatTokens,
  recentDays,
  rowsOf,
  sessionRowsOf,
  workspaceName,
} from "./usageModel";

const totals = (over: Partial<UsageTotals> = {}): UsageTotals => ({
  turns: 0,
  turnsWithoutData: 0,
  freshInput: 0,
  cacheRead: 0,
  cacheWrite: 0,
  output: 0,
  total: 0,
  known: {
    freshInput: over.turns ?? 0, cacheRead: over.turns ?? 0,
    cacheWrite: over.turns ?? 0, output: over.turns ?? 0, total: over.turns ?? 0,
  },
  ...over,
});
const group = (key: string, over: Partial<UsageTotals> = {}): UsageGroup => ({ key, totals: totals(over) });
const report = (over: Partial<UsageReport> = {}): UsageReport => ({
  vendor: "claude",
  totals: totals(),
  byWorkspace: [],
  byDay: [],
  byModel: [],
  topSessions: [],
  partialSplitTurns: 0,
  ...over,
});

describe("formatTokens", () => {
  test("scales to K, M and B with one or two decimals", () => {
    expect(formatTokens(0)).toBe("0");
    expect(formatTokens(999)).toBe("999");
    expect(formatTokens(1_500)).toBe("1.5K");
    expect(formatTokens(960_683_040)).toBe("960.7M");
    expect(formatTokens(2_340_000_000)).toBe("2.34B");
  });
});

describe("cacheReadShare", () => {
  test("is the share of cache reads in the total, one decimal", () => {
    expect(cacheReadShare(totals({ turns: 1, total: 960_683_040, cacheRead: 932_811_339 }))).toBe("97.1%");
  });
  test("is a dash when there is nothing to divide", () => {
    expect(cacheReadShare(totals())).toBe("—");
  });
});

describe("metric coverage", () => {
  const known = { freshInput: 2, cacheRead: 1, cacheWrite: 0, output: 2, total: 1 };
  test("distinguishes missing metrics, genuine zero and partial sums", () => {
    const t = totals({ turns: 2, freshInput: 5, total: 0, known });
    expect(formatMetric(t, "cacheWrite")).toBe("—");
    expect(formatMetric(t, "total")).toBe("0 (partial)");
    expect(formatMetric(t, "freshInput")).toBe("5");
    expect(formatMetric(totals({ turns: 1 }), "total")).toBe("0");
    expect(completeTotal(t)).toBeNull();
  });
  test("suppresses ratios with incomplete numerator or denominator coverage", () => {
    const t = totals({ turns: 2, total: 100, cacheRead: 50, known });
    expect(cacheReadShare(t)).toBe("—");
    expect(cacheReadShare({ ...t, known: { ...known, total: 2 } })).toBe("—");
    expect(rowsOf([{ key: "w", totals: t }], (key) => key, 100)[0].share).toBe("—");
    expect(rowsOf([group("w", { turns: 1, total: 100 })], (key) => key, null)[0].share).toBe("—");
  });
  test("model rows report fresh input, caches and output as unknown", () => {
    const row = rowsOf([group("opus", { turns: 1, total: 100,
      known: { freshInput: 0, cacheRead: 0, cacheWrite: 0, output: 0, total: 1 } })], (key) => key, 100)[0];
    expect(row).toMatchObject({ total: "100", freshInput: "—", cacheRead: "—", cacheWrite: "—", output: "—" });
  });
});

describe("workspaceName", () => {
  test("names a known workspace, no workspace, and a removed one", () => {
    const names = { w1: "fsap-security" };
    expect(workspaceName("w1", names)).toBe("fsap-security");
    expect(workspaceName("", names)).toBe("No workspace");
    expect(workspaceName("gone", names)).toBe("Removed workspace");
  });
});

describe("rowsOf", () => {
  test("keeps the order given and adds each row's share of the grand total", () => {
    const rows = rowsOf(
      [group("a", { turns: 3, total: 800, cacheRead: 700, cacheWrite: 50, output: 10 }), group("b", { turns: 1, total: 200 })],
      (key) => key.toUpperCase(),
      1000,
    );
    expect(rows.map((r) => r.name)).toEqual(["A", "B"]);
    expect(rows[0]).toMatchObject({ key: "a", turns: 3, total: "800", cacheRead: "700", cacheWrite: "50", output: "10", share: "80.0%" });
    expect(rows[1].share).toBe("20.0%");
  });
  test("has a dash for the share when the grand total is 0", () => {
    expect(rowsOf([group("a")], (k) => k, 0)[0].share).toBe("—");
  });
});

describe("sessionRowsOf", () => {
  test("names a session by a short id and its workspace", () => {
    const rows = sessionRowsOf(
      [{ sessionId: "11111111-2222-3333-4444-555555555555", workspaceId: "w1", totals: totals({ turns: 9, total: 250 }) }],
      { w1: "fsap-security" },
      1000,
    );
    expect(rows[0].name).toBe("11111111 · fsap-security");
    expect(rows[0]).toMatchObject({ turns: 9, total: "250", share: "25.0%" });
  });
});

describe("recentDays", () => {
  test("lists the newest days first and no more than asked", () => {
    const days = [group("2026-10-24"), group("2026-10-25"), group("2026-10-26")];
    expect(recentDays(days, 2).map((g) => g.key)).toEqual(["2026-10-26", "2026-10-25"]);
  });
});

describe("dataNotes", () => {
  test("says nothing when every turn has data and every split matches", () => {
    expect(dataNotes(report({ totals: totals({ turns: 5 }) }))).toEqual([]);
  });
  test("says how many turns have no data and how many splits differ", () => {
    const notes = dataNotes(report({ totals: totals({ turns: 512, turnsWithoutData: 4 }), partialSplitTurns: 95 }));
    expect(notes).toHaveLength(2);
    expect(notes[0]).toContain("4 of 512 turns have no token data");
    expect(notes[1]).toContain("95 turns");
    expect(notes[1]).toContain("incomplete or differs");
    expect(notes[1]).not.toContain("larger than");
    expect(notes[1]).not.toContain("helper models");
  });
});

describe("QUOTA_NOTE", () => {
  test("says tokens are not a share of the subscription limit", () => {
    expect(QUOTA_NOTE).toContain("not a share of your subscription limit");
    expect(QUOTA_NOTE).toContain("not known");
  });
});
```

`frontend/src/bridge/agentUsage.test.ts`:

```ts
import { describe, expect, test } from "bun:test";
import type { AgentUsageReportInfo, AgentUsageTotalsInfo } from "../../bindings/devkit/internal/bridge/models";
import { reportOf } from "./agentUsage";

const totals = (over: Partial<AgentUsageTotalsInfo> = {}): AgentUsageTotalsInfo => ({
  turns: 2,
  turnsWithoutData: 1,
  freshInput: 3,
  cacheRead: 4,
  cacheWrite: 5,
  output: 6,
  total: 18,
  known: { freshInput: 1, cacheRead: 1, cacheWrite: 1, output: 1, total: 1 },
  ...over,
});

describe("reportOf", () => {
  test("keeps what Go sent", () => {
    const raw: AgentUsageReportInfo = {
      vendor: "claude",
      totals: totals(),
      byWorkspace: [{ key: "w1", totals: totals({ total: 10 }) }],
      byDay: [{ key: "2026-10-26", totals: totals() }],
      byModel: [{ key: "opus", totals: totals({ turns: 1 }) }],
      topSessions: [{ sessionId: "s1", workspaceId: "w1", totals: totals() }],
      partialSplitTurns: 7,
    };
    const report = reportOf(raw);
    expect(report.vendor).toBe("claude");
    expect(report.totals).toEqual({ turns: 2, turnsWithoutData: 1, freshInput: 3, cacheRead: 4, cacheWrite: 5, output: 6, total: 18,
      known: { freshInput: 1, cacheRead: 1, cacheWrite: 1, output: 1, total: 1 } });
    expect(report.byWorkspace[0]).toEqual({ key: "w1", totals: expect.objectContaining({ total: 10 }) });
    expect(report.topSessions[0]).toMatchObject({ sessionId: "s1", workspaceId: "w1" });
    expect(report.partialSplitTurns).toBe(7);
  });

  test("turns the bindings' null lists into empty ones", () => {
    const raw: AgentUsageReportInfo = {
      vendor: "codex",
      totals: totals({ turns: 0, turnsWithoutData: 0 }),
      byWorkspace: null,
      byDay: null,
      byModel: null,
      topSessions: null,
      partialSplitTurns: 0,
    };
    const report = reportOf(raw);
    expect(report.byWorkspace).toEqual([]);
    expect(report.byDay).toEqual([]);
    expect(report.byModel).toEqual([]);
    expect(report.topSessions).toEqual([]);
  });
});
```

- [x] **Step 2: Run them to verify they fail**

Run: `cd frontend && bun test src/modules/agenthub/usage/usageModel.test.ts src/bridge/agentUsage.test.ts`
Expected: FAIL with an error that the module `./usageModel` (and `./agentUsage`) cannot be found.

- [x] **Step 3: Write the wrapper**

`frontend/src/bridge/agentUsage.ts`:

```ts
import { AppService } from "../../bindings/devkit/internal/bridge";
import type {
  AgentUsageGroupInfo as RawGroup,
  AgentUsageReportInfo as RawReport,
  AgentUsageSessionInfo as RawSession,
  AgentUsageTotalsInfo as RawTotals,
} from "../../bindings/devkit/internal/bridge/models";
import { callBridge } from "./client";

/**
 * What a vendor's turns spent, in tokens (Settings → Agents → Accounts & limits).
 * The window's own call: there is no MCP tool for it and the backend refuses an
 * agent caller. It carries numbers and ids only. Tokens are not a share of a
 * subscription limit: how a limit counts cached reads is not known.
 */

export type UsageMetric = "freshInput" | "cacheRead" | "cacheWrite" | "output" | "total";

/** Sums plus counts of known values; a zero sum without coverage is not measured zero. */
export type UsageTotals = {
  known: Record<UsageMetric, number>;
  turns: number;
  turnsWithoutData: number;
  freshInput: number;
  cacheRead: number;
  cacheWrite: number;
  output: number;
  total: number;
};

/** One workspace id (`""` = none), UTC day (`YYYY-MM-DD`) or model. A model group fills `turns` and `total` only. */
export type UsageGroup = { key: string; totals: UsageTotals };

export type UsageSession = { sessionId: string; workspaceId: string; totals: UsageTotals };

export type UsageReport = {
  vendor: string;
  totals: UsageTotals;
  byWorkspace: UsageGroup[];
  /** Oldest first. */
  byDay: UsageGroup[];
  byModel: UsageGroup[];
  /** Heaviest first. */
  topSessions: UsageSession[];
  /** Turns whose model split is incomplete, differs, or cannot be compared with a known turn total. */
  partialSplitTurns: number;
};

const totalsOf = (raw: RawTotals): UsageTotals => ({
  known: {
    freshInput: raw.known?.["freshInput"] ?? 0,
    cacheRead: raw.known?.["cacheRead"] ?? 0,
    cacheWrite: raw.known?.["cacheWrite"] ?? 0,
    output: raw.known?.["output"] ?? 0,
    total: raw.known?.["total"] ?? 0,
  },
  turns: raw.turns,
  turnsWithoutData: raw.turnsWithoutData,
  freshInput: raw.freshInput,
  cacheRead: raw.cacheRead,
  cacheWrite: raw.cacheWrite,
  output: raw.output,
  total: raw.total,
});

const groupOf = (raw: RawGroup): UsageGroup => ({ key: raw.key, totals: totalsOf(raw.totals) });

const sessionOf = (raw: RawSession): UsageSession => ({
  sessionId: raw.sessionId,
  workspaceId: raw.workspaceId,
  totals: totalsOf(raw.totals),
});

/** The bindings type every list `T[] | null`; the window wants `[]`. */
export function reportOf(raw: RawReport): UsageReport {
  return {
    vendor: raw.vendor,
    totals: totalsOf(raw.totals),
    byWorkspace: (raw.byWorkspace ?? []).map(groupOf),
    byDay: (raw.byDay ?? []).map(groupOf),
    byModel: (raw.byModel ?? []).map(groupOf),
    topSessions: (raw.topSessions ?? []).map(sessionOf),
    partialSplitTurns: raw.partialSplitTurns,
  };
}

export function agentUsageReport(vendor: string): Promise<UsageReport> {
  return callBridge(async () => reportOf(await AppService.AgentUsageReport(vendor)));
}
```

- [x] **Step 4: Write the model**

`frontend/src/modules/agenthub/usage/usageModel.ts`:

```ts
import type { UsageGroup, UsageMetric, UsageReport, UsageSession, UsageTotals } from "@/bridge/agentUsage";

/** Shown with every report: a token count is not the subscription's own measure. */
export const QUOTA_NOTE =
  "These are tokens, not a share of your subscription limit. How the limit counts cached reads is not known, so fewer tokens does not by itself mean a smaller share.";

const DASH = "—";

export function formatTokens(n: number): string {
  if (n < 1_000) return String(n);
  if (n < 1_000_000) return `${(n / 1_000).toFixed(1)}K`;
  if (n < 1_000_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  return `${(n / 1_000_000_000).toFixed(2)}B`;
}

function percent(part: number, whole: number): string {
  return whole > 0 ? `${((part / whole) * 100).toFixed(1)}%` : DASH;
}

export function formatMetric(totals: UsageTotals, metric: UsageMetric): string {
  const known = totals.known[metric];
  if (known === 0) return DASH;
  return `${formatTokens(totals[metric])}${known < totals.turns ? " (partial)" : ""}`;
}

/** A usable denominator only when every turn reported its total. */
export function completeTotal(totals: UsageTotals): number | null {
  return totals.turns > 0 && totals.known.total === totals.turns ? totals.total : null;
}

/** Only divide metrics covering the same complete set of turns. */
export function cacheReadShare(totals: UsageTotals): string {
  const total = completeTotal(totals);
  return total !== null && totals.known.cacheRead === totals.turns
    ? percent(totals.cacheRead, total) : DASH;
}

/** A workspace id as the window names it: `""` is no workspace, an id it does not know was removed. */
export function workspaceName(id: string, names: Record<string, string>): string {
  if (id === "") return "No workspace";
  return names[id] ?? "Removed workspace";
}

export type UsageRow = {
  key: string;
  name: string;
  turns: number;
  freshInput: string;
  total: string;
  cacheRead: string;
  cacheWrite: string;
  output: string;
  /** This row's share of the grand total, or a dash. */
  share: string;
};

/** Rows in the order given, each with its share of `grandTotal`. */
export function rowsOf(groups: UsageGroup[], name: (key: string) => string, grandTotal: number | null): UsageRow[] {
  return groups.map((group) => ({
    key: group.key,
    name: name(group.key),
    turns: group.totals.turns,
    freshInput: formatMetric(group.totals, "freshInput"),
    total: formatMetric(group.totals, "total"),
    cacheRead: formatMetric(group.totals, "cacheRead"),
    cacheWrite: formatMetric(group.totals, "cacheWrite"),
    output: formatMetric(group.totals, "output"),
    share: grandTotal !== null && completeTotal(group.totals) !== null
      ? percent(group.totals.total, grandTotal) : DASH,
  }));
}

const SHORT_ID = 8;

/** Heaviest-session rows: a short id and the workspace's name, never a title. */
export function sessionRowsOf(sessions: UsageSession[], names: Record<string, string>, grandTotal: number | null): UsageRow[] {
  return sessions.map((session) => ({
    ...rowsOf([{ key: session.sessionId, totals: session.totals }], () => "", grandTotal)[0],
    name: `${session.sessionId.slice(0, SHORT_ID)} · ${workspaceName(session.workspaceId, names)}`,
  }));
}

/** The newest `limit` days, newest first (the report lists them oldest first). */
export function recentDays(days: UsageGroup[], limit: number): UsageGroup[] {
  return days.slice(-limit).reverse();
}

/** What a reader must know to read the numbers right; empty when nothing is off. */
export function dataNotes(report: UsageReport): string[] {
  const notes: string[] = [];
  const { turns, turnsWithoutData } = report.totals;
  if (turnsWithoutData > 0) {
    notes.push(`${turnsWithoutData} of ${turns} turns have no token data and are not counted.`);
  }
  if (report.partialSplitTurns > 0) {
    notes.push(
      `On ${report.partialSplitTurns} turns the per-model split is incomplete or differs from the turn total. Model totals may not add up to the overall total.`,
    );
  }
  return notes;
}
```

- [x] **Step 5: Run the tests to verify they pass**

Run: `cd frontend && bun test src/modules/agenthub/usage/usageModel.test.ts src/bridge/agentUsage.test.ts`
Expected: all model and wrapper tests pass, including coverage and fresh-input cases.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/bridge/agentUsage.ts frontend/src/bridge/agentUsage.test.ts frontend/src/modules/agenthub/usage/usageModel.ts frontend/src/modules/agenthub/usage/usageModel.test.ts
git commit -m "feat(frontend): token usage wrapper and formatting model

The bindings type every list T[] | null; the wrapper gives the window []. The
note that tokens are not a share of the subscription limit is a constant the
table always shows."
```

---

### Task 8: The report table

**Files:**
- Create: `frontend/src/modules/agenthub/usage/UsageReportTable.tsx`
- Test: `frontend/src/modules/agenthub/usage/UsageReportTable.test.tsx`

- [x] **Step 1: Write the failing test**

`frontend/src/modules/agenthub/usage/UsageReportTable.test.tsx`:

```tsx
import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import type { UsageGroup, UsageReport, UsageTotals } from "@/bridge/agentUsage";
import { QUOTA_NOTE } from "./usageModel";
import { UsageReportTable } from "./UsageReportTable";

const totals = (over: Partial<UsageTotals> = {}): UsageTotals => ({
  turns: 0,
  turnsWithoutData: 0,
  freshInput: 0,
  cacheRead: 0,
  cacheWrite: 0,
  output: 0,
  total: 0,
  known: {
    freshInput: over.turns ?? 0, cacheRead: over.turns ?? 0,
    cacheWrite: over.turns ?? 0, output: over.turns ?? 0, total: over.turns ?? 0,
  },
  ...over,
});
const group = (key: string, over: Partial<UsageTotals> = {}): UsageGroup => ({ key, totals: totals(over) });

const report = (over: Partial<UsageReport> = {}): UsageReport => ({
  vendor: "claude",
  totals: totals({ turns: 512, total: 960_683_040, cacheRead: 932_811_339, cacheWrite: 24_759_563, output: 3_105_232 }),
  byWorkspace: [
    group("w1", { turns: 427, total: 824_519_570, cacheRead: 800_000_000 }),
    group("", { turns: 3, total: 3_583_422 }),
    group("gone", { turns: 1, total: 791_005 }),
  ],
  byDay: [group("2026-10-25", { turns: 1, total: 1_000 }), group("2026-10-26", { turns: 16, total: 5_000_000 })],
  byModel: [group("claude-opus-5-5", { turns: 326, total: 637_349_761,
    known: { freshInput: 0, cacheRead: 0, cacheWrite: 0, output: 0, total: 326 } })],
  topSessions: [{ sessionId: "11111111-2222-3333-4444-555555555555", workspaceId: "w1", totals: totals({ turns: 9, total: 90_000_000 }) }],
  partialSplitTurns: 0,
  ...over,
});

const render = (over: { report?: UsageReport | undefined; loadError?: string } = {}) =>
  renderToStaticMarkup(
    <UsageReportTable
      vendorName="Claude"
      report={"report" in over ? over.report : report()}
      loadError={over.loadError ?? ""}
      workspaceNames={{ w1: "fsap-security" }}
    />,
  );

describe("UsageReportTable", () => {
  test("always says tokens are not a share of the subscription limit", () => {
    expect(render()).toContain(QUOTA_NOTE);
    expect(render({ report: undefined })).toContain(QUOTA_NOTE);
  });

  test("gives the headline: tokens, turns and the cache read share", () => {
    const html = render();
    expect(html).toContain("960.7M tokens over 512 turns");
    expect(html).toContain("97.1% cache reads");
  });

  test("renders fresh input and does not turn unknown totals into a zero headline", () => {
    const t = totals({ turns: 1, freshInput: 5, output: 7,
      known: { freshInput: 1, output: 1, total: 0, cacheRead: 0, cacheWrite: 0 } });
    const html = render({ report: report({ totals: t, byWorkspace: [group("w1", t)], byModel: [] }) });
    expect(html).toContain("Fresh input");
    expect(html).toContain(">5<");
    expect(html).toContain("— tokens over 1 turns");
    expect(html).not.toContain("0 tokens over");
    expect(html).toContain("— cache reads");
    const partial = render({ report: report({ totals: { ...t, turns: 2, total: 100,
      known: { ...t.known, total: 1 } } }) });
    expect(partial).toContain("100 (partial) tokens over 2 turns");
  });

  test("names each workspace by the window's own list", () => {
    const html = render();
    expect(html).toContain("fsap-security");
    expect(html).toContain("No workspace");
    expect(html).toContain("Removed workspace");
    expect(html).not.toContain(">w1<");
  });

  test("lists days newest first and models by name", () => {
    const html = render();
    expect(html.indexOf("2026-10-26")).toBeLessThan(html.indexOf("2026-10-25"));
    expect(html).toContain("claude-opus-5-5");
  });

  test("shows a session by a short id and its workspace, never a title", () => {
    const html = render();
    expect(html).toContain("11111111");
    expect(html).not.toContain("11111111-2222-3333-4444-555555555555");
  });

  test("says when turns have no data and when a split differs", () => {
    const html = render({ report: report({ totals: totals({ turns: 512, turnsWithoutData: 4, total: 10, cacheRead: 5 }), partialSplitTurns: 95 }) });
    expect(html).toContain("4 of 512 turns have no token data");
    expect(html).toContain("95 turns");
  });

  test("says there is no data yet when no turn has any", () => {
    const html = render({ report: report({ totals: totals({ turns: 3, turnsWithoutData: 3 }), byWorkspace: [], byDay: [], byModel: [], topSessions: [] }) });
    expect(html).toContain("No token data yet for Claude.");
    expect(html).not.toContain("tokens over");
  });

  test("shows a skeleton while loading and the error when the load failed", () => {
    expect(render({ report: undefined })).toContain("animate-pulse");
    const failed = render({ report: undefined, loadError: "Could not read usage." });
    expect(failed).toContain("Could not read usage.");
    expect(failed).toContain('role="alert"');
  });
});
```

- [x] **Step 2: Run it to verify it fails**

Run: `cd frontend && bun test src/modules/agenthub/usage/UsageReportTable.test.tsx`
Expected: FAIL with an error that `./UsageReportTable` cannot be found.

- [x] **Step 3: Write the component (trap 5: the shared `DataTable`)**

`frontend/src/modules/agenthub/usage/UsageReportTable.tsx`:

```tsx
import { DataTable, type DataTableColumn } from "@/components/ui/data-table/DataTable";
import { Skeleton } from "@/components/ui/skeleton";
import type { UsageReport } from "@/bridge/agentUsage";
import {
  QUOTA_NOTE,
  cacheReadShare,
  completeTotal,
  dataNotes,
  formatMetric,
  recentDays,
  rowsOf,
  sessionRowsOf,
  workspaceName,
  type UsageRow,
} from "./usageModel";

const DAYS_SHOWN = 14;

export type UsageReportTableProps = {
  vendorName: string;
  /** Undefined while the first read is on its way. */
  report: UsageReport | undefined;
  loadError: string;
  /** Workspace id → name, from the workspace list the window already has. */
  workspaceNames: Record<string, string>;
};

/**
 * Settings → Agents → Accounts & limits, token usage for one vendor. It only draws:
 * the page reads the report and the workspace names and passes them in. It shows
 * tokens and says they are not a share of the subscription limit.
 */
export function UsageReportTable({ vendorName, report, loadError, workspaceNames }: UsageReportTableProps) {
  const noData = report !== undefined && report.totals.turns === report.totals.turnsWithoutData;
  return (
    <section className="flex flex-col gap-3" aria-label={`${vendorName} token usage`}>
      <h3 className="text-body-14-medium text-text-primary">Token usage</h3>
      <p className="text-body-12 text-text-tertiary">{QUOTA_NOTE}</p>
      {report === undefined && !loadError ? <Skeleton className="h-24 w-full rounded-control" /> : null}
      {loadError ? (
        <p className="text-body-12 text-status-danger" role="alert">
          {loadError}
        </p>
      ) : null}
      {report !== undefined && noData ? (
        <p className="rounded-control border border-dashed border-border-subtle px-3.5 py-3 text-body-13 text-text-secondary">
          No token data yet for {vendorName}.
        </p>
      ) : null}
      {report !== undefined && !noData ? <ReportBody report={report} workspaceNames={workspaceNames} /> : null}
    </section>
  );
}

function ReportBody({ report, workspaceNames }: { report: UsageReport; workspaceNames: Record<string, string> }) {
  const grand = completeTotal(report.totals);
  const named = (key: string) => workspaceName(key, workspaceNames);
  return (
    <div className="flex flex-col gap-4">
      <p className="text-body-13 text-text-primary">
        {formatMetric(report.totals, "total")} tokens over {report.totals.turns} turns · {cacheReadShare(report.totals)} cache reads
      </p>
      {dataNotes(report).map((note) => (
        <p key={note} className="text-body-12 text-text-tertiary">
          {note}
        </p>
      ))}
      <UsageTable title="By workspace" nameHeader="Workspace" rows={rowsOf(report.byWorkspace, named, grand)} />
      <UsageTable
        title={`By day (last ${DAYS_SHOWN}, UTC)`}
        nameHeader="Day"
        rows={rowsOf(recentDays(report.byDay, DAYS_SHOWN), (key) => key, grand)}
      />
      <UsageTable title="By model" nameHeader="Model" rows={rowsOf(report.byModel, (key) => key, grand)} />
      <UsageTable title="Heaviest sessions" nameHeader="Session" rows={sessionRowsOf(report.topSessions, workspaceNames, grand)} />
    </div>
  );
}

const NUMBER_COLUMNS = ["Turns", "Total", "Fresh input", "Cache read", "Cache write", "Output", "Share"];

function UsageTable({ title, nameHeader, rows }: { title: string; nameHeader: string; rows: UsageRow[] }) {
  if (rows.length === 0) return null;
  const columns: DataTableColumn[] = [
    { key: "name", header: nameHeader, nowrap: true },
    ...NUMBER_COLUMNS.map((header) => ({ key: header, header, align: "right" as const })),
  ];
  return (
    <div className="flex flex-col gap-1.5">
      <h4 className="text-body-13-medium text-text-secondary">{title}</h4>
      <DataTable
        dense
        fill
        caption={title}
        columns={columns}
        rows={rows.map((row) => [row.name, String(row.turns), row.total, row.freshInput, row.cacheRead, row.cacheWrite, row.output, row.share])}
      />
    </div>
  );
}
```

- [x] **Step 4: Run the tests to verify they pass**

Run: `cd frontend && bun test src/modules/agenthub/usage src/smoothnessRatchet.test.ts`
Expected: all pass, including `smoothness ratchet > unvirtualizedTables does not grow` (the file contains no `<table`).

- [ ] **Step 5: Commit**

```bash
git add frontend/src/modules/agenthub/usage/UsageReportTable.tsx frontend/src/modules/agenthub/usage/UsageReportTable.test.tsx
git commit -m "feat(frontend): token usage table

Presentational. Tokens only, with the note that they are not a share of the
subscription limit, per-metric coverage, fresh input, and neutral notes for
incomplete or unequal model splits. Drawn with the shared DataTable."
```

---

### Task 9: Hooks, the connected panel, the page and the search keywords

**Files:**
- Create: `frontend/src/modules/agenthub/usage/useAgentUsage.ts`, `frontend/src/modules/agenthub/usage/AgentUsageSetting.tsx`
- Test: `frontend/src/modules/agenthub/usage/useAgentUsage.test.ts`
- Modify: `frontend/src/modules/settings/AgentsPages.tsx`, `frontend/src/modules/settings/settingsNavigation.ts`, `frontend/src/modules/settings/settingsNavigation.test.ts`

- [x] **Step 1: Write the failing navigation test**

`frontend/src/modules/settings/settingsNavigation.test.ts`:

```diff
--- a/frontend/src/modules/settings/settingsNavigation.test.ts
+++ b/frontend/src/modules/settings/settingsNavigation.test.ts
@@ -55,6 +55,8 @@ describe("Settings navigation", () => {
     expect(idsOf("threshold")).toEqual(["agents-accounts"]);
     expect(idsOf("subscription")).toEqual(["agents-accounts"]);
     expect(idsOf("quota")).toEqual(["agents-accounts"]);
+    expect(idsOf("tokens")).toEqual(["agents-accounts"]);
+    expect(idsOf("cache")).toEqual(["agents-accounts"]);
     expect(idsOf("accounts limits")).toEqual(["agents-accounts"]);
     expect(resolveSettingsTab("agents-accounts")).toBe("agents-accounts");
   });
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd frontend && bun test src/modules/settings/settingsNavigation.test.ts`
Expected: FAIL on `idsOf("tokens")` (received `[]`).

- [x] **Step 2a: Add the focus regression test**

Create `frontend/src/modules/agenthub/usage/useAgentUsage.test.ts`. It imports the hook's options and the application's real
QueryClient defaults, mounts an active observer, then simulates losing and regaining focus. It must fail if the explicit
`refetchOnWindowFocus: true` is removed; checking `staleTime` alone is not sufficient.

```ts
import { expect, test } from "bun:test";
import { focusManager, QueryClient, QueryObserver } from "@tanstack/react-query";
import { queryClient } from "@/lib/query";
import { agentUsageOptions } from "./useAgentUsage";

test("a stale active usage report refetches on focus despite the shared disabled default", async () => {
  const client = new QueryClient({ defaultOptions: queryClient.getDefaultOptions() });
  let reads = 0;
  const observer = new QueryObserver(client, {
    ...agentUsageOptions("claude"),
    queryFn: async () => ++reads,
    staleTime: 0,
  });
  focusManager.setFocused(false);
  client.mount();
  const off = observer.subscribe(() => {});
  let stopWaiting = () => {};
  try {
    await observer.refetch();
    const before = reads;
    const refreshed = new Promise<void>((resolve) => {
      stopWaiting = observer.subscribe((result) => {
        if (result.isSuccess && result.data > before) resolve();
      });
    });
    focusManager.setFocused(true);
    await refreshed;
    expect(reads).toBe(before + 1);
  } finally {
    stopWaiting();
    off();
    client.unmount();
    client.clear();
    focusManager.setFocused(undefined);
  }
}, 2_000);
```

Run: `cd frontend && bun test src/modules/agenthub/usage/useAgentUsage.test.ts`
Expected before implementation: the hook module does not exist. After implementation: pass. If focus refresh is removed, the
regression test times out. Run in the suite with `--isolate`, since `focusManager` is shared state.

- [x] **Step 3: Write the hooks**

`frontend/src/modules/agenthub/usage/useAgentUsage.ts`:

```ts
import { useQuery } from "@tanstack/react-query";
import { agentUsageReport } from "@/bridge/agentUsage";
import { listWorkspaces } from "@/bridge/workspace";

/** Every vendor's usage report; one entry per vendor under it. */
export const AGENT_USAGE_KEY = ["agenthub", "usage"] as const;
export const agentUsageKey = (vendor: string) => [...AGENT_USAGE_KEY, vendor] as const;

/** What a vendor's turns spent, in tokens. Read when the page opens and again when the window regains focus. */
export const agentUsageOptions = (vendor: string) => ({
  queryKey: agentUsageKey(vendor),
  queryFn: () => agentUsageReport(vendor),
  staleTime: 30_000,
  // The shared QueryClient disables this; staleTime alone does not schedule reads.
  refetchOnWindowFocus: true,
});

export function useAgentUsage(vendor: string) {
  return useQuery(agentUsageOptions(vendor));
}

/** Workspace id → name. The report carries ids only; the window names them. */
export function useWorkspaceNames() {
  return useQuery({
    queryKey: ["workspaces", "names"],
    queryFn: async () => Object.fromEntries((await listWorkspaces()).map((workspace) => [workspace.id, workspace.name])),
    staleTime: 60_000,
  });
}
```

- [x] **Step 4: Write the connected panel**

`frontend/src/modules/agenthub/usage/AgentUsageSetting.tsx`:

```tsx
import { errorMessage } from "@/bridge/errors";
import { vendorInfo } from "../vendors";
import { UsageReportTable } from "./UsageReportTable";
import { useAgentUsage, useWorkspaceNames } from "./useAgentUsage";

/**
 * Settings → Agents → Accounts & limits, token usage for one vendor. Reads through
 * the UI-only bridge method; the table below only draws. Keyed by vendor, so a
 * Claude | Codex switch never carries one vendor's report over to the other.
 */
export function AgentUsageSetting({ vendor }: { vendor: string }) {
  return <VendorUsage key={vendor} vendor={vendor} />;
}

function VendorUsage({ vendor }: { vendor: string }) {
  const usage = useAgentUsage(vendor);
  const names = useWorkspaceNames();
  return (
    <UsageReportTable
      vendorName={vendorInfo(vendor).name}
      report={usage.data}
      loadError={usage.error ? errorMessage(usage.error) : ""}
      workspaceNames={names.data ?? {}}
    />
  );
}
```

- [x] **Step 5: Mount it and add the keywords**

`frontend/src/modules/settings/AgentsPages.tsx`:

```diff
--- a/frontend/src/modules/settings/AgentsPages.tsx
+++ b/frontend/src/modules/settings/AgentsPages.tsx
@@ -3,6 +3,7 @@ import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
 import { ACCOUNT_VENDORS } from "@/modules/agenthub/accounts/accountModel";
 import { AgentAccountsSetting } from "@/modules/agenthub/accounts/AgentAccountsSetting";
 import { AgentEnvironmentPanel } from "@/modules/agenthub/environment/AgentEnvironment";
+import { AgentUsageSetting } from "@/modules/agenthub/usage/AgentUsageSetting";
 import { CODEX_INFO, SHIPPED_VENDORS, vendorInfo } from "@/modules/agenthub/vendors";
 import { AgentRuntimeSetting } from "./AgentRuntimeSetting";
 import { AgentModelProfilesSetting } from "./AgentModelProfilesSetting";
@@ -99,9 +100,18 @@ export function AgentAccountsPage() {
     <TabColumn>
       <SectionHeader
         title="Accounts & limits"
-        description="Several accounts per agent, in the order DevKit should use them, and when a session should move to the next one."
+        description="Several accounts per agent, in the order DevKit should use them, when a session should move to the next one, and what the sessions spent in tokens."
+      />
+      <VendorSwitch
+        label="Agent accounts"
+        vendors={ACCOUNT_VENDORS}
+        render={(vendor) => (
+          <>
+            <AgentAccountsSetting vendor={vendor} />
+            <AgentUsageSetting vendor={vendor} />
+          </>
+        )}
       />
-      <VendorSwitch label="Agent accounts" vendors={ACCOUNT_VENDORS} render={(vendor) => <AgentAccountsSetting vendor={vendor} />} />
     </TabColumn>
   );
 }
```

`frontend/src/modules/settings/settingsNavigation.ts`:

```diff
--- a/frontend/src/modules/settings/settingsNavigation.ts
+++ b/frontend/src/modules/settings/settingsNavigation.ts
@@ -38,7 +38,7 @@ export const SETTINGS_GROUPS = [
   { label: "Agents", items: [
     { id: "agents-runtimes", label: "Runtimes", keywords: "claude codex opencode install version node sign in login api key observed sessions" },
     { id: "agents-models", label: "Models & subagents", keywords: "model effort reasoning profiles jobs subagent delegate limit max nesting depth concurrent cost" },
-    { id: "agents-accounts", label: "Accounts & limits", keywords: "accounts several multiple subscription usage quota threshold switch failover order priority terms add claude codex" },
+    { id: "agents-accounts", label: "Accounts & limits", keywords: "accounts several multiple subscription usage quota threshold switch failover order priority terms add claude codex tokens cache report" },
     { id: "agents-skills", label: "Skills & MCP", keywords: "tools skills mcp servers import folders override disable customization" },
     { id: "agents-safety", label: "Safety", keywords: "sandbox checkpoints rollback docker home folder isolation permissions scoped approvals approval" },
     { id: "agents-notifications", label: "Notifications & remote", keywords: "notify alerts telegram bot pairing phone approvals remote control" },
```

- [x] **Step 6: Run the tests, the whole suite and the build**

Run: `cd frontend && bun test --isolate && bun run build`
Expected: `0 fail` (5,036 tests at the time of writing), then `built in ...s` and `Performance budget: initial JS ... KiB, CSS ... KiB` with no error. If the CSS budget trips, raise the budget; never inline the styles.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/modules/agenthub/usage/useAgentUsage.ts frontend/src/modules/agenthub/usage/useAgentUsage.test.ts frontend/src/modules/agenthub/usage/AgentUsageSetting.tsx frontend/src/modules/settings/AgentsPages.tsx frontend/src/modules/settings/settingsNavigation.ts frontend/src/modules/settings/settingsNavigation.test.ts
git commit -m "feat(settings): token usage under Accounts & limits

One panel per vendor below the accounts. Searchable as tokens, cache and report."
```

---

### Task 10: Document it and verify everything

**Files:**
- Modify: `docs/agent-reference/agent-hub-accounts.md`

- [x] **Step 1: Append this bullet to `docs/agent-reference/agent-hub-accounts.md`**

```markdown
- **Token usage report: Settings → Agents → Accounts & limits** (`internal/agenthub/turn_usage.go`, `usage_report.go`, `usage_source.go`;
  bridge `agenthub_usage.go` `AgentUsageReport`, scope `usage:report`, UI-only; frontend `modules/agenthub/usage/`; spec
  `docs/superpowers/specs/2026-10-26-subscription-harness-tuning-design.md`, phase 1a). Numbers and ids only, never a prompt, title or path.
  **Usage is re-derived from each turn's raw meta** with the vendor's current `NormalizeUsage`, because the stored `InputTokens` /
  `OutputTokens` / `CostUSD` are always serialized and turns stored before the normalizer read `quota.token_count` hold a false 0 beside
  real numbers in `RawMeta`; the stored fields are used only for a row with no raw meta. Every aggregate carries `Known` coverage per metric (DTO `known`):
  missing values do not increase coverage, real zeros do. Cells without coverage show `—`, incomplete sums say `(partial)`, and
  percentages require complete coverage of both operands. Model rows expose total coverage only; other cells show `—`.
  Fresh input is displayed beside cache read/write and output. The query explicitly refetches stale data on window focus.
  A turn whose normalized answer has no pointer
  field and only zeros is **unknown** (counted in `TurnsWithoutData`, never added as 0), so legacy input/output-only meta reporting zeros
  is indistinguishable from no data; explicit pointer counters at 0 remain known. `model_usage` is the per-model split and can include helper models that the turn total leaves out,
  so model totals can differ from the overall total; `PartialSplitTurns` counts incomplete, unequal or uncomparable splits.
  The warning does not attribute a mismatch to helper models without evidence. The panel says these are tokens, not a share of
  the subscription limit (how the limit counts cached reads is not known). Claude only; opencode and Codex have no usable usage rows.
  **Not done:** the account id and profile revision on each turn and tool result sizes (plan 1b), so there is no per-account view yet.
```

- [x] **Step 2: Full verification**

```bash
gofmt -l internal/
go vet ./internal/agenthub/ ./internal/bridge/ ./internal/app/
go test ./internal/agenthub/ ./internal/bridge/ ./internal/app/ ./internal/arch/ ./internal/mcp/ -count=1
cd frontend && bun test --isolate && bun run build
```

Expected: `gofmt` and `go vet` print nothing; five lines starting with `ok`; `0 fail`; the build finishes with the performance budget line.

- [ ] **Step 3: Look at it in the running app**

Build and run DevKit (`task run` on a desktop, or see `docs/agent-reference/core-development.md`), open Settings → Agents → Accounts & limits, and check, with Claude selected, that: the headline shows tokens over turns and the cache-read share when coverage is complete; missing totals show `—`, partial sums say `(partial)`, fresh input is visible, non-total model metrics show `—`, and refocusing after another turn refreshes the report; the workspace rows carry workspace names, not ids; the note that tokens are not a share of the subscription limit is visible; switching to Codex shows "No token data yet for Codex.". If you cannot run the desktop app, say so in the pull request instead of claiming it was checked.

- [ ] **Step 4: Commit**

```bash
git add docs/agent-reference/agent-hub-accounts.md
git commit -m "docs(agenthub): describe the token usage report

Records why usage is re-derived from the raw meta, what counts as unknown, and
that model totals can exceed the overall total."
```

---

## Self-review against the spec (phase 1, plan 1a)

| Spec item | Where |
| --- | --- |
| 1. Reading usage (re-derive from raw, unknown is not 0, no double count of `model_usage`, no migration) | Task 1 |
| 2. Report by workspace, session, model, day; UI-only bridge method; numbers only | Tasks 2, 3, 5 (account view moves to plan 1b, noted in the spec's Delivery paragraph) |
| 4. UI under Settings → Agents, wording "tokens" and the subscription caveat | Tasks 8, 9 |
| 5. Claude only, other vendors show no data | Task 8 (`No token data yet`), Task 10 doc |
| Done criteria: reproduces the numbers; the five fixtures (false zero, real zero, absent field, no tokens, no raw), per-metric coverage, and incomplete/unequal/uncomparable splits | Tasks 1, 2, 4, 7, 8 |
| Fresh input visible, missing/partial values marked, focus refresh despite shared defaults | Tasks 7, 8, 9 |
| 3. Per-turn account and profile revision, tool result sizes | **Plan 1b**, not in this plan |

Names used across tasks: `TurnUsage`, `DeriveTurnUsage`, `TurnUsageRow`, `UsageCoverage`, `UsageTotals`, `UsageGroup`, `UsageSessionGroup`, `UsageReport` (with `PartialSplitTurns`), `BuildUsageReport`, `TurnUsageSource`, `TurnUsageRows`, `Service.UsageReport`, `ErrUsageUnavailable`; bridge `AgentUsageReport` and `AgentUsage{Totals,Group,Session,Report}Info`; frontend `agentUsageReport`, `reportOf`, `UsageReport`, `usageModel` helpers, `UsageReportTable`, `useAgentUsage`, `useWorkspaceNames`, `AgentUsageSetting`.
