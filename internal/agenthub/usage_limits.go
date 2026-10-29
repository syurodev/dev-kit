package agenthub

import (
	"slices"
	"time"
)

// mergeUsage is the session's usage after next arrives on top of prev.
//
// The context counter, cost and tokens are next's: they describe the latest
// request. The plan's windows are not: Claude puts them on some usage_updates
// only, and a window means "this much of the plan is used until it resets", so
// an update that does not repeat it has not withdrawn it. A window next names
// (by label) replaces prev's, one it does not stays, and one whose reset time
// has passed goes: its usage started over, so its percent is no longer true.
//
// A rate-limit verdict follows the same rule, except a rejected one: that is an
// event on the turn it stopped, not a standing state, and carrying it would
// keep a stopped-session banner up after the plan has let turns through again.
func mergeUsage(prev *Usage, next Usage, now time.Time) Usage {
	if prev == nil {
		return next
	}
	at := now.Unix()
	live := func(resetsAt int64) bool { return resetsAt == 0 || resetsAt > at }

	var carried []LimitWindow
	for _, w := range prev.Limits {
		if live(w.ResetsAt) {
			carried = append(carried, w)
		}
	}
	if len(next.Limits) > 0 {
		merged := make([]LimitWindow, 0, len(carried)+len(next.Limits))
		for _, w := range carried {
			if i := slices.IndexFunc(next.Limits, func(n LimitWindow) bool { return n.Label == w.Label }); i >= 0 {
				w = next.Limits[i]
			}
			merged = append(merged, w)
		}
		for _, n := range next.Limits {
			if !slices.ContainsFunc(carried, func(w LimitWindow) bool { return w.Label == n.Label }) {
				merged = append(merged, n)
			}
		}
		next.Limits = merged
	} else if len(carried) > 0 {
		next.Limits = carried
	}

	if next.RateLimit == nil && prev.RateLimit != nil && prev.RateLimit.Status != "rejected" && live(prev.RateLimit.ResetsAt) {
		kept := *prev.RateLimit
		next.RateLimit = &kept
	}
	return next
}
