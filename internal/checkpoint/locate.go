// internal/checkpoint/locate.go
package checkpoint

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Finding a git the app may actually run.
//
// Same problem internal/agentruntime/locate.go solves for node: an app opened
// from Finder or the Dock inherits launchd's PATH, which is not the developer's
// PATH. The extra wrinkle here is macOS's /usr/bin/git, which is not git at all
// but a stub that pops the "install the command line developer tools" dialog
// when the tools are absent. Running it to ask its version would put that
// dialog in front of a user who only opened an agent session, so the stub is
// probed by looking for the toolchain behind it, never by executing it.

// MinGitVersion is the floor the engine needs. Chosen for `git update-index
// --stdin -z` + `checkout-index --stdin -z` + `diff-tree --numstat -z` being
// stable together, which they are well before this; it is a "this is ancient,
// stop" line rather than a feature line.
const MinGitVersion = "2.26"

// gitProbeTimeout bounds one `git --version`.
const gitProbeTimeout = 10 * time.Second

// Availability is what the rest of the app asks: can checkpoints run, and if
// not, what should the Settings row tell the user to do about it.
type Availability struct {
	Path    string
	Version string
	// Reason is empty when Path is set. Otherwise it is a sentence for the UI,
	// not an error string — it names the fix where there is one.
	Reason string
}

// OK reports whether a usable git was found.
func (a Availability) OK() bool { return a.Path != "" }

// Injection points for the tests: the real machine must not decide whether the
// shim case is covered.
var (
	gitGOOS = runtime.GOOS
	// gitShimPath is macOS's stub. Never executed unless a real toolchain is
	// found behind it.
	gitShimPath = "/usr/bin/git"
	// gitToolchainPaths are the real gits the stub dispatches to. Their
	// existence is the test for "the Command Line Tools are installed".
	gitToolchainPaths = []string{
		"/Library/Developer/CommandLineTools/usr/bin/git",
		"/Applications/Xcode.app/Contents/Developer/usr/bin/git",
	}
	gitSearchDirs = []string{"/opt/homebrew/bin", "/usr/local/bin"}
	gitLookPath   = exec.LookPath
)

// Locate picks the first git that answers with a new enough version.
//
// Unlike node, where newest wins, the first working one wins: any git that
// clears the floor writes the same trees, so there is nothing to gain from
// spawning more processes.
func Locate(ctx context.Context) Availability {
	var (
		sawOld     Availability
		sawShim    bool
		candidates = gitCandidates()
	)
	for _, path := range candidates {
		if gitGOOS == "darwin" && sameFile(path, gitShimPath) && !commandLineToolsInstalled() {
			// Executing this is what opens the install dialog. Remember that it
			// was the only thing on offer so the reason can say so.
			sawShim = true
			continue
		}
		version, err := probeGit(ctx, path)
		if err != nil {
			continue
		}
		if compareGitVersions(version, MinGitVersion) < 0 {
			if sawOld.Version == "" || compareGitVersions(version, sawOld.Version) > 0 {
				sawOld = Availability{Version: version, Reason: gitTooOldReason(version)}
			}
			continue
		}
		return Availability{Path: path, Version: version}
	}
	switch {
	case sawOld.Version != "":
		return sawOld
	case sawShim:
		return Availability{Reason: "Git needs Apple's Command Line Tools. Run `xcode-select --install`, then reopen DevKit."}
	default:
		return Availability{Reason: "Git was not found, so file checkpoints are off."}
	}
}

func gitTooOldReason(version string) string {
	return fmt.Sprintf("Git %s is older than %s, so file checkpoints are off.", version, MinGitVersion)
}

// gitCandidates lists where a git may be, in preference order: the places a
// developer's own git is installed first, PATH after them, and the macOS stub
// last so a real git always outranks it.
func gitCandidates() []string {
	name := "git"
	if gitGOOS == "windows" {
		name = "git.exe"
	}
	var out []string
	if gitGOOS == "windows" {
		for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA"} {
			if root := os.Getenv(env); root != "" {
				out = append(out, filepath.Join(root, "Git", "cmd", name))
			}
		}
	} else {
		for _, dir := range gitSearchDirs {
			out = append(out, filepath.Join(dir, name))
		}
	}
	if path, err := gitLookPath(name); err == nil {
		out = append(out, path)
	}
	if gitGOOS == "darwin" {
		out = append(out, gitShimPath)
	}
	return dedupeStrings(out)
}

// commandLineToolsInstalled reports whether anything real sits behind the stub.
func commandLineToolsInstalled() bool {
	for _, path := range gitToolchainPaths {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

// sameFile compares two paths without needing them to be spelled the same way:
// PATH may hand back /usr/bin/git through a symlinked directory.
func sameFile(a, b string) bool {
	if a == b {
		return true
	}
	infoA, err := os.Lstat(a)
	if err != nil {
		return false
	}
	infoB, err := os.Lstat(b)
	if err != nil {
		return false
	}
	return os.SameFile(infoA, infoB)
}

var gitVersionPattern = regexp.MustCompile(`([0-9]+)\.([0-9]+)(?:\.([0-9]+))?`)

func probeGit(ctx context.Context, path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", fmt.Errorf("checkpoint: %s is not a program", path)
	}
	probeCtx, cancel := context.WithTimeout(ctx, gitProbeTimeout)
	defer cancel()
	out, err := exec.CommandContext(probeCtx, path, "--version").Output()
	if err != nil {
		return "", err
	}
	version := gitVersionPattern.FindString(string(out))
	if version == "" {
		return "", fmt.Errorf("checkpoint: %s did not report a version", path)
	}
	return version, nil
}

// compareGitVersions orders dotted versions numerically. Apple's git reports
// "2.39.5 (Apple Git-154)", so trailing noise has to be ignored rather than
// parsed.
func compareGitVersions(a, b string) int {
	left, right := gitVersionParts(a), gitVersionParts(b)
	for i := 0; i < 3; i++ {
		if left[i] != right[i] {
			if left[i] > right[i] {
				return 1
			}
			return -1
		}
	}
	return 0
}

func gitVersionParts(v string) [3]int {
	var out [3]int
	match := gitVersionPattern.FindStringSubmatch(v)
	if match == nil {
		return out
	}
	for i := 0; i < 3; i++ {
		out[i], _ = strconv.Atoi(strings.TrimSpace(match[i+1]))
	}
	return out
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := in[:0]
	for _, v := range in {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}
