// internal/lua/ast.go
package lua

// The syntax tree the parser builds. It is deliberately close to the grammar
// in the Lua reference manual rather than desugared: CodeGraph's analyzer reads
// it to answer "who calls what", and every desugaring it would have to undo —
// a method call rewritten as an index plus a call, a function statement
// rewritten as an assignment — is a fact about the source it would then have to
// guess back.
//
// Positions are lines, not offsets. That is the granularity the index stores
// (see codegraph.Node.Line) and the granularity the editor jumps to, so
// carrying columns would be carrying something nothing downstream can use.

// Position is where a node sits in its file: the line it starts on and the
// line it ends on, both 1-based. EndLine equals Line for anything that fits on
// one line, which is most expressions.
type Position struct {
	Line    int
	EndLine int
}

// Pos lets every node answer where it is through one interface.
func (p Position) Pos() Position { return p }

// Node is anything with a place in the source.
type Node interface{ Pos() Position }

// Stmt is a statement. The unexported marker keeps the set closed: a type from
// outside this package cannot claim to be Lua syntax.
type Stmt interface {
	Node
	stmt()
}

// Expr is an expression, under the same closed-set rule as Stmt.
type Expr interface {
	Node
	expr()
}

// Chunk is one parsed file: its top-level block, how long it was, and whatever
// the parser could not make sense of.
//
// Diagnostics rather than an error return, because a chunk with a broken
// statement in the middle is still worth everything around it. A parser that
// gave up on the first bad token would drop the whole file from the index —
// exactly what the Java analyzer does today, and exactly the failure that
// makes a symbol silently missing rather than visibly wrong.
type Chunk struct {
	Block       []Stmt
	Lines       int
	Diagnostics []Diagnostic
}

// Diagnostic is one place the parser had to recover. Message is written for a
// human reading a log, not for matching on.
type Diagnostic struct {
	Line    int
	Message string
}

// Statements.

type (
	// LocalStmt is `local a, b <const> = 1, 2`. Attribs is parallel to Names,
	// "" where a name carries no attribute (every name before Lua 5.4).
	LocalStmt struct {
		Position
		Names   []string
		Attribs []string
		Exprs   []Expr
	}

	// LocalFuncStmt is `local function f() end`. Separate from LocalStmt
	// because the name is in scope inside its own body, which is what makes a
	// local function recursive — and what an analyzer resolving a self-call
	// needs to know.
	LocalFuncStmt struct {
		Position
		Name string
		Func *FuncExpr
	}

	// FuncStmt is `function a.b.c:d() end`. Path is the dotted prefix
	// (["a","b","c"]), Method the name after the colon, "" when there is none.
	// Kept in grammar form rather than as an assignment to an IndexExpr: the
	// colon is the only thing that says the body has an implicit self, and
	// rewriting it away loses that.
	FuncStmt struct {
		Position
		Path   []string
		Method string
		Func   *FuncExpr
	}

	// AssignStmt is `a, b.c = 1, 2`.
	AssignStmt struct {
		Position
		Targets []Expr
		Exprs   []Expr
	}

	// CallStmt is a call used as a statement. Call is a *CallExpr or a
	// *MethodCallExpr; Lua allows nothing else in this position.
	CallStmt struct {
		Position
		Call Expr
	}

	DoStmt struct {
		Position
		Body []Stmt
	}

	WhileStmt struct {
		Position
		Cond Expr
		Body []Stmt
	}

	RepeatStmt struct {
		Position
		Body []Stmt
		Cond Expr
	}

	// IfStmt holds the `if` and every `elseif` as one clause list, so a walker
	// treats them the same way the language does.
	IfStmt struct {
		Position
		Clauses []IfClause
		Else    []Stmt
		HasElse bool
	}

	NumericForStmt struct {
		Position
		Name               string
		Start, Limit, Step Expr
		Body               []Stmt
	}

	GenericForStmt struct {
		Position
		Names []string
		Exprs []Expr
		Body  []Stmt
	}

	ReturnStmt struct {
		Position
		Exprs []Expr
	}

	BreakStmt struct{ Position }

	GotoStmt struct {
		Position
		Label string
	}

	LabelStmt struct {
		Position
		Name string
	}
)

// IfClause is one `if`/`elseif` condition with the block it guards.
type IfClause struct {
	Cond Expr
	Body []Stmt
}

func (*LocalStmt) stmt()      {}
func (*LocalFuncStmt) stmt()  {}
func (*FuncStmt) stmt()       {}
func (*AssignStmt) stmt()     {}
func (*CallStmt) stmt()       {}
func (*DoStmt) stmt()         {}
func (*WhileStmt) stmt()      {}
func (*RepeatStmt) stmt()     {}
func (*IfStmt) stmt()         {}
func (*NumericForStmt) stmt() {}
func (*GenericForStmt) stmt() {}
func (*ReturnStmt) stmt()     {}
func (*BreakStmt) stmt()      {}
func (*GotoStmt) stmt()       {}
func (*LabelStmt) stmt()      {}

// Expressions.

type (
	NilExpr struct{ Position }

	BoolExpr struct {
		Position
		Value bool
	}

	// VarargExpr is `...`.
	VarargExpr struct{ Position }

	// NumberExpr keeps the literal as written. Nothing downstream does
	// arithmetic on it, and the text is what a reader recognises.
	NumberExpr struct {
		Position
		Text string
	}

	// StringExpr carries the decoded value — escapes resolved for a quoted
	// string, taken verbatim for a long bracket. Long says which, because a
	// long string is also how Lua writes a multi-line literal and the analyzer
	// treats a `[[/path]]` route no differently from a `"/path"` one.
	StringExpr struct {
		Position
		Value string
		Long  bool
	}

	NameExpr struct {
		Position
		Name string
	}

	// IndexExpr is `obj[key]`, and `obj.name` with Dot set and Key a
	// StringExpr. Keeping the two apart matters: `t.get` names a field the
	// analyzer can follow, while `t[k]` names one only the runtime knows.
	IndexExpr struct {
		Position
		Obj Expr
		Key Expr
		Dot bool
	}

	CallExpr struct {
		Position
		Func Expr
		Args []Expr
	}

	// MethodCallExpr is `recv:name(args)`. Not rewritten into a CallExpr on an
	// IndexExpr, because the colon is what says the receiver is passed as
	// self — the single most useful fact when resolving a call inside a module
	// that was written in method style.
	MethodCallExpr struct {
		Position
		Recv   Expr
		Method string
		Args   []Expr
	}

	// FuncExpr is a function body. Method reports that it was declared with a
	// colon, so `self` is a parameter the source never wrote down.
	FuncExpr struct {
		Position
		Params   []string
		IsVararg bool
		Method   bool
		Body     []Stmt
	}

	BinOpExpr struct {
		Position
		Op          string
		Left, Right Expr
	}

	UnOpExpr struct {
		Position
		Op      string
		Operand Expr
	}

	TableExpr struct {
		Position
		Fields []TableField
	}
)

// TableField is one entry in a table constructor. Key is nil for a positional
// item; Name is set (with Key a StringExpr) for the `name = value` form, which
// is the one a module table is written in.
type TableField struct {
	Key   Expr
	Name  string
	Value Expr
	Line  int
}

func (*NilExpr) expr()        {}
func (*BoolExpr) expr()       {}
func (*VarargExpr) expr()     {}
func (*NumberExpr) expr()     {}
func (*StringExpr) expr()     {}
func (*NameExpr) expr()       {}
func (*IndexExpr) expr()      {}
func (*CallExpr) expr()       {}
func (*MethodCallExpr) expr() {}
func (*FuncExpr) expr()       {}
func (*BinOpExpr) expr()      {}
func (*UnOpExpr) expr()       {}
func (*TableExpr) expr()      {}
