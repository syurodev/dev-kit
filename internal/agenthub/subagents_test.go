package agenthub

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devkit/internal/preferences"
	"github.com/pelletier/go-toml/v2"
)

func subProfile(id, vendor, model, effort string) preferences.AgentModelProfile {
	return preferences.AgentModelProfile{
		ID: id, Name: id, Description: "use " + id, Vendor: vendor, Model: model, Effort: effort,
		Subagent: true, Prompt: "You are " + id + ". {{git.commit}} stays text.",
	}
}

func TestPlanSubagentsKeepsToVendorAndLeavesOutWhatCannotRun(t *testing.T) {
	models := &VendorModels{Vendor: "mock", Status: ModelsOK, Models: []ModelChoice{{Value: "m-small", Efforts: []string{"low", "medium"}}}}
	off := subProfile("off", "mock", "m-small", "")
	off.Subagent = false
	profiles := []preferences.AgentModelProfile{
		subProfile("review", "mock", "m-small", "low"),
		subProfile("gone", "mock", "m-gone", ""),
		subProfile("effort", "mock", "m-small", "high"),
		subProfile("codex-one", "codex", "gpt-x", ""),
		off,
	}
	specs, states := planSubagents(profiles, "mock", models, map[string]bool{})
	if len(specs) != 1 || specs[0].Name != "review" || specs[0].Model != "m-small" || specs[0].Effort != "low" {
		t.Fatalf("specs = %+v", specs)
	}
	if specs[0].Prompt != "You are review. {{git.commit}} stays text." {
		t.Errorf("the person's text was changed: %q", specs[0].Prompt)
	}
	if states[0].Status != SubagentActive || states[1].Status != SubagentSkipped || !strings.Contains(states[1].Reason, "no longer offers") {
		t.Errorf("states = %+v", states)
	}
	if states[2].Status != SubagentSkipped || !strings.Contains(states[2].Reason, "low, medium") {
		t.Errorf("a wrong effort must name what the model takes: %+v", states[2])
	}
	if states[3].Status != "" || states[4].Status != "" {
		t.Errorf("another vendor's profile and one that is off are not this session's business: %+v %+v", states[3], states[4])
	}

	// A subagent the person keeps under the same name wins: ours is left out.
	_, states = planSubagents(profiles[:1], "mock", models, map[string]bool{"review": true})
	if states[0].Status != SubagentSkipped || !strings.Contains(states[0].Reason, "already have a subagent") {
		t.Errorf("taken name: %+v", states[0])
	}
	// Claude's effort names are fixed, with or without a model list.
	_, states = planSubagents([]preferences.AgentModelProfile{subProfile("a", "claude", "sonnet", "ultra")}, AgentTypeClaude, nil, nil)
	if states[0].Status != SubagentSkipped || !strings.Contains(states[0].Reason, "low, medium, high, xhigh, max") {
		t.Errorf("claude effort: %+v", states[0])
	}
	// A model name that cannot be a config value is refused, not escaped.
	_, states = planSubagents([]preferences.AgentModelProfile{subProfile("b", "codex", "x\"; sandbox_mode=\"danger", "")}, AgentTypeCodex, nil, nil)
	if states[0].Status != SubagentSkipped {
		t.Errorf("hostile model: %+v", states[0])
	}
}

func TestClaudeSubagentsMetaWritesOnlyWhatCarriesNoPermission(t *testing.T) {
	base := sessionNewMeta("/bin/devkit", "prompt", "mcp__devkit__git_commit")
	meta := claudeSubagentsMeta(base, []SubagentSpec{
		{Name: "review", Description: "reading code", Prompt: "Be brief.", Model: "haiku", Effort: "low"},
		{Name: "plain", Description: "d", Prompt: "p", Model: "default"},
	})
	var body struct {
		ClaudeCode struct {
			Options map[string]any `json:"options"`
		} `json:"claudeCode"`
	}
	if err := json.Unmarshal(meta, &body); err != nil {
		t.Fatal(err)
	}
	agents, _ := body.ClaudeCode.Options["agents"].(map[string]any)
	review, _ := agents["review"].(map[string]any)
	if review["model"] != "haiku" || review["effort"] != "low" || review["prompt"] != "Be brief." || review["description"] != "reading code" {
		t.Fatalf("review = %v", review)
	}
	for name, def := range agents {
		for key := range def.(map[string]any) {
			switch key {
			case "description", "prompt", "model", "effort":
			default:
				t.Errorf("%s: a subagent definition carries %q — it must not set tools, permissions or servers", name, key)
			}
		}
	}
	if plain := agents["plain"].(map[string]any); plain["model"] != nil {
		t.Errorf("model 'default' means the main model: %v", plain)
	}
	// The rest of the options — the gate's allowedTools, the hook — are as they were.
	if body.ClaudeCode.Options["allowedTools"] == nil || body.ClaudeCode.Options["settings"] == nil || body.ClaudeCode.Options["settingSources"] == nil {
		t.Errorf("options lost: %v", body.ClaudeCode.Options)
	}
	if string(claudeSubagentsMeta(base, nil)) != string(base) {
		t.Error("with no subagents the meta must be byte-identical")
	}
}

func TestCodexSubagentsAreRoleFilesRegisteredOnArgv(t *testing.T) {
	home := t.TempDir()
	specs := []SubagentSpec{{Name: "review", Description: "reading \"code\"\nsecond line", Prompt: "Be brief.\nStop.", Model: "gpt-x", Effort: "low"}}
	args, deny, err := writeCodexSubagents(home, specs)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, codexSubagentsDir)
	if len(deny) != 1 || deny[0] != dir {
		t.Errorf("the jail must not write the roles: %v", deny)
	}
	if len(args) != 4 || args[0] != "-c" || !strings.HasPrefix(args[1], "agents.review.description=") || !strings.HasPrefix(args[3], "agents.review.config_file=") {
		t.Fatalf("args = %q", args)
	}
	var desc, file string
	if err := json.Unmarshal([]byte(strings.TrimPrefix(args[1], "agents.review.description=")), &desc); err != nil || desc != specs[0].Description {
		t.Errorf("description round trip: %q, %v", desc, err)
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(args[3], "agents.review.config_file=")), &file); err != nil || !strings.HasPrefix(file, dir) {
		t.Fatalf("config_file = %q, %v", file, err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var role map[string]any
	if err := toml.Unmarshal(data, &role); err != nil {
		t.Fatal(err)
	}
	if len(role) != 3 || role["model"] != "gpt-x" || role["model_reasoning_effort"] != "low" || role["developer_instructions"] != "Be brief.\nStop." {
		t.Errorf("a role file holds exactly model, effort and instructions — never a sandbox or a server: %v", role)
	}
	if info, _ := os.Stat(file); info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", info.Mode())
	}

	// Named by content: an edit makes a new file and leaves the one a running
	// session was started with; an old unreferenced file is swept.
	specs[0].Prompt = "Changed."
	args2, _, err := writeCodexSubagents(home, specs)
	if err != nil || args2[3] == args[3] {
		t.Fatalf("an edited role must be a new file: %q %v", args2, err)
	}
	if _, err := os.Stat(file); err != nil {
		t.Errorf("the file a running session uses was removed: %v", err)
	}
	old := time.Now().Add(-2 * codexRoleRetention)
	if err := os.Chtimes(file, old, old); err != nil {
		t.Fatal(err)
	}
	if _, _, err := writeCodexSubagents(home, specs); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("an old unreferenced role was kept: %v", err)
	}
	if args, deny, err := writeCodexSubagents(home, nil); args != nil || deny != nil || err != nil {
		t.Errorf("no subagents must leave no trace: %v %v %v", args, deny, err)
	}
}

func TestUserSubagentNamesReadsTheirOwnFiles(t *testing.T) {
	cwd := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	write := func(path, text string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(cwd, ".claude", "agents", "Reviewer.md"), "---\nname: code-reviewer\n---\nbody")
	write(filepath.Join(home, ".claude", "agents", "docs.md"), "no front matter")
	write(filepath.Join(cwd, ".codex", "agents", "helper.toml"), "name = \"fixer\"\ndeveloper_instructions = \"x\"")
	write(filepath.Join(home, ".codex", "config.toml"), "[agents.Scout]\ndescription = \"d\"\n")

	claude := userSubagentNames(AgentTypeClaude, cwd, true)
	if !claude["reviewer"] || !claude["code-reviewer"] || !claude["docs"] || claude["helper"] {
		t.Errorf("claude = %v", claude)
	}
	if only := userSubagentNames(AgentTypeClaude, cwd, false); only["docs"] {
		t.Errorf("an unshared home is not read: %v", only)
	}
	codex := userSubagentNames(AgentTypeCodex, cwd, true)
	if !codex["helper"] || !codex["fixer"] || !codex["scout"] || codex["reviewer"] {
		t.Errorf("codex = %v", codex)
	}
}

func TestASessionOnlyGetsItsOwnVendorsSubagents(t *testing.T) {
	f := newChoiceFixture(t)
	f.profile.Profiles = []preferences.AgentModelProfile{
		subProfile("review", "mock", "m-small", "low"),
		subProfile("elsewhere", "codex", "gpt-x", ""),
	}
	off := subProfile("quiet", "mock", "m-small", "")
	off.Subagent = false
	f.profile.Profiles = append(f.profile.Profiles, off)
	if _, err := f.svc.CreateSession(context.Background(), CreateInput{AgentType: AgentTypeMock, Cwd: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	got := f.adapter.LastSpec().Subagents
	if len(got) != 1 || got[0].Name != "review" || got[0].Effort != "low" {
		t.Fatalf("subagents = %+v", got)
	}

	// With everything off, nothing changes.
	f.profile.Profiles = []preferences.AgentModelProfile{off}
	if _, err := f.svc.CreateSession(context.Background(), CreateInput{AgentType: AgentTypeMock, Cwd: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if got := f.adapter.LastSpec().Subagents; got != nil {
		t.Fatalf("a switched-off profile reached a session: %+v", got)
	}
}

func TestListingSaysWhichProfilesAreSubagentsAndWhy(t *testing.T) {
	f := newChoiceFixture(t)
	f.profile.Profiles = []preferences.AgentModelProfile{
		subProfile("review", "mock", "m-small", "low"),
		subProfile("old", "mock", "m-gone", ""),
		{ID: "plain", Name: "Plain", Vendor: "mock", Model: "m-mid"},
	}
	listing := f.svc.ListModelProfiles(f.ctx)
	by := map[string]ModelProfileInfo{}
	for _, p := range listing.Profiles {
		by[p.ID] = p
	}
	if r := by["review"]; !r.Subagent || r.SubagentName != "review" || r.SubagentStatus != SubagentActive {
		t.Errorf("review = %+v", r)
	}
	if o := by["old"]; o.SubagentStatus != SubagentSkipped || !strings.Contains(o.SubagentProblem, "no longer offers") {
		t.Errorf("old = %+v", o)
	}
	if p := by["plain"]; p.Subagent || p.SubagentName != "" || p.SubagentStatus != "" {
		t.Errorf("a profile that is off says nothing: %+v", p)
	}
	for _, p := range listing.Profiles {
		if p.Prompt != "" {
			t.Errorf("an agent's listing must not carry the instructions: %+v", p)
		}
	}
	if !strings.Contains(listing.Note, "subagentName") {
		t.Errorf("note = %s", listing.Note)
	}
}
