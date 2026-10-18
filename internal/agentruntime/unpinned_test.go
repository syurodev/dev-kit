package agentruntime

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeFakeOpenCodeInstall writes the tree `npm install @opencode/cli` leaves:
// a wrapper package and the platform package that holds the real binary.
func writeFakeOpenCodeInstall(t *testing.T, prefix, version string, withBinary bool) {
	t.Helper()
	files := map[string]string{
		"package-lock.json":                                     "{}",
		"node_modules/@opencode/cli/package.json":               `{"name":"@opencode/cli","version":"` + version + `"}`,
		"node_modules/@opencode/cli/bin/" + exeName("opencode"): "wrapper",
	}
	if withBinary {
		files["node_modules/@opencode/cli-darwin-arm64/bin/"+exeName("opencode")] = "#!/bin/sh\necho opencode v" + version + "\n"
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

// npmRegistry answers like registry.npmjs.org/<package>/latest.
func npmRegistry(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.EscapedPath(), "/@opencode%2Fcli/latest") {
			t.Errorf("registry path = %q", r.URL.EscapedPath())
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func opencodeTestManager(t *testing.T, registry *httptest.Server, withBinary bool) (*testManager, *[]string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake opencode binary is a shell script")
	}
	tm := newTestManager(t)
	tm.NPMRegistry = registry.URL
	specs := &[]string{}
	tm.npm = func(_ context.Context, prefix, spec string, _ func(string)) error {
		*specs = append(*specs, spec)
		writeFakeOpenCodeInstall(t, prefix, spec[strings.LastIndex(spec, "@")+1:], withBinary)
		return nil
	}
	return tm, specs
}

func TestUnpinnedInstallTakesNPMsLatest(t *testing.T) {
	tm, specs := opencodeTestManager(t, npmRegistry(t, http.StatusOK, `{"name":"@opencode/cli","version":"2.0.32"}`), true)

	if err := tm.Install(context.Background(), OpenCodeCLI, ""); err != nil {
		t.Fatal(err)
	}
	if len(*specs) != 1 || (*specs)[0] != "@opencode/cli@2.0.32" {
		t.Fatalf("npm specs = %v, want the version npm's latest tag named", *specs)
	}
	rt, err := tm.Resolve(OpenCodeCLI)
	if err != nil {
		t.Fatal(err)
	}
	if rt.Version != "2.0.32" || filepath.Base(rt.Command) != exeName("opencode") || len(rt.Args) != 0 {
		t.Fatalf("resolved = %+v, want the platform binary of 2.0.32 with no entry script", rt)
	}
	if !strings.Contains(rt.Command, "cli-darwin-arm64") {
		t.Fatalf("command = %q, want the platform package's binary, not the wrapper", rt.Command)
	}

	st, err := tm.Status(context.Background(), OpenCodeCLI)
	if err != nil {
		t.Fatal(err)
	}
	if st.TestedVersion != "" {
		t.Fatalf("TestedVersion = %q, an unpinned agent has none", st.TestedVersion)
	}
	if st.LatestVersion == nil || *st.LatestVersion != "2.0.32" || st.Current == nil || st.Current.Version != "2.0.32" {
		t.Fatalf("status = %+v", st)
	}
}

func TestUnpinnedInstallAcceptsAnExplicitVersion(t *testing.T) {
	// A registry that would fail proves it was not asked.
	tm, specs := opencodeTestManager(t, npmRegistry(t, http.StatusInternalServerError, ``), true)
	if err := tm.Install(context.Background(), OpenCodeCLI, "2.0.30"); err != nil {
		t.Fatal(err)
	}
	if len(*specs) != 1 || (*specs)[0] != "@opencode/cli@2.0.30" {
		t.Fatalf("npm specs = %v", *specs)
	}
}

func TestUnpinnedInstallRefusesALatestItCannotTrust(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   string
	}{
		"registry down":   {http.StatusServiceUnavailable, `{}`},
		"another package": {http.StatusOK, `{"name":"left-pad","version":"1.0.0"}`},
		"prerelease":      {http.StatusOK, `{"name":"@opencode/cli","version":"2.1.0-beta.1"}`},
		"not a version":   {http.StatusOK, `{"name":"@opencode/cli","version":"latest"}`},
		"not even a json": {http.StatusOK, `<html>`},
	} {
		t.Run(name, func(t *testing.T) {
			tm, specs := opencodeTestManager(t, npmRegistry(t, tc.status, tc.body), true)
			err := tm.Install(context.Background(), OpenCodeCLI, "")
			if !errors.Is(err, ErrInstallFailed) {
				t.Fatalf("err = %v, want ErrInstallFailed", err)
			}
			if len(*specs) != 0 {
				t.Fatalf("npm ran for an untrusted latest: %v", *specs)
			}
			if cur := tm.readCurrent(OpenCodeCLI); cur.Version != "" {
				t.Fatalf("current = %+v after a refused install", cur)
			}
		})
	}
}

func TestUnpinnedInstallWithoutPlatformBinaryFails(t *testing.T) {
	tm, _ := opencodeTestManager(t, npmRegistry(t, http.StatusOK, `{"name":"@opencode/cli","version":"2.0.32"}`), false)
	err := tm.Install(context.Background(), OpenCodeCLI, "")
	if !errors.Is(err, ErrInstallFailed) || !strings.Contains(err.Error(), "no opencode executable") {
		t.Fatalf("err = %v, want ErrInstallFailed naming the missing executable", err)
	}
}

func TestUnpinnedCheckLatestCachesNPMsAnswer(t *testing.T) {
	tm, _ := opencodeTestManager(t, npmRegistry(t, http.StatusOK, `{"name":"@opencode/cli","version":"2.0.33"}`), true)
	if err := tm.CheckLatest(context.Background(), OpenCodeCLI); err != nil {
		t.Fatal(err)
	}
	st, err := tm.Status(context.Background(), OpenCodeCLI)
	if err != nil {
		t.Fatal(err)
	}
	if st.LatestVersion == nil || *st.LatestVersion != "2.0.33" {
		t.Fatalf("latest = %v, want 2.0.33 from the cache", st.LatestVersion)
	}

	// A later bad answer keeps the cache that works.
	bad := npmRegistry(t, http.StatusOK, `{"name":"left-pad","version":"9.9.9"}`)
	tm.NPMRegistry = bad.URL
	if err := tm.CheckLatest(context.Background(), OpenCodeCLI); err != nil {
		t.Fatal(err)
	}
	st, _ = tm.Status(context.Background(), OpenCodeCLI)
	if st.LatestVersion == nil || *st.LatestVersion != "2.0.33" {
		t.Fatalf("latest = %v after a bad answer, want the cached 2.0.33", st.LatestVersion)
	}
}
