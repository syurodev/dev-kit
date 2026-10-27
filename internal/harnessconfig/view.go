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
			v.Changes = append(v.Changes, ViewChange{Vendor: vd.Vendor, Field: c.Field, Key: c.Key, Effect: e, Scope: ScopeStored})
			if !seen[e] {
				seen[e] = true
				lines = append(lines, e)
			}
		}
	}
	// The running session is judged separately, under its pinned launch W, so the
	// two lists can disagree (saved W already allows what the session's W blocks)
	// and are never merged or deduplicated against each other.
	var liveLines []string
	if ev.Live != nil {
		liveSeen := map[string]bool{}
		for _, g := range ev.Live.Groups {
			l := liveLine(g)
			v.Changes = append(v.Changes, ViewChange{Field: "toolGroups", Key: g.Name, Effect: l, Scope: ScopeRunning})
			if !liveSeen[l] {
				liveSeen[l] = true
				liveLines = append(liveLines, l)
			}
		}
	}
	if len(lines)+len(liveLines) > maxViewChanges {
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
	// Without a running session the sentence is the stored-only form, as before.
	switch {
	case ev.Live == nil:
		for _, l := range lines {
			fmt.Fprintf(&b, "\n- %s", l)
		}
	case len(lines) > 0:
		b.WriteString("\nSaved configuration (what the next session opens with):")
		for _, l := range lines {
			fmt.Fprintf(&b, "\n- %s", l)
		}
	default:
		b.WriteString("\nSaved configuration: no difference in effect.")
	}
	if ev.Live != nil {
		fmt.Fprintf(&b, "\nThis running session (opened with workspace profile revision %s, vendor revision %d):", pinnedW(ev.Live.PinnedWorkspace), ev.Live.PinnedVendor)
		if len(liveLines) == 0 {
			b.WriteString("\n- no change to what it can use")
		}
		for _, l := range liveLines {
			fmt.Fprintf(&b, "\n- %s", l)
		}
		b.WriteString("\nTools turned on become usable only after the agent refreshes its tool list or the session is reloaded; this does not pre-approve them.")
	}
	if b.Len() > maxSummaryBytes {
		return View{}, ErrTooLargeToShow
	}
	v.Summary = b.String()
	return v, nil
}

// pinnedW states the workspace profile a session was opened with: its revision,
// or that there was none (absent, possibly after a deletion).
func pinnedW(s harness.Stamp) string {
	if !s.Present {
		return "none"
	}
	return fmt.Sprint(s.Revision)
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
