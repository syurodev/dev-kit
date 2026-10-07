package codegraph

import (
	"context"
	"regexp"
	"sort"
	"strings"
)

// ConfigUsageQuery asks where one setting matters. Key may be an environment
// variable (FSAP_ACCESS_MODULE_BASED_ENABLED), a property
// (fsap.access.module-based-enabled) or a prefix of properties; empty lists the
// switches that name no key.
type ConfigUsageQuery struct {
	Key string
	// Match is "exact" (default) or "prefix": every key beneath Key.
	Match string
	// Limit caps the affected entry points returned; 0 means DefaultConfigEntryLimit.
	Limit int
}

const (
	// DefaultConfigEntryLimit and MaxConfigEntryLimit bound the entry points one answer lists.
	DefaultConfigEntryLimit = 50
	MaxConfigEntryLimit     = 200
	// maxConfigUses bounds the uses one answer expands (readers, gated methods, guards for each).
	maxConfigUses = 50
	// maxConfigSeeds and maxConfigReach bound the walk from what a setting touches up to the entry points.
	maxConfigSeeds   = 500
	maxConfigReach   = 3000
	configReachDepth = 16
)

// ConfigUsageReport is everything the index says about one setting: where the
// files set it, where the code reads or switches on it, what that code
// touches, and which entry points reach it.
type ConfigUsageReport struct {
	Query string `json:"query"`
	// Canonical is the spelling matched on (Spring's relaxed binding: case and separators ignored).
	Canonical string `json:"canonical"`
	// Keys are the settings in config files, one per file and profile.
	Keys []ConfigKey `json:"keys"`
	// EnvVars are the environment variables seen feeding the setting: written in a
	// file as ${NAME:default}, or read with System.getenv.
	EnvVars []string `json:"envVars,omitempty"`
	// Usages are the code's uses, each with what it touches.
	Usages     []ConfigUsageDetail `json:"usages"`
	UsageTotal int                 `json:"usageTotal"`
	// EntryPoints are the entry points whose flow reaches anything a use touches.
	EntryPoints     []ConfigEntryPoint `json:"entryPoints"`
	EntryPointTotal int                `json:"entryPointTotal"`
	// Notes say what this answer cannot see.
	Notes []string `json:"notes,omitempty"`
}

// ConfigUsageDetail is one use and what it touches.
type ConfigUsageDetail struct {
	ConfigUsage
	// Label is the class or method the use is in.
	Label string `json:"label"`
	// Readers are the methods that read or write the field the value lands in.
	Readers []ConfigTouch `json:"readers,omitempty"`
	// Gated are the methods that exist only when a condition on the setting holds.
	Gated []ConfigTouch `json:"gated,omitempty"`
	// Guarded are the calls written under a condition that names the field.
	Guarded []ConfigGuardedCall `json:"guarded,omitempty"`
}

// ConfigTouch is a method a setting reaches.
type ConfigTouch struct {
	NodeID   string `json:"nodeId"`
	Label    string `json:"label"`
	FilePath string `json:"filePath"`
	Line     int    `json:"line"`
	// Access is "read" or "write" for a reader; empty for a gated method.
	Access string `json:"access,omitempty"`
}

// ConfigGuardedCall is a call whose guard (Edge.Guard) names the setting's field.
type ConfigGuardedCall struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Guard string `json:"guard"`
}

// ConfigEntryPoint is an entry point that reaches a setting, and through what.
type ConfigEntryPoint struct {
	ID      string `json:"id"`
	Verb    string `json:"verb"`
	Target  string `json:"target"`
	Kind    string `json:"kind"`
	Service string `json:"service,omitempty"`
	// Via is the first method on the way that the setting touches.
	Via string `json:"via"`
	// Depth is how many calls from the entry point to Via.
	Depth int `json:"depth"`
}

// FindConfigUsage answers "if I change this setting, what changes": the files
// that set it, the code that reads it and what that code reaches, and the
// entry points whose behaviour can differ.
func (s *Service) FindConfigUsage(ctx context.Context, q ConfigUsageQuery) (ConfigUsageReport, error) {
	store, release, err := s.activeStore()
	if err != nil {
		return ConfigUsageReport{}, err
	}
	defer release()
	limit, err := clampLimit(q.Limit, DefaultConfigEntryLimit, MaxConfigEntryLimit)
	if err != nil {
		return ConfigUsageReport{}, err
	}
	prefix := false
	switch q.Match {
	case "", MatchExact:
	case MatchPrefix:
		prefix = true
	default:
		return ConfigUsageReport{}, errBadConfigMatch(q.Match)
	}

	report := ConfigUsageReport{Query: q.Key, Keys: []ConfigKey{}, Usages: []ConfigUsageDetail{}, EntryPoints: []ConfigEntryPoint{}}

	// No key: the switches that name none — profiles, bean conditions.
	if strings.TrimSpace(q.Key) == "" {
		uses, err := store.FindConfigUsages(ctx, nil, false, []string{ConfigUseProfile, ConfigUseBeanCondition})
		if err != nil {
			return ConfigUsageReport{}, err
		}
		report.UsageTotal = len(uses)
		details, err := s.describeUses(ctx, store, uses[:min(len(uses), maxConfigUses)], false)
		if err != nil {
			return ConfigUsageReport{}, err
		}
		report.Usages = details
		return report, nil
	}

	canon := CanonicalConfigKey(q.Key)
	report.Canonical = canon
	if canon == "" {
		return ConfigUsageReport{}, errBadConfigMatch("key has no letters or digits")
	}
	keys, err := store.FindConfigKeys(ctx, canon, prefix)
	if err != nil {
		return ConfigUsageReport{}, err
	}
	report.Keys = append(report.Keys, keys...)

	// The keys an environment variable feeds are the key's own spelling too:
	// a use written against the property is found from the variable's name.
	canons := map[string]bool{canon: true}
	envs := map[string]bool{}
	for _, k := range keys {
		canons[CanonicalConfigKey(k.Key)] = true
		for _, e := range k.EnvVars {
			envs[e] = true
		}
	}
	list := make([]string, 0, len(canons))
	for c := range canons {
		list = append(list, c)
	}
	sort.Strings(list)
	uses, err := store.FindConfigUsages(ctx, list, prefix, nil)
	if err != nil {
		return ConfigUsageReport{}, err
	}
	for _, u := range uses {
		if u.Kind == ConfigUseEnvVar {
			envs[u.Key] = true
		}
	}
	for e := range envs {
		report.EnvVars = append(report.EnvVars, e)
	}
	sort.Strings(report.EnvVars)

	report.UsageTotal = len(uses)
	shown := uses[:min(len(uses), maxConfigUses)]
	details, err := s.describeUses(ctx, store, shown, true)
	if err != nil {
		return ConfigUsageReport{}, err
	}
	report.Usages = details
	if report.UsageTotal > len(shown) {
		report.Notes = append(report.Notes, "only the first uses are expanded; narrow the key to see the rest")
	}

	entries, total, err := s.entryPointsReaching(ctx, store, details, limit)
	if err != nil {
		return ConfigUsageReport{}, err
	}
	report.EntryPoints, report.EntryPointTotal = entries, total

	if len(keys) == 0 && len(uses) == 0 {
		report.Notes = append(report.Notes, "no config file sets this key and no code names it")
	}
	for _, d := range details {
		if d.Kind == ConfigUseProperties && len(d.Readers) == 0 {
			report.Notes = append(report.Notes, "a @ConfigurationProperties field is usually read through its getter or record accessor; a call to a Lombok-generated or implicit accessor cannot be resolved, so its readers may be missing")
			break
		}
	}
	return report, nil
}

func errBadConfigMatch(what string) error {
	return &configQueryError{what}
}

type configQueryError struct{ what string }

func (e *configQueryError) Error() string { return "codegraph: findConfigUsage: " + e.what }

// describeUses adds to each use what it touches. expand false skips the
// field readers and guarded calls (a list of switches has no field).
func (s *Service) describeUses(ctx context.Context, store Store, uses []ConfigUsage, expand bool) ([]ConfigUsageDetail, error) {
	details := make([]ConfigUsageDetail, 0, len(uses))
	for _, u := range uses {
		d := ConfigUsageDetail{ConfigUsage: u}
		if node, err := store.GetNode(ctx, u.From); err == nil {
			d.Label = node.Label
		} else if err != ErrNodeNotFound {
			return nil, err
		}
		if expand {
			if err := s.touch(ctx, store, &d); err != nil {
				return nil, err
			}
		}
		details = append(details, d)
	}
	return details, nil
}

// touch fills what the use reaches: readers of its field, the methods a
// condition gates, and the calls guarded by a condition on the field.
func (s *Service) touch(ctx context.Context, store Store, d *ConfigUsageDetail) error {
	switch d.Kind {
	case ConfigUseCondition, ConfigUseProfile, ConfigUseBeanCondition:
		// The class (every method of it) or the one method exists only when the condition holds.
		node, err := store.GetNode(ctx, d.From)
		if err == ErrNodeNotFound {
			return nil
		}
		if err != nil {
			return err
		}
		if node.Kind == "method" {
			d.Gated = []ConfigTouch{touchOf(node, "")}
			return nil
		}
		methods, err := store.ListClassMethods(ctx, d.From)
		if err != nil {
			return err
		}
		for _, m := range methods[:min(len(methods), maxConfigUses)] {
			d.Gated = append(d.Gated, touchOf(m, ""))
		}
		return nil
	}
	if d.Field == "" {
		return nil
	}

	accesses, err := store.GetFieldAccesses(ctx, d.From, d.Field)
	if err != nil {
		return err
	}
	for _, a := range accesses {
		if len(d.Readers) >= maxConfigUses {
			break
		}
		if node, err := store.GetNode(ctx, a.NodeID); err == nil {
			d.Readers = append(d.Readers, touchOf(node, a.Kind))
		} else if err != ErrNodeNotFound {
			return err
		}
	}

	// A @ConfigurationProperties field is read through an accessor, so the
	// methods that are its accessors count as readers; whoever calls them is
	// found by the walk to the entry points.
	var patterns []*regexp.Regexp
	var terms []string
	fromPrefix := ""
	if d.Kind == ConfigUseProperties {
		upper := strings.ToUpper(d.Field[:1]) + d.Field[1:]
		terms = []string{"get" + upper, "is" + upper, d.Field + "("}
		patterns = []*regexp.Regexp{
			regexp.MustCompile(`(?:^|[^\w$])(?:get|is)` + regexp.QuoteMeta(upper) + `\(`),
			regexp.MustCompile(`\.` + regexp.QuoteMeta(d.Field) + `\(`),
		}
		methods, err := store.ListClassMethods(ctx, d.From)
		if err != nil {
			return err
		}
		for _, m := range methods {
			name := m.Label[strings.LastIndex(m.Label, ".")+1:]
			if name == "get"+upper || name == "is"+upper || name == d.Field {
				d.Readers = append(d.Readers, touchOf(m, "read"))
			}
		}
	} else {
		terms = []string{d.Field}
		patterns = []*regexp.Regexp{regexp.MustCompile(`(?:^|[^\w$])` + regexp.QuoteMeta(d.Field) + `(?:[^\w$]|$)`)}
		fromPrefix = d.From + "."
	}
	edges, err := store.FindEdgesByGuard(ctx, terms, fromPrefix)
	if err != nil {
		return err
	}
	for _, e := range edges {
		if len(d.Guarded) >= maxConfigUses {
			break
		}
		for _, p := range patterns {
			if p.MatchString(e.Guard) {
				d.Guarded = append(d.Guarded, ConfigGuardedCall{From: e.From, To: e.To, Guard: e.Guard})
				break
			}
		}
	}
	return nil
}

func touchOf(n Node, access string) ConfigTouch {
	return ConfigTouch{NodeID: n.ID, Label: n.Label, FilePath: n.FilePath, Line: n.Line, Access: access}
}

// entryPointsReaching walks from every method the uses touch up through its
// callers to the entry points, nearest first. The walk is bounded: it answers
// "which endpoints can this change", not "everything that ever calls this".
func (s *Service) entryPointsReaching(ctx context.Context, store Store, details []ConfigUsageDetail, limit int) ([]ConfigEntryPoint, int, error) {
	type seed struct{ id, label string }
	var seeds []seed
	seen := map[string]bool{}
	add := func(id, label string) {
		if id != "" && !seen[id] && len(seeds) < maxConfigSeeds {
			seen[id] = true
			seeds = append(seeds, seed{id, label})
		}
	}
	for _, d := range details {
		for _, r := range d.Readers {
			add(r.NodeID, r.Label)
		}
		for _, g := range d.Gated {
			add(g.NodeID, g.Label)
		}
		for _, g := range d.Guarded {
			add(g.From, g.From)
		}
	}
	if len(seeds) == 0 {
		return []ConfigEntryPoint{}, 0, nil
	}

	entries, err := store.ListEntryPoints(ctx)
	if err != nil {
		return nil, 0, err
	}
	entries = completeEntryPoints(entries)
	sortEntryPoints(entries)
	byNode := map[string][]EntryPoint{}
	for _, e := range entries {
		byNode[e.EntryNode] = append(byNode[e.EntryNode], e)
	}

	// Breadth-first from all seeds at once, so the first time an entry node
	// is reached is by its nearest seed.
	type step struct {
		id    string
		via   string
		depth int
	}
	visited := map[string]bool{}
	queue := make([]step, 0, len(seeds))
	for _, sd := range seeds {
		visited[sd.id] = true
		queue = append(queue, step{sd.id, sd.label, 0})
	}
	found := map[string]ConfigEntryPoint{}
	for head := 0; head < len(queue) && len(visited) < maxConfigReach; head++ {
		cur := queue[head]
		for _, ep := range byNode[cur.id] {
			if _, ok := found[ep.ID]; !ok {
				found[ep.ID] = ConfigEntryPoint{ID: ep.ID, Verb: ep.Verb, Target: ep.Target, Kind: ep.Kind, Service: ep.Service, Via: cur.via, Depth: cur.depth}
			}
		}
		if cur.depth >= configReachDepth {
			continue
		}
		callers, err := store.GetInEdges(ctx, cur.id)
		if err != nil {
			return nil, 0, err
		}
		for _, e := range callers {
			if e.Kind != "call" || visited[e.From] {
				continue
			}
			visited[e.From] = true
			queue = append(queue, step{e.From, cur.via, cur.depth + 1})
		}
	}

	out := make([]ConfigEntryPoint, 0, len(found))
	for _, e := range found {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Depth != out[j].Depth {
			return out[i].Depth < out[j].Depth
		}
		if out[i].Target != out[j].Target {
			return out[i].Target < out[j].Target
		}
		return out[i].Verb < out[j].Verb
	})
	total := len(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, total, nil
}
