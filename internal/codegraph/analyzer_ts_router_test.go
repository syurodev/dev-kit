// internal/codegraph/analyzer_ts_router_test.go
package codegraph

import (
	"strings"
	"testing"
)

// Routes registered by calling a method are the third way a TypeScript project
// declares an entry point, and the reason the model carries call expressions.
func TestExpressRoutesAreEntryPoints(t *testing.T) {
	result := analyzeTS(t, map[string]string{
		"tsconfig.json": tsconfig(),
		"package.json":  `{"name":"api","dependencies":{"express":"^4.19.0"}}`,
		"src/handlers.ts": `
export async function listUsers() {
  return [];
}
`,
		"src/server.ts": `
import express from 'express';
import { listUsers } from './handlers';

const app = express();

app.get('/health', (req, res) => {
  res.json({ ok: true });
});

app.post('/users', async (req, res) => {
  res.json(await listUsers());
});

app.get('/users/:id', listUsers);

export function registerAdmin(router: express.Router) {
  router.delete('/admin/:id', (req, res) => res.sendStatus(204));
}

export default app;
`,
	})

	byID := map[string]Node{}
	for _, node := range result.Nodes {
		byID[node.ID] = node
	}
	entry := map[string]Node{}
	for _, endpoint := range result.EntryPoints {
		entry[endpoint.Verb+" "+endpoint.Target] = byID[endpoint.EntryNode]
	}

	for _, want := range []string{"GET /health", "POST /users", "GET /users/:id", "DELETE /admin/:id"} {
		if _, found := entry[want]; !found {
			t.Errorf("missing %q; got %v", want, tsRoutes(result))
		}
	}

	// An inline callback is still a declaration — the language server reports
	// it — so a flow starts at the handler rather than at the file.
	if node := entry["GET /health"]; !strings.Contains(node.Label, "callback") {
		t.Errorf("GET /health should start at its inline handler, got %q", node.Label)
	}
	// A named handler resolves to the function it names, in another file.
	if node := entry["GET /users/:id"]; node.FilePath != "src/handlers.ts" {
		t.Errorf("GET /users/:id should start at the named handler, got %+v", node)
	}
	// A route registered inside a function still belongs to its own callback.
	if node := entry["DELETE /admin/:id"]; node.FilePath != "src/server.ts" {
		t.Errorf("DELETE /admin/:id = %+v", node)
	}
}

// Every one of these names is a method on something else too. The path is what
// tells a route from a Map lookup, and it is the only thing that does.
func TestARouterMethodNameAloneIsNotARoute(t *testing.T) {
	result := analyzeTS(t, map[string]string{
		"tsconfig.json": tsconfig(),
		"package.json":  `{"name":"api","dependencies":{"express":"^4.19.0"}}`,
		"src/cache.ts": `
const store = new Map<string, number>();

export function read(key: string) {
  return store.get(key);
}

export function readPath() {
  return store.get('not-a-path');
}
`,
	})

	if len(result.EntryPoints) != 0 {
		t.Errorf("a Map lookup is not a route, got %v", tsRoutes(result))
	}
}

func TestFastifyAndHonoUseTheSameReading(t *testing.T) {
	for name, fixture := range map[string]map[string]string{
		"fastify": {
			"tsconfig.json": tsconfig(),
			"package.json":  `{"name":"api","dependencies":{"fastify":"^4.26.0"}}`,
			"src/app.ts": `
import Fastify from 'fastify';
const server = Fastify();
server.get('/ping', async () => ({ pong: true }));
export default server;
`,
		},
		"hono": {
			"tsconfig.json": tsconfig(),
			"package.json":  `{"name":"api","dependencies":{"hono":"^4.0.0"}}`,
			"src/app.ts": `
import { Hono } from 'hono';
const app = new Hono();
app.post('/submit', (c) => c.json({ ok: true }));
export default app;
`,
		},
	} {
		result := analyzeTS(t, fixture)
		routes := strings.Join(tsRoutes(result), ",")
		if routes == "" {
			t.Errorf("%s: no routes found", name)
		}
	}
}
