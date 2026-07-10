package bridge

import (
	"context"

	"changeme/internal/services/database"
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
