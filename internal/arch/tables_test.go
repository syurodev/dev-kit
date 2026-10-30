package arch

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Who may write SQL against which table. A table that holds policy, a ledger or a
// lock is read and written by the package that owns it and by the storage kernel
// that creates and migrates it - not by whichever package happens to hold a
// *sql.DB. Another package reaches the data through the owner's API, so the
// invariants that live next to the table (compare-and-swap, fences, triggers)
// cannot be skipped by a second writer.
//
// A table name matches as a whole word, or as a prefix when the key ends in "_".
// Only production code is scanned. To give a package access, add it here and say
// why in the change.
var tableOwners = map[string][]string{
	// W and S, and their applied reference: the harness store is the only writer,
	// and every change to them goes through harnessconfig's Prepare/Apply.
	"workspace_harness_profiles":    {"internal/storage"},
	"session_harness_overrides":     {"internal/storage"},
	"agent_session_harness_applied": {"internal/storage", "internal/agenthub"},
	// The child tool-group request ledger and the person's locks.
	"agent_group_requests":            {"internal/storage", "internal/agenthub"},
	"agent_group_request_open_groups": {"internal/storage", "internal/agenthub"},
	"agent_session_group_locks":       {"internal/storage", "internal/agenthub"},
	// Managed work (groups, items, attempts, packages, leases, bindings): the
	// ledger in storage is the only door.
	"agent_work_": {"internal/storage"},
	// Peer delivery state.
	"agent_peer_outbox":      {"internal/storage", "internal/agenthub"},
	"agent_peer_guard_state": {"internal/storage", "internal/agenthub"},
}

func TestProtectedTablesAreTouchedOnlyByTheirOwners(t *testing.T) {
	type rule struct {
		table   string
		pattern *regexp.Regexp
		allowed map[string]bool
	}
	var rules []rule
	for table, owners := range tableOwners {
		expr := `\b` + regexp.QuoteMeta(table) + `\b`
		if strings.HasSuffix(table, "_") {
			expr = `\b` + regexp.QuoteMeta(table)
		}
		allowed := map[string]bool{}
		for _, o := range owners {
			allowed[o] = true
		}
		rules = append(rules, rule{table, regexp.MustCompile(expr), allowed})
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].table < rules[j].table })
	for _, f := range prodFiles(t) {
		for _, r := range rules {
			if r.pattern.Match(f.Src) && !r.allowed[f.Dir] {
				t.Errorf("table ownership: %s names %q but only %s may; go through the owner's API", f.Rel, r.table, strings.Join(owners(r.allowed), ", "))
			}
		}
	}
}

// A table that no longer appears anywhere is a stale rule, not a safe one.
func TestEveryOwnedTableStillExists(t *testing.T) {
	files := prodFiles(t)
	for table := range tableOwners {
		expr := `\b` + regexp.QuoteMeta(table) + `\b`
		if strings.HasSuffix(table, "_") {
			expr = `\b` + regexp.QuoteMeta(table)
		}
		re := regexp.MustCompile(expr)
		found := false
		for _, f := range files {
			if f.Dir == "internal/storage" && re.Match(f.Src) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("table ownership: %q is in tableOwners but no migration in internal/storage creates it; remove or rename the rule", table)
		}
	}
}

func owners(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
