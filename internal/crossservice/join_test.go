package crossservice

import (
	"strings"
	"testing"

	"devkit/internal/codegraph"
	"devkit/internal/workspace"
)

func endpoint(id, verb, target string) codegraph.SnapshotEndpoint {
	return codegraph.SnapshotEndpoint{
		EntryPoint: codegraph.EntryPoint{ID: id, Verb: verb, Target: target, EntryNode: "node:" + id, Kind: codegraph.EntryKindHTTP},
		Label:      "Ctl." + id, File: "Ctl.java", Line: 10,
	}
}

func call(from, client, verb, serviceName, url string, line int) codegraph.SnapshotCall {
	return codegraph.SnapshotCall{
		OutboundCall: codegraph.OutboundCall{From: from, File: "Caller.java", Line: line, Client: client, HTTPMethod: verb, ServiceName: serviceName, URL: url},
		Label:        from,
	}
}

func svc(name string, port string, endpoints []codegraph.SnapshotEndpoint, calls ...codegraph.SnapshotCall) Service {
	cfg := workspace.SpringConfig{Props: map[string]string{}}
	if port != "" {
		cfg.Props["server.port"] = port
	}
	return Service{
		Name: name, Path: "/ws/" + name, Config: cfg,
		Snapshot: codegraph.ServiceSnapshot{Path: "/ws/" + name, State: codegraph.SnapshotIndexed, IndexedAt: 1700000000000, Endpoints: endpoints, Outbound: calls},
	}
}

func byLine(edges []Edge, service string) map[int]Edge {
	out := map[int]Edge{}
	for _, e := range edges {
		if e.Caller.Service == service {
			out[e.Caller.Line] = e
		}
	}
	return out
}

// access is the service in the feedback's example: a Kong plugin and two Java
// services call it, in the three ways the join can know where a call goes.
func workspaceOfFour() []Service {
	access := svc("access-service", "8081", []codegraph.SnapshotEndpoint{
		endpoint("get-access", "GET", "/v1/internal/api-access/{org}/api/{api}"),
		endpoint("enable", "POST", "/v1/internal/api-access/{org}/enable"),
		endpoint("health", "GET", "/health"),
	})
	plugin := svc("fsap-api-access", "", nil,
		call("plugin.access", codegraph.OutboundRestyHTTP, "GET", "", "{conf.access_url}/v1/internal/api-access/{org}/api/{api}", 9))
	billing := svc("billing", "8082", nil,
		call("Billing.enable", codegraph.OutboundFeign, "POST", "access-service", "/v1/internal/api-access/{org}/enable", 20),
		call("Billing.iface", codegraph.OutboundHTTPInterface, "GET", "", "/v1/internal/api-access/{org}/api/{api}", 30),
		call("Billing.stripe", codegraph.OutboundRestTemplate, "POST", "", "https://api.stripe.com/v1/charges", 40),
		call("Billing.reports", codegraph.OutboundFeign, "GET", "reports-service", "/reports/{id}", 50))
	orders := svc("orders", "8083", nil,
		call("Orders.gone", codegraph.OutboundRestTemplate, "GET", "", "http://access-service:8081/v1/internal/api-access/{org}/removed", 10),
		call("Orders.dev", codegraph.OutboundRestTemplate, "GET", "", "http://localhost:8081/health", 11),
		call("Orders.dynamic", codegraph.OutboundRestTemplate, "GET", "", "{base}/health", 12),
		call("Orders.unknown", codegraph.OutboundRestTemplate, "GET", "", "http://inventory-service/items", 13),
		call("Orders.byhost", codegraph.OutboundRestTemplate, "GET", "", "http://access-service/v1/internal/api-access/{org}/api/{api}?x=1", 14))
	reports := Service{Name: "reports-service", Path: "/ws/reports-service", Snapshot: codegraph.ServiceSnapshot{State: codegraph.SnapshotNotIndexed}}
	return []Service{access, plugin, billing, orders, reports}
}

func TestJoinLinksACallByTheHostItNames(t *testing.T) {
	edges := byLine(Join(workspaceOfFour()), "orders")

	linked := edges[14]
	if linked.Status != StatusLinked || linked.Callee == nil || linked.Callee.EntryPointID != "get-access" {
		t.Fatalf("by host: %+v", linked)
	}
	if linked.Provenance != workspace.ProvenanceLiteral {
		t.Fatalf("provenance = %q: the host was written in the code", linked.Provenance)
	}
	if linked.CalleeIndexedAt == 0 || linked.CalleeState != codegraph.SnapshotIndexed {
		t.Fatalf("the edge must say how fresh the other end is: %+v", linked)
	}
	// The same service, a route it does not serve.
	if gone := edges[10]; gone.Status != StatusEndpointGone || gone.Callee == nil || gone.Callee.Service != "access-service" {
		t.Fatalf("gone: %+v", gone)
	}
	// A loopback host is matched by port: a dev setup, said so.
	if dev := edges[11]; dev.Status != StatusLinked || dev.Provenance != workspace.ProvenancePort || dev.Callee.EntryPointID != "health" {
		t.Fatalf("dev: %+v", dev)
	}
	// Nothing says where, and /health is every service's.
	if dyn := edges[12]; dyn.Status != StatusDynamicURL {
		t.Fatalf("dynamic: %+v", dyn)
	}
	if unknown := edges[13]; unknown.Status != StatusUnknownService {
		t.Fatalf("unknown: %+v", unknown)
	}
}

func TestJoinLinksByFeignNameAndExplainsWhatItCannotRead(t *testing.T) {
	edges := byLine(Join(workspaceOfFour()), "billing")
	if feign := edges[20]; feign.Status != StatusLinked || feign.Provenance != workspace.ProvenanceFeignName || feign.Callee.EntryPointID != "enable" {
		t.Fatalf("feign: %+v", feign)
	}
	if stripe := edges[40]; stripe.Status != StatusExternal {
		t.Fatalf("stripe: %+v", stripe)
	}
	// reports-service is in the workspace and has no index: said, not skipped.
	notIndexed := edges[50]
	if notIndexed.Status != StatusNotIndexed || notIndexed.Callee == nil || notIndexed.Callee.Service != "reports-service" || notIndexed.CalleeState != codegraph.SnapshotNotIndexed {
		t.Fatalf("not indexed: %+v", notIndexed)
	}
}

// The Kong plugin has no host the analyzer can fold — its base URL is plugin
// config — but a route this specific is one service's.
func TestJoinLinksByARouteSpecificEnoughToBeOneService(t *testing.T) {
	edges := Join(workspaceOfFour())
	plugin := byLine(edges, "fsap-api-access")[9]
	if plugin.Status != StatusLinked || plugin.Provenance != ProvenancePathUnique || plugin.Callee.Service != "access-service" || plugin.Callee.EntryPointID != "get-access" {
		t.Fatalf("plugin: %+v", plugin)
	}
	iface := byLine(edges, "billing")[30]
	if iface.Status != StatusLinked || iface.Provenance != ProvenancePathUnique || iface.Callee.EntryPointID != "get-access" {
		t.Fatalf("http interface: %+v", iface)
	}
}

func TestJoinSaysAmbiguousWhenTwoServicesServeTheRoute(t *testing.T) {
	a := svc("a-service", "8081", []codegraph.SnapshotEndpoint{endpoint("a", "GET", "/v1/shared/thing/{id}")})
	b := svc("b-service", "8082", []codegraph.SnapshotEndpoint{endpoint("b", "GET", "/v1/shared/thing/{id}")})
	c := svc("caller", "", nil, call("C.go", codegraph.OutboundRestyHTTP, "GET", "", "{conf.url}/v1/shared/thing/{id}", 1))
	edge := Join([]Service{a, b, c})[0]
	if edge.Status != StatusAmbiguousPath || len(edge.Candidates) != 2 {
		t.Fatalf("edge = %+v", edge)
	}
	if edge.Callee != nil {
		t.Fatal("an ambiguous edge must not pick one")
	}
}

func TestJoinNeverLinksAGenericRouteByItself(t *testing.T) {
	a := svc("a-service", "8081", []codegraph.SnapshotEndpoint{endpoint("h", "GET", "/health"), endpoint("x", "GET", "/{id}")})
	c := svc("caller", "", nil,
		call("C.health", codegraph.OutboundRestyHTTP, "GET", "", "{conf.url}/health", 1),
		call("C.root", codegraph.OutboundRestyHTTP, "GET", "", "{conf.url}/{id}", 2))
	for _, e := range Join([]Service{a, c}) {
		if e.Status != StatusDynamicURL {
			t.Fatalf("%s: %+v", e.Caller.Label, e)
		}
	}
}

func TestJoinMatchesTheMethodAndTheMostSpecificRoute(t *testing.T) {
	a := svc("a-service", "8081", []codegraph.SnapshotEndpoint{
		endpoint("by-id", "GET", "/v1/users/{id}"),
		endpoint("me", "GET", "/v1/users/me"),
		endpoint("create", "POST", "/v1/users/{id}"),
	})
	c := svc("caller", "", nil,
		call("C.me", codegraph.OutboundRestTemplate, "GET", "", "http://a-service:8081/v1/users/me", 1),
		call("C.post", codegraph.OutboundRestTemplate, "POST", "", "http://a-service:8081/v1/users/{id}", 2),
		call("C.any", codegraph.OutboundRestTemplate, "ANY", "", "http://a-service:8081/v1/users/me", 3))
	edges := byLine(Join([]Service{a, c}), "caller")
	if edges[1].Callee.EntryPointID != "me" || len(edges[1].Matches) != 0 {
		t.Fatalf("the literal route beats the variable one: %+v", edges[1])
	}
	if edges[2].Callee.EntryPointID != "create" {
		t.Fatalf("post: %+v", edges[2])
	}
	// An unknown method matches either route that is equally specific.
	if edges[3].Callee.EntryPointID != "me" {
		t.Fatalf("any: %+v", edges[3])
	}
}

func TestJoinPrefixesRoutesWithTheServiceContextPath(t *testing.T) {
	a := svc("a-service", "8081", []codegraph.SnapshotEndpoint{endpoint("x", "GET", "/v1/items/{id}")})
	a.Snapshot.ContextPath = "/inventory"
	c := svc("caller", "", nil,
		call("C.ok", codegraph.OutboundRestTemplate, "GET", "", "http://a-service:8081/inventory/v1/items/{id}", 1),
		call("C.without", codegraph.OutboundRestTemplate, "GET", "", "http://a-service:8081/v1/items/{id}", 2))
	edges := byLine(Join([]Service{a, c}), "caller")
	if edges[1].Status != StatusLinked {
		t.Fatalf("with the context path: %+v", edges[1])
	}
	if edges[2].Status != StatusEndpointGone {
		t.Fatalf("without it the route is not there: %+v", edges[2])
	}
}

// Placeholders fold against the CALLER's config.
func TestJoinFoldsAPlaceholderAgainstTheCallersConfig(t *testing.T) {
	a := svc("access-service", "8081", []codegraph.SnapshotEndpoint{endpoint("x", "GET", "/v1/x")})
	c := svc("caller", "", nil, call("C.go", codegraph.OutboundRestTemplate, "GET", "", "${clients.access.url}/v1/x", 1))
	c.Config.Props["clients.access.url"] = "http://access-service:8081"
	edge := Join([]Service{a, c})[0]
	if edge.Status != StatusLinked || edge.Provenance != workspace.ProvenanceProperty {
		t.Fatalf("edge = %+v", edge)
	}
}

func TestJoinNeverLinksAServiceToItself(t *testing.T) {
	a := svc("a-service", "8081", []codegraph.SnapshotEndpoint{endpoint("x", "GET", "/v1/internal/self/{id}")},
		call("A.go", codegraph.OutboundRestyHTTP, "GET", "", "{conf.url}/v1/internal/self/{id}", 1))
	edge := Join([]Service{a})[0]
	if edge.Status == StatusLinked {
		t.Fatalf("a route-only match must look at the OTHER services: %+v", edge)
	}
}

func TestRunReportsWhatACallsAndWhatCallsIt(t *testing.T) {
	services := workspaceOfFour()
	access := "/ws/access-service"
	report, err := Run(services, Query{Service: access})
	if err != nil {
		t.Fatal(err)
	}
	if report.Service.Name != "access-service" || report.OutboundTotal != 0 {
		t.Fatalf("service = %+v, outbound %d", report.Service, report.OutboundTotal)
	}
	// Who calls it: the plugin and billing twice by route/name, orders by host and by port, and one endpoint-gone.
	callers := map[string]int{}
	for _, e := range report.Inbound {
		callers[e.Caller.Service]++
	}
	if callers["fsap-api-access"] != 1 || callers["billing"] != 2 || callers["orders"] != 3 || report.InboundTotal != 6 {
		t.Fatalf("inbound = %v (total %d)", callers, report.InboundTotal)
	}
	if report.Statuses[StatusLinked] != 5 || report.Statuses[StatusEndpointGone] != 1 {
		t.Fatalf("statuses = %v", report.Statuses)
	}

	// One route: who calls /v1/internal/api-access/{org}/api/{api}.
	one, err := Run(services, Query{Service: access, Direction: Inbound, EndpointID: "get-access"})
	if err != nil {
		t.Fatal(err)
	}
	if one.InboundTotal != 3 {
		t.Fatalf("callers of get-access = %d: %+v", one.InboundTotal, one.Inbound)
	}
	for _, e := range one.Inbound {
		if e.Status != StatusLinked {
			t.Fatalf("a route-specific question lists only linked edges: %+v", e)
		}
	}
	if len(one.Outbound) != 0 {
		t.Fatal("inbound only was asked")
	}

	// What billing calls, with the service that was not readable named.
	billing, err := Run(services, Query{Service: "/ws/billing", Direction: Outbound})
	if err != nil {
		t.Fatal(err)
	}
	if billing.OutboundTotal != 4 || len(billing.Inbound) != 0 {
		t.Fatalf("billing outbound = %d", billing.OutboundTotal)
	}
	said := false
	for _, n := range billing.Notes {
		if strings.Contains(n, "reports-service") {
			said = true
		}
	}
	if !said {
		t.Fatalf("a service the join could not read must be named: %v", billing.Notes)
	}
	if len(billing.Services) != 5 {
		t.Fatalf("services = %+v", billing.Services)
	}
}

func TestRunLimitsAndRejectsBadInput(t *testing.T) {
	services := workspaceOfFour()
	report, err := Run(services, Query{Service: "/ws/access-service", Direction: Inbound, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Inbound) != 2 || report.InboundTotal != 6 {
		t.Fatalf("listed %d of %d", len(report.Inbound), report.InboundTotal)
	}
	for name, q := range map[string]Query{
		"direction": {Service: "/ws/access-service", Direction: "sideways"},
		"limit":     {Service: "/ws/access-service", Limit: -1},
		"service":   {Service: "/ws/nowhere"},
	} {
		if _, err := Run(services, q); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}
