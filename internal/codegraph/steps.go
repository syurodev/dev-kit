package codegraph

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
)

// RoleOverride marks the call site the Java analyzer writes from an overridden
// method to an override: a dispatch link, not a call anyone wrote. Step.Impls
// is built from these; they are never returned as steps.
const RoleOverride = "override"

// ErrTooManyNodes is returned when one GetSteps call asks for more methods
// than MaxStepNodeIDs; the frontend asks in chunks.
var ErrTooManyNodes = errors.New("codegraph: too many node ids")

// StepFrame is one control structure around a call, outermost first. It is
// language-neutral: Kind is one of if, else, case, catch, finally, try, loop,
// closure, and, or, ternary, await (open — a reader shows an unknown kind by
// its Text); ID is the structure's "line:col", shared by every call in it, and
// for a closure passed as an argument it is the position of the call it is
// passed to; Exits is true when the branch ends in return/throw/break/continue,
// and Exit says which ("return", "throw", "break", "continue"; empty when it
// does not leave, or in an index written before it was recorded). Both ride in
// call_sites.frames as JSON, so an older row simply has no Exit.
type StepFrame struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Text  string `json:"text"`
	Exits bool   `json:"exits,omitempty"`
	Exit  string `json:"exit,omitempty"`
}

// StepEffect is what an extractor recognised at a call: a Kafka produce, an
// HTTP call, a Redis or DB access, as recorded (not evaluated).
type StepEffect struct {
	Kind   string `json:"kind"`   // kafka | http | redis | db
	Role   string `json:"role"`   // produce | consume | call | read | write
	Target string `json:"target"` // topic, method + url, key pattern or table
}

// StepEffectAt is a StepEffect with the method and position it was seen at.
// Col is 0 for a row read off a declaration or by an analyzer that records
// no column; GetSteps then joins it by line.
type StepEffectAt struct {
	From   string
	Line   int
	Col    int
	Effect StepEffect
}

const (
	// MaxStepNodeIDs bounds one GetSteps call; the frontend asks in chunks.
	MaxStepNodeIDs = 200
	// maxStepsPerMethod bounds one method's steps; the rest are reported as truncated.
	maxStepsPerMethod = 300
	// helperDepth is how far below the asked methods callees are read to tell
	// a helper from a step that matters.
	helperDepth = 3
	// maxStepGraph bounds how many methods that reading may load in all.
	maxStepGraph = 2000
)

// Step is one call in a method body, as the flow outline draws it.
type Step struct {
	Line      int    `json:"line"`
	Col       int    `json:"col"`
	Text      string `json:"text"`
	CalleeKey string `json:"calleeKey"`
	To        string `json:"to,omitempty"` // the project node it lands on
	// Target is the project node a call names when To is empty because that
	// node has no body (a repository, Feign client or HTTP interface method).
	Target string      `json:"target,omitempty"`
	Impls  []string    `json:"impls,omitempty"` // overrides of To, when To is overridden
	Frames []StepFrame `json:"frames"`
	// Visible and Reason are Visibility's answer.
	Visible bool        `json:"visible"`
	Reason  string      `json:"reason"`
	Effect  *StepEffect `json:"effect,omitempty"`
}

// StepsResult is GetSteps' answer.
type StepsResult struct {
	Steps     map[string][]Step `json:"steps"`     // node id → steps in source order
	Truncated []string          `json:"truncated"` // node ids whose steps were capped
	// Legacy: a Java index written before steps, read as a flat outline until
	// a re-index. Only Java records steps, so no other language is told to
	// re-index for them.
	Legacy bool `json:"legacy"`
}

// GetSteps returns the steps of each asked method, scored for visibility.
// overrides are the person's "always show / hide" choices
// ({"<key>": "show"|"hide"}, "<Type>#*" for a whole type).
func (s *Service) GetSteps(ctx context.Context, nodeIDs []string, overrides map[string]string) (StepsResult, error) {
	nodeIDs = uniqueIDs(nodeIDs)
	if len(nodeIDs) > MaxStepNodeIDs {
		return StepsResult{}, fmt.Errorf("%w: %d asked, at most %d", ErrTooManyNodes, len(nodeIDs), MaxStepNodeIDs)
	}
	store, detected, release, err := s.activeStoreAndDetection()
	if err != nil {
		return StepsResult{}, err
	}
	defer release()
	out, err := buildSteps(ctx, store, nodeIDs, overrides)
	if err != nil {
		return StepsResult{}, err
	}
	out.Legacy = out.Legacy && detected.Language == "java"
	return out, nil
}

func uniqueIDs(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// buildSteps reports Legacy whenever no asked method's call sites carry step
// text; GetSteps narrows that to the language that would record it.
func buildSteps(ctx context.Context, store Store, nodeIDs []string, overrides map[string]string) (StepsResult, error) {
	out := StepsResult{Steps: make(map[string][]Step, len(nodeIDs)), Truncated: []string{}}
	if len(nodeIDs) == 0 {
		return out, nil
	}
	stats, err := store.CallStats(ctx)
	if err != nil {
		return StepsResult{}, err
	}
	g, err := loadStepGraph(ctx, store, nodeIDs)
	if err != nil {
		return StepsResult{}, err
	}
	j := &stepJudge{g: g, stats: stats, overrides: overrides, helper: map[helperKey]bool{}, joined: map[string]map[int]StepEffect{}}
	anySite, anyText := false, false
	for _, id := range nodeIDs {
		steps := []Step{}
		for i, c := range g.sites[id] {
			if c.Role == RoleOverride {
				continue
			}
			anySite = true
			anyText = anyText || c.Text != ""
			if len(steps) == maxStepsPerMethod {
				out.Truncated = append(out.Truncated, id)
				break
			}
			steps = append(steps, j.step(id, i))
		}
		out.Steps[id] = steps
	}
	out.Legacy = anySite && !anyText
	return out, nil
}

// stepGraph is the asked methods and the callees below them, as far as
// telling a helper apart needs.
type stepGraph struct {
	loaded map[string]bool
	sites  map[string][]CallSite // in GetStepSites' order: line, then column
	impls  map[string][]string
	// effects are what extractors saw in each method, at any line; for a
	// bodyless target too, which has no sites of its own.
	effects map[string][]StepEffectAt
	writers map[string]bool
	// closureCalls[from][frameID] are the project calls made inside the
	// closure passed to the call at frameID ("line:col").
	closureCalls map[string]map[string][]CallSite
}

func loadStepGraph(ctx context.Context, store Store, roots []string) (*stepGraph, error) {
	g := &stepGraph{loaded: map[string]bool{}, sites: map[string][]CallSite{}, impls: map[string][]string{},
		effects: map[string][]StepEffectAt{}, writers: map[string]bool{}, closureCalls: map[string]map[string][]CallSite{}}
	frontier := roots
	for level := 0; level <= helperDepth && len(frontier) > 0 && len(g.loaded) < maxStepGraph; level++ {
		var ask []string
		for _, id := range frontier {
			if !g.loaded[id] && len(g.loaded) < maxStepGraph {
				g.loaded[id] = true
				ask = append(ask, id)
			}
		}
		if len(ask) == 0 {
			break
		}
		sites, err := store.GetStepSites(ctx, ask)
		if err != nil {
			return nil, err
		}
		var next []string
		for _, c := range sites {
			g.sites[c.From] = append(g.sites[c.From], c)
			if c.Role == RoleOverride {
				g.impls[c.From] = append(g.impls[c.From], c.To)
			}
			if c.To != "" && !g.loaded[c.To] {
				next = append(next, c.To)
			}
		}
		if err := g.addEffects(ctx, store, ask); err != nil {
			return nil, err
		}
		writers, err := store.FieldWriters(ctx, ask)
		if err != nil {
			return nil, err
		}
		for id := range writers {
			g.writers[id] = true
		}
		frontier = next
	}

	// What the graph did not load but a step still needs: the overrides of
	// the asked methods' own callees (the cap may have stopped short of
	// them), and what extractors saw in the bodyless targets of any loaded
	// call — a repository method's table, a Feign method's route.
	rootSet := make(map[string]bool, len(roots))
	for _, id := range roots {
		rootSet[id] = true
	}
	unloadedCallees, bodyless := map[string]bool{}, map[string]bool{}
	for id := range g.loaded {
		for _, c := range g.sites[id] {
			if c.Role == RoleOverride {
				continue
			}
			if c.To != "" && !g.loaded[c.To] && rootSet[id] {
				unloadedCallees[c.To] = true
			}
			if c.To == "" && c.Target != "" && !g.loaded[c.Target] {
				bodyless[c.Target] = true
			}
			if c.To == "" && c.Target == "" {
				continue
			}
			for _, fr := range c.Frames {
				if fr.Kind != "closure" {
					continue
				}
				if g.closureCalls[id] == nil {
					g.closureCalls[id] = map[string][]CallSite{}
				}
				g.closureCalls[id][fr.ID] = append(g.closureCalls[id][fr.ID], c)
			}
		}
	}
	if len(unloadedCallees) > 0 {
		impls, err := store.OverridesOf(ctx, slices.Sorted(maps.Keys(unloadedCallees)))
		if err != nil {
			return nil, err
		}
		maps.Copy(g.impls, impls)
	}
	if len(bodyless) > 0 {
		if err := g.addEffects(ctx, store, slices.Sorted(maps.Keys(bodyless))); err != nil {
			return nil, err
		}
	}
	return g, nil
}

func (g *stepGraph) addEffects(ctx context.Context, store Store, ids []string) error {
	effects, err := store.ListEffectsFrom(ctx, ids)
	if err != nil {
		return err
	}
	for _, e := range effects {
		g.effects[e.From] = append(g.effects[e.From], e)
	}
	return nil
}

// stepJudge scores steps, remembering which callees are helpers.
type stepJudge struct {
	g         *stepGraph
	stats     CallStats
	overrides map[string]string
	// helper is keyed by depth too: how far below the asked methods a callee
	// is judged decides how much of its own body can be read, so a verdict
	// reached deep down says nothing about the same method called directly.
	helper map[helperKey]bool
	joined map[string]map[int]StepEffect // method → site index → its effect
}

type helperKey struct {
	id    string
	depth int
}

func (j *stepJudge) step(from string, i int) Step {
	c := j.g.sites[from][i]
	st := Step{Line: c.Line, Col: c.Col, Text: c.Text, CalleeKey: c.CalleeKey, To: c.To, Frames: c.Frames}
	if c.To == "" {
		st.Target = c.Target
	}
	if st.Frames == nil {
		st.Frames = []StepFrame{}
	}
	if c.To != "" {
		st.Impls = j.g.impls[c.To]
	}
	f := j.facts(from, i, 0)
	st.Effect = f.Effect
	st.Visible, st.Reason = Visibility(f, j.stats, j.overrides)
	return st
}

func (j *stepJudge) facts(from string, i int, depth int) StepFacts {
	c := j.g.sites[from][i]
	f := StepFacts{CalleeKey: c.CalleeKey, Text: c.Text, Receiver: c.Receiver, ResultUse: c.ResultUse}
	switch {
	case c.To != "":
		f.Project = true
		// A project method that runs a closure (withLock(k, () -> pay(x)))
		// is shown when the closure's call is: it decides how that runs.
		f.Helper = len(j.g.impls[c.To]) == 0 && !j.wraps(from, c, depth) && j.isHelper(c.To, depth)
		return f
	case c.Target != "":
		// The project's own method with no body to read: never a helper,
		// since nothing says it does nothing, and what extractors read off
		// its declaration is what it does.
		f.Project = true
		if e, ok := preferredEffect(j.g.effects[c.Target]); ok {
			f.Effect = &e
			return f
		}
	}
	if e, ok := j.effectsAt(from)[i]; ok {
		f.Effect = &e
	}
	if !f.Project {
		f.WrapsProjectCall = j.wraps(from, c, depth)
	}
	return f
}

// effectsAt joins what extractors saw in from to its steps, once per method.
// An effect with a column goes to the step at exactly that position. One
// without goes to the first library call on its line only in a method whose
// calls carry no column either (Lua, TypeScript, an older Java index): where
// they do, a column-less row was read off a declaration (a @KafkaListener)
// and is at no call. Each effect goes to at most one step, and a step that
// several land on keeps the one that changes something.
func (j *stepJudge) effectsAt(from string) map[int]StepEffect {
	if joined, ok := j.joined[from]; ok {
		return joined
	}
	effects := j.g.effects[from]
	joined := map[int]StepEffect{}
	j.joined[from] = joined
	if len(effects) == 0 {
		return joined
	}
	type pos struct{ line, col int }
	at, firstLibrary := map[pos]int{}, map[int]int{}
	hasColumns := false
	for i, c := range j.g.sites[from] {
		if c.Role == RoleOverride {
			continue
		}
		hasColumns = hasColumns || c.Col > 0
		if _, ok := at[pos{c.Line, c.Col}]; !ok {
			at[pos{c.Line, c.Col}] = i
		}
		if _, ok := firstLibrary[c.Line]; !ok && c.To == "" && c.Target == "" {
			firstLibrary[c.Line] = i
		}
	}
	for _, e := range effects {
		var i int
		var ok bool
		switch {
		case e.Col > 0:
			i, ok = at[pos{e.Line, e.Col}]
		case !hasColumns:
			i, ok = firstLibrary[e.Line]
		}
		if !ok {
			continue
		}
		if cur, has := joined[i]; !has || effectRank(e.Effect) < effectRank(cur) {
			joined[i] = e.Effect
		}
	}
	return joined
}

// effectRank orders effects by what an outline row should say first: a
// write or a produce changes something, a read only looks.
func effectRank(e StepEffect) int {
	switch e.Role {
	case "write", "produce":
		return 0
	case "call":
		return 1
	default:
		return 2
	}
}

func preferredEffect(effects []StepEffectAt) (StepEffect, bool) {
	var best StepEffect
	found := false
	for _, e := range effects {
		if !found || effectRank(e.Effect) < effectRank(best) {
			best, found = e.Effect, true
		}
	}
	return best, found
}

// wraps reports whether a closure passed to c holds a project call that matters.
func (j *stepJudge) wraps(from string, c CallSite, depth int) bool {
	for _, o := range j.g.closureCalls[from][fmt.Sprintf("%d:%d", c.Line, c.Col)] {
		if o.To == "" || len(j.g.impls[o.To]) > 0 || !j.isHelper(o.To, depth) {
			return true
		}
	}
	return false
}

// isHelper: loaded, not overridden, writes no field, touches nothing an
// extractor saw, and none of its own steps is visible. Anything not known
// for sure is not a helper — hiding a step that matters is worse than showing
// one that does not. Depth bounds the recursion, so a cycle reaches the limit
// and counts as not a helper.
func (j *stepJudge) isHelper(id string, depth int) bool {
	if depth >= helperDepth || !j.g.loaded[id] || j.g.writers[id] || len(j.g.impls[id]) > 0 || len(j.g.effects[id]) > 0 {
		return false
	}
	key := helperKey{id, depth}
	if helper, ok := j.helper[key]; ok {
		return helper
	}
	helper := true
	for i, c := range j.g.sites[id] {
		if c.Role == RoleOverride {
			continue
		}
		if visible, _ := Visibility(j.facts(id, i, depth+1), j.stats, j.overrides); visible {
			helper = false
			break
		}
	}
	j.helper[key] = helper
	return helper
}
