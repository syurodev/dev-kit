// Package auth implements the desktop OAuth 2.0 Authorization Code flow with
// PKCE. It is deliberately separate from sync: this package owns IdP tokens,
// while sync only consumes short-lived access tokens through a narrow interface.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	defaultCallbackAddress = "127.0.0.1:62364"
	defaultCallbackPath    = "/callback"
	maxTokenBytes          = 16 * 1024
)

var (
	ErrAuthenticationRequired = errors.New("authentication is required")
	ErrAuthenticationFailed   = errors.New("authentication failed")
)

// Config only accepts the fixed public DevKit identity provider. The desktop
// never accepts an issuer or redirect URL from the UI.
type Config struct {
	Issuer           string
	AuthorizationURL string
	TokenURL         string
	ClientID         string
	Scope            string
	CallbackAddress  string
	CallbackPath     string
	LoginTimeout     time.Duration
	RefreshBefore    time.Duration
}

// RefreshTokenStore persists only the rotating refresh token. Access tokens
// stay in memory and are never written to SQLite, the frontend, or audit logs.
type RefreshTokenStore interface {
	Load() (token string, found bool, err error)
	Save(token string) error
	Delete() error
}

// BrowserOpener opens the authorization request in the operating-system
// browser. It is injected by main, which keeps this core package Wails-free.
type BrowserOpener func(url string) error

// Dependencies make the browser, clock, keychain and HTTP boundary testable.
type Dependencies struct {
	Config      Config
	Store       RefreshTokenStore
	OpenBrowser BrowserOpener
	HTTPClient  *http.Client
	Now         func() time.Time
}

// Status contains only display-safe metadata.
type Status struct {
	Authenticated bool      `json:"authenticated"`
	ExpiresAt     time.Time `json:"expires_at,omitempty"`
}

// Service owns the in-memory access token and the rotating keychain refresh
// token. Calls are serialized so two sync requests cannot rotate one refresh
// token concurrently.
type Service struct {
	config      Config
	store       RefreshTokenStore
	openBrowser BrowserOpener
	client      *http.Client
	now         func() time.Time

	mu            sync.RWMutex
	accessToken   string
	expiresAt     time.Time
	refreshMu     sync.Mutex
	interactiveMu sync.Mutex
}

func New(deps Dependencies) (*Service, error) {
	config := deps.Config
	if config.CallbackAddress == "" {
		config.CallbackAddress = defaultCallbackAddress
	}
	if config.CallbackPath == "" {
		config.CallbackPath = defaultCallbackPath
	}
	if config.LoginTimeout <= 0 {
		config.LoginTimeout = 3 * time.Minute
	}
	if config.RefreshBefore <= 0 {
		config.RefreshBefore = time.Minute
	}
	if deps.Store == nil || deps.OpenBrowser == nil || !validConfig(config) {
		return nil, ErrAuthenticationFailed
	}
	client := deps.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	now := deps.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{config: config, store: deps.Store, openBrowser: deps.OpenBrowser, client: client, now: now}, nil
}

func validConfig(config Config) bool {
	if config.ClientID == "" || config.Scope == "" || !strings.HasPrefix(config.CallbackPath, "/") {
		return false
	}
	host, _, err := net.SplitHostPort(config.CallbackAddress)
	if err != nil || host != "127.0.0.1" {
		return false
	}
	for _, raw := range []string{config.Issuer, config.AuthorizationURL, config.TokenURL} {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
			return false
		}
	}
	return true
}

// EnsureAuthenticated reuses the locally protected refresh token whenever
// possible. The browser is opened only if no valid refresh session remains.
func (s *Service) EnsureAuthenticated(ctx context.Context) (Status, error) {
	if _, expiresAt, err := s.AccessToken(ctx); err == nil {
		return Status{Authenticated: true, ExpiresAt: expiresAt}, nil
	} else if !errors.Is(err, ErrAuthenticationRequired) {
		return Status{}, ErrAuthenticationFailed
	}
	return s.interactiveLogin(ctx)
}

// AccessToken satisfies the sync runtime's token-source contract. It refreshes
// before expiry and commits a rotated refresh token to the OS keychain before
// exposing the corresponding access token to a request.
func (s *Service) AccessToken(ctx context.Context) (string, time.Time, error) {
	if s == nil {
		return "", time.Time{}, ErrAuthenticationRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", time.Time{}, err
	}
	if token, expiresAt, ok := s.cachedToken(); ok {
		return token, expiresAt, nil
	}

	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	if token, expiresAt, ok := s.cachedToken(); ok {
		return token, expiresAt, nil
	}
	refreshToken, found, err := s.store.Load()
	if err != nil || !found || !validToken(refreshToken) {
		return "", time.Time{}, ErrAuthenticationRequired
	}
	tokens, invalidGrant, err := s.exchange(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {s.config.ClientID},
		"refresh_token": {refreshToken},
	})
	if invalidGrant {
		_ = s.store.Delete()
		return "", time.Time{}, ErrAuthenticationRequired
	}
	if err != nil {
		return "", time.Time{}, ErrAuthenticationFailed
	}
	if tokens.RefreshToken == "" || s.store.Save(tokens.RefreshToken) != nil {
		return "", time.Time{}, ErrAuthenticationFailed
	}
	s.setAccessToken(tokens.AccessToken, tokens.ExpiresAt)
	return tokens.AccessToken, tokens.ExpiresAt, nil
}

func (s *Service) Status() Status {
	if s == nil {
		return Status{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return Status{Authenticated: s.accessToken != "" && s.now().Before(s.expiresAt), ExpiresAt: s.expiresAt}
}

// Logout removes the long-lived credential from the operating-system keychain
// and drops the access token immediately. It does not attempt a remote IdP
// logout because that would require a browser session and is best handled by
// the user in Keycloak when desired.
func (s *Service) Logout() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	s.accessToken = ""
	s.expiresAt = time.Time{}
	s.mu.Unlock()
	if err := s.store.Delete(); err != nil {
		return ErrAuthenticationFailed
	}
	return nil
}

func (s *Service) cachedToken() (string, time.Time, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.accessToken == "" || !s.now().Add(s.config.RefreshBefore).Before(s.expiresAt) {
		return "", time.Time{}, false
	}
	return s.accessToken, s.expiresAt, true
}

func (s *Service) setAccessToken(token string, expiresAt time.Time) {
	s.mu.Lock()
	s.accessToken = token
	s.expiresAt = expiresAt.UTC()
	s.mu.Unlock()
}

func (s *Service) interactiveLogin(ctx context.Context) (Status, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.interactiveMu.Lock()
	defer s.interactiveMu.Unlock()
	// Another caller may have completed the browser flow while this one waited.
	if token, expiresAt, ok := s.cachedToken(); ok && token != "" {
		return Status{Authenticated: true, ExpiresAt: expiresAt}, nil
	}
	listener, err := net.Listen("tcp4", s.config.CallbackAddress)
	if err != nil {
		return Status{}, ErrAuthenticationFailed
	}
	defer listener.Close()

	state, err := randomURLValue(32)
	if err != nil {
		return Status{}, ErrAuthenticationFailed
	}
	verifier, err := randomURLValue(48)
	if err != nil {
		return Status{}, ErrAuthenticationFailed
	}
	challengeBytes := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(challengeBytes[:])
	redirectURI := "http://" + listener.Addr().String() + s.config.CallbackPath

	result := make(chan callbackResult, 1)
	server := &http.Server{Handler: callbackHandler(s.config.CallbackPath, state, result), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(listener) }()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	authorizeURL, err := url.Parse(s.config.AuthorizationURL)
	if err != nil {
		return Status{}, ErrAuthenticationFailed
	}
	query := authorizeURL.Query()
	query.Set("response_type", "code")
	query.Set("client_id", s.config.ClientID)
	query.Set("redirect_uri", redirectURI)
	query.Set("scope", s.config.Scope)
	query.Set("state", state)
	query.Set("code_challenge", challenge)
	query.Set("code_challenge_method", "S256")
	authorizeURL.RawQuery = query.Encode()
	if err := s.openBrowser(authorizeURL.String()); err != nil {
		return Status{}, ErrAuthenticationFailed
	}

	timer := time.NewTimer(s.config.LoginTimeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return Status{}, ctx.Err()
	case <-timer.C:
		return Status{}, ErrAuthenticationRequired
	case callback := <-result:
		if callback.err != nil {
			return Status{}, ErrAuthenticationRequired
		}
		tokens, _, err := s.exchange(ctx, url.Values{
			"grant_type":    {"authorization_code"},
			"client_id":     {s.config.ClientID},
			"code":          {callback.code},
			"redirect_uri":  {redirectURI},
			"code_verifier": {verifier},
		})
		if err != nil || tokens.RefreshToken == "" || s.store.Save(tokens.RefreshToken) != nil {
			return Status{}, ErrAuthenticationFailed
		}
		s.setAccessToken(tokens.AccessToken, tokens.ExpiresAt)
		return Status{Authenticated: true, ExpiresAt: tokens.ExpiresAt}, nil
	}
}

type callbackResult struct {
	code string
	err  error
}

func callbackHandler(path, state string, result chan<- callbackResult) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != path {
			http.NotFound(writer, request)
			return
		}
		candidateState := request.URL.Query().Get("state")
		code := request.URL.Query().Get("code")
		if code == "" || subtle.ConstantTimeCompare([]byte(state), []byte(candidateState)) != 1 {
			select {
			case result <- callbackResult{err: ErrAuthenticationFailed}:
			default:
			}
			http.Error(writer, "Authentication could not be completed.", http.StatusBadRequest)
			return
		}
		select {
		case result <- callbackResult{code: code}:
		default:
		}
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(writer, "<html><body>You may return to DevKit.</body></html>")
	})
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

type tokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

func (s *Service) exchange(ctx context.Context, form url.Values) (tokens, bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.config.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return tokens{}, false, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		return tokens{}, false, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusBadRequest || response.StatusCode == http.StatusUnauthorized {
		return tokens{}, true, ErrAuthenticationFailed
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return tokens{}, false, ErrAuthenticationFailed
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxTokenBytes))
	if err != nil {
		return tokens{}, false, err
	}
	var decoded tokenResponse
	if err := json.Unmarshal(body, &decoded); err != nil || !validToken(decoded.AccessToken) || decoded.ExpiresIn <= 0 {
		return tokens{}, false, ErrAuthenticationFailed
	}
	if decoded.ExpiresIn > int64((24 * time.Hour).Seconds()) {
		return tokens{}, false, ErrAuthenticationFailed
	}
	return tokens{AccessToken: decoded.AccessToken, RefreshToken: decoded.RefreshToken, ExpiresAt: s.now().Add(time.Duration(decoded.ExpiresIn) * time.Second).UTC()}, false, nil
}

func validToken(token string) bool {
	return token != "" && len(token) <= maxTokenBytes && strings.IndexFunc(token, func(r rune) bool { return r <= 0x20 || r == 0x7f }) < 0
}

func randomURLValue(bytes int) (string, error) {
	value := make([]byte, bytes)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("random value: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
