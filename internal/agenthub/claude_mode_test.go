package agenthub

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"devkit/internal/acp"
)

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
