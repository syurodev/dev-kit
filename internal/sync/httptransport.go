package sync

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	deviceHeader                = "X-DevKit-Device-ID"
	protocolHeader              = "X-DevKit-Sync-Protocol"
	maxHTTPRequestBytes         = 4 << 20
	maxHTTPResponseBytes        = 4 << 20
	maxOperationCiphertextBytes = 1 << 20
	maxPullOperations           = 1000
	defaultRequestTimeout       = 30 * time.Second
)

// PullResult contains encrypted operations and the server's opaque next
// cursor. Pulled ciphertext is not interpreted by the transport.
type PullResult struct {
	Operations []PushOperation
	NextCursor Cursor
}

// PullTransport is the optional bidirectional capability used by remote sync.
// Keeping it separate preserves the push-only runner and local transports.
type PullTransport interface {
	Transport
	Pull(ctx context.Context, cursor Cursor, limit int) (PullResult, error)
}

// HTTPTransport sends encrypted replication envelopes to an authenticated
// remote service. It has no access to vault keys or plaintext record data.
type HTTPTransport struct {
	baseURL  *url.URL
	sessions SessionProvider
	client   *http.Client
	now      func() time.Time
}

// HTTPAuthenticator establishes server-authoritative account context before a
// SessionProvider exposes it to HTTPTransport.
type HTTPAuthenticator struct {
	baseURL *url.URL
	client  *http.Client
	now     func() time.Time
}

func NewHTTPAuthenticator(baseURL string, client *http.Client) (*HTTPAuthenticator, error) {
	parsed, err := validateHTTPBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	return &HTTPAuthenticator{
		baseURL: parsed,
		client:  secureHTTPClient(client),
		now:     func() time.Time { return time.Now().UTC() },
	}, nil
}

// Authenticate calls the session handshake without putting credentials or
// account identity in a request body. AccountID is accepted only from the
// authenticated response and the returned device must match the local device.
func (a *HTTPAuthenticator) Authenticate(ctx context.Context, accessToken, localDeviceID string) (AuthSession, error) {
	if a == nil || a.baseURL == nil || a.client == nil {
		return AuthSession{}, TransportError{Reason: FailureUnknown}
	}
	if err := validateSyncIdentifier("device id", localDeviceID, 256); err != nil {
		return AuthSession{}, TransportError{Reason: FailureInvalidResponse}
	}
	if err := validateAccessToken(accessToken); err != nil {
		return AuthSession{}, TransportError{Reason: FailureUnknown}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	target := a.baseURL.JoinPath("v1/sync/session")
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return AuthSession{}, TransportError{Reason: FailureInvalidResponse}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set(deviceHeader, localDeviceID)
	request.Header.Set(protocolHeader, strconv.FormatUint(uint64(SyncProtocolVersion), 10))

	response, err := doHTTP(a.client, request)
	if err != nil {
		return AuthSession{}, err
	}
	defer response.Body.Close()
	if err := validateHTTPStatus(response.StatusCode); err != nil {
		return AuthSession{}, err
	}
	var wire sessionResponse
	if err := decodeBoundedJSON(response.Body, &wire); err != nil {
		return AuthSession{}, TransportError{Reason: FailureInvalidResponse}
	}
	if wire.DeviceID != localDeviceID {
		return AuthSession{}, TransportError{Reason: FailureInvalidResponse}
	}
	session, err := newAuthSession(wire.AccountID, wire.DeviceID, accessToken, wire.ExpiresAt)
	if err != nil || session.Validate(a.now()) != nil {
		return AuthSession{}, TransportError{Reason: FailureInvalidResponse}
	}
	return session, nil
}

// NewHTTPTransport constructs a secure HTTP transport. Plain HTTP is accepted
// only for explicit loopback hosts used by local development and httptest.
func NewHTTPTransport(baseURL string, sessions SessionProvider, client *http.Client) (*HTTPTransport, error) {
	parsed, err := validateHTTPBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	if sessions == nil {
		return nil, errors.New("session provider is required")
	}

	return &HTTPTransport{
		baseURL:  parsed,
		sessions: sessions,
		client:   secureHTTPClient(client),
		now:      func() time.Time { return time.Now().UTC() },
	}, nil
}

func (t *HTTPTransport) Push(ctx context.Context, operations []PushOperation) (PushResult, error) {
	session, err := t.currentSession(ctx)
	if err != nil {
		return PushResult{}, err
	}
	if err := validatePushOperationsForSession(operations, session); err != nil {
		return PushResult{}, TransportError{Reason: FailureInvalidResponse}
	}

	body, err := json.Marshal(pushRequest{Operations: operations})
	if err != nil || len(body) > maxHTTPRequestBytes {
		return PushResult{}, TransportError{Reason: FailureInvalidResponse}
	}
	response, err := t.do(ctx, http.MethodPost, "v1/sync/push", body, session, nil)
	if err != nil {
		return PushResult{}, err
	}
	defer response.Body.Close()
	if err := validateHTTPStatus(response.StatusCode); err != nil {
		return PushResult{}, err
	}

	var wire pushResponse
	if err := decodeBoundedJSON(response.Body, &wire); err != nil {
		return PushResult{}, TransportError{Reason: FailureInvalidResponse}
	}
	result := PushResult{SentIdempotencyKeys: append([]string(nil), wire.SentIdempotencyKeys...)}
	result.Conflicts = make([]Conflict, 0, len(wire.Conflicts))
	for _, conflict := range wire.Conflicts {
		result.Conflicts = append(result.Conflicts, conflict.toConflict())
	}
	if err := validateHTTPPushResult(operations, result); err != nil {
		return PushResult{}, TransportError{Reason: FailureInvalidResponse}
	}
	return result, nil
}

func (t *HTTPTransport) Pull(ctx context.Context, cursor Cursor, limit int) (PullResult, error) {
	if err := cursor.Validate(); err != nil || limit <= 0 || limit > maxPullOperations {
		return PullResult{}, TransportError{Reason: FailureInvalidResponse}
	}
	session, err := t.currentSession(ctx)
	if err != nil {
		return PullResult{}, err
	}
	query := url.Values{
		"cursor": []string{cursor.Token},
		"limit":  []string{strconv.Itoa(limit)},
	}
	response, err := t.do(ctx, http.MethodGet, "v1/sync/pull", nil, session, query)
	if err != nil {
		return PullResult{}, err
	}
	defer response.Body.Close()
	if err := validateHTTPStatus(response.StatusCode); err != nil {
		return PullResult{}, err
	}

	var wire pullResponse
	if err := decodeBoundedJSON(response.Body, &wire); err != nil {
		return PullResult{}, TransportError{Reason: FailureInvalidResponse}
	}
	result, err := validatePullResponse(wire, session, limit)
	if err != nil {
		return PullResult{}, TransportError{Reason: FailureInvalidResponse}
	}
	return result, nil
}

func (t *HTTPTransport) currentSession(ctx context.Context) (AuthSession, error) {
	if t == nil || t.sessions == nil || t.client == nil || t.baseURL == nil {
		return AuthSession{}, TransportError{Reason: FailureUnknown}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return AuthSession{}, err
	}
	session, err := t.sessions.Session(ctx)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return AuthSession{}, ctxErr
		}
		// Provider errors may contain credential details, so do not propagate
		// their text beyond this boundary.
		return AuthSession{}, TransportError{Reason: FailureUnknown}
	}
	if err := session.Validate(t.now()); err != nil {
		return AuthSession{}, ErrAuthentication
	}
	return session, nil
}

func (t *HTTPTransport) do(ctx context.Context, method, endpoint string, body []byte, session AuthSession, query url.Values) (*http.Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	target := t.baseURL.JoinPath(endpoint)
	if query != nil {
		target.RawQuery = query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, method, target.String(), bytes.NewReader(body))
	if err != nil {
		return nil, TransportError{Reason: FailureInvalidResponse}
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Authorization", "Bearer "+session.accessToken)
	request.Header.Set(deviceHeader, session.deviceID)
	request.Header.Set(protocolHeader, strconv.FormatUint(uint64(SyncProtocolVersion), 10))

	return doHTTP(t.client, request)
}

func doHTTP(client *http.Client, request *http.Request) (*http.Response, error) {
	response, err := client.Do(request)
	if err == nil {
		return response, nil
	}
	if ctxErr := request.Context().Err(); ctxErr != nil {
		return nil, ctxErr
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil, err
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return nil, TransportError{Reason: FailureTimeout}
	}
	return nil, TransportError{Reason: FailureNetwork}
}

func secureHTTPClient(client *http.Client) *http.Client {
	if client == nil {
		client = &http.Client{Timeout: defaultRequestTimeout}
	} else {
		clone := *client
		client = &clone
		if client.Timeout <= 0 {
			client.Timeout = defaultRequestTimeout
		}
	}
	// Redirects are rejected rather than relying on header-copy behavior. This
	// guarantees a bearer token is never forwarded to another origin.
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return client
}

func validateHTTPBaseURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return nil, errors.New("invalid sync base URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return nil, errors.New("sync base URL must not contain user info, query, or fragment")
	}
	if parsed.Scheme != "https" {
		if parsed.Scheme != "http" || !isLoopbackHost(parsed.Hostname()) {
			return nil, errors.New("sync base URL requires HTTPS")
		}
	}
	parsed.RawPath = ""
	return parsed, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func validatePushOperationsForSession(operations []PushOperation, session AuthSession) error {
	if len(operations) > maxPullOperations {
		return errors.New("push batch is too large")
	}
	seen := make(map[string]struct{}, len(operations))
	// Account for the request wrapper and each comma. The per-operation JSON
	// estimate below is exact for the ciphertext substitution and avoids ever
	// building an oversized batch allocation.
	estimatedBodyBytes := len(`{"operations":[]}`)
	for _, operation := range operations {
		if len(operation.Envelope.Ciphertext) > maxOperationCiphertextBytes {
			return errors.New("push operation ciphertext is too large")
		}
		if err := operation.Validate(); err != nil {
			return err
		}
		if operation.Envelope.AccountID != session.accountID ||
			operation.Envelope.DeviceID != session.deviceID ||
			operation.Envelope.ProtocolVersion != SyncProtocolVersion {
			return errors.New("push operation does not match authenticated session")
		}
		if _, duplicate := seen[operation.IdempotencyKey]; duplicate {
			return errors.New("duplicate push idempotency key")
		}
		seen[operation.IdempotencyKey] = struct{}{}

		metadataOnly := operation
		metadataOnly.Envelope.Ciphertext = nil
		metadataJSON, err := json.Marshal(metadataOnly)
		if err != nil {
			return errors.New("invalid push operation metadata")
		}
		// A nil []byte marshals as `null`; the real ciphertext marshals as a
		// quoted base64 string. Replace those lengths without allocating it.
		estimatedBodyBytes += len(metadataJSON) - len("null") +
			base64.StdEncoding.EncodedLen(len(operation.Envelope.Ciphertext)) + 2
		if len(seen) > 1 {
			estimatedBodyBytes++
		}
		if estimatedBodyBytes > maxHTTPRequestBytes {
			return errors.New("push body is too large")
		}
	}
	return nil
}

func validateHTTPStatus(status int) error {
	if status >= http.StatusOK && status < http.StatusMultipleChoices {
		return nil
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return ErrAuthentication
	}
	if status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= http.StatusInternalServerError {
		return TransportError{Reason: FailureNetwork}
	}
	return TransportError{Reason: FailureInvalidResponse}
}

func decodeBoundedJSON(reader io.Reader, destination any) error {
	limited := io.LimitReader(reader, maxHTTPResponseBytes+1)
	payload, err := io.ReadAll(limited)
	if err != nil || len(payload) == 0 || len(payload) > maxHTTPResponseBytes {
		return errors.New("invalid sync response")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return errors.New("invalid sync response")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("invalid sync response")
	}
	return nil
}

func validateHTTPPushResult(operations []PushOperation, result PushResult) error {
	known := make(map[string]PushOperation, len(operations))
	for _, operation := range operations {
		known[operation.IdempotencyKey] = operation
	}
	seen := make(map[string]struct{}, len(result.SentIdempotencyKeys)+len(result.Conflicts))
	for _, key := range result.SentIdempotencyKeys {
		if _, ok := known[key]; !ok {
			return errors.New("unknown push acknowledgement")
		}
		if _, duplicate := seen[key]; duplicate {
			return errors.New("duplicate push acknowledgement")
		}
		seen[key] = struct{}{}
	}
	for _, conflict := range result.Conflicts {
		operation, ok := known[conflict.LocalChangeKey]
		if !ok || conflict.Validate() != nil || conflict.RecordID != operation.Envelope.RecordID || conflict.RecordType != operation.Envelope.RecordType {
			return errors.New("invalid push conflict")
		}
		if _, duplicate := seen[conflict.LocalChangeKey]; duplicate {
			return errors.New("duplicate push acknowledgement")
		}
		seen[conflict.LocalChangeKey] = struct{}{}
	}
	return nil
}

func validatePullResponse(wire pullResponse, session AuthSession, limit int) (PullResult, error) {
	if wire.AccountID != session.accountID || wire.DeviceID != session.deviceID || wire.ProtocolVersion != SyncProtocolVersion {
		return PullResult{}, errors.New("pull response does not match authenticated session")
	}
	if len(wire.Operations) > limit {
		return PullResult{}, errors.New("pull response exceeds requested limit")
	}
	nextCursor := Cursor{Token: wire.NextCursor}
	if err := nextCursor.Validate(); err != nil {
		return PullResult{}, err
	}
	seenKeys := make(map[string]struct{}, len(wire.Operations))
	seenOperations := make(map[string]struct{}, len(wire.Operations))
	operations := make([]PushOperation, 0, len(wire.Operations))
	for _, operation := range wire.Operations {
		if err := operation.Validate(); err != nil {
			return PullResult{}, err
		}
		// Source device IDs may differ for cross-device sync. Envelope.Validate
		// checks their syntax; account and protocol must match this session.
		if operation.Envelope.AccountID != session.accountID || operation.Envelope.ProtocolVersion != SyncProtocolVersion {
			return PullResult{}, errors.New("pulled operation has invalid account or protocol")
		}
		if _, duplicate := seenKeys[operation.IdempotencyKey]; duplicate {
			return PullResult{}, errors.New("duplicate pulled idempotency key")
		}
		if _, duplicate := seenOperations[operation.Envelope.OperationID]; duplicate {
			return PullResult{}, errors.New("duplicate pulled operation id")
		}
		seenKeys[operation.IdempotencyKey] = struct{}{}
		seenOperations[operation.Envelope.OperationID] = struct{}{}
		operations = append(operations, clonePushOperation(operation))
	}
	return PullResult{Operations: operations, NextCursor: nextCursor}, nil
}

type pushRequest struct {
	Operations []PushOperation `json:"operations"`
}

type pushResponse struct {
	SentIdempotencyKeys []string       `json:"sent_idempotency_keys"`
	Conflicts           []conflictWire `json:"conflicts"`
}

type pullResponse struct {
	AccountID       string          `json:"account_id"`
	DeviceID        string          `json:"device_id"`
	ProtocolVersion uint32          `json:"protocol_version"`
	Operations      []PushOperation `json:"operations"`
	NextCursor      string          `json:"next_cursor"`
}

type sessionResponse struct {
	AccountID string    `json:"account_id"`
	DeviceID  string    `json:"device_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

type conflictWire struct {
	ID              string    `json:"id"`
	RecordID        string    `json:"record_id"`
	RecordType      string    `json:"record_type"`
	LocalChangeKey  string    `json:"local_change_key"`
	RemoteChangeKey string    `json:"remote_change_key"`
	DetectedAt      time.Time `json:"detected_at"`
}

func (c conflictWire) toConflict() Conflict {
	return Conflict{
		ID:              c.ID,
		RecordID:        c.RecordID,
		RecordType:      c.RecordType,
		LocalChangeKey:  c.LocalChangeKey,
		RemoteChangeKey: c.RemoteChangeKey,
		DetectedAt:      c.DetectedAt,
	}
}
