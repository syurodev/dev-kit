package usagemetrics

import (
	"context"
	"database/sql"
	"sort"
	"strconv"
	"strings"
	"time"
)

type SQLStore struct{ db *sql.DB }

func NewSQLStore(db *sql.DB) *SQLStore { return &SQLStore{db} }
func stamp(t time.Time) string         { return t.UTC().Format(time.RFC3339Nano) }
func (s *SQLStore) Start(ctx context.Context, id string, at time.Time) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `UPDATE agent_tool_usage_runs SET unclean=1 WHERE closed_at IS NULL`); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO agent_tool_usage_runs(id,started_at) VALUES(?,?)`, id, stamp(at)); e != nil {
		return e
	}
	return tx.Commit()
}
func revision(k string) any {
	if k == "none" {
		return nil
	}
	n, _ := strconv.ParseInt(strings.TrimPrefix(k, "v:"), 10, 64)
	return n
}
func (s *SQLStore) Commit(ctx context.Context, b Batch) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var seq int64
	if e = tx.QueryRowContext(ctx, `SELECT batch_seq FROM agent_tool_usage_runs WHERE id=?`, b.RunID).Scan(&seq); e != nil {
		return e
	}
	if seq >= b.Sequence {
		return nil
	}
	for _, r := range b.Rows {
		_, e = tx.ExecContext(ctx, `INSERT INTO agent_tool_usage_daily VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
 ON CONFLICT(day,vendor,workspace_key,tool,profile_key,override_key,version,cut_mode) DO UPDATE SET
 calls=calls+excluded.calls,results=results+excluded.results,text_calls=text_calls+excluded.text_calls,non_text_calls=non_text_calls+excluded.non_text_calls,
 tool_errors=tool_errors+excluded.tool_errors,handler_errors=handler_errors+excluded.handler_errors,exempt_calls=exempt_calls+excluded.exempt_calls,cuts=cuts+excluded.cuts,
 before_bytes=before_bytes+excluded.before_bytes,after_bytes=after_bytes+excluded.after_bytes,max_before=MAX(max_before,excluded.max_before),max_after=MAX(max_after,excluded.max_after)`,
			r.Day, r.Vendor, r.WorkspaceID, r.Tool, r.ProfileKey, r.OverrideKey, r.Version, r.CutMode, revision(r.ProfileKey), revision(r.OverrideKey), r.Calls, r.Results, r.TextCalls, r.NonTextCalls, r.ToolErrors, r.HandlerErrors, r.ExemptCalls, r.Cuts, r.BeforeBytes, r.AfterBytes, r.MaxBefore, r.MaxAfter)
		if e != nil {
			return e
		}
	}
	if _, e = tx.ExecContext(ctx, `UPDATE agent_tool_usage_runs SET batch_seq=?,last_flushed_at=?,dropped=?,write_failures=? WHERE id=?`, b.Sequence, stamp(b.At), b.Dropped, b.WriteFailures, b.RunID); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *SQLStore) Close(ctx context.Context, id string, at time.Time) error {
	_, e := s.db.ExecContext(ctx, `UPDATE agent_tool_usage_runs SET closed_at=? WHERE id=?`, stamp(at), id)
	return e
}
func (s *SQLStore) Report(ctx context.Context, vendor string, q Query) (Report, error) {
	q, e := Window(q, time.Now())
	if e != nil {
		return Report{}, e
	}
	rep := Report{Vendor: vendor, From: q.From, To: q.To, Rows: []ToolRow{}}
	// Aggregate in SQL; query and returned top list are bounded, never read per-call data.
	rows, e := s.db.QueryContext(ctx, `SELECT tool,SUM(calls),SUM(results),SUM(text_calls),SUM(non_text_calls),SUM(tool_errors),SUM(handler_errors),SUM(exempt_calls),SUM(cuts),SUM(before_bytes),SUM(after_bytes),MAX(max_before),MAX(max_after) FROM agent_tool_usage_daily WHERE vendor=? AND day>=? AND day<=? AND (? IS NULL OR workspace_key=?) GROUP BY tool ORDER BY SUM(before_bytes) DESC,tool LIMIT ?`, vendor, q.From, q.To, q.WorkspaceID, q.WorkspaceID, q.Limit+1)
	if e != nil {
		return rep, e
	}
	for rows.Next() {
		var r ToolRow
		v := &r.Stats
		if e = rows.Scan(&r.Tool, &v.Calls, &v.Results, &v.TextCalls, &v.NonTextCalls, &v.ToolErrors, &v.HandlerErrors, &v.ExemptCalls, &v.Cuts, &v.BeforeBytes, &v.AfterBytes, &v.MaxBefore, &v.MaxAfter); e != nil {
			rows.Close()
			return rep, e
		}
		rep.Rows = append(rep.Rows, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return rep, e
	}
	if len(rep.Rows) > q.Limit {
		rep.HasMore = true
		rep.Rows = rep.Rows[:q.Limit]
	}
	var start, last sql.NullString
	e = s.db.QueryRowContext(ctx, `SELECT MIN(started_at),MAX(last_flushed_at),COALESCE(SUM(dropped),0),COALESCE(SUM(write_failures),0),COALESCE(SUM(unclean),0) FROM agent_tool_usage_runs`).Scan(&start, &last, &rep.Health.Dropped, &rep.Health.WriteFailures, &rep.Health.UncleanRuns)
	rep.Health.StartedAt = start.String
	rep.Health.SavedThrough = last.String
	// Stable even when a driver changes its row return order.
	sort.SliceStable(rep.Rows, func(i, j int) bool {
		if rep.Rows[i].Stats.BeforeBytes != rep.Rows[j].Stats.BeforeBytes {
			return rep.Rows[i].Stats.BeforeBytes > rep.Rows[j].Stats.BeforeBytes
		}
		return rep.Rows[i].Tool < rep.Rows[j].Tool
	})
	return rep, e
}
