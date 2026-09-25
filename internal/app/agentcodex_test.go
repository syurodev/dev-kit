package app

import (
	"context"
	"errors"
	"testing"

	"devkit/internal/accounts"
	"devkit/internal/agenthub"
)

// The key Codex signs in with is an Accounts token chosen by id: resolved at
// spawn, rotated in one place, and never a value preferences hold.
func TestCodexAPIKeyResolvesTheChosenToken(t *testing.T) {
	c := credentialContainer(t)
	c.Accounts.RegisterUsage(CodexAPIKey{C: c})
	key := CodexAPIKey{C: c}

	if got, err := key.Key(context.Background()); err != nil || got != "" {
		t.Fatalf("nothing chosen = %q, %v; want ChatGPT", got, err)
	}
	token, err := c.Accounts.Create(accounts.CreateInput{Provider: "openai", Kind: accounts.KindToken, Label: "OpenAI", Secret: "sk-first"})
	if err != nil {
		t.Fatal(err)
	}
	if err := key.Choose(token.ID); err != nil {
		t.Fatal(err)
	}
	if stored, _ := c.Preferences.CodexAPIKeyAccount(); stored != token.ID {
		t.Fatalf("preferences hold %q, want the account id", stored)
	}
	if got, err := key.Key(context.Background()); err != nil || got != "sk-first" {
		t.Fatalf("Key = %q, %v", got, err)
	}
	if err := c.Accounts.Rotate(token.ID, "", "sk-second"); err != nil {
		t.Fatal(err)
	}
	if got, _ := key.Key(context.Background()); got != "sk-second" {
		t.Fatalf("after rotate Key = %q, want the new value", got)
	}

	// Deleting it from under Codex is refused and says why.
	var inUse *accounts.InUseError
	if err := c.Accounts.Delete(token.ID); !errors.As(err, &inUse) || inUse.UsedBy[0] != codexAPIKeyUsage {
		t.Fatalf("delete = %v, want InUseError naming Codex", err)
	}
	if err := key.Choose(""); err != nil {
		t.Fatal(err)
	}
	if got, err := key.Key(context.Background()); err != nil || got != "" {
		t.Fatalf("cleared = %q, %v", got, err)
	}
	if err := c.Accounts.Delete(token.ID); err != nil {
		t.Fatalf("delete after unlinking = %v", err)
	}
}

// Choose refuses what a spawn could not use, so the mistake shows in
// Settings rather than as a failed session later.
func TestCodexAPIKeyRefusesAnAccountItCannotUse(t *testing.T) {
	c := credentialContainer(t)
	key := CodexAPIKey{C: c}
	basic, _ := c.Accounts.Create(accounts.CreateInput{Provider: "generic", Kind: accounts.KindBasic, Label: "basic", User: "u", Secret: "p"})
	limited, _ := c.Accounts.Create(accounts.CreateInput{Provider: "openai", Kind: accounts.KindToken, Label: "prod only", Secret: "sk", Env: []string{"prod"}})
	scoped, _ := c.Accounts.Create(accounts.CreateInput{Provider: "database", Kind: accounts.KindSecret, Label: "db", Secret: "pw", ScopeOwner: "0123456789abcdef01234567"})
	for name, id := range map[string]string{
		"basic":      basic.ID,
		"env-bound":  limited.ID,
		"scoped":     scoped.ID,
		"missing":    "fedcba9876543210fedcba98",
		"not an id!": "../../etc/passwd",
	} {
		if err := key.Choose(id); !errors.Is(err, ErrCodexAPIKeyAccount) {
			t.Errorf("%s: Choose = %v, want ErrCodexAPIKeyAccount", name, err)
		}
	}
	if stored, _ := c.Preferences.CodexAPIKeyAccount(); stored != "" {
		t.Fatalf("a refused choice was stored: %q", stored)
	}
	// Chosen while it was fine, then gone: the session is told, not handed "".
	good, _ := c.Accounts.Create(accounts.CreateInput{Provider: "openai", Kind: accounts.KindToken, Label: "ok", Secret: "sk"})
	if err := key.Choose(good.ID); err != nil {
		t.Fatal(err)
	}
	if err := c.Preferences.SetCodexAPIKeyAccount("fedcba9876543210fedcba98"); err != nil {
		t.Fatal(err)
	}
	if _, err := key.Key(context.Background()); err == nil {
		t.Fatal("a chosen key that is gone must be an error, not a silent ChatGPT fallback")
	}
}

// Codex's ChatGPT sign-in shows in Accounts as one external row, and goes
// with the sign-in. Other agents record nothing.
func TestAgentSignInIsNotedInAccounts(t *testing.T) {
	c := credentialContainer(t)
	for range 2 {
		if err := RecordAgentSignIn(c, agenthub.AgentTypeCodex); err != nil {
			t.Fatal(err)
		}
	}
	if err := RecordAgentSignIn(c, agenthub.AgentTypeClaude); err != nil {
		t.Fatal(err)
	}
	list, _ := c.Accounts.List()
	if len(list) != 1 || list[0].Kind != accounts.KindExternal || list[0].Label != CodexExternalLabel || list[0].Provider != "openai" {
		t.Fatalf("accounts = %+v, want one Codex (ChatGPT) note", list)
	}
	if err := ForgetAgentSignIn(c, agenthub.AgentTypeCodex); err != nil {
		t.Fatal(err)
	}
	if list, _ := c.Accounts.List(); len(list) != 0 {
		t.Fatalf("accounts after sign-out = %+v", list)
	}
	if RecordAgentSignIn(nil, agenthub.AgentTypeCodex) != nil || ForgetAgentSignIn(&Container{}, agenthub.AgentTypeCodex) != nil {
		t.Fatal("no Accounts is nothing to record, not an error")
	}
}
