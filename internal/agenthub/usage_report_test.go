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
