package database

import (
	"context"
	"errors"
	"sync"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

const queryTimeout = 30 * time.Second

// PoolManager owns one live connection per profile for the lifetime of the
// application, plus the SSH forward that connection may depend on. It is
// engine-agnostic: everything driver-specific lives behind Conn.
type PoolManager struct {
	mu      sync.Mutex
	conns   map[string]Conn
	tunnels Tunnel
	// forwards maps a profile to the forward opened for it, so the tunnel dies
	// with the connection rather than outliving it.
	forwards map[string]string
	// opening serialises opening and dropping a connection per PROFILE (#46).
	// One lock for all of them meant one SSH host that did not answer held
	// every other profile's first query behind it.
	opening map[string]*sync.Mutex
}

func NewPoolManager() *PoolManager {
	return &PoolManager{
		conns:    make(map[string]Conn),
		forwards: make(map[string]string),
		opening:  make(map[string]*sync.Mutex),
	}
}

func (m *PoolManager) profileLock(profileID string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	lock := m.opening[profileID]
	if lock == nil {
		lock = &sync.Mutex{}
		m.opening[profileID] = lock
	}
	return lock
}

// SetTunnel wires port forwarding. Without it a profile that asks for a tunnel
// fails rather than connecting directly — see ErrSSHProfileMissing.
func (m *PoolManager) SetTunnel(t Tunnel) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tunnels = t
}

// connectionSpec is everything needed to open one profile's connection.
type connectionSpec struct {
	profileID string
	cfg       ConnConfig
	// tunnel is nil when the profile connects directly.
	tunnel *TunnelSpec
}

func (m *PoolManager) Get(ctx context.Context, spec connectionSpec) (Conn, error) {
	lock := m.profileLock(spec.profileID)
	lock.Lock()
	defer lock.Unlock()

	m.mu.Lock()
	conn := m.conns[spec.profileID]
	tunnels := m.tunnels
	m.mu.Unlock()
	if conn != nil {
		return conn, nil
	}

	driver, err := driverFor(spec.cfg.Engine)
	if err != nil {
		return nil, err
	}
	// Bounded here, for every caller: opening a tunnel or a connection had no
	// deadline unless the caller happened to set one, and ListSchemas and
	// IndexSchema did not (#46).
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	cfg := spec.cfg
	var forwardID string
	if spec.tunnel != nil {
		if tunnels == nil {
			return nil, ErrSSHProfileMissing
		}
		handle, err := tunnels.Open(ctx, *spec.tunnel)
		if err != nil {
			return nil, err
		}
		forwardID = handle.ID
		// Connect to the local end, but keep verifying the certificate against
		// the host the profile actually names.
		cfg.TLSServerName = cfg.Host
		cfg.Host = handle.LocalHost
		cfg.Port = handle.LocalPort
	}

	conn, err = driver.Dial(ctx, cfg)
	if err != nil {
		if forwardID != "" {
			_ = tunnels.Close(forwardID)
		}
		return nil, err
	}
	m.mu.Lock()
	m.conns[spec.profileID] = conn
	if forwardID != "" {
		m.forwards[spec.profileID] = forwardID
	}
	m.mu.Unlock()
	return conn, nil
}

// Remove waits for an open in progress for the same profile, so a connection
// dialled for a profile being deleted is not cached after the delete.
func (m *PoolManager) Remove(profileID string) {
	lock := m.profileLock(profileID)
	lock.Lock()
	defer lock.Unlock()

	m.mu.Lock()
	conn := m.conns[profileID]
	delete(m.conns, profileID)
	forwardID := m.forwards[profileID]
	delete(m.forwards, profileID)
	tunnels := m.tunnels
	m.mu.Unlock()
	if conn != nil {
		conn.Close()
	}
	if forwardID != "" && tunnels != nil {
		_ = tunnels.Close(forwardID)
	}
}

func (m *PoolManager) Close() {
	m.mu.Lock()
	conns := m.conns
	forwards := m.forwards
	tunnels := m.tunnels
	m.conns = make(map[string]Conn)
	m.forwards = make(map[string]string)
	m.mu.Unlock()
	for _, conn := range conns {
		conn.Close()
	}
	if tunnels != nil {
		for _, forwardID := range forwards {
			_ = tunnels.Close(forwardID)
		}
	}
}

func queryContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, queryTimeout)
}

// answeredByServer is whether an error is the database's own reply, which
// means the connection under it is working.
func answeredByServer(err error) bool {
	var pgErr *pgconn.PgError
	var myErr *mysqldriver.MySQLError
	return errors.As(err, &pgErr) || errors.As(err, &myErr)
}

// openProbeTunnel and closeProbeTunnel serve connection tests for a profile
// that does not exist yet: the forward is opened and closed by the caller
// rather than being tied to a cached connection, because there is no profile
// id to tie it to.
func (m *PoolManager) openProbeTunnel(ctx context.Context, spec TunnelSpec) (TunnelHandle, error) {
	m.mu.Lock()
	tunnels := m.tunnels
	m.mu.Unlock()
	if tunnels == nil {
		return TunnelHandle{}, ErrSSHProfileMissing
	}
	return tunnels.Open(ctx, spec)
}

func (m *PoolManager) closeProbeTunnel(id string) {
	m.mu.Lock()
	tunnels := m.tunnels
	m.mu.Unlock()
	if tunnels != nil && id != "" {
		_ = tunnels.Close(id)
	}
}
