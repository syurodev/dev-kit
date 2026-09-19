// internal/lua/walk.go
package lua

// Walk visits node and everything under it, depth first, calling visit on each.
// Returning false from visit prunes that node's children — which is how the
// analyzer stops at a nested function body it is about to descend into with a
// scope of its own, rather than folding a closure's calls into its enclosing
// function.
//
// Statements and expressions are walked by the same function on purpose. Lua
// puts calls everywhere — in a condition, a table value, an argument, an index
// — and an analyzer looking for call sites would otherwise need a matching
// pair of walkers that must never disagree about where it looked.
func Walk(node Node, visit func(Node) bool) {
	if node == nil || !visit(node) {
		return
	}
	for _, child := range Children(node) {
		Walk(child, visit)
	}
}

// Children is every syntax node one node directly holds, in source order. A
// nil child (an `if` with no `else`, a `for` with no step) is left out, so
// callers never have to check.
func Children(node Node) []Node {
	var out []Node
	add := func(nodes ...Node) {
		for _, n := range nodes {
			if n == nil {
				continue
			}
			// A typed nil in an interface is not a nil interface; the
			// statement and expression lists below can hold one when a parse
			// recovered mid-statement.
			switch typed := n.(type) {
			case Stmt:
				if typed == nil {
					continue
				}
			case Expr:
				if typed == nil {
					continue
				}
			}
			out = append(out, n)
		}
	}
	addStmts := func(stmts []Stmt) {
		for _, s := range stmts {
			if s != nil {
				out = append(out, s)
			}
		}
	}
	addExprs := func(exprs []Expr) {
		for _, e := range exprs {
			if e != nil {
				out = append(out, e)
			}
		}
	}

	switch n := node.(type) {
	case *LocalStmt:
		addExprs(n.Exprs)
	case *LocalFuncStmt:
		add(n.Func)
	case *FuncStmt:
		add(n.Func)
	case *AssignStmt:
		addExprs(n.Targets)
		addExprs(n.Exprs)
	case *CallStmt:
		add(n.Call)
	case *DoStmt:
		addStmts(n.Body)
	case *WhileStmt:
		add(n.Cond)
		addStmts(n.Body)
	case *RepeatStmt:
		addStmts(n.Body)
		add(n.Cond)
	case *IfStmt:
		for _, clause := range n.Clauses {
			add(clause.Cond)
			addStmts(clause.Body)
		}
		addStmts(n.Else)
	case *NumericForStmt:
		add(n.Start, n.Limit, n.Step)
		addStmts(n.Body)
	case *GenericForStmt:
		addExprs(n.Exprs)
		addStmts(n.Body)
	case *ReturnStmt:
		addExprs(n.Exprs)
	case *IndexExpr:
		add(n.Obj, n.Key)
	case *CallExpr:
		add(n.Func)
		addExprs(n.Args)
	case *MethodCallExpr:
		add(n.Recv)
		addExprs(n.Args)
	case *FuncExpr:
		addStmts(n.Body)
	case *BinOpExpr:
		add(n.Left, n.Right)
	case *UnOpExpr:
		add(n.Operand)
	case *TableExpr:
		for _, field := range n.Fields {
			add(field.Key, field.Value)
		}
	}
	return out
}
