package agenthub

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestCodexSharedHomeInheritsCustomizationWithoutHostIdentity(t *testing.T) {
	host := t.TempDir()
	t.Setenv("HOME", host)
	t.Setenv("GH_TOKEN", "test-cli-token")
	t.Setenv("OPENAI_API_KEY", "must-not-select-provider")
	userHome := filepath.Join(host, ".codex")
	skill := filepath.Join(userHome, "skills", "review")
	for _, dir := range []string{skill, filepath.Join(userHome, "plugins"), filepath.Join(userHome, "sessions")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	config := `model_provider = "other"
approval_policy = "never"
[mcp_servers.docs]
url = "https://example.test/mcp"
[mcp_servers.devkit]
url = "https://wrong.test/mcp"
[features]
plugins = true
[projects."/repo"]
trust_level = "trusted"
`
	for path, body := range map[string]string{
		filepath.Join(userHome, "config.toml"):       config,
		filepath.Join(userHome, "AGENTS.md"):         "Use the review skill.",
		filepath.Join(userHome, "auth.json"):         "host-sign-in",
		filepath.Join(skill, "SKILL.md"):             "---\nname: review\ndescription: Review code\n---\nRead helpers/check.txt",
		filepath.Join(skill, "helpers", "check.txt"): "helper content",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f := newCodexFixture(t, "turn")
	if err := os.MkdirAll(f.home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.home, "auth.json"), []byte("devkit-sign-in"), 0o600); err != nil {
		t.Fatal(err)
	}
	shared := false
	pol, cmd, err := f.adapter.jail(f.cwd, launch{path: "/bin/cat"}, OpenSpec{PrivateHome: &shared}, codexStoreFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := pol.Validate(); err != nil {
		t.Fatalf("invalid inherited policy: %v", err)
	}
	if pol.Env["GH_TOKEN"] != "test-cli-token" || pol.Env["CODEX_HOME"] != f.home {
		t.Fatal("CLI environment or managed home missing")
	}
	if _, ok := pol.Env["OPENAI_API_KEY"]; ok {
		t.Fatal("host provider identity was inherited")
	}
	if !pol.AllowLoopbackListeners {
		t.Fatal("agent cannot run a development server")
	}
	if !pol.AllowKeychain {
		t.Fatal("shared CLI environment cannot use keychain-backed authentication")
	}
	if strings.Contains(strings.Join(cmd.Args, " "), "--disable") {
		t.Fatal("native extensions were disabled")
	}
	for _, dir := range []string{"skills", "plugins"} {
		if !slices.Contains(pol.ReadOnlyPaths, filepath.Join(userHome, dir)) {
			t.Fatalf("%s not granted", dir)
		}
	}
	if slices.Contains(pol.DenyReadPaths, userHome) {
		t.Fatal("customization is still hidden")
	}
	for _, path := range []string{"auth.json", "sessions"} {
		if !slices.Contains(pol.DenyReadPaths, filepath.Join(userHome, path)) {
			t.Fatalf("host %s must remain private", path)
		}
	}
	data, err := os.ReadFile(filepath.Join(f.home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	var inherited map[string]any
	if err := toml.Unmarshal(data, &inherited); err != nil {
		t.Fatal(err)
	}
	servers := inherited["mcp_servers"].(map[string]any)
	if _, ok := servers["docs"]; !ok {
		t.Fatal("external MCP missing")
	}
	if _, ok := servers["devkit"]; ok {
		t.Fatal("host MCP overrides the identity-bound endpoint")
	}
	for _, key := range []string{"model_provider", "approval_policy"} {
		if _, ok := inherited[key]; ok {
			t.Fatalf("%s overrides DevKit's runtime policy", key)
		}
	}
	for path, want := range map[string]string{
		"auth.json":                       "devkit-sign-in",
		"AGENTS.md":                       "Use the review skill.",
		"skills/review/helpers/check.txt": "helper content",
	} {
		got, err := os.ReadFile(filepath.Join(f.home, path))
		if err != nil || string(got) != want {
			t.Fatalf("%s not preserved/imported: %v", path, err)
		}
	}
	// Starting again must retain both user skills and system skills.
	if err := os.MkdirAll(filepath.Join(f.home, "skills", ".system", "bundled"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.adapter.jail(f.cwd, launch{path: "/bin/cat"}, OpenSpec{PrivateHome: &shared}, codexStoreFile); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(f.home, "skills", ".system", "bundled")); err != nil {
		t.Fatal("system skills were erased")
	}
	// Turning ShareHome off removes portable config and denies the user source.
	private := true
	pol, _, err = f.adapter.jail(f.cwd, launch{path: "/bin/cat"}, OpenSpec{PrivateHome: &private}, codexStoreFile)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(pol.DenyReadPaths, userHome) {
		t.Fatal("private session exposes user customization")
	}
	if pol.AllowKeychain {
		t.Fatal("private Codex session exposes the login keychain")
	}
	if _, ok := pol.Env["GH_TOKEN"]; ok {
		t.Fatal("private session inherited CLI credentials")
	}
	if got, _ := os.ReadFile(filepath.Join(f.home, "config.toml")); string(got) != codexConfigHeader {
		t.Fatal("private session kept shared MCP config")
	}
}

func TestCodexInvalidUserConfigDoesNotExposeItsContents(t *testing.T) {
	home, userHome := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(userHome, "config.toml"), []byte("token = secret-invalid-toml"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := inheritCodexCustomization(home, userHome)
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("config error missing or exposes content")
	}
}

func TestClaudeSharedHomeInheritsCLIEnvironment(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "test-cli-token")
	shared := false
	pol, _ := sandboxFor(t.TempDir(), launch{path: "/bin/cat"}, OpenSpec{PrivateHome: &shared})
	if pol.Env["GITLAB_TOKEN"] != "test-cli-token" || !pol.AllowLoopbackListeners {
		t.Fatal("shared CLI environment is incomplete")
	}
	private := true
	pol, _ = sandboxFor(t.TempDir(), launch{path: "/bin/cat"}, OpenSpec{PrivateHome: &private})
	if _, ok := pol.Env["GITLAB_TOKEN"]; ok {
		t.Fatal("private session inherited CLI credentials")
	}
}
