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
