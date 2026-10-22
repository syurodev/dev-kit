package agenthub

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

const tableAgentAccounts = "agent_accounts"

// SQLAccountStore implements AccountBook over the agent_accounts table
// (storage migration 45).
type SQLAccountStore struct{ db *sql.DB }

func NewSQLAccountStore(db *sql.DB) *SQLAccountStore { return &SQLAccountStore{db: db} }

var _ AccountBook = (*SQLAccountStore)(nil)

const accountColumns = `id, vendor, label, home_dir, signed_in, shared, exhausted_until, usage, usage_source, observed_at`

func scanAccount(scan func(dest ...any) error) (AccountRecord, error) {
	var a AccountRecord
	var vendor, usage string
	var signedIn, shared int
	var exhausted, observed int64
	if err := scan(&a.ID, &vendor, &a.Label, &a.HomeDir, &signedIn, &shared, &exhausted, &usage, &a.UsageSource, &observed); err != nil {
		return AccountRecord{}, err
	}
	a.Vendor = AgentType(vendor)
	a.SignedIn, a.Shared = signedIn != 0, shared != 0
	if exhausted > 0 {
		a.ExhaustedUntil = time.Unix(exhausted, 0)
	}
	if observed > 0 {
		a.ObservedAt = time.Unix(observed, 0)
	}
	if usage != "" {
		// A reading this build cannot read is no reading, not a failed list.
		_ = json.Unmarshal([]byte(usage), &a.Usage)
	}
	return a, nil
}

func (st *SQLAccountStore) ensureBuiltin(ctx context.Context, vendor AgentType) error {
	_, err := st.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO `+tableAgentAccounts+` (id, vendor, label, home_dir, signed_in, created_at)
		 VALUES (?, ?, ?, '', 1, ?)`,
		BuiltinAccountID(vendor), string(vendor), builtinAccountLabel(vendor), formatStoreTime(time.Now()))
	return err
}

func (st *SQLAccountStore) List(ctx context.Context, vendor AgentType) ([]AccountRecord, error) {
	if err := st.ensureBuiltin(ctx, vendor); err != nil {
		return nil, err
	}
	rows, err := st.db.QueryContext(ctx,
		`SELECT `+accountColumns+` FROM `+tableAgentAccounts+` WHERE vendor = ?
		 ORDER BY (home_dir = '') DESC, created_at ASC, id ASC`, string(vendor))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AccountRecord
	for rows.Next() {
		a, err := scanAccount(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (st *SQLAccountStore) Get(ctx context.Context, id string) (AccountRecord, error) {
	row := st.db.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM `+tableAgentAccounts+` WHERE id = ?`, id)
	a, err := scanAccount(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return AccountRecord{}, ErrAccountNotFound
	}
	return a, err
}

func (st *SQLAccountStore) Put(ctx context.Context, a AccountRecord) error {
	_, err := st.db.ExecContext(ctx,
		`INSERT INTO `+tableAgentAccounts+` (id, vendor, label, home_dir, signed_in, shared, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET label = excluded.label, home_dir = excluded.home_dir,
		   signed_in = excluded.signed_in, shared = excluded.shared`,
		a.ID, string(a.Vendor), a.Label, a.HomeDir, boolInt(a.SignedIn), boolInt(a.Shared), formatStoreTime(time.Now()))
	return err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (st *SQLAccountStore) SaveUsage(ctx context.Context, id string, windows []LimitWindow, source string, at time.Time) error {
	cur, err := st.Get(ctx, id)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(mergeWindows(cur.Usage, windows))
	if err != nil {
		return err
	}
	_, err = st.db.ExecContext(ctx,
		`UPDATE `+tableAgentAccounts+` SET usage = ?, usage_source = ?, observed_at = ? WHERE id = ?`,
		string(raw), source, at.Unix(), id)
	return err
}

func (st *SQLAccountStore) MarkExhausted(ctx context.Context, id string, until time.Time) error {
	if _, err := st.Get(ctx, id); err != nil {
		return err
	}
	_, err := st.db.ExecContext(ctx,
		`UPDATE `+tableAgentAccounts+` SET exhausted_until = ? WHERE id = ? AND exhausted_until < ?`,
		until.Unix(), id, until.Unix())
	return err
}
