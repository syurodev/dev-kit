package agenthub

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"devkit/internal/preferences"
	"github.com/pelletier/go-toml/v2"
)

// A model profile can also be a vendor subagent: Claude's Agent tool, Codex's
// spawn_agent. The person turns that on per profile (Settings → Agents → Models
// by job); DevKit hands each session the profiles of its own vendor, translated
// into that vendor's shape (adapter_claude.go, codex_subagents.go).
//
// This is not agenthub.createAgentSession. A subagent lives inside the vendor's
// session, runs on the vendor's runtime and returns its answer to the caller;
// a child session is a separate DevKit session the person can open. A Claude
// subagent cannot run on a Codex model, so a profile only reaches its vendor.
//
// A subagent definition carries no permission. It is a description, a model, an
// effort and the person's instructions; the tools it calls pass the same gates
// as the session's own (GateOwnedMCP, the PreToolUse hook, the sandbox). The
// translators never write a permission mode, an MCP server or a tool list.

// SubagentSpec is one subagent for one session: already checked, already
// restricted to the session's vendor, in no vendor's shape.
type SubagentSpec struct {
	// Name is what the vendor's agent calls it.
	Name string
	// Description tells the calling model when to use it.
	Description string
	// Prompt is the person's text, passed as written.
	Prompt string
	// Model is the vendor's model id; empty leaves the vendor's default.
	Model string
	// Effort is empty to leave the model's own.
	Effort string
}

// SubagentStatus values.
const (
	// SubagentActive: new sessions of the profile's vendor will have it.
	SubagentActive = "active"
	// SubagentSkipped: switched on, but left out; Reason says why.
	SubagentSkipped = "skipped"
)

// SubagentState is what becomes of one profile's subagent.
type SubagentState struct {
	Name   string `json:"name,omitempty"`
	Status string `json:"status,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// claudeSubagentEfforts are the names Claude's AgentDefinition takes for effort.
var claudeSubagentEfforts = []string{"low", "medium", "high", "xhigh", "max"}

var subagentValue = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:\[\]/@-]*$`)

// planSubagents decides, for every profile switched on, what becomes of it.
// states is aligned with profiles (empty for a profile that is not a subagent);
// specs are the ones of vendor that can be handed to a session of that vendor.
//
// catalog is that vendor's model list when it is at hand; without it a model is
// not checked against the vendor, only against what its translator can carry.
// taken holds names the person already uses for their own subagents: a profile
// never shares one, since the programmatic definition would shadow theirs.
func planSubagents(profiles []preferences.AgentModelProfile, vendor AgentType, catalog *VendorModels, taken map[string]bool) ([]SubagentSpec, []SubagentState) {
	names := preferences.SubagentNames(profiles)
	states := make([]SubagentState, len(profiles))
	var specs []SubagentSpec
	for i, p := range profiles {
		if !p.Subagent {
			continue
		}
		states[i].Name = names[i]
		if AgentType(p.Vendor) != vendor {
			// Not an error: it is for the other vendor's sessions. The caller
			// plans each vendor in turn and merges, so say nothing here.
			states[i] = SubagentState{}
			continue
		}
		reason := subagentProblem(p, vendor, catalog)
		if taken[strings.ToLower(names[i])] {
			reason = fmt.Sprintf("you already have a subagent called %q of your own; rename this job, or that agent", names[i])
		}
		if reason != "" {
			states[i].Status, states[i].Reason = SubagentSkipped, reason
			continue
		}
		states[i].Status = SubagentActive
		specs = append(specs, SubagentSpec{Name: names[i], Description: p.Description, Prompt: p.Prompt, Model: p.Model, Effort: p.Effort})
	}
	return specs, states
}

// subagentProblem is why a profile cannot be offered to its vendor, or "".
func subagentProblem(p preferences.AgentModelProfile, vendor AgentType, catalog *VendorModels) string {
	if !subagentValue.MatchString(p.Model) {
		return fmt.Sprintf("the model %q is not a name the agent can take", p.Model)
	}
	if p.Effort != "" && !subagentValue.MatchString(p.Effort) {
		return fmt.Sprintf("the effort %q is not a name the agent can take", p.Effort)
	}
	if vendor == AgentTypeClaude && p.Effort != "" && !containsString(claudeSubagentEfforts, p.Effort) {
		return fmt.Sprintf("Claude subagents take effort %s, not %q", valuesText(claudeSubagentEfforts), p.Effort)
	}
	if catalog != nil && catalog.Status == ModelsOK {
		model, ok := catalog.Model(p.Model)
		if !ok {
			return fmt.Sprintf("%s no longer offers %q (it offers: %s)", vendor, p.Model, valuesText(catalog.ModelValues()))
		}
		if p.Effort != "" && !containsString(model.Efforts, p.Effort) {
			return fmt.Sprintf("%q does not take effort %q (it takes: %s)", p.Model, p.Effort, valuesText(model.Efforts))
		}
	}
	return ""
}

// userSubagentNames are the names of subagents the person keeps themselves for
// vendor, as far as their files say: Claude's .claude/agents/*.md in the user's
// home (when the session shares it) and in the project; Codex's .codex/agents/
// and `[agents.<name>]` of config.toml in the same two places. Best effort: an
// unreadable file adds nothing. Lower-cased.
func userSubagentNames(vendor AgentType, cwd string, sharedHome bool) map[string]bool {
	taken := map[string]bool{}
	var roots []string
	if sharedHome {
		if home, err := os.UserHomeDir(); err == nil {
			roots = append(roots, home)
		}
	}
	if cwd != "" {
		roots = append(roots, cwd)
	}
	for _, root := range roots {
		switch vendor {
		case AgentTypeClaude:
			addAgentFileNames(taken, filepath.Join(root, ".claude", "agents"), ".md")
		case AgentTypeCodex:
			addAgentFileNames(taken, filepath.Join(root, ".codex", "agents"), ".toml")
			data, err := os.ReadFile(filepath.Join(root, ".codex", "config.toml"))
			if err != nil {
				continue
			}
			var config struct {
				Agents map[string]any `toml:"agents"`
			}
			if toml.Unmarshal(data, &config) == nil {
				for name := range config.Agents {
					taken[strings.ToLower(name)] = true
				}
			}
		}
	}
	return taken
}

// addAgentFileNames reads the names agent files in dir go by: the file's stem,
// and a `name:` in its front matter or `name = ` in its TOML.
func addAgentFileNames(taken map[string]bool, dir, ext string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ext {
			continue
		}
		taken[strings.ToLower(strings.TrimSuffix(entry.Name(), ext))] = true
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		head := string(data)
		if len(head) > 4096 {
			head = head[:4096]
		}
		for _, line := range strings.Split(head, "\n") {
			key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
			if ext == ".toml" {
				key, value, ok = strings.Cut(strings.TrimSpace(line), "=")
			}
			if ok && strings.TrimSpace(key) == "name" {
				if name := strings.Trim(strings.TrimSpace(value), `"' `); name != "" {
					taken[strings.ToLower(name)] = true
				}
				break
			}
		}
	}
}

func (s *Service) homeShared() bool {
	if s.SandboxExtras == nil {
		return false
	}
	return s.SandboxExtras().HomeShared()
}

// applySubagents hands a session the subagents of its vendor. It runs where an
// OpenSpec is built, after applySandboxExtras (which decides whether the home
// is shared). It reads only what is already cached — a cold model list is not
// worth holding a session back, so the first session checks the model against
// the translator's rules only and the list is fetched for the next one. It
// never fails: a profile that cannot be offered is left out and logged.
func (s *Service) applySubagents(spec *OpenSpec, vendor AgentType) {
	s.applySubagentLimits(spec, vendor)
	profiles := s.modelProfiles()
	wanted := false
	for _, p := range profiles {
		if p.Subagent && AgentType(p.Vendor) == vendor {
			wanted = true
			break
		}
	}
	if !wanted {
		return
	}
	var catalog *VendorModels
	if cached, ok := s.catalog.get(vendor); ok {
		catalog = &cached
	} else {
		go s.ModelCatalog(context.Background(), vendor)
	}
	shared := spec.PrivateHome != nil && !*spec.PrivateHome
	specs, states := planSubagents(profiles, vendor, catalog, userSubagentNames(vendor, spec.Cwd, shared))
	for i, state := range states {
		if state.Status == SubagentSkipped {
			log.Printf("agenthub: %s subagent %q (profile %q) left out: %s", vendor, state.Name, profiles[i].ID, state.Reason)
		}
	}
	spec.Subagents = specs
}
