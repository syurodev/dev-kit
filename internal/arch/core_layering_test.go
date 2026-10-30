package arch

import (
	"sort"
	"strings"
	"testing"
)

// The harness core is the part of DevKit that every later feature leans on. These
// rules keep it small by making each dependency a decision somebody has to write
// down here, in review, instead of an import that appears while fixing something
// else. A failure names the package and the import; to allow a new one, edit the
// table and say why in the change.

// coreImports is a CLOSED list: the package may import exactly these in-module
// packages (production code) and nothing else of the module.
var coreImports = map[string][]string{
	// Pure policy and types: the resolver, the tiers, the store contract. It does
	// not know there are sessions, tools servers, windows or databases.
	"harness": {},
	// Proposals and applying them. Everything it needs from the outside (store,
	// session directory, confirmation, live endpoints) is a port declared here.
	"harnessconfig": {"harness", "identity"},
	// Task hand-off orchestration. A stdlib leaf: capability checks, caller
	// identity and gates stay in the services that own them.
	"agentwork": {},
	// The shared question hub.
	"interaction": {"identity", "vaultitem"},
	// Who is calling, what they may do, and the record of it.
	"identity":     {},
	"permissions":  {},
	"audit":        {},
	"capabilities": {"audit", "identity", "permissions"},
	// Measurement and settings stay below everything that reads them.
	"usagemetrics": {},
	"preferences":  {},
	"apperrors":    {},
}

func TestCorePackagesImportOnlyWhatTheyMay(t *testing.T) {
	graph := importGraph(t)
	for name, allowed := range coreImports {
		ok := map[string]bool{}
		for _, a := range allowed {
			ok[pkg(a)] = true
		}
		for _, imp := range graph[pkg(name)] {
			if !ok[imp] {
				t.Errorf("core layering: %s must not import %s (allowed: %s)", name, short(imp), allowedList(allowed))
			}
		}
	}
}

// coreImporters is a CLOSED list of who may import a core package. Importing one
// is how a feature gets coupled to it, so a new importer is a design decision.
// "main" is the root package.
var coreImporters = map[string][]string{
	// Policy types are shared, but only the layers that serve or store a profile
	// know them; the hub reads them through its own ports.
	"harness": {"main", "internal/agenthub", "internal/app", "internal/bridge", "internal/harnessconfig", "internal/mcp", "internal/storage"},
	// agenthub deliberately does not import harnessconfig: internal/app joins them.
	"harnessconfig": {"internal/app", "internal/bridge", "internal/mcp"},
	"agentwork":     {"internal/app", "internal/mcp", "internal/storage"},
	// The adapters are assembled by the entry points and imported by nothing below
	// them (see adapters_test.go): replacing the window toolkit, or hosting the Go
	// side without one, must not reach into the domain.
	"bridge":          {"main"},
	"mcp":             {"main", "cmd/devkit-mcp"},
	"telegramcontrol": {"main", "internal/app", "internal/bridge"},
}

func TestCorePackagesAreImportedOnlyByWhoMay(t *testing.T) {
	graph := importGraph(t)
	for name, allowed := range coreImporters {
		ok := map[string]bool{}
		for _, a := range allowed {
			ok[a] = true
		}
		var importers []string
		for from, imps := range graph {
			for _, imp := range imps {
				if imp == pkg(name) {
					importers = append(importers, importerName(from))
				}
			}
		}
		sort.Strings(importers)
		for _, from := range importers {
			if !ok[from] {
				t.Errorf("core layering: %s must not be imported by %s (allowed: %s)", name, from, allowedList(allowed))
			}
		}
	}
}

// importerName turns an import path into the names used in coreImporters.
func importerName(importPath string) string {
	if importPath == "devkit" {
		return "main"
	}
	return strings.TrimPrefix(importPath, modulePrefix)
}

// The leaves the harness is built from use the standard library and nothing else:
// no third-party module, so the core never inherits a dependency's failure modes.
func TestHarnessCoreUsesOnlyTheStandardLibraryAndItself(t *testing.T) {
	out := goList(t, "{{.ImportPath}}\t{{join .Imports \" \"}}", "./internal/harness/", "./internal/harnessconfig/", "./internal/agentwork/")
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.SplitN(line, "\t", 2)
		if len(fields) < 2 {
			continue
		}
		for _, imp := range strings.Fields(fields[1]) {
			first := strings.SplitN(imp, "/", 2)[0]
			if strings.HasPrefix(imp, modulePrefix) || !strings.Contains(first, ".") {
				continue // the module itself, or the standard library
			}
			t.Errorf("core dependency: %s imports third-party %s", short(fields[0]), imp)
		}
	}
}

func allowedList(allowed []string) string {
	if len(allowed) == 0 {
		return "none"
	}
	return strings.Join(allowed, ", ")
}
