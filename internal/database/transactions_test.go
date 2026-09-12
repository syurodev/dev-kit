package database

import (
	"context"
	"sync"
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

	if err := svc.SetTxMode(context.Background(), id, true); err != nil {
		t.Fatalf("SetTxMode: %v", err)
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
			if err := svc.SetTxMode(context.Background(), id, true); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.Query(context.Background(), id, "select 1", 0); err != nil {
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
			// Still Manual, so the next statement opens a transaction of its
			// own rather than escaping to the pool — ending one is not
			// leaving the mode.
			if _, err := svc.Query(context.Background(), id, "select 2", 0); err != nil {
				t.Fatal(err)
			}
			if len(conn.pooled) != 0 {
				t.Fatalf("pooled = %v; a statement escaped manual mode", conn.pooled)
			}
			if conn.begun != 2 {
				t.Fatalf("opened %d transactions, want a second one for the next statement", conn.begun)
			}
		})
	}
}

// Choosing the mode must not open anything. An idle transaction holds a
// metadata lock on every table it has touched and pins a read view, so
// "switch to Manual and go to lunch" would hold that against the user's server
// having run no statement at all — which is the question that found this.
func TestManualModeOpensNothingUntilAStatementRuns(t *testing.T) {
	svc := newService(t)
	conn := &recordingConn{transactor: true}
	id := plant(t, svc, conn)

	if err := svc.SetTxMode(context.Background(), id, true); err != nil {
		t.Fatal(err)
	}
	if conn.begun != 0 {
		t.Fatalf("opened %d transactions before any statement, want 0", conn.begun)
	}
	if svc.TxOpen(id) {
		t.Fatal("a transaction is reported open before any statement ran")
	}

	if _, err := svc.Query(context.Background(), id, "select 1", 0); err != nil {
		t.Fatal(err)
	}
	if conn.begun != 1 || !svc.TxOpen(id) {
		t.Fatalf("begun = %d, open = %v; the first statement should open one", conn.begun, svc.TxOpen(id))
	}
}

// Reading in Auto mode — which is what opening a table to look at it does —
// must touch no transaction at all.
func TestAutoModeNeverOpensATransaction(t *testing.T) {
	svc := newService(t)
	conn := &recordingConn{transactor: true}
	id := plant(t, svc, conn)

	for range 3 {
		if _, err := svc.Query(context.Background(), id, "select * from t limit 200", 0); err != nil {
			t.Fatal(err)
		}
	}
	if conn.begun != 0 {
		t.Fatalf("opened %d transactions in auto mode, want 0", conn.begun)
	}
	if len(conn.pooled) != 3 {
		t.Fatalf("pooled = %v, want all three statements", conn.pooled)
	}
}

// Two statements arriving together must not each open one: the loser would be
// orphaned on a pinned connection for the life of the app.
func TestConcurrentStatementsShareOneTransaction(t *testing.T) {
	svc := newService(t)
	conn := &recordingConn{transactor: true}
	id := plant(t, svc, conn)
	if err := svc.SetTxMode(context.Background(), id, true); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = svc.Query(context.Background(), id, "select 1", 0)
		}()
	}
	wg.Wait()

	if conn.begun != 1 {
		t.Fatalf("opened %d transactions, want exactly 1", conn.begun)
	}
}

// Leaving Manual commits rather than discarding: throwing away someone's work
// is what Roll back is for.
func TestLeavingManualCommitsWhatWasOpen(t *testing.T) {
	svc := newService(t)
	conn := &recordingConn{transactor: true}
	id := plant(t, svc, conn)
	if err := svc.SetTxMode(context.Background(), id, true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Query(context.Background(), id, "update t set a = 1", 0); err != nil {
		t.Fatal(err)
	}
	tx := conn.activeTx

	if err := svc.SetTxMode(context.Background(), id, false); err != nil {
		t.Fatal(err)
	}
	if !tx.committed {
		t.Fatal("leaving manual mode did not commit what was open")
	}
	if svc.TxMode(id) || svc.TxOpen(id) {
		t.Fatal("still manual, or still open, after leaving the mode")
	}
}

// In manual mode a transaction exists only once a statement has run, so
// "commit" after reading nothing is a request that is already satisfied — not
// an error to show the user.
func TestCommitWithNothingOpenIsFine(t *testing.T) {
	svc := newService(t)
	id := plant(t, svc, &recordingConn{transactor: true})
	if err := svc.CommitTx(context.Background(), id); err != nil {
		t.Fatalf("CommitTx with nothing open = %v, want nil", err)
	}
	if err := svc.RollbackTx(context.Background(), id); err != nil {
		t.Fatalf("RollbackTx with nothing open = %v, want nil", err)
	}
}

// Dropping the connection has to drop the transaction pinned to it, or the Tx
// holds a session on a Conn nobody can reach again.
func TestClosingTheServiceRollsBackWhatWasOpen(t *testing.T) {
	svc := newService(t)
	conn := &recordingConn{transactor: true}
	id := plant(t, svc, conn)
	if err := svc.SetTxMode(context.Background(), id, true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Query(context.Background(), id, "update t set a = 1", 0); err != nil {
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
