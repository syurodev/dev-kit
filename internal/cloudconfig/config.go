package cloudconfig

import (
	"errors"
	"strconv"
	"strings"

	"devkit/internal/auth"
)

// Snapshot is the validated desktop remote config payload.
type Snapshot struct {
	ConfigVersion    string `json:"config_version"`
	OIDCIssuer       string `json:"oidc_issuer"`
	OIDCClientID     string `json:"oidc_client_id"`
	OIDCScopes       string `json:"oidc_scopes"`
	OIDCAuthURL      string `json:"oidc_auth_url"`
	OIDCTokenURL     string `json:"oidc_token_url"`
	MinAppVersion    string `json:"min_app_version"`
	LatestAppVersion string `json:"latest_app_version"`
	UpdateURL        string `json:"update_url"`
}

// AuthConfig derives a validated auth.Config from the snapshot OIDC fields.
func (s Snapshot) AuthConfig() (auth.Config, error) {
	return auth.ConfigFromOIDC(s.OIDCIssuer, s.OIDCClientID, s.OIDCScopes, s.OIDCAuthURL, s.OIDCTokenURL)
}

var errInvalidSemver = errors.New("invalid semver")

// CompareSemver reports whether running is strictly below minimum. An empty
// minimum is treated as no floor.
func CompareSemver(running, minimum string) (belowMin bool, err error) {
	minimum = strings.TrimSpace(minimum)
	if minimum == "" {
		return false, nil
	}
	runningParts, err := parseSemverParts(running)
	if err != nil {
		return false, err
	}
	minimumParts, err := parseSemverParts(minimum)
	if err != nil {
		return false, err
	}
	maxLen := len(runningParts)
	if len(minimumParts) > maxLen {
		maxLen = len(minimumParts)
	}
	for i := 0; i < maxLen; i++ {
		runningPart := 0
		if i < len(runningParts) {
			runningPart = runningParts[i]
		}
		minimumPart := 0
		if i < len(minimumParts) {
			minimumPart = minimumParts[i]
		}
		if runningPart < minimumPart {
			return true, nil
		}
		if runningPart > minimumPart {
			return false, nil
		}
	}
	return false, nil
}

func parseSemverParts(version string) ([]int, error) {
	version = strings.TrimSpace(version)
	if version == "" {
		return nil, errInvalidSemver
	}
	rawParts := strings.Split(version, ".")
	parts := make([]int, len(rawParts))
	for i, raw := range rawParts {
		if raw == "" {
			return nil, errInvalidSemver
		}
		part, err := strconv.Atoi(raw)
		if err != nil {
			return nil, errInvalidSemver
		}
		parts[i] = part
	}
	return parts, nil
}
