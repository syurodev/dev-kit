package agentruntime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"22", "22.0.0", 0},
		{"24.14.1", "22", 1},
		{"20.19.0", "22", -1},
		{"v26.4.0", "v24.14.1", 1},
		{"0.74.0", "0.76.0", -1},
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// fakeNode writes a node that answers probeNode the way the real one does,
// with an npm beside it unless noNPM.
func fakeNode(t *testing.T, binDir, version string, noNPM bool) string {
	t.Helper()
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	node := filepath.Join(binDir, "node")
	script := fmt.Sprintf("#!/bin/sh\necho v%s\necho \"$0\"\n", version)
	if err := os.WriteFile(node, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if !noNPM {
		if err := os.WriteFile(filepath.Join(binDir, "npm"), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return node
}

// isolateLocator takes the real machine out of node discovery.
func isolateLocator(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake node programs are shell scripts")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("NVM_DIR", "")
	t.Setenv("FNM_DIR", "")
	t.Setenv("PATH", t.TempDir())
	saved := systemBinDirs
	systemBinDirs = nil
	t.Cleanup(func() { systemBinDirs = saved })
	return home
}

func resolved(t *testing.T, path string) string {
	t.Helper()
	out, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestLocateNodePicksNewestAtLeast22(t *testing.T) {
	home := isolateLocator(t)
	versions := filepath.Join(home, ".nvm", "versions", "node")
	fakeNode(t, filepath.Join(versions, "v20.19.0", "bin"), "20.19.0", false)
	fakeNode(t, filepath.Join(versions, "v22.3.0", "bin"), "22.3.0", false)
	want := fakeNode(t, filepath.Join(versions, "v24.14.1", "bin"), "24.14.1", false)

	node, reqs := locateNode(context.Background())
	if !reqs[0].Met || !reqs[1].Met {
		t.Fatalf("requirements = %+v", reqs)
	}
	if node.Version != "24.14.1" || node.Node != resolved(t, want) {
		t.Fatalf("picked %+v, want 24.14.1 at %s", node, want)
	}
	if node.NPM != filepath.Join(filepath.Dir(node.Node), "npm") {
		t.Fatalf("npm = %q, want the sibling of node", node.NPM)
	}
}

func TestLocateNodeRefusesOlderThan22(t *testing.T) {
	home := isolateLocator(t)
	fakeNode(t, filepath.Join(home, ".nvm", "versions", "node", "v20.19.0", "bin"), "20.19.0", false)

	_, reqs := locateNode(context.Background())
	if reqs[0].Met || reqs[0].Found != "20.19.0" || !strings.Contains(reqs[0].Problem, "older than 22") {
		t.Fatalf("node requirement = %+v", reqs[0])
	}
	if reqs[0].InstallHint == "" || reqs[1].Met {
		t.Fatalf("requirements = %+v", reqs)
	}
}

func TestLocateNodePrefersOneWithNPM(t *testing.T) {
	home := isolateLocator(t)
	versions := filepath.Join(home, ".nvm", "versions", "node")
	fakeNode(t, filepath.Join(versions, "v26.0.0", "bin"), "26.0.0", true)
	fakeNode(t, filepath.Join(versions, "v22.3.0", "bin"), "22.3.0", false)

	node, reqs := locateNode(context.Background())
	if node.Version != "22.3.0" || !reqs[1].Met {
		t.Fatalf("picked %+v (%+v), want the node that has npm", node, reqs)
	}
}

func TestLocateNodeFallsBackToPATH(t *testing.T) {
	isolateLocator(t)
	dir := t.TempDir()
	want := fakeNode(t, dir, "22.12.0", false)
	t.Setenv("PATH", dir)

	node, _ := locateNode(context.Background())
	if node.Node != resolved(t, want) {
		t.Fatalf("picked %+v, want %s from PATH", node, want)
	}
}

func TestNodeReadPathsGrantsHomebrewRoot(t *testing.T) {
	got := nodeReadPaths("/opt/homebrew/Cellar/node/24.1.0/bin/node")
	if !slices.Equal(got, []string{"/opt/homebrew/Cellar/node/24.1.0", "/opt/homebrew"}) {
		t.Fatalf("got %v", got)
	}
	got = nodeReadPaths("/home/u/.nvm/versions/node/v24.1.0/bin/node")
	if !slices.Equal(got, []string{"/home/u/.nvm/versions/node/v24.1.0"}) {
		t.Fatalf("got %v", got)
	}
}

// testManager has a fake node and an npm that writes the tree a real install
// would, so nothing touches the network.
type testManager struct {
	*Manager
	node     string
	npmCalls atomic.Int32
	// npm replaces the default fake when set.
	npm func(ctx context.Context, prefix, spec string, onLine func(string)) error
}

func newTestManager(t *testing.T) *testManager {
	t.Helper()
	root := filepath.Join(t.TempDir(), "runtimes")
	nodeDir := filepath.Join(t.TempDir(), "node", "bin")
	if err := os.MkdirAll(nodeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	node := filepath.Join(nodeDir, exeName("node"))
	if err := os.WriteFile(node, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	tm := &testManager{Manager: New(root), node: node}
	tm.findNode = func(context.Context) (nodeInstall, []Requirement) {
		return nodeInstall{Node: node, NPM: filepath.Join(nodeDir, "npm"), Version: "24.14.1"},
			[]Requirement{{Name: "node", Met: true}, {Name: "npm", Met: true}}
	}
	tm.findClaude = func(context.Context) *MachineClaude { return nil }
	tm.runNPM = func(ctx context.Context, _ string, args, _ []string, onLine func(string)) error {
		if args[0] != "install" {
			onLine("11.6.0")
			return nil
		}
		tm.npmCalls.Add(1)
		prefix, spec := args[slices.Index(args, "--prefix")+1], args[3]
		if !slices.Contains(args, "--ignore-scripts") {
			t.Errorf("npm install without --ignore-scripts: %v", args)
		}
		if tm.npm != nil {
			return tm.npm(ctx, prefix, spec, onLine)
		}
		writeFakeInstall(t, prefix, spec[strings.LastIndex(spec, "@")+1:])
		return nil
	}
	return tm
}

func writeFakeInstall(t *testing.T, prefix, version string) {
	t.Helper()
	files := map[string]string{
		"package-lock.json": "{}",
		"node_modules/@agentclientprotocol/claude-agent-acp/package.json":       `{"version":"` + version + `"}`,
		"node_modules/@agentclientprotocol/claude-agent-acp/dist/index.js":      "",
		"node_modules/@anthropic-ai/claude-agent-sdk/package.json":              `{"version":"0.3.257"}`,
		"node_modules/@anthropic-ai/claude-agent-sdk-test/" + exeName("claude"): "",
	}
	for rel, body := range files {
		path := filepath.Join(prefix, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func (tm *testManager) current(t *testing.T) currentFile {
	t.Helper()
	return tm.readCurrent(ClaudeACP)
}

func leftovers(t *testing.T, dir string) []string {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	var out []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			out = append(out, e.Name())
		}
	}
	return out
}

func TestInstallActivateRemoveKeepsPrevious(t *testing.T) {
	// The default install is whatever DevKit is measured against.
	tested := agents[ClaudeACP].TestedVersion
	tm := newTestManager(t)
	ctx := context.Background()

	if err := tm.Install(ctx, ClaudeACP, ""); err != nil {
		t.Fatal(err)
	}
	if got := tm.current(t); got != (currentFile{Version: tested}) {
		t.Fatalf("after first install current = %+v", got)
	}
	if err := tm.Install(ctx, ClaudeACP, "0.76.0"); err != nil {
		t.Fatal(err)
	}
	if got := tm.current(t); got != (currentFile{Version: "0.76.0", Previous: tested}) {
		t.Fatalf("after update current = %+v", got)
	}
	// Reinstalling the current version must not make it its own previous.
	if err := tm.Install(ctx, ClaudeACP, "0.76.0"); err != nil {
		t.Fatal(err)
	}
	if got := tm.current(t); got != (currentFile{Version: "0.76.0", Previous: tested}) {
		t.Fatalf("after reinstall current = %+v", got)
	}

	if err := tm.Activate(ClaudeACP, tested); err != nil {
		t.Fatal(err)
	}
	if got := tm.current(t); got != (currentFile{Version: tested, Previous: "0.76.0"}) {
		t.Fatalf("after rollback current = %+v", got)
	}
	if err := tm.Remove(ClaudeACP, tested); !errors.Is(err, ErrVersionInUse) {
		t.Fatalf("removing current: %v", err)
	}
	if err := tm.Remove(ClaudeACP, "0.76.0"); err != nil {
		t.Fatal(err)
	}
	if got := tm.current(t); got != (currentFile{Version: tested}) {
		t.Fatalf("after removing previous current = %+v", got)
	}
	if err := tm.Activate(ClaudeACP, "0.76.0"); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("activating a removed version: %v", err)
	}
	if err := tm.Activate(ClaudeACP, "../"+tested); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("activating a path: %v", err)
	}
	if got := leftovers(t, tm.agentDir(ClaudeACP)); len(got) != 0 {
		t.Fatalf("temp, staging or trash left behind: %v", got)
	}

	st, err := tm.Status(ctx, ClaudeACP)
	if err != nil {
		t.Fatal(err)
	}
	if st.Current == nil || st.Current.SDKVersion != "0.3.257" || st.Current.NodePath != tm.node ||
		!strings.HasSuffix(filepath.ToSlash(st.Current.BundledClaude), "claude-agent-sdk-test/"+exeName("claude")) {
		t.Fatalf("status current = %+v", st.Current)
	}
	if st.Previous != nil || st.Installing || st.DiskBytes == 0 || st.ClaudeBinary.Source != ClaudeBundled {
		t.Fatalf("status = %+v", st)
	}
}

func TestInstallFailureCleansUpAndRedacts(t *testing.T) {
	tm := newTestManager(t)
	var streamed []string
	tm.OnOutput = func(_, line string) { streamed = append(streamed, line) }
	tm.npm = func(_ context.Context, prefix, _ string, onLine func(string)) error {
		writeFakeInstall(t, prefix, "0.74.0") // partial tree that must not survive
		onLine("npm error code E404")
		onLine("npm error 404 Not Found - GET https://bob:hunter2@registry.example/x - _authToken=npm_SECRET")
		onLine("npm error A complete log of this run can be found in: /x.log")
		return errors.New("exit status 1")
	}

	err := tm.Install(context.Background(), ClaudeACP, "")
	var rtErr *Error
	if !errors.Is(err, ErrInstallFailed) || !errors.As(err, &rtErr) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(rtErr.Reason, "404 Not Found") {
		t.Fatalf("reason %q should name what npm said", rtErr.Reason)
	}
	for _, text := range append(streamed, rtErr.Reason) {
		if strings.Contains(text, "hunter2") || strings.Contains(text, "npm_SECRET") {
			t.Fatalf("credential reached the UI: %q", text)
		}
	}
	if got := leftovers(t, tm.agentDir(ClaudeACP)); len(got) != 0 {
		t.Fatalf("staging left behind: %v", got)
	}
	if _, err := os.Stat(filepath.Join(tm.agentDir(ClaudeACP), "current.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("current.json written by a failed install: %v", err)
	}
}

func TestInstallVerifyFailureKeepsCurrent(t *testing.T) {
	// The default install is whatever DevKit is measured against.
	tested := agents[ClaudeACP].TestedVersion
	tm := newTestManager(t)
	if err := tm.Install(context.Background(), ClaudeACP, ""); err != nil {
		t.Fatal(err)
	}
	var verified Resolved
	tm.Verify = func(_ context.Context, rt Resolved) error {
		verified = rt
		return errors.New("initialize: agent process exited")
	}
	err := tm.Install(context.Background(), ClaudeACP, "0.76.0")
	if !errors.Is(err, ErrInstallFailed) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(verified.Args[0], ".staging-0.76.0-") {
		t.Fatalf("Verify ran against %v, want the staging tree", verified.Args)
	}
	if got := tm.current(t); got != (currentFile{Version: tested}) {
		t.Fatalf("current = %+v", got)
	}
	if _, err := os.Stat(filepath.Join(tm.agentDir(ClaudeACP), "0.76.0")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("an unverified version was activated")
	}
	if got := leftovers(t, tm.agentDir(ClaudeACP)); len(got) != 0 {
		t.Fatalf("staging left behind: %v", got)
	}
}

func TestInstallRejectsVersionMismatch(t *testing.T) {
	tm := newTestManager(t)
	tm.npm = func(_ context.Context, prefix, _ string, _ func(string)) error {
		writeFakeInstall(t, prefix, "0.75.0")
		return nil
	}
	if err := tm.Install(context.Background(), ClaudeACP, "0.76.0"); !errors.Is(err, ErrInstallFailed) {
		t.Fatalf("err = %v", err)
	}
}

func TestInstallRequirementNotMet(t *testing.T) {
	tm := newTestManager(t)
	tm.findNode = func(context.Context) (nodeInstall, []Requirement) {
		return nodeInstall{}, []Requirement{{Name: "node", Problem: "Node.js was not found"}}
	}
	if err := tm.Install(context.Background(), ClaudeACP, ""); !errors.Is(err, ErrRequirementNotMet) {
		t.Fatalf("err = %v", err)
	}
	if tm.npmCalls.Load() != 0 {
		t.Fatal("npm ran without a node")
	}
}

func TestInstallSingleFlightAndCancel(t *testing.T) {
	tm := newTestManager(t)
	started := make(chan struct{})
	tm.npm = func(ctx context.Context, prefix, _ string, _ func(string)) error {
		writeFakeInstall(t, prefix, "0.74.0")
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- tm.Install(ctx, ClaudeACP, "") }()
	<-started

	if err := tm.Install(context.Background(), ClaudeACP, ""); !errors.Is(err, ErrInstallAlreadyRunning) {
		t.Fatalf("second install: %v", err)
	}
	if st, _ := tm.Status(context.Background(), ClaudeACP); !st.Installing {
		t.Fatal("status does not report the running install")
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled install: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("install ignored cancellation")
	}
	if got := leftovers(t, tm.agentDir(ClaudeACP)); len(got) != 0 {
		t.Fatalf("staging left behind: %v", got)
	}
	if st, _ := tm.Status(context.Background(), ClaudeACP); st.Installing || st.Current != nil {
		t.Fatalf("status after cancel = %+v", st)
	}
}

func TestResolve(t *testing.T) {
	// The default install is whatever DevKit is measured against.
	tested := agents[ClaudeACP].TestedVersion
	tm := newTestManager(t)
	if _, err := tm.Resolve(ClaudeACP); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("before install: %v", err)
	}
	if _, err := tm.Resolve("gemini"); !errors.Is(err, ErrUnknownAgent) {
		t.Fatalf("unknown agent: %v", err)
	}
	if err := tm.Install(context.Background(), ClaudeACP, ""); err != nil {
		t.Fatal(err)
	}

	rt, err := tm.Resolve(ClaudeACP)
	if err != nil {
		t.Fatal(err)
	}
	versionDir := resolved(t, filepath.Join(tm.agentDir(ClaudeACP), tested))
	node := resolved(t, tm.node)
	if rt.Command != node || rt.Version != tested {
		t.Fatalf("resolved = %+v", rt)
	}
	if len(rt.Args) != 1 || rt.Args[0] != filepath.Join(versionDir, filepath.FromSlash(agents[ClaudeACP].Entry)) {
		t.Fatalf("args = %v", rt.Args)
	}
	for _, want := range []string{versionDir, filepath.Dir(filepath.Dir(node))} {
		if !slices.Contains(rt.ReadOnlyPaths, want) {
			t.Fatalf("ReadOnlyPaths %v missing %s", rt.ReadOnlyPaths, want)
		}
	}
	if !strings.HasPrefix(rt.Env["PATH"], filepath.Dir(node)+string(os.PathListSeparator)) {
		t.Fatalf("PATH = %q, want node's directory first", rt.Env["PATH"])
	}
	if _, ok := rt.Env["CLAUDE_CODE_EXECUTABLE"]; ok {
		t.Fatal("bundled Claude must not set CLAUDE_CODE_EXECUTABLE")
	}

	if err := tm.SetClaudeBinary(context.Background(), ClaudeMachine); !errors.Is(err, ErrRequirementNotMet) {
		t.Fatalf("machine Claude with none installed: %v", err)
	}
	if err := tm.SetClaudeBinary(context.Background(), "npx"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unknown source: %v", err)
	}

	// ~/.local/bin/claude is a symlink into a versions directory; both the env
	// and the grant must name where it really is.
	versions := filepath.Join(t.TempDir(), "share", "claude", "versions")
	if err := os.MkdirAll(versions, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(versions, "2.1.269")
	if err := os.WriteFile(target, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), exeName("claude"))
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	tm.findClaude = func(context.Context) *MachineClaude { return &MachineClaude{Path: link, Version: "2.1.269"} }
	if err := tm.SetClaudeBinary(context.Background(), ClaudeMachine); err != nil {
		t.Fatal(err)
	}
	rt, err = tm.Resolve(ClaudeACP)
	if err != nil {
		t.Fatal(err)
	}
	if rt.Env["CLAUDE_CODE_EXECUTABLE"] != resolved(t, target) {
		t.Fatalf("CLAUDE_CODE_EXECUTABLE = %q, want %q", rt.Env["CLAUDE_CODE_EXECUTABLE"], resolved(t, target))
	}
	if !slices.Contains(rt.ReadOnlyPaths, resolved(t, versions)) {
		t.Fatalf("ReadOnlyPaths %v missing the machine Claude's directory", rt.ReadOnlyPaths)
	}
	if st, _ := tm.Status(context.Background(), ClaudeACP); st.ClaudeBinary.Source != ClaudeMachine || st.ClaudeBinary.Machine == nil {
		t.Fatalf("status claude binary = %+v", st.ClaudeBinary)
	}
}

func TestGCOnStartup(t *testing.T) {
	tm := newTestManager(t)
	dir := tm.agentDir(ClaudeACP)
	for _, name := range []string{".staging-0.77.0-x", ".trash-0.73.0-y", "0.72.0", "0.74.0", "0.76.0"} {
		if err := os.MkdirAll(filepath.Join(dir, name, "node_modules"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// No current.json: nothing says which version is live, so only leftovers go.
	if err := tm.GCOnStartup(); err != nil {
		t.Fatal(err)
	}
	assertEntries(t, dir, "0.72.0", "0.74.0", "0.76.0")

	if err := writeJSON(filepath.Join(dir, "current.json"), currentFile{Version: "0.76.0", Previous: "0.74.0"}); err != nil {
		t.Fatal(err)
	}
	if err := tm.GCOnStartup(); err != nil {
		t.Fatal(err)
	}
	assertEntries(t, dir, "0.74.0", "0.76.0", "current.json")
}

func assertEntries(t *testing.T, dir string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	if !slices.Equal(got, want) {
		t.Fatalf("entries = %v, want %v", got, want)
	}
}

const registryBody = `{"version":"1.0.0","agents":[
 {"id":"gemini","distribution":{"npx":{"package":"@google/gemini-cli@1.0.0"}}},
 {"id":"claude-acp","version":"0.76.0","license_url":"https://example.com/license",
  "distribution":{"npx":{"package":"@agentclientprotocol/claude-agent-acp@0.76.0"}}}]}`

func TestCheckLatestCachesWithETag(t *testing.T) {
	var requests, notModified atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("If-None-Match") == `"v1"` {
			notModified.Add(1)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte(registryBody))
	}))
	tm := newTestManager(t)
	tm.RegistryURL = srv.URL
	ctx := context.Background()

	if st, _ := tm.Status(ctx, ClaudeACP); st.LatestVersion != nil {
		t.Fatalf("latest before any check = %v", *st.LatestVersion)
	}
	for i := 0; i < 2; i++ {
		if err := tm.CheckLatest(ctx, ClaudeACP); err != nil {
			t.Fatal(err)
		}
		st, _ := tm.Status(ctx, ClaudeACP)
		if st.LatestVersion == nil || *st.LatestVersion != "0.76.0" || st.LicenseURL == nil || *st.LicenseURL != "https://example.com/license" {
			t.Fatalf("check %d: latest=%v license=%v", i, st.LatestVersion, st.LicenseURL)
		}
	}
	if requests.Load() != 2 || notModified.Load() != 1 {
		t.Fatalf("requests=%d notModified=%d, want the second to revalidate", requests.Load(), notModified.Load())
	}

	// Offline: the cache still answers and the check is not an error.
	srv.Close()
	if err := tm.CheckLatest(ctx, ClaudeACP); err != nil {
		t.Fatal(err)
	}
	if st, _ := tm.Status(ctx, ClaudeACP); st.LatestVersion == nil || *st.LatestVersion != "0.76.0" {
		t.Fatal("cached latest lost when offline")
	}
}

func TestRegistryEntryIgnoresAnotherPackage(t *testing.T) {
	body := `{"agents":[{"id":"claude-acp","distribution":{"npx":{"package":"@someone/else@9.0.0"}}}]}`
	if v, _ := registryEntry([]byte(body), agents[ClaudeACP]); v != "" {
		t.Fatalf("version %q from a different package", v)
	}
	if v, _ := registryEntry([]byte("not json"), agents[ClaudeACP]); v != "" {
		t.Fatal("version from garbage")
	}
}
