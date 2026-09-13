package agentruntime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Finding node, npm and the machine's own Claude Code without trusting PATH.
//
// An app opened from Finder or the Dock inherits launchd's PATH, which holds
// none of the places developers actually get node from — nvm, Volta, fnm,
// Homebrew. internal/codegraph/locate.go solves the same problem for JDKs; this
// is a deliberately small copy rather than a shared package, because the two
// need different things from a candidate (node reports its real execPath, which
// is what the sandbox has to be told about).

// minNodeVersion is the adapter's own engines floor.
const minNodeVersion = "22"

// maxNodeCandidates bounds how many programs get spawned to ask their version.
const maxNodeCandidates = 24

// probeTimeout bounds one `--version`-style probe. A shim that resolves a
// version on first use (Volta) is the slow case.
const probeTimeout = 10 * time.Second

// systemBinDirs are the fixed install locations checked after the version
// managers. A variable so tests can take the real machine out of the picture.
var systemBinDirs = defaultSystemBinDirs()

func defaultSystemBinDirs() []string {
	if runtime.GOOS == "windows" {
		var dirs []string
		for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
			if root := os.Getenv(env); root != "" {
				dirs = append(dirs, filepath.Join(root, "nodejs"))
			}
		}
		return dirs
	}
	return []string{"/opt/homebrew/bin", "/usr/local/bin"}
}

// nodeInstall is a node that meets minNodeVersion, with the npm beside it.
type nodeInstall struct {
	// Node is process.execPath as the program itself reports it, which sees
	// through shims and symlinks: /opt/homebrew/bin/node answers with its
	// Cellar path, a Volta shim with the image it dispatches to.
	Node    string
	NPM     string
	Version string
}

// nodeCandidates lists where a node may be, in preference order.
func nodeCandidates() []string {
	node := exeName("node")
	home, _ := os.UserHomeDir()
	var out []string
	if home != "" {
		out = append(out, globNewest(filepath.Join(home, ".nvm", "versions", "node", "*", "bin", node))...)
	}
	if dir := os.Getenv("NVM_DIR"); dir != "" {
		out = append(out, globNewest(filepath.Join(dir, "versions", "node", "*", "bin", node))...)
	}
	// nvm-windows keeps node.exe at the top of each version directory.
	if dir := os.Getenv("NVM_HOME"); dir != "" {
		out = append(out, globNewest(filepath.Join(dir, "v*", node))...)
	}
	if home != "" {
		out = append(out, filepath.Join(home, ".volta", "bin", node))
	}
	fnmRoots := []string{os.Getenv("FNM_DIR")}
	if home != "" {
		fnmRoots = append(fnmRoots,
			filepath.Join(home, ".local", "share", "fnm"),
			filepath.Join(home, "Library", "Application Support", "fnm"),
			filepath.Join(home, ".fnm"))
	}
	if appData := os.Getenv("APPDATA"); appData != "" {
		fnmRoots = append(fnmRoots, filepath.Join(appData, "fnm"))
	}
	for _, root := range fnmRoots {
		if root == "" {
			continue
		}
		installation := filepath.Join(root, "node-versions", "*", "installation")
		if runtime.GOOS == "windows" {
			out = append(out, globNewest(filepath.Join(installation, node))...)
		} else {
			out = append(out, globNewest(filepath.Join(installation, "bin", node))...)
		}
	}
	for _, dir := range systemBinDirs {
		out = append(out, filepath.Join(dir, node))
	}
	if path, err := exec.LookPath(node); err == nil {
		out = append(out, path)
	}
	return out
}

// globNewest returns the matches of pattern with the highest version first.
// The version is read off the path (v24.14.1, 22.3.0) — only an ordering hint,
// since every candidate is probed and the best one wins by what it reports.
func globNewest(pattern string) []string {
	matches, _ := filepath.Glob(pattern)
	sort.SliceStable(matches, func(i, j int) bool {
		return compareVersions(matches[i], matches[j]) > 0
	})
	return matches
}

var versionComponent = regexp.MustCompile(`[0-9]+`)

// compareVersions orders dotted versions numerically, ignoring anything that
// is not a digit run. Mirrors internal/codegraph's; see the note at the top.
func compareVersions(a, b string) int {
	left := versionComponent.FindAllString(a, -1)
	right := versionComponent.FindAllString(b, -1)
	for i := 0; i < len(left) || i < len(right); i++ {
		l, r := versionPart(left, i), versionPart(right, i)
		if l != r {
			if l > r {
				return 1
			}
			return -1
		}
	}
	return 0
}

func versionPart(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	n, _ := strconv.Atoi(parts[i])
	return n
}

// locateNode picks the newest node >= minNodeVersion and reports the node and
// npm requirements either way.
func locateNode(ctx context.Context) (nodeInstall, []Requirement) {
	nodeReq := Requirement{Name: "node", MinVersion: minNodeVersion, InstallHint: nodeInstallHint()}
	npmReq := Requirement{Name: "npm", InstallHint: "npm ships with Node.js; reinstall Node.js if it is missing."}

	var best, closest nodeInstall
	seen := map[string]bool{}
	probed := 0
	for _, candidate := range nodeCandidates() {
		if seen[candidate] || !isExecutable(candidate) {
			continue
		}
		seen[candidate] = true
		if probed == maxNodeCandidates {
			break
		}
		probed++
		execPath, version, err := probeNode(ctx, candidate)
		if err != nil {
			continue
		}
		found := nodeInstall{Node: execPath, NPM: siblingNPM(execPath, candidate), Version: version}
		if compareVersions(version, minNodeVersion) < 0 {
			if closest.Version == "" || compareVersions(version, closest.Version) > 0 {
				closest = found
			}
			continue
		}
		// A newer node with no npm beside it cannot install, so having npm
		// outranks being newer.
		better := best.Version == ""
		if !better && (found.NPM != "") != (best.NPM != "") {
			better = found.NPM != ""
		} else if !better {
			better = compareVersions(version, best.Version) > 0
		}
		if better {
			best = found
		}
	}

	switch {
	case best.Version != "":
		nodeReq.Met, nodeReq.Found, nodeReq.Path = true, best.Version, best.Node
		if best.NPM != "" {
			npmReq.Met, npmReq.Path = true, best.NPM
		} else {
			npmReq.Problem = "npm was not found next to " + best.Node
		}
	case closest.Version != "":
		nodeReq.Found, nodeReq.Path = closest.Version, closest.Node
		nodeReq.Problem = fmt.Sprintf("Node.js %s is older than %s", closest.Version, minNodeVersion)
		npmReq.Problem = "needs Node.js " + minNodeVersion + " or newer first"
	default:
		nodeReq.Problem = "Node.js was not found"
		npmReq.Problem = "needs Node.js " + minNodeVersion + " or newer first"
	}
	return best, []Requirement{nodeReq, npmReq}
}

func nodeInstallHint() string {
	switch runtime.GOOS {
	case "darwin":
		return "Install Node.js " + minNodeVersion + " or newer, e.g. `brew install node` or `nvm install " + minNodeVersion + "`."
	case "windows":
		return "Install Node.js " + minNodeVersion + " or newer, e.g. `winget install OpenJS.NodeJS.LTS`."
	default:
		return "Install Node.js " + minNodeVersion + " or newer, e.g. `nvm install " + minNodeVersion + "` or your distribution's NodeSource package."
	}
}

// probeNode asks a candidate for its version and real executable path.
func probeNode(ctx context.Context, path string) (execPath, version string, err error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "-e", "console.log(process.version);console.log(process.execPath)").Output()
	if err != nil {
		return "", "", err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 {
		return "", "", fmt.Errorf("unexpected output from %s", path)
	}
	version = strings.TrimPrefix(strings.TrimSpace(lines[0]), "v")
	execPath = strings.TrimSpace(lines[1])
	if !versionComponent.MatchString(version) || !filepath.IsAbs(execPath) {
		return "", "", fmt.Errorf("unexpected output from %s", path)
	}
	if resolved, err := filepath.EvalSymlinks(execPath); err == nil {
		execPath = resolved
	}
	return execPath, version, nil
}

// siblingNPM finds npm next to the real node first, then next to the path the
// node was found at (a Volta shim directory holds an npm shim too).
func siblingNPM(execPath, candidate string) string {
	name := "npm"
	if runtime.GOOS == "windows" {
		name = "npm.cmd"
	}
	for _, dir := range []string{filepath.Dir(execPath), filepath.Dir(candidate)} {
		if path := filepath.Join(dir, name); isExecutable(path) {
			return path
		}
	}
	return ""
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return runtime.GOOS == "windows" || info.Mode()&0o111 != 0
}

// nodeReadPaths is what the jail needs to read for node to start: its install
// prefix (<prefix>/bin/node, or the directory holding node.exe).
func nodeReadPaths(node string) []string {
	dir := filepath.Dir(node)
	prefix := dir
	if filepath.Base(dir) == "bin" {
		prefix = filepath.Dir(dir)
	}
	out := []string{prefix}
	// ponytail: a Homebrew node loads icu4c, openssl, libuv and friends from
	// elsewhere under the Homebrew root, so the whole root is granted
	// read-only. Narrow it to the opt/ entries `otool -L` names if that grant
	// ever matters.
	if i := strings.Index(node, "/Cellar/"); i > 0 {
		out = append(out, node[:i])
	} else if strings.HasPrefix(node, "/opt/homebrew/") {
		out = append(out, "/opt/homebrew")
	}
	return out
}

var claudeVersionPattern = regexp.MustCompile(`[0-9]+\.[0-9]+\.[0-9]+`)

// locateMachineClaude finds Claude Code as the user installed it, or nil. The
// first candidate that answers --version wins: unlike node, which one the user
// runs in their terminal is the point, not the newest.
func locateMachineClaude(ctx context.Context) *MachineClaude {
	claude := exeName("claude")
	home, _ := os.UserHomeDir()
	var candidates []string
	if home != "" {
		candidates = append(candidates,
			filepath.Join(home, ".local", "bin", claude),
			filepath.Join(home, ".claude", "local", claude))
	}
	for _, dir := range systemBinDirs {
		candidates = append(candidates, filepath.Join(dir, claude))
	}
	if path, err := exec.LookPath(claude); err == nil {
		candidates = append(candidates, path)
	}
	for _, path := range candidates {
		if !isExecutable(path) {
			continue
		}
		probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
		out, err := exec.CommandContext(probeCtx, path, "--version").Output()
		cancel()
		if err != nil {
			continue
		}
		if version := claudeVersionPattern.FindString(string(out)); version != "" {
			return &MachineClaude{Path: path, Version: version}
		}
	}
	return nil
}
