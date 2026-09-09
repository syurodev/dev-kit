// internal/codegraph/locate.go
package codegraph

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
)

// Finding the programs a toolchain needs, wherever they are.
//
// PATH is not enough and never was. Developers get their toolchains from IDEs,
// version managers and language-specific installers, most of which never touch
// PATH: a JDK inside IntelliJ, a Node inside nvm, a Python inside pyenv. A
// toolchain that reports "not installed" to someone who uses that language
// every day is both wrong and unfixable-looking.
//
// So this file is the vocabulary for saying where to look, and each language's
// own file (toolchain_java.go and its future siblings) does the saying.
// Nothing here knows about any particular language.

// candidate is one place a program might live, with a name for where that is so
// the UI can say which one it picked. "Using the JDK bundled with your IDE" is
// a very different message from "java is missing".
type candidate struct {
	path   string
	source string
}

// maxCandidates bounds how many programs get their version probed. Each probe
// is a process, and a glob over a directory of SDKs on a busy machine can match
// more than anyone wants to spawn. Candidates are ordered best-guess first, so
// a truncated list is still the right list.
const maxCandidates = 24

// search collects candidates in preference order. A language's locator builds
// one, calls the helpers below in the order it wants them tried, and returns
// results().
type search struct {
	found []candidate
}

// at adds explicit paths. Empty ones are skipped, so a caller can pass the
// result of os.Getenv without checking it first.
func (s *search) at(source string, paths ...string) {
	for _, path := range paths {
		if path != "" {
			s.found = append(s.found, candidate{path: path, source: source})
		}
	}
}

// glob adds every match of each pattern, newest-looking first.
//
// Reverse lexical order is a deliberate approximation of "newest": a directory
// of SDKs holds names like jdk-17 and jdk-21, and probing 21 before 17 means a
// truncated candidate list still contains the one that will win. It is only an
// ordering hint — probe picks the actual best by version.
func (s *search) glob(source string, patterns ...string) {
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		sort.Sort(sort.Reverse(sort.StringSlice(matches)))
		s.at(source, matches...)
	}
}

// envHome adds <$VAR>/bin/<exe> for each named variable — the shape every
// *_HOME convention uses.
func (s *search) envHome(exe string, variables ...string) {
	for _, variable := range variables {
		if home := os.Getenv(variable); home != "" {
			s.at(variable, filepath.Join(home, "bin", exe))
		}
	}
}

// onPath adds whatever the shell would run.
func (s *search) onPath(exe string) {
	if path, err := exec.LookPath(exe); err == nil {
		s.at("PATH", path)
	}
}

// userHome is the user's home directory, or "" when there is none. Locators
// guard on it rather than building paths under an empty root.
func userHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

// results drops candidates that are not there or not runnable, de-duplicates
// what is left by resolved path, and caps the count.
func (s *search) results() []candidate {
	seen := make(map[string]bool, len(s.found))
	kept := make([]candidate, 0, len(s.found))
	for _, c := range s.found {
		resolved, err := filepath.Abs(c.path)
		if err != nil {
			resolved = c.path
		}
		if seen[resolved] {
			continue
		}
		info, err := os.Stat(resolved)
		if err != nil || info.IsDir() {
			continue
		}
		// The executable bit does not exist on Windows, where being there with
		// the right extension is the whole test.
		if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
			continue
		}
		seen[resolved] = true
		c.path = resolved
		kept = append(kept, c)
		if len(kept) == maxCandidates {
			break
		}
	}
	return kept
}

// programKind says which extension a program carries on Windows. The two cases
// are real and different: a compiled tool ships as .exe, while a launcher
// written as a batch script ships as .cmd (Maven and npm both do). Everywhere
// else the name is unchanged.
type programKind int

const (
	// nativeProgram is a compiled executable — java, node, python.
	nativeProgram programKind = iota
	// scriptProgram is a launcher script — mvn, npm, gradle.
	scriptProgram
)

func programName(base string, kind programKind) string {
	if runtime.GOOS != "windows" {
		return base
	}
	if kind == scriptProgram {
		return base + ".cmd"
	}
	return base + ".exe"
}
