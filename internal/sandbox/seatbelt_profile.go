package sandbox

import (
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

	b.WriteString("(allow file-read* file-write*\n")
	writeSBPLSubpath(&b, p.WorkDir)
	for _, path := range p.ReadWritePaths {
		writeSBPLSubpath(&b, path)
	}
	if p.EffectivePrivateHome() {
		writeSBPLSubpath(&b, "/tmp")
		writeSBPLSubpath(&b, "/private/tmp")
	}
	b.WriteString(")\n")

	if len(p.ReadOnlyPaths) > 0 {
		b.WriteString("(allow file-read*\n")
		for _, path := range p.ReadOnlyPaths {
			writeSBPLSubpath(&b, path)
		}
		b.WriteString(")\n")
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
		// model API). Outbound sockets plus the DNS/config system services are
		// allowed; verified live against sandbox-exec on macOS — getaddrinfo
		// resolves through the /var/run/mDNSResponder unix socket, which needs
		// the /var read grant (deny rules below still mask sensitive subtrees).
		b.WriteString("(allow network-outbound)\n")
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

	denyPaths := p.EffectiveDenyRead()
	if len(denyPaths) > 0 {
		b.WriteString("(deny file-read*\n")
		for _, path := range denyPaths {
			writeSBPLSubpath(&b, path)
		}
		b.WriteString(")\n")
	}

	return b.String()
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
