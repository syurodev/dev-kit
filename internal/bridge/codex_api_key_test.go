package bridge

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"devkit/internal/accounts"
	"devkit/internal/agenthub"
	"devkit/internal/app"
	"devkit/internal/storage"
)

// Choosing Codex's API key crosses the bridge as an account id: stored,
// read back, audited by id, and the key itself never in the audit log.
func TestCodexAPIKeyAccountThroughTheGateway(t *testing.T) {
	svc, container := newBridgeWithAccounts(t)
	token, err := container.Accounts.Create(accounts.CreateInput{
		Provider: "openai", Kind: accounts.KindToken, Label: "OpenAI", Secret: "sk-live-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := svc.PreferencesGetCodexAPIKeyAccount(); err != nil || got != "" {
		t.Fatalf("unset = %q, %v", got, err)
	}
	if err := svc.PreferencesSetCodexAPIKeyAccount(token.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := svc.PreferencesGetCodexAPIKeyAccount(); err != nil || got != token.ID {
		t.Fatalf("stored = %q, %v", got, err)
	}
	if got, err := (app.CodexAPIKey{C: container}).Key(context.Background()); err != nil || got != "sk-live-secret" {
		t.Fatalf("resolved = %q, %v", got, err)
	}

	var audited bool
	for _, event := range container.Audit.Events() {
		raw, _ := json.Marshal(event.Metadata)
		if strings.Contains(string(raw), "sk-live-secret") {
			t.Fatalf("the key reached the audit log: %s", raw)
		}
		if event.Scope == "agent_codex_api_key" && event.Metadata["action"] == "set_codex_api_key_account" {
			audited = event.Metadata["account_id"] == token.ID
		}
	}
	if !audited {
		t.Fatal("choosing the key was not audited by account id")
	}
}

// An account a session could not use is refused with a sentence, not an
// internal error.
func TestCodexAPIKeyAccountRefusesANonToken(t *testing.T) {
	svc, container := newBridgeWithAccounts(t)
	basic, err := container.Accounts.Create(accounts.CreateInput{
		Provider: "generic", Kind: accounts.KindBasic, Label: "basic", User: "u", Secret: "p",
	})
	if err != nil {
		t.Fatal(err)
	}
	err = svc.PreferencesSetCodexAPIKeyAccount(basic.ID)
	if err == nil || err.Error() != "validation_failed: Choose an API key (a token account) that is not limited to an environment." {
		t.Fatalf("basic account = %v", err)
	}
}

// Signing Codex out removes the Accounts note its sign-in left.
func TestAgentHubSignOutForgetsTheAccountsNote(t *testing.T) {
	svc, container := newBridgeWithAccounts(t)
	db, err := storage.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	reg := agenthub.NewRegistry()
	stub := &codexSignInStub{MockAdapter: agenthub.NewMockAdapter()}
	reg.Register(stub)
	container.AgentHub = agenthub.NewService(reg, agenthub.NewSQLStore(db), nil)

	if err := app.RecordAgentSignIn(container, agenthub.AgentTypeCodex); err != nil {
		t.Fatal(err)
	}
	if list, _ := container.Accounts.List(); len(list) != 1 {
		t.Fatalf("accounts = %+v, want the note", list)
	}
	if err := svc.AgentHubSignOut(context.Background(), "codex"); err != nil {
		t.Fatal(err)
	}
	if stub.signedOut != 1 {
		t.Fatal("the vendor was not signed out")
	}
	if list, _ := container.Accounts.List(); len(list) != 0 {
		t.Fatalf("accounts after sign-out = %+v", list)
	}
}
