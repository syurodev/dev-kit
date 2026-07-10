package permissions

import "testing"

func TestPolicyAllowsMatchingGrant(t *testing.T) {
	policy := NewPolicy([]Grant{
		{
			SubjectID:  "builtin.tools",
			Capability: "tools:hash",
			Scope:      "*",
		},
	})

	decision := policy.Check(Request{
		SubjectID:  "builtin.tools",
		Capability: "tools:hash",
		Scope:      "sha256",
	})

	if !decision.Allowed {
		t.Fatalf("Allowed = false, reason = %q", decision.Reason)
	}
}

func TestPolicyDeniesMissingGrant(t *testing.T) {
	policy := NewPolicy(nil)

	decision := policy.Check(Request{
		SubjectID:  "plugin.example",
		Capability: "vault:read",
		Scope:      "secret:connection/postgres/main",
	})

	if decision.Allowed {
		t.Fatal("Allowed = true, want false")
	}
}
