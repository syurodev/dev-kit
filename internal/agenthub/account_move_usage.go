package agenthub

import (
	"context"
	"database/sql"
	"devkit/internal/usagemetrics"
	"encoding/base64"
	"encoding/json"
	"time"
)

type AccountMoveQuery struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Limit  int    `json:"limit"`
	Cursor string `json:"cursor"`
}
type AccountMoveUsage struct {
	ID            string `json:"id"`
	SessionID     string `json:"sessionId"`
	Seq           int64  `json:"seq"`
	At            string `json:"at"`
	From          string `json:"from"`
	To            string `json:"to"`
	ToGeneration  string `json:"toGeneration"`
	Status        string `json:"status"`
	TurnID        string `json:"turnId"`
	TurnStartedAt string `json:"turnStartedAt"`
	CacheWrite    *int   `json:"cacheWrite"`
	// Host-only projected provenance, never vendor text.
	kind   string
	opened *bool
	nextAt string
}
type AccountMoveReport struct {
	Vendor     string             `json:"vendor"`
	From       string             `json:"from"`
	To         string             `json:"to"`
	Rows       []AccountMoveUsage `json:"rows"`
	NextCursor string             `json:"nextCursor"`
}
type moveCursor struct{ Vendor, From, To, At, ID string }
type AccountMoveSource interface {
	AccountMoveUsageRows(context.Context, AgentType, AccountMoveQuery) (AccountMoveReport, error)
}

func (s *Service) AccountMoveUsageReport(ctx context.Context, vendor AgentType, q AccountMoveQuery) (AccountMoveReport, error) {
	if e := requireWindow(ctx); e != nil {
		return AccountMoveReport{}, e
	}
	if _, ok := s.reg.Get(vendor); !ok {
		return AccountMoveReport{}, ErrVendorUnknown
	}
	source, ok := s.store.(AccountMoveSource)
	if !ok {
		return AccountMoveReport{}, usagemetrics.ErrUnavailable
	}
	return source.AccountMoveUsageRows(ctx, vendor, q)
}

const noticeJSON = `CASE WHEN json_valid(t.payload) THEN t.payload ELSE '{}' END`
const noticeKind = `json_extract(` + noticeJSON + `,'$.accountNotice.kind')`

func (st *SQLStore) AccountMoveUsageRows(ctx context.Context, vendor AgentType, q AccountMoveQuery) (AccountMoveReport, error) {
	window, e := usagemetrics.Window(usagemetrics.Query{From: q.From, To: q.To, Limit: q.Limit}, time.Now())
	if e != nil {
		return AccountMoveReport{}, e
	}
	q.From, q.To, q.Limit = window.From, window.To, window.Limit
	rep := AccountMoveReport{Vendor: string(vendor), From: q.From, To: q.To, Rows: []AccountMoveUsage{}}
	cursor := moveCursor{}
	if q.Cursor != "" {
		if len(q.Cursor) > 2048 {
			return rep, usagemetrics.ErrInvalidQuery
		}
		raw, err := base64.RawURLEncoding.DecodeString(q.Cursor)
		if err != nil || json.Unmarshal(raw, &cursor) != nil || cursor.Vendor != string(vendor) || cursor.From != q.From || cursor.To != q.To || cursor.ID == "" {
			return rep, usagemetrics.ErrInvalidQuery
		}
		if _, err = time.Parse(time.RFC3339Nano, cursor.At); err != nil {
			return rep, usagemetrics.ErrInvalidQuery
		}
	}
	upper, _ := time.Parse(time.DateOnly, q.To)
	upper = upper.AddDate(0, 0, 1)
	// Only scalar fields are projected. Reasons/raw transcript bodies never leave SQL.
	rows, e := st.db.QueryContext(ctx, `SELECT t.id,t.session_id,t.seq,t.created_at,`+noticeKind+`,
 json_extract(`+noticeJSON+`,'$.accountNotice.from'),json_extract(`+noticeJSON+`,'$.accountNotice.to'),
 json_extract(`+noticeJSON+`,'$.accountNotice.toGeneration'),json_extract(`+noticeJSON+`,'$.accountNotice.runtimeOpened'),
 (SELECT n.created_at FROM agent_transcript n WHERE n.session_id=t.session_id AND n.kind='session_info_update'
 AND json_extract(CASE WHEN json_valid(n.payload) THEN n.payload ELSE '{}' END,'$.accountNotice.kind') IN ('switch','failed')
 AND n.seq>t.seq ORDER BY n.seq LIMIT 1)
 FROM agent_transcript t JOIN agent_sessions s ON s.id=t.session_id
 WHERE s.agent_type=? AND t.kind='session_info_update' AND `+noticeKind+` IN ('switch','failed')
 AND t.created_at>=? AND t.created_at<? AND (?='' OR julianday(t.created_at)<julianday(?) OR (julianday(t.created_at)=julianday(?) AND t.id<?))
 ORDER BY julianday(t.created_at) DESC,t.id DESC LIMIT ?`, string(vendor), q.From+"T00:00:00", upper.Format(time.DateOnly)+"T00:00:00", cursor.At, cursor.At, cursor.At, cursor.ID, q.Limit+1)
	if e != nil {
		return rep, e
	}
	for rows.Next() {
		var m AccountMoveUsage
		var from, to, gen, next sql.NullString
		var opened sql.NullBool
		if e = rows.Scan(&m.ID, &m.SessionID, &m.Seq, &m.At, &m.kind, &from, &to, &gen, &opened, &next); e != nil {
			rows.Close()
			return rep, e
		}
		m.From, m.To, m.ToGeneration, m.nextAt = from.String, to.String, gen.String, next.String
		if opened.Valid {
			v := opened.Bool
			m.opened = &v
		}
		rep.Rows = append(rep.Rows, m)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return rep, e
	}
	if len(rep.Rows) > q.Limit {
		rep.Rows = rep.Rows[:q.Limit]
		last := rep.Rows[len(rep.Rows)-1]
		raw, _ := json.Marshal(moveCursor{string(vendor), q.From, q.To, last.At, last.ID})
		rep.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	// One bounded candidate query for the page, with at most two turns per notice.
	if len(rep.Rows) > 0 {
		query := `WITH moves(id,sid,at,next_at) AS (VALUES `
		args := []any{}
		for i, m := range rep.Rows {
			if i > 0 {
				query += ","
			}
			query += "(?,?,?,?)"
			args = append(args, m.ID, m.SessionID, m.At, m.nextAt)
		}
		query += `) , candidates AS (SELECT m.id mid,a.id,a.account_id,a.account_generation,a.started_at,a.usage_json,a.raw_meta,
 ROW_NUMBER() OVER(PARTITION BY m.id ORDER BY julianday(a.started_at),a.id) rn
 FROM moves m JOIN agent_turns a ON a.session_id=m.sid AND a.usage_context_version=1
 AND julianday(a.started_at)>=julianday(m.at) AND (m.next_at='' OR julianday(a.started_at)<julianday(m.next_at)))
 SELECT mid,id,account_id,account_generation,started_at,usage_json,raw_meta FROM candidates WHERE rn<=2 ORDER BY mid,rn`
		rs, err := st.db.QueryContext(ctx, query, args...)
		if err != nil {
			return rep, err
		}
		candidates := map[string][]moveCandidate{}
		for rs.Next() {
			var mid string
			var c moveCandidate
			var account, gen, start, usage, raw sql.NullString
			if err = rs.Scan(&mid, &c.ID, &account, &gen, &start, &usage, &raw); err != nil {
				rs.Close()
				return rep, err
			}
			c.Account, c.Generation, c.StartedAt = account.String, gen.String, start.String
			c.UsageJSON = []byte(usage.String)
			c.RawMeta = []byte(raw.String)
			candidates[mid] = append(candidates[mid], c)
		}
		err = rs.Err()
		rs.Close()
		if err != nil {
			return rep, err
		}
		// Only Claude has token accounting in this phase; another vendor stays unknown.
		var adapter VendorAdapter = NewServerModeAdapter(vendor)
		if vendor == AgentTypeClaude {
			adapter = NewClaudeAdapter(ClaudeOptions{})
		}
		for i := range rep.Rows {
			correlateMove(&rep.Rows[i], candidates[rep.Rows[i].ID], adapter)
		}
	}
	return rep, nil
}

type moveCandidate struct {
	ID, Account, Generation, StartedAt string
	UsageJSON, RawMeta                 []byte
}

func correlateMove(m *AccountMoveUsage, candidates []moveCandidate, adapter VendorAdapter) {
	m.Status = "legacy_unverified"
	if m.kind == "failed" {
		m.Status = "failed"
		return
	}
	if m.opened == nil {
		return
	}
	if !*m.opened {
		m.Status = "binding_only"
		return
	}
	if m.ToGeneration == "" {
		return
	}
	m.Status = "no_accounting"
	if len(candidates) == 0 {
		return
	}
	c := candidates[0]
	at, e := time.Parse(time.RFC3339Nano, m.At)
	start, se := time.Parse(time.RFC3339Nano, c.StartedAt)
	if e != nil || se != nil || !start.After(at) || c.Account != m.To || c.Generation != m.ToGeneration {
		m.Status = "ambiguous"
		return
	}
	if m.nextAt != "" {
		next, ne := time.Parse(time.RFC3339Nano, m.nextAt)
		if ne != nil || !next.After(at) || !start.Before(next) {
			m.Status = "ambiguous"
			return
		}
	}
	if len(candidates) > 1 {
		other, err := time.Parse(time.RFC3339Nano, candidates[1].StartedAt)
		// SQLite julianday orders with millisecond precision. Multiple starts
		// inside that bucket cannot prove which was first, even if their full
		// RFC3339 nanoseconds differ; the SQL id tie-break is not chronology.
		if err != nil || !other.After(start) || other.Sub(start) < time.Millisecond {
			m.Status = "ambiguous"
			return
		}
	}
	m.TurnID = c.ID
	m.TurnStartedAt = c.StartedAt
	m.CacheWrite = DeriveTurnUsage(adapter, c.UsageJSON, c.RawMeta).CacheWrite
	if m.CacheWrite != nil {
		m.Status = "matched"
	}
}
