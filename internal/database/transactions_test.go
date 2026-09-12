package database

import (
	"context"
	"errors"
	"testing"
)

// A Conn that records where each statement went. The seam this exercises is
// the one a real database cannot be asked about here: whether a statement runs
// inside the open transaction or beside it.
type recordingConn struct {
	pooled     []string
	transactor bool
	beginErr   error
	begun      int
	activeTx   *recordingTx
}

func (c *recordingConn) Ping(context.Context) error { return nil }
func (c *recordingConn) Close()                     {}
func (c *recordingConn) Query(_ context.Context, sql string, _ ...any) (Rows, error) {
	c.pooled = append(c.pooled, sql)
	return emptyRows{}, nil
}
func (c *recordingConn) ListSchemas(context.Context) ([]string, error) { return nil, nil }
func (c *recordingConn) ListTables(context.Context, string) ([]TableInfo, error) {
	return nil, nil
}
func (c *recordingConn) ListColumns(context.Context, string, string) ([]ColumnInfo, error) {
	return nil, nil
}
func (c *recordingConn) ListForeignKeys(context.Context, string) ([]ForeignKey, error) {
	return nil, nil
}

func (c *recordingConn) Begin(context.Context) (Tx, error) {
	if !c.transactor {
		panic("Begin on a conn that is not a Transactor")
	}
	if c.beginErr != nil {
		return nil, c.beginErr
	}
	c.begun++
	c.activeTx = &recordingTx{}
	return c.activeTx, nil
}

type recordingTx struct {
	ran        []string
	committed  bool
	rolledBack bool
}

func (t *recordingTx) Query(_ context.Context, sql string, _ ...any) (Rows, error) {
	t.ran = append(t.ran, sql)
	return emptyRows{}, nil
}
func (t *recordingTx) Commit(context.Context) error   { t.committed = true; return nil }
func (t *recordingTx) Rollback(context.Context) error { t.rolledBack = true; return nil }

type emptyRows struct{}

func (emptyRows) Columns() []string         { return []string{"?column?"} }
func (emptyRows) Next() bool                { return false }
func (emptyRows) Values() ([]string, error) { return nil, nil }
func (emptyRows) Err() error                { return nil }
func (emptyRows) Close()                    {}

// Plants a live connection for a profile, which is what lets these tests reach
// Query and BeginTx without dialling anything. Same package, so the pool's map
// is reachable.
func plant(t *testing.T, svc *Service, conn Conn) string {
	t.Helper()
	summary, err := svc.CreateProfile(CreateProfileInput{
		Name: "local", Host: "127.0.0.1", Password: "secret", Env: "dev",
	})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	svc.pools.conns[summary.ID] = conn
	return summary.ID
}

func TestQueryRunsOnThePoolWithNoTransaction(t *testing.T) {
	svc := newService(t)
	conn := &recordingConn{transactor: true}
	id := plant(t, svc, conn)

	if _, err := svc.Query(context.Background(), id, "select 1", 0); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(conn.pooled) != 1 || conn.pooled[0] != "select 1" {
		t.Fatalf("pooled = %v, want the statement", conn.pooled)
	}
}

// The point of the whole feature: with a transaction open, the statement has
// to go down the pinned connection. On the pool it would run outside the
// transaction and could not be rolled back — while the transaction sat open
// holding a connection.
func TestQueryRunsInsideAnOpenTransaction(t *testing.T) {
	svc := newService(t)
	conn := &recordingConn{transactor: true}
	id := plant(t, svc, conn)

	if err := svc.BeginTx(context.Background(), id); err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := svc.Query(context.Background(), id, "update t set a = 1", 0); err != nil {
		t.Fatalf("Query: %v", err)
	}

	if got := conn.activeTx.ran; len(got) != 1 || got[0] != "update t set a = 1" {
		t.Fatalf("transaction ran %v, want the statement", got)
	}
	if len(conn.pooled) != 0 {
		t.Fatalf("pool also saw %v; the statement escaped the transaction", conn.pooled)
	}
}

func TestCommitAndRollbackEndTheTransaction(t *testing.T) {
	for _, tc := range []struct {
		name string
		end  func(*Service, string) error
		want func(*recordingTx) bool
	}{
		{"commit", func(s *Service, id string) error { return s.CommitTx(context.Background(), id) },
			func(tx *recordingTx) bool { return tx.committed }},
		{"rollback", func(s *Service, id string) error { return s.RollbackTx(context.Background(), id) },
			func(tx *recordingTx) bool { return tx.rolledBack }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := newService(t)
			conn := &recordingConn{transactor: true}
			id := plant(t, svc, conn)
			if err := svc.BeginTx(context.Background(), id); err != nil {
				t.Fatal(err)
			}
			tx := conn.activeTx
			if err := tc.end(svc, id); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if !tc.want(tx) {
				t.Fatalf("the transaction was not %sted", tc.name)
			}
			if svc.TxOpen(id) {
				t.Fatal("a transaction is still reported open")
			}
			// And the next statement is back on the pool.
			if _, err := svc.Query(context.Background(), id, "select 2", 0); err != nil {
				t.Fatal(err)
			}
			if len(conn.pooled) != 1 {
				t.Fatalf("pooled = %v, want the statement after the transaction ended", conn.pooled)
			}
		})
	}
}

// Two Begins would leave the first transaction open on the server with nothing
// able to reach it — a pinned connection lost for the app's lifetime.
func TestBeginRefusesASecondTransaction(t *testing.T) {
	svc := newService(t)
	conn := &recordingConn{transactor: true}
	id := plant(t, svc, conn)

	if err := svc.BeginTx(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if err := svc.BeginTx(context.Background(), id); !errors.Is(err, ErrTransactionOpen) {
		t.Fatalf("second BeginTx = %v, want ErrTransactionOpen", err)
	}
	if conn.begun != 1 {
		t.Fatalf("opened %d transactions, want 1", conn.begun)
	}
}

func TestCommitWithoutATransactionSaysSo(t *testing.T) {
	svc := newService(t)
	id := plant(t, svc, &recordingConn{transactor: true})
	if err := svc.CommitTx(context.Background(), id); !errors.Is(err, ErrNoTransaction) {
		t.Fatalf("CommitTx = %v, want ErrNoTransaction", err)
	}
}

// Dropping the connection has to drop the transaction pinned to it, or the Tx
// holds a session on a Conn nobody can reach again.
func TestClosingTheServiceRollsBackWhatWasOpen(t *testing.T) {
	svc := newService(t)
	conn := &recordingConn{transactor: true}
	id := plant(t, svc, conn)
	if err := svc.BeginTx(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	tx := conn.activeTx

	svc.Close()

	if !tx.rolledBack {
		t.Fatal("the open transaction survived Close")
	}
	if svc.TxOpen(id) {
		t.Fatal("a transaction is still reported open after Close")
	}
}
