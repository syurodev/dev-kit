package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"devkit/internal/acp"
	"devkit/internal/identity"
	"github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
)

type EnvironmentEntry struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
	Source string `json:"source,omitempty"`
	Detail string `json:"detail"`
	Action string `json:"action,omitempty"`
}

type EnvironmentReport struct {
	Agent     string             `json:"agent"`
	Scope     string             `json:"scope"` // session | next
	Entries   []EnvironmentEntry `json:"entries"`
	CheckedAt int64              `json:"checkedAt"`
}

type environmentSnapshot struct {
	Agent     AgentType
	Spec      OpenSpec
	Env       map[string]string
	Command   string
	SkillHome string
}

func captureEnvironment(agent AgentType, spec OpenSpec, env map[string]string, command, skillHome string) *environmentSnapshot {
	if spec.PrivateHome != nil {
		value := *spec.PrivateHome
		spec.PrivateHome = &value
	}
	spec.ExtraReadWritePaths = append([]string(nil), spec.ExtraReadWritePaths...)
	spec.McpServers = append([]acp.McpServerStdio(nil), spec.McpServers...)
	copyEnv := make(map[string]string, len(env))
	for key, value := range env {
		copyEnv[key] = value
	}
	return &environmentSnapshot{Agent: agent, Spec: spec, Env: copyEnv, Command: command, SkillHome: skillHome}
}

type environmentInspector interface {
	inspectEnvironment(context.Context, OpenSpec) (EnvironmentReport, error)
}

type environmentReporter interface {
	reportEnvironment(context.Context) (EnvironmentReport, error)
}

// Environment is UI-only and never opens/resumes a session or starts inference.
// A live report uses the process's original environment and grants; changed
// preferences must not be presented as permissions that process already has.
func (s *Service) Environment(ctx context.Context, vendor AgentType, sessionID string) (EnvironmentReport, error) {
	if caller, ok := identity.FromContext(ctx); ok && caller.Kind != identity.KindUI {
		return EnvironmentReport{}, ErrPermissionDenied
	}
	spec := OpenSpec{}
	if sessionID != "" {
		if err := s.authorizeTarget(ctx, sessionID); err != nil {
			return EnvironmentReport{}, err
		}
		sess, err := s.store.GetSession(ctx, sessionID)
		if err != nil {
			return EnvironmentReport{}, err
		}
		vendor = sess.AgentType
		spec = sessionOpenSpec(sess)
		s.mu.Lock()
		rt := s.runtimes[sessionID]
		s.mu.Unlock()
		if reporter, ok := rt.(environmentReporter); ok {
			return reporter.reportEnvironment(ctx)
		}
		s.applyAccount(ctx, &spec, sess)
	}
	s.applySandboxExtras(&spec)
	adapter, ok := s.reg.Get(vendor)
	if !ok {
		return EnvironmentReport{}, ErrVendorUnknown
	}
	if inspector, ok := adapter.(environmentInspector); ok {
		return inspector.inspectEnvironment(ctx, spec)
	}
	return EnvironmentReport{Agent: string(vendor), Scope: "next", CheckedAt: time.Now().UnixMilli(), Entries: []EnvironmentEntry{
		{Name: "Environment inspection", Kind: "access", Status: "unavailable", Detail: "This runtime does not expose environment inspection."},
	}}, nil
}

func (a *CodexAdapter) inspectEnvironment(ctx context.Context, spec OpenSpec) (EnvironmentReport, error) {
	l, err := a.resolveCommand()
	if err != nil {
		return missingEnvironment(AgentTypeCodex), nil
	}
	defer l.releaseLease()
	// An account's session reports from its own home; the built-in account
	// reports from the adapter's.
	home := a.opts.Home
	if spec.AccountHome != "" {
		home = spec.AccountHome
	}
	return (environmentSnapshot{Agent: AgentTypeCodex, Spec: spec, Env: codexEnv(l, spec, home), Command: l.path, SkillHome: home}).report(ctx, "next")
}

func (a *ClaudeAdapter) inspectEnvironment(ctx context.Context, spec OpenSpec) (EnvironmentReport, error) {
	l, err := a.resolveCommand()
	if err != nil {
		return missingEnvironment(AgentTypeClaude), nil
	}
	defer l.releaseLease()
	pol, _ := sandboxFor(spec.Cwd, l, spec)
	return (environmentSnapshot{Agent: AgentTypeClaude, Spec: spec, Env: pol.Env, Command: l.path}).report(ctx, "next")
}

func missingEnvironment(vendor AgentType) EnvironmentReport {
	return EnvironmentReport{Agent: string(vendor), Scope: "next", CheckedAt: time.Now().UnixMilli(), Entries: []EnvironmentEntry{
		{Name: "Agent runtime", Kind: "access", Status: "unavailable", Detail: "The runtime could not be resolved.", Action: "Install or repair this agent in Settings → Agents, then check again."},
	}}
}

func (r *claudeRuntime) reportEnvironment(ctx context.Context) (EnvironmentReport, error) {
	r.mu.Lock()
	snapshot, closed := r.environment, r.closed
	r.mu.Unlock()
	if snapshot == nil {
		return missingEnvironment(AgentTypeClaude), nil
	}
	scope := "session"
	if closed {
		scope = "next"
	}
	return snapshot.report(ctx, scope)
}

func (r *codexRuntime) reportEnvironment(ctx context.Context) (EnvironmentReport, error) {
	r.mu.Lock()
	snapshot, conn, closed := r.environment, r.conn, r.closed
	r.mu.Unlock()
	if snapshot == nil {
		return missingEnvironment(AgentTypeCodex), nil
	}
	scope := "session"
	if closed {
		scope = "next"
	}
	report, err := snapshot.report(ctx, scope)
	if err != nil || closed || conn == nil {
		return report, err
	}
	// Independent metadata requests: neither submits a prompt nor executes a
	// skill/tool. Bound each call so an older runtime cannot hang the dialog.
	readCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	var skills struct {
		Data *[]struct {
			Skills []struct {
				Name    string `json:"name"`
				Path    string `json:"path"`
				Enabled *bool  `json:"enabled"`
			} `json:"skills"`
		} `json:"data"`
	}
	err = conn.Call(readCtx, "skills/list", map[string]any{"cwds": []string{snapshot.Spec.Cwd}}, &skills)
	cancel()
	if err == nil && skills.Data != nil {
		report.Entries = withoutEnvironmentKind(report.Entries, "skills")
		for _, group := range *skills.Data {
			for _, skill := range group.Skills {
				status, detail := "loaded", "Reported by this runtime."
				if skill.Enabled != nil && !*skill.Enabled {
					status, detail = "disabled", "Disabled in the runtime's skill configuration."
				}
				addEnvironmentEntry(&report, EnvironmentEntry{Name: clipEnvironmentText(skill.Name), Kind: "skills", Status: status, Source: environmentSkillPath(skill.Path), Detail: detail})
			}
		}
	} else {
		addEnvironmentEntry(&report, EnvironmentEntry{Name: "Skill loading", Kind: "skills", Status: "unverified", Detail: "The runtime did not answer the skill inventory. Configured files are shown instead.", Action: "Refresh, or check the agent runtime version in Settings → Agents."})
	}
	readCtx, cancel = context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var servers struct {
		Data *[]struct {
			Name       string                     `json:"name"`
			AuthStatus string                     `json:"authStatus"`
			Tools      map[string]json.RawMessage `json:"tools"`
		} `json:"data"`
		NextCursor *string `json:"nextCursor"`
	}
	if err := conn.Call(readCtx, "mcpServerStatus/list", map[string]any{"limit": 100}, &servers); err == nil && servers.Data != nil {
		report.Entries = withoutEnvironmentKind(report.Entries, "mcp")
		for _, server := range *servers.Data {
			detail, status, action := "Runtime reports this server; authentication has not been tested.", "reported", ""
			if len(server.Tools) > 0 {
				detail = strconv.Itoa(len(server.Tools)) + " tools advertised by this runtime; individual calls have not been tested."
			}
			if server.AuthStatus == "notLoggedIn" {
				status, detail, action = "needs_auth", "Runtime reports that this server is not signed in.", "Authenticate this MCP server in the agent's DevKit environment, then refresh."
			}
			addEnvironmentEntry(&report, EnvironmentEntry{Name: clipEnvironmentText(server.Name), Kind: "mcp", Status: status, Detail: detail, Action: action})
		}
		if servers.NextCursor != nil && *servers.NextCursor != "" {
			addEnvironmentEntry(&report, EnvironmentEntry{Name: "More MCP servers", Kind: "mcp", Status: "unverified", Detail: "Only the first 100 servers are shown."})
		}
	} else {
		addEnvironmentEntry(&report, EnvironmentEntry{Name: "MCP connection status", Kind: "mcp", Status: "unverified", Detail: "The runtime did not answer. Server names below come from configuration; connectivity and authentication are unverified."})
	}
	return report, ctx.Err()
}

func (snap environmentSnapshot) report(ctx context.Context, scope string) (EnvironmentReport, error) {
	report := EnvironmentReport{Agent: string(snap.Agent), Scope: scope, CheckedAt: time.Now().UnixMilli(), Entries: []EnvironmentEntry{}}
	shared := snap.Spec.PrivateHome != nil && !*snap.Spec.PrivateHome
	mode, detail, action := "configured", "Personal configuration and CLI environment are inherited.", ""
	if !shared {
		mode, detail, action = "restricted", "Personal environment inheritance is off.", "Enable Share home folder in Settings → Agents → Sandbox, then reopen the session."
	}
	addEnvironmentEntry(&report, EnvironmentEntry{Name: "Personal environment", Kind: "access", Status: mode, Detail: detail, Action: action})
	addEnvironmentEntry(&report, EnvironmentEntry{Name: "Agent runtime", Kind: "access", Status: "found", Source: snap.Command, Detail: "Resolved runtime executable. Login and model access are checked separately."})
	if snap.Spec.Cwd != "" {
		addEnvironmentEntry(&report, EnvironmentEntry{Name: "Working directory", Kind: "access", Status: "configured", Source: snap.Spec.Cwd, Detail: "The session may write here and in its additional granted folders."})
	}
	for _, path := range snap.Spec.ExtraReadWritePaths {
		addEnvironmentEntry(&report, EnvironmentEntry{Name: "Additional writable folder", Kind: "access", Status: "configured", Source: path, Detail: "Granted when this session was created."})
	}
	for _, tool := range []string{"git", "rg", "gh", "glab", "docker", "node", "bun", "go", "python3"} {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		entry := EnvironmentEntry{Name: tool, Kind: "tools", Status: "missing", Detail: "Not found on the agent's PATH. This tool is optional.", Action: "Install it or add its directory to DevKit's launch PATH, then reopen the session."}
		for _, dir := range filepath.SplitList(snap.Env["PATH"]) {
			if !filepath.IsAbs(dir) {
				continue
			}
			path := filepath.Join(dir, tool)
			if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
				entry.Status, entry.Source, entry.Detail, entry.Action = "found", path, "Executable found. Execution and authentication have not been tested.", ""
				break
			}
		}
		addEnvironmentEntry(&report, entry)
	}
	host, _ := os.UserHomeDir()
	switch snap.Agent {
	case AgentTypeCodex:
		if shared {
			scanSkillRoot(ctx, &report, filepath.Join(host, ".codex", "skills"))
			scanSkillRoot(ctx, &report, filepath.Join(host, ".agents", "skills"))
			scanMCPConfig(&report, filepath.Join(host, ".codex", "config.toml"), true, snap.Spec.Cwd)
		}
		if snap.SkillHome != "" {
			scanSkillRoot(ctx, &report, filepath.Join(snap.SkillHome, "skills"))
		}
		if snap.Spec.Cwd != "" {
			scanMCPConfig(&report, filepath.Join(snap.Spec.Cwd, ".codex", "config.toml"), true, snap.Spec.Cwd)
		}
	case AgentTypeClaude:
		configDir := snap.Env["CLAUDE_CONFIG_DIR"]
		if configDir == "" {
			configDir = filepath.Join(host, ".claude")
		}
		scanSkillRoot(ctx, &report, filepath.Join(configDir, "skills"))
		scanMCPConfig(&report, filepath.Join(host, ".claude.json"), false, snap.Spec.Cwd)
		if shared {
			scanSkillRoot(ctx, &report, filepath.Join(host, ".agents", "skills"))
		}
		if snap.Spec.Cwd != "" {
			scanSkillRoot(ctx, &report, filepath.Join(snap.Spec.Cwd, ".claude", "skills"))
			scanMCPConfig(&report, filepath.Join(snap.Spec.Cwd, ".mcp.json"), false, snap.Spec.Cwd)
		}
	}
	if snap.Spec.Cwd != "" {
		scanSkillRoot(ctx, &report, filepath.Join(snap.Spec.Cwd, ".agents", "skills"))
	}
	for _, server := range snap.Spec.McpServers {
		addEnvironmentEntry(&report, EnvironmentEntry{Name: server.Name, Kind: "mcp", Status: "configured", Detail: "Provided by DevKit to this session."})
	}
	customizationEnvironment(ctx, &report, snap.Spec.Customizations[string(snap.Agent)])
	return report, ctx.Err()
}

func readEnvironmentFile(path string, limit int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(data)) > limit {
		return nil, errors.New("file too large")
	}
	return data, err
}

func scanSkillRoot(ctx context.Context, report *EnvironmentReport, root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			addEnvironmentEntry(report, EnvironmentEntry{Name: "Skill directory", Kind: "skills", Status: "unverified", Source: root, Detail: "Could not read this skill directory.", Action: "Check directory permissions, then refresh."})
		}
		return
	}
	for _, entry := range entries {
		if ctx.Err() != nil || len(report.Entries) >= 300 {
			return
		}
		path := filepath.Join(root, entry.Name(), "SKILL.md")
		if entry.Name() == ".system" {
			scanSkillRoot(ctx, report, filepath.Join(root, entry.Name()))
			continue
		}
		data, err := readEnvironmentFile(path, 64*1024)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			continue
		}
		text := strings.ReplaceAll(string(data), "\r\n", "\n")
		var meta struct {
			Name        string `yaml:"name"`
			Description string `yaml:"description"`
		}
		body, hasHeader := strings.CutPrefix(text, "---\n")
		header, _, hasEnd := strings.Cut(body, "\n---")
		if !hasHeader || !hasEnd || yaml.Unmarshal([]byte(header), &meta) != nil || strings.TrimSpace(meta.Name) == "" || strings.TrimSpace(meta.Description) == "" {
			addEnvironmentEntry(report, EnvironmentEntry{Name: entry.Name(), Kind: "skills", Status: "unverified", Source: path, Detail: "SKILL.md has missing or invalid metadata.", Action: "Add a valid name and description in the skill's YAML frontmatter."})
			continue
		}
		addEnvironmentEntry(report, EnvironmentEntry{Name: clipEnvironmentText(meta.Name), Kind: "skills", Status: "configured", Source: path, Detail: "Skill file found; runtime loading has not been verified."})
	}
}

func scanMCPConfig(report *EnvironmentReport, path string, isTOML bool, cwd string) {
	data, err := readEnvironmentFile(path, 1024*1024)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	var config map[string]any
	if err == nil {
		if isTOML {
			err = toml.Unmarshal(data, &config)
		} else {
			err = json.Unmarshal(data, &config)
		}
	}
	if err != nil {
		addEnvironmentEntry(report, EnvironmentEntry{Name: "MCP configuration", Kind: "mcp", Status: "unverified", Source: path, Detail: "Could not read or parse this configuration.", Action: "Check its format and file permissions, then refresh."})
		return
	}
	key := "mcpServers"
	if isTOML {
		key = "mcp_servers"
	}
	appendNames := func(value any) {
		if servers, ok := value.(map[string]any); ok {
			var names []string
			for name := range servers {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				if name == "devkit" {
					continue
				}
				addEnvironmentEntry(report, EnvironmentEntry{Name: clipEnvironmentText(name), Kind: "mcp", Status: "configured", Source: path, Detail: "Server configuration found. Connectivity and authentication have not been tested."})
			}
		}
	}
	appendNames(config[key])
	if projects, ok := config["projects"].(map[string]any); ok {
		if project, ok := projects[cwd].(map[string]any); ok {
			appendNames(project[key])
		}
	}
}

// Paths used by management controls must remain exact, rather than prose-clipped.
func environmentSkillPath(path string) string {
	if len(path) > 4096 || !filepath.IsAbs(path) {
		return ""
	}
	return path
}

func clipEnvironmentText(text string) string {
	r := []rune(strings.TrimSpace(text))
	if len(r) > 240 {
		r = r[:240]
	}
	return string(r)
}
func addEnvironmentEntry(report *EnvironmentReport, entry EnvironmentEntry) {
	if entry.Kind == "skills" && strings.HasSuffix(entry.Source, "SKILL.md") {
		if real, err := filepath.EvalSymlinks(entry.Source); err == nil {
			entry.Source = real
		}
	}
	if len(report.Entries) >= 300 {
		return
	}
	for _, existing := range report.Entries {
		if existing.Kind == entry.Kind && existing.Name == entry.Name && existing.Source == entry.Source {
			return
		}
	}
	report.Entries = append(report.Entries, entry)
}
func withoutEnvironmentKind(entries []EnvironmentEntry, kind string) []EnvironmentEntry {
	out := make([]EnvironmentEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Kind != kind {
			out = append(out, entry)
		}
	}
	return out
}
