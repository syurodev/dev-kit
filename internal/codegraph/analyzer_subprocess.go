// internal/codegraph/analyzer_subprocess.go
package codegraph

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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

func (a toolchainAnalyzer) Analyze(ctx context.Context, projectPath string) (AnalysisResult, error) {
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
	if out, err := cmd.CombinedOutput(); err != nil {
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
