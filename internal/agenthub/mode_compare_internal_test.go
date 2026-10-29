package agenthub

import (
	"context"
	"testing"
)

func TestModeNotWiderThanRanksAndFailsClosedOnUnknown(t *testing.T) {
	cases := []struct {
		name          string
		parent, child string
		ok, known     bool
	}{
		{"same mode", "auto", "auto", true, true},
		{"child stricter", "bypassPermissions", "default", true, true},
		{"child wider", "default", "auto", false, true},
		{"read only parent, edits child", "plan", "acceptEdits", false, true},
		{"claude default equals codex ask", "default", codexModeAsk, true, true},
		{"cross vendor full vs ask", codexModeAsk, "bypassPermissions", false, true},
		{"neither side has a mode concept", "", "", true, true},
		{"parent has none, child has one", "", "plan", true, false},
		{"parent has one, child cannot be read", "plan", "", false, false},
		{"child mode is unranked", "auto", "someVendorMode", false, false},
		{"parent mode is unranked and child differs", "someVendorMode", "plan", false, false},
		{"unranked mode carried over as is", "someVendorMode", "someVendorMode", true, true},
	}
	for _, c := range cases {
		ok, known := ModeNotWiderThan(c.parent, c.child)
		if ok != c.ok || known != c.known {
			t.Errorf("%s: ModeNotWiderThan(%q, %q) = (%v, %v), want (%v, %v)", c.name, c.parent, c.child, ok, known, c.ok, c.known)
		}
	}
}

func TestPermissionModeOfUsesLiveRuntimeThenTranscript(t *testing.T) {
	ctx := context.Background()
	f := newModeFixture(t)
	parent, _ := f.parent(t, AgentTypeClaude, "default")
	if mode, known := f.svc.PermissionModeOf(ctx, parent.ID); !known || mode != "default" {
		t.Fatalf("live mode = %q known=%v", mode, known)
	}
	// The live runtime moves ahead of what the transcript recorded.
	f.claude.last().silent = true
	if err := f.svc.SetMode(ctx, parent.ID, "bypassPermissions"); err != nil {
		t.Fatal(err)
	}
	if got := storedMode(t, f.svc, parent.ID); got != "default" {
		t.Fatalf("the test needs a stale transcript, got %q", got)
	}
	if mode, known := f.svc.PermissionModeOf(ctx, parent.ID); !known || mode != "bypassPermissions" {
		t.Fatalf("the live runtime must win over the transcript: %q known=%v", mode, known)
	}
	// With no live runtime the last recorded mode answers.
	_ = f.svc.recorder.Flush(ctx, parent.ID)
	f.svc.mu.Lock()
	old := f.svc.runtimes[parent.ID]
	delete(f.svc.runtimes, parent.ID)
	f.svc.mu.Unlock()
	_ = old.Close(ctx)
	if mode, known := f.svc.PermissionModeOf(ctx, parent.ID); !known || mode != "default" {
		t.Fatalf("recorded mode = %q known=%v", mode, known)
	}
	if mode, known := f.svc.PermissionModeOf(ctx, "no-such-session"); known || mode != "" {
		t.Fatalf("an unknown session has no readable mode: %q %v", mode, known)
	}
	if mode, known := f.svc.PermissionModeOf(ctx, ""); known || mode != "" {
		t.Fatalf("an empty id has no readable mode: %q %v", mode, known)
	}
}

func TestPermissionModeOfIsUnknownForAVendorWithoutModes(t *testing.T) {
	reg := NewRegistry()
	reg.Register(NewMockAdapter())
	svc := NewService(reg, newInternalStore(t), nil)
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })
	s, err := svc.CreateSession(context.Background(), CreateInput{AgentType: AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if mode, known := svc.PermissionModeOf(context.Background(), s.ID); known || mode != "" {
		t.Fatalf("a session with no mode concept reads as unknown: %q %v", mode, known)
	}
}
