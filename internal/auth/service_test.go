package auth

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type memoryRefreshStore struct {
	mu      sync.Mutex
	token   string
	found   bool
	deleted bool
}

func (s *memoryRefreshStore) Load() (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.token, s.found, nil
}
func (s *memoryRefreshStore) Save(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.token, s.found, s.deleted = token, true, false
	return nil
}
func (s *memoryRefreshStore) Delete() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.token, s.found, s.deleted = "", false, true
	return nil
}

func testConfig(serverURL string) Config {
	return Config{
		Issuer:           serverURL,
		AuthorizationURL: serverURL + "/authorize",
		TokenURL:         serverURL + "/token",
		ClientID:         "desktop-test",
		Scope:            "openid profile",
		CallbackAddress:  "127.0.0.1:0",
		CallbackPath:     "/callback",
		LoginTimeout:     time.Second,
		RefreshBefore:    time.Minute,
	}
}

func TestAccessTokenRotatesRefreshTokenOnce(t *testing.T) {
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		if err := request.ParseForm(); err != nil || request.Form.Get("grant_type") != "refresh_token" || request.Form.Get("refresh_token") != "old-refresh" {
			t.Fatalf("unexpected refresh request: form=%v err=%v", request.Form, err)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"access_token":"new-access","refresh_token":"new-refresh","expires_in":300}`)
	}))
	defer server.Close()
	store := &memoryRefreshStore{token: "old-refresh", found: true}
	service, err := New(Dependencies{Config: testConfig(server.URL), Store: store, OpenBrowser: func(string) error { t.Fatal("browser must not open"); return nil }, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	token, expiresAt, err := service.AccessToken(context.Background())
	if err != nil || token != "new-access" {
		t.Fatalf("AccessToken = %q %s %v", token, expiresAt, err)
	}
	if expiresAt.Sub(time.Now()) < 4*time.Minute || expiresAt.Sub(time.Now()) > 6*time.Minute {
		t.Fatalf("expiresAt = %s, want ~5m from now", expiresAt)
	}
	if store.token != "new-refresh" || !store.found {
		t.Fatalf("rotated refresh token was not saved")
	}
	if _, _, err := service.AccessToken(context.Background()); err != nil {
		t.Fatalf("cached AccessToken: %v", err)
	}
	if requests != 1 {
		t.Fatalf("token requests = %d, want 1", requests)
	}
}

func TestAccessTokenDeletesRejectedRefreshToken(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(writer, `{"error":"invalid_grant"}`)
	}))
	defer server.Close()
	store := &memoryRefreshStore{token: "revoked-refresh", found: true}
	service, err := New(Dependencies{Config: testConfig(server.URL), Store: store, OpenBrowser: func(string) error { return nil }, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, _, err := service.AccessToken(context.Background()); err != ErrAuthenticationRequired {
		t.Fatalf("AccessToken error = %v, want authentication required", err)
	}
	if !store.deleted || store.found {
		t.Fatal("revoked refresh token was retained")
	}
}

func TestInteractiveLoginUsesLoopbackStateAndPKCE(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil || request.Form.Get("grant_type") != "authorization_code" || request.Form.Get("code") != "accepted-code" || request.Form.Get("code_verifier") == "" {
			t.Fatalf("unexpected authorization-code exchange: form=%v err=%v", request.Form, err)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"access_token":"interactive-access","refresh_token":"interactive-refresh","expires_in":300}`)
	}))
	defer server.Close()
	store := &memoryRefreshStore{}
	service, err := New(Dependencies{
		Config: testConfig(server.URL), Store: store, HTTPClient: server.Client(),
		OpenBrowser: func(raw string) error {
			parsed, err := url.Parse(raw)
			if err != nil {
				return err
			}
			query := parsed.Query()
			if query.Get("code_challenge_method") != "S256" || query.Get("code_challenge") == "" || query.Get("state") == "" || query.Get("redirect_uri") == "" {
				t.Fatal("missing PKCE or state")
			}
			callback, _ := url.Parse(query.Get("redirect_uri"))
			values := callback.Query()
			values.Set("code", "accepted-code")
			values.Set("state", query.Get("state"))
			callback.RawQuery = values.Encode()
			response, err := http.Get(callback.String())
			if err == nil {
				response.Body.Close()
			}
			return err
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	status, err := service.EnsureAuthenticated(context.Background())
	if err != nil || !status.Authenticated || store.token != "interactive-refresh" {
		t.Fatalf("EnsureAuthenticated = %#v %v", status, err)
	}
}

func TestCallbackHandlerRejectsWrongState(t *testing.T) {
	result := make(chan callbackResult, 1)
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/callback?code=code&state=wrong", nil)
	response := httptest.NewRecorder()
	callbackHandler("/callback", "expected", result).ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", response.Code)
	}
	if callback := <-result; callback.err == nil || strings.Contains(callback.err.Error(), "code") {
		t.Fatalf("callback error leaked code: %v", callback.err)
	}
}
