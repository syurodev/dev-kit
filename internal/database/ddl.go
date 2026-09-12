package database

import (
	"context"
	"errors"
	"fmt"
)

// ErrEngineHasNoDDL means this engine's driver cannot hand back a table's
// definition, the same way ErrEngineHasNoSessions and ErrEngineHasNoQueryPlan
// mean the engine has no sessions or no plan.
var ErrEngineHasNoDDL = errors.New("this engine cannot show a table's definition yet")

// DDLReader is optional on Conn, like SessionInspector and QueryPlanner.
//
// Optional because the engines differ by more than a dialect here. MySQL and
// SQLite each keep the statement that made the table and hand it back whole —
// SHOW CREATE TABLE, sqlite_master.sql. Postgres keeps no such text: its DDL
// has to be REBUILT from the catalogues, column by column, constraint by
// constraint, and a reconstruction that is subtly wrong is worse than an
// honest "not yet" — it is a statement someone will paste into a migration.
type DDLReader interface {
	TableDDL(ctx context.Context, schema, table string) (string, error)
}

func (s *Service) ddlReader(ctx context.Context, id string) (DDLReader, error) {
	conn, err := s.connect(ctx, id)
	if err != nil {
		return nil, err
	}
	reader, ok := conn.(DDLReader)
	if !ok {
		return nil, ErrEngineHasNoDDL
	}
	return reader, nil
}

// TableDDL returns the statement that defines a table.
func (s *Service) TableDDL(ctx context.Context, id, schema, table string) (string, error) {
	if schema == "" || table == "" {
		return "", fmt.Errorf("schema and table are required")
	}
	ctx, cancel := queryContext(ctx)
	defer cancel()

	reader, err := s.ddlReader(ctx, id)
	if err != nil {
		return "", err
	}
	return reader.TableDDL(ctx, schema, table)
}
