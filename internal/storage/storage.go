package storage

import "context"

type Transaction interface {
	Commit() error
	Rollback() error
}

type Transactioner interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context, tx Transaction) error) error
}
