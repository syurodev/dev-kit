package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"devkit/internal/accounts"
	"devkit/internal/agenthub"
	"devkit/internal/capabilities"
	"devkit/internal/identity"
)

// ErrCodexAPIKeyAccount is an account that cannot be Codex's API key: gone,
// not a token, a single connection's secret, or limited to an environment
// (a spawn has none to offer, so Resolve would refuse it every time). The
// sentence the person reads is the bridge's (codexAPIKeyAccountMessage).
var ErrCodexAPIKeyAccount = errors.New("app.codexAPIKeyAccount")

// The note Accounts keeps of Codex's ChatGPT sign-in. The credential itself
// is in Codex's own home; this row only answers "what am I signed in to".
const (
	codexExternalProvider = "openai"
	CodexExternalLabel    = "Codex (ChatGPT)"
	codexExternalRef      = "Sign in again in Settings → Agents → Codex."
)

// codexAPIKeyUsage is what Accounts says binds the chosen key.
const codexAPIKeyUsage = "Agent Hub: Codex API key"

// CodexAPIKey is the API key Codex sessions sign in with instead of ChatGPT:
// an Accounts token, chosen in Settings, named in preferences by id and
// resolved here at spawn (Codex spec §6.2). The key lives in Accounts and
// nowhere else — rotating it there is all it takes — and it reaches Codex
// over the wire after initialize, never on disk, in the environment or argv.
//
// It holds the container rather than the two services because main builds
// the Codex adapter before the database and Accounts exist; both are read at
// call time.
type CodexAPIKey struct{ C *Container }

// Key is CodexOptions.APIKey: the chosen key, or "" for ChatGPT.
func (k CodexAPIKey) Key(context.Context) (string, error) {
	id, err := k.chosen()
	if err != nil || id == "" {
		return "", err
	}
	if k.C.Accounts == nil {
		return "", accounts.ErrNotFound
	}
	credential, err := k.C.Accounts.Resolve(accounts.ResolveInput{AccountID: id, By: "agenthub.codex"})
	if err != nil {
		return "", err
	}
	if credential.Kind != accounts.KindToken || strings.TrimSpace(credential.Secret) == "" {
		return "", ErrCodexAPIKeyAccount
	}
	return credential.Secret, nil
}

// Choose stores which account is the key; "" goes back to ChatGPT. It
// refuses an account a spawn could not resolve, so the mistake is made here
// and not at the next session.
//
// The person's only, like signing in: the key decides whose account every
// later session runs as. No MCP tool reaches this and the MCP subject holds
// no preferences grant; a caller the gateway stamped as an agent or a
// terminal MCP client is refused here as well, so a door added by mistake
// later still opens onto nothing.
func (k CodexAPIKey) Choose(ctx context.Context, accountID string) error {
	if caller, ok := identity.FromContext(ctx); ok && caller.Kind != identity.KindUI {
		return fmt.Errorf("%w: choosing Codex's API key is the person's, from the app", capabilities.ErrPermissionDenied)
	}
	if k.C.Preferences == nil {
		return errors.New("preferences are not available")
	}
	accountID = strings.TrimSpace(accountID)
	if accountID != "" {
		if k.C.Accounts == nil {
			return ErrCodexAPIKeyAccount
		}
		account, err := k.C.Accounts.Get(accountID)
		if err != nil || account.Kind != accounts.KindToken || account.Scoped() || len(account.Env) > 0 {
			return ErrCodexAPIKeyAccount
		}
	}
	return k.C.Preferences.SetCodexAPIKeyAccount(accountID)
}

// UsedBy is accounts.Usage: the chosen key cannot be deleted from under
// Codex without saying so.
func (k CodexAPIKey) UsedBy(accountID string) []string {
	id, err := k.chosen()
	if err != nil || id == "" || id != accountID {
		return nil
	}
	return []string{codexAPIKeyUsage}
}

func (k CodexAPIKey) chosen() (string, error) {
	if k.C == nil || k.C.Preferences == nil {
		return "", nil
	}
	return k.C.Preferences.CodexAPIKeyAccount()
}

// RecordAgentSignIn notes in Accounts that an agent signed in inside DevKit
// (a KindExternal row: no secret, how to sign in again). Only Codex does.
func RecordAgentSignIn(c *Container, agent agenthub.AgentType) error {
	if agent != agenthub.AgentTypeCodex || c == nil || c.Accounts == nil {
		return nil
	}
	_, err := c.Accounts.RecordExternal(codexExternalProvider, CodexExternalLabel, codexExternalRef)
	return err
}

// ForgetAgentSignIn removes that note after the agent signed out.
func ForgetAgentSignIn(c *Container, agent agenthub.AgentType) error {
	if agent != agenthub.AgentTypeCodex || c == nil || c.Accounts == nil {
		return nil
	}
	return c.Accounts.ForgetExternal(codexExternalProvider, CodexExternalLabel)
}
