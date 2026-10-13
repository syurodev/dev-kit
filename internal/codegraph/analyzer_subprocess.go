// internal/codegraph/analyzer_subprocess.go
package codegraph

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Running a built analyzer as a one-shot subprocess.
//
// The contract every language's analyzer honours: it is invoked with a project
// directory and an output path, writes an index as JSON to that path, and
// exits. Nothing here knows which language, which runtime, or which artifact —
// the Toolchain says all three (see Toolchain.Run and the ${...} placeholders),
// so a second language needs no change in this file.

// toolchainAnalyzer runs whatever artifact its toolchain currently resolves to.
//
// It holds the toolchain rather than a path because both change under it: the
// artifact appears when a build finishes, and the runtime appears when the user
// installs a JDK. Resolving per analysis means either takes effect immediately,
// and costs one extra process on a call that already spawns one.
type toolchainAnalyzer struct {
	toolchain Toolchain
}

// unconfiguredAnalyzer stands in where no toolchain has been wired at all — the
// in-memory container, every test double. It fails clearly rather than trying
// to exec an empty path.
type unconfiguredAnalyzer struct{}

// NewSubprocessAnalyzer returns an analyzer with nothing behind it. Service
// starts here and is given a real one once a toolchain is registered.
func NewSubprocessAnalyzer() Analyzer {
	return unconfiguredAnalyzer{}
}

func (unconfiguredAnalyzer) Analyze(context.Context, string) (AnalysisResult, error) {
	return AnalysisResult{}, ErrAnalyzerNotConfigured
}

// analyzerTimeout bounds one analysis when the caller set no deadline of its
// own, which is every caller today: the bridge and the background re-index both
// start from context.Background().
//
// Generous, because a cold monorepo is minutes of real work — the number is not
// a performance target, it is the line past which the process is not working
// any more. Without it a wedged analyzer is unbounded and invisible: a JVM
// handed a bad JAVA_HOME spins forever, the parent waits forever, and quitting
// the app orphans the child rather than ending it.
const analyzerTimeout = 10 * time.Minute

func (a toolchainAnalyzer) Analyze(ctx context.Context, projectPath string) (AnalysisResult, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, analyzerTimeout)
		defer cancel()
	}

	outputFile, err := os.CreateTemp("", "codegraph-index-*.json")
	if err != nil {
		return AnalysisResult{}, fmt.Errorf("codegraph: create temp output file: %w", err)
	}
	outputPath := outputFile.Name()
	outputFile.Close()
	defer os.Remove(outputPath)

	argv, env, err := a.toolchain.analyzerCommand(ctx, projectPath, outputPath)
	if err != nil {
		return AnalysisResult{}, err
	}

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	// The toolchain's own environment, not whatever the app inherited: the
	// runtime that was discovered may live somewhere PATH has never heard of,
	// and an analyzer that shells out again needs to find it.
	cmd.Env = env
	// CommandContext kills the process on cancellation but then waits on the
	// output pipes, which a grandchild holding them open can keep open forever.
	// WaitDelay puts a bound on that second wait, so a cancelled analysis
	// actually returns; killing the process group means there is no such
	// grandchild left to wait for in the first place.
	cmd.WaitDelay = 5 * time.Second
	killProcessGroup(cmd)
	// Only the tail of what the analyzer prints is kept, for the error. The
	// index goes to the output file; a chatty analyzer logging a line per file
	// on a large project would otherwise be buffered whole, and then pasted
	// whole into an error message.
	output := newTailBuffer(maxAnalyzerOutput)
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return AnalysisResult{}, fmt.Errorf("codegraph: %s analyzer stopped: %w (output: %s)", a.toolchain.Language, ctxErr, output)
		}
		return AnalysisResult{}, fmt.Errorf("codegraph: %s analyzer subprocess failed: %w (output: %s)", a.toolchain.Language, err, output)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		return AnalysisResult{}, fmt.Errorf("codegraph: read analyzer output: %w", err)
	}

	var out subprocessOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return AnalysisResult{}, fmt.Errorf("codegraph: parse analyzer output: %w", err)
	}
	result := out.AnalysisResult
	for _, ep := range out.Endpoints {
		result.EntryPoints = append(result.EntryPoints, EntryPoint{
			ID: ep.ID, Verb: ep.HTTPMethod, Target: ep.Path, EntryNode: ep.EntryNode, Kind: EntryKindHTTP,
		})
	}
	return result, nil
}

// subprocessOutput is the file the Java analyzer writes. Non-HTTP entryPoints
// already use AnalysisResult's additive wire fields. Its legacy endpoints are
// HTTP routes and keep the jar's own field names: a jar already installed
// beside an older or newer DevKit must still read, so the wire format is the
// jar's, and only this is translated.
type subprocessOutput struct {
	AnalysisResult
	Endpoints []struct {
		ID         string `json:"id"`
		HTTPMethod string `json:"httpMethod"`
		Path       string `json:"path"`
		EntryNode  string `json:"entryNode"`
	} `json:"endpoints"`
}

// maxAnalyzerOutput is how much of a subprocess's output is kept for an error
// message — the end of it, where the failure is.
const maxAnalyzerOutput = 64 << 10

// tailBuffer keeps the last max bytes written to it. Safe for the concurrent
// writes exec makes when Stdout and Stderr are the same writer.
type tailBuffer struct {
	mu        sync.Mutex
	buf       []byte
	max       int
	truncated bool
}

func newTailBuffer(max int) *tailBuffer {
	return &tailBuffer{max: max}
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := len(p)
	if len(p) >= t.max {
		t.buf = append(t.buf[:0], p[len(p)-t.max:]...)
		t.truncated = true
		return n, nil
	}
	if over := len(t.buf) + len(p) - t.max; over > 0 {
		t.buf = append(t.buf[:0], t.buf[over:]...)
		t.truncated = true
	}
	t.buf = append(t.buf, p...)
	return n, nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.truncated {
		return "…" + string(t.buf)
	}
	return string(t.buf)
}
