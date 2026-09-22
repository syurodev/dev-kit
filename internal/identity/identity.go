// Package identity names who is calling DevKit. It is a leaf: it must not
// import any devkit/internal package, because capabilities, interaction,
// agenthub and mcp all depend on it.
package identity

import "context"

// Kind is the class of caller.
type Kind string

const (
	// KindAgent is a vendor agent spawned by Agent Hub; SessionID is its hub session.
	KindAgent Kind = "agent"
	// KindUserTerminal is a CLI the user ran themselves, connected through the shared mcp.sock.
	KindUserTerminal Kind = "user-terminal"
	// KindUI is the desktop frontend calling through the bridge.
	KindUI Kind = "ui"
)

// Unknown is the audit label for a context that carries no Caller.
const Unknown = "unknown"

// Caller identifies the origin of a request.
type Caller struct {
	Kind      Kind
	SessionID string // hub session UUID; set only for KindAgent
}

// Agent returns the caller for an Agent Hub session.
func Agent(sessionID string) Caller { return Caller{Kind: KindAgent, SessionID: sessionID} }

// UserTerminal returns the caller for the user's own terminal CLIs.
func UserTerminal() Caller { return Caller{Kind: KindUserTerminal} }

// UI returns the caller for the desktop frontend.
func UI() Caller { return Caller{Kind: KindUI} }

// IsAgent reports whether c is a hub agent (Kind agent with a session id).
func (c Caller) IsAgent() bool { return c.Kind == KindAgent && c.SessionID != "" }

// String is the stable audit / allow_always label: "agent:<id>", "user-terminal", "ui".
func (c Caller) String() string {
	switch c.Kind {
	case KindAgent:
		return "agent:" + c.SessionID
	case "":
		return Unknown
	default:
		return string(c.Kind)
	}
}

type ctxKey struct{}

// WithCaller returns ctx carrying c.
func WithCaller(ctx context.Context, c Caller) context.Context {
	return context.WithValue(ctx, ctxKey{}, c)
}

// FromContext returns the caller stamped on ctx, if any.
func FromContext(ctx context.Context) (Caller, bool) {
	c, ok := ctx.Value(ctxKey{}).(Caller)
	return c, ok && c.Kind != ""
}

// Label returns the caller's String(), or Unknown when ctx carries none.
func Label(ctx context.Context) string {
	if c, ok := FromContext(ctx); ok {
		return c.String()
	}
	return Unknown
}
