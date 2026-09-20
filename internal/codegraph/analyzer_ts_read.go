// internal/codegraph/analyzer_ts_read.go
package codegraph

import (
	"strings"

	"devkit/internal/lsp"
)

// Reading the things a language server does not say.
//
// documentSymbol gives names, kinds and ranges, and nothing else: its `detail`
// comes back empty from tsgo, so a signature has to be read from the source.
// So do decorators, which no LSP request exposes at all.
//
// This is text work, and it is bounded text work — which is the only reason it
// is acceptable. The server has already said exactly where each declaration
// starts and where its name is, so nothing here searches for a declaration or
// guesses at nesting. A decorator is whatever sits between those two points,
// and a parameter list is whatever is inside the parentheses after the name.

// tsReadDecorators reads the decorators attached to a declaration.
//
// The window is the server's: a decorated declaration's range STARTS at its
// first decorator, while its selectionRange starts at its name, so everything
// between the two is decoration and nothing else can be. That is worth more
// than it sounds — the alternative is walking backwards from the declaration
// balancing brackets, and getting `@Inject(TOKEN) private x: X` in a
// constructor's parameter list wrong.
func tsReadDecorators(lines []string, fromLine, toLine int) []tsDecorator {
	if fromLine < 0 || toLine > len(lines) || fromLine >= toLine {
		return nil
	}
	text := strings.Join(lines[fromLine:toLine], "\n")

	var decorators []tsDecorator
	scan := newTSScanner(text)
	for !scan.done() {
		if scan.peek() != '@' {
			scan.advance()
			continue
		}
		at := scan.pos
		scan.advance() // past '@'
		name := scan.readIdentifier()
		if name == "" {
			continue
		}
		decorator := tsDecorator{Name: name, Line: fromLine + strings.Count(text[:at], "\n")}
		scan.skipSpace()
		if !scan.done() && scan.peek() == '(' {
			if args, ok := scan.readArgs(); ok {
				decorator.Args = args
			}
		}
		decorators = append(decorators, decorator)
	}
	return decorators
}

// tsReadParams reads a callable's parameter list, starting from where the
// server said its name is.
//
// Types come with them, which is the one thing TypeScript gives this module
// that Lua could not: a parameter reads as "dto: CreateUserDto" in the
// inspector rather than as a bare name. The types are also what a
// dependency-injection framework needs — a NestJS constructor's parameter
// types ARE its dependencies.
func tsReadParams(lines []string, sel lsp.Position) []Param {
	if sel.Line < 0 || sel.Line >= len(lines) {
		return nil
	}
	text := strings.Join(lines[sel.Line:], "\n")
	scan := newTSTypeScanner(text)
	// Past the name itself, then to the opening parenthesis. A generic
	// signature puts type parameters in between, which readArgs's bracket
	// tracking steps over.
	for !scan.done() && scan.peek() != '(' && scan.peek() != '{' && scan.peek() != '\n' {
		if scan.peek() == '<' {
			if !scan.skipBracketed('<', '>') {
				return nil
			}
			continue
		}
		scan.advance()
	}
	if scan.done() || scan.peek() != '(' {
		return nil
	}
	args, ok := scan.readArgs()
	if !ok {
		return nil
	}

	params := make([]Param, 0, len(args))
	for _, arg := range args {
		if param, ok := tsParseParam(arg); ok {
			params = append(params, param)
		}
	}
	return params
}

// tsParseParam turns one parameter's source into a name and a type.
//
// It has to survive everything TypeScript allows in that position: parameter
// decorators (`@Body() dto: CreateUserDto`), the accessibility modifiers that
// make a parameter a property (`private readonly repo: Repo`), optionality,
// defaults, and destructuring.
func tsParseParam(text string) (Param, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Param{}, false
	}

	// Decorators first: they are prefixes, and each may carry arguments.
	// They are also where the parameter says what part of the request it is,
	// so the name and the first argument are kept rather than stepped over.
	var source, wire string
	for strings.HasPrefix(text, "@") {
		scan := newTSScanner(text)
		scan.advance()
		name := scan.readIdentifier()
		scan.skipSpace()
		var args []string
		if !scan.done() && scan.peek() == '(' {
			read, ok := scan.readArgs()
			if !ok {
				break
			}
			args = read
		}
		if bound, ok := tsParamSources[name]; ok && source == "" {
			source = bound
			if len(args) > 0 {
				wire = tsStringLiteral(args[0])
			}
		}
		text = strings.TrimSpace(text[scan.pos:])
	}

	for _, modifier := range []string{"public ", "private ", "protected ", "readonly ", "override ", "..."} {
		for strings.HasPrefix(text, modifier) {
			text = strings.TrimSpace(strings.TrimPrefix(text, modifier))
		}
	}

	// A default value is not part of the signature a reader wants.
	if name, _, found := tsCutTopLevel(text, '='); found {
		text = strings.TrimSpace(name)
	}
	name, typeName, found := tsCutTopLevel(text, ':')
	name = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(name), "?"))
	if name == "" {
		return Param{}, false
	}
	param := Param{Name: name, Source: source}
	if found {
		param.Type = strings.TrimSpace(typeName)
	}
	if wire != "" && wire != name {
		param.WireName = wire
	}
	return param, true
}

// tsParamSources is NestJS's parameter decorators, which are the same shapes
// Spring's annotations are.
//
// Only the ones that name a part of the request are here. `@Req`, `@Res`,
// `@Ip`, `@Session` and the file decorators are the framework handing the
// handler something that is not a value on the wire, so they read as `other`
// and a draft leaves them alone. A decorator that is not in this table at all
// — a project's own `@CurrentUser`, an `@Inject` on a constructor — leaves the
// source EMPTY, which says "this analyzer cannot tell" rather than "this is
// not part of the request".
var tsParamSources = map[string]string{
	"Body":          ParamSourceBody,
	"Param":         ParamSourcePath,
	"Query":         ParamSourceQuery,
	"Headers":       ParamSourceHeader,
	"Header":        ParamSourceHeader,
	"Req":           ParamSourceOther,
	"Request":       ParamSourceOther,
	"Res":           ParamSourceOther,
	"Response":      ParamSourceOther,
	"Next":          ParamSourceOther,
	"Ip":            ParamSourceOther,
	"Session":       ParamSourceOther,
	"HostParam":     ParamSourceOther,
	"UploadedFile":  ParamSourceOther,
	"UploadedFiles": ParamSourceOther,
}

// tsReadProperties reads a class's own fields, with their types, for a body
// skeleton.
//
// Bounded text work like everything else in this file: the server has already
// said where the class starts and ends, and a property is a line inside it
// that declares a name and a type and is not a method. Anything with a
// parenthesis before the colon is a method or a constructor and is skipped —
// a skeleton made of a DTO's methods is worse than an empty one.
func tsReadProperties(lines []string, line, endLine int) []Field {
	if line < 1 || endLine > len(lines) || line > endLine {
		return nil
	}
	var fields []Field
	for _, raw := range lines[line:min(endLine, len(lines))] {
		text := strings.TrimSpace(raw)
		if text == "" || strings.HasPrefix(text, "//") || strings.HasPrefix(text, "*") ||
			strings.HasPrefix(text, "/*") || strings.HasPrefix(text, "@") {
			continue
		}
		for _, modifier := range []string{"public ", "private ", "protected ", "readonly ", "static ", "declare "} {
			for strings.HasPrefix(text, modifier) {
				text = strings.TrimSpace(strings.TrimPrefix(text, modifier))
			}
		}
		name, typeName, found := tsCutTopLevel(text, ':')
		if !found {
			continue
		}
		name = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(name), "?"))
		name = strings.TrimSuffix(name, "!")
		if name == "" || strings.ContainsAny(name, "()[]{}<>=,;\"'` ") {
			continue
		}
		typeName = strings.TrimSpace(typeName)
		if cut, _, ok := tsCutTopLevel(typeName, '='); ok {
			typeName = strings.TrimSpace(cut)
		}
		typeName = strings.TrimSuffix(strings.TrimSpace(typeName), ";")
		typeName = strings.TrimSpace(typeName)
		if typeName == "" {
			continue
		}
		fields = append(fields, Field{Name: name, Type: typeName})
	}
	return fields
}

// tsReadDoc is the JSDoc block immediately above a declaration — the same
// place a Javadoc sits, and the only doc comment TypeScript has.
func tsReadDoc(lines []string, line int) string {
	end := line - 1
	for end >= 0 && strings.TrimSpace(lines[end]) == "" {
		end--
	}
	if end < 0 || !strings.HasSuffix(strings.TrimSpace(lines[end]), "*/") {
		return ""
	}
	start := end
	for start >= 0 && !strings.HasPrefix(strings.TrimSpace(lines[start]), "/*") {
		start--
	}
	if start < 0 {
		return ""
	}

	var parts []string
	for _, raw := range lines[start : end+1] {
		text := strings.TrimSpace(raw)
		text = strings.TrimPrefix(text, "/**")
		text = strings.TrimPrefix(text, "/*")
		text = strings.TrimSuffix(text, "*/")
		text = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), "*"))
		if strings.HasPrefix(text, "@") {
			break // a JSDoc tag ends the description, as in Javadoc
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

// tsImportLines reports which lines belong to an import or re-export
// statement, and the specifiers they name.
//
// The line set matters as much as the specifiers: tsgo reports every imported
// NAME as a variable symbol of the file, so without knowing which lines are
// imports, a file's symbol list starts with one phantom declaration per
// imported identifier.
func tsImportLines(lines []string) (map[int]bool, []tsImport) {
	inImport := map[int]bool{}
	var imports []tsImport

	statementStart := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if statementStart < 0 && (strings.HasPrefix(trimmed, "import") || strings.HasPrefix(trimmed, "export")) {
			statementStart = i
		}
		if statementStart < 0 {
			// A require() or a dynamic import() can sit anywhere.
			if spec, char, ok := tsSpecifierOn(line, "require(", "import("); ok {
				imports = append(imports, tsImport{Spec: spec, Line: i, SpecChar: lsp.UTF16Offset(line, char)})
			}
			continue
		}

		spec, char, found := tsSpecifierOn(line, "from ", "from'", "from\"", "import ", "import'", "import\"")
		if found {
			for l := statementStart; l <= i; l++ {
				inImport[l] = true
			}
			imports = append(imports, tsImport{Spec: spec, Line: i, SpecChar: lsp.UTF16Offset(line, char)})
			statementStart = -1
			continue
		}
		// An `export class Foo {` is not an import statement; only keep
		// looking while the statement could still turn out to be one.
		if strings.HasSuffix(trimmed, ";") || (statementStart == i && !tsCouldBeImportStart(trimmed)) {
			statementStart = -1
		}
	}
	return inImport, imports
}

// tsCouldBeImportStart rules out the `export` statements that declare
// something rather than re-export it.
func tsCouldBeImportStart(trimmed string) bool {
	if strings.HasPrefix(trimmed, "import") {
		return true
	}
	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "export"))
	for _, declaration := range []string{"class ", "const ", "let ", "var ", "function", "async ", "interface ", "enum ", "type ", "abstract ", "default "} {
		if strings.HasPrefix(rest, declaration) {
			return false
		}
	}
	return true
}

// tsSpecifierOn finds a quoted module specifier that follows one of the given
// keywords on a line, and where its text begins.
func tsSpecifierOn(line string, keywords ...string) (string, int, bool) {
	for _, keyword := range keywords {
		at := strings.Index(line, keyword)
		if at < 0 {
			continue
		}
		rest := line[at+len(keyword):]
		for i := 0; i < len(rest); i++ {
			c := rest[i]
			if c == ' ' || c == '\t' {
				continue
			}
			if c != '\'' && c != '"' && c != '`' {
				break
			}
			end := strings.IndexByte(rest[i+1:], c)
			if end < 0 {
				break
			}
			return rest[i+1 : i+1+end], at + len(keyword) + i + 1, true
		}
	}
	return "", 0, false
}

// tsSplitTopLevel splits on every separator that is not inside a bracket, a
// string or a template — what an array or object literal's elements need.
func tsSplitTopLevel(text string, sep byte) []string {
	var parts []string
	scan := newTSTypeScanner(text)
	start := 0
	for !scan.done() {
		if scan.peek() == sep && scan.depth == 0 {
			parts = append(parts, text[start:scan.pos])
			scan.advance()
			start = scan.pos
			continue
		}
		scan.advance()
	}
	if rest := strings.TrimSpace(text[start:]); rest != "" {
		parts = append(parts, text[start:])
	}
	return parts
}

// tsCutTopLevel splits on the first occurrence of sep that is not inside a
// bracket, a string or a template — which is what keeps `Map<string, number>`
// one type and `{ a, b }: Options` one parameter.
func tsCutTopLevel(text string, sep byte) (string, string, bool) {
	scan := newTSTypeScanner(text)
	for !scan.done() {
		c := scan.peek()
		if c == sep && scan.depth == 0 {
			return text[:scan.pos], text[scan.pos+1:], true
		}
		scan.advance()
	}
	return text, "", false
}

// tsControlKeywords are the words followed by a parenthesis that are not calls
// — but only when nothing precedes them. After a dot they are member names
// like any other, and several are ones that matter: `router.delete('/x')` is a
// route, `Array.of(...)` is a call, and a generator has `.return()`.
//
// Keeping the bare forms out is not required — a framework filters by the name
// it cares about — but a call list full of `if` and `catch` is a call list
// nobody can read while debugging one.
var tsControlKeywords = map[string]bool{
	"if": true, "for": true, "while": true, "switch": true, "catch": true,
	"return": true, "typeof": true, "await": true, "function": true,
	"do": true, "with": true, "yield": true, "void": true, "delete": true,
	"in": true, "of": true, "case": true, "instanceof": true,
}

// tsReadCalls finds every call expression in a file, with the path it was made
// on and its arguments as written.
//
// Whole-file text scanning, unlike everything else in this file, because there
// is no bound to work inside: a call can be anywhere. What makes it affordable
// is that it is one linear pass with no backtracking, and what makes it safe
// is the scanner underneath — strings, templates and comments are stepped
// over, and an argument list that never closes rewinds rather than swallowing
// the rest of the file.
//
// It is deliberately generous. A framework filters by the name it cares about,
// so a false positive costs nothing and a miss costs a route.
func tsReadCalls(source string) []tsCall {
	scan := newTSScanner(source)
	var calls []tsCall
	var chain []string

	for !scan.done() {
		c := scan.peek()
		if !isTSNameStart(c) {
			scan.advance()
			chain = nil
			continue
		}

		line := scan.line
		name := scan.readIdentifier()
		scan.skipSpace()
		if scan.done() {
			break
		}
		switch scan.peek() {
		case '.':
			// Another segment of the same path: `this` . `client` . `send`.
			chain = append(chain, name)
			scan.advance()
		case '(':
			if len(chain) == 0 && tsControlKeywords[name] {
				chain = nil
				continue
			}
			if args, ok := scan.readArgs(); ok {
				calls = append(calls, tsCall{
					Receiver: strings.Join(chain, "."),
					Method:   name,
					Args:     args,
					Line:     line,
					EndLine:  scan.line,
				})
			}
			chain = nil
		default:
			chain = nil
		}
	}
	return calls
}

func isTSNameStart(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
