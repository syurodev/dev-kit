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
type UsageChecker interface {
	CheckUsage(ctx context.Context, spec OpenSpec) ([]LimitWindow, error)
}

// SignInChecker asks the vendor whether a home is signed in, without a prompt.
type SignInChecker interface {
	CheckSignIn(ctx context.Context, spec OpenSpec) (SignInState, error)
}

// RateLimitReader is a running session's own connection answering the same question
// as a UsageChecker. Used instead of starting a second process on a home a session is
// already using (two processes could refresh the login's tokens at once).
type RateLimitReader interface {
	ReadRateLimits(ctx context.Context) ([]LimitWindow, error)
}
