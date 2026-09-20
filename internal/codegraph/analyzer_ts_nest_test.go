// internal/codegraph/analyzer_ts_nest_test.go
package codegraph

import (
	"sort"
	"strings"
	"testing"
)

// nestMicroserviceFixture is a Nest service that is not just an HTTP API: the
// microservice handlers, gRPC, a websocket gateway, a GraphQL resolver, a
// scheduled job, and a controller whose paths are arrays.
var nestMicroserviceFixture = map[string]string{
	"tsconfig.json": tsconfig(),
	"package.json": `{"name":"svc","dependencies":{
	  "@nestjs/common":"^10.0.0","@nestjs/core":"^10.0.0",
	  "@nestjs/microservices":"^10.0.0","@nestjs/websockets":"^10.0.0",
	  "@nestjs/schedule":"^4.0.0","@nestjs/graphql":"^12.0.0"}}`,
	"src/math/math.controller.ts": `
import { Controller } from '@nestjs/common';
import { MessagePattern, EventPattern } from '@nestjs/microservices';

@Controller()
export class MathController {
  @MessagePattern('sum')
  accumulate(data: number[]): number { return data.reduce((a, b) => a + b); }

  @MessagePattern({ cmd: 'divide' })
  divide(data: number[]): number { return data[0] / data[1]; }

  @EventPattern('user.created')
  handleUserCreated(data: Record<string, unknown>) {}
}
`,
	"src/heroes/heroes.controller.ts": `
import { Controller } from '@nestjs/common';
import { GrpcMethod, GrpcStreamMethod } from '@nestjs/microservices';

@Controller()
export class HeroesController {
  @GrpcMethod('HeroesService', 'FindOne')
  findOne(data: { id: number }) { return { id: data.id }; }

  @GrpcStreamMethod()
  findMany(data: unknown) { return data; }
}
`,
	"src/jobs/cleanup.service.ts": `
import { Injectable } from '@nestjs/common';
import { Cron, Interval } from '@nestjs/schedule';

@Injectable()
export class CleanupService {
  @Cron('45 * * * * *')
  purge() {}

  @Interval(10000)
  heartbeat() {}
}
`,
	"src/chat/chat.gateway.ts": `
import { WebSocketGateway, SubscribeMessage } from '@nestjs/websockets';

@WebSocketGateway(8080, { namespace: 'chat' })
export class ChatGateway {
  @SubscribeMessage('events')
  handleEvent(data: string): string { return data; }
}
`,
	"src/users/users.resolver.ts": `
import { Resolver, Query, Mutation } from '@nestjs/graphql';

@Resolver('User')
export class UsersResolver {
  @Query()
  users() { return []; }

  @Mutation('createUser')
  createUser(name: string) { return { name }; }
}
`,
	"src/misc/misc.controller.ts": `
import { Controller, Get } from '@nestjs/common';

@Controller(['legacy', 'v2'])
export class MiscController {
  @Get(['a', 'b'])
  both() { return 1; }
}
`,
}

// tsRoutes is every endpoint as "METHOD path", for comparing whole sets.
func tsRoutes(result AnalysisResult) []string {
	var routes []string
	for _, endpoint := range result.Endpoints {
		routes = append(routes, endpoint.HTTPMethod+" "+endpoint.Path)
	}
	sort.Strings(routes)
	return routes
}

// Nest's microservice handlers are entry points with no URL, the same way a
// Kong plugin's phases are. One handler per pattern, whatever transport
// carries it — TCP, Redis, NATS, Kafka, RabbitMQ and MQTT all use these two
// decorators, so covering them covers most of Nest's microservice surface.
func TestNestMicroserviceHandlersAreEntryPoints(t *testing.T) {
	result := analyzeTS(t, nestMicroserviceFixture)
	routes := strings.Join(tsRoutes(result), "\n")

	for _, want := range []string{
		"MESSAGE sum",
		"EVENT user.created",
		// An object pattern is the idiom for request/response, and the handler's
		// own name is precisely what a caller does NOT send.
		"MESSAGE cmd: divide",
		"SOCKET events",
	} {
		if !strings.Contains(routes, want) {
			t.Errorf("missing %q; got:\n%s", want, routes)
		}
	}
}

func TestNestGrpcHandlersAreEntryPoints(t *testing.T) {
	result := analyzeTS(t, nestMicroserviceFixture)
	routes := strings.Join(tsRoutes(result), "\n")

	if !strings.Contains(routes, "GRPC HeroesService/FindOne") {
		t.Errorf("missing the named gRPC method; got:\n%s", routes)
	}
	// Both arguments are optional, and Nest fills them in the same way: the
	// class without its Controller suffix, and the handler capitalised.
	if !strings.Contains(routes, "GRPC Heroes/FindMany") {
		t.Errorf("a bare @GrpcStreamMethod should default its names; got:\n%s", routes)
	}
}

// A scheduled job is an entry point nothing calls — the scheduler does — and
// it lives on a provider, never a controller. An earlier version listed the
// decorators and then gated them out on the class, so they never fired.
func TestNestScheduledJobsAreEntryPointsOnAProvider(t *testing.T) {
	result := analyzeTS(t, nestMicroserviceFixture)
	routes := strings.Join(tsRoutes(result), "\n")

	if !strings.Contains(routes, "CRON CleanupService.purge (45 * * * * *)") {
		t.Errorf("missing the cron job with its schedule; got:\n%s", routes)
	}
	if !strings.Contains(routes, "INTERVAL CleanupService.heartbeat") {
		t.Errorf("missing the interval job; got:\n%s", routes)
	}
}

func TestNestResolverFieldsAreEntryPoints(t *testing.T) {
	result := analyzeTS(t, nestMicroserviceFixture)
	routes := strings.Join(tsRoutes(result), "\n")

	// A bare @Query() is not missing a name: the schema field IS the handler's
	// name, unlike a message pattern.
	if !strings.Contains(routes, "QUERY users") {
		t.Errorf("missing the query; got:\n%s", routes)
	}
	if !strings.Contains(routes, "MUTATION createUser") {
		t.Errorf("missing the mutation; got:\n%s", routes)
	}
}

// An array on either side of the route is how one handler serves several
// paths. Reading only the first — or, as an earlier version did, none, and
// reporting a bare "/" — invents a route the service does not serve.
func TestNestArrayPathsCrossIntoEveryRoute(t *testing.T) {
	result := analyzeTS(t, nestMicroserviceFixture)
	routes := strings.Join(tsRoutes(result), "\n")

	for _, want := range []string{"GET /legacy/a", "GET /legacy/b", "GET /v2/a", "GET /v2/b"} {
		if !strings.Contains(routes, want) {
			t.Errorf("missing %q; got:\n%s", want, routes)
		}
	}
	if strings.Contains(routes, "GET /\n") || strings.HasSuffix(routes, "GET /") {
		t.Errorf("a route with no path at all was reported; got:\n%s", routes)
	}
}

// A controller that serves HTTP and messages at once is the normal shape of a
// service that does both, and neither kind may swallow the other.
func TestNestHybridControllerReportsBothKinds(t *testing.T) {
	result := analyzeTS(t, map[string]string{
		"tsconfig.json": tsconfig(),
		"package.json":  `{"name":"svc","dependencies":{"@nestjs/common":"^10.0.0","@nestjs/microservices":"^10.0.0"}}`,
		"src/orders/orders.controller.ts": `
import { Controller, Get } from '@nestjs/common';
import { MessagePattern } from '@nestjs/microservices';

@Controller('orders')
export class OrdersController {
  @Get(':id')
  find(id: string) { return id; }

  @MessagePattern('order.settle')
  settle(id: string) { return id; }
}
`,
	})

	routes := strings.Join(tsRoutes(result), "\n")
	if !strings.Contains(routes, "GET /orders/:id") || !strings.Contains(routes, "MESSAGE order.settle") {
		t.Errorf("expected both an HTTP route and a message handler; got:\n%s", routes)
	}
}

// The gate works in the other direction too: a decorator that happens to share
// a framework's name is not an entry point on a class that is not one.
func TestNestDecoratorsOnlyCountOnTheRightKindOfClass(t *testing.T) {
	result := analyzeTS(t, map[string]string{
		"tsconfig.json": tsconfig(),
		"package.json":  `{"name":"svc","dependencies":{"@nestjs/common":"^10.0.0"}}`,
		"src/util/helper.service.ts": `
import { Injectable } from '@nestjs/common';

function Get(): MethodDecorator { return () => {}; }

@Injectable()
export class HelperService {
  @Get()
  notARoute() { return 1; }
}
`,
	})

	if len(result.Endpoints) != 0 {
		t.Errorf("a provider declares no routes, got %v", tsRoutes(result))
	}
}
