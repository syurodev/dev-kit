package database

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const queryTimeout = 30 * time.Second

// PoolManager owns PostgreSQL pools for the lifetime of the application. A
// profile gets one bounded pool, reused by connection tests and queries.
type PoolManager struct {
	mu      sync.Mutex
	pools   map[string]*pgxpool.Pool
	tunnels Tunnel
	// forwards maps a profile to the forward opened for it, so the tunnel dies
	// with the pool rather than outliving it.
	forwards map[string]string
}

func NewPoolManager() *PoolManager {
	return &PoolManager{pools: make(map[string]*pgxpool.Pool), forwards: make(map[string]string)}
}

// SetTunnel wires port forwarding. Without it a profile that asks for a tunnel
// fails rather than connecting directly — see ErrSSHProfileMissing.
func (m *PoolManager) SetTunnel(t Tunnel) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tunnels = t
}

// connectionSpec is everything needed to build one pool.
type connectionSpec struct {
	profileID string
	dsn       string
	readOnly  bool
	host      string
	// tunnel is nil when the profile connects directly.
	tunnel *TunnelSpec
}

func (m *PoolManager) Get(ctx context.Context, spec connectionSpec) (*pgxpool.Pool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if pool := m.pools[spec.profileID]; pool != nil {
		return pool, nil
	}

	dsn := spec.dsn
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
		dsn, err = rewriteDSNHost(dsn, handle.LocalHost, handle.LocalPort)
		if err != nil {
			_ = m.tunnels.Close(forwardID)
			return nil, err
		}
	}

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		if forwardID != "" {
			_ = m.tunnels.Close(forwardID)
		}
		return nil, err
	}
	// The DSN now points at loopback, but the server certificate was issued for
	// the real host. Without pinning ServerName, verify-full would fail against
	// 127.0.0.1 and the only way out would be weakening TLS — exactly what
	// ADR-007 forbids.
	if spec.tunnel != nil && config.ConnConfig.TLSConfig != nil && spec.host != "" {
		config.ConnConfig.TLSConfig.ServerName = spec.host
	}
	config.MaxConns = 4
	config.MinConns = 0
	config.MaxConnLifetime = 30 * time.Minute

	if spec.readOnly {
		config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
			_, err := conn.Exec(ctx, "SET default_transaction_read_only = on")
			return err
		}
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		if forwardID != "" {
			_ = m.tunnels.Close(forwardID)
		}
		return nil, err
	}
	m.pools[spec.profileID] = pool
	if forwardID != "" {
		m.forwards[spec.profileID] = forwardID
	}
	return pool, nil
}

// rewriteDSNHost repoints a DSN at the local end of a forward. Only the host
// and port change; credentials, database and sslmode are left untouched.
func rewriteDSNHost(dsn, host string, port int) (string, error) {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	parsed.Host = net.JoinHostPort(host, fmt.Sprintf("%d", port))
	return parsed.String(), nil
}

func (m *PoolManager) Remove(profileID string) {
	m.mu.Lock()
	pool := m.pools[profileID]
	delete(m.pools, profileID)
	forwardID := m.forwards[profileID]
	delete(m.forwards, profileID)
	tunnels := m.tunnels
	m.mu.Unlock()
	if pool != nil {
		pool.Close()
	}
	if forwardID != "" && tunnels != nil {
		_ = tunnels.Close(forwardID)
	}
}

func (m *PoolManager) Close() {
	m.mu.Lock()
	pools := m.pools
	forwards := m.forwards
	tunnels := m.tunnels
	m.pools = make(map[string]*pgxpool.Pool)
	m.forwards = make(map[string]string)
	m.mu.Unlock()
	for _, pool := range pools {
		pool.Close()
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
