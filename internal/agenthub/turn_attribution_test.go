package agenthub

import "testing"

func TestUsageScopeRejectsStaleEndpointAfterRuntimeReplacement(t *testing.T) {
	s := &Service{runtimeUsage: map[string]RuntimeUsageFacts{}, sessionAccount: map[string]string{}}
	sess := AgentSession{ID: "s", AgentType: AgentTypeClaude}
	spec := OpenSpec{WorkspaceID: "workspace", UsageToken: "old"}
	s.publishUsageLocked(sess, spec, BuiltinAccountID(sess.AgentType))
	if f, ok := s.ToolUsageScope("s", "old"); !ok || f.Vendor != "claude" || f.WorkspaceID != "workspace" {
		t.Fatalf("scope %+v %v", f, ok)
	}
	a := s.turnAttribution("s")
	if a.Facts.AccountID != "claude-default" || a.Facts.AccountGeneration != "builtin" {
		t.Fatalf("builtin attribution %+v", a)
	}
	spec.UsageToken = "new"
	spec.UsageAccountGeneration = "generation"
	s.publishUsageLocked(sess, spec, "work")
	if _, ok := s.ToolUsageScope("s", "old"); ok {
		t.Fatal("old endpoint acquired replacement runtime's facts")
	}
	if _, ok := s.ToolUsageScope("s", ""); ok {
		t.Fatal("unidentified endpoint accepted")
	}
	if _, ok := s.ToolUsageScope("s", "new"); !ok {
		t.Fatal("current endpoint rejected")
	}
	if a.Facts.AccountID != "claude-default" {
		t.Fatal("captured facts mutated on replacement")
	}
	delete(s.runtimeUsage, "s")
	if _, ok := s.ToolUsageScope("s", "new"); ok {
		t.Fatal("closed runtime was attributed")
	}
}
