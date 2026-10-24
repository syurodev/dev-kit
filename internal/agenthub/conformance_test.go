package agenthub

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
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

var conformanceCases []ConformanceCase

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

func TestConformance_LiveConfiguredAdapters(t *testing.T) {
	for _, tc := range []struct {
		name, env string
		adapter   func(string) VendorAdapter
	}{
		{"codex", "DEVKIT_CODEX_COMMAND", func(command string) VendorAdapter { return NewCodexAdapter(CodexOptions{Command: command}) }},
		{"opencode", "DEVKIT_OPENCODE_COMMAND", func(command string) VendorAdapter { return NewOpenCodeAdapter(OpenCodeOptions{Command: command}) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			command := os.Getenv(tc.env)
			if command == "" {
				t.Skipf("set %s to run the live adapter check", tc.env)
			}
			a := tc.adapter(command)
			var spec OpenSpec
			if tc.name == "opencode" {
				sharedHome := false
				spec.PrivateHome = &sharedHome
			}
			spec.Cwd = t.TempDir()
			rt, err := a.Open(context.Background(), spec)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer rt.Close(context.Background())
			if _, err := rt.Prompt(context.Background(), TextPrompt("Reply with the single word OK."), nil); err != nil {
				t.Fatalf("Prompt: %v", err)
			}
		})
	}
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

func assertWireField(t *testing.T, v VendorQuirks, method, key, want string) {
	t.Helper()
	if v.wireLogPath == nil || v.wireLogPath() == "" {
		return
	}
	raw, err := os.ReadFile(v.wireLogPath())
	if err != nil {
		t.Fatalf("read adapter wire log: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var frame struct {
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if json.Unmarshal([]byte(line), &frame) == nil && frame.Method == method && fmt.Sprint(frame.Params[key]) == want {
			return
		}
	}
	t.Fatalf("wire log has no %s with %s=%q: %s", method, key, want, strings.TrimSpace(string(raw)))
}

func assertPermissionWireAnswer(t *testing.T, v VendorQuirks, allow bool) {
	t.Helper()
	if v.wireLogPath == nil || v.wireLogPath() == "" {
		return
	}
	raw, err := os.ReadFile(v.wireLogPath())
	if err != nil {
		t.Fatalf("read adapter wire log: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var frame map[string]json.RawMessage
		if json.Unmarshal([]byte(line), &frame) != nil || len(frame["id"]) == 0 {
			continue
		}
		if allow && (strings.Contains(string(frame["result"]), `"selected"`) || strings.Contains(string(frame["result"]), `"accept"`)) {
			return
		}
		if !allow && (len(frame["error"]) > 0 || strings.Contains(string(frame["result"]), `"cancelled"`) || strings.Contains(string(frame["result"]), `"cancel"`) || strings.Contains(string(frame["result"]), `"reject"`) || strings.Contains(string(frame["result"]), `"decline"`)) {
			return
		}
	}
	want := "deny/cancel answer"
	if allow {
		want = "allow/accept answer"
	}
	t.Fatalf("wire log has no serialized %s: %s", want, strings.TrimSpace(string(raw)))
}

func valueOfConfigOption(rt SessionRuntime, id string) string {
	var opts []ConfigOption
	switch r := rt.(type) {
	case *acpRuntime:
		r.mu.Lock()
		opts = append(opts, r.options...)
		r.mu.Unlock()
	case *openCodeRuntime:
		r.acpRuntime.mu.Lock()
		opts = append(opts, r.acpRuntime.options...)
		r.acpRuntime.mu.Unlock()
	case *mockRuntime:
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.setOptions[id]
	case *codexRuntime:
		r.mu.Lock()
		defer r.mu.Unlock()
		if id == codexModelOption {
			return r.model
		}
		if id == codexEffortOption {
			return r.effort
		}
	}
	for _, opt := range opts {
		if opt.ID == id {
			var value string
			_ = json.Unmarshal(opt.CurrentValue, &value)
			return value
		}
	}
	return ""
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
