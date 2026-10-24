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

func init() {
	conformanceCases = append(conformanceCases,
		ConformanceCase{
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
		ConformanceCase{
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
					OnPermission: func(context.Context, json.RawMessage) (json.RawMessage, error) {
						return json.RawMessage(`{"outcome":"selected","optionId":"allow"}`), nil
					},
				})

				var wg sync.WaitGroup
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, err := rt.Prompt(context.Background(), TextPrompt("NEED_PERMISSION HOLD_AFTER"), nil)
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
				select {
				case <-done:
					t.Fatal("Prompt completed before Cancel; fake did not hold a live turn")
				default:
				}

				if err := rt.Cancel(context.Background()); err != nil {
					t.Fatalf("Cancel: %v", err)
				}

				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Fatal("Prompt did not return after Cancel")
				}
				wg.Wait()
			},
		},
		ConformanceCase{
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

				// Parse the wire update through the same production decoder used by
				// the transcript. A missing update or all-zero fixture is a failure.
				found := false
				mu.Lock()
				for _, raw := range updates {
					parsed := ParseSessionUpdate(raw)
					if parsed.Kind == UpdateUsage && v.ExpectedUpdateUsage != nil {
						if parsed.Usage == nil || parsed.Usage.Used != v.ExpectedUpdateUsage.Used || parsed.Usage.Size != v.ExpectedUpdateUsage.Size {
							mu.Unlock()
							t.Fatalf("decoded usage = %+v, want used=10 size=100", parsed.Usage)
						}
						found = true
					}
				}
				mu.Unlock()
				if len(outcome.Meta) > 0 {
					norm, err := v.Normalize(outcome.Meta)
					if err != nil {
						t.Fatalf("NormalizeUsage: %v", err)
					}
					if v.ExpectedUpdateUsage == nil && (norm.InputTokens != v.ExpectedUsage.InputTokens || norm.OutputTokens != v.ExpectedUsage.OutputTokens || norm.CostUSD != v.ExpectedUsage.CostUSD) {
						t.Fatalf("normalized usage = %+v, want %+v", norm, v.ExpectedUsage)
					}
				}
				if v.ExpectedUpdateUsage != nil && !found {
					t.Fatal("no usage update decoded")
				}
			},
		},
		ConformanceCase{
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
				if modes, ok := rt.(RuntimeModes); ok && modes.Modes() != nil && modes.Modes().CurrentModeID == modeID {
					return
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
		ConformanceCase{
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
		ConformanceCase{
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
		ConformanceCase{
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
		ConformanceCase{
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
	)
}
