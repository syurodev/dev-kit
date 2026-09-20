package accounts

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"devkit/internal/vaultitem"
)

// ItemStore is the vault-item surface a shared account needs, declared here so
// this package accepts an interface rather than importing a concrete store.
type ItemStore interface {
	Create(itemType, keyID string, metadata json.RawMessage, secret []byte) (vaultitem.Record, error)
	Get(id string) (vaultitem.Record, error)
	ListByType(itemType string) ([]vaultitem.Record, error)
	UpdateMetadata(id string, metadata json.RawMessage) (vaultitem.Record, error)
	Update(id, keyID string, metadata json.RawMessage, secret []byte) (vaultitem.Record, error)
	OpenPayload(record vaultitem.Record, keyID string) ([]byte, error)
	Delete(id string) error
}

// EnvLookup is the environment registry as this package needs it — the same
// locally-declared boundary ssh, database and kafka keep.
type EnvLookup interface {
	Exists(name string) bool
}

// Usage reports where an account is bound, so one in use cannot be deleted
// without saying by what. Each module that binds accounts registers one.
type Usage interface {
	// UsedBy returns human-readable descriptions of what binds accountID.
	UsedBy(accountID string) []string
}

// Service owns every credential DevKit holds.
type Service struct {
	items     ItemStore
	scoped    *scopedStore
	providers *Registry
	env       EnvLookup
	usage     []Usage
	http      *http.Client
	now       func() time.Time
}

// NewService wires the two stores behind one surface. db may be nil in tests
// that only exercise shared accounts.
func NewService(items ItemStore, db *sql.DB, table string, vault Vault, providers *Registry, env EnvLookup) *Service {
	svc := &Service{items: items, providers: providers, env: env, now: time.Now}
	if db != nil {
		svc.scoped = &scopedStore{db: db, table: table, vault: vault}
	}
	return svc
}

// RegisterUsage adds a module that can say what it has bound.
func (s *Service) RegisterUsage(u Usage) {
	if u != nil {
		s.usage = append(s.usage, u)
	}
}

// CreateInput is a new account. Secret is the value; it is never stored in
// metadata and never returned.
type CreateInput struct {
	Provider string
	Kind     Kind
	Label    string
	User     string
	Secret   string
	Env      []string
	// ScopeOwner set makes this a scoped account: it goes to the local table
	// and does not sync.
	ScopeOwner string
	Ref        string
	ExpiresAt  time.Time
}

// Create stores a new account. A scoped one goes to the local table, a shared
// one to vault_items.
func (s *Service) Create(in CreateInput) (Account, error) {
	if !ValidKind(in.Kind) {
		return Account{}, ErrUnknownKind
	}
	if strings.TrimSpace(in.Label) == "" || strings.TrimSpace(in.Provider) == "" {
		return Account{}, ErrInvalid
	}
	if _, ok := s.providers.Get(in.Provider); !ok {
		return Account{}, ErrInvalid
	}
	for _, name := range in.Env {
		if s.env != nil && !s.env.Exists(name) {
			return Account{}, ErrInvalid
		}
	}
	if in.ScopeOwner != "" {
		return s.createScoped(in)
	}
	return s.createShared(in)
}

func (s *Service) createScoped(in CreateInput) (Account, error) {
	if s.scoped == nil {
		return Account{}, ErrInvalid
	}
	if len(in.Env) > 0 {
		// A scoped credential belongs to one connection, and that connection
		// already carries the env tag. Two answers to one question is how they
		// drift apart.
		return Account{}, ErrInvalid
	}
	// Reuse the existing row's id when this owner already has one.
	//
	// The upsert keys on scope_owner and leaves the id alone, but the envelope
	// is sealed with the id as part of its AAD. Minting a fresh id here sealed
	// the new secret against an id the row would never carry, so the next read
	// failed authentication — a rotation that silently destroyed the
	// credential it was rotating.
	id := ""
	if existing, err := s.scoped.byOwner(in.ScopeOwner); err == nil {
		id = existing.ID
	} else if !errors.Is(err, ErrNotFound) {
		return Account{}, err
	}
	if id == "" {
		fresh, err := vaultitem.NewID()
		if err != nil {
			return Account{}, err
		}
		id = fresh
	}
	row := scopedRow{
		ID: id, ScopeOwner: in.ScopeOwner, Provider: in.Provider,
		Kind: in.Kind, Label: in.Label, Username: in.User,
	}
	if err := s.scoped.upsert(row, in.Secret); err != nil {
		return Account{}, err
	}
	stored, err := s.scoped.byOwner(in.ScopeOwner)
	if err != nil {
		return Account{}, err
	}
	return stored.account(s.now()), nil
}

func (s *Service) createShared(in CreateInput) (Account, error) {
	m := metadata{
		Provider: in.Provider, Kind: string(in.Kind), Label: in.Label,
		Env: in.Env, State: string(StateUnverified), Ref: in.Ref,
		ExpiresAt: formatTime(in.ExpiresAt),
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return Account{}, err
	}
	// KindExternal holds nothing, but a vault item must carry a valid
	// envelope, so it seals an empty payload rather than becoming a special
	// case in the sync validator.
	secret := []byte(in.Secret)
	if in.Kind == KindExternal {
		secret = []byte{}
	}
	if in.User != "" {
		secret = []byte(in.User + "\x00" + in.Secret)
	}
	record, err := s.items.Create(vaultitem.ItemTypeAccount, KeyID, raw, secret)
	if err != nil {
		return Account{}, err
	}
	return s.decorate(m.account(record.ID, record.CreatedAt, record.UpdatedAt)), nil
}

// List returns every account, shared and scoped, without any value.
func (s *Service) List() ([]Account, error) {
	records, err := s.items.ListByType(vaultitem.ItemTypeAccount)
	if err != nil {
		return nil, err
	}
	out := make([]Account, 0, len(records))
	for _, record := range records {
		account, err := decodeMetadata(record)
		if err != nil {
			continue // a record this build cannot read is not a reason to show nothing
		}
		out = append(out, s.decorate(account))
	}
	if s.scoped != nil {
		rows, err := s.scoped.list()
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			out = append(out, row.account(s.now()))
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Scoped() != out[j].Scoped() {
			return !out[i].Scoped() // shared first; scoped is the collapsed group
		}
		return out[i].Label < out[j].Label
	})
	return out, nil
}

// Get returns one account without its value.
func (s *Service) Get(id string) (Account, error) {
	record, err := s.items.Get(id)
	if err == nil {
		account, decodeErr := decodeMetadata(record)
		if decodeErr != nil {
			return Account{}, decodeErr
		}
		return s.decorate(account), nil
	}
	if s.scoped != nil {
		if row, scopedErr := s.scoped.byID(id); scopedErr == nil {
			return row.account(s.now()), nil
		}
	}
	return Account{}, ErrNotFound
}

// ForOwner returns the scoped account of a profile, or StateNeedsLocalSecret
// when this machine does not have it. That second case is the whole reason
// the link is the owner rather than a stored id: the profile synced, the
// secret did not, and the answer is a sentence rather than a dangling id.
func (s *Service) ForOwner(owner string) (Account, error) {
	if s.scoped == nil || owner == "" {
		return Account{}, ErrNotFound
	}
	row, err := s.scoped.byOwner(owner)
	if errors.Is(err, ErrNotFound) {
		return Account{ScopeOwner: owner, State: StateNeedsLocalSecret}, nil
	}
	if err != nil {
		return Account{}, err
	}
	return row.account(s.now()), nil
}

// ResolveInput asks for a credential at the point of use.
type ResolveInput struct {
	// AccountID names a shared account.
	AccountID string
	// ScopeOwner names a profile whose scoped account to use. Exactly one of
	// the two is set.
	ScopeOwner string
	// Env is the environment tag the call is running against, checked against
	// the account's restriction.
	Env string
	// By is the module asking, for audit.
	By string
}

// Resolve returns a credential, in memory, for the call about to use it.
// Nothing it returns crosses the bridge.
func (s *Service) Resolve(in ResolveInput) (Credential, error) {
	if in.ScopeOwner != "" {
		if s.scoped == nil {
			return Credential{}, ErrNotFound
		}
		row, err := s.scoped.byOwner(in.ScopeOwner)
		if err != nil {
			return Credential{}, err
		}
		secret, err := s.scoped.open(row)
		if err != nil {
			return Credential{}, err
		}
		return Credential{Kind: row.Kind, User: row.Username, Secret: secret}, nil
	}
	record, err := s.items.Get(in.AccountID)
	if err != nil {
		return Credential{}, ErrNotFound
	}
	account, err := decodeMetadata(record)
	if err != nil {
		return Credential{}, err
	}
	if !account.AllowsEnv(in.Env) {
		return Credential{}, ErrEnvNotAllowed
	}
	if account.Kind == KindExternal {
		// Nothing to hand back, by design: this row records that an identity
		// exists somewhere DevKit does not hold it.
		return Credential{}, ErrNotFound
	}
	plaintext, err := s.items.OpenPayload(record, KeyID)
	if err != nil {
		return Credential{}, err
	}
	user, secret := splitCredential(string(plaintext))
	return Credential{Kind: account.Kind, User: user, Secret: secret}, nil
}

func splitCredential(raw string) (user, secret string) {
	if idx := strings.IndexByte(raw, 0); idx >= 0 {
		return raw[:idx], raw[idx+1:]
	}
	return "", raw
}

// AllowsEnv reports whether the account may be used against an environment
// tag. No restriction means anywhere.
func (a Account) AllowsEnv(env string) bool {
	if len(a.Env) == 0 {
		return true
	}
	for _, allowed := range a.Env {
		if allowed == env {
			return true
		}
	}
	return false
}

// Rotate replaces an account's value in place, so every binding keeps working.
// This is why bindings hold an id and not a value.
func (s *Service) Rotate(id, user, secret string) error {
	record, err := s.items.Get(id)
	if err == nil {
		account, decodeErr := decodeMetadata(record)
		if decodeErr != nil {
			return decodeErr
		}
		if account.Kind == KindExternal {
			return ErrInvalid
		}
		payload := []byte(secret)
		if user != "" {
			payload = []byte(user + "\x00" + secret)
		}
		// Rotating invalidates what verification last said about the value.
		m := metadataOf(account)
		m.State = string(StateUnverified)
		m.VerifiedAt = ""
		raw, marshalErr := json.Marshal(m)
		if marshalErr != nil {
			return marshalErr
		}
		_, err = s.items.Update(id, KeyID, raw, payload)
		return err
	}
	if s.scoped == nil {
		return ErrNotFound
	}
	row, err := s.scoped.byID(id)
	if err != nil {
		return err
	}
	row.Username = user
	return s.scoped.upsert(row, secret)
}

// Delete removes an account, refusing one something still binds.
func (s *Service) Delete(id string) error {
	if used := s.UsedBy(id); len(used) > 0 {
		return &InUseError{AccountID: id, UsedBy: used}
	}
	if _, err := s.items.Get(id); err == nil {
		return s.items.Delete(id)
	}
	if s.scoped == nil {
		return ErrNotFound
	}
	return s.scoped.deleteByID(id)
}

// DeleteForOwner removes the scoped credential of a profile being deleted.
func (s *Service) DeleteForOwner(owner string) error {
	if s.scoped == nil {
		return nil
	}
	return s.scoped.deleteByOwner(owner)
}

// UsedBy asks every registered module what binds this account.
func (s *Service) UsedBy(id string) []string {
	var out []string
	for _, u := range s.usage {
		out = append(out, u.UsedBy(id)...)
	}
	return out
}

// InUseError names what still binds an account, so the refusal is actionable.
type InUseError struct {
	AccountID string
	UsedBy    []string
}

func (e *InUseError) Error() string {
	return "accounts.inUse: " + strings.Join(e.UsedBy, ", ")
}

// Promote turns a scoped credential into a shared one: the only way a
// connection's password starts syncing, and a deliberate act rather than a
// default. There is no demote — one that three things use would break three
// things.
func (s *Service) Promote(id, label string) (Account, error) {
	if s.scoped == nil {
		return Account{}, ErrNotFound
	}
	row, err := s.scoped.byID(id)
	if err != nil {
		return Account{}, err
	}
	secret, err := s.scoped.open(row)
	if err != nil {
		return Account{}, err
	}
	if label == "" {
		label = row.Label
	}
	shared, err := s.createShared(CreateInput{
		Provider: row.Provider, Kind: row.Kind, Label: label,
		User: row.Username, Secret: secret,
	})
	if err != nil {
		return Account{}, err
	}
	if err := s.scoped.deleteByID(id); err != nil {
		return Account{}, err
	}
	return shared, nil
}

// SweepOrphans removes local scoped credentials whose owning profile is gone —
// usually deleted on another device and synced here.
func (s *Service) SweepOrphans(liveOwners []string) (int, error) {
	if s.scoped == nil {
		return 0, nil
	}
	owners := make(map[string]bool, len(liveOwners))
	for _, owner := range liveOwners {
		owners[owner] = true
	}
	rows, err := s.scoped.orphans(owners)
	if err != nil {
		return 0, err
	}
	for _, row := range rows {
		if err := s.scoped.deleteByID(row.ID); err != nil {
			return 0, err
		}
	}
	return len(rows), nil
}

// Providers lists the registry, for the UI's New Account form.
func (s *Service) Providers() []Provider { return s.providers.List() }

func (s *Service) decorate(a Account) Account {
	a.State = derivedState(a.State, a.ExpiresAt, s.now())
	return a
}

func metadataOf(a Account) metadata {
	return metadata{
		Provider: a.Provider, Kind: string(a.Kind), Label: a.Label,
		Subject: a.Subject, Scopes: a.Scopes, Env: a.Env,
		State: string(a.State), ExpiresAt: formatTime(a.ExpiresAt),
		VerifiedAt: formatTime(a.VerifiedAt), Ref: a.Ref,
	}
}

// PutForOwner stores a connection's credential as that profile's scoped
// account, replacing whatever was there.
//
// It is the write half of moving a connection secret out of its profile's
// payload: the profile keeps its metadata and syncs, the secret stays on this
// machine. The upsert is by owner, so calling it twice for one profile
// replaces rather than accumulates.
func (s *Service) PutForOwner(ownerID, provider, label, user, secret string) error {
	if ownerID == "" || provider == "" {
		return ErrInvalid
	}
	if s.scoped == nil {
		return ErrNotFound
	}
	kind := KindSecret
	if user != "" {
		kind = KindBasic
	}
	_, err := s.Create(CreateInput{
		Provider: provider, Kind: kind, Label: label,
		User: user, Secret: secret, ScopeOwner: ownerID,
	})
	return err
}

// SecretForOwner reads a profile's scoped credential.
//
// found is false when this machine simply does not have it — a profile synced
// from another device before its secret was entered here. That is a sentence
// the caller can show, not an error.
func (s *Service) SecretForOwner(ownerID string) (user, secret string, found bool, err error) {
	if s.scoped == nil || ownerID == "" {
		return "", "", false, nil
	}
	credential, err := s.Resolve(ResolveInput{ScopeOwner: ownerID, By: "profile"})
	if errors.Is(err, ErrNotFound) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	return credential.User, credential.Secret, true, nil
}
