package agenthub

import (
	"fmt"
	"sort"
	"time"

	"devkit/internal/preferences"
)

const (
	// accountFlipHold is how long a session stays on the account it just moved
	// to, whatever the numbers say (a real refusal still moves it).
	accountFlipHold = 5 * time.Minute
	// accountReturnGap is how far under the threshold an account must be before
	// a session goes back to the one it left.
	accountReturnGap = 10.0
	// accountProbeCooldown is how long a refusal that names no reset keeps an
	// account out, before the next turn may try it again.
	accountProbeCooldown = 15 * time.Minute
)

// PickInput is everything PickAccount decides from. Accounts are all of one
// vendor's; Current is the account the session is bound to ("" is the built-in).
type PickInput struct {
	Policy     preferences.AccountVendorPolicy
	Accounts   []AccountRecord
	Current    string
	Now        time.Time
	SwitchedAt time.Time // when this session last switched; zero = never
	Previous   string    // the account it left then
}

// PickDecision says which account the next turn runs as.
type PickDecision struct {
	Target string
	Switch bool
	// Reason is why the current account was left ("Weekly 92% ≥ 90%"), or why
	// nothing could be chosen.
	Reason string
	// AllLimited: every signed-in account is marked exhausted. RetryAt is when
	// the first comes back.
	AllLimited bool
	RetryAt    time.Time
}

// PickAccount is pure. A reading is used as it is: a window's usage only grows
// until it resets, so an old reading over the threshold is still true, and an old
// one under it proves nothing and leaves the account eligible.
func PickAccount(in PickInput) PickDecision {
	stay := PickDecision{Target: in.Current}
	if !in.Policy.Enabled || len(in.Accounts) < 2 {
		return stay
	}
	ordered := orderedAccounts(in.Policy.Order, in.Accounts)
	cur := findAccount(ordered, in.Current)
	if cur == nil {
		return stay
	}
	stay.Target = cur.ID
	ok, why := accountFine(*cur, in.Policy, in.Now, 0)
	if ok {
		return stay
	}
	refused := cur.ExhaustedUntil.After(in.Now)
	if !refused && !in.SwitchedAt.IsZero() && in.Now.Sub(in.SwitchedAt) < accountFlipHold {
		return stay
	}
	for _, a := range ordered {
		if a.ID == cur.ID || !a.SignedIn {
			continue
		}
		margin := 0.0
		if a.ID == in.Previous {
			margin = accountReturnGap
		}
		if fine, _ := accountFine(a, in.Policy, in.Now, margin); fine {
			return PickDecision{Target: a.ID, Switch: true, Reason: why}
		}
	}
	if retry, all := earliestReturn(ordered, in.Now); all {
		return PickDecision{Target: cur.ID, AllLimited: true, RetryAt: retry, Reason: why}
	}
	stay.Reason = why
	return stay
}

// accountFine reports whether an account may run a turn: not marked exhausted
// and every window not yet reset under its threshold (lowered by margin).
func accountFine(a AccountRecord, p preferences.AccountVendorPolicy, now time.Time, margin float64) (bool, string) {
	if a.ExhaustedUntil.After(now) {
		return false, "usage limit reached"
	}
	threshold := float64(p.ThresholdPercent)
	if threshold <= 0 {
		threshold = preferences.DefaultAccountThresholdPercent
	}
	for _, w := range a.Usage {
		if w.ResetsAt != 0 && w.ResetsAt <= now.Unix() {
			continue
		}
		limit := threshold
		if p.WeeklyThresholdPercent > 0 && isWeeklyLabel(w.Label) {
			limit = float64(p.WeeklyThresholdPercent)
		}
		if w.UsedPercent >= limit-margin {
			return false, fmt.Sprintf("%s %.0f%% ≥ %.0f%%", w.Label, w.UsedPercent, limit)
		}
	}
	return true, ""
}

func orderedAccounts(order []string, accounts []AccountRecord) []AccountRecord {
	rank := make(map[string]int, len(order))
	for i, id := range order {
		rank[id] = i
	}
	out := append([]AccountRecord(nil), accounts...)
	sort.SliceStable(out, func(i, j int) bool {
		ri, iok := rank[out[i].ID]
		rj, jok := rank[out[j].ID]
		switch {
		case iok && jok:
			return ri < rj
		case iok != jok:
			return iok
		}
		return false
	})
	return out
}

// findAccount resolves id; "" is the vendor's built-in account.
func findAccount(accounts []AccountRecord, id string) *AccountRecord {
	for i := range accounts {
		if (id == "" && accounts[i].Builtin()) || accounts[i].ID == id {
			return &accounts[i]
		}
	}
	return nil
}

// earliestReturn reports whether every signed-in account is marked exhausted
// and, if so, when the first comes back.
func earliestReturn(accounts []AccountRecord, now time.Time) (time.Time, bool) {
	var first time.Time
	signedIn := 0
	for _, a := range accounts {
		if !a.SignedIn {
			continue
		}
		signedIn++
		if !a.ExhaustedUntil.After(now) {
			return time.Time{}, false
		}
		if first.IsZero() || a.ExhaustedUntil.Before(first) {
			first = a.ExhaustedUntil
		}
	}
	return first, signedIn > 0
}
