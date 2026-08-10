package cloudconfig

import (
	"errors"
	"testing"

	"devkit/internal/auth"
)

func TestSnapshotAuthConfigDerivesKeycloakURLs(t *testing.T) {
	cfg, err := (Snapshot{OIDCIssuer: "https://auth.example.test/realms/devkit"}).AuthConfig()
	if err != nil {
		t.Fatalf("AuthConfig: %v", err)
	}
	wantAuth := "https://auth.example.test/realms/devkit/protocol/openid-connect/auth"
	wantToken := "https://auth.example.test/realms/devkit/protocol/openid-connect/token"
	if cfg.AuthorizationURL != wantAuth || cfg.TokenURL != wantToken {
		t.Fatalf("urls auth=%q token=%q", cfg.AuthorizationURL, cfg.TokenURL)
	}
}

func TestSnapshotAuthConfigRejectsHTTPIssuer(t *testing.T) {
	_, err := (Snapshot{OIDCIssuer: "http://auth.example.test/realms/devkit"}).AuthConfig()
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, auth.ErrAuthenticationFailed) {
		t.Fatalf("err = %v, want ErrAuthenticationFailed", err)
	}
}

func TestCompareSemverBelowMinimum(t *testing.T) {
	below, err := CompareSemver("0.0.9", "0.1.0")
	if err != nil {
		t.Fatalf("CompareSemver: %v", err)
	}
	if !below {
		t.Fatal("expected running version to be below minimum")
	}
}

func TestCompareSemverAtOrAboveMinimum(t *testing.T) {
	for _, running := range []string{"0.1.0", "0.2.0", "1.0.0"} {
		below, err := CompareSemver(running, "0.1.0")
		if err != nil {
			t.Fatalf("CompareSemver(%q): %v", running, err)
		}
		if below {
			t.Fatalf("%q should not be below 0.1.0", running)
		}
	}
}

func TestCompareSemverEmptyMinimum(t *testing.T) {
	below, err := CompareSemver("0.0.1", "")
	if err != nil {
		t.Fatalf("CompareSemver: %v", err)
	}
	if below {
		t.Fatal("empty minimum should not gate")
	}
}

func TestCompareSemverRejectsInvalid(t *testing.T) {
	_, err := CompareSemver("0.1.x", "0.1.0")
	if !errors.Is(err, errInvalidSemver) {
		t.Fatalf("err = %v, want errInvalidSemver", err)
	}
}
