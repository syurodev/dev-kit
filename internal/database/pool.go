package database

import (
	"context"
	"sync"
	"time"
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
}

func NewPoolManager() *PoolManager {
	return &PoolManager{conns: make(map[string]Conn), forwards: make(map[string]string)}
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
	m.mu.Lock()
	defer m.mu.Unlock()
	if conn := m.conns[spec.profileID]; conn != nil {
		return conn, nil
	}

	driver, err := driverFor(spec.cfg.Engine)
	if err != nil {
		return nil, err
	}

	cfg := spec.cfg
	var forwardID string
	if spec.tunnel != nil {
		if m.tunnels == nil {
			return nil, ErrSSHProfileMissing
		}
		handle, err := m.tunnels.Open(ctx, *spec.tunnel)
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

	conn, err := driver.Dial(ctx, cfg)
	if err != nil {
		if forwardID != "" {
			_ = m.tunnels.Close(forwardID)
		}
		return nil, err
	}
	m.conns[spec.profileID] = conn
	if forwardID != "" {
		m.forwards[spec.profileID] = forwardID
	}
	return conn, nil
}

func (m *PoolManager) Remove(profileID string) {
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
