package app

import (
	"context"
	"strings"
	"testing"

	"devkit/internal/accounts"
	"devkit/internal/storage"
	"devkit/internal/vault"
	"devkit/internal/vaultitem"
)

func credentialContainer(t *testing.T) *Container {
	t.Helper()
	v := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := v.Setup("correct-horse"); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(t.TempDir() + "/devkit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	container := NewContainer()
	container.Vault = v
	container.Items = vaultitem.NewStore(vaultitem.NewMemoryRepository(), v)
	container.Accounts = accounts.NewService(
		container.Items, db, storage.TableAccountSecrets, v,
		accounts.NewRegistry(accounts.DefaultProviders()), container.Environments,
	)
	return container
}

// A profile written the old way: its secret in its own payload, which syncs.
func writeLegacyProfile(t *testing.T, c *Container, itemType, keyID, name, secret string) vaultitem.Record {
	t.Helper()
	meta := []byte(`{"name":"` + name + `","host":"h","port":5432,"username":"u","ssl_mode":"disable","engine":"postgres","env":"dev"}`)
	if itemType != vaultitem.ItemTypeDatabase {
		meta = []byte(`{"name":"` + name + `"}`)
	}
	record, err := c.Items.Create(itemType, keyID, meta, []byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return record
}

// Copying, never moving. A profile's payload syncs and a scoped account does
// not, so emptying the payload here would leave every other device with a
// profile it cannot use and no local copy to fall back on.
func TestMigrateCopiesAndLeavesThePayloadAlone(t *testing.T) {
	c := credentialContainer(t)
	record := writeLegacyProfile(t, c, vaultitem.ItemTypeDatabase, "db-password", "staging", "hunter2")

	report, err := MigrateLegacyCredentials(c)
	if err != nil {
		t.Fatal(err)
	}
	if report.Copied != 1 || report.Cleared != 0 {
		t.Fatalf("report = %+v; migration must copy and clear nothing", report)
	}
	_, secret, found, err := c.Accounts.SecretForOwner(record.ID)
	if err != nil || !found || secret != "hunter2" {
		t.Fatalf("scoped secret = %q, found=%v, %v", secret, found, err)
	}
	// Still there: another device may not have run this yet.
	payload, err := c.Items.OpenPayload(mustGet(t, c, record.ID), "db-password")
	if err != nil || string(payload) != "hunter2" {
		t.Fatalf("payload = %q, %v; the migration must not empty it", payload, err)
	}
}

// Running it twice must not overwrite a password entered since with the stale
// payload.
func TestMigrateIsIdempotentAndNeverOverwritesANewerSecret(t *testing.T) {
	c := credentialContainer(t)
	record := writeLegacyProfile(t, c, vaultitem.ItemTypeDatabase, "db-password", "staging", "old")
	if _, err := MigrateLegacyCredentials(c); err != nil {
		t.Fatal(err)
	}
	// The user rotates the password; the stale payload still says "old".
	if err := c.Accounts.PutForOwner(record.ID, accounts.ProviderDatabase, "staging", "", "new"); err != nil {
		t.Fatal(err)
	}

	report, err := MigrateLegacyCredentials(c)
	if err != nil {
		t.Fatal(err)
	}
	if report.Copied != 0 || report.AlreadyThere != 1 {
		t.Fatalf("report = %+v", report)
	}
	_, secret, _, _ := c.Accounts.SecretForOwner(record.ID)
	if secret != "new" {
		t.Fatalf("secret = %q; the stale payload overwrote the current one", secret)
	}
}

// The step that actually stops connection passwords syncing, and it is
// separate because it has a consequence on every other device.
func TestClearEmptiesOnlyWhatIsSafeToEmpty(t *testing.T) {
	c := credentialContainer(t)
	migrated := writeLegacyProfile(t, c, vaultitem.ItemTypeDatabase, "db-password", "staging", "hunter2")
	untouched := writeLegacyProfile(t, c, vaultitem.ItemTypeKafka, "kafka-secret", "events", "sasl-secret")

	if _, err := MigrateLegacyCredentials(c); err != nil {
		t.Fatal(err)
	}
	// Take one profile's local copy away again: clearing its payload would
	// then destroy the only secret this machine has.
	if err := c.Accounts.DeleteForOwner(untouched.ID); err != nil {
		t.Fatal(err)
	}

	report, err := ClearLegacyCredentialPayloads(c)
	if err != nil {
		t.Fatal(err)
	}
	if report.Cleared != 1 {
		t.Fatalf("cleared %d, want 1", report.Cleared)
	}
	if payload, _ := c.Items.OpenPayload(mustGet(t, c, migrated.ID), "db-password"); len(payload) != 0 {
		t.Fatalf("a migrated payload was not cleared: %q", payload)
	}
	payload, err := c.Items.OpenPayload(mustGet(t, c, untouched.ID), "kafka-secret")
	if err != nil || string(payload) != "sasl-secret" {
		t.Fatalf("the only local copy was destroyed: %q, %v", payload, err)
	}
}

// Every connection type moves, including the SSH key passphrase — it is a
// credential belonging to one key, the same shape as a connection's password.
func TestMigrateCoversEveryConnectionType(t *testing.T) {
	c := credentialContainer(t)
	writeLegacyProfile(t, c, vaultitem.ItemTypeDatabase, "db-password", "db", "a")
	writeLegacyProfile(t, c, vaultitem.ItemTypeSSH, "ssh-secret", "ssh", "b")
	writeLegacyProfile(t, c, vaultitem.ItemTypeKafka, "kafka-secret", "kafka", "c")
	writeLegacyProfile(t, c, "ssh-key-passphrase", "ssh-key-passphrase", "key", "d")

	report, err := MigrateLegacyCredentials(c)
	if err != nil {
		t.Fatal(err)
	}
	if report.Copied != 4 {
		t.Fatalf("copied %d, want 4", report.Copied)
	}
	list, err := c.Accounts.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, account := range list {
		if !account.Scoped() {
			t.Fatalf("a migrated credential became a shared account: %+v", account)
		}
		// The label is for the list, never the value.
		if strings.ContainsAny(account.Label, "abcd") && len(account.Label) == 1 {
			t.Fatalf("a secret reached a label: %+v", account)
		}
	}
}

// A profile that never had a payload — one created after the move — is not a
// migration candidate and must not be reported as one.
func TestMigrateSkipsProfilesWithNoLegacyPayload(t *testing.T) {
	c := credentialContainer(t)
	if _, err := c.Items.Create(vaultitem.ItemTypeKafka, "kafka-secret",
		[]byte(`{"name":"new"}`), []byte{}); err != nil {
		t.Fatal(err)
	}
	report, err := MigrateLegacyCredentials(c)
	if err != nil {
		t.Fatal(err)
	}
	if report.Copied != 0 || report.AlreadyThere != 0 {
		t.Fatalf("report = %+v", report)
	}
}

func mustGet(t *testing.T, c *Container, id string) vaultitem.Record {
	t.Helper()
	record, err := c.Items.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return record
}
