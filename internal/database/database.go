package database

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"sort"
	"strings"
	"sync"

	"devkit/internal/vaultitem"
)

const (
	passwordKeyID = "db-password"
	itemType      = "db-profile"
)

// ErrInsecureTLSForRemoteHost names the way out, not only the refusal: a host
// reached over an already-encrypted network may go without TLS if the profile
// says so.
var ErrInsecureTLSForRemoteHost = errors.New("insecure TLS is only allowed for loopback database hosts, or for a profile marked as reached over an encrypted network (Tailscale, VPN)")

// ErrUnknownSSLMode is an SSL mode outside disable, require, verify-ca and
// verify-full — including the ones that fall back to plaintext.
var ErrUnknownSSLMode = errors.New("SSL mode must be disable, require, verify-ca or verify-full")

// ErrInvalidCACertificate means the supplied CA bundle held no usable
// certificate. Reported rather than ignored: a bundle that silently does
// nothing leaves a connection that reads as verified and is not.
var ErrInvalidCACertificate = errors.New("CA certificate is not valid PEM")

// ErrCACertificateWithoutTLS means a CA bundle was supplied under an SSL mode
// that never verifies one. The profile would read as pinned to an authority it
// does not actually check, so the combination is refused instead.
var ErrCACertificateWithoutTLS = errors.New("a CA certificate needs an SSL mode that verifies it (verify-full)")

// DefaultMaxRows caps query results so the UI stays responsive (§12.4). It is
// what a caller that names no cap of its own gets.
const DefaultMaxRows = 200

// MaxRowsCeiling is the most any caller may ask for. A page size is a choice
// made in the UI, and a choice made in the UI must not be able to ask the
// driver for every row of a hundred-million-row table: the grid virtualises
// rendering, not memory, and the whole result is marshalled to strings and
// carried over the bridge before it ever reaches the grid.
const MaxRowsCeiling = 5000

// EnvLookup is the narrow view of the environments registry the database
// service needs to validate a profile's env tag. Declared here (the
// consumer), not by internal/environments — README.md "Accept interfaces,
// return structs".
type EnvLookup interface {
	Exists(name string) bool
}

// ProfileSummary is a connection profile without the (encrypted) password.
type ProfileSummary struct {
	ParentProfileID string `json:"parent_profile_id,omitempty"`
	ID              string `json:"id"`
	Name            string `json:"name"`
	Host            string `json:"host"`
	Port            int    `json:"port"`
	Database        string `json:"database"`
	Username        string `json:"username"`
	SSLMode         string `json:"ssl_mode"`
	// TrustedNetwork allows ssl_mode "disable" to a non-loopback host, for a
	// network that is already encrypted end to end (Tailscale, WireGuard, a
	// VPN). The app cannot tell such a network from the open internet, so it
	// is the user's statement, made per profile, never a guess.
	TrustedNetwork bool   `json:"trusted_network,omitempty"`
	Env            string `json:"env"`
	ReadOnly       bool   `json:"read_only"`
	// SSHProfileID is optional. When set, Host/Port are addresses as seen from
	// the SSH server, not from this machine.
	SSHProfileID string `json:"ssh_profile_id,omitempty"`
	// Engine is empty on profiles created before multi-engine support; those
	// are all Postgres, which is why it defaults rather than being required.
	Engine string `json:"engine,omitempty"`
	// DriverOptions carries engine-specific parameters that do not generalise.
	DriverOptions map[string]string `json:"driver_options,omitempty"`
	// CACert is a PEM bundle used to verify the server certificate instead of
	// the machine's trust store. Plaintext metadata, not a payload: a CA
	// certificate is public by construction — it is what everyone checks
	// against, not something to hide.
	CACert string `json:"ca_cert,omitempty"`
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
	// TrustedNetwork allows ssl_mode "disable" to a non-loopback host, for a
	// network that is already encrypted end to end (Tailscale, WireGuard, a
	// VPN). The app cannot tell such a network from the open internet, so it
	// is the user's statement, made per profile, never a guess.
	TrustedNetwork bool   `json:"trusted_network,omitempty"`
	Env            string `json:"env"`
	// SSHProfileID is accepted at creation so a tunnelled profile never exists
	// with a direct connection path, however briefly.
	SSHProfileID string `json:"ssh_profile_id"`
	// CACert is a PEM bundle used to verify the server certificate instead of
	// the machine's trust store. Plaintext metadata, not a payload: a CA
	// certificate is public by construction — it is what everyone checks
	// against, not something to hide.
	CACert string `json:"ca_cert,omitempty"`
}

type UpdateProfileOptionsInput struct {
	Env      string `json:"env"`
	ReadOnly bool   `json:"read_only"`
	// TrustedNetwork is the same statement CreateProfileInput carries. Editable
	// here because a profile refused for sslmode=disable is fixed from this
	// dialog, not by deleting it and typing it again.
	TrustedNetwork bool `json:"trusted_network"`
	// SSHProfileID empty means connect directly.
	SSHProfileID string `json:"ssh_profile_id"`
	// CACert is a PEM bundle used to verify the server certificate instead of
	// the machine's trust store. Plaintext metadata, not a payload: a CA
	// certificate is public by construction — it is what everyone checks
	// against, not something to hide.
	CACert string `json:"ca_cert,omitempty"`
}

type QueryResult struct {
	Columns []string `json:"columns"`
	// Rows holds each cell as text, null for SQL NULL (#47).
	Rows      [][]*string `json:"rows"`
	RowCount  int         `json:"row_count"`
	Truncated bool        `json:"truncated"`
	// RowsAffected is what an UPDATE, INSERT or DELETE changed; -1 when the
	// driver cannot say. An edit that matched nothing reads 0 here, which is
	// how it stops reporting success (#47).
	RowsAffected int64 `json:"rows_affected"`
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
	ParentProfileID string `json:"parent_profile_id,omitempty"`
	Name            string `json:"name"`
	Host            string `json:"host"`
	Port            int    `json:"port"`
	Database        string `json:"database"`
	Username        string `json:"username"`
	SSLMode         string `json:"ssl_mode"`
	// TrustedNetwork allows ssl_mode "disable" to a non-loopback host, for a
	// network that is already encrypted end to end (Tailscale, WireGuard, a
	// VPN). The app cannot tell such a network from the open internet, so it
	// is the user's statement, made per profile, never a guess.
	TrustedNetwork bool   `json:"trusted_network,omitempty"`
	Env            string `json:"env"`
	ReadOnly       bool   `json:"read_only"`
	// SSHProfileID is optional. When set, Host/Port are addresses as seen from
	// the SSH server, not from this machine.
	SSHProfileID string `json:"ssh_profile_id,omitempty"`
	// Engine is empty on profiles created before multi-engine support; those
	// are all Postgres, which is why it defaults rather than being required.
	Engine string `json:"engine,omitempty"`
	// DriverOptions carries engine-specific parameters that do not generalise.
	DriverOptions map[string]string `json:"driver_options,omitempty"`
	// CACert is a PEM bundle used to verify the server certificate instead of
	// the machine's trust store. Plaintext metadata, not a payload: a CA
	// certificate is public by construction — it is what everyone checks
	// against, not something to hide.
	CACert string `json:"ca_cert,omitempty"`
}

type Service struct {
	store         Store
	credentials   CredentialStore
	envs          EnvLookup
	mu            sync.Mutex
	agentActiveID string
	pools         *PoolManager
	maxRows       int
	history       QueryHistoryRepository
	sealer        Sealer
	schemaCache   SchemaCacheRepository
	consoles      *ConsoleStore
	txs           *transactions
	sessions      *sessions
}

func NewService(store Store, envs EnvLookup) *Service {
	return &Service{
		store:    store,
		envs:     envs,
		pools:    NewPoolManager(),
		maxRows:  DefaultMaxRows,
		txs:      newTransactions(),
		sessions: newSessions(),
	}
}

// normalizeCreateInput applies the same rules to a profile whether it is about
// to be saved or only tested, so a connection that tests green cannot fail
// validation a second later on save.
func (s *Service) normalizeCreateInput(input CreateProfileInput) (CreateProfileInput, error) {
	if input.Name == "" {
		return input, errors.New("name is required")
	}
	if input.Host == "" {
		return input, errors.New("host is required")
	}
	if input.Env == "" {
		return input, errors.New("env is required")
	}
	if !s.envs.Exists(input.Env) {
		return input, fmt.Errorf("unknown env tag %q", input.Env)
	}
	if input.Engine == "" {
		input.Engine = EnginePostgres
	}
	if _, err := driverFor(input.Engine); err != nil {
		return input, err
	}
	if input.Port == 0 {
		input.Port = DefaultPort(input.Engine)
	}
	sslMode, err := normalizeSSLMode(input.Host, input.SSLMode, input.TrustedNetwork)
	if err != nil {
		return input, err
	}
	input.SSLMode = sslMode

	// Checked here so a mistyped or half-pasted certificate is refused by the
	// dialog that typed it, rather than surfacing as a TLS handshake failure
	// the first time someone runs a query.
	input.CACert = strings.TrimSpace(input.CACert)
	if err := validateCACert(input.CACert, input.SSLMode); err != nil {
		return input, err
	}
	return input, nil
}

// validateCACert rejects a bundle that cannot be parsed, and one supplied under
// a mode that would never look at it.
func validateCACert(caCert, sslMode string) error {
	if caCert == "" {
		return nil
	}
	if sslMode != "verify-full" && sslMode != "verify-ca" {
		return ErrCACertificateWithoutTLS
	}
	if _, err := rootCAPool(caCert); err != nil {
		return err
	}
	return nil
}

// TestNewConnection dials a profile that does not exist yet. Nothing is
// written: no vault item, and no pool entry — there is no profile id to cache
// under, and a throwaway probe should not outlive the dialog that made it.
func (s *Service) TestNewConnection(ctx context.Context, input CreateProfileInput) error {
	input, err := s.normalizeCreateInput(input)
	if err != nil {
		return err
	}
	driver, err := driverFor(input.Engine)
	if err != nil {
		return err
	}
	cfg := ConnConfig{
		Engine: input.Engine, Host: input.Host, Port: input.Port,
		Database: defaultDatabase(input.Engine, input.Database), Username: input.Username,
		Password: input.Password, SSLMode: input.SSLMode,
		RootCAPEM: input.CACert,
	}
	if input.SSHProfileID != "" {
		handle, err := s.pools.openProbeTunnel(ctx, TunnelSpec{
			SSHProfileID: input.SSHProfileID,
			TargetHost:   input.Host,
			TargetPort:   input.Port,
		})
		if err != nil {
			return err
		}
		defer s.pools.closeProbeTunnel(handle.ID)
		cfg.TLSServerName = cfg.Host
		cfg.Host = handle.LocalHost
		cfg.Port = handle.LocalPort
	}
	ctx, cancel := queryContext(ctx)
	defer cancel()
	conn, err := driver.Dial(ctx, cfg)
	if err != nil {
		return err
	}
	defer conn.Close()
	return conn.Ping(ctx)
}

func (s *Service) CreateProfile(input CreateProfileInput) (ProfileSummary, error) {
	input, err := s.normalizeCreateInput(input)
	if err != nil {
		return ProfileSummary{}, err
	}

	meta, err := json.Marshal(metadata{
		Engine:       input.Engine,
		SSHProfileID: input.SSHProfileID,
		Name:         input.Name,
		Host:         input.Host,
		Port:         input.Port,
		Database:     input.Database,
		Username:     input.Username,
		SSLMode:      input.SSLMode,
		Env:          input.Env,
		CACert:       input.CACert,
		// Kept only where it does something: on any other mode the path is
		// encrypted anyway, and a stored flag would read as a promise.
		TrustedNetwork: input.TrustedNetwork && input.SSLMode == "disable",
	})
	if err != nil {
		return ProfileSummary{}, err
	}
	// The password goes to the credential store when there is one, and the
	// profile keeps an empty payload: metadata syncs, the secret does not.
	payload := []byte(input.Password)
	if s.credentials != nil {
		payload = []byte{}
	}
	record, err := s.store.Create(itemType, passwordKeyID, meta, payload)
	if err != nil {
		return ProfileSummary{}, err
	}
	if s.credentials != nil {
		// The profile exists but has no usable credential until this lands, so
		// a failure here deletes it rather than leaving one that cannot
		// connect and gives no reason why.
		if err := s.credentials.PutForOwner(record.ID, ProviderID, input.Name, input.Username, input.Password); err != nil {
			_ = s.store.Delete(record.ID)
			return ProfileSummary{}, err
		}
	}
	return s.profileSummary(record)
}

func (s *Service) ListProfiles() ([]ProfileSummary, error) {
	records, err := s.store.ListByType(itemType)
	if err != nil {
		return nil, err
	}
	summaries := make([]ProfileSummary, 0, len(records))
	for _, record := range records {
		summary, err := s.profileSummary(record)
		if err != nil {
			meta, decodeErr := decodeMetadata(record)
			if decodeErr == nil && meta.ParentProfileID != "" && errors.Is(err, vaultitem.ErrNotFound) {
				continue
			}
			return nil, err
		}
		summaries = append(summaries, summary)
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Name < summaries[j].Name
	})
	return summaries, nil
}

// ApprovalRevision is an opaque generation for the saved endpoint and options.
// UpdatedAt changes on metadata or encrypted credential edits, without exposing
// the profile body or secret to the approval store.
func (s *Service) ApprovalRevision(id string) (string, error) {
	record, err := s.store.Get(id)
	if err != nil {
		return "", err
	}
	if record.Type != itemType {
		return "", vaultitem.ErrNotFound
	}
	meta, err := decodeMetadata(record)
	if err != nil {
		return "", err
	}
	parentRevision := ""
	if meta.ParentProfileID != "" {
		parent, err := s.store.Get(meta.ParentProfileID)
		if err != nil {
			return "", err
		}
		parentRevision = fmt.Sprintf("%s:%d", parent.ID, parent.UpdatedAt.UnixNano())
	}
	profiles, err := s.ListProfiles()
	if err != nil {
		return "", err
	}
	for _, profile := range profiles {
		if profile.ID == id {
			canonical, err := json.Marshal(profile)
			if err != nil {
				return "", err
			}
			sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%s:%s", record.ID, record.UpdatedAt.UnixNano(), parentRevision, canonical)))
			return hex.EncodeToString(sum[:]), nil
		}
	}
	return "", vaultitem.ErrNotFound
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
	// A changed CA bundle is a changed TLS config, which is built when the pool
	// dials — so like read_only it has to drop the pool, not only the metadata.
	trustedNetwork := opts.TrustedNetwork && meta.SSLMode == "disable"
	poolChanged := meta.ReadOnly != opts.ReadOnly ||
		meta.TrustedNetwork != trustedNetwork ||
		meta.SSHProfileID != opts.SSHProfileID ||
		meta.CACert != strings.TrimSpace(opts.CACert)

	if meta.ParentProfileID != "" {
		return ProfileSummary{}, errors.New("change the parent connection options")
	}
	// The mode is not editable here, so the bundle is checked against the one
	// the profile already carries.
	caCert := strings.TrimSpace(opts.CACert)
	if err := validateCACert(caCert, meta.SSLMode); err != nil {
		return ProfileSummary{}, err
	}

	meta.Env = opts.Env
	meta.ReadOnly = opts.ReadOnly
	meta.TrustedNetwork = trustedNetwork
	meta.SSHProfileID = opts.SSHProfileID
	meta.CACert = caCert

	rawMeta, err := json.Marshal(meta)
	if err != nil {
		return ProfileSummary{}, err
	}

	updatedRecord, err := s.store.UpdateMetadata(id, rawMeta)
	if err != nil {
		return ProfileSummary{}, err
	}

	if poolChanged {
		// The transaction goes with the connection it was pinned to: keeping
		// it would leave a Tx holding a session on a Conn nobody can reach.
		s.rollbackTx(id)
		s.closeConnection(id)
		children, err := s.databaseChildren(id)
		if err != nil {
			return ProfileSummary{}, err
		}
		for _, child := range children {
			s.rollbackTx(child)
			s.closeConnection(child)
		}
	}

	return s.profileSummary(updatedRecord)
}

func (s *Service) DeleteProfile(id string) error {
	children, err := s.databaseChildren(id)
	if err != nil {
		return err
	}
	for _, child := range children {
		if err := s.DeleteProfile(child); err != nil {
			return err
		}
	}

	if err := s.store.Delete(id); err != nil {
		return err
	}
	s.rollbackTx(id)
	s.closeConnection(id)
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
// Close ends every open transaction before dropping the connections they are
// pinned to. Rolling back rather than committing: work nobody confirmed is
// work nobody asked to keep.
func (s *Service) Close() {
	// Bounded: on the way out, with the network possibly gone, an unbounded
	// rollback is a quit that never finishes (#42).
	for _, tx := range s.txs.takeAll() {
		ctx, cancel := context.WithTimeout(context.Background(), rollbackTimeout)
		_ = tx.Rollback(ctx)
		cancel()
	}
	s.sessions.close(func(sessionKey) bool { return true })
	s.pools.Close()
}

// TestConnection decrypts the password, connects, and pings the database.
func (s *Service) TestConnection(ctx context.Context, id string) error {
	target, err := s.target(id)
	if err != nil {
		return err
	}
	parent := ctx
	ctx, cancel := queryContext(ctx)
	defer cancel()

	pool, err := s.pool(ctx, id, target)
	if err != nil {
		return err
	}
	err = pool.Ping(ctx)
	s.dropIfDisconnected(parent, id, err)
	return err
}

// Query runs SQL against the profile's database and returns rows (capped).
//
// maxRows is the caller's cap. Zero or less means the service default, which
// is what every caller that does not page sends; anything above MaxRowsCeiling
// is clamped to it rather than refused, because a page size is a preference
// and refusing one would fail a query over a number the user can only guess at.
func (s *Service) Query(ctx context.Context, id string, sql string, maxRows int) (QueryResult, error) {
	return s.QueryInSession(ctx, id, "", sql, maxRows)
}

// QueryInSession runs sql on the session kept for sessionID — a console — so
// what one statement leaves on its connection is there for the next (#48). An
// empty sessionID runs on the pool, as a table tab's page does.
func (s *Service) QueryInSession(ctx context.Context, id, sessionID, sql string, maxRows int) (QueryResult, error) {
	result, err := s.query(ctx, id, sessionID, sql, maxRows)
	s.dropIfDisconnected(ctx, id, err)
	return result, err
}

/*
maxResultBytes caps a result by what it holds, not only by how many rows.

5,000 rows of multi-megabyte text or bytea were all held here and sent over the
bridge. The cap cuts at a row boundary rather than inside a cell: a cell cut
short would go on to be an UPDATE's key or a value written back (#46).
*/
const maxResultBytes = 64 << 20

func (s *Service) query(ctx context.Context, id, sessionID, sql string, maxRows int) (QueryResult, error) {
	target, err := s.target(id)
	if err != nil {
		return QueryResult{}, err
	}
	// No deadline of its own (#46). A statement was cut off at 30s — a COMMIT,
	// a CREATE INDEX, a report — while Stop, which now reaches the server, is
	// how a person ends one. Opening the connection is still bounded, in the
	// pool, and an agent's call is bounded by the MCP tool.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	pool, err := s.pool(ctx, id, target)
	if err != nil {
		return QueryResult{}, err
	}

	limit := s.maxRows
	if maxRows > 0 {
		limit = min(maxRows, MaxRowsCeiling)
	}

	// Inside a transaction when the profile commits by hand, opening one here
	// if this is the first statement since the last commit. That laziness is
	// the whole reason the mode lives in the service: an open transaction
	// holds a metadata lock on every table it touches, and choosing "Manual"
	// and then reading nothing must not hold anything at all.
	tx, err := s.txs.ensure(ctx, id, pool)
	if err != nil {
		return QueryResult{}, err
	}
	// A manual transaction wins over the console's session: it is the
	// profile's one connection for as long as it is open, whichever console
	// runs the statement.
	var rows Rows
	if tx != nil {
		rows, err = tx.Query(ctx, sql)
	} else {
		opener, _ := pool.(SessionOpener)
		key := sessionKey{profileID: id, sessionID: sessionID}
		entry, openErr := s.sessions.acquire(ctx, key, opener)
		if openErr != nil {
			return QueryResult{}, openErr
		}
		if entry != nil {
			defer s.sessions.release(key, entry)
			rows, err = entry.session.Query(ctx, sql)
		} else {
			rows, err = pool.Query(ctx, sql)
		}
	}
	// Marked from here down: everything below is the engine answering the SQL
	// the caller typed, and the bridge redacts a database error unless it is
	// told that is what it is. See StatementError.
	if err != nil {
		return QueryResult{}, NewStatementError(err)
	}
	defer rows.Close()

	result := QueryResult{Columns: rows.Columns(), Rows: [][]*string{}}
	size := 0
	for rows.Next() {
		if result.RowCount >= limit || size >= maxResultBytes {
			result.Truncated = true
			break
		}
		// Values are already strings: each driver stringifies, so one result
		// shape serves every engine.
		row, err := rows.Values()
		if err != nil {
			return QueryResult{}, NewStatementError(err)
		}
		for _, cell := range row {
			if cell != nil {
				size += len(*cell)
			}
		}
		result.Rows = append(result.Rows, row)
		result.RowCount++
	}
	// Stopped, not drained (#46). Closing rows reads the rest of the result to
	// the end in both drivers, so a SELECT * over a large table kept the server
	// and the network busy for rows nobody would see. Not inside a transaction:
	// cancelling there takes the connection, and the transaction with it.
	if result.Truncated && tx == nil {
		cancel()
	}
	// Where a statement that started fine ends badly — a division by zero on
	// row 900, a cast that only the tenth value fails. pgx reports those on
	// the drain, not on Query.
	if err := rows.Err(); err != nil && !result.Truncated {
		return QueryResult{}, NewStatementError(err)
	}
	// Closed here rather than by the defer, because the count is only known
	// once the statement has finished. Close is safe to call twice.
	rows.Close()
	result.RowsAffected = rows.RowsAffected()
	s.recordHistory(id, sql)
	s.refreshSchemaAfterDDL(id, sql)
	return result, nil
}

/*
dropIfDisconnected discards a profile's cached connection after an error the
server did not send (#46).

pgxpool and database/sql replace a broken connection on their own, but not the
SSH forward under it, and not a pool whose every dial fails: a connection that
could not log in stayed cached, tunnel included, until the profile was edited.
An error the server answered with — a syntax error, a denied login — says the
connection works. A cancelled call says nothing about it. And a connection
holding a transaction is never dropped from under it here.
*/
func (s *Service) dropIfDisconnected(ctx context.Context, id string, err error) {
	if err == nil || ctx.Err() != nil || answeredByServer(err) || s.txs.get(id) != nil {
		return
	}
	s.closeConnection(id)
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
	if meta.ParentProfileID != "" {
		parent, err := s.store.Get(meta.ParentProfileID)
		if err != nil {
			return connectionTarget{}, err
		}
		if parent.Type != itemType {
			return connectionTarget{}, errors.New("invalid parent profile")
		}
		parentMeta, err := decodeMetadata(parent)
		if err != nil {
			return connectionTarget{}, err
		}
		if parentMeta.ParentProfileID != "" {
			return connectionTarget{}, errors.New("nested database context is invalid")
		}
		target, err := s.target(meta.ParentProfileID)
		if err != nil {
			return connectionTarget{}, err
		}
		target.cfg.Database = meta.Database
		return target, nil
	}
	password, err := s.secretFor(record)
	if err != nil {
		return connectionTarget{}, err
	}
	// Match the SSH/Kafka hygiene: wipe the decrypted bytes once the config
	// copy is built.
	defer zeroBytes(password)
	sslmode, err := normalizeSSLMode(meta.Host, meta.SSLMode, meta.TrustedNetwork)
	if err != nil {
		return connectionTarget{}, err
	}
	return connectionTarget{
		cfg: ConnConfig{
			Engine:    meta.Engine,
			Host:      meta.Host,
			Port:      meta.Port,
			Database:  defaultDatabase(meta.Engine, meta.Database),
			Username:  meta.Username,
			Password:  string(password),
			SSLMode:   sslmode,
			ReadOnly:  meta.ReadOnly,
			Options:   meta.DriverOptions,
			RootCAPEM: meta.CACert,
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

// normalizeSSLMode accepts the modes whose answer is a yes or a no.
//
// An allowlist (#45): only "disable" used to be refused for a remote host, and
// "prefer" and "allow" — which the UI never offers but a synced profile or the
// bridge can still send — try TLS and quietly carry on in plaintext when it is
// refused, which is the downgrade an attacker on the path asks for.
//
// trustedNetwork lets "disable" reach a remote host: the user has said the
// path is already encrypted (Tailscale, a VPN), so TLS on top buys nothing.
// It does not change the empty default — that still asks for verify-full.
func normalizeSSLMode(host, sslMode string, trustedNetwork bool) (string, error) {
	switch sslMode {
	case "":
		if isLoopbackHost(host) {
			return "disable", nil
		}
		return "verify-full", nil
	case "disable":
		if !isLoopbackHost(host) && !trustedNetwork {
			return "", ErrInsecureTLSForRemoteHost
		}
		return sslMode, nil
	case "require", "verify-ca", "verify-full":
		return sslMode, nil
	default:
		return "", ErrUnknownSSLMode
	}
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// stringify is a driver value as text. SQL NULL is the caller's to handle: it
// is not a string, and "NULL" is (#47).
func stringify(value any) string {
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
		ID:             record.ID,
		Name:           meta.Name,
		Host:           meta.Host,
		Port:           meta.Port,
		Database:       meta.Database,
		Username:       meta.Username,
		SSLMode:        meta.SSLMode,
		Env:            meta.Env,
		ReadOnly:       meta.ReadOnly,
		TrustedNetwork: meta.TrustedNetwork,
		// Engine and the tunnel were stored but never returned, so every caller
		// saw a Postgres profile with no tunnel no matter what was saved.
		Engine:        meta.Engine,
		SSHProfileID:  meta.SSHProfileID,
		DriverOptions: meta.DriverOptions,
		CACert:        meta.CACert,
	}, nil
}

func zeroBytes(value []byte) {
	for i := range value {
		value[i] = 0
	}
}

func defaultDatabase(engine, name string) string {
	if name == "" && (engine == "" || engine == EnginePostgres) {
		return "postgres"
	}
	return name
}
