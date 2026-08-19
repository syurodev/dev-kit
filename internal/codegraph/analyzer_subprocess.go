// internal/codegraph/analyzer_subprocess.go
package codegraph

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
)

// subprocessAnalyzer invokes the bundled Java analyzer CLI (see
// docs/superpowers/plans/2026-08-19-codegraph-java-analyzer.md) as a
// one-shot subprocess: `java -jar <jarPath> <projectPath> <outputPath>`,
// then reads and parses the index.json it writes. An empty jarPath means
// "not configured yet" (e.g. the jar hasn't been built/bundled) — Analyze
// fails clearly rather than trying to exec an empty path.
type subprocessAnalyzer struct {
	jarPath string
}

func NewSubprocessAnalyzer() Analyzer {
	return subprocessAnalyzer{}
}

// NewSubprocessAnalyzerWithJar points the analyzer at a specific built jar.
// Production wiring (internal/app/container.go) will resolve the real
// bundled jar path once packaging/distribution is decided — a separate,
// later concern from getting the subprocess invocation itself correct.
func NewSubprocessAnalyzerWithJar(jarPath string) Analyzer {
	return subprocessAnalyzer{jarPath: jarPath}
}

func (a subprocessAnalyzer) Analyze(ctx context.Context, projectPath string) (AnalysisResult, error) {
	if a.jarPath == "" {
		return AnalysisResult{}, ErrAnalyzerNotConfigured
	}

	outputFile, err := os.CreateTemp("", "codegraph-index-*.json")
	if err != nil {
		return AnalysisResult{}, fmt.Errorf("codegraph: create temp output file: %w", err)
	}
	outputPath := outputFile.Name()
	outputFile.Close()
	defer os.Remove(outputPath)

	cmd := exec.CommandContext(ctx, "java", "-jar", a.jarPath, projectPath, outputPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return AnalysisResult{}, fmt.Errorf("codegraph: analyzer subprocess failed: %w (output: %s)", err, out)
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
