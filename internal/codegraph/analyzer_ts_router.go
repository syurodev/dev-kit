// internal/codegraph/analyzer_ts_router.go
package codegraph

import (
	"path"
	"strings"
)

// The Express family: every framework whose routes are registered by CALLING
// a method on an app or a router.
//
// Express, Fastify, Hono, Koa's router, Restify — they disagree about almost
// everything else and agree completely about this: `app.get('/users', handler)`.
// One reading covers them all, which is why this is one framework rather than
// five, and it is the reading that needed the model to carry call expressions
// at all. NestJS declares its routes and Next.js names them after directories;
// these declare them at run time, and the only trace in the source is the call.
//
// What it does not do is follow `app.use('/api', subrouter)`. Mounting moves
// every route underneath it, and knowing where means tracking a router value
// across the files it is passed through — a real piece of work, and reporting
// a route at the wrong path would be worse than reporting it at its own.

type routerFramework struct{}

func (routerFramework) Name() string { return "router" }

func (routerFramework) Detect(p *tsProject) bool {
	return p.dependsOn(
		"express", "fastify", "hono", "koa", "@koa/router", "koa-router",
		"restify", "polka", "@hono/node-server",
	)
}

// routerMethods are the registration calls, by the HTTP method they serve.
// `all` matches every method and is reported as "ANY", the same word the Java
// analyzer uses for a mapping that named none.
var routerMethods = map[string]string{
	"get": "GET", "post": "POST", "put": "PUT", "patch": "PATCH",
	"delete": "DELETE", "del": "DELETE", "head": "HEAD",
	"options": "OPTIONS", "all": "ANY",
}

func (routerFramework) EntryPoints(p *tsProject, f *tsFile) []EntryPoint {
	var entries []EntryPoint
	for _, call := range f.calls {
		httpMethod, isRoute := routerMethods[call.Method]
		if !isRoute {
			continue
		}
		// The path is the guard. Every one of these names is a method on
		// something else too — a Map has get, a cache has all — and a route is
		// the only one of them whose first argument is a path.
		route := call.StringArg(0)
		if !strings.HasPrefix(route, "/") {
			continue
		}
		handler := p.handlerFor(f, call)
		if handler == nil {
			// Registered from somewhere with no declaration around it at all.
			// There is nothing for a flow to start from, and inventing a node
			// for it would put a symbol in the index that names nothing.
			continue
		}
		entries = append(entries, EntryPoint{
			Verb:      httpMethod,
			Target:    route,
			EntryNode: handler.nodeID,
			Kind:      EntryKindHTTP,
			// The file that registers the route is its heading. The handler's
			// own class, when it has one, is a detail of how it is written.
			Owner: f.module,
			Group: path.Base(f.rel),
		})
	}
	return entries
}

// Layer: a file that registers routes is where this project is entered.
func (routerFramework) Layer(p *tsProject, f *tsFile, d *tsDecl) string {
	if !routerRegistersRoutes(f) {
		return ""
	}
	return "controller"
}

func routerRegistersRoutes(f *tsFile) bool {
	for _, call := range f.calls {
		if routerMethods[call.Method] == "" {
			continue
		}
		if strings.HasPrefix(call.StringArg(0), "/") {
			return true
		}
	}
	return false
}

// Edges: none. These frameworks wire nothing — a handler imports what it needs
// and calls it, which the checker already resolved into the call graph.
func (routerFramework) Edges(p *tsProject, f *tsFile) []Edge { return nil }
