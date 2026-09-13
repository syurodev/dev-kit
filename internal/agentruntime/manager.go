package agentruntime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultRegistryURL is the ACP registry's latest index.
const DefaultRegistryURL = "https://cdn.agentclientprotocol.com/registry/v1/latest/registry.json"

// Manager owns <dataDir>/runtimes:
//
//	registry.json, registry.etag   cached ACP registry
//	.npm-cache/                    npm's cache, so a failed install never touches ~/.npm
//	<agent>/current.json           {"version", "previous"}
//	<agent>/settings.json          {"claudeBinary"}
//	<agent>/<version>/             npm --prefix tree + install.json
//	<agent>/.staging-*, .trash-*   only while an install or removal runs
type Manager struct {
	root string

	// OnOutput receives every line npm writes during an install, on the
	// goroutine draining npm. Non-blocking implementations only.
	OnOutput func(agent, line string)
	// Verify proves a staged install runs before it is activated. agenthub
	// supplies a sandboxed ACP initialize under the same policy a real session
	// gets; nil skips the check, which only tests should do.
	Verify func(ctx context.Context, rt Resolved) error

	RegistryURL string
	HTTPClient  *http.Client

	// Seams for tests: npm without the network, node and claude without the
	// machine.
	runNPM     func(ctx context.Context, npm string, args, env []string, onLine func(string)) error
	findNode   func(ctx context.Context) (nodeInstall, []Requirement)
	findClaude func(ctx context.Context) *MachineClaude

	// mu guards installing and every read-modify-write of the state files.
	// Held briefly: an install takes it only to activate, never around npm.
	mu         sync.Mutex
	installing map[string]bool
}

// New returns a Manager rooted at root (normally <dataDir>/runtimes).
func New(root string) *Manager {
	return &Manager{
		root:        root,
		RegistryURL: DefaultRegistryURL,
		runNPM:      runStreaming,
		findNode:    locateNode,
		findClaude:  locateMachineClaude,
		installing:  map[string]bool{},
	}
}

type currentFile struct {
	Version  string `json:"version"`
	Previous string `json:"previous,omitempty"`
}

type settingsFile struct {
	ClaudeBinary string `json:"claudeBinary"`
}

type installManifest struct {
	Agent         string `json:"agent"`
	Version       string `json:"version"`
	Package       string `json:"package"`
	Entry         string `json:"entry"`
	SDKVersion    string `json:"sdkVersion"`
	BundledClaude string `json:"bundledClaude"`
	Node          struct {
		Path    string `json:"path"`
		Version string `json:"version"`
	} `json:"node"`
	NPMVersion  string `json:"npmVersion"`
	InstalledAt string `json:"installedAt"`
	Verified    struct {
		Handshake bool `json:"handshake"`
	} `json:"verified"`
}

func (m *Manager) agentDir(agent string) string { return filepath.Join(m.root, agent) }

func (m *Manager) readCurrent(agent string) currentFile {
	var cur currentFile
	_ = readJSON(filepath.Join(m.agentDir(agent), "current.json"), &cur)
	return cur
}

func (m *Manager) claudeSource(agent string) string {
	var s settingsFile
	if readJSON(filepath.Join(m.agentDir(agent), "settings.json"), &s) == nil && s.ClaudeBinary == ClaudeMachine {
		return ClaudeMachine
	}
	return ClaudeBundled
}

// Status reports an agent's installs, requirements and choices. No network:
// latestVersion comes from the cached registry, refreshed by CheckLatest.
func (m *Manager) Status(ctx context.Context, agentID string) (Status, error) {
	agent, err := lookupAgent(agentID)
	if err != nil {
		return Status{}, err
	}
	m.mu.Lock()
	installing := m.installing[agentID]
	cur := m.readCurrent(agentID)
	m.mu.Unlock()

	_, reqs := m.findNode(ctx)
	st := Status{
		Agent:         agentID,
		TestedVersion: agent.TestedVersion,
		Current:       m.installed(agentID, cur.Version),
		Previous:      m.installed(agentID, cur.Previous),
		Installing:    installing,
		Requirements:  reqs,
		ClaudeBinary:  ClaudeBinary{Source: m.claudeSource(agentID), Machine: m.findClaude(ctx)},
		DiskBytes:     dirSize(m.agentDir(agentID)),
	}
	if body, err := os.ReadFile(m.registryPath()); err == nil {
		if latest, license := registryEntry(body, agent); latest != "" {
			st.LatestVersion = &latest
			if license != "" {
				st.LicenseURL = &license
			}
		}
	}
	return st, nil
}

func (m *Manager) installed(agent, version string) *InstalledRuntime {
	if version == "" {
		return nil
	}
	dir := filepath.Join(m.agentDir(agent), version)
	var mf installManifest
	if readJSON(filepath.Join(dir, "install.json"), &mf) != nil {
		return nil
	}
	out := &InstalledRuntime{
		Version:     mf.Version,
		SDKVersion:  mf.SDKVersion,
		InstalledAt: mf.InstalledAt,
		Path:        dir,
		NodePath:    mf.Node.Path,
		NodeVersion: mf.Node.Version,
	}
	if mf.BundledClaude != "" {
		out.BundledClaude = filepath.Join(dir, filepath.FromSlash(mf.BundledClaude))
	}
	return out
}

// Install installs version (TestedVersion when empty) and activates it.
//
// It runs outside the sandbox — npm needs the network and somewhere to write —
// which is why --ignore-scripts is not optional: no code from the package runs
// until Verify starts it inside the jail. Neither the adapter nor its SDK has
// an install script (checked for 0.74.0 and SDK 0.3.257); the Claude binary
// arrives as an optionalDependency picked by os/cpu.
//
// Cancelling ctx kills npm and deletes the staging tree; a failure at any step
// leaves current.json as it was.
func (m *Manager) Install(ctx context.Context, agentID, version string) error {
	agent, err := lookupAgent(agentID)
	if err != nil {
		return err
	}
	if version == "" {
		version = agent.TestedVersion
	}
	if !validVersion(version) {
		return fail(ErrInvalidInput, "the version must look like 1.2.3")
	}

	m.mu.Lock()
	if m.installing[agentID] {
		m.mu.Unlock()
		return ErrInstallAlreadyRunning
	}
	m.installing[agentID] = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.installing, agentID)
		m.mu.Unlock()
	}()

	node, reqs := m.findNode(ctx)
	for _, req := range reqs {
		if !req.Met {
			return fail(ErrRequirementNotMet, req.Problem)
		}
	}

	agentDir := m.agentDir(agentID)
	if err := os.MkdirAll(agentDir, 0o700); err != nil {
		return fail(ErrInstallFailed, "could not create the runtime directory")
	}
	staging, err := os.MkdirTemp(agentDir, ".staging-"+version+"-")
	if err != nil {
		return fail(ErrInstallFailed, "could not create a staging directory")
	}
	activated := false
	defer func() {
		if !activated {
			_ = os.RemoveAll(staging)
		}
	}()

	env := npmEnv(filepath.Dir(node.Node), filepath.Join(m.root, ".npm-cache"))
	var npmVersion string
	_ = m.runNPM(ctx, node.NPM, []string{"--version"}, env, func(line string) {
		if line = strings.TrimSpace(line); line != "" {
			npmVersion = line
		}
	})

	var npmError string
	args := []string{"install", "--prefix", staging, agent.Package + "@" + version,
		"--ignore-scripts", "--no-audit", "--no-fund", "--omit=dev", "--no-update-notifier"}
	if err := m.runNPM(ctx, node.NPM, args, env, func(line string) {
		line = sanitizeReason(line)
		if npmError == "" {
			npmError = npmErrorMessage(line)
		}
		if m.OnOutput != nil {
			m.OnOutput(agentID, line)
		}
	}); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		reason := "npm install failed"
		if npmError != "" {
			reason += ": " + npmError
		}
		return fail(ErrInstallFailed, reason)
	}

	mf, err := inspectInstall(agent, staging, version, m.claudeSource(agentID) == ClaudeBundled)
	if err != nil {
		return err
	}
	mf.Node.Path, mf.Node.Version = node.Node, node.Version
	mf.NPMVersion = npmVersion
	mf.InstalledAt = time.Now().UTC().Format(time.RFC3339)

	if m.Verify != nil {
		rt, err := m.resolveAt(ctx, agent, staging, mf)
		if err != nil {
			return err
		}
		if err := m.Verify(ctx, rt); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fail(ErrInstallFailed, sanitizeReason("the installed agent did not start: "+err.Error()))
		}
		mf.Verified.Handshake = true
	}
	if err := writeJSON(filepath.Join(staging, "install.json"), mf); err != nil {
		return fail(ErrInstallFailed, "could not write install.json")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	activated, err = m.activateStaged(agentID, staging, version)
	if err != nil {
		return fail(ErrInstallFailed, "could not activate the new version")
	}
	return nil
}

// activateStaged moves a verified staging tree into place and points
// current.json at it. activated reports whether staging is gone, which is
// true even when only the current.json write failed. Caller holds m.mu.
func (m *Manager) activateStaged(agent, staging, version string) (activated bool, err error) {
	dest := filepath.Join(m.agentDir(agent), version)
	var trash string
	if _, err := os.Stat(dest); err == nil {
		// A reinstall of the same version. Renamed aside rather than deleted
		// first, so a failure between the two renames can put it back.
		trash = m.trashPath(agent, version)
		if err := os.Rename(dest, trash); err != nil {
			return false, err
		}
	}
	if err := os.Rename(staging, dest); err != nil {
		if trash != "" {
			_ = os.Rename(trash, dest)
		}
		return false, err
	}
	if trash != "" {
		// Best-effort: on Windows a running session holds files open; GC
		// retries at the next start.
		_ = os.RemoveAll(trash)
	}
	cur := m.readCurrent(agent)
	next := currentFile{Version: version, Previous: cur.Previous}
	if cur.Version != "" && cur.Version != version {
		next.Previous = cur.Version
	}
	return true, writeJSON(filepath.Join(m.agentDir(agent), "current.json"), next)
}

func (m *Manager) trashPath(agent, version string) string {
	return filepath.Join(m.agentDir(agent), ".trash-"+version+"-"+strconv.FormatInt(time.Now().UnixNano(), 36))
}

// inspectInstall is the static half of verification: the files a launch needs
// are where they should be, and npm installed what was asked for.
func inspectInstall(agent Agent, dir, version string, needBundledClaude bool) (installManifest, error) {
	var mf installManifest
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(agent.Entry))); err != nil {
		return mf, fail(ErrInstallFailed, "the adapter's entry script is missing")
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if err := readJSON(filepath.Join(dir, "node_modules", filepath.FromSlash(agent.Package), "package.json"), &pkg); err != nil {
		return mf, fail(ErrInstallFailed, "the adapter's package.json is unreadable")
	}
	if pkg.Version != version {
		return mf, fail(ErrInstallFailed, fmt.Sprintf("npm installed %s, not %s", pkg.Version, version))
	}
	if _, err := os.Stat(filepath.Join(dir, "package-lock.json")); err != nil {
		return mf, fail(ErrInstallFailed, "package-lock.json is missing")
	}
	var sdk struct {
		Version string `json:"version"`
	}
	_ = readJSON(filepath.Join(dir, "node_modules", filepath.FromSlash(agent.SDKPackage), "package.json"), &sdk)

	mf.Agent, mf.Version, mf.Entry, mf.SDKVersion = agent.ID, version, agent.Entry, sdk.Version
	mf.Package = agent.Package + "@" + version
	for _, pattern := range agent.BundledClaude {
		matches, _ := filepath.Glob(filepath.Join(dir, filepath.FromSlash(pattern)))
		for _, match := range matches {
			if isExecutable(match) {
				rel, _ := filepath.Rel(dir, match)
				mf.BundledClaude = filepath.ToSlash(rel)
				break
			}
		}
		if mf.BundledClaude != "" {
			break
		}
	}
	if mf.BundledClaude == "" && needBundledClaude {
		return mf, fail(ErrInstallFailed, "npm installed no Claude Code binary for this platform")
	}
	return mf, nil
}

// Activate points current.json at an installed version; the old current
// becomes previous, which is what makes it a rollback.
func (m *Manager) Activate(agentID, version string) error {
	if _, err := lookupAgent(agentID); err != nil {
		return err
	}
	if !validVersion(version) {
		return fail(ErrInvalidInput, "the version must look like 1.2.3")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := os.Stat(filepath.Join(m.agentDir(agentID), version, "install.json")); err != nil {
		return ErrNotInstalled
	}
	cur := m.readCurrent(agentID)
	if cur.Version == version {
		return nil
	}
	return writeJSON(filepath.Join(m.agentDir(agentID), "current.json"), currentFile{Version: version, Previous: cur.Version})
}

// Remove deletes an installed version other than the current one. Sessions
// already running on it keep running on Unix; on Windows the delete may be
// partial, and GCOnStartup finishes it.
func (m *Manager) Remove(agentID, version string) error {
	if _, err := lookupAgent(agentID); err != nil {
		return err
	}
	if !validVersion(version) {
		return fail(ErrInvalidInput, "the version must look like 1.2.3")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cur := m.readCurrent(agentID)
	if cur.Version == version {
		return ErrVersionInUse
	}
	dir := filepath.Join(m.agentDir(agentID), version)
	if _, err := os.Stat(dir); err != nil {
		return ErrNotInstalled
	}
	// Renamed first so a half-finished delete never looks like an install.
	trash := m.trashPath(agentID, version)
	if err := os.Rename(dir, trash); err != nil {
		return fmt.Errorf("remove %s: %w", version, err)
	}
	_ = os.RemoveAll(trash)
	if cur.Previous == version {
		cur.Previous = ""
		return writeJSON(filepath.Join(m.agentDir(agentID), "current.json"), cur)
	}
	return nil
}

// SetClaudeBinary chooses which Claude Code the Claude adapter runs: the one
// bundled with the adapter's SDK (the default, and the one that matches it) or
// the machine's own install.
func (m *Manager) SetClaudeBinary(ctx context.Context, source string) error {
	if source != ClaudeBundled && source != ClaudeMachine {
		return fail(ErrInvalidInput, `the Claude binary must be "bundled" or "machine"`)
	}
	if source == ClaudeMachine && m.findClaude(ctx) == nil {
		return fail(ErrRequirementNotMet, "Claude Code was not found on this machine")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	dir := m.agentDir(ClaudeACP)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return writeJSON(filepath.Join(dir, "settings.json"), settingsFile{ClaudeBinary: source})
}

// Resolve returns how to launch the agent's current version, or
// ErrNotInstalled. It never falls back to PATH or npx.
func (m *Manager) Resolve(agentID string) (Resolved, error) {
	agent, err := lookupAgent(agentID)
	if err != nil {
		return Resolved{}, err
	}
	m.mu.Lock()
	cur := m.readCurrent(agentID)
	m.mu.Unlock()
	if cur.Version == "" {
		return Resolved{}, ErrNotInstalled
	}
	dir := filepath.Join(m.agentDir(agentID), cur.Version)
	var mf installManifest
	if readJSON(filepath.Join(dir, "install.json"), &mf) != nil {
		return Resolved{}, ErrNotInstalled
	}
	return m.resolveAt(context.Background(), agent, dir, mf)
}

func (m *Manager) resolveAt(ctx context.Context, agent Agent, dir string, mf installManifest) (Resolved, error) {
	// Resolved once, and that spelling used for the command, its argument and
	// the grant alike: bwrap binds exactly the path it is given, so a symlink
	// in the runtime's path would leave the command pointing outside the jail.
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return Resolved{}, ErrNotInstalled
	}
	entry := filepath.Join(dir, filepath.FromSlash(agent.Entry))
	if _, err := os.Stat(entry); err != nil {
		return Resolved{}, ErrNotInstalled
	}

	// The node the install used, while it is still there. Probing on every
	// session open would spawn a process per candidate; relocating only when
	// it is gone (nvm uninstall, brew upgrade) costs that once.
	node := mf.Node.Path
	if !isExecutable(node) {
		found, reqs := m.findNode(ctx)
		for _, req := range reqs {
			if req.Name == "node" && !req.Met {
				return Resolved{}, fail(ErrRequirementNotMet, req.Problem)
			}
		}
		node = found.Node
	}
	if resolved, err := filepath.EvalSymlinks(node); err == nil {
		node = resolved
	}

	rt := Resolved{
		Command:       node,
		Args:          []string{entry},
		ReadOnlyPaths: append([]string{dir}, nodeReadPaths(node)...),
		Env:           map[string]string{"PATH": filepath.Dir(node) + string(os.PathListSeparator) + hostPath()},
		Version:       mf.Version,
	}
	if agent.ID == ClaudeACP && m.claudeSource(agent.ID) == ClaudeMachine {
		machine := m.findClaude(ctx)
		if machine == nil {
			return Resolved{}, fail(ErrRequirementNotMet, "Claude Code was not found on this machine; choose the bundled one in Settings → Agents")
		}
		exe, err := filepath.EvalSymlinks(machine.Path)
		if err != nil {
			exe = machine.Path
		}
		// The adapter reads this to pick the binary (acp-agent.js), and
		// ~/.local/bin/claude is a symlink into ~/.local/share/claude/versions,
		// so the grant has to be the resolved directory.
		rt.Env["CLAUDE_CODE_EXECUTABLE"] = exe
		rt.ReadOnlyPaths = append(rt.ReadOnlyPaths, filepath.Dir(exe))
	}
	return rt, nil
}

// GCOnStartup removes staging and trash trees and every version other than
// current and previous. Only safe before any session starts, which is the one
// time main calls it.
func (m *Manager) GCOnStartup() error {
	var errs []error
	for id := range agents {
		dir := m.agentDir(id)
		entries, err := os.ReadDir(dir)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		var cur currentFile
		// Without a readable current.json nothing says which version is live,
		// so only the unmistakable leftovers go.
		haveCurrent := readJSON(filepath.Join(dir, "current.json"), &cur) == nil && cur.Version != ""
		for _, entry := range entries {
			name := entry.Name()
			if !entry.IsDir() {
				continue
			}
			leftover := strings.HasPrefix(name, ".staging-") || strings.HasPrefix(name, ".trash-")
			stale := haveCurrent && validVersion(name) && name != cur.Version && name != cur.Previous
			if leftover || stale {
				if err := os.RemoveAll(filepath.Join(dir, name)); err != nil {
					errs = append(errs, err)
				}
			}
		}
	}
	return errors.Join(errs...)
}

// CheckLatest refreshes the cached registry. Being offline is not an error:
// Status keeps showing whatever was cached, or no latest at all.
func (m *Manager) CheckLatest(ctx context.Context, agentID string) error {
	if _, err := lookupAgent(agentID); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.RegistryURL, nil)
	if err != nil {
		return nil
	}
	if cached, err := os.ReadFile(m.registryPath()); err == nil && json.Valid(cached) {
		if etag, err := os.ReadFile(m.registryPath() + ".etag"); err == nil {
			req.Header.Set("If-None-Match", strings.TrimSpace(string(etag)))
		}
	}
	client := m.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil // 304 included: the cache is current
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil || !json.Valid(body) {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := os.MkdirAll(m.root, 0o700); err != nil {
		return nil
	}
	if writeFileAtomic(m.registryPath(), body) == nil {
		if etag := resp.Header.Get("ETag"); etag != "" {
			_ = writeFileAtomic(m.registryPath()+".etag", []byte(etag))
		} else {
			_ = os.Remove(m.registryPath() + ".etag")
		}
	}
	return nil
}

func (m *Manager) registryPath() string { return filepath.Join(m.root, "registry.json") }

// registryEntry reads an agent's latest version and license URL from a
// registry document. The version comes from distribution.npx.package, and only
// when that names DevKit's own package: a registry entry that moved to another
// package is not an update DevKit knows how to install.
func registryEntry(body []byte, agent Agent) (version, licenseURL string) {
	var doc struct {
		Agents []struct {
			ID           string `json:"id"`
			LicenseURL   string `json:"license_url"`
			Distribution struct {
				NPX *struct {
					Package string `json:"package"`
				} `json:"npx"`
			} `json:"distribution"`
		} `json:"agents"`
	}
	if json.Unmarshal(body, &doc) != nil {
		return "", ""
	}
	for _, entry := range doc.Agents {
		if entry.ID != agent.ID || entry.Distribution.NPX == nil {
			continue
		}
		v, ok := strings.CutPrefix(entry.Distribution.NPX.Package, agent.Package+"@")
		if !ok || !validVersion(v) {
			return "", ""
		}
		return v, entry.LicenseURL
	}
	return "", ""
}

// runStreaming runs a program and hands each line of its combined output to
// onLine, in order, returning only after the last line.
func runStreaming(ctx context.Context, program string, args, env []string, onLine func(string)) error {
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Env = env
	// ponytail: cancelling kills npm itself. On Windows npm.cmd is cmd.exe
	// running node, and that node can outlive it; WaitDelay keeps Wait from
	// hanging on the pipe it still holds. A job object is the fix if an
	// orphan ever keeps a staging tree locked.
	cmd.WaitDelay = 5 * time.Second
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		pw.Close()
		pr.Close()
		return err
	}
	var drained sync.WaitGroup
	drained.Add(1)
	go func() {
		defer drained.Done()
		scanner := bufio.NewScanner(pr)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			onLine(scanner.Text())
		}
		_, _ = io.Copy(io.Discard, pr)
	}()
	err := cmd.Wait()
	pw.Close()
	drained.Wait()
	pr.Close()
	return err
}

// npmEnv is the host environment with node's directory first on PATH and
// npm's cache moved into the runtimes directory. HOME stays real so npm still
// reads the user's .npmrc (a proxy, a private registry).
func npmEnv(nodeDir, cache string) []string {
	env := make([]string, 0, len(os.Environ())+2)
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if strings.EqualFold(key, "PATH") || strings.EqualFold(key, "npm_config_cache") {
			continue
		}
		env = append(env, kv)
	}
	return append(env,
		"PATH="+nodeDir+string(os.PathListSeparator)+hostPath(),
		"npm_config_cache="+cache)
}

func hostPath() string {
	if path := os.Getenv("PATH"); path != "" {
		return path
	}
	if runtime.GOOS == "windows" {
		return ""
	}
	return "/usr/local/bin:/usr/bin:/bin"
}

// npmErrorMessage extracts the useful part of an `npm error` line, or "".
// The first such line after `code …` is the one that says what happened
// ("404 Not Found - GET …", "notarget No matching version …"); what follows
// is advice and a pointer to a log file.
func npmErrorMessage(line string) string {
	for _, prefix := range []string{"npm error", "npm ERR!"} {
		if rest, ok := strings.CutPrefix(line, prefix); ok {
			rest = strings.TrimSpace(rest)
			if rest == "" || strings.HasPrefix(rest, "code ") || strings.Contains(rest, "complete log of this run") {
				return ""
			}
			return rest
		}
	}
	return ""
}

var secretPatterns = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`(?i)(_authToken|_auth|_password|password|token)(\s*[=:]\s*)\S+`), "${1}${2}<redacted>"},
	{regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=-]+`), "${1} <redacted>"},
	{regexp.MustCompile(`://[^/\s:@]+:[^/\s@]+@`), "://<redacted>@"},
}

// sanitizeReason strips what looks like a credential and bounds the length.
// npm echoes registry URLs and, misconfigured, .npmrc lines; either can carry
// a token, and this text is shown in the UI.
func sanitizeReason(s string) string {
	for _, p := range secretPatterns {
		s = p.re.ReplaceAllString(s, p.repl)
	}
	if runes := []rune(s); len(runes) > 300 {
		s = string(runes[:300]) + "…"
	}
	return s
}

func dirSize(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

func readJSON(path string, v any) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}

func writeJSON(path string, v any) error {
	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(body, '\n'))
}

// writeFileAtomic writes through a temp file in the same directory and a
// rename, so a crash leaves the old file or the new one, never half of either.
func writeFileAtomic(path string, body []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}
