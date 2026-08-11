package mcp

import (
	"context"

	"devkit/internal/capabilities"
	"devkit/internal/environments"
	"devkit/internal/notes"
	"devkit/internal/tools"
	"devkit/internal/vault"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Caller is the narrow capability-gateway surface MCP tools use.
// Same shape as bridge.Caller; defined locally so mcp never imports bridge.
type Caller interface {
	Call(
		ctx context.Context,
		subject, capability, scope string,
		metadata map[string]any,
		handler capabilities.Handler,
	) (any, error)
}

// NotesAPI is the notes domain surface MCP tools invoke (duck-typed).
type NotesAPI interface {
	List() ([]notes.Summary, error)
	Search(query string) ([]notes.Summary, error)
	Get(id string) (notes.Note, error)
	Create(title, body string) (notes.Summary, error)
	Delete(id string) error
}

// VaultAPI reports vault lock/setup state.
type VaultAPI interface {
	Status() (vault.State, error)
}

// SearchHit is one cross-type vault search result (metadata only; no secrets).
type SearchHit struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Label     string `json:"label"`
	UpdatedAt string `json:"updated_at"`
}

// SearchAPI is cross-type plaintext metadata search (works while locked).
type SearchAPI interface {
	Search(query string) ([]SearchHit, error)
}

// ToolsAPI is the local tools surface (hash today).
type ToolsAPI interface {
	Hash(request tools.HashRequest) (tools.HashResult, error)
}

// EnvAPI lists environment tags for MCP.
type EnvAPI interface {
	ListTags() ([]environments.Tag, error)
}

// VaultStatusResult is the structured output of vault.status.
type VaultStatusResult struct {
	State string `json:"state"`
}

// Deps wires MCP tool handlers. Production composition is Task 8 / main.
type Deps struct {
	Caller Caller
	Notes  NotesAPI
	Vault  VaultAPI
	Search SearchAPI
	Tools  ToolsAPI
	Envs   EnvAPI
	// later: SSH, DB, Kafka, Hub
}

// Server hosts the MCP tool registry for DevKit.
type Server struct {
	deps Deps
	mcp  *sdk.Server
}

// NewServer builds an MCP server and registers vault/notes/tools/env tools.
func NewServer(d Deps) *Server {
	s := &Server{deps: d}
	ms := sdk.NewServer(&sdk.Implementation{Name: "devkit", Version: "0.1.0"}, nil)
	s.mcp = ms
	s.registerVault()
	s.registerNotes()
	s.registerTools()
	s.registerEnv()
	return s
}

// MCP returns the underlying SDK server (for ServeConn / proxy wiring).
func (s *Server) MCP() *sdk.Server { return s.mcp }

func boolPtr(v bool) *bool { return &v }
