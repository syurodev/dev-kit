package bridge

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"

	"devkit/internal/database"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func (s *AppService) DBListProfiles() ([]database.ProfileSummary, error) {
	result, err := s.call(context.Background(), "builtin.database", "db:query", "profile:list", map[string]any{
		"action": "list_profiles",
	}, func(context.Context) (any, error) {
		return s.container.Database.ListProfiles()
	})
	if err != nil {
		return nil, err
	}
	return result.([]database.ProfileSummary), nil
}

func (s *AppService) DBCreateProfile(input database.CreateProfileInput) (database.ProfileSummary, error) {
	result, err := s.call(context.Background(), "builtin.database", "db:query", "profile:new", map[string]any{
		"action": "create_profile",
	}, func(context.Context) (any, error) {
		return s.container.Database.CreateProfile(input)
	})
	if err != nil {
		return database.ProfileSummary{}, err
	}
	return result.(database.ProfileSummary), nil
}

func (s *AppService) DBDeleteProfile(id string) error {
	opaqueID := recordID(id)
	_, err := s.call(context.Background(), "builtin.database", "db:query", "profile:"+opaqueID, map[string]any{
		"action":     "delete_profile",
		"profile_id": opaqueID,
	}, func(context.Context) (any, error) {
		return nil, s.container.Database.DeleteProfile(id)
	})
	return err
}

// DBTestNewConnection dials a profile that has not been saved yet, so the user
// finds out before committing rather than after. Audit records the engine and
// host; the password arrives as a parameter and never reaches the trail.
func (s *AppService) DBTestNewConnection(ctx context.Context, input database.CreateProfileInput) error {
	_, err := s.call(ctx, "builtin.database", "db:query", "profile:new", map[string]any{
		"action": "test_new_connection",
		"engine": input.Engine,
		"host":   input.Host,
	}, func(callCtx context.Context) (any, error) {
		return nil, s.container.Database.TestNewConnection(callCtx, input)
	})
	return err
}

func (s *AppService) DBTestConnection(ctx context.Context, id string) error {
	opaqueID := recordID(id)
	_, err := s.call(ctx, "builtin.database", "db:query", "profile:"+opaqueID, map[string]any{
		"action":     "test_connection",
		"profile_id": opaqueID,
	}, func(callCtx context.Context) (any, error) {
		return nil, s.container.Database.TestConnection(callCtx, id)
	})
	return err
}

// DBQuery runs one statement. maxRows is the caller's row cap — the table
// browser's page size — and zero means the service default.
//
// sessionID names the console the statement comes from, so it runs on that
// console's own connection and what it SETs is there for the next run (#48).
// Empty for a table tab's page, which has no session to keep.
func (s *AppService) DBQuery(ctx context.Context, id string, sql string, maxRows int, sessionID string) (database.QueryResult, error) {
	opaqueID := recordID(id)
	result, err := s.call(ctx, "builtin.database", "db:query", "profile:"+opaqueID, map[string]any{
		"action":     "query",
		"profile_id": opaqueID,
	}, func(callCtx context.Context) (any, error) {
		return s.container.Database.QueryInSession(callCtx, id, sessionID, sql, maxRows)
	})
	if err != nil {
		return database.QueryResult{}, err
	}
	return result.(database.QueryResult), nil
}

// DBCloseSession gives back a console's connection — the console closed — or,
// with an empty sessionID, every console connection of the profile, when the
// UI switches to another one.
func (s *AppService) DBCloseSession(ctx context.Context, id string, sessionID string) error {
	opaqueID := recordID(id)
	_, err := s.call(ctx, "builtin.database", "db:query", "profile:"+opaqueID, map[string]any{
		"action":     "close_session",
		"profile_id": opaqueID,
	}, func(context.Context) (any, error) {
		s.container.Database.CloseSession(id, sessionID)
		return nil, nil
	})
	return err
}

// DBSetTxMode, DBCommitTx, DBRollbackTx and DBTxOpen are manual-commit mode.
//
// Through the same capability and scope as a query, because that is what they
// are: a transaction is a property of the connection a statement runs on, and
// anything allowed to run statements on a profile is allowed to decide whether
// they are committed. The audit entry names the action, so a rollback is
// visible in the log as itself.
// DBSetTxMode turns manual commit on or off. Turning it ON opens nothing: the
// first statement does, so choosing the mode and then reading nothing holds no
// locks on the user's server.
func (s *AppService) DBSetTxMode(ctx context.Context, id string, manual bool) error {
	opaqueID := recordID(id)
	_, err := s.call(ctx, "builtin.database", "db:query", "profile:"+opaqueID, map[string]any{
		"action":     "set_tx_mode",
		"profile_id": opaqueID,
		"manual":     manual,
	}, func(callCtx context.Context) (any, error) {
		return nil, s.container.Database.SetTxMode(callCtx, id, manual)
	})
	return err
}

func (s *AppService) DBCommitTx(ctx context.Context, id string) error {
	opaqueID := recordID(id)
	_, err := s.call(ctx, "builtin.database", "db:query", "profile:"+opaqueID, map[string]any{
		"action":     "commit_tx",
		"profile_id": opaqueID,
	}, func(callCtx context.Context) (any, error) {
		return nil, s.container.Database.CommitTx(callCtx, id)
	})
	return err
}

func (s *AppService) DBRollbackTx(ctx context.Context, id string) error {
	opaqueID := recordID(id)
	_, err := s.call(ctx, "builtin.database", "db:query", "profile:"+opaqueID, map[string]any{
		"action":     "rollback_tx",
		"profile_id": opaqueID,
	}, func(callCtx context.Context) (any, error) {
		return nil, s.container.Database.RollbackTx(callCtx, id)
	})
	return err
}

// DBTxMode is asked on mount: the mode belongs to the connection and outlives
// the window that chose it, so a window reopened onto a manual connection has
// to show Manual rather than Auto.
func (s *AppService) DBTxMode(ctx context.Context, id string) (bool, error) {
	opaqueID := recordID(id)
	result, err := s.call(ctx, "builtin.database", "db:query", "profile:"+opaqueID, map[string]any{
		"action":     "tx_mode",
		"profile_id": opaqueID,
	}, func(context.Context) (any, error) {
		return s.container.Database.TxMode(id), nil
	})
	if err != nil {
		return false, err
	}
	return result.(bool), nil
}

// DBTxOpen is asked on mount: a transaction outlives the tab that opened it,
// and a window reopened onto one has to show that rather than a clean slate.
func (s *AppService) DBTxOpen(ctx context.Context, id string) (bool, error) {
	opaqueID := recordID(id)
	result, err := s.call(ctx, "builtin.database", "db:query", "profile:"+opaqueID, map[string]any{
		"action":     "tx_open",
		"profile_id": opaqueID,
	}, func(context.Context) (any, error) {
		return s.container.Database.TxOpen(id), nil
	})
	if err != nil {
		return false, err
	}
	return result.(bool), nil
}

// DBTableDDL returns the statement that defines a table.
//
// Through db:query and the profile scope, like every other catalogue read: it
// is the server being asked about its own schema.
func (s *AppService) DBTableDDL(ctx context.Context, id, schema, table string) (string, error) {
	opaqueID := recordID(id)
	result, err := s.call(ctx, "builtin.database", "db:query", "profile:"+opaqueID, map[string]any{
		"action":     "table_ddl",
		"profile_id": opaqueID,
		"schema":     schema,
		"table":      table,
	}, func(callCtx context.Context) (any, error) {
		return s.container.Database.TableDDL(callCtx, id, schema, table)
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}

func (s *AppService) DBUpdateProfileOptions(id string, opts database.UpdateProfileOptionsInput) (database.ProfileSummary, error) {
	opaqueID := recordID(id)
	result, err := s.call(context.Background(), "builtin.database", "db:query", "profile:"+opaqueID, map[string]any{
		"action":     "update_profile_options",
		"profile_id": opaqueID,
		"env":        opts.Env,
		"read_only":  opts.ReadOnly,
	}, func(context.Context) (any, error) {
		return s.container.Database.UpdateProfileOptions(id, opts)
	})
	if err != nil {
		return database.ProfileSummary{}, err
	}
	return result.(database.ProfileSummary), nil
}

func defaultSaveFileDialog(suggestedName string) (string, error) {
	app := application.Get()
	if app == nil || app.Dialog == nil {
		return "", errors.New("dialog service unavailable")
	}
	dialog := app.Dialog.SaveFile().SetFilename(suggestedName)
	lower := strings.ToLower(suggestedName)
	if strings.HasSuffix(lower, ".csv") {
		dialog.AddFilter("CSV Files (*.csv)", "*.csv")
	} else if strings.HasSuffix(lower, ".json") {
		dialog.AddFilter("JSON Files (*.json)", "*.json")
	}
	return dialog.PromptForSingleSelection()
}

func (s *AppService) DBSaveExportFile(suggestedName, content string) (string, error) {
	result, err := s.call(context.Background(), "builtin.database", "db:query", "profile:export", map[string]any{
		"action":         "save_export_file",
		"suggested_name": suggestedName,
	}, func(context.Context) (any, error) {
		dialogFunc := s.saveFileDialog
		if dialogFunc == nil {
			dialogFunc = defaultSaveFileDialog
		}
		path, err := dialogFunc(suggestedName)
		if err != nil {
			return "", err
		}
		if path == "" {
			return "", nil
		}
		// 0o600, not 0o644: an exported result carries whatever the query
		// returned — the same data the vault exists to protect. Matches
		// storage/sqlite.go and mcp/install/*.go.
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			return "", err
		}
		return path, nil
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}

// DBListQueryHistory returns the newest statements run against a profile on
// this device. A locked vault is an error, not an empty list.
func (s *AppService) DBListQueryHistory(id string) ([]database.HistoryEntry, error) {
	opaqueID := recordID(id)
	result, err := s.call(context.Background(), "builtin.database", "db:query", "profile:"+opaqueID, map[string]any{
		"action":     "list_query_history",
		"profile_id": opaqueID,
	}, func(context.Context) (any, error) {
		return s.container.Database.ListQueryHistory(id)
	})
	if err != nil {
		return nil, err
	}
	return result.([]database.HistoryEntry), nil
}

// DBSaveQuery keeps a named statement. Audit records the name but never the
// statement — saved SQL can carry literals.
func (s *AppService) DBSaveQuery(id, name, sql string) (database.SavedQuery, error) {
	opaqueID := recordID(id)
	result, err := s.call(context.Background(), "builtin.database", "db:query", "profile:"+opaqueID, map[string]any{
		"action":     "save_query",
		"profile_id": opaqueID,
		"name":       name,
	}, func(context.Context) (any, error) {
		return s.container.Database.SaveQuery(id, name, sql)
	})
	if err != nil {
		return database.SavedQuery{}, err
	}
	return result.(database.SavedQuery), nil
}

func (s *AppService) DBListSavedQueries(id string) ([]database.SavedQuery, error) {
	opaqueID := recordID(id)
	result, err := s.call(context.Background(), "builtin.database", "db:query", "profile:"+opaqueID, map[string]any{
		"action":     "list_saved_queries",
		"profile_id": opaqueID,
	}, func(context.Context) (any, error) {
		return s.container.Database.ListSavedQueries(id)
	})
	if err != nil {
		return nil, err
	}
	return result.([]database.SavedQuery), nil
}

// DBDeleteSavedQuery hard-deletes. Queries have no trash, unlike notes.
func (s *AppService) DBDeleteSavedQuery(id string) error {
	opaqueID := recordID(id)
	_, err := s.call(context.Background(), "builtin.database", "db:query", "query:"+opaqueID, map[string]any{
		"action":   "delete_saved_query",
		"query_id": opaqueID,
	}, func(context.Context) (any, error) {
		return nil, s.container.Database.DeleteSavedQuery(id)
	})
	return err
}

// DBListSchemas, DBListTables and DBListColumns replace the hardcoded
// pg_catalog SQL the UI used to send through DBQuery. Catalogue queries are not
// portable, so each driver owns its own.
func (s *AppService) DBListSchemas(ctx context.Context, id string) ([]string, error) {
	opaqueID := recordID(id)
	result, err := s.call(ctx, "builtin.database", "db:query", "profile:"+opaqueID, map[string]any{
		"action":     "list_schemas",
		"profile_id": opaqueID,
	}, func(callCtx context.Context) (any, error) {
		return s.container.Database.ListSchemas(callCtx, id)
	})
	if err != nil {
		return nil, err
	}
	return result.([]string), nil
}

func (s *AppService) DBListTables(ctx context.Context, id, schema string) ([]database.TableInfo, error) {
	opaqueID := recordID(id)
	result, err := s.call(ctx, "builtin.database", "db:query", "profile:"+opaqueID, map[string]any{
		"action":     "list_tables",
		"profile_id": opaqueID,
		"schema":     schema,
	}, func(callCtx context.Context) (any, error) {
		return s.container.Database.ListTables(callCtx, id, schema)
	})
	if err != nil {
		return nil, err
	}
	return result.([]database.TableInfo), nil
}

func (s *AppService) DBListColumns(ctx context.Context, id, schema, table string) ([]database.ColumnInfo, error) {
	opaqueID := recordID(id)
	result, err := s.call(ctx, "builtin.database", "db:query", "profile:"+opaqueID, map[string]any{
		"action":     "list_columns",
		"profile_id": opaqueID,
		"schema":     schema,
		"table":      table,
	}, func(callCtx context.Context) (any, error) {
		return s.container.Database.ListColumns(callCtx, id, schema, table)
	})
	if err != nil {
		return nil, err
	}
	return result.([]database.ColumnInfo), nil
}

func (s *AppService) DBListForeignKeys(ctx context.Context, id, schema string) ([]database.ForeignKey, error) {
	opaqueID := recordID(id)
	result, err := s.call(ctx, "builtin.database", "db:query", "profile:"+opaqueID, map[string]any{
		"action": "list_foreign_keys", "profile_id": opaqueID, "schema": schema,
	}, func(callCtx context.Context) (any, error) {
		return s.container.Database.ListForeignKeys(callCtx, id, schema)
	})
	if err != nil {
		return nil, err
	}
	return result.([]database.ForeignKey), nil
}

// DBListIndexes carries no row data, only index names and the columns they
// cover, so it is scoped like any other schema read.
func (s *AppService) DBListIndexes(ctx context.Context, id, schema string) ([]database.IndexInfo, error) {
	opaqueID := recordID(id)
	result, err := s.call(ctx, "builtin.database", "db:query", "profile:"+opaqueID, map[string]any{
		"action": "list_indexes", "profile_id": opaqueID, "schema": schema,
	}, func(callCtx context.Context) (any, error) {
		return s.container.Database.ListIndexes(callCtx, id, schema)
	})
	if err != nil {
		return nil, err
	}
	return result.([]database.IndexInfo), nil
}

func (s *AppService) DBIndexSchema(ctx context.Context, id string) (database.SchemaSnapshot, error) {
	opaqueID := recordID(id)
	result, err := s.call(ctx, "builtin.database", "db:query", "profile:"+opaqueID, map[string]any{
		"action": "index_schema", "profile_id": opaqueID,
	}, func(callCtx context.Context) (any, error) {
		return s.container.Database.IndexSchema(callCtx, id)
	})
	if err != nil {
		return database.SchemaSnapshot{}, err
	}
	return result.(database.SchemaSnapshot), nil
}

func (s *AppService) DBGetSchemaCache(ctx context.Context, id string) (database.SchemaSnapshot, error) {
	opaqueID := recordID(id)
	result, err := s.call(ctx, "builtin.database", "db:query", "profile:"+opaqueID, map[string]any{
		"action": "get_schema_cache", "profile_id": opaqueID,
	}, func(callCtx context.Context) (any, error) {
		return s.container.Database.GetSchemaCache(callCtx, id)
	})
	if err != nil {
		return database.SchemaSnapshot{}, err
	}
	return result.(database.SchemaSnapshot), nil
}

// DBListActivity returns other people's sessions, statements included. Human
// only: the rows carry SQL with literals, which is why this never reaches MCP.
func (s *AppService) DBListActivity(ctx context.Context, id string) ([]database.SessionRow, error) {
	opaqueID := recordID(id)
	result, err := s.call(ctx, "builtin.database", "db:query", "profile:"+opaqueID, map[string]any{
		"action": "list_activity", "profile_id": opaqueID,
	}, func(callCtx context.Context) (any, error) {
		return s.container.Database.ListActivity(callCtx, id)
	})
	if err != nil {
		return nil, err
	}
	return result.([]database.SessionRow), nil
}

// DBCancelQuery stops one statement. Safe mode does not cover this — a
// read-only profile can still cancel, because pg_cancel_backend is a SELECT.
func (s *AppService) DBCancelQuery(ctx context.Context, id, pid string) error {
	opaqueID := recordID(id)
	_, err := s.call(ctx, "builtin.database", "db:query", "session:"+pid, map[string]any{
		"action": "cancel_query", "profile_id": opaqueID, "pid": pid,
	}, func(callCtx context.Context) (any, error) {
		return nil, s.container.Database.CancelQuery(callCtx, id, pid)
	})
	return err
}

// DBTerminateSession closes a connection and rolls back its transaction.
func (s *AppService) DBTerminateSession(ctx context.Context, id, pid string) error {
	opaqueID := recordID(id)
	_, err := s.call(ctx, "builtin.database", "db:query", "session:"+pid, map[string]any{
		"action": "terminate_session", "profile_id": opaqueID, "pid": pid,
	}, func(callCtx context.Context) (any, error) {
		return nil, s.container.Database.TerminateSession(callCtx, id, pid)
	})
	return err
}

// DBExplain plans without running. Audit records that a plan was asked for, not
// the statement — SQL can carry literals.
func (s *AppService) DBExplain(ctx context.Context, id, sql string) (database.QueryPlan, error) {
	opaqueID := recordID(id)
	result, err := s.call(ctx, "builtin.database", "db:query", "profile:"+opaqueID, map[string]any{
		"action": "explain", "profile_id": opaqueID, "analyzed": false,
	}, func(callCtx context.Context) (any, error) {
		return s.container.Database.Explain(callCtx, id, sql)
	})
	if err != nil {
		return database.QueryPlan{}, err
	}
	return result.(database.QueryPlan), nil
}

// DBExplainAnalyze runs the statement inside a rolled-back transaction, so it
// follows the same env-tag confirmation policy as DBQuery and stays off MCP.
func (s *AppService) DBExplainAnalyze(ctx context.Context, id, sql string) (database.QueryPlan, error) {
	opaqueID := recordID(id)
	result, err := s.call(ctx, "builtin.database", "db:query", "profile:"+opaqueID, map[string]any{
		"action": "explain_analyze", "profile_id": opaqueID, "analyzed": true,
	}, func(callCtx context.Context) (any, error) {
		return s.container.Database.ExplainAnalyze(callCtx, id, sql)
	})
	if err != nil {
		return database.QueryPlan{}, err
	}
	return result.(database.QueryPlan), nil
}

/*
Query consoles: the scratch text, not the saved query.

Their own capability rather than another scope under db:query. Reading a
console is reading what the user typed in every window they left open, which
is a different permission from being allowed to ask a database a question —
and the files are in the app's own directory, so no profile scopes them.
*/
func (s *AppService) DBListConsoles() ([]database.Console, error) {
	result, err := s.call(context.Background(), "builtin.database", "db:console", "console:list", map[string]any{
		"action": "list_consoles",
	}, func(context.Context) (any, error) {
		return s.container.Database.ListConsoles()
	})
	if err != nil {
		return nil, err
	}
	return result.([]database.Console), nil
}

/*
DBSaveConsole writes one console's text. Called on a debounce as the user
types, so it is the one bridge call here that runs without being asked for.

Four strings rather than the Console struct, and the reason is the struct's
UpdatedAt. Wails parses each argument out of JSON before the method runs, so
a time.Time in the signature makes a timestamp REQUIRED of the caller — and
the window has no business having one. It sent "", every call failed in the
binding layer before reaching any code here ("cannot parse \"\" as
\"2006\""), and nothing was ever written. The stamp belongs to the writer:
the store puts it on at the moment the bytes land.
*/
func (s *AppService) DBSaveConsole(id, profileID, databaseName, sql string) error {
	_, err := s.call(context.Background(), "builtin.database", "db:console", "console:"+recordID(id), map[string]any{
		"action":     "save_console",
		"console_id": recordID(id),
	}, func(context.Context) (any, error) {
		return nil, s.container.Database.SaveConsole(database.Console{
			ID:        id,
			ProfileID: profileID,
			Database:  databaseName,
			SQL:       sql,
		})
	})
	return err
}

func (s *AppService) DBDeleteConsole(id string) error {
	_, err := s.call(context.Background(), "builtin.database", "db:console", "console:"+recordID(id), map[string]any{
		"action":     "delete_console",
		"console_id": recordID(id),
	}, func(context.Context) (any, error) {
		return nil, s.container.Database.DeleteConsole(id)
	})
	return err
}

// ConsoleDirKey is where the chosen console folder is remembered. A ui_state
// row rather than a typed preference: it is one string, and main reads it
// straight back at startup to build the store.
const ConsoleDirKey = "database.console_dir"

// ConsoleDirectory is the folder the consoles are in, and the one they would
// return to. Both, because "Use the default" has to be offerable without the
// window knowing what the default is.
type ConsoleDirectory struct {
	Path string `json:"path"`
	// Default is where they go when nothing is chosen.
	Default string `json:"default"`
	// Custom is whether Path is a choice somebody made.
	Custom bool `json:"custom"`
}

func (s *AppService) DBConsoleDirectory() (ConsoleDirectory, error) {
	result, err := s.call(context.Background(), "builtin.database", "db:console", "console:list", map[string]any{
		"action": "console_directory",
	}, func(context.Context) (any, error) {
		current, fallback, err := s.container.Database.ConsoleDirectory()
		if err != nil {
			return ConsoleDirectory{}, err
		}
		return ConsoleDirectory{Path: current, Default: fallback, Custom: current != fallback}, nil
	})
	if err != nil {
		return ConsoleDirectory{}, err
	}
	return result.(ConsoleDirectory), nil
}

/*
DBSetConsoleDirectory moves the consoles and remembers where they went.

The move happens first and the row is written only if it worked, so the
setting the window shows on the next launch is the folder the files are
actually in. Failing to remember is not failing to move: the consoles are
where the user asked for, and saying otherwise would send them looking for
files that are not there. It is logged and the call succeeds.

An empty path is "use the default" — one call for both, because the window's
two buttons are the same decision with different arguments.
*/
func (s *AppService) DBSetConsoleDirectory(path string) (ConsoleDirectory, error) {
	result, err := s.call(context.Background(), "builtin.database", "db:console", "console:directory", map[string]any{
		"action": "set_console_directory",
	}, func(context.Context) (any, error) {
		if err := s.container.Database.SetConsoleDirectory(path); err != nil {
			return ConsoleDirectory{}, err
		}
		current, fallback, err := s.container.Database.ConsoleDirectory()
		if err != nil {
			return ConsoleDirectory{}, err
		}
		if s.container.UIState != nil {
			stored := current
			if current == fallback {
				stored = ""
			}
			if err := s.container.UIState.Set(ConsoleDirKey, stored); err != nil {
				log.Printf("database: remember console folder: %v", err)
			}
		}
		return ConsoleDirectory{Path: current, Default: fallback, Custom: current != fallback}, nil
	})
	if err != nil {
		return ConsoleDirectory{}, err
	}
	return result.(ConsoleDirectory), nil
}

// DBChooseConsoleDirectory opens the native folder picker, starting in the
// folder they are in now. Cancelling returns "" and is not an error — the
// settings panel stays as it was, and an error there would read as a failure
// the user caused by changing their mind.
func (s *AppService) DBChooseConsoleDirectory() (string, error) {
	result, err := s.call(context.Background(), "builtin.database", "db:console", "console:directory", map[string]any{
		"action": "choose_console_directory",
	}, func(context.Context) (any, error) {
		startIn, _, err := s.container.Database.ConsoleDirectory()
		if err != nil {
			startIn = ""
		}
		dialog := s.openDirDialog
		if dialog == nil {
			dialog = defaultOpenDirectoryDialog
		}
		return dialog("Where to keep query consoles", startIn)
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}
