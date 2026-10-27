package app

import (
	"strings"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
)

// Enabling a group makes its tools listed, never exempt: the environment
// confirmation rule is the tool's own and runs through the shared hub on every
// call, "always" answers included, for a tag that requires it.
func TestAGroupEnabledLiveStillAsksForEnvironmentConfirmation(t *testing.T) {
	r := newCompRig(t, compOptions{realEnvs: true})
	r.workspaceOff("db")
	sess, spec := r.create(agenthub.AgentTypeClaude)
	cs := r.connect(spec)
	args := map[string]any{"profileId": probeProfileID}
	if got := r.callWith(cs, "db.testConnection", args); !strings.HasPrefix(got, harness.CodeGroupOff) {
		t.Fatalf("the hidden db group must refuse before the gate: %q", got)
	}
	if len(r.prompter.prompts) != 0 {
		t.Fatal("a hidden tool must not reach the environment gate")
	}
	if _, err := r.apply(sess.ID, []string{"db"}, nil); err != nil {
		t.Fatal(err)
	}

	// "prod" and an unknown tag (which fails closed) ask on every call; an
	// "allow_always" answer is one allow and is never kept.
	for _, tag := range []string{"prod", "a-tag-nobody-defined"} {
		r.dbEnv.Store(tag)
		r.prompter.answer = "allow_always"
		before := len(r.prompter.prompts)
		for i := 1; i <= 2; i++ {
			got := r.callWith(cs, "db.testConnection", args)
			if strings.HasPrefix(got, harness.CodeGroupOff) {
				t.Fatalf("tag %q: the enabled group must be served: %q", tag, got)
			}
			if n := len(r.prompter.prompts) - before; n != i {
				t.Fatalf("tag %q must ask on every call: %d prompts after %d calls", tag, n, i)
			}
		}
	}

	// A denial is final for that call: its handler never runs.
	r.dbEnv.Store("prod")
	r.prompter.answer = "deny"
	handled := r.gate.handled.Load()
	got := r.callWith(cs, "db.testConnection", args)
	if r.gate.handled.Load() != handled {
		t.Fatalf("a denied confirmation must stop the call before its handler: %q", got)
	}

	// The built-in "dev" tag does not require confirmation and never prompts.
	r.dbEnv.Store("dev")
	r.prompter.answer = "allow"
	asked := len(r.prompter.prompts)
	_ = r.callWith(cs, "db.testConnection", args)
	if len(r.prompter.prompts) != asked {
		t.Fatalf("a dev tag must not ask: %d new prompts", len(r.prompter.prompts)-asked)
	}
}
