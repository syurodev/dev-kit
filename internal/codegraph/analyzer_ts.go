// internal/codegraph/analyzer_ts.go
package codegraph

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"devkit/internal/lsp"
)

// The TypeScript analyzer: a language server, driven.
//
// It is the third shape CodeGraph has. Java's analyzer is a jar launched by a
// JDK the user must have; Lua's is a function call in this process; this one
// is a subprocess that needs nothing installed — tsgo is a self-contained Go
// binary DevKit ships — and, unlike either of the others, it is LONG-LIVED.
//
// That last part is the design. A cold index of a mid-sized project is a
// couple of seconds, almost all of it building the program: parsing the
// project, its dependencies' type declarations and the standard library. A
// warm one is milliseconds. The file watcher re-indexes on every save, so
// throwing the server away between saves would pay the cold cost every time —
// which is why this analyzer holds its connection and Service closes it.
//
// What it does NOT do is resolve calls itself. The checker already has the
// answer, and asking for it is what makes TypeScript the best-resolved
// language in the module: there is no "unresolved" category here, because a
// call TypeScript cannot resolve is a call that does not compile.

const (
	lspSymbolClass       = lsp.SymbolKindClass
	lspSymbolConstructor = lsp.SymbolKindConstructor
)

// maxTSFiles bounds one project. A TypeScript tree past this is a monorepo of
// everything, and the answer to it is opening one package rather than making
// this number bigger.
const maxTSFiles = 20000

// tsConcurrency is how many requests are kept in flight. The server answers
// them on its own schedule; the point is only that a round trip is not paid
// one at a time, tens of thousands of times.
const tsConcurrency = 8

// tsAnalyzer holds the connection between analyses.
type tsAnalyzer struct {
	// server is the argv that launches the language server, with the artifact
	// already substituted in. Resolved per analysis by the toolchain.
	server func() ([]string, []string, error)

	mu   sync.Mutex
	root string
	conn *lsp.Conn
	// open is what the server already has, by URI and content hash. A
	// language server keeps every opened document; telling it again about a
	// file it already has is not free, and telling it a file is at version 1
	// twice is a lie. Without this a re-index costs MORE than the first index,
	// which is the opposite of the reason this analyzer is long-lived.
	open map[string]uint64
}

// NewTypeScriptAnalyzer returns an analyzer that drives a language server
// launched by launch, which reports the argv and environment to use.
func NewTypeScriptAnalyzer(launch func() ([]string, []string, error)) Analyzer {
	return &tsAnalyzer{server: launch}
}

// CloseAnalyzer shuts the language server down. Service calls it when the
// project changes or the app quits; see analyzerCloser.
func (a *tsAnalyzer) CloseAnalyzer() error {
	a.mu.Lock()
	conn := a.conn
	a.conn, a.root, a.open = nil, "", nil
	a.mu.Unlock()
	if conn != nil {
		return conn.Close()
	}
	return nil
}

// connect returns a server loaded with this project, reusing the one already
// running when it is the same project.
func (a *tsAnalyzer) connect(ctx context.Context, root string) (*lsp.Conn, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.conn != nil && a.root == root {
		return a.conn, nil
	}
	if a.conn != nil {
		_ = a.conn.Close()
		a.conn, a.root, a.open = nil, "", nil
	}

	argv, env, err := a.server()
	if err != nil {
		return nil, err
	}
	conn, err := lsp.Start(argv, root, env)
	if err != nil {
		return nil, err
	}
	if err := conn.Initialize(ctx, lsp.URIFor(root)); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("codegraph: initialize typescript server: %w", err)
	}
	a.conn, a.root, a.open = conn, root, map[string]uint64{}
	return conn, nil
}

func (a *tsAnalyzer) Analyze(ctx context.Context, projectPath string) (AnalysisResult, error) {
	root, err := filepath.Abs(projectPath)
	if err != nil {
		return AnalysisResult{}, fmt.Errorf("codegraph: resolve %q: %w", projectPath, err)
	}

	files, err := tsSourceFiles(root)
	if err != nil {
		return AnalysisResult{}, err
	}
	if len(files) == 0 {
		return AnalysisResult{}, nil
	}

	conn, err := a.connect(ctx, root)
	if err != nil {
		// A server that failed to start is not a server to keep.
		_ = a.CloseAnalyzer()
		return AnalysisResult{}, err
	}

	project := &tsProject{root: root, deps: tsDependencies(root), byPath: map[string]*tsFile{}}
	for _, abs := range files {
		file, err := tsReadFile(root, abs)
		if err != nil {
			continue // a file that vanished between the walk and the read
		}
		project.files = append(project.files, file)
		project.byPath[file.abs] = file
	}

	if err := a.sync(conn, project); err != nil {
		return AnalysisResult{}, err
	}
	if err := tsDeclareAll(ctx, conn, project); err != nil {
		return AnalysisResult{}, err
	}
	tsAssignIDs(project)
	tsPlaceCalls(project)
	if err := tsResolveImports(ctx, conn, project); err != nil {
		return AnalysisResult{}, err
	}

	result := AnalysisResult{}
	tsApplyFrameworks(project, &result)
	tsEmitNodes(project, &result)
	tsEmitDependencies(project, &result)
	tsEmitFields(project, &result)
	if err := tsEmitCalls(ctx, conn, project, &result); err != nil {
		return AnalysisResult{}, err
	}
	tsSortEndpoints(result.Endpoints)
	return result, nil
}

// tsSourceExtensions are the files a TypeScript project's program holds. The
// JavaScript ones are in because `allowJs` is common and because a project
// migrating to TypeScript is exactly the project someone wants a graph of.
var tsSourceExtensions = map[string]bool{
	".ts": true, ".tsx": true, ".mts": true, ".cts": true,
	".js": true, ".jsx": true, ".mjs": true, ".cjs": true,
}

// tsIgnoredDirs are skipped on top of the shared list: build output and, above
// all, node_modules. The program includes the type declarations in there and
// the checker needs them, but they are not this project's source and nobody
// navigates them.
var tsIgnoredDirs = map[string]bool{
	"node_modules": true, ".next": true, ".nuxt": true, ".turbo": true,
	"coverage": true, "storybook-static": true, ".angular": true, ".svelte-kit": true,
}

func tsSourceFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
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
			if ignoredDir(name) || tsIgnoredDirs[name] || strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
			return nil
		}
		name := entry.Name()
		if !tsSourceExtensions[strings.ToLower(filepath.Ext(name))] {
			return nil
		}
		// A declaration file describes something else's API; it declares no
		// implementation and contributes no call anybody can follow.
		if strings.HasSuffix(name, ".d.ts") || strings.HasSuffix(name, ".d.mts") {
			return nil
		}
		files = append(files, current)
		if len(files) >= maxTSFiles {
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

// tsDependencies reads every package.json under the root for the names it
// depends on. That list is how a framework recognises itself — a guess from
// file names would call any project with a `users.controller.ts` a Nest one.
func tsDependencies(root string) map[string]bool {
	deps := map[string]bool{}
	_ = filepath.WalkDir(root, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			name := entry.Name()
			if current != root && (ignoredDir(name) || tsIgnoredDirs[name] || strings.HasPrefix(name, ".")) {
				return fs.SkipDir
			}
			return nil
		}
		if entry.Name() != "package.json" {
			return nil
		}
		content, readErr := os.ReadFile(current)
		if readErr != nil {
			return nil
		}
		var manifest struct {
			Dependencies     map[string]string `json:"dependencies"`
			DevDependencies  map[string]string `json:"devDependencies"`
			PeerDependencies map[string]string `json:"peerDependencies"`
		}
		if json.Unmarshal(content, &manifest) != nil {
			return nil
		}
		for _, set := range []map[string]string{manifest.Dependencies, manifest.DevDependencies, manifest.PeerDependencies} {
			for name := range set {
				deps[name] = true
			}
		}
		return nil
	})
	return deps
}

func tsReadFile(root, abs string) (*tsFile, error) {
	content, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return nil, err
	}
	source := string(content)
	return &tsFile{
		rel:    filepath.ToSlash(rel),
		abs:    abs,
		uri:    lsp.URIFor(abs),
		source: source,
		lines:  strings.Split(source, "\n"),
		module: tsModuleName(filepath.ToSlash(rel)),
	}, nil
}

// sync tells the server what it does not already know: a file it has never
// seen is opened, one whose content changed is changed, and one that has not
// is left alone. Notifications, so they are ordered on the wire and the
// questions that follow can go out concurrently.
//
// The third case is the one that matters. On the first index every file is new
// and this is exactly tsOpenAll; on every index after it — which is what the
// file watcher triggers, on every save — it is usually one file.
func (a *tsAnalyzer) sync(conn *lsp.Conn, p *tsProject) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.open == nil {
		a.open = map[string]uint64{}
	}

	seen := make(map[string]bool, len(p.files))
	for _, file := range p.files {
		seen[file.uri] = true
		hash := tsContentHash(file.source)
		previous, known := a.open[file.uri]
		switch {
		case !known:
			if err := conn.DidOpen(file.uri, tsLanguageID(file.rel), file.source); err != nil {
				return fmt.Errorf("codegraph: open %s: %w", file.rel, err)
			}
		case previous == hash:
			continue
		default:
			if err := conn.Notify("textDocument/didChange", map[string]any{
				"textDocument":   map[string]any{"uri": file.uri, "version": 2},
				"contentChanges": []any{map[string]any{"text": file.source}},
			}); err != nil {
				return fmt.Errorf("codegraph: update %s: %w", file.rel, err)
			}
		}
		a.open[file.uri] = hash
	}

	// A file that went away is closed, so the server stops holding a document
	// for something that is not there any more.
	for uri := range a.open {
		if seen[uri] {
			continue
		}
		_ = conn.Notify("textDocument/didClose", map[string]any{
			"textDocument": map[string]any{"uri": uri},
		})
		delete(a.open, uri)
	}
	return nil
}

// tsContentHash is FNV-1a over a file's text. It only has to notice a change,
// never to be unguessable.
func tsContentHash(source string) uint64 {
	const (
		offset = 14695981039346656037
		prime  = 1099511628211
	)
	hash := uint64(offset)
	for i := 0; i < len(source); i++ {
		hash ^= uint64(source[i])
		hash *= prime
	}
	return hash
}

func tsLanguageID(rel string) string {
	switch strings.ToLower(filepath.Ext(rel)) {
	case ".tsx":
		return "typescriptreact"
	case ".jsx":
		return "javascriptreact"
	case ".js", ".mjs", ".cjs":
		return "javascript"
	default:
		return "typescript"
	}
}

// tsEach runs work over every file, a few at a time. The first error wins and
// stops the rest.
func tsEach(ctx context.Context, files []*tsFile, work func(*tsFile) error) error {
	var (
		next    int
		nextMu  sync.Mutex
		wait    sync.WaitGroup
		failure error
		failMu  sync.Mutex
	)
	workers := tsConcurrency
	if workers > len(files) {
		workers = len(files)
	}
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
				failMu.Lock()
				stop := failure != nil
				failMu.Unlock()
				if stop {
					return
				}
				if err := work(files[i]); err != nil {
					failMu.Lock()
					if failure == nil {
						failure = err
					}
					failMu.Unlock()
					return
				}
			}
		}()
	}
	wait.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	return failure
}

// tsDeclareAll asks every file what it declares, and reads out of the source
// what the answer leaves out.
func tsDeclareAll(ctx context.Context, conn *lsp.Conn, p *tsProject) error {
	return tsEach(ctx, p.files, func(file *tsFile) error {
		symbols, err := conn.DocumentSymbol(ctx, file.uri)
		if err != nil {
			// One unreadable file is not a failed index. It contributes no
			// nodes, the way an unparseable file does everywhere else.
			return nil
		}
		inImport, imports := tsImportLines(file.lines)
		file.imports = imports
		file.calls = tsReadCalls(file.source)
		tsCollect(file, symbols, nil, inImport)
		return nil
	})
}

// tsCollect turns the server's symbol tree into declarations.
func tsCollect(file *tsFile, symbols []lsp.DocumentSymbol, owner *tsDecl, inImport map[int]bool) {
	for _, symbol := range symbols {
		// Every imported NAME comes back as a symbol of the file. Without
		// this, a file's declarations start with one phantom per import.
		if inImport[symbol.SelectionRange.Start.Line] {
			continue
		}
		kind := tsNodeKind(file, symbol)
		if kind == "" {
			if owner != nil && (symbol.Kind == lsp.SymbolKindProperty || symbol.Kind == lsp.SymbolKindField) {
				if owner.properties == nil {
					owner.properties = map[string]bool{}
				}
				owner.properties[symbol.Name] = true
			}
			tsCollect(file, symbol.Children, owner, inImport)
			continue
		}

		decl := &tsDecl{
			kind:       kind,
			name:       symbol.Name,
			file:       file,
			owner:      owner,
			symbolKind: symbol.Kind,
			line:       symbol.Range.Start.Line + 1,
			endLine:    symbol.Range.End.Line + 1,
			sel:        symbol.SelectionRange.Start,
			decorators: tsReadDecorators(file.lines, symbol.Range.Start.Line, symbol.SelectionRange.Start.Line),
			summary:    tsReadDoc(file.lines, symbol.Range.Start.Line),
		}
		if kind == "method" {
			decl.params = tsReadParams(file.lines, symbol.SelectionRange.Start)
		}
		file.decls = append(file.decls, decl)
		tsCollect(file, symbol.Children, decl, inImport)
	}
}

// tsNodeKind maps a language server's symbol kind onto the graph's two, or ""
// for a symbol that is not a node.
//
// A variable is the awkward one: `export const handler = async () => {}` is a
// callable and `export const MAX = 10` is not, and the server reports both the
// same way. The source says which, and it says it within the declaration's own
// range, so there is nothing to search.
func tsNodeKind(file *tsFile, symbol lsp.DocumentSymbol) string {
	switch symbol.Kind {
	case lsp.SymbolKindClass, lsp.SymbolKindInterface, lsp.SymbolKindEnum:
		return "class"
	case lsp.SymbolKindMethod, lsp.SymbolKindFunction, lsp.SymbolKindConstructor:
		return "method"
	case lsp.SymbolKindVariable, lsp.SymbolKindConstant:
		if tsLooksCallable(file, symbol.Range) {
			return "method"
		}
	}
	return ""
}

func tsLooksCallable(file *tsFile, where lsp.Range) bool {
	start, end := where.Start.Line, where.End.Line
	if start < 0 || end >= len(file.lines) || start > end {
		return false
	}
	text := strings.Join(file.lines[start:end+1], "\n")
	return strings.Contains(text, "=>") || strings.Contains(text, "function")
}

// tsAssignIDs gives every declaration its id and label.
//
// The shape matches the Java analyzer's, because the whole query layer was
// written against it: a class's label is its qualified name, a method's is
// "<class>.<name>", and the id adds the parameter list so two declarations
// that differ only in shape stay two nodes. What is qualified BY differs — a
// TypeScript module is a path, not a dotted package — so a label reads
// "src/users/users.controller.UsersController", which still ends in the
// simple name every lookup matches on.
func tsAssignIDs(p *tsProject) {
	used := map[string]bool{}
	unique := func(base string) string {
		if !used[base] {
			used[base] = true
			return base
		}
		for n := 2; ; n++ {
			candidate := fmt.Sprintf("%s [#%d]", base, n)
			if !used[candidate] {
				used[candidate] = true
				return candidate
			}
		}
	}

	for _, file := range p.files {
		for _, decl := range file.decls {
			container := file.module
			if decl.owner != nil {
				container = decl.owner.label
			}
			decl.label = container + "." + decl.name
			if decl.kind == "class" {
				decl.nodeID = unique(decl.label)
				continue
			}
			names := make([]string, 0, len(decl.params))
			for _, param := range decl.params {
				names = append(names, param.Name)
			}
			decl.nodeID = unique(decl.label + "(" + strings.Join(names, ",") + ")")
		}
	}
}

// tsPlaceCalls says which declaration each call sits in.
//
// Innermost wins: a call inside a route handler inside a registration function
// belongs to the handler, which is the node a flow should start from.
func tsPlaceCalls(p *tsProject) {
	for _, file := range p.files {
		for i := range file.calls {
			file.calls[i].Enclosing = tsInnermostAt(file, file.calls[i].Line)
		}
	}
}

// tsInnermostAt is the smallest declaration covering a zero-based line.
func tsInnermostAt(f *tsFile, line int) *tsDecl {
	var found *tsDecl
	for _, decl := range f.decls {
		if decl.line-1 > line || decl.endLine-1 < line {
			continue
		}
		if found == nil || (decl.endLine-decl.line) <= (found.endLine-found.line) {
			found = decl
		}
	}
	return found
}

// tsResolveImports asks the server where each import points.
//
// Through the server rather than by hand, because module resolution in
// TypeScript is not a path join: tsconfig `paths`, `baseUrl`, a package's
// `exports` map, extension inference, NodeNext's `.js` meaning `.ts`, and a
// barrel file re-exporting a directory. Reimplementing that would be wrong in
// ways that silently delete edges; the compiler already implements it.
func tsResolveImports(ctx context.Context, conn *lsp.Conn, p *tsProject) error {
	return tsEach(ctx, p.files, func(file *tsFile) error {
		for i := range file.imports {
			imp := &file.imports[i]
			locations, err := conn.Definition(ctx, file.uri, lsp.Position{Line: imp.Line, Character: imp.SpecChar})
			if err != nil || len(locations) == 0 {
				continue
			}
			target := p.byPath[lsp.PathFor(locations[0].URI)]
			if target != nil && target != file {
				imp.Target = target
			}
		}
		return nil
	})
}

func tsApplyFrameworks(p *tsProject, result *AnalysisResult) {
	var active []tsFramework
	for _, framework := range tsFrameworks() {
		if framework.Detect(p) {
			active = append(active, framework)
		}
	}

	used := map[string]bool{}
	for _, file := range p.files {
		for _, framework := range active {
			for _, endpoint := range framework.Endpoints(p, file) {
				endpoint.ID = luaEndpointID(endpoint.HTTPMethod+" "+endpoint.Path, file.module, used)
				result.Endpoints = append(result.Endpoints, endpoint)
			}
			result.Edges = append(result.Edges, framework.Edges(p, file)...)
		}
		for _, decl := range file.decls {
			decl.layer = tsLayerFor(active, p, file, decl)
		}
	}
}

// tsLayerFor asks each framework in turn and takes the first opinion, falling
// back to what the file is called.
func tsLayerFor(active []tsFramework, p *tsProject, file *tsFile, decl *tsDecl) string {
	for _, framework := range active {
		if layer := framework.Layer(p, file, decl); layer != "" {
			return layer
		}
	}
	// A method takes its class's layer, the way it does for Java and Lua.
	if decl.owner != nil && decl.owner.layer != "" {
		return decl.owner.layer
	}
	if layer := tsGenericLayer(file); layer != "" {
		return layer
	}
	return "unknown"
}

func tsEmitNodes(p *tsProject, result *AnalysisResult) {
	for _, file := range p.files {
		for _, decl := range file.decls {
			result.Nodes = append(result.Nodes, Node{
				ID:       decl.nodeID,
				Label:    decl.label,
				Kind:     decl.kind,
				Layer:    decl.layer,
				FilePath: file.rel,
				Line:     decl.line,
				EndLine:  decl.endLine,
				Summary:  decl.summary,
				Params:   decl.params,
			})
		}
	}
}

func tsEmitDependencies(p *tsProject, result *AnalysisResult) {
	for _, file := range p.files {
		seen := map[string]bool{}
		for _, imp := range file.imports {
			if imp.Target == nil || seen[imp.Target.rel] {
				continue
			}
			seen[imp.Target.rel] = true
			result.FileDependencies = append(result.FileDependencies, FileDependency{
				FilePath:          file.rel,
				DependsOnFilePath: imp.Target.rel,
			})
		}
	}
}

// tsEmitFields records the reads and writes of a class's own fields, which is
// what the data-flow trace follows. `this.x` and nothing else: a field on
// another object belongs to that object's class, and attributing it here would
// put it on the wrong one.
func tsEmitFields(p *tsProject, result *AnalysisResult) {
	for _, file := range p.files {
		for _, decl := range file.decls {
			if decl.kind != "method" || decl.owner == nil || len(decl.owner.properties) == 0 {
				continue
			}
			start, end := decl.line-1, decl.endLine
			if start < 0 || end > len(file.lines) {
				continue
			}
			for offset, line := range file.lines[start:end] {
				for _, access := range tsFieldAccessesOn(line, decl.owner.properties) {
					result.FieldAccesses = append(result.FieldAccesses, FieldAccess{
						ClassID:   decl.owner.nodeID,
						NodeID:    decl.nodeID,
						FieldName: access.name,
						Kind:      access.kind,
						Line:      start + offset + 1,
					})
				}
			}
		}
	}
}

type tsFieldAccess struct {
	name string
	kind string
}

// tsFieldAccessesOn finds `this.name` on one line and decides whether it is
// read or written. A call — `this.repo.find()` — is neither: it is a use of
// the field, and the call it makes is already an edge.
func tsFieldAccessesOn(line string, properties map[string]bool) []tsFieldAccess {
	var found []tsFieldAccess
	for at := 0; ; {
		next := strings.Index(line[at:], "this.")
		if next < 0 {
			return found
		}
		at += next + len("this.")
		scan := newTSScanner(line[at:])
		name := scan.readIdentifier()
		if name == "" || !properties[name] {
			continue
		}
		rest := strings.TrimSpace(line[at+len(name):])
		kind := "read"
		if tsIsAssignment(rest) {
			kind = "write"
		}
		found = append(found, tsFieldAccess{name: name, kind: kind})
	}
}

// tsIsAssignment tells `this.x = 1` from `this.x == 1`, `this.x === 1` and
// `this.x => …`, and counts the compound assignments as writes too.
func tsIsAssignment(rest string) bool {
	switch {
	case strings.HasPrefix(rest, "=="), strings.HasPrefix(rest, "=>"):
		return false
	case strings.HasPrefix(rest, "="):
		return true
	}
	for _, operator := range []string{"+=", "-=", "*=", "/=", "%=", "??=", "||=", "&&=", "++", "--"} {
		if strings.HasPrefix(rest, operator) {
			return true
		}
	}
	return false
}

// tsEmitCalls asks the checker what every callable calls.
//
// Two requests per callable: one to turn a position into a handle, one to ask
// what that handle calls. There is no third tier and no fallback by name — the
// checker either resolved the call or the code does not compile, which is why
// this analyzer has no "unresolved" call sites. A target outside the project
// is a wall, and gets the same "library" reason the other analyzers use.
func tsEmitCalls(ctx context.Context, conn *lsp.Conn, p *tsProject, result *AnalysisResult) error {
	byLocation := map[string]*tsDecl{}
	for _, file := range p.files {
		for _, decl := range file.decls {
			byLocation[tsLocationKey(file.abs, decl.sel)] = decl
		}
	}

	var (
		mu    sync.Mutex
		edges = map[string]bool{}
	)
	return tsEach(ctx, p.files, func(file *tsFile) error {
		for _, decl := range file.decls {
			if decl.kind != "method" {
				continue
			}
			items, err := conn.PrepareCallHierarchy(ctx, file.uri, decl.sel)
			if err != nil || len(items) == 0 {
				continue
			}
			calls, err := conn.OutgoingCalls(ctx, items[0])
			if err != nil {
				continue
			}
			for _, call := range calls {
				target := byLocation[tsLocationKey(lsp.PathFor(call.To.URI), call.To.SelectionRange.Start)]
				mu.Lock()
				for _, from := range call.FromRanges {
					site := CallSite{From: decl.nodeID, Line: from.Start.Line + 1}
					if target != nil {
						site.To = target.nodeID
					} else {
						site.Unresolved = "library"
					}
					result.CallSites = append(result.CallSites, site)
				}
				if target != nil {
					key := decl.nodeID + "\x00" + target.nodeID
					if !edges[key] {
						edges[key] = true
						result.Edges = append(result.Edges, Edge{From: decl.nodeID, To: target.nodeID, Kind: "call"})
					}
				}
				mu.Unlock()
			}
		}
		return nil
	})
}

func tsLocationKey(absPath string, at lsp.Position) string {
	return fmt.Sprintf("%s:%d:%d", filepath.ToSlash(absPath), at.Line, at.Character)
}
