package agenthub

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const tableAgentAccounts = "agent_accounts"

// SQLAccountStore implements AccountBook over the agent_accounts table
// (storage migrations 45 and 46).
type SQLAccountStore struct{ db *sql.DB }

func NewSQLAccountStore(db *sql.DB) *SQLAccountStore { return &SQLAccountStore{db: db} }

var _ AccountBook = (*SQLAccountStore)(nil)

const accountColumns = `id, vendor, label, home_dir, signed_in, shared, exhausted_until, cooldown_until, usage, usage_source, observed_at, usage_generation`

// scanAccount reads one row of accountColumns. Migration 45 stores
// exhausted_until and observed_at as Unix seconds, while created_at is an
// RFC3339 string.
func scanAccount(scan func(dest ...any) error) (AccountRecord, error) {
	var a AccountRecord
	var generation sql.NullString
	var vendor, usage string
	var signedIn, shared int
	var exhausted, cooldown, observed int64
	if err := scan(&a.ID, &vendor, &a.Label, &a.HomeDir, &signedIn, &shared, &exhausted, &cooldown, &usage, &a.UsageSource, &observed, &generation); err != nil {
		return AccountRecord{}, err
	}
	a.UsageGeneration = generation.String
	a.Vendor = AgentType(vendor)
	a.SignedIn, a.Shared = signedIn != 0, shared != 0
	if exhausted > 0 {
		a.ExhaustedUntil = time.Unix(exhausted, 0)
	}
	if cooldown > 0 {
		a.CooldownUntil = time.Unix(cooldown, 0)
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

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (st *SQLAccountStore) ensureBuiltin(ctx context.Context, vendor AgentType) error {
	_, err := st.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO `+tableAgentAccounts+` (id, vendor, label, home_dir, signed_in, created_at, usage_generation)
		 VALUES (?, ?, ?, '', 1, ?, 'builtin')`,
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
	switch {
	case a.ID == "":
		return fmt.Errorf("%w: an account needs an id", ErrInvalidAccount)
	case a.Vendor == "":
		return fmt.Errorf("%w: an account needs a vendor", ErrInvalidAccount)
	case a.ID == BuiltinAccountID(a.Vendor) && a.HomeDir != "":
		return fmt.Errorf("%w: the built-in account has no home folder", ErrInvalidAccount)
	case a.ID != BuiltinAccountID(a.Vendor) && a.HomeDir == "":
		return fmt.Errorf("%w: only the built-in account may have no home folder", ErrInvalidAccount)
	}
	generation := uuid.NewString()
	if a.Builtin() {
		generation = "builtin"
	}
	_, err := st.db.ExecContext(ctx,
		`INSERT INTO `+tableAgentAccounts+` (id, vendor, label, home_dir, signed_in, shared, created_at, usage_generation)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET label = excluded.label, home_dir = excluded.home_dir,
		   shared = excluded.shared`,
		a.ID, string(a.Vendor), a.Label, a.HomeDir, boolInt(a.SignedIn), boolInt(a.Shared), formatStoreTime(time.Now()), generation)
	return err
}

// SaveUsage reads, merges and writes the usage in one transaction (writing nothing
// when every fresh window is stale, so observed_at is not refreshed by it): sessions of
// one account report concurrently, each with a different window, and a
// read-merge-write across separate statements would lose one of them.
func (st *SQLAccountStore) SaveUsage(ctx context.Context, id string, windows []LimitWindow, source string, at time.Time) error {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // a no-op once committed
	var stored string
	if err := tx.QueryRowContext(ctx, `SELECT usage FROM `+tableAgentAccounts+` WHERE id = ?`, id).Scan(&stored); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrAccountNotFound
		}
		return err
	}
	var cur []LimitWindow
	if stored != "" {
		// A reading this build cannot read is no reading, as in scanAccount.
		_ = json.Unmarshal([]byte(stored), &cur)
	}
	merged, accepted := mergeWindowsReport(cur, windows)
	if !accepted {
		// Every window was a stale reading of one already stored: keep the
		// stored usage and its time, or a stale reading would look newer.
		return nil
	}
	raw, err := json.Marshal(merged)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE `+tableAgentAccounts+` SET usage = ?, usage_source = ?, observed_at = ? WHERE id = ?`,
		string(raw), source, at.Unix(), id); err != nil {
		return err
	}
	return tx.Commit()
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

func (st *SQLAccountStore) Delete(ctx context.Context, id string) error {
	acct, err := st.Get(ctx, id)
	if err != nil {
		return err
	}
	if acct.Builtin() {
		return fmt.Errorf("%w: the built-in account cannot be removed", ErrInvalidAccount)
	}
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // a no-op once committed
	if _, err := tx.ExecContext(ctx, `UPDATE `+tableAgentSessions+` SET agent_account_id = '' WHERE agent_account_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM `+tableAgentAccounts+` WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// updateAccount runs one UPDATE and reports an unknown id.
func (st *SQLAccountStore) updateAccount(ctx context.Context, set, id string, args ...any) error {
	res, err := st.db.ExecContext(ctx, `UPDATE `+tableAgentAccounts+` SET `+set+` WHERE id = ?`, append(args, id)...)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrAccountNotFound
	}
	return nil
}

func (st *SQLAccountStore) SetSignedIn(ctx context.Context, id string, signedIn bool) error {
	return st.updateAccount(ctx, `signed_in = ?`, id, boolInt(signedIn))
}

func (st *SQLAccountStore) SetLabel(ctx context.Context, id, label string) error {
	return st.updateAccount(ctx, `label = ?`, id, label)
}

func (st *SQLAccountStore) SetShared(ctx context.Context, id string, shared bool) error {
	return st.updateAccount(ctx, `shared = ?`, id, boolInt(shared))
}

func (st *SQLAccountStore) ReplaceUsage(ctx context.Context, id string, windows []LimitWindow, source string, at time.Time) error {
	raw, err := json.Marshal(windows)
	if err != nil {
		return err
	}
	return st.updateAccount(ctx, `usage = ?, usage_source = ?, observed_at = ?`, id, string(raw), source, at.Unix())
}

func (st *SQLAccountStore) MarkCooldown(ctx context.Context, id string, until time.Time) error {
	if _, err := st.Get(ctx, id); err != nil {
		return err
	}
	_, err := st.db.ExecContext(ctx,
		`UPDATE `+tableAgentAccounts+` SET cooldown_until = ? WHERE id = ? AND cooldown_until < ?`,
		until.Unix(), id, until.Unix())
	return err
}
