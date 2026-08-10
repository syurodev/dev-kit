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
	clientID := strings.TrimSpace(os.Getenv(envOIDCClientID))
	if clientID == "" {
		clientID = "devkit-desktop"
	}
	scope := strings.TrimSpace(os.Getenv(envOIDCScopes))
	if scope == "" {
		scope = "openid profile email roles"
	}
	authURL := strings.TrimSpace(os.Getenv(envOIDCAuthURL))
	tokenURL := strings.TrimSpace(os.Getenv(envOIDCTokenURL))
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
