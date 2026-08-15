package preferences_test

import (
	"context"
	"path/filepath"
	"testing"

	"devkit/internal/preferences"
	"devkit/internal/storage"
)

func TestSQLRepositoryThemeRoundTrip(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}

	service := preferences.NewService(preferences.NewSQLRepository(db))
	if got, err := service.Theme(); err != nil || got != preferences.ThemeDark {
		t.Fatalf("initial theme = %q, err = %v", got, err)
	}
	if err := service.SetTheme(preferences.ThemeSystem); err != nil {
		t.Fatal(err)
	}
	if got, err := service.Theme(); err != nil || got != preferences.ThemeSystem {
		t.Fatalf("stored theme = %q, err = %v", got, err)
	}
}
