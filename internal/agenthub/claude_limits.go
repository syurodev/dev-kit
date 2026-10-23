package agenthub

import (
	"encoding/json"
	"log"
	"math"
	"slices"
	"sync"
)

// unifiedWindowOrder is the order Claude's windows are listed in; any other key
// follows, alphabetically.
var unifiedWindowOrder = []string{"five_hour", "seven_day", "seven_day_opus", "seven_day_sonnet"}

// unifiedFractionMax is the largest utilization read as a 0–1 fraction. Claude
// reports overage above 1 (1.2 is 120%), so the cut-off sits above it: a value
// beyond it can only be a percentage already.
const unifiedFractionMax = 1.5

var unifiedPercentOnce sync.Once

// roundPercent rounds to 0.01, which is as fine as a fraction with two
// decimals can be and keeps 0.69*100 from showing as 68.99999999999999.
func roundPercent(pct float64) float64 { return math.Round(pct*100) / 100 }

// unifiedPercent reads a window's utilization as a percentage. Measured against
// Claude Code 2.1.308 it is a 0–1 fraction (0.69 while /usage said 69%); if a
// release changes the scale to a percentage, the value is used as it is.
func unifiedPercent(utilization float64) float64 {
	utilization = math.Max(utilization, 0)
	if utilization > unifiedFractionMax {
		unifiedPercentOnce.Do(func() {
			log.Print("agenthub: claude unifiedWindows utilization looks like a percentage; reading it as one")
		})
		return roundPercent(math.Min(utilization, 100))
	}
	return roundPercent(utilization * 100)
}

// unifiedLimitWindows reads the windows a Claude rate_limit_event carries in
// rate_limit_info.unifiedWindows. The key is not in the SDK's declared type, so
// an absent or unreadable one is no windows, never an error, and a window with
// no utilization is skipped (it would read as 0% and overwrite a real reading).
func unifiedLimitWindows(meta json.RawMessage) []LimitWindow {
	if len(meta) == 0 {
		return nil
	}
	var parsed struct {
		RateLimit *struct {
			Unified map[string]json.RawMessage `json:"unifiedWindows"`
		} `json:"_claude/rateLimit"`
	}
	if json.Unmarshal(meta, &parsed) != nil || parsed.RateLimit == nil {
		return nil
	}
	type reading struct {
		key    string
		window LimitWindow
	}
	var found []reading
	for key, raw := range parsed.RateLimit.Unified {
		var w struct {
			Utilization *float64 `json:"utilization"`
			// A JSON number, possibly fractional: one odd value must not cost
			// the whole map.
			ResetsAt float64 `json:"resetsAt"`
		}
		if json.Unmarshal(raw, &w) != nil || w.Utilization == nil {
			continue
		}
		pct := unifiedPercent(*w.Utilization)
		found = append(found, reading{key, LimitWindow{
			Label: claudeLimitLabel(key), UsedPercent: pct, ResetsAt: int64(w.ResetsAt), Status: percentStatus(pct),
		}})
	}
	rank := func(key string) int {
		if i := slices.Index(unifiedWindowOrder, key); i >= 0 {
			return i
		}
		return len(unifiedWindowOrder)
	}
	slices.SortFunc(found, func(a, b reading) int {
		if d := rank(a.key) - rank(b.key); d != 0 {
			return d
		}
		switch {
		case a.key < b.key:
			return -1
		case a.key > b.key:
			return 1
		}
		return 0
	})
	out := make([]LimitWindow, len(found))
	for i, f := range found {
		out[i] = f.window
	}
	return out
}

// applyRateLimitStatus lets the event's own verdict win over the percentages:
// "rejected" means the window named by rateLimitType is reached even when its
// unified utilization reads below 100, and "allowed_warning" means it is at
// least a warning. A rejected window the map does not list is added.
func applyRateLimitStatus(windows []LimitWindow, r *RateLimit) []LimitWindow {
	// A plan that goes on through overage still answers: a window past 100 % is a
	// warning, not a refusal, unless the event itself says "rejected".
	if r != nil && r.IsUsingOverage && r.Status != "rejected" {
		for i := range windows {
			if windows[i].Status == "reached" {
				windows[i].Status = "warning"
			}
		}
	}
	if r == nil || (r.Status != "rejected" && r.Status != "allowed_warning") {
		return windows
	}
	label := claudeLimitLabel(r.LimitType)
	i := slices.IndexFunc(windows, func(w LimitWindow) bool { return w.Label == label })
	if r.Status == "rejected" {
		if i < 0 {
			return append(windows, LimitWindow{Label: label, UsedPercent: r.Utilization, ResetsAt: r.ResetsAt, Status: "reached"})
		}
		windows[i].Status = "reached"
	} else if i >= 0 && windows[i].Status == "ok" {
		windows[i].Status = "warning"
	}
	return windows
}
