package agenthub

import (
	"context"
	"os"
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
