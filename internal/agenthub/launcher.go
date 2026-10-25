package agenthub

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"devkit/internal/sandbox"
)

// Launcher is the shared boundary for starting a vendor process inside the
// sandbox. Vendor adapters continue to build their own policy and command.
type Launcher struct {
	Runner SandboxRunner
}

// LaunchedProcess contains the process and its validated protocol pipes.
type LaunchedProcess struct {
	Proc   *sandbox.Proc
	Stdin  io.WriteCloser
	Stdout io.ReadCloser
}

// Start starts a process under the supplied policy and reaps it if the runner
// fails to provide the stdio pipes required by vendor protocols.
func (l Launcher) Start(ctx context.Context, policy sandbox.Policy, command sandbox.Cmd) (*LaunchedProcess, error) {
	runner := l.Runner
	if runner == nil {
		runner = sandbox.DefaultRunner()
	}
	proc, err := runner.StartPiped(ctx, policy, command)
	if err != nil {
		return nil, mapSandboxErr(err)
	}
	stdin, stdout := proc.StdinWriter(), proc.StdoutReader()
	if stdin == nil || stdout == nil {
		_ = proc.Kill()
		_ = proc.Wait()
		return nil, fmt.Errorf("%w: StartPiped missing stdio pipes", ErrProcessFailed)
	}
	return &LaunchedProcess{Proc: proc, Stdin: stdin, Stdout: stdout}, nil
}

// logStderrTail records the useful tail of a vendor process's stderr after an
// exit. The reason returned to the caller is kept short; surrounding lines
// often explain the failure.
func logStderrTail(stage string, proc *sandbox.Proc) {
	_, stderr := proc.Output()
	tail := strings.TrimSpace(string(stderr))
	if tail == "" {
		log.Printf("agenthub: %s: agent exited with nothing on stderr", stage)
		return
	}
	if len(tail) > 2000 {
		tail = "…" + tail[len(tail)-2000:]
	}
	log.Printf("agenthub: %s: agent stderr:\n%s", stage, tail)
}

// stderrReason stops and reaps a dead or unresponsive vendor process, then
// returns the last error-like stderr line, if any.
func stderrReason(proc *sandbox.Proc) string {
	_ = proc.Kill()
	waited := make(chan struct{})
	go func() {
		_ = proc.Wait()
		close(waited)
	}()
	select {
	case <-waited:
	case <-time.After(2 * time.Second):
		return "" // the buffer is still being written; reading it would race
	}
	_, stderr := proc.Output()
	var reason string
	for _, line := range strings.Split(string(stderr), "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "Error") || strings.Contains(line, "error") {
			reason = line
		}
	}
	if reason == "" {
		return ""
	}
	if len(reason) > 200 {
		reason = reason[:200]
	}
	return ": " + reason
}
