package agenthub

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// accountsDirName is the entry of the data directory that holds each added
// vendor account's home (<data>/agent-accounts/<vendor>/<id>).
const accountsDirName = "agent-accounts"

// AccountsDirOf is Service.AccountsDir for a data directory. carveOutAll matches
// paths lexically, so the accounts folder must be spelled exactly as
// ProtectDataDir spells its deny entry for it: both are filepath.Join(dataDir, …)
// of the same dataDir string, and main.go must pass the same one to both.
func AccountsDirOf(dataDir string) string {
	if dataDir == "" {
		return ""
	}
	return filepath.Join(dataDir, accountsDirName)
}

// dataDirSecrets are the entries of DevKit's data directory hidden even when
// they do not exist yet at session open: Seatbelt denies a path that is not
// there, and the database's WAL files come and go.
var dataDirSecrets = []string{
	"devkit.db", "devkit.db-wal", "devkit.db-shm", // the vault, sealed but offline-attackable
	"mcp.sock",        // the shared MCP socket, which carries no session identity
	"agents",          // every session's own MCP socket
	"checkpoints",     // shadow copies of every project checkpointed
	"consoles",        // saved query consoles
	"agent-documents", // PDFs attached to sessions: read through the pdf.* tools, scoped per session
	"pdf-engine",      // the PDF engine's compiled-module cache
	accountsDirName,   // each added account's own home: one account's session reads only its own
}

// ProtectDataDir is what to hide of DevKit's data directory from an agent
// session: everything in it except keep. Not the directory itself, because
// what is kept — the agent runtimes DevKit installs there — has to stay
// reachable, and a session's own MCP socket inside "agents" is granted back
// on its own (sandbox.Policy lets a socket sit under a caller's deny path).
//
// With a private $HOME none of this is visible anyway; it matters once home
// is shared, which is the default.
func ProtectDataDir(dataDir string, keep ...string) []string {
	if dataDir == "" {
		return nil
	}
	names := append([]string(nil), dataDirSecrets...)
	if entries, err := os.ReadDir(dataDir); err == nil {
		for _, entry := range entries {
			if !slices.Contains(names, entry.Name()) {
				names = append(names, entry.Name())
			}
		}
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		if slices.Contains(keep, name) {
			continue
		}
		out = append(out, filepath.Join(dataDir, name))
	}
	return out
}

// carveOut returns deny with one path inside it made reachable again: each
// deny path that holds keep is replaced by its entries, except the one on the
// way to keep, one level at a time down to keep itself.
//
// For a vendor whose home lives in DevKit's data directory, which every
// session hides. Keeping the home in ProtectDataDir's list instead would hand
// it to every vendor — a Claude session would read Codex's auth.json — so the
// adapter that owns the home carves it out of its own sessions' deny list and
// nobody else's. Entries are listed when the session opens; one created later
// beside the path is not hidden from that session.
func carveOut(deny []string, keep string) []string {
	out := make([]string, 0, len(deny))
	for _, path := range deny {
		if !pathIsInside(keep, path) {
			out = append(out, path)
			continue
		}
		out = append(out, siblingsOnTheWay(path, keep)...)
	}
	return out
}

// siblingsOnTheWay lists everything under dir except the path down to keep.
func siblingsOnTheWay(dir, keep string) []string {
	rel, err := filepath.Rel(dir, keep)
	if err != nil {
		return []string{dir}
	}
	next, rest, _ := strings.Cut(filepath.ToSlash(rel), "/")
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		// Unlistable is not empty: keep the whole directory hidden. The home
		// inside it then cannot be granted, and the spawn fails closed.
		return []string{dir}
	}
	var out []string
	for _, entry := range entries {
		if entry.Name() != next {
			out = append(out, filepath.Join(dir, entry.Name()))
		}
	}
	if rest != "" {
		out = append(out, siblingsOnTheWay(filepath.Join(dir, next), keep)...)
	}
	return out
}

// pathIsInside reports whether path lies strictly below dir.
func pathIsInside(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// carveOutAll is carveOut for several kept paths at once: every deny path that
// holds one of keeps is replaced by its entries, recursively, except the way
// down to each kept path. Calling carveOut once per kept path cannot do this
// when two kept paths share a deny root: after the first call the second kept
// path is already in the output as a denied sibling, and carveOut does not
// carve a path equal to the keep, so it stays denied.
//
// It differs from carveOut in three ways, all deliberate:
//   - a kept path that equals a deny path is allowed (the entry is dropped);
//     carveOut keeps denying it.
//   - a deny directory that does not exist, and holds a kept path, is denied
//     whole; carveOut returns nothing for it. A kept path under a directory that
//     cannot be listed cannot be reached, so the spawn fails closed.
//   - matching is lexical. A keep and a deny spelled differently (a symlinked
//     data directory, macOS /var against /private/var) do not match, so the deny
//     directory stays denied whole. That fails closed, but it means callers must
//     build keeps and denies from the same base string.
//
// Entries are listed when a session opens; an entry created later beside a kept
// path is not hidden from that session (as with carveOut). An unlistable
// directory stays hidden whole.
func carveOutAll(deny []string, keeps ...string) []string {
	var out []string
	for _, path := range deny {
		out = append(out, carveKeeps(path, keeps)...)
	}
	return out
}

// carveKeeps returns what of dir stays denied once every kept path is
// reachable: dir itself when no kept path is inside it, nothing when dir is a
// kept path, else the entries of dir carved the same way.
func carveKeeps(dir string, keeps []string) []string {
	var inside []string
	for _, keep := range keeps {
		if keep == dir {
			return nil
		}
		if pathIsInside(keep, dir) {
			inside = append(inside, keep)
		}
	}
	if len(inside) == 0 {
		return []string{dir}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []string{dir}
	}
	var out []string
	for _, entry := range entries {
		out = append(out, carveKeeps(filepath.Join(dir, entry.Name()), inside)...)
	}
	return out
}
