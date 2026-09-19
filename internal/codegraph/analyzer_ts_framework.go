// internal/codegraph/analyzer_ts_framework.go
package codegraph

import (
	"path"
	"sort"
	"strings"
)

// What a TypeScript project is FOR, as a seam rather than a switch.
//
// The Lua analyzer put Kong's conventions in a file of its own, which was
// enough while there was one framework. TypeScript has no such luck: NestJS
// declares its entry points with decorators, Next.js declares them with
// directory names and exported identifiers, Express declares them with calls
// at run time. Those have nothing in common except that each is a reading of
// the same language-level model, so that is exactly what the interface says —
// a framework is handed files, declarations and decorators, and answers in
// endpoints, layers and edges.
//
// The rule for adding one: a new framework is a new file implementing
// tsFramework plus a line in tsFrameworks. It may not touch the model, the
// analyzer, or another framework. If it needs something the model does not
// carry, the model grows a language-level fact — never a framework-shaped one.

// tsFramework is one framework's conventions.
type tsFramework interface {
	// Name identifies the framework in diagnostics and tests.
	Name() string

	// Detect reports whether this project uses it. A project may use several
	// (a Nest API beside a Next dashboard), so this is not exclusive.
	Detect(p *tsProject) bool

	// Endpoints are the entry points one file declares. ID is left empty and
	// filled in by the analyzer, which is the only thing that can see the
	// whole project and so the only thing that can make an id unique.
	Endpoints(p *tsProject, f *tsFile) []Endpoint

	// Layer classifies one declaration, or returns "" for no opinion — which
	// is the normal answer, and lets the generic fallback have it.
	Layer(p *tsProject, f *tsFile, d *tsDecl) string

	// Edges are relationships this framework knows about that no call
	// expression shows: NestJS's constructor injection is the example, and it
	// is the same "di" edge Spring's field injection produces.
	Edges(p *tsProject, f *tsFile) []Edge
}

// tsFrameworks is the registry, in the order they are asked. First opinion
// wins for a layer, so a framework that is sure about a file should come
// before one that guesses.
func tsFrameworks() []tsFramework {
	return []tsFramework{nestFramework{}, nextFramework{}}
}

// tsClassNamed is how a framework turns a type NAME into the node it refers
// to — what constructor injection needs, since a parameter's type is written
// as a bare identifier and the graph needs a node.
//
// Exactly one match, or nothing. Two classes of the same name in one project
// is common (a DTO per module named `Query`), and picking either would put an
// edge on a screen that is simply wrong. A framework that gets nothing back
// records no edge, which is the honest outcome.
func (p *tsProject) classNamed(name string) *tsDecl {
	if name == "" {
		return nil
	}
	var found *tsDecl
	for _, file := range p.files {
		for _, decl := range file.decls {
			if decl.kind != "class" || decl.name != name || decl.owner != nil {
				continue
			}
			if found != nil {
				return nil
			}
			found = decl
		}
	}
	return found
}

// tsBaseType strips a type expression down to the name a class node could
// carry: `Repository<User>` is a Repository, `Foo | undefined` is nothing this
// can resolve, and an inline object type is nothing at all.
func tsBaseType(typeName string) string {
	name := strings.TrimSpace(typeName)
	if at := strings.IndexByte(name, '<'); at >= 0 {
		name = name[:at]
	}
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, " |&(){}[]'\"`=>") {
		return ""
	}
	// A qualified type (`typeorm.Repository`) resolves by its last segment,
	// which is the name a class node carries.
	if at := strings.LastIndexByte(name, '.'); at >= 0 {
		name = name[at+1:]
	}
	return name
}

// tsGenericLayer is the fallback when no framework has an opinion: what the
// file is called.
//
// Weak evidence, and deliberately last. It is here because the convention is
// near-universal in TypeScript backends — `users.service.ts`, `user.entity.ts`
// — and because a graph where every node is the same grey is a graph nobody
// reads. A framework that knows better always wins.
func tsGenericLayer(f *tsFile) string {
	base := strings.ToLower(path.Base(f.rel))
	for suffix, layer := range map[string]string{
		".controller": "controller",
		".resolver":   "controller",
		".gateway":    "controller",
		".router":     "controller",
		".route":      "controller",
		".service":    "service",
		".usecase":    "service",
		".repository": "repository",
		".dao":        "repository",
		".entity":     "repository",
		".schema":     "repository",
		".model":      "repository",
		".store":      "repository",
	} {
		if tsBaseHasPart(base, suffix) {
			return layer
		}
	}
	// A directory can say it too: src/repositories/user.ts.
	for _, segment := range strings.Split(strings.ToLower(path.Dir(f.rel)), "/") {
		switch segment {
		case "controllers", "routes", "resolvers":
			return "controller"
		case "services", "usecases":
			return "service"
		case "repositories", "entities", "models", "daos":
			return "repository"
		}
	}
	return ""
}

// tsBaseHasPart reports whether a file's base name carries a dotted part such
// as ".service" — matching `users.service.ts` but not `serviceable.ts`.
func tsBaseHasPart(base, part string) bool {
	return strings.Contains(base, part+".") || strings.HasSuffix(base, part)
}

// tsPathJoin joins a route prefix and a route path the way a router does:
// always one leading slash, no trailing one, and an empty part contributing
// nothing rather than a stray separator.
func tsPathJoin(parts ...string) string {
	var segments []string
	for _, part := range parts {
		for _, segment := range strings.Split(part, "/") {
			if segment != "" {
				segments = append(segments, segment)
			}
		}
	}
	if len(segments) == 0 {
		return "/"
	}
	return "/" + strings.Join(segments, "/")
}

// tsSortEndpoints keeps the endpoint list stable between two identical runs.
func tsSortEndpoints(endpoints []Endpoint) {
	sort.SliceStable(endpoints, func(i, j int) bool { return endpoints[i].ID < endpoints[j].ID })
}
