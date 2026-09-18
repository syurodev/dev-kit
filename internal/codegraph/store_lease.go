// internal/codegraph/store_lease.go
package codegraph

import "sync"

// storeLease is how queries borrow the open project's Store.
//
// A project switch used to close the old store the moment the new one was in
// place, while a query that had captured the old one a moment earlier was
// still reading it — "sql: database is closed" at best. Now the switch retires
// the lease instead: nobody new can borrow it, and the store closes when the
// last borrower gives it back.
//
// A counter rather than an RWMutex on purpose: borrowing is re-entrant (a
// query can call a helper that borrows again) and retiring never blocks the
// switch waiting on a slow reader.
type storeLease struct {
	mu      sync.Mutex
	store   Store
	refs    int
	retired bool
	closed  bool
}

func newStoreLease(store Store) *storeLease {
	return &storeLease{store: store}
}

// acquire borrows the store. ok is false once the lease is retired; release
// must be called exactly once otherwise.
func (l *storeLease) acquire() (Store, func(), bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.retired {
		return nil, nil, false
	}
	l.refs++
	var once sync.Once
	return l.store, func() { once.Do(l.release) }, true
}

func (l *storeLease) release() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refs--
	l.closeIfIdle()
}

// retire stops new borrows and closes the store once the current ones end.
func (l *storeLease) retire() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.retired = true
	l.closeIfIdle()
}

func (l *storeLease) closeIfIdle() {
	if l.retired && l.refs == 0 && !l.closed {
		l.closed = true
		_ = l.store.Close()
	}
}
