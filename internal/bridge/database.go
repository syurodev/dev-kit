package bridge

import (
	"context"
	"errors"
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

func (s *AppService) DBQuery(ctx context.Context, id string, sql string) (database.QueryResult, error) {
	opaqueID := recordID(id)
	result, err := s.call(ctx, "builtin.database", "db:query", "profile:"+opaqueID, map[string]any{
		"action":     "query",
		"profile_id": opaqueID,
	}, func(callCtx context.Context) (any, error) {
		return s.container.Database.Query(callCtx, id, sql)
	})
	if err != nil {
		return database.QueryResult{}, err
	}
	return result.(database.QueryResult), nil
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
