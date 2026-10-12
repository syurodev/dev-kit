package agenthub

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"devkit/internal/pdfdoc"
	"devkit/internal/storage"
)

// An agent that invokes another session (agenthub.invokeAgent → Service.Invoke)
// chose its attachment paths itself, and DevKit reads them with its own
// privileges. A PDF is held to the same rule as any other file: inside the
// session's project, symlinks resolved first.
func TestAnAgentsPDFAttachmentIsConfinedToTheSessionsProject(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	store := NewSQLStore(db)
	engine := pdfdoc.New(pdfdoc.Options{})
	t.Cleanup(engine.Close)
	svc := NewService(NewRegistry(), store, nil)
	svc.Documents = NewDocumentLibrary(store, filepath.Join(t.TempDir(), "docs"), engine)

	project, outside := t.TempDir(), t.TempDir()
	data, err := os.ReadFile(filepath.Join("..", "pdfdoc", "testdata", "webprint.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(project, "ok.pdf")
	secret := filepath.Join(outside, "secret.pdf")
	for _, p := range []string{inside, secret} {
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(project, "link.pdf")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatal(err)
	}
	sess := AgentSession{ID: "11111111-2222-3333-4444-555555555555", ProjectPath: project}
	if err := store.InsertSession(context.Background(), AgentSession{ID: sess.ID, AgentType: AgentTypeMock, ProjectPath: project, Status: SessionStatusIdle}); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if docs, _, err := svc.importDocuments(ctx, sess, []string{inside}, true); err != nil || len(docs) != 1 {
		t.Fatalf("a PDF inside the project: %v, %v", docs, err)
	}
	for name, path := range map[string]string{"outside": secret, "symlink out": link} {
		if _, _, err := svc.importDocuments(ctx, sess, []string{path}, true); !errors.Is(err, ErrCwdOutsideParent) {
			t.Errorf("%s: err = %v, want ErrCwdOutsideParent", name, err)
		}
	}
	// Nothing was kept for the refused ones.
	if list, _ := svc.Documents.List(ctx, sess.ID); len(list) != 1 {
		t.Fatalf("documents = %d, want only the one inside", len(list))
	}
	// A person's own attachment is not confined: they chose the file.
	if docs, _, err := svc.importDocuments(ctx, sess, []string{secret}, false); err != nil || len(docs) != 1 {
		t.Fatalf("a person's attachment: %v, %v", docs, err)
	}
}
