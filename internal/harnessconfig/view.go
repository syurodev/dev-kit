package harnessconfig

import (
	"fmt"
	"strings"

	"devkit/internal/harness"
)

// The confirmation must show the whole effective diff or the proposal is not
// offered: a person cannot approve changes they were not shown.
const (
	maxViewChanges  = 80
	maxSummaryBytes = 8 * 1024
)

// buildView writes the confirmation content from the effective diff. Strings
// that come from configuration (tool, group and server names, paths) appear
// only as keys of a fixed sentence; nothing from the proposer's rationale does.
// Every change is listed once (a workspace change judged for several vendors
// repeats its sentence only once). A diff too large to list in full is refused
// with ErrTooLargeToShow rather than shortened.
func buildView(ev evaluation, policy Policy) (View, error) {
	v := View{Target: ev.Target, Tier: ev.Tier.String(), NeedsConfirmation: ev.needsApproval, RationaleUntrusted: ev.Rationale}
	seen := map[string]bool{}
	var lines []string
	for _, vd := range ev.Vendors {
		for _, c := range vd.Changes {
			e := effect(c)
			v.Changes = append(v.Changes, ViewChange{Vendor: vd.Vendor, Field: c.Field, Key: c.Key, Effect: e})
			if !seen[e] {
				seen[e] = true
				lines = append(lines, e)
			}
		}
	}
	if len(lines) > maxViewChanges {
		return View{}, ErrTooLargeToShow
	}
	var b strings.Builder
	switch ev.Target {
	case TargetSession:
		b.WriteString("Change the tools served to this session only.")
	default:
		b.WriteString("Change this workspace's harness profile. Running sessions keep what they opened with until reloaded.")
	}
	fmt.Fprintf(&b, " Effect: %s.", v.Tier)
	for _, l := range lines {
		fmt.Fprintf(&b, "\n- %s", l)
	}
	if b.Len() > maxSummaryBytes {
		return View{}, ErrTooLargeToShow
	}
	v.Summary = b.String()
	return v, nil
}

func effect(c harness.Change) string {
	switch c.Field {
	case "toolGroups":
		if c.Tier == harness.TierWidening {
			return "turn on tool group " + c.Key
		}
		return "turn off tool group " + c.Key
	case "resultBudget.bytes":
		if c.Tier == harness.TierWidening {
			return "allow longer results from " + c.Key
		}
		return "shorten results from " + c.Key
	case "resultBudget.cut":
		return "change how results from " + c.Key + " are cut"
	case "disabledMCP":
		if c.Tier == harness.TierWidening {
			return "stop denying MCP server " + c.Key
		}
		return "deny MCP server " + c.Key
	case "disabledSkills":
		if c.Tier == harness.TierWidening {
			return "stop denying skill " + c.Key
		}
		return "deny skill " + c.Key
	case "executable":
		if c.Tier == harness.TierExecutable {
			return "add or change " + c.Key
		}
		return "remove " + c.Key
	}
	return "change " + c.Field
}
