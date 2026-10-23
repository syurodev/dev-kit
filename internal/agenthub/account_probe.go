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
