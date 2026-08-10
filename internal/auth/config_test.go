package auth

import (
	"errors"
	"testing"
)

func TestLoadConfigFromEnvMissingIssuer(t *testing.T) {
	t.Setenv("DEVKIT_OIDC_ISSUER", "")
	t.Setenv("DEVKIT_OIDC_CLIENT_ID", "")
	t.Setenv("DEVKIT_OIDC_SCOPES", "")
	t.Setenv("DEVKIT_OIDC_AUTH_URL", "")
	t.Setenv("DEVKIT_OIDC_TOKEN_URL", "")
	_, err := LoadConfigFromEnv()
	if !errors.Is(err, ErrOIDCConfigRequired) {
		t.Fatalf("err = %v, want ErrOIDCConfigRequired", err)
	}
}

func TestLoadConfigFromEnvDerivesKeycloakURLs(t *testing.T) {
	t.Setenv("DEVKIT_OIDC_ISSUER", "https://auth.example.test/realms/devkit")
	t.Setenv("DEVKIT_OIDC_CLIENT_ID", "")
	t.Setenv("DEVKIT_OIDC_SCOPES", "")
	t.Setenv("DEVKIT_OIDC_AUTH_URL", "")
	t.Setenv("DEVKIT_OIDC_TOKEN_URL", "")
	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv: %v", err)
	}
	if cfg.ClientID != "devkit-desktop" {
		t.Fatalf("ClientID = %q", cfg.ClientID)
	}
	if cfg.Scope != "openid profile email roles" {
		t.Fatalf("Scope = %q", cfg.Scope)
	}
	wantAuth := "https://auth.example.test/realms/devkit/protocol/openid-connect/auth"
	wantToken := "https://auth.example.test/realms/devkit/protocol/openid-connect/token"
	if cfg.AuthorizationURL != wantAuth || cfg.TokenURL != wantToken {
		t.Fatalf("urls auth=%q token=%q", cfg.AuthorizationURL, cfg.TokenURL)
	}
	if cfg.CallbackAddress != defaultCallbackAddress || cfg.CallbackPath != defaultCallbackPath {
		t.Fatalf("callback mutated")
	}
}

func TestLoadConfigFromEnvRejectsHTTPIssuer(t *testing.T) {
	t.Setenv("DEVKIT_OIDC_ISSUER", "http://auth.example.test/realms/devkit")
	_, err := LoadConfigFromEnv()
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, ErrOIDCConfigRequired) {
		t.Fatal("http issuer should be validation error, not missing-config")
	}
}

func TestLoadConfigFromEnvHonorsURLOverrides(t *testing.T) {
	t.Setenv("DEVKIT_OIDC_ISSUER", "https://auth.example.test/realms/devkit")
	t.Setenv("DEVKIT_OIDC_AUTH_URL", "https://auth.example.test/custom/auth")
	t.Setenv("DEVKIT_OIDC_TOKEN_URL", "https://auth.example.test/custom/token")
	t.Setenv("DEVKIT_OIDC_CLIENT_ID", "custom-client")
	t.Setenv("DEVKIT_OIDC_SCOPES", "openid")
	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv: %v", err)
	}
	if cfg.AuthorizationURL != "https://auth.example.test/custom/auth" || cfg.TokenURL != "https://auth.example.test/custom/token" {
		t.Fatalf("override not applied")
	}
	if cfg.ClientID != "custom-client" || cfg.Scope != "openid" {
		t.Fatalf("client/scope override failed")
	}
}

func TestConfigFromOIDCRejectsHTTPIssuer(t *testing.T) {
	_, err := ConfigFromOIDC("http://auth.example.test/realms/devkit", "", "", "", "")
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, ErrOIDCConfigRequired) {
		t.Fatal("http issuer should be validation error, not missing-config")
	}
}
