package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/app"
	"devkit/internal/apperrors"
	"devkit/internal/storage"
)

func newBridgeWithAgentHub(t *testing.T) *AppService {
	t.Helper()
	svc, _ := newBridgeWithAgentHubStore(t)
	return svc
}

// newBridgeWithAgentHubStore is newBridgeWithAgentHub that also hands back the
// session store, for a test that needs rows the bridge has no method to write.
func newBridgeWithAgentHubStore(t *testing.T) (*AppService, *agenthub.SQLStore) {
	t.Helper()
	return newBridgeWithAgentHubAdapter(t, agenthub.NewMockAdapter())
}

// newBridgeWithAgentHubAdapter is newBridgeWithAgentHubStore with the given adapter
// registered instead of the plain mock (it must report itself as the mock vendor).
func newBridgeWithAgentHubAdapter(t *testing.T, adapter agenthub.VendorAdapter) (*AppService, *agenthub.SQLStore) {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	container := app.NewContainer()
	reg := agenthub.NewRegistry()
	reg.Register(adapter)
	waiter := agenthub.NewPermissionWaiter(nil)
	store := agenthub.NewSQLStore(db)
	svc := agenthub.NewService(reg, store, nil)
	svc.ResolvePermission = waiter.Resolve
	questions := agenthub.NewElicitationWaiter(nil, nil)
	svc.ResolveElicitation = questions.Resolve
	svc.CancelPendingElicitations = questions.CancelSession
	container.AgentHub = svc
	container.AgentHubPermissions = waiter
	container.AgentHubElicitations = questions
	return NewAppService(container), store
}

func TestAgentHubListEmpty(t *testing.T) {
	svc := newBridgeWithAgentHub(t)
	list, err := svc.AgentHubListAgents()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("%v", list)
	}
}

// The window reads an agent's models and environment before any session
// exists. Both asked the gateway for scopes ("catalog:<agent>",
// "environment:<agent>") that no default grant covered, so each was refused with
// a bare "Permission denied." and the draft picker said "Models unavailable".
// This goes through the real default grants: what comes back may be anything
// but a refusal at the gateway.
func TestAgentHubPreSessionReadsAreNotRefusedByTheGateway(t *testing.T) {
	svc := newBridgeWithAgentHub(t)
	for _, vendor := range []string{"claude", "codex"} {
		_, err := svc.AgentHubConfigCatalog(context.Background(), vendor)
		assertNotPermissionDenied(t, "AgentHubConfigCatalog("+vendor+")", err)
		_, err = svc.AgentHubEnvironment(context.Background(), vendor, "")
		assertNotPermissionDenied(t, "AgentHubEnvironment("+vendor+")", err)
	}
}

func assertNotPermissionDenied(t *testing.T, call string, err error) {
	t.Helper()
	var public *apperrors.AppError
	if errors.As(err, &public) && public.Code == "permission_denied" {
		t.Fatalf("%s was refused at the gateway: %v", call, err)
	}
}

func TestAgentHubCreateMock(t *testing.T) {
	svc := newBridgeWithAgentHub(t)
	s, err := svc.AgentHubCreateSession(agenthub.CreateInput{AgentType: "mock", Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if s.ID == "" {
		t.Fatal("missing id")
	}
}

func TestAgentHubCreateAndInvokeMock(t *testing.T) {
	svc := newBridgeWithAgentHub(t)
	sess, err := svc.AgentHubCreateSession(agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	out, err := svc.AgentHubInvoke(context.Background(), agenthub.InvokeInput{
		SessionID: sess.ID,
		Prompt:    "hello",
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if out.Status != "completed" {
		t.Fatalf("status = %q, want completed", out.Status)
	}

	st, err := svc.AgentHubGetStatus(sess.ID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Status != agenthub.SessionStatusIdle {
		t.Fatalf("session status = %q", st.Status)
	}
}

func TestAgentHubListVendorTypes(t *testing.T) {
	svc := newBridgeWithAgentHub(t)
	types, err := svc.AgentHubListVendorTypes()
	if err != nil {
		t.Fatal(err)
	}
	if len(types) != 1 || types[0] != agenthub.AgentTypeMock {
		t.Fatalf("types = %v, want [mock]", types)
	}
}

func TestAgentHubBridgeNilServiceDoesNotPanic(t *testing.T) {
	container := app.NewContainer()
	container.AgentHub = nil
	service := NewAppService(container)

	_, err := service.AgentHubListAgents()
	if err == nil || err.Error() != "internal_error: Agent Hub is not available." {
		t.Fatalf("AgentHubListAgents error = %v, want internal_error unavailable", err)
	}
}

func TestAgentHubResolvePermissionNoPending(t *testing.T) {
	svc := newBridgeWithAgentHub(t)
	err := svc.AgentHubResolvePermission("0123456789abcdef01234567", json.RawMessage(`{"outcome":{"outcome":"cancelled"}}`))
	if err == nil {
		t.Fatal("expected error when no pending permission")
	}
}

// TestElicitationScopesAreGranted goes through the real gateway: a missing
// grant is not a compile error, it is every answer the user gives failing at
// runtime with permission_denied.
func TestElicitationScopesAreGranted(t *testing.T) {
	svc := newBridgeWithAgentHub(t)
	waiter := svc.container.AgentHubElicitations

	answered := make(chan json.RawMessage, 1)
	go func() {
		response, _ := waiter.Wait(context.Background(), "s1", json.RawMessage(`{"mode":"form","sessionId":"s1","message":"Pick","requestedSchema":{"type":"object","properties":{"question_0":{"type":"string"}}}}`))
		answered <- response
	}()

	var pending []agenthub.PendingElicitation
	deadline := time.Now().Add(2 * time.Second)
	for len(pending) == 0 && time.Now().Before(deadline) {
		list, err := svc.AgentHubPendingElicitations()
		if err != nil {
			t.Fatalf("AgentHubPendingElicitations: %v", err)
		}
		pending = list
		time.Sleep(5 * time.Millisecond)
	}
	if len(pending) != 1 {
		t.Fatalf("pending = %v, want one question", pending)
	}
	if err := svc.AgentHubRespondElicitation(pending[0].ID, json.RawMessage(`{"action":"accept","content":{"question_0":"yes"}}`)); err != nil {
		t.Fatalf("AgentHubRespondElicitation: %v", err)
	}
	select {
	case got := <-answered:
		if string(got) != `{"action":"accept","content":{"question_0":"yes"}}` {
			t.Fatalf("agent got %s", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the answer never reached the agent")
	}
}

func TestAgentHubRespondElicitationNoPending(t *testing.T) {
	svc := newBridgeWithAgentHub(t)
	err := svc.AgentHubRespondElicitation("0123456789abcdef01234567", json.RawMessage(`{"action":"decline"}`))
	if err == nil || err.Error() != "validation_failed: agenthub: no pending elicitation" {
		t.Fatalf("error = %v, want the waiter's own message", err)
	}
}

func TestAgentHubChooseDirectoryPassesStartInAndReturnsChoice(t *testing.T) {
	svc := newBridgeWithAgentHub(t)
	var gotTitle, gotStartIn string
	svc.openDirDialog = func(title, startIn string) (string, error) {
		gotTitle, gotStartIn = title, startIn
		return "/acme/billing-service", nil
	}

	got, err := svc.AgentHubChooseDirectory("/acme")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/acme/billing-service" {
		t.Fatalf("path = %q, want /acme/billing-service", got)
	}
	if gotStartIn != "/acme" {
		t.Fatalf("startIn = %q, want /acme", gotStartIn)
	}
	if gotTitle == "" {
		t.Fatal("dialog opened with no title")
	}
}

func TestAgentHubChooseDirectoryCancelIsNotAnError(t *testing.T) {
	// The picker returns "" when the user backs out. Reporting that as an
	// error would render a failure the user did not cause, in a dialog that
	// is meant to stay open with the field untouched.
	svc := newBridgeWithAgentHub(t)
	svc.openDirDialog = func(string, string) (string, error) { return "", nil }

	got, err := svc.AgentHubChooseDirectory("")
	if err != nil {
		t.Fatalf("cancel reported as error: %v", err)
	}
	if got != "" {
		t.Fatalf("path = %q, want empty", got)
	}
}

func TestAgentHubCloseSessionMarksClosedAndKeepsHistory(t *testing.T) {
	svc := newBridgeWithAgentHub(t)
	sess, err := svc.AgentHubCreateSession(agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := svc.AgentHubCloseSession(context.Background(), sess.ID); err != nil {
		t.Fatalf("close: %v", err)
	}

	after, err := svc.AgentHubGetStatus(sess.ID)
	if err != nil {
		t.Fatalf("status after close: %v", err)
	}
	if after.Status != agenthub.SessionStatusClosed {
		t.Fatalf("status = %q, want closed", after.Status)
	}

	// Closing ends the process, it does not delete the record: the tree shows
	// closed agents as history, and a transcript you can no longer read is a
	// worse outcome than one you cannot reply to.
	list, err := svc.AgentHubListAgents()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("sessions after close = %d, want 1", len(list))
	}
}

func TestAgentHubDeleteSessionRemovesItFromTheList(t *testing.T) {
	svc := newBridgeWithAgentHub(t)
	sess, err := svc.AgentHubCreateSession(agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := svc.AgentHubDeleteSession(context.Background(), sess.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Unlike close, which keeps the row as history, delete is meant to leave
	// nothing behind.
	list, err := svc.AgentHubListAgents()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("sessions after delete = %d, want 0", len(list))
	}
}

func TestAgentHubChooseFilesCancelReturnsEmptyNotNil(t *testing.T) {
	// A nil slice marshals as null where the frontend expects a list, and the
	// composer would then try to spread it. Cancel is the common case.
	svc := newBridgeWithAgentHub(t)
	svc.openFilesDialog = func(string, string) ([]string, error) { return nil, nil }

	got, err := svc.AgentHubChooseFiles("")
	if err != nil {
		t.Fatalf("cancel reported as error: %v", err)
	}
	if got == nil {
		t.Fatal("returned nil; the frontend expects a list")
	}
	if len(got) != 0 {
		t.Fatalf("paths = %v, want empty", got)
	}
}

func TestAgentHubDescribeFilesAnswersEachPathInOrder(t *testing.T) {
	svc := newBridgeWithAgentHub(t)
	dir := t.TempDir()
	ok := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(ok, []byte("# hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := svc.AgentHubDescribeFiles([]string{filepath.Join(dir, "gone.txt"), ok})
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if len(got) != 2 || got[0].Problem == "" || got[1].Kind != "text" || got[1].Size != 4 {
		t.Fatalf("got %+v", got)
	}
	// A list, not null, when nothing was asked: the chip hook spreads it.
	none, err := svc.AgentHubDescribeFiles(nil)
	if err != nil || none == nil {
		t.Fatalf("empty ask: %v %v", none, err)
	}
}

func TestAgentHubNewScratchDirectoryMakesAFreshFolderUnderTheRoot(t *testing.T) {
	// Each "no project" session gets a folder of its own: a shared one would
	// let two sessions read and overwrite each other's files.
	svc := newBridgeWithAgentHub(t)
	svc.container.ConfigDir = t.TempDir()

	first, err := svc.AgentHubNewScratchDirectory()
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.AgentHubNewScratchDirectory()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("two sessions share %q", first)
	}
	root := svc.AgentHubScratchRoot()
	for _, dir := range []string{first, second} {
		if filepath.Dir(dir) != root {
			t.Fatalf("%q is not directly under %q", dir, root)
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Fatalf("%q was not created: %v", dir, err)
		}
	}
}

func TestAgentHubListDocumentsReadsTheSessionsDocuments(t *testing.T) {
	svc, store := newBridgeWithAgentHubStore(t)
	svc.container.AgentHub.Documents = agenthub.NewDocumentLibrary(store, t.TempDir(), nil)
	sess, err := svc.AgentHubCreateSession(agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertDocument(context.Background(), agenthub.Document{
		ID: "doc1", SessionID: sess.ID, Kind: "pdf", Name: "spec.pdf", MIME: "application/pdf",
		Size: 2048, SHA256: "abc", Pages: 12, TextLayer: "full", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	docs, err := svc.AgentHubListDocuments(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].Name != "spec.pdf" || docs[0].Pages != 12 {
		t.Fatalf("documents = %+v, want spec.pdf with 12 pages", docs)
	}
}

func TestAgentHubListDocumentsWithoutALibraryIsAnEmptyList(t *testing.T) {
	// Server mode wires no library; the summary card still expects a list.
	svc := newBridgeWithAgentHub(t)
	sess, err := svc.AgentHubCreateSession(agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	docs, err := svc.AgentHubListDocuments(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if docs == nil || len(docs) != 0 {
		t.Fatalf("documents = %#v, want an empty, non-nil list", docs)
	}
}
