package environments_test

import (
	"context"
	"path/filepath"
	"testing"

	"devkit/internal/environments"
	"devkit/internal/storage"
)

func newTestRepo(t *testing.T) *environments.SQLRepository {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return environments.NewSQLRepository(db)
}

func TestSQLRepositoryListReturnsMigrationSeed(t *testing.T) {
	repo := newTestRepo(t)

	tags, err := repo.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tags) != 3 {
		t.Fatalf("len(tags) = %d, want 3", len(tags))
	}
}

func TestSQLRepositoryGetByName(t *testing.T) {
	repo := newTestRepo(t)

	tag, err := repo.Get("prod")
	if err != nil {
		t.Fatalf("Get(prod): %v", err)
	}
	if !tag.RequiresConfirmation || !tag.Locked || !tag.IsBuiltin {
		t.Fatalf("prod tag = %+v, want locked builtin requires_confirmation", tag)
	}

	if _, err := repo.Get("does-not-exist"); err != environments.ErrTagNotFound {
		t.Fatalf("Get(does-not-exist) error = %v, want ErrTagNotFound", err)
	}
}

func TestSQLRepositoryCreateGetByIDUpdateDelete(t *testing.T) {
	repo := newTestRepo(t)

	tag := environments.Tag{ID: "aaaaaaaaaaaaaaaaaaaaaaaa", Name: "qa", RequiresConfirmation: false}
	if err := repo.Create(tag); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(tag.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != "qa" {
		t.Fatalf("GetByID name = %q, want qa", got.Name)
	}

	got.RequiresConfirmation = true
	if err := repo.Update(got); err != nil {
		t.Fatalf("Update: %v", err)
	}
	updated, err := repo.GetByID(tag.ID)
	if err != nil {
		t.Fatalf("GetByID after update: %v", err)
	}
	if !updated.RequiresConfirmation {
		t.Fatal("Update did not persist RequiresConfirmation=true")
	}

	if err := repo.Delete(tag.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.GetByID(tag.ID); err != environments.ErrTagNotFound {
		t.Fatalf("GetByID after delete error = %v, want ErrTagNotFound", err)
	}
}

func TestSQLRepositoryUpdateUnknownIDReturnsNotFound(t *testing.T) {
	repo := newTestRepo(t)

	err := repo.Update(environments.Tag{ID: "does-not-exist", Name: "x"})
	if err != environments.ErrTagNotFound {
		t.Fatalf("Update(unknown) error = %v, want ErrTagNotFound", err)
	}
}
