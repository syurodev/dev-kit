//go:build linux

package sandbox

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var bwrapSystemReadOnlyPaths = []string{"/usr", "/bin", "/lib", "/lib64", "/etc"}

// resolvConfPath is where glibc reads its nameservers. A var so a test can
// point it at a fixture rather than the machine's real one.
var resolvConfPath = "/etc/resolv.conf"

// appendResolvConf binds the file /etc/resolv.conf actually resolves to, when
// that is somewhere the jail cannot otherwise see.
//
// /etc is bound, which looks like enough until the host runs systemd-resolved:
// there /etc/resolv.conf is a symlink to /run/systemd/resolve/stub-resolv.conf,
// /run is bound nowhere, and the symlink dangles. glibc then finds no
// nameserver at all and every lookup in the jail fails — measured: `getent
// hosts github.com` returns nothing inside a jail built exactly like this one,
// and an agent that starts perfectly well sits retrying a name it can never
// resolve, producing no output and no error anyone can see.
//
// Only the resolved file is bound, at its own path, rather than /run wholesale:
// /run also carries the session bus, the keyring and every pid file on the
// machine, which this jail has no business seeing. A resolv.conf that is a real
// file (or a symlink within /etc) is already covered by the /etc bind and adds
// nothing here.
func appendResolvConf(args *[]string, p Policy) {
	if p.Network == NetworkNone {
		return // no network, no names to resolve
	}
	target, err := filepath.EvalSymlinks(resolvConfPath)
	if err != nil {
		return
	}
	for _, root := range bwrapSystemReadOnlyPaths {
		if target == root || pathIsUnder(target, root) {
			return
		}
	}
	*args = append(*args, "--ro-bind", target, target)
}

func buildBwrapArgs(p Policy, c Cmd) []string {
	p = p.withDefaults()

	args := []string{
		"--unshare-user",
		"--unshare-pid",
		"--unshare-ipc",
		"--die-with-parent",
	}
	if p.Network == NetworkNone {
		args = append(args, "--unshare-net")
	}

	args = append(args, "--dev", "/dev", "--proc", "/proc")
	for _, path := range bwrapSystemReadOnlyPaths {
		if pathExists(path) {
			args = append(args, "--ro-bind", path, path)
		}
	}
	appendResolvConf(&args, p)

	// withPrivateHomeEnv (below) sets TMPDIR=/tmp whenever it isn't already
	// overridden, but bwrap starts from an empty mount namespace, so without
	// this the jail promises a temp directory that plainly does not exist.
	// A fresh tmpfs, not a bind of the host's real /tmp: unlike
	// WorkDir/ReadWritePaths, /tmp is a shared, unauthenticated dump
	// everything on the host uses, and binding it for real would leak every
	// other process's temp files into the jail (a canary file written
	// straight to the host's /tmp, same as t.TempDir() defaults to, would
	// suddenly be readable — measured via TestSmokeAgentShapedProcess
	// catching exactly that). A private, empty /tmp is the same tradeoff
	// the seatbelt backend already makes by a different mechanism (see
	// writeSBPLSubpath in seatbelt_profile.go).
	//
	// Placed before WorkDir/ReadWritePaths/ReadOnlyPaths/sockets, not after:
	// any of those that happen to resolve under /tmp (t.TempDir() again, or
	// a project genuinely checked out under /tmp) must still bind on top of
	// this tmpfs, the same "punch a hole through the blanket mount" order
	// the private-home tmpfs below already relies on.
	//
	// Measured: a Node CLI that reaches for os.tmpdir() early in startup
	// (lockfiles, IPC sockets, npm/V8 caches) doesn't necessarily surface
	// the ENOENT as a visible error — it can sit retrying, which reads from
	// the outside as a session stuck on "starting" forever with no further
	// log line.
	if p.EffectivePrivateHome() {
		args = append(args, "--tmpfs", "/tmp")
	}

	binds := []bwrapBind{{"--bind", p.WorkDir}}
	for _, path := range p.ReadWritePaths {
		if pathExists(path) {
			binds = append(binds, bwrapBind{"--bind", path})
		}
	}
	for _, path := range p.ReadOnlyPaths {
		if pathExists(path) {
			binds = append(binds, bwrapBind{"--ro-bind", path})
		}
	}
	appendNestedBinds(&args, binds)

	denyPaths := p.EffectiveDenyRead()
	for _, path := range denyPaths {
		appendDenyReadMount(&args, path)
	}

	home := p.policyHomeDir()
	if p.EffectivePrivateHome() && home != "" {
		args = append(args, "--tmpfs", home)
		var homeBinds []bwrapBind
		if pathExists(p.WorkDir) && pathIsContainedIn(p.WorkDir, home) && !pathIsDenied(p.WorkDir, denyPaths) {
			homeBinds = append(homeBinds, bwrapBind{"--bind", p.WorkDir})
		}
		homeBinds = appendHomeAllowlist(homeBinds, home, denyPaths, p.ReadWritePaths, "--bind")
		homeBinds = appendHomeAllowlist(homeBinds, home, denyPaths, p.ReadOnlyPaths, "--ro-bind")
		appendNestedBinds(&args, homeBinds)
	}

	for _, sock := range p.AllowUnixSockets {
		if pathExists(sock) {
			args = append(args, "--bind", sock, sock)
		}
	}

	env := p.Env
	if p.EffectivePrivateHome() {
		env = withPrivateHomeEnv(p, env)
	}
	args = appendEnvArgs(args, env)
	args = append(args, "--chdir", p.EffectiveChdir(c), "--", c.Path)
	args = append(args, c.Args...)
	return args
}

func appendDenyReadMount(args *[]string, path string) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if info.IsDir() {
		*args = append(*args, "--tmpfs", path)
		return
	}
	*args = append(*args, "--bind", "/dev/null", path)
}

func appendHomeAllowlist(binds []bwrapBind, home string, denyPaths []string, paths []string, flag string) []bwrapBind {
	for _, path := range paths {
		if !pathExists(path) || !pathIsUnder(path, home) || pathIsDenied(path, denyPaths) {
			continue
		}
		binds = append(binds, bwrapBind{flag, path})
	}
	return binds
}

// bwrapBind is one --bind / --ro-bind of a path onto itself.
type bwrapBind struct {
	flag string
	path string
}

// appendNestedBinds emits binds shallowest path first, so a bind inside
// another one always lands on top of it whatever their modes.
//
// bwrap applies mounts in argv order and a later mount over the same subtree
// wins. Emitting every RW bind and then every RO one (the old order) meant a
// read-only parent silently re-covered a read-write child: a workspace root
// RO with one service inside it RW came out with the service read-only, the
// exact layout workspace.mdx's worktree tiers are built on. Seatbelt has no
// such ordering — its allow rules add up — so this is what makes the two
// backends agree.
//
// The sort is stable and callers list RW before RO, so the one case depth
// cannot decide — the same path given both ways — still resolves read-only,
// as it did before.
func appendNestedBinds(args *[]string, binds []bwrapBind) {
	sort.SliceStable(binds, func(i, j int) bool {
		return pathDepth(binds[i].path) < pathDepth(binds[j].path)
	})
	for _, b := range binds {
		*args = append(*args, b.flag, b.path, b.path)
	}
}

func pathDepth(path string) int {
	clean := filepath.Clean(path)
	if clean == string(filepath.Separator) {
		return 0
	}
	return strings.Count(clean, string(filepath.Separator))
}

func appendEnvArgs(args []string, env map[string]string) []string {
	args = append(args, "--clearenv")
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		args = append(args, "--setenv", key, env[key])
	}
	return args
}

func pathIsUnder(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func pathIsDenied(path string, denyPaths []string) bool {
	for _, denyPath := range denyPaths {
		if path == denyPath || pathIsUnder(path, denyPath) {
			return true
		}
	}
	return false
}
