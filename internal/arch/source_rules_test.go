package arch

import (
	"go/ast"
	"sort"
	"strings"
	"testing"
)

// Rules read from the source itself (go/ast). They exist for the mistakes that
// compile, pass a happy-path test and then fail in production: a cleanup that
// borrows a cancelled request's context, a process that exits from deep inside a
// library, a sleep standing in for synchronisation.

// A process exits from main and from the exec helper that has to (it runs after
// fork, before there is anything to unwind). Library code returns an error.
var mayExitProcess = map[string]bool{
	"internal/fchdirexec": true,
}

func TestOnlyMainEndsTheProcess(t *testing.T) {
	for _, f := range prodFiles(t) {
		if f.Dir == "." || f.Dir == "cmd" || strings.HasPrefix(f.Dir, "cmd/") || mayExitProcess[f.Dir] {
			continue
		}
		ast.Inspect(f.File, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if selector(call, "os", "Exit") || selector(call, "log", "Fatal") || selector(call, "log", "Fatalf") || selector(call, "log", "Fatalln") {
				t.Errorf("%s: library code must return an error, not end the process", f.Fset.Position(call.Pos()))
			}
			return true
		})
	}
}

// panic is for a constructor handed something that makes it unusable (a missing
// policy, a malformed built-in schema), found at start-up. These are the files
// that do that today; a new panic is a new decision.
var mayPanic = map[string]bool{
	"internal/capabilities/gateway.go":    true,
	"internal/codegraph/comments.go":      true,
	"internal/mcp/ops.go":                 true,
	"internal/telegramcontrol/service.go": true,
}

func TestPanicIsOnlyForConstructorInvariants(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range prodFiles(t) {
		ast.Inspect(f.File, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "panic" {
				seen[f.Rel] = true
				if !mayPanic[f.Rel] {
					t.Errorf("%s: panic in production code; return an error (or, for a constructor invariant, add the file to mayPanic with the reason)", f.Fset.Position(call.Pos()))
				}
			}
			return true
		})
	}
	for rel := range mayPanic {
		if !seen[rel] {
			t.Errorf("mayPanic lists %s but it no longer panics; remove it", rel)
		}
	}
}

// time.Sleep in production code is polling or a missing signal. These two wait on
// the operating system by design.
var maySleep = map[string]bool{
	"internal/git/bound_unix.go":    true,
	"internal/database/consoles.go": true,
}

func TestProductionCodeDoesNotSleep(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range prodFiles(t) {
		ast.Inspect(f.File, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok && selector(call, "time", "Sleep") {
				seen[f.Rel] = true
				if !maySleep[f.Rel] {
					t.Errorf("%s: time.Sleep in production code; wait on a channel, a context or a condition", f.Fset.Position(call.Pos()))
				}
			}
			return true
		})
	}
	for rel := range maySleep {
		if !seen[rel] {
			t.Errorf("maySleep lists %s but it no longer sleeps; remove it", rel)
		}
	}
}

// A context that has been detached from its request (context.WithoutCancel) runs
// until somebody stops it. A cleanup that borrows a cancelled request's context
// fails exactly when it is needed, and the fix is a detached one - which must then
// carry a deadline, or a hung close or a hung Git call holds a goroutine and a
// lock for ever. Within one function, WithoutCancel is therefore expected next to
// WithTimeout or WithDeadline.
//
// The call sites below predate the rule and are of three kinds: a database write
// that must land whatever the request did (recording a finished turn, saving
// usage), bounded by the database and not by a deadline (agenthub); the database
// drivers, which finish a user's statement after the request that sent it ended;
// and the question put to the person after a parent forwards a request, which
// waits for as long as the person does (app). The count per file can only go
// down: a new unbounded one fails, and removing one means lowering the number.
var unboundedDetached = map[string]int{
	"internal/agenthub/account_switch.go":    2,
	"internal/agenthub/service.go":           2,
	"internal/app/harness_group_requests.go": 1,
	"internal/database/driver_mysql.go":      1,
	"internal/database/driver_postgres.go":   1,
}

func TestDetachedContextsCarryADeadline(t *testing.T) {
	got := map[string]int{}
	for _, f := range prodFiles(t) {
		for _, decl := range f.File.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			var detached, bounded int
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch {
				case selector(call, "context", "WithoutCancel"):
					detached++
				case selector(call, "context", "WithTimeout"), selector(call, "context", "WithDeadline"):
					bounded++
				}
				return true
			})
			if detached > 0 && bounded == 0 {
				got[f.Rel] += detached
			}
		}
	}
	var files []string
	for rel := range got {
		files = append(files, rel)
	}
	for rel := range unboundedDetached {
		if _, ok := got[rel]; !ok {
			files = append(files, rel)
		}
	}
	sort.Strings(files)
	for _, rel := range files {
		switch want := unboundedDetached[rel]; {
		case got[rel] > want:
			t.Errorf("%s: %d context.WithoutCancel without a deadline in the same function (allowed %d); wrap it: context.WithTimeout(context.WithoutCancel(ctx), d)", rel, got[rel], want)
		case got[rel] < want:
			t.Errorf("%s: %d unbounded context.WithoutCancel left, the list says %d; lower it in unboundedDetached", rel, got[rel], want)
		}
	}
}
