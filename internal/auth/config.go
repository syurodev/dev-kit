package auth

import (
	"errors"
	"os"
	"strings"
	"time"
)

var ErrOIDCConfigRequired = errors.New("OIDC configuration is required")

const (
	envOIDCIssuer   = "DEVKIT_OIDC_ISSUER"
	envOIDCClientID = "DEVKIT_OIDC_CLIENT_ID"
	envOIDCScopes   = "DEVKIT_OIDC_SCOPES"
	envOIDCAuthURL  = "DEVKIT_OIDC_AUTH_URL"
	envOIDCTokenURL = "DEVKIT_OIDC_TOKEN_URL"
)

// LoadConfigFromEnv reads trusted process environment only. Missing issuer
// returns ErrOIDCConfigRequired so callers can keep the app local-first.
func LoadConfigFromEnv() (Config, error) {
	issuer := strings.TrimSpace(os.Getenv(envOIDCIssuer))
	if issuer == "" {
		return Config{}, ErrOIDCConfigRequired
	}
	return ConfigFromOIDC(
		issuer,
		os.Getenv(envOIDCClientID),
		os.Getenv(envOIDCScopes),
		os.Getenv(envOIDCAuthURL),
		os.Getenv(envOIDCTokenURL),
	)
}

// ConfigFromOIDC builds a validated auth.Config from explicit OIDC settings.
// Empty clientID and scopes receive production defaults; empty auth/token URLs
// are derived from the issuer using the Keycloak path convention.
func ConfigFromOIDC(issuer, clientID, scopes, authURL, tokenURL string) (Config, error) {
	issuer = strings.TrimSpace(issuer)
	if issuer == "" {
		return Config{}, ErrOIDCConfigRequired
	}
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		clientID = "devkit-desktop"
	}
	scope := strings.TrimSpace(scopes)
	if scope == "" {
		scope = "openid profile email roles"
	}
	authURL = strings.TrimSpace(authURL)
	tokenURL = strings.TrimSpace(tokenURL)
	if authURL == "" {
		authURL = strings.TrimRight(issuer, "/") + "/protocol/openid-connect/auth"
	}
	if tokenURL == "" {
		tokenURL = strings.TrimRight(issuer, "/") + "/protocol/openid-connect/token"
	}
	cfg := Config{
		Issuer:           issuer,
		AuthorizationURL: authURL,
		TokenURL:         tokenURL,
		ClientID:         clientID,
		Scope:            scope,
		CallbackAddress:  defaultCallbackAddress,
		CallbackPath:     defaultCallbackPath,
		LoginTimeout:     3 * time.Minute,
		RefreshBefore:    60 * time.Second,
	}
	if !validConfig(cfg) {
		return Config{}, ErrAuthenticationFailed
	}
	return cfg, nil
}
