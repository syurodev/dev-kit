package arch

import (
	"os/exec"
	"strings"
	"testing"
)

const modulePrefix = "devkit/"

func pkg(name string) string { return "devkit/internal/" + name }

// importGraph returns each devkit package's direct in-module (production)
// imports. Test-only imports are intentionally excluded: tests may wire any
// layer together, but production code must obey the architecture.
func importGraph(t *testing.T) map[string][]string {
	t.Helper()
	out, err := exec.Command("go", "list", "-f", "{{.ImportPath}}\t{{join .Imports \" \"}}", "devkit/...").CombinedOutput()
	if err != nil {
		t.Fatalf("go list failed: %v\n%s", err, out)
	}
	graph := map[string][]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.SplitN(line, "\t", 2)
		var imports []string
		if len(fields) == 2 {
			for _, imp := range strings.Fields(fields[1]) {
				if strings.HasPrefix(imp, modulePrefix) {
					imports = append(imports, imp)
				}
			}
		}
		graph[fields[0]] = imports
	}
	return graph
}

// TestLayering enforces the dependency direction. The rules mirror the "Code
// organization" section of the README: dependencies flow one way, the bridge is
// a thin boundary that never reaches storage/crypto, and app is the sole
// composition root imported only by main.
func TestLayering(t *testing.T) {
	graph := importGraph(t)

	bans := map[string][]string{
		// The bridge is the Wails boundary: it goes through app + services and
		// must never reach the storage, sync, or crypto layers directly.
		pkg("bridge"): {pkg("storage"), pkg("syncstore"), pkg("sync"), pkg("crypto")},
		// app is the composition root; it wires services and must not depend on
		// low-level persistence/crypto or on the bridge above it.
		pkg("app"): {pkg("storage"), pkg("syncstore"), pkg("sync"), pkg("crypto"), pkg("bridge")},
		// vaultitem is the shared substrate; domain modules depend on it, not the
		// other way around.
		pkg("vaultitem"): {
			pkg("notes"), pkg("database"), pkg("ssh"), pkg("kafka"), pkg("tools"),
			pkg("agenthub"), pkg("syncclient"), pkg("syncconflict"), pkg("bridge"), pkg("app"),
		},
		// environments is a shared registry substrate like vaultitem; ssh/database
		// consume it through their own locally-declared EnvLookup interface, never
		// the reverse.
		pkg("environments"): {
			pkg("ssh"), pkg("database"), pkg("kafka"), pkg("bridge"), pkg("app"),
		},
		// ssh/database must never import environments directly — this is the
		// central design decision behind their locally-declared EnvLookup
		// interfaces ("accept interfaces, return structs"), not just a
		// preference, so it is pinned the same way vaultitem's boundary is.
		pkg("ssh"): {pkg("environments"), pkg("mcp")},
		// accounts is the credential substrate: modules depend on it, never the
		// reverse, and it declares EnvLookup locally like ssh/database/kafka.
		// It takes a *sql.DB and a table name rather than importing storage,
		// so the scoped-secret table stays the composition root's decision.
		// apiclient sends HTTP from Go. It declares Credentials, EnvLookup and
		// Confirmer locally: it must not reach into accounts, environments or
		// interaction, which is what keeps the credential resolution a
		// composition-root decision rather than a package dependency.
		pkg("apiclient"): {
			pkg("accounts"), pkg("environments"), pkg("interaction"), pkg("codegraph"),
			pkg("storage"), pkg("mcp"), pkg("bridge"), pkg("app"),
		},
		// tasks is a thin adapter on vaultitem like notes. It declares
		// Confirmer, Notifier and SessionLiveness locally rather than
		// importing interaction, notify or agenthub — the board must not
		// depend on the agent runtime to exist.
		pkg("tasks"): {
			pkg("interaction"), pkg("notify"), pkg("agenthub"), pkg("git"),
			pkg("storage"), pkg("mcp"), pkg("bridge"), pkg("app"),
		},
		// git runs against the project's own work tree. It may use checkpoint
		// (for Locate) and gitparse, both leaves below it, but never accounts,
		// environments or interaction directly — it declares those as local
		// interfaces so the composition root decides what satisfies them.
		pkg("git"): {
			pkg("accounts"), pkg("environments"), pkg("interaction"), pkg("vaultitem"),
			pkg("mcp"), pkg("bridge"), pkg("app"), pkg("storage"),
		},
		pkg("accounts"): {
			pkg("environments"), pkg("storage"), pkg("mcp"), pkg("bridge"), pkg("app"),
			pkg("ssh"), pkg("database"), pkg("kafka"), pkg("notes"), pkg("agenthub"),
		},
		pkg("database"): {pkg("environments"), pkg("mcp")},
		pkg("kafka"):    {pkg("environments"), pkg("mcp")},
		// notes must not depend upward on the MCP host.
		pkg("notes"): {pkg("mcp")},
		// Shared memory owns only its domain/repository; host identity and
		// migrations are supplied by composition, not imported by the store.
		pkg("workspacememory"): {pkg("storage"), pkg("agenthub"), pkg("mcp"), pkg("bridge"), pkg("app"), pkg("vaultitem")},
		// agenthub may use sandbox for vendor spawn, but must not reach host
		// boundaries (mcp/bridge/app) or own the storage migration package.
		pkg("agenthub"): {pkg("mcp"), pkg("bridge"), pkg("app"), pkg("storage")},
		// acp is a leaf transport: no hub/sandbox/host imports (adapters own sandbox).
		pkg("acp"): {pkg("agenthub"), pkg("sandbox"), pkg("mcp"), pkg("bridge"), pkg("app")},
		// crossservice joins service snapshots with the workspace's resolver; it is
		// domain logic that hosts call, never a host or a store itself.
		pkg("crossservice"): {pkg("storage"), pkg("mcp"), pkg("bridge"), pkg("app"), pkg("agenthub")},
		// mcp is an alternate host boundary: capability-gated tools only, never
		// storage/crypto/sync, and never bridge/app (Caller is defined locally).
		pkg("mcp"): {pkg("storage"), pkg("syncstore"), pkg("sync"), pkg("crypto"), pkg("app"), pkg("bridge")},
		// storage is the generic DB kernel; the sync persistence adapter builds on
		// it, never the reverse.
		pkg("storage"): {pkg("syncstore")},
	}
	for from, forbidden := range bans {
		for _, imp := range graph[from] {
			for _, bad := range forbidden {
				if imp == bad {
					t.Errorf("layering violation: %s must not import %s", short(from), short(imp))
				}
			}
		}
	}

	// crypto is a leaf: it must not import any other in-module package.
	for _, imp := range graph[pkg("crypto")] {
		t.Errorf("layering violation: crypto must not import %s", short(imp))
	}

	// sandbox is a leaf execution boundary: production code must not import any
	// other in-module package.
	for _, imp := range graph[pkg("sandbox")] {
		t.Errorf("layering violation: sandbox must not import %s", short(imp))
	}

	// gitparse is a leaf: it reads what git printed, which does not depend on
	// which git directory printed it. checkpoint (shadow repo) and the Git
	// module (the project's own repo) both consume it.
	for _, imp := range graph[pkg("gitparse")] {
		t.Errorf("layering violation: gitparse must not import %s", short(imp))
	}

	// notify is a leaf: it raises OS notifications for any module and must not
	// know what a module is. That is why its Target is three strings of its
	// own rather than the workbench's EntityRef — see internal/notify.
	for _, imp := range graph[pkg("notify")] {
		t.Errorf("layering violation: notify must not import %s", short(imp))
	}

	// gitssh is a leaf: it serves an SSH key to ssh and reads one, and both
	// accounts (which validates keys) and git (which uses them) import it, so it
	// can import neither.
	for _, imp := range graph[pkg("gitssh")] {
		t.Errorf("layering violation: gitssh must not import %s", short(imp))
	}

	// updater and buildinfo are leaves. updater takes the running version as a
	// parameter instead of importing it, so it can be tested against any
	// version and knows nothing about how the linker stamps one; buildinfo is
	// a string the linker overwrites and must not pull anything in.
	for _, leaf := range []string{"updater", "buildinfo"} {
		for _, imp := range graph[pkg(leaf)] {
			t.Errorf("layering violation: %s must not import %s", leaf, short(imp))
		}
	}

	// wssearch and docwatch are leaves behind the workspace's file features:
	// they are handed directories and know nothing of workspaces, the vault or
	// the bridge, so a search or a watcher can be tested against a temp dir.
	for _, leaf := range []string{"wssearch", "docwatch"} {
		for _, imp := range graph[pkg(leaf)] {
			t.Errorf("layering violation: %s must not import %s", leaf, short(imp))
		}
	}

	// codexapp is a leaf transport for `codex app-server`, like acp: the
	// adapter in agenthub owns the sandbox and the translation to ACP.
	for _, imp := range graph[pkg("codexapp")] {
		t.Errorf("layering violation: codexapp must not import %s", short(imp))
	}

	// checkpoint runs git against a project directory and knows nothing about
	// sessions, transcripts or storage. agenthub consumes it through a
	// locally-declared interface, never the reverse — the same boundary
	// ssh/database keep from environments.
	//
	// gitparse is the one import allowed: it is itself a leaf, it sits below
	// checkpoint rather than above it, and the alternative is a second copy of
	// the diff parsers in internal/git.
	for _, imp := range graph[pkg("checkpoint")] {
		if imp == pkg("gitparse") {
			continue
		}
		t.Errorf("layering violation: checkpoint must not import %s", short(imp))
	}

	// Nothing but main may import the bridge; nothing but the bridge (and main)
	// may import app. main is the module-root package "devkit".
	for from, imports := range graph {
		for _, imp := range imports {
			if imp == pkg("bridge") && from != "devkit" {
				t.Errorf("layering violation: %s must not import bridge (top boundary)", short(from))
			}
			if imp == pkg("app") && from != "devkit" && from != pkg("bridge") {
				t.Errorf("layering violation: %s must not import app (only bridge/main may)", short(from))
			}
		}
	}
}

// TestNoImportCycles asserts the in-module import graph is acyclic. The Go
// compiler already rejects cycles, but this documents the invariant and fails
// fast with the offending path.
func TestNoImportCycles(t *testing.T) {
	graph := importGraph(t)
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[string]int{}
	var stack []string
	var visit func(string) bool
	visit = func(node string) bool {
		color[node] = gray
		stack = append(stack, node)
		for _, next := range graph[node] {
			switch color[next] {
			case gray:
				t.Errorf("import cycle: %s", strings.Join(append(stack, next), " -> "))
				return true
			case white:
				if visit(next) {
					return true
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[node] = black
		return false
	}
	for node := range graph {
		if color[node] == white && visit(node) {
			return
		}
	}
}

func short(importPath string) string {
	return strings.TrimPrefix(importPath, modulePrefix)
}
