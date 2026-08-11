package ssh

import "testing"

func TestAgentActiveProfileSticky(t *testing.T) {
	svc := newService(t)
	if id := svc.AgentActiveProfileID(); id != "" {
		t.Fatalf("want empty, got %q", id)
	}

	summary, err := svc.CreateProfile(CreateProfileInput{
		Name:     "dev-box",
		Host:     "127.0.0.1",
		Username: "ubuntu",
		Secret:   "s3cr3t",
		Env:      "dev",
	})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}

	if err := svc.SetAgentActiveProfile(summary.ID); err != nil {
		t.Fatalf("SetAgentActiveProfile: %v", err)
	}
	if got := svc.AgentActiveProfileID(); got != summary.ID {
		t.Fatalf("AgentActiveProfileID = %q, want %q", got, summary.ID)
	}

	if err := svc.SetAgentActiveProfile("deadbeefdeadbeefdeadbeef"); err == nil {
		t.Fatal("unknown id must fail")
	}
	if got := svc.AgentActiveProfileID(); got != summary.ID {
		t.Fatalf("failed set must leave sticky id; got %q", got)
	}

	if err := svc.SetAgentActiveProfile(""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if got := svc.AgentActiveProfileID(); got != "" {
		t.Fatalf("after clear want empty, got %q", got)
	}
}
