package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Embedding a nil interface satisfies it without a body: enough for tools to
// register, never called by the tests that use these.
type (
	stubNotes       struct{ NotesAPI }
	stubVault       struct{ VaultAPI }
	stubSearch      struct{ SearchAPI }
	stubToolsAPI    struct{ ToolsAPI }
	stubEnv         struct{ EnvAPI }
	stubSSH         struct{ SSHAPI }
	stubDB          struct{ DatabaseAPI }
	stubKafka       struct{ KafkaAPI }
	stubCodeGraph   struct{ CodeGraphAPI }
	stubCGServices  struct{ CodeGraphServices }
	stubTasks       struct{ TasksAPI }
	stubGit         struct{ GitAPI }
	stubGitRepos    struct{ GitRepos }
	stubPRs         struct{ PullRequestsAPI }
	stubAccounts    struct{ AccountsAPI }
	stubAPI         struct{ APIClientAPI }
	stubCodexHook   struct{ CodexHookAPI }
	stubBacklog     struct{ PlanBacklog }
	stubBranches    struct{ TaskBranches }
	stubAgentHub    struct{ AgentHubAPI }
	stubWorkspace   struct{ WorkspaceAPI }
	stubMemory      struct{ WorkspaceMemoryAPI }
	stubPDFDocument struct{ PDFDocumentsAPI }
)

// fullDeps wires every optional surface, so every tool the product can offer
// is registered.
func fullDeps() Deps {
	return Deps{
		Caller:            &fakeCaller{},
		Notes:             stubNotes{},
		Vault:             stubVault{},
		Search:            stubSearch{},
		Tools:             stubToolsAPI{},
		Envs:              stubEnv{},
		SSH:               stubSSH{},
		DB:                stubDB{},
		Kafka:             stubKafka{},
		CodeGraph:         stubCodeGraph{},
		CodeGraphServices: stubCGServices{},
		Tasks:             stubTasks{},
		Git:               stubGit{},
		GitRepos:          stubGitRepos{},
		PullRequests:      stubPRs{},
		Accounts:          stubAccounts{},
		API:               stubAPI{},
		CodexHook:         stubCodexHook{},
		PlanBacklog:       stubBacklog{},
		TaskBranches:      stubBranches{},
		AgentHub:          stubAgentHub{},
		Workspace:         stubWorkspace{},
		WorkspaceMemory:   stubMemory{},
		PDFDocuments:      stubPDFDocument{},
	}
}

// listAllTools returns the tool list a client sees from a fully wired server.
func listAllTools(t *testing.T) []*sdk.Tool {
	t.Helper()
	srv := NewServer(fullDeps())
	ctx := context.Background()
	st, ct := sdk.NewInMemoryTransports()
	ss, err := srv.MCP().Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "alltools", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()
	var out []*sdk.Tool
	for tool, err := range cs.Tools(ctx, nil) {
		if err != nil {
			t.Fatalf("list tools: %v", err)
		}
		out = append(out, tool)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func TestDumpToolSizes(t *testing.T) {
	path := os.Getenv("DEVKIT_DUMP_TOOL_SIZES")
	if path == "" {
		t.Skip("set DEVKIT_DUMP_TOOL_SIZES=<file> to write the table")
	}
	var b []byte
	for _, tool := range listAllTools(t) {
		in, _ := jsonSize(tool.InputSchema)
		out, _ := jsonSize(tool.OutputSchema)
		b = append(b, []byte(fmt.Sprintf("%s\tdesc=%d\tin=%d\tout=%d\n", tool.Name, len(tool.Description), in, out))...)
	}
	_ = os.WriteFile(path, b, 0o644)
}

func jsonSize(v any) (int, error) {
	raw, err := json.Marshal(v)
	return len(raw), err
}

func TestDumpDescriptions(t *testing.T) {
	path := os.Getenv("DEVKIT_DUMP_DESCRIPTIONS")
	if path == "" {
		t.Skip()
	}
	var b []byte
	for _, tool := range listAllTools(t) {
		b = append(b, []byte(fmt.Sprintf("%d\t%s\t%s\n", len(tool.Description), tool.Name, tool.Description))...)
	}
	_ = os.WriteFile(path, b, 0o644)
}
