// Package syncclient coordinates one authenticated push/pull sync cycle.
//
// The service is deliberately runtime-only: it keeps the bearer-backed
// AuthSession and HTTP transport in memory, while persistence remains behind
// narrow local interfaces owned by the caller. The lifecycle (Connect,
// Disconnect, Status) lives in connect.go, the push/pull cycle in syncnow.go,
// and conflict listing in conflicts.go.
package syncclient

import (
	"context"
	"errors"
	"net/http"
	stdsync "sync"
	"time"

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
	ErrConflictsFailed     = errors.New("sync conflicts could not be listed")
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

// ConflictInfo is non-secret conflict metadata safe for UI/diagnostics.
type ConflictInfo struct {
	RecordID        string    `json:"record_id"`
	RecordType      string    `json:"record_type"`
	LocalChangeKey  string    `json:"local_change_key"`
	RemoteChangeKey string    `json:"remote_change_key"`
	DetectedAt      time.Time `json:"detected_at"`
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
