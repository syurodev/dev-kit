package ssh

import (
	"context"
	"os"
	"strings"
	"testing"
)

// TestSSHIntegration exercises TestConnection + RunCommand against a real SSH
// server using key auth. Gated behind DEVKIT_SSH_TEST=1.
//
// Requires a running sshd and:
//
//	DEVKIT_SSH_KEY_PATH  path to a private key whose public key is in the
//	                     target user's authorized_keys
//	DEVKIT_SSH_USER      target user (default: current $USER or "ubuntu")
//	DEVKIT_SSH_HOST      default 127.0.0.1
func TestSSHIntegration(t *testing.T) {
	if os.Getenv("DEVKIT_SSH_TEST") != "1" {
		t.Skip("set DEVKIT_SSH_TEST=1 to run the SSH integration test")
	}
	keyPath := os.Getenv("DEVKIT_SSH_KEY_PATH")
	if keyPath == "" {
		t.Fatal("DEVKIT_SSH_KEY_PATH is required")
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read key: %v", err)
	}

	svc := newService(t)
	summary, err := svc.CreateProfile(CreateProfileInput{
		Name:       "local",
		Host:       envOr("DEVKIT_SSH_HOST", "127.0.0.1"),
		Port:       22,
		Username:   envOr("DEVKIT_SSH_USER", envOr("USER", "ubuntu")),
		AuthMethod: AuthKey,
		Secret:     string(key),
	})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}

	ctx := context.Background()
	fingerprint, err := svc.ObserveHostKey(ctx, summary.ID)
	if err != nil {
		t.Fatalf("ObserveHostKey: %v", err)
	}
	if err := svc.TrustHostKey(ctx, summary.ID, fingerprint); err != nil {
		t.Fatalf("TrustHostKey: %v", err)
	}
	if err := svc.TestConnection(ctx, summary.ID, ""); err != nil {
		t.Fatalf("TestConnection: %v", err)
	}

	result, err := svc.RunCommand(ctx, summary.ID, "echo devkit-ssh-ok", "")
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	if strings.TrimSpace(result.Output) != "devkit-ssh-ok" {
		t.Fatalf("output = %q", result.Output)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
