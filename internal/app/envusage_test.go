package app

import (
	"encoding/json"
	"errors"
	"testing"

	"devkit/internal/environments"
	sshservice "devkit/internal/ssh"
	"devkit/internal/vault"
	"devkit/internal/vaultitem"
)

func TestVaultItemEnvUsageDetectsSSHAndDatabaseProfiles(t *testing.T) {
	v := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := v.Setup("master-password"); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	store := vaultitem.NewStore(vaultitem.NewMemoryRepository(), v)
	usage := newVaultItemEnvUsage(store)

	metaSSH, err := json.Marshal(map[string]any{
		"name": "box", "host": "h", "port": 22, "username": "u",
		"auth_method": "password", "env": "qa",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := store.Create("ssh-profile", "ssh-secret", metaSSH, []byte("secret")); err != nil {
		t.Fatalf("Create ssh: %v", err)
	}
	inUse, err := usage.EnvInUse("qa")
	if err != nil || !inUse {
		t.Fatalf("EnvInUse(qa) after ssh = (%v, %v), want true", inUse, err)
	}
	if inUse, err := usage.EnvInUse("dev"); err != nil || inUse {
		t.Fatalf("EnvInUse(dev) = (%v, %v), want false", inUse, err)
	}

	metaDB, err := json.Marshal(map[string]any{
		"name": "local", "host": "127.0.0.1", "port": 5432,
		"database": "db", "username": "u", "ssl_mode": "disable", "env": "canary",
	})
	if err != nil {
		t.Fatalf("marshal db: %v", err)
	}
	if _, err := store.Create("db-profile", "db-password", metaDB, []byte("pw")); err != nil {
		t.Fatalf("Create db: %v", err)
	}
	if inUse, err := usage.EnvInUse("canary"); err != nil || !inUse {
		t.Fatalf("EnvInUse(canary) after db = (%v, %v), want true", inUse, err)
	}

	metaKafka, err := json.Marshal(map[string]any{
		"name":              "events",
		"bootstrap_servers": []string{"127.0.0.1:9092"},
		"security_protocol": "PLAINTEXT",
		"env":               "kafka-env",
	})
	if err != nil {
		t.Fatalf("marshal kafka: %v", err)
	}
	if _, err := store.Create("kafka-profile", "kafka-secret", metaKafka, []byte("secret")); err != nil {
		t.Fatalf("Create kafka: %v", err)
	}
	if inUse, err := usage.EnvInUse("kafka-env"); err != nil || !inUse {
		t.Fatalf("EnvInUse(kafka-env) after kafka = (%v, %v), want true", inUse, err)
	}
}

func TestDeleteTagBlockedWhenSSHProfileReferencesTag(t *testing.T) {
	c := NewContainer()
	if _, err := c.Vault.Setup("master-password"); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	tag, err := c.Environments.CreateTag("qa", false)
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	if _, err := c.SSH.CreateProfile(sshservice.CreateProfileInput{
		Name: "box", Host: "127.0.0.1", Port: 22, Username: "u",
		AuthMethod: sshservice.AuthPassword, Secret: "s3cret", Env: "qa",
	}); err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	if err := c.Environments.DeleteTag(tag.ID); !errors.Is(err, environments.ErrTagInUse) {
		t.Fatalf("DeleteTag error = %v, want ErrTagInUse", err)
	}
	if !c.Environments.Exists("qa") {
		t.Fatal("tag qa must remain after rejected delete")
	}
}

// An API environment names a tag the same way a profile does, and the tag is
// what decides whether a send asks first — deleting it would leave a send
// nothing could gate.
func TestVaultItemEnvUsageDetectsAPIEnvironments(t *testing.T) {
	v := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := v.Setup("master-password"); err != nil {
		t.Fatal(err)
	}
	store := vaultitem.NewStore(vaultitem.NewMemoryRepository(), v)
	usage := newVaultItemEnvUsage(store)

	meta, err := json.Marshal(map[string]any{"name": "Prod", "env": "prod"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(vaultitem.ItemTypeAPIEnvironment, "api-environment", meta, []byte("{}")); err != nil {
		t.Fatal(err)
	}

	inUse, err := usage.EnvInUse("prod")
	if err != nil || !inUse {
		t.Fatalf("EnvInUse(prod) = %v, %v; an API environment holds it", inUse, err)
	}
	if inUse, err := usage.EnvInUse("dev"); err != nil || inUse {
		t.Fatalf("EnvInUse(dev) = %v, %v", inUse, err)
	}
}

// An account's env is a LIST — it may be restricted to several tags, or to
// none — so the scan cannot assume the single-string shape every profile uses.
func TestVaultItemEnvUsageDetectsAccountsWithAListOfTags(t *testing.T) {
	v := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := v.Setup("master-password"); err != nil {
		t.Fatal(err)
	}
	store := vaultitem.NewStore(vaultitem.NewMemoryRepository(), v)
	usage := newVaultItemEnvUsage(store)

	meta, err := json.Marshal(map[string]any{
		"provider": "github", "kind": "token", "label": "deploy",
		"state": "valid", "env": []string{"stg", "prod"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(vaultitem.ItemTypeAccount, "account-secret", meta, []byte("x")); err != nil {
		t.Fatal(err)
	}

	for _, tag := range []string{"stg", "prod"} {
		inUse, err := usage.EnvInUse(tag)
		if err != nil || !inUse {
			t.Fatalf("EnvInUse(%s) = %v, %v; the account is restricted to it", tag, inUse, err)
		}
	}
	if inUse, err := usage.EnvInUse("dev"); err != nil || inUse {
		t.Fatalf("EnvInUse(dev) = %v, %v", inUse, err)
	}

	// An unrestricted account holds no tag at all, so it must not pin one.
	free, err := json.Marshal(map[string]any{
		"provider": "github", "kind": "token", "label": "personal", "state": "valid",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(vaultitem.ItemTypeAccount, "account-secret", free, []byte("y")); err != nil {
		t.Fatal(err)
	}
	if inUse, err := usage.EnvInUse("dev"); err != nil || inUse {
		t.Fatalf("an account with no restriction pinned dev: %v, %v", inUse, err)
	}
}
