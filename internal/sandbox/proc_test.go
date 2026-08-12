package sandbox

import (
	"bytes"
	"context"
	"os/exec"
	"testing"
)

func TestProcOutputReturnsCapturedBuffers(t *testing.T) {
	cmd := exec.CommandContext(context.Background(), "sh", "-c", "printf stdout; printf stderr >&2")
	proc := NewProcWithBuffers(cmd, &bytes.Buffer{}, &bytes.Buffer{})
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := proc.Wait(); err != nil {
		t.Fatal(err)
	}

	stdout, stderr := proc.Output()
	if string(stdout) != "stdout" {
		t.Fatalf("stdout = %q, want %q", stdout, "stdout")
	}
	if string(stderr) != "stderr" {
		t.Fatalf("stderr = %q, want %q", stderr, "stderr")
	}
}
