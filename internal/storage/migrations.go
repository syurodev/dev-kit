package storage

import (
	"context"
	"fmt"
	"sort"
)

type Migration struct {
	Version int
	Name    string
	Up      func(ctx context.Context) error
}

type MigrationStore interface {
	CurrentVersion(ctx context.Context) (int, error)
	ApplyMigration(ctx context.Context, migration Migration) error
}

type MigrationRunner struct {
	store      MigrationStore
	migrations []Migration
}

func NewMigrationRunner(store MigrationStore, migrations []Migration) (*MigrationRunner, error) {
	if store == nil {
		return nil, fmt.Errorf("migration store is nil")
	}

	copied := append([]Migration(nil), migrations...)
	sort.Slice(copied, func(i int, j int) bool {
		return copied[i].Version < copied[j].Version
	})

	for i := 1; i < len(copied); i++ {
		if copied[i-1].Version == copied[i].Version {
			return nil, fmt.Errorf("duplicate migration version: %d", copied[i].Version)
		}
	}

	for _, migration := range copied {
		if migration.Up == nil {
			return nil, fmt.Errorf("migration %d %q has no Up function", migration.Version, migration.Name)
		}
	}

	return &MigrationRunner{
		store:      store,
		migrations: copied,
	}, nil
}

func (r *MigrationRunner) Run(ctx context.Context) error {
	current, err := r.store.CurrentVersion(ctx)
	if err != nil {
		return fmt.Errorf("read current migration version: %w", err)
	}

	for _, migration := range r.migrations {
		if migration.Version <= current {
			continue
		}
		if err := r.store.ApplyMigration(ctx, migration); err != nil {
			return fmt.Errorf("apply migration %d %q: %w", migration.Version, migration.Name, err)
		}
		current = migration.Version
	}

	return nil
}
