package database

import (
	"context"
	"fmt"
)

/*
ApplyEdits writes a table tab's reviewed changes, all of them or none (#52).

On a profile whose environment asks for review, a grid edit is staged instead of
written, and Apply sends every staged change at once. One transaction, so a
change that fails — or that matches no row, because the row changed or went away
after the page loaded — takes the others back with it rather than leaving half a
review applied.

In manual commit mode they run inside the profile's open transaction instead,
and committing stays the user's call, as it is for any other statement.
*/
func (s *Service) ApplyEdits(ctx context.Context, id string, statements []string) error {
	if len(statements) == 0 {
		return nil
	}
	target, err := s.target(id)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, commitTimeout)
	defer cancel()
	pool, err := s.pool(ctx, id, target)
	if err != nil {
		return err
	}

	open, err := s.txs.ensure(ctx, id, pool)
	if err != nil {
		return err
	}
	if open != nil {
		return runEdits(ctx, open, statements)
	}

	transactor, ok := pool.(Transactor)
	if !ok {
		return ErrNoTransactions
	}
	tx, err := transactor.Begin(ctx)
	if err != nil {
		return err
	}
	if err := runEdits(ctx, tx, statements); err != nil {
		rollback, done := context.WithTimeout(context.Background(), rollbackTimeout)
		defer done()
		_ = tx.Rollback(rollback)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return NewStatementError(err)
	}
	return nil
}

func runEdits(ctx context.Context, tx Tx, statements []string) error {
	for i, statement := range statements {
		rows, err := tx.Query(ctx, statement)
		if err != nil {
			return NewStatementError(err)
		}
		for rows.Next() {
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return NewStatementError(err)
		}
		if rows.RowsAffected() == 0 {
			// Marked as the engine's answer so the bridge shows it: it is about
			// the user's data, and carries nothing from the connection.
			return NewStatementError(fmt.Errorf(
				"change %d of %d matched no row: it changed or was deleted after the page loaded, so nothing was written",
				i+1, len(statements)))
		}
	}
	return nil
}
