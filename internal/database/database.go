package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"sort"
	"sync"

	"devkit/internal/vaultitem"
)

const (
	passwordKeyID = "db-password"
	itemType      = "db-profile"
)

var ErrInsecureTLSForRemoteHost = errors.New("insecure TLS is only allowed for loopback database hosts")

// DefaultMaxRows caps query results so the UI stays responsive (§12.4).
// ponytail: fixed cap; make it configurable per query later if needed.
const DefaultMaxRows = 200

// EnvLookup is the narrow view of the environments registry the database
// service needs to validate a profile's env tag. Declared here (the
// consumer), not by internal/environments — README.md "Accept interfaces,
// return structs".
type EnvLookup interface {
	Exists(name string) bool
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
	Env      string `json:"env"`
	ReadOnly bool   `json:"read_only"`
	// SSHProfileID is optional. When set, Host/Port are addresses as seen from
	// the SSH server, not from this machine.
	SSHProfileID string `json:"ssh_profile_id,omitempty"`
	// Engine is empty on profiles created before multi-engine support; those
	// are all Postgres, which is why it defaults rather than being required.
	Engine string `json:"engine,omitempty"`
	// DriverOptions carries engine-specific parameters that do not generalise.
	DriverOptions map[string]string `json:"driver_options,omitempty"`
}

type CreateProfileInput struct {
	Engine   string `json:"engine"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	Username string `json:"username"`
	Password string `json:"password"`
	SSLMode  string `json:"ssl_mode"`
	Env      string `json:"env"`
}

type UpdateProfileOptionsInput struct {
	Env      string `json:"env"`
	ReadOnly bool   `json:"read_only"`
	// SSHProfileID empty means connect directly.
	SSHProfileID string `json:"ssh_profile_id"`
}

type QueryResult struct {
	Columns   []string   `json:"columns"`
	Rows      [][]string `json:"rows"`
	RowCount  int        `json:"row_count"`
	Truncated bool       `json:"truncated"`
}

// Store is the shared vault-item substrate the database service needs.
type Store interface {
	Create(itemType, keyID string, metadata json.RawMessage, secret []byte) (vaultitem.Record, error)
	Get(id string) (vaultitem.Record, error)
	OpenPayload(record vaultitem.Record, keyID string) ([]byte, error)
	ListByType(itemType string) ([]vaultitem.Record, error)
	UpdateMetadata(id string, metadata json.RawMessage) (vaultitem.Record, error)
	Delete(id string) error
}

// metadata is the plaintext, non-sensitive connection metadata for a profile.
type metadata struct {
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	Username string `json:"username"`
	SSLMode  string `json:"ssl_mode"`
	Env      string `json:"env"`
	ReadOnly bool   `json:"read_only"`
	// SSHProfileID is optional. When set, Host/Port are addresses as seen from
	// the SSH server, not from this machine.
	SSHProfileID string `json:"ssh_profile_id,omitempty"`
	// Engine is empty on profiles created before multi-engine support; those
	// are all Postgres, which is why it defaults rather than being required.
	Engine string `json:"engine,omitempty"`
	// DriverOptions carries engine-specific parameters that do not generalise.
	DriverOptions map[string]string `json:"driver_options,omitempty"`
}

type Service struct {
	store         Store
	envs          EnvLookup
	mu            sync.Mutex
	agentActiveID string
	pools         *PoolManager
	maxRows       int
	history       QueryHistoryRepository
	sealer        Sealer
	schemaCache   SchemaCacheRepository
}

func NewService(store Store, envs EnvLookup) *Service {
	return &Service{store: store, envs: envs, pools: NewPoolManager(), maxRows: DefaultMaxRows}
}

func (s *Service) CreateProfile(input CreateProfileInput) (ProfileSummary, error) {
	if input.Name == "" {
		return ProfileSummary{}, errors.New("name is required")
	}
	if input.Host == "" {
		return ProfileSummary{}, errors.New("host is required")
	}
	if input.Env == "" {
		return ProfileSummary{}, errors.New("env is required")
	}
	if !s.envs.Exists(input.Env) {
		return ProfileSummary{}, fmt.Errorf("unknown env tag %q", input.Env)
	}
	if input.Engine == "" {
		input.Engine = EnginePostgres
	}
	if _, err := driverFor(input.Engine); err != nil {
		return ProfileSummary{}, err
	}
	if input.Port == 0 {
		input.Port = DefaultPort(input.Engine)
	}
	sslMode, err := normalizeSSLMode(input.Host, input.SSLMode)
	if err != nil {
		return ProfileSummary{}, err
	}
	input.SSLMode = sslMode

	meta, err := json.Marshal(metadata{
		Engine:   input.Engine,
		Name:     input.Name,
		Host:     input.Host,
		Port:     input.Port,
		Database: input.Database,
		Username: input.Username,
		SSLMode:  input.SSLMode,
		Env:      input.Env,
	})
	if err != nil {
		return ProfileSummary{}, err
	}
	record, err := s.store.Create(itemType, passwordKeyID, meta, []byte(input.Password))
	if err != nil {
		return ProfileSummary{}, err
	}
	return summaryFromRecord(record)
}

func (s *Service) ListProfiles() ([]ProfileSummary, error) {
	records, err := s.store.ListByType(itemType)
	if err != nil {
		return nil, err
	}
	summaries := make([]ProfileSummary, 0, len(records))
	for _, record := range records {
		summary, err := summaryFromRecord(record)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Name < summaries[j].Name
	})
	return summaries, nil
}

func (s *Service) UpdateProfileOptions(id string, opts UpdateProfileOptionsInput) (ProfileSummary, error) {
	if opts.Env == "" {
		return ProfileSummary{}, errors.New("env is required")
	}
	if !s.envs.Exists(opts.Env) {
		return ProfileSummary{}, fmt.Errorf("unknown env tag %q", opts.Env)
	}

	record, err := s.store.Get(id)
	if err != nil {
		return ProfileSummary{}, err
	}
	meta, err := decodeMetadata(record)
	if err != nil {
		return ProfileSummary{}, err
	}

	// env costs nothing at runtime; read_only needs a fresh pool so AfterConnect
	// runs again; the tunnel needs the forward torn down with it.
	poolChanged := meta.ReadOnly != opts.ReadOnly || meta.SSHProfileID != opts.SSHProfileID

	meta.Env = opts.Env
	meta.ReadOnly = opts.ReadOnly
	meta.SSHProfileID = opts.SSHProfileID

	rawMeta, err := json.Marshal(meta)
	if err != nil {
		return ProfileSummary{}, err
	}

	updatedRecord, err := s.store.UpdateMetadata(id, rawMeta)
	if err != nil {
		return ProfileSummary{}, err
	}

	if poolChanged {
		s.pools.Remove(id)
	}

	return summaryFromRecord(updatedRecord)
}

func (s *Service) DeleteProfile(id string) error {
	if err := s.store.Delete(id); err != nil {
		return err
	}
	s.pools.Remove(id)
	// History and the schema index live outside vault_items, so they ride no
	// vault-item cascade.
	if repo, _ := s.queryLog(); repo != nil {
		if err := repo.DeleteProfile(id); err != nil {
			log.Printf("db history: delete for profile: %v", err)
		}
	}
	if cache := s.schemaCacheRepo(); cache != nil {
		if err := cache.Delete(id); err != nil {
			log.Printf("db schema cache: delete for profile: %v", err)
		}
	}
	return nil
}

// ListSchemas, ListTables and ListColumns are driver methods rather than SQL
// the service writes: catalogue queries are not portable, and routing them
// through Query would hardcode one engine's system tables into shared code.
func (s *Service) ListSchemas(ctx context.Context, id string) ([]string, error) {
	conn, err := s.connect(ctx, id)
	if err != nil {
		return nil, err
	}
	ctx, cancel := queryContext(ctx)
	defer cancel()
	return conn.ListSchemas(ctx)
}

func (s *Service) ListTables(ctx context.Context, id, schema string) ([]TableInfo, error) {
	conn, err := s.connect(ctx, id)
	if err != nil {
		return nil, err
	}
	ctx, cancel := queryContext(ctx)
	defer cancel()
	return conn.ListTables(ctx, schema)
}

func (s *Service) ListColumns(ctx context.Context, id, schema, table string) ([]ColumnInfo, error) {
	conn, err := s.connect(ctx, id)
	if err != nil {
		return nil, err
	}
	ctx, cancel := queryContext(ctx)
	defer cancel()
	return conn.ListColumns(ctx, schema, table)
}

// ListForeignKeys is separate from ListColumns because the sidebar opens one
// table at a time and does not want relationships, while an ERD loads a whole
// schema and wants nothing else.
func (s *Service) ListForeignKeys(ctx context.Context, id, schema string) ([]ForeignKey, error) {
	conn, err := s.connect(ctx, id)
	if err != nil {
		return nil, err
	}
	ctx, cancel := queryContext(ctx)
	defer cancel()
	return conn.ListForeignKeys(ctx, schema)
}

func (s *Service) connect(ctx context.Context, id string) (Conn, error) {
	target, err := s.target(id)
	if err != nil {
		return nil, err
	}
	return s.pool(ctx, id, target)
}

// SetTunnel wires SSH port forwarding for profiles that use one.
func (s *Service) SetTunnel(t Tunnel) { s.pools.SetTunnel(t) }

// Close releases all PostgreSQL pools during application shutdown.
func (s *Service) Close() { s.pools.Close() }

// TestConnection decrypts the password, connects, and pings the database.
func (s *Service) TestConnection(ctx context.Context, id string) error {
	target, err := s.target(id)
	if err != nil {
		return err
	}
	ctx, cancel := queryContext(ctx)
	defer cancel()

	pool, err := s.pool(ctx, id, target)
	if err != nil {
		return err
	}
	return pool.Ping(ctx)
}

// Query runs SQL against the profile's database and returns rows (capped).
func (s *Service) Query(ctx context.Context, id string, sql string) (QueryResult, error) {
	target, err := s.target(id)
	if err != nil {
		return QueryResult{}, err
	}
	ctx, cancel := queryContext(ctx)
	defer cancel()

	pool, err := s.pool(ctx, id, target)
	if err != nil {
		return QueryResult{}, err
	}

	rows, err := pool.Query(ctx, sql)
	if err != nil {
		return QueryResult{}, err
	}
	defer rows.Close()

	result := QueryResult{Columns: rows.Columns(), Rows: [][]string{}}
	for rows.Next() {
		if result.RowCount >= s.maxRows {
			result.Truncated = true
			break
		}
		// Values are already strings: each driver stringifies, so one result
		// shape serves every engine.
		row, err := rows.Values()
		if err != nil {
			return QueryResult{}, err
		}
		result.Rows = append(result.Rows, row)
		result.RowCount++
	}
	if err := rows.Err(); err != nil && !result.Truncated {
		return QueryResult{}, err
	}
	s.recordHistory(id, sql)
	s.refreshSchemaAfterDDL(id, sql)
	return result, nil
}

type connectionTarget struct {
	cfg          ConnConfig
	sshProfileID string
}

func (s *Service) target(id string) (connectionTarget, error) {
	record, err := s.store.Get(id)
	if err != nil {
		return connectionTarget{}, err
	}
	meta, err := decodeMetadata(record)
	if err != nil {
		return connectionTarget{}, err
	}
	password, err := s.store.OpenPayload(record, passwordKeyID)
	if err != nil {
		return connectionTarget{}, err
	}
	// Match the SSH/Kafka hygiene: wipe the decrypted bytes once the config
	// copy is built.
	defer zeroBytes(password)
	sslmode, err := normalizeSSLMode(meta.Host, meta.SSLMode)
	if err != nil {
		return connectionTarget{}, err
	}
	return connectionTarget{
		cfg: ConnConfig{
			Engine:   meta.Engine,
			Host:     meta.Host,
			Port:     meta.Port,
			Database: meta.Database,
			Username: meta.Username,
			Password: string(password),
			SSLMode:  sslmode,
			ReadOnly: meta.ReadOnly,
			Options:  meta.DriverOptions,
		},
		sshProfileID: meta.SSHProfileID,
	}, nil
}

func (s *Service) pool(ctx context.Context, id string, target connectionTarget) (Conn, error) {
	spec := connectionSpec{profileID: id, cfg: target.cfg}
	if target.sshProfileID != "" {
		spec.tunnel = &TunnelSpec{
			SSHProfileID: target.sshProfileID,
			TargetHost:   target.cfg.Host,
			TargetPort:   target.cfg.Port,
		}
	}
	return s.pools.Get(ctx, spec)
}

func normalizeSSLMode(host, sslMode string) (string, error) {
	if sslMode == "" {
		if isLoopbackHost(host) {
			return "disable", nil
		}
		return "verify-full", nil
	}
	if sslMode == "disable" && !isLoopbackHost(host) {
		return "", ErrInsecureTLSForRemoteHost
	}
	return sslMode, nil
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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

func decodeMetadata(record vaultitem.Record) (metadata, error) {
	var meta metadata
	if err := json.Unmarshal(record.Metadata, &meta); err != nil {
		return metadata{}, err
	}
	return meta, nil
}

func summaryFromRecord(record vaultitem.Record) (ProfileSummary, error) {
	meta, err := decodeMetadata(record)
	if err != nil {
		return ProfileSummary{}, err
	}
	return ProfileSummary{
		ID:       record.ID,
		Name:     meta.Name,
		Host:     meta.Host,
		Port:     meta.Port,
		Database: meta.Database,
		Username: meta.Username,
		SSLMode:  meta.SSLMode,
		Env:      meta.Env,
		ReadOnly: meta.ReadOnly,
	}, nil
}

func zeroBytes(value []byte) {
	for i := range value {
		value[i] = 0
	}
}
