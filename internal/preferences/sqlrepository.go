package preferences

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
)

const (
	themeKey              = "theme"
	themeFamilyKey        = "theme_family"
	agentNotificationsKey = "agent_notifications"
	updateChecksKey       = "update_checks"
	agentCheckpointsKey   = "agent_checkpoints"
	agentSandboxKey       = "agent_sandbox"
	codexAPIKeyAccountKey = "agent_codex_api_key_account"
	revealPasswordKey     = "accounts_reveal_requires_password"
	callVisibilityKey     = "codegraph_call_visibility"
	agentModelProfilesKey = "agent_model_profiles"
)

// GetRevealRequiresPassword treats a value it cannot read as unset, so the
// default applies. The default is ON, which is the safe direction for an
// unreadable row: the worst case is one extra prompt, not a credential shown
// without one.
func (r *SQLRepository) GetRevealRequiresPassword() (bool, bool, error) {
	var value string
	err := r.db.QueryRowContext(context.Background(),
		"SELECT value FROM app_preferences WHERE key = ?", revealPasswordKey).Scan(&value)
	if err == sql.ErrNoRows {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	required, parseErr := strconv.ParseBool(value)
	if parseErr != nil {
		return false, false, nil
	}
	return required, true, nil
}

func (r *SQLRepository) SetRevealRequiresPassword(required bool) error {
	_, err := r.db.ExecContext(context.Background(),
		`INSERT INTO app_preferences (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, revealPasswordKey, strconv.FormatBool(required))
	return err
}

// GetAgentNotifications treats a value it cannot read as unset, so the default
// applies rather than the setting becoming unreadable.
func (r *SQLRepository) GetAgentNotifications() (bool, bool, error) {
	var value string
	err := r.db.QueryRowContext(context.Background(),
		"SELECT value FROM app_preferences WHERE key = ?", agentNotificationsKey).Scan(&value)
	if err == sql.ErrNoRows {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	enabled, parseErr := strconv.ParseBool(value)
	if parseErr != nil {
		return false, false, nil
	}
	return enabled, true, nil
}

func (r *SQLRepository) SetAgentNotifications(enabled bool) error {
	_, err := r.db.ExecContext(context.Background(),
		`INSERT INTO app_preferences (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, agentNotificationsKey, strconv.FormatBool(enabled))
	return err
}

// GetAgentCheckpoints reads the settings back, treating anything it cannot
// parse as unset so the defaults apply rather than the feature becoming
// unreadable.
//
// A bare "true"/"false" is accepted as well: the switch shipped on its own
// before the numbers joined it, and a row written then still means what it
// said.
func (r *SQLRepository) GetAgentCheckpoints() (preferences AgentCheckpointSettings, found bool, err error) {
	var value string
	scanErr := r.db.QueryRowContext(context.Background(),
		"SELECT value FROM app_preferences WHERE key = ?", agentCheckpointsKey).Scan(&value)
	if scanErr == sql.ErrNoRows {
		return AgentCheckpointSettings{}, false, nil
	}
	if scanErr != nil {
		return AgentCheckpointSettings{}, false, scanErr
	}
	if enabled, parseErr := strconv.ParseBool(value); parseErr == nil {
		settings := DefaultAgentCheckpoints()
		settings.Enabled = enabled
		return settings, true, nil
	}
	var settings AgentCheckpointSettings
	if jsonErr := json.Unmarshal([]byte(value), &settings); jsonErr != nil {
		return AgentCheckpointSettings{}, false, nil
	}
	return settings.Normalized(), true, nil
}

func (r *SQLRepository) SetAgentCheckpoints(settings AgentCheckpointSettings) error {
	body, err := json.Marshal(settings.Normalized())
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(context.Background(),
		`INSERT INTO app_preferences (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, agentCheckpointsKey, string(body))
	return err
}

// GetAgentSandbox reads a value it cannot parse as StrictAgentSandbox rather
// than as unset: unset is the permissive default, and a stored "don't share
// home" that got mangled must not quietly turn into sharing it.
func (r *SQLRepository) GetAgentSandbox() (AgentSandboxSettings, bool, error) {
	var value string
	err := r.db.QueryRowContext(context.Background(),
		"SELECT value FROM app_preferences WHERE key = ?", agentSandboxKey).Scan(&value)
	if err == sql.ErrNoRows {
		return AgentSandboxSettings{}, false, nil
	}
	if err != nil {
		return AgentSandboxSettings{}, false, err
	}
	var settings AgentSandboxSettings
	if jsonErr := json.Unmarshal([]byte(value), &settings); jsonErr != nil {
		return StrictAgentSandbox(), true, nil
	}
	return settings.Normalized(), true, nil
}

func (r *SQLRepository) SetAgentSandbox(settings AgentSandboxSettings) error {
	body, err := json.Marshal(settings.Normalized())
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(context.Background(),
		`INSERT INTO app_preferences (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, agentSandboxKey, string(body))
	return err
}

func (r *SQLRepository) GetCodexAPIKeyAccount() (string, bool, error) {
	var value string
	err := r.db.QueryRowContext(context.Background(),
		"SELECT value FROM app_preferences WHERE key = ?", codexAPIKeyAccountKey).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

func (r *SQLRepository) SetCodexAPIKeyAccount(accountID string) error {
	_, err := r.db.ExecContext(context.Background(),
		`INSERT INTO app_preferences (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, codexAPIKeyAccountKey, accountID)
	return err
}

type SQLRepository struct {
	db *sql.DB
}

func NewSQLRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

func (r *SQLRepository) GetTheme() (Theme, bool, error) {
	var value string
	err := r.db.QueryRowContext(context.Background(),
		"SELECT value FROM app_preferences WHERE key = ?", themeKey).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return Theme(value), true, nil
}

func (r *SQLRepository) SetTheme(theme Theme) error {
	_, err := r.db.ExecContext(context.Background(),
		`INSERT INTO app_preferences (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, themeKey, string(theme))
	return err
}

func (r *SQLRepository) GetThemeFamily() (ThemeFamily, bool, error) {
	var value string
	err := r.db.QueryRowContext(context.Background(),
		"SELECT value FROM app_preferences WHERE key = ?", themeFamilyKey).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return ThemeFamily(value), true, nil
}

func (r *SQLRepository) SetThemeFamily(family ThemeFamily) error {
	_, err := r.db.ExecContext(context.Background(),
		`INSERT INTO app_preferences (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, themeFamilyKey, string(family))
	return err
}

// GetUpdateChecks treats a value it cannot read as unset, so the default
// applies rather than the setting becoming unreadable.
func (r *SQLRepository) GetUpdateChecks() (bool, bool, error) {
	var value string
	err := r.db.QueryRowContext(context.Background(),
		"SELECT value FROM app_preferences WHERE key = ?", updateChecksKey).Scan(&value)
	if err == sql.ErrNoRows {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	enabled, parseErr := strconv.ParseBool(value)
	if parseErr != nil {
		return false, false, nil
	}
	return enabled, true, nil
}

func (r *SQLRepository) SetUpdateChecks(enabled bool) error {
	_, err := r.db.ExecContext(context.Background(),
		`INSERT INTO app_preferences (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, updateChecksKey, strconv.FormatBool(enabled))
	return err
}

// GetCallVisibility reads a value it cannot parse as unset: losing the
// overrides shows the defaults, which is safe.
func (r *SQLRepository) GetCallVisibility() (map[string]string, bool, error) {
	var value string
	err := r.db.QueryRowContext(context.Background(),
		"SELECT value FROM app_preferences WHERE key = ?", callVisibilityKey).Scan(&value)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var overrides map[string]string
	if jsonErr := json.Unmarshal([]byte(value), &overrides); jsonErr != nil {
		return nil, false, nil
	}
	return overrides, true, nil
}

func (r *SQLRepository) SetCallVisibility(overrides map[string]string) error {
	body, err := json.Marshal(overrides)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(context.Background(),
		`INSERT INTO app_preferences (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, callVisibilityKey, string(body))
	return err
}

// GetAgentModelProfiles reads a value it cannot parse as unset: the profiles
// are a convenience, and an agent without them inherits its parent's model.
func (r *SQLRepository) GetAgentModelProfiles() (AgentModelProfiles, bool, error) {
	var value string
	err := r.db.QueryRowContext(context.Background(),
		"SELECT value FROM app_preferences WHERE key = ?", agentModelProfilesKey).Scan(&value)
	if err == sql.ErrNoRows {
		return AgentModelProfiles{}, false, nil
	}
	if err != nil {
		return AgentModelProfiles{}, false, err
	}
	var profiles AgentModelProfiles
	if jsonErr := json.Unmarshal([]byte(value), &profiles); jsonErr != nil {
		return AgentModelProfiles{}, false, nil
	}
	return profiles, true, nil
}

func (r *SQLRepository) SetAgentModelProfiles(profiles AgentModelProfiles) error {
	body, err := json.Marshal(profiles)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(context.Background(),
		`INSERT INTO app_preferences (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, agentModelProfilesKey, string(body))
	return err
}
