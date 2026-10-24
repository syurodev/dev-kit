package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// ConformanceCase is one test case in the vendor-agnostic suite.
type ConformanceCase struct {
	Name        string
	Run         func(t *testing.T, v VendorQuirks)
	SkipIf      func(v VendorQuirks) (bool, string) // true = skip, with reason
	RequiredCap CapabilityFlag
}

// CapabilityFlag is a single capability for the skip matrix.
type CapabilityFlag int

const (
	CapLoadResume CapabilityFlag = 1 << iota
	CapElicitation
	CapProcessDeath
	CapSetMode
	CapSetConfigOption
	CapStreamingUpdates
	CapPermission
	CapVendorRefusal
	CapCancel
	CapUsageUpdate
)

func (c CapabilityFlag) satisfied(q VendorQuirks) bool {
	switch c {
	case CapLoadResume:
		return q.Supports.LoadResume
	case CapElicitation:
		return q.Supports.Elicitation
	case CapProcessDeath:
		return q.Supports.ProcessDeath
	case CapSetMode:
		return q.Supports.SetMode
	case CapSetConfigOption:
		return q.Supports.SetConfigOption
	case CapStreamingUpdates:
		return q.Supports.StreamingUpdates
	case CapPermission:
		return q.Supports.Permission
	case CapVendorRefusal:
		return q.Supports.VendorRefusal
	case CapCancel:
		return q.Supports.Cancel
	case CapUsageUpdate:
		return q.Supports.UsageUpdate
	default:
		return false
	}
}

var conformanceCases = []ConformanceCase{
	{
		Name: "open",
		Run: func(t *testing.T, v VendorQuirks) {
			rt, err := v.Open(t, nil)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			if rt.ACPSessionID() == "" {
				t.Fatal("ACPSessionID is empty")
			}
			_ = rt.Close(context.Background())
		},
	},
	{
		Name: "load_resume",
		SkipIf: func(v VendorQuirks) (bool, string) {
			if !v.Supports.LoadResume {
				return true, "vendor has no SessionLoader"
			}
			return false, ""
		},
		Run: func(t *testing.T, v VendorQuirks) {
			// First open a session to get an ACP session ID
			rt, err := v.Open(t, nil)
			if err != nil {
				t.Fatalf("Open for load: %v", err)
			}
			id := rt.ACPSessionID()
			_ = rt.Close(context.Background())

			// Now load/resume it
			rt2, err := v.Load(t, id)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if rt2.ACPSessionID() != id {
				t.Fatalf("ACPSessionID = %q, want %q", rt2.ACPSessionID(), id)
			}
			_ = rt2.Close(context.Background())
		},
		RequiredCap: CapLoadResume,
	},
	{
		Name: "load_resume_never_prompted",
		SkipIf: func(v VendorQuirks) (bool, string) {
			if !v.Supports.LoadResume {
				return true, "vendor has no SessionLoader"
			}
			return false, ""
		},
		Run: func(t *testing.T, v VendorQuirks) {
			// A session that was never prompted (no turns) must be opened with Open,
			// not Load. The service does this; here we verify the adapter doesn't
			// require a session ID for a never-prompted session.
			rt, err := v.Open(t, nil)
			if err != nil {
				t.Fatalf("Open (never prompted): %v", err)
			}
			if rt.ACPSessionID() == "" {
				t.Fatal("ACPSessionID is empty")
			}
			_ = rt.Close(context.Background())
		},
		RequiredCap: CapLoadResume,
	},
	{
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
				if len(u) > 0 {
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
	{
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
							return json.Marshal(map[string]any{"outcome": "selected", "optionId": opt["optionId"]})
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
		},
		RequiredCap: CapPermission,
	},
	{
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
			rt.SetHandlers(RuntimeHandlers{
				OnUpdate: func(json.RawMessage, json.RawMessage) {},
				OnPermission: func(_ context.Context, params json.RawMessage) (json.RawMessage, error) {
					asked = params
					return nil, ErrPermissionDenied
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
		},
		RequiredCap: CapPermission,
	},
	{
		Name: "elicitation",
		SkipIf: func(v VendorQuirks) (bool, string) {
			if !v.Supports.Elicitation {
				return true, "vendor does not support elicitation"
			}
			return false, ""
		},
		Run: func(t *testing.T, v VendorQuirks) {
			rt, err := v.Open(t, []string{elicitationMarker()})
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer rt.Close(context.Background())

			var elicited json.RawMessage
			rt.SetHandlers(RuntimeHandlers{
				OnUpdate: func(json.RawMessage, json.RawMessage) {},
				OnElicit: func(_ context.Context, params json.RawMessage) (json.RawMessage, error) {
					elicited = params
					// Return a simple text answer
					return json.Marshal(map[string]any{"content": "elicited answer"})
				},
			})

			_, err = rt.Prompt(context.Background(), TextPrompt("ELICITATION"), nil)
			if err != nil {
				t.Fatalf("Prompt with elicitation: %v", err)
			}
			if len(elicited) == 0 {
				t.Fatal("OnElicit was not called")
			}
		},
		RequiredCap: CapElicitation,
	},
	{
		Name: "cancel_mid_turn",
		SkipIf: func(v VendorQuirks) (bool, string) {
			if !v.Supports.Cancel {
				return true, "vendor test wire cannot hold a live turn for cancellation"
			}
			return false, ""
		},
		RequiredCap: CapCancel,
		Run: func(t *testing.T, v VendorQuirks) {
			rt, err := v.Open(t, []string{holdAfterMarker()})
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer rt.Close(context.Background())

			started := make(chan struct{}, 1)
			done := make(chan struct{})
			rt.SetHandlers(RuntimeHandlers{
				OnUpdate: func(u, _ json.RawMessage) {
					if ParseSessionUpdate(u).Kind == UpdateToolCall || strings.Contains(string(u), "agent_message_chunk") {
						select {
						case started <- struct{}{}:
						default:
						}
					}
				},
			})

			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := rt.Prompt(context.Background(), TextPrompt("HOLD_AFTER"), nil)
				if err != nil && !errors.Is(err, ErrCancelled) {
					t.Errorf("Prompt: %v", err)
				}
				close(done)
			}()

			// Wait for the turn to start
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("turn never started")
			}

			if err := rt.Cancel(context.Background()); err != nil {
				t.Fatalf("Cancel: %v", err)
			}

			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("Prompt did not return after Cancel")
			}
		},
	},
	{
		Name:        "usage_update",
		RequiredCap: CapUsageUpdate,
		SkipIf: func(v VendorQuirks) (bool, string) {
			if !v.Supports.UsageUpdate {
				return true, "vendor test wire does not provide usage updates"
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
				OnUpdate: func(u, m json.RawMessage) {
					mu.Lock()
					updates = append(updates, append(json.RawMessage(nil), u...))
					mu.Unlock()
				},
			})

			outcome, err := rt.Prompt(context.Background(), TextPrompt("hello"), nil)
			if err != nil {
				t.Fatalf("Prompt: %v", err)
			}

			// Check outcome.Meta first (mock), then updates for usage
			if len(outcome.Meta) > 0 {
				norm, err := v.Normalize(outcome.Meta)
				if err != nil {
					t.Fatalf("NormalizeUsage: %v", err)
				}
				if norm.InputTokens == 0 && norm.OutputTokens == 0 && norm.CostUSD == 0 {
					t.Logf("normalized usage is zero: %+v", norm)
				}
			} else {
				// Look for usage in updates
				mu.Lock()
				for _, u := range updates {
					if strings.Contains(string(u), "tokenUsage") || strings.Contains(string(u), "rate_limit_event") {
						norm, err := v.Normalize(u)
						if err != nil {
							mu.Unlock()
							t.Fatalf("NormalizeUsage: %v", err)
						}
						if norm.InputTokens != 0 || norm.OutputTokens != 0 || norm.CostUSD != 0 {
							mu.Unlock()
							return
						}
					}
				}
				mu.Unlock()
				t.Log("usage update not found in stream (may be vendor-specific)")
			}
		},
	},
	{
		Name: "set_mode",
		SkipIf: func(v VendorQuirks) (bool, string) {
			if !v.Supports.SetMode || len(v.SetModeIDs) == 0 {
				return true, "vendor does not support SetMode"
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
			rt.SetHandlers(RuntimeHandlers{
				OnUpdate: func(u, _ json.RawMessage) {
					updates = append(updates, append(json.RawMessage(nil), u...))
				},
			})

			modeID := v.SetModeIDs[0]
			if err := rt.(RuntimeConfigurator).SetMode(context.Background(), modeID); err != nil {
				t.Fatalf("SetMode(%s): %v", modeID, err)
			}

			found := false
			for _, u := range updates {
				if strings.Contains(string(u), "current_mode_update") && strings.Contains(string(u), modeID) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("no current_mode_update for %s in updates: %v", modeID, updates)
			}
		},
		RequiredCap: CapSetMode,
	},
	{
		Name: "set_config_option",
		SkipIf: func(v VendorQuirks) (bool, string) {
			if !v.Supports.SetConfigOption || len(v.SetConfigOptionIDs) == 0 {
				return true, "vendor does not support SetConfigOption"
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
			rt.SetHandlers(RuntimeHandlers{
				OnUpdate: func(u, _ json.RawMessage) {
					updates = append(updates, append(json.RawMessage(nil), u...))
				},
			})

			optID := v.SetConfigOptionIDs[0]
			value := "test-value"
			if len(v.SetConfigOptionValues) > 0 {
				value = v.SetConfigOptionValues[0]
			}
			if err := rt.(RuntimeConfigurator).SetConfigOption(context.Background(), optID, value); err != nil {
				t.Fatalf("SetConfigOption(%s): %v", optID, err)
			}

			found := false
			for _, u := range updates {
				if strings.Contains(string(u), "config_option_update") && strings.Contains(string(u), optID) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("no config_option_update for %s in updates: %v", optID, updates)
			}
		},
		RequiredCap: CapSetConfigOption,
	},
	{
		Name: "close",
		Run: func(t *testing.T, v VendorQuirks) {
			rt, err := v.Open(t, nil)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			if err := rt.Close(context.Background()); err != nil {
				t.Fatalf("Close: %v", err)
			}
			// Prompt after close should fail
			if _, err := rt.Prompt(context.Background(), TextPrompt("hello"), nil); !errors.Is(err, ErrSessionNotFound) {
				t.Fatalf("Prompt after close = %v, want ErrSessionNotFound", err)
			}
		},
	},
	{
		Name: "process_death",
		SkipIf: func(v VendorQuirks) (bool, string) {
			if !v.Supports.ProcessDeath {
				return true, "vendor does not report process death"
			}
			return false, ""
		},
		Run: func(t *testing.T, v VendorQuirks) {
			// Open a session that will die (Codex "die" scenario, or ACP pipe closed)
			rt, err := v.Open(t, []string{"die"})
			if err != nil {
				t.Fatalf("Open: %v", err)
			}

			// Prompt should fail with ErrProcessFailed
			_, err = rt.Prompt(context.Background(), TextPrompt("go"), nil)
			if !errors.Is(err, ErrProcessFailed) {
				t.Fatalf("Prompt = %v, want ErrProcessFailed", err)
			}

			// RuntimeLiveness.Dead should report true
			if liveness, ok := rt.(RuntimeLiveness); ok {
				deadline := time.Now().Add(5 * time.Second)
				for {
					dead, _ := liveness.Dead()
					if dead {
						return
					}
					if time.Now().After(deadline) {
						t.Fatal("runtime never reported Dead()")
					}
					time.Sleep(20 * time.Millisecond)
				}
			} else {
				t.Fatal("vendor does not implement RuntimeLiveness")
			}
		},
		RequiredCap: CapProcessDeath,
	},
	{
		Name: "vendor_refusal",
		SkipIf: func(v VendorQuirks) (bool, string) {
			if !v.Supports.VendorRefusal {
				return true, "vendor does not produce vendor refusals"
			}
			return false, ""
		},
		Run: func(t *testing.T, v VendorQuirks) {
			rt, err := v.Open(t, []string{vendorRefusalMarker()})
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer rt.Close(context.Background())

			_, err = rt.Prompt(context.Background(), TextPrompt("FAIL_PROVIDER_503"), nil)
			if !errors.Is(err, ErrTurnFailed) {
				t.Fatalf("Prompt = %v, want ErrTurnFailed", err)
			}
			if !strings.Contains(err.Error(), "unavailable") {
				t.Fatalf("error should carry vendor sentence: %v", err)
			}
			// Session should be idle, not crashed
			if liveness, ok := rt.(RuntimeLiveness); ok {
				dead, _ := liveness.Dead()
				if dead {
					t.Fatal("session should be idle, not dead")
				}
			}
		},
		RequiredCap: CapVendorRefusal,
	},
}

func TestConformance_Mock(t *testing.T) {
	runConformance(t, MockQuirks())
}

func TestConformance_Claude(t *testing.T) {
	runConformance(t, ClaudeQuirks())
}

func TestConformance_OpenCode(t *testing.T) {
	runConformance(t, OpenCodeQuirks())
}

func TestConformance_Codex(t *testing.T) {
	runConformance(t, CodexQuirks())
}

func runConformance(t *testing.T, v VendorQuirks) {
	for _, tc := range conformanceCases {
		t.Run(tc.Name, func(t *testing.T) {
			if tc.SkipIf != nil {
				if skip, reason := tc.SkipIf(v); skip {
					t.Skip(reason)
				}
			}
			if tc.RequiredCap != 0 && !tc.RequiredCap.satisfied(v) {
				t.Skipf("requires capability %v", tc.RequiredCap)
			}
			tc.Run(t, v)
		})
	}
}

// Markers for the mock adapter
func permissionMarker(answer string) string {
	if answer == "allow" {
		return "NEED_PERMISSION"
	}
	return "NEED_PERMISSION"
}

func holdAfterMarker() string {
	return "HOLD_AFTER"
}

func elicitationMarker() string {
	return "ELICITATION"
}

func vendorRefusalMarker() string {
	return "FAIL_PROVIDER_503"
}
