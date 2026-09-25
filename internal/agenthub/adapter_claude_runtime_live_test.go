package agenthub_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/agentruntime"
)

// TestAgentRuntimeLiveInstall installs the tested claude-agent-acp for real —
// the node and npm this machine has, the npm registry, ~250MB with the Claude
// binary — into a temporary root, verifies it with the sandboxed initialize
// probe, and probes the activated copy once more through Resolve.
//
// It is the only test that proves the install flow end to end: the flags npm
// accepts, the tree it writes, and that the jail's read grants are enough for
// node to load the adapter. Everything else fakes one of those.
//
//	DEVKIT_AGENT_INSTALL_TEST=1 go test ./internal/agenthub/ -run TestAgentRuntimeLiveInstall -v -timeout 15m
func TestAgentRuntimeLiveInstall(t *testing.T) {
	if os.Getenv("DEVKIT_AGENT_INSTALL_TEST") != "1" {
		t.Skip("set DEVKIT_AGENT_INSTALL_TEST=1 to install claude-agent-acp for real (network, ~250MB)")
	}

	root := filepath.Join(t.TempDir(), "runtimes")
	manager := agentruntime.New(root)
	manager.OnOutput = func(_, line string) { t.Log("npm: " + line) }
	adapter := agenthub.NewClaudeAdapter(agenthub.ClaudeOptions{
		Runtime: func() (agentruntime.Resolved, error) { return manager.Resolve(agentruntime.ClaudeACP) },
	})
	manager.Verifiers = map[string]func(context.Context, agentruntime.Resolved) error{
		agentruntime.ClaudeACP: adapter.Probe,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	if err := manager.Install(ctx, agentruntime.ClaudeACP, ""); err != nil {
		t.Fatalf("Install: %v", err)
	}

	st, err := manager.Status(ctx, agentruntime.ClaudeACP)
	if err != nil {
		t.Fatal(err)
	}
	if st.Current == nil || st.Current.Version != st.TestedVersion {
		t.Fatalf("current = %+v, want the tested version %s", st.Current, st.TestedVersion)
	}
	t.Logf("installed %s (sdk %s) with node %s; bundled claude %s; %d bytes",
		st.Current.Version, st.Current.SDKVersion, st.Current.NodeVersion, st.Current.BundledClaude, st.DiskBytes)

	rt, err := manager.Resolve(agentruntime.ClaudeACP)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if err := adapter.Probe(ctx, rt); err != nil {
		t.Fatalf("Probe of the activated install: %v", err)
	}
}
