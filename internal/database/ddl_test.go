package database

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestQuoteMySQLIdentifier(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"ba_user", "`ba_user`"},
		// A reserved word is the everyday case, not an exotic one.
		{"order", "`order`"},
		{"a name with spaces", "`a name with spaces`"},
		// The one that matters: a backtick inside the name must not close the
		// identifier and turn the rest into a second statement.
		{"we`ird", "`we``ird`"},
	} {
		if got := quoteMySQLIdentifier(tc.in); got != tc.want {
			t.Errorf("quoteMySQLIdentifier(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

// A driver that does not implement DDLReader says so by name. Both shipped
// drivers do; this is the path an engine added later takes before it does.
func TestDDLIsRefusedForAnEngineThatCannotShowIt(t *testing.T) {
	svc := newService(t)
	id := plant(t, svc, &recordingConn{transactor: true})

	_, err := svc.TableDDL(context.Background(), id, "chiro", "ba_user")
	if !errors.Is(err, ErrEngineHasNoDDL) {
		t.Fatalf("TableDDL = %v, want ErrEngineHasNoDDL", err)
	}
}

func TestDDLNeedsASchemaAndATable(t *testing.T) {
	svc := newService(t)
	id := plant(t, svc, &recordingConn{transactor: true})

	for _, tc := range []struct{ schema, table string }{{"", "t"}, {"s", ""}} {
		if _, err := svc.TableDDL(context.Background(), id, tc.schema, tc.table); err == nil {
			t.Fatalf("TableDDL(%q, %q) error = nil, want a validation error", tc.schema, tc.table)
		}
	}
}

// A conn that answers DDL, to prove the service routes to the optional
// interface and hands back what the engine said, verbatim.
type ddlConn struct {
	recordingConn
	asked string
	ddl   string
}

func (c *ddlConn) TableDDL(_ context.Context, schema, table string) (string, error) {
	c.asked = schema + "." + table
	return c.ddl, nil
}

func TestDDLComesBackVerbatim(t *testing.T) {
	svc := newService(t)
	const statement = "CREATE TABLE `ba_user` (\n  `id` int NOT NULL AUTO_INCREMENT\n) ENGINE=InnoDB"
	conn := &ddlConn{ddl: statement}
	id := plant(t, svc, conn)

	got, err := svc.TableDDL(context.Background(), id, "chiro", "ba_user")
	if err != nil {
		t.Fatalf("TableDDL: %v", err)
	}
	if got != statement {
		t.Fatalf("TableDDL = %q, want the engine's own text", got)
	}
	if conn.asked != "chiro.ba_user" {
		t.Fatalf("asked for %q, want chiro.ba_user", conn.asked)
	}
	// Not reformatted, not re-indented: it is pasted into migrations.
	if !strings.Contains(got, "ENGINE=InnoDB") {
		t.Fatal("the engine clause was lost on the way back")
	}
}
