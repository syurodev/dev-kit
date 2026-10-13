package codegraph

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Pages exist for agents: an index of a few hundred services answers
// "list the entry points" with a result bigger than the agent can take, and a
// result the agent cannot take is a result it never sees. The UI keeps reading
// the whole list through ListEntryPoints / SearchSymbol; only the MCP tools
// page.

const (
	// DefaultEntryPointPage and MaxEntryPointPage bound one page of entry
	// points (~250 bytes a row).
	DefaultEntryPointPage = 100
	MaxEntryPointPage     = 300
	// DefaultSymbolPage and MaxSymbolPage bound one page of symbol matches
	// (~300 bytes a row).
	DefaultSymbolPage = 50
	MaxSymbolPage     = 200

	maxCursorLen = 2048
)

// ErrInvalidCursor is a cursor that is malformed, was issued for another
// query, or points at a row that no longer exists (the project re-indexed
// between pages). The fix is always the same: start again without a cursor.
var ErrInvalidCursor = errors.New("codegraph: invalid cursor — repeat the query without a cursor to start over")

// Symbol match modes for SymbolQuery.Match.
const (
	MatchContains = "contains" // the label contains the query (default)
	MatchPrefix   = "prefix"   // a dot-separated segment of the label starts with the query
	MatchExact    = "exact"    // the label, or its last segment, equals the query
)

// EntryPointQuery narrows and pages ListEntryPoints. Every filter is optional
// and they combine with AND.
type EntryPointQuery struct {
	Kind       string // http, message, plugin… (exact)
	Service    string // the entry point's Service (exact)
	Verb       string // GET, POST, ACCESS… (exact, case-insensitive)
	PathPrefix string // Target starts with this
	Contains   string // Target or Owner contains this, case-insensitive
	Limit      int    // 0 means DefaultEntryPointPage
	Cursor     string // NextCursor of the previous page
}

// EntryPointPage is one page of entry points. Total counts every match of the
// filters, not just this page; NextCursor is empty on the last page.
type EntryPointPage struct {
	Result     []EntryPoint `json:"result"`
	Total      int          `json:"total"`
	NextCursor string       `json:"nextCursor,omitempty"`
}

// SymbolQuery narrows and pages SearchSymbol. Query is required.
type SymbolQuery struct {
	Query      string
	Match      string // MatchContains (default), MatchPrefix or MatchExact
	Kind       string // class | method
	Layer      string // controller | service | repository | unknown
	PathPrefix string // the symbol's file path starts with this
	Limit      int    // 0 means DefaultSymbolPage
	Cursor     string // NextCursor of the previous page
}

// SymbolPage is one page of symbol matches, shaped like EntryPointPage.
type SymbolPage struct {
	Result     []SymbolMatch `json:"result"`
	Total      int           `json:"total"`
	NextCursor string        `json:"nextCursor,omitempty"`
}

// pageCursor is what a cursor says: which query it belongs to (Binding) and
// the last row served. After is a row id for entry points; Label+ID is the
// keyset position for symbols.
type pageCursor struct {
	Version int    `json:"v"`
	Binding string `json:"b"`
	After   string `json:"a,omitempty"`
	Label   string `json:"l,omitempty"`
}

func cursorBinding(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:8])
}

func encodePageCursor(c pageCursor) string {
	c.Version = 1
	data, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(data)
}

// decodePageCursor returns the zero cursor for "", and ErrInvalidCursor for
// anything that is not a version-1 cursor issued for binding.
func decodePageCursor(text, binding string) (pageCursor, error) {
	if text == "" {
		return pageCursor{}, nil
	}
	if len(text) > maxCursorLen {
		return pageCursor{}, ErrInvalidCursor
	}
	data, err := base64.RawURLEncoding.DecodeString(text)
	if err != nil {
		return pageCursor{}, ErrInvalidCursor
	}
	var c pageCursor
	if err := json.Unmarshal(data, &c); err != nil || c.Version != 1 || c.Binding != binding {
		return pageCursor{}, ErrInvalidCursor
	}
	return c, nil
}

func clampLimit(limit, def, max int) (int, error) {
	if limit < 0 {
		return 0, fmt.Errorf("codegraph: limit must be positive (got %d)", limit)
	}
	if limit == 0 {
		return def, nil
	}
	return min(limit, max), nil
}

func (q EntryPointQuery) binding() string {
	return cursorBinding("entrypoints", q.Kind, q.Service, strings.ToUpper(q.Verb), q.PathPrefix, strings.ToLower(q.Contains))
}

func (q EntryPointQuery) matches(ep EntryPoint) bool {
	if q.Kind != "" && ep.Kind != q.Kind {
		return false
	}
	if q.Service != "" && ep.Service != q.Service {
		return false
	}
	if q.Verb != "" && !strings.EqualFold(ep.Verb, q.Verb) {
		return false
	}
	if q.PathPrefix != "" && !strings.HasPrefix(ep.Target, q.PathPrefix) {
		return false
	}
	if q.Contains != "" {
		needle := strings.ToLower(q.Contains)
		if !strings.Contains(strings.ToLower(ep.Target+" "+ep.RouteID+" "+ep.Topic+" "+ep.ConsumerGroup), needle) && !strings.Contains(strings.ToLower(ep.Owner), needle) {
			return false
		}
	}
	return true
}

// PageEntryPoints filters entries (already completed and sorted, as
// ListEntryPoints returns them) and cuts one page. The cursor names the last
// row served, so a page boundary survives rows being added or removed around
// it; only the loss of that very row invalidates it.
func PageEntryPoints(entries []EntryPoint, q EntryPointQuery) (EntryPointPage, error) {
	limit, err := clampLimit(q.Limit, DefaultEntryPointPage, MaxEntryPointPage)
	if err != nil {
		return EntryPointPage{}, err
	}
	binding := q.binding()
	cursor, err := decodePageCursor(q.Cursor, binding)
	if err != nil {
		return EntryPointPage{}, err
	}

	matched := make([]EntryPoint, 0, len(entries))
	for _, ep := range entries {
		if q.matches(ep) {
			matched = append(matched, ep)
		}
	}

	start := 0
	if cursor.After != "" {
		start = -1
		for i, ep := range matched {
			if ep.ID == cursor.After {
				start = i + 1
				break
			}
		}
		if start < 0 {
			return EntryPointPage{}, ErrInvalidCursor
		}
	}

	end := min(start+limit, len(matched))
	page := EntryPointPage{Result: matched[start:end], Total: len(matched)}
	if page.Result == nil {
		page.Result = []EntryPoint{}
	}
	if end < len(matched) {
		page.NextCursor = encodePageCursor(pageCursor{Binding: binding, After: matched[end-1].ID})
	}
	return page, nil
}

func (q SymbolQuery) binding() string {
	return cursorBinding("symbols", q.Query, q.Match, q.Kind, q.Layer, q.PathPrefix)
}

// normalize validates q and fills the defaults, returning the page size.
func (q *SymbolQuery) normalize() (int, error) {
	switch q.Match {
	case "":
		q.Match = MatchContains
	case MatchContains, MatchPrefix, MatchExact:
	default:
		return 0, fmt.Errorf("codegraph: match must be %q, %q or %q (got %q)", MatchContains, MatchPrefix, MatchExact, q.Match)
	}
	return clampLimit(q.Limit, DefaultSymbolPage, MaxSymbolPage)
}
