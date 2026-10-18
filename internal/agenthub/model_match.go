package agenthub

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// How a model an agent names is found in a vendor's list.
//
// A vendor lists a model by an id (`opencode/glm-5.3-flash-free`) and shows a
// name (`GLM-5.3 Flash Free`). An agent that read the name off a screen, or
// recalled it, writes the name, the id in another case, or the two joined. The
// first rules take a spelling that can only mean one model; anything that
// would be a guess is refused with the models that come closest, so the agent
// corrects the name instead of getting a bare "invalid setting".
//
// Taking a looser spelling cannot widen what the agent may do: the model that
// was found — not the words the agent used — is what the cost policy compares
// with the parent's and what the person is asked about.

const (
	// modelListAllUpTo: a vendor with this many models or fewer is listed whole
	// in an error; a larger one gets its closest matches.
	modelListAllUpTo = 12
	// modelSuggestions is how many close models an error names.
	modelSuggestions = 5
	// modelSuggestionClip bounds one model's text inside an error: the names
	// are the vendor's, and the error is read by an agent.
	modelSuggestionClip = 80
)

// ModelChoiceError is a model or effort an agent named that the vendor does
// not offer, with a sentence saying what would work. It is an
// ErrInvalidConfigOption, and the bridge publishes its Detail (the sentence is
// built here from the vendor's list and the caller's own words, never from an
// underlying error).
type ModelChoiceError struct {
	Detail string
}

func (e *ModelChoiceError) Error() string { return ErrInvalidConfigOption.Error() + ": " + e.Detail }
func (e *ModelChoiceError) Unwrap() error { return ErrInvalidConfigOption }

// ModelChoiceDetail is the sentence of a ModelChoiceError anywhere in err's
// chain.
func ModelChoiceDetail(err error) (string, bool) {
	var mc *ModelChoiceError
	if errors.As(err, &mc) {
		return mc.Detail, true
	}
	return "", false
}

func modelChoiceErrorf(format string, args ...any) error {
	return &ModelChoiceError{Detail: oneLine(fmt.Sprintf(format, args...))}
}

// oneLine folds whitespace (a vendor's name may hold a newline) so the
// sentence stays one line.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// normalizeModelText keeps letters and digits, lower-cased: "GLM-5.3 Flash Free"
// and "glm_5.3-flash-free" are the same text.
func normalizeModelText(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// ModelMatch is the outcome of VendorModels.Resolve.
type ModelMatch struct {
	// Model is the one model the text meant, when Found.
	Model ModelChoice
	Found bool
	// Candidates are, when not Found, the models that come closest — or, when
	// Ambiguous, the several the text matched.
	Candidates []ModelChoice
	Ambiguous  bool
}

// Resolve finds the one model ref means.
func (v VendorModels) Resolve(ref string) ModelMatch {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ModelMatch{}
	}
	// Rules in order of how sure the match is. A rule that matches several
	// models is ambiguous and ends the search: a later, looser rule must not
	// pick one of them.
	norm := normalizeModelText(ref)
	rules := []func(ModelChoice) bool{
		func(m ModelChoice) bool { return m.Value == ref },
		func(m ModelChoice) bool { return strings.EqualFold(m.Value, ref) },
		func(m ModelChoice) bool { return m.Name != "" && strings.EqualFold(m.Name, ref) },
	}
	if norm != "" {
		rules = append(rules, func(m ModelChoice) bool {
			return normalizeModelText(m.Value) == norm || (m.Name != "" && normalizeModelText(m.Name) == norm)
		})
		// A fragment of one model's id or name ("flash free"): only when it is
		// long enough to mean something.
		if utf8.RuneCountInString(norm) >= 4 {
			rules = append(rules, func(m ModelChoice) bool {
				return strings.Contains(normalizeModelText(m.Value), norm) || (m.Name != "" && strings.Contains(normalizeModelText(m.Name), norm))
			})
		}
	}
	for _, rule := range rules {
		switch hits := pickModels(v.Models, rule); len(hits) {
		case 0:
		case 1:
			return ModelMatch{Model: hits[0], Found: true}
		default:
			return ModelMatch{Candidates: rankModels(hits, ref, false), Ambiguous: true}
		}
	}
	return ModelMatch{Candidates: rankModels(v.Models, ref, true)}
}

func pickModels(models []ModelChoice, rule func(ModelChoice) bool) []ModelChoice {
	var out []ModelChoice
	for _, m := range models {
		if rule(m) {
			out = append(out, m)
		}
	}
	return out
}

// rankModels orders models by how near their id or name is to ref (edit
// distance over the letters and digits) and keeps the nearest few — only the
// ones that are near at all when nearOnly (the models that matched are all
// candidates).
func rankModels(models []ModelChoice, ref string, nearOnly bool) []ModelChoice {
	norm := normalizeModelText(ref)
	type scored struct {
		m    ModelChoice
		dist int
	}
	var all []scored
	for _, m := range models {
		d := editDistance(norm, normalizeModelText(m.Value))
		if m.Name != "" {
			if nd := editDistance(norm, normalizeModelText(m.Name)); nd < d {
				d = nd
			}
		}
		all = append(all, scored{m, d})
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].dist < all[j].dist })
	// Near means a third of the text, at least 3 edits: a typo or a missing
	// word, not "anything".
	limit := len(norm) / 3
	if limit < 3 {
		limit = 3
	}
	var out []ModelChoice
	for _, s := range all {
		if len(out) == modelSuggestions || (nearOnly && s.dist > limit) {
			break
		}
		out = append(out, s.m)
	}
	return out
}

// editDistance is the Levenshtein distance between two strings, by runes.
func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 {
		return len(rb)
	}
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

// modelDescribe is how an error names one model: its id, and its name when
// that differs.
func modelDescribe(m ModelChoice) string {
	value := clipRunes(oneLine(m.Value), modelSuggestionClip)
	name := oneLine(m.Name)
	if name == "" || name == m.Value {
		return value
	}
	return fmt.Sprintf("%s (%s)", value, clipRunes(name, modelSuggestionClip))
}

func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

// unknownModelError says a model is not offered and what would work: the whole
// list for a small vendor, the closest few for a large one (with the way to
// search the rest), or the several models a name matched.
func unknownModelError(vendor AgentType, v VendorModels, ref string, match ModelMatch) error {
	ref = clipRunes(oneLine(ref), 120)
	near := match.Candidates
	if match.Ambiguous {
		names := make([]string, 0, len(near))
		for _, m := range near {
			names = append(names, modelDescribe(m))
		}
		return modelChoiceErrorf("%q matches several %s models (%s) — pass the full id.", ref, vendor, strings.Join(names, "; "))
	}
	if len(v.Models) <= modelListAllUpTo {
		return modelChoiceErrorf("%s has no model %q (it offers: %s).", vendor, ref, valuesText(v.ModelValues()))
	}
	if len(near) == 0 {
		return modelChoiceErrorf("%s has no model %q, and none of its %d models is close. Search the list with agenthub.listModels (agentType %q, query).", vendor, ref, len(v.Models), vendor)
	}
	names := make([]string, 0, len(near))
	for _, m := range near {
		names = append(names, modelDescribe(m))
	}
	return modelChoiceErrorf("%s has no model %q. Closest: %s. Pass one of those ids; agenthub.listModels (agentType %q, query) searches all %d.", vendor, ref, strings.Join(names, "; "), vendor, len(v.Models))
}
