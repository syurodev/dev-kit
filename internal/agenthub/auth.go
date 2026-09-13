package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"

	"devkit/internal/acp"
)

// ErrAuthMethodUnknown is a sign-in id the agent did not offer.
var ErrAuthMethodUnknown = errors.New("agenthub.authMethodUnknown")

// AuthMethod is a sign-in the agent offers, resolved to what DevKit would run
// for it. DevKit never signs anyone in itself: every method is the agent's own
// login command, run unmodified in a terminal the user watches, so sign-in
// completes through Anthropic's flow and no credential passes through DevKit.
type AuthMethod struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Label is the agent's name for the terminal, e.g. "Claude Login".
	Label   string   `json:"label"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// SignInCommand is an AuthMethod ready to start: the environment is resolved
// here, not in the UI, and never crosses the bridge.
type SignInCommand struct {
	Path  string
	Args  []string
	Env   []string
	Label string
}

// AuthProvider is a vendor adapter that can list its sign-ins without a session.
type AuthProvider interface {
	AuthMethods(ctx context.Context) ([]AuthMethod, error)
	SignInCommand(ctx context.Context, methodID string) (SignInCommand, error)
}

var _ AuthProvider = (*ClaudeAdapter)(nil)

// AuthMethods lists an agent type's sign-ins. A vendor with none (mock) has an
// empty list, not an error.
func (s *Service) AuthMethods(ctx context.Context, t AgentType) ([]AuthMethod, error) {
	adapter, ok := s.reg.Get(t)
	if !ok {
		return nil, ErrVendorUnknown
	}
	provider, ok := adapter.(AuthProvider)
	if !ok {
		return []AuthMethod{}, nil
	}
	return provider.AuthMethods(ctx)
}

// SignInCommand resolves one of an agent type's sign-ins to the process to run.
func (s *Service) SignInCommand(ctx context.Context, t AgentType, methodID string) (SignInCommand, error) {
	adapter, ok := s.reg.Get(t)
	if !ok {
		return SignInCommand{}, ErrVendorUnknown
	}
	provider, ok := adapter.(AuthProvider)
	if !ok {
		return SignInCommand{}, ErrAuthMethodUnknown
	}
	return provider.SignInCommand(ctx, methodID)
}

// terminalAuthMeta is _meta["terminal-auth"]: the full command line an agent
// suggests for a terminal method.
type terminalAuthMeta struct {
	TerminalAuth *struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
		Label   string   `json:"label"`
	} `json:"terminal-auth"`
}

// resolveAuthMethods turns initialize's authMethods into what DevKit will run.
//
// Only "terminal" methods are kept: the others (gateway, env_var) are not a
// command to run, and running the adapter bare would start an ACP server on a
// terminal. For each, the command is the launch command DevKit already runs
// the agent with, and the args are one of two things:
//
//   - _meta["terminal-auth"].args, when that command IS the launch command and
//     its args begin with the launch args — i.e. the same node running the same
//     adapter entry, only with login flags after it;
//   - otherwise the launch args with the method's own args appended, which is
//     what the ACP spec says a terminal method means.
//
// The meta is never trusted as it stands. Taken at its word, a compromised or
// simply odd agent could name any program — or the right node with a different
// script — and DevKit would run it outside the sandbox. The fallback can only
// ever run the adapter DevKit installed.
func resolveAuthMethods(raw []acp.AuthMethod, l launch) []AuthMethod {
	out := make([]AuthMethod, 0, len(raw))
	for _, m := range raw {
		if m.Type != "terminal" || m.ID == "" {
			continue
		}
		method := AuthMethod{
			ID:          m.ID,
			Name:        m.Name,
			Description: strings.TrimSpace(m.Description),
			Label:       m.Name,
			Command:     l.path,
			Args:        append(append([]string(nil), l.args...), m.Args...),
		}
		var meta terminalAuthMeta
		if len(m.Meta) > 0 && json.Unmarshal(m.Meta, &meta) == nil && meta.TerminalAuth != nil {
			ta := meta.TerminalAuth
			if ta.Label != "" {
				method.Label = ta.Label
			}
			if acceptTerminalAuth(ta.Command, ta.Args, l) {
				method.Args = append([]string(nil), ta.Args...)
			} else {
				log.Printf("agenthub: auth method %q: terminal-auth command %q is not the launch command; using its args instead", m.ID, ta.Command)
			}
		}
		out = append(out, method)
	}
	return out
}

// acceptTerminalAuth reports whether an agent-suggested command line is the
// launch command with extra arguments after the launch arguments.
func acceptTerminalAuth(command string, args []string, l launch) bool {
	if l.path == "" || !filepath.IsAbs(command) || filepath.Clean(command) != l.path {
		return false
	}
	if len(args) < len(l.args) {
		return false
	}
	for i, arg := range l.args {
		if args[i] != arg {
			return false
		}
	}
	return true
}

// signInEnv is the environment a login runs with: the user's own, as if they
// had typed the command, with the launch env laid over it — PATH with node's
// directory first, and the Claude Code binary sessions use. DEVKIT_* is DevKit's
// own configuration and stays behind.
func signInEnv(launchEnv map[string]string) []string {
	env := make([]string, 0, len(os.Environ())+len(launchEnv))
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(strings.ToUpper(key), "DEVKIT_") || nestedClaudeMarker(key) {
			continue
		}
		if _, overridden := lookupFold(launchEnv, key); overridden {
			continue
		}
		env = append(env, kv)
	}
	for key, value := range launchEnv {
		env = append(env, key+"="+value)
	}
	return env
}

// nestedClaudeMarker is a variable Claude Code sets in the processes it starts.
// DevKit is often launched from a terminal inside a Claude Code session, and a
// login that inherits these believes it is running nested in that session.
// Only the markers: CLAUDE_CODE_USE_BEDROCK and friends are the user's own
// configuration and pass through.
//
// ponytail: a fixed list of the markers known today; widen it if a new one
// makes a login misbehave.
func nestedClaudeMarker(key string) bool {
	switch strings.ToUpper(key) {
	case "CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT":
		return true
	}
	return false
}

// lookupFold is a case-insensitive map lookup: Windows spells it Path.
func lookupFold(m map[string]string, key string) (string, bool) {
	for k, v := range m {
		if strings.EqualFold(k, key) {
			return v, true
		}
	}
	return "", false
}
