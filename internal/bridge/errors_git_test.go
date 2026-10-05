package bridge

import (
	"strings"
	"testing"

	"devkit/internal/accounts"
	gitservice "devkit/internal/git"
	"devkit/internal/gitssh"
	"devkit/internal/interaction"
)

func TestPublicErrorNamesTheAccountAGitRemoteRefused(t *testing.T) {
	cause := &gitservice.AuthError{
		Op: "push", Remote: "origin", Transport: gitservice.TransportHTTP,
		AccountID: "acct-1", Account: "github · acme-dev",
	}
	err := publicError("builtin.git", cause)
	if err == nil {
		t.Fatal("no public error")
	}
	text := err.Error()
	for _, want := range []string{"git_auth_failed", "origin", "github · acme-dev"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q does not mention %q", text, want)
		}
	}
}

func TestPublicErrorForARefusedSSHKeyBlamesNoAccount(t *testing.T) {
	err := publicError("builtin.git", &gitservice.AuthError{
		Op: "push", Remote: "origin", Transport: gitservice.TransportSSH,
	})
	if err == nil || !strings.Contains(err.Error(), "SSH key") || strings.Contains(err.Error(), "account \u201c") {
		t.Fatalf("public error = %v", err)
	}
}

// The address in git's own message can carry a password; the public message is
// built from the remote's name and the account's label alone.
func TestPublicGitAuthErrorLeaksNothingFromGitsOwnMessage(t *testing.T) {
	cause := &gitservice.AuthError{
		Op: "fetch", Remote: "origin", Transport: gitservice.TransportHTTP,
		URL: "https://octocat@github.com/acme/api.git",
		Err: errString("fatal: Authentication failed for 'https://octocat" + ":ghp_secret" + "@github.com/acme/api.git/'"),
	}
	err := publicError("builtin.git", cause)
	if err == nil || strings.Contains(err.Error(), "ghp_secret") || strings.Contains(err.Error(), "github.com") {
		t.Fatalf("public error = %v", err)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// Two accounts fit and nobody can be asked (server mode, a headless agent). The
// generic "Selection required." says nothing about what to do.
func TestPublicErrorForAGitRemoteNobodyCanAskAboutSaysWhatToDo(t *testing.T) {
	err := publicError("builtin.git", interaction.ErrSelectionRequired)
	if err == nil || !strings.Contains(err.Error(), "Bind an account to the remote") {
		t.Fatalf("public error = %v", err)
	}
	// Other subjects keep the generic wording.
	if other := publicError("builtin.ssh", interaction.ErrSelectionRequired); other == nil || other.Error() != "validation_failed: Selection required." {
		t.Fatalf("another subject's error changed: %v", other)
	}
}

func TestPublicErrorForARefusedSSHKeyNamesTheAccountWhenOneWasUsed(t *testing.T) {
	err := publicError("builtin.git", &gitservice.AuthError{
		Op: "push", Remote: "origin", Transport: gitservice.TransportSSH,
		AccountID: "acct-key", Account: "github · acme-dev",
	})
	if err == nil || !strings.Contains(err.Error(), "SSH key of account “github · acme-dev”") {
		t.Fatalf("public error = %v", err)
	}
}

func TestPublicErrorWordsAKeyThatCannotBeSaved(t *testing.T) {
	for cause, want := range map[error]string{
		accounts.ErrKeyInvalid:    "private key ssh can read",
		accounts.ErrKeyPassphrase: "passphrase",
	} {
		err := publicError("builtin.accounts", cause)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%v: public error = %v", cause, err)
		}
	}
}

// A key that cannot be served stops the push, and the message says that is why
// rather than "An unexpected error occurred."
func TestPublicErrorSaysWhySSHKeyPushStopped(t *testing.T) {
	for cause, want := range map[error]string{
		gitssh.ErrUnsupported:        "not supported on this platform",
		gitssh.ErrNoSSH:              "no ssh program",
		gitssh.ErrBadPassphrase:      "can no longer be opened",
		gitssh.ErrNotAKey:            "can no longer be opened",
		gitssh.ErrPassphraseRequired: "can no longer be opened",
	} {
		err := publicError("builtin.git", cause)
		if err == nil || !strings.Contains(err.Error(), want) || strings.Contains(err.Error(), "unexpected") {
			t.Errorf("%v: public error = %v", cause, err)
		}
	}
}
