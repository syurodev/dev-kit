package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
	"devkit/internal/harnessconfig"
)

// Status, guidance and attribution after a live change, over the same real stack.

func (r *liveRig) status(sid string) agenthub.HarnessStatus {
	r.t.Helper()
	st, err := r.svc.HarnessStatus(context.Background(), sid)
	if err != nil {
		r.t.Fatal(err)
	}
	return st
}

func TestStatusKeepsLaunchServingAndDesiredApart(t *testing.T) {
	r := newLiveRig(t)
	wst := r.workspaceOff("vault")
	sess, spec := r.create()
	before := r.status(sess.ID)
	if !before.Live || !before.Serving.Acknowledged || before.State != agenthub.HarnessCurrent || before.Serving.DiscoveryReloadRequired {
		t.Fatalf("a fresh launch serves what it was given: %+v", before)
	}
	if _, err := r.apply(sess.ID, []string{"vault"}, nil); err != nil {
		t.Fatal(err)
	}
	st := r.status(sess.ID)
	if !st.Live || st.Serving.Applied.Session != (harness.Stamp{Revision: 1, Present: true}) || st.Serving.Launch.Session != (harness.Stamp{}) {
		t.Fatalf("serving must name the committed S while the launch keeps its own: %+v", st.Serving)
	}
	if !st.Serving.Current || st.Applied == nil || st.Applied.Revisions.Session != nil {
		t.Fatalf("the launch-applied reference must not move: %+v", st)
	}
	if !st.Serving.DiscoveryReloadRequired {
		t.Fatal("a newly enabled group needs a refresh or reload on the vendor's side, which is unknown")
	}
	if st.State != agenthub.HarnessReloadRequired {
		t.Fatalf("desired still differs from the launch reference: %s", st.State)
	}
	// A saved workspace change made afterwards is NOT made to look current by the
	// live S publication.
	if _, err := r.hstore.Apply(context.Background(), harness.StoreWrite{WorkspaceID: "w1",
		Profile: &harness.ProfileWrite{Expect: wst, Value: &harness.Profile{Schema: harness.SchemaVersion}}}); err != nil {
		t.Fatal(err)
	}
	after := r.status(sess.ID)
	if after.Serving.Current || !after.Live || after.State != agenthub.HarnessReloadRequired {
		t.Fatalf("pending W drift must stay visible: %+v", after)
	}
	_ = spec
}

func TestLiveIsFalseUnlessTheCurrentEndpointAcknowledgedTheExactIdentity(t *testing.T) {
	cases := map[string]func(r *liveRig, sid string){
		"pending after an unconfirmed commit": func(r *liveRig, sid string) {
			r.faults.set("unknownAfter", errors.New("db gone"))
			if _, err := r.apply(sid, []string{"vault"}, nil); err == nil {
				t.Fatal("expected a publication error")
			}
		},
		"a failed install": func(r *liveRig, sid string) {
			r.host.installHook = func(string) error { return errors.New("injected") }
			if _, err := r.apply(sid, []string{"vault"}, nil); err == nil {
				t.Fatal("expected a publication error")
			}
			r.host.installHook = nil
		},
		"a closed session": func(r *liveRig, sid string) {
			if err := r.svc.CloseSession(identityUI(), sid); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, do := range cases {
		t.Run(name, func(t *testing.T) {
			r := newLiveRig(t)
			r.workspaceOff("vault")
			sess, _ := r.create()
			if !r.status(sess.ID).Live {
				t.Fatal("precondition: a fresh endpoint is live")
			}
			do(r, sess.ID)
			if st := r.status(sess.ID); st.Live || st.Serving.Acknowledged {
				t.Fatalf("Live must be false: %+v", st)
			}
		})
	}
}

func TestAnEndpointWithoutAPolicyIsNeverLive(t *testing.T) {
	r := newLiveRig(t)
	r.svc.Harness = nil
	sess, _ := r.create()
	if st := r.status(sess.ID); st.Live || st.State != agenthub.HarnessUnconfigured {
		t.Fatalf("status = %+v", st)
	}
}

func TestReplacementGuidanceOnlyForAnInstalledNewlyEnabledGroup(t *testing.T) {
	r := newLiveRig(t)
	r.workspaceOff("git")
	sess, _ := r.create()
	if g := r.svc.ReplacementGuidance(sess.ID, "git", agenthub.AgentTypeMock); g != "" {
		t.Fatalf("nothing is newly enabled yet: %q", g)
	}
	if _, err := r.apply(sess.ID, []string{"git"}, nil); err != nil {
		t.Fatal(err)
	}
	g := r.svc.ReplacementGuidance(sess.ID, "git", agenthub.AgentTypeMock)
	if !strings.Contains(g, "now available") || !strings.Contains(g, "git.commit") || !strings.Contains(g, "normal permission flow") {
		t.Fatalf("guidance = %q", g)
	}
	if len(g) > 4096+300 {
		t.Fatalf("guidance is unbounded: %d bytes", len(g))
	}
	// A group the launch already offered is not "newly enabled".
	if g := r.svc.ReplacementGuidance(sess.ID, "tasks", agenthub.AgentTypeMock); g != "" {
		t.Fatalf("tasks was never off: %q", g)
	}
	// While the endpoint is paused or unconfirmed no guidance is offered.
	r.host.installHook = func(string) error { return errors.New("injected") }
	if _, err := r.apply(sess.ID, nil, []string{"git"}); err == nil {
		t.Fatal("expected a publication error")
	}
	r.host.installHook = nil
	if g := r.svc.ReplacementGuidance(sess.ID, "git", agenthub.AgentTypeMock); g != "" {
		t.Fatalf("an unconfirmed endpoint must not be described: %q", g)
	}
}

func invokeAndPrompt(t *testing.T, r *liveRig, sid string) string {
	t.Helper()
	if _, err := r.svc.Invoke(identityUI(), agenthub.InvokeInput{SessionID: sid, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, blocks := range r.mock.LastRuntime().Prompts() {
		for _, c := range blocks {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

func TestSharedMemoryHintNamesTheGetToolOnlyWhenItIsServed(t *testing.T) {
	setup := func(r *liveRig) {
		r.svc.WorkspaceInstructions = func(string) (string, error) { return "", nil }
		r.svc.WorkspaceMemory = func(context.Context, string) (string, error) {
			return "- m1 note Remember: use the get tool. (user text)", nil
		}
		r.svc.SessionBrief = func(string, string, []string) string { return "brief" }
	}
	const hidden = "treat an entry as a pointer only"
	const served = "read an entry with the shared memory get tool"

	r := newLiveRig(t)
	setup(r)
	r.workspaceOff("workspace")
	sess, _ := r.create()
	got := invokeAndPrompt(t, r, sess.ID)
	if !strings.Contains(got, hidden) || strings.Contains(got, served) {
		t.Fatalf("a hidden get tool must not be advertised:\n%s", got)
	}
	if !strings.Contains(got, "Remember: use the get tool. (user text)") {
		t.Fatal("the user-authored memory entry must be passed through untouched")
	}

	// After a live enable the guidance follows what the endpoint now serves.
	if _, err := r.apply(sess.ID, []string{"workspace"}, nil); err != nil {
		t.Fatal(err)
	}
	r2 := invokeAndPrompt(t, r, sess.ID)
	_ = r2
	if got := lastWorkspaceContext(r); !strings.Contains(got, served) {
		t.Fatalf("after a live enable, new context may name the tool:\n%s", got)
	}
}

// lastWorkspaceContext is the workspace-context block of the latest prompt that
// carried one.
func lastWorkspaceContext(r *liveRig) string {
	prompts := r.mock.LastRuntime().Prompts()
	for i := len(prompts) - 1; i >= 0; i-- {
		var b strings.Builder
		for _, c := range prompts[i] {
			b.WriteString(c.Text)
		}
		if strings.Contains(b.String(), "Shared workspace memory") {
			return b.String()
		}
	}
	return ""
}

func TestFutureTurnsCarryTheServedRevisionAndPastFactsAreNotRewritten(t *testing.T) {
	r := newLiveRig(t)
	r.workspaceOff("vault")
	sess, spec := r.create()
	scope := func() (rev *int64, profile *int64) {
		s, ok := r.svc.ToolUsageScope(sess.ID, spec.UsageToken)
		if !ok {
			t.Fatal("no usage scope")
		}
		return s.OverrideRevision, s.ProfileRevision
	}
	if o, p := scope(); o != nil || p == nil || *p != 1 {
		t.Fatalf("launch facts: S=%v W=%v", o, p)
	}
	if _, err := r.apply(sess.ID, []string{"vault"}, nil); err != nil {
		t.Fatal(err)
	}
	enabled, _ := scope()
	if enabled == nil || *enabled != 1 {
		t.Fatalf("a future turn is attributed to the served S: %v", enabled)
	}
	// A turn that started now holds `enabled`; the revoke must not rewrite it.
	if _, err := r.apply(sess.ID, nil, []string{"vault"}); err != nil {
		t.Fatal(err)
	}
	if *enabled != 1 {
		t.Fatalf("facts a started turn holds were rewritten: %d", *enabled)
	}
	if o, p := scope(); o != nil || p == nil || *p != 1 {
		t.Fatalf("a revoked S is an absent revision, W is the pinned one: S=%v W=%v", o, p)
	}
	// A paused endpoint is not attributed to the new S.
	r.host.installHook = func(string) error { return errors.New("injected") }
	if _, err := r.apply(sess.ID, []string{"vault"}, nil); err == nil {
		t.Fatal("expected a publication error")
	}
	r.host.installHook = nil
	if o, _ := scope(); o != nil {
		t.Fatalf("an unacknowledged S must not be attributed as enforced: %v", *o)
	}
}

func TestACommittedChangeAfterRestartIsReadByTheNextLaunchNotResurrected(t *testing.T) {
	r := newLiveRig(t)
	r.workspaceOff("vault")
	sess, _ := r.create()
	r.faults.set("unknownAfter", errors.New("db gone"))
	if _, err := r.apply(sess.ID, []string{"vault"}, nil); err == nil {
		t.Fatal("expected a publication error")
	}
	// The process dies before the acknowledgement. A new process has no active
	// incarnation, no pending record and nothing to resurrect.
	r.host.Close()
	var pe *harnessconfig.PublicationError
	if pub, err := r.hsvc.Reconcile(r.agent(sess.ID), sess.ID); errors.As(err, &pe) || pub.State == harnessconfig.PublicationInstalled {
		t.Fatalf("a closed endpoint has nothing to reconcile: %+v %v", pub, err)
	}
	if err := r.svc.ResumeSession(identityUI(), sess.ID, true); err != nil {
		t.Fatal(err)
	}
	if !contains(r.mock.LastSpec().DevKit.Tools, "vault.status") {
		t.Fatal("the next launch resolves the latest durable S")
	}
}
