// internal/codegraph/analyzer_ts_model.go
package codegraph

import (
	"strings"

	"devkit/internal/lsp"
)

// The language-level model of a TypeScript project: files, what they declare,
// what decorates each declaration, and what they import.
//
// Nothing here knows what a controller is. That is the whole point of the
// split — a framework is a reading of this model (see analyzer_ts_framework.go),
// not a fork of it. Decorators are a TypeScript feature and are extracted here;
// what `@Controller('users')` MEANS is NestJS's business and lives in its own
// file, beside the next framework's.

// tsProject is everything one analysis knows about the folder it was given.
type tsProject struct {
	root string
	// deps is every package named as a dependency in any package.json under
	// the root. It is how a framework recognises itself without guessing from
	// file names.
	deps  map[string]bool
	files []*tsFile
	// byPath indexes files by absolute path, for turning a server's answer
	// back into one of ours.
	byPath map[string]*tsFile
}

// dependsOn reports whether the project declares any of these packages.
func (p *tsProject) dependsOn(packages ...string) bool {
	for _, name := range packages {
		if p.deps[name] {
			return true
		}
	}
	return false
}

// tsFile is one source file and everything read out of it.
type tsFile struct {
	rel    string // relative to the project root, forward slashes
	abs    string
	uri    string
	source string
	lines  []string
	// module is the name this file is known by in a node's label — its path
	// without the extension, which is how TypeScript itself identifies a
	// module and how a developer would write the import.
	module  string
	decls   []*tsDecl
	imports []tsImport
}

// tsDecl is one declaration: a class, an interface, an enum, or something
// callable. Nested declarations hang off their owner.
type tsDecl struct {
	nodeID string
	label  string
	kind   string // "class" | "method"
	name   string
	// layer is decided after every declaration is known, because a framework
	// needs the whole project to decide one (a class is a repository partly
	// because of what it injects) and a method takes its class's.
	layer string
	file  *tsFile
	// owner is the enclosing class, nil for a top-level declaration.
	owner *tsDecl
	// symbolKind is the LSP kind the server reported, kept because a
	// framework sometimes needs the distinction this package flattens away —
	// a constructor is not a method to a dependency-injection framework.
	symbolKind int
	line       int
	endLine    int
	// sel is where the declaration's NAME is. Every follow-up request has to
	// be made there, so it is carried rather than recomputed.
	sel        lsp.Position
	params     []Param
	decorators []tsDecorator
	summary    string
	// properties are the fields a class declares, used to tell a real field
	// access from a method call on this.
	properties map[string]bool
}

// decorator returns the decorator with one of these names, and whether there
// was one. Names are matched exactly: TypeScript decorators are identifiers,
// and a case-insensitive match would confuse a project's own `get` helper with
// a framework's `Get`.
func (d *tsDecl) decorator(names ...string) (tsDecorator, bool) {
	for _, decorator := range d.decorators {
		for _, name := range names {
			if decorator.Name == name {
				return decorator, true
			}
		}
	}
	return tsDecorator{}, false
}

// tsDecorator is `@Name(args)` as written. Args are the top-level argument
// expressions, still as source text — a framework that wants a route path
// takes the first string literal, one that wants an options object reads the
// text, and neither needs a parser here.
type tsDecorator struct {
	Name string
	Args []string
	Line int
}

// StringArg is the i-th argument when it is a string literal, "" otherwise.
// `@Controller('users')` and `@Get(":id")` are the same shape to a caller.
func (d tsDecorator) StringArg(i int) string {
	if i >= len(d.Args) {
		return ""
	}
	return tsStringLiteral(d.Args[i])
}

// StringArgs is the i-th argument's string literals: one for a plain literal,
// several for an array of them, none for anything else.
//
// The array form is not exotic — `@Controller(['legacy', 'v2'])` and
// `@Get(['a', 'b'])` are how one handler serves several paths, and reading
// only the first (or, worse, none) reports a route the service does not
// serve.
func (d tsDecorator) StringArgs(i int) []string {
	if i >= len(d.Args) {
		return nil
	}
	return tsStringLiterals(d.Args[i])
}

// tsStringLiterals reads one argument as a list of strings: a plain literal is
// a list of one, an array literal is its string elements, and anything else is
// no list at all.
func tsStringLiterals(text string) []string {
	text = strings.TrimSpace(text)
	if literal := tsStringLiteral(text); literal != "" {
		return []string{literal}
	}
	if !strings.HasPrefix(text, "[") || !strings.HasSuffix(text, "]") {
		return nil
	}
	var found []string
	for _, element := range tsSplitTopLevel(text[1:len(text)-1], ',') {
		if literal := tsStringLiteral(element); literal != "" {
			found = append(found, literal)
		}
	}
	return found
}

// tsObjectSummary turns an object literal into something a sidebar can show:
// `{ cmd: 'divide' }` becomes "cmd: divide".
//
// A message pattern is as often an object as a string — `@MessagePattern({ cmd
// 'divide' })` is the idiom for a request/response handler — and the handler's
// own name is a poor substitute for the pattern a caller has to send.
func tsObjectSummary(text string) string {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "{") || !strings.HasSuffix(text, "}") {
		return ""
	}
	var parts []string
	for _, field := range tsSplitTopLevel(text[1:len(text)-1], ',') {
		key, value, found := tsCutTopLevel(field, ':')
		if !found {
			continue
		}
		key = strings.Trim(strings.TrimSpace(key), `'"`+"`")
		value = strings.TrimSpace(value)
		if literal := tsStringLiteral(value); literal != "" {
			value = literal
		}
		if key == "" || value == "" {
			continue
		}
		parts = append(parts, key+": "+value)
	}
	return strings.Join(parts, ", ")
}

// tsImport is one module this file pulls in.
type tsImport struct {
	// Spec is the specifier as written: "./users.service", "@nestjs/common".
	Spec string
	Line int
	// SpecChar is where the specifier's text starts, in the units the language
	// server counts in, so a definition request can be made at it.
	SpecChar int
	// Target is the project file it resolved to, nil when it resolves outside
	// the project (a package) or not at all.
	Target *tsFile
}

// tsStringLiteral unwraps a quoted string, or returns "" for an expression
// that is not one. All three of TypeScript's quote characters count.
func tsStringLiteral(text string) string {
	text = strings.TrimSpace(text)
	if len(text) < 2 {
		return ""
	}
	quote := text[0]
	if quote != '\'' && quote != '"' && quote != '`' {
		return ""
	}
	if text[len(text)-1] != quote {
		return ""
	}
	inner := text[1 : len(text)-1]
	if strings.ContainsRune(inner, rune(quote)) {
		// Not one literal but two, or an escape this does not decode. Either
		// way it is not a plain string and a framework should not treat it as
		// a route path.
		return ""
	}
	return inner
}

// tsModuleName is the name a file is known by: its path without the extension.
//
// Paths rather than dots, unlike Java and Lua, because that is how TypeScript
// identifies a module and how the import that reaches it is written. `.d.ts`
// loses both suffixes, and an `index` file is named by its directory, which is
// what the import for it says.
func tsModuleName(rel string) string {
	name := rel
	for _, ext := range []string{".d.ts", ".tsx", ".ts", ".d.mts", ".mts", ".cts", ".jsx", ".js", ".mjs", ".cjs"} {
		if strings.HasSuffix(name, ext) {
			name = strings.TrimSuffix(name, ext)
			break
		}
	}
	if trimmed := strings.TrimSuffix(name, "/index"); trimmed != name {
		return trimmed
	}
	if name == "index" {
		return "."
	}
	return name
}
