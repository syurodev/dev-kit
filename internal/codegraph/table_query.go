package codegraph

import (
	"context"
	"sort"
	"strings"
)

// TableUsageQuery asks who reads and writes a table. Table may be the table
// (organization_api_enablement), the JPQL entity name or the class
// (OrganizationApiEnablement) — case and separators are ignored, so all three
// find the same thing — or, with Match prefix, the start of any of them.
type TableUsageQuery struct {
	Table string
	// Match is "exact" (default) or "prefix".
	Match string
	// Access narrows to "read" or "write"; empty is both.
	Access string
	// Limit caps the entry points listed; 0 means DefaultConfigEntryLimit.
	Limit int
}

// TableUsageReport is what the index says about one table: which entities it
// belongs to, every place that reads or writes it, and the entry points whose
// flow reaches one of those places.
type TableUsageReport struct {
	Query     string `json:"query"`
	Canonical string `json:"canonical"`
	// Entities are the entities stored in the table, and the tables of those entities.
	Entities []TableEntity `json:"entities"`
	// Tables are the distinct tables the accesses are to (schema-qualified when the source said so).
	Tables []string `json:"tables"`
	// Accesses are the places that read or write it, each with who calls it.
	Accesses    []TableAccessDetail `json:"accesses"`
	AccessTotal int                 `json:"accessTotal"`
	Reads       int                 `json:"reads"`
	Writes      int                 `json:"writes"`
	// EntryPoints are the entry points that reach an access; Access says whether to read it, write it or both.
	EntryPoints     []ReachedEntryPoint `json:"entryPoints"`
	EntryPointTotal int                 `json:"entryPointTotal"`
	Notes           []string            `json:"notes,omitempty"`
}

// TableEntity is an EntityTable and the class it names.
type TableEntity struct {
	EntityTable
	Label string `json:"label"`
}

// TableAccessDetail is an access with the method that makes it and who calls that.
type TableAccessDetail struct {
	TableAccess
	Label string `json:"label"`
	// CalledBy are the nearest callers of From, for a repository method the services that use it.
	CalledBy []ConfigTouch `json:"calledBy,omitempty"`
}

const (
	maxTableAccessDetails = 100
	maxTableCallers       = 5
)

// FindTableUsage answers "who reads or writes this table" — the question that
// matters most before dropping or changing one — from the entities' mapping,
// the queries the code declares, and the call graph above them.
func (s *Service) FindTableUsage(ctx context.Context, q TableUsageQuery) (TableUsageReport, error) {
	store, release, err := s.activeStore()
	if err != nil {
		return TableUsageReport{}, err
	}
	defer release()
	limit, err := clampLimit(q.Limit, DefaultConfigEntryLimit, MaxConfigEntryLimit)
	if err != nil {
		return TableUsageReport{}, err
	}
	prefix := false
	switch q.Match {
	case "", MatchExact:
	case MatchPrefix:
		prefix = true
	default:
		return TableUsageReport{}, &configQueryError{"match must be exact or prefix (got " + q.Match + ")"}
	}
	switch q.Access {
	case "", "read", "write":
	default:
		return TableUsageReport{}, &configQueryError{"access must be read or write (got " + q.Access + ")"}
	}
	canon := CanonicalConfigKey(q.Table)
	if canon == "" {
		return TableUsageReport{}, &configQueryError{"table has no letters or digits"}
	}

	report := TableUsageReport{Query: q.Table, Canonical: canon, Entities: []TableEntity{}, Tables: []string{},
		Accesses: []TableAccessDetail{}, EntryPoints: []ReachedEntryPoint{}}

	entities, err := store.FindEntityTables(ctx, canon, prefix)
	if err != nil {
		return TableUsageReport{}, err
	}
	// The tables to look for. Asked by table name, only that table: an entity's
	// join table is another table. Asked by entity or class name, every table
	// the entity is stored in.
	matches := func(name string) bool {
		c := CanonicalConfigKey(name)
		if prefix {
			return strings.HasPrefix(c, canon)
		}
		return c == canon
	}
	// An entity named by its entity or class name brings all its tables — unless
	// the name is also one of its tables (the default naming makes them the same
	// word), when the question was about that table.
	tableNamed := map[string]bool{}
	for _, e := range entities {
		if matches(e.Table) {
			tableNamed[e.Entity] = true
		}
	}
	byEntity := map[string]bool{}
	for _, e := range entities {
		if !tableNamed[e.Entity] && (matches(e.EntityName) || matches(simpleNodeName(e.Entity))) {
			byEntity[e.Entity] = true
		}
	}
	canons := map[string]bool{canon: true}
	for _, e := range entities {
		if matches(e.Table) || byEntity[e.Entity] {
			canons[CanonicalConfigKey(e.Table)] = true
		}
		te := TableEntity{EntityTable: e}
		if node, err := store.GetNode(ctx, e.Entity); err == nil {
			te.Label = node.Label
		} else if err != ErrNodeNotFound {
			return TableUsageReport{}, err
		}
		report.Entities = append(report.Entities, te)
	}
	list := make([]string, 0, len(canons))
	for c := range canons {
		list = append(list, c)
	}
	sort.Strings(list)
	// A prefix applies to what was asked; the tables an entity brought are exact names.
	var accesses []TableAccess
	if prefix {
		accesses, err = store.FindTableAccesses(ctx, []string{canon}, true, q.Access)
		if err != nil {
			return TableUsageReport{}, err
		}
		var derived []string
		for _, c := range list {
			if c != canon {
				derived = append(derived, c)
			}
		}
		more, err := store.FindTableAccesses(ctx, derived, false, q.Access)
		if err != nil {
			return TableUsageReport{}, err
		}
		accesses = dedupeAccesses(append(accesses, more...))
	} else {
		accesses, err = store.FindTableAccesses(ctx, list, false, q.Access)
		if err != nil {
			return TableUsageReport{}, err
		}
	}

	tables := map[string]bool{}
	for _, a := range accesses {
		name := a.Table
		if a.Schema != "" {
			name = a.Schema + "." + a.Table
		}
		tables[name] = true
		if a.Access == "write" {
			report.Writes++
		} else {
			report.Reads++
		}
	}
	for t := range tables {
		report.Tables = append(report.Tables, t)
	}
	sort.Strings(report.Tables)
	report.AccessTotal = len(accesses)

	shown := accesses[:min(len(accesses), maxTableAccessDetails)]
	details, err := s.describeAccesses(ctx, store, shown)
	if err != nil {
		return TableUsageReport{}, err
	}
	report.Accesses = details
	if len(accesses) > len(shown) {
		report.Notes = append(report.Notes, "only the first accesses are listed; narrow the table or the access to see the rest")
	}

	entries, total, err := s.entryPointsToTable(ctx, store, accesses, limit)
	if err != nil {
		return TableUsageReport{}, err
	}
	report.EntryPoints, report.EntryPointTotal = entries, total

	if len(entities) == 0 && len(accesses) == 0 {
		report.Notes = append(report.Notes, "no entity maps to this table and no code names it")
	}
	// An entity can live in more than one table; the ones not asked about are said, not mixed in.
	for _, e := range report.Entities {
		if !canons[CanonicalConfigKey(e.Table)] {
			report.Notes = append(report.Notes, "entity "+e.EntityName+" is also stored in "+e.Table+" ("+e.Kind+" table); ask for it by name, or by the entity's name when that differs from its table")
		}
	}
	if len(accesses) > 0 {
		report.Notes = append(report.Notes, "read from the source: a table named only by a variable, a trigger, a cascade or a view is not seen, and a JPQL join path names no table of its own")
	}
	return report, nil
}

// describeAccesses names the method behind each access and who calls it.
func (s *Service) describeAccesses(ctx context.Context, store Store, accesses []TableAccess) ([]TableAccessDetail, error) {
	details := make([]TableAccessDetail, 0, len(accesses))
	for _, a := range accesses {
		d := TableAccessDetail{TableAccess: a}
		if node, err := store.GetNode(ctx, a.From); err == nil {
			d.Label = node.Label
		} else if err != ErrNodeNotFound {
			return nil, err
		}
		callers, err := store.GetInEdges(ctx, a.From)
		if err != nil {
			return nil, err
		}
		var ids []string
		for _, e := range callers {
			if e.Kind == "call" && len(ids) < maxTableCallers {
				ids = append(ids, e.From)
			}
		}
		nodes, err := store.GetNodes(ctx, ids)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			if n, ok := nodes[id]; ok {
				d.CalledBy = append(d.CalledBy, touchOf(n, ""))
			}
		}
		details = append(details, d)
	}
	return details, nil
}

// entryPointsToTable walks up from every method that accesses the table, once
// for the readers and once for the writers, and says per entry point which it does.
func (s *Service) entryPointsToTable(ctx context.Context, store Store, accesses []TableAccess, limit int) ([]ReachedEntryPoint, int, error) {
	var readers, writers []reachSeed
	seenRead, seenWrite := map[string]bool{}, map[string]bool{}
	label := map[string]string{}
	for _, a := range accesses {
		if _, ok := label[a.From]; !ok {
			label[a.From] = a.From
			if node, err := store.GetNode(ctx, a.From); err == nil {
				label[a.From] = node.Label
			} else if err != ErrNodeNotFound {
				return nil, 0, err
			}
		}
		if a.Access == "write" {
			if !seenWrite[a.From] && len(writers) < maxConfigSeeds {
				seenWrite[a.From] = true
				writers = append(writers, reachSeed{a.From, label[a.From]})
			}
		} else if !seenRead[a.From] && len(readers) < maxConfigSeeds {
			seenRead[a.From] = true
			readers = append(readers, reachSeed{a.From, label[a.From]})
		}
	}
	if len(readers)+len(writers) == 0 {
		return []ReachedEntryPoint{}, 0, nil
	}
	byNode, err := entryPointsByNode(ctx, store)
	if err != nil {
		return nil, 0, err
	}
	reads, err := reachEntryPoints(ctx, store, readers, byNode)
	if err != nil {
		return nil, 0, err
	}
	writes, err := reachEntryPoints(ctx, store, writers, byNode)
	if err != nil {
		return nil, 0, err
	}

	merged := map[string]ReachedEntryPoint{}
	for id, e := range reads {
		e.Access = "read"
		merged[id] = e
	}
	for id, w := range writes {
		if r, ok := merged[id]; ok {
			// Both: the nearer of the two is where it reaches the table first.
			if w.Depth < r.Depth {
				r.Via, r.Depth = w.Via, w.Depth
			}
			r.Access = "read, write"
			merged[id] = r
			continue
		}
		w.Access = "write"
		merged[id] = w
	}
	out, total := sortedReached(merged, limit)
	return out, total, nil
}

// dedupeAccesses drops the repeats two overlapping lookups return, keeping order.
func dedupeAccesses(in []TableAccess) []TableAccess {
	seen := map[TableAccess]bool{}
	out := make([]TableAccess, 0, len(in))
	for _, a := range in {
		if !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	return out
}
