//go:build linux

package sandbox

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var bwrapSystemReadOnlyPaths = []string{"/usr", "/bin", "/lib", "/lib64", "/etc"}

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

	args = append(args, "--bind", p.WorkDir, p.WorkDir)
	for _, path := range p.ReadWritePaths {
		if pathExists(path) {
			args = append(args, "--bind", path, path)
		}
	}
	for _, path := range p.ReadOnlyPaths {
		if pathExists(path) {
			args = append(args, "--ro-bind", path, path)
		}
	}

	denyPaths := p.EffectiveDenyRead()
	for _, path := range denyPaths {
		appendDenyReadMount(&args, path)
	}

	home := p.policyHomeDir()
	if p.EffectivePrivateHome() && home != "" {
		args = append(args, "--tmpfs", home)
		if pathExists(p.WorkDir) && pathIsContainedIn(p.WorkDir, home) && !pathIsDenied(p.WorkDir, denyPaths) {
			args = append(args, "--bind", p.WorkDir, p.WorkDir)
		}
		appendHomeAllowlist(&args, home, denyPaths, p.ReadWritePaths, "--bind")
		appendHomeAllowlist(&args, home, denyPaths, p.ReadOnlyPaths, "--ro-bind")
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

func appendHomeAllowlist(args *[]string, home string, denyPaths []string, paths []string, flag string) {
	for _, path := range paths {
		if !pathExists(path) || !pathIsUnder(path, home) || pathIsDenied(path, denyPaths) {
			continue
		}
		*args = append(*args, flag, path, path)
	}
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
