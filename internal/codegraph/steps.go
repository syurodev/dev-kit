package codegraph

import "errors"

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

// CallStats is how often each callee is called across the index.
type CallStats struct {
	Callers map[string]int // callee key → distinct calling methods
	Methods int            // method nodes in the index
}
