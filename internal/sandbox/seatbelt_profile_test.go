package sandbox

import (
	"errors"
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

func TestSeatbeltProfileAllowsOnlyMetadataOnPrivateParent(t *testing.T) {
	prof := buildSeatbeltProfile(Policy{WorkDir: "/tmp/proj", Network: NetworkNone}.withDefaults())
	if !strings.Contains(prof, `(allow file-read-metadata (literal "/private"))`) {
		t.Fatal("profile must allow metadata lookup of /private")
	}
	if strings.Contains(prof, `(allow file-read-data (literal "/private"))`) || strings.Contains(prof, `(subpath "/private")`) {
		t.Fatal("/private must not grant directory contents")
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

// Host networking used to read all of /var and /private/var, which listed the
// user's /var/folders temp and cache trees, /var/log and /var/db. DNS needs
// only the path walk to the resolver socket, resolv.conf and the timezone
// database (measured on a macOS host: without the metadata rule curl, Node and
// Go fail to resolve; without the timezone rule the jail's clock is UTC).
func TestSeatbeltProfileNetworkHostDoesNotReadAllOfVar(t *testing.T) {
	host := buildSeatbeltProfile(Policy{WorkDir: "/work", Network: NetworkHost}.withDefaults())
	for _, broad := range []string{
		`(allow file-read* (subpath "/var"))`,
		`(allow file-read* (subpath "/private/var"))`,
	} {
		if strings.Contains(host, broad) {
			t.Fatalf("host networking must not read the whole of /var: %s", broad)
		}
	}
	for _, rule := range []string{
		`(allow file-read-metadata (literal "/private/var") (literal "/private/var/run") (literal "/var") (literal "/var/run"))`,
		`(allow file-read* (literal "/private/var/run/resolv.conf") (literal "/var/run/resolv.conf"))`,
		`(allow file-read* (subpath "/private/var/db/timezone") (subpath "/var/db/timezone"))`,
		`(deny file-read* (subpath "/private/var/root"))`,
		`(deny file-read* (subpath "/private/var/db/Keychains"))`,
	} {
		if !strings.Contains(host, rule) {
			t.Fatalf("missing %s: %s", rule, host)
		}
	}
	if none := buildSeatbeltProfile(Policy{WorkDir: "/work", Network: NetworkNone}.withDefaults()); strings.Contains(none, "resolv.conf") || strings.Contains(none, "db/timezone") {
		t.Fatalf("a jail with no network must not gain the DNS and timezone reads: %s", none)
	}
}

// A session with its own scratch directory gets no shared /tmp: it reaches the
// directory by a stat of /private/tmp and nothing else there. A policy without
// one keeps the shared tree, because its TMPDIR is /tmp.
func TestSeatbeltProfileSharedTempOnlyWithoutSessionTemp(t *testing.T) {
	const session = "/private/tmp/dk-12345678"
	own := buildSeatbeltProfile(Policy{WorkDir: "/work", SessionTempDir: session, Network: NetworkHost}.withDefaults())
	for _, shared := range []string{`(subpath "/tmp")`, `(subpath "/private/tmp")`} {
		if strings.Contains(own, shared) {
			t.Fatalf("a session with its own temp directory must not get the shared %s: %s", shared, own)
		}
	}
	for _, rule := range []string{
		`(allow file-read-metadata (literal "/private/tmp"))`,
		`(allow file-read* file-write* (subpath "` + session + `"))`,
	} {
		if !strings.Contains(own, rule) {
			t.Fatalf("missing %s: %s", rule, own)
		}
	}
	if strings.Contains(own, `(allow file-read-data (literal "/private/tmp"))`) {
		t.Fatal("the shared temp directory must not be listable")
	}
	shared := buildSeatbeltProfile(Policy{WorkDir: "/work", Network: NetworkHost}.withDefaults())
	for _, rule := range []string{`(subpath "/tmp")`, `(subpath "/private/tmp")`} {
		if !strings.Contains(shared, rule) {
			t.Fatalf("without a session directory TMPDIR is /tmp and must stay writable, missing %s: %s", rule, shared)
		}
	}
	if strings.Contains(shared, `(literal "/private/tmp"))`) {
		t.Fatalf("no session directory, no extra stat rule: %s", shared)
	}
}

func TestSeatbeltPreferencesAreOptInAndReadOnly(t *testing.T) {
	base := Policy{WorkDir: "/tmp/proj", Network: NetworkNone}
	if prof := buildSeatbeltProfile(base); strings.Contains(prof, "cfprefsd") || strings.Contains(prof, "user-preference-read") {
		t.Fatal("preferences must stay closed by default")
	}
	base.ReadPreferenceDomains = []string{"com.openai.codex"}
	prof := buildSeatbeltProfile(base)
	if !strings.Contains(prof, `(preference-domain "com.openai.codex")`) ||
		!strings.Contains(prof, `(global-name "com.apple.cfprefsd.agent")`) {
		t.Fatalf("named preference domain unavailable: %s", prof)
	}
	if strings.Contains(prof, "user-preference-write") || strings.Contains(prof, "(allow user-preference-read)") {
		t.Fatalf("preferences grant must be scoped and read-only: %s", prof)
	}
}

// A host-networking jail can check a server's certificate against the
// system roots, and that is all the securityd lookup buys it: the login
// keychain stays unreadable unless AllowKeychain asks for it — also under a
// shared home, which grants ~/Library like the rest of $HOME — and a jail
// with no network gets no lookup.
func TestSeatbeltProfileNetworkHostCanCheckCertificates(t *testing.T) {
	const lookup = `(global-name "com.apple.SecurityServer")`
	keychains := `(subpath "/Users/alice/Library/Keychains")`
	host := buildSeatbeltProfile(Policy{WorkDir: "/tmp/proj", Network: NetworkHost, Env: map[string]string{"HOME": "/Users/alice"}}.withDefaults())
	if !strings.Contains(host, lookup) {
		t.Fatalf("NetworkHost must reach securityd to verify TLS: %s", host)
	}
	if !strings.Contains(host, `(global-name "com.apple.trustd.agent")`) {
		t.Fatalf("NetworkHost must reach trustd, which the platform verifier asks: %s", host)
	}
	none := buildSeatbeltProfile(Policy{WorkDir: "/tmp/proj", Env: map[string]string{"HOME": "/Users/alice"}}.withDefaults())
	if strings.Contains(none, lookup) {
		t.Fatalf("a jail with no network has no certificate to check: %s", none)
	}

	shared := buildSeatbeltProfile(Policy{WorkDir: "/tmp/proj", Network: NetworkHost, PrivateHome: boolPtr(false),
		Env: map[string]string{"HOME": "/Users/alice"}}.withDefaults())
	deny := strings.LastIndex(shared, "(deny file-read* file-write*")
	if deny < 0 || !strings.Contains(shared[deny:], keychains) {
		t.Fatalf("a shared home must not hand the login keychain to the jail: %s", shared)
	}
	if strings.LastIndex(shared, "(allow ") > deny {
		t.Fatalf("the keychain deny must come after every allow: %s", shared)
	}
	if !strings.Contains(shared, `(deny file-write-unlink (require-all (vnode-type DIRECTORY) (literal "/Users/alice/Library")))`) {
		t.Fatalf("renaming ~/Library must not move the keychain out from under the deny: %s", shared)
	}

	allowed := buildSeatbeltProfile(Policy{WorkDir: "/tmp/proj", Network: NetworkHost, PrivateHome: boolPtr(false), AllowKeychain: true,
		Env: map[string]string{"HOME": "/Users/alice"}}.withDefaults())
	if at := strings.LastIndex(allowed, "(deny file-read* file-write*"); at >= 0 && strings.Contains(allowed[at:], keychains) {
		t.Fatalf("AllowKeychain must keep the keychain readable: %s", allowed)
	}
}

func TestSeatbeltProfileAllowUnixSocketsIsResolvedAndAfterNetworkDeny(t *testing.T) {
	// /tmp resolves to /private/tmp on macOS; Seatbelt matches the resolved
	// path, so the emitted literal must be the resolved one.
	dir := t.TempDir()
	sock := filepath.Join(dir, "s.sock")
	// Profile generation only needs a resolvable path, not a live socket.
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	resolved := resolveDenyPath(sock)

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

// A sign-in callback listens on its one loopback port and no other; a policy
// that asks for none gets none, and a port that is not one is refused.
func TestSeatbeltProfileListensOnlyOnTheLoopbackPortsAsked(t *testing.T) {
	prof := buildSeatbeltProfile(Policy{WorkDir: "/tmp/proj", Network: NetworkHost, ListenLoopbackPorts: []int{1455}}.withDefaults())
	for _, want := range []string{
		`(allow network-bind (local ip "localhost:1455"))`,
		`(allow network-inbound (local ip "localhost:1455"))`,
	} {
		if !strings.Contains(prof, want) {
			t.Fatalf("missing %s: %s", want, prof)
		}
	}
	if strings.Count(prof, "network-bind") != 1 || strings.Count(prof, "network-inbound") != 1 {
		t.Fatalf("only the one port may listen: %s", prof)
	}
	if none := buildSeatbeltProfile(Policy{WorkDir: "/tmp/proj", Network: NetworkHost}.withDefaults()); strings.Contains(none, "network-bind") || strings.Contains(none, "network-inbound") {
		t.Fatalf("no listener unless asked: %s", none)
	}
	for _, port := range []int{0, -1, 65536} {
		if err := (Policy{WorkDir: "/tmp", ListenLoopbackPorts: []int{port}}).Validate(); !errors.Is(err, ErrInvalidPolicy) {
			t.Errorf("port %d: Validate = %v, want ErrInvalidPolicy", port, err)
		}
	}
}

func TestSeatbeltAncestorsAreStatOnlyUnlessAskedToBeReadable(t *testing.T) {
	work := "/Users/someone/Workspace/project"
	private := true
	plain := buildSeatbeltProfile(Policy{WorkDir: work, PrivateHome: &private})
	for _, dir := range []string{"/Users", "/Users/someone", "/Users/someone/Workspace"} {
		if !strings.Contains(plain, `(allow file-read-metadata (literal "`+dir+`"))`) {
			t.Errorf("no stat of %s: resolving a path walks it\n%s", dir, plain)
		}
	}
	if strings.Contains(plain, "file-read-data (literal") {
		t.Fatalf("ancestors are listable without being asked:\n%s", plain)
	}

	readable := buildSeatbeltProfile(Policy{WorkDir: work, PrivateHome: &private, ReadAncestors: true})
	for _, dir := range []string{"/Users", "/Users/someone", "/Users/someone/Workspace"} {
		if !strings.Contains(readable, `(allow file-read-data (literal "`+dir+`"))`) {
			t.Errorf("ReadAncestors did not make %s readable", dir)
		}
	}
	// The directory itself is not an ancestor of itself: its grant is the subpath one.
	if strings.Contains(readable, `(allow file-read-data (literal "`+work+`"))`) {
		t.Error("the working directory is listed as its own ancestor")
	}
	// Nothing but the ancestors: no write, no broader read.
	if strings.Contains(readable, `file-write* (literal "/Users"`) || strings.Contains(readable, `(allow file-read* (literal "/Users"`) {
		t.Errorf("more than listing was granted:\n%s", readable)
	}
}
