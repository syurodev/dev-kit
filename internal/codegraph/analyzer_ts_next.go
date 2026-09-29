// internal/codegraph/analyzer_ts_next.go
package codegraph

import (
	"path"
	"strings"
)

// Next.js App Router, and the reason it is here.
//
// It is not the framework this work was asked for — NestJS is. It exists
// because an interface with one implementation is a guess about what varies,
// and the guess is usually wrong. Next declares its entry points with nothing
// NestJS uses: no decorators, no classes, no dependency injection. A route is
// a DIRECTORY, and a handler is an exported function whose NAME is the HTTP
// method. If tsFramework can express that as well as it expresses Nest, the
// seam is real; if it could not, better to find out at two frameworks than at
// five.
//
// It is deliberately small. Route handlers only — a page is an entry point for
// a person rather than for a caller, and the graph is about callers.

type nextFramework struct{}

func (nextFramework) Name() string { return "nextjs" }

func (nextFramework) Detect(p *tsProject) bool { return p.dependsOn("next") }

// nextRouteExports are the exported names the App Router serves, by HTTP
// method. The name IS the method, which is the whole convention.
var nextRouteExports = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "PATCH": true,
	"DELETE": true, "HEAD": true, "OPTIONS": true,
}

func (nextFramework) EntryPoints(p *tsProject, f *tsFile) []EntryPoint {
	route, ok := nextRoutePath(f.rel)
	if !ok {
		return nil
	}
	var entries []EntryPoint
	for _, decl := range f.decls {
		if decl.owner != nil || decl.kind != "method" || !nextRouteExports[decl.name] {
			continue
		}
		entries = append(entries, EntryPoint{
			Verb:      decl.name,
			Target:    route,
			EntryNode: decl.nodeID,
			Kind:      EntryKindHTTP,
			// A route file IS the grouping: GET and POST of /api/users sit
			// under the route they share, not under "route.ts".
			Owner: f.module,
			Group: route,
		})
	}
	return entries
}

func (nextFramework) Layer(p *tsProject, f *tsFile, d *tsDecl) string {
	if _, ok := nextRoutePath(f.rel); ok {
		return "controller"
	}
	return ""
}

// Edges: none. Next wires nothing — a route handler imports what it needs and
// calls it, which the call graph already shows.
func (nextFramework) Edges(p *tsProject, f *tsFile) []Edge { return nil }

// nextRoutePath turns a file path into the URL the App Router serves it at,
// or reports that the file is not a route handler.
//
// The two conventions that matter are both about segments that are NOT part of
// the URL: a `(group)` exists to organise files and a `@slot` is a parallel
// route, and neither appears in the path a caller requests. A `[param]` does
// appear, and is kept as written — it is what the route looks like in every
// other tool that shows one.
func nextRoutePath(rel string) (string, bool) {
	base := path.Base(rel)
	name := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(base, ".ts"), ".tsx"), ".js")
	if name != "route" {
		return "", false
	}

	segments := strings.Split(path.Dir(rel), "/")
	start := -1
	for i, segment := range segments {
		if segment == "app" {
			start = i + 1
		}
	}
	// `app/` may sit at the root or under `src/`; without it this is not the
	// App Router and the file is some other route.ts.
	if start < 0 {
		return "", false
	}

	var kept []string
	for _, segment := range segments[start:] {
		if segment == "" || segment == "." {
			continue
		}
		if strings.HasPrefix(segment, "(") && strings.HasSuffix(segment, ")") {
			continue // a route group organises files, not URLs
		}
		if strings.HasPrefix(segment, "@") {
			continue // a parallel route slot
		}
		kept = append(kept, segment)
	}
	return tsPathJoin(kept...), true
}
