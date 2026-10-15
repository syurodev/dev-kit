package preferences_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"devkit/internal/preferences"
	"devkit/internal/storage"
)

func TestSQLRepositoryThemeRoundTrip(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}

	service := preferences.NewService(preferences.NewSQLRepository(db))
	if got, err := service.Theme(); err != nil || got != preferences.ThemeDark {
		t.Fatalf("initial theme = %q, err = %v", got, err)
	}
	if err := service.SetTheme(preferences.ThemeSystem); err != nil {
		t.Fatal(err)
	}
	if got, err := service.Theme(); err != nil || got != preferences.ThemeSystem {
		t.Fatalf("stored theme = %q, err = %v", got, err)
	}
}

func TestSQLRepositoryAgentNotificationsDefaultOnAndRoundTrip(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}

	service := preferences.NewService(preferences.NewSQLRepository(db))
	if got, err := service.AgentNotifications(); err != nil || !got {
		t.Fatalf("initial = %v, err = %v; want on by default", got, err)
	}
	if err := service.SetAgentNotifications(false); err != nil {
		t.Fatal(err)
	}
	if got, err := service.AgentNotifications(); err != nil || got {
		t.Fatalf("after turning off = %v, err = %v", got, err)
	}
}

func TestSQLRepositoryUpdateChecksDefaultOnAndRoundTrip(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}

	service := preferences.NewService(preferences.NewSQLRepository(db))
	if got, err := service.UpdateChecks(); err != nil || !got {
		t.Fatalf("initial = %v, err = %v; want on by default", got, err)
	}
	if err := service.SetUpdateChecks(false); err != nil {
		t.Fatal(err)
	}
	if got, err := service.UpdateChecks(); err != nil || got {
		t.Fatalf("after turning off = %v, err = %v", got, err)
	}
	if _, err := db.Exec(`UPDATE app_preferences SET value = 'garbage' WHERE key = 'update_checks'`); err != nil {
		t.Fatal(err)
	}
	if got, err := service.UpdateChecks(); err != nil || !got {
		t.Fatalf("unreadable row = %v, err = %v; want the default", got, err)
	}
}

// checkpointRepo opens a migrated database and a repository over it.
func checkpointRepo(t *testing.T) (*preferences.SQLRepository, *sql.DB) {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return preferences.NewSQLRepository(db), db
}

// The stored value is JSON now; a row written when the setting was a bare
// boolean still reads as the switch it was, with the numbers defaulted. Losing
// it would silently turn checkpoints back on for a user who turned them off.
func TestSQLRepositoryReadsABareBooleanCheckpointRow(t *testing.T) {
	repo, db := checkpointRepo(t)
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO app_preferences (key, value) VALUES ('agent_checkpoints', 'false')`); err != nil {
		t.Fatal(err)
	}
	settings, found, err := repo.GetAgentCheckpoints()
	if err != nil || !found {
		t.Fatalf("GetAgentCheckpoints = %+v, %v, %v", settings, found, err)
	}
	if settings.Enabled {
		t.Error("a stored false read as on")
	}
	if settings.MaxFileSizeMiB != 10 || settings.RetentionDays != 30 {
		t.Errorf("settings = %+v, want the numbers defaulted", settings)
	}
}

func TestSQLRepositoryRoundTripsCheckpointSettings(t *testing.T) {
	repo, _ := checkpointRepo(t)
	want := preferences.AgentCheckpointSettings{
		Enabled:        true,
		MaxFileSizeMiB: 25,
		RetentionDays:  7,
		ExtraExcludes:  []string{"generated/"},
	}
	if err := repo.SetAgentCheckpoints(want); err != nil {
		t.Fatal(err)
	}
	got, found, err := repo.GetAgentCheckpoints()
	if err != nil || !found {
		t.Fatalf("GetAgentCheckpoints = %+v, %v, %v", got, found, err)
	}
	if got.MaxFileSizeMiB != 25 || got.RetentionDays != 7 || len(got.ExtraExcludes) != 1 {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
}

func TestSQLRepositoryRoundTripsAgentSandboxSettings(t *testing.T) {
	repo, _ := checkpointRepo(t)
	off := false
	want := preferences.AgentSandboxSettings{
		DockerSocketPath:   "/var/run/docker.sock",
		ExtraReadOnlyPaths: []string{"/opt/shared-lib"},
		ShareHome:          &off,
		AllowDocker:        &off,
	}
	if err := repo.SetAgentSandbox(want); err != nil {
		t.Fatal(err)
	}
	got, found, err := repo.GetAgentSandbox()
	if err != nil || !found {
		t.Fatalf("GetAgentSandbox = %+v, %v, %v", got, found, err)
	}
	if got.DockerSocketPath != want.DockerSocketPath || len(got.ExtraReadOnlyPaths) != 1 || got.HomeShared() || got.DockerAllowed() {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
}

// A machine that has never touched this setting shares home and allows Docker.
func TestSQLRepositoryAgentSandboxDefaultsToHomeAndDocker(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	service := preferences.NewService(preferences.NewSQLRepository(db))
	settings, err := service.AgentSandbox()
	if err != nil {
		t.Fatal(err)
	}
	if settings.DockerSocketPath != "" || len(settings.ExtraReadOnlyPaths) != 0 || !settings.HomeShared() || !settings.DockerAllowed() {
		t.Fatalf("settings = %+v, want the zero value", settings)
	}
}

// A value nothing can parse is unset, so the defaults apply rather than the
// setting becoming unreadable.
func TestSQLRepositoryTreatsAnUnreadableCheckpointRowAsUnset(t *testing.T) {
	repo, db := checkpointRepo(t)
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO app_preferences (key, value) VALUES ('agent_checkpoints', '{{ not json')`); err != nil {
		t.Fatal(err)
	}
	settings, found, err := repo.GetAgentCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatalf("an unreadable row read as stored: %+v", settings)
	}
}

// The default is permissive, so a stored value that no longer parses must
// read as the narrowest jail, not as unset.
func TestSQLRepositoryUnreadableAgentSandboxIsStrict(t *testing.T) {
	repo, db := checkpointRepo(t)
	if _, err := db.Exec(`INSERT INTO app_preferences (key, value) VALUES ('agent_sandbox', '{not json')`); err != nil {
		t.Fatal(err)
	}
	got, found, err := repo.GetAgentSandbox()
	if err != nil || !found {
		t.Fatalf("GetAgentSandbox = %+v, %v, %v", got, found, err)
	}
	if got.HomeShared() || got.DockerAllowed() {
		t.Fatalf("settings = %+v, want StrictAgentSandbox", got)
	}
}

// The Codex API key is an Accounts id, never the key; unset and cleared both
// read as "use the ChatGPT sign-in".
func TestSQLRepositoryCodexAPIKeyAccountRoundTrip(t *testing.T) {
	repo, _ := checkpointRepo(t)
	service := preferences.NewService(repo)
	if got, err := service.CodexAPIKeyAccount(); err != nil || got != "" {
		t.Fatalf("unset = %q, %v", got, err)
	}
	if err := service.SetCodexAPIKeyAccount(" 0123456789abcdef01234567 "); err != nil {
		t.Fatal(err)
	}
	if got, err := service.CodexAPIKeyAccount(); err != nil || got != "0123456789abcdef01234567" {
		t.Fatalf("stored = %q, %v", got, err)
	}
	if err := service.SetCodexAPIKeyAccount(""); err != nil {
		t.Fatal(err)
	}
	if got, err := service.CodexAPIKeyAccount(); err != nil || got != "" {
		t.Fatalf("cleared = %q, %v", got, err)
	}
}

func TestSQLRepositoryCallVisibilityRoundTrip(t *testing.T) {
	repo, _ := checkpointRepo(t)
	if _, found, err := repo.GetCallVisibility(); err != nil || found {
		t.Fatalf("unset: found = %v, err = %v", found, err)
	}
	if err := repo.SetCallVisibility(map[string]string{"a#b": "show"}); err != nil {
		t.Fatal(err)
	}
	got, found, err := repo.GetCallVisibility()
	if err != nil || !found || got["a#b"] != "show" {
		t.Fatalf("got = %v, found = %v, err = %v", got, found, err)
	}
}
