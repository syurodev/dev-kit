package database

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
)

// The multi-engine acceptance criteria ask for a real MariaDB, reached through
// the same `mysql` driver, with no MariaDB-specific code path. That cannot be
// asserted against a fake, so this test only runs when pointed at a server:
//
//	docker run -d --name devkit-mariadb-test -p 3308:3306 \
//	  -e MARIADB_USER=devkit -e MARIADB_PASSWORD=devkitpass \
//	  -e MARIADB_DATABASE=devkitdb -e MARIADB_ROOT_PASSWORD=devkitroot mariadb:11
//
//	DEVKIT_MARIADB_ADDR=127.0.0.1:3308 DEVKIT_MARIADB_USER=devkit \
//	DEVKIT_MARIADB_PASSWORD=devkitpass DEVKIT_MARIADB_DB=devkitdb go test ./internal/database/
//
// The suite reads a fixture it does not create. A fresh container fails three
// tests on "table doesn't exist" until this is loaded into the database:
//
//	CREATE TABLE customers (id INT PRIMARY KEY AUTO_INCREMENT,
//	  email VARCHAR(255) NOT NULL, UNIQUE KEY uq_email (email)) ENGINE=InnoDB;
//	CREATE TABLE orders (id INT PRIMARY KEY AUTO_INCREMENT,
//	  customer_id INT NOT NULL, total DECIMAL(10,2) NOT NULL DEFAULT 0,
//	  note VARCHAR(255) NULL, KEY fk_o_c (customer_id),
//	  FOREIGN KEY (customer_id) REFERENCES customers (id)) ENGINE=InnoDB;
//	CREATE TABLE profiles (id INT PRIMARY KEY AUTO_INCREMENT,
//	  customer_id INT NOT NULL, UNIQUE KEY (customer_id),
//	  FOREIGN KEY (customer_id) REFERENCES customers (id)) ENGINE=InnoDB;
//	INSERT INTO customers (id, email) VALUES (1, 'a@example.com'), (2, 'b@example.com');
//	INSERT INTO orders (id, customer_id, total) VALUES (1, 1, 10), (2, 1, 20), (3, 2, 30);
//	INSERT INTO profiles (id, customer_id) VALUES (1, 1), (2, 2);
//
// Two customers and a non-unique orders.customer_id beside a unique
// profiles.customer_id are what the tests assert; the rollback test inserts
// into orders.note.
func mariaDBConfig(t *testing.T) ConnConfig {
	t.Helper()
	addr := os.Getenv("DEVKIT_MARIADB_ADDR")
	if addr == "" {
		t.Skip("DEVKIT_MARIADB_ADDR not set; skipping the live MariaDB check")
	}
	host, portText, found := strings.Cut(addr, ":")
	if !found {
		t.Fatalf("DEVKIT_MARIADB_ADDR must be host:port, got %q", addr)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("bad port in DEVKIT_MARIADB_ADDR: %v", err)
	}
	return ConnConfig{
		Engine:   EngineMariaDB,
		Host:     host,
		Port:     port,
		Database: os.Getenv("DEVKIT_MARIADB_DB"),
		Username: os.Getenv("DEVKIT_MARIADB_USER"),
		Password: os.Getenv("DEVKIT_MARIADB_PASSWORD"),
		SSLMode:  "disable",
	}
}

func TestMariaDBSpeaksEveryConnMethodThroughTheMySQLDriver(t *testing.T) {
	cfg := mariaDBConfig(t)
	ctx := context.Background()

	driver, err := driverFor(EngineMariaDB)
	if err != nil {
		t.Fatalf("driverFor(mariadb): %v", err)
	}
	if _, ok := driver.(mysqlDriver); !ok {
		t.Fatalf("mariadb must go through the mysql driver, got %T", driver)
	}

	conn, err := driver.Dial(ctx, cfg)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if err := conn.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}

	schemas, err := conn.ListSchemas(ctx)
	if err != nil {
		t.Fatalf("list schemas: %v", err)
	}
	if len(schemas) == 0 {
		t.Fatal("no schemas reported")
	}

	tables, err := conn.ListTables(ctx, cfg.Database)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	names := map[string]bool{}
	for _, tbl := range tables {
		names[tbl.Name] = true
	}
	for _, want := range []string{"customers", "orders", "profiles"} {
		if !names[want] {
			t.Fatalf("table %q missing from %v", want, names)
		}
	}

	columns, err := conn.ListColumns(ctx, cfg.Database, "orders")
	if err != nil {
		t.Fatalf("list columns: %v", err)
	}
	byName := map[string]ColumnInfo{}
	for _, c := range columns {
		byName[c.Name] = c
	}
	if byName["customer_id"].Nullable {
		t.Fatal("customer_id is NOT NULL in the schema but came back nullable")
	}
	if !byName["note"].Nullable {
		t.Fatal("note is nullable in the schema but came back NOT NULL")
	}

	fks, err := conn.ListForeignKeys(ctx, cfg.Database)
	if err != nil {
		t.Fatalf("list foreign keys: %v", err)
	}
	var foundFK bool
	for _, fk := range fks {
		if fk.Table == "orders" && fk.Column == "customer_id" &&
			fk.ReferencedTable == "customers" && fk.ReferencedColumn == "id" {
			foundFK = true
		}
	}
	if !foundFK {
		t.Fatalf("declared foreign key orders.customer_id -> customers.id missing from %v", fks)
	}

	lister, ok := conn.(IndexLister)
	if !ok {
		t.Fatal("the mysql driver must implement IndexLister")
	}
	indexes, err := lister.ListIndexes(ctx, cfg.Database)
	if err != nil {
		t.Fatalf("list indexes: %v", err)
	}
	// profiles.customer_id is unique and orders.customer_id is not — the exact
	// difference the ERD reads as one-to-one versus one-to-many.
	var profilesUnique, ordersUnique bool
	for _, idx := range indexes {
		if len(idx.Columns) != 1 || idx.Columns[0] != "customer_id" || !idx.Unique {
			continue
		}
		switch idx.Table {
		case "profiles":
			profilesUnique = true
		case "orders":
			ordersUnique = true
		}
	}
	if !profilesUnique {
		t.Fatal("unique index on profiles.customer_id not reported")
	}
	if ordersUnique {
		t.Fatal("orders.customer_id reported unique; it is not")
	}

	rows, err := conn.Query(ctx, "SELECT id, email FROM customers ORDER BY id")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	var count int
	for rows.Next() {
		count++
	}
	if count != 2 {
		t.Fatalf("want 2 seeded customers, got %d", count)
	}
}

func TestMariaDBProducesARealQueryPlan(t *testing.T) {
	cfg := mariaDBConfig(t)
	ctx := context.Background()

	driver, _ := driverFor(EngineMariaDB)
	conn, err := driver.Dial(ctx, cfg)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	planner, ok := conn.(QueryPlanner)
	if !ok {
		t.Fatal("the mysql driver must implement QueryPlanner")
	}

	const stmt = "SELECT o.id FROM orders o JOIN customers c ON c.id = o.customer_id"

	plan, err := planner.Explain(ctx, stmt)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	if plan.Analyzed {
		t.Fatal("plain EXPLAIN must not claim the statement ran")
	}
	if len(plan.Nodes) == 0 {
		t.Fatalf("explain produced no spine: %q", plan.Raw)
	}

	analyzed, err := planner.ExplainAnalyze(ctx, stmt)
	if err != nil {
		t.Fatalf("explain analyze: %v", err)
	}
	if !analyzed.Analyzed {
		t.Fatal("analyzed flag missing")
	}
	// MariaDB is the engine that can fill the spine with real numbers; that is
	// the whole reason ANALYZE FORMAT=JSON is used here instead of the text
	// tree MySQL returns.
	var sawActual bool
	var walk func(nodes []PlanNode)
	walk = func(nodes []PlanNode) {
		for _, n := range nodes {
			if n.ActualRows > 0 {
				sawActual = true
			}
			walk(n.Children)
		}
	}
	walk(analyzed.Nodes)
	if !sawActual {
		t.Fatalf("no actual row counts in an analyzed plan: %q", analyzed.Raw)
	}
}

// The interface promises the statement is rolled back. Nothing else in the
// suite checks that promise against a real server.
func TestMariaDBExplainAnalyzeRollsBackItsStatement(t *testing.T) {
	cfg := mariaDBConfig(t)
	ctx := context.Background()

	driver, _ := driverFor(EngineMariaDB)
	conn, err := driver.Dial(ctx, cfg)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	/*
		Rows of orders, counted by walking them.

		This used to walk the rows of `SELECT COUNT(*) FROM orders` — which is
		always exactly one row, whatever the table holds — so before and after
		were both 1 and the test could not fail. An INSERT that survived rollback
		passed. Walking the ids counts the table instead of counting the count.
	*/
	count := func() int {
		rows, err := conn.Query(ctx, "SELECT id FROM orders")
		if err != nil {
			t.Fatalf("count: %v", err)
		}
		defer rows.Close()
		n := 0
		for rows.Next() {
			n++
		}
		return n
	}

	before := count()
	if _, err := conn.(QueryPlanner).ExplainAnalyze(ctx,
		"INSERT INTO orders (customer_id, note) VALUES (1, 'analyzed')"); err != nil {
		t.Fatalf("explain analyze insert: %v", err)
	}
	if after := count(); after != before {
		t.Fatalf("the analyzed INSERT survived rollback: %d rows before, %d after", before, after)
	}
}
