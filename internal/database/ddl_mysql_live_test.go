package database

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
)

/*
MySQL DDL against a real server.

	docker run -d --name devkit-mysql-test -e MYSQL_ROOT_PASSWORD=devkitpass \
	  -e MYSQL_DATABASE=devkitdb -e MYSQL_USER=devkit -e MYSQL_PASSWORD=devkitpass \
	  -p 3307:3306 mysql:8
	DEVKIT_MYSQL_TEST=1 go test ./internal/database/ -run TestMySQLTableDDL

One statement stands behind this feature on MySQL, and it is the one thing a
fake cannot check: that SHOW CREATE TABLE's result is scanned out of the right
column, for a table AND for a view, whose column names differ.
*/
func TestMySQLTableDDL(t *testing.T) {
	if os.Getenv("DEVKIT_MYSQL_TEST") != "1" {
		t.Skip("set DEVKIT_MYSQL_TEST=1 to run the MySQL DDL test")
	}
	port, _ := strconv.Atoi(envOr("DEVKIT_MYSQL_PORT", "3307"))
	ctx := context.Background()
	conn, err := mysqlDriver{engine: EngineMySQL}.Dial(ctx, ConnConfig{
		Engine:   EngineMySQL,
		Host:     envOr("DEVKIT_MYSQL_HOST", "127.0.0.1"),
		Port:     port,
		Database: envOr("DEVKIT_MYSQL_DB", "devkitdb"),
		Username: envOr("DEVKIT_MYSQL_USER", "devkit"),
		Password: envOr("DEVKIT_MYSQL_PASSWORD", "devkitpass"),
		SSLMode:  "disable",
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	for _, statement := range []string{
		"DROP VIEW IF EXISTS ba_open_users",
		"DROP TABLE IF EXISTS ba_user",
		"CREATE TABLE ba_user (\n" +
			"  id int NOT NULL AUTO_INCREMENT,\n" +
			"  name varchar(255) NOT NULL,\n" +
			"  email varchar(255) DEFAULT NULL,\n" +
			"  `order` int DEFAULT NULL,\n" +
			"  created_at timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,\n" +
			"  PRIMARY KEY (id),\n" +
			"  UNIQUE KEY ba_user_email_unique (email),\n" +
			"  KEY ba_user_created_at_idx (created_at)\n" +
			") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci",
		"CREATE VIEW ba_open_users AS SELECT id, name FROM ba_user WHERE email IS NOT NULL",
	} {
		rows, err := conn.Query(ctx, statement)
		if err != nil {
			t.Fatalf("%s\n  -> %v", statement, err)
		}
		rows.Close()
	}

	reader, ok := conn.(DDLReader)
	if !ok {
		t.Fatal("the mysql driver does not implement DDLReader")
	}

	ddl, err := reader.TableDDL(ctx, "devkitdb", "ba_user")
	if err != nil {
		t.Fatalf("TableDDL: %v", err)
	}
	t.Logf("\n%s", ddl)
	for _, want := range []string{
		"CREATE TABLE `ba_user`",
		"AUTO_INCREMENT",
		"PRIMARY KEY (`id`)",
		"UNIQUE KEY `ba_user_email_unique`",
		// A reserved word must come back quoted or the statement will not run.
		"`order` int",
		"ENGINE=InnoDB",
		"utf8mb4_unicode_ci",
	} {
		if !strings.Contains(ddl, want) {
			t.Errorf("DDL is missing %q", want)
		}
	}

	// A view's SHOW CREATE returns four columns with different NAMES — View,
	// Create View, character_set_client, collation_connection — and the
	// statement is still the second. Scanning by name would have worked for
	// one of these two and not the other.
	view, err := reader.TableDDL(ctx, "devkitdb", "ba_open_users")
	if err != nil {
		t.Fatalf("TableDDL(view): %v", err)
	}
	if !strings.Contains(view, "CREATE") || !strings.Contains(view, "ba_open_users") {
		t.Fatalf("view DDL = %q, want its CREATE VIEW", view)
	}
}
