package sandbox

import (
	"strings"
)

// Typical read-only system roots for agent-style sandboxes on macOS.
var seatbeltSystemReadOnlyPaths = []string{
	"/usr",
	"/bin",
	"/lib",
	"/System/Library",
	"/Library/Frameworks",
	"/private/var/db/dyld",
	"/etc",
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

	if p.Network == NetworkNone {
		b.WriteString("(deny network-outbound)\n")
		b.WriteString("(deny network-inbound)\n")
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
