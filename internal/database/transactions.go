package database

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrTransactionOpen and ErrNoTransaction are the two mistakes the UI can make.
var (
	ErrTransactionOpen = errors.New("a transaction is already open on this connection")
	ErrNoTransaction   = errors.New("no transaction is open on this connection")
	ErrNoTransactions  = errors.New("this engine's driver cannot open a transaction")
)

// transactions holds the open transaction for each profile.
//
// One per PROFILE, not per tab, and that is the model rather than a shortcut:
// a transaction lives on a connection, DevKit keeps one connection per profile,
// and so a console and a table tab pointed at the same connection are inside
// the same transaction. That is what every database tool does, and the
// alternative — a connection per tab — would multiply locks taken against the
// user's server by however many tabs they left open.
type transactions struct {
	mu sync.Mutex
	// open is the transaction a profile is currently inside, if any.
	open map[string]Tx
	// manual is whether a profile COMMITS BY HAND. Separate from open,
	// because the whole point is that being in manual mode costs nothing
	// until a statement runs: see ensure.
	manual map[string]bool
}

func newTransactions() *transactions {
	return &transactions{open: make(map[string]Tx), manual: make(map[string]bool)}
}

func (t *transactions) isManual(profileID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.manual[profileID]
}

func (t *transactions) setManual(profileID string, manual bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if manual {
		t.manual[profileID] = true
		return
	}
	delete(t.manual, profileID)
}

/*
ensure returns the transaction a statement should run in, opening one if the
profile is in manual mode and none is open yet.

Lazily, and that is the point rather than an optimisation. An open transaction
holds a shared metadata lock on every table it has touched — ACCESS SHARE in
Postgres, an MDL in MySQL — which blocks ALTER, DROP and TRUNCATE for as long
as it lives, and on both engines it pins a read view so old row versions cannot
be cleaned up. Opening one when the MODE changes meant choosing "Manual" and
walking away held all of that against the user's server having run nothing at
all.

The lock is held across Begin on purpose: two statements arriving together must
not each open a transaction, which would leave one of them orphaned on a pinned
connection for the life of the app. The critical section is a single round trip
on one user's own connection.
*/
func (t *transactions) ensure(ctx context.Context, profileID string, conn Conn) (Tx, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if tx := t.open[profileID]; tx != nil {
		return tx, nil
	}
	if !t.manual[profileID] {
		return nil, nil
	}
	transactor, ok := conn.(Transactor)
	if !ok {
		return nil, ErrNoTransactions
	}
	tx, err := transactor.Begin(ctx)
	if err != nil {
		return nil, err
	}
	t.open[profileID] = tx
	return tx, nil
}

func (t *transactions) get(profileID string) Tx {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.open[profileID]
}

func (t *transactions) take(profileID string) Tx {
	t.mu.Lock()
	defer t.mu.Unlock()
	tx := t.open[profileID]
	delete(t.open, profileID)
	return tx
}

// takeAll empties the map, for shutdown.
func (t *transactions) takeAll() []Tx {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Tx, 0, len(t.open))
	for id, tx := range t.open {
		out = append(out, tx)
		delete(t.open, id)
	}
	return out
}

// SetTxMode turns manual commit on or off for a profile.
//
// Turning it OFF commits whatever is open: leaving the mode must not throw
// away work silently — discarding it is what RollbackTx is for.
//
// Turning it ON opens nothing. The first statement does, through ensure, so
// that choosing the mode and then doing nothing holds no locks.
func (s *Service) SetTxMode(ctx context.Context, id string, manual bool) error {
	if manual {
		s.txs.setManual(id, true)
		return nil
	}
	s.txs.setManual(id, false)
	if tx := s.txs.take(id); tx != nil {
		ctx, cancel := queryContext(ctx)
		defer cancel()
		return tx.Commit(ctx)
	}
	return nil
}

// TxMode reports whether a profile commits by hand.
func (s *Service) TxMode(id string) bool { return s.txs.isManual(id) }

// CommitTx ends the open transaction, keeping its work.
//
// Nothing open is success, not an error: in manual mode a transaction exists
// only once a statement has run, so "commit" after reading nothing is a
// request that is already satisfied.
func (s *Service) CommitTx(ctx context.Context, id string) error {
	tx := s.txs.take(id)
	if tx == nil {
		return nil
	}
	ctx, cancel := queryContext(ctx)
	defer cancel()
	return tx.Commit(ctx)
}

// RollbackTx ends the open transaction, discarding its work.
func (s *Service) RollbackTx(ctx context.Context, id string) error {
	tx := s.txs.take(id)
	if tx == nil {
		return nil
	}
	ctx, cancel := queryContext(ctx)
	defer cancel()
	return tx.Rollback(ctx)
}

// TxOpen reports whether a profile is inside a transaction.
//
// The UI asks on mount: a transaction outlives the tab that opened it, and a
// window reopened onto an open transaction has to show that rather than a
// clean slate.
func (s *Service) TxOpen(id string) bool {
	return s.txs.get(id) != nil
}

// rollbackTx discards a profile's transaction without reporting, for the paths
// that are tearing the connection down anyway.
func (s *Service) rollbackTx(id string) {
	if tx := s.txs.take(id); tx != nil {
		ctx, cancel := context.WithTimeout(context.Background(), rollbackTimeout)
		defer cancel()
		_ = tx.Rollback(ctx)
	}
}

// rollbackTimeout bounds the rollbacks nobody is waiting on: shutdown, and a
// profile being edited or deleted.
const rollbackTimeout = 5 * time.Second
