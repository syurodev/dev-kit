package sandbox

import (
	"path/filepath"
	"sort"
	"strings"
)

// Typical read-only system roots for agent-style sandboxes on macOS. /etc is
// a symlink to /private/etc, so both spellings are listed — TLS config and CA
// bundles live under the latter.
var seatbeltSystemReadOnlyPaths = []string{
	"/usr",
	"/bin",
	"/lib",
	"/System/Library",
	"/Library/Frameworks",
	"/private/var/db/dyld",
	"/etc",
	"/private/etc",
}

// seatbeltNetworkMachServices are the system services a host-networking
// process needs under (deny default): network configuration (configd), the
// classic and modern DNS responders, and the unified logging daemons. This
// mirrors the service sets used by WebKit/Chromium sandbox profiles.
var seatbeltNetworkMachServices = []string{
	"com.apple.SystemConfiguration.configd",
	"com.apple.mDNSResponder",
	"com.apple.dns.service",
	"com.apple.dns.registration",
	"com.apple.system.logger",
	"com.apple.system.notification_center",
}

// buildSeatbeltProfile returns an SBPL profile string for sandbox-exec.
func buildSeatbeltProfile(p Policy) string {
	p = p.withDefaults()

	var b strings.Builder
	b.WriteString("(version 1)\n")
	b.WriteString("(deny default)\n")

	b.WriteString("(allow file-read*\n")
	for _, path := range seatbeltSystemReadOnlyPaths {
		writeSBPLSubpath(&b, path)
	}
	b.WriteString("  (literal \"/\")\n")
	b.WriteString(")\n")

	// `2>/dev/null` and O_RDWR opens of /dev/null (ssh-add does this) fail
	// under (deny default) without this. vnode-type guard as in Codex's
	// seatbelt_base_policy.sbpl, so only the real character device matches.
	b.WriteString("(allow file-read* file-write* (require-all (literal \"/dev/null\") (vnode-type CHARACTER-DEVICE)))\n")

	b.WriteString("(allow file-read* file-write*\n")
	writeSBPLPath(&b, p.WorkDir)
	for _, path := range p.ReadWritePaths {
		writeSBPLPath(&b, path)
	}
	// /tmp whether or not home is shared: a CLI reaches for os.tmpdir()
	// either way, and TMPDIR points there (withHomeEnv).
	writeSBPLSubpath(&b, "/tmp")
	writeSBPLSubpath(&b, "/private/tmp")
	// A shared home is the whole of it, read-write. The deny rule emitted
	// last still takes ~/.ssh and the rest back out: SBPL's later match wins.
	if home := p.sharedHome(); home != "" {
		writeSBPLPath(&b, home)
	}
	b.WriteString(")\n")

	if len(p.ReadOnlyPaths) > 0 {
		b.WriteString("(allow file-read*\n")
		for _, path := range p.ReadOnlyPaths {
			writeSBPLPath(&b, path)
		}
		b.WriteString(")\n")
	}

	// macOS keychain. Measured minimum: the securityd mach service plus a read
	// on the login keychain store — either alone fails. Read-only: the agent
	// looks its own token up, it never has to write one back.
	if p.AllowKeychain {
		b.WriteString("(allow mach-lookup (global-name \"com.apple.SecurityServer\"))\n")
		if home := p.policyHomeDir(); home != "" {
			b.WriteString("(allow file-read*\n")
			writeSBPLPath(&b, filepath.Join(home, "Library", "Keychains"))
			b.WriteString(")\n")
		}
	}

	// Ancestor lookup. (subpath X) does not let a process stat X's parents, and
	// resolving a path walks every one of them: node exits with
	// `EPERM: lstat '/Users'` before it reads a single file it was granted.
	// Metadata only — this permits stat, never open/read of the directories.
	for _, dir := range pathAncestors(p.grantedPaths()) {
		b.WriteString("(allow file-read-metadata (literal \"" + escapeSBPL(dir) + "\"))\n")
	}

	b.WriteString("(allow process-exec)\n")
	b.WriteString("(allow process-fork)\n")
	b.WriteString("(allow sysctl-read)\n")

	switch p.Network {
	case NetworkNone:
		b.WriteString("(deny network-outbound)\n")
		b.WriteString("(deny network-inbound)\n")
	case NetworkHost:
		// Host networking is the policy's contract (ADR-009: agents need the
		// model API). Outbound is limited to IP sockets: a bare
		// (allow network-outbound) also covers AF_UNIX, which hands the agent
		// every unix socket on the host — docker.sock (= host root),
		// ssh-agent's launchd listener, and so on. The one unix socket the
		// process legitimately needs is mDNSResponder (getaddrinfo goes through
		// it, verified live), so that is allowed by literal path, the same way
		// Apple's own /System/Library/Sandbox/Profiles/*.sb do it. The /var
		// read grant below is what lets the path resolve.
		// ponytail: loopback TCP (local DBs, dev servers) is still reachable;
		// closing that needs a proxy architecture, not a profile rule.
		b.WriteString("(allow network-outbound (remote ip \"*:*\"))\n")
		b.WriteString("(allow network-outbound (literal \"/private/var/run/mDNSResponder\"))\n")
		b.WriteString("(allow file-read* (subpath \"/var\"))\n")
		b.WriteString("(allow file-read* (subpath \"/private/var\"))\n")
		for _, service := range seatbeltNetworkMachServices {
			b.WriteString("(allow mach-lookup (global-name \"" + service + "\"))\n")
		}
		b.WriteString("(deny file-read* (subpath \"/var/root\"))\n")
		b.WriteString("(deny file-read* (subpath \"/private/var/root\"))\n")
		b.WriteString("(deny file-read* (subpath \"/var/db/Keychains\"))\n")
		b.WriteString("(deny file-read* (subpath \"/private/var/db/Keychains\"))\n")
	}

	// The session's own MCP socket. Independent of NetworkMode (local IPC,
	// not network) and emitted after the NetworkNone deny because the later
	// rule wins. Seatbelt matches resolved paths — /tmp/x.sock is
	// /private/tmp/x.sock to the kernel — so resolve before emitting; with
	// the resolved literal the connect right alone suffices (verified live).
	for _, sock := range p.AllowUnixSockets {
		b.WriteString("(allow network-outbound (literal \"" + escapeSBPL(resolveDenyPath(sock)) + "\"))\n")
	}

	// Deny both operation classes: SBPL evaluates them independently, so a
	// read-only deny leaves the subtree writable whenever WorkDir or a RW path
	// is an ancestor of it (WorkDir=$HOME made ~/.ssh/authorized_keys
	// appendable). Emitted last because the later matching rule wins.
	// Deny-write after every allow: SBPL's later matching rule wins, so these
	// stay read-only inside WorkDir, a RW path or a shared home.
	if len(p.DenyWritePaths) > 0 {
		b.WriteString("(deny file-write*\n")
		for _, path := range p.DenyWritePaths {
			writeSBPLPath(&b, path)
		}
		b.WriteString(")\n")
	}

	denyPaths := p.EffectiveDenyRead()
	if len(denyPaths) > 0 {
		b.WriteString("(deny file-read* file-write*\n")
		for _, path := range denyPaths {
			writeSBPLPath(&b, path)
		}
		b.WriteString(")\n")
	}
	// (subpath X) stops protecting X once an ancestor is renamed: with
	// WorkDir=/Users, `mv /Users/me /Users/x` makes /Users/x/.ssh writable
	// (verified live). Deny rename/unlink of every ancestor directory, the
	// same rule Codex uses for its protected paths — for deny-write paths as
	// much as deny-read ones.
	protected := append(append([]string(nil), denyPaths...), p.DenyWritePaths...)
	for _, dir := range pathAncestors(protected) {
		b.WriteString("(deny file-write-unlink (require-all (vnode-type DIRECTORY) (literal \"" + escapeSBPL(dir) + "\")))\n")
	}

	return b.String()
}

// grantedPaths is every path the profile grants access to, in both spellings
// (as given and symlink-resolved), so ancestor lookup covers each.
func (p Policy) grantedPaths() []string {
	out := make([]string, 0, len(p.ReadWritePaths)+len(p.ReadOnlyPaths)+len(p.AllowUnixSockets)+1)
	add := func(path string) {
		if strings.TrimSpace(path) == "" {
			return
		}
		out = append(out, normalizePath(path))
		if resolved := resolveDenyPath(path); resolved != "" {
			out = append(out, resolved)
		}
	}
	add(p.WorkDir)
	add(p.sharedHome())
	for _, path := range p.ReadWritePaths {
		add(path)
	}
	for _, path := range p.ReadOnlyPaths {
		add(path)
	}
	for _, path := range p.AllowUnixSockets {
		add(path)
	}
	return out
}

// pathAncestors returns every ancestor directory of the given paths,
// excluding "/", deduplicated and sorted.
func pathAncestors(paths []string) []string {
	seen := map[string]struct{}{}
	for _, path := range paths {
		for dir := filepath.Dir(path); dir != "/" && dir != "."; dir = filepath.Dir(dir) {
			seen[dir] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for dir := range seen {
		out = append(out, dir)
	}
	sort.Strings(out)
	return out
}

// writeSBPLPath emits the grant for a bind path, plus its symlink-resolved
// form when they differ. Seatbelt matches the *resolved* path, so a grant on
// a symlink alone never fires: nvm's `bin/npx -> ../lib/node_modules/npm/bin/
// npx-cli.js` produced "Cannot find module .../bin/npx" inside the jail, and a
// project under a symlinked parent was silently read-only. Both spellings are
// emitted because callers (and Cmd.Dir) still use the unresolved one. Widening
// is bounded: Policy.validateBindPaths already resolves every bind path and
// rejects one that lands inside a deny path.
func writeSBPLPath(b *strings.Builder, path string) {
	writeSBPLSubpath(b, path)
	if resolved := resolveDenyPath(path); resolved != "" && resolved != normalizePath(path) {
		writeSBPLSubpath(b, resolved)
	}
}

func writeSBPLSubpath(b *strings.Builder, path string) {
	b.WriteString("  (subpath \"")
	b.WriteString(escapeSBPL(path))
	b.WriteString("\")\n")
}

func escapeSBPL(path string) string {
	path = strings.ReplaceAll(path, "\\", "\\\\")
	return strings.ReplaceAll(path, "\"", "\\\"")
}
