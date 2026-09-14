package preferences

import (
	"context"
	"database/sql"
	"strconv"
)

const (
	themeKey              = "theme"
	themeFamilyKey        = "theme_family"
	agentNotificationsKey = "agent_notifications"
)

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
