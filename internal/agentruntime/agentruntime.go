// Package agentruntime installs and resolves the ACP adapters DevKit runs its
// agents through.
//
// DevKit used to pick an adapter once at startup from PATH, and fall back to
// `npx -y` when there was none. An app launched from the Dock inherits launchd's
// minimal PATH, so an nvm or Homebrew install was usually invisible, and npx
// cannot work inside the jail at all — it needs a writable ~/.npm. The version
// that ran was whatever the machine happened to have, while DevKit's isolation
// rides on a private channel of that package (_meta.claudeCode.options) that a
// new release can drop without a word.
//
// So the adapter is now DevKit's to install: a pinned version, into a directory
// DevKit owns, run with an absolute node and an absolute entry that the sandbox
// is told about explicitly. See
// docs/superpowers/specs/2026-09-13-agent-runtime-install-design.md.
//
// Nothing here imports internal/agenthub. The installer only needs to prove an
// install works, and agenthub hands that proof in as Manager.Verifiers.
package agentruntime

import (
	"errors"
	"regexp"
	"runtime"
)

// ClaudeACP is the ACP registry id of Claude's adapter.
const ClaudeACP = "claude-acp"

// CodexCLI is OpenAI's Codex CLI, run as `codex app-server`. Not an ACP
// adapter: updates come from npm rather than the ACP registry. The pinned
// TestedVersion is still the default install.
const CodexCLI = "codex"

// Agent is what DevKit knows about one installable adapter.
type Agent struct {
	ID      string
	Package string
	// TestedVersion is the version DevKit was measured against and installs by
	// default. Raising it is a code change that re-runs
	// TestClaudeACPMetaChannelStillWorks and the live Agent Hub tests: the
	// registry's latest is offered as an update, never as the default.
	TestedVersion string
	// Entry is the adapter's bin script, relative to a version directory.
	Entry string
	// SDKPackage is reported as sdkVersion, because the SDK is what actually
	// changes behaviour between adapter releases.
	SDKPackage string
	// BundledClaude lists globs, relative to a version directory, where the
	// SDK's platform package puts the Claude Code binary. npm picks the package
	// by os/cpu (and libc on Linux), so which one exists is decided at install.
	BundledClaude []string
	// NativeEntry lists globs, relative to a version directory, where npm puts
	// the agent's own executable. When set, the agent is that binary: Entry and
	// node are unused, and Resolved.Command is the match. npm picks the
	// platform package by os/cpu, so which one exists is decided at install —
	// the same reason BundledClaude is a list.
	NativeEntry []string
}

var agents = map[string]Agent{
	ClaudeACP: {
		ID:            ClaudeACP,
		Package:       "@agentclientprotocol/claude-agent-acp",
		TestedVersion: "0.78.0",
		Entry:         "node_modules/@agentclientprotocol/claude-agent-acp/dist/index.js",
		SDKPackage:    "@anthropic-ai/claude-agent-sdk",
		BundledClaude: []string{
			"node_modules/@anthropic-ai/claude-agent-sdk-*/" + exeName("claude"),
			// Only when npm could not hoist it; seen in no real install yet, and
			// one more glob is cheaper than a failed install that was fine.
			"node_modules/@anthropic-ai/claude-agent-sdk/node_modules/@anthropic-ai/claude-agent-sdk-*/" + exeName("claude"),
		},
	},
	CodexCLI: {
		ID:            CodexCLI,
		Package:       "@openai/codex",
		TestedVersion: "0.147.0",
		NativeEntry: []string{
			"node_modules/@openai/codex-*/vendor/*/bin/" + exeName("codex"),
			// npm nests the platform package when it cannot hoist it; the global
			// install on the machine this was measured on did exactly that.
			"node_modules/@openai/codex/node_modules/@openai/codex-*/vendor/*/bin/" + exeName("codex"),
		},
	},
}

func lookupAgent(id string) (Agent, error) {
	agent, ok := agents[id]
	if !ok {
		return Agent{}, ErrUnknownAgent
	}
	return agent, nil
}

var (
	ErrUnknownAgent          = errors.New("agentruntime.unknownAgent")
	ErrNotInstalled          = errors.New("agentruntime.notInstalled")
	ErrInstallAlreadyRunning = errors.New("agentruntime.installAlreadyRunning")
	ErrRequirementNotMet     = errors.New("agentruntime.requirementNotMet")
	ErrInstallFailed         = errors.New("agentruntime.installFailed")
	ErrVersionInUse          = errors.New("agentruntime.versionInUse")
	// ErrInvalidInput is a version or option that fails validation. Versions
	// become npm arguments and directory names, so anything that is not plainly
	// a version is refused before it reaches either.
	ErrInvalidInput = errors.New("agentruntime.invalidInput")
)

// Error pairs a sentinel with a reason written for a person. The bridge shows
// Reason as is, so it must never carry anything read from .npmrc or an
// environment — see sanitizeReason.
type Error struct {
	Kind   error
	Reason string
}

func (e *Error) Error() string {
	if e.Reason == "" {
		return e.Kind.Error()
	}
	return e.Kind.Error() + ": " + e.Reason
}

func (e *Error) Unwrap() error { return e.Kind }

func fail(kind error, reason string) error { return &Error{Kind: kind, Reason: reason} }

var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`)

func validVersion(v string) bool { return versionPattern.MatchString(v) }

// Resolved is everything a sandboxed launch needs, all absolute.
type Resolved struct {
	// Release is an idempotent artifact lease, present only from ResolveLeased.
	Release func()   `json:"-"`
	Command string   // node
	Args    []string // [entry]
	// ReadOnlyPaths are what the jail must be able to read for Command to run:
	// the version directory, node's install prefix, and the machine's Claude
	// Code when that is the one chosen. On Linux they sit under $HOME, which
	// bwrap otherwise hides behind a tmpfs.
	ReadOnlyPaths []string
	// Env is merged over the adapter's own allowlist; PATH here wins.
	Env     map[string]string
	Version string
}

// Requirement is one thing an install needs from the machine.
type Requirement struct {
	Name       string `json:"name"`
	MinVersion string `json:"minVersion"`
	Met        bool   `json:"met"`
	Found      string `json:"found"`
	Path       string `json:"path"`
	// Problem says what is wrong in one phrase, empty when Met.
	Problem     string `json:"problem"`
	InstallHint string `json:"installHint"`
}

// InstalledRuntime is one installed version, as the settings page shows it.
type InstalledRuntime struct {
	Version       string `json:"version"`
	SDKVersion    string `json:"sdkVersion"`
	InstalledAt   string `json:"installedAt"`
	Path          string `json:"path"`
	BundledClaude string `json:"bundledClaude"`
	NodePath      string `json:"nodePath"`
	NodeVersion   string `json:"nodeVersion"`
}

// Claude binary sources.
const (
	ClaudeBundled = "bundled"
	ClaudeMachine = "machine"
)

type ClaudeBinary struct {
	Source  string         `json:"source"`
	Machine *MachineClaude `json:"machine"`
}

type MachineClaude struct {
	Path    string `json:"path"`
	Version string `json:"version"`
}

type Status struct {
	Agent         string            `json:"agent"`
	TestedVersion string            `json:"testedVersion"`
	LatestVersion *string           `json:"latestVersion"`
	Current       *InstalledRuntime `json:"current"`
	Previous      *InstalledRuntime `json:"previous"`
	Installing    bool              `json:"installing"`
	Requirements  []Requirement     `json:"requirements"`
	ClaudeBinary  ClaudeBinary      `json:"claudeBinary"`
	LicenseURL    *string           `json:"licenseUrl"`
	// DiskBytes is everything under the agent's directory — the versions kept,
	// plus a staging tree while an install runs.
	DiskBytes int64 `json:"diskBytes"`
}

// exeName is a native program's file name on this platform.
func exeName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}
