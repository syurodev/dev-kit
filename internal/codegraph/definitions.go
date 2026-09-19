// internal/codegraph/definitions.go
package codegraph

import (
	"context"
	"sort"
	"strings"
)

// maxDefinitionMatches caps the fallback-by-name tier of FindDefinitions.
// Unlike SearchSymbol's substring LIKE (which a one-letter query turns into
// most of the project, hence maxSymbolMatches at the SQL layer), an exact name
// match is naturally small — a real project rarely declares hundreds of
// classes or methods with the identical simple name — so the cap is applied
// here, after ranking, rather than at the query: capping the query first could
// cut same-file/dependency candidates before they ever got a chance to rank
// above the noise.
const maxDefinitionMatches = 50

// matchesIdentifier is FindDefinitions' single definition of "does this node
// name the identifier under the cursor". A node's label is either a class's
// bare qualified name ("com.acme.OrderService") or "<typeId>.<methodName>"
// for a method — never a bare simple name and never carrying the parameter
// signature that lives in the ID instead (see ProjectAnalyzer.addMethodNode's
// node.id / node.label split). So the identifier under the cursor — always
// just the simple name the editor highlighted — either equals the whole
// label (a top-level class in the default package) or names its last
// dot-separated segment. Comparison is case-sensitive: Java identifiers are
// case-sensitive, and a case-insensitive match would confuse "Order" the
// class with an "order" field or parameter on the same screen.
func matchesIdentifier(label, identifier string) bool {
	if identifier == "" {
		return false
	}
	if label == identifier {
		return true
	}
	return strings.HasSuffix(label, "."+identifier)
}

// hasLocation reports whether a node is something the editor could actually
// jump to. A zero file path or line is how an unresolved/external reference
// shows up here — a library call site's To is "", a field's declaring class
// lives in a jar — and FindDefinitions must skip those rather than send the
// editor to open a location that does not exist in this project.
func hasLocation(n Node) bool {
	return n.FilePath != "" && n.Line > 0
}

func definitionFromNode(n Node) Definition {
	return Definition{NodeID: n.ID, Label: n.Label, Kind: n.Kind, FilePath: n.FilePath, Line: n.Line}
}

// FindDefinitions answers "go to definition" for the identifier under the
// cursor at filePath:line. The index has line granularity only (no columns),
// so it cannot know which identifier on a busy line was clicked — the editor
// resolves that locally and sends the exact text.
//
// Three tiers, tried in order, stopping at the first that yields anything:
//
//  1. Call sites recorded at that exact line, filtered to the ones whose
//     target's name matches — the precise case, since the analyzer already
//     resolved the call.
//  2. Field accesses recorded at that exact line, resolved to the declaring
//     class — the index has no separate field nodes (see field_accesses'
//     schema comment), so "go to definition" on a field lands on its class.
//  3. Fallback by name across the whole project — what makes a bare type
//     reference like "DeviceListResponse" work, since it is never the target
//     of a call site or a field access.
//
// Each tier can return more than one Definition (an ambiguous overload, two
// calls to the same name on one line) — that is not an error, it is the
// contract: the caller shows a list.
func (s *Service) FindDefinitions(ctx context.Context, filePath string, line int, identifier string) ([]Definition, error) {
	store, root, release, err := s.activeProject()
	if err != nil {
		return nil, err
	}
	defer release()

	filePath, err = projectRelative(root, filePath)
	if err != nil {
		return nil, err
	}
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return []Definition{}, nil
	}

	if defs, err := definitionsFromCallSites(ctx, store, filePath, line, identifier); err != nil || len(defs) > 0 {
		return defs, err
	}
	if defs, err := definitionsFromFieldAccesses(ctx, store, filePath, line, identifier); err != nil || len(defs) > 0 {
		return defs, err
	}
	return definitionsByName(ctx, store, filePath, line, identifier)
}

// definitionsFromCallSites is FindDefinitions' first tier. It does not care
// which node encloses the line — GetCallSitesAt already scopes to filePath
// and the exact line by joining call_sites to the enclosing node's own
// file_path — so a call inside a method and a call inside a static field
// initializer (recorded against the class itself, not a method) are both
// found the same way.
func definitionsFromCallSites(ctx context.Context, store Store, filePath string, line int, identifier string) ([]Definition, error) {
	sites, err := store.GetCallSitesAt(ctx, filePath, line)
	if err != nil {
		return nil, err
	}
	if len(sites) == 0 {
		return nil, nil
	}

	targetIDs := make([]string, 0, len(sites))
	for _, cs := range sites {
		if cs.To != "" {
			targetIDs = append(targetIDs, cs.To)
		}
	}
	targets, err := store.GetNodes(ctx, targetIDs)
	if err != nil {
		return nil, err
	}

	var out []Definition
	seen := make(map[string]bool, len(sites))
	for _, cs := range sites {
		target, ok := targets[cs.To]
		if !ok || seen[target.ID] || !hasLocation(target) || !matchesIdentifier(target.Label, identifier) {
			continue
		}
		seen[target.ID] = true
		out = append(out, definitionFromNode(target))
	}
	return out, nil
}

// definitionsFromFieldAccesses is FindDefinitions' second tier: a field read
// or write at this exact line, resolved to the class that declares it.
// identifier is passed straight to the store as the field name being looked
// up — field_accesses has no separate "does this field's name match" step to
// run through matchesIdentifier, since a field access's field_name is already
// the plain field identifier, not a label built out of an enclosing type.
func definitionsFromFieldAccesses(ctx context.Context, store Store, filePath string, line int, identifier string) ([]Definition, error) {
	accesses, err := store.GetFieldAccessesAt(ctx, filePath, line, identifier)
	if err != nil {
		return nil, err
	}
	if len(accesses) == 0 {
		return nil, nil
	}

	classIDs := make([]string, 0, len(accesses))
	for _, fa := range accesses {
		classIDs = append(classIDs, fa.ClassID)
	}
	classes, err := store.GetNodes(ctx, classIDs)
	if err != nil {
		return nil, err
	}

	var out []Definition
	seen := make(map[string]bool, len(accesses))
	for _, fa := range accesses {
		class, ok := classes[fa.ClassID]
		if !ok || seen[class.ID] || !hasLocation(class) {
			continue
		}
		seen[class.ID] = true
		out = append(out, definitionFromNode(class))
	}
	return out, nil
}

// definitionsByName is FindDefinitions' last-resort tier: every node in the
// project whose label names identifier, ranked by how likely it is to be the
// one the cursor meant.
//
// Rank: the same file the cursor is in, then a file that file directly
// depends on (file_dependencies — one hop, the same relation GetBlastRadius
// walks transitively for the opposite question), then everything else. Within
// a rank, node ID order keeps the result deterministic across calls.
//
// The node sitting at the cursor's own declaration line, in the same file, is
// excluded — jumping to exactly where you clicked is not "going to"
// anywhere — UNLESS removing it would leave nothing at all, in which case it
// is the only useful answer available and is kept.
func definitionsByName(ctx context.Context, store Store, filePath string, line int, identifier string) ([]Definition, error) {
	candidates, err := store.FindNodesByName(ctx, identifier)
	if err != nil {
		return nil, err
	}
	deps, err := store.GetDependencies(ctx, filePath)
	if err != nil {
		return nil, err
	}
	dependsOn := make(map[string]bool, len(deps))
	for _, d := range deps {
		dependsOn[d] = true
	}

	rankOf := func(n Node) int {
		switch {
		case n.FilePath == filePath:
			return 0
		case dependsOn[n.FilePath]:
			return 1
		default:
			return 2
		}
	}

	matched := candidates[:0]
	for _, n := range candidates {
		if matchesIdentifier(n.Label, identifier) && hasLocation(n) {
			matched = append(matched, n)
		}
	}
	sort.SliceStable(matched, func(i, j int) bool {
		if ri, rj := rankOf(matched[i]), rankOf(matched[j]); ri != rj {
			return ri < rj
		}
		return matched[i].ID < matched[j].ID
	})

	all := make([]Definition, 0, len(matched))
	withoutSelf := make([]Definition, 0, len(matched))
	for _, n := range matched {
		d := definitionFromNode(n)
		all = append(all, d)
		if !(n.FilePath == filePath && n.Line == line) {
			withoutSelf = append(withoutSelf, d)
		}
	}

	out := withoutSelf
	if len(out) == 0 {
		out = all
	}
	if len(out) > maxDefinitionMatches {
		out = out[:maxDefinitionMatches]
	}
	if out == nil {
		out = []Definition{}
	}
	return out, nil
}
