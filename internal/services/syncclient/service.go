// Package syncclient coordinates one authenticated push/pull sync cycle.
//
// The service is deliberately runtime-only: it keeps the bearer-backed
// AuthSession and HTTP transport in memory, while persistence remains behind
// narrow local interfaces owned by the caller.
package syncclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	stdsync "sync"
	"time"

	"changeme/internal/crypto"
	"changeme/internal/services/vaultitem"
	"changeme/internal/storage"
	replicationsync "changeme/internal/sync"
)

const (
	defaultCursorName = "pull"
	defaultPushLimit  = 100
	defaultPullLimit  = 100
	maxRecordJSONSize = replicationsync.MaxRecordSnapshotBytes
)

var (
	ErrInvalidDependencies = errors.New("sync client dependencies are required")
	ErrNotConnected        = errors.New("sync client is not connected")
	ErrConnectFailed       = errors.New("sync connection failed")
	ErrSyncInProgress      = errors.New("sync is already in progress")
	ErrPushFailed          = errors.New("sync push failed")
	ErrPullFailed          = errors.New("sync pull failed")
	ErrInvalidRemoteData   = errors.New("remote sync data is invalid")
	ErrDecryptFailed       = errors.New("remote sync data could not be authenticated")
	ErrApplyFailed         = errors.New("remote sync data could not be applied")
	ErrLocalCaptureFailed  = errors.New("local sync changes could not be queued")
)

// DeviceIdentityStore owns only the stable local device identity. Account
// identity must come from the authenticated server handshake.
type DeviceIdentityStore interface {
	EnsureLocalDevice(ctx context.Context) (string, error)
}

// CursorStore returns the opaque cursor for the configured pull stream.
type CursorStore interface {
	GetCursor(ctx context.Context, name string) (replicationsync.Cursor, error)
}

// RemoteStore atomically applies authenticated mutations and advances the
// cursor. Implementations must roll back both when any mutation fails.
type RemoteStore interface {
	ValidateAuthenticatedAccount(ctx context.Context, accountID string) error
	BindAuthenticatedAccount(ctx context.Context, accountID string) error
	ApplyRemoteBatch(ctx context.Context, accountID, cursorName, nextCursor string, mutations []storage.RemoteMutation) error
}

// LocalMutationStore owns the durable handoff from account-neutral local
// intents to authenticated encrypted outbox changes.
type LocalMutationStore interface {
	Pending(ctx context.Context, limit int) ([]storage.LocalMutationIntent, error)
	CommitMaterialized(ctx context.Context, accountID string, intent storage.LocalMutationIntent, change replicationsync.Change) error
}

// Dependencies are the capabilities required by the orchestration service.
// HTTPClient may be nil, in which case the sync package supplies a bounded
// production client.
type Dependencies struct {
	DeviceIdentity DeviceIdentityStore
	Cursors        CursorStore
	Remote         RemoteStore
	LocalMutations LocalMutationStore
	Outbox         replicationsync.Outbox
	Vault          replicationsync.Vault
	HTTPClient     *http.Client
	CursorName     string
	PushLimit      int
	PullLimit      int
}

type AuthenticatorFactory func(baseURL string, client *http.Client) (replicationsync.Authenticator, error)
type TransportFactory func(baseURL string, sessions replicationsync.SessionProvider, client *http.Client) (replicationsync.PullTransport, error)

// Factories are exposed only to make network boundaries deterministic in
// tests. New installs the production HTTP implementations.
type Factories struct {
	Authenticator AuthenticatorFactory
	Transport     TransportFactory
}

// Status contains non-secret connection metadata suitable for UI/diagnostics.
// It intentionally has no token or raw transport state.
type Status struct {
	Connected bool      `json:"connected"`
	Endpoint  string    `json:"endpoint,omitempty"`
	AccountID string    `json:"account_id,omitempty"`
	DeviceID  string    `json:"device_id,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
}

// Report contains counts only. It never includes decrypted records, tokens,
// response bodies, or raw upstream error text.
type Report struct {
	PushAttempted int `json:"push_attempted"`
	PushSent      int `json:"push_sent"`
	PushRetried   int `json:"push_retried"`
	PushConflicts int `json:"push_conflicts"`
	Pulled        int `json:"pulled"`
	Applied       int `json:"applied"`
}

type sessionProvider struct {
	mu      stdsync.RWMutex
	session replicationsync.AuthSession
	set     bool
}

func (p *sessionProvider) Session(ctx context.Context) (replicationsync.AuthSession, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return replicationsync.AuthSession{}, err
		}
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if !p.set {
		return replicationsync.AuthSession{}, ErrNotConnected
	}
	return p.session, nil
}

func (p *sessionProvider) metadata() (replicationsync.AuthSession, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.session, p.set
}

func (p *sessionProvider) clear() {
	p.mu.Lock()
	p.session = replicationsync.AuthSession{}
	p.set = false
	p.mu.Unlock()
}

type connection struct {
	endpoint  string
	provider  *sessionProvider
	transport replicationsync.PullTransport
}

// Service serializes connection lifecycle changes with SyncNow. A second
// SyncNow fails immediately with ErrSyncInProgress instead of starting a
// competing push/pull transaction.
type Service struct {
	deps      Dependencies
	factories Factories

	stateMu stdsync.RWMutex
	state   *connection
	runMu   stdsync.Mutex
}

// New creates a service using the production HTTP authenticator and transport.
func New(deps Dependencies) (*Service, error) {
	return NewWithFactories(deps, Factories{
		Authenticator: func(baseURL string, client *http.Client) (replicationsync.Authenticator, error) {
			return replicationsync.NewHTTPAuthenticator(baseURL, client)
		},
		Transport: func(baseURL string, sessions replicationsync.SessionProvider, client *http.Client) (replicationsync.PullTransport, error) {
			return replicationsync.NewHTTPTransport(baseURL, sessions, client)
		},
	})
}

// NewWithFactories creates a service with injectable network constructors.
func NewWithFactories(deps Dependencies, factories Factories) (*Service, error) {
	if deps.DeviceIdentity == nil || deps.Cursors == nil || deps.Remote == nil || deps.Outbox == nil || deps.Vault == nil ||
		factories.Authenticator == nil || factories.Transport == nil {
		return nil, ErrInvalidDependencies
	}
	if deps.CursorName == "" {
		deps.CursorName = defaultCursorName
	}
	if deps.PushLimit <= 0 {
		deps.PushLimit = defaultPushLimit
	}
	if deps.PullLimit <= 0 {
		deps.PullLimit = defaultPullLimit
	}
	if deps.PullLimit > 1000 {
		return nil, ErrInvalidDependencies
	}
	return &Service{deps: deps, factories: factories}, nil
}

// Connect performs the authenticated handshake using the stable local device.
// The account and final device identity are accepted only from AuthSession.
func (s *Service) Connect(ctx context.Context, baseURL, accessToken string) error {
	if s == nil {
		return ErrInvalidDependencies
	}
	ctx = nonNilContext(ctx)
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	deviceID, err := s.deps.DeviceIdentity.EnsureLocalDevice(ctx)
	if err != nil {
		return safeStageError(ctx, ErrConnectFailed)
	}
	authenticator, err := s.factories.Authenticator(baseURL, s.deps.HTTPClient)
	if err != nil {
		return safeStageError(ctx, ErrConnectFailed)
	}
	session, err := authenticator.Authenticate(ctx, accessToken, deviceID)
	if err != nil {
		return safeStageError(ctx, ErrConnectFailed)
	}
	// Check an existing claim before constructing transport, but do not make the
	// first durable account claim until the user explicitly chooses SyncNow.
	if err := s.deps.Remote.ValidateAuthenticatedAccount(ctx, session.AccountID()); err != nil {
		return safeStageError(ctx, ErrConnectFailed)
	}
	provider := &sessionProvider{session: session, set: true}
	transport, err := s.factories.Transport(baseURL, provider, s.deps.HTTPClient)
	if err != nil {
		provider.clear()
		return safeStageError(ctx, ErrConnectFailed)
	}

	s.stateMu.Lock()
	previous := s.state
	s.state = &connection{endpoint: baseURL, provider: provider, transport: transport}
	s.stateMu.Unlock()
	if previous != nil {
		previous.provider.clear()
	}
	return nil
}

// Disconnect clears all runtime connection state. AuthSession intentionally
// stores its token in a private string, which Go cannot reliably zero; dropping
// the session is the strongest practical action without persisting a copy.
func (s *Service) Disconnect() {
	if s == nil {
		return
	}
	s.runMu.Lock()
	s.stateMu.Lock()
	previous := s.state
	s.state = nil
	s.stateMu.Unlock()
	if previous != nil {
		previous.provider.clear()
	}
	s.runMu.Unlock()
}

func (s *Service) Status() Status {
	if s == nil {
		return Status{}
	}
	s.stateMu.RLock()
	current := s.state
	if current == nil {
		s.stateMu.RUnlock()
		return Status{}
	}
	endpoint := current.endpoint
	provider := current.provider
	s.stateMu.RUnlock()
	session, ok := provider.metadata()
	if !ok {
		return Status{}
	}
	if err := session.Validate(time.Now().UTC()); err != nil {
		// Expired credentials must not remain available to later requests. The
		// compare-before-clear keeps a concurrent replacement connection intact.
		s.clearConnection(current)
		return Status{}
	}
	return Status{
		Connected: true,
		Endpoint:  endpoint,
		AccountID: session.AccountID(),
		DeviceID:  session.DeviceID(),
		ExpiresAt: session.ExpiresAt(),
	}
}

// SyncNow pushes the current outbox, pulls one bounded page, decrypts and
// validates each operation, then atomically applies it with the next cursor.
func (s *Service) SyncNow(ctx context.Context) (Report, error) {
	var report Report
	if s == nil {
		return report, ErrInvalidDependencies
	}
	if !s.runMu.TryLock() {
		return report, ErrSyncInProgress
	}
	defer s.runMu.Unlock()
	ctx = nonNilContext(ctx)
	if err := ctx.Err(); err != nil {
		return report, err
	}

	current, session, ok := s.snapshot()
	if !ok {
		return report, ErrNotConnected
	}
	if err := s.deps.Remote.BindAuthenticatedAccount(ctx, session.AccountID()); err != nil {
		return report, safeStageError(ctx, ErrLocalCaptureFailed)
	}
	if err := s.materializeLocalMutations(ctx, session); err != nil {
		return report, safeStageError(ctx, ErrLocalCaptureFailed)
	}
	push, err := replicationsync.NewRunner(s.deps.Outbox, current.transport).Run(ctx, s.deps.PushLimit)
	report.PushAttempted = push.Attempted
	report.PushSent = push.Sent
	report.PushRetried = push.Retried
	report.PushConflicts = push.Conflicts
	if err != nil {
		if errors.Is(err, replicationsync.ErrAuthentication) {
			s.clearConnection(current)
		}
		return report, safeStageError(ctx, ErrPushFailed)
	}

	cursor, err := s.deps.Cursors.GetCursor(ctx, s.deps.CursorName)
	if err != nil {
		return report, safeStageError(ctx, ErrPullFailed)
	}
	pulled, err := current.transport.Pull(ctx, cursor, s.deps.PullLimit)
	if err != nil {
		if errors.Is(err, replicationsync.ErrAuthentication) {
			s.clearConnection(current)
		}
		return report, safeStageError(ctx, ErrPullFailed)
	}
	if len(pulled.Operations) > s.deps.PullLimit || pulled.NextCursor.Validate() != nil {
		return report, ErrInvalidRemoteData
	}
	report.Pulled = len(pulled.Operations)

	mutations := make([]storage.RemoteMutation, 0, len(pulled.Operations))
	defer func() { zeroMutations(mutations) }()
	for _, operation := range pulled.Operations {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		mutation, err := s.decryptMutation(operation, session.AccountID())
		if err != nil {
			zeroMutations(mutations)
			return report, err
		}
		mutations = append(mutations, mutation)
	}
	if err := s.deps.Remote.ApplyRemoteBatch(ctx, session.AccountID(), s.deps.CursorName, pulled.NextCursor.Token, mutations); err != nil {
		return report, safeStageError(ctx, ErrApplyFailed)
	}
	report.Applied = len(mutations)
	return report, nil
}

func (s *Service) materializeLocalMutations(ctx context.Context, session replicationsync.AuthSession) error {
	if s.deps.LocalMutations == nil {
		return nil
	}
	intents, err := s.deps.LocalMutations.Pending(ctx, s.deps.PushLimit)
	if err != nil {
		return err
	}
	for i := range intents {
		intent := intents[i]
		if err := ctx.Err(); err != nil {
			zeroBytes(intent.Snapshot)
			return err
		}
		change, queueErr := replicationsync.QueuePayload(
			s.deps.Vault,
			s.deps.Outbox,
			replicationsync.SyncContext{
				AccountID:       session.AccountID(),
				DeviceID:        session.DeviceID(),
				ProtocolVersion: replicationsync.SyncProtocolVersion,
				OperationID:     intent.OperationID,
				EntityVersion:   intent.EntityVersion,
			},
			intent.RecordID,
			intent.RecordType,
			intent.KeyID,
			intent.KeyVersion,
			intent.Operation,
			intent.Snapshot,
			intent.CreatedAt,
		)
		zeroBytes(intent.Snapshot)
		if queueErr != nil {
			return queueErr
		}
		if err := s.deps.LocalMutations.CommitMaterialized(ctx, session.AccountID(), intent, change); err != nil {
			return err
		}
	}
	return nil
}

// clearConnection removes current only if it is still the active connection.
// This prevents an old failing request from clearing a newer successful login.
func (s *Service) clearConnection(current *connection) {
	if current == nil {
		return
	}
	s.stateMu.Lock()
	if s.state == current {
		s.state = nil
	}
	s.stateMu.Unlock()
	current.provider.clear()
}

func (s *Service) snapshot() (*connection, replicationsync.AuthSession, bool) {
	s.stateMu.RLock()
	current := s.state
	s.stateMu.RUnlock()
	if current == nil || current.transport == nil || current.provider == nil {
		return nil, replicationsync.AuthSession{}, false
	}
	session, ok := current.provider.metadata()
	return current, session, ok
}

func (s *Service) decryptMutation(operation replicationsync.PushOperation, accountID string) (storage.RemoteMutation, error) {
	if err := operation.Validate(); err != nil || operation.Envelope.AccountID != accountID || operation.Envelope.ProtocolVersion != replicationsync.SyncProtocolVersion {
		return storage.RemoteMutation{}, ErrInvalidRemoteData
	}
	if !vaultitem.IsSupportedItemType(operation.Envelope.RecordType) {
		return storage.RemoteMutation{}, ErrInvalidRemoteData
	}

	// The nested envelope's key ID is authenticated by OpenPayload's AAD. It is
	// parsed here only to select that authenticated key identity; no key bytes
	// leave the vault.
	var nested crypto.Envelope
	if err := json.Unmarshal(operation.Envelope.Ciphertext, &nested); err != nil || nested.Validate() != nil {
		return storage.RemoteMutation{}, ErrInvalidRemoteData
	}
	plaintext, err := replicationsync.OpenPayload(s.deps.Vault, operation.Envelope, nested.KeyID)
	if err != nil {
		return storage.RemoteMutation{}, ErrDecryptFailed
	}
	defer zeroBytes(plaintext)
	if len(plaintext) == 0 || len(plaintext) > maxRecordJSONSize {
		return storage.RemoteMutation{}, ErrInvalidRemoteData
	}

	mutation := storage.RemoteMutation{
		AccountID:     accountID,
		RecordID:      operation.Envelope.RecordID,
		RecordType:    operation.Envelope.RecordType,
		Operation:     operation.Operation,
		OperationID:   operation.Envelope.OperationID,
		EntityVersion: operation.Envelope.EntityVersion,
	}
	switch operation.Operation {
	case replicationsync.OperationCreate, replicationsync.OperationUpdate:
		normalized, innerPayload, err := normalizeSnapshot(plaintext, operation.Envelope)
		if err != nil {
			return storage.RemoteMutation{}, ErrInvalidRemoteData
		}
		// Authenticate the nested module payload before persistence. Structural
		// validation alone cannot detect a payload copied from another record or
		// an invalid authentication tag produced by a faulty authorized device.
		innerPlaintext, err := s.deps.Vault.OpenWithAD(*innerPayload, crypto.AssociatedData{
			RecordType: operation.Envelope.RecordType,
			RecordID:   operation.Envelope.RecordID,
			KeyID:      innerPayload.KeyID,
		})
		if err != nil {
			return storage.RemoteMutation{}, ErrDecryptFailed
		}
		zeroBytes(innerPlaintext)
		mutation.RecordJSON = normalized
	case replicationsync.OperationDelete:
		if !validTombstone(plaintext, mutation.RecordID, mutation.RecordType) {
			return storage.RemoteMutation{}, ErrInvalidRemoteData
		}
	default:
		return storage.RemoteMutation{}, ErrInvalidRemoteData
	}
	return mutation, nil
}

// snapshot is the complete cross-device representation of a vault item. It
// deliberately excludes local-only fields such as sync_state; those fields
// are derived by this device after the authenticated snapshot is accepted.
type snapshot struct {
	ID         string           `json:"id"`
	Type       string           `json:"type"`
	Metadata   json.RawMessage  `json:"metadata"`
	Payload    *crypto.Envelope `json:"payload,omitempty"`
	CreatedAt  time.Time        `json:"created_at"`
	UpdatedAt  time.Time        `json:"updated_at"`
	KeyVersion uint32           `json:"key_version"`
}

type localRecord struct {
	ID         string           `json:"id"`
	Type       string           `json:"type"`
	Metadata   json.RawMessage  `json:"metadata"`
	Payload    *crypto.Envelope `json:"payload,omitempty"`
	CreatedAt  time.Time        `json:"created_at"`
	UpdatedAt  time.Time        `json:"updated_at"`
	KeyVersion uint32           `json:"key_version"`
	SyncState  string           `json:"sync_state"`
}

func normalizeSnapshot(payload []byte, outer replicationsync.ReplicationEnvelope) ([]byte, *crypto.Envelope, error) {
	var remote snapshot
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&remote); err != nil {
		return nil, nil, ErrInvalidRemoteData
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, nil, ErrInvalidRemoteData
	}
	if remote.ID != outer.RecordID || remote.Type != outer.RecordType ||
		!json.Valid(remote.Metadata) || remote.CreatedAt.IsZero() || remote.UpdatedAt.IsZero() ||
		remote.UpdatedAt.Before(remote.CreatedAt) || remote.KeyVersion == 0 || remote.KeyVersion != outer.KeyVersion {
		return nil, nil, ErrInvalidRemoteData
	}
	if err := vaultitem.ValidateRecordSchema(remote.Type, remote.Metadata, remote.Payload); err != nil {
		return nil, nil, ErrInvalidRemoteData
	}

	normalized, err := json.Marshal(localRecord{
		ID:         remote.ID,
		Type:       remote.Type,
		Metadata:   append(json.RawMessage(nil), remote.Metadata...),
		Payload:    remote.Payload,
		CreatedAt:  remote.CreatedAt,
		UpdatedAt:  remote.UpdatedAt,
		KeyVersion: remote.KeyVersion,
		SyncState:  "synced",
	})
	if err != nil {
		return nil, nil, ErrInvalidRemoteData
	}
	return normalized, remote.Payload, nil
}

func validTombstone(payload []byte, recordID, recordType string) bool {
	var tombstone struct {
		RecordID   string `json:"record_id"`
		RecordType string `json:"record_type"`
		Deleted    bool   `json:"deleted"`
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&tombstone); err != nil {
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return false
	}
	return tombstone.Deleted && tombstone.RecordID == recordID && tombstone.RecordType == recordType
}

func zeroMutations(mutations []storage.RemoteMutation) {
	for i := range mutations {
		zeroBytes(mutations[i].RecordJSON)
		mutations[i].RecordJSON = nil
	}
}

func zeroBytes(value []byte) {
	for i := range value {
		value[i] = 0
	}
}

func nonNilContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func safeStageError(ctx context.Context, fallback error) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return fallback
}
