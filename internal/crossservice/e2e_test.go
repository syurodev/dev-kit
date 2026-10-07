package crossservice

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"devkit/internal/codegraph"
	"devkit/internal/workspace"
)

// composite hands a Java folder to the real jar and a Lua folder to the Lua
// analyzer, the way the Registry's per-language choice does in the app.
type composite struct{ java codegraph.Analyzer }

func (c composite) Analyze(ctx context.Context, path string) (codegraph.AnalysisResult, error) {
	if _, err := os.Stat(filepath.Join(path, "src")); err == nil {
		return c.java.Analyze(ctx, path)
	}
	return codegraph.NewLuaAnalyzer().Analyze(ctx, path)
}

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// The feedback's own example, end to end through the real analyzers: a Kong
// plugin (Lua) and a Java service call the access service, and the join finds
// both without any of them naming the other's host the same way.
func TestRealAnalyzersFeedTheJoin(t *testing.T) {
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, "analyzer", "target", "codegraph-analyzer.jar")); err != nil {
		t.Skip("analyzer not built (run `cd analyzer && mvn package` first)")
	}
	ctx := t.Context()
	ws := t.TempDir()
	access := filepath.Join(ws, "access-service")
	orders := filepath.Join(ws, "orders-service")
	plugin := filepath.Join(ws, "kong-plugin")

	writeTree(t, access, map[string]string{
		"src/main/resources/application.yml": "server:\n  port: 8081\n",
		"src/main/java/acme/AccessController.java": `package acme;
import org.springframework.web.bind.annotation.*;
@RestController
@RequestMapping("/v1/internal/api-access")
class AccessController {
    @GetMapping("/{org}/api/{api}") boolean get(@PathVariable String org, @PathVariable String api) { return true; }
    @PostMapping("/{org}/enable") void enable(@PathVariable String org) {}
}
`,
		"src/main/java/acme/AccessCache.java": `package acme;
import org.springframework.data.redis.core.StringRedisTemplate;
class AccessCache {
    static final String PREFIX = "fsap:access:";
    private final StringRedisTemplate redis = null;
    void put(String org, String api) { redis.opsForValue().set(PREFIX + org + ":" + api, "1"); }
}
`,
	})
	writeTree(t, orders, map[string]string{
		"src/main/java/acme/AccessClient.java": `package acme;
import org.springframework.web.service.annotation.*;
@HttpExchange("/v1/internal/api-access")
interface AccessClient {
    @GetExchange("/{org}/api/{api}") boolean get(String org, String api);
}
`,
		"src/main/java/acme/EnableService.java": `package acme;
import org.springframework.web.client.RestTemplate;
class EnableService {
    private final RestTemplate rest = new RestTemplate();
    void enable(String org) { rest.postForObject("http://access-service:8081/v1/internal/api-access/" + org + "/enable", null, Void.class); }
    void ping() { rest.getForObject("http://access-service:8081/v1/internal/api-access/ping", String.class); }
}
`,
	})
	writeTree(t, plugin, map[string]string{
		"kong/plugins/fsap-api-access/handler.lua": `local http = require "resty.http"
local redis = require "resty.redis"
local plugin = { PRIORITY = 900 }
function plugin:access(conf)
  local org = kong.request.get_header("x-org")
  local api = kong.router.get_service().name
  local red = redis:new()
  local cached = red:get("fsap:access:" .. org .. ":" .. api)
  local res = http.new():request_uri(conf.access_url .. "/v1/internal/api-access/" .. org .. "/api/" .. api, { method = "GET" })
end
return plugin
`,
	})

	focus := codegraph.NewService(codegraph.NewNilStore(), nil)
	focus.SetConfigDir(t.TempDir())
	registry := codegraph.NewRegistry(focus, 8)
	registry.SetAnalyzer(composite{java: codegraph.JavaToolchain(repo).Analyzer()})
	t.Cleanup(func() { _ = registry.Shutdown() })

	var services []Service
	for _, s := range []struct{ name, path string }{{"access-service", access}, {"orders-service", orders}, {"fsap-api-access", plugin}} {
		svc, err := registry.Open(ctx, s.path)
		if err != nil {
			t.Fatalf("open %s: %v", s.name, err)
		}
		if _, err := svc.WaitForIndexReady(ctx); err != nil {
			t.Fatalf("index %s: %v", s.name, err)
		}
		snapshot, err := registry.Snapshot(ctx, s.path)
		if err != nil {
			t.Fatal(err)
		}
		config, _ := workspace.ReadSpringConfig(s.path)
		services = append(services, Service{Name: s.name, Path: s.path, Config: config, Snapshot: snapshot})
	}

	report, err := Run(services, Query{Service: access, Direction: Inbound})
	if err != nil {
		t.Fatal(err)
	}
	type key struct{ caller, route string }
	got := map[key]Edge{}
	for _, e := range report.Inbound {
		if e.Callee != nil {
			got[key{e.Caller.Service, e.HTTPMethod + " " + e.Callee.Target}] = e
		}
	}
	for _, want := range []struct {
		caller, route, provenance string
	}{
		{"fsap-api-access", "GET /v1/internal/api-access/{org}/api/{api}", ProvenancePathUnique},
		{"orders-service", "GET /v1/internal/api-access/{org}/api/{api}", ProvenancePathUnique}, // the HTTP interface: no host
		{"orders-service", "POST /v1/internal/api-access/{org}/enable", workspace.ProvenanceLiteral},
	} {
		e, ok := got[key{want.caller, want.route}]
		if !ok || e.Status != StatusLinked || e.Provenance != want.provenance {
			t.Errorf("missing %s %s (%s): got %+v", want.caller, want.route, want.provenance, got)
		}
	}
	// A route the service does not have — /ping is not in AccessController.
	gone := 0
	for _, e := range report.Inbound {
		if e.Status == StatusEndpointGone {
			gone++
		}
	}
	if gone != 1 {
		t.Errorf("endpoint-gone edges = %d, want the one ping call: %+v", gone, report.Inbound)
	}

	// The same two services share a Redis key: Java writes it, the plugin reads it.
	keys, err := RunRedis(services, RedisQuery{Key: "fsap:access:*"})
	if err != nil {
		t.Fatal(err)
	}
	if keys.KeyTotal != 1 {
		t.Fatalf("keys = %+v", keys.Keys)
	}
	shared := keys.Keys[0]
	if !shared.CrossService || len(shared.Writers) != 1 || len(shared.Readers) != 1 ||
		shared.Writers[0].Service != "access-service" || shared.Readers[0].Service != "fsap-api-access" {
		t.Fatalf("key = %+v", shared)
	}
	if shared.Writers[0].Key != "fsap:access:{org}:{api}" || shared.Readers[0].Key != "fsap:access:{org}:{api}" {
		t.Fatalf("keys as written: %q / %q", shared.Writers[0].Key, shared.Readers[0].Key)
	}
}
