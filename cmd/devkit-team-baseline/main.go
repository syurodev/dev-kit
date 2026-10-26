// Command devkit-team-baseline reports privacy-preserving usage coverage from
// an existing DevKit SQLite database. It never opens the database for writes.
package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"devkit/internal/agenthub"
	_ "modernc.org/sqlite"
)

// semanticsVersion identifies how a token counter is read. Version 2 reads each
// turn with agenthub.DeriveTurnUsage, the derivation the Agent Hub usage report
// uses, instead of the allowlisted stored counters of version 1.
const semanticsVersion = 2

var counterFields = []string{"inputTokens", "outputTokens", "totalTokens", "cachedInputTokens", "cachedWriteTokens", "reasoningOutputTokens"}

type fieldTotal struct {
	KnownRows   int   `json:"knownRows"`
	UnknownRows int   `json:"unknownRows"`
	InvalidRows int   `json:"invalidRows"`
	Sum         int64 `json:"sum,omitempty"`
}

type vendorUsage struct {
	Sessions          int `json:"sessions"`
	Turns             int `json:"turns"`
	TurnsWithUsage    int `json:"turnsWithUsage"`
	TurnsWithoutUsage int `json:"turnsWithoutUsage"`
	// TokenCoverage is "claude" where tokens are read and "unknown" for a vendor
	// whose token accounting is not covered; its turns count as without usage.
	TokenCoverage string `json:"tokenCoverage"`
	// PartialSplitTurns counts turns whose per-model split is incomplete or
	// differs from the turn total. A mismatch alone does not say why.
	PartialSplitTurns int                   `json:"partialSplitTurns"`
	Fields            map[string]fieldTotal `json:"fields"`
}

type unknownMetric struct {
	Value  string `json:"value"`
	Reason string `json:"reason"`
}

type report struct {
	Source string `json:"source"`
	Scope  struct {
		SessionIDs []string `json:"sessionIds,omitempty"`
		From       string   `json:"from,omitempty"`
		To         string   `json:"to,omitempty"`
	} `json:"scope"`
	Sessions    int    `json:"sessions"`
	Turns       int    `json:"turns"`
	UsageSource string `json:"usageSource"`
	// SemanticsVersion is 2 since the report reads usage with
	// agenthub.DeriveTurnUsage; version 1 reports are not comparable.
	SemanticsVersion int                      `json:"semanticsVersion"`
	Vendors          map[string]*vendorUsage  `json:"vendors"`
	Unknown          map[string]unknownMetric `json:"unknown"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "devkit-team-baseline:", err)
		os.Exit(2)
	}
}

func run(args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("devkit-team-baseline", flag.ContinueOnError)
	fs.SetOutput(errOut)
	dbPath := fs.String("db", "", "existing DevKit SQLite database path (required)")
	var sessions stringList
	fs.Var(&sessions, "session", "session ID to include (repeatable; default: all sessions)")
	from := fs.String("from", "", "include turns at or after RFC3339 timestamp")
	to := fs.String("to", "", "include turns before RFC3339 timestamp")
	if e := fs.Parse(args); e != nil {
		return e
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if strings.TrimSpace(*dbPath) == "" {
		return errors.New("--db is required")
	}
	fromTime, e := parseBound(*from)
	if e != nil {
		return fmt.Errorf("--from: %w", e)
	}
	toTime, e := parseBound(*to)
	if e != nil {
		return fmt.Errorf("--to: %w", e)
	}
	if fromTime != nil && toTime != nil && !fromTime.Before(*toTime) {
		return errors.New("--from must be earlier than --to")
	}

	abs, e := filepath.Abs(*dbPath)
	if e != nil {
		return e
	}
	info, e := os.Stat(abs)
	if e != nil {
		return fmt.Errorf("stat database: %w", e)
	}
	if !info.Mode().IsRegular() {
		return errors.New("database path must be a regular file")
	}
	uri := (&url.URL{Scheme: "file", Path: abs, RawQuery: "mode=ro"}).String()
	db, e := sql.Open("sqlite", uri)
	if e != nil {
		return e
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if e = db.Ping(); e != nil {
		return fmt.Errorf("open read-only database: %w", e)
	}

	r, e := collect(db, sessions, fromTime, toTime)
	if e != nil {
		return e
	}
	r.Source = "DevKit local SQLite agent_sessions + agent_turns"
	r.SemanticsVersion = semanticsVersion
	r.UsageSource = "agenthub.DeriveTurnUsage over agent_turns.usage_json and raw_meta (Claude only; other vendors unknown); raw metadata is normalized in process and never emitted"
	r.Scope.SessionIDs = sessions
	if fromTime != nil {
		r.Scope.From = fromTime.Format(time.RFC3339Nano)
	}
	if toTime != nil {
		r.Scope.To = toTime.Format(time.RFC3339Nano)
	}
	r.Unknown = map[string]unknownMetric{
		"coordinatorWakeups":           {Value: "unknown", Reason: "no durable coordinator wake-event counter is available in the inspected local schema; no wake reason is inferred"},
		"coordinatorContextPerRequest": {Value: "unknown", Reason: "the stored normalized usage does not measure coordinator context/request size"},
		"model":                        {Value: "unknown", Reason: "model identifiers are not included in the allowlisted normalized usage fields"},
		"cacheCounterSemantics":        {Value: "unknown", Reason: "vendors may define input/cache counters differently; counters are kept separate without reconstruction"},
		"cumulativeOrPerTurn":          {Value: "unknown", Reason: "the local normalized row does not carry a uniform vendor counter-scope marker"},
		"tokenSavings":                 {Value: "unknown", Reason: "no equivalent clean-session pilot comparison was provided; no savings are calculated"},
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error {
	if strings.TrimSpace(v) == "" {
		return errors.New("session ID cannot be empty")
	}
	*s = append(*s, v)
	return nil
}

func parseBound(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, e := time.Parse(time.RFC3339Nano, s)
	if e != nil {
		return nil, e
	}
	t = t.UTC()
	return &t, nil
}

func collect(db *sql.DB, sessionIDs []string, from, to *time.Time) (report, error) {
	var r report
	r.Vendors = make(map[string]*vendorUsage)
	var exists int
	if e := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('agent_sessions','agent_turns')`).Scan(&exists); e != nil {
		return r, e
	}
	if exists != 2 {
		return r, errors.New("database is missing agent_sessions or agent_turns")
	}
	query := `SELECT s.id, s.agent_type, t.id, t.usage_json, t.raw_meta, t.created_at
		FROM agent_sessions s LEFT JOIN agent_turns t ON t.session_id=s.id`
	where := []string{}
	args := []any{}
	if len(sessionIDs) > 0 {
		marks := make([]string, len(sessionIDs))
		for i, id := range sessionIDs {
			marks[i] = "?"
			args = append(args, id)
		}
		where = append(where, "s.id IN ("+strings.Join(marks, ",")+")")
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY s.agent_type, s.id, t.created_at, t.id"
	rows, e := db.Query(query, args...)
	if e != nil {
		return r, fmt.Errorf("query local session/turn rows: %w", e)
	}
	defer rows.Close()
	seenSessions := map[string]bool{}
	for rows.Next() {
		var sid, vendor string
		var turnID, usageJSON, rawMeta, createdAt sql.NullString
		if e = rows.Scan(&sid, &vendor, &turnID, &usageJSON, &rawMeta, &createdAt); e != nil {
			return r, e
		}
		if !seenSessions[sid] {
			seenSessions[sid] = true
			r.Sessions++
			if vendor == "" {
				vendor = "unknown"
			}
			v := getVendor(r.Vendors, vendor)
			v.Sessions++
		}
		if !turnID.Valid {
			continue
		}
		created, parseErr := time.Parse(time.RFC3339Nano, createdAt.String)
		if parseErr != nil {
			return r, errors.New("selected turn has an invalid created_at timestamp")
		}
		if from != nil && created.Before(*from) || to != nil && !created.Before(*to) {
			continue
		}
		r.Turns++
		if vendor == "" {
			vendor = "unknown"
		}
		v := getVendor(r.Vendors, vendor)
		v.Turns++
		usage := turnUsage(vendor, usageJSON, rawMeta)
		if usage == nil {
			v.TurnsWithoutUsage++
			continue
		}
		v.TurnsWithUsage++
		if usage.SplitPartial {
			v.PartialSplitTurns++
		}
		for _, f := range counterFields {
			current := v.Fields[f]
			if value := counterOf(*usage, f); value == nil {
				current.UnknownRows++
			} else if int64(*value) > 0 && current.Sum > int64(^uint64(0)>>1)-int64(*value) {
				current.UnknownRows++
				current.InvalidRows++
			} else {
				current.KnownRows++
				current.Sum += int64(*value)
			}
			v.Fields[f] = current
		}
	}
	if e = rows.Err(); e != nil {
		return r, e
	}
	for _, v := range r.Vendors {
		for _, f := range counterFields {
			x := v.Fields[f]
			x.UnknownRows += v.TurnsWithoutUsage
			v.Fields[f] = x
		}
	}
	return r, nil
}

func getVendor(vendors map[string]*vendorUsage, name string) *vendorUsage {
	v := vendors[name]
	if v == nil {
		v = &vendorUsage{Fields: make(map[string]fieldTotal), TokenCoverage: "unknown"}
		if name == string(agenthub.AgentTypeClaude) {
			v.TokenCoverage = name
		}
		for _, f := range counterFields {
			v.Fields[f] = fieldTotal{}
		}
		vendors[name] = v
	}
	return v
}

// turnUsage derives one turn's usage with the Agent Hub's own derivation, so
// this report and the Settings report agree on what is known. Token accounting
// is Claude-only: any other vendor, an unusable raw meta and a vendor that
// reported nothing all return nil, which is "unknown", never zero. The raw meta
// is normalized in process and no part of it reaches the report.
func turnUsage(vendor string, usageJSON, rawMeta sql.NullString) *agenthub.TurnUsage {
	if vendor != string(agenthub.AgentTypeClaude) {
		return nil
	}
	var usage, meta json.RawMessage
	if usageJSON.Valid {
		usage = json.RawMessage(usageJSON.String)
	}
	if rawMeta.Valid {
		meta = json.RawMessage(rawMeta.String)
	}
	u := agenthub.DeriveTurnUsage(claudeAdapter, usage, meta)
	if !u.Known() {
		return nil
	}
	return &u
}

var claudeAdapter = agenthub.NewClaudeAdapter(agenthub.ClaudeOptions{})

func counterOf(u agenthub.TurnUsage, field string) *int {
	switch field {
	case "inputTokens":
		return u.FreshInput
	case "outputTokens":
		return u.Output
	case "totalTokens":
		return u.Total
	case "cachedInputTokens":
		return u.CacheRead
	case "cachedWriteTokens":
		return u.CacheWrite
	case "reasoningOutputTokens":
		return u.Reasoning
	}
	return nil
}
