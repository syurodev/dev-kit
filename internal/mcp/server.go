package mcp

import (
	"context"

	"devkit/internal/capabilities"
	"devkit/internal/database"
	"devkit/internal/environments"
	"devkit/internal/interaction"
	"devkit/internal/kafka"
	"devkit/internal/notes"
	sshservice "devkit/internal/ssh"
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
	Create(title, body, folderID string, tags []string) (notes.Summary, error)
	Update(id, title, body string) (notes.Note, error)
	Delete(id string) error
	SetOrganization(id, folderID string, tags []string) (notes.Summary, error)
	ListFolders() ([]notes.Folder, error)
	CreateFolder(name, color string, tags []string) (notes.Folder, error)
	UpdateFolder(id, name, color string, tags []string) (notes.Folder, error)
	DeleteFolder(id string) error
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

// EnvAPI lists environment tags and reports MCP confirmation policy.
type EnvAPI interface {
	ListTags() ([]environments.Tag, error)
	RequiresConfirmation(name string) bool
}

// SSHAPI is the SSH domain surface MCP tools invoke.
type SSHAPI interface {
	ListProfiles() ([]sshservice.ProfileSummary, error)
	TestConnection(ctx context.Context, id, passphrase string) error
	AgentActiveProfileID() string
	SetAgentActiveProfile(id string) error
	RunCommand(ctx context.Context, id, command, passphrase string) (sshservice.CommandResult, error)
}

// DatabaseAPI is the database domain surface MCP tools invoke.
type DatabaseAPI interface {
	ListProfiles() ([]database.ProfileSummary, error)
	TestConnection(ctx context.Context, id string) error
	AgentActiveProfileID() string
	SetAgentActiveProfile(id string) error
	Query(ctx context.Context, id, sql string) (database.QueryResult, error)
}

// KafkaAPI is the Kafka domain surface MCP tools invoke.
type KafkaAPI interface {
	ListProfiles() ([]kafka.ProfileSummary, error)
	Status() kafka.ConnectionStatus
	Connect(ctx context.Context, id string) error
	ListTopics(ctx context.Context) ([]kafka.TopicInfo, error)
	DescribeTopic(ctx context.Context, topic string) (kafka.TopicDetail, error)
	PeekMessages(ctx context.Context, topic string, partition int32, limit int, offset int64) ([]kafka.Message, error)
	ListConsumerGroups(ctx context.Context) ([]kafka.ConsumerGroupInfo, error)
	PublishMessage(ctx context.Context, req kafka.PublishRequest) (kafka.PublishResult, error)
}

// VaultStatusResult is the structured output of vault.status.
type VaultStatusResult struct {
	State string `json:"state"`
}

// ActiveProfileResult is the sticky/live active profile for SSH, DB, or Kafka tools.
type ActiveProfileResult struct {
	ProfileID string `json:"profileId,omitempty"`
}

// Deps wires MCP tool handlers. Production composition is Task 8 / main.
type Deps struct {
	Caller   Caller
	Notes    NotesAPI
	Vault    VaultAPI
	Search   SearchAPI
	Tools    ToolsAPI
	Envs     EnvAPI
	SSH      SSHAPI
	DB       DatabaseAPI
	Kafka    KafkaAPI
	AgentHub AgentHubAPI      // optional nil until wired from main
	Hub      *interaction.Hub // optional nil → headless
}

// Server hosts the MCP tool registry for DevKit.
type Server struct {
	deps Deps
	mcp  *sdk.Server
}

// NewServer builds an MCP server and registers vault/notes/tools/env/ssh/db/kafka/agenthub tools.
func NewServer(d Deps) *Server {
	s := &Server{deps: d}
	ms := sdk.NewServer(&sdk.Implementation{Name: "devkit", Version: "0.1.0"}, nil)
	s.mcp = ms
	s.registerVault()
	s.registerNotes()
	s.registerTools()
	s.registerEnv()
	s.registerSSH()
	s.registerDatabase()
	s.registerKafka()
	s.registerAgentHub()
	return s
}

// MCP returns the underlying SDK server (for ServeConn / proxy wiring).
func (s *Server) MCP() *sdk.Server { return s.mcp }

func boolPtr(v bool) *bool { return &v }

func (s *Server) sshTarget() *sshTarget {
	return &sshTarget{api: s.deps.SSH, hub: s.deps.Hub}
}

func (s *Server) databaseTarget() *databaseTarget {
	return &databaseTarget{api: s.deps.DB, hub: s.deps.Hub}
}

func (s *Server) kafkaTarget() *kafkaTarget {
	return &kafkaTarget{api: s.deps.Kafka, hub: s.deps.Hub}
}
