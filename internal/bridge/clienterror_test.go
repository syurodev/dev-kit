package bridge

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"devkit/internal/app"
	"devkit/internal/audit"
	"devkit/internal/clienterror"
	"devkit/internal/identity"
)

type clientErrorCapture struct {
	mu    sync.Mutex
	lines []string
}

func (c *clientErrorCapture) logf(format string, args ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = append(c.lines, fmt.Sprintf(format, args...))
}

type discardSink struct{}

func (discardSink) Append(string) {}

func newClientErrorBridge(t *testing.T) (*AppService, *clientErrorCapture) {
	t.Helper()
	c := &clientErrorCapture{}
	container := app.NewContainer()
	container.ClientErrors = clienterror.New(clienterror.Options{Logf: c.logf, Sink: discardSink{}})
	return NewAppService(container), c
}

// A crash of the window is recorded by the window only: an agent session or a
// terminal on the shared socket is refused, silently, because the method has no
// error to give back.
func TestReportClientErrorIsWindowOnly(t *testing.T) {
	win := context.Background()
	callers := []struct {
		name    string
		ctx     context.Context
		allowed bool
	}{
		{"window", win, true},
		{"explicit ui", identity.WithCaller(win, identity.UI()), true},
		{"agent", identity.WithCaller(win, identity.Agent("11111111-1111-1111-1111-111111111111")), false},
		{"terminal", identity.WithCaller(win, identity.UserTerminal()), false},
	}
	for _, tc := range callers {
		t.Run(tc.name, func(t *testing.T) {
			svc, c := newClientErrorBridge(t)
			svc.reportClientError(tc.ctx, "root", "boom", "Error: boom\n at f", "in App")
			if got := len(c.lines) == 1; got != tc.allowed {
				t.Fatalf("lines = %v, allowed = %v", c.lines, tc.allowed)
			}
			if tc.allowed && !strings.HasPrefix(c.lines[0], "webview: client error [root] boom stack: Error: boom | at f componentStack: in App") {
				t.Fatalf("line = %q", c.lines[0])
			}
		})
	}
}

func TestReportClientErrorWithNothingWiredOrNothingSentDoesNotPanic(t *testing.T) {
	NewAppService(app.NewContainer()).ReportClientError("", "", "", "")
	svc, c := newClientErrorBridge(t)
	svc.ReportClientError("", "", "", "")
	if len(c.lines) != 1 {
		t.Fatalf("lines = %v", c.lines)
	}
}

// The bound method has no result: fire and forget, nothing for the window to
// handle, no error text to leak back.
func TestReportClientErrorReturnsNothing(t *testing.T) {
	method, ok := reflect.TypeFor[*AppService]().MethodByName("ReportClientError")
	if !ok {
		t.Fatal("ReportClientError is not on the bound surface")
	}
	if method.Type.NumOut() != 0 || method.Type.NumIn() != 5 { // receiver + four strings
		t.Fatalf("signature = %v", method.Type)
	}
	for i := 1; i < method.Type.NumIn(); i++ {
		if method.Type.In(i).Kind() != reflect.String {
			t.Fatalf("argument %d is %v", i, method.Type.In(i))
		}
	}
}

// The audit record of a report carries the action only: never the message or
// the stack the window sent.
func TestReportClientErrorAuditsTheActionNotThePayload(t *testing.T) {
	svc, _ := newClientErrorBridge(t)
	secret := "super-secret-token-123"
	svc.ReportClientError("root", secret, secret, secret)
	events := svc.container.Audit.Events()
	if len(events) != 1 || events[0].Capability != "diagnostics:write" || events[0].Outcome != audit.OutcomeAllowed {
		t.Fatalf("audit events = %+v", events)
	}
	if strings.Contains(fmt.Sprintf("%+v", events), secret) {
		t.Fatalf("the audit event holds the payload: %+v", events)
	}
}
