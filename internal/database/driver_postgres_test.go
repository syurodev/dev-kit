package database

import (
	"context"
	"testing"
	"time"
)

// Pool shape is the Postgres driver's business, not the pool manager's — the
// manager is engine-agnostic and cannot assert pgx specifics.
func TestPostgresDriverBoundsThePoolAndAppliesSafeMode(t *testing.T) {
	base := ConnConfig{
		Engine: EnginePostgres, Host: "127.0.0.1", Port: 1,
		Database: "devkit", Username: "user", Password: "pass", SSLMode: "disable",
	}

	open, err := postgresDriver{}.Dial(context.Background(), base)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(open.Close)
	config := open.(*postgresConn).pool.Config()
	if config.MaxConns != 4 || config.MinConns != 0 || config.MaxConnLifetime != 30*time.Minute {
		t.Fatalf("pool bounds = %d/%d/%s", config.MaxConns, config.MinConns, config.MaxConnLifetime)
	}
	if config.AfterConnect != nil {
		t.Fatal("AfterConnect must be nil unless the profile is read-only")
	}

	readOnly := base
	readOnly.ReadOnly = true
	ro, err := postgresDriver{}.Dial(context.Background(), readOnly)
	if err != nil {
		t.Fatalf("Dial read-only: %v", err)
	}
	t.Cleanup(ro.Close)
	if ro.(*postgresConn).pool.Config().AfterConnect == nil {
		t.Fatal("safe mode must install AfterConnect so every pooled connection carries it")
	}
}

func TestPostgresDriverPinsCertificateNameThroughATunnel(t *testing.T) {
	cfg := ConnConfig{
		Engine: EnginePostgres, Host: "127.0.0.1", Port: 54413,
		Database: "app", Username: "devkit", Password: "pw",
		SSLMode: "verify-full", TLSServerName: "db.internal",
	}
	conn, err := postgresDriver{}.Dial(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(conn.Close)

	tlsConfig := conn.(*postgresConn).pool.Config().ConnConfig.TLSConfig
	if tlsConfig == nil {
		t.Fatal("verify-full produced no TLS config")
	}
	// Without this the certificate for db.internal is checked against
	// 127.0.0.1 and the only escape would be weakening sslmode.
	if tlsConfig.ServerName != "db.internal" {
		t.Fatalf("ServerName = %q, want the profile's real host", tlsConfig.ServerName)
	}
	if tlsConfig.InsecureSkipVerify {
		t.Fatal("tunnelling must not disable verification")
	}
}
