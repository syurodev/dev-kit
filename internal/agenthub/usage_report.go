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
	Attribution *TurnAttribution
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

// UsageAccountGroup is one local account configuration lifetime, or unknown.
type UsageAccountGroup struct {
	AccountID  string
	Generation string
	UsageTotals
}

// UsageReport is what a vendor's turns spent, in tokens. Tokens are not a share
// of a subscription limit: how a limit counts cached reads is not known.
type UsageReport struct {
	ByAccount   []UsageAccountGroup
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
	if !u.Known() {
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
	byAccount := map[struct{ id, generation string }]*UsageTotals{}
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
		key := struct{ id, generation string }{}
		if row.Attribution != nil {
			key.id = row.Attribution.Facts.AccountID
			key.generation = row.Attribution.Facts.AccountGeneration
		}
		if byAccount[key] == nil {
			byAccount[key] = &UsageTotals{}
		}
		byAccount[key].add(u)
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
	rep.ByAccount = make([]UsageAccountGroup, 0, len(byAccount))
	for k, v := range byAccount {
		rep.ByAccount = append(rep.ByAccount, UsageAccountGroup{k.id, k.generation, *v})
	}
	sort.Slice(rep.ByAccount, func(i, j int) bool {
		a, b := rep.ByAccount[i], rep.ByAccount[j]
		if a.Total != b.Total {
			return a.Total > b.Total
		}
		if a.AccountID != b.AccountID {
			return a.AccountID < b.AccountID
		}
		return a.Generation < b.Generation
	})
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
