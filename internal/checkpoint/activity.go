// internal/checkpoint/activity.go
package checkpoint

import (
	"sort"
	"sync"
	"time"
)

// Who was writing, and when.
//
// The jail lets several sessions share one project_path, and a sub-session runs
// nested inside its parent's turn. So when a turn's snapshot shows a file that
// no tool call and no vendor report accounts for, the honest answer is not
// "the agent did it" — it is "something did it, and here is who else was
// running at the time". This registry is what supplies that second half, and
// what a restore checks before it offers to roll anything back.

// Kind is what a span of activity was.
type Kind string

const (
	KindTurn    Kind = "turn"
	KindRestore Kind = "restore"
	KindRewind  Kind = "rewind"
)

// Span is one stretch of time a session held a project.
type Span struct {
	Project string    `json:"project"`
	Session string    `json:"session"`
	Kind    Kind      `json:"kind"`
	Start   time.Time `json:"start"`
	// End is zero while the span is still running.
	End time.Time `json:"end,omitempty"`
}

// Running reports whether the span has not ended.
func (s Span) Running() bool { return s.End.IsZero() }

// spanHistory is how far back Overlap can see. A turn's overlap question is
// asked the moment the turn ends, so this only has to outlive the longest
// plausible turn; the cap is what stops a long-lived app from growing a list.
const (
	spanHistory  = 6 * time.Hour
	maxSpansKept = 2048
)

// Activity is the registry. Safe for concurrent use.
type Activity struct {
	mu    sync.Mutex
	now   func() time.Time
	spans []*Span
}

// NewActivity returns an empty registry.
func NewActivity() *Activity { return &Activity{now: time.Now} }

// Handle ends one span. End is idempotent, so a defer and an explicit call on
// the success path cannot double-close.
type Handle struct {
	activity *Activity
	span     *Span
}

// Begin records that a session has started writing in a project.
func (a *Activity) Begin(project, session string, kind Kind) *Handle {
	a.mu.Lock()
	defer a.mu.Unlock()
	span := &Span{Project: project, Session: session, Kind: kind, Start: a.now()}
	a.spans = append(a.spans, span)
	a.trimLocked()
	return &Handle{activity: a, span: span}
}

// End closes the span. A nil handle is a no-op so callers need not branch on a
// disabled engine.
func (h *Handle) End() {
	if h == nil || h.activity == nil {
		return
	}
	h.activity.mu.Lock()
	defer h.activity.mu.Unlock()
	if h.span.End.IsZero() {
		h.span.End = h.activity.now()
	}
}

// Span returns a copy of what the handle is tracking.
func (h *Handle) Span() Span {
	if h == nil || h.activity == nil {
		return Span{}
	}
	h.activity.mu.Lock()
	defer h.activity.mu.Unlock()
	return *h.span
}

// Overlap lists the spans in a project that were running at any point between
// start and end, excluding the sessions named.
//
// A span with no end is still running and therefore overlaps anything that has
// not finished before it started.
func (a *Activity) Overlap(project string, start, end time.Time, exclude ...string) []Span {
	skip := make(map[string]bool, len(exclude))
	for _, session := range exclude {
		skip[session] = true
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []Span
	for _, span := range a.spans {
		if span.Project != project || skip[span.Session] {
			continue
		}
		spanEnd := span.End
		if spanEnd.IsZero() {
			spanEnd = a.now()
		}
		// Half-open on both sides: two spans that merely touch at an instant
		// did not run at the same time.
		if spanEnd.After(start) && span.Start.Before(end) {
			out = append(out, *span)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out
}

// RunningIn lists the spans in a project that have not ended, excluding the
// sessions named. A restore asks this before it touches anything.
func (a *Activity) RunningIn(project string, exclude ...string) []Span {
	skip := make(map[string]bool, len(exclude))
	for _, session := range exclude {
		skip[session] = true
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []Span
	for _, span := range a.spans {
		if span.Project == project && span.Running() && !skip[span.Session] {
			out = append(out, *span)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out
}

// trimLocked drops spans that ended long ago, oldest first. A running span is
// never dropped however old it is — an agent can hold a turn open for hours,
// and forgetting it would be forgetting the one thing the caller is asking about.
func (a *Activity) trimLocked() {
	cutoff := a.now().Add(-spanHistory)
	kept := a.spans[:0]
	for _, span := range a.spans {
		if span.Running() || span.End.After(cutoff) {
			kept = append(kept, span)
		}
	}
	a.spans = kept
	if overflow := len(a.spans) - maxSpansKept; overflow > 0 {
		var running []*Span
		for _, span := range a.spans[:overflow] {
			if span.Running() {
				running = append(running, span)
			}
		}
		a.spans = append(running, a.spans[overflow:]...)
	}
}
