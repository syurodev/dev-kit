package bridge

import (
	"bytes"
	"context"
	"devkit/internal/app"
	"devkit/internal/mcp"
	"devkit/internal/ops"
	"encoding/json"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestBridgeShimsMatchTheRegistry(t *testing.T) {
	raw, err := os.ReadFile("testdata/legacy_ops_methods.json")
	if err != nil {
		t.Fatal(err)
	}
	var legacy []string
	if err := json.Unmarshal(raw, &legacy); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{}
	for _, name := range legacy {
		allowed[name] = true
	}
	typ := reflect.TypeFor[*AppService]()
	for _, op := range ops.All() {
		d := op.Descriptor()
		if d.Exposure == ops.Agent {
			continue
		}
		if allowed[d.Bridge] {
			t.Errorf("remove migrated %s from legacy list", d.Bridge)
		}
		allowed[d.Bridge] = true
		method, ok := typ.MethodByName(d.Bridge)
		if !ok {
			t.Errorf("missing %s", d.Bridge)
			continue
		}
		offset := 1
		if method.Type.NumIn() > 1 && method.Type.In(1) == reflect.TypeFor[context.Context]() {
			offset++
		}
		in := op.InputType()
		if method.Type.NumIn()-offset != in.NumField() {
			t.Errorf("%s argument count", d.Bridge)
			continue
		}
		for i := 0; i < in.NumField(); i++ {
			field := in.Field(i)
			if len(d.BridgeArgs) > 0 {
				var ok bool
				field, ok = in.FieldByName(d.BridgeArgs[i])
				if !ok {
					t.Fatal(d.BridgeArgs[i])
				}
			}
			if method.Type.In(i+offset) != field.Type {
				t.Errorf("%s argument %d", d.Bridge, i)
			}
		}
		out := op.OutputType()
		void := out == reflect.TypeFor[ops.Unit]()
		if (!void && (method.Type.NumOut() != 2 || method.Type.Out(0) != out)) || (void && method.Type.NumOut() != 1) {
			t.Errorf("%s return type", d.Bridge)
		}
		if method.Type.Out(method.Type.NumOut()-1) != reflect.TypeFor[error]() {
			t.Errorf("%s error return", d.Bridge)
		}
	}
	for i := 0; i < typ.NumMethod(); i++ {
		name := typ.Method(i).Name
		if !allowed[name] {
			t.Errorf("%s is neither an op nor legacy method", name)
		}
	}
	for _, name := range legacy {
		if _, ok := typ.MethodByName(name); !ok {
			t.Errorf("remove missing legacy method %s", name)
		}
	}
}

// Baseline captured from notes.go before adding context parameters. This also
// checks argument grouping/order and exact Go return types independently of ops.
func TestNotesKeepExactNonContextSignatures(t *testing.T) {
	raw, err := os.ReadFile("testdata/notes_signatures.json")
	if err != nil {
		t.Fatal(err)
	}
	var baseline map[string]string
	if err := json.Unmarshal(raw, &baseline); err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	actual, err := parser.ParseFile(fset, "notes.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	methods := map[string]*ast.FuncDecl{}
	for _, decl := range actual.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			methods[fn.Name.Name] = fn
		}
	}
	for name, sig := range baseline {
		old, err := parser.ParseFile(fset, "before.go", "package bridge\n"+sig+" {}", 0)
		if err != nil {
			t.Fatal(err)
		}
		want := old.Decls[0].(*ast.FuncDecl).Type
		got := *methods[name].Type
		args := *got.Params
		args.List = append([]*ast.Field(nil), args.List...)
		if len(args.List) > 0 {
			if typ, ok := args.List[0].Type.(*ast.SelectorExpr); ok && typ.Sel.Name == "Context" {
				args.List = args.List[1:]
			}
		}
		got.Params = &args
		var a, b bytes.Buffer
		if err := format.Node(&a, fset, want); err != nil {
			t.Fatal(err)
		}
		if err := format.Node(&b, fset, &got); err != nil {
			t.Fatal(err)
		}
		if a.String() != b.String() {
			t.Errorf("%s signature changed: %s => %s", name, a.String(), b.String())
		}
	}
}

func TestNotesRegistryAdaptersShareTheRealGateway(t *testing.T) {
	container := app.NewContainer()
	service := NewAppService(container)
	if _, err := service.SetupVault("master-password"); err != nil {
		t.Fatal(err)
	}
	note, err := service.NotesCreate(context.Background(), "pilot", "private-body", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	server := mcp.NewServer(mcp.Deps{Caller: NewCaller(service), Notes: container.Notes})
	text, failed := callMCPTool(t, server, "notes.get", map[string]any{"id": note.ID})
	if failed || !strings.Contains(text, "private-body") {
		t.Fatalf("get: %s failed=%v", text, failed)
	}
	text, failed = callMCPTool(t, server, "notes.delete", map[string]any{"id": note.ID})
	if failed || text != "" {
		t.Fatalf("delete result changed: %q failed=%v", text, failed)
	}
	trash, err := service.NotesListTrash(context.Background())
	if err != nil || len(trash) != 1 {
		t.Fatalf("trash=%v err=%v", trash, err)
	}
	events := container.Audit.Events()
	var sawUI, sawMCP bool
	for _, event := range events {
		if event.SubjectID != "builtin.notes" {
			continue
		}
		raw, _ := json.Marshal(event.Metadata)
		if strings.Contains(string(raw), "private-body") {
			t.Fatal("body leaked")
		}
		if event.Caller == "ui" {
			sawUI = true
		}
		if event.Caller == "user-terminal" && event.Metadata["origin"] == "mcp" {
			sawMCP = true
		}
	}
	if !sawUI || !sawMCP {
		t.Fatalf("UI=%v MCP=%v", sawUI, sawMCP)
	}
	if err := service.LockVault(); err != nil {
		t.Fatal(err)
	}
	text, failed = callMCPTool(t, server, "notes.get", map[string]any{"id": note.ID})
	if !failed || !strings.Contains(text, "vault_locked") {
		t.Fatalf("locked: %q failed=%v", text, failed)
	}
}
