package bridge

import (
	"path/filepath"
	"testing"

	"devkit/internal/app"
	"devkit/internal/codexhook"
	"devkit/internal/codexsessions"
	"devkit/internal/storage"
	"devkit/internal/vault"
)

func newBridgeWithCodexSessions(t *testing.T) (*AppService, *vault.Service) {
	t.Helper()
	v := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := v.Setup("correct-horse"); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := storage.Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	container := app.NewContainer()
	container.CodexSessions = codexsessions.NewService(db, storage.TableCodexHookEvents, v)
	return NewAppService(container), v
}

func TestCodexSessionsListAndEventsRoundTrip(t *testing.T) {
	svc, v := newBridgeWithCodexSessions(t)
	secret := "the user's actual prompt text"
	if err := svc.container.CodexSessions.Record(codexhook.Call{
		Event: codexhook.EventUserPromptSubmit, SessionID: "s1", CWD: "/repo", Prompt: secret,
	}); err != nil {
		t.Fatal(err)
	}

	sessions, err := svc.CodexSessionsList()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != "s1" {
		t.Fatalf("sessions = %+v", sessions)
	}

	events, err := svc.CodexSessionEvents("s1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Prompt != secret {
		t.Fatalf("events = %+v", events)
	}

	// A locked vault gives the row back without the prompt, not an error —
	// the UI has to be able to show the list while the vault is locked.
	v.Lock()
	events, err = svc.CodexSessionEvents("s1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Prompt != "" {
		t.Fatalf("a locked vault handed back the prompt: %+v", events)
	}
}

func TestCodexSessionsClearSessionAndClearAll(t *testing.T) {
	svc, _ := newBridgeWithCodexSessions(t)
	for _, id := range []string{"s1", "s2"} {
		if err := svc.container.CodexSessions.Record(codexhook.Call{
			Event: codexhook.EventStop, SessionID: id, CWD: "/repo",
		}); err != nil {
			t.Fatal(err)
		}
	}

	if err := svc.CodexSessionsClearSession("s1"); err != nil {
		t.Fatal(err)
	}
	sessions, err := svc.CodexSessionsList()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != "s2" {
		t.Fatalf("after clearing s1: %+v", sessions)
	}

	if err := svc.CodexSessionsClear(); err != nil {
		t.Fatal(err)
	}
	if sessions, err = svc.CodexSessionsList(); err != nil || len(sessions) != 0 {
		t.Fatalf("Clear left sessions: %+v %v", sessions, err)
	}
}

// An unknown session id is an empty transcript, not an error — the same
// answer as a session that simply has not reported anything yet, which is
// indistinguishable from "does not exist" without a separate concept of
// registration this package does not have.
func TestCodexSessionEventsOnAnUnknownSessionIsEmptyNotAnError(t *testing.T) {
	svc, _ := newBridgeWithCodexSessions(t)
	events, err := svc.CodexSessionEvents("no-such-session", 0)
	if err != nil {
		t.Fatalf("unknown session id returned an error: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("events = %+v, want none", events)
	}
}
