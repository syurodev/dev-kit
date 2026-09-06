package agenthub

import (
	"context"
	"encoding/json"
	"sort"
	"sync"

	"devkit/internal/acp"
)

// OpenSpec is everything an adapter needs to spawn one session.
type OpenSpec struct {
	Cwd string
	// AllowUnixSockets is the session's identity-bound MCP socket (see sandbox.Policy).
	AllowUnixSockets []string
	// McpServers is advertised to the agent via ACP session/new.mcpServers.
	McpServers []acp.McpServerStdio
	// ReadOnlyPaths are extra RO binds — the devkit-mcp proxy binary.
	ReadOnlyPaths []string
}

// VendorAdapter wraps a concrete agent vendor (mock, claude, …).
// Only adapters parse vendor-specific _meta shapes.
type VendorAdapter interface {
	Type() AgentType
	Open(ctx context.Context, spec OpenSpec) (SessionRuntime, error)
	NormalizeUsage(meta json.RawMessage) (NormalizedUsage, error)
	NormalizeToolSummary(meta json.RawMessage, update json.RawMessage) (filesChanged []string, summary string, err error)
	Extensions() []string
}

// SessionRuntime is a live vendor session (in-process or subprocess).
type SessionRuntime interface {
	ACPSessionID() string
	Prompt(ctx context.Context, text string) (PromptOutcome, error)
	Cancel(ctx context.Context) error
	Close(ctx context.Context) error
	SetHandlers(h RuntimeHandlers)
}

// SessionLoader is implemented by adapters whose vendor can resume a session
// that outlived the process that created it.
//
// Optional on purpose: the agent advertises loadSession in its initialize
// capabilities, so whether it can be done is a fact about the vendor, and an
// interface every adapter must satisfy would force the ones that cannot into
// faking it. Service checks the assertion and falls back to ErrUnavailable.
type SessionLoader interface {
	// Load reopens acpSessionID in a new process. The vendor replays the
	// session's history as updates during the call; the caller must not have
	// installed handlers yet, or a resume would re-record a transcript the
	// store already has.
	Load(ctx context.Context, spec OpenSpec, acpSessionID string) (SessionRuntime, error)
}

// RuntimeHandlers wires permission + update callbacks before Prompt.// RuntimeHandlers wires permission + update callbacks before Prompt.
type RuntimeHandlers struct {
	OnUpdate     func(update json.RawMessage, meta json.RawMessage)
	OnPermission func(ctx context.Context, params json.RawMessage) (json.RawMessage, error)
}

// PromptOutcome is the result of a single Prompt turn.
type PromptOutcome struct {
	StopReason string
	Meta       json.RawMessage
}

// Registry maps AgentType → VendorAdapter.
type Registry struct {
	mu       sync.RWMutex
	adapters map[AgentType]VendorAdapter
}

// NewRegistry returns an empty adapter registry.
func NewRegistry() *Registry {
	return &Registry{adapters: make(map[AgentType]VendorAdapter)}
}

// Register adds or replaces an adapter by Type().
func (r *Registry) Register(a VendorAdapter) {
	if a == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[a.Type()] = a
}

// Get looks up an adapter by type.
func (r *Registry) Get(t AgentType) (VendorAdapter, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[t]
	return a, ok
}

// Types returns registered vendor types in stable sorted order.
func (r *Registry) Types() []AgentType {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]AgentType, 0, len(r.adapters))
	for t := range r.adapters {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
