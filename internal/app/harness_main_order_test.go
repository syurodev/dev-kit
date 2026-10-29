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

// keyedFields returns the field names set by the first composite literal inside
// call whose type's last element is typeName.
func keyedFields(call *ast.CallExpr, typeName string) map[string]bool {
	out := map[string]bool{}
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
					out[id.Name] = true
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
	if !keyedFields(server.call, "Deps")["Harness"] {
		t.Error("mcp.Deps.Harness must be set when the MCP server is built")
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
		if !fields[f] {
			t.Errorf("HarnessAssemblyConfig.%s is not wired in main.go", f)
		}
	}
	startFields := keyedFields(start.call, "HarnessStartup")
	for _, f := range []string{"Port", "Config", "Listen", "Cleanup"} {
		if !startFields[f] {
			t.Errorf("HarnessStartup.%s is not wired in main.go", f)
		}
	}
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
