// internal/codegraph/analyzer_ts_nest.go
package codegraph

import (
	"strings"
)

// NestJS, which is the closest thing TypeScript has to Spring — and is read
// here almost exactly the way the Java analyzer reads Spring.
//
// The correspondence is not a coincidence: Nest copied the model. @Controller
// is @RestController, @Injectable is @Service, constructor injection is field
// injection, and a route is a class-level prefix crossed with a method-level
// path. So the layers mean the same thing they mean everywhere else in
// CodeGraph, and a "di" edge here is the same edge Spring produces.
//
// Everything below is matched by the decorator's simple name, the same policy
// LayerClassifier.java uses and for the same reason: it works whether or not
// the import can be resolved.

type nestFramework struct{}

func (nestFramework) Name() string { return "nestjs" }

func (nestFramework) Detect(p *tsProject) bool {
	return p.dependsOn("@nestjs/common", "@nestjs/core")
}

// nestRouteDecorators map a method decorator to the HTTP method it serves.
// @All matches every method, and is reported as "ANY" — the same word the
// Java analyzer uses for a @RequestMapping that named none.
var nestRouteDecorators = map[string]string{
	"Get": "GET", "Post": "POST", "Put": "PUT", "Patch": "PATCH",
	"Delete": "DELETE", "Options": "OPTIONS", "Head": "HEAD", "All": "ANY",
}

// nestMessageDecorators are the entry points that are not HTTP at all. A Nest
// service can be a microservice or a websocket gateway, and those handlers are
// every bit as much an entry point as a route — the same way a Kong plugin's
// phases are, with no URL to show for it either.
var nestMessageDecorators = map[string]string{
	"MessagePattern":   "MESSAGE",
	"EventPattern":     "EVENT",
	"SubscribeMessage": "SOCKET",
	"Cron":             "CRON",
	"Interval":         "INTERVAL",
	"Timeout":          "TIMEOUT",
}

func (nestFramework) Endpoints(p *tsProject, f *tsFile) []Endpoint {
	var endpoints []Endpoint
	for _, class := range f.decls {
		if class.kind != "class" || class.owner != nil {
			continue
		}
		controller, isController := class.decorator("Controller")
		gateway, isGateway := class.decorator("WebSocketGateway")
		if !isController && !isGateway {
			continue
		}
		prefix := controller.StringArg(0)
		_ = gateway

		for _, method := range f.decls {
			if method.owner != class {
				continue
			}
			for _, decorator := range method.decorators {
				if httpMethod, ok := nestRouteDecorators[decorator.Name]; ok {
					endpoints = append(endpoints, Endpoint{
						HTTPMethod: httpMethod,
						Path:       tsPathJoin(prefix, decorator.StringArg(0)),
						EntryNode:  method.nodeID,
					})
					continue
				}
				if kind, ok := nestMessageDecorators[decorator.Name]; ok {
					endpoints = append(endpoints, Endpoint{
						HTTPMethod: kind,
						Path:       nestPatternName(decorator, class, method),
						EntryNode:  method.nodeID,
					})
				}
			}
		}
	}
	return endpoints
}

// nestPatternName is what to show beside a non-HTTP entry point. The pattern
// when it is a plain string, and the handler's own name when it is an object
// or a constant — which is better than showing the source text of an object
// literal in a sidebar.
func nestPatternName(decorator tsDecorator, class, method *tsDecl) string {
	if pattern := decorator.StringArg(0); pattern != "" {
		return pattern
	}
	return class.name + "." + method.name
}

// nestRepositoryTypes are the types that make the class holding one a
// repository, whatever it calls itself.
var nestRepositoryTypes = map[string]bool{
	"Repository": true, "EntityManager": true, "DataSource": true,
	"MongoRepository": true, "TreeRepository": true, "Connection": true,
	"PrismaService": true, "PrismaClient": true, "Knex": true, "Pool": true,
	"Model": true, "Sequelize": true,
}

func (nestFramework) Layer(p *tsProject, f *tsFile, d *tsDecl) string {
	if d.kind != "class" || d.owner != nil {
		return ""
	}
	if _, ok := d.decorator("Controller", "WebSocketGateway", "Resolver"); ok {
		return "controller"
	}
	if _, ok := d.decorator("Entity", "Schema", "ObjectType", "InputType"); ok {
		return "repository"
	}
	if _, ok := d.decorator("Injectable"); !ok {
		return ""
	}
	// An injectable that holds a database handle is where the data is, whether
	// or not it is called a repository. That is the one place this reads more
	// than the decorator: Nest marks every provider the same way, so
	// @Injectable alone cannot tell a service from a repository, and the
	// difference is the entire point of having two layers.
	if nestInjectsAStore(p, f, d) {
		return "repository"
	}
	for _, suffix := range []string{"Repository", "Dao", "Store"} {
		if strings.HasSuffix(d.name, suffix) {
			return "repository"
		}
	}
	return "service"
}

// nestInjectsAStore reports whether a class takes a database or cache handle
// in its constructor.
func nestInjectsAStore(p *tsProject, f *tsFile, class *tsDecl) bool {
	for _, member := range f.decls {
		if member.owner != class || member.symbolKind != lspSymbolConstructor {
			continue
		}
		for _, param := range member.params {
			if nestRepositoryTypes[tsBaseType(param.Type)] {
				return true
			}
		}
	}
	return false
}

// Edges turns constructor injection into the graph's "di" edge.
//
// This is the relationship no call expression shows and the one that makes a
// Nest project readable: a controller never calls its service's constructor,
// it declares a dependency and the framework supplies it. Exactly what the
// Java analyzer records for an @Autowired field.
func (nestFramework) Edges(p *tsProject, f *tsFile) []Edge {
	var edges []Edge
	seen := map[string]bool{}
	for _, class := range f.decls {
		if class.kind != "class" || class.owner != nil {
			continue
		}
		if _, injectable := class.decorator("Controller", "Injectable", "WebSocketGateway", "Resolver"); !injectable {
			continue
		}
		for _, member := range f.decls {
			if member.owner != class || member.symbolKind != lspSymbolConstructor {
				continue
			}
			for _, param := range member.params {
				target := p.classNamed(tsBaseType(param.Type))
				if target == nil || target == class {
					continue
				}
				key := class.nodeID + "\x00" + target.nodeID
				if seen[key] {
					continue
				}
				seen[key] = true
				edges = append(edges, Edge{From: class.nodeID, To: target.nodeID, Kind: "di"})
			}
		}
	}
	return edges
}
