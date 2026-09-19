// internal/codegraph/references.go
package codegraph

import (
	"context"
	"sort"
	"strings"
)

// maxReferences caps FindReferences' result the same way maxDefinitionMatches
// caps FindDefinitions' fallback tier: a widely-used method or a field
// touched from a hundred places is a real project shape (a shared util, a
// heavily-injected repository), not a bug, so this is a display ceiling
// rather than a correctness signal.
const maxReferences = 200

// symbol is what an identifier at file:line refers to, resolved once and
// shared by FindReferences and FindImplementations — both need to answer
// "which class/method/field is this" before they can ask their own question
// about it. FindDefinitions asks a related but different question ("where
// should the editor jump to"); see resolveSymbol's doc comment for how the
// two diverge. Two shapes because the index has no field nodes (see
// FieldAccess's doc comment): a class or method resolves to its Node; a
// field resolves to the class that declares it plus the field's own name.
type symbol struct {
	Node  *Node
	Field *fieldSymbol
}

// fieldSymbol identifies a field the index has no node for: the class that
// declares it (a class Node.ID) and the field's plain name.
type fieldSymbol struct {
	ClassID   string
	FieldName string
}

// resolveSymbol answers "what does the identifier at file:line refer to" —
// the shared first step of FindReferences and FindImplementations. It is
// FindDefinitions' question turned around: FindDefinitions asks "where
// should the editor jump to", and for that the node sitting at the cursor's
// own declaration line is deliberately excluded (jumping to where you
// already are is not "going to" anywhere) unless it is the only candidate.
// Here the opposite must hold: landing on a symbol's own declaration line
// must resolve to THAT symbol, not to some other same-named node elsewhere
// that FindDefinitions' ranking would have preferred — "find references to
// the method my cursor is declared on" is the ordinary case, not a
// degenerate one.
//
// Tiers, in order, stopping at the first that yields anything:
//
//  0. A node declared at exactly this file:line whose label matches — the
//     cursor is on the symbol's own declaration.
//  1. Call sites at this line (FindDefinitions' tier 1, shared via
//     callSiteTargets) — the identifier is itself a call.
//  2. Field accesses at this line (FindDefinitions' tier 2, shared via
//     fieldAccessClasses) — the identifier is a field read/write; the
//     symbol is the field itself, not the class the tier surfaces it
//     through.
//  3. The narrowest node enclosing this line, if the identifier names it.
//     Tier 0 asks for the node's own line, and that is where its
//     declaration STARTS: for a method under @Override or @GetMapping —
//     most methods in a Spring project — the annotation's line, one or more
//     above the name. So a cursor on the name misses tier 0. It comes after
//     the call-site and field tiers on purpose: inside list(), a line
//     calling repository.list() names the callee, and tier 1 has already
//     said so; only a line with no such use falls through to here.
//  4. Every node named identifier, project-wide — the same candidate set
//     FindDefinitions' fallback tier ranks (nodesByName), recomputed from
//     tier 0's own query rather than queried twice.
//
// Can return more than one symbol when the identifier is genuinely
// ambiguous (two overloads called on one line, several same-named classes
// with nothing to disambiguate by) — same contract as FindDefinitions.
// Callers union the results of each.
func resolveSymbol(ctx context.Context, store Store, filePath string, line int, identifier string) ([]symbol, error) {
	named, err := nodesByName(ctx, store, identifier)
	if err != nil {
		return nil, err
	}
	for i, n := range named {
		if n.FilePath == filePath && n.Line == line {
			return []symbol{{Node: &named[i]}}, nil
		}
	}

	if targets, err := callSiteTargets(ctx, store, filePath, line, identifier); err != nil || len(targets) > 0 {
		if err != nil {
			return nil, err
		}
		return nodeSymbols(targets), nil
	}
	if classes, err := fieldAccessClasses(ctx, store, filePath, line, identifier); err != nil || len(classes) > 0 {
		if err != nil {
			return nil, err
		}
		out := make([]symbol, len(classes))
		for i, c := range classes {
			out[i] = symbol{Field: &fieldSymbol{ClassID: c.ID, FieldName: identifier}}
		}
		return out, nil
	}
	if enclosing, ok, err := store.FindNodeAtLine(ctx, filePath, line); err != nil {
		return nil, err
	} else if ok && matchesIdentifier(enclosing.Label, identifier) {
		return []symbol{{Node: &enclosing}}, nil
	}
	return nodeSymbols(named), nil
}

func nodeSymbols(nodes []Node) []symbol {
	out := make([]symbol, len(nodes))
	for i := range nodes {
		out[i] = symbol{Node: &nodes[i]}
	}
	return out
}

// FindReferences answers "find references" for the identifier under the
// cursor at filePath:line: every other place in the project that uses it.
//
// Resolves the symbol once (resolveSymbol), then asks referencesToSymbol a
// per-kind question for each resolved symbol and unions the results —
// ambiguous input (resolveSymbol returning more than one symbol) is treated
// the same way FindDefinitions treats it: show everything it could mean
// rather than guessing.
func (s *Service) FindReferences(ctx context.Context, filePath string, line int, identifier string) ([]Reference, error) {
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
		return []Reference{}, nil
	}

	symbols, err := resolveSymbol(ctx, store, filePath, line, identifier)
	if err != nil {
		return nil, err
	}

	type dedupeKey struct {
		file string
		line int
		kind string
	}
	seen := make(map[dedupeKey]bool)
	var out []Reference
	for _, sym := range symbols {
		refs, err := referencesToSymbol(ctx, store, sym)
		if err != nil {
			return nil, err
		}
		for _, r := range refs {
			k := dedupeKey{r.FilePath, r.Line, r.Kind}
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, r)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].FilePath != out[j].FilePath {
			return out[i].FilePath < out[j].FilePath
		}
		return out[i].Line < out[j].Line
	})
	if len(out) > maxReferences {
		out = out[:maxReferences]
	}
	attachReferenceCode(root, out)
	if out == nil {
		out = []Reference{}
	}
	return out, nil
}

// referencesToSymbol is FindReferences' per-kind logic, once the symbol
// under the cursor is known. A symbol of a kind not covered here (there
// currently are none besides method/class/field) yields no references
// rather than an error — an unsupported "what is this" is a reason to show
// nothing, not to fail the whole call.
func referencesToSymbol(ctx context.Context, store Store, sym symbol) ([]Reference, error) {
	switch {
	case sym.Field != nil:
		return referencesToField(ctx, store, *sym.Field)
	case sym.Node != nil && sym.Node.Kind == "method":
		return referencesToMethod(ctx, store, *sym.Node)
	case sym.Node != nil && sym.Node.Kind == "class":
		return referencesToClass(ctx, store, *sym.Node)
	default:
		return nil, nil
	}
}

// referencesToField is every recorded read/write of one field, wherever it
// happens in the project. GetFieldAccesses already scopes to (classID,
// fieldName), so — unlike the method and class tiers — there is no further
// resolution needed: every row it returns is a reference.
func referencesToField(ctx context.Context, store Store, f fieldSymbol) ([]Reference, error) {
	accesses, err := store.GetFieldAccesses(ctx, f.ClassID, f.FieldName)
	if err != nil {
		return nil, err
	}
	if len(accesses) == 0 {
		return nil, nil
	}
	nodeIDs := make([]string, len(accesses))
	for i, a := range accesses {
		nodeIDs[i] = a.NodeID
	}
	nodes, err := store.GetNodes(ctx, nodeIDs)
	if err != nil {
		return nil, err
	}

	var out []Reference
	for _, a := range accesses {
		n, ok := nodes[a.NodeID]
		if !ok || !hasLocation(n) {
			continue
		}
		out = append(out, Reference{NodeID: n.ID, Label: n.Label, Kind: a.Kind, FilePath: n.FilePath, Line: a.Line})
	}
	return out, nil
}

// referencesToClass is every node injecting the class (a "di" edge into it)
// plus, when the index recorded any, calls reaching one of its constructors.
//
// An injection's location is the injecting node's OWN declaration line —
// unlike a call site, a "di" Edge carries no line of its own (the analyzer
// records dependency injection as a plain graph edge, not a call
// expression), so the nearest thing to "where" is where the injecting
// class/method itself is declared.
func referencesToClass(ctx context.Context, store Store, class Node) ([]Reference, error) {
	edges, err := store.GetInEdges(ctx, class.ID)
	if err != nil {
		return nil, err
	}
	var injectorIDs []string
	for _, e := range edges {
		if e.Kind == "di" {
			injectorIDs = append(injectorIDs, e.From)
		}
	}
	injectors, err := store.GetNodes(ctx, injectorIDs)
	if err != nil {
		return nil, err
	}

	var out []Reference
	for _, id := range injectorIDs {
		n, ok := injectors[id]
		if !ok || !hasLocation(n) {
			continue
		}
		out = append(out, Reference{NodeID: n.ID, Label: n.Label, Kind: "inject", FilePath: n.FilePath, Line: n.Line})
	}

	ctorRefs, err := referencesToConstructors(ctx, store, class)
	if err != nil {
		return nil, err
	}
	return append(out, ctorRefs...), nil
}

// classSimpleName recovers a class's simple name from its Node.ID: the
// qualified name (see matchesIdentifier's doc comment on label/id shapes),
// minus a possible " [module]" / " [module #2]" disambiguating suffix
// Index.uniqueId appends when two classes share a qualified name across
// modules. That suffix never contains a dot — its module qualifier has dots
// replaced with underscores before bracketing (ProjectAnalyzer.uniqueId) —
// so stripping it before splitting on the last "." recovers exactly the
// simple name Java requires a constructor to share with its class.
func classSimpleName(classID string) string {
	base := classID
	if i := strings.LastIndex(base, " ["); i >= 0 {
		base = base[:i]
	}
	if i := strings.LastIndex(base, "."); i >= 0 {
		return base[i+1:]
	}
	return base
}

// referencesToConstructors finds the class's own constructor node(s) and
// returns the real calls reaching them.
//
// Constructors ARE indexed as ordinary method nodes:
// ProjectAnalyzer.collectDeclarations runs addMethodNode over every
// ConstructorDeclaration exactly like it does for methods ("constructors
// too: to the graph they are callables like any other" — addMethodNode's own
// comment), so a constructor's label is "<classId>.<ClassSimpleName>", the
// same shape as any other method's label. That makes it findable among
// ListClassMethods' results by name alone, with no separate storage or
// query needed.
func referencesToConstructors(ctx context.Context, store Store, class Node) ([]Reference, error) {
	methods, err := store.ListClassMethods(ctx, class.ID)
	if err != nil {
		return nil, err
	}
	ctorLabel := class.ID + "." + classSimpleName(class.ID)

	var out []Reference
	for _, m := range methods {
		if m.Label != ctorLabel {
			continue
		}
		real, _, err := methodCallers(ctx, store, m)
		if err != nil {
			return nil, err
		}
		out = append(out, real...)
	}
	return out, nil
}

// referencesToMethod is every real call to the method, plus — because a
// Spring-style codebase routes nearly every call through an interface — the
// real calls to every interface/abstract method this one implements.
func referencesToMethod(ctx context.Context, store Store, method Node) ([]Reference, error) {
	real, implements, err := methodCallers(ctx, store, method)
	if err != nil {
		return nil, err
	}
	out := real
	for _, iface := range implements {
		// One hop only: the interface method's own real callers, not
		// whatever IT in turn implements. A codebase deep enough to have
		// interfaces extending interfaces the same-named method is rare
		// enough that going further is not worth the extra query fan-out.
		ifaceReal, _, err := methodCallers(ctx, store, iface)
		if err != nil {
			return nil, err
		}
		out = append(out, ifaceReal...)
	}
	return out, nil
}

// methodCallers splits method's incoming call sites (GetCallSitesTo) into
// real calls and dispatch links, and returns each separately.
//
// A dispatch link is ProjectAnalyzer.linkOverrides' bookkeeping edge from an
// interface/abstract method to every one of its implementations, recorded as
// a call_site FROM the abstract method TO the implementation, at the
// abstract method's OWN declaration line — not a place in source where
// anyone wrote a call. It is recognisable by exactly that: cs.Line equals
// the calling node's (from's) own declared Line, which a real call
// essentially never does (the one false-positive this admits — a genuine
// call on the exact line its enclosing method's signature starts on, i.e. a
// one-line method body — is the same tradeoff the task's own framing of this
// heuristic accepts).
//
// implements collects the FROM node of every dispatch link into method: the
// interface/abstract methods method itself implements, for
// referencesToMethod's one-hop "also show calls through the interface" step.
func methodCallers(ctx context.Context, store Store, method Node) (real []Reference, implements []Node, err error) {
	sites, err := store.GetCallSitesTo(ctx, method.ID)
	if err != nil || len(sites) == 0 {
		return nil, nil, err
	}
	fromIDs := make([]string, len(sites))
	for i, cs := range sites {
		fromIDs[i] = cs.From
	}
	fromNodes, err := store.GetNodes(ctx, fromIDs)
	if err != nil {
		return nil, nil, err
	}

	seenIface := make(map[string]bool)
	for _, cs := range sites {
		from, ok := fromNodes[cs.From]
		if !ok || !hasLocation(from) {
			continue
		}
		if cs.Line == from.Line {
			if !seenIface[from.ID] {
				seenIface[from.ID] = true
				implements = append(implements, from)
			}
			continue
		}
		real = append(real, Reference{NodeID: from.ID, Label: from.Label, Kind: "call", FilePath: from.FilePath, Line: cs.Line})
	}
	return real, implements, nil
}

// FindImplementations answers "find implementations" for the identifier
// under the cursor at filePath:line.
//
//   - A method resolves to the targets of its own dispatch links
//     (implementationsOfMethod). A method that is itself an implementation
//     — nothing dispatches FROM it — has none, and this does not fall back
//     to returning the method itself.
//   - A class or interface resolves to every distinct class that owns a
//     method implementing one of ITS methods (implementationsOfClass).
//   - A field has no meaningful "implementation": yields none.
func (s *Service) FindImplementations(ctx context.Context, filePath string, line int, identifier string) ([]Definition, error) {
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

	symbols, err := resolveSymbol(ctx, store, filePath, line, identifier)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var out []Definition
	for _, sym := range symbols {
		if sym.Node == nil {
			continue // a field: no implementations to find
		}
		var defs []Definition
		switch sym.Node.Kind {
		case "method":
			defs, err = implementationsOfMethod(ctx, store, *sym.Node)
		case "class":
			defs, err = implementationsOfClass(ctx, store, *sym.Node)
		}
		if err != nil {
			return nil, err
		}
		for _, d := range defs {
			if seen[d.NodeID] {
				continue
			}
			seen[d.NodeID] = true
			out = append(out, d)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].FilePath != out[j].FilePath {
			return out[i].FilePath < out[j].FilePath
		}
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].NodeID < out[j].NodeID
	})
	if out == nil {
		out = []Definition{}
	}
	return out, nil
}

// implementationsOfMethod returns the targets of method's own dispatch
// links: call_sites FROM method whose line equals method's own declaration
// line (see methodCallers' doc comment for the same recognition rule applied
// from the callee's side). A method with none is either itself an
// implementation or an ordinary method nothing overrides — either way there
// is nothing to return, and the method is never returned in its own place.
func implementationsOfMethod(ctx context.Context, store Store, method Node) ([]Definition, error) {
	sites, err := store.GetCallSites(ctx, method.ID)
	if err != nil {
		return nil, err
	}
	var targetIDs []string
	for _, cs := range sites {
		if cs.Line == method.Line && cs.To != "" {
			targetIDs = append(targetIDs, cs.To)
		}
	}
	if len(targetIDs) == 0 {
		return nil, nil
	}
	targets, err := store.GetNodes(ctx, targetIDs)
	if err != nil {
		return nil, err
	}

	var out []Definition
	seen := make(map[string]bool)
	for _, id := range targetIDs {
		n, ok := targets[id]
		if !ok || seen[n.ID] || !hasLocation(n) {
			continue
		}
		seen[n.ID] = true
		out = append(out, definitionFromNode(n))
	}
	return out, nil
}

// implementationsOfClass returns the distinct classes that own a method
// implementing one of class's own declared methods: for each of class's
// methods, its dispatch targets (implementationsOfMethod, reused as-is),
// then each target's OWNING class — derived from the implementing method's
// Label via methodClassID, not looked up by a second query.
func implementationsOfClass(ctx context.Context, store Store, class Node) ([]Definition, error) {
	methods, err := store.ListClassMethods(ctx, class.ID)
	if err != nil {
		return nil, err
	}

	classIDs := make(map[string]bool)
	for _, m := range methods {
		targets, err := implementationsOfMethod(ctx, store, m)
		if err != nil {
			return nil, err
		}
		for _, t := range targets {
			if classID, ok := methodClassID(t.Label); ok {
				classIDs[classID] = true
			}
		}
	}
	if len(classIDs) == 0 {
		return nil, nil
	}

	ids := make([]string, 0, len(classIDs))
	for id := range classIDs {
		ids = append(ids, id)
	}
	sort.Strings(ids) // deterministic order regardless of map iteration
	classes, err := store.GetNodes(ctx, ids)
	if err != nil {
		return nil, err
	}

	var out []Definition
	for _, id := range ids {
		n, ok := classes[id]
		if !ok || !hasLocation(n) {
			continue
		}
		out = append(out, definitionFromNode(n))
	}
	return out, nil
}

// methodClassID recovers a method's owning class ID from its Label: a
// method's label is always "<classId>.<methodName>" with no signature (see
// matchesIdentifier's doc comment), and a class's own Node.ID always equals
// its Label — so the prefix before the last "." is already a valid Node.ID
// to look up directly, with no bracket-stripping needed (unlike
// classSimpleName, which goes the other way: ID down to a bare simple name).
func methodClassID(label string) (string, bool) {
	i := strings.LastIndex(label, ".")
	if i < 0 {
		return "", false
	}
	return label[:i], true
}

// maxReferenceCodeLen matches attachSourceLines' code-length cap for the
// data-flow trace (graph_query.go) — a references panel row shows the same
// kind of single clipped source line.
const maxReferenceCodeLen = 160

// attachReferenceCode fills each reference's Code with its source line,
// reading every distinct file once — the same approach attachSourceLines
// (graph_query.go) uses for a DataFlowTrace's steps, applied here to
// References instead. Best effort: a file that moved or cannot be read
// leaves its references without code rather than failing an otherwise
// correct result.
func attachReferenceCode(root string, refs []Reference) {
	files := make(map[string][]string)
	for i := range refs {
		path := refs[i].FilePath
		lines, seen := files[path]
		if !seen {
			if resolved, err := resolvePath(root, path); err == nil {
				if text, err := readTextFile(resolved); err == nil {
					lines = strings.Split(text, "\n")
				}
			}
			files[path] = lines
		}
		if n := refs[i].Line; n >= 1 && n <= len(lines) {
			refs[i].Code = clipLine(strings.TrimSpace(lines[n-1]), maxReferenceCodeLen)
		}
	}
}
