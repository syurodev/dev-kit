package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devkit/internal/agenthub"
	_ "modernc.org/sqlite"
)

func fixtureDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE agent_sessions (id TEXT PRIMARY KEY, agent_type TEXT NOT NULL);
CREATE TABLE agent_turns (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, usage_json TEXT, raw_meta TEXT, created_at TEXT NOT NULL);
INSERT INTO agent_sessions VALUES ('s1','claude'),('s2','codex'),('empty','claude');
INSERT INTO agent_turns VALUES
 ('t1','s1','{"inputTokens":100,"outputTokens":12,"totalTokens":112,"cachedInputTokens":40,"cachedWriteTokens":5}','{"privatePrompt":"DO_NOT_PRINT_9cb"}','2026-10-01T00:00:00Z'),
 ('t2','s1','{"inputTokens":0,"outputTokens":0,"totalTokens":0}','{"apiKey":"DO_NOT_PRINT_7aa"}','2026-10-02T00:00:00Z'),
 ('t3','s2',NULL,'{"body":"DO_NOT_PRINT_188"}','2026-10-03T00:00:00Z'),
 ('t4','s2','{"inputTokens":25,"outputTokens":7,"cachedInputTokens":25}','{}','2026-10-04T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCollectReportsKnownAndUnknownWithoutAddingCacheTwice(t *testing.T) {
	path := fixtureDB(t)
	var out, stderr bytes.Buffer
	if err := run([]string{"--db", path}, &out, &stderr); err != nil {
		t.Fatalf("run: %v (%s)", err, stderr.String())
	}
	var got report
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Sessions != 3 || got.Turns != 4 {
		t.Fatalf("sessions/turns = %d/%d", got.Sessions, got.Turns)
	}
	claude := got.Vendors["claude"]
	if claude == nil || claude.Sessions != 2 || claude.TurnsWithUsage != 2 || claude.TurnsWithoutUsage != 0 {
		t.Fatalf("claude = %+v", claude)
	}
	input := claude.Fields["inputTokens"]
	if input.Sum != 100 || input.KnownRows != 2 || input.UnknownRows != 0 {
		t.Fatalf("claude input = %+v", input)
	}
	cached := claude.Fields["cachedInputTokens"]
	if cached.Sum != 40 || cached.KnownRows != 1 || cached.UnknownRows != 1 {
		t.Fatalf("claude cached = %+v", cached)
	}
	codex := got.Vendors["codex"]
	if codex.TurnsWithUsage != 1 || codex.TurnsWithoutUsage != 1 {
		t.Fatalf("codex = %+v", codex)
	}
	if codex.Fields["inputTokens"].Sum != 25 || codex.Fields["cachedInputTokens"].Sum != 25 {
		t.Fatalf("codex counters = %+v", codex.Fields)
	}
	if strings.Contains(out.String(), "DO_NOT_PRINT") {
		t.Fatal("report exposed raw metadata")
	}
	if got.Unknown["coordinatorWakeups"].Value != "unknown" || got.Unknown["tokenSavings"].Value != "unknown" {
		t.Fatalf("unknown metrics = %+v", got.Unknown)
	}
}

func TestTimeBoundsCompareInstantsAtNanosecondPrecision(t *testing.T) {
	path := fixtureDB(t)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO agent_turns VALUES
 ('t5','s2','{"inputTokens":3}','{}','2026-10-01T00:00:00.000000001Z'),
 ('t6','s2','{"inputTokens":4}','{}','2026-10-01T01:00:00+01:00'),
 ('t7','s2','{"inputTokens":5}','{}','2026-10-01T00:00:00.000000002Z')`)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = run([]string{"--db", path, "--from", "2026-10-01T00:00:00Z", "--to", "2026-10-01T00:00:00.000000002+00:00"}, &out, &out)
	if err != nil {
		t.Fatal(err)
	}
	var got report
	if err = json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Turns != 3 || got.Vendors["claude"].Turns != 1 || got.Vendors["codex"].Turns != 2 {
		t.Fatalf("instant-bounded report: sessions=%d turns=%d vendors=%+v", got.Sessions, got.Turns, got.Vendors)
	}
	if sum := got.Vendors["codex"].Fields["inputTokens"].Sum; sum != 7 {
		t.Fatalf("codex input sum = %d, want 7; end bound must be exclusive", sum)
	}
}

func TestCounterRejectsNegativeAndOverflowWithoutWrapping(t *testing.T) {
	path := fixtureDB(t)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO agent_sessions VALUES ('s4','gemini'),('empty-gemini','gemini');
INSERT INTO agent_turns VALUES
 ('g1','s4','{"inputTokens":9223372036854775807}','{}','2026-10-05T00:00:00Z'),
 ('g2','s4','{"inputTokens":1}','{}','2026-10-05T00:00:01Z'),
 ('g3','s4','{"inputTokens":-1,"outputTokens":9223372036854775808}','{}','2026-10-05T00:00:02Z')`)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err = run([]string{"--db", path}, &out, &out); err != nil {
		t.Fatal(err)
	}
	var got report
	if err = json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	gemini := got.Vendors["gemini"]
	if gemini == nil || gemini.Sessions != 2 || gemini.Turns != 3 {
		t.Fatalf("gemini coverage = %+v", gemini)
	}
	input := gemini.Fields["inputTokens"]
	if input.Sum != 9223372036854775807 || input.KnownRows != 1 || input.UnknownRows != 2 || input.InvalidRows != 2 {
		t.Fatalf("input counter did not preserve partial sum and mark invalid rows: %+v", input)
	}
	output := gemini.Fields["outputTokens"]
	if output.KnownRows != 0 || output.UnknownRows != 3 || output.InvalidRows != 1 {
		t.Fatalf("out-of-range JSON integer should be unknown: %+v", output)
	}
}

func TestUsageFromNormalizedUsageMarshalUsesGoFieldNamesAndRedactsEmbeddedRawMeta(t *testing.T) {
	path := fixtureDB(t)
	total, cached, cachedWrite, reasoning := 42, 17, 3, 2
	usage, err := json.Marshal(agenthub.NormalizedUsage{
		InputTokens:           39,
		OutputTokens:          3,
		TotalTokens:           &total,
		CachedInputTokens:     &cached,
		CachedWriteTokens:     &cachedWrite,
		ReasoningOutputTokens: &reasoning,
		RawMeta:               json.RawMessage(`{"secret":"DO_NOT_PRINT_EMBEDDED_84f"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO agent_sessions VALUES ('s-real','claude');
INSERT INTO agent_turns VALUES ('t-real','s-real',?,'{"secret":"DO_NOT_PRINT_COLUMN_84f"}','2026-10-06T00:00:00Z')`, string(usage))
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err = run([]string{"--db", path, "--session", "s-real"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	var got report
	if err = json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	claude := got.Vendors["claude"]
	if claude == nil || claude.TurnsWithUsage != 1 {
		t.Fatalf("real NormalizedUsage row = %+v", claude)
	}
	wants := map[string]int64{"inputTokens": 39, "outputTokens": 3, "totalTokens": 42, "cachedInputTokens": 17, "cachedWriteTokens": 3, "reasoningOutputTokens": 2}
	for field, want := range wants {
		if value := claude.Fields[field]; value.Sum != want || value.KnownRows != 1 || value.UnknownRows != 0 {
			t.Errorf("%s = %+v, want one known value %d", field, value, want)
		}
	}
	if strings.Contains(out.String(), "DO_NOT_PRINT") || strings.Contains(out.String(), "secret") {
		t.Fatal("report exposed embedded or column raw metadata")
	}
}

func TestUsageCounterAliasesDoNotDoubleCountOrHideConflicts(t *testing.T) {
	for _, tc := range []struct {
		name        string
		json        string
		want        int64
		wantKnown   bool
		wantInvalid bool
	}{
		{name: "historical camelCase", json: `{"inputTokens":5}`, want: 5, wantKnown: true},
		{name: "same aliases count once", json: `{"InputTokens":5,"inputTokens":5}`, want: 5, wantKnown: true},
		{name: "conflicting aliases unknown", json: `{"InputTokens":5,"inputTokens":6}`, wantInvalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values, invalid, hasUsage, err := usageCounters(sql.NullString{String: tc.json, Valid: true})
			if err != nil || !hasUsage {
				t.Fatalf("usageCounters = %v, %v, %v", values, hasUsage, err)
			}
			_, known := values["inputTokens"]
			if known != tc.wantKnown || known && values["inputTokens"] != tc.want || invalid["inputTokens"] != tc.wantInvalid {
				t.Fatalf("values=%v invalid=%v, want known=%v value=%d invalid=%v", values, invalid, tc.wantKnown, tc.want, tc.wantInvalid)
			}
		})
	}
}

func TestCLIExplicitSessionAndTimeBounds(t *testing.T) {
	path := fixtureDB(t)
	var out bytes.Buffer
	err := run([]string{"--db", path, "--session", "s1", "--from", "2026-10-02T00:00:00Z", "--to", "2026-10-03T00:00:00Z"}, &out, &out)
	if err != nil {
		t.Fatal(err)
	}
	var got report
	if err = json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Sessions != 1 || got.Turns != 1 || got.Vendors["claude"].Fields["inputTokens"].Sum != 0 {
		t.Fatalf("bounded report: %+v", got)
	}
}

func TestDatabaseOpenedReadOnlyAndDoesNotCreateMissingFile(t *testing.T) {
	path := fixtureDB(t)
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
