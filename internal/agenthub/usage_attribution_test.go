package agenthub_test

import (
	"context"
	"devkit/internal/agenthub"
	"devkit/internal/identity"
	"devkit/internal/preferences"
	"devkit/internal/usagemetrics"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAccountGenerationSurvivesEditsButNotSlugReuse(t *testing.T) {
	ctx := context.Background()
	db := newUsageDB(t)
	book := agenthub.NewSQLAccountStore(db)
	a := agenthub.AccountRecord{ID: "work", Vendor: agenthub.AgentTypeClaude, HomeDir: t.TempDir(), Label: "One", UsageGeneration: "untrusted"}
	if e := book.Put(ctx, a); e != nil {
		t.Fatal(e)
	}
	first, e := book.Get(ctx, a.ID)
	if e != nil || first.UsageGeneration == "" || first.UsageGeneration == "untrusted" {
		t.Fatalf("generation %+v %v", first, e)
	}
	a.Label = "Renamed"
	if e = book.Put(ctx, a); e != nil {
		t.Fatal(e)
	}
	edited, _ := book.Get(ctx, a.ID)
	if edited.UsageGeneration != first.UsageGeneration {
		t.Fatal("edit reset generation")
	}
	if _, e = db.Exec(`DELETE FROM agent_accounts WHERE id='work'`); e != nil {
		t.Fatal(e)
	}
	if e = book.Put(ctx, a); e != nil {
		t.Fatal(e)
	}
	recreated, _ := book.Get(ctx, a.ID)
	if recreated.UsageGeneration == first.UsageGeneration {
		t.Fatal("reused slug inherited generation")
	}
	rows, e := book.List(ctx, agenthub.AgentTypeClaude)
	if e != nil {
		t.Fatal(e)
	}
	if rows[0].UsageGeneration != "builtin" {
		t.Fatalf("builtin %+v", rows[0])
	}
}
func TestTurnAttributionPreservesWorkspaceAndLegacyUnknown(t *testing.T) {
	ctx := context.Background()
	db := newUsageDB(t)
	st := agenthub.NewSQLStore(db)
	s := sampleSession("session")
	s.AgentType = agenthub.AgentTypeClaude
	s.WorkspaceID = "w1"
	if e := st.InsertSession(ctx, s); e != nil {
		t.Fatal(e)
	}
	at := time.Now().UTC()
	raw := json.RawMessage(`{"quota":{"token_count":{"totalTokens":10,"cachedWriteTokens":0}}}`)
	facts := agenthub.RuntimeUsageFacts{AccountID: "work", AccountGeneration: "old-generation", WorkspaceID: ""}
	for _, turn := range []agenthub.TurnRecord{{ID: "old", SessionID: s.ID, CreatedAt: at, RawMeta: raw}, {ID: "new", SessionID: s.ID, CreatedAt: at, RawMeta: raw, Attribution: &agenthub.TurnAttribution{ContextVersion: 1, StartedAt: at, Facts: facts}}} {
		if e := st.InsertTurn(ctx, turn); e != nil {
			t.Fatal(e)
		}
	}
	s.WorkspaceID = "w2"
	s.AccountID = "some-current-account"
	if e := st.UpdateSession(ctx, s); e != nil {
		t.Fatal(e)
	}
	turns, e := st.ListTurns(ctx, s.ID)
	if e != nil {
		t.Fatal(e)
	}
	for _, turn := range turns {
		if turn.ID == "old" && turn.Attribution != nil {
			t.Fatal("legacy backfilled")
		}
		if turn.ID == "new" && (turn.Attribution == nil || !reflect.DeepEqual(turn.Attribution.Facts, facts)) {
			t.Fatalf("roundtrip %+v", turn)
		}
	}
	rows, e := st.TurnUsageRows(ctx, s.AgentType)
	if e != nil {
		t.Fatal(e)
	}
	for _, r := range rows {
		if r.TurnID == "old" && r.WorkspaceID != "w2" {
			t.Fatal("legacy fallback lost")
		}
		if r.TurnID == "new" && r.WorkspaceID != "" {
			t.Fatal("captured unbound workspace adopted mutable session workspace")
		}
	}
	rep := agenthub.BuildUsageReport(s.AgentType, agenthub.NewClaudeAdapter(agenthub.ClaudeOptions{}), rows)
	var total agenthub.UsageTotals
	for _, g := range rep.ByAccount {
		total.Turns += g.Turns
		total.Total += g.Total
		total.Known.Total += g.Known.Total
	}
	if len(rep.ByAccount) != 2 || total.Turns != rep.Totals.Turns || total.Total != rep.Totals.Total || total.Known.Total != rep.Totals.Known.Total {
		t.Fatalf("account totals do not reconcile %+v", rep)
	}
}
func TestInvokeCapturesActualMovedAccountAndHostNotice(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	s := startSession(t, rig)
	rig.fillBuiltin(t, 95)
	if _, e := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: s.ID, Prompt: "hello"}); e != nil {
		t.Fatal(e)
	}
	turns, e := rig.store.ListTurns(context.Background(), s.ID)
	if e != nil || len(turns) != 1 {
		t.Fatalf("turns %v %v", turns, e)
	}
	account, _ := rig.book.Get(context.Background(), "work")
	a := turns[0].Attribution
	if a == nil || a.ContextVersion != 1 || a.Facts.AccountID != "work" || a.Facts.AccountGeneration != account.UsageGeneration || a.StartedAt.IsZero() || a.StartedAt.After(turns[0].CreatedAt) {
		t.Fatalf("actual runtime attribution %+v", a)
	}
	report, e := rig.store.(agenthub.AccountMoveSource).AccountMoveUsageRows(context.Background(), agenthub.AgentTypeMock, agenthub.AccountMoveQuery{})
	if e != nil || len(report.Rows) != 1 || report.Rows[0].ToGeneration != account.UsageGeneration || report.Rows[0].Status != "no_accounting" {
		t.Fatalf("host notice report %+v %v", report, e)
	}
}
func TestMeasurementServiceReadsAreWindowOnly(t *testing.T) {
	svc := newUsageService(t)
	for _, caller := range []identity.Caller{identity.Agent("session"), identity.UserTerminal(), {Kind: identity.KindRemoteHuman}} {
		ctx := identity.WithCaller(context.Background(), caller)
		if _, e := svc.ToolUsageReport(ctx, agenthub.AgentTypeClaude, usagemetrics.Query{}); !errors.Is(e, agenthub.ErrPermissionDenied) {
			t.Fatalf("tools read %v", e)
		}
		if _, e := svc.AccountMoveUsageReport(ctx, agenthub.AgentTypeClaude, agenthub.AccountMoveQuery{}); !errors.Is(e, agenthub.ErrPermissionDenied) {
			t.Fatalf("moves read %v", e)
		}
	}
}
func TestAccountMoveSQLPaginationIsFilterBoundAndProjectsNoReason(t *testing.T) {
	ctx := context.Background()
	db := newUsageDB(t)
	st := agenthub.NewSQLStore(db)
	s := sampleSession("moves")
	s.AgentType = agenthub.AgentTypeClaude
	if e := st.InsertSession(ctx, s); e != nil {
		t.Fatal(e)
	}
	for i, id := range []string{"n1", "n2", "n3"} {
		payload := `{"accountNotice":{"kind":"switch","from":"old","to":"work","reason":"SECRET-REASON","runtimeOpened":true,"toGeneration":"g1"}}`
		if _, e := db.Exec(`INSERT INTO agent_transcript(id,session_id,seq,kind,payload,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, id, s.ID, i+1, "session_info_update", payload, "2026-10-26T09:00:00Z", "2026-10-26T09:00:00Z"); e != nil {
			t.Fatal(e)
		}
	}
	if e := st.InsertTurn(ctx, agenthub.TurnRecord{ID: "first-accounted", SessionID: s.ID, CreatedAt: time.Date(2026, 10, 26, 9, 0, 2, 0, time.UTC), RawMeta: json.RawMessage(`{"quota":{"token_count":{"cachedWriteTokens":0}}}`), Attribution: &agenthub.TurnAttribution{ContextVersion: 1, StartedAt: time.Date(2026, 10, 26, 9, 0, 1, 0, time.UTC), Facts: agenthub.RuntimeUsageFacts{AccountID: "work", AccountGeneration: "g1"}}}); e != nil {
		t.Fatal(e)
	}
	q := agenthub.AccountMoveQuery{From: "2026-10-26", To: "2026-10-26", Limit: 2}
	r, e := st.AccountMoveUsageRows(ctx, s.AgentType, q)
	if e != nil || len(r.Rows) != 2 || r.NextCursor == "" || r.Rows[0].ID != "n3" || r.Rows[0].Status != "matched" || r.Rows[0].CacheWrite == nil || *r.Rows[0].CacheWrite != 0 {
		t.Fatalf("first %+v %v", r, e)
	}
	raw, _ := json.Marshal(r)
	if strings.Contains(string(raw), "SECRET-REASON") {
		t.Fatal("reason leaked")
	}
	q.Cursor = r.NextCursor
	r, e = st.AccountMoveUsageRows(ctx, s.AgentType, q)
	if e != nil || len(r.Rows) != 1 || r.Rows[0].ID != "n1" || r.NextCursor != "" {
		t.Fatalf("second %+v %v", r, e)
	}
	q.To = "2026-10-27"
	if _, e = st.AccountMoveUsageRows(ctx, s.AgentType, q); !errors.Is(e, usagemetrics.ErrInvalidQuery) {
		t.Fatalf("cursor reused across filters %v", e)
	}
}

func TestAccountPartitionReconcilesEveryMetricAndCoverageAcrossGenerations(t *testing.T) {
	at := time.Now().UTC()
	rows := []agenthub.TurnUsageRow{claudeRow("a", "s", "w1", at, 100, 10, 50, 20, 20, "model"), claudeRow("b", "s", "w1", at, 0, 0, 0, 0, 0, "model"), {TurnID: "legacy", SessionID: "s", CreatedAt: at, RawMeta: json.RawMessage(`{}`)}}
	for i, gen := range []string{"first-generation", "second-generation"} {
		rows[i].Attribution = &agenthub.TurnAttribution{ContextVersion: 1, Facts: agenthub.RuntimeUsageFacts{AccountID: "reused-slug", AccountGeneration: gen}}
	}
	rep := agenthub.BuildUsageReport(agenthub.AgentTypeClaude, agenthub.NewClaudeAdapter(agenthub.ClaudeOptions{}), rows)
	sum := agenthub.UsageTotals{}
	for _, g := range rep.ByAccount {
		sum.Turns += g.Turns
		sum.TurnsWithoutData += g.TurnsWithoutData
		sum.FreshInput += g.FreshInput
		sum.CacheRead += g.CacheRead
		sum.CacheWrite += g.CacheWrite
		sum.Output += g.Output
		sum.Total += g.Total
		sum.Known.FreshInput += g.Known.FreshInput
		sum.Known.CacheRead += g.Known.CacheRead
		sum.Known.CacheWrite += g.Known.CacheWrite
		sum.Known.Output += g.Known.Output
		sum.Known.Total += g.Known.Total
	}
	if len(rep.ByAccount) != 3 || !reflect.DeepEqual(sum, rep.Totals) {
		t.Fatalf("partition %+v != %+v", sum, rep.Totals)
	}
}

func TestRuntimeAttributionKeepsOpeningAccountRemovalGuard(t *testing.T) {
	rig, _, _ := manageRig(t)
	acct, e := rig.svc.AddAccount(uiCtx(), agenthub.AgentTypeMock, "Busy attribution")
	if e != nil {
		t.Fatal(e)
	}
	sess := startSession(t, rig)
	if e = rig.svc.BindSessionAccountForTest(context.Background(), sess.ID, acct.ID); e != nil {
		t.Fatal(e)
	}
	started, release := rig.vendor.holdNextOpen()
	defer release()
	done := make(chan error, 1)
	go func() { done <- rig.svc.ResumeSession(uiCtx(), sess.ID, true) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("resume did not enter vendor")
	}
	if e = rig.svc.RemoveAccount(uiCtx(), acct.ID); !errors.Is(e, agenthub.ErrAccountInUse) {
		t.Fatalf("opening account removal guard: %v", e)
	}
	release()
	if e = <-done; e != nil {
		t.Fatal(e)
	}
}
