// internal/codegraph/analyzer_subprocess.go
package codegraph

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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
	// actually returns.
	cmd.WaitDelay = 5 * time.Second
	if out, err := cmd.CombinedOutput(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return AnalysisResult{}, fmt.Errorf("codegraph: %s analyzer gave up after %s: %w (output: %s)", a.toolchain.Language, analyzerTimeout, ctxErr, out)
		}
		return AnalysisResult{}, fmt.Errorf("codegraph: %s analyzer subprocess failed: %w (output: %s)", a.toolchain.Language, err, out)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		return AnalysisResult{}, fmt.Errorf("codegraph: read analyzer output: %w", err)
	}

	var result AnalysisResult
	if err := json.Unmarshal(data, &result); err != nil {
		return AnalysisResult{}, fmt.Errorf("codegraph: parse analyzer output: %w", err)
	}
	return result, nil
}
