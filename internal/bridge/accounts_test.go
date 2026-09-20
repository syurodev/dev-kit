package bridge

import (
	"errors"
	"path/filepath"
	"testing"

	"devkit/internal/accounts"
	"devkit/internal/app"
	"devkit/internal/preferences"
	"devkit/internal/storage"
	"devkit/internal/vault"
	"devkit/internal/vaultitem"
)

func newBridgeWithAccounts(t *testing.T) (*AppService, *app.Container) {
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
	container.Vault = v
	container.Items = vaultitem.NewStore(vaultitem.NewMemoryRepository(), v)
	container.Preferences = preferences.NewService(preferences.NewMemoryRepository())
	container.Accounts = accounts.NewService(
		container.Items, db, storage.TableAccountSecrets, v,
		accounts.NewRegistry(accounts.DefaultProviders()), container.Environments,
	)
	container.Accounts.SetHostChoices(db, storage.TableAccountHostChoices)
	return NewAppService(container), container
}

// The default is on, and the whole point of on is that a wrong answer does not
// hand the value over.
func TestRevealAsksForTheMasterPasswordByDefault(t *testing.T) {
	svc, container := newBridgeWithAccounts(t)
	created, err := container.Accounts.Create(accounts.CreateInput{
		Provider: "github", Kind: accounts.KindToken, Label: "GitHub", Secret: "ghp_x",
	})
	if err != nil {
		t.Fatal(err)
	}

	required, err := svc.AccountsRevealRequiresPassword()
	if err != nil {
		t.Fatal(err)
	}
	if !required {
		t.Fatal("the reveal policy must default to asking")
	}

	if _, err := svc.AccountsReveal(created.ID, "wrong"); err == nil {
		t.Fatal("a wrong password revealed the value")
	}
	// Checking a password is not unlocking: a mistyped answer in a dialog must
	// not lock a vault that is open.
	if container.Vault.IsLocked() {
		t.Fatal("a wrong answer locked the vault")
	}

	revealed, err := svc.AccountsReveal(created.ID, "correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	if revealed.Secret != "ghp_x" {
		t.Fatalf("revealed = %+v", revealed)
	}
}

// Turning it off is the user's call — the cost falls on exactly them — and it
// leaves reveal behind a confirmation in the UI rather than behind nothing at
// the service.
func TestRevealSkipsThePasswordOnceTurnedOff(t *testing.T) {
	svc, container := newBridgeWithAccounts(t)
	created, err := container.Accounts.Create(accounts.CreateInput{
		Provider: "github", Kind: accounts.KindToken, Label: "GitHub", Secret: "ghp_x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.AccountsSetRevealRequiresPassword(false); err != nil {
		t.Fatal(err)
	}
	revealed, err := svc.AccountsReveal(created.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if revealed.Secret != "ghp_x" {
		t.Fatalf("revealed = %+v", revealed)
	}
}

// Two accounts on one host is a question. Answering it once answers it for
// that pair and for no other.
func TestForHostAsksOnceAndRemembersAcrossTheBridge(t *testing.T) {
	svc, container := newBridgeWithAccounts(t)
	personal, err := container.Accounts.Create(accounts.CreateInput{
		Provider: "github", Kind: accounts.KindToken, Label: "Personal", Secret: "ghp_1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := container.Accounts.Create(accounts.CreateInput{
		Provider: "github", Kind: accounts.KindToken, Label: "Work", Secret: "ghp_2",
	}); err != nil {
		t.Fatal(err)
	}

	match, err := svc.AccountsForHost("api.github.com", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if match.Decided || len(match.Options) != 2 {
		t.Fatalf("two matches must be a question: %+v", match)
	}

	if err := svc.AccountsChooseForHost("api.github.com", "dev", personal.ID); err != nil {
		t.Fatal(err)
	}
	if match, err = svc.AccountsForHost("api.github.com", "dev"); err != nil {
		t.Fatal(err)
	}
	if !match.Decided || match.Account.ID != personal.ID {
		t.Fatalf("the answer was not remembered: %+v", match)
	}
	if onProd, err := svc.AccountsForHost("api.github.com", "prod"); err != nil || onProd.Decided {
		t.Fatalf("an answer for dev answered for prod: %+v %v", onProd, err)
	}
	if err := svc.AccountsForgetHostChoice("api.github.com", "dev"); err != nil {
		t.Fatal(err)
	}
	if again, err := svc.AccountsForHost("api.github.com", "dev"); err != nil || again.Decided {
		t.Fatalf("forgetting did not restore the question: %+v %v", again, err)
	}
}

// Nothing to reveal is its own answer, not "the account is missing".
func TestRevealOnAnExternalAccountSaysThereIsNothingToShow(t *testing.T) {
	svc, container := newBridgeWithAccounts(t)
	external, err := container.Accounts.Create(accounts.CreateInput{
		Provider: "anthropic", Kind: accounts.KindExternal, Label: "Claude", Ref: "claude login",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.AccountsSetRevealRequiresPassword(false); err != nil {
		t.Fatal(err)
	}
	_, err = svc.AccountsReveal(external.ID, "")
	if err == nil {
		t.Fatal("an external account revealed something")
	}
	if errors.Is(err, accounts.ErrNotFound) {
		t.Fatalf("reported as missing rather than as empty: %v", err)
	}
}
