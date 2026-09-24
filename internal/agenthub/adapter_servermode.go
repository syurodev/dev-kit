package agenthub

import (
	"context"
	"encoding/json"
)

// ServerModeAdapter stands in for a real vendor in server mode.
//
// Server mode serves every bridge method over loopback HTTP with no
// authentication (README "Server mode security"), and Agent Hub's jail
// shares the host loopback. A session there could POST
// AgentHubResolvePermission for its own tool calls, or InteractionComplete
// for its own prod confirmation. Refusing to start the vendor removes the
// agent; keeping its AgentType registered keeps the answer specific —
// "not in server mode" instead of "unknown vendor".
type ServerModeAdapter struct {
	t AgentType
}

var (
	_ SessionLoader = (*ServerModeAdapter)(nil)
	_ AuthProvider  = (*ServerModeAdapter)(nil)
	_ RuntimeForker = (*ServerModeAdapter)(nil)
)

// NewServerModeAdapter returns a stub for t whose sessions never start.
func NewServerModeAdapter(t AgentType) *ServerModeAdapter { return &ServerModeAdapter{t: t} }

// ForServerMode replaces each adapter with a stub of the same type.
func ForServerMode(adapters ...VendorAdapter) []VendorAdapter {
	out := make([]VendorAdapter, 0, len(adapters))
	for _, a := range adapters {
		out = append(out, NewServerModeAdapter(a.Type()))
	}
	return out
}

func (a *ServerModeAdapter) Type() AgentType { return a.t }

func (a *ServerModeAdapter) Open(context.Context, OpenSpec) (SessionRuntime, error) {
	return nil, ErrUnavailableInServerMode
}

func (a *ServerModeAdapter) Load(context.Context, OpenSpec, string) (SessionRuntime, error) {
	return nil, ErrUnavailableInServerMode
}

func (a *ServerModeAdapter) NormalizeUsage(meta json.RawMessage) (NormalizedUsage, error) {
	return NormalizedUsage{RawMeta: append(json.RawMessage(nil), meta...)}, nil
}

func (a *ServerModeAdapter) NormalizeToolSummary(json.RawMessage, json.RawMessage) ([]string, string, error) {
	return nil, "", nil
}

func (a *ServerModeAdapter) Extensions() []string { return nil }

// AuthMethods, SignInCommand and Fork refuse for the same reason Open does:
// otherwise server mode would answer "no sign-in offered" or "cannot
// rewind", which sends the user looking in the wrong place.
func (a *ServerModeAdapter) AuthMethods(context.Context) ([]AuthMethod, error) {
	return nil, ErrUnavailableInServerMode
}

func (a *ServerModeAdapter) SignInCommand(context.Context, string) (SignInCommand, error) {
	return SignInCommand{}, ErrUnavailableInServerMode
}

func (a *ServerModeAdapter) Fork(context.Context, OpenSpec, string, string) (SessionRuntime, error) {
	return nil, ErrUnavailableInServerMode
}
