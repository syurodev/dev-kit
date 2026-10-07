package mcp

import (
	"context"
	"strings"
	"testing"

	"devkit/internal/codegraph"
	"devkit/internal/crossservice"
	"devkit/internal/identity"
)

func servicesServer(snapshots map[string]codegraph.ServiceSnapshot) *Server {
	d := agentHubTestDeps(&fakeCaller{}, &fakeAgentHub{})
	d.Workspace = workspaceFixture()
	d.CodeGraphServices = &fakeCodeGraphServices{snapshots: snapshots}
	return NewServer(d)
}

func snapshotWith(path string, endpoints []codegraph.SnapshotEndpoint, calls ...codegraph.SnapshotCall) codegraph.ServiceSnapshot {
	return codegraph.ServiceSnapshot{Path: path, State: codegraph.SnapshotIndexed, IndexedAt: 1700000000000, Endpoints: endpoints, Outbound: calls}
}

// The Kong plugin calls the authentication service; asked from the service's
// side, the plugin is its caller.
func TestFindServiceCallsAnswersAcrossTheWorkspace(t *testing.T) {
	verify := codegraph.SnapshotEndpoint{
		EntryPoint: codegraph.EntryPoint{ID: "GET /v1/internal/verify/{token}", Verb: "GET", Target: "/v1/internal/verify/{token}", EntryNode: "auth.verify", Kind: codegraph.EntryKindHTTP},
		Label:      "AuthController.verify",
	}
	plugin := codegraph.SnapshotCall{
		OutboundCall: codegraph.OutboundCall{From: "kong.access", File: "handler.lua", Line: 12, Client: codegraph.OutboundRestyHTTP, HTTPMethod: "GET", URL: "{conf.auth_url}/v1/internal/verify/{token}"},
		Label:        "kong.plugins.x.handler.access",
	}
	srv := servicesServer(map[string]codegraph.ServiceSnapshot{
		auth: snapshotWith(auth, []codegraph.SnapshotEndpoint{verify}),
		kong: snapshotWith(kong, nil, plugin),
	})
	ctx := identity.WithCaller(context.Background(), identity.UserTerminal())

	_, v, err := srv.toolCodeGraphFindServiceCalls(ctx, nil, codeGraphFindServiceCallsInput{ServicePath: auth, Direction: "inbound"})
	if err != nil {
		t.Fatal(err)
	}
	report, ok := unwrap(v).(crossservice.Report)
	if !ok || report.InboundTotal != 1 {
		t.Fatalf("report = %#v", v)
	}
	edge := report.Inbound[0]
	if edge.Status != crossservice.StatusLinked || edge.Caller.Service != "kong-plugin" || edge.Callee.Target != "/v1/internal/verify/{token}" {
		t.Fatalf("edge = %+v", edge)
	}
	if edge.Provenance != crossservice.ProvenancePathUnique {
		t.Fatalf("provenance = %q", edge.Provenance)
	}
	// The workspace's other services are listed, so a reader sees what was read.
	if len(report.Services) != 3 {
		t.Fatalf("services = %+v", report.Services)
	}
	notIndexed := ""
	for _, n := range report.Notes {
		if strings.Contains(n, "fsap-config-service") {
			notIndexed = n
		}
	}
	if notIndexed == "" {
		t.Fatalf("an unindexed service must be named: %v", report.Notes)
	}

	// Asked from the plugin's side: what it calls.
	_, v, err = srv.toolCodeGraphFindServiceCalls(ctx, nil, codeGraphFindServiceCallsInput{ServicePath: kong + "/kong/plugins/x", Direction: "outbound"})
	if err != nil {
		t.Fatal(err)
	}
	out := unwrap(v).(crossservice.Report)
	if out.OutboundTotal != 1 || out.Outbound[0].Callee.Service != "fsap-authentication-service" {
		t.Fatalf("outbound = %+v", out.Outbound)
	}
}

func TestFindServiceCallsNeedsAServiceAndAWorkspace(t *testing.T) {
	srv := servicesServer(nil)
	ctx := identity.WithCaller(context.Background(), identity.UserTerminal())
	if _, _, err := srv.toolCodeGraphFindServiceCalls(ctx, nil, codeGraphFindServiceCallsInput{}); err == nil || !strings.Contains(err.Error(), "servicePath") {
		t.Fatalf("a terminal has no working directory to take the service from: %v", err)
	}
	if _, _, err := srv.toolCodeGraphFindServiceCalls(ctx, nil, codeGraphFindServiceCallsInput{ServicePath: "/elsewhere/svc"}); err == nil {
		t.Fatal("a folder in no workspace must be refused")
	}
	if _, _, err := srv.toolCodeGraphFindServiceCalls(ctx, nil, codeGraphFindServiceCallsInput{ServicePath: "relative"}); err == nil {
		t.Fatal("a relative servicePath must be refused")
	}
	if _, _, err := NewServer(Deps{Caller: &fakeCaller{}}).toolCodeGraphFindServiceCalls(ctx, nil, codeGraphFindServiceCallsInput{ServicePath: auth}); err == nil {
		t.Fatal("without workspaces wired there is nothing to join")
	}
}
