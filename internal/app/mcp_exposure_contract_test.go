package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"devkit/internal/audit"
	"devkit/internal/capabilities"
	"devkit/internal/database"
	"devkit/internal/identity"
	"devkit/internal/interaction"
	"devkit/internal/kafka"
	"devkit/internal/mcp"
	"devkit/internal/permissions"
	sshservice "devkit/internal/ssh"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func exposureDeps(caller mcp.Caller) mcp.Deps {
	prompt := &autoPrompt{}
	hub := interaction.NewHub(prompt)
	prompt.hub = hub
	return mcp.Deps{
		Caller: caller,
		Notes:  stubNotes{}, Vault: stubVault{}, Search: stubSearch{}, Tools: stubTools{},
		Envs: stubEnvs{}, SSH: stubSSH{}, DB: stubDB{}, Kafka: stubKafka{}, Hub: hub,
		CodeGraph: stubCodeGraph{}, CodeGraphServices: stubCodeGraphServices{}, Tasks: stubTasks{},
		Git: stubGit{}, GitRepos: stubGitRepos{}, PullRequests: stubPullRequests{},
		Accounts: stubAccounts{}, API: stubAPI{}, CodexHook: stubCodexHook{},
		PlanBacklog: stubPlanBacklog{}, TaskBranches: stubTaskBranches{}, AgentHub: stubAgentHub{},
		Workspace: stubWorkspace{}, WorkspaceWrite: stubWorkspaceWrite{}, WorkspaceMemory: stubWorkspaceMemory{}, PDFDocuments: stubPDFDocuments{},
	}
}

// Embedding each interface avoids implementing domain methods: a successful
// grant check stops before the handler, so these methods are never reached.
type (
	stubNotes             struct{ mcp.NotesAPI }
	stubVault             struct{ mcp.VaultAPI }
	stubSearch            struct{ mcp.SearchAPI }
	stubTools             struct{ mcp.ToolsAPI }
	stubEnvs              struct{ mcp.EnvAPI }
	stubSSH               struct{ mcp.SSHAPI }
	stubDB                struct{ mcp.DatabaseAPI }
	stubKafka             struct{ mcp.KafkaAPI }
	stubCodeGraph         struct{ mcp.CodeGraphAPI }
	stubCodeGraphServices struct{ mcp.CodeGraphServices }
	stubTasks             struct{ mcp.TasksAPI }
	stubGit               struct{ mcp.GitAPI }
	stubGitRepos          struct{ mcp.GitRepos }
	stubPullRequests      struct{ mcp.PullRequestsAPI }
	stubAccounts          struct{ mcp.AccountsAPI }
	stubAPI               struct{ mcp.APIClientAPI }
	stubCodexHook         struct{ mcp.CodexHookAPI }
	stubPlanBacklog       struct{ mcp.PlanBacklog }
	stubTaskBranches      struct{ mcp.TaskBranches }
	stubAgentHub          struct{ mcp.AgentHubAPI }
	stubWorkspace         struct{ mcp.WorkspaceAPI }
	stubWorkspaceWrite    struct{ mcp.WorkspaceWriteAPI }
	stubWorkspaceMemory   struct{ mcp.WorkspaceMemoryAPI }
	stubPDFDocuments      struct{ mcp.PDFDocumentsAPI }
)

type autoPrompt struct{ hub *interaction.Hub }

func (p *autoPrompt) Prompt(pending interaction.Pending) error {
	go func() { _ = p.hub.Complete(pending.ID, "allow") }()
	return nil
}

const probeProfileID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

func (stubDB) AgentActiveProfileID() string { return probeProfileID }
func (stubDB) ListProfiles() ([]database.ProfileSummary, error) {
	return []database.ProfileSummary{{ID: probeProfileID, Env: "development"}}, nil
}
func (stubDB) SetAgentActiveProfile(string) error { return nil }
func (stubSSH) AgentActiveProfileID() string      { return probeProfileID }
func (stubSSH) ListProfiles() ([]sshservice.ProfileSummary, error) {
	return []sshservice.ProfileSummary{{ID: probeProfileID, Env: "development"}}, nil
}
func (stubSSH) SetAgentActiveProfile(string) error { return nil }
func (stubKafka) AgentActiveProfileID() string     { return probeProfileID }
func (stubKafka) Status() kafka.ConnectionStatus {
	return kafka.ConnectionStatus{Connected: true, ProfileID: probeProfileID}
}
func (stubKafka) ListProfiles() ([]kafka.ProfileSummary, error) {
	return []kafka.ProfileSummary{{ID: probeProfileID, Env: "development"}}, nil
}
func (stubKafka) SetAgentActiveProfile(string) error { return nil }
func (stubEnvs) RequiresConfirmation(string) bool    { return false }

type policyProbe struct {
	policy *permissions.Policy
	checks []probeCheck
}

type probeCheck struct {
	name    string
	request permissions.Request
	allowed bool
}

var errStopBeforeHandler = errors.New("grant probe stopped before handler")

func (p *policyProbe) record(name, subject, capability, scope string) error {
	decision := p.policy.Check(permissions.Request{SubjectID: subject, Capability: capability, Scope: scope})
	p.checks = append(p.checks, probeCheck{
		name:    name,
		request: permissions.Request{SubjectID: subject, Capability: capability, Scope: scope},
		allowed: decision.Allowed,
	})
	return errStopBeforeHandler
}

func (p *policyProbe) Call(ctx context.Context, subject, capability, scope string, _ map[string]any, _ capabilities.Handler) (any, error) {
	return nil, p.record("legacy MCP tool", subject, capability, scope)
}

func (p *policyProbe) CallOperation(_ context.Context, request capabilities.CallRequest) (any, error) {
	decision := p.policy.Check(permissions.Request{SubjectID: request.SubjectID, Capability: request.Capability, Scope: request.Scope})
	p.checks = append(p.checks, probeCheck{
		name:    request.Operation,
		request: permissions.Request{SubjectID: request.SubjectID, Capability: request.Capability, Scope: request.Scope},
		allowed: decision.Allowed,
	})
	request.Handler = func(context.Context) (any, error) {
		return nil, errStopBeforeHandler
	}
	ctx := identity.WithCaller(context.Background(), identity.UserTerminal())
	_, err := capabilities.NewGateway(p.policy, audit.NewMemorySink()).Call(ctx, request)
	return nil, err
}

func (p *policyProbe) PublishError(_ context.Context, _ string, err error) error { return err }

func TestEveryRegisteredMCPToolReachesAnAllowedDefaultGrant(t *testing.T) {
	container := NewContainer()
	probe := &policyProbe{policy: container.Permissions}
	server := mcp.NewServer(exposureDeps(probe))
	ctx := context.Background()
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	serverSession, err := server.MCP().Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	clientSession, err := sdk.NewClient(&sdk.Implementation{Name: "exposure-contract", Version: "0"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	for tool, err := range clientSession.Tools(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		before := len(probe.checks)
		args := sampleToolArguments(t, tool.Name, tool.InputSchema)
		result, callErr := clientSession.CallTool(ctx, &sdk.CallToolParams{Name: tool.Name, Arguments: args})
		if len(probe.checks) == before {
			detail := ""
			if result != nil && len(result.Content) > 0 {
				if content, ok := result.Content[0].(*sdk.TextContent); ok {
					detail = content.Text
				}
			}
			t.Errorf("%s did not reach the capability gateway (err=%v result=%s)", tool.Name, callErr, detail)
			continue
		}
		for _, check := range probe.checks[before:] {
			if !check.allowed {
				t.Errorf("%s has no default grant for %s / %s on %s", tool.Name, check.request.SubjectID, check.request.Capability, check.request.Scope)
			}
		}
	}
}

func sampleToolArguments(t *testing.T, toolName string, schema any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	var shape map[string]any
	if err := json.Unmarshal(raw, &shape); err != nil {
		t.Fatal(err)
	}
	value, ok := sampleSchemaValue(toolName, "", shape).(map[string]any)
	if !ok {
		t.Fatalf("tool input schema is not an object: %s", raw)
	}
	return value
}

func sampleSchemaValue(toolName, name string, schema map[string]any) any {
	switch name {
	case "event":
		return "SessionStart"
	case "pages":
		return "1"
	}
	if enum, ok := schema["enum"].([]any); ok && len(enum) > 0 {
		return enum[0]
	}
	typ, _ := schema["type"].(string)
	if typ == "" {
		if options, ok := schema["type"].([]any); ok {
			for _, option := range options {
				if option, ok := option.(string); ok && option != "null" {
					typ = option
					break
				}
			}
			if typ == "" {
				return nil
			}
		}
	}
	switch typ {
	case "object":
		out := map[string]any{}
		properties, _ := schema["properties"].(map[string]any)
		for name, raw := range properties {
			if name == "taskId" && toolName == "agenthub.createAgentSession" {
				continue // the shared MCP server is a user-terminal caller
			}
			child, _ := raw.(map[string]any)
			out[name] = sampleSchemaValue(toolName, name, child)
		}
		return out
	case "array":
		item, _ := schema["items"].(map[string]any)
		return []any{sampleSchemaValue(toolName, "", item)}
	case "string":
		if strings.Contains(strings.ToLower(name), "path") || name == "project" {
			return os.TempDir()
		}
		if name == "pageRange" {
			return "1"
		}
		return "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	case "integer", "number":
		return 1
	case "boolean":
		return false
	default:
		return "sample"
	}
}
