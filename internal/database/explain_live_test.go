package database

import (
	"context"
	"os"
	"testing"
	"time"
)

/*
EXPLAIN against real engines.

Live, because what broke was a conversation with a server, not a function: a
MariaDB server saved in its profile as "MySQL" was sent `EXPLAIN ANALYZE`,
which MariaDB does not have, and answered with error 1064. Nothing short of a
MariaDB on the other end of the wire can say whether that is fixed.

	docker run -d --name devkit-mariadb-test -p 3308:3306 \
	  -e MARIADB_USER=devkit -e MARIADB_PASSWORD=devkitpass \
	  -e MARIADB_DATABASE=devkitdb -e MARIADB_ROOT_PASSWORD=devkitroot mariadb:11

	DEVKIT_PG_TEST=1 DEVKIT_MYSQL_TEST=1 \
	DEVKIT_MARIADB_ADDR=127.0.0.1:3308 DEVKIT_MARIADB_USER=devkit \
	DEVKIT_MARIADB_PASSWORD=devkitpass DEVKIT_MARIADB_DB=devkitdb \
	  go test ./internal/database/ -run 'Explain|MariaDB' -v
*/

// Every engine has information_schema, so no fixture has to exist first.
const explainProbe = "SELECT table_name FROM information_schema.tables WHERE table_schema = 'information_schema'"

func explainContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func plannerOf(t *testing.T, conn Conn) QueryPlanner {
	t.Helper()
	planner, ok := conn.(QueryPlanner)
	if !ok {
		t.Fatalf("%T does not plan queries", conn)
	}
	return planner
}

/*
dialMariaDBLabelledMySQL is the profile the bug came from: a MariaDB server,
and the engine dropdown left on MySQL — the natural pick, since the port, the
driver and the protocol are all MySQL's.

The server comes from mariaDBConfig, the same variables the rest of the
MariaDB live suite reads. Only the label is changed, because the label is the
bug.
*/
func dialMariaDBLabelledMySQL(t *testing.T) Conn {
	t.Helper()
	cfg := mariaDBConfig(t)
	cfg.Engine = EngineMySQL
	conn, err := mysqlDriver{engine: EngineMySQL}.Dial(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Dial mariadb: %v", err)
	}
	t.Cleanup(conn.Close)
	return conn
}

func TestExplainAnalyzeOnMariaDBSavedAsMySQL(t *testing.T) {
	ctx := explainContext(t)
	conn := dialMariaDBLabelledMySQL(t)

	if !conn.(*mysqlConn).isMariaDB(ctx) {
		t.Fatal("the server says MariaDB in VERSION() and the connection did not hear it")
	}

	plan, err := plannerOf(t, conn).ExplainAnalyze(ctx, explainProbe)
	if err != nil {
		t.Fatalf("ExplainAnalyze on MariaDB labelled MySQL: %v", err)
	}
	if !plan.Analyzed {
		t.Error("an analyzed plan came back marked unanalyzed")
	}
	// MariaDB's ANALYZE FORMAT=JSON is the one that fills the spine; MySQL's
	// text tree does not. Nodes here is proof the MariaDB syntax went out.
	if len(plan.Nodes) == 0 {
		t.Fatalf("no nodes — the JSON path was not taken; raw: %.200s", plan.Raw)
	}
}

func TestExplainOnMariaDBSavedAsMySQL(t *testing.T) {
	ctx := explainContext(t)
	plan, err := plannerOf(t, dialMariaDBLabelledMySQL(t)).Explain(ctx, explainProbe)
	if err != nil {
		t.Fatalf("Explain on MariaDB: %v", err)
	}
	if len(plan.Nodes) == 0 {
		t.Fatalf("no plan nodes; raw: %.200s", plan.Raw)
	}
}

/*
MySQL must keep its own word for it. The fix asks the server what it is, and
a MySQL server must still be told `EXPLAIN ANALYZE` — which answers in a
text tree, so the plan comes back with Raw and no nodes, on purpose.
*/
func TestExplainAnalyzeOnMySQLKeepsMySQLSyntax(t *testing.T) {
	if os.Getenv("DEVKIT_MYSQL_TEST") != "1" {
		t.Skip("set DEVKIT_MYSQL_TEST=1 to run the MySQL explain test")
	}
	ctx := explainContext(t)
	conn := dialMySQL(t)
	t.Cleanup(conn.Close)

	if conn.(*mysqlConn).isMariaDB(ctx) {
		t.Fatal("a MySQL 8 server was read as MariaDB")
	}
	plan, err := plannerOf(t, conn).ExplainAnalyze(ctx, explainProbe)
	if err != nil {
		t.Fatalf("ExplainAnalyze on MySQL: %v", err)
	}
	if plan.Raw == "" {
		t.Fatal("MySQL's text tree did not come back in Raw")
	}
	if len(plan.Nodes) != 0 {
		t.Fatalf("MySQL's text tree was half-parsed into %d nodes", len(plan.Nodes))
	}
}

func TestExplainAndAnalyzeOnPostgres(t *testing.T) {
	if os.Getenv("DEVKIT_PG_TEST") != "1" {
		t.Skip("set DEVKIT_PG_TEST=1 to run the PostgreSQL explain test")
	}
	ctx := explainContext(t)
	conn := dialPostgres(t)
	t.Cleanup(conn.Close)
	planner := plannerOf(t, conn)

	plan, err := planner.Explain(ctx, explainProbe)
	if err != nil {
		t.Fatalf("Explain on Postgres: %v", err)
	}
	if len(plan.Nodes) == 0 || plan.Analyzed {
		t.Fatalf("Explain: want nodes and not analyzed, got %d nodes analyzed=%v", len(plan.Nodes), plan.Analyzed)
	}

	analyzed, err := planner.ExplainAnalyze(ctx, explainProbe)
	if err != nil {
		t.Fatalf("ExplainAnalyze on Postgres: %v", err)
	}
	if len(analyzed.Nodes) == 0 || !analyzed.Analyzed {
		t.Fatalf("ExplainAnalyze: want nodes and analyzed, got %d nodes analyzed=%v", len(analyzed.Nodes), analyzed.Analyzed)
	}
}
