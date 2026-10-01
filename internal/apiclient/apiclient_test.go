package apiclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"devkit/internal/vault"
	"devkit/internal/vaultitem"
)

type stubEnvs struct {
	exists  map[string]bool
	confirm map[string]bool
}

func (s stubEnvs) Exists(name string) bool               { return s.exists[name] }
func (s stubEnvs) RequiresConfirmation(name string) bool { return s.confirm[name] }

type stubCreds struct {
	user, secret string
	env          string
	calls        int
	err          error
}

func (s *stubCreds) ResolveForRequest(_ context.Context, _ string, env string) (string, string, error) {
	s.calls++
	s.env = env
	return s.user, s.secret, s.err
}

type stubConfirm struct {
	allow bool
	asked int
}

func (s *stubConfirm) Confirm(context.Context, string, string) (bool, error) {
	s.asked++
	return s.allow, nil
}

func newService(t *testing.T) *Service {
	t.Helper()
	v := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := v.Setup("correct-horse"); err != nil {
		t.Fatal(err)
	}
	envs := stubEnvs{
		exists:  map[string]bool{"dev": true, "prod": true},
		confirm: map[string]bool{"prod": true},
	}
	return NewService(vaultitem.NewStore(vaultitem.NewMemoryRepository(), v), envs)
}

func TestInterpolateLeavesUnknownVariablesVisible(t *testing.T) {
	vars := map[string]string{"baseUrl": "https://api.example"}
	got := Interpolate("{{baseUrl}}/users/{{id}}", vars)
	if got != "https://api.example/users/{{id}}" {
		t.Fatalf("Interpolate = %q", got)
	}
	missing := MissingVars("{{baseUrl}}/users/{{id}}?x={{id}}", vars)
	if len(missing) != 1 || missing[0] != "id" {
		t.Fatalf("MissingVars = %v", missing)
	}
}

func TestEnvironmentRequiresAKnownTag(t *testing.T) {
	svc := newService(t)
	if _, err := svc.CreateEnvironment("Local", "", nil, nil); err == nil {
		t.Fatal("an environment with no tag was accepted")
	}
	if _, err := svc.CreateEnvironment("Local", "nope", nil, nil); err == nil {
		t.Fatal("an environment with an unknown tag was accepted")
	}
}

func TestSecretVariablesAreNamedButNeverReturned(t *testing.T) {
	svc := newService(t)
	if _, err := svc.CreateEnvironment("Dev", "dev",
		map[string]string{"baseUrl": "https://api.example"},
		map[string]string{"token": "s3cr3t"}); err != nil {
		t.Fatal(err)
	}
	envs, err := svc.Environments()
	if err != nil {
		t.Fatal(err)
	}
	if len(envs) != 1 {
		t.Fatalf("environments = %+v", envs)
	}
	if len(envs[0].SecretVars) != 1 || envs[0].SecretVars[0] != "token" {
		t.Fatalf("secret names = %v", envs[0].SecretVars)
	}
	for _, value := range envs[0].Vars {
		if value == "s3cr3t" {
			t.Fatal("a secret value was returned in Vars")
		}
	}
}

// The whole point of the auth binding: the request stores an id, and the value
// is resolved inside Go at the moment of sending.
func TestAuthResolvesAnAccountAtSendTime(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	svc := newService(t)
	creds := &stubCreds{secret: "ghp_live"}
	svc.SetCredentials(creds)
	env, err := svc.CreateEnvironment("Dev", "dev", map[string]string{"baseUrl": server.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := svc.SaveRequest(Request{
		Name: "me", Method: "get", URL: "{{baseUrl}}/user",
		Auth: Auth{Kind: AuthBearer, AccountID: "acct-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Method != "GET" {
		t.Fatalf("method not normalised: %q", saved.Method)
	}

	resp, err := svc.Send(context.Background(), SendInput{Request: saved, EnvironmentID: env.ID})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != 200 || !strings.Contains(resp.Body, "ok") {
		t.Fatalf("response = %+v", resp)
	}
	if gotAuth != "Bearer ghp_live" {
		t.Fatalf("server saw %q", gotAuth)
	}
	if creds.calls != 1 || creds.env != "dev" {
		t.Fatalf("resolve calls = %d for env %q", creds.calls, creds.env)
	}

	// The stored request never holds the value, which is what makes rotation
	// a one-place edit.
	stored, err := svc.Request(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored.Body, "ghp_live") {
		t.Fatal("the credential was stored on the request")
	}
	if stored.Auth.AccountID != "acct-1" {
		t.Fatalf("binding = %+v", stored.Auth)
	}
}

// Env wins, every time, with no allow-always.
func TestProdEnvironmentAsksEverySend(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))
	defer server.Close()

	svc := newService(t)
	confirm := &stubConfirm{allow: true}
	svc.SetConfirmer(confirm)
	env, _ := svc.CreateEnvironment("Prod", "prod", map[string]string{"baseUrl": server.URL}, nil)
	req, _ := svc.SaveRequest(Request{Name: "ping", Method: "GET", URL: "{{baseUrl}}/ping"})

	for i := 0; i < 3; i++ {
		if _, err := svc.Send(context.Background(), SendInput{Request: req, EnvironmentID: env.ID}); err != nil {
			t.Fatal(err)
		}
	}
	if confirm.asked != 3 {
		t.Fatalf("asked %d times for 3 sends", confirm.asked)
	}
}

func TestProdSendIsRefusedWhenDeniedOrHeadless(t *testing.T) {
	var reached int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached++
		w.WriteHeader(200)
	}))
	defer server.Close()

	svc := newService(t)
	svc.SetConfirmer(&stubConfirm{allow: false})
	env, _ := svc.CreateEnvironment("Prod", "prod", map[string]string{"baseUrl": server.URL}, nil)
	req, _ := svc.SaveRequest(Request{Name: "ping", Method: "GET", URL: "{{baseUrl}}/ping"})
	if _, err := svc.Send(context.Background(), SendInput{Request: req, EnvironmentID: env.ID}); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("denied send = %v", err)
	}

	// Headless: no confirmer at all.
	svc.SetConfirmer(nil)
	if _, err := svc.Send(context.Background(), SendInput{Request: req, EnvironmentID: env.ID}); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("headless prod send = %v", err)
	}
	if reached != 0 {
		t.Fatalf("a refused send still left the machine %d times", reached)
	}
}

func TestSendWithoutAnEnvironmentIsRefused(t *testing.T) {
	svc := newService(t)
	req, _ := svc.SaveRequest(Request{Name: "x", Method: "GET", URL: "https://example.test"})
	if _, err := svc.Send(context.Background(), SendInput{Request: req}); !errors.Is(err, ErrEnvRequired) {
		t.Fatalf("send with no environment = %v; nothing could have gated it", err)
	}
}

// Plain http is a dev server on the LAN — ordinary — unless what travels in
// the clear matters: a bound credential, or an environment that asks first.
func TestPlainHTTPIsRefusedWhereItWouldMatter(t *testing.T) {
	svc := newService(t)
	svc.SetCredentials(&stubCreds{secret: "tok"})
	svc.SetConfirmer(&stubConfirm{allow: true})
	dev, _ := svc.CreateEnvironment("Dev", "dev", nil, nil)
	prod, _ := svc.CreateEnvironment("Prod", "prod", nil, nil)

	for _, target := range []string{"file:///etc/passwd", "ftp://example.test"} {
		req := Request{Name: "x", Method: "GET", URL: target}
		if _, err := svc.Send(context.Background(), SendInput{Request: req, EnvironmentID: dev.ID}); !errors.Is(err, ErrSchemeInvalid) {
			t.Fatalf("%s = %v, want ErrSchemeInvalid", target, err)
		}
	}
	bound := Request{Name: "x", Method: "GET", URL: "http://192.0.2.1/x", Auth: Auth{Kind: AuthBearer, AccountID: "a"}}
	if _, err := svc.Send(context.Background(), SendInput{Request: bound, EnvironmentID: dev.ID}); !errors.Is(err, ErrPlainHTTP) {
		t.Fatalf("a credential over plain http = %v, want ErrPlainHTTP", err)
	}
	gated := Request{Name: "x", Method: "GET", URL: "http://192.0.2.1/x"}
	if _, err := svc.Send(context.Background(), SendInput{Request: gated, EnvironmentID: prod.ID}); !errors.Is(err, ErrPlainHTTP) {
		t.Fatalf("plain http on a gated environment = %v, want ErrPlainHTTP", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	defer server.Close()
	// httptest listens on 127.0.0.1 — loopback is always fine, even bound.
	local := Request{Name: "x", Method: "GET", URL: server.URL, Auth: Auth{Kind: AuthBearer, AccountID: "a"}}
	if resp, err := svc.Send(context.Background(), SendInput{Request: local, EnvironmentID: dev.ID}); err != nil || resp.Status != 204 {
		t.Fatalf("loopback = %+v, %v", resp, err)
	}
	if err := checkScheme(mustParse(t, "http://10.0.0.5:8080/x"), false, false); err != nil {
		t.Fatalf("a LAN dev server with nothing to protect was refused: %v", err)
	}
}

// A redirect that changes host would carry the Authorization header somewhere
// the user never aimed at.
func TestRedirectToAnotherHostStops(t *testing.T) {
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("the credential followed a cross-host redirect")
		}
		w.WriteHeader(200)
	}))
	defer elsewhere.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{}, elsewhere.URL+"/landed", http.StatusFound)
	}))
	defer origin.Close()

	svc := newService(t)
	svc.SetCredentials(&stubCreds{secret: "tok"})
	env, _ := svc.CreateEnvironment("Dev", "dev", map[string]string{"baseUrl": origin.URL}, nil)
	req, _ := svc.SaveRequest(Request{
		Name: "x", Method: "GET", URL: "{{baseUrl}}/go",
		Auth: Auth{Kind: AuthBearer, AccountID: "acct-1"},
	})
	resp, err := svc.Send(context.Background(), SendInput{Request: req, EnvironmentID: env.ID})
	if err != nil {
		t.Fatal(err)
	}
	// The redirect itself is the answer, with where it pointed — not an error
	// that leaves the person guessing what happened.
	if resp.Status != http.StatusFound || !strings.HasPrefix(resp.RedirectBlocked, elsewhere.URL) {
		t.Fatalf("response = %d, blocked = %q", resp.Status, resp.RedirectBlocked)
	}
}

func TestAgentResponsesAreBoundedHarder(t *testing.T) {
	big := strings.Repeat("x", MaxAgentBytes+1024)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(big))
	}))
	defer server.Close()

	svc := newService(t)
	env, _ := svc.CreateEnvironment("Dev", "dev", map[string]string{"baseUrl": server.URL}, nil)
	req, _ := svc.SaveRequest(Request{Name: "big", Method: "GET", URL: "{{baseUrl}}/big"})

	agent, err := svc.Send(context.Background(), SendInput{Request: req, EnvironmentID: env.ID, ForAgent: true})
	if err != nil {
		t.Fatal(err)
	}
	if !agent.Truncated || agent.Bytes != MaxAgentBytes {
		t.Fatalf("agent response = %d bytes, truncated=%v", agent.Bytes, agent.Truncated)
	}
	human, err := svc.Send(context.Background(), SendInput{Request: req, EnvironmentID: env.ID})
	if err != nil {
		t.Fatal(err)
	}
	if human.Truncated || human.Bytes != len(big) {
		t.Fatalf("display response = %d bytes, truncated=%v", human.Bytes, human.Truncated)
	}
}

// Copy as cURL must be safe to paste into a ticket.
func TestCurlLeavesTheCredentialAsAVariable(t *testing.T) {
	got := CurlFor(Request{
		Method: "POST", URL: "{{baseUrl}}/refunds", Body: `{"amount":1}`,
		Auth: Auth{Kind: AuthBearer, AccountID: "acct-1"},
	}, map[string]string{"baseUrl": "https://api.example"})
	if !strings.Contains(got, "$DEVKIT_TOKEN") {
		t.Fatalf("curl has no placeholder: %s", got)
	}
	if strings.Contains(got, "acct-1") {
		t.Fatalf("curl leaked the binding: %s", got)
	}
	if !strings.Contains(got, "https://api.example/refunds") {
		t.Fatalf("curl did not interpolate: %s", got)
	}
}

func TestEnvUsageNamesEnvironmentsHoldingATag(t *testing.T) {
	svc := newService(t)
	if _, err := svc.CreateEnvironment("Prod", "prod", nil, nil); err != nil {
		t.Fatal(err)
	}
	used := svc.EnvUsage("prod")
	if len(used) != 1 || !strings.Contains(used[0], "Prod") {
		t.Fatalf("EnvUsage = %v", used)
	}
	if len(svc.EnvUsage("dev")) != 0 {
		t.Fatal("an unused tag reported usage")
	}
}
