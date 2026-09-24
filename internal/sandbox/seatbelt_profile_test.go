package sandbox

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSeatbeltProfileAllowsWorkDir(t *testing.T) {
	prof := buildSeatbeltProfile(Policy{WorkDir: "/tmp/proj", Network: NetworkNone}.withDefaults())
	if !strings.Contains(prof, "/tmp/proj") {
		t.Fatal(prof)
	}
	if !strings.Contains(prof, "(deny network-outbound)") {
		t.Fatalf("expected network-outbound denial in profile: %s", prof)
	}
	if !strings.Contains(prof, "(deny network-inbound)") {
		t.Fatalf("expected network-inbound denial in profile: %s", prof)
	}
}

func TestSeatbeltProfileDenyReadPaths(t *testing.T) {
	prof := buildSeatbeltProfile(Policy{
		WorkDir:       "/tmp/proj",
		DenyReadPaths: []string{"/Users/alice/.ssh"},
		Env:           map[string]string{"HOME": "/Users/alice"},
	}.withDefaults())
	if !strings.Contains(prof, "/Users/alice/.ssh") {
		t.Fatalf("expected deny path in profile: %s", prof)
	}
	if !strings.Contains(prof, "(deny file-read* file-write*") {
		t.Fatalf("expected deny rule to cover writes as well as reads: %s", prof)
	}
	// The deny must come after every allow: the later matching rule wins.
	if strings.LastIndex(prof, "(allow ") > strings.Index(prof, "(deny file-read* file-write*") {
		t.Fatalf("deny block must be emitted after all allow rules: %s", prof)
	}
	// Ancestors of a denied path must not be renamable, or the subpath deny
	// can be sidestepped by moving the parent.
	for _, dir := range []string{"/Users/alice", "/Users"} {
		want := `(deny file-write-unlink (require-all (vnode-type DIRECTORY) (literal "` + dir + `")))`
		if !strings.Contains(prof, want) {
			t.Fatalf("expected ancestor rename protection for %s: %s", dir, prof)
		}
	}
	if strings.Contains(prof, `(literal "/")))`) {
		t.Fatalf("root must not be listed as an ancestor: %s", prof)
	}
}

func TestSeatbeltProfileNetworkHostAllowsOutbound(t *testing.T) {
	prof := buildSeatbeltProfile(Policy{WorkDir: "/tmp/proj", Network: NetworkHost}.withDefaults())
	if strings.Contains(prof, "(allow network-outbound)") {
		t.Fatalf("bare network-outbound also allows AF_UNIX (docker.sock, ssh-agent); must be filtered: %s", prof)
	}
	if !strings.Contains(prof, `(allow network-outbound (remote ip "*:*"))`) {
		t.Fatalf("expected IP-only network-outbound allowance for NetworkHost: %s", prof)
	}
	if !strings.Contains(prof, `(allow network-outbound (literal "/private/var/run/mDNSResponder"))`) {
		t.Fatalf("expected the mDNSResponder unix socket to stay reachable for DNS: %s", prof)
	}
	if strings.Contains(prof, "(deny network-outbound)") {
		t.Fatalf("NetworkHost must not deny outbound: %s", prof)
	}
	// DNS + logging mach services must be reachable under (deny default).
	for _, service := range seatbeltNetworkMachServices {
		if !strings.Contains(prof, service) {
			t.Fatalf("expected mach-lookup for %q: %s", service, prof)
		}
	}
}

// A host-networking jail can check a server's certificate against the
// system roots, and that is all the securityd lookup buys it: the login
// keychain stays unreadable unless AllowKeychain asks for it, and a jail with
// no network gets neither.
func TestSeatbeltProfileNetworkHostCanCheckCertificates(t *testing.T) {
	const lookup = `(global-name "com.apple.SecurityServer")`
	host := buildSeatbeltProfile(Policy{WorkDir: "/tmp/proj", Network: NetworkHost, Env: map[string]string{"HOME": "/Users/alice"}}.withDefaults())
	if !strings.Contains(host, lookup) {
		t.Fatalf("NetworkHost must reach securityd to verify TLS: %s", host)
	}
	if strings.Contains(host, "Library/Keychains") {
		t.Fatalf("certificate checks must not open the login keychain: %s", host)
	}
	none := buildSeatbeltProfile(Policy{WorkDir: "/tmp/proj", Env: map[string]string{"HOME": "/Users/alice"}}.withDefaults())
	if strings.Contains(none, lookup) {
		t.Fatalf("a jail with no network has no certificate to check: %s", none)
	}
}

func TestSeatbeltProfileAllowUnixSocketsIsResolvedAndAfterNetworkDeny(t *testing.T) {
	// /tmp resolves to /private/tmp on macOS; Seatbelt matches the resolved
	// path, so the emitted literal must be the resolved one.
	dir, err := os.MkdirTemp("/tmp", "sb")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	resolved, _ := filepath.EvalSymlinks(sock)

	prof := buildSeatbeltProfile(Policy{WorkDir: "/tmp/proj", Network: NetworkNone, AllowUnixSockets: []string{sock}}.withDefaults())
	want := `(allow network-outbound (literal "` + resolved + `"))`
	if !strings.Contains(prof, want) {
		t.Fatalf("expected %s in profile:\n%s", want, prof)
	}
	if strings.Index(prof, want) < strings.Index(prof, "(deny network-outbound)") {
		t.Fatalf("socket allow must come after the NetworkNone deny (later rule wins):\n%s", prof)
	}
}

func TestSeatbeltProfileEmitsResolvedFormOfSymlinkedPaths(t *testing.T) {
	// Seatbelt matches resolved paths. nvm installs npx as a symlink into the
	// npm lib tree, so a grant on the symlink alone never fires.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(root, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	prof := buildSeatbeltProfile(Policy{WorkDir: link, ReadOnlyPaths: []string{link}, Network: NetworkNone}.withDefaults())
	for _, want := range []string{`(subpath "` + link + `")`, `(subpath "` + real + `")`} {
		if !strings.Contains(prof, want) {
			t.Fatalf("expected %s in profile:\n%s", want, prof)
		}
	}
}

func TestSeatbeltProfileDoesNotDuplicatePlainPaths(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	prof := buildSeatbeltProfile(Policy{WorkDir: dir, Network: NetworkNone}.withDefaults())
	if got := strings.Count(prof, `(subpath "`+dir+`")`); got != 1 {
		t.Fatalf("non-symlinked WorkDir emitted %d times, want 1:\n%s", got, prof)
	}
}

func TestSeatbeltProfileDenyIsResolvedToo(t *testing.T) {
	// The deny block must be symlink-resolved for the same reason the allow
	// block is: an unresolved deny never matches, so a resolved allow on an
	// ancestor would win and expose the subtree.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(root, "real")
	if err := os.MkdirAll(real, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "secrets")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	prof := buildSeatbeltProfile(Policy{
		WorkDir: root, DenyReadPaths: []string{link}, Env: map[string]string{"HOME": root},
	}.withDefaults())
	denyBlock := prof[strings.Index(prof, "(deny file-read* file-write*"):]
	for _, want := range []string{`(subpath "` + link + `")`, `(subpath "` + real + `")`} {
		if !strings.Contains(denyBlock, want) {
			t.Fatalf("expected %s inside the deny block:\n%s", want, denyBlock)
		}
	}
}

// A shared home is granted whole, read-write, and the built-in deny list is
// still emitted after it so ~/.ssh stays out. Private, home is not granted —
// but /tmp is either way.
func TestSeatbeltProfileSharedHome(t *testing.T) {
	off := false
	prof := buildSeatbeltProfile(Policy{
		WorkDir:     "/tmp/proj",
		Env:         map[string]string{"HOME": "/Users/alice"},
		PrivateHome: &off,
	}.withDefaults())
	grant := strings.Index(prof, `(subpath "/Users/alice")`)
	deny := strings.Index(prof, "(deny file-read* file-write*")
	if grant < 0 {
		t.Fatalf("expected the shared home to be granted: %s", prof)
	}
	if deny < grant || !strings.Contains(prof[deny:], "/Users/alice/.ssh") {
		t.Fatalf("expected ~/.ssh denied after the home grant: %s", prof)
	}
	if !strings.Contains(prof, `(subpath "/private/tmp")`) {
		t.Fatalf("expected /tmp with a shared home: %s", prof)
	}

	private := buildSeatbeltProfile(Policy{
		WorkDir: "/tmp/proj",
		Env:     map[string]string{"HOME": "/Users/alice"},
	}.withDefaults())
	if strings.Contains(private, `(subpath "/Users/alice")`) {
		t.Fatalf("a private home must not be granted: %s", private)
	}
}
