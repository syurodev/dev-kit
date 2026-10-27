package agenthub

import (
	"fmt"
	"math"
	"sort"
	"strings"
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
)

// PickInput is everything PickAccount decides from. Accounts are all of one
// vendor's.
type PickInput struct {
	Policy   preferences.AccountVendorPolicy
	Accounts []AccountRecord
	// Current is the account the session is bound to; "" means the vendor's
	// built-in account.
	Current string
	Now     time.Time
	// SwitchedAt is when this session last switched; zero = never.
	SwitchedAt time.Time
	// Previous is the account the session left at that switch. It must be an
	// account id: the built-in's is BuiltinAccountID(vendor), never "". "" means
	// none (never switched).
	Previous string
	// Model is the model the session runs ("claude-fable-5-1", an alias, any
	// spelling that names it); "" when it is not known. A model's own window
	// ("Weekly · Fable") counts only for a session on that model.
	Model string
}

// PickDecision says which account the next turn runs as.
type PickDecision struct {
	// Target is the account id the next turn runs as (the built-in is
	// BuiltinAccountID(vendor), not ""), EXCEPT on the early "stay" returns
	// (disabled policy, fewer than two accounts, Current not in the list), where
	// it is Current exactly as given. Callers decide on Switch and AllLimited,
	// never on Target != Current.
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
//
// An account that is signed out is not usable, current or not. As a candidate it
// is skipped. As the CURRENT account it makes the session move, at once (no flip
// hold: a dead sign-in is not a number that may drop again), to the best
// signed-in account that is fine and not cooling down, with the reason
// "signed out". With no such account the session stays: a sign-out alone is
// never AllLimited (no account reached a limit), and nothing is moved to an
// account that is no better, so a session never thrashes between dead logins.
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
	ok, why := accountFine(*cur, in.Policy, in.Now, 0, in.Model)
	if !cur.SignedIn {
		ok, why = false, "signed out"
	}
	if ok {
		return stay
	}
	refused := cur.ExhaustedUntil.After(in.Now) || !cur.SignedIn
	if !refused && !in.SwitchedAt.IsZero() && in.Now.Sub(in.SwitchedAt) < accountFlipHold {
		return stay
	}
	var skipped []string
	var fine []AccountRecord
	for _, a := range ordered {
		if a.ID == cur.ID {
			continue
		}
		if !a.SignedIn {
			name := a.Label
			if name == "" {
				name = a.ID
			}
			skipped = append(skipped, name)
			continue
		}
		// A cooldown keeps a candidate out, not the account a session already
		// runs on: another session's failed reopen says nothing about this one.
		if a.CooldownUntil.After(in.Now) {
			continue
		}
		margin := 0.0
		if a.ID == in.Previous {
			margin = accountReturnGap
		}
		if ok, _ := accountFine(a, in.Policy, in.Now, margin, in.Model); ok {
			fine = append(fine, a)
		}
	}
	if len(fine) > 0 {
		reason := why
		if len(skipped) > 0 {
			reason += "; skipped " + strings.Join(skipped, ", ") + " (signed out)"
		}
		return PickDecision{Target: bestTarget(fine, in).ID, Switch: true, Reason: reason}
	}
	if !cur.SignedIn {
		// Nothing better to move to. No account reached a limit, so this is
		// not AllLimited: the turn runs where it is and the vendor answers.
		stay.Reason = why
		return stay
	}
	if retry, all := earliestReturn(ordered, in.Now); all {
		return PickDecision{Target: cur.ID, AllLimited: true, RetryAt: retry, Reason: why}
	}
	// Exhausted, with the only alternative cooling down: stay, and do not say
	// AllLimited. The turn runs on the refused account and the person sees the
	// vendor's own refusal, because a just-failed reopen is the strongest
	// evidence about the target.
	stay.Reason = why
	return stay
}

// accountFine reports whether an account may run a turn: not marked exhausted
// and every window not yet reset under its threshold (lowered by margin). A
// window that belongs to one model counts only for a session on that model
// (windowAppliesToModel). A
// cooldown is not read here: PickAccount applies it to candidates only. A
// weekly window (isWeeklyLabel) uses the policy's weekly threshold in place of
// the main one when that is set. A non-positive main threshold falls back to the
// default. A window with ResetsAt == 0 has no known reset and stays live. margin
// lowers the limit: a window at limit-margin or above refuses the account.
func accountFine(a AccountRecord, p preferences.AccountVendorPolicy, now time.Time, margin float64, model string) (bool, string) {
	if a.ExhaustedUntil.After(now) {
		return false, "usage limit reached"
	}
	threshold := float64(p.ThresholdPercent)
	if own := p.AccountThresholds[a.ID]; own > 0 {
		threshold = float64(own)
	}
	if threshold <= 0 {
		threshold = preferences.DefaultAccountThresholdPercent
	}
	for _, w := range a.Usage {
		if w.ResetsAt != 0 && w.ResetsAt <= now.Unix() {
			continue
		}
		if !windowAppliesToModel(w.Label, model) {
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

// orderedAccounts returns a copy of accounts sorted by order: accounts missing
// from order come after the ordered ones, in input order.
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
// and, if so, when the first comes back. An account that is only cooling down
// is not exhausted, so it makes this report false: nothing has reached a limit.
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

// bestTarget picks, among accounts that are fine to move to (already in the
// person's order), the one the policy's strategy prefers. A tie, or nothing to
// compare, keeps the order: sort.SliceStable never reorders equal elements.
func bestTarget(fine []AccountRecord, in PickInput) AccountRecord {
	rank := fine
	switch in.Policy.Strategy {
	case preferences.StrategyBest:
		rank = rankedBy(fine, func(a AccountRecord) (float64, bool) { return fullestWindow(a, in.Now, in.Model) })
	case preferences.StrategyConsumeFirst:
		rank = rankedBy(fine, func(a AccountRecord) (float64, bool) { return soonestWeeklyReset(a, in.Now, in.Model) })
	}
	return rank[0]
}

// rankedBy orders accounts by a key, lowest first, with the accounts that have
// no key after those that do.
func rankedBy(accounts []AccountRecord, key func(AccountRecord) (float64, bool)) []AccountRecord {
	out := append([]AccountRecord(nil), accounts...)
	sort.SliceStable(out, func(i, j int) bool {
		ki, iok := key(out[i])
		kj, jok := key(out[j])
		switch {
		case iok && jok:
			return ki < kj
		case iok != jok:
			return iok
		}
		return false
	})
	return out
}

// liveWindows are the readings that still say something now: not reset yet and
// applicable to the model (windowAppliesToModel).
func liveWindows(a AccountRecord, now time.Time, model string) []LimitWindow {
	var out []LimitWindow
	for _, w := range a.Usage {
		if (w.ResetsAt != 0 && w.ResetsAt <= now.Unix()) || !windowAppliesToModel(w.Label, model) {
			continue
		}
		out = append(out, w)
	}
	return out
}

// fullestWindow is the highest percent among the live windows; false without one.
func fullestWindow(a AccountRecord, now time.Time, model string) (float64, bool) {
	windows := liveWindows(a, now, model)
	if len(windows) == 0 {
		return 0, false
	}
	top := windows[0].UsedPercent
	for _, w := range windows[1:] {
		top = math.Max(top, w.UsedPercent)
	}
	return top, true
}

// soonestWeeklyReset is the earliest known reset (Unix seconds) among the live
// weekly windows; false when none has a known reset.
func soonestWeeklyReset(a AccountRecord, now time.Time, model string) (float64, bool) {
	var soonest int64
	for _, w := range liveWindows(a, now, model) {
		if w.ResetsAt > 0 && isWeeklyLabel(w.Label) && (soonest == 0 || w.ResetsAt < soonest) {
			soonest = w.ResetsAt
		}
	}
	return float64(soonest), soonest > 0
}
