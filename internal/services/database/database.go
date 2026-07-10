package database

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"time"

	"changeme/internal/crypto"

	"github.com/jackc/pgx/v5"
)

const (
	passwordKeyID             = "db-password"
	databaseProfileRecordType = "database-profile"
)

// DefaultMaxRows caps query results so the UI stays responsive (§12.4).
// ponytail: fixed cap; make it configurable per query later if needed.
const DefaultMaxRows = 200

// Vault is the encryption capability the database service needs. The DEK stays
// inside the vault; the service only gets seal/open.
type Vault interface {
	Seal(keyID string, plaintext []byte) (crypto.Envelope, error)
	Open(envelope crypto.Envelope) ([]byte, error)
	SealV2(associatedData crypto.AssociatedData, plaintext []byte) (crypto.Envelope, error)
	OpenWithAD(envelope crypto.Envelope, associatedData crypto.AssociatedData) ([]byte, error)
}

// ProfileRecord is the at-rest form of a connection profile. Only the password
// is encrypted; the rest is non-sensitive connection metadata.
type ProfileRecord struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Host      string          `json:"host"`
	Port      int             `json:"port"`
	Database  string          `json:"database"`
	Username  string          `json:"username"`
	SSLMode   string          `json:"ssl_mode"`
	Password  crypto.Envelope `json:"password"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type Repository interface {
	List() ([]ProfileRecord, error)
	Get(id string) (ProfileRecord, error)
	Put(record ProfileRecord) error
	Delete(id string) error
}

// ProfileSummary is a connection profile without the (encrypted) password.
type ProfileSummary struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	Username string `json:"username"`
	SSLMode  string `json:"ssl_mode"`
}

type CreateProfileInput struct {
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	Username string `json:"username"`
	Password string `json:"password"`
	SSLMode  string `json:"ssl_mode"`
}

type QueryResult struct {
	Columns   []string   `json:"columns"`
	Rows      [][]string `json:"rows"`
	RowCount  int        `json:"row_count"`
	Truncated bool       `json:"truncated"`
}

type Service struct {
	repo    Repository
	vault   Vault
	maxRows int
}

func NewService(repo Repository, vault Vault) *Service {
	return &Service{repo: repo, vault: vault, maxRows: DefaultMaxRows}
}

func (s *Service) CreateProfile(input CreateProfileInput) (ProfileSummary, error) {
	if input.Name == "" {
		return ProfileSummary{}, errors.New("name is required")
	}
	if input.Host == "" {
		return ProfileSummary{}, errors.New("host is required")
	}
	if input.Port == 0 {
		input.Port = 5432
	}
	if input.SSLMode == "" {
		input.SSLMode = "disable"
	}

	id, err := newID()
	if err != nil {
		return ProfileSummary{}, err
	}
	envelope, err := s.vault.SealV2(crypto.AssociatedData{RecordType: databaseProfileRecordType, RecordID: id, KeyID: passwordKeyID}, []byte(input.Password))
	if err != nil {
		return ProfileSummary{}, err
	}
	now := time.Now().UTC()
	record := ProfileRecord{
		ID:        id,
		Name:      input.Name,
		Host:      input.Host,
		Port:      input.Port,
		Database:  input.Database,
		Username:  input.Username,
		SSLMode:   input.SSLMode,
		Password:  envelope,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repo.Put(record); err != nil {
		return ProfileSummary{}, err
	}
	return toSummary(record), nil
}

func (s *Service) ListProfiles() ([]ProfileSummary, error) {
	records, err := s.repo.List()
	if err != nil {
		return nil, err
	}
	summaries := make([]ProfileSummary, 0, len(records))
	for _, record := range records {
		summaries = append(summaries, toSummary(record))
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Name < summaries[j].Name
	})
	return summaries, nil
}

func (s *Service) DeleteProfile(id string) error {
	return s.repo.Delete(id)
}

// TestConnection decrypts the password, connects, and pings the database.
func (s *Service) TestConnection(ctx context.Context, id string) error {
	dsn, err := s.dsn(id)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	return conn.Ping(ctx)
}

// Query runs SQL against the profile's database and returns rows (capped).
func (s *Service) Query(ctx context.Context, id string, sql string) (QueryResult, error) {
	dsn, err := s.dsn(id)
	if err != nil {
		return QueryResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return QueryResult{}, err
	}
	defer conn.Close(ctx)

	rows, err := conn.Query(ctx, sql)
	if err != nil {
		return QueryResult{}, err
	}
	defer rows.Close()

	fields := rows.FieldDescriptions()
	columns := make([]string, len(fields))
	for i, f := range fields {
		columns[i] = f.Name
	}

	result := QueryResult{Columns: columns, Rows: [][]string{}}
	for rows.Next() {
		if result.RowCount >= s.maxRows {
			result.Truncated = true
			break
		}
		values, err := rows.Values()
		if err != nil {
			return QueryResult{}, err
		}
		row := make([]string, len(values))
		for i, v := range values {
			row[i] = stringify(v)
		}
		result.Rows = append(result.Rows, row)
		result.RowCount++
	}
	if err := rows.Err(); err != nil && !result.Truncated {
		return QueryResult{}, err
	}
	return result, nil
}

func (s *Service) dsn(id string) (string, error) {
	record, err := s.repo.Get(id)
	if err != nil {
		return "", err
	}
	password, err := s.vault.OpenWithAD(record.Password, databaseProfileAssociatedData(record))
	if err != nil {
		return "", err
	}
	sslmode := record.SSLMode
	if sslmode == "" {
		sslmode = "disable"
	}
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(record.Username, string(password)),
		Host:   fmt.Sprintf("%s:%d", record.Host, record.Port),
		Path:   record.Database,
	}
	query := u.Query()
	query.Set("sslmode", sslmode)
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func databaseProfileAssociatedData(record ProfileRecord) crypto.AssociatedData {
	return crypto.AssociatedData{RecordType: databaseProfileRecordType, RecordID: record.ID, KeyID: passwordKeyID}
}

func stringify(value any) string {
	if value == nil {
		return "NULL"
	}
	switch typed := value.(type) {
	case []byte:
		return string(typed)
	case string:
		return typed
	default:
		return fmt.Sprintf("%v", typed)
	}
}

func toSummary(record ProfileRecord) ProfileSummary {
	return ProfileSummary{
		ID:       record.ID,
		Name:     record.Name,
		Host:     record.Host,
		Port:     record.Port,
		Database: record.Database,
		Username: record.Username,
		SSLMode:  record.SSLMode,
	}
}

func newID() (string, error) {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}
