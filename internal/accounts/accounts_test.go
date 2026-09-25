package accounts

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"devkit/internal/storage"
	"devkit/internal/vault"
	"devkit/internal/vaultitem"
)

type stubEnvLookup map[string]bool

func (s stubEnvLookup) Exists(name string) bool { return s[name] }

var testEnvs = stubEnvLookup{"dev": true, "stg": true, "prod": true}

func newService(t *testing.T) (*Service, *sql.DB) {
	t.Helper()
	v := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := v.Setup("correct-horse"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	db, err := storage.Open(t.TempDir() + "/devkit.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	items := vaultitem.NewStore(vaultitem.NewMemoryRepository(), v)
	svc := NewService(items, db, storage.TableAccountSecrets, v, NewRegistry(DefaultProviders()), testEnvs)
	return svc, db
}

func TestSharedAccountRoundTrips(t *testing.T) {
	svc, _ := newService(t)
	created, err := svc.Create(CreateInput{Provider: "github", Kind: KindToken, Label: "GitHub", Secret: "ghp_x"})
	if err != nil {
		t.Fatal(err)
	}
	if created.Scoped() {
		t.Fatal("an account with no owner must be shared")
	}
	cred, err := svc.Resolve(ResolveInput{AccountID: created.ID, By: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if cred.Secret != "ghp_x" {
		t.Fatalf("secret = %q", cred.Secret)
	}
}

// The listing and the Account that crosses the bridge must never carry a
// value; that is the whole reason Credential is a separate type.
func TestListedAccountsCarryNoSecret(t *testing.T) {
	svc, _ := newService(t)
	if _, err := svc.Create(CreateInput{Provider: "github", Kind: KindToken, Label: "GitHub", Secret: "ghp_secret"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(CreateInput{Provider: ProviderDatabase, Kind: KindSecret, Label: "staging", Secret: "db_secret", ScopeOwner: "profile-1"}); err != nil {
		t.Fatal(err)
	}
	list, err := svc.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("listed %d accounts", len(list))
	}
	for _, account := range list {
		for _, field := range []string{account.Label, account.Subject, account.Ref} {
			if field == "ghp_secret" || field == "db_secret" {
				t.Fatalf("a secret reached a listed account: %+v", account)
			}
		}
	}
	// Shared sorts before scoped: the scoped ones are the collapsed group.
	if list[0].Scoped() || !list[1].Scoped() {
		t.Fatalf("ordering put scoped first: %+v", list)
	}
}

// The invariant the whole split exists for.
func TestScopedAccountIsNotAVaultItem(t *testing.T) {
	svc, db := newService(t)
	scoped, err := svc.Create(CreateInput{
		Provider: ProviderDatabase, Kind: KindSecret, Label: "staging",
		Secret: "hunter2", ScopeOwner: "profile-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !scoped.Scoped() {
		t.Fatal("an account with an owner must be scoped")
	}
	records, err := svc.items.ListByType(vaultitem.ItemTypeAccount)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("a scoped account entered vault_items: %+v", records)
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + storage.TableAccountSecrets).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("scoped rows = %d, want 1", rows)
	}

	cred, err := svc.Resolve(ResolveInput{ScopeOwner: "profile-1"})
	if err != nil {
		t.Fatal(err)
	}
	if cred.Secret != "hunter2" {
		t.Fatalf("scoped secret = %q", cred.Secret)
	}
}

// Even in the local table the value is sealed; "does not sync" is not "not
// encrypted".
func TestScopedSecretIsSealedAtRest(t *testing.T) {
	svc, db := newService(t)
	if _, err := svc.Create(CreateInput{Provider: ProviderSSH, Kind: KindSecret, Label: "key", Secret: "passphrase-abc", ScopeOwner: "profile-1"}); err != nil {
		t.Fatal(err)
	}
	var envelope string
	if err := db.QueryRow(`SELECT envelope FROM ` + storage.TableAccountSecrets).Scan(&envelope); err != nil {
		t.Fatal(err)
	}
	if envelope == "" || contains(envelope, "passphrase-abc") {
		t.Fatalf("plaintext in the stored envelope: %q", envelope)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

// A profile synced from another device has no local secret. That must read as
// a sentence, not as a decryption failure or a dangling id.
func TestProfileWithoutALocalSecretSaysSo(t *testing.T) {
	svc, _ := newService(t)
	account, err := svc.ForOwner("profile-from-another-device")
	if err != nil {
		t.Fatal(err)
	}
	if account.State != StateNeedsLocalSecret {
		t.Fatalf("state = %q, want %q", account.State, StateNeedsLocalSecret)
	}
}

// Rotating in place is why bindings hold an id rather than a value.
func TestRotateKeepsTheIDSoBindingsKeepWorking(t *testing.T) {
	svc, _ := newService(t)
	created, err := svc.Create(CreateInput{Provider: "github", Kind: KindToken, Label: "GitHub", Secret: "old"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Rotate(created.ID, "", "new"); err != nil {
		t.Fatal(err)
	}
	cred, err := svc.Resolve(ResolveInput{AccountID: created.ID})
	if err != nil {
		t.Fatal(err)
	}
	if cred.Secret != "new" {
		t.Fatalf("secret after rotate = %q", cred.Secret)
	}
	after, err := svc.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.ID != created.ID {
		t.Fatal("rotate changed the id")
	}
	if after.State != StateUnverified {
		t.Fatalf("state after rotate = %q; a new value is not a verified one", after.State)
	}
}

func TestEnvRestrictionIsEnforcedAtResolve(t *testing.T) {
	svc, _ := newService(t)
	created, err := svc.Create(CreateInput{
		Provider: "github", Kind: KindToken, Label: "prod deploy",
		Secret: "ghp_x", Env: []string{"prod"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resolve(ResolveInput{AccountID: created.ID, Env: "dev"}); !errors.Is(err, ErrEnvNotAllowed) {
		t.Fatalf("resolving a prod-only account on dev = %v, want ErrEnvNotAllowed", err)
	}
	if _, err := svc.Resolve(ResolveInput{AccountID: created.ID, Env: "prod"}); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownEnvTagIsRejected(t *testing.T) {
	svc, _ := newService(t)
	if _, err := svc.Create(CreateInput{Provider: "github", Kind: KindToken, Label: "x", Secret: "y", Env: []string{"nope"}}); err == nil {
		t.Fatal("an account was restricted to an environment that does not exist")
	}
}

// KindExternal exists so the list can name an identity DevKit deliberately
// does not hold. Resolving one must give nothing rather than an empty string
// that reads like a credential.
func TestExternalAccountHasNothingToResolve(t *testing.T) {
	svc, _ := newService(t)
	created, err := svc.Create(CreateInput{
		Provider: ProviderGeneric, Kind: KindExternal, Label: "Claude sign-in",
		Ref: "run `claude login`",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resolve(ResolveInput{AccountID: created.ID}); err == nil {
		t.Fatal("an external account handed back a credential")
	}
	if err := svc.Rotate(created.ID, "", "x"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("rotating an external account = %v, want ErrInvalid", err)
	}
}

type stubUsage map[string][]string

func (s stubUsage) UsedBy(id string) []string { return s[id] }

func TestDeleteRefusesAnAccountStillBound(t *testing.T) {
	svc, _ := newService(t)
	created, err := svc.Create(CreateInput{Provider: "github", Kind: KindToken, Label: "GitHub", Secret: "x"})
	if err != nil {
		t.Fatal(err)
	}
	svc.RegisterUsage(stubUsage{created.ID: {"git remote origin"}})
	err = svc.Delete(created.ID)
	var inUse *InUseError
	if !errors.As(err, &inUse) {
		t.Fatalf("delete = %v, want InUseError", err)
	}
	if len(inUse.UsedBy) != 1 || inUse.UsedBy[0] != "git remote origin" {
		t.Fatalf("refusal did not name what binds it: %+v", inUse)
	}
}

// Promoting is the one way a connection's credential starts syncing, and it
// must be a move rather than a copy.
func TestPromoteMovesAScopedSecretIntoTheVault(t *testing.T) {
	svc, db := newService(t)
	scoped, err := svc.Create(CreateInput{
		Provider: ProviderDatabase, Kind: KindSecret, Label: "staging",
		Secret: "hunter2", ScopeOwner: "profile-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	shared, err := svc.Promote(scoped.ID, "Shared staging")
	if err != nil {
		t.Fatal(err)
	}
	if shared.Scoped() {
		t.Fatal("promote left the account scoped")
	}
	cred, err := svc.Resolve(ResolveInput{AccountID: shared.ID})
	if err != nil || cred.Secret != "hunter2" {
		t.Fatalf("promoted secret = %q, %v", cred.Secret, err)
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + storage.TableAccountSecrets).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("promote copied instead of moved: %d local rows left", rows)
	}
}

func TestSweepOrphansRemovesSecretsWhoseProfileIsGone(t *testing.T) {
	svc, _ := newService(t)
	for _, owner := range []string{"alive", "deleted-elsewhere"} {
		if _, err := svc.Create(CreateInput{Provider: ProviderDatabase, Kind: KindSecret, Label: owner, Secret: "x", ScopeOwner: owner}); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := svc.SweepOrphans([]string{"alive"})
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("swept %d, want 1", removed)
	}
	if account, _ := svc.ForOwner("alive"); account.State == StateNeedsLocalSecret {
		t.Fatal("the live profile's secret was swept")
	}
}

func TestExpiryIsDerivedNotStored(t *testing.T) {
	svc, _ := newService(t)
	now := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	soon, err := svc.Create(CreateInput{
		Provider: "github", Kind: KindToken, Label: "expiring", Secret: "x",
		ExpiresAt: now.Add(3 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Created unverified, so the stored state stands until verification runs.
	if soon.State != StateUnverified {
		t.Fatalf("state = %q", soon.State)
	}

	// Once verified, expiry is recomputed from the clock: it passes with
	// nobody writing anything.
	verified := soon
	verified.State = StateValid
	if got := derivedState(verified.State, verified.ExpiresAt, now); got != StateExpiring {
		t.Fatalf("three days out = %q, want %q", got, StateExpiring)
	}
	if got := derivedState(verified.State, verified.ExpiresAt, now.Add(30*24*time.Hour)); got != StateExpired {
		t.Fatalf("past expiry = %q, want %q", got, StateExpired)
	}
	// A revoked account stays revoked whatever the clock says.
	if got := derivedState(StateRevoked, verified.ExpiresAt, now); got != StateRevoked {
		t.Fatalf("revoked became %q", got)
	}
}

func TestProviderHostMatchIsExactOrASuffix(t *testing.T) {
	reg := NewRegistry(DefaultProviders())
	if got := reg.ProvidersForHost("api.github.com"); len(got) != 1 || got[0] != "github" {
		t.Fatalf("api.github.com -> %v", got)
	}
	// A lookalike must not match: this is the difference between a suffix
	// check and a substring one.
	if got := reg.ProvidersForHost("evil-github.com.attacker.test"); len(got) != 0 {
		t.Fatalf("lookalike host matched %v", got)
	}
	if got := reg.ProvidersForHost("unknown.example"); len(got) != 0 {
		t.Fatalf("unknown host matched %v", got)
	}
}

func TestScopedAccountCannotCarryAnEnvRestriction(t *testing.T) {
	svc, _ := newService(t)
	_, err := svc.Create(CreateInput{
		Provider: ProviderDatabase, Kind: KindSecret, Label: "x", Secret: "y",
		ScopeOwner: "profile-1", Env: []string{"prod"},
	})
	if err == nil {
		t.Fatal("a scoped account took an env restriction; the connection already carries one")
	}
}

// Replacing a scoped credential must keep the row's id, because the envelope
// is sealed with that id in its AAD. Minting a fresh one sealed the new secret
// against an id the row never carried, and the next read failed
// authentication — a rotation that destroyed what it was rotating.
func TestReplacingAScopedSecretKeepsItReadable(t *testing.T) {
	svc, _ := newService(t)
	first, err := svc.Create(CreateInput{
		Provider: ProviderDatabase, Kind: KindSecret, Label: "staging",
		Secret: "old", ScopeOwner: "profile-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.PutForOwner("profile-1", ProviderDatabase, "staging", "", "new"); err != nil {
		t.Fatal(err)
	}

	_, secret, found, err := svc.SecretForOwner("profile-1")
	if err != nil || !found || secret != "new" {
		t.Fatalf("secret after replace = %q, found=%v, %v", secret, found, err)
	}
	after, err := svc.ForOwner("profile-1")
	if err != nil {
		t.Fatal(err)
	}
	if after.ID != first.ID {
		t.Fatalf("the row's id changed from %s to %s", first.ID, after.ID)
	}
}

// Reveal is the one method here whose result is meant to cross the bridge. It
// exists because the alternative is worse: somebody who cannot read back the
// token they saved keeps a second copy somewhere DevKit did not seal.
func TestRevealReturnsTheValueForBothKindsOfAccount(t *testing.T) {
	svc, _ := newService(t)
	shared, err := svc.Create(CreateInput{
		Provider: "github", Kind: KindToken, Label: "GitHub", User: "octocat", Secret: "ghp_x",
	})
	if err != nil {
		t.Fatal(err)
	}
	credential, err := svc.Reveal(shared.ID)
	if err != nil {
		t.Fatal(err)
	}
	if credential.Secret != "ghp_x" || credential.User != "octocat" {
		t.Fatalf("revealed = %+v", credential)
	}

	if err := svc.PutForOwner("owner-1", ProviderDatabase, "staging", "postgres", "hunter2"); err != nil {
		t.Fatal(err)
	}
	scoped, err := svc.ForOwner("owner-1")
	if err != nil {
		t.Fatal(err)
	}
	if credential, err = svc.Reveal(scoped.ID); err != nil {
		t.Fatal(err)
	}
	if credential.Secret != "hunter2" {
		t.Fatalf("a scoped account did not reveal: %+v", credential)
	}
}

// An external account records that an identity exists somewhere DevKit does
// not hold it. There is nothing to reveal, and saying so is not an error about
// the account being missing.
func TestRevealRefusesAnExternalAccount(t *testing.T) {
	svc, _ := newService(t)
	external, err := svc.Create(CreateInput{
		Provider: "anthropic", Kind: KindExternal, Label: "Claude", Ref: "claude login",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Reveal(external.ID); !errors.Is(err, ErrNothingToReveal) {
		t.Fatalf("Reveal on an external account = %v, want ErrNothingToReveal", err)
	}
}

// Revealing is not using. An account limited to prod is still the user's to
// read, so the env restriction — which guards Resolve — must not guard this.
func TestRevealIgnoresTheEnvironmentRestriction(t *testing.T) {
	svc, _ := newService(t)
	restricted, err := svc.Create(CreateInput{
		Provider: "github", Kind: KindToken, Label: "Prod only", Secret: "ghp_p", Env: []string{"prod"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resolve(ResolveInput{AccountID: restricted.ID, Env: "dev", By: "test"}); !errors.Is(err, ErrEnvNotAllowed) {
		t.Fatalf("resolving on the wrong env = %v, want ErrEnvNotAllowed", err)
	}
	credential, err := svc.Reveal(restricted.ID)
	if err != nil {
		t.Fatalf("reveal was blocked by an env restriction: %v", err)
	}
	if credential.Secret != "ghp_p" {
		t.Fatalf("revealed = %+v", credential)
	}
}

// A vendor sign-in DevKit does not hold is one row however often it happens,
// and goes when the sign-in does.
func TestExternalAccountIsRecordedOnceAndForgotten(t *testing.T) {
	svc, _ := newService(t)
	first, err := svc.RecordExternal("openai", "Codex (ChatGPT)", "Settings → Agents → Codex")
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.RecordExternal("openai", "Codex (ChatGPT)", "Settings → Agents → Codex")
	if err != nil || again.ID != first.ID {
		t.Fatalf("second record = %+v, %v; want the first row back", again, err)
	}
	if first.Kind != KindExternal || first.Ref != "Settings → Agents → Codex" {
		t.Fatalf("row = %+v", first)
	}
	// A token under the same provider and label is not the note.
	if _, err := svc.Create(CreateInput{Provider: "openai", Kind: KindToken, Label: "Codex (ChatGPT)", Secret: "sk-x"}); err != nil {
		t.Fatal(err)
	}
	list, _ := svc.List()
	if n := countKind(list, KindExternal); n != 1 {
		t.Fatalf("%d external rows, want 1", n)
	}
	if err := svc.ForgetExternal("openai", "Codex (ChatGPT)"); err != nil {
		t.Fatal(err)
	}
	if err := svc.ForgetExternal("openai", "Codex (ChatGPT)"); err != nil {
		t.Fatalf("forgetting twice = %v", err)
	}
	list, _ = svc.List()
	if n := countKind(list, KindExternal); n != 0 {
		t.Fatalf("%d external rows after forgetting, want 0", n)
	}
	if n := countKind(list, KindToken); n != 1 {
		t.Fatalf("forgetting the note took the token with it: %+v", list)
	}
}

func countKind(list []Account, kind Kind) int {
	n := 0
	for _, a := range list {
		if a.Kind == kind {
			n++
		}
	}
	return n
}
