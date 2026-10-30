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
// Two exceptions. A window next names without a reading (NoReading: a
// rate-limit block with no utilization) keeps the percent prev had for the
// label and takes only the status and reset time. And a window with no reset
// time is carried only while next says nothing about windows: nothing says
// when it stops being true, so one the vendor stops sending, or relabels,
// would otherwise stay for good.
//
// A rate-limit verdict follows the same rule, except a rejected one: that is an
// event on the turn it stopped, not a standing state, and carrying it would
// keep a stopped-session banner up after the plan has let turns through again.
func mergeUsage(prev *Usage, next Usage, now time.Time) Usage {
	if prev == nil {
		return next
	}
	at := now.Unix()
	brings := len(next.Limits) > 0
	// live: its reset is ahead, or it names none and nothing newer replaces it.
	live := func(resetsAt int64) bool { return resetsAt > at || (resetsAt == 0 && !brings) }

	var carried []LimitWindow
	for _, w := range prev.Limits {
		if live(w.ResetsAt) {
			carried = append(carried, w)
		}
	}
	if brings {
		merged := make([]LimitWindow, 0, len(carried)+len(next.Limits))
		for _, w := range carried {
			if i := slices.IndexFunc(next.Limits, func(n LimitWindow) bool { return n.Label == w.Label }); i >= 0 {
				r := next.Limits[i]
				if r.NoReading {
					r.UsedPercent, r.NoReading = w.UsedPercent, w.NoReading
				}
				w = r
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

// mergeSubagentUsage is prev after a usage_update tagged with a subagent's
// origin. Its context counter, cost and tokens describe the subagent's own
// context window and are not taken; its windows do count, because a subagent
// spends the same plan. false when it carries none, so there is nothing to
// write. With no usage before it, the result holds the windows alone.
func mergeSubagentUsage(prev *Usage, next Usage, now time.Time) (Usage, bool) {
	if len(next.Limits) == 0 && next.RateLimit == nil {
		return Usage{}, false
	}
	windows := Usage{Limits: next.Limits, RateLimit: next.RateLimit}
	if prev != nil {
		windows.Used, windows.Size, windows.Amount, windows.Currency, windows.Tokens = prev.Used, prev.Size, prev.Amount, prev.Currency, prev.Tokens
	}
	return mergeUsage(prev, windows, now), true
}
