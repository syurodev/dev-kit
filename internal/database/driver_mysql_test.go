package database

import (
	"net/url"
	"strings"
	"testing"
)

func mysqlParams(t *testing.T, cfg ConnConfig) url.Values {
	t.Helper()
	dsn, tlsName, err := mysqlDSN(cfg)
	if err != nil {
		t.Fatalf("mysqlDSN: %v", err)
	}
	t.Cleanup(func() { releaseMySQLTLS(tlsName) })
	query := dsn[strings.Index(dsn, "?")+1:]
	values, err := url.ParseQuery(query)
	if err != nil {
		t.Fatalf("parse dsn params: %v", err)
	}
	return values
}

func baseMySQL() ConnConfig {
	return ConnConfig{
		Engine: EngineMySQL, Host: "db.internal", Port: 3306,
		Database: "billing", Username: "devkit", Password: "pw", SSLMode: "verify-full",
	}
}

func TestMySQLSafeModeTravelsOnEveryConnection(t *testing.T) {
	off := mysqlParams(t, baseMySQL())
	if off.Has("transaction_read_only") {
		t.Fatal("safe mode is off but the DSN still asks for a read-only session")
	}

	cfg := baseMySQL()
	cfg.ReadOnly = true
	on := mysqlParams(t, cfg)
	// The driver turns unknown parameters into session system variables, which
	// is how MySQL gets the per-connection guarantee pgx gets from AfterConnect.
	if on.Get("transaction_read_only") != "1" {
		t.Fatalf("read-only DSN params = %v, want transaction_read_only=1", on)
	}
}

func TestMySQLPinsCertificateNameThroughATunnel(t *testing.T) {
	cfg := baseMySQL()
	cfg.Host = "127.0.0.1"
	cfg.Port = 54413
	cfg.TLSServerName = "db.internal"

	dsn, tlsName, err := mysqlDSN(cfg)
	if err != nil {
		t.Fatalf("mysqlDSN: %v", err)
	}
	t.Cleanup(func() { releaseMySQLTLS(tlsName) })

	// A named config is registered precisely because go-sql-driver has no
	// system-CA mode with a hostname override, unlike pgx.
	if tlsName == "" || tlsName == "true" || tlsName == "skip-verify" {
		t.Fatalf("tls name = %q, want a registered config carrying ServerName", tlsName)
	}
	if !strings.Contains(dsn, "tls="+tlsName) {
		t.Fatalf("dsn does not reference the registered TLS config: %s", dsn)
	}
}

func TestMySQLSSLModeMapping(t *testing.T) {
	for mode, want := range map[string]string{"disable": "", "require": "skip-verify", "verify-full": "true"} {
		cfg := baseMySQL()
		cfg.SSLMode = mode
		_, tlsName, err := mysqlDSN(cfg)
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		releaseMySQLTLS(tlsName)
		if tlsName != want {
			t.Fatalf("sslmode %s -> tls %q, want %q", mode, tlsName, want)
		}
	}
}

func TestMariaDBSharesTheMySQLDriver(t *testing.T) {
	maria, err := driverFor(EngineMariaDB)
	if err != nil {
		t.Fatalf("driverFor: %v", err)
	}
	my, err := driverFor(EngineMySQL)
	if err != nil {
		t.Fatalf("driverFor: %v", err)
	}
	if _, ok := maria.(mysqlDriver); !ok {
		t.Fatalf("mariadb driver = %T, want the mysql driver", maria)
	}
	// Same driver, but the engine stays nameable so a future behaviour
	// difference has somewhere to live.
	if maria.(mysqlDriver).engine == my.(mysqlDriver).engine {
		t.Fatal("mariadb and mysql collapsed into one engine name")
	}
}

func TestEnginesDifferInPortsButSQLIsNeverRewritten(t *testing.T) {
	if DefaultPort(EnginePostgres) != 5432 || DefaultPort(EngineMariaDB) != 3306 {
		t.Fatal("default ports do not reflect the engines")
	}
	// There is deliberately no translation helper anywhere: DevKit shows the
	// hint and lets the driver reject wrong syntax with its own error.
	if _, err := driverFor("oracle"); err == nil {
		t.Fatal("an unknown engine must be refused, not guessed at")
	}
}

func TestLegacyProfilesWithoutAnEngineAreStillPostgres(t *testing.T) {
	d, err := driverFor("")
	if err != nil {
		t.Fatalf("driverFor: %v", err)
	}
	if _, ok := d.(postgresDriver); !ok {
		t.Fatalf("empty engine resolved to %T, want postgres", d)
	}
}
