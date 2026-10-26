package mcp

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ResultBudget is how many bytes of text one tool call may put into an agent's
// context. Everything a tool returns stays in the conversation for every later
// request of the turn, so a 47 KB codegraph.searchSymbol page is paid again on
// each of the next fifty model calls. 24 KB is about 6k tokens: a page of
// results the agent can use, not a dump it has to scroll past. A result over it
// is cut at a boundary and says what was left out and how to ask for it
// (limitResult below). One place, here — not a check in each of the tools.
const ResultBudget = 24 * 1024

// resultMarkerReserve is what the marker may take on top of the cut result.
const resultMarkerReserve = 700

// budgetExempt are the tools whose result is never cut, each for a reason a
// test pins (TestBudgetExemptionsAreRealToolsWithAReason):
//
//   - pdf.*: bounded by their own budget (~48 KiB of text a call, with the pages
//     that did not fit listed so the agent asks for them next). Cutting that
//     again would make "the pages that did not fit" wrong, and viewPages answers
//     with page images, which are never text to cut.
//   - codegraph.readFile: "the full contents of one file" is its contract. A
//     source file cut mid-way and then edited from is worse than a big result;
//     codegraph.getCodeSnippet reads a line range for the agent who wants less.
//
// A result with any non-text block (an image) is exempt whatever the tool is.
var budgetExempt = map[string]string{
	"pdf.list":           "own byte budget",
	"pdf.info":           "own byte budget",
	"pdf.search":         "own byte budget",
	"pdf.readText":       "own byte budget, names the pages that did not fit",
	"pdf.viewPages":      "page images",
	"codegraph.readFile": "its contract is the whole file",
}

// resultHints say how to get the part of an answer that was cut, per tool. A
// tool without an entry gets the generic advice.
var resultHints = map[string]string{
	"codegraph.searchSymbol":   "Repeat with a smaller limit, a more specific query, or kind/layer/pathPrefix filters.",
	"codegraph.listEndpoints":  "Repeat with a smaller limit, or narrow with kind/service/verb/pathPrefix/contains.",
	"codegraph.getApiFlow":     "Read a part with codegraph.getCodeSnippet, or start from a deeper method with codegraph.getNeighbors (smaller depth).",
	"codegraph.getNeighbors":   "Repeat with a smaller depth or a filter.",
	"codegraph.getCodeSnippet": "Repeat with a narrower line..endLine range.",
	"codegraph.getBlastRadius": "Name a narrower file or folder.",
	"git.diff":                 "Name one commit with rev, or read a single file with the shell.",
	"git.log":                  "Repeat with a smaller limit.",
	"tasks.comment.list":       "Pass nextBeforeId as beforeId for older comments; use tasks.comment.get with taskId and commentId for a full comment body.",
	"db.query":                 "Repeat with a smaller maxRows, fewer columns, or a WHERE/LIMIT.",
	"kafka.peekMessages":       "Repeat with a smaller limit.",
	"workspace.memory.list":    "Pass nextCursor from the previous page, or a smaller limit.",
	"workspace.memory.search":  "Pass nextCursor from the previous page, or a smaller limit.",
	"workspace.memory.history": "Repeat with a smaller limit; read one revision's body with includeBody.",
	"api.listCollections":      "Name one collection.",
}

const genericResultHint = "Ask for less: a smaller limit, a narrower query or filter, or the cursor/offset the tool offers."

// withResultBudget cuts a tool result that would put more than budget bytes of
// text in an agent's context. It sits in front of every registered tool, so a
// new tool is covered without knowing about it.
func withResultBudget(budget int) sdk.Middleware {
	return withMeasuredResultBudget(budget, nil, nil)
}

// limitResult shrinks call in place when its text is over budget.
func limitResult(tool string, call *sdk.CallToolResult, budget int) {
	total := 0
	for _, c := range call.Content {
		text, ok := c.(*sdk.TextContent)
		if !ok {
			return // an image (or anything but text) is never cut
		}
		total += len(text.Text)
	}
	if total <= budget {
		return
	}
	hint := resultHints[tool]
	if hint == "" {
		hint = genericResultHint
	}
	// The usual shape: one text block that is the JSON of structuredContent.
	// Shrink the value so what remains is still the object the agent and the
	// window's card expect, with a "_truncated" note beside the data.
	if len(call.Content) == 1 && call.StructuredContent != nil {
		if shrinkStructured(call, budget, hint) {
			return
		}
	}
	cutText(call, budget, hint)
}

// shrinkStructured reports whether it managed; if not, call is untouched.
func shrinkStructured(call *sdk.CallToolResult, budget int, hint string) bool {
	text := call.Content[0].(*sdk.TextContent)
	raw, err := json.Marshal(call.StructuredContent)
	if err != nil || string(raw) != text.Text {
		return false
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return false
	}
	root, ok := v.(map[string]any)
	if !ok {
		return false
	}
	notes, cutPaged, ok := shrinkValue(root, budget-resultMarkerReserve)
	if !ok {
		return false
	}
	if cutPaged {
		// The cursor points past rows that were just taken out; following it
		// would skip them without a word.
		delete(root, "nextCursor")
		notes = append(notes, "nextCursor removed (it would skip the rows left out)")
	}
	if _, ok := root["truncated"]; ok {
		root["truncated"] = true // db.query / kafka rows say so themselves
	}
	root["_truncated"] = fmt.Sprintf("Result cut to the %d KB budget: %s. %s", budget/1024, strings.Join(notes, "; "), hint)
	out, err := json.Marshal(root)
	if err != nil {
		return false
	}
	call.Content = []sdk.Content{&sdk.TextContent{Text: string(out)}}
	call.StructuredContent = json.RawMessage(out)
	return true
}

// shrinkValue trims root until it marshals within limit: the largest array
// keeps the most leading items that fit, and when no array is left to trim
// the longest string is cut. It returns what it took out, whether an array
// beside a cursor was among it, and whether the value now fits.
func shrinkValue(root map[string]any, limit int) (notes []string, cutPaged, fits bool) {
	removed := map[string]int{}
	order := []string{}
	_, hasCursor := root["nextCursor"]
	cutStrings := 0
	for i := 0; i < 200; i++ {
		raw, err := json.Marshal(root)
		if err != nil {
			return nil, false, false
		}
		if len(raw) <= limit {
			break
		}
		if slot := largestArray(root, ""); slot != nil && len(slot.get()) >= 1 {
			items := slot.get()
			// The most items that still fit with the rest as it is; when not
			// even none do, the loop moves on to the next array or string.
			lo, hi := 0, len(items)-1
			for lo < hi {
				mid := (lo + hi + 1) / 2
				slot.set(items[:mid])
				if probe, err := json.Marshal(root); err == nil && len(probe) <= limit {
					lo = mid
				} else {
					hi = mid - 1
				}
			}
			keep := lo
			if _, seen := removed[slot.path]; !seen {
				order = append(order, slot.path)
			}
			removed[slot.path] += len(items) - keep
			slot.set(items[:keep])
			if hasCursor && !strings.Contains(slot.path, ".") {
				cutPaged = true
			}
			continue
		}
		if s := longestString(root); s != nil && len(s.get()) > 256 {
			excess := len(raw) - limit
			cur := s.get()
			target := len(cur) - excess - 64
			if target < 128 {
				target = 128
			}
			s.set(cutAtBoundary(cur, target) + "…")
			cutStrings++
			continue
		}
		break
	}
	raw, err := json.Marshal(root)
	if err != nil || len(raw) > limit {
		return nil, false, false
	}
	for _, p := range order {
		notes = append(notes, fmt.Sprintf("%d more items of %q left out", removed[p], p))
	}
	if cutStrings > 0 {
		notes = append(notes, fmt.Sprintf("%d long text value(s) shortened", cutStrings))
	}
	return notes, cutPaged, true
}

type arraySlot struct {
	path string
	size int
	get  func() []any
	set  func([]any)
}

// largestArray finds the array that takes the most bytes anywhere in v.
func largestArray(v any, path string) *arraySlot {
	var best *arraySlot
	var walk func(v any, path string, set func([]any))
	walk = func(v any, path string, set func([]any)) {
		switch t := v.(type) {
		case map[string]any:
			keys := make([]string, 0, len(t))
			for k := range t {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				k := k
				child := k
				if path != "" {
					child = path + "." + k
				}
				walk(t[k], child, func(a []any) { t[k] = a })
			}
		case []any:
			raw, _ := json.Marshal(t)
			slot := &arraySlot{path: path, size: len(raw)}
			cur := t
			slot.get = func() []any { return cur }
			slot.set = func(a []any) { cur = a; set(a) }
			if len(t) > 0 && (best == nil || slot.size > best.size) {
				best = slot
			}
			for i, item := range t {
				i := i
				walk(item, path+"[]", func(a []any) { t[i] = a })
			}
		}
	}
	walk(v, path, func([]any) {})
	return best
}

type stringSlot struct {
	get func() string
	set func(string)
}

func longestString(v any) *stringSlot {
	var best *stringSlot
	bestLen := 0
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			for k, child := range t {
				k := k
				if s, ok := child.(string); ok {
					if len(s) > bestLen {
						bestLen = len(s)
						best = &stringSlot{get: func() string { return t[k].(string) }, set: func(n string) { t[k] = n }}
					}
					continue
				}
				walk(child)
			}
		case []any:
			for i, child := range t {
				i := i
				if s, ok := child.(string); ok {
					if len(s) > bestLen {
						bestLen = len(s)
						best = &stringSlot{get: func() string { return t[i].(string) }, set: func(n string) { t[i] = n }}
					}
					continue
				}
				walk(child)
			}
		}
	}
	walk(v)
	return best
}

// cutText cuts plain text blocks to the budget, at the last line break in the
// final fifth of the allowance (else at a rune boundary), and says what went.
func cutText(call *sdk.CallToolResult, budget int, hint string) {
	room := budget - resultMarkerReserve
	left := 0
	kept := make([]sdk.Content, 0, len(call.Content))
	for _, c := range call.Content {
		text := c.(*sdk.TextContent)
		if room <= 0 {
			left += len(text.Text)
			continue
		}
		if len(text.Text) <= room {
			room -= len(text.Text)
			kept = append(kept, c)
			continue
		}
		cut := cutAtBoundary(text.Text, room)
		left += len(text.Text) - len(cut)
		room = 0
		kept = append(kept, &sdk.TextContent{Text: cut})
	}
	marker := fmt.Sprintf("\n[Result cut to the %d KB budget: %d bytes left out. %s]", budget/1024, left, hint)
	if n := len(kept); n > 0 {
		last := kept[n-1].(*sdk.TextContent)
		kept[n-1] = &sdk.TextContent{Text: last.Text + marker}
	} else {
		kept = append(kept, &sdk.TextContent{Text: marker})
	}
	call.Content = kept
	call.StructuredContent = nil // it held the whole result; the cut text is the answer
}

// cutAtBoundary returns at most n bytes of s, ending at a line break when one
// falls in the last fifth of them, else at a rune boundary.
func cutAtBoundary(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := s[:n]
	if i := strings.LastIndexByte(cut, '\n'); i >= n-n/5 {
		return cut[:i]
	}
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}
