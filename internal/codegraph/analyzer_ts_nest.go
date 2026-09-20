// internal/codegraph/analyzer_ts_nest.go
package codegraph

import (
	"sort"
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

// What each decorator makes a method into, and on what kind of class it
// counts. The second half matters as much as the first: `@Get` on a class that
// is not a controller is not a route, and a scheduled job is never on a
// controller at all — it is on a provider, which is exactly the case the first
// version of this gated out and so never reported.

// nestRouteDecorators map a method decorator to the HTTP method it serves.
// @All matches every method, and is reported as "ANY" — the same word the
// Java analyzer uses for a @RequestMapping that named none.
var nestRouteDecorators = map[string]string{
	"Get": "GET", "Post": "POST", "Put": "PUT", "Patch": "PATCH",
	"Delete": "DELETE", "Options": "OPTIONS", "Head": "HEAD", "All": "ANY",
}

// nestMessageDecorators are the microservice entry points: one handler per
// pattern, whatever transport carries it. TCP, Redis, NATS, Kafka, RabbitMQ
// and MQTT all use these two, which is why they cover most of Nest's
// microservice surface between them.
var nestMessageDecorators = map[string]string{
	"MessagePattern": "MESSAGE",
	"EventPattern":   "EVENT",
}

// nestGrpcDecorators are gRPC's, which name their service and method rather
// than carrying a pattern.
var nestGrpcDecorators = map[string]bool{
	"GrpcMethod": true, "GrpcStreamMethod": true, "GrpcStreamCall": true,
}

// nestGraphQLDecorators are a resolver's entry points.
var nestGraphQLDecorators = map[string]string{
	"Query": "QUERY", "Mutation": "MUTATION", "Subscription": "SUBSCRIPTION",
	"ResolveField": "FIELD", "ResolveReference": "REFERENCE",
}

// nestScheduleDecorators are the entry points nothing calls: the scheduler
// does. They sit on ordinary providers, so they are the one kind that is not
// gated on what decorates the class.
var nestScheduleDecorators = map[string]string{
	"Cron": "CRON", "Interval": "INTERVAL", "Timeout": "TIMEOUT",
}

// nestRole is what a class is, as far as entry points go. A class can be more
// than one at once — a controller that also handles messages is the normal
// shape of a service that serves both.
type nestRole struct {
	controller bool
	gateway    bool
	resolver   bool
	// prefixes are the class-level route paths, always at least one entry so
	// a method path can be crossed with it unconditionally.
	prefixes []string
}

func nestRoleOf(class *tsDecl) nestRole {
	role := nestRole{prefixes: []string{""}}
	if controller, ok := class.decorator("Controller"); ok {
		role.controller = true
		if prefixes := controller.StringArgs(0); len(prefixes) > 0 {
			role.prefixes = prefixes
		}
	}
	if _, ok := class.decorator("WebSocketGateway"); ok {
		role.gateway = true
	}
	if _, ok := class.decorator("Resolver"); ok {
		role.resolver = true
	}
	return role
}

func (nestFramework) Endpoints(p *tsProject, f *tsFile) []Endpoint {
	var endpoints []Endpoint
	for _, class := range f.decls {
		if class.kind != "class" || class.owner != nil {
			continue
		}
		role := nestRoleOf(class)
		for _, method := range f.decls {
			if method.owner != class {
				continue
			}
			endpoints = append(endpoints, nestEntryPoints(role, class, method)...)
		}
	}
	return endpoints
}

// nestEntryPoints is every entry point one method declares.
func nestEntryPoints(role nestRole, class, method *tsDecl) []Endpoint {
	var endpoints []Endpoint
	add := func(kind, path string) {
		endpoints = append(endpoints, Endpoint{HTTPMethod: kind, Path: path, EntryNode: method.nodeID})
	}

	for _, decorator := range method.decorators {
		switch {
		case role.controller && nestRouteDecorators[decorator.Name] != "":
			// Every class prefix crossed with every method path, which is what
			// an array on either side means.
			paths := decorator.StringArgs(0)
			if len(paths) == 0 {
				paths = []string{""}
			}
			for _, prefix := range role.prefixes {
				for _, path := range paths {
					add(nestRouteDecorators[decorator.Name], tsPathJoin(prefix, path))
				}
			}

		case role.controller && nestMessageDecorators[decorator.Name] != "":
			add(nestMessageDecorators[decorator.Name], nestPatternName(decorator, class, method))

		case role.controller && nestGrpcDecorators[decorator.Name]:
			add("GRPC", nestGrpcName(decorator, class, method))

		case role.gateway && decorator.Name == "SubscribeMessage":
			add("SOCKET", nestPatternName(decorator, class, method))

		case role.resolver && nestGraphQLDecorators[decorator.Name] != "":
			add(nestGraphQLDecorators[decorator.Name], nestGraphQLName(decorator, method))

		case nestScheduleDecorators[decorator.Name] != "":
			// Not gated on the class: a scheduled job lives on a provider.
			add(nestScheduleDecorators[decorator.Name], nestScheduleName(decorator, class, method))
		}
	}
	return endpoints
}

// nestPatternName is what to show beside an entry point that has no URL: the
// pattern it answers to.
//
// A pattern is a string as often as it is an object, and an object is the
// idiom for request/response — `@MessagePattern({ cmd: 'divide' })`. Reading
// only the string form left those showing the handler's own name, which is
// precisely the thing a caller does NOT send.
func nestPatternName(decorator tsDecorator, class, method *tsDecl) string {
	if pattern := decorator.StringArg(0); pattern != "" {
		return pattern
	}
	if len(decorator.Args) > 0 {
		if summary := tsObjectSummary(decorator.Args[0]); summary != "" {
			return summary
		}
	}
	return class.name + "." + method.name
}

// nestGraphQLName is the schema field a resolver serves. Unlike a message
// pattern, a bare `@Query()` is not missing anything: the field name IS the
// handler's name, which is the whole convention.
func nestGraphQLName(decorator tsDecorator, method *tsDecl) string {
	if name := decorator.StringArg(0); name != "" {
		return name
	}
	return method.name
}

// nestGrpcName is the "Service/Method" a gRPC handler answers to.
//
// Both arguments are optional, and Nest fills them the same way this does when
// they are missing: the service is the class name without its Controller
// suffix, and the method is the handler's name capitalised — which is how the
// proto spells it.
func nestGrpcName(decorator tsDecorator, class, method *tsDecl) string {
	service := decorator.StringArg(0)
	if service == "" {
		service = strings.TrimSuffix(class.name, "Controller")
	}
	rpc := decorator.StringArg(1)
	if rpc == "" {
		rpc = nestCapitalise(method.name)
	}
	return service + "/" + rpc
}

func nestCapitalise(name string) string {
	if name == "" {
		return ""
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

// nestScheduleName is the schedule itself where it is written as one, and the
// handler otherwise — `@Interval(10000)` carries a number, not a name.
func nestScheduleName(decorator tsDecorator, class, method *tsDecl) string {
	if schedule := decorator.StringArg(0); schedule != "" {
		return class.name + "." + method.name + " (" + schedule + ")"
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

// nestClientTypes are the injected handles a service talks to another service
// through. Every transport has its own, and they all carry send and emit.
var nestClientTypes = map[string]bool{
	"ClientProxy": true, "ClientKafka": true, "ClientRMQ": true,
	"ClientGrpc": true, "ClientRedis": true, "ClientTCP": true,
	"ClientNats": true, "ClientMqtt": true,
}

// nestPatternKey is the canonical form of a message pattern, used on BOTH
// sides of the match: the pattern a handler declares and the pattern a caller
// sends. Without one canonical form the two never meet.
//
// Object keys are sorted, because Nest matches patterns by their content
// rather than by how they were typed — `{ role, cmd }` and `{ cmd, role }` are
// the same pattern, and a caller and a handler in different files written by
// different people will not agree on the order.
func nestPatternKey(arg string) string {
	if literal := tsStringLiteral(arg); literal != "" {
		return literal
	}
	summary := tsObjectSummary(arg)
	if summary == "" {
		return ""
	}
	parts := strings.Split(summary, ", ")
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

// nestHandlerByPattern indexes every message and event handler in the project
// by the pattern it answers to. Built once; see tsMemo.
func nestHandlerByPattern(p *tsProject) map[string]*tsDecl {
	return tsMemo(p, "nest.patterns", func() map[string]*tsDecl {
		handlers := map[string]*tsDecl{}
		ambiguous := map[string]bool{}
		for _, file := range p.files {
			for _, method := range file.decls {
				if method.owner == nil {
					continue
				}
				for _, decorator := range method.decorators {
					if nestMessageDecorators[decorator.Name] == "" || len(decorator.Args) == 0 {
						continue
					}
					key := nestPatternKey(decorator.Args[0])
					if key == "" {
						continue
					}
					// Two handlers for one pattern is a project that would not
					// start; pointing at either is worse than pointing at none.
					if _, taken := handlers[key]; taken {
						ambiguous[key] = true
						continue
					}
					handlers[key] = method
				}
			}
		}
		for key := range ambiguous {
			delete(handlers, key)
		}
		return handlers
	})
}

// nestClientProperties are the fields of a class that hold a microservice
// client, by the name the class reaches them through.
func nestClientProperties(f *tsFile, class *tsDecl) map[string]bool {
	properties := map[string]bool{}
	for _, member := range f.decls {
		if member.owner != class || member.symbolKind != lspSymbolConstructor {
			continue
		}
		for _, param := range member.params {
			if nestClientTypes[tsBaseType(param.Type)] {
				properties[param.Name] = true
			}
		}
	}
	return properties
}

// nestMessageEdges links a caller to the handler in another service.
//
// This is the edge that makes a microservice graph a map rather than a pile of
// unrelated services. The checker cannot produce it and is right not to:
// `client.send({ cmd: 'divide' })` resolves to a library method, and the thing
// it actually reaches is decided at run time by a broker. What connects them
// is the pattern, written on both sides, and that is exactly what the model
// now carries.
//
// It is a "call" edge on purpose, so a flow walks across the service boundary
// the way a reader does. Only a literal pattern matches — one built at run
// time reaches nothing here, which is the honest answer.
func nestMessageEdges(p *tsProject, f *tsFile) []Edge {
	var edges []Edge
	seen := map[string]bool{}
	handlers := nestHandlerByPattern(p)
	if len(handlers) == 0 {
		return nil
	}

	for _, class := range f.decls {
		if class.kind != "class" || class.owner != nil {
			continue
		}
		properties := nestClientProperties(f, class)
		if len(properties) == 0 {
			continue
		}
		for _, call := range f.calls {
			if call.Method != "send" && call.Method != "emit" {
				continue
			}
			receiver, ok := strings.CutPrefix(call.Receiver, "this.")
			if !ok || !properties[receiver] {
				continue
			}
			if call.Enclosing == nil || call.Enclosing.owner != class {
				continue
			}
			target := handlers[nestPatternKey(call.StringArgOrObject(0))]
			if target == nil {
				continue
			}
			key := call.Enclosing.nodeID + "\x00" + target.nodeID
			if seen[key] {
				continue
			}
			seen[key] = true
			edges = append(edges, Edge{From: call.Enclosing.nodeID, To: target.nodeID, Kind: "call"})
		}
	}
	return edges
}

// Edges turns constructor injection into the graph's "di" edge.
//
// This is the relationship no call expression shows and the one that makes a
// Nest project readable: a controller never calls its service's constructor,
// it declares a dependency and the framework supplies it. Exactly what the
// Java analyzer records for an @Autowired field.
func (nestFramework) Edges(p *tsProject, f *tsFile) []Edge {
	edges := nestMessageEdges(p, f)
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
