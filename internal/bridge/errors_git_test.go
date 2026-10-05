package bridge

import (
	"strings"
	"testing"

	gitservice "devkit/internal/git"
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
