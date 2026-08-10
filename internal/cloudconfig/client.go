package cloudconfig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// ProductionAPIBase is the pinned HTTPS host for desktop remote config.
	ProductionAPIBase = "https://api.synx.io.vn"
	configPath        = "/v1/desktop/config"
	defaultTimeout    = 5 * time.Second
)

var (
	ErrRateLimited    = errors.New("desktop config rate limited")
	ErrInvalidBaseURL = errors.New("desktop config base URL must be https")
)

// Fetch retrieves the remote desktop config from baseURL using the required
// client headers. baseURL must use HTTPS.
func Fetch(ctx context.Context, baseURL, appVersion string, client *http.Client) (Snapshot, error) {
	endpoint, err := configEndpoint(baseURL)
	if err != nil {
		return Snapshot{}, err
	}
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Snapshot{}, err
	}
	req.Header.Set("X-DevKit-Client", "desktop/"+appVersion)
	req.Header.Set("User-Agent", "DevKit-Desktop/"+appVersion)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return Snapshot{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return Snapshot{}, ErrRateLimited
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return Snapshot{}, fmt.Errorf("desktop config fetch: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var snapshot Snapshot
	if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
		return Snapshot{}, fmt.Errorf("decode desktop config: %w", err)
	}
	if strings.TrimSpace(snapshot.OIDCIssuer) == "" {
		return Snapshot{}, errors.New("desktop config missing oidc_issuer")
	}
	if _, err := snapshot.AuthConfig(); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func configEndpoint(baseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil {
		return "", ErrInvalidBaseURL
	}
	if parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return "", ErrInvalidBaseURL
	}
	parsed.Path = configPath
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}
