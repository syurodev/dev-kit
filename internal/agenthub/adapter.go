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
	// GateOwnedMCP is supplied by the trusted identity-bound endpoint registry.
	// These tools ask through DevKit's gate after vendor dispatch, exactly once.
	GateOwnedMCP map[string][]string
	Cwd          string
	WorkspaceID  string
	// AllowUnixSockets is the session's identity-bound MCP socket (see sandbox.Policy).
	AllowUnixSockets []string
	// McpServers is advertised to the agent via ACP session/new.mcpServers.
	McpServers []acp.McpServerStdio
	// ReadOnlyPaths are extra RO binds — the devkit-mcp proxy binary.
	ReadOnlyPaths []string
	// ExtraReadWritePaths widen the jail beyond Cwd — a workspace's other
	// services, picked deliberately in the draft's project picker because a
	// workspace's services do not necessarily share a root Cwd could cover
	// (docs/dev-kit/workspace.mdx: "một service không cần nằm dưới root").
	// Set only from CreateInput on the bridge (UI) path — never from the MCP
	// tool handler's own input struct, which has no such field — so an agent
	// can never grant a session it spawns a wider jail than its own.
	ExtraReadWritePaths []string
	// ExtraEnv is merged into the vendor's sandbox environment, winning over
	// the base allowlist and the vendor's own launch env. applySandboxExtras
	// puts DOCKER_HOST here, and PATH when the docker CLI lives somewhere an
	// app opened from the Dock does not look.
	ExtraEnv map[string]string
	// DenyReadPaths are hidden from the session on top of sandbox.Policy's
	// own deny list — DevKit's data directory, once $HOME is shared. A socket
	// in AllowUnixSockets may still sit inside one (the session's own MCP
	// socket does).
	DenyReadPaths []string
	// PrivateHome overrides sandbox.Policy.PrivateHome when non-nil. Nil keeps
	// the adapter's own default (private home on). applySandboxExtras sets it
	// to false when preferences.AgentSandboxSettings.ShareHome is on — its
	// default — and the jail then binds the real $HOME, with sandbox.Policy's
	// deny list (~/.ssh, ~/.gnupg, ~/.aws, ~/.config/gcloud) and DenyReadPaths
	// still hidden on top.
	PrivateHome *bool
	// Brief says where the session is — its workspace, its folders and which
	// of them it may write — for the prompt (devkitPrompt). Service.SessionBrief
	// fills it; empty, the prompt tells the agent to look instead.
	Brief string
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
	//
	// meta is the request's `_meta`, or nil. It carries the vendor extensions a
	// single turn opts into — today the file-change report a checkpoint asks
	// the agent to tag with its id — which is per-prompt state, not per-session,
	// so it belongs in the call rather than in the runtime.
	Prompt(ctx context.Context, prompt []acp.ContentBlock, meta json.RawMessage) (PromptOutcome, error)
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

// VendorConfigCatalog reads choices before there is a conversation or turn.
type VendorConfigCatalog interface {
	ConfigCatalog(context.Context, OpenSpec) ([]ConfigOption, error)
}

// SessionSteerer is implemented by runtimes whose agent advertises the
// steering and goal extensions. Optional for the same reason
// RuntimeConfigurator is: whether an agent takes a message mid-turn, or keeps
// an objective, is a fact about the vendor, and an interface every adapter had
// to satisfy would force the ones that cannot into stubs that lie.
type SessionSteerer interface {
	// Steer injects a message into the running turn. The outcome says what
	// happened to it: "injected", "startedNewTurn", or "promptRequired" — the
	// session was idle and the message is still the caller's to send.
	Steer(ctx context.Context, prompt []acp.ContentBlock) (string, error)
	// SetGoal sets the session's objective, or clears it when empty.
	SetGoal(ctx context.Context, objective string) error
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

// RuntimeForker is implemented by adapters whose vendor can branch a session at
// one of its messages.
//
// Optional like SessionLoader, and for the same reason: whether a conversation
// can be rewound at all is a fact about the vendor. A hub whose adapter cannot
// fork still restores files — the two halves of a rewind are independent, which
// is why the user can ask for either on its own.
type RuntimeForker interface {
	// Fork makes a new vendor session holding srcACPSessionID's history up to
	// and including messageID, and returns a runtime attached to it. The source
	// session is left alone.
	//
	// Like Load, the vendor replays the forked history as updates during the
	// call and the runtime's handlers are still nil, so nothing is re-recorded.
	Fork(ctx context.Context, spec OpenSpec, srcACPSessionID, messageID string) (SessionRuntime, error)
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

// VendorSessionDeleter is implemented by runtimes that can ask their vendor to
// forget a session other than their own.
//
// A rewind needs exactly this: the conversation it left behind belongs to a
// process that has already been closed, so the only connection left to ask on
// is the forked session's. Measured (spike S6): claude-agent-acp deletes a
// session the asking process never loaded.
type VendorSessionDeleter interface {
	DeleteSession(ctx context.Context, acpSessionID string) error
}

// RuntimeModes is implemented by runtimes whose vendor answered session/new or
// session/load with a mode set.
//
// Optional for the same reason SessionLoader is: whether an agent has modes at
// all is a fact about the vendor. The answer arrives before the service has
// attached any handler, so the runtime keeps it and the service asks.
type RuntimeModes interface {
	// Modes returns the mode set from the handshake, or nil for none.
	Modes() *acp.SessionModeState
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

// RuntimeHandlers wires the callbacks a runtime calls when the agent sends
// something to the client.
type RuntimeHandlers struct {
	OnUpdate     func(update json.RawMessage, meta json.RawMessage)
	OnPermission func(ctx context.Context, params json.RawMessage) (json.RawMessage, error)
	// OnElicit answers a question the agent puts to the person (elicitation/create).
	// Unlike OnPermission it is installed outside a turn as well: nothing is
	// authorised by answering, so "nobody to ask" is a worse answer than asking.
	// ctx ends when the agent takes the question back.
	OnElicit func(ctx context.Context, params json.RawMessage) (json.RawMessage, error)
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
