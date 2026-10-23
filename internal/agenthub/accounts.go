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
	// CooldownUntil keeps the account out of selection until then without saying
	// it reached a limit: a reopen in its home failed. Zero or past: not cooling.
	CooldownUntil time.Time
	Usage         []LimitWindow
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
	// SaveUsage merges windows into the stored ones by label. A reading that is
	// stale for every window it carries (a lower percentage of a window already
	// stored with the same reset), and an empty list, change nothing: usage and
	// its observed time stay as they were. An unknown id is ErrAccountNotFound.
	SaveUsage(ctx context.Context, id string, windows []LimitWindow, source string, at time.Time) error
	// MarkExhausted moves the marker later, never earlier.
	MarkExhausted(ctx context.Context, id string, until time.Time) error
	// Delete removes an added account and returns its sessions to the built-in
	// account. The built-in account cannot be deleted.
	Delete(ctx context.Context, id string) error
	// SetSignedIn records what the vendor's own status said.
	SetSignedIn(ctx context.Context, id string, signedIn bool) error
	// ReplaceUsage stores windows as the whole reading: an on-demand check returns
	// the full set, unlike an observed update, which carries some windows.
	ReplaceUsage(ctx context.Context, id string, windows []LimitWindow, source string, at time.Time) error
	// MarkCooldown moves the cooldown later, never earlier.
	MarkCooldown(ctx context.Context, id string, until time.Time) error
}

// mergeWindowsReport returns old with each of fresh replacing the window of the same
// label, in old's order, new labels last.
//
// A reading is not replaced by an older one of the same window: usage only
// grows within one window, and two sessions on one account each re-send their
// own last-known snapshot, so a lower percentage with the same label and the
// same known reset is the stale one and the stored window wins entirely
// (percentage, reset and status). A different reset is a new window and a
// window with no known reset (ResetsAt 0) cannot be told apart, so both replace.
//
// It also says whether any fresh window was accepted (replaced one, or was
// appended), as opposed to every one being rejected as a stale reading of a
// window already stored. A reading that is all stale says nothing new about the
// account.
func mergeWindowsReport(old, fresh []LimitWindow) ([]LimitWindow, bool) {
	out := append([]LimitWindow(nil), old...)
	accepted := false
	for _, w := range fresh {
		replaced := false
		for i := range out {
			if out[i].Label == w.Label {
				stale := w.ResetsAt != 0 && out[i].ResetsAt == w.ResetsAt && w.UsedPercent < out[i].UsedPercent
				if !stale {
					out[i] = w
					accepted = true
				}
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, w)
			accepted = true
		}
	}
	return out, accepted
}

// isWeeklyLabel reports whether a window is one of the weekly ones ("Weekly",
// "Weekly · Opus", "7-day").
func isWeeklyLabel(label string) bool {
	l := strings.ToLower(label)
	return strings.Contains(l, "week") || strings.HasPrefix(l, "7-day")
}
