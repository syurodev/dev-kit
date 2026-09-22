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
	agentCheckpointsKey   = "agent_checkpoints"
	agentSandboxKey       = "agent_sandbox"
	revealPasswordKey     = "accounts_reveal_requires_password"
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

// GetAgentSandbox treats anything it cannot parse as unset, so the default —
// fully off — applies rather than the setting becoming unreadable.
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
		return AgentSandboxSettings{}, false, nil
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
