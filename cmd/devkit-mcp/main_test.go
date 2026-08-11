package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunProxyMissingSocket(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	// Ensure DevKit dir exists but no socket.
	if err := os.MkdirAll(filepath.Join(cfg, "DevKit"), 0o700); err != nil {
		t.Fatal(err)
	}

	err := runProxy()
	if err == nil {
		t.Fatal("expected error when socket is missing")
	}
	if !strings.Contains(err.Error(), "DevKit is not running or MCP host is unavailable") {
		t.Fatalf("error = %q, want clear unavailable message", err)
	}
}
