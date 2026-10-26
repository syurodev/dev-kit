package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"devkit/internal/harness"
	"devkit/internal/storage"
)

func TestTheLifecycleAdapterClearsThroughTheStoreAndReportsFailure(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = storage.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO workspaces(id,name,root) VALUES('w1','one','/one')`,
		`INSERT INTO agent_sessions(id,agent_type,project_path,status,depth,created_at,updated_at,workspace_id) VALUES('s1','claude','/one','idle',0,'x','x','w1')`,
	} {
		if _, err = db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	store := storage.NewHarnessStore(db)
	if _, err = store.Apply(ctx, harness.StoreWrite{WorkspaceID: "w1", SessionID: "s1",
		Override: &harness.OverrideWrite{Value: &harness.Override{Schema: harness.SchemaVersion, Groups: []string{"git"}}}}); err != nil {
		t.Fatal(err)
	}
	if err = (harnessLifecycle{store: store}).SessionClosed(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	if o, st, _ := store.GetOverride(ctx, "s1", "w1"); o != nil || st.Revision != 2 {
		t.Fatalf("override after close: %+v %+v", o, st)
	}
	if err = (harnessLifecycle{store: failingClearer{}}).SessionClosed(ctx, "s1"); err == nil {
		t.Fatal("a failed clear was reported as success")
	}
}

type failingClearer struct{}

func (failingClearer) ClearOverride(context.Context, string) (harness.Stamp, error) {
	return harness.Stamp{}, errors.New("db busy")
}
