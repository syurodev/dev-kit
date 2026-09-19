// internal/lua/parser.go
package lua

import (
	"fmt"
	"strings"
)

// A recursive-descent parser for the whole Lua grammar, with precedence
// climbing for expressions.
//
// Two properties matter more here than they would in a compiler's front end.
//
// It recovers. Parse never returns an error: a statement it cannot read
// becomes a diagnostic and the parser resynchronises at the next statement
// boundary. An analyzer exists to answer questions about code that is being
// worked on, which is code that is sometimes mid-edit, and a file that
// disappears from the index because of one typo is worse than a file missing
// one function.
//
// It is bounded. maxDepth stops a pathological file — generated data, a
// thousand nested parentheses — from taking the stack down with it. The
// process this runs in is the desktop app, not a batch job.

const maxDepth = 200

// Parse reads one Lua chunk. The result always has a Block (possibly empty)
// and lists everything that could not be read in Diagnostics.
func Parse(src string) *Chunk {
	lex := newLexer(src)
	p := &parser{toks: lex.scan()}
	chunk := &Chunk{Lines: countLines(src)}
	chunk.Block = p.parseBlock()
	if !p.at(tokEOF) {
		p.note(p.peek().line, "unexpected %s after the end of the chunk", describe(p.peek()))
		// Everything left is unreachable by the grammar, but a `end` too many
		// is a common mid-edit state and the rest of the file may still parse.
		for !p.at(tokEOF) {
			p.advance()
			chunk.Block = append(chunk.Block, p.parseBlock()...)
		}
	}
	chunk.Diagnostics = append(lex.diagnostics, p.diagnostics...)
	return chunk
}

func countLines(src string) int {
	if src == "" {
		return 0
	}
	return strings.Count(src, "\n") + 1
}

type parser struct {
	toks        []token
	pos         int
	depth       int
	diagnostics []Diagnostic
}

func (p *parser) peek() token { return p.toks[p.pos] }

func (p *parser) advance() token {
	tok := p.toks[p.pos]
	if tok.kind != tokEOF {
		p.pos++
	}
	return tok
}

func (p *parser) at(kind tokenKind) bool { return p.peek().kind == kind }

func (p *parser) atOp(text string) bool {
	tok := p.peek()
	return tok.kind == tokOp && tok.text == text
}

func (p *parser) atKeyword(text string) bool {
	tok := p.peek()
	return tok.kind == tokKeyword && tok.text == text
}

// accept consumes the token when it is the operator or keyword asked for.
func (p *parser) accept(text string) bool {
	if p.atOp(text) || p.atKeyword(text) {
		p.advance()
		return true
	}
	return false
}

// expect consumes the expected token or records why it could not. The caller
// decides whether that is fatal to the statement it is building.
func (p *parser) expect(text string) bool {
	if p.accept(text) {
		return true
	}
	p.note(p.peek().line, "expected %q, found %s", text, describe(p.peek()))
	return false
}

func (p *parser) expectName() (string, bool) {
	if p.at(tokName) {
		return p.advance().text, true
	}
	p.note(p.peek().line, "expected a name, found %s", describe(p.peek()))
	return "", false
}

func (p *parser) note(line int, format string, args ...any) {
	// Same rule as the lexer's: one diagnostic per line, so a cascade cannot
	// bury the first cause.
	if n := len(p.diagnostics); n > 0 && p.diagnostics[n-1].Line == line {
		return
	}
	p.diagnostics = append(p.diagnostics, Diagnostic{Line: line, Message: fmt.Sprintf(format, args...)})
}

func describe(tok token) string {
	switch tok.kind {
	case tokEOF:
		return "the end of the file"
	case tokString:
		return "a string"
	case tokNumber:
		return "a number"
	case tokName:
		return "name " + tok.text
	default:
		return fmt.Sprintf("%q", tok.text)
	}
}

// blockEnd reports the tokens that close a block. `return` is not one — it is
// the last statement inside a block, not after it.
func (p *parser) blockEnd() bool {
	if p.at(tokEOF) {
		return true
	}
	tok := p.peek()
	if tok.kind != tokKeyword {
		return false
	}
	switch tok.text {
	case "end", "else", "elseif", "until":
		return true
	}
	return false
}

func (p *parser) parseBlock() []Stmt {
	if p.depth++; p.depth > maxDepth {
		p.depth--
		p.note(p.peek().line, "nesting is too deep to read")
		p.skipToBlockEnd()
		return nil
	}
	defer func() { p.depth-- }()

	var block []Stmt
	for !p.blockEnd() {
		before := p.pos
		stmt, ok := p.parseStatement()
		if stmt != nil {
			block = append(block, stmt)
		}
		if !ok {
			p.resynchronise(before)
			continue
		}
		if _, isReturn := stmt.(*ReturnStmt); isReturn {
			// `return` must be the last statement in its block. Anything after
			// it is a new block's worth of statements to whoever is above.
			return block
		}
		if p.pos == before {
			// A statement that consumed nothing would spin here forever.
			p.note(p.peek().line, "unexpected %s", describe(p.peek()))
			p.advance()
		}
	}
	return block
}

// resynchronise skips forward to the next plausible statement boundary after a
// failed parse, guaranteeing at least one token of progress.
func (p *parser) resynchronise(before int) {
	if p.pos == before {
		p.advance()
	}
	for !p.at(tokEOF) && !p.blockEnd() {
		tok := p.peek()
		if tok.kind == tokKeyword {
			switch tok.text {
			case "local", "function", "if", "while", "for", "do", "repeat", "return", "break", "goto":
				return
			}
		}
		if tok.kind == tokOp && tok.text == ";" {
			p.advance()
			return
		}
		p.advance()
	}
}

func (p *parser) skipToBlockEnd() {
	for !p.at(tokEOF) && !p.blockEnd() {
		p.advance()
	}
}

// parseStatement returns the statement and whether it was read cleanly. A
// false with a non-nil statement is deliberate: `function f() <broken> end`
// still declares f, and the node is worth having.
func (p *parser) parseStatement() (Stmt, bool) {
	tok := p.peek()
	line := tok.line

	if tok.kind == tokOp {
		switch tok.text {
		case ";":
			p.advance()
			return nil, true
		case "::":
			p.advance()
			name, ok := p.expectName()
			if ok {
				ok = p.expect("::")
			}
			return &LabelStmt{Position: Position{line, p.prevLine()}, Name: name}, ok
		}
	}

	if tok.kind == tokKeyword {
		switch tok.text {
		case "break":
			p.advance()
			return &BreakStmt{Position: Position{line, line}}, true
		case "goto":
			p.advance()
			name, ok := p.expectName()
			return &GotoStmt{Position: Position{line, p.prevLine()}, Label: name}, ok
		case "do":
			p.advance()
			body := p.parseBlock()
			ok := p.expect("end")
			return &DoStmt{Position: Position{line, p.prevLine()}, Body: body}, ok
		case "while":
			return p.parseWhile()
		case "repeat":
			return p.parseRepeat()
		case "if":
			return p.parseIf()
		case "for":
			return p.parseFor()
		case "function":
			return p.parseFunctionStmt()
		case "local":
			return p.parseLocal()
		case "return":
			return p.parseReturn()
		}
	}

	return p.parseExprStatement()
}

// prevLine is the line the last consumed token ended on — where a statement
// that has just been closed by `end` actually finishes.
func (p *parser) prevLine() int {
	if p.pos == 0 {
		return p.toks[0].line
	}
	return p.toks[p.pos-1].endLine
}

func (p *parser) parseWhile() (Stmt, bool) {
	line := p.advance().line // while
	cond := p.parseExpr()
	ok := p.expect("do")
	body := p.parseBlock()
	ok = p.expect("end") && ok
	return &WhileStmt{Position: Position{line, p.prevLine()}, Cond: cond, Body: body}, ok
}

func (p *parser) parseRepeat() (Stmt, bool) {
	line := p.advance().line // repeat
	body := p.parseBlock()
	ok := p.expect("until")
	cond := p.parseExpr()
	return &RepeatStmt{Position: Position{line, p.prevLine()}, Body: body, Cond: cond}, ok
}

func (p *parser) parseIf() (Stmt, bool) {
	line := p.advance().line // if
	stmt := &IfStmt{Position: Position{line, line}}
	ok := true
	for {
		cond := p.parseExpr()
		ok = p.expect("then") && ok
		stmt.Clauses = append(stmt.Clauses, IfClause{Cond: cond, Body: p.parseBlock()})
		if !p.accept("elseif") {
			break
		}
	}
	if p.accept("else") {
		stmt.HasElse = true
		stmt.Else = p.parseBlock()
	}
	ok = p.expect("end") && ok
	stmt.EndLine = p.prevLine()
	return stmt, ok
}

func (p *parser) parseFor() (Stmt, bool) {
	line := p.advance().line // for
	first, ok := p.expectName()
	if !ok {
		return nil, false
	}

	if p.accept("=") {
		stmt := &NumericForStmt{Position: Position{line, line}, Name: first}
		stmt.Start = p.parseExpr()
		ok = p.expect(",")
		stmt.Limit = p.parseExpr()
		if p.accept(",") {
			stmt.Step = p.parseExpr()
		}
		ok = p.expect("do") && ok
		stmt.Body = p.parseBlock()
		ok = p.expect("end") && ok
		stmt.EndLine = p.prevLine()
		return stmt, ok
	}

	stmt := &GenericForStmt{Position: Position{line, line}, Names: []string{first}}
	for p.accept(",") {
		name, named := p.expectName()
		if !named {
			return stmt, false
		}
		stmt.Names = append(stmt.Names, name)
	}
	ok = p.expect("in")
	stmt.Exprs = p.parseExprList()
	ok = p.expect("do") && ok
	stmt.Body = p.parseBlock()
	ok = p.expect("end") && ok
	stmt.EndLine = p.prevLine()
	return stmt, ok
}

// parseFunctionStmt reads `function a.b.c:d() ... end`.
func (p *parser) parseFunctionStmt() (Stmt, bool) {
	line := p.advance().line // function
	name, ok := p.expectName()
	if !ok {
		return nil, false
	}
	stmt := &FuncStmt{Position: Position{line, line}, Path: []string{name}}
	for p.accept(".") {
		next, named := p.expectName()
		if !named {
			return stmt, false
		}
		stmt.Path = append(stmt.Path, next)
	}
	if p.accept(":") {
		method, named := p.expectName()
		if !named {
			return stmt, false
		}
		stmt.Method = method
	}
	body, bodyOK := p.parseFuncBody(line, stmt.Method != "")
	stmt.Func = body
	stmt.EndLine = p.prevLine()
	return stmt, bodyOK
}

func (p *parser) parseLocal() (Stmt, bool) {
	line := p.advance().line // local

	if p.accept("function") {
		name, ok := p.expectName()
		if !ok {
			return nil, false
		}
		body, bodyOK := p.parseFuncBody(line, false)
		return &LocalFuncStmt{Position: Position{line, p.prevLine()}, Name: name, Func: body}, bodyOK
	}

	stmt := &LocalStmt{Position: Position{line, line}}
	for {
		name, ok := p.expectName()
		if !ok {
			return stmt, false
		}
		stmt.Names = append(stmt.Names, name)
		attrib := ""
		// Lua 5.4's `<const>` / `<close>`. Read and kept so the name list
		// stays aligned; nothing downstream acts on it yet.
		if p.accept("<") {
			attrib, _ = p.expectName()
			p.expect(">")
		}
		stmt.Attribs = append(stmt.Attribs, attrib)
		if !p.accept(",") {
			break
		}
	}
	if p.accept("=") {
		stmt.Exprs = p.parseExprList()
	}
	stmt.EndLine = p.prevLine()
	return stmt, true
}

func (p *parser) parseReturn() (Stmt, bool) {
	line := p.advance().line // return
	stmt := &ReturnStmt{Position: Position{line, line}}
	if !p.blockEnd() && !p.atOp(";") {
		stmt.Exprs = p.parseExprList()
	}
	p.accept(";")
	stmt.EndLine = p.prevLine()
	return stmt, true
}

// parseExprStatement reads the two statement forms that start with an
// expression: a call, and an assignment to one or more targets.
func (p *parser) parseExprStatement() (Stmt, bool) {
	line := p.peek().line
	first := p.parseSuffixedExpr()
	if first == nil {
		return nil, false
	}

	if p.atOp("=") || p.atOp(",") {
		stmt := &AssignStmt{Position: Position{line, line}, Targets: []Expr{first}}
		for p.accept(",") {
			target := p.parseSuffixedExpr()
			if target == nil {
				return stmt, false
			}
			stmt.Targets = append(stmt.Targets, target)
		}
		if !p.expect("=") {
			return stmt, false
		}
		stmt.Exprs = p.parseExprList()
		stmt.EndLine = p.prevLine()
		return stmt, true
	}

	switch first.(type) {
	case *CallExpr, *MethodCallExpr:
		return &CallStmt{Position: Position{line, p.prevLine()}, Call: first}, true
	}
	p.note(line, "this expression is not a statement")
	return nil, false
}

func (p *parser) parseExprList() []Expr {
	exprs := []Expr{p.parseExpr()}
	for p.accept(",") {
		exprs = append(exprs, p.parseExpr())
	}
	return exprs
}

// Binary operator precedence, left and right, straight from the reference
// manual. A right binding power below the left makes the operator
// right-associative, which is what `..` and `^` need.
var binaryPrecedence = map[string][2]int{
	"or":  {1, 1},
	"and": {2, 2},
	"<":   {3, 3}, ">": {3, 3}, "<=": {3, 3}, ">=": {3, 3}, "~=": {3, 3}, "==": {3, 3},
	"|":  {4, 4},
	"~":  {5, 5},
	"&":  {6, 6},
	"<<": {7, 7}, ">>": {7, 7},
	"..": {9, 8},
	"+":  {10, 10}, "-": {10, 10},
	"*": {11, 11}, "/": {11, 11}, "//": {11, 11}, "%": {11, 11},
	"^": {14, 13},
}

// unaryPrecedence binds tighter than every binary operator but `^`.
const unaryPrecedence = 12

func (p *parser) parseExpr() Expr { return p.parseBinaryExpr(0) }

func (p *parser) parseBinaryExpr(limit int) Expr {
	if p.depth++; p.depth > maxDepth {
		p.depth--
		p.note(p.peek().line, "expression nesting is too deep to read")
		return nil
	}
	defer func() { p.depth-- }()

	var left Expr
	if op, ok := p.unaryOp(); ok {
		line := p.advance().line
		operand := p.parseBinaryExpr(unaryPrecedence)
		left = &UnOpExpr{Position: Position{line, p.prevLine()}, Op: op, Operand: operand}
	} else {
		left = p.parseSimpleExpr()
	}

	for {
		op, ok := p.binaryOp()
		if !ok {
			return left
		}
		prec := binaryPrecedence[op]
		if prec[0] <= limit {
			return left
		}
		line := p.advance().line
		right := p.parseBinaryExpr(prec[1])
		left = &BinOpExpr{Position: Position{line, p.prevLine()}, Op: op, Left: left, Right: right}
	}
}

func (p *parser) unaryOp() (string, bool) {
	tok := p.peek()
	if tok.kind == tokKeyword && tok.text == "not" {
		return "not", true
	}
	if tok.kind == tokOp {
		switch tok.text {
		case "-", "#", "~":
			return tok.text, true
		}
	}
	return "", false
}

func (p *parser) binaryOp() (string, bool) {
	tok := p.peek()
	switch tok.kind {
	case tokKeyword:
		if tok.text == "and" || tok.text == "or" {
			return tok.text, true
		}
	case tokOp:
		if _, ok := binaryPrecedence[tok.text]; ok {
			return tok.text, true
		}
	}
	return "", false
}

func (p *parser) parseSimpleExpr() Expr {
	tok := p.peek()
	line := tok.line
	switch tok.kind {
	case tokNumber:
		p.advance()
		return &NumberExpr{Position: Position{line, tok.endLine}, Text: tok.text}
	case tokString:
		p.advance()
		return &StringExpr{Position: Position{line, tok.endLine}, Value: tok.value, Long: tok.long}
	case tokKeyword:
		switch tok.text {
		case "nil":
			p.advance()
			return &NilExpr{Position: Position{line, line}}
		case "true", "false":
			p.advance()
			return &BoolExpr{Position: Position{line, line}, Value: tok.text == "true"}
		case "function":
			p.advance()
			body, _ := p.parseFuncBody(line, false)
			return body
		}
	case tokOp:
		switch tok.text {
		case "...":
			p.advance()
			return &VarargExpr{Position: Position{line, line}}
		case "{":
			return p.parseTable()
		}
	}
	return p.parseSuffixedExpr()
}

// parsePrimaryExpr reads what a suffix chain can start from: a name, or a
// parenthesised expression.
func (p *parser) parsePrimaryExpr() Expr {
	tok := p.peek()
	switch {
	case tok.kind == tokName:
		p.advance()
		return &NameExpr{Position: Position{tok.line, tok.line}, Name: tok.text}
	case p.atOp("("):
		p.advance()
		inner := p.parseExpr()
		p.expect(")")
		return inner
	}
	p.note(tok.line, "expected an expression, found %s", describe(tok))
	return nil
}

// parseSuffixedExpr reads a primary expression followed by any number of
// indexes and calls: `kong.db.plugins:select(id).name`.
func (p *parser) parseSuffixedExpr() Expr {
	if p.depth++; p.depth > maxDepth {
		p.depth--
		p.note(p.peek().line, "expression nesting is too deep to read")
		return nil
	}
	defer func() { p.depth-- }()

	expr := p.parsePrimaryExpr()
	if expr == nil {
		return nil
	}
	for {
		tok := p.peek()
		switch {
		case p.atOp("."):
			line := p.advance().line
			name, ok := p.expectName()
			if !ok {
				return expr
			}
			expr = &IndexExpr{
				Position: Position{expr.Pos().Line, p.prevLine()},
				Obj:      expr,
				Key:      &StringExpr{Position: Position{line, line}, Value: name},
				Dot:      true,
			}
		case p.atOp("["):
			p.advance()
			key := p.parseExpr()
			p.expect("]")
			expr = &IndexExpr{Position: Position{expr.Pos().Line, p.prevLine()}, Obj: expr, Key: key}
		case p.atOp(":"):
			p.advance()
			name, ok := p.expectName()
			if !ok {
				return expr
			}
			args, argsOK := p.parseArgs()
			if !argsOK {
				return expr
			}
			expr = &MethodCallExpr{
				Position: Position{expr.Pos().Line, p.prevLine()},
				Recv:     expr,
				Method:   name,
				Args:     args,
			}
		case p.atOp("("), p.atOp("{"), tok.kind == tokString:
			args, ok := p.parseArgs()
			if !ok {
				return expr
			}
			expr = &CallExpr{Position: Position{expr.Pos().Line, p.prevLine()}, Func: expr, Args: args}
		default:
			return expr
		}
	}
}

// parseArgs reads a call's arguments in all three forms Lua allows: a
// parenthesised list, a lone table, or a lone string. The last two are what
// `require "kong.plugins.x"` and `describe { ... }` are written with, and an
// analyzer that only read the parenthesised form would miss most `require`s in
// real code.
func (p *parser) parseArgs() ([]Expr, bool) {
	tok := p.peek()
	switch {
	case p.atOp("("):
		p.advance()
		if p.accept(")") {
			return nil, true
		}
		args := p.parseExprList()
		return args, p.expect(")")
	case p.atOp("{"):
		return []Expr{p.parseTable()}, true
	case tok.kind == tokString:
		p.advance()
		return []Expr{&StringExpr{Position: Position{tok.line, tok.endLine}, Value: tok.value, Long: tok.long}}, true
	}
	p.note(tok.line, "expected call arguments, found %s", describe(tok))
	return nil, false
}

func (p *parser) parseTable() Expr {
	line := p.peek().line
	table := &TableExpr{Position: Position{line, line}}
	if !p.expect("{") {
		return table
	}
	if p.depth++; p.depth > maxDepth {
		p.depth--
		p.note(line, "table nesting is too deep to read")
		return table
	}
	defer func() { p.depth-- }()

	for !p.atOp("}") && !p.at(tokEOF) {
		field := TableField{Line: p.peek().line}
		switch {
		case p.atOp("["):
			p.advance()
			field.Key = p.parseExpr()
			p.expect("]")
			if !p.expect("=") {
				return table
			}
			field.Value = p.parseExpr()
		case p.at(tokName) && p.nextIsAssign():
			name := p.advance().text
			keyLine := p.prevLine()
			p.advance() // =
			field.Name = name
			field.Key = &StringExpr{Position: Position{keyLine, keyLine}, Value: name}
			field.Value = p.parseExpr()
		default:
			field.Value = p.parseExpr()
		}
		table.Fields = append(table.Fields, field)
		if !p.accept(",") && !p.accept(";") {
			break
		}
	}
	p.expect("}")
	table.EndLine = p.prevLine()
	return table
}

// nextIsAssign distinguishes `{ name = value }` from `{ name }`, one token of
// lookahead past the name.
func (p *parser) nextIsAssign() bool {
	if p.pos+1 >= len(p.toks) {
		return false
	}
	next := p.toks[p.pos+1]
	return next.kind == tokOp && next.text == "="
}

// parseFuncBody reads `(params) block end`. method adds the implicit self a
// colon declaration never spells out, so a walker reading Params sees the same
// parameter list the runtime does.
func (p *parser) parseFuncBody(line int, method bool) (*FuncExpr, bool) {
	fn := &FuncExpr{Position: Position{line, line}, Method: method}
	if method {
		fn.Params = append(fn.Params, "self")
	}
	if !p.expect("(") {
		return fn, false
	}
	for !p.atOp(")") && !p.at(tokEOF) {
		if p.accept("...") {
			fn.IsVararg = true
			break
		}
		name, ok := p.expectName()
		if !ok {
			return fn, false
		}
		fn.Params = append(fn.Params, name)
		if !p.accept(",") {
			break
		}
	}
	ok := p.expect(")")
	fn.Body = p.parseBlock()
	ok = p.expect("end") && ok
	fn.EndLine = p.prevLine()
	return fn, ok
}
