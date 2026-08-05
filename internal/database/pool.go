package database

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const queryTimeout = 30 * time.Second

// PoolManager owns PostgreSQL pools for the lifetime of the application. A
// profile gets one bounded pool, reused by connection tests and queries.
type PoolManager struct {
	mu    sync.Mutex
	pools map[string]*pgxpool.Pool
}

func NewPoolManager() *PoolManager {
	return &PoolManager{pools: make(map[string]*pgxpool.Pool)}
}

func (m *PoolManager) Get(ctx context.Context, profileID, dsn string) (*pgxpool.Pool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if pool := m.pools[profileID]; pool != nil {
		return pool, nil
	}

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	config.MaxConns = 4
	config.MinConns = 0
	config.MaxConnLifetime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	m.pools[profileID] = pool
	return pool, nil
}

func (m *PoolManager) Remove(profileID string) {
	m.mu.Lock()
	pool := m.pools[profileID]
	delete(m.pools, profileID)
	m.mu.Unlock()
	if pool != nil {
		pool.Close()
	}
}

func (m *PoolManager) Close() {
	m.mu.Lock()
	pools := m.pools
	m.pools = make(map[string]*pgxpool.Pool)
	m.mu.Unlock()
	for _, pool := range pools {
		pool.Close()
	}
}

func queryContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, queryTimeout)
}
