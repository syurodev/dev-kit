package harness

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func registry() Registry {
	return Registry{Tools: []Tool{
		{Name: "git.status", DefinitionBytes: 900},
		{Name: "git.commit", DefinitionBytes: 1100},
		{Name: "tasks.list", DefinitionBytes: 800},
		{Name: "codegraph.searchSymbol", DefinitionBytes: 2000},
		{Name: "codegraph.readFile", DefinitionBytes: 700, BudgetExempt: true},
		{Name: "pdf.readText", DefinitionBytes: 600, BudgetExempt: true},
		{Name: "harness.profile.get", DefinitionBytes: 500},
		{Name: ProposeTool, DefinitionBytes: 600},
	}}
}

func base() Inputs { return Inputs{Registry: registry()} }

func resolve(t *testing.T, in Inputs) Effective {
	t.Helper()
	e, err := Resolve(in)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return e
}

func code(t *testing.T, err error) string {
	t.Helper()
	var he *Error
	if !errors.As(err, &he) {
		t.Fatalf("want *harness.Error, got %v", err)
	}
	return he.Code
}

func ptr[T any](v T) *T { return &v }

func profile(f func(*Profile)) *Profile {
	p := &Profile{Schema: SchemaVersion, Revision: 3}
	f(p)
	return p
}

func TestNoProfileServesEverythingAtTheDefaultBudget(t *testing.T) {
	e := resolve(t, base())
	for _, tool := range registry().Tools {
		if !e.Served(tool.Name) {
			t.Errorf("%s not served with no profile", tool.Name)
		}
		b, _ := e.Budget(tool.Name)
		if tool.BudgetExempt {
			if !b.Exempt {
				t.Errorf("%s should stay exempt", tool.Name)
			}
			continue
		}
		if b.Bytes != DefaultResultBudget || b.Cut != CutHead {
			t.Errorf("%s budget = %+v, want default head cut", tool.Name, b)
		}
	}
	if e.Revisions.Workspace != nil || e.Revisions.Session != nil {
		t.Errorf("a missing W or S must stay nil, got %+v", e.Revisions)
	}
	if len(e.OffGroups()) != 0 {
		t.Errorf("no group is off: %+v", e.OffGroups())
	}
}

func TestAnEmptyProfileEqualsNoProfile(t *testing.T) {
	none := resolve(t, base())
	in := base()
	in.Workspace = profile(func(p *Profile) {
		p.ToolGroups = map[string]bool{}
		p.DisabledMCP = []string{}
		p.Servers = []Server{}
		p.SkillFolders = []string{}
		p.ResultBudget = &BudgetProfile{}
	})
	empty := resolve(t, in)
	if c := Classify(none, empty); c.Tier != TierNone {
		t.Fatalf("an empty profile changed behavior: %+v", c)
	}
}

func TestAbsentAndPresentEmptySurviveJSON(t *testing.T) {
	absent, _ := json.Marshal(Profile{Schema: 1})
	if strings.Contains(string(absent), "toolGroups") || strings.Contains(string(absent), "disabledMCP") {
		t.Fatalf("absent fields must be omitted: %s", absent)
	}
	empty, _ := json.Marshal(Profile{Schema: 1, ToolGroups: map[string]bool{}, DisabledMCP: []string{}, Servers: []Server{}})
	var back Profile
	if err := json.Unmarshal(empty, &back); err != nil {
		t.Fatal(err)
	}
	if back.ToolGroups == nil || back.DisabledMCP == nil || back.Servers == nil {
		t.Fatalf("present-empty fields became absent: %s -> %+v", empty, back)
	}
	if back.SkillFolders != nil || back.ResultBudget != nil {
		t.Fatalf("absent fields became present: %+v", back)
	}
}

func TestUnsupportedVersionsAreRefused(t *testing.T) {
	in := base()
	in.Workspace = &Profile{Revision: 1}
	if _, err := Resolve(in); code(t, err) != CodeProfileInvalid {
		t.Errorf("W without a version: %v", err)
	}
	in.Workspace = nil
	in.Session = &Override{Schema: 9}
	if _, err := Resolve(in); code(t, err) != CodeProfileInvalid {
		t.Errorf("S with a future version: %v", err)
	}
}

func TestToolGroups(t *testing.T) {
	in := base()
	in.Workspace = profile(func(p *Profile) { p.ToolGroups = map[string]bool{"git": false, "tasks": true} })
	e := resolve(t, in)
	if e.Served("git.status") || e.Served("git.commit") {
		t.Error("git is off in W")
	}
	if !e.Served("tasks.list") || !e.Served("codegraph.searchSymbol") {
		t.Error("an explicit on and an absent group are both on")
	}
	off := e.OffGroups()
	if len(off) != 1 || off[0].Name != "git" || off[0].DefinitionBytes != 2000 || off[0].Tools != 2 {
		t.Errorf("OffGroups = %+v", off)
	}

	in.Session = &Override{Schema: 1, Revision: 1, Groups: []string{"git"}}
	if !resolve(t, in).Served("git.commit") {
		t.Error("S turns a group on")
	}
	in.Workspace = profile(func(p *Profile) {})
	in.Session = &Override{Schema: 1, Revision: 1, Groups: []string{}}
	if !resolve(t, in).Served("git.commit") {
		t.Error("S never turns anything off")
	}
}

func TestUnknownGroupsAreRejectedEverywhere(t *testing.T) {
	cases := map[string]func(*Inputs){
		"W toolGroups": func(in *Inputs) {
			in.Workspace = profile(func(p *Profile) { p.ToolGroups = map[string]bool{"nope": false} })
		},
		"S groups": func(in *Inputs) { in.Session = &Override{Schema: 1, Groups: []string{"nope"}} },
		"budget group": func(in *Inputs) {
			in.Workspace = profile(func(p *Profile) {
				p.ResultBudget = &BudgetProfile{Groups: map[string]BudgetRule{"nope": {Bytes: ptr(4096)}}}
			})
		},
	}
	for name, mutate := range cases {
		in := base()
		mutate(&in)
		if _, err := Resolve(in); code(t, err) != CodeGroupUnknown {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestProposeIsServedWithItsGroupOffButNothingElseIs(t *testing.T) {
	in := base()
	in.Workspace = profile(func(p *Profile) { p.ToolGroups = map[string]bool{"harness": false} })
	e := resolve(t, in)
	if !e.Served(ProposeTool) {
		t.Fatal("propose must stay served so an agent can ask")
	}
	if e.Served("harness.profile.get") {
		t.Error("the exception is one tool, not the group")
	}
	if e.Served("never.registered") {
		t.Error("an unknown tool is not served")
	}
	var found bool
	for _, g := range e.OffGroups() {
		found = found || (g.Name == "harness" && g.DefinitionBytes == 1100)
	}
	if !found {
		t.Errorf("propose must be able to name the harness group and its cost: %+v", e.OffGroups())
	}
}

func TestBudgetPrecedenceToolThenGroupThenDefault(t *testing.T) {
	in := base()
	in.Workspace = profile(func(p *Profile) {
		p.ResultBudget = &BudgetProfile{
			Groups: map[string]BudgetRule{"git": {Bytes: ptr(8192)}, "codegraph": {Bytes: ptr(2048)}},
			Tools:  map[string]BudgetRule{"git.commit": {Bytes: ptr(65536)}},
		}
	})
	e := resolve(t, in)
	want := map[string]int{
		"git.commit":             65536,
		"git.status":             8192,
		"codegraph.searchSymbol": 2048,
		"tasks.list":             DefaultResultBudget,
	}
	for tool, bytes := range want {
		if b, _ := e.Budget(tool); b.Bytes != bytes {
			t.Errorf("%s = %d, want %d", tool, b.Bytes, bytes)
		}
	}
	// Exemptions are fixed: a group rule never reaches an exempt tool.
	if b, _ := e.Budget("codegraph.readFile"); !b.Exempt || b.Bytes != 0 {
		t.Errorf("readFile must stay exempt: %+v", b)
	}
}

func TestBudgetBoundsAndExemptionsAreValidated(t *testing.T) {
	bad := map[string]BudgetProfile{
		"under the floor":  {Tools: map[string]BudgetRule{"git.status": {Bytes: ptr(2047)}}},
		"over the ceiling": {Groups: map[string]BudgetRule{"git": {Bytes: ptr(65537)}}},
		"exempt tool":      {Tools: map[string]BudgetRule{"pdf.readText": {Bytes: ptr(4096)}}},
		"unknown tool":     {Tools: map[string]BudgetRule{"git.nope": {Bytes: ptr(4096)}}},
		"empty rule":       {Tools: map[string]BudgetRule{"git.status": {}}},
		"head and tail":    {Tools: map[string]BudgetRule{"git.status": {Cut: CutHeadTail}}},
		"unknown cut":      {Groups: map[string]BudgetRule{"git": {Cut: "middle"}}},
		"zero bytes":       {Groups: map[string]BudgetRule{"git": {Bytes: ptr(0)}}},
	}
	for name, b := range bad {
		in := base()
		in.Workspace = profile(func(p *Profile) { p.ResultBudget = &b })
		if _, err := Resolve(in); code(t, err) != CodeBudgetInvalid {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, size := range []int{MinResultBudget, MaxResultBudget} {
		in := base()
		in.Workspace = profile(func(p *Profile) {
			p.ResultBudget = &BudgetProfile{Tools: map[string]BudgetRule{"git.status": {Bytes: ptr(size)}}}
		})
		resolve(t, in)
	}
}

func TestHeadTailIsRefusedNotSilentlyHead(t *testing.T) {
	in := base()
	in.Workspace = profile(func(p *Profile) {
		p.ResultBudget = &BudgetProfile{Tools: map[string]BudgetRule{"git.status": {Cut: CutHeadTail}}}
	})
	_, err := Resolve(in)
	if code(t, err) != CodeBudgetInvalid || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("headTail must be rejected clearly: %v", err)
	}
}

func TestDenyListsAreAUnionAndWCannotRemoveV(t *testing.T) {
	in := base()
	in.Vendor = Vendor{DisabledMCP: []string{"slack", "jira"}, DisabledSkills: []string{"/skills/a/SKILL.md"}}
	in.Workspace = profile(func(p *Profile) {
		p.DisabledMCP = []string{"github", "slack"}
		p.DisabledSkills = []string{}
	})
	e := resolve(t, in)
	if got := strings.Join(e.DisabledMCP, ","); got != "github,jira,slack" {
		t.Errorf("DisabledMCP = %s", got)
	}
	if len(e.DisabledSkills) != 1 {
		t.Errorf("a present-empty W list cannot remove a V entry: %v", e.DisabledSkills)
	}
	in.Workspace = profile(func(p *Profile) { p.DisabledMCP = []string{"devkit"} })
	if _, err := Resolve(in); code(t, err) != CodeProfileInvalid {
		t.Errorf("devkit cannot be disabled: %v", err)
	}
}

func stdio(name, cmd string) Server { return Server{Name: name, Command: cmd, Enabled: true} }

func TestServersReplaceByNameAndRejectDuplicatesAndCollisions(t *testing.T) {
	in := base()
	in.Vendor.Servers = []Server{stdio("db", "v-cmd"), stdio("keep", "k")}
	in.Workspace = profile(func(p *Profile) { p.Servers = []Server{stdio("db", "w-cmd"), stdio("new", "n")} })
	e := resolve(t, in)
	got := map[string]string{}
	for _, s := range e.Servers {
		got[s.Name] = s.Command
	}
	if len(got) != 3 || got["db"] != "w-cmd" || got["keep"] != "k" || got["new"] != "n" {
		t.Errorf("servers = %v", got)
	}
	if e.Servers[0].Name != "db" || e.Servers[2].Name != "new" {
		t.Errorf("servers must be sorted by name: %+v", e.Servers)
	}

	in.Workspace = profile(func(p *Profile) { p.Servers = []Server{stdio("x", "a"), stdio("x", "b")} })
	if _, err := Resolve(in); code(t, err) != CodeProfileInvalid {
		t.Errorf("duplicate inside W: %v", err)
	}
	in.Workspace = profile(func(p *Profile) { p.Servers = []Server{stdio("vendor-own", "a")} })
	in.Environment.Servers = []Server{stdio("vendor-own", "theirs")}
	if _, err := Resolve(in); code(t, err) != CodeProfileInvalid {
		t.Errorf("collision with the vendor environment: %v", err)
	}
	for name, s := range map[string]Server{
		"reserved": stdio("devkit", "x"),
		"both":     {Name: "a", Command: "x", URL: "https://x.test"},
		"neither":  {Name: "a"},
		"no name":  {Command: "x"},
	} {
		in := base()
		in.Workspace = profile(func(p *Profile) { p.Servers = []Server{s} })
		if _, err := Resolve(in); code(t, err) != CodeProfileInvalid {
			t.Errorf("%s: %v", name, err)
		}
	}
	in = base()
	in.Vendor.Servers = []Server{stdio("a", "x"), stdio("a", "y")}
	if _, err := Resolve(in); code(t, err) != CodeProfileInvalid {
		t.Errorf("duplicate inside V: %v", err)
	}
}

func TestSkillFoldersAreAUnionByCanonicalPath(t *testing.T) {
	alias := map[string]string{"/link/skills": "/real/skills", "/link/other": "/real/other"}
	in := base()
	in.Canonical = func(p string) (string, error) {
		if r, ok := alias[p]; ok {
			return r, nil
		}
		return p, nil
	}
	in.Vendor.SkillFolders = []string{"/real/skills", "/real/a/"}
	in.Workspace = profile(func(p *Profile) { p.SkillFolders = []string{"/link/skills", "/link/other", "/real/a"} })
	e := resolve(t, in)
	if got := strings.Join(e.SkillFolders, ","); got != "/real/a,/real/other,/real/skills" {
		t.Errorf("SkillFolders = %s", got)
	}

	in.Workspace = profile(func(p *Profile) { p.SkillFolders = []string{"relative/skills"} })
	if _, err := Resolve(in); code(t, err) != CodeProfileInvalid {
		t.Errorf("a relative folder: %v", err)
	}
	in.Workspace = profile(func(p *Profile) { p.SkillFolders = []string{"/gone"} })
	in.Canonical = func(string) (string, error) { return "", errors.New("/gone: secret detail") }
	_, err := Resolve(in)
	if code(t, err) != CodeProfileInvalid || strings.Contains(err.Error(), "secret") {
		t.Errorf("an unresolvable folder must fail without the host's error text: %v", err)
	}
}

func TestRegistryFactsAreValidated(t *testing.T) {
	for name, r := range map[string]Registry{
		"empty name": {Tools: []Tool{{Name: ""}}},
		"duplicate":  {Tools: []Tool{{Name: "a.b"}, {Name: "a.b"}}},
		"negative":   {Tools: []Tool{{Name: "a.b", DefinitionBytes: -1}}},
	} {
		if _, err := Resolve(Inputs{Registry: r}); code(t, err) != CodeProfileInvalid {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestGroupIsThePartBeforeTheFirstDot(t *testing.T) {
	for tool, want := range map[string]string{"git.status": "git", "tasks.comment.list": "tasks", "solo": "solo"} {
		if got := Group(tool); got != want {
			t.Errorf("Group(%q) = %q, want %q", tool, got, want)
		}
	}
}

// --- classification ---

func classify(t *testing.T, before, after Inputs) Classification {
	t.Helper()
	return Classify(resolve(t, before), resolve(t, after))
}

func withBudget(rules BudgetProfile) Inputs {
	in := base()
	in.Workspace = profile(func(p *Profile) { p.ResultBudget = &rules })
	return in
}

func TestClassifyGroupsAndBudgets(t *testing.T) {
	off := base()
	off.Workspace = profile(func(p *Profile) { p.ToolGroups = map[string]bool{"git": false} })
	if c := classify(t, base(), off); c.Tier != TierNarrowing {
		t.Errorf("turning a group off = %v", c.Tier)
	}
	if c := classify(t, off, base()); c.Tier != TierWidening {
		t.Errorf("turning a group on = %v", c.Tier)
	}
	if c := classify(t, base(), base()); c.Tier != TierNone || len(c.Changes) != 0 {
		t.Errorf("no change = %+v", c)
	}

	tool := func(bytes int) Inputs {
		return withBudget(BudgetProfile{Tools: map[string]BudgetRule{"git.status": {Bytes: ptr(bytes)}}})
	}
	if c := classify(t, tool(2048), tool(16384)); c.Tier != TierWidening {
		t.Errorf("2 KiB -> 16 KiB = %v", c.Tier)
	}
	if c := classify(t, tool(65536), tool(32768)); c.Tier != TierNarrowing {
		t.Errorf("64 KiB -> 32 KiB = %v", c.Tier)
	}
	// 24 KiB is the default: an override of 16 KiB is smaller than the default,
	// so deleting it widens even though the patch only removes an entry.
	if c := classify(t, tool(16384), base()); c.Tier != TierWidening {
		t.Errorf("deleting a 16 KiB override = %v, compare to the actual previous value", c.Tier)
	}
	if c := classify(t, tool(65536), base()); c.Tier != TierNarrowing {
		t.Errorf("deleting a 64 KiB override = %v", c.Tier)
	}
}

func TestClassifyGroupBudgetCountsByTheToolsItChanges(t *testing.T) {
	group := func(bytes int) Inputs {
		return withBudget(BudgetProfile{Groups: map[string]BudgetRule{"git": {Bytes: ptr(bytes)}}})
	}
	pinned := withBudget(BudgetProfile{
		Groups: map[string]BudgetRule{"git": {Bytes: ptr(4096)}},
		Tools:  map[string]BudgetRule{"git.status": {Bytes: ptr(4096)}, "git.commit": {Bytes: ptr(4096)}},
	})
	// Every git tool has its own 4 KiB entry, so the group value changes nothing.
	pinned2 := withBudget(BudgetProfile{
		Groups: map[string]BudgetRule{"git": {Bytes: ptr(60000)}},
		Tools:  map[string]BudgetRule{"git.status": {Bytes: ptr(4096)}, "git.commit": {Bytes: ptr(4096)}},
	})
	if c := Classify(resolve(t, pinned), resolve(t, pinned2)); c.Tier != TierNone {
		t.Errorf("an overridden group value changed nothing effective: %+v", c)
	}
	c := classify(t, group(4096), group(8192))
	if c.Tier != TierWidening || len(c.Changes) != 2 {
		t.Errorf("group 4 -> 8 KiB reaches both git tools: %+v", c)
	}
}

func TestClassifyCutModeAloneIsNeutralInTheNarrowingTier(t *testing.T) {
	a := withBudget(BudgetProfile{Tools: map[string]BudgetRule{"git.status": {Bytes: ptr(4096)}}})
	b := withBudget(BudgetProfile{Tools: map[string]BudgetRule{"git.status": {Bytes: ptr(4096), Cut: CutHead}}})
	if c := classify(t, a, b); c.Tier != TierNone {
		t.Errorf("explicit head equals the default head: %+v", c)
	}
}

func TestClassifyDenyEntries(t *testing.T) {
	deny := func(names ...string) Inputs {
		in := base()
		in.Workspace = profile(func(p *Profile) { p.DisabledMCP = names })
		return in
	}
	if c := classify(t, deny(), deny("slack")); c.Tier != TierNarrowing {
		t.Errorf("a larger deny set = %v", c.Tier)
	}
	if c := classify(t, deny("slack"), deny()); c.Tier != TierWidening {
		t.Errorf("an effective deny entry gone = %v", c.Tier)
	}
	// V still denies it: the effective set is unchanged.
	v := func(w ...string) Inputs {
		in := deny(w...)
		in.Vendor.DisabledMCP = []string{"slack"}
		return in
	}
	if c := classify(t, v("slack"), v()); c.Tier != TierNone {
		t.Errorf("W removing an entry V still holds changes nothing: %+v", c)
	}
}

func TestClassifyDenyRemovalThatExposesAVendorServerIsExecutable(t *testing.T) {
	denied := base()
	denied.Environment.Servers = []Server{stdio("vendor-db", "their-cmd")}
	denied.Workspace = profile(func(p *Profile) { p.DisabledMCP = []string{"vendor-db"} })
	open := denied
	open.Workspace = profile(func(p *Profile) { p.DisabledMCP = []string{} })
	c := classify(t, denied, open)
	if c.Tier != TierExecutable {
		t.Fatalf("a patch that only deletes a line exposed a vendor executable: %+v", c)
	}
	back := classify(t, open, denied)
	if back.Tier != TierNarrowing {
		t.Errorf("hiding it again = %v", back.Tier)
	}
	// A deny entry for something that does not exist is plain widening.
	ghost := base()
	ghost.Workspace = profile(func(p *Profile) { p.DisabledMCP = []string{"ghost"} })
	if c := classify(t, ghost, base()); c.Tier != TierWidening {
		t.Errorf("removing an unrelated deny entry = %v", c.Tier)
	}
}

func TestClassifyServerReplacementFallbackAndAlteration(t *testing.T) {
	withW := func(s ...Server) Inputs {
		in := base()
		in.Vendor.Servers = []Server{stdio("db", "v-cmd")}
		in.Workspace = profile(func(p *Profile) { p.Servers = s })
		return in
	}
	// Removing the W override falls back to the V server: an altered executable.
	if c := classify(t, withW(stdio("db", "w-cmd")), withW()); c.Tier != TierExecutable {
		t.Errorf("fallback to V's server = %v", c.Tier)
	}
	if c := classify(t, withW(), withW(stdio("db", "w-cmd"))); c.Tier != TierExecutable {
		t.Errorf("replacing V's server = %v", c.Tier)
	}
	args := func(a ...string) Inputs {
		s := stdio("db", "cmd")
		s.Args = a
		return withW(s)
	}
	if c := classify(t, args("a"), args("b")); c.Tier != TierExecutable {
		t.Errorf("an argument change = %v", c.Tier)
	}
	env := func(k ...string) Inputs {
		s := stdio("db", "cmd")
		s.EnvKeys = k
		return withW(s)
	}
	if c := classify(t, env("A", "B"), env("B", "A")); c.Tier != TierNone {
		t.Errorf("env-name order is not a change: %+v", c)
	}
	if c := classify(t, env("A"), env("A", "B")); c.Tier != TierExecutable {
		t.Errorf("a new env name = %v", c.Tier)
	}
	off := stdio("db", "cmd")
	off.Enabled = false
	if c := classify(t, withW(stdio("db", "w-cmd")), withW(off)); c.Tier != TierNarrowing {
		t.Errorf("disabling a server = %v", c.Tier)
	}
	if c := classify(t, withW(off), withW(stdio("db", "w-cmd"))); c.Tier != TierExecutable {
		t.Errorf("enabling a server = %v", c.Tier)
	}
}

func TestClassifySkillFolders(t *testing.T) {
	with := func(folders ...string) Inputs {
		in := base()
		in.Workspace = profile(func(p *Profile) { p.SkillFolders = folders })
		return in
	}
	if c := classify(t, with(), with("/skills/a")); c.Tier != TierExecutable {
		t.Errorf("a new skill folder = %v", c.Tier)
	}
	if c := classify(t, with("/skills/a"), with()); c.Tier != TierNarrowing {
		t.Errorf("fewer skill folders = %v", c.Tier)
	}
	denied := base()
	denied.Environment.Skills = []string{"/home/.claude/skills/x/SKILL.md"}
	denied.Workspace = profile(func(p *Profile) { p.DisabledSkills = []string{"/home/.claude/skills/x/SKILL.md"} })
	open := denied
	open.Workspace = profile(func(p *Profile) { p.DisabledSkills = []string{} })
	if c := classify(t, denied, open); c.Tier != TierExecutable {
		t.Errorf("exposing a vendor skill = %v", c.Tier)
	}
}

func TestMixedChangesTakeTheStrictestTier(t *testing.T) {
	before := base()
	before.Workspace = profile(func(p *Profile) { p.ToolGroups = map[string]bool{"git": true} })
	after := base()
	after.Workspace = profile(func(p *Profile) {
		p.ToolGroups = map[string]bool{"git": false, "tasks": true}
		p.ResultBudget = &BudgetProfile{Tools: map[string]BudgetRule{"tasks.list": {Bytes: ptr(65536)}}}
	})
	c := classify(t, before, after)
	if c.Tier != TierWidening {
		t.Fatalf("narrowing + widening = %v", c.Tier)
	}
	after.Workspace.Servers = []Server{stdio("new", "x")}
	c = classify(t, before, after)
	if c.Tier != TierExecutable || c.Changes[0].Tier != TierExecutable {
		t.Fatalf("an executable among the changes = %+v", c)
	}
}

func TestSessionOverrideIsAWideningAndItsRevocationNarrows(t *testing.T) {
	off := base()
	off.Workspace = profile(func(p *Profile) { p.ToolGroups = map[string]bool{"git": false} })
	on := off
	on.Session = &Override{Schema: 1, Revision: 1, Groups: []string{"git"}}
	if c := classify(t, off, on); c.Tier != TierWidening {
		t.Errorf("enabling by S = %v", c.Tier)
	}
	if c := classify(t, on, off); c.Tier != TierNarrowing {
		t.Errorf("revoking S = %v", c.Tier)
	}
}

// --- fingerprint ---

func TestFingerprintIsDeterministicAndOrderIndependent(t *testing.T) {
	mk := func(servers []Server, skills []string, tools []Tool) Inputs {
		in := base()
		in.Registry = Registry{Tools: tools}
		in.Environment = Environment{Servers: servers, Skills: skills}
		return in
	}
	a := mk([]Server{{Name: "a", Command: "x", EnvKeys: []string{"K1", "K2"}}, stdio("b", "y")}, []string{"/s/1/SKILL.md", "/s/2/SKILL.md"}, registry().Tools)
	rev := registry().Tools
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	b := mk([]Server{stdio("b", "y"), {Name: "a", Command: "x", EnvKeys: []string{"K2", "K1"}}}, []string{"/s/2/SKILL.md", "/s/1/SKILL.md"}, rev)
	ea, eb := resolve(t, a), resolve(t, b)
	if ea.Fingerprint != eb.Fingerprint {
		t.Fatalf("ordering changed the fingerprint:\n%s\n%s", ea.Fingerprint, eb.Fingerprint)
	}
	if !strings.HasPrefix(ea.Fingerprint, "sha256:v1:") || ea.Fingerprint != resolve(t, a).Fingerprint {
		t.Errorf("fingerprint must be versioned and stable: %s", ea.Fingerprint)
	}
}

func TestFingerprintMovesWithEveryInputThatMattersToADiff(t *testing.T) {
	ref := base()
	ref.Workspace = profile(func(p *Profile) {})
	ref.Session = &Override{Schema: 1, Revision: 1}
	ref.Environment = Environment{Servers: []Server{stdio("v", "cmd")}, Skills: []string{"/s/SKILL.md"}}
	refFP := resolve(t, ref).Fingerprint

	moves := map[string]func(*Inputs){
		"V revision":     func(in *Inputs) { in.Vendor.Revision++ },
		"W revision":     func(in *Inputs) { w := *in.Workspace; w.Revision++; in.Workspace = &w },
		"S revision":     func(in *Inputs) { in.Session = &Override{Schema: 1, Revision: 2} },
		"no S":           func(in *Inputs) { in.Session = nil },
		"no W":           func(in *Inputs) { in.Workspace = nil },
		"vendor command": func(in *Inputs) { in.Environment.Servers = []Server{stdio("v", "other")} },
		"vendor args": func(in *Inputs) {
			s := stdio("v", "cmd")
			s.Args = []string{"--x"}
			in.Environment.Servers = []Server{s}
		},
		"vendor skill":  func(in *Inputs) { in.Environment.Skills = []string{"/s2/SKILL.md"} },
		"new tool":      func(in *Inputs) { in.Registry.Tools = append(slicesClone(in.Registry.Tools), Tool{Name: "new.tool"}) },
		"exempt change": func(in *Inputs) { r := slicesClone(in.Registry.Tools); r[0].BudgetExempt = true; in.Registry.Tools = r },
	}
	for name, mutate := range moves {
		in := ref
		mutate(&in)
		if resolve(t, in).Fingerprint == refFP {
			t.Errorf("%s did not move the fingerprint", name)
		}
	}
	// A revision 0 input and a missing input are different facts.
	zero := ref
	zero.Workspace = &Profile{Schema: 1}
	none := ref
	none.Workspace = nil
	if resolve(t, zero).Fingerprint == resolve(t, none).Fingerprint {
		t.Error("W at revision 0 must differ from no W")
	}
}

func TestFingerprintCarriesNoValuesOrDefinitionBytes(t *testing.T) {
	in := base()
	s := stdio("v", "secret-command-name")
	s.EnvKeys = []string{"TOKEN_NAME"}
	in.Environment.Servers = []Server{s}
	fp := resolve(t, in).Fingerprint
	if strings.Contains(fp, "secret") || len(fp) != len("sha256:v1:")+64 {
		t.Errorf("fingerprint should be an opaque digest: %s", fp)
	}
	// Definition sizes change when a description is edited; that is not a
	// reason to stale a proposal.
	other := base()
	other.Environment.Servers = in.Environment.Servers
	other.Registry = Registry{Tools: slicesClone(registry().Tools)}
	other.Registry.Tools[0].DefinitionBytes += 100
	if resolve(t, other).Fingerprint != fp {
		t.Error("definition bytes must not enter the fingerprint")
	}
}

func slicesClone[T any](s []T) []T { return append([]T(nil), s...) }

func TestResolveDoesNotMutateItsInputs(t *testing.T) {
	in := base()
	in.Vendor = Vendor{DisabledMCP: []string{"b", "a"}, Servers: []Server{stdio("z", "x"), stdio("y", "x")}}
	in.Workspace = profile(func(p *Profile) { p.DisabledMCP = []string{"d", "c"} })
	before, _ := json.Marshal(in.Vendor)
	wBefore, _ := json.Marshal(in.Workspace)
	resolve(t, in)
	after, _ := json.Marshal(in.Vendor)
	wAfter, _ := json.Marshal(in.Workspace)
	if string(before) != string(after) || string(wBefore) != string(wAfter) {
		t.Error("Resolve changed its inputs")
	}
}

// Cut modes other than head are refused today, so a cut-only change cannot be
// reached through Resolve yet. Pin the tier rule on hand-built values so
// phase 3 inherits it.
func TestClassifyCutModeAloneIsNeutralAndTakesTheNarrowingTier(t *testing.T) {
	head := resolve(t, base())
	tail := resolve(t, base())
	tail.budgets = map[string]Budget{}
	for k, v := range head.budgets {
		tail.budgets[k] = v
	}
	b := tail.budgets["git.status"]
	b.Cut = CutHeadTail
	tail.budgets["git.status"] = b
	c := Classify(head, tail)
	if c.Tier != TierNarrowing || len(c.Changes) != 1 || !c.Changes[0].Neutral {
		t.Fatalf("cut-only change = %+v", c)
	}
}

// An imported skill that is denied does not load. Removing the deny entry makes
// it load, which is the same executable change as adding the folder.
func TestClassifyUndenyingAnImportedSkillIsExecutable(t *testing.T) {
	denied := base()
	denied.Vendor.SkillFolders = []string{"/skills/a"}
	denied.Workspace = profile(func(p *Profile) { p.DisabledSkills = []string{"/skills/a/SKILL.md"} })
	open := denied
	open.Workspace = profile(func(p *Profile) { p.DisabledSkills = []string{} })
	if c := classify(t, denied, open); c.Tier != TierExecutable {
		t.Fatalf("un-denying an imported skill = %+v", c)
	}
	if c := classify(t, open, denied); c.Tier != TierNarrowing {
		t.Fatalf("denying it again = %v", c.Tier)
	}
	// Denied from the start, a folder that is added changes nothing it can run.
	none := base()
	none.Workspace = profile(func(p *Profile) { p.DisabledSkills = []string{"/skills/a/SKILL.md"} })
	if c := classify(t, none, denied); c.Tier != TierNone {
		t.Errorf("adding a folder whose skill is denied = %+v", c)
	}
}

// Same revisions, same names; a symlink now points somewhere else.
func TestFingerprintFollowsWhereASkillPathResolves(t *testing.T) {
	target := "/skills/a"
	in := base()
	in.Canonical = func(p string) (string, error) {
		if p == "/link/skill" {
			return target, nil
		}
		return p, nil
	}
	in.Workspace = profile(func(p *Profile) { p.SkillFolders = []string{"/link/skill"} })
	a := resolve(t, in)
	target = "/skills/b"
	b := resolve(t, in)
	if a.Fingerprint == b.Fingerprint {
		t.Fatal("re-pointing a symlink must change the fingerprint")
	}
	if c := Classify(a, b); c.Tier != TierExecutable {
		t.Errorf("the effective set changed: %+v", c)
	}

	// A denied skill's target is part of what the deny entry means.
	target = "/skills/a"
	in.Workspace = profile(func(p *Profile) { p.DisabledSkills = []string{"/link/skill/SKILL.md"} })
	in.Canonical = func(p string) (string, error) {
		if p == "/link/skill/SKILL.md" {
			return target + "/SKILL.md", nil
		}
		return p, nil
	}
	d1 := resolve(t, in).Fingerprint
	target = "/skills/b"
	if resolve(t, in).Fingerprint == d1 {
		t.Error("a deny entry's resolved target must be in the fingerprint")
	}
}

// Storage writes empty Args/EnvKeys as absent; nothing may change by saving.
func TestServersAreUnchangedByAStorageRoundTrip(t *testing.T) {
	orig := Server{Name: "db", Command: "cmd", Args: []string{}, EnvKeys: []string{}, Enabled: true}
	raw, err := json.Marshal([]Server{orig})
	if err != nil {
		t.Fatal(err)
	}
	var loaded []Server
	if err := json.Unmarshal(raw, &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded[0].Args != nil || loaded[0].EnvKeys != nil {
		t.Fatalf("test premise: empty lists should come back absent, got %+v", loaded[0])
	}
	mk := func(s Server) Inputs {
		in := base()
		in.Vendor.Servers = []Server{s}
		in.Environment.Servers = []Server{{Name: "vendor", Command: "v", Args: s.Args, EnvKeys: s.EnvKeys}}
		return in
	}
	before, after := resolve(t, mk(orig)), resolve(t, mk(loaded[0]))
	if c := Classify(before, after); c.Tier != TierNone {
		t.Errorf("a round trip looks like a change: %+v", c)
	}
	if before.Fingerprint != after.Fingerprint {
		t.Error("a round trip moved the fingerprint")
	}
}

// Joining name and exemption into one string made "a!" and an exempt "a" the
// same fact.
func TestFingerprintKeepsToolNamesAndExemptionsApart(t *testing.T) {
	a := Inputs{Registry: Registry{Tools: []Tool{{Name: "a.x"}, {Name: "a.x!"}}}}
	b := Inputs{Registry: Registry{Tools: []Tool{{Name: "a.x", BudgetExempt: true}, {Name: "a.x!"}}}}
	c := Inputs{Registry: Registry{Tools: []Tool{{Name: "a.x", BudgetExempt: true}}}}
	d := Inputs{Registry: Registry{Tools: []Tool{{Name: "a.x!"}, {Name: "a.x", BudgetExempt: false}}}}
	fa, fb, fc, fd := resolve(t, a).Fingerprint, resolve(t, b).Fingerprint, resolve(t, c).Fingerprint, resolve(t, d).Fingerprint
	if fa == fb || fb == fc || fa == fc {
		t.Fatalf("distinct registries collided: %s %s %s", fa, fb, fc)
	}
	if fa != fd {
		t.Error("tool order must not matter")
	}
}

func TestServingIsDefaultOnlyWhenNothingChangesWhatTheServerServes(t *testing.T) {
	if !resolve(t, base()).ServingIsDefault() {
		t.Fatal("no profile serves the default")
	}
	off := base()
	off.Workspace = profile(func(p *Profile) { p.ToolGroups = map[string]bool{"git": false} })
	if resolve(t, off).ServingIsDefault() {
		t.Error("a group off is not the default")
	}
	b := base()
	b.Workspace = profile(func(p *Profile) {
		p.ResultBudget = &BudgetProfile{Tools: map[string]BudgetRule{"git.status": {Bytes: ptr(4096)}}}
	})
	if resolve(t, b).ServingIsDefault() {
		t.Error("a smaller budget is not the default")
	}
	// Lists and servers change the launch, not what the server serves.
	l := base()
	l.Workspace = profile(func(p *Profile) { p.DisabledMCP = []string{"x"} })
	if !resolve(t, l).ServingIsDefault() {
		t.Error("a deny list does not change the served tools")
	}
}

func TestExecutableDigestIgnoresRevisionsAndOrder(t *testing.T) {
	a := base()
	a.Workspace = profile(func(p *Profile) { p.Servers = []Server{stdio("a", "x"), stdio("b", "y")} })
	b := base()
	b.Workspace = profile(func(p *Profile) { p.Revision = 99; p.Servers = []Server{stdio("b", "y"), stdio("a", "x")} })
	if resolve(t, a).ExecutableDigest() != resolve(t, b).ExecutableDigest() {
		t.Error("revisions or order moved the executable digest")
	}
	c := base()
	c.Workspace = profile(func(p *Profile) { p.Servers = []Server{stdio("a", "x"), stdio("b", "z")} })
	if resolve(t, a).ExecutableDigest() == resolve(t, c).ExecutableDigest() {
		t.Error("a changed command kept the digest")
	}
}
