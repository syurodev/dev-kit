package codegraph

import (
	"context"
	"errors"
	"fmt"
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
// passed to; Exits is true when the branch ends in return/throw/break/continue.
type StepFrame struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Text  string `json:"text"`
	Exits bool   `json:"exits,omitempty"`
}

// StepEffect is what an extractor recognised at a call: a Kafka produce, an
// HTTP call, a Redis or DB access, as recorded (not evaluated).
type StepEffect struct {
	Kind   string `json:"kind"`   // kafka | http | redis | db
	Role   string `json:"role"`   // produce | consume | call | read | write
	Target string `json:"target"` // topic, method + url, key pattern or table
}

// StepEffectAt is a StepEffect with the method and line it was seen at.
type StepEffectAt struct {
	From   string
	Line   int
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
	Line      int         `json:"line"`
	Col       int         `json:"col"`
	Text      string      `json:"text"`
	CalleeKey string      `json:"calleeKey"`
	To        string      `json:"to,omitempty"`    // the project node it lands on
	Impls     []string    `json:"impls,omitempty"` // overrides of To, when To is overridden
	Frames    []StepFrame `json:"frames"`
	Visible   bool        `json:"visible"`
	Reason    string      `json:"reason"`
	Effect    *StepEffect `json:"effect,omitempty"`
}

// StepsResult is GetSteps' answer.
type StepsResult struct {
	Steps     map[string][]Step `json:"steps"`     // node id → steps in source order
	Truncated []string          `json:"truncated"` // node ids whose steps were capped
	Legacy    bool              `json:"legacy"`    // the index predates steps: a flat outline
}

// GetSteps returns the steps of each asked method, scored for visibility.
// overrides are the person's "always show / hide" choices
// ({"<key>": "show"|"hide"}, "<Type>#*" for a whole type).
func (s *Service) GetSteps(ctx context.Context, nodeIDs []string, overrides map[string]string) (StepsResult, error) {
	if len(nodeIDs) > MaxStepNodeIDs {
		return StepsResult{}, fmt.Errorf("%w: %d asked, at most %d", ErrTooManyNodes, len(nodeIDs), MaxStepNodeIDs)
	}
	store, release, err := s.activeStore()
	if err != nil {
		return StepsResult{}, err
	}
	defer release()
	return buildSteps(ctx, store, nodeIDs, overrides)
}

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
	j := &stepJudge{g: g, stats: stats, overrides: overrides, helper: map[string]int8{}}
	anySite, anyText := false, false
	for _, id := range nodeIDs {
		steps := []Step{}
		for _, c := range g.sites[id] {
			if c.Role == RoleOverride {
				continue
			}
			anySite = true
			anyText = anyText || c.Text != ""
			if len(steps) == maxStepsPerMethod {
				out.Truncated = append(out.Truncated, id)
				break
			}
			steps = append(steps, j.step(id, c))
		}
		out.Steps[id] = steps
	}
	out.Legacy = anySite && !anyText
	return out, nil
}

// stepGraph is the asked methods and the callees below them, as far as
// telling a helper apart needs.
type stepGraph struct {
	loaded  map[string]bool
	sites   map[string][]CallSite
	impls   map[string][]string
	effects map[string][]StepEffect // effectKey(from, line) → effects
	writers map[string]bool
}

func effectKey(from string, line int) string { return fmt.Sprintf("%s\x00%d", from, line) }

func loadStepGraph(ctx context.Context, store Store, roots []string) (*stepGraph, error) {
	g := &stepGraph{loaded: map[string]bool{}, sites: map[string][]CallSite{}, impls: map[string][]string{},
		effects: map[string][]StepEffect{}, writers: map[string]bool{}}
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
		effects, err := store.ListEffectsFrom(ctx, ask)
		if err != nil {
			return nil, err
		}
		for _, e := range effects {
			k := effectKey(e.From, e.Line)
			g.effects[k] = append(g.effects[k], e.Effect)
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
	return g, nil
}

// stepJudge scores steps, remembering which callees are helpers.
type stepJudge struct {
	g         *stepGraph
	stats     CallStats
	overrides map[string]string
	helper    map[string]int8 // 1 helper, 2 not, 3 being judged (a cycle counts as not)
}

func (j *stepJudge) step(from string, c CallSite) Step {
	st := Step{Line: c.Line, Col: c.Col, Text: c.Text, CalleeKey: c.CalleeKey, To: c.To, Frames: c.Frames}
	if st.Frames == nil {
		st.Frames = []StepFrame{}
	}
	if c.To != "" {
		st.Impls = j.g.impls[c.To]
	}
	f := j.facts(from, c, 0)
	st.Effect = f.Effect
	st.Visible, st.Reason = Visibility(f, j.stats, j.overrides)
	return st
}

func (j *stepJudge) facts(from string, c CallSite, depth int) StepFacts {
	f := StepFacts{CalleeKey: c.CalleeKey, Text: c.Text, Receiver: c.Receiver, ResultUse: c.ResultUse}
	if c.To != "" {
		f.Project = true
		f.Helper = len(j.g.impls[c.To]) == 0 && j.isHelper(c.To, depth)
		return f
	}
	if effs := j.g.effects[effectKey(from, c.Line)]; len(effs) > 0 {
		e := effs[0]
		f.Effect = &e
	}
	f.WrapsProjectCall = j.wraps(from, c, depth)
	return f
}

// wraps reports whether a closure passed to c holds a project call that matters.
func (j *stepJudge) wraps(from string, c CallSite, depth int) bool {
	at := fmt.Sprintf("%d:%d", c.Line, c.Col)
	for _, o := range j.g.sites[from] {
		if o.To == "" || o.Role == RoleOverride {
			continue
		}
		for _, fr := range o.Frames {
			if fr.Kind == "closure" && fr.ID == at && (len(j.g.impls[o.To]) > 0 || !j.isHelper(o.To, depth)) {
				return true
			}
		}
	}
	return false
}

// isHelper: loaded, not overridden, writes no field, and none of its own
// steps is visible. Anything not known for sure is not a helper — hiding a
// step that matters is worse than showing one that does not.
func (j *stepJudge) isHelper(id string, depth int) bool {
	switch j.helper[id] {
	case 1:
		return true
	case 2, 3:
		return false
	}
	if depth >= helperDepth || !j.g.loaded[id] || j.g.writers[id] || len(j.g.impls[id]) > 0 {
		return false
	}
	j.helper[id] = 3
	helper := true
	for _, c := range j.g.sites[id] {
		if c.Role == RoleOverride {
			continue
		}
		if visible, _ := Visibility(j.facts(id, c, depth+1), j.stats, j.overrides); visible {
			helper = false
			break
		}
	}
	if helper {
		j.helper[id] = 1
	} else {
		j.helper[id] = 2
	}
	return helper
}
