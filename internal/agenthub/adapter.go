package agenthub

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
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
	// Prompt sends one turn. Blocks rather than a string because a turn is not
	// always text: the agent advertises promptCapabilities.image and
	// embeddedContext, and acp.PromptRequest has modelled []ContentBlock since
	// this package was written — the string was the narrowing, not the wire.
	Prompt(ctx context.Context, prompt []acp.ContentBlock) (PromptOutcome, error)
	Cancel(ctx context.Context) error
	Close(ctx context.Context) error
	SetHandlers(h RuntimeHandlers)
}

// RuntimeConfigurator is optional because ACP agents advertise these controls
// independently. The service reports an explicit unsupported error when an
// agent exposes no corresponding method.
type RuntimeConfigurator interface {
	SetConfigOption(ctx context.Context, optionID, value string) error
	SetMode(ctx context.Context, modeID string) error
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

// RuntimeDeleter is implemented by runtimes whose vendor keeps its own copy of
// the conversation and can be told to destroy it.
//
// Optional for the same reason SessionLoader is: whether the vendor stores a
// session at all is a fact about the vendor. A runtime without this is not
// broken, it simply has nothing of its own to delete.
type RuntimeDeleter interface {
	// Delete asks the vendor to forget the session. Called before Close,
	// because after Close there is no connection to ask on.
	Delete(ctx context.Context) error
}

// RuntimeLiveness is implemented by runtimes whose process can die under them
// (subprocess-backed ones; an in-process runtime cannot).
//
// Optional for the same reason SessionLoader is: whether a runtime can crash
// out from under the session is a fact about how it is built. Service treats
// a dead runtime as absent, so the session resumes on a fresh process instead
// of prompting a corpse on every retry.
type RuntimeLiveness interface {
	// Dead reports whether the process exited unexpectedly, and why.
	Dead() (bool, error)
}

// RuntimeHandlers wires permission + update callbacks before Prompt.// RuntimeHandlers wires permission + update callbacks before Prompt.// RuntimeHandlers wires permission + update callbacks before Prompt.
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

// TextPrompt is the common case: one block, all text. Named rather than inlined
// so a caller that only has a string does not have to know the block shape.
func TextPrompt(text string) []acp.ContentBlock {
	return []acp.ContentBlock{{Type: "text", Text: text}}
}

// PromptText joins the text blocks of a prompt, ignoring the rest. For
// adapters that speak a text-only vendor protocol, and for logs.
func PromptText(prompt []acp.ContentBlock) string {
	var b strings.Builder
	for _, block := range prompt {
		if block.Type == "text" || block.Type == "" {
			b.WriteString(block.Text)
		}
	}
	return b.String()
}
