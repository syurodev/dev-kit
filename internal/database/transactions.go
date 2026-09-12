package database

import (
	"context"
	"errors"
	"sync"
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
	mu   sync.Mutex
	open map[string]Tx
}

func newTransactions() *transactions {
	return &transactions{open: make(map[string]Tx)}
}

func (t *transactions) get(profileID string) Tx {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.open[profileID]
}

func (t *transactions) put(profileID string, tx Tx) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.open[profileID] != nil {
		return ErrTransactionOpen
	}
	t.open[profileID] = tx
	return nil
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

// BeginTx opens a transaction on a profile's connection.
//
// Explicit, rather than started for you by the first statement after the mode
// changes. The service then has one thing to know — a transaction is open or it
// is not — and the policy of when to open one belongs to the UI that offers
// Auto and Manual.
func (s *Service) BeginTx(ctx context.Context, id string) error {
	target, err := s.target(id)
	if err != nil {
		return err
	}
	ctx, cancel := queryContext(ctx)
	defer cancel()

	conn, err := s.pool(ctx, id, target)
	if err != nil {
		return err
	}
	transactor, ok := conn.(Transactor)
	if !ok {
		return ErrNoTransactions
	}
	// Asked BEFORE dialling a transaction, so a second Begin cannot leave one
	// orphaned on the server while the map keeps the first.
	if existing := s.txs.get(id); existing != nil {
		return ErrTransactionOpen
	}
	tx, err := transactor.Begin(ctx)
	if err != nil {
		return err
	}
	if err := s.txs.put(id, tx); err != nil {
		// Lost a race with another Begin. Give the connection back rather than
		// holding it for a transaction nothing can reach.
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}

// CommitTx ends the open transaction, keeping its work.
func (s *Service) CommitTx(ctx context.Context, id string) error {
	tx := s.txs.take(id)
	if tx == nil {
		return ErrNoTransaction
	}
	ctx, cancel := queryContext(ctx)
	defer cancel()
	return tx.Commit(ctx)
}

// RollbackTx ends the open transaction, discarding its work.
func (s *Service) RollbackTx(ctx context.Context, id string) error {
	tx := s.txs.take(id)
	if tx == nil {
		return ErrNoTransaction
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
		_ = tx.Rollback(context.Background())
	}
}
