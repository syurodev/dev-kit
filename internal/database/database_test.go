package database

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"devkit/internal/crypto"
	"devkit/internal/vault"
	"devkit/internal/vaultitem"
)

type stubEnvLookup map[string]bool

func (s stubEnvLookup) Exists(name string) bool { return s[name] }

var testEnvs = stubEnvLookup{"dev": true, "stg": true, "prod": true}

func newService(t *testing.T) *Service {
	t.Helper()
	v := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := v.Setup("correct-horse"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	return NewService(vaultitem.NewStore(vaultitem.NewMemoryRepository(), v), testEnvs)
}

// newServiceWithStore is like newService but also returns the concrete store
// so tests can inspect records (and their still-encrypted payloads) directly.
func newServiceWithStore(t *testing.T) (*Service, *vaultitem.Store) {
	t.Helper()
	v := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := v.Setup("correct-horse"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store := vaultitem.NewStore(vaultitem.NewMemoryRepository(), v)
	return NewService(store, testEnvs), store
}

func TestCreateProfileEncryptsPassword(t *testing.T) {
	svc, store := newServiceWithStore(t)

	summary, err := svc.CreateProfile(CreateProfileInput{
		Name:     "local",
		Host:     "127.0.0.1",
		Port:     5432,
		Database: "devkitdb",
		Username: "devkit",
		Password: "devkitpass",
		Env:      "dev",
	})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}

	record, err := store.Get(summary.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if record.Payload == nil {
		t.Fatal("expected payload to be set")
	}
	if string(record.Payload.Ciphertext) == "devkitpass" {
		t.Fatal("password must be stored encrypted, not plaintext")
	}
	plain, err := store.OpenPayload(record, passwordKeyID)
	if err != nil {
		t.Fatalf("OpenPayload: %v", err)
	}
	if string(plain) != "devkitpass" {
		t.Fatalf("decrypted password = %q", plain)
	}
}

func TestCreateProfileUsesV2EnvelopeBoundToProfile(t *testing.T) {
	svc, store := newServiceWithStore(t)

	summary, err := svc.CreateProfile(CreateProfileInput{Name: "local", Host: "127.0.0.1", Password: "devkitpass", Env: "dev"})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	record, err := store.Get(summary.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if record.Payload == nil {
		t.Fatal("expected payload to be set")
	}
	if record.Payload.Version != crypto.EnvelopeVersionV2 {
		t.Fatalf("envelope version = %d, want V2", record.Payload.Version)
	}
}

func TestCreateProfileRequiresNameAndHost(t *testing.T) {
	svc := newService(t)
	if _, err := svc.CreateProfile(CreateProfileInput{Host: "h"}); err == nil {
		t.Fatal("missing name must error")
	}
	if _, err := svc.CreateProfile(CreateProfileInput{Name: "n"}); err == nil {
		t.Fatal("missing host must error")
	}
}

func TestCreateProfileDefaultsRemotePostgresToVerifyFull(t *testing.T) {
	svc := newService(t)

	summary, err := svc.CreateProfile(CreateProfileInput{
		Name: "production",
		Host: "db.example.com",
		Env:  "prod",
	})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	if summary.SSLMode != "verify-full" {
		t.Fatalf("SSLMode = %q, want verify-full", summary.SSLMode)
	}
}

func TestCreateProfileRejectsExplicitInsecureTLSForRemoteHost(t *testing.T) {
	svc := newService(t)

	_, err := svc.CreateProfile(CreateProfileInput{
		Name:    "production",
		Host:    "db.example.com",
		SSLMode: "disable",
		Env:     "prod",
	})
	if !errors.Is(err, ErrInsecureTLSForRemoteHost) {
		t.Fatalf("CreateProfile error = %v, want ErrInsecureTLSForRemoteHost", err)
	}
}

// A tailnet or VPN address is remote to this machine but the path to it is
// already encrypted, so the profile may say so and go without TLS.
func TestCreateProfilePermitsInsecureTLSOverATrustedNetwork(t *testing.T) {
	svc := newService(t)

	summary, err := svc.CreateProfile(CreateProfileInput{
		Name:           "tailnet",
		Host:           "100.101.102.103",
		SSLMode:        "disable",
		TrustedNetwork: true,
		Env:            "dev",
	})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	if summary.SSLMode != "disable" || !summary.TrustedNetwork {
		t.Fatalf("summary = %+v, want disable over a trusted network", summary)
	}
	if _, err := svc.target(summary.ID); err != nil {
		t.Fatalf("target: %v", err)
	}
}

// The flag only means something without TLS; on any other mode it is dropped
// rather than stored as a promise nothing reads.
func TestTrustedNetworkIsNotKeptWhenTLSIsOn(t *testing.T) {
	svc := newService(t)

	summary, err := svc.CreateProfile(CreateProfileInput{
		Name:           "tailnet",
		Host:           "db.tail1234.ts.net",
		SSLMode:        "require",
		TrustedNetwork: true,
		Env:            "dev",
	})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	if summary.TrustedNetwork {
		t.Fatal("trusted_network was kept on a profile that uses TLS")
	}
}

// A profile refused for sslmode=disable is fixed from the options dialog.
func TestUpdateProfileOptionsCanMarkANetworkTrusted(t *testing.T) {
	svc, store := newServiceWithStore(t)
	summary, err := svc.CreateProfile(CreateProfileInput{Name: "tailnet", Host: "100.101.102.103", Password: "secret", Env: "dev"})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	record, err := store.Get(summary.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	meta, err := decodeMetadata(record)
	if err != nil {
		t.Fatalf("decodeMetadata: %v", err)
	}
	meta.SSLMode = "disable"
	raw, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := store.UpdateMetadata(summary.ID, raw); err != nil {
		t.Fatalf("UpdateMetadata: %v", err)
	}
	if _, err := svc.target(summary.ID); !errors.Is(err, ErrInsecureTLSForRemoteHost) {
		t.Fatalf("target error = %v, want ErrInsecureTLSForRemoteHost", err)
	}

	updated, err := svc.UpdateProfileOptions(summary.ID, UpdateProfileOptionsInput{Env: "dev", TrustedNetwork: true})
	if err != nil {
		t.Fatalf("UpdateProfileOptions: %v", err)
	}
	if !updated.TrustedNetwork {
		t.Fatal("trusted_network was not saved")
	}
	if _, err := svc.target(summary.ID); err != nil {
		t.Fatalf("target after marking trusted: %v", err)
	}
}

func TestCreateProfilePermitsExplicitInsecureTLSForLoopbackHosts(t *testing.T) {
	for _, host := range []string{"localhost", "127.0.0.1", "::1"} {
		t.Run(host, func(t *testing.T) {
			svc := newService(t)
			summary, err := svc.CreateProfile(CreateProfileInput{
				Name:    "local",
				Host:    host,
				SSLMode: "disable",
				Env:     "dev",
			})
			if err != nil {
				t.Fatalf("CreateProfile: %v", err)
			}
			if summary.SSLMode != "disable" {
				t.Fatalf("SSLMode = %q, want disable", summary.SSLMode)
			}
		})
	}
}

func TestDSNRejectsPersistedRemoteProfileWithInsecureTLS(t *testing.T) {
	svc, store := newServiceWithStore(t)
	summary, err := svc.CreateProfile(CreateProfileInput{Name: "production", Host: "db.example.com", Password: "secret", Env: "prod"})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	record, err := store.Get(summary.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	meta, err := decodeMetadata(record)
	if err != nil {
		t.Fatalf("decodeMetadata: %v", err)
	}
	meta.SSLMode = "disable"
	raw, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := store.UpdateMetadata(summary.ID, raw); err != nil {
		t.Fatalf("UpdateMetadata: %v", err)
	}

	_, err = svc.target(summary.ID)
	if !errors.Is(err, ErrInsecureTLSForRemoteHost) {
		t.Fatalf("target error = %v, want ErrInsecureTLSForRemoteHost", err)
	}
}

func TestQueryPreservesCallerCancellation(t *testing.T) {
	svc := newService(t)
	summary, err := svc.CreateProfile(CreateProfileInput{Name: "local", Host: "127.0.0.1", Password: "secret", Env: "dev"})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = svc.Query(ctx, summary.ID, "select 1", 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Query error = %v, want context.Canceled", err)
	}
}

func TestListProfiles(t *testing.T) {
	svc := newService(t)
	if _, err := svc.CreateProfile(CreateProfileInput{Name: "a", Host: "h", Env: "dev"}); err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	list, err := svc.ListProfiles()
	if err != nil {
		t.Fatalf("ListProfiles: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len(list) = %d, want 1", len(list))
	}
}

func TestCreateProfileRequiresEnv(t *testing.T) {
	svc := newService(t)
	if _, err := svc.CreateProfile(CreateProfileInput{Name: "n", Host: "h"}); err == nil {
		t.Fatal("missing env must error")
	}
}

func TestCreateProfileRejectsUnknownEnv(t *testing.T) {
	svc := newService(t)
	if _, err := svc.CreateProfile(CreateProfileInput{Name: "n", Host: "h", Env: "canary"}); err == nil {
		t.Fatal("unknown env must error")
	}
}

func TestUpdateProfileOptionsUpdatesEnvAndReadOnly(t *testing.T) {
	svc, store := newServiceWithStore(t)
	summary, err := svc.CreateProfile(CreateProfileInput{
		Name:     "local",
		Host:     "127.0.0.1",
		Port:     5432,
		Database: "devkitdb",
		Username: "devkit",
		Password: "devkitpass",
		Env:      "dev",
	})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	if summary.ReadOnly {
		t.Fatal("expected ReadOnly default to be false")
	}

	updated, err := svc.UpdateProfileOptions(summary.ID, UpdateProfileOptionsInput{
		Env:      "prod",
		ReadOnly: true,
	})
	if err != nil {
		t.Fatalf("UpdateProfileOptions: %v", err)
	}
	if updated.Env != "prod" {
		t.Fatalf("updated.Env = %q, want prod", updated.Env)
	}
	if !updated.ReadOnly {
		t.Fatal("expected updated.ReadOnly to be true")
	}

	record, err := store.Get(summary.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	meta, err := decodeMetadata(record)
	if err != nil {
		t.Fatalf("decodeMetadata: %v", err)
	}
	if meta.Env != "prod" || !meta.ReadOnly {
		t.Fatalf("persisted meta = %+v, want Env=prod and ReadOnly=true", meta)
	}
}

func TestUpdateProfileOptionsValidatesIncomingEnvEvenWhenStoredEnvIsUnresolvable(t *testing.T) {
	svc, store := newServiceWithStore(t)
	summary, err := svc.CreateProfile(CreateProfileInput{
		Name: "local",
		Host: "127.0.0.1",
		Env:  "dev",
	})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}

	// Corrupt / delete the stored env tag so stored state is unresolvable
	record, err := store.Get(summary.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	meta, err := decodeMetadata(record)
	if err != nil {
		t.Fatalf("decodeMetadata: %v", err)
	}
	meta.Env = "deleted-tag"
	raw, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := store.UpdateMetadata(summary.ID, raw); err != nil {
		t.Fatalf("UpdateMetadata: %v", err)
	}

	// 1. Updating with a valid incoming tag MUST succeed despite the stored tag being unresolvable
	updated, err := svc.UpdateProfileOptions(summary.ID, UpdateProfileOptionsInput{
		Env:      "stg",
		ReadOnly: true,
	})
	if err != nil {
		t.Fatalf("UpdateProfileOptions with valid incoming env failed: %v", err)
	}
	if updated.Env != "stg" || !updated.ReadOnly {
		t.Fatalf("updated = %+v, want Env=stg and ReadOnly=true", updated)
	}

	// 2. Updating with an invalid incoming tag MUST fail by validating the incoming value
	if _, err := svc.UpdateProfileOptions(summary.ID, UpdateProfileOptionsInput{
		Env: "non-existent-tag",
	}); err == nil {
		t.Fatal("expected error when incoming env tag is unknown")
	}

	// 3. Updating with an empty incoming tag MUST fail
	if _, err := svc.UpdateProfileOptions(summary.ID, UpdateProfileOptionsInput{
		Env: "",
	}); err == nil {
		t.Fatal("expected error when incoming env tag is empty")
	}
}

func TestUpdateProfileOptionsInvalidatesPoolOnReadOnlyChange(t *testing.T) {
	svc := newService(t)
	summary, err := svc.CreateProfile(CreateProfileInput{
		Name: "local",
		Host: "127.0.0.1",
		Env:  "dev",
	})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}

	// Populate a pool entry
	pool, err := svc.pools.Get(context.Background(), pgSpec(summary.ID, false))
	if err != nil {
		t.Fatalf("pools.Get: %v", err)
	}
	if pool == nil {
		t.Fatal("expected pool to be created")
	}

	// Toggle read_only: true -> pool must be removed/invalidated
	_, err = svc.UpdateProfileOptions(summary.ID, UpdateProfileOptionsInput{
		Env:      "dev",
		ReadOnly: true,
	})
	if err != nil {
		t.Fatalf("UpdateProfileOptions: %v", err)
	}

	svc.pools.mu.Lock()
	cachedPool := svc.pools.conns[summary.ID]
	svc.pools.mu.Unlock()
	if cachedPool != nil {
		t.Fatal("expected pool to be removed from PoolManager after read_only toggle")
	}
}

func TestTestNewConnectionWritesNothing(t *testing.T) {
	svc, store := newServiceWithStore(t)

	before, err := store.ListByType("db-profile")
	if err != nil {
		t.Fatalf("ListByType: %v", err)
	}
	// Port 1 refuses; the dial failing is fine — what matters is the absence of
	// side effects on the way there.
	_ = svc.TestNewConnection(context.Background(), CreateProfileInput{
		Name: "probe", Host: "127.0.0.1", Port: 1, Database: "d",
		Username: "u", Password: "p", SSLMode: "disable", Env: "dev",
	})

	after, err := store.ListByType("db-profile")
	if err != nil {
		t.Fatalf("ListByType: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("a connection test created %d profile(s)", len(after)-len(before))
	}
	svc.pools.mu.Lock()
	pooled := len(svc.pools.conns)
	svc.pools.mu.Unlock()
	if pooled != 0 {
		t.Fatalf("a throwaway probe left %d connection(s) cached", pooled)
	}
}

func TestTestNewConnectionValidatesLikeCreate(t *testing.T) {
	svc := newService(t)
	// A connection that tests green must not fail validation a second later on
	// save, so both paths share one normaliser.
	for name, input := range map[string]CreateProfileInput{
		"no name":      {Host: "127.0.0.1", Env: "dev", SSLMode: "disable"},
		"no env":       {Name: "p", Host: "127.0.0.1", SSLMode: "disable"},
		"unknown env":  {Name: "p", Host: "127.0.0.1", Env: "nope", SSLMode: "disable"},
		"bad engine":   {Name: "p", Host: "127.0.0.1", Env: "dev", Engine: "oracle"},
		"remote + off": {Name: "p", Host: "db.acme.internal", Env: "dev", SSLMode: "disable"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := svc.TestNewConnection(context.Background(), input); err == nil {
				t.Fatalf("%s was accepted by the test path but would be rejected on save", name)
			}
		})
	}
}

func TestCreateProfileCarriesTheTunnel(t *testing.T) {
	svc := newService(t)
	summary, err := svc.CreateProfile(CreateProfileInput{
		Name: "remote", Host: "db.internal", Port: 5432, Database: "app",
		Username: "devkit", Password: "pw", SSLMode: "verify-full", Env: "dev",
		SSHProfileID: "bastion-1",
	})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	// Creating then patching would leave a window where the profile exists with
	// a direct connection path — exactly what ErrSSHProfileMissing prevents.
	target, err := svc.target(summary.ID)
	if err != nil {
		t.Fatalf("target: %v", err)
	}
	if target.sshProfileID != "bastion-1" {
		t.Fatalf("sshProfileID = %q, want it set at creation", target.sshProfileID)
	}
}

func TestProfileSummaryReturnsEngineAndTunnel(t *testing.T) {
	svc := newService(t)
	created, err := svc.CreateProfile(CreateProfileInput{
		Engine: EngineMySQL, Name: "billing", Host: "127.0.0.1", Port: 3306,
		Database: "billing", Username: "devkit", Password: "pw",
		SSLMode: "disable", Env: "dev", SSHProfileID: "bastion-1",
	})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	// The summary is what every caller sees. Storing a field in metadata and
	// forgetting it here shows up as a MySQL profile that claims to be Postgres.
	if created.Engine != EngineMySQL {
		t.Fatalf("created.Engine = %q, want %q", created.Engine, EngineMySQL)
	}
	if created.SSHProfileID != "bastion-1" {
		t.Fatalf("created.SSHProfileID = %q, want bastion-1", created.SSHProfileID)
	}

	listed, err := svc.ListProfiles()
	if err != nil || len(listed) != 1 {
		t.Fatalf("ListProfiles: err=%v n=%d", err, len(listed))
	}
	if listed[0].Engine != EngineMySQL || listed[0].SSHProfileID != "bastion-1" {
		t.Fatalf("listed summary lost engine/tunnel: %+v", listed[0])
	}
}
