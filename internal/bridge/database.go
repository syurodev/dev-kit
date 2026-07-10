package bridge

import (
	"context"

	"changeme/internal/services/database"
)

func (s *AppService) DBListProfiles() ([]database.ProfileSummary, error) {
	return s.container.Database.ListProfiles()
}

func (s *AppService) DBCreateProfile(input database.CreateProfileInput) (database.ProfileSummary, error) {
	return s.container.Database.CreateProfile(input)
}

func (s *AppService) DBDeleteProfile(id string) error {
	return s.container.Database.DeleteProfile(id)
}

func (s *AppService) DBTestConnection(ctx context.Context, id string) error {
	return s.container.Database.TestConnection(ctx, id)
}

func (s *AppService) DBQuery(ctx context.Context, id string, sql string) (database.QueryResult, error) {
	return s.container.Database.Query(ctx, id, sql)
}
