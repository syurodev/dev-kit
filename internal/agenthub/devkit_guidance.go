package agenthub

import (
	"regexp"
	"sort"
	"strings"
)

// DevKitServing is the DevKit tool surface one prepared launch serves: the
// names of the tools its endpoint really registered and its policy lets through.
// It is the only input to host guidance besides the vendor spelling and the
// brief. A nil *DevKitServing is the legacy launch (no policy was prepared; the
// full guidance is rendered, as before); a non-nil one with no tools is a
// positively prepared policy that serves nothing, which is not the same thing.
type DevKitServing struct {
	// Tools is sorted and has no duplicates.
	Tools []string
}

// NewDevKitServing copies and normalizes names.
func NewDevKitServing(names []string) *DevKitServing {
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, n := range names {
		if _, dup := seen[n]; dup || n == "" {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	sort.Strings(out)
	return &DevKitServing{Tools: out}
}

// Clone detaches the value from its source; nil stays nil (legacy).
func (d *DevKitServing) Clone() *DevKitServing {
	if d == nil {
		return nil
	}
	return NewDevKitServing(d.Tools)
}

func (d *DevKitServing) set() map[string]struct{} {
	m := make(map[string]struct{}, len(d.Tools))
	for _, t := range d.Tools {
		m[t] = struct{}{}
	}
	return m
}

// The prompt template (devkit_prompt.go) is the one source of host guidance. It
// is split once into chunks, and a chunk renders only when every tool it names
// is served, so a section can never advertise a tool the endpoint hides and no
// second list of tool names has to be kept. Inside a chunk:
//
//	{{tool}}              a tool reference, spelled for the vendor
//	{{?a,b}}text{{/}}     text kept only when all the listed tools are served
//	{{+tool}}             requires the tool without naming it
//
// Text that is not a chunk requirement (the safety core, the headings) always
// renders. The workspace brief is inserted last and is data: it is not scanned.

var (
	spanRe    = regexp.MustCompile(`(?s)\{\{\?([A-Za-z_.,]+)\}\}(.*?)\{\{/\}\}`)
	requireRe = regexp.MustCompile(`\{\{\+([A-Za-z_.]+)\}\}`)
)

type promptChunk struct {
	id    string
	text  string // without its trailing newlines
	sep   string // the newlines that followed it in the template
	needs []string
}

var promptChunks = splitPrompt(devkitPromptTemplate)

// splitPrompt cuts the template at every blank-line boundary and at every
// top-level bullet, headings included, keeping each chunk's own separator so an
// unfiltered render is the template byte for byte.
func splitPrompt(tpl string) []promptChunk {
	lines := strings.SplitAfter(tpl, "\n")
	var chunks []promptChunk
	var cur []string
	flush := func() {
		if len(cur) == 0 {
			return
		}
		raw := strings.Join(cur, "")
		text := strings.TrimRight(raw, "\n")
		chunks = append(chunks, promptChunk{text: text, sep: raw[len(text):]})
		cur = nil
	}
	prevBlank := false
	for _, ln := range lines {
		blank := strings.TrimRight(ln, "\n") == ""
		if !blank && (prevBlank || strings.HasPrefix(ln, "- ") || strings.HasPrefix(ln, "%NAMING%") || strings.HasPrefix(ln, "## ")) {
			flush()
		}
		cur = append(cur, ln)
		prevBlank = blank
	}
	flush()
	for i := range chunks {
		c := &chunks[i]
		c.id = chunkID(c.text)
		c.needs = chunkNeeds(c.text)
	}
	return chunks
}

var boldTitle = regexp.MustCompile(`^- (?:\{\{\+[A-Za-z_.]+\}\})?\*\*([^*]+)\*\*`)

func chunkID(text string) string {
	if m := boldTitle.FindStringSubmatch(text); m != nil {
		return strings.TrimSuffix(strings.ToLower(m[1]), ".")
	}
	first, _, _ := strings.Cut(text, "\n")
	if len(first) > 40 {
		first = first[:40]
	}
	return strings.ToLower(first)
}

// chunkNeeds are the tools a chunk cannot render without: every plain reference
// and requirement marker outside an optional span.
func chunkNeeds(text string) []string {
	outside := spanRe.ReplaceAllString(text, "")
	set := map[string]struct{}{}
	for _, m := range promptToolRef.FindAllStringSubmatch(outside, -1) {
		set[m[1]] = struct{}{}
	}
	for _, m := range requireRe.FindAllStringSubmatch(outside, -1) {
		set[m[1]] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// guards say, in fixed words, what a hidden guarded section would have said
// that is not a tool advertisement: the action is unavailable and the shell is
// not a way around the guard. Keyed by chunk id.
var guards = map[string]string{
	"git writes go through devkit": "- **Git writes are guarded.** DevKit's git tools are not available in this session. " +
		"Do not run `git commit`, `git push`, `git pull`, `git rebase`, `git cherry-pick` or `git revert` in the shell " +
		"to get around that: the shell skips the branch guards and task rules, and DevKit may refuse it. " +
		"Read-only git in the shell (log, show, diff, blame) is fine. When a change needs one of those actions, tell the person.",
}

// noBriefFallbackUnserved stands in for the brief when DevKit could not say where
// the session is and workspace.list is not served: it asks instead of naming an
// absent tool.
const noBriefFallbackUnserved = "DevKit could not say which folders this session may use. Work only inside your\n" +
	"working directory and any folder the person names, and ask the person when you\n" +
	"need to know more about the workspace."

// devkitPrompt renders the host guidance for one vendor and one session. brief
// says where the session is (Service.SessionBrief), or is empty. served is what
// the prepared launch's endpoint serves; nil is the legacy launch and renders
// everything.
func devkitPrompt(naming toolNaming, brief string, served *DevKitServing) string {
	var have map[string]struct{}
	if served != nil {
		have = served.set()
	}
	ok := func(tools []string) bool {
		if served == nil {
			return true
		}
		for _, t := range tools {
			if _, in := have[t]; !in {
				return false
			}
		}
		return true
	}
	spelled := func(text string) string { return spellText(naming, served, text) }

	type piece struct{ text, sep string }
	var pieces []piece
	carry := "" // the longest separator among the chunks dropped since the last one kept
	for _, c := range promptChunks {
		var text string
		switch {
		case c.text == "%NAMING%":
			text = renderNote(naming, served)
		case c.text == "%BRIEF%":
			text = "%BRIEF%"
		case ok(c.needs):
			text = spelled(c.text)
		default:
			g, guarded := guards[c.id]
			if !guarded {
				if len(c.sep) > len(carry) {
					carry = c.sep
				}
				continue
			}
			text = g
		}
		if n := len(pieces); n > 0 && len(carry) > len(pieces[n-1].sep) {
			pieces[n-1].sep = carry
		}
		carry = ""
		pieces = append(pieces, piece{text, c.sep})
	}
	var b strings.Builder
	for _, p := range pieces {
		b.WriteString(p.text)
		b.WriteString(p.sep)
	}

	brief = strings.TrimSpace(brief)
	if brief == "" {
		if ok([]string{"workspace.list"}) {
			brief = spelled(devkitPromptNoBrief)
		} else {
			brief = noBriefFallbackUnserved
		}
	}
	// The brief goes in last, untouched: tool-looking text in it is data.
	return strings.Replace(b.String(), "%BRIEF%", brief, 1)
}

// spellText resolves optional spans and requirement markers against what is
// served and spells every remaining tool reference for the vendor.
func spellText(naming toolNaming, served *DevKitServing, text string) string {
	text = spanRe.ReplaceAllStringFunc(text, func(span string) string {
		m := spanRe.FindStringSubmatch(span)
		if servesAll(served, strings.Split(m[1], ",")...) {
			return m[2]
		}
		return ""
	})
	text = requireRe.ReplaceAllString(text, "")
	return promptToolRef.ReplaceAllStringFunc(text, func(ref string) string {
		return naming.tool(ref[2 : len(ref)-2])
	})
}

func renderChunk(naming toolNaming, served *DevKitServing, c promptChunk) string {
	return spellText(naming, served, c.text)
}

// renderNote is the vendor's naming note with an example the session really
// serves, or nothing when it serves no tool at all.
func renderNote(naming toolNaming, served *DevKitServing) string {
	if naming.note == "" {
		return ""
	}
	example := naming.example
	if served != nil {
		have := served.set()
		if _, in := have[example]; !in {
			if len(served.Tools) == 0 {
				return ""
			}
			example = served.Tools[0]
		}
	}
	return strings.NewReplacer("%EX%", example, "%EXU%", strings.ReplaceAll(example, ".", "_")).Replace(naming.note)
}

// servesAll reports whether every named tool is served; a nil value (legacy)
// serves everything.
func servesAll(d *DevKitServing, tools ...string) bool {
	if d == nil {
		return true
	}
	have := d.set()
	for _, t := range tools {
		if _, ok := have[t]; !ok {
			return false
		}
	}
	return true
}

// replacementGuidanceLimit bounds the text a live group change may carry.
const replacementGuidanceLimit = 4096

// replacementGuidance is the compact paragraph a future live change (H4c) can
// attach to a confirmed result when group becomes available: the same section
// definitions the launch prompt is rendered from, restricted to the sections
// that name a tool of that group and that the new surface serves completely.
// Pure: it mutates nothing, asks nobody and grants nothing — a tool it names is
// still subject to the vendor's and the host's normal permission checks, and
// the launch-time prompt and pre-approved list stay as they were until a
// reload. Empty when the group has no section.
func replacementGuidance(naming toolNaming, served *DevKitServing, group string) string {
	if served == nil {
		return ""
	}
	inGroup := func(tool string) bool { g, _, _ := strings.Cut(tool, "."); return g == group }
	var out []string
	size := 0
	for _, c := range promptChunks {
		if !strings.HasPrefix(c.text, "- ") || !servesAll(served, c.needs...) {
			continue
		}
		touches := false
		for _, n := range c.needs {
			touches = touches || inGroup(n)
		}
		if !touches {
			continue
		}
		piece := renderChunk(naming, served, c)
		if size+len(piece) > replacementGuidanceLimit {
			break
		}
		size += len(piece)
		out = append(out, piece)
	}
	if len(out) == 0 {
		return ""
	}
	return "DevKit tools are now available in this session, from the same rules as at launch. " +
		"Your launch permissions did not change, so each tool still follows the normal permission flow:\n\n" + strings.Join(out, "\n")
}
