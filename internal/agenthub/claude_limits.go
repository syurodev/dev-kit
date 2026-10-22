package agenthub

import (
	"encoding/json"
	"math"
	"sort"
)

// unifiedWindowOrder is the order Claude's windows are listed in; any other key
// follows, alphabetically.
var unifiedWindowOrder = []string{"five_hour", "seven_day", "seven_day_opus", "seven_day_sonnet"}

// unifiedLimitWindows reads the windows a Claude rate_limit_event carries in
// rate_limit_info.unifiedWindows. Measured against the real CLI: utilization
// there is a 0–1 fraction (0.69 while /usage said 69%), so it is shown as a
// percentage rounded to 0.01. The key is not in the SDK's declared type, so an
// absent or unreadable one is no windows, never an error.
func unifiedLimitWindows(meta json.RawMessage) []LimitWindow {
	if len(meta) == 0 {
		return nil
	}
	var parsed struct {
		RateLimit *struct {
			Unified map[string]struct {
				Utilization float64 `json:"utilization"`
				ResetsAt    int64   `json:"resetsAt"`
			} `json:"unifiedWindows"`
		} `json:"_claude/rateLimit"`
	}
	if json.Unmarshal(meta, &parsed) != nil || parsed.RateLimit == nil || len(parsed.RateLimit.Unified) == 0 {
		return nil
	}
	unified := parsed.RateLimit.Unified
	keys := make([]string, 0, len(unified))
	listed := make(map[string]bool, len(unifiedWindowOrder))
	for _, k := range unifiedWindowOrder {
		listed[k] = true
		if _, ok := unified[k]; ok {
			keys = append(keys, k)
		}
	}
	var rest []string
	for k := range unified {
		if !listed[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	keys = append(keys, rest...)

	out := make([]LimitWindow, 0, len(keys))
	for _, k := range keys {
		w := unified[k]
		pct := math.Round(w.Utilization*10000) / 100
		status := "ok"
		switch {
		case pct >= 100:
			status = "reached"
		case pct >= 80:
			status = "warning"
		}
		out = append(out, LimitWindow{Label: claudeLimitLabel(k), UsedPercent: pct, ResetsAt: w.ResetsAt, Status: status})
	}
	return out
}
