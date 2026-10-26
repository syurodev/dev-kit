package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devkit/internal/agenthub"
	_ "modernc.org/sqlite"
)

// claudeMeta is the raw meta of a real Claude turn: the numbers sit under
// quota.token_count, with a per-model split of the same totals.
const claudeMeta = `{"quota":{"token_count":{"totalTokens":2440835,"inputTokens":8,"cachedInputTokens":1832413,"cachedWriteTokens":601633,"outputTokens":6781,"reasoningOutputTokens":0},"model_usage":[{"model":"claude-opus-5-5","token_count":{"totalTokens":2440835,"inputTokens":8,"cachedInputTokens":1832413,"cachedWriteTokens":601633,"outputTokens":6781,"reasoningOutputTokens":0}}]}}`

type turnRow struct {
	id, session, usage, raw, created string
}

func exec(t *testing.T, path, stmt string, args ...any) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(stmt, args...); err != nil {
		t.Fatal(err)
	}
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func insertTurns(t *testing.T, path string, rows ...turnRow) {
	t.Helper()
	for _, r := range rows {
		exec(t, path, `INSERT INTO agent_turns VALUES (?,?,?,?,?)`, r.id, r.session, nullable(r.usage), nullable(r.raw), r.created)
	}
}

func emptyDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.db")
	exec(t, path, `CREATE TABLE agent_sessions (id TEXT PRIMARY KEY, agent_type TEXT NOT NULL);
CREATE TABLE agent_turns (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, usage_json TEXT, raw_meta TEXT, created_at TEXT NOT NULL);
INSERT INTO agent_sessions VALUES ('s1','claude'),('s2','codex'),('empty','claude')`)
	return path
}

func storedUsage(t *testing.T, n agenthub.NormalizedUsage, rawMeta string) string {
	t.Helper()
	if rawMeta != "" {
		n.RawMeta = json.RawMessage(rawMeta)
	}
	b, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func runReport(t *testing.T, args ...string) (report, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	if err := run(args, &out, &errOut); err != nil {
		t.Fatalf("run: %v (%s)", err, errOut.String())
	}
	var got report
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got, out.String() + errOut.String()
}

func wantField(t *testing.T, v *vendorUsage, field string, sum int64, known, unknown int) {
	t.Helper()
	got := v.Fields[field]
	if got.Sum != sum || got.KnownRows != known || got.UnknownRows != unknown {
		t.Errorf("%s = %+v, want sum %d known %d unknown %d", field, got, sum, known, unknown)
	}
}

// A row stored while the normalizer did not understand the meta holds 0 next to
// real numbers in the raw meta. The raw meta wins, as in the Settings report.
func TestFalseStructuredZeroReadsTheRawMeta(t *testing.T) {
	path := emptyDB(t)
	insertTurns(t, path, turnRow{"t1", "s1", storedUsage(t, agenthub.NormalizedUsage{}, claudeMeta), claudeMeta, "2026-10-01T00:00:00Z"})
	got, _ := runReport(t, "--db", path)
	claude := got.Vendors["claude"]
	if claude.TurnsWithUsage != 1 || claude.TurnsWithoutUsage != 0 {
		t.Fatalf("claude = %+v", claude)
	}
	wantField(t, claude, "inputTokens", 8, 1, 0)
	wantField(t, claude, "outputTokens", 6781, 1, 0)
	wantField(t, claude, "totalTokens", 2440835, 1, 0)
	wantField(t, claude, "cachedInputTokens", 1832413, 1, 0)
	wantField(t, claude, "cachedWriteTokens", 601633, 1, 0)
	wantField(t, claude, "reasoningOutputTokens", 0, 1, 0)
	if got.SemanticsVersion != semanticsVersion || claude.TokenCoverage != "claude" {
		t.Fatalf("semantics = %d coverage = %q", got.SemanticsVersion, claude.TokenCoverage)
	}
}

func TestRawMetaFromEitherPlaceAndStructuredOnlyFallback(t *testing.T) {
	path := emptyDB(t)
	total, cached := 42, 17
	structured := storedUsage(t, agenthub.NormalizedUsage{InputTokens: 39, OutputTokens: 3, TotalTokens: &total, CachedInputTokens: &cached}, "")
	insertTurns(t, path,
		// Meta only inside the stored usage: the copy kept there is used.
		turnRow{"t1", "s1", storedUsage(t, agenthub.NormalizedUsage{}, claudeMeta), "", "2026-10-01T00:00:00Z"},
		// No raw meta anywhere: the stored fields are used, pointers stay absent.
		turnRow{"t2", "s1", structured, "", "2026-10-02T00:00:00Z"},
	)
	got, _ := runReport(t, "--db", path)
	claude := got.Vendors["claude"]
	if claude.TurnsWithUsage != 2 {
		t.Fatalf("claude = %+v", claude)
	}
	wantField(t, claude, "inputTokens", 47, 2, 0)
	wantField(t, claude, "totalTokens", 2440877, 2, 0)
	wantField(t, claude, "cachedInputTokens", 1832430, 2, 0)
	wantField(t, claude, "cachedWriteTokens", 601633, 1, 1)
}

// A real 0 in a pointer field is known; a meta with no tokens, a null row and an
// unreadable meta are unknown and never add zeros.
func TestTrueZeroAbsentUnusableAndEmptyRows(t *testing.T) {
	path := emptyDB(t)
	zeros := `{"quota":{"token_count":{"totalTokens":0,"inputTokens":0,"outputTokens":0,"cachedInputTokens":0}}}`
	noWrite := `{"quota":{"token_count":{"totalTokens":10,"inputTokens":4,"outputTokens":6,"cachedInputTokens":2}}}`
	insertTurns(t, path,
		turnRow{"t1", "s1", "", zeros, "2026-10-01T00:00:00Z"},
		turnRow{"t2", "s1", "", noWrite, "2026-10-02T00:00:00Z"},
		turnRow{"t3", "s1", "", `{"claude_code":{}}`, "2026-10-03T00:00:00Z"},
		turnRow{"t4", "s1", "", `{not json`, "2026-10-04T00:00:00Z"},
		turnRow{"t5", "s1", "", "", "2026-10-05T00:00:00Z"},
	)
	got, _ := runReport(t, "--db", path)
	claude := got.Vendors["claude"]
	if claude.Turns != 5 || claude.TurnsWithUsage != 2 || claude.TurnsWithoutUsage != 3 {
		t.Fatalf("claude = %+v", claude)
	}
	wantField(t, claude, "totalTokens", 10, 2, 3)
	wantField(t, claude, "cachedInputTokens", 2, 2, 3)
	wantField(t, claude, "cachedWriteTokens", 0, 0, 5)
}

func TestHelperModelSplitMismatchIsCountedNotAdded(t *testing.T) {
	path := emptyDB(t)
	split := `{"quota":{"token_count":{"totalTokens":100,"inputTokens":1,"outputTokens":2},"model_usage":[{"model":"a","token_count":{"totalTokens":100}},{"model":"helper","token_count":{"totalTokens":30}}]}}`
	insertTurns(t, path,
		turnRow{"t1", "s1", "", split, "2026-10-01T00:00:00Z"},
		turnRow{"t2", "s1", "", claudeMeta, "2026-10-02T00:00:00Z"},
	)
	got, _ := runReport(t, "--db", path)
	claude := got.Vendors["claude"]
	if claude.PartialSplitTurns != 1 {
		t.Fatalf("partial split turns = %d, want 1", claude.PartialSplitTurns)
	}
	wantField(t, claude, "totalTokens", 2440935, 2, 0)
}

// Token accounting is Claude-only: other vendors keep their turns but never
// report a token, even when their row holds counters.
func TestOtherVendorsAreUnknown(t *testing.T) {
	path := emptyDB(t)
	exec(t, path, `INSERT INTO agent_sessions VALUES ('s3','gemini')`)
	insertTurns(t, path,
		turnRow{"t1", "s2", `{"InputTokens":25,"OutputTokens":7,"CachedInputTokens":25}`, `{"codex":{"turn":{"inputTokens":25}}}`, "2026-10-01T00:00:00Z"},
		turnRow{"t2", "s3", `{"InputTokens":5}`, "", "2026-10-02T00:00:00Z"},
	)
	got, _ := runReport(t, "--db", path)
	for _, name := range []string{"codex", "gemini"} {
		v := got.Vendors[name]
		if v.TokenCoverage != "unknown" || v.Turns != 1 || v.TurnsWithUsage != 0 || v.TurnsWithoutUsage != 1 {
			t.Fatalf("%s = %+v", name, v)
		}
		for field, f := range v.Fields {
			if f.KnownRows != 0 || f.Sum != 0 || f.UnknownRows != 1 {
				t.Errorf("%s %s = %+v, want unknown", name, field, f)
			}
		}
	}
}

// The CLI and the Settings report read the same rows the same way.
func TestCLIAgreesWithAgentHubReport(t *testing.T) {
	path := emptyDB(t)
	total, cached, write := 42, 17, 3
	structured := storedUsage(t, agenthub.NormalizedUsage{InputTokens: 39, OutputTokens: 3, TotalTokens: &total, CachedInputTokens: &cached, CachedWriteTokens: &write}, "")
	rows := []turnRow{
		{"t1", "s1", storedUsage(t, agenthub.NormalizedUsage{}, claudeMeta), claudeMeta, "2026-10-01T00:00:00Z"},
		{"t2", "s1", "", `{"quota":{"token_count":{"totalTokens":0,"inputTokens":0,"outputTokens":0}}}`, "2026-10-02T00:00:00Z"},
		{"t3", "s1", "", `{"quota":{"token_count":{"totalTokens":10,"inputTokens":4,"outputTokens":6}}}`, "2026-10-03T00:00:00Z"},
		{"t4", "s1", "", `{broken`, "2026-10-04T00:00:00Z"},
		{"t5", "s1", structured, "", "2026-10-05T00:00:00Z"},
		{"t6", "s1", "", `{"claude_code":{}}`, "2026-10-06T00:00:00Z"},
		{"t7", "s1", "", "", "2026-10-07T00:00:00Z"},
	}
	insertTurns(t, path, rows...)

	var want []agenthub.TurnUsageRow
	for _, r := range rows {
		ts, err := time.Parse(time.RFC3339, r.created)
		if err != nil {
			t.Fatal(err)
		}
		row := agenthub.TurnUsageRow{TurnID: r.id, SessionID: r.session, CreatedAt: ts}
		if r.usage != "" {
			row.UsageJSON = []byte(r.usage)
		}
		if r.raw != "" {
			row.RawMeta = []byte(r.raw)
		}
		want = append(want, row)
	}
	hub := agenthub.BuildUsageReport(agenthub.AgentTypeClaude, agenthub.NewClaudeAdapter(agenthub.ClaudeOptions{}), want)

	got, _ := runReport(t, "--db", path, "--session", "s1")
	claude := got.Vendors["claude"]
	if claude.Turns != hub.Totals.Turns || claude.TurnsWithoutUsage != hub.Totals.TurnsWithoutData || claude.PartialSplitTurns != hub.PartialSplitTurns {
		t.Fatalf("turns cli=%+v hub=%+v", claude, hub.Totals)
	}
	for _, c := range []struct {
		field string
		sum   int64
		known int
	}{
		{"inputTokens", hub.Totals.FreshInput, hub.Totals.Known.FreshInput},
		{"outputTokens", hub.Totals.Output, hub.Totals.Known.Output},
		{"totalTokens", hub.Totals.Total, hub.Totals.Known.Total},
		{"cachedInputTokens", hub.Totals.CacheRead, hub.Totals.Known.CacheRead},
		{"cachedWriteTokens", hub.Totals.CacheWrite, hub.Totals.Known.CacheWrite},
	} {
		f := claude.Fields[c.field]
		if f.Sum != c.sum || f.KnownRows != c.known || f.UnknownRows != hub.Totals.Turns-c.known {
			t.Errorf("%s cli=%+v hub sum=%d known=%d turns=%d", c.field, f, c.sum, c.known, hub.Totals.Turns)
		}
	}
}

func TestReportNeverEmitsRawOrStoredContent(t *testing.T) {
	path := emptyDB(t)
	secretMeta := `{"privatePrompt":"DO_NOT_PRINT_9cb","quota":{"token_count":{"totalTokens":5,"inputTokens":2,"outputTokens":3},"model_usage":[{"model":"DO_NOT_PRINT_model","token_count":{"totalTokens":5}}]}}`
	insertTurns(t, path,
		turnRow{"t1", "s1", storedUsage(t, agenthub.NormalizedUsage{}, `{"secret":"DO_NOT_PRINT_EMBEDDED_84f"}`), secretMeta, "2026-10-01T00:00:00Z"},
		turnRow{"t2", "s1", "", `{"apiKey":"DO_NOT_PRINT_7aa" broken`, "2026-10-02T00:00:00Z"},
		turnRow{"t3", "s2", "", `{"body":"DO_NOT_PRINT_188"}`, "2026-10-03T00:00:00Z"},
	)
	_, text := runReport(t, "--db", path)
	if strings.Contains(text, "DO_NOT_PRINT") || strings.Contains(text, "secret") || strings.Contains(text, "apiKey") {
		t.Fatal("report exposed raw metadata")
	}
}

func TestTimeBoundsCompareInstantsAtNanosecondPrecision(t *testing.T) {
	path := emptyDB(t)
	meta := func(n int) string {
		return fmt.Sprintf(`{"quota":{"token_count":{"totalTokens":%d,"inputTokens":%d,"outputTokens":0}}}`, n, n)
	}
	exec(t, path, `UPDATE agent_sessions SET agent_type='claude' WHERE id='s2'`)
	insertTurns(t, path,
		turnRow{"t5", "s2", "", meta(3), "2026-10-01T00:00:00.000000001Z"},
		turnRow{"t6", "s2", "", meta(4), "2026-10-01T01:00:00+01:00"},
		turnRow{"t7", "s2", "", meta(5), "2026-10-01T00:00:00.000000002Z"},
	)
	got, _ := runReport(t, "--db", path, "--from", "2026-10-01T00:00:00Z", "--to", "2026-10-01T00:00:00.000000002+00:00")
	claude := got.Vendors["claude"]
	if got.Turns != 2 || claude.Turns != 2 {
		t.Fatalf("instant-bounded report: turns=%d vendors=%+v", got.Turns, got.Vendors)
	}
	if sum := claude.Fields["inputTokens"].Sum; sum != 7 {
		t.Fatalf("input sum = %d, want 7; end bound must be exclusive", sum)
	}
}

func TestCLIExplicitSessionAndTimeBounds(t *testing.T) {
	path := emptyDB(t)
	insertTurns(t, path,
		turnRow{"t1", "s1", "", claudeMeta, "2026-10-01T00:00:00Z"},
		turnRow{"t2", "s1", "", claudeMeta, "2026-10-02T00:00:00Z"},
		turnRow{"t3", "s2", "", claudeMeta, "2026-10-02T12:00:00Z"},
	)
	got, _ := runReport(t, "--db", path, "--session", "s1", "--from", "2026-10-02T00:00:00Z", "--to", "2026-10-03T00:00:00Z")
	if got.Sessions != 1 || got.Turns != 1 || got.Vendors["claude"].Fields["inputTokens"].Sum != 8 {
		t.Fatalf("bounded report: %+v", got)
	}
}

func TestDatabaseOpenedReadOnlyAndDoesNotCreateMissingFile(t *testing.T) {
	path := emptyDB(t)
	insertTurns(t, path, turnRow{"t1", "s1", "", claudeMeta, "2026-10-01T00:00:00Z"})
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err = run([]string{"--db", path}, &out, &out); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("read-only report changed database bytes")
	}
	missing := filepath.Join(t.TempDir(), "missing.db")
	if err = run([]string{"--db", missing}, &out, &out); err == nil {
		t.Fatal("missing database unexpectedly opened")
	}
	if _, err = os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("missing db stat error = %v", err)
	}
}

func TestParseBoundRequiresRFC3339(t *testing.T) {
	if _, err := parseBound("yesterday"); err == nil {
		t.Fatal("accepted invalid bound")
	}
	got, err := parseBound("2026-10-01T01:00:00+01:00")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("bound = %s", got)
	}
}
