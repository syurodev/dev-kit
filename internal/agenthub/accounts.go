package agenthub

import (
	"context"
	"errors"
	"strings"
	"time"
)

// ErrAccountNotFound is an account id the book does not hold.
var ErrAccountNotFound = errors.New("agenthub.accountNotFound")

// ErrInvalidAccount is a record Put refuses: no id or vendor, or a home that
// would turn the built-in account into a second one or the reverse.
var ErrInvalidAccount = errors.New("agenthub.invalidAccount")

// AccountRecord is one vendor account a person can run sessions as. It holds no
// credential: the vendor CLI keeps its login in HomeDir, and the built-in
// account (HomeDir "") is the machine's own Claude login or DevKit's own Codex
// home, exactly what sessions ran as before accounts existed.
type AccountRecord struct {
	ID       string
	Vendor   AgentType
	Label    string
	HomeDir  string
	SignedIn bool
	// Shared says other people also use this login (a team account), so what
	// this app's sessions observed is only a lower bound of its usage.
	Shared bool
	// ExhaustedUntil is when a real limit refusal says the account is usable
	// again; zero (or past) means not marked.
	ExhaustedUntil time.Time
	Usage          []LimitWindow
	// UsageSource is "observed" (from a session's own updates) or "checked"
	// (asked of the vendor). Empty when there is no reading.
	UsageSource string
	ObservedAt  time.Time
}

// Builtin reports whether this is the vendor's built-in account.
func (a AccountRecord) Builtin() bool { return a.HomeDir == "" }

// BuiltinAccountID is the id of a vendor's built-in account.
func BuiltinAccountID(vendor AgentType) string { return string(vendor) + "-default" }

func builtinAccountLabel(vendor AgentType) string {
	if vendor == AgentTypeClaude {
		return "System default"
	}
	return "DevKit default"
}

// AccountBook is where accounts and what is known of their usage are kept. nil
// on Service means the feature is off and every session runs as before.
type AccountBook interface {
	// List returns the vendor's accounts, its built-in one first, creating that
	// one on first use.
	List(ctx context.Context, vendor AgentType) ([]AccountRecord, error)
	Get(ctx context.Context, id string) (AccountRecord, error)
	// Put adds an account or edits its label, home, sign-in state and shared
	// flag. It never touches usage or the exhausted marker.
	Put(ctx context.Context, a AccountRecord) error
	// SaveUsage merges windows into the stored ones by label.
	SaveUsage(ctx context.Context, id string, windows []LimitWindow, source string, at time.Time) error
	// MarkExhausted moves the marker later, never earlier.
	MarkExhausted(ctx context.Context, id string, until time.Time) error
}

// mergeWindows returns old with each of fresh replacing the window of the same
// label, in old's order, new labels last.
func mergeWindows(old, fresh []LimitWindow) []LimitWindow {
	out := append([]LimitWindow(nil), old...)
	for _, w := range fresh {
		replaced := false
		for i := range out {
			if out[i].Label == w.Label {
				out[i] = w
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, w)
		}
	}
	return out
}

// isWeeklyLabel reports whether a window is one of the weekly ones ("Weekly",
// "Weekly · Opus", "7-day").
func isWeeklyLabel(label string) bool {
	l := strings.ToLower(label)
	return strings.Contains(l, "week") || strings.HasPrefix(l, "7-day")
}
