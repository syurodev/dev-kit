package agenthub

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

func init() {
	conformanceCases = append(conformanceCases,
		ConformanceCase{
			Name: "prompt_streaming_updates",
			SkipIf: func(v VendorQuirks) (bool, string) {
				if !v.Supports.StreamingUpdates {
					return true, "vendor does not stream updates"
				}
				return false, ""
			},
			Run: func(t *testing.T, v VendorQuirks) {
				rt, err := v.Open(t, nil)
				if err != nil {
					t.Fatalf("Open: %v", err)
				}
				defer rt.Close(context.Background())

				var updates []json.RawMessage
				var mu sync.Mutex
				rt.SetHandlers(RuntimeHandlers{
					OnUpdate: func(u, _ json.RawMessage) {
						mu.Lock()
						updates = append(updates, append(json.RawMessage(nil), u...))
						mu.Unlock()
					},
				})

				_, err = rt.Prompt(context.Background(), TextPrompt("hello"), nil)
				if err != nil {
					t.Fatalf("Prompt: %v", err)
				}

				mu.Lock()
				defer mu.Unlock()
				found := false
				for _, u := range updates {
					parsed := ParseSessionUpdate(u)
					if parsed.Kind == UpdateAgentMessageChunk && parsed.Content != nil && parsed.Content.Text == v.ExpectedStream {
						found = true
						break
					}
				}
				if !found {
					t.Logf("updates: %v", updates)
					t.Fatal("no agent_message_chunk update received")
				}
			},
			RequiredCap: CapStreamingUpdates,
		},
		ConformanceCase{
			Name: "permission_allow",
			SkipIf: func(v VendorQuirks) (bool, string) {
				if !v.Supports.Permission {
					return true, "vendor does not invoke OnPermission"
				}
				return false, ""
			},
			Run: func(t *testing.T, v VendorQuirks) {
				rt, err := v.Open(t, []string{permissionMarker("allow")})
				if err != nil {
					t.Fatalf("Open: %v", err)
				}
				defer rt.Close(context.Background())

				var asked json.RawMessage
				var answer json.RawMessage
				rt.SetHandlers(RuntimeHandlers{
					OnUpdate: func(json.RawMessage, json.RawMessage) {},
					OnPermission: func(_ context.Context, params json.RawMessage) (json.RawMessage, error) {
						asked = params
						// Return "allow" option
						var p map[string]json.RawMessage
						_ = json.Unmarshal(params, &p)
						// Find the allow option
						var opts []map[string]any
						_ = json.Unmarshal(p["options"], &opts)
						for _, opt := range opts {
							if opt["optionId"] == "allow" || opt["kind"] == "allow_once" {
								answer, _ = json.Marshal(map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": opt["optionId"]}})
								return answer, nil
							}
						}
						return nil, ErrPermissionDenied
					},
				})

				_, err = rt.Prompt(context.Background(), TextPrompt("NEED_PERMISSION"), nil)
				if err != nil {
					t.Fatalf("Prompt with permission: %v", err)
				}
				if len(asked) == 0 {
					t.Fatal("OnPermission was not called")
				}
				if !strings.Contains(string(answer), "allow") {
					t.Fatalf("allow answer = %s", answer)
				}
				assertPermissionWireAnswer(t, v, true)
			},
			RequiredCap: CapPermission,
		},
		ConformanceCase{
			Name: "permission_deny",
			SkipIf: func(v VendorQuirks) (bool, string) {
				if !v.Supports.Permission {
					return true, "vendor does not invoke OnPermission"
				}
				return false, ""
			},
			Run: func(t *testing.T, v VendorQuirks) {
				rt, err := v.Open(t, []string{permissionMarker("deny")})
				if err != nil {
					t.Fatalf("Open: %v", err)
				}
				defer rt.Close(context.Background())

				var asked json.RawMessage
				var answer json.RawMessage
				rt.SetHandlers(RuntimeHandlers{
					OnUpdate: func(json.RawMessage, json.RawMessage) {},
					OnPermission: func(_ context.Context, params json.RawMessage) (json.RawMessage, error) {
						asked = params
						answer = json.RawMessage(`{"outcome":{"outcome":"cancelled"}}`)
						return answer, ErrPermissionDenied
					},
				})

				_, err = rt.Prompt(context.Background(), TextPrompt("NEED_PERMISSION"), nil)
				// A denial ends the tool call, not the turn — the turn continues.
				// The mock returns a successful outcome after denial.
				if err != nil {
					t.Fatalf("Prompt with denied permission: %v", err)
				}
				if len(asked) == 0 {
					t.Fatal("OnPermission was not called")
				}
				if !strings.Contains(string(answer), "cancelled") {
					t.Fatalf("deny answer = %s", answer)
				}
				assertPermissionWireAnswer(t, v, false)
			},
			RequiredCap: CapPermission,
		},
	)
}
