package telegramcontrol

import (
	"context"
	"database/sql"
	"encoding/json"
)

// State contains no token, prompt, answer or approval. app_preferences is local.
type Config struct {
	SessionIDs          []string `json:"sessionIds"`
	Enabled             bool     `json:"enabled"`
	AccountID           string   `json:"accountId"`
	Workspaces          []string `json:"workspaces"`
	SendDetails         bool     `json:"sendDetails"`
	ReadTranscript      bool     `json:"readTranscript"`
	ApproveInteractions bool     `json:"approveInteractions"`
}
type State struct {
	Config Config `json:"config"`
	UserID int64  `json:"userId"`
	ChatID int64  `json:"chatId"`
	Offset int64  `json:"offset"`
}
type Store interface {
	Load() (State, error)
	Save(State) error
}
type SQLStore struct{ db *sql.DB }

func NewSQLStore(db *sql.DB) *SQLStore { return &SQLStore{db: db} }
func (s *SQLStore) Load() (State, error) {
	var raw string
	var state State
	err := s.db.QueryRowContext(context.Background(), "SELECT value FROM app_preferences WHERE key = 'telegram_control'").Scan(&raw)
	if err == sql.ErrNoRows {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	err = json.Unmarshal([]byte(raw), &state)
	return state, err
}
func (s *SQLStore) Save(state State) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(context.Background(), `INSERT INTO app_preferences(key,value) VALUES('telegram_control',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, string(raw))
	return err
}

// SettingsError contains only a sanitized, human-readable setup failure.
type SettingsError struct{ Message string }

func (e *SettingsError) Error() string { return e.Message }
