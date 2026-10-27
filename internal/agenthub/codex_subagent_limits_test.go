package agenthub

import (
	"context"
	"slices"
	"strings"
	"testing"

	"devkit/internal/preferences"
)

func intp(n int) *int { return &n }

// Measured on the real app-server 0.163.0 (cmd/pdfe2e/subagent_limits_test.go):
// the v2 key counts the main agent, agents.max_threads does not, agents.max_threads=0
// stops Codex starting, and agents.max_depth does nothing — so it is never written.
func TestCodexSubagentLimitArgs(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   *int
		want []string
	}{
		{"unset writes nothing", nil, nil},
		{"three", intp(3), []string{"-c", "features.multi_agent_v2.max_concurrent_threads_per_session=4", "-c", "agents.max_threads=3"}},
		{"none: only the v2 key, agents.max_threads=0 would stop Codex", intp(0), []string{"-c", "features.multi_agent_v2.max_concurrent_threads_per_session=1"}},
		{"capped", intp(500), []string{"-c", "features.multi_agent_v2.max_concurrent_threads_per_session=17", "-c", "agents.max_threads=16"}},
		{"negative is none", intp(-4), []string{"-c", "features.multi_agent_v2.max_concurrent_threads_per_session=1"}},
	} {
		got := codexSubagentLimitArgs(SubagentLimits{MaxSubagents: tc.in})
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: args = %v, want %v", tc.name, got, tc.want)
		}
		for _, a := range got {
			if strings.Contains(a, "max_depth") {
				t.Errorf("%s: max_depth is ignored by Codex's app-server and must not be written: %v", tc.name, got)
			}
		}
	}
}

func TestOnlyCodexSessionsGetTheSubagentLimit(t *testing.T) {
	svc := NewService(NewRegistry(), newInternalStore(t), nil)
	svc.SubagentLimits = func() (preferences.AgentSubagentLimits, error) {
		return preferences.AgentSubagentLimits{Codex: preferences.CodexSubagentLimits{MaxSubagents: intp(2)}}, nil
	}
	var codex, claude OpenSpec
	svc.applySubagents(&codex, AgentTypeCodex)
	svc.applySubagents(&claude, AgentTypeClaude)
	if codex.SubagentLimits.MaxSubagents == nil || *codex.SubagentLimits.MaxSubagents != 2 {
		t.Fatalf("codex spec = %+v", codex.SubagentLimits)
	}
	if claude.SubagentLimits.MaxSubagents != nil {
		t.Fatalf("Claude has no such control, yet its spec carries %d", *claude.SubagentLimits.MaxSubagents)
	}

	// Unset, or unreadable, is no limit: DevKit passes Codex nothing.
	svc.SubagentLimits = func() (preferences.AgentSubagentLimits, error) { return preferences.AgentSubagentLimits{}, nil }
	var unset OpenSpec
	svc.applySubagents(&unset, AgentTypeCodex)
	if unset.SubagentLimits.MaxSubagents != nil {
		t.Fatalf("unset limit became %d", *unset.SubagentLimits.MaxSubagents)
	}
	svc.SubagentLimits = nil
	svc.applySubagents(&unset, AgentTypeCodex)
	if unset.SubagentLimits.MaxSubagents != nil {
		t.Fatal("no reader, yet a limit")
	}
}

// The limit rides on the process's argv — a session keeps what it started with —
// and never in the config.toml every session shares.
func TestCodexAppServerStartsWithTheSubagentLimit(t *testing.T) {
	f := newCodexFixture(t, "turn")
	rt, err := f.adapter.Open(context.Background(), OpenSpec{Cwd: f.cwd, SubagentLimits: SubagentLimits{MaxSubagents: intp(2)}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rt.Close(context.Background())
	_, cmd := f.runner.started()
	joined := strings.Join(cmd.Args, " ")
	for _, want := range []string{"features.multi_agent_v2.max_concurrent_threads_per_session=3", "agents.max_threads=2"} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv lacks %s: %v", want, cmd.Args)
		}
	}
}

func TestCodexAppServerStartsWithoutALimitByDefault(t *testing.T) {
	f := newCodexFixture(t, "turn")
	rt, err := f.adapter.Open(context.Background(), OpenSpec{Cwd: f.cwd})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rt.Close(context.Background())
	_, cmd := f.runner.started()
	if joined := strings.Join(cmd.Args, " "); strings.Contains(joined, "max_threads") || strings.Contains(joined, "max_concurrent_threads") {
		t.Errorf("an unset limit reached argv: %v", cmd.Args)
	}
}

// Agents may read nothing about the limits and change nothing: no MCP tool
// mentions them, and the prompt does not teach one.
func TestNoToolTouchesSubagentLimits(t *testing.T) {
	for name := range mcpToolNames(t) {
		lower := strings.ToLower(name)
		if strings.Contains(lower, "subagentlimit") || strings.Contains(lower, "maxsubagent") {
			t.Errorf("tool %s touches the subagent limits; only the window may", name)
		}
	}
	if prompt := devkitPrompt(claudeNaming, "", nil); strings.Contains(strings.ToLower(prompt), "subagent limit") {
		t.Error("the prompt tells agents about the limits")
	}
}
