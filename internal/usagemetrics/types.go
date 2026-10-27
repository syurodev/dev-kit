// Package usagemetrics measures host-served text, not vendor tokens. It is a
// stdlib-only leaf; observations contain values, never results or arguments.
package usagemetrics

import (
	"errors"
	"strconv"
	"time"
)

// Version is the measurement version new rows are written under. v2 records the
// algorithm that actually handled each result in the cut-mode key (CutNone …
// CutUnrecorded below). LegacyVersion rows were written before that: their
// cut-mode key is the literal "head" whatever the result went through, they stay
// exactly as stored and are never relabelled or backfilled. The byte counters mean
// the same in both versions.
const Version = "text-utf8-v2"

// LegacyVersion is the measurement version of rows written before cut modes were
// recorded.
const LegacyVersion = "text-utf8-v1"
const MaxKeys = 4096

// The cut modes a v2 row can carry: a fixed vocabulary, never a string a tool or a
// profile supplies.
const (
	CutNone       = "none"       // text within the budget (or no result): nothing was cut
	CutHead       = "head"       // leading bytes kept, then a marker
	CutHeadTail   = "headTail"   // both ends of plain text kept around a marker
	CutStructured = "structured" // arrays and strings of a JSON value trimmed
	CutExempt     = "exempt"     // a fixed exemption or a non-text result: never cut
	CutError      = "error"      // a tool error or a handler error: never cut
	// CutUnrecorded is a result whose producer did not say how it was handled (or
	// said something outside this list). It is never read as any of the above.
	CutUnrecorded = "unrecorded"
)

func knownCutMode(m string) bool {
	switch m {
	case CutNone, CutHead, CutHeadTail, CutStructured, CutExempt, CutError, CutUnrecorded:
		return true
	}
	return false
}

var ErrInvalidQuery = errors.New("usage metrics: invalid query")
var ErrUnavailable = errors.New("usage metrics: unavailable")

type Scope struct {
	Vendor           string
	WorkspaceID      string
	ProfileRevision  *int64
	OverrideRevision *int64
}
type Observation struct {
	Scope                                                       Scope
	At                                                          time.Time
	Tool                                                        string
	Result, Text, NonText, ToolError, HandlerError, Exempt, Cut bool
	Before, After                                               int64
	// CutMode is how the limiter actually handled a result, one of the Cut*
	// constants. Empty on an observation that has no result (it is then CutNone); an
	// empty or unknown value on a result is recorded as CutUnrecorded, never as a
	// guess.
	CutMode string
}
type Stats struct {
	Calls         int64 `json:"calls"`
	Results       int64 `json:"results"`
	TextCalls     int64 `json:"textCalls"`
	NonTextCalls  int64 `json:"nonTextCalls"`
	ToolErrors    int64 `json:"toolErrors"`
	HandlerErrors int64 `json:"handlerErrors"`
	ExemptCalls   int64 `json:"exemptCalls"`
	Cuts          int64 `json:"cuts"`
	BeforeBytes   int64 `json:"beforeBytes"`
	AfterBytes    int64 `json:"afterBytes"`
	MaxBefore     int64 `json:"maxBefore"`
	MaxAfter      int64 `json:"maxAfter"`
}
type Key struct {
	Day, Vendor, WorkspaceID, Tool, ProfileKey, OverrideKey, Version, CutMode string
}
type DailyRow struct {
	Key
	Stats
}
type ToolRow struct {
	Tool  string `json:"tool"`
	Stats Stats  `json:"stats"`
}
type Health struct {
	StartedAt     string `json:"startedAt"`
	SavedThrough  string `json:"savedThrough"`
	Dropped       int64  `json:"dropped"`
	WriteFailures int64  `json:"writeFailures"`
	UncleanRuns   int64  `json:"uncleanRuns"`
}
type Query struct {
	WorkspaceID *string `json:"workspaceId,omitempty"`
	From        string  `json:"from"`
	To          string  `json:"to"`
	Limit       int     `json:"limit"`
}
type Report struct {
	Vendor  string    `json:"vendor"`
	From    string    `json:"from"`
	To      string    `json:"to"`
	Rows    []ToolRow `json:"rows"`
	Health  Health    `json:"health"`
	HasMore bool      `json:"hasMore"`
}

func Window(q Query, now time.Time) (Query, error) {
	if q.To == "" {
		q.To = now.UTC().Format(time.DateOnly)
	}
	to, e := time.Parse(time.DateOnly, q.To)
	if e != nil {
		return q, ErrInvalidQuery
	}
	if q.From == "" {
		q.From = to.AddDate(0, 0, -29).Format(time.DateOnly)
	}
	from, e := time.Parse(time.DateOnly, q.From)
	if e != nil || to.Before(from) || to.Sub(from) > 89*24*time.Hour {
		return q, ErrInvalidQuery
	}
	if q.WorkspaceID != nil && !validID(*q.WorkspaceID, 128, true) {
		return q, ErrInvalidQuery
	}
	if q.Limit == 0 {
		q.Limit = 100
	}
	if q.Limit < 1 || q.Limit > 100 {
		return q, ErrInvalidQuery
	}
	return q, nil
}
func revisionKey(n *int64) string {
	if n == nil {
		return "none"
	}
	return "v:" + strconv.FormatInt(*n, 10)
}
func flag(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
func validID(s string, maxLen int, empty bool) bool {
	if len(s) > maxLen || (!empty && s == "") {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}
func rowOf(o Observation) (DailyRow, bool) {
	if !validID(o.Scope.Vendor, 64, false) || !validID(o.Scope.WorkspaceID, 128, true) || !validID(o.Tool, 128, false) || o.At.IsZero() || o.Before < 0 || o.After < 0 {
		return DailyRow{}, false
	}
	if o.Scope.ProfileRevision != nil && *o.Scope.ProfileRevision < 0 || o.Scope.OverrideRevision != nil && *o.Scope.OverrideRevision < 0 {
		return DailyRow{}, false
	}
	mode := o.CutMode
	switch {
	case !o.Result:
		mode = CutNone
	case !knownCutMode(mode) || mode == "":
		mode = CutUnrecorded
	}
	return DailyRow{Key: Key{o.At.UTC().Format(time.DateOnly), o.Scope.Vendor, o.Scope.WorkspaceID, o.Tool, revisionKey(o.Scope.ProfileRevision), revisionKey(o.Scope.OverrideRevision), Version, mode}, Stats: Stats{1, flag(o.Result), flag(o.Text), flag(o.NonText), flag(o.ToolError), flag(o.HandlerError), flag(o.Exempt), flag(o.Cut), o.Before, o.After, o.Before, o.After}}, true
}
func (s *Stats) Add(v Stats) {
	s.Calls += v.Calls
	s.Results += v.Results
	s.TextCalls += v.TextCalls
	s.NonTextCalls += v.NonTextCalls
	s.ToolErrors += v.ToolErrors
	s.HandlerErrors += v.HandlerErrors
	s.ExemptCalls += v.ExemptCalls
	s.Cuts += v.Cuts
	s.BeforeBytes += v.BeforeBytes
	s.AfterBytes += v.AfterBytes
	if v.MaxBefore > s.MaxBefore {
		s.MaxBefore = v.MaxBefore
	}
	if v.MaxAfter > s.MaxAfter {
		s.MaxAfter = v.MaxAfter
	}
}
