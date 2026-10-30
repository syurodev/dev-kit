package arch

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Rules about the repository's own map: the instructions an agent reads first, the
// documentation a package carries, and how big a file may get. They are ratchets:
// what exists today is listed, what is listed can only shrink, and anything new
// meets the rule from its first commit.

// AGENTS.md is read at the start of every task, so it stays short; detail lives in
// docs/agent-reference. (AGENTS.md says the same: at or below 10 KiB.)
const agentsMaxBytes = 10 * 1024

func TestAgentsGuideStaysShortAndItsLinksResolve(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > agentsMaxBytes {
		t.Errorf("AGENTS.md is %d bytes, the limit is %d: move detail into docs/agent-reference", len(raw), agentsMaxBytes)
	}
	link := regexp.MustCompile(`\]\((docs/[^)#\s]+)`)
	for _, m := range link.FindAllStringSubmatch(string(raw), -1) {
		if _, err := os.Stat(filepath.Join(root, m[1])); err != nil {
			t.Errorf("AGENTS.md links %s, which does not exist", m[1])
		}
	}
}

// Every reference guide is reachable from AGENTS.md; a guide nobody is pointed to
// is a guide nobody reads. Listed here are guides that predate the rule.
var guidesNotYetLinked = map[string]bool{
	"team-baseline.md": true,
}

func TestEveryReferenceGuideIsLinkedFromAgents(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	guides, _ := filepath.Glob(filepath.Join(root, "docs", "agent-reference", "*.md"))
	for _, g := range guides {
		name := filepath.Base(g)
		linked := strings.Contains(string(raw), "docs/agent-reference/"+name)
		switch {
		case !linked && !guidesNotYetLinked[name]:
			t.Errorf("docs/agent-reference/%s is not linked from AGENTS.md", name)
		case linked && guidesNotYetLinked[name]:
			t.Errorf("docs/agent-reference/%s is linked now; remove it from guidesNotYetLinked", name)
		}
	}
	for name := range guidesNotYetLinked {
		if _, err := os.Stat(filepath.Join(root, "docs", "agent-reference", name)); err != nil {
			t.Errorf("guidesNotYetLinked lists %s, which does not exist", name)
		}
	}
}

// A package says what it is for in a package comment ("// Package x ..."): the
// first thing a person or an agent reads when deciding whether code belongs there.
// These packages predate the rule; the list can only shrink, and a new package
// has its comment from the first commit.
var packagesWithoutDoc = map[string]bool{
	"internal/acp":          true,
	"internal/agenthub":     true,
	"internal/app":          true,
	"internal/apperrors":    true,
	"internal/audit":        true,
	"internal/bridge":       true,
	"internal/capabilities": true,
	"internal/checkpoint":   true,
	"internal/cloudconfig":  true,
	"internal/codegraph":    true,
	"internal/commands":     true,
	"internal/crypto":       true,
	"internal/database":     true,
	"internal/interaction":  true,
	"internal/kafka":        true,
	"internal/lsp":          true,
	"internal/lua":          true,
	"internal/mcp":          true,
	"internal/mcp/install":  true,
	"internal/modules":      true,
	"internal/notes":        true,
	"internal/permissions":  true,
	"internal/ssh":          true,
	"internal/storage":      true,
	"internal/syncstore":    true,
	"internal/terminal":     true,
	"internal/tools":        true,
	"internal/vault":        true,
}

func TestEveryPackageHasADocComment(t *testing.T) {
	documented := map[string]bool{}
	seen := map[string]bool{}
	for _, f := range prodFiles(t) {
		if !strings.HasPrefix(f.Dir, "internal/") {
			continue
		}
		seen[f.Dir] = true
		if f.File.Doc != nil && strings.HasPrefix(strings.TrimSpace(f.File.Doc.Text()), "Package ") {
			documented[f.Dir] = true
		}
	}
	var dirs []string
	for d := range seen {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		switch {
		case !documented[d] && !packagesWithoutDoc[d]:
			t.Errorf("%s has no package comment (\"// Package %s ...\"); say what the package is for", d, filepath.Base(d))
		case documented[d] && packagesWithoutDoc[d]:
			t.Errorf("%s has a package comment now; remove it from packagesWithoutDoc", d)
		}
	}
	for d := range packagesWithoutDoc {
		if !seen[d] {
			t.Errorf("packagesWithoutDoc lists %s, which no longer exists", d)
		}
	}
}

// A production file past this size is doing more than one thing, and is where the
// next change goes wrong because nobody can hold it all. These files are over it
// today; each may not grow past its cap, and a new file may not exceed the limit.
// Splitting one is the way to lower its cap.
const maxFileLines = 1200

var fileLineCaps = map[string]int{
	"internal/agenthub/adapter_codex.go": 1850,
	"internal/agenthub/peer.go":          1400,
	"internal/agenthub/service.go":       3250,
	"internal/bridge/errors.go":          1250,
	"internal/codegraph/store.go":        1900,
	"internal/storage/sqlite.go":         1700,
	"main.go":                            1700,
}

func TestProductionFilesDoNotGrowPastTheirCaps(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range prodFiles(t) {
		limit := maxFileLines
		if c, ok := fileLineCaps[f.Rel]; ok {
			limit = c
			seen[f.Rel] = true
		}
		if n := f.lines(); n > limit {
			t.Errorf("%s has %d lines, the limit is %d: split it by responsibility instead of raising the limit", f.Rel, n, limit)
		}
	}
	for rel := range fileLineCaps {
		if !seen[rel] {
			t.Errorf("fileLineCaps lists %s, which no longer exists", rel)
		}
	}
}
