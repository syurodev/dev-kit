package bridge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
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

	const sentence = "DevKit has no pull request support for this remote's host yet (GitHub and gitlab.com only). Use the host's CLI in a shell."
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

// ---- internal/git refusals ----------------------------------------------

// onePushedRepo is At for a real repository, so the refusals under test come
// from internal/git's own checks and not from a stand-in.
type onePushedRepo struct{ git *gitservice.Service }

func (o onePushedRepo) At(context.Context, string) (mcp.GitAPI, error) { return o.git, nil }
func (o onePushedRepo) Summaries(context.Context, []string) ([]gitservice.Repository, error) {
	return nil, nil
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-c", "user.name=T", "-c", "user.email=t@example.test", "-c", "commit.gpgsign=false"}, args...)...)
	command.Dir = dir
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// A refusal of internal/git's own is a plain sentinel, so it used to reach an
// agent as internal_error and be logged as unclassified. This runs the real
// gateway and error mapping in front of a real repository whose only commit is
// already on a remote.
func TestGitRefusalsTellTheAgentWhyThroughTheRealBridge(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	var logged bytes.Buffer
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	folder := t.TempDir()
	remote := t.TempDir()
	runGit(t, remote, "init", "--bare", "--initial-branch=main")
	runGit(t, folder, "init", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(folder, "a.txt"), []byte("a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, folder, "add", "a.txt")
	runGit(t, folder, "commit", "-m", "first")
	runGit(t, folder, "remote", "add", "origin", remote)
	runGit(t, folder, "push", "-u", "origin", "main")

	repo := gitservice.NewService(context.Background())
	if err := repo.Open(context.Background(), folder); err != nil {
		t.Fatal(err)
	}
	server := mcp.NewServer(mcp.Deps{
		Caller:    NewCaller(NewAppService(app.NewContainer())),
		Workspace: oneFolder{path: folder},
		Git:       repo,
		GitRepos:  onePushedRepo{git: repo},
	})

	for _, call := range []struct {
		tool, code, sentence string
		args                 map[string]any
	}{
		{"git.amend", "git.pushedHistory", "the last commit is already on a remote, and DevKit never force-pushes", map[string]any{"repo": folder, "message": "again"}},
		{"git.squash", "git.pushedHistory", "only the newest 0 commit(s) are not on a remote", map[string]any{"repo": folder, "count": 2}},
		{"git.rebase", "git.notAllowed", "not-a-ref is not a commit here", map[string]any{"repo": folder, "onto": "not-a-ref"}},
		{"git.rebase", "git.nothingInProgress", "no rebase is under way", map[string]any{"repo": folder, "action": "continue"}},
		{"git.cherryPick", "git.notAllowed", "name at least one commit", map[string]any{"repo": folder}},
		{"git.revert", "git.notAllowed", "nope is not a commit here", map[string]any{"repo": folder, "commits": []string{"nope"}}},
		{"git.commit", "git.notAllowed", "a commit needs a message", map[string]any{"repo": folder, "message": " "}},
	} {
		text, isError := callMCPTool(t, server, call.tool, call.args)
		if !isError || !strings.Contains(text, call.code) || !strings.Contains(text, call.sentence) {
			t.Errorf("%s: agent was told %q (error=%v), want %s with %q", call.tool, text, isError, call.code, call.sentence)
		}
		if strings.Contains(text, "internal_error") || strings.Contains(text, unexpectedMessage) {
			t.Errorf("%s: still redacted: %q", call.tool, text)
		}
	}

	// A dirty tree stops a rebase before it starts.
	if err := os.WriteFile(filepath.Join(folder, "a.txt"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	text, isError := callMCPTool(t, server, "git.rebase", map[string]any{"repo": folder, "onto": "main"})
	if !isError || !strings.Contains(text, "git.dirtyWorkTree") || !strings.Contains(text, "commit or stash the changes first") {
		t.Errorf("git.rebase on a dirty tree: %q", text)
	}
	if strings.Contains(logged.String(), "unclassified error") {
		t.Errorf("the audit log still reports an unclassified error:\n%s", logged.String())
	}
}

// Every refusal sentinel internal/git declares is published by gitRefusals;
// a new one fails here until someone words it for the reader.
func TestEveryGitSentinelIsPublished(t *testing.T) {
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	files, err := filepath.Glob(filepath.Join("..", "git", "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no internal/git sources found: %v", err)
	}
	declared := regexp.MustCompile(`(?m)^\s*(Err[A-Za-z]+)\s*=\s*errors\.New\(`)
	names := map[string]bool{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range declared.FindAllSubmatch(source, -1) {
			names[string(match[1])] = true
		}
	}
	if len(names) < 10 {
		t.Fatalf("found only %v — the scan no longer reads the sources", names)
	}
	// The remaining sentinels are not refusals an agent can act on.
	notRefusals := map[string]bool{}
	published := len(gitRefusals)
	if published+len(notRefusals) < len(names) {
		t.Errorf("internal/git declares %d error sentinels (%v) and gitRefusals lists %d: map the new one in internal/bridge/git_errors.go", len(names), names, published)
	}
	for _, refusal := range gitRefusals {
		got := publicError("builtin.git", fmt.Errorf("outer: %w", refusal.sentinel))
		if got.Code != refusal.code || got.Message != refusal.fallback {
			t.Errorf("%v bare = %s %q, want %s %q", refusal.sentinel, got.Code, got.Message, refusal.code, refusal.fallback)
		}
		got = publicError("builtin.git", fmt.Errorf("outer: %w", fmt.Errorf("%w: a\nsentence", refusal.sentinel)))
		if got.Code != refusal.code || got.Message != "a sentence" {
			t.Errorf("%v wrapped = %s %q, want its own sentence", refusal.sentinel, got.Code, got.Message)
		}
	}
}
