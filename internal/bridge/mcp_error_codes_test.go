package bridge

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"devkit/internal/app"
	"devkit/internal/apperrors"
	"devkit/internal/forge"
	gitservice "devkit/internal/git"
	"devkit/internal/mcp"
	"devkit/internal/prwatch"
	"devkit/internal/workspace"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const unexpectedMessage = "An unexpected error occurred."

// mcpErrorCodes are the codes internal/mcp raises as AppErrors (the files are
// scanned, not imported: a code added tomorrow is found without anyone editing
// this test). Every non-test file counts, not only tools_*.go — git_repo.go
// raised four of them.
func mcpErrorCodes(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "mcp", "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no internal/mcp sources found: %v", err)
	}
	raised := regexp.MustCompile(`(?:apperrors\.(?:New|Wrap)|pdfError)\(\s*"([^"]+)"`)
	seen := map[string]bool{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range raised.FindAllSubmatch(source, -1) {
			seen[string(match[1])] = true
		}
	}
	codes := make([]string, 0, len(seen))
	for code := range seen {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

// redactedOnPurpose are codes an MCP tool raises whose message the bridge
// deliberately does not publish. Add to it only after reading where the
// message comes from; a new pr.* or git.* code that an agent has to act on
// belongs in keptCodes instead.
var redactedOnPurpose = map[string]string{
	// The generic code. Every site is a wiring state ("Agent Hub is not
	// available.", "Git is not available for other repositories.") that an
	// agent cannot act on; the person reads the log line.
	"internal_error": "wiring states, nothing an agent can do",
}

// A code written for the agent that the bridge turns into "An unexpected
// error occurred." leaves the agent guessing: pr.unsupportedHost did, and it
// fell back to `glab api` on its own. The earlier git.fetch bug had the same
// shape (tests green, real app refused), so this reads the sources.
func TestEveryMCPErrorCodeIsPublishedOrRedactedOnPurpose(t *testing.T) {
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	codes := mcpErrorCodes(t)
	if len(codes) < 20 {
		t.Fatalf("found only %v — the scan no longer reads the tool sources", codes)
	}
	found := map[string]bool{}
	for _, code := range codes {
		found[code] = true
		public := publicErrorForCode("builtin.git", errors.New(code), &apperrors.AppError{Code: code, Message: "probe", Recoverable: true})
		if public.Message != unexpectedMessage {
			continue
		}
		if _, ok := redactedOnPurpose[code]; !ok {
			t.Errorf("%s reaches an agent as %q. Add it to keptCodes (after reading where its message comes from), map it in publicErrorForCode, or list it in redactedOnPurpose with the reason", code, unexpectedMessage)
		}
	}
	for code, why := range redactedOnPurpose {
		if !found[code] {
			t.Errorf("redactedOnPurpose lists %s (%s), which no tool raises any more — remove it", code, why)
		}
		if public := publicErrorForCode("builtin.git", errors.New(code), &apperrors.AppError{Code: code, Message: "probe"}); public.Message != unexpectedMessage {
			t.Errorf("%s is published, so it is not redacted on purpose — remove it from the list", code)
		}
	}
	if !found["pr.unsupportedHost"] {
		t.Error("the scan did not see pr.unsupportedHost")
	}
}

// ---- through the real bridge ---------------------------------------------

type gitlabCheckout struct{}

func (gitlabCheckout) Checkout(_ context.Context, folder string) (prwatch.Checkout, error) {
	remote := prwatch.RemoteRef{Name: "origin", Host: "gitlab.com", Path: "acme/widgets", Owner: "acme"}
	return prwatch.Checkout{Folder: folder, Branch: "feat/x", Origin: remote, Push: remote, DefaultBranch: "main"}, nil
}

type noToken struct{}

func (noToken) Token(context.Context, prwatch.TokenRequest) (string, string, error) {
	return "", "", errors.New("must not be asked: the host is unsupported")
}

type oneFolder struct{ path string }

func (o oneFolder) ListWorkspaces() ([]workspace.Workspace, error) {
	return []workspace.Workspace{{ID: "w1", Name: "acme", Root: o.path}}, nil
}
func (o oneFolder) ListServices(string) ([]workspace.WorkspaceService, error) {
	return []workspace.WorkspaceService{{ID: "s1", Name: "widgets", Path: o.path}}, nil
}

type repoStub struct{}

// At hands back a repository the pr.* tools never touch before the pull
// request host is resolved.
func (repoStub) At(context.Context, string) (mcp.GitAPI, error) { return struct{ mcp.GitAPI }{}, nil }
func (repoStub) Summaries(context.Context, []string) ([]gitservice.Repository, error) {
	return nil, nil
}

func callMCPTool(t *testing.T, server *mcp.Server, name string, args map[string]any) (string, bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	clientTransport, serverTransport := sdk.NewInMemoryTransports()
	go func() { _ = server.MCP().Run(ctx, serverTransport) }()
	client := sdk.NewClient(&sdk.Implementation{Name: "error-test", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer session.Close()
	result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return err.Error(), true
	}
	var text bytes.Buffer
	for _, content := range result.Content {
		if c, ok := content.(*sdk.TextContent); ok {
			text.WriteString(c.Text)
		}
	}
	return text.String(), result.IsError
}

// The user's own log: pr.create on a GitLab remote reached the agent as
// internal_error. This goes through the real container, the real gateway and
// the real error mapping, with the real pull request service in front of a
// GitLab checkout.
func TestPullRequestToolsTellTheAgentWhyThroughTheRealBridge(t *testing.T) {
	var logged bytes.Buffer
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	folder := t.TempDir()
	container := app.NewContainer()
	server := mcp.NewServer(mcp.Deps{
		Caller:       NewCaller(NewAppService(container)),
		Workspace:    oneFolder{path: folder},
		GitRepos:     repoStub{},
		PullRequests: prwatch.New(forge.NewRegistry(forge.NewGitHub()), gitlabCheckout{}, noToken{}, prwatch.NewMemoryStore()),
	})

	const sentence = "DevKit has no pull request support for this remote's host yet (GitHub only). Use the host's CLI in a shell."
	for _, call := range []struct {
		tool string
		args map[string]any
	}{
		{"pr.get", map[string]any{"repo": folder}},
		{"pr.create", map[string]any{"repo": folder, "title": "Add retries"}},
	} {
		text, isError := callMCPTool(t, server, call.tool, call.args)
		if !isError || !strings.Contains(text, "pr.unsupportedHost") || !strings.Contains(text, sentence) {
			t.Errorf("%s: agent was told %q (error=%v), want pr.unsupportedHost with its sentence", call.tool, text, isError)
		}
		if strings.Contains(text, "internal_error") || strings.Contains(text, unexpectedMessage) {
			t.Errorf("%s: still redacted: %q", call.tool, text)
		}
	}
	if strings.Contains(logged.String(), "unclassified error") {
		t.Errorf("the audit log still reports an unclassified error:\n%s", logged.String())
	}

	// A folder outside every workspace is git_repo.go's code, also written for
	// the agent.
	text, isError := callMCPTool(t, server, "pr.get", map[string]any{"repo": "/somewhere/else"})
	if !isError || !strings.Contains(text, "git.repoOutsideWorkspace") || !strings.Contains(text, "is not a folder of your workspace") {
		t.Errorf("an unknown repo reached the agent as %q", text)
	}
}
