package agenthub

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"devkit/internal/acp"
)

// This adapter uses the real ACP runtime and transport. It clamps every mode
// request before answering, including requests made before handlers attach.
type clampingModeAdapter struct {
	*MockAdapter
	t         *testing.T
	effective string
	opened    *acpRuntime
	attached  chan bool
}

func (a *clampingModeAdapter) Type() AgentType { return AgentTypeClaude }

func (a *clampingModeAdapter) Open(context.Context, OpenSpec) (SessionRuntime, error) {
	client, agent := net.Pipe()
	rt := &acpRuntime{acpSessionID: "clamped-session", modes: &acp.SessionModeState{CurrentModeID: "default"}}
	for _, id := range claudeLikeModes {
		rt.modes.AvailableModes = append(rt.modes.AvailableModes, acp.SessionMode{ID: id})
	}
	rt.conn = acp.NewConn(client, &acpConnHandler{rt: rt})
	ctx, cancel := context.WithCancel(context.Background())
	a.t.Cleanup(func() { cancel(); client.Close(); agent.Close() })
	go rt.conn.Serve(ctx)
	go func() {
		msg, err := acp.ReadMessage(agent)
		if err != nil {
			return
		}
		a.attached <- rt.handlersSnapshot().OnUpdate != nil
		params, _ := json.Marshal(acp.SessionUpdateParams{SessionID: rt.acpSessionID, Update: marshalUpdate(map[string]any{
			"sessionUpdate": "current_mode_update", "currentModeId": a.effective,
		})})
		if err := acp.WriteMessage(agent, acp.Notification{JSONRPC: "2.0", Method: "session/update", Params: params}); err == nil {
			_ = acp.WriteMessage(agent, acp.Response{JSONRPC: "2.0", ID: msg.ID, Result: json.RawMessage(`{}`)})
		}
	}()
	a.opened = rt
	return rt, nil
}

func TestClaudeClampBeforeHandlersDuringChildCreationAndResume(t *testing.T) {
	for _, resume := range []bool{false, true} {
		// Also cover a clamp that stays in the handshake's mode: observing it
		// must still move the revision even though the current id is unchanged.
		for _, effective := range []string{"acceptEdits", "default"} {
			t.Run(fmt.Sprintf("resume=%v/%s", resume, effective), func(t *testing.T) {
				f := newModeFixture(t)
				vendor := AgentTypeCodex
				if resume {
					vendor = AgentTypeClaude
				}
				parent, caller := f.parent(t, vendor, "auto")
				a := &clampingModeAdapter{MockAdapter: NewMockAdapter(), t: t, effective: effective, attached: make(chan bool, 1)}
				f.svc.reg.Register(a)
				events := make(chan SessionUpdate, 8)
				f.svc.emit = func(_ string, raw json.RawMessage) {
					if u := ParseSessionUpdate(raw); u.Kind == UpdateCurrentMode {
						events <- u
					}
				}
				id := parent.ID
				if resume {
					_ = f.svc.recorder.Flush(caller, parent.ID)
					f.svc.mu.Lock()
					old := f.svc.runtimes[parent.ID]
					delete(f.svc.runtimes, parent.ID)
					f.svc.mu.Unlock()
					_ = old.Close(caller)
					if _, err := f.svc.resumeRuntime(caller, parent); err != nil {
						t.Fatal(err)
					}
				} else {
					child, err := f.svc.CreateSession(caller, CreateInput{AgentType: AgentTypeClaude, Cwd: parent.ProjectPath})
					if err != nil {
						t.Fatal(err)
					}
					id = child.ID
				}
				if <-a.attached {
					t.Fatal("test did not exercise SetMode before attaching handlers")
				}
				if got := a.opened.Modes().CurrentModeID; got != effective {
					t.Fatalf("live mode %q, want vendor clamp %q", got, effective)
				}
				if got := storedMode(t, f.svc, id); got != effective {
					t.Fatalf("stored mode %q, want vendor clamp %q", got, effective)
				}
				if len(events) != 1 {
					t.Fatalf("emitted %d mode announcements on attachment, want one", len(events))
				}
				if u := <-events; u.Mode != effective || len(u.AvailableModes) != len(claudeLikeModes) {
					t.Fatalf("buffered mode announcement = %+v", u)
				}
			})
		}
	}
}

func TestClaudeModeRevisionKeepsTheStricterMode(t *testing.T) {
	for _, tc := range []struct{ observed, requested, want string }{
		{"acceptEdits", "default", "default"},
		{"default", "acceptEdits", "default"},
		{"plan", "auto", "plan"},
		{"auto", "plan", "plan"},
		{"futureMode", "auto", "futureMode"},
		{"auto", "futureMode", "futureMode"},
		{"futureMode", "anotherUnknown", "futureMode"},
	} {
		t.Run(tc.observed+"/"+tc.requested, func(t *testing.T) {
			rt := &acpRuntime{modes: &acp.SessionModeState{CurrentModeID: "auto"}}
			rt.observeModeUpdate(marshalUpdate(map[string]any{
				"sessionUpdate": "current_mode_update", "currentModeId": tc.observed,
			}), nil)
			rt.recordSetMode(tc.requested, 0)
			if got := rt.Modes().CurrentModeID; got != tc.want {
				t.Fatalf("live mode = %q, want %q", got, tc.want)
			}
			var updates []SessionUpdate
			h := RuntimeHandlers{OnUpdate: func(raw json.RawMessage, _ json.RawMessage) {
				updates = append(updates, ParseSessionUpdate(raw))
			}}
			rt.SetHandlers(h)
			rt.SetHandlers(h) // buffered change is consumed only once
			if len(updates) != 1 || updates[0].Mode != tc.want {
				t.Fatalf("buffered changes = %+v, want one %q", updates, tc.want)
			}
		})
	}
}

func TestClaudeOverlappingModeCompletionsKeepTheStricterMode(t *testing.T) {
	for _, ids := range [][]string{{"default", "auto"}, {"auto", "default"}} {
		rt := &acpRuntime{modes: &acp.SessionModeState{CurrentModeID: "bypassPermissions"}}
		// Both calls began at revision zero, but their completions can arrive
		// in either order. Completing one must move the revision for the other.
		rt.recordSetMode(ids[0], 0)
		rt.recordSetMode(ids[1], 0)
		if got := rt.Modes().CurrentModeID; got != "default" {
			t.Fatalf("completion order %v left mode %q, want default", ids, got)
		}
	}
}

func TestClaudeDelayedAttachModeCannotOverwriteANewerRecord(t *testing.T) {
	f := newModeFixture(t)
	parent, _ := f.parent(t, AgentTypeClaude, "default")
	rt := &acpRuntime{acpSessionID: "parent", modes: &acp.SessionModeState{CurrentModeID: "default"}}
	rt.observeModeUpdate(marshalUpdate(map[string]any{
		"sessionUpdate": "current_mode_update", "currentModeId": "auto",
	}), nil)
	var modes []string
	// Capture the same pending snapshot SetHandlers takes under mu, then
	// deterministically let a newer vendor notification win the emit race.
	rt.mu.Lock()
	rt.handlers = RuntimeHandlers{OnUpdate: func(raw json.RawMessage, meta json.RawMessage) {
		_ = rt.Modes() // callbacks can read state without deadlocking on mu
		modes = append(modes, ParseSessionUpdate(raw).Mode)
		f.svc.handleUpdate(context.Background(), parent.ID, raw)
	}}
	old := rt.takePendingModeLocked()
	rt.mu.Unlock()
	params, _ := json.Marshal(acp.SessionUpdateParams{SessionID: "parent", Update: marshalUpdate(map[string]any{
		"sessionUpdate": "current_mode_update", "currentModeId": "plan",
	})})
	if err := (&acpConnHandler{rt: rt}).SessionUpdate(context.Background(), params); err != nil {
		t.Fatal(err)
	}
	rt.emitMode(old)
	if len(modes) != 1 || modes[0] != "plan" {
		t.Fatalf("mode announcements = %v, want only the newer plan", modes)
	}
	if got := storedMode(t, f.svc, parent.ID); got != "plan" {
		t.Fatalf("stored mode = %q, want newer plan", got)
	}
}

func TestOpenCodeVendorModeCannotReplaceDevKitPermissionMode(t *testing.T) {
	for _, permission := range codexLikeModes {
		t.Run(permission, func(t *testing.T) {
			f := newModeFixture(t)
			f.svc.reg.Register(&modeAdapter{MockAdapter: NewMockAdapter(), typ: AgentTypeOpenCode, offered: codexLikeModes, start: permission})
			sess, err := f.svc.CreateSession(context.Background(), CreateInput{AgentType: AgentTypeOpenCode, Cwd: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			base := &acpRuntime{hooks: acpRuntimeHooks{Type: AgentTypeOpenCode}, acpSessionID: "opencode", modes: &acp.SessionModeState{
				CurrentModeID: "build", AvailableModes: []acp.SessionMode{{ID: "build"}, {ID: "plan"}},
			}}
			rt := &openCodeRuntime{acpRuntime: base, mode: permission}
			handler := &acpConnHandler{rt: base}
			var announcements []string
			f.svc.emit = func(_ string, raw json.RawMessage) {
				if u := ParseSessionUpdate(raw); u.Kind == UpdateCurrentMode {
					announcements = append(announcements, u.Mode)
				}
			}
			for _, attached := range []bool{false, true} {
				if attached {
					f.svc.attachUpdates(sess.ID, rt)
				}
				params, _ := json.Marshal(acp.SessionUpdateParams{SessionID: "opencode", Update: marshalUpdate(map[string]any{
					"sessionUpdate": "current_mode_update", "currentModeId": "plan",
				})})
				if err := handler.SessionUpdate(context.Background(), params); err != nil {
					t.Fatal(err)
				}
			}
			if got := rt.Modes().CurrentModeID; got != permission {
				t.Fatalf("live permission mode = %q, want %q", got, permission)
			}
			if got := storedMode(t, f.svc, sess.ID); got != permission {
				t.Fatalf("stored permission mode = %q, want %q", got, permission)
			}
			if len(announcements) != 1 || announcements[0] != permission {
				t.Fatalf("permission announcements = %v, want only DevKit's %q", announcements, permission)
			}
		})
	}
}

// Exercise the real Claude SetMode and notification handler through Service,
// then create a child both with a live parent and after losing its runtime.
func TestClaudeModeChangeIsInheritedAndRecordedOnce(t *testing.T) {
	for _, echo := range []string{"none", "before", "after", "fallback"} {
		t.Run(echo, func(t *testing.T) {
			f := newModeFixture(t)
			parent, caller := f.parent(t, AgentTypeClaude, "default")
			client, agent := net.Pipe()
			defer client.Close()
			defer agent.Close()
			rt := &acpRuntime{acpSessionID: "acp-parent", modes: &acp.SessionModeState{
				CurrentModeID: "default", AvailableModes: []acp.SessionMode{{ID: "default"}, {ID: "auto"}, {ID: "acceptEdits"}},
			}}
			rt.conn = acp.NewConn(client, &acpConnHandler{rt: rt})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			go rt.conn.Serve(ctx)
			f.svc.mu.Lock()
			f.svc.runtimes[parent.ID] = rt
			f.svc.mu.Unlock()
			f.svc.attachUpdates(parent.ID, rt)
			// Count only mode changes, after attachUpdates' handshake seed.
			updates := make(chan string, 8)
			rt.SetHandlers(RuntimeHandlers{OnUpdate: func(update json.RawMessage, _ json.RawMessage) {
				if parsed := ParseSessionUpdate(update); parsed.Kind == UpdateCurrentMode {
					updates <- parsed.Mode
				}
				f.svc.handleUpdate(ctx, parent.ID, update)
			}})
			want := "auto"
			if echo == "fallback" {
				want = "acceptEdits"
			}
			notify := func() error {
				params, _ := json.Marshal(acp.SessionUpdateParams{
					SessionID: "acp-parent", Update: marshalUpdate(map[string]any{"sessionUpdate": "current_mode_update", "currentModeId": want}),
				})
				return acp.WriteMessage(agent, acp.Notification{JSONRPC: "2.0", Method: "session/update", Params: params})
			}
			done := make(chan error, 1)
			go func() {
				msg, err := acp.ReadMessage(agent)
				if err == nil && (echo == "before" || echo == "fallback") {
					err = notify()
				}
				if err == nil {
					err = acp.WriteMessage(agent, acp.Response{JSONRPC: "2.0", ID: msg.ID, Result: json.RawMessage(`{}`)})
				}
				done <- err
			}()
			if err := f.svc.SetMode(ctx, parent.ID, "auto"); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if echo == "after" {
				// Deterministically deliver the late echo after SetMode returns.
				params, _ := json.Marshal(acp.SessionUpdateParams{SessionID: "acp-parent", Update: marshalUpdate(map[string]any{
					"sessionUpdate": "current_mode_update", "currentModeId": want,
				})})
				if err := (&acpConnHandler{rt: rt}).SessionUpdate(ctx, params); err != nil {
					t.Fatal(err)
				}
			}
			if len(updates) != 1 {
				t.Fatalf("recorded %d mode changes, want exactly one", len(updates))
			}
			if got := storedMode(t, f.svc, parent.ID); got != want || rt.Modes().CurrentModeID != want {
				t.Fatalf("stored %q, live %q, want %q", got, rt.Modes().CurrentModeID, want)
			}
			for _, live := range []bool{true, false} {
				if !live {
					f.svc.mu.Lock()
					delete(f.svc.runtimes, parent.ID)
					f.svc.mu.Unlock()
				}
				// Start in ask so even auto must actually reach SetMode.
				f.codex.start = "ask"
				if _, err := f.svc.CreateSession(caller, CreateInput{AgentType: AgentTypeCodex, Cwd: parent.ProjectPath}); err != nil {
					t.Fatal(err)
				}
				if got, sets, _ := f.codex.last().state(); got != "auto" || len(sets) != 1 {
					t.Fatalf("live=%v child in %q after %v, want auto", live, got, sets)
				}
			}
		})
	}
}

func TestClaudeVendorModeChangesDoNotAdoptSubagentModes(t *testing.T) {
	rt := &acpRuntime{acpSessionID: "parent", modes: &acp.SessionModeState{CurrentModeID: "default"}}
	var updates []SessionUpdate
	rt.SetHandlers(RuntimeHandlers{OnUpdate: func(raw json.RawMessage, _ json.RawMessage) {
		updates = append(updates, ParseSessionUpdate(raw))
	}})
	h := &acpConnHandler{rt: rt}
	for _, session := range []string{"child", "parent"} {
		params, _ := json.Marshal(acp.SessionUpdateParams{SessionID: session, Update: marshalUpdate(map[string]any{
			"sessionUpdate": "current_mode_update", "currentModeId": "plan",
		})})
		if err := h.SessionUpdate(context.Background(), params); err != nil {
			t.Fatal(err)
		}
		want := "default"
		if session == "parent" {
			want = "plan"
		}
		if got := rt.Modes().CurrentModeID; got != want {
			t.Fatalf("after %s notification: live mode %q, want %q", session, got, want)
		}
	}
	if len(updates) != 2 {
		t.Fatalf("forwarded %d notifications, want both parent and tagged subagent", len(updates))
	}
}
