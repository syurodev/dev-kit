package agenthub

import (
	"context"
	"errors"
)

// ErrUsageNotCheckable: the vendor could not say what the account has left (signed
// out, an API-key login with no plan limits, a CLI that does not know the request).
var ErrUsageNotCheckable = errors.New("agenthub.usageNotCheckable")

// SignInState is what a vendor's own status check says about a home.
type SignInState struct {
	SignedIn bool
	Email    string
	Plan     string
}

// UsageChecker asks the vendor, through its own CLI under the session jail and with
// spec's account home, what an account has left. It returns the account-wide windows
// (other people's use of a shared login included) and never reads a token.
//
// The caller builds spec like accountProbeSpec does: the account's home, the shared
// store, the deny-read paths carved around them and the private/shared home choice. A
// probe runs under the same jail a session does, so a spec without them probes a wider
// jail than a session would have. For an added account (AccountHome set) the Claude
// probe also drops the host's credential variables, so it describes the account's own
// login and never a host key.
type UsageChecker interface {
	CheckUsage(ctx context.Context, spec OpenSpec) ([]LimitWindow, error)
}

// SignInChecker asks the vendor whether a home is signed in, without a prompt. The
// caller builds spec as for UsageChecker (accountProbeSpec-style fields).
type SignInChecker interface {
	CheckSignIn(ctx context.Context, spec OpenSpec) (SignInState, error)
}

// RateLimitReader is a running session's own connection answering the same question
// as a UsageChecker. Used instead of starting a second process on a home a session is
// already using (two processes could refresh the login's tokens at once).
type RateLimitReader interface {
	ReadRateLimits(ctx context.Context) ([]LimitWindow, error)
}

// AccountAuthProvider is a vendor that signs in through a terminal (Claude): the same
// command as SignInCommand, run with an account's home as its config directory.
type AccountAuthProvider interface {
	SignInCommandFor(ctx context.Context, methodID, home string) (SignInCommand, error)
}

// AccountInAppSignIn is a vendor that signs in inside the app (Codex), at an account's
// home. spec is built like accountProbeSpec's (the home, the shared store, the deny
// paths, the private/shared home choice): the login runs under the same jail a session
// does. accountID rides on the result so the window can tell it from the built-in
// sign-in. Starting one holds the loopback callback port, so it cancels another
// sign-in in progress (for any account, the built-in one included).
type AccountInAppSignIn interface {
	StartBrowserSignInAt(ctx context.Context, spec OpenSpec, accountID string) (SignInFlow, error)
	StartDeviceSignInAt(ctx context.Context, spec OpenSpec, accountID string) (SignInFlow, error)
}
