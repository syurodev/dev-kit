package workspace

import "testing"

func TestResolveTarget(t *testing.T) {
	services := []TargetService{
		{Name: "billing-service", Path: "/ws/billing", Port: "8081"},
		{Name: "refund-service", Path: "/ws/refund", Port: "8082"},
		{Name: "dup", Path: "/ws/dup-a", Port: "9000"},
		{Name: "dup", Path: "/ws/dup-b", Port: "9000"},
	}
	caller := SpringConfig{Props: map[string]string{
		"clients.billing.url": "http://billing-service",
		"clients.local.url":   "http://localhost:8082",
		"clients.feign.name":  "refund-service",
	}}

	cases := []struct {
		name   string
		target OutboundTarget
		want   TargetResolution
	}{
		{"feign name", OutboundTarget{ServiceName: "billing-service", URL: "/refunds/{id}"},
			TargetResolution{Status: TargetResolved, Service: "/ws/billing", Host: "billing-service", Path: "/refunds/{id}", Provenance: ProvenanceFeignName}},
		{"feign name from a property", OutboundTarget{ServiceName: "${clients.feign.name}"},
			TargetResolution{Status: TargetResolved, Service: "/ws/refund", Host: "refund-service", Provenance: ProvenanceProperty}},
		{"feign name nobody registered", OutboundTarget{ServiceName: "ledger.service"},
			TargetResolution{Status: TargetUnknownService, Host: "ledger.service", Provenance: ProvenanceFeignName}},
		{"feign url overrides name", OutboundTarget{ServiceName: "billing-service", URL: "${clients.local.url}/x"},
			TargetResolution{Status: TargetResolved, Service: "/ws/refund", Host: "localhost:8082", Path: "/x", Provenance: ProvenancePort}},
		{"literal URL", OutboundTarget{URL: "http://billing-service/refunds?x=1"},
			TargetResolution{Status: TargetResolved, Service: "/ws/billing", Host: "billing-service", Path: "/refunds", Provenance: ProvenanceLiteral}},
		{"URL from @Value property", OutboundTarget{URL: "${clients.billing.url}/refunds/{id}"},
			TargetResolution{Status: TargetResolved, Service: "/ws/billing", Host: "billing-service", Path: "/refunds/{id}", Provenance: ProvenanceProperty}},
		{"env var with default", OutboundTarget{URL: "${BILLING_URL:http://billing-service:8081}/r"},
			TargetResolution{Status: TargetResolved, Service: "/ws/billing", Host: "billing-service", Path: "/r", Provenance: ProvenanceProperty}},
		{"cluster DNS name", OutboundTarget{URL: "http://billing-service.payments.svc.cluster.local/r"},
			TargetResolution{Status: TargetResolved, Service: "/ws/billing", Host: "billing-service.payments.svc.cluster.local", Path: "/r", Provenance: ProvenanceLiteral}},
		{"localhost by port", OutboundTarget{URL: "http://localhost:8081/r"},
			TargetResolution{Status: TargetResolved, Service: "/ws/billing", Host: "localhost:8081", Path: "/r", Provenance: ProvenancePort}},
		{"localhost, no service on that port", OutboundTarget{URL: "http://127.0.0.1:7777/r"},
			TargetResolution{Status: TargetUnknownService, Host: "127.0.0.1:7777", Path: "/r", Provenance: ProvenancePort}},
		{"unfolded env var", OutboundTarget{URL: "${BILLING_URL}/refunds"},
			TargetResolution{Status: TargetDynamicURL, Path: "${BILLING_URL}/refunds"}},
		{"host is a hole", OutboundTarget{URL: "{baseUrl}/refunds"},
			TargetResolution{Status: TargetDynamicURL, Path: "{baseUrl}/refunds"}},
		{"relative URL", OutboundTarget{URL: "/refunds"},
			TargetResolution{Status: TargetDynamicURL, Path: "/refunds"}},
		{"public host", OutboundTarget{URL: "https://api.stripe.com/v1/charges"},
			TargetResolution{Status: TargetExternal, Host: "api.stripe.com", Path: "/v1/charges", Provenance: ProvenanceLiteral}},
		{"internal host not in the workspace", OutboundTarget{URL: "http://ledger/entries"},
			TargetResolution{Status: TargetUnknownService, Host: "ledger", Path: "/entries", Provenance: ProvenanceLiteral}},
		{"two services share the name", OutboundTarget{URL: "http://dup/x"},
			TargetResolution{Status: TargetAmbiguousService, Candidates: []string{"/ws/dup-a", "/ws/dup-b"}, Host: "dup", Path: "/x", Provenance: ProvenanceLiteral}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ResolveTarget(c.target, caller, services)
			if got.Status != c.want.Status || got.Service != c.want.Service || got.Host != c.want.Host ||
				got.Path != c.want.Path || got.Provenance != c.want.Provenance || !equal(got.Candidates, c.want.Candidates) {
				t.Fatalf("got  %+v\nwant %+v", got, c.want)
			}
		})
	}
}
