package mcp

import (
	"context"
	"sort"
	"sync"

	"devkit/internal/capabilities"
	"devkit/internal/codegraph"
	"devkit/internal/codexhook"
	"devkit/internal/database"
	"devkit/internal/environments"
	"devkit/internal/identity"
	"devkit/internal/interaction"
	"devkit/internal/kafka"
	"devkit/internal/ops"
	sshservice "devkit/internal/ssh"
	"devkit/internal/tools"
	"devkit/internal/usagemetrics"
	"devkit/internal/vault"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Caller is the narrow capability-gateway surface MCP tools use.
// Same shape as bridge.Caller; defined locally so mcp never imports bridge.
type Caller interface {
	ops.Caller
	Call(
		ctx context.Context,
		subject, capability, scope string,
		metadata map[string]any,
		handler capabilities.Handler,
	) (any, error)
}

// CodexHookAPI is the write side of an external Codex CLI session's
// lifecycle hooks — the only method a hook call needs.
type CodexHookAPI interface {
	Record(call codexhook.Call) error
}

// NotesAPI is the notes domain surface MCP tools invoke (duck-typed).
type NotesAPI = ops.NotesAPI

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
	Query(ctx context.Context, id, sql string, maxRows int) (database.QueryResult, error)
	ListQueryHistory(id string) ([]database.HistoryEntry, error)
	ListSavedQueries(id string) ([]database.SavedQuery, error)
	ListForeignKeys(ctx context.Context, id, schema string) ([]database.ForeignKey, error)
	GetSchemaCache(ctx context.Context, id string) (database.SchemaSnapshot, error)
	Explain(ctx context.Context, id, sql string) (database.QueryPlan, error)
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

// CodeGraphAPI is the codegraph domain surface MCP tools invoke.
type CodeGraphAPI interface {
	// OpenProjectDir opens a folder with its index under the config dir the
	// desktop app itself uses, so MCP and the UI share one index per project.
	OpenProjectDir(ctx context.Context, projectPath string) error
	ActiveDetection() codegraph.DetectedProject
	ListEntryPointsPage(ctx context.Context, q codegraph.EntryPointQuery) (codegraph.EntryPointPage, error)
	GetApiFlow(ctx context.Context, entryPointID string) (codegraph.ApiFlow, error)
	SearchSymbolsPage(ctx context.Context, q codegraph.SymbolQuery) (codegraph.SymbolPage, error)
	FindConfigUsage(ctx context.Context, q codegraph.ConfigUsageQuery) (codegraph.ConfigUsageReport, error)
	FindTableUsage(ctx context.Context, q codegraph.TableUsageQuery) (codegraph.TableUsageReport, error)
	ListFiles(ctx context.Context) ([]codegraph.FileEntry, error)
	ReadFile(ctx context.Context, relativePath string) (string, error)
	GetCodeSnippet(ctx context.Context, filePath string, line, endLine int) (codegraph.CodeSnippet, error)
	GetBlastRadius(ctx context.Context, filePath string) ([]string, error)
	WaitForIndexReady(ctx context.Context) (int, error)
	GetNeighbors(ctx context.Context, rootID string, depth int, filter codegraph.NeighborFilter) (codegraph.Subgraph, error)
	TraceDataFlow(ctx context.Context, symbolID, fieldName string) (codegraph.DataFlowTrace, error)
	ListFields(ctx context.Context, nodeID string) ([]codegraph.FieldRef, error)
	ProposeFlow(ctx context.Context, plan codegraph.Plan) (codegraph.PlanProgress, error)
	GetPlannedFlow(ctx context.Context) (codegraph.Plan, error)
	GetPlanProgress(ctx context.Context) (codegraph.PlanProgress, error)
	RelinkPlanNode(ctx context.Context, planNodeID, fqn string) error
	ReplyPlanNode(ctx context.Context, planNodeID, body, sessionID string) error
	ListPlanNodeComments(ctx context.Context, planNodeID string) ([]codegraph.PlanComment, error)
}

// CodeGraphServices reaches the workspace services besides the focused
// project — codegraph.Registry, through RegistryServices.
type CodeGraphServices interface {
	// Service is the one for servicePath, opened on first use.
	Service(ctx context.Context, servicePath string) (CodeGraphAPI, error)
	// Focus makes path the focused project, taking it from a resident
	// service already open on it.
	Focus(ctx context.Context, path string) (CodeGraphAPI, error)
	// Snapshot reads a service's routes and outbound calls without making it
	// resident: from the open service when there is one, else from its index
	// on disk.
	Snapshot(ctx context.Context, servicePath string) (codegraph.ServiceSnapshot, error)
}

// RegistryServices adapts a codegraph.Registry to CodeGraphServices.
func RegistryServices(r *codegraph.Registry) CodeGraphServices { return registryServices{r} }

type registryServices struct{ r *codegraph.Registry }

func (s registryServices) Service(ctx context.Context, servicePath string) (CodeGraphAPI, error) {
	return s.r.Open(ctx, servicePath)
}

func (s registryServices) Focus(ctx context.Context, path string) (CodeGraphAPI, error) {
	return s.r.Focus(ctx, path)
}

func (s registryServices) Snapshot(ctx context.Context, servicePath string) (codegraph.ServiceSnapshot, error) {
	return s.r.Snapshot(ctx, servicePath)
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
	BeginUsage  func() bool
	EndUsage    func()
	UsageScope  func(sessionID, endpointToken string) (usagemetrics.Scope, bool)
	RecordUsage func(usagemetrics.Observation)
	DropUsage   func()
	Caller      Caller
	Notes       NotesAPI
	Vault       VaultAPI
	Search      SearchAPI
	Tools       ToolsAPI
	Envs        EnvAPI
	SSH         SSHAPI
	DB          DatabaseAPI
	Kafka       KafkaAPI
	CodeGraph   CodeGraphAPI
	// CodeGraphServices lets codegraph.* tools name a workspace service
	// (servicePath) instead of the focused project. Optional: nil leaves
	// only the focused project, and a servicePath is refused.
	CodeGraphServices CodeGraphServices
	Tasks             TasksAPI // optional nil until wired from main
	Git               GitAPI   // optional nil until wired from main
	// GitRepos lets the git tools work in a repository other than the one
	// the app has open — an agent's own working directory, or a service it
	// names. Optional nil: every tool then acts on Git alone.
	GitRepos GitRepos
	// PullRequests backs the pr.* tools. Nil leaves them unregistered.
	PullRequests PullRequestsAPI
	Accounts     AccountsAPI  // optional nil until wired from main
	API          APIClientAPI // optional nil until wired from main
	// CodexHook records a lifecycle hook call from an external Codex CLI
	// session — a session DevKit did not spawn. Optional nil until wired
	// from main; codexsessions.Service is itself nil-safe, so a nil here
	// only means the tool answers every hook without recording anything.
	CodexHook CodexHookAPI
	// PlanBacklog joins Tasks and Code Graph. Nil leaves tasks.createFromPlan
	// unregistered. Anything assigned here must be a real value: a nil
	// pointer in this interface is not a nil interface, and the check below
	// would pass.
	PlanBacklog PlanBacklog
	// TaskBranches answers which branches an agent session may push. Nil
	// means an agent may commit but never push — a permission that outlives
	// the session because it lives on the task, not in it.
	TaskBranches TaskBranches
	AgentHub     AgentHubAPI // optional nil until wired from main
	// Workspace answers workspace.list. Optional nil: the tool then says
	// workspaces are not available.
	Workspace       WorkspaceAPI
	WorkspaceMemory WorkspaceMemoryAPI
	// PDFDocuments backs the pdf.* tools: the PDFs attached to Agent Hub
	// sessions. Nil leaves them unregistered, so an agent is never offered a
	// tool that cannot answer.
	PDFDocuments PDFDocumentsAPI
	Hub          *interaction.Hub // optional nil → headless
}

// Server hosts the MCP tool registry for DevKit.
type Server struct {
	deps      Deps
	mcp       *sdk.Server
	gateOwned sync.Map

	// inv is the registry as registered (Inventory), read once.
	invOnce sync.Once
	inv     Inventory
	invErr  error
	// pageSize is the SDK's tools/list page size; zero keeps its default. Tests
	// set it small to exercise pagination.
	pageSize int
	// extraTools registers more tools on every instance. Tests only.
	extraTools func(*sdk.Server)
}

// gateOwnedTool is an explicit registration declaration: the handler must
// confirm through a gate before any action. Vendor wrappers may delegate it.
func (s *Server) gateOwnedTool(tool *sdk.Tool) *sdk.Tool {
	s.gateOwned.Store(tool.Name, struct{}{})
	return tool
}

// PreApprovedTools are the tools an agent may run without being asked: the
// read-only pdf.* tools, which answer only for the calling session's own
// documents. Reading a document the user attached is what attaching it is for;
// a prompt per page would make the feature unusable. agenthub.listPeers is the
// other: it names the sessions of the caller's own workspace and nothing else.
// agenthub.readSession reads only the caller's own tree.
// agenthub.sendMessage is not on the list — what an agent says to another
// session is the person's to approve through the vendor's own permission flow.
func (s *Server) PreApprovedTools() []string {
	var names []string
	if s.deps.PDFDocuments != nil {
		names = append(names, "pdf.list", "pdf.info", "pdf.search", "pdf.readText", "pdf.viewPages")
	}
	if s.deps.AgentHub != nil {
		names = append(names, "agenthub.listPeers", "agenthub.readSession")
	}
	return names
}

func (s *Server) GateOwnedTools() []string {
	// Do not delegate when the agent confirmation transport is unavailable.
	if s.deps.Hub == nil || s.deps.Hub.Headless || s.deps.Hub.ConfirmAgent == nil {
		return nil
	}
	var names []string
	s.gateOwned.Range(func(key, _ any) bool { names = append(names, key.(string)); return true })
	sort.Strings(names)
	return names
}

// NewServer builds the tool registry. The returned Server's MCP() is the
// user-terminal instance served on the shared socket; SessionHost builds one
// more instance per agent socket via sdkServerFor.
func NewServer(d Deps) *Server {
	s := &Server{deps: d}
	s.mcp = s.sdkServerFor(identity.UserTerminal())
	return s
}

// sdkServerFor builds an SDK server whose every request carries caller. One
// instance per identity — the shared socket gets user-terminal, each agent
// socket its own — so there is no per-connection lookup to race against.
func (s *Server) sdkServerFor(caller identity.Caller, token ...string) *sdk.Server {
	endpointToken := ""
	if len(token) > 0 {
		endpointToken = token[0]
	}
	return s.sdkServerWith(&caller, endpointToken, nil)
}

// registerAll registers every tool this server offers on ms, in one fixed order.
// It is the only place the registry is declared. A session endpoint calls it
// again to bring back tools its policy had hidden: the registrations replace
// same-named tools with equivalent definitions and create no per-endpoint state,
// so nothing is fabricated and no handler is duplicated by hand.
func (s *Server) registerAll(ms *sdk.Server) {
	s.registerVault(ms)
	RegisterOps(ms, ops.Runtime{Deps: ops.Deps{Notes: s.deps.Notes}, Caller: s.deps.Caller, Origin: "mcp"}, ops.Notes()...)
	s.registerTools(ms)
	s.registerEnv(ms)
	s.registerSSH(ms)
	s.registerDatabase(ms)
	s.registerKafka(ms)
	s.registerAgentHub(ms)
	s.registerCodeGraph(ms)
	s.registerCodeGraphPlan(ms)
	s.registerTasks(ms)
	s.registerGit(ms)
	s.registerPullRequests(ms)
	s.registerAccounts(ms)
	s.registerAPIClient(ms)
	s.registerCodexHook(ms)
	s.registerWorkspace(ms)
	s.registerWorkspaceMemory(ms)
	s.registerPDF(ms)
	if s.extraTools != nil {
		s.extraTools(ms)
	}
}

// sdkServerWith builds one instance. A nil caller is the throwaway instance
// Inventory reads. With a serving holder the instance lists only what the
// policy serves and answers a refusal for a registered tool it hides; without
// one it is today's server, byte for byte.
func (s *Server) sdkServerWith(c *identity.Caller, endpointToken string, serving *servingHolder) *sdk.Server {
	caller := identity.UserTerminal()
	if c != nil {
		caller = *c
	}
	ms := sdk.NewServer(&sdk.Implementation{Name: "devkit", Version: "0.1.0"}, &sdk.ServerOptions{PageSize: s.pageSize, HasTools: serving != nil})
	ms.AddReceivingMiddleware(withCaller(caller))
	var observer *ResultObservation
	if caller.IsAgent() && endpointToken != "" && s.deps.RecordUsage != nil && s.deps.UsageScope != nil {
		observer = &ResultObservation{Begin: s.deps.BeginUsage, End: s.deps.EndUsage, Scope: func() (usagemetrics.Scope, bool) { return s.deps.UsageScope(caller.SessionID, endpointToken) }, Record: s.deps.RecordUsage, Drop: s.deps.DropUsage}
	}
	ms.AddReceivingMiddleware(withMeasuredResultBudget(ResultBudget, observer, serving))
	s.registerAll(ms)
	// Hide what the policy does not serve from discovery itself: the instance
	// never holds those tools, so no page, cursor or total can name them.
	if snap := serving.load(); snap != nil {
		if inv, err := s.Inventory(); err == nil {
			if hidden := hiddenTools(inv, snap.eff); len(hidden) > 0 {
				ms.RemoveTools(hidden...)
			}
		}
	}
	return ms
}

// withCaller stamps caller on the context of every incoming request.
func withCaller(c identity.Caller) sdk.Middleware {
	return func(next sdk.MethodHandler) sdk.MethodHandler {
		return func(ctx context.Context, method string, req sdk.Request) (sdk.Result, error) {
			return next(identity.WithCaller(ctx, c), method, req)
		}
	}
}

// MCP returns the underlying SDK server (for ServeConn / proxy wiring).
func (s *Server) MCP() *sdk.Server { return s.mcp }

func boolPtr(v bool) *bool { return &v }

func (s *Server) sshTarget() *sshTarget {
	return &sshTarget{api: s.deps.SSH, hub: s.deps.Hub}
}

func (s *Server) databaseTarget() *databaseTarget {
	return &databaseTarget{api: s.deps.DB, ssh: s.deps.SSH, hub: s.deps.Hub}
}

func (s *Server) kafkaTarget() *kafkaTarget {
	return &kafkaTarget{api: s.deps.Kafka, hub: s.deps.Hub}
}
