package codegraph

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
)

// identifiedAnalyzer is an Analyzer that can say what it is, precisely enough
// that two indexes stamped with the same identity were written by the same
// code (workspace.mdx § Registry, "Mở lại không được chặn truy vấn").
type identifiedAnalyzer interface {
	AnalyzerIdentity() string
}

// analyzerIdentity is the identity an index written by a is stamped with, or
// "" when a cannot name itself. "" never matches anything, so an analyzer
// that cannot say what it is keeps the old rule: a reopened index is not
// served until it has been refreshed.
func analyzerIdentity(a Analyzer) string {
	if id, ok := a.(identifiedAnalyzer); ok {
		return id.AnalyzerIdentity()
	}
	return ""
}

// identity names what this toolchain's analyzer is right now: its language,
// the artifact it would run (path, size and modification time, so a rebuilt
// jar or a new tsgo is a different analyzer), and the DevKit build — the Go
// side turns the analyzer's output into the index, so a new build of DevKit
// can change an index without the artifact changing. A native analyzer has
// no artifact; it is exactly the DevKit build. "" when either part is
// unknown: no artifact found, or a build that cannot name itself.
func (t Toolchain) identity() string {
	build := buildIdentity()
	if build == "" {
		return ""
	}
	if t.Native != nil {
		return t.Language + "|" + build
	}
	artifact := t.installedArtifact()
	if artifact == "" {
		return ""
	}
	info, err := os.Stat(artifact)
	if err != nil {
		return ""
	}
	if abs, err := filepath.Abs(artifact); err == nil {
		artifact = abs
	}
	return fmt.Sprintf("%s|%s|%d|%d|%s", t.Language, artifact, info.Size(), info.ModTime().UnixNano(), build)
}

// toolchainIdentified wraps the analyzer a toolchain builds so it can name
// itself. A pointer, so two analyzers compare by identity like the ones they
// wrap did; and it forwards CloseAnalyzer, which Service relies on to stop a
// language server.
type toolchainIdentified struct {
	Analyzer
	toolchain Toolchain
}

func (a *toolchainIdentified) AnalyzerIdentity() string { return a.toolchain.identity() }

func (a *toolchainIdentified) CloseAnalyzer() error {
	if closer, ok := a.Analyzer.(analyzerCloser); ok {
		return closer.CloseAnalyzer()
	}
	return nil
}

// buildIdentity names the running DevKit build: the VCS revision it was built
// from (plus the executable's size and time when the tree was modified, since
// two dirty builds share a revision), or the executable alone when the build
// carries no VCS stamp — `go test`, a build outside a checkout. "" only when
// neither can be read.
var buildIdentity = sync.OnceValue(func() string {
	var revision string
	modified := false
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				revision = setting.Value
			case "vcs.modified":
				modified = setting.Value == "true"
			}
		}
	}
	if revision != "" && !modified {
		return revision
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	info, err := os.Stat(exe)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%s+exe:%d:%d", revision, info.Size(), info.ModTime().UnixNano())
})
