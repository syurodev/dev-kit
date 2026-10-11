package telegramcontrol

import (
	"context"
	"path/filepath"
	"testing"

	"devkit/internal/storage"
)

func TestLocalStatePersistsPairAndOffsetWithoutCredential(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	store := NewSQLStore(db)
	state := State{Config: Config{AccountID: "account-reference", Workspaces: []string{"ws"}, ReadTranscript: true}, UserID: 7, ChatID: 8, Offset: 42}
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Offset != 42 || loaded.UserID != 7 || loaded.ChatID != 8 || loaded.Config.AccountID != "account-reference" {
		t.Fatalf("state=%+v", loaded)
	}
}
