package bridge

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/app"
	"devkit/internal/identity"
	"devkit/internal/storage"
)

// codexSignInStub stands in for the Codex adapter: a vendor that signs in
// inside DevKit, with a page and a code no audit line may carry.
type codexSignInStub struct {
	*agenthub.MockAdapter
	cancelled []string
	signedOut int
}

const (
	stubAuthURL  = "https://auth.openai.example/oauth/authorize?state=secret-state"
	stubUserCode = "WXYZ-9876"
)

func (c *codexSignInStub) Type() agenthub.AgentType { return agenthub.AgentTypeCodex }

func (c *codexSignInStub) SignInStatus(context.Context) (agenthub.SignInStatus, error) {
	return agenthub.SignInStatus{SignedIn: true, Method: "chatgpt", Email: "a@example.com", Plan: "plus"}, nil
}

func (c *codexSignInStub) StartBrowserSignIn(context.Context) (agenthub.SignInFlow, error) {
	return agenthub.SignInFlow{LoginID: "login-1", Method: agenthub.SignInBrowser, AuthURL: stubAuthURL}, nil
}

func (c *codexSignInStub) StartDeviceSignIn(context.Context) (agenthub.SignInFlow, error) {
	return agenthub.SignInFlow{LoginID: "login-2", Method: agenthub.SignInDeviceCode,
		VerificationURL: "https://auth.openai.example/device", UserCode: stubUserCode}, nil
}

func (c *codexSignInStub) CancelSignIn(_ context.Context, loginID string) error {
	c.cancelled = append(c.cancelled, loginID)
	return nil
}

func (c *codexSignInStub) SignOut(context.Context) error {
	c.signedOut++
	return nil
}

func newBridgeWithSignIn(t *testing.T) (*AppService, *app.Container, *codexSignInStub) {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	container := app.NewContainer()
	reg := agenthub.NewRegistry()
	stub := &codexSignInStub{MockAdapter: agenthub.NewMockAdapter()}
	reg.Register(stub)
	reg.Register(agenthub.NewMockAdapter())
	container.AgentHub = agenthub.NewService(reg, agenthub.NewSQLStore(db), nil)
	return NewAppService(container), container, stub
}

// The five sign-in methods reach the vendor through the auth:<agent> grant,
// and the audit log says what was done and how — never the page or the code.
func TestAgentHubInAppSignInThroughTheGateway(t *testing.T) {
	svc, container, stub := newBridgeWithSignIn(t)
	ctx := context.Background()

	status, err := svc.AgentHubSignInStatus(ctx, "codex")
	if err != nil || status.Email != "a@example.com" || status.Method != "chatgpt" {
		t.Fatalf("status = %+v, %v", status, err)
	}
	browser, err := svc.AgentHubStartBrowserSignIn(ctx, "codex")
	if err != nil || browser.AuthURL != stubAuthURL || browser.LoginID != "login-1" {
		t.Fatalf("browser = %+v, %v", browser, err)
	}
	device, err := svc.AgentHubStartDeviceSignIn(ctx, "codex")
	if err != nil || device.UserCode != stubUserCode {
		t.Fatalf("device = %+v, %v", device, err)
	}
	if err := svc.AgentHubCancelSignIn(ctx, "codex", "login-2"); err != nil || len(stub.cancelled) != 1 || stub.cancelled[0] != "login-2" {
		t.Fatalf("cancel: %v, %v", err, stub.cancelled)
	}
	if err := svc.AgentHubSignOut(ctx, "codex"); err != nil || stub.signedOut != 1 {
		t.Fatalf("sign out: %v, %d", err, stub.signedOut)
	}

	actions := map[string]string{}
	for _, event := range container.Audit.Events() {
		if event.SubjectID != "builtin.agenthub" || event.Scope != "auth:codex" {
			continue
		}
		action, _ := event.Metadata["action"].(string)
		method, _ := event.Metadata["method"].(string)
		actions[action] = method
		if event.Outcome != "allowed" || event.Caller != "ui" {
			t.Errorf("%s: outcome %q caller %q", action, event.Outcome, event.Caller)
		}
		raw, _ := json.Marshal(event.Metadata)
		for _, secret := range []string{stubAuthURL, "secret-state", stubUserCode, "login-1", "login-2"} {
			if strings.Contains(string(raw), secret) {
				t.Errorf("%s: audit metadata carries %q: %s", action, secret, raw)
			}
		}
	}
	want := map[string]string{
		"sign_in_status":        "",
		"start_in_app_sign_in":  agenthub.SignInDeviceCode, // the later of the two starts
		"cancel_in_app_sign_in": "",
		"sign_out":              "",
	}
	for action, method := range want {
		got, ok := actions[action]
		if !ok || got != method {
			t.Errorf("audit %s: method %q (recorded %v), want %q", action, got, ok, method)
		}
	}
}

// A caller the gateway already knows is an agent — what an MCP tool handler
// carries — is refused, whichever door it found.
func TestAgentHubInAppSignInRefusesAnAgentCaller(t *testing.T) {
	svc, _, stub := newBridgeWithSignIn(t)
	ctx := identity.WithCaller(context.Background(), identity.Agent("11111111-1111-1111-1111-111111111111"))
	if _, err := svc.AgentHubStartBrowserSignIn(ctx, "codex"); err == nil || !strings.HasPrefix(err.Error(), "permission_denied") {
		t.Fatalf("agent start = %v, want permission_denied", err)
	}
	if err := svc.AgentHubSignOut(ctx, "codex"); err == nil || !strings.HasPrefix(err.Error(), "permission_denied") {
		t.Fatalf("agent sign out = %v, want permission_denied", err)
	}
	if stub.signedOut != 0 {
		t.Fatal("an agent signed the person out")
	}
}

// Claude signs in in a terminal; asking it for an in-app sign-in says so
// rather than failing as an internal error.
func TestAgentHubInAppSignInUnsupportedVendor(t *testing.T) {
	svc, _, _ := newBridgeWithSignIn(t)
	_, err := svc.AgentHubSignInStatus(context.Background(), "mock")
	if err == nil || err.Error() != "validation_failed: This agent doesn't support that." {
		t.Fatalf("mock status = %v", err)
	}
	if _, err := svc.AgentHubStartBrowserSignIn(context.Background(), "nobody"); err == nil ||
		err.Error() != "validation_failed: Unknown agent vendor." {
		t.Fatalf("unknown vendor = %v", err)
	}
}
