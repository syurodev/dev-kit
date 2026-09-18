package codegraph

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestTailBufferKeepsOnlyTheEnd(t *testing.T) {
	buf := newTailBuffer(10)
	buf.Write([]byte("0123456789"))
	buf.Write([]byte("abc"))
	if got := buf.String(); got != "…3456789abc" {
		t.Fatalf("tail = %q", got)
	}
	buf.Write([]byte(strings.Repeat("x", 50) + "END"))
	if got := buf.String(); got != "…xxxxxxxEND" {
		t.Fatalf("tail after an oversized write = %q", got)
	}
	small := newTailBuffer(10)
	small.Write([]byte("ok"))
	if small.String() != "ok" {
		t.Fatalf("short output must not be marked truncated: %q", small.String())
	}
}

// A chatty analyzer that fails must not paste megabytes into the error.
func TestAnalyzerErrorCarriesOnlyTheTailOfItsOutput(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh on this platform")
	}
	toolchain := writingToolchain(t, "true")
	script := `i=0; while [ $i -lt 20000 ]; do echo "noise line $i"; i=$((i+1)); done; echo "the real failure" >&2; exit 3`
	if err := os.WriteFile(toolchain.Artifact, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := toolchain.Analyzer().Analyze(context.Background(), t.TempDir())
	if err == nil {
		t.Fatal("a failing analyzer must fail")
	}
	msg := err.Error()
	if !strings.Contains(msg, "the real failure") {
		t.Fatalf("the error lost the end of the output, where the failure is: %.200s", msg)
	}
	if len(msg) > maxAnalyzerOutput+1024 {
		t.Fatalf("error is %d bytes, want the output capped near %d", len(msg), maxAnalyzerOutput)
	}
}

// CancelAnalyzerBuild stops a build in flight — including a grandchild it
// forked, which a plain kill of the launcher used to leave running and holding
// the output pipe.
func TestCancelAnalyzerBuildStopsTheWholeBuild(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh on this platform")
	}
	service := NewService(nilStore{}, NewSubprocessAnalyzer())
	started := make(chan struct{}, 1)
	service.OnAnalyzerBuildOutput = func(_, line string) {
		if line == "started" {
			started <- struct{}{}
		}
	}
	toolchain := writingToolchain(t, `sleep 600 & echo started; wait`)
	service.SetToolchains([]Toolchain{toolchain})

	done := make(chan error, 1)
	go func() { done <- service.BuildAnalyzer(context.Background(), "fake") }()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("the build never started")
	}
	begin := time.Now()
	service.CancelAnalyzerBuild()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled build = %v, want context.Canceled", err)
		}
		// Well under WaitDelay: the group kill took the sleeping grandchild
		// with it, so nothing was left holding the pipe.
		if elapsed := time.Since(begin); elapsed > 4*time.Second {
			t.Fatalf("cancel took %v; the grandchild outlived the kill", elapsed)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("BuildAnalyzer did not return after CancelAnalyzerBuild")
	}
	// Nothing building: a no-op, not a panic.
	service.CancelAnalyzerBuild()
}
