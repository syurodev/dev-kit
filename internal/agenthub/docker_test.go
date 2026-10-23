package agenthub

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// listen makes a real unix socket at path. Kept short: sun_path is ~104 bytes
// and t.TempDir() is long, so tests root their sockets in a short dir.
func listen(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
}

// withoutSystemDocker hides the host's own daemon from DetectDocker for the
// test, so a machine with Docker running (CI) answers like one without.
func withoutSystemDocker(t *testing.T) {
	t.Helper()
	saved := systemDockerSocket
	systemDockerSocket = filepath.Join(t.TempDir(), "no-docker.sock")
	t.Cleanup(func() { systemDockerSocket = saved })
}

func shortHome(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "dk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func env(vars map[string]string) func(string) string {
	return func(key string) string { return vars[key] }
}

// writeContext lays a docker context down the way the CLI does: config.json
// naming it, and meta.json under the SHA-256 of its name.
func writeContext(t *testing.T, home, name, host string) {
	t.Helper()
	sum := sha256.Sum256([]byte(name))
	meta := filepath.Join(home, ".docker", "contexts", "meta", hex.EncodeToString(sum[:]))
	if err := os.MkdirAll(meta, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".docker", "config.json"), []byte(`{"currentContext":"`+name+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	body := `{"Name":"` + name + `","Endpoints":{"docker":{"Host":"` + host + `","SkipTLSVerify":false}}}`
	if err := os.WriteFile(filepath.Join(meta, "meta.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// OrbStack switches the CLI to its own context; the socket it names wins
// over any other daemon that happens to be around.
func TestDetectDockerFollowsOrbStackContext(t *testing.T) {
	home := shortHome(t)
	orb := filepath.Join(home, ".orbstack", "run", "docker.sock")
	listen(t, orb)
	listen(t, filepath.Join(home, ".docker", "run", "docker.sock")) // Docker Desktop, also installed
	writeContext(t, home, "orbstack", "unix://"+orb)

	if got := DetectDocker(home, env(nil)).Socket; got != orb {
		t.Fatalf("Socket = %q, want OrbStack's %q", got, orb)
	}
}

// Docker Desktop on Linux: context "desktop-linux", socket under ~/.docker/desktop.
func TestDetectDockerFollowsDockerDesktopContext(t *testing.T) {
	home := shortHome(t)
	desktop := filepath.Join(home, ".docker", "desktop", "docker.sock")
	listen(t, desktop)
	writeContext(t, home, "desktop-linux", "unix://"+desktop)

	if got := DetectDocker(home, env(nil)).Socket; got != desktop {
		t.Fatalf("Socket = %q, want %q", got, desktop)
	}
}

// DOCKER_CONTEXT beats config.json, and DOCKER_HOST beats both — the CLI's
// own precedence.
func TestDetectDockerPrecedence(t *testing.T) {
	home := shortHome(t)
	a := filepath.Join(home, "a.sock")
	b := filepath.Join(home, "b.sock")
	c := filepath.Join(home, "c.sock")
	listen(t, a)
	listen(t, b)
	listen(t, c)
	writeContext(t, home, "ctx-a", "unix://"+a)
	writeContext(t, home, "ctx-b", "unix://"+b) // config.json now says ctx-b
	writeContext(t, home, "ctx-a", "unix://"+a) // and back to ctx-a

	if got := DetectDocker(home, env(map[string]string{"DOCKER_CONTEXT": "ctx-b"})).Socket; got != b {
		t.Fatalf("with DOCKER_CONTEXT: Socket = %q, want %q", got, b)
	}
	if got := DetectDocker(home, env(map[string]string{"DOCKER_CONTEXT": "ctx-b", "DOCKER_HOST": "unix://" + c})).Socket; got != c {
		t.Fatalf("with DOCKER_HOST: Socket = %q, want %q", got, c)
	}
	// A tcp:// host is network, which the jail already has: nothing to grant.
	if got := DetectDocker(home, env(map[string]string{"DOCKER_HOST": "tcp://10.0.0.1:2376"})).Socket; got != "" {
		t.Fatalf("tcp DOCKER_HOST: Socket = %q, want none", got)
	}
}

// With no context, the desktop runtimes' well-known sockets are found, and a
// symlink (Docker Desktop's /var/run/docker.sock) resolves to its target.
func TestDetectDockerFallsBackToKnownSocketsAndResolvesSymlinks(t *testing.T) {
	home := shortHome(t)
	withoutSystemDocker(t)
	real := filepath.Join(home, ".docker", "run", "docker.sock")
	listen(t, real)
	if got := detectDockerSocket(home, env(nil)); got != real {
		t.Fatalf("Socket = %q, want Docker Desktop's %q", got, real)
	}

	link := filepath.Join(home, "link.sock")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if got := unixSocket("unix://" + link); got != real {
		t.Fatalf("unixSocket(link) = %q, want the target %q", got, real)
	}
	// A regular file is not a socket.
	plain := filepath.Join(home, "plain")
	if err := os.WriteFile(plain, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := existingSocket(plain); got != "" {
		t.Fatalf("existingSocket(file) = %q, want none", got)
	}
}

// Docker Desktop and OrbStack install `docker` as a symlink into their app
// bundle; the bundle has to be readable for the CLI to run at all, and the
// link's directory on PATH for it to be found from a Dock-launched app.
func TestDockerCLIGrantsTheAppBundle(t *testing.T) {
	home := shortHome(t)
	bin := filepath.Join(home, "Applications", "OrbStack.app", "Contents", "MacOS", "xbin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	links := filepath.Join(home, ".orbstack", "bin")
	if err := os.MkdirAll(links, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(bin, "docker"), filepath.Join(links, "docker")); err != nil {
		t.Fatal(err)
	}

	readOnly, pathDirs := dockerCLI(home, env(map[string]string{"PATH": links}))
	if !slices.Equal(pathDirs, []string{links}) {
		t.Fatalf("pathDirs = %v, want [%s]", pathDirs, links)
	}
	if want := filepath.Join(home, "Applications", "OrbStack.app"); !slices.Equal(readOnly, []string{want}) {
		t.Fatalf("readOnly = %v, want [%s]", readOnly, want)
	}
}

func TestPathWithAppendsOnlyWhatIsMissing(t *testing.T) {
	if got := pathWith("/usr/bin:/bin", []string{"/usr/bin"}); got != "" {
		t.Fatalf("pathWith = %q, want no change", got)
	}
	if got := pathWith("/usr/bin:/bin", []string{"/usr/local/bin"}); got != "/usr/bin:/bin:/usr/local/bin" {
		t.Fatalf("pathWith = %q", got)
	}
}

// Everything in the data directory is hidden but what is kept — the agent
// runtimes, which the jail has to run — and the known secrets are listed
// even before they exist.
func TestProtectDataDir(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"runtimes", "devkit.db", "something-new"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got := ProtectDataDir(dir, "runtimes")
	for _, want := range []string{"devkit.db", "devkit.db-wal", "agents", "mcp.sock", "something-new"} {
		if !slices.Contains(got, filepath.Join(dir, want)) {
			t.Errorf("missing %s in %v", want, got)
		}
	}
	if slices.Contains(got, filepath.Join(dir, "runtimes")) {
		t.Errorf("runtimes must stay reachable: %v", got)
	}
}

// The accounts' homes are hidden before the first one exists: a session that
// is already running must not see a directory created later, and Seatbelt
// denies a path that is not there. A name passed in keep is still excluded.
func TestProtectDataDirHidesAgentAccountsBeforeTheyExist(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, "agent-accounts")
	if _, err := os.Stat(want); err == nil {
		t.Fatal("precondition: agent-accounts must not exist")
	}
	if got := ProtectDataDir(dir, "runtimes"); !slices.Contains(got, want) {
		t.Errorf("agent-accounts missing from %v", got)
	}
	if got := ProtectDataDir(dir, "agent-accounts"); slices.Contains(got, want) {
		t.Errorf("a kept name must be excluded: %v", got)
	}
}

// A vendor's home inside the hidden data directory is carved out for that
// vendor's sessions: the path down to it opens, everything beside it on the
// way stays hidden, and deny paths that do not hold it are left alone.
func TestCarveOutKeepsOnlyThePathToTheHome(t *testing.T) {
	data := t.TempDir()
	home := filepath.Join(data, "agent-homes", "codex")
	for _, dir := range []string{home, filepath.Join(data, "agent-homes", "claude"), filepath.Join(data, "checkpoints")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	deny := ProtectDataDir(data, "runtimes")
	got := carveOut(deny, home)
	for _, hidden := range []string{
		filepath.Join(data, "agent-homes", "claude"),
		filepath.Join(data, "checkpoints"),
		filepath.Join(data, "devkit.db"),
	} {
		if !slices.Contains(got, hidden) {
			t.Errorf("%s must stay hidden: %v", hidden, got)
		}
	}
	for _, open := range []string{filepath.Join(data, "agent-homes"), home} {
		if slices.Contains(got, open) {
			t.Errorf("%s is on the way to the home and must not be hidden: %v", open, got)
		}
	}
	if other := carveOut([]string{"/elsewhere"}, home); !slices.Equal(other, []string{"/elsewhere"}) {
		t.Errorf("a deny path that does not hold the home changed: %v", other)
	}
}
