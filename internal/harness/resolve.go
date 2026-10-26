package harness

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// Resolve merges V, W and S over the registry. It rejects an invalid input
// rather than ignoring part of it, so a failure means "do not open", never "fall
// back to unfiltered defaults". With no profile and no override the result
// serves every group at the default budget and equals today's behavior.
func Resolve(in Inputs) (Effective, error) {
	reg, groups, err := indexRegistry(in.Registry)
	if err != nil {
		return Effective{}, err
	}
	canon := in.Canonical
	if canon == nil {
		canon = lexical
	}
	strict := func(path string) (string, error) {
		if !filepath.IsAbs(path) {
			return "", fail(CodeProfileInvalid, "A skill path must be absolute.")
		}
		real, err := canon(path)
		if err != nil {
			return "", fail(CodeProfileInvalid, "A skill path cannot be resolved.")
		}
		return filepath.Clean(real), nil
	}
	// Deny entries and vendor skills that no longer resolve keep their own
	// path, as disabledSkill does today: a missing file must not break a launch.
	lenient := func(path string) string {
		if real, err := canon(path); err == nil && filepath.IsAbs(real) {
			return filepath.Clean(real)
		}
		return filepath.Clean(path)
	}

	var w Profile
	if in.Workspace != nil {
		w = *in.Workspace
		if w.Schema != SchemaVersion {
			return Effective{}, fail(CodeProfileInvalid, "The workspace profile has an unsupported version.")
		}
	}
	if in.Session != nil && in.Session.Schema != SchemaVersion {
		return Effective{}, fail(CodeProfileInvalid, "The session override has an unsupported version.")
	}

	// Tool groups: absent is on, W can turn one off, S can only turn one on.
	groupOn := make(map[string]bool, len(groups))
	for name := range groups {
		groupOn[name] = true
	}
	for name, on := range w.ToolGroups {
		if _, ok := groups[name]; !ok {
			return Effective{}, fail(CodeGroupUnknown, "The profile names a tool group that does not exist.")
		}
		groupOn[name] = on
	}
	if in.Session != nil {
		for _, name := range in.Session.Groups {
			if _, ok := groups[name]; !ok {
				return Effective{}, fail(CodeGroupUnknown, "The session override names a tool group that does not exist.")
			}
			groupOn[name] = true
		}
	}

	budgets, err := resolveBudgets(reg, groups, w.ResultBudget)
	if err != nil {
		return Effective{}, err
	}

	// Deny lists are a union; W cannot remove a V entry.
	for _, name := range w.DisabledMCP {
		if name == "devkit" {
			return Effective{}, fail(CodeProfileInvalid, "The DevKit server cannot be disabled.")
		}
	}
	disabledMCP := union(in.Vendor.DisabledMCP, w.DisabledMCP)
	disabledSkills := map[string]bool{}
	for _, p := range slices.Concat(in.Vendor.DisabledSkills, w.DisabledSkills) {
		disabledSkills[lenient(p)] = true
	}

	servers, err := mergeServers(in.Vendor.Servers, w.Servers, in.Environment.Servers)
	if err != nil {
		return Effective{}, err
	}

	folders := map[string]bool{}
	for _, f := range slices.Concat(in.Vendor.SkillFolders, w.SkillFolders) {
		real, err := strict(f)
		if err != nil {
			return Effective{}, err
		}
		folders[real] = true
	}
	envSkills := map[string]bool{}
	for _, p := range in.Environment.Skills {
		envSkills[lenient(p)] = true
	}

	eff := Effective{
		Revisions:      Revisions{Vendor: in.Vendor.Revision},
		DisabledSkills: sortedKeys(disabledSkills),
		DisabledMCP:    disabledMCP,
		Servers:        servers,
		SkillFolders:   sortedKeys(folders),
		groupOn:        groupOn,
		budgets:        budgets,
		executable:     map[string]string{},
	}
	if in.Workspace != nil {
		r := w.Revision
		eff.Revisions.Workspace = &r
	}
	if in.Session != nil {
		r := in.Session.Revision
		eff.Revisions.Session = &r
	}
	for _, name := range sortedKeys(groups) {
		g := groups[name]
		eff.Groups = append(eff.Groups, GroupState{Name: name, On: groupOn[name], Tools: g.tools, DefinitionBytes: g.bytes})
	}

	// What could run or load. A DevKit server counts when enabled and not
	// denied; a vendor-defined server or skill counts when not denied, so a
	// deny entry whose removal exposes one is an executable change.
	for _, s := range servers {
		if s.Enabled && !slices.Contains(disabledMCP, s.Name) {
			eff.executable["mcp:"+s.Name] = serverShape(s)
		}
	}
	for _, s := range in.Environment.Servers {
		if _, mine := eff.executable["mcp:"+s.Name]; !mine && !slices.Contains(disabledMCP, s.Name) {
			eff.executable["mcp:"+s.Name] = serverShape(s)
		}
	}
	// An imported skill that is denied is not loaded, so it is not executable
	// until its deny entry goes: removing that entry is an executable change.
	for f := range folders {
		skill := lenient(filepath.Join(f, "SKILL.md"))
		if !disabledSkills[skill] {
			eff.executable["skill:"+skill] = "folder"
		}
	}
	for p := range envSkills {
		if _, mine := eff.executable["skill:"+p]; !mine && !disabledSkills[p] {
			eff.executable["skill:"+p] = "vendor"
		}
	}
	eff.Fingerprint = fingerprint(in, envSkills, folders, disabledSkills)
	eff.execDigest = executableDigest(eff.executable)
	eff.wsDigest = workspaceDigest(in.Workspace, w, strict, lenient)
	return eff, nil
}

type groupFacts struct{ tools, bytes int }

func indexRegistry(r Registry) (map[string]Tool, map[string]groupFacts, error) {
	tools := make(map[string]Tool, len(r.Tools))
	groups := map[string]groupFacts{}
	for _, t := range r.Tools {
		if t.Name == "" || strings.ContainsAny(t.Name, " \t\n") || t.DefinitionBytes < 0 {
			return nil, nil, fail(CodeProfileInvalid, "The tool registry holds an invalid entry.")
		}
		if _, dup := tools[t.Name]; dup {
			return nil, nil, fail(CodeProfileInvalid, "The tool registry holds a duplicate tool.")
		}
		tools[t.Name] = t
		g := groups[Group(t.Name)]
		g.tools++
		g.bytes += t.DefinitionBytes
		groups[Group(t.Name)] = g
	}
	return tools, groups, nil
}

func resolveBudgets(reg map[string]Tool, groups map[string]groupFacts, p *BudgetProfile) (map[string]Budget, error) {
	var tools, groupRules map[string]BudgetRule
	if p != nil {
		tools, groupRules = p.Tools, p.Groups
	}
	check := func(rule BudgetRule) error {
		if rule.Bytes == nil && rule.Cut == CutDefault {
			return fail(CodeBudgetInvalid, "A budget rule needs a size or a cut mode.")
		}
		if rule.Bytes != nil && (*rule.Bytes < MinResultBudget || *rule.Bytes > MaxResultBudget) {
			return fail(CodeBudgetInvalid, "A result budget must be between 2 KiB and 64 KiB.")
		}
		switch rule.Cut {
		case CutDefault, CutHead:
		case CutHeadTail:
			return fail(CodeBudgetInvalid, "The head-and-tail cut is not available yet.")
		default:
			return fail(CodeBudgetInvalid, "Unknown result cut mode.")
		}
		return nil
	}
	for name, rule := range tools {
		t, ok := reg[name]
		if !ok {
			return nil, fail(CodeBudgetInvalid, "A result budget names a tool that does not exist.")
		}
		if t.BudgetExempt {
			return nil, fail(CodeBudgetInvalid, "A tool whose result is never cut cannot have a budget.")
		}
		if err := check(rule); err != nil {
			return nil, err
		}
	}
	for name, rule := range groupRules {
		if _, ok := groups[name]; !ok {
			return nil, fail(CodeGroupUnknown, "A result budget names a tool group that does not exist.")
		}
		if err := check(rule); err != nil {
			return nil, err
		}
	}
	out := make(map[string]Budget, len(reg))
	for name, t := range reg {
		if t.BudgetExempt {
			out[name] = Budget{Exempt: true}
			continue
		}
		b := Budget{Bytes: DefaultResultBudget, Cut: CutHead}
		g, byTool := groupRules[Group(name)], tools[name]
		switch {
		case byTool.Bytes != nil:
			b.Bytes = *byTool.Bytes
		case g.Bytes != nil:
			b.Bytes = *g.Bytes
		}
		switch {
		case byTool.Cut != CutDefault:
			b.Cut = byTool.Cut
		case g.Cut != CutDefault:
			b.Cut = g.Cut
		}
		out[name] = b
	}
	return out, nil
}

// mergeServers lets a W server replace the V server of the same name, rejects a
// duplicate inside V or W and a W name the vendor's own environment already
// defines (which one wins there is the vendor's rule, not ours).
func mergeServers(v, w, env []Server) ([]Server, error) {
	byName := map[string]Server{}
	for _, s := range v {
		if _, dup := byName[s.Name]; dup {
			return nil, fail(CodeProfileInvalid, "Two configured MCP servers share a name.")
		}
		byName[s.Name] = s
	}
	seen := map[string]bool{}
	for _, s := range w {
		if err := validServer(s); err != nil {
			return nil, err
		}
		if seen[s.Name] {
			return nil, fail(CodeProfileInvalid, "The profile defines two MCP servers with one name.")
		}
		seen[s.Name] = true
		for _, e := range env {
			if e.Name == s.Name {
				return nil, fail(CodeProfileInvalid, "A profile MCP server name collides with a server the vendor already defines.")
			}
		}
		byName[s.Name] = s
	}
	out := make([]Server, 0, len(byName))
	for _, name := range sortedKeys(byName) {
		out = append(out, byName[name])
	}
	return out, nil
}

func validServer(s Server) error {
	if s.Name == "" || s.Name == "devkit" {
		return fail(CodeProfileInvalid, "An MCP server needs a name other than devkit.")
	}
	if (s.Command == "") == (s.URL == "") {
		return fail(CodeProfileInvalid, "An MCP server needs either a command or an HTTP URL.")
	}
	return nil
}

// serverShape is the part of a server whose change is an executable change.
func serverShape(s Server) string {
	// nil and empty are one value: storage drops empty lists (omitempty), so a
	// save/load round trip must not look like an altered executable.
	keys := append([]string{}, s.EnvKeys...)
	sort.Strings(keys)
	args := append([]string{}, s.Args...)
	b, _ := json.Marshal(struct {
		Command string
		Args    []string
		URL     string
		EnvKeys []string
	}{s.Command, args, s.URL, keys})
	return string(b)
}

func union(a, b []string) []string {
	set := map[string]bool{}
	for _, s := range slices.Concat(a, b) {
		set[s] = true
	}
	return sortedKeys(set)
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// workspaceDigest identifies everything W contributes after canonicalization:
// its groups and budgets, deny entries, servers and skill folders with their
// resolved targets. Two snapshots with the same digest were shaped by the same
// workspace profile content even when a symlink inside it was retargeted without
// the revision moving; a reload that changes executables under an unchanged
// digest comes from the vendor customization or the vendor's own files.
func workspaceDigest(present *Profile, w Profile, strict func(string) (string, error), lenient func(string) string) string {
	if present == nil {
		return "none"
	}
	type server struct {
		Name, Shape string
		Enabled     bool
	}
	facts := struct {
		Groups                               map[string]bool
		Budget                               *BudgetProfile
		DisabledMCP, DisabledSkills, Folders []string
		Servers                              []server
	}{Groups: w.ToolGroups, Budget: w.ResultBudget, DisabledMCP: sortedCopy(w.DisabledMCP)}
	for _, p := range w.DisabledSkills {
		facts.DisabledSkills = append(facts.DisabledSkills, lenient(p))
	}
	sort.Strings(facts.DisabledSkills)
	for _, f := range w.SkillFolders {
		if real, err := strict(f); err == nil {
			facts.Folders = append(facts.Folders, real)
		}
	}
	sort.Strings(facts.Folders)
	for _, sv := range w.Servers {
		facts.Servers = append(facts.Servers, server{sv.Name, serverShape(sv), sv.Enabled})
	}
	sort.Slice(facts.Servers, func(i, j int) bool { return facts.Servers[i].Name < facts.Servers[j].Name })
	b, _ := json.Marshal(facts)
	sum := sha256.Sum256(b)
	return "sha256:v1:" + hex.EncodeToString(sum[:])
}

func sortedCopy(in []string) []string {
	out := slices.Clone(in)
	sort.Strings(out)
	return out
}
