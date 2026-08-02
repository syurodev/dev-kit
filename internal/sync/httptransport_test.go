package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type staticSessionProvider struct {
	session AuthSession
	err     error
	calls   atomic.Int32
}

func (p *staticSessionProvider) Session(context.Context) (AuthSession, error) {
	p.calls.Add(1)
	return p.session, p.err
}

func testAuthSession(t *testing.T, expiresAt time.Time) AuthSession {
	t.Helper()
	session, err := newAuthSession("acct-1", "dev-1", "access-token", expiresAt)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func testPushOperation(t *testing.T, key string) PushOperation {
	t.Helper()
	operation, err := pushOperationFromChange(validChange(key, time.Unix(100, 0).UTC()))
	if err != nil {
		t.Fatal(err)
	}
	return operation
}

func newTestHTTPTransport(t *testing.T, server *httptest.Server, provider SessionProvider, now time.Time) *HTTPTransport {
	t.Helper()
	transport, err := NewHTTPTransport(server.URL, provider, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	transport.now = func() time.Time { return now }
	return transport
}

func assertAuthHeaders(t *testing.T, request *http.Request) {
	t.Helper()
	if got := request.Header.Get("Authorization"); got != "Bearer access-token" {
		t.Errorf("Authorization = %q", got)
	}
	if got := request.Header.Get(deviceHeader); got != "dev-1" {
		t.Errorf("device header = %q", got)
	}
	if got := request.Header.Get(protocolHeader); got != fmt.Sprint(SyncProtocolVersion) {
		t.Errorf("protocol header = %q", got)
	}
}

func TestHTTPAuthenticatorUsesServerAccountAndLocalDevice(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/v1/sync/session" {
			t.Errorf("unexpected handshake request: %s %s", request.Method, request.URL.Path)
			return
		}
		assertAuthHeaders(t, request)
		if request.Body != http.NoBody {
			payload, _ := io.ReadAll(request.Body)
			if len(payload) != 0 {
				t.Errorf("handshake body must be empty: %q", payload)
				return
			}
		}
		_ = json.NewEncoder(writer).Encode(sessionResponse{
			AccountID: "server-account",
			DeviceID:  "dev-1",
			ExpiresAt: now.Add(time.Hour),
		})
	}))
	defer server.Close()

	authenticator, err := NewHTTPAuthenticator(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	authenticator.now = func() time.Time { return now }
	session, err := authenticator.Authenticate(context.Background(), "access-token", "dev-1")
	if err != nil {
		t.Fatal(err)
	}
	if session.AccountID() != "server-account" || session.DeviceID() != "dev-1" {
		t.Fatalf("server session context not preserved: %v", session)
	}
}

func TestHTTPAuthenticatorRejectsDeviceMismatchAndExpiredResponse(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	for _, testCase := range []struct {
		name      string
		deviceID  string
		expiresAt time.Time
	}{
		{name: "device mismatch", deviceID: "other-device", expiresAt: now.Add(time.Hour)},
		{name: "expired", deviceID: "dev-1", expiresAt: now},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(writer).Encode(sessionResponse{
					AccountID: "acct-1", DeviceID: testCase.deviceID, ExpiresAt: testCase.expiresAt,
				})
			}))
			defer server.Close()
			authenticator, err := NewHTTPAuthenticator(server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			authenticator.now = func() time.Time { return now }
			if _, err := authenticator.Authenticate(context.Background(), "access-token", "dev-1"); err == nil {
				t.Fatal("invalid handshake response must be rejected")
			}
		})
	}
}

func TestHTTPTransportPushHeadersAndBody(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	operation := testPushOperation(t, "push-1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/sync/push" {
			t.Errorf("unexpected push request: %s %s", request.Method, request.URL.Path)
			return
		}
		assertAuthHeaders(t, request)
		if request.Header.Get("Content-Type") != "application/json" {
			t.Fatal("push content type is missing")
		}
		var body pushRequest
		decoder := json.NewDecoder(request.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil {
			t.Errorf("decode push request: %v", err)
			return
		}
		if len(body.Operations) != 1 || body.Operations[0].IdempotencyKey != operation.IdempotencyKey {
			t.Errorf("push operations = %+v", body.Operations)
			return
		}
		_ = json.NewEncoder(writer).Encode(pushResponse{SentIdempotencyKeys: []string{operation.IdempotencyKey}})
	}))
	defer server.Close()
	provider := &staticSessionProvider{session: testAuthSession(t, now.Add(time.Hour))}
	transport := newTestHTTPTransport(t, server, provider, now)

	result, err := transport.Push(context.Background(), []PushOperation{operation})
	if err != nil || len(result.SentIdempotencyKeys) != 1 || result.SentIdempotencyKeys[0] != operation.IdempotencyKey {
		t.Fatalf("Push = %+v, %v", result, err)
	}
	if provider.calls.Load() != 1 {
		t.Fatalf("session provider calls = %d", provider.calls.Load())
	}
}

func TestHTTPTransportRejectsSessionMismatchBeforeNetwork(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()
	provider := &staticSessionProvider{session: testAuthSession(t, now.Add(time.Hour))}
	transport := newTestHTTPTransport(t, server, provider, now)

	for _, mutate := range []func(*PushOperation){
		func(operation *PushOperation) { operation.Envelope.AccountID = "other-account" },
		func(operation *PushOperation) { operation.Envelope.DeviceID = "other-device" },
	} {
		operation := testPushOperation(t, "mismatch")
		mutate(&operation)
		if _, err := transport.Push(context.Background(), []PushOperation{operation}); err == nil {
			t.Fatal("session mismatch must fail")
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("mismatch made %d network requests", requests.Load())
	}
}

func TestHTTPTransportRejectsExpiredSessionBeforeNetwork(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer server.Close()
	provider := &staticSessionProvider{session: testAuthSession(t, now)}
	transport := newTestHTTPTransport(t, server, provider, now)

	if _, err := transport.Push(context.Background(), []PushOperation{testPushOperation(t, "expired")}); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("expired session error = %v, want ErrAuthentication", err)
	}
	if requests.Load() != 0 {
		t.Fatal("expired session reached the network")
	}
}

func TestHTTPTransportClassifiesAuthenticationStatusWithoutResponseBody(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "sensitive upstream detail", http.StatusUnauthorized)
	}))
	defer server.Close()
	transport := newTestHTTPTransport(t, server, &staticSessionProvider{session: testAuthSession(t, now.Add(time.Hour))}, now)

	_, err := transport.Push(context.Background(), []PushOperation{testPushOperation(t, "unauthorized")})
	if !errors.Is(err, ErrAuthentication) || strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("authentication error = %v", err)
	}
}

func TestHTTPTransportRequiresTLSOutsideLoopback(t *testing.T) {
	provider := &staticSessionProvider{}
	for _, rawURL := range []string{
		"http://example.com", "https://user@example.com", "https://example.com?tenant=x", "https://example.com/#fragment",
	} {
		if _, err := NewHTTPTransport(rawURL, provider, nil); err == nil {
			t.Fatalf("unsafe URL accepted: %s", rawURL)
		}
	}
	if _, err := NewHTTPTransport("http://127.0.0.1:9245", provider, nil); err != nil {
		t.Fatalf("loopback HTTP should be accepted: %v", err)
	}
}

func TestHTTPTransportDoesNotFollowRedirectsWithAuthorization(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	var redirectedRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		redirectedRequests.Add(1)
		if request.Header.Get("Authorization") != "" {
			t.Error("redirect leaked Authorization header")
		}
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assertAuthHeaders(t, request)
		http.Redirect(writer, request, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	provider := &staticSessionProvider{session: testAuthSession(t, now.Add(time.Hour))}
	transport := newTestHTTPTransport(t, source, provider, now)

	if _, err := transport.Push(context.Background(), []PushOperation{testPushOperation(t, "redirect")}); err == nil {
		t.Fatal("redirect response must be rejected")
	}
	if redirectedRequests.Load() != 0 {
		t.Fatalf("redirect target received %d requests", redirectedRequests.Load())
	}
}

func TestHTTPTransportRejectsOversizedRequestBeforeNetwork(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer server.Close()
	provider := &staticSessionProvider{session: testAuthSession(t, now.Add(time.Hour))}
	transport := newTestHTTPTransport(t, server, provider, now)
	operation := testPushOperation(t, "large-request")
	operation.Envelope.Ciphertext = make([]byte, maxHTTPRequestBytes+1)

	if _, err := transport.Push(context.Background(), []PushOperation{operation}); err == nil {
		t.Fatal("oversized request must fail")
	}
	if requests.Load() != 0 {
		t.Fatal("oversized request reached the network")
	}
}

func TestHTTPTransportPreflightsPushBoundsBeforeMarshalOrNetwork(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer server.Close()
	provider := &staticSessionProvider{session: testAuthSession(t, now.Add(time.Hour))}
	transport := newTestHTTPTransport(t, server, provider, now)

	tooMany := make([]PushOperation, maxPullOperations+1)
	if _, err := transport.Push(context.Background(), tooMany); err == nil {
		t.Fatal("oversized operation count must fail")
	}

	perOperation := testPushOperation(t, "per-operation")
	perOperation.Envelope.Ciphertext = make([]byte, maxOperationCiphertextBytes+1)
	if _, err := transport.Push(context.Background(), []PushOperation{perOperation}); err == nil {
		t.Fatal("oversized per-operation ciphertext must fail")
	}

	// Keep every ciphertext below 1 MiB while making its outer base64 JSON
	// representation push the aggregate body beyond 4 MiB.
	nestedEnvelope := []byte(fmt.Sprintf(
		`{"version":2,"algorithm":"xchacha20-poly1305","key_id":"key-1","nonce":"AA==","ciphertext":"%s"}`,
		strings.Repeat("A", 800<<10),
	))
	aggregate := make([]PushOperation, 0, 4)
	for i := 0; i < 4; i++ {
		operation := testPushOperation(t, fmt.Sprintf("aggregate-%d", i))
		operation.Envelope.Ciphertext = nestedEnvelope
		aggregate = append(aggregate, operation)
	}
	if _, err := transport.Push(context.Background(), aggregate); err == nil {
		t.Fatal("oversized aggregate body estimate must fail")
	}
	if requests.Load() != 0 {
		t.Fatalf("preflight failures made %d network requests", requests.Load())
	}
}

func TestHTTPTransportRejectsOversizedAndMalformedResponses(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	operation := testPushOperation(t, "response")
	provider := &staticSessionProvider{session: testAuthSession(t, now.Add(time.Hour))}
	for _, testCase := range []struct {
		name string
		body func(http.ResponseWriter)
	}{
		{name: "oversized", body: func(writer http.ResponseWriter) {
			_, _ = io.WriteString(writer, strings.Repeat("x", maxHTTPResponseBytes+1))
		}},
		{name: "malformed", body: func(writer http.ResponseWriter) {
			_, _ = io.WriteString(writer, `{"sent_idempotency_keys":[],"conflicts":[],"unexpected":true}`)
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { testCase.body(writer) }))
			defer server.Close()
			transport := newTestHTTPTransport(t, server, provider, now)
			if _, err := transport.Push(context.Background(), []PushOperation{operation}); err == nil {
				t.Fatal("invalid response must fail")
			}
		})
	}
}

func TestHTTPTransportHonorsCancellation(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	operation := testPushOperation(t, "cancel")
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(started)
		select {
		case <-request.Context().Done():
		case <-release:
		case <-time.After(2 * time.Second):
		}
	}))
	defer func() {
		close(release)
		server.Close()
	}()
	provider := &staticSessionProvider{session: testAuthSession(t, now.Add(time.Hour))}
	transport := newTestHTTPTransport(t, server, provider, now)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := transport.Push(ctx, []PushOperation{operation})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not reach test server")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Push error = %v, want context cancellation", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Push did not return after context cancellation")
	}
}

func TestHTTPTransportPullValidatesContextCursorAndDuplicates(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	provider := &staticSessionProvider{session: testAuthSession(t, now.Add(time.Hour))}
	operation := testPushOperation(t, "remote-1")
	operation.Envelope.DeviceID = "remote-device"

	tests := []struct {
		name     string
		response pullResponse
		wantErr  bool
	}{
		{name: "valid cross-device", response: pullResponse{
			AccountID: "acct-1", DeviceID: "dev-1", ProtocolVersion: SyncProtocolVersion,
			Operations: []PushOperation{operation}, NextCursor: "next-opaque",
		}},
		{name: "wrong target device", response: pullResponse{
			AccountID: "acct-1", DeviceID: "other-device", ProtocolVersion: SyncProtocolVersion, NextCursor: "next",
		}, wantErr: true},
		{name: "wrong protocol", response: pullResponse{
			AccountID: "acct-1", DeviceID: "dev-1", ProtocolVersion: SyncProtocolVersion + 1, NextCursor: "next",
		}, wantErr: true},
		{name: "wrong operation account", response: pullResponse{
			AccountID: "acct-1", DeviceID: "dev-1", ProtocolVersion: SyncProtocolVersion,
			Operations: []PushOperation{func() PushOperation { copy := operation; copy.Envelope.AccountID = "other-account"; return copy }()},
		}, wantErr: true},
		{name: "duplicate idempotency", response: pullResponse{
			AccountID: "acct-1", DeviceID: "dev-1", ProtocolVersion: SyncProtocolVersion,
			Operations: []PushOperation{operation, operation},
		}, wantErr: true},
		{name: "duplicate operation id", response: pullResponse{
			AccountID: "acct-1", DeviceID: "dev-1", ProtocolVersion: SyncProtocolVersion,
			Operations: []PushOperation{operation, func() PushOperation { copy := operation; copy.IdempotencyKey = "remote-2"; return copy }()},
		}, wantErr: true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path != "/v1/sync/pull" || request.URL.Query().Get("cursor") != "current-opaque" || request.URL.Query().Get("limit") != "10" {
					t.Errorf("unexpected pull target: %s", request.URL.String())
					return
				}
				assertAuthHeaders(t, request)
				_ = json.NewEncoder(writer).Encode(testCase.response)
			}))
			defer server.Close()
			transport := newTestHTTPTransport(t, server, provider, now)
			result, err := transport.Pull(context.Background(), Cursor{Token: "current-opaque"}, 10)
			if testCase.wantErr && err == nil {
				t.Fatal("invalid pull response must fail")
			}
			if !testCase.wantErr && (err != nil || len(result.Operations) != 1 || result.NextCursor.Token != "next-opaque") {
				t.Fatalf("Pull = %+v, %v", result, err)
			}
		})
	}
}

func TestHTTPTransportRejectsInvalidPullCursorBeforeNetwork(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer server.Close()
	provider := &staticSessionProvider{session: testAuthSession(t, now.Add(time.Hour))}
	transport := newTestHTTPTransport(t, server, provider, now)

	if _, err := transport.Pull(context.Background(), Cursor{Token: strings.Repeat("x", 513)}, 10); err == nil {
		t.Fatal("oversized cursor must fail")
	}
	if requests.Load() != 0 {
		t.Fatal("invalid cursor reached the network")
	}
}
