package agenthub

import (
	"context"
	"errors"
	"testing"
)

// A chosen API key that fails — unreadable, or refused by Codex — is still a
// sign-in problem, but not one a ChatGPT sign-in fixes: the key wins at every
// spawn. It says so, so the UI can point at Settings instead of a sign-in
// that succeeds and changes nothing.
func TestCodexAPIKeyFailureIsNotAChatGPTSignIn(t *testing.T) {
	unreadable := newCodexFixture(t, "turn")
	unreadable.adapter.opts.APIKey = func(context.Context) (string, error) {
		return "", errors.New("accounts.notFound")
	}
	refused := newCodexFixture(t, "badkey")
	refused.adapter.opts.APIKey = func(context.Context) (string, error) { return "sk-revoked", nil }

	for name, f := range map[string]codexFixture{"unreadable": unreadable, "refused": refused} {
		_, err := f.adapter.Open(context.Background(), OpenSpec{Cwd: f.cwd})
		if !errors.Is(err, ErrAgentAuthRequired) || !errors.Is(err, ErrAgentAPIKeyUnusable) {
			t.Errorf("%s: Open = %v, want auth-required and api-key-unusable", name, err)
		}
		if ErrorVendor(err) != AgentTypeCodex {
			t.Errorf("%s: vendor = %q", name, ErrorVendor(err))
		}
	}
	// An unreadable key fails before any process starts; a refused one after
	// the login, and never reaches a thread.
	if len(refused.runner.sent(t, "thread/start")) != 0 {
		t.Error("a session with a refused key must not start a thread")
	}

	// The home of the "turn" fixture is signed in to ChatGPT, and that must
	// not be the answer while a key is chosen: sessions would not use it.
	status, err := unreadable.adapter.SignInStatus(context.Background())
	if err != nil || status != (SignInStatus{SignedIn: false, Method: "apiKey"}) {
		t.Fatalf("status with an unreadable key = %+v, %v", status, err)
	}

	// A signed-out ChatGPT home is plain auth-required, not a key problem.
	out := newCodexFixture(t, "signedout")
	if _, err := out.adapter.Open(context.Background(), OpenSpec{Cwd: out.cwd}); !errors.Is(err, ErrAgentAuthRequired) || errors.Is(err, ErrAgentAPIKeyUnusable) {
		t.Fatalf("signed out = %v, want auth-required only", err)
	}
}
