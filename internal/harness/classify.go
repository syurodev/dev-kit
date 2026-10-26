package harness

import "sort"

// Tier orders how much a change needs the person's say. A change with several
// effects takes the strictest tier.
type Tier int

const (
	TierNone       Tier = iota // nothing differs
	TierNarrowing              // only decreases, or a neutral cut-mode change
	TierWidening               // more capability
	TierExecutable             // a server or skill that could run or load is new or altered
)

func (t Tier) String() string {
	switch t {
	case TierNarrowing:
		return "narrowing"
	case TierWidening:
		return "widening"
	case TierExecutable:
		return "executable"
	}
	return "none"
}

// Change is one effective difference. Key is a tool, group, server name, path
// or deny entry: never a value, argument or environment value.
type Change struct {
	Field   string
	Key     string
	Tier    Tier
	Neutral bool // a cut-mode-only change; takes the narrowing tier
}

// Classification is computed on two effective configurations, never on a
// patch: deleting a deny entry or an override can widen access or expose a
// fallback executable although the patch only removes a line.
type Classification struct {
	Tier    Tier
	Changes []Change
}

// Classify compares what a session would be able to do before and after. The
// two values should come from the same registry; a tool present on one side
// only counts as unserved on the other.
func Classify(before, after Effective) Classification {
	var changes []Change
	add := func(field, key string, tier Tier, neutral bool) {
		changes = append(changes, Change{Field: field, Key: key, Tier: tier, Neutral: neutral})
	}

	for _, name := range union(groupNames(before), groupNames(after)) {
		switch b, a := before.groupOn[name], after.groupOn[name]; {
		case !b && a:
			add("toolGroups", name, TierWidening, false)
		case b && !a:
			add("toolGroups", name, TierNarrowing, false)
		}
	}

	for _, name := range union(budgetNames(before), budgetNames(after)) {
		b, a := before.budgets[name], after.budgets[name]
		if b.Exempt || a.Exempt {
			continue
		}
		switch {
		case a.Bytes > b.Bytes:
			add("resultBudget.bytes", name, TierWidening, false)
		case a.Bytes < b.Bytes:
			add("resultBudget.bytes", name, TierNarrowing, false)
		case a.Cut != b.Cut:
			add("resultBudget.cut", name, TierNarrowing, true)
		}
	}

	diffSet := func(field string, b, a []string) {
		for _, k := range union(b, a) {
			inB, inA := contains(b, k), contains(a, k)
			switch {
			case inB && !inA:
				add(field, k, TierWidening, false)
			case !inB && inA:
				add(field, k, TierNarrowing, false)
			}
		}
	}
	diffSet("disabledMCP", before.DisabledMCP, after.DisabledMCP)
	diffSet("disabledSkills", before.DisabledSkills, after.DisabledSkills)

	for _, key := range union(mapKeys(before.executable), mapKeys(after.executable)) {
		b, inB := before.executable[key]
		a, inA := after.executable[key]
		switch {
		case inA && (!inB || a != b):
			add("executable", key, TierExecutable, false)
		case inB && !inA:
			add("executable", key, TierNarrowing, false)
		}
	}

	out := Classification{Changes: changes}
	for _, c := range changes {
		tier := c.Tier
		if tier > out.Tier {
			out.Tier = tier
		}
	}
	sort.SliceStable(out.Changes, func(i, j int) bool {
		x, y := out.Changes[i], out.Changes[j]
		if x.Tier != y.Tier {
			return x.Tier > y.Tier
		}
		if x.Field != y.Field {
			return x.Field < y.Field
		}
		return x.Key < y.Key
	})
	return out
}

func groupNames(e Effective) []string  { return mapKeys(e.groupOn) }
func budgetNames(e Effective) []string { return mapKeys(e.budgets) }

func mapKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
