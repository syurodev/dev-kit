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
