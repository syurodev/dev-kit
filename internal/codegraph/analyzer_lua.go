// internal/codegraph/analyzer_lua.go
package codegraph

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"devkit/internal/lua"
)

// The Lua analyzer, in process.
//
// Every other analyzer CodeGraph has is a subprocess built from source on the
// user's machine (see toolchain.go), because JavaParser only exists on a JVM.
// Lua needs none of that: the parser is internal/lua, so the analysis is a
// function call. That is not just tidier — it is the difference between a
// module that works the moment DevKit starts and one that shows a setup panel
// asking for a runtime, on every platform including the two packages that
// cannot carry an artifact at all (AppImage, MSIX).
//
// What it resolves, and what it does not. Lua has no types, so a call is
// resolvable only when the value it is made on can be traced back to something
// declared: a `require` of a file in this project, the module's own table, or
// `self` inside a method. Everything else is recorded as a call site with a
// reason ("library" for the standard library and required modules that are not
// in this project, "unresolved" otherwise) rather than guessed at. A wrong
// edge is worse than a missing one: the graph is read as evidence.

const (
	// maxLuaFiles caps how many source files one project contributes. A Lua
	// tree that big is a vendored world, not a project someone is reading.
	maxLuaFiles = 20000
	// maxLuaFileBytes skips a file too large to be hand-written source —
	// generated tables of data, a bundled dependency concatenated into one
	// file. Parsing it would cost more than everything else together and
	// produce nothing anybody navigates.
	maxLuaFileBytes = 2 << 20
)

// luaAnalyzer is the Analyzer implementation. Stateless: every Analyze builds
// its own world, so two projects can never see each other's modules.
type luaAnalyzer struct{}

// NewLuaAnalyzer returns the in-process Lua analyzer.
func NewLuaAnalyzer() Analyzer { return luaAnalyzer{} }

// luaModule is one .lua file: what it is called, what it declares, and what it
// required. One file is one module, which is what `require` means in Lua —
// there is no unit smaller than a file for it to name.
type luaModule struct {
	rel     string // path relative to the project root, forward slashes
	dotted  string // the require path this file answers to
	nodeID  string
	chunk   *lua.Chunk
	lines   []string
	summary string

	// tableVar is the local name this file's module table is bound to — the M
	// in `local M = {} ... return M`. It is how a declaration path becomes a
	// public name: `M.access` is exported as `access`, and a caller in another
	// file writes it as `util.access` with a name of its own choosing.
	tableVar string

	// funcs is every function this file declares, keyed by the path it was
	// declared under ("M.access", "helper"). exports is the subset another
	// file can reach, keyed by the name it would use there.
	funcs   map[string]*luaFunc
	exports map[string]*luaFunc
	// order keeps declaration order for deterministic output; map iteration
	// is not, and an index that reshuffles itself between two identical runs
	// makes every diff of it unreadable.
	order []*luaFunc

	requires []luaRequire
	// requireByName is requires indexed by the local variable that holds them.
	requireByName map[string]luaRequire

	layer string
}

// luaFunc is one declared function, and the node it became.
type luaFunc struct {
	nodeID  string
	label   string
	decl    string // the path it was declared under, e.g. "M.access"
	public  string // the name another file reaches it by, "" when not exported
	params  []Param
	line    int
	endLine int
	summary string
	body    *lua.FuncExpr
	// selfTable is the table `self` refers to inside this body — set for a
	// colon declaration, "" otherwise.
	selfTable string
	// global marks a declaration made without `local`. A global function is
	// reachable from any file that runs after this one, so it counts as
	// exported even when the module returns a table that does not hold it.
	global bool
}

// luaRequire is one `require` this file performs, and what it resolved to.
type luaRequire struct {
	name   string     // the local variable it was bound to, "" when unbound
	path   string     // the string passed to require
	target *luaModule // nil when the path names nothing in this project
	line   int
}

func (a luaAnalyzer) Analyze(ctx context.Context, projectPath string) (AnalysisResult, error) {
	files, err := luaSourceFiles(projectPath)
	if err != nil {
		return AnalysisResult{}, err
	}
	if len(files) == 0 {
		// Not an error: detection already decided this folder is a Lua
		// project, and a project whose sources were all skipped is an empty
		// index, which the UI can say something honest about.
		return AnalysisResult{}, nil
	}

	modules, err := luaParseAll(ctx, projectPath, files)
	if err != nil {
		return AnalysisResult{}, err
	}

	index := newLuaIndex(modules)
	for _, module := range modules {
		module.declare(index)
	}
	for _, module := range modules {
		module.resolveRequires(index)
	}

	result := AnalysisResult{}
	classifyLuaLayers(modules, index, &result)

	for _, module := range modules {
		module.emitNodes(&result)
	}
	for _, module := range modules {
		module.emitDependencies(&result)
		module.resolveCalls(index, &result)
	}
	sortEndpoints(result.Endpoints)
	return result, nil
}

// luaSourceFiles walks the project for .lua files, in a stable order.
//
// It skips what the rest of CodeGraph skips (ignoredDirs and hidden
// directories) plus Lua's own two: luarocks' install tree and busted's, both
// of which hold a copy of every dependency and neither of which anybody
// navigates.
func luaSourceFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable directory costs its subtree, not the analysis.
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if current == root {
				return nil
			}
			name := entry.Name()
			if ignoredDir(name) || strings.HasPrefix(name, ".") || name == "lua_modules" || name == ".luarocks" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".lua") {
			return nil
		}
		if info, statErr := entry.Info(); statErr == nil && info.Size() > maxLuaFileBytes {
			return nil
		}
		files = append(files, current)
		if len(files) >= maxLuaFiles {
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("codegraph: walk %q: %w", root, err)
	}
	sort.Strings(files)
	return files, nil
}

// luaParseAll reads and parses every file, in parallel but reporting in path
// order. Parsing is CPU-bound and files are independent, and a plugin
// repository with a vendored spec suite is thousands of them.
func luaParseAll(ctx context.Context, root string, files []string) ([]*luaModule, error) {
	modules := make([]*luaModule, len(files))
	workers := runtime.GOMAXPROCS(0)
	if workers > len(files) {
		workers = len(files)
	}

	var (
		next    int
		nextMu  sync.Mutex
		wait    sync.WaitGroup
		failure error
		failMu  sync.Mutex
	)
	wait.Add(workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer wait.Done()
			for {
				nextMu.Lock()
				i := next
				next++
				nextMu.Unlock()
				if i >= len(files) {
					return
				}
				if ctx.Err() != nil {
					return
				}
				module, err := luaReadModule(root, files[i])
				if err != nil {
					failMu.Lock()
					if failure == nil {
						failure = err
					}
					failMu.Unlock()
					return
				}
				modules[i] = module
			}
		}()
	}
	wait.Wait()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if failure != nil {
		return nil, failure
	}
	// A file that vanished between the walk and the read leaves a nil.
	kept := modules[:0]
	for _, module := range modules {
		if module != nil {
			kept = append(kept, module)
		}
	}
	return kept, nil
}

func luaReadModule(root, file string) (*luaModule, error) {
	source, err := os.ReadFile(file)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("codegraph: read %q: %w", file, err)
	}
	rel, err := filepath.Rel(root, file)
	if err != nil {
		return nil, fmt.Errorf("codegraph: locate %q under %q: %w", file, root, err)
	}
	text := string(source)
	module := &luaModule{
		rel:           filepath.ToSlash(rel),
		chunk:         lua.Parse(text),
		lines:         strings.Split(text, "\n"),
		funcs:         map[string]*luaFunc{},
		exports:       map[string]*luaFunc{},
		requireByName: map[string]luaRequire{},
		layer:         "unknown",
	}
	module.dotted = luaRequirePath(module.rel)
	module.summary = luaHeaderComment(module.lines)
	return module, nil
}

// luaRequirePath turns a file path into the name `require` would use for it.
//
// The two conventions that matter are both here: `foo/init.lua` is required as
// "foo", and a leading source root ("lua/" for Neovim and LuaRocks, "src/" for
// a rockspec that builds from src) is not part of the name. Everything else is
// the path with its separators turned into dots, which is exactly what Kong
// plugins rely on — kong/plugins/x/handler.lua is required as
// "kong.plugins.x.handler" and nothing has to know that Kong exists.
func luaRequirePath(rel string) string {
	clean := strings.TrimSuffix(path.Clean(rel), ".lua")
	segments := strings.Split(clean, "/")
	if len(segments) > 1 && (segments[0] == "lua" || segments[0] == "src") {
		segments = segments[1:]
	}
	if len(segments) > 1 && segments[len(segments)-1] == "init" {
		segments = segments[:len(segments)-1]
	}
	return strings.Join(segments, ".")
}

// luaIndex is the project-wide view: which module answers to which require
// path, and the id allocator that keeps two of them from colliding.
type luaIndex struct {
	byRequire map[string][]*luaModule
	modules   []*luaModule
	usedIDs   map[string]bool
}

func newLuaIndex(modules []*luaModule) *luaIndex {
	index := &luaIndex{
		byRequire: map[string][]*luaModule{},
		modules:   modules,
		usedIDs:   map[string]bool{},
	}
	for _, module := range modules {
		module.nodeID = index.uniqueID(module.dotted)
		for _, key := range luaRequireKeys(module.rel) {
			index.byRequire[key] = append(index.byRequire[key], module)
		}
	}
	return index
}

// luaRequireKeys is every name this file plausibly answers to. More than one,
// because a repository can be checked out at any depth relative to where its
// modules are installed: a Kong plugin's own repository has handler.lua at the
// root and is still required as "kong.plugins.name.handler" in production.
func luaRequireKeys(rel string) []string {
	keys := map[string]bool{luaRequirePath(rel): true}
	full := strings.TrimSuffix(path.Clean(rel), ".lua")
	keys[strings.ReplaceAll(full, "/", ".")] = true
	if strings.HasSuffix(full, "/init") {
		keys[strings.ReplaceAll(strings.TrimSuffix(full, "/init"), "/", ".")] = true
	}
	out := make([]string, 0, len(keys))
	for key := range keys {
		if key != "" {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

// uniqueID hands out a node id, qualifying a repeat rather than dropping it.
// Two files can share a require path — `lua/x.lua` next to `x.lua` — and the
// second one is still a file somebody opens.
func (i *luaIndex) uniqueID(base string) string {
	if base == "" {
		base = "chunk"
	}
	if !i.usedIDs[base] {
		i.usedIDs[base] = true
		return base
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s [#%d]", base, n)
		if !i.usedIDs[candidate] {
			i.usedIDs[candidate] = true
			return candidate
		}
	}
}

// lookup resolves a require path to the module that serves it.
//
// Exact first. Then a suffix match, which is what makes a plugin repository
// work when it is checked out at its own root: the source says
// require("kong.plugins.rate-limit.policies") and the file on disk is
// policies.lua, whose only honest name is "policies". A suffix match is
// accepted only when exactly one module matches — two would be a guess, and
// the call is better recorded as unresolved than attached to the wrong file.
func (i *luaIndex) lookup(requirePath string) *luaModule {
	if candidates := i.byRequire[requirePath]; len(candidates) == 1 {
		return candidates[0]
	} else if len(candidates) > 1 {
		return nil
	}
	var found *luaModule
	for _, module := range i.modules {
		for _, key := range luaRequireKeys(module.rel) {
			if key != "" && strings.HasSuffix(requirePath, "."+key) {
				if found != nil && found != module {
					return nil
				}
				found = module
			}
		}
	}
	return found
}

func (m *luaModule) resolveRequires(index *luaIndex) {
	for i := range m.requires {
		m.requires[i].target = index.lookup(m.requires[i].path)
		if name := m.requires[i].name; name != "" {
			m.requireByName[name] = m.requires[i]
		}
	}
}

func (m *luaModule) emitNodes(result *AnalysisResult) {
	result.Nodes = append(result.Nodes, Node{
		ID:       m.nodeID,
		Label:    m.dotted,
		Kind:     "class",
		Layer:    m.layer,
		FilePath: m.rel,
		Line:     1,
		EndLine:  len(m.lines),
		Summary:  m.summary,
	})
	for _, fn := range m.order {
		result.Nodes = append(result.Nodes, Node{
			ID:       fn.nodeID,
			Label:    fn.label,
			Kind:     "method",
			Layer:    m.layer,
			FilePath: m.rel,
			Line:     fn.line,
			EndLine:  fn.endLine,
			Summary:  fn.summary,
			Params:   fn.params,
		})
	}
}

func (m *luaModule) emitDependencies(result *AnalysisResult) {
	seen := map[string]bool{}
	for _, req := range m.requires {
		if req.target == nil || req.target == m || seen[req.target.nodeID] {
			continue
		}
		seen[req.target.nodeID] = true
		result.FileDependencies = append(result.FileDependencies, FileDependency{
			FilePath:          m.rel,
			DependsOnFilePath: req.target.rel,
		})
		// "di" is what the graph calls a structural dependency that is not a
		// call — a Spring bean injected into a field, and here a module a file
		// pulls in at load time. Same meaning, same colour, and it is what
		// makes a module diagram of a Lua project readable before a single
		// call has been resolved.
		result.Edges = append(result.Edges, Edge{From: m.nodeID, To: req.target.nodeID, Kind: "di"})
	}
}

// luaHeaderComment is the comment block a file opens with, which is where Lua
// puts what a Javadoc would say about a class.
func luaHeaderComment(lines []string) string {
	var collected []string
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if i == 0 && strings.HasPrefix(trimmed, "#!") {
			continue
		}
		if trimmed == "" && len(collected) == 0 {
			continue
		}
		if !strings.HasPrefix(trimmed, "--") {
			break
		}
		collected = append(collected, trimmed)
	}
	return luaCommentText(collected)
}

// luaCommentAbove is the comment block immediately above a line — the LDoc
// convention, and the only doc comment Lua has.
func luaCommentAbove(lines []string, line int) string {
	var collected []string
	for i := line - 2; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(trimmed, "--") {
			break
		}
		collected = append([]string{trimmed}, collected...)
	}
	return luaCommentText(collected)
}

// luaCommentText turns comment lines into the one-paragraph summary the
// inspector shows: markers off, LDoc tag lines dropped, and everything after
// the first blank line left behind — the same shape as a Javadoc description.
func luaCommentText(lines []string) string {
	var parts []string
	for _, line := range lines {
		text := strings.TrimLeft(line, "-")
		text = strings.TrimPrefix(text, "[[")
		text = strings.TrimSpace(text)
		if strings.HasPrefix(text, "@") {
			// An LDoc tag ends the description, the way a Javadoc block tag does.
			break
		}
		if text == "" {
			if len(parts) > 0 {
				break
			}
			continue
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, " ")
}
