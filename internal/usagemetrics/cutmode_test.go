package usagemetrics_test

import (
	"context"
	"strings"
	"testing"
	"time"

	m "devkit/internal/usagemetrics"
)

func modeObservation(tool, mode string) m.Observation {
	o := observation(tool)
	o.CutMode = mode
	return o
}

// New rows say which algorithm ran, from a fixed vocabulary. A result whose
// producer said nothing, or said something outside the vocabulary, is "unrecorded"
// and is never read as a head-and-tail (or any other) measurement.
func TestRowsCarryTheAlgorithmThatRanFromAFixedVocabulary(t *testing.T) {
	ctx := context.Background()
	db := database(t)
	store := m.NewSQLStore(db)
	c, err := m.New(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{m.CutNone, m.CutHead, m.CutHeadTail, m.CutStructured, m.CutExempt, m.CutError} {
		c.Record(modeObservation("tool."+mode, mode))
	}
	c.Record(modeObservation("tool.empty", ""))                  // a result nobody classified
	c.Record(modeObservation("tool.forged", "headTail; DROP--")) // a string outside the vocabulary
	noResult := observation("tool.noresult")
	noResult.Result, noResult.CutMode = false, "headTail" // a call with no result has nothing cut
	c.Record(noResult)
	if err := c.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	got := map[string]string{}
	rows, err := db.Query(`SELECT tool, version, cut_mode FROM agent_tool_usage_daily`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var tool, version, mode string
		if err := rows.Scan(&tool, &version, &mode); err != nil {
			t.Fatal(err)
		}
		if version != m.Version || version == m.LegacyVersion {
			t.Fatalf("%s written under %q, want the new measurement version", tool, version)
		}
		got[tool] = mode
	}
	for _, mode := range []string{m.CutNone, m.CutHead, m.CutHeadTail, m.CutStructured, m.CutExempt, m.CutError} {
		if got["tool."+mode] != mode {
			t.Errorf("tool.%s recorded as %q", mode, got["tool."+mode])
		}
	}
	for tool, want := range map[string]string{"tool.empty": m.CutUnrecorded, "tool.forged": m.CutUnrecorded, "tool.noresult": m.CutNone} {
		if got[tool] != want {
			t.Errorf("%s recorded as %q, want %q", tool, got[tool], want)
		}
	}
	if m.Version == m.LegacyVersion {
		t.Fatal("the clarified semantics need a new measurement version")
	}
}

// History is never rewritten: a v1 row keeps its literal "head" key and counters
// whatever is recorded afterwards, and a report adds v1 and v2 rows once each.
func TestLegacyRowsStayExactlyAsStoredAndReportsAddEachRowOnce(t *testing.T) {
	ctx := context.Background()
	db := database(t)
	today := time.Now().UTC().Format(time.DateOnly)
	if _, err := db.Exec(`INSERT INTO agent_tool_usage_daily(day,vendor,workspace_key,tool,profile_key,override_key,version,cut_mode,profile_revision,override_revision,
 calls,results,text_calls,non_text_calls,tool_errors,handler_errors,exempt_calls,cuts,before_bytes,after_bytes,max_before,max_after)
 VALUES(?,?,?,?,?,?,?,?,NULL,NULL,5,5,5,0,0,0,0,2,500,200,300,100)`, today, "claude", "w1", "git.diff", "none", "none", m.LegacyVersion, "head"); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		rows, err := db.Query(`SELECT version||'|'||cut_mode||'|'||calls||'|'||cuts||'|'||before_bytes||'|'||after_bytes||'|'||max_before||'|'||max_after FROM agent_tool_usage_daily WHERE version=? ORDER BY 1`, m.LegacyVersion)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				t.Fatal(err)
			}
			out = append(out, s)
		}
		return strings.Join(out, ";")
	}
	before := snapshot()
	if before == "" {
		t.Fatal("setup: no legacy row")
	}

	store := m.NewSQLStore(db)
	c, err := m.New(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		o := modeObservation("git.diff", m.CutHeadTail)
		o.Before, o.After = 1000, 400
		c.Record(o)
	}
	c.Record(modeObservation("git.diff", m.CutHead))
	if err := c.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if after := snapshot(); after != before {
		t.Fatalf("a legacy row changed:\n before %s\n after  %s", before, after)
	}

	rep, err := store.Report(ctx, "claude", m.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Rows) != 1 || rep.Rows[0].Tool != "git.diff" {
		t.Fatalf("report rows = %+v", rep.Rows)
	}
	s := rep.Rows[0].Stats
	// 5 legacy calls + 3 headTail + 1 head, each counted once; bytes add the same way.
	if s.Calls != 9 || s.Cuts != 2+4 || s.BeforeBytes != 500+3*1000+100 || s.AfterBytes != 200+3*400+25 || s.MaxBefore != 1000 {
		t.Fatalf("aggregate across versions: %+v", s)
	}
	var versions int
	if err := db.QueryRow(`SELECT COUNT(DISTINCT version) FROM agent_tool_usage_daily`).Scan(&versions); err != nil || versions != 2 {
		t.Fatalf("expected both measurement versions side by side, got %d (%v)", versions, err)
	}
}
