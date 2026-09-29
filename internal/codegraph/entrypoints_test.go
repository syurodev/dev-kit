package codegraph

import "testing"

func TestCompleteEntryPointsFillsWhatAnAnalyzerLeftEmpty(t *testing.T) {
	entries := completeEntryPoints([]EntryPoint{
		// The Java analyzer's: HTTP routes, owner read off the node id.
		{Verb: "GET", Target: "/refunds", EntryNode: "com.acme.refund.web.RefundController.list()"},
		{Verb: "POST", Target: "/orders", EntryNode: "com.acme.order.web.OrderController.create(com.acme.order.Order)"},
		// An index written before kinds existed: a Kong phase.
		{Verb: "ACCESS", Target: "rate-limit", EntryNode: "kong.plugins.rate-limit.handler.access()"},
		// A Nest cron job, as an old index stored it.
		{Verb: "CRON", Target: "0 * * * *", EntryNode: "src/jobs.Jobs.hourly()"},
		// What an analyzer said is kept.
		{Verb: "GET", Target: "/x", EntryNode: "n", Kind: EntryKindHTTP, Owner: "src/app", Group: "app.ts"},
	})

	refund, order, phase, cron, kept := entries[0], entries[1], entries[2], entries[3], entries[4]
	if refund.Kind != EntryKindHTTP || refund.Owner != "com.acme.refund.web.RefundController" || refund.Group != "RefundController" {
		t.Errorf("java route = %+v", refund)
	}
	// Two services under com.acme: each is named by the segment after it.
	if refund.Service != "refund" || order.Service != "order" {
		t.Errorf("services = %q, %q; want refund, order", refund.Service, order.Service)
	}
	if phase.Kind != EntryKindPlugin || phase.Group != "rate-limit" || phase.Order != kongPhaseOrder("access") {
		t.Errorf("kong phase = %+v", phase)
	}
	if cron.Kind != EntryKindSchedule {
		t.Errorf("cron kind = %q", cron.Kind)
	}
	if kept.Owner != "src/app" || kept.Group != "app.ts" || kept.Service != "" {
		t.Errorf("an analyzer's own answer was overwritten: %+v", kept)
	}
}

func TestCompleteEntryPointsNamesNoServiceForOne(t *testing.T) {
	entries := completeEntryPoints([]EntryPoint{
		{Verb: "GET", Target: "/a", EntryNode: "com.acme.web.AController.a()"},
		{Verb: "GET", Target: "/b", EntryNode: "com.acme.web.BController.b()"},
	})
	// Both controllers sit in the same package: one service is no heading.
	for _, ep := range entries {
		if ep.Service != "" {
			t.Errorf("%s: service %q, want none", ep.Target, ep.Service)
		}
	}
}

func TestClassOfMemberKeepsAModuleQualifierInTheKeyOnly(t *testing.T) {
	owner, group := classOfMember("com.acme.web.UsersController [users-api].get(java.lang.String)")
	if owner != "com.acme.web.UsersController [users-api]" || group != "UsersController" {
		t.Errorf("classOfMember = %q, %q", owner, group)
	}
}
