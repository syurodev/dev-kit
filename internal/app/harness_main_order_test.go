package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

type mainCall struct {
	pos  token.Pos
	end  token.Pos
	call *ast.CallExpr
}

// mainCalls parses the real composition root and returns every call of the form
// ident.name, keyed "ident.name", with its position and node.
func mainCalls(t *testing.T) map[string][]mainCall {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "../../main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	calls := map[string][]mainCall{}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok {
			key := id.Name + "." + sel.Sel.Name
			calls[key] = append(calls[key], mainCall{call.Pos(), call.End(), call})
		}
		return true
	})
	return calls
}

func once(t *testing.T, calls map[string][]mainCall, key string) mainCall {
	t.Helper()
	if len(calls[key]) != 1 {
		t.Fatalf("main.go must call %s exactly once, found %d", key, len(calls[key]))
	}
	return calls[key][0]
}

// keyedFields returns the keyed fields (name to value expression) of the first
// composite literal inside call whose type's last element is typeName.
func keyedFields(call *ast.CallExpr, typeName string) map[string]ast.Expr {
	out := map[string]ast.Expr{}
	found := false
	ast.Inspect(call, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok || found {
			return !found
		}
		var name string
		switch ty := lit.Type.(type) {
		case *ast.SelectorExpr:
			name = ty.Sel.Name
		case *ast.Ident:
			name = ty.Name
		}
		if name != typeName {
			return true
		}
		found = true
		for _, el := range lit.Elts {
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				if id, ok := kv.Key.(*ast.Ident); ok {
					out[id.Name] = kv.Value
				}
			}
		}
		return false
	})
	return out
}

// The startup order of main.go, read from its syntax tree and checked against
// the order the plan requires. What that order does (what is attached when the
// listener binds, and every failure path) is exercised through StartHarness in
// harness_startup_test.go; this only proves that main.go reaches StartHarness at
// the right point, with its real collaborators, and binds the socket no other way.
func TestMainWiresTheHarnessInTheRequiredOrder(t *testing.T) {
	calls := mainCalls(t)
	server := once(t, calls, "mcp.NewServerFromContainer")
	wailsNew := once(t, calls, "application.New")
	start := once(t, calls, "app.StartHarness")
	telegram := once(t, calls, "telegramcontrol.New")
	run := once(t, calls, "wailsApp.Run")

	// 1. The port is handed to the MCP server's dependencies, so the two public
	//    tools are registered before Inventory freezes the registry.
	if !isSelector(keyedFields(server.call, "Deps")["Harness"], "container", "Harness") {
		t.Error("mcp.Deps.Harness must be the container's port (container.Harness), not nil or another value")
	}
	// 2. Compose and attach after the server exists and after application.New (a
	//    second launch exits inside New, before it binds anything).
	if !(server.pos < wailsNew.pos && wailsNew.pos < start.pos) {
		t.Error("order must be: MCP server, application.New, StartHarness")
	}
	// 3. Anything that can create or prompt a session follows, and so does Run.
	if !(start.end < telegram.pos && start.end < run.pos) {
		t.Error("Telegram and Run must come after StartHarness has returned")
	}
	// The assembly receives its real collaborators and the audit sink.
	fields := keyedFields(start.call, "HarnessAssemblyConfig")
	for _, f := range []string{"Store", "Preferences", "Hub", "Workspaces", "Agents", "Vendors", "Inventory", "Audit"} {
		if v := fields[f]; v == nil || isNil(v) {
			t.Errorf("HarnessAssemblyConfig.%s is not wired in main.go", f)
		}
	}
	// The same port the MCP server was given, and an audit sink built by the
	// adapter, not a hand-made callback.
	startFields := keyedFields(start.call, "HarnessStartup")
	if !isSelector(startFields["Port"], "container", "Harness") {
		t.Error("HarnessStartup.Port must be container.Harness, the port handed to mcp.Deps")
	}
	if call, ok := fields["Audit"].(*ast.CallExpr); !ok || !isSelector(call.Fun, "app", "HarnessAuditSink") {
		t.Error("HarnessAssemblyConfig.Audit must be app.HarnessAuditSink(...)")
	}
	if !isIdent(fields["Agents"], "agentSvc") {
		t.Error("HarnessAssemblyConfig.Agents must be the Agent Hub service the MCP endpoints were assigned to")
	}
	for _, f := range []string{"Config", "Listen", "Cleanup"} {
		if v := startFields[f]; v == nil || isNil(v) {
			t.Errorf("HarnessStartup.%s is not wired in main.go", f)
		}
	}
	checkStartupFailureTerminates(t)
	// 4. The shared socket is bound only by Listen, inside StartHarness; the early
	//    ServeUnix start is gone and nothing else binds or serves it.
	if len(calls["mcp.ServeUnix"]) != 0 {
		t.Error("mcp.ServeUnix would bind the shared socket before the harness is attached")
	}
	for _, key := range []string{"mcp.ListenUnix", "mcp.ServeListener"} {
		c := once(t, calls, key)
		if c.pos < start.pos || c.end > start.end {
			t.Errorf("%s must be called inside the StartHarness call", key)
		}
	}
}

func isNil(e ast.Expr) bool { return isIdent(e, "nil") }

func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

// isSelector reports whether e is the selector x.sel (any x when x is "").
func isSelector(e ast.Expr, x, sel string) bool {
	se, ok := e.(*ast.SelectorExpr)
	if !ok || se.Sel.Name != sel {
		return false
	}
	if x == "" {
		return true
	}
	return isIdent(se.X, x)
}

// checkStartupFailureTerminates finds the `if _, err := app.StartHarness(...); err
// != nil { ... }` statement and requires that its branch ends the process
// (log.Fatal, Fatalf, Fatalln, os.Exit or panic) and never returns, logs and
// continues, or has an else. A failed startup that continued would run with
// Harness nil, which is the forbidden fallback.
func checkStartupFailureTerminates(t *testing.T) {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "../../main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var branch *ast.IfStmt
	ast.Inspect(file, func(n ast.Node) bool {
		ifs, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		as, ok := ifs.Init.(*ast.AssignStmt)
		if !ok || len(as.Rhs) != 1 {
			return true
		}
		if call, ok := as.Rhs[0].(*ast.CallExpr); ok && isSelector(call.Fun, "app", "StartHarness") {
			branch = ifs
			return false
		}
		return true
	})
	if branch == nil {
		t.Fatal("main.go must handle the result of app.StartHarness in an if statement")
	}
	cond, ok := branch.Cond.(*ast.BinaryExpr)
	if !ok || cond.Op != token.NEQ || !isNil(cond.Y) || !isIdent(cond.X, "err") {
		t.Fatal("the StartHarness result must be checked as `err != nil`")
	}
	if branch.Else != nil {
		t.Error("the StartHarness failure branch must not have an else")
	}
	terminates := false
	ast.Inspect(branch.Body, func(n ast.Node) bool {
		if _, ok := n.(*ast.ReturnStmt); ok {
			t.Error("the StartHarness failure branch must not return: startup would continue")
		}
		return true
	})
	if n := len(branch.Body.List); n > 0 {
		if es, ok := branch.Body.List[n-1].(*ast.ExprStmt); ok {
			if call, ok := es.X.(*ast.CallExpr); ok {
				terminates = isSelector(call.Fun, "log", "Fatal") || isSelector(call.Fun, "log", "Fatalf") ||
					isSelector(call.Fun, "log", "Fatalln") || isSelector(call.Fun, "os", "Exit") || isIdent(call.Fun, "panic")
			}
		}
	}
	if !terminates {
		t.Error("the StartHarness failure branch must end in log.Fatal*, os.Exit or panic")
	}
}
