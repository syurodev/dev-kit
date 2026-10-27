package harnessconfig

import (
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"devkit/internal/harness"
)

// Target says what a proposal writes.
type Target string

const (
	// TargetSession writes S for one session.
	TargetSession Target = "session"
	// TargetWorkspace writes W and, when a session is known, turns the groups in
	// Patch.EnableGroups on in that session's S so it has them now (other
	// running sessions keep the W they opened with).
	TargetWorkspace Target = "workspace"
)

// Patch is the requested change. It is data to apply to copies of W and S; it is
// never classified itself (the effective before and after are).
type Patch struct {
	EnableGroups []string      `json:"enableGroups,omitempty"`
	RevokeGroups []string      `json:"revokeGroups,omitempty"`
	Workspace    *ProfilePatch `json:"workspace,omitempty"`
	// BudgetEdits are partial edits of result budgets in W, translated over the
	// current W before anything is classified. They never touch S.
	BudgetEdits []BudgetEdit `json:"budgetEdits,omitempty"`
}

func (p Patch) isBudgetOnly() bool {
	return len(p.BudgetEdits) > 0 && len(p.EnableGroups) == 0 && len(p.RevokeGroups) == 0 && p.Workspace == nil
}

// ProfilePatch edits W. A map value of nil removes that entry; a list pointer
// replaces the whole list (present-empty clears it); a nil pointer leaves it.
type ProfilePatch struct {
	Groups       map[string]*bool               `json:"groups,omitempty"`
	ToolBudgets  map[string]*harness.BudgetRule `json:"toolBudgets,omitempty"`
	GroupBudgets map[string]*harness.BudgetRule `json:"groupBudgets,omitempty"`

	DisabledMCP    *[]string         `json:"disabledMCP,omitempty"`
	DisabledSkills *[]string         `json:"disabledSkills,omitempty"`
	Servers        *[]harness.Server `json:"servers,omitempty"`
	SkillFolders   *[]string         `json:"skillFolders,omitempty"`
}

// Request is a caller's proposal. WorkspaceID and SessionID are honored for the
// UI only; an agent's target always comes from its authenticated session.
type Request struct {
	WorkspaceID string
	SessionID   string
	Target      Target
	Patch       Patch
	// Rationale is the agent's own words. It is shown labelled as untrusted and
	// never decides a target, tier or any markup.
	Rationale string
}

const (
	maxRationale = 400
	maxPatchList = 64
)

// normalized returns a deep, canonical copy of the patch: set-like lists sorted
// and de-duplicated, so equal intents are equal bytes. Callers keep no alias.
func (p Patch) normalized() (Patch, error) {
	raw, err := json.Marshal(p)
	if err != nil || len(raw) > 64*1024 {
		return Patch{}, ErrInvalid
	}
	var out Patch
	if err := json.Unmarshal(raw, &out); err != nil {
		return Patch{}, ErrInvalid
	}
	out.EnableGroups, out.RevokeGroups = setOf(out.EnableGroups), setOf(out.RevokeGroups)
	if len(out.EnableGroups) > maxPatchList || len(out.RevokeGroups) > maxPatchList {
		return Patch{}, ErrInvalid
	}
	for _, g := range out.EnableGroups {
		if slices.Contains(out.RevokeGroups, g) {
			return Patch{}, ErrInvalid
		}
	}
	if err := normalizeBudgetEdits(&out); err != nil {
		return Patch{}, err
	}
	if w := out.Workspace; w != nil {
		for _, l := range []*[]string{w.DisabledMCP, w.DisabledSkills, w.SkillFolders} {
			if l != nil {
				*l = setOf(*l)
				if len(*l) > maxPatchList {
					return Patch{}, ErrInvalid
				}
			}
		}
		if w.Servers != nil {
			sort.SliceStable(*w.Servers, func(i, j int) bool { return (*w.Servers)[i].Name < (*w.Servers)[j].Name })
		}
	}
	return out, nil
}

const maxBudgetEdits = 64

// normalizeBudgetEdits checks the shape of the edits and puts them in a canonical
// order, so equal intents are equal bytes. Whether a named tool or group exists
// is the resolver's to say (against the real registry); this refuses what can be
// seen without it: an unknown kind or mode, a size out of range, duplicate
// targets, contradictory intents and an edit that does nothing.
func normalizeBudgetEdits(p *Patch) error {
	if len(p.BudgetEdits) == 0 {
		p.BudgetEdits = nil
		return nil
	}
	if len(p.BudgetEdits) > maxBudgetEdits {
		return ErrInvalid
	}
	seen := map[[2]string]bool{}
	for _, e := range p.BudgetEdits {
		if e.Kind != BudgetTool && e.Kind != BudgetGroup {
			return ErrInvalid
		}
		if e.Name == "" || len(e.Name) > 128 || strings.ContainsAny(e.Name, " \t\r\n") {
			return ErrInvalid
		}
		key := [2]string{string(e.Kind), e.Name}
		if seen[key] {
			return ErrInvalid
		}
		seen[key] = true
		if e.Bytes != nil && e.ResetBytes || e.Cut != "" && e.ResetCut {
			return ErrInvalid
		}
		if e.Bytes == nil && !e.ResetBytes && e.Cut == "" && !e.ResetCut {
			return ErrInvalid
		}
		if e.Bytes != nil && (*e.Bytes < harness.MinResultBudget || *e.Bytes > harness.MaxResultBudget) {
			return ErrInvalidBudget
		}
		if e.Cut != "" && e.Cut != string(harness.CutHead) && e.Cut != string(harness.CutHeadTail) {
			return ErrInvalidBudget
		}
	}
	sort.SliceStable(p.BudgetEdits, func(i, j int) bool {
		a, b := p.BudgetEdits[i], p.BudgetEdits[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Name < b.Name
	})
	return nil
}

// applyBudgetEdits returns W' = W with the edits applied to its result budgets.
// Each field is edited alone: a size edit keeps the rule's cut mode and the other
// way round; a rule left empty is removed, and empty maps and an empty budget
// profile are dropped so an unbudgeted profile is the same bytes as before.
func applyBudgetEdits(base *harness.Profile, edits []BudgetEdit) *harness.Profile {
	out := clone(base)
	if out == nil {
		out = &harness.Profile{Schema: harness.SchemaVersion}
	}
	if len(edits) == 0 {
		return out
	}
	if out.ResultBudget == nil {
		out.ResultBudget = &harness.BudgetProfile{}
	}
	for _, e := range edits {
		rules := &out.ResultBudget.Tools
		if e.Kind == BudgetGroup {
			rules = &out.ResultBudget.Groups
		}
		rule := (*rules)[e.Name] // zero when absent
		switch {
		case e.ResetBytes:
			rule.Bytes = nil
		case e.Bytes != nil:
			n := *e.Bytes
			rule.Bytes = &n
		}
		switch {
		case e.ResetCut:
			rule.Cut = harness.CutDefault
		case e.Cut != "":
			rule.Cut = harness.CutMode(e.Cut)
		}
		if rule.Bytes == nil && rule.Cut == harness.CutDefault {
			delete(*rules, e.Name)
			continue
		}
		if *rules == nil {
			*rules = map[string]harness.BudgetRule{}
		}
		(*rules)[e.Name] = rule
	}
	if len(out.ResultBudget.Tools) == 0 {
		out.ResultBudget.Tools = nil
	}
	if len(out.ResultBudget.Groups) == 0 {
		out.ResultBudget.Groups = nil
	}
	if out.ResultBudget.Tools == nil && out.ResultBudget.Groups == nil {
		out.ResultBudget = nil
	}
	return out
}

func setOf(in []string) []string {
	if in == nil {
		return nil
	}
	out := slices.Clone(in)
	sort.Strings(out)
	out = slices.Compact(out)
	if out == nil {
		out = []string{}
	}
	return out
}

// applyProfile returns W' = W with the patch applied. base may be nil (no W).
func applyProfile(base *harness.Profile, p *ProfilePatch) *harness.Profile {
	out := clone(base)
	if out == nil {
		out = &harness.Profile{Schema: harness.SchemaVersion}
	}
	if p == nil {
		return out
	}
	for name, on := range p.Groups {
		if on == nil {
			delete(out.ToolGroups, name)
			continue
		}
		if out.ToolGroups == nil {
			out.ToolGroups = map[string]bool{}
		}
		out.ToolGroups[name] = *on
	}
	editRules := func(dst *map[string]harness.BudgetRule, edits map[string]*harness.BudgetRule) {
		for name, rule := range edits {
			if rule == nil {
				delete(*dst, name)
				continue
			}
			if *dst == nil {
				*dst = map[string]harness.BudgetRule{}
			}
			(*dst)[name] = *rule
		}
	}
	if len(p.ToolBudgets) > 0 || len(p.GroupBudgets) > 0 {
		if out.ResultBudget == nil {
			out.ResultBudget = &harness.BudgetProfile{}
		}
		editRules(&out.ResultBudget.Tools, p.ToolBudgets)
		editRules(&out.ResultBudget.Groups, p.GroupBudgets)
	}
	if p.DisabledMCP != nil {
		out.DisabledMCP = slices.Clone(*p.DisabledMCP)
	}
	if p.DisabledSkills != nil {
		out.DisabledSkills = slices.Clone(*p.DisabledSkills)
	}
	if p.SkillFolders != nil {
		out.SkillFolders = slices.Clone(*p.SkillFolders)
	}
	if p.Servers != nil {
		out.Servers = clone(&harness.Profile{Servers: *p.Servers}).Servers
	}
	return out
}

// applyOverride returns S' for the patch. base may be nil. The result is nil
// when nothing remains and an S existed (a delete) or none was needed.
func applyOverride(base *harness.Override, enable, revoke []string) *harness.Override {
	groups := map[string]bool{}
	if base != nil {
		for _, g := range base.Groups {
			groups[g] = true
		}
	}
	for _, g := range enable {
		groups[g] = true
	}
	for _, g := range revoke {
		delete(groups, g)
	}
	if len(groups) == 0 {
		return nil
	}
	out := &harness.Override{Schema: harness.SchemaVersion}
	for g := range groups {
		out.Groups = append(out.Groups, g)
	}
	sort.Strings(out.Groups)
	return out
}

// clone deep-copies W through its own JSON, which keeps absent and present-empty
// fields distinct (omitzero). Callers can then never alias a prepared value.
func clone(p *harness.Profile) *harness.Profile {
	if p == nil {
		return nil
	}
	raw, _ := json.Marshal(p)
	var out harness.Profile
	_ = json.Unmarshal(raw, &out)
	out.Revision = p.Revision
	return &out
}

func cloneOverride(o *harness.Override) *harness.Override {
	if o == nil {
		return nil
	}
	out := *o
	out.Groups = slices.Clone(o.Groups)
	return &out
}

// touchesExecutableSurface reports whether the patch edits what could expose a
// vendor-defined server or skill, or add one of our own. Only then must the
// vendor facts be complete.
func (p Patch) touchesExecutableSurface() bool {
	w := p.Workspace
	return w != nil && (w.DisabledMCP != nil || w.DisabledSkills != nil || w.Servers != nil || w.SkillFolders != nil)
}

// cleanRationale makes the agent's words safe to show: bounded, one paragraph
// of printable text. It is displayed as text under an "untrusted" label only.
func cleanRationale(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t' || r == '\r':
			b.WriteRune(' ')
		case unicode.IsControl(r) || r == utf8.RuneError || unicode.Is(unicode.Cf, r):
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if utf8.RuneCountInString(out) > maxRationale {
		out = string([]rune(out)[:maxRationale]) + "…"
	}
	return out
}
