package cloudconfig

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"devkit/internal/auth"
)

const startupFetchTimeout = 5 * time.Second

// StartupOptions configures desktop remote config resolution at launch.
type StartupOptions struct {
	ConfigDir string
	Client    *http.Client
	Timeout   time.Duration
	// Fetch, when set, replaces the default remote fetch (tests only).
	Fetch func(context.Context, string, string, *http.Client) (Snapshot, error)
}

// StartupResult is the outcome of startup config resolution.
type StartupResult struct {
	AuthConfig    auth.Config
	HasAuthConfig bool
	FromEnv       bool
	Blocked       error
	Unavailable   bool
	IssuerChanged bool
}

// SyncBlocked returns the error that should block AuthLogin and SyncConnect.
func (r StartupResult) SyncBlocked() error {
	if r.Blocked != nil {
		return r.Blocked
	}
	if r.Unavailable {
		return ErrUnavailable
	}
	return nil
}

// ResolveStartup loads env, cache, and remote config using the approved order.
func ResolveStartup(ctx context.Context, opts StartupOptions) (StartupResult, error) {
	var result StartupResult

	envCfg, envErr := auth.LoadConfigFromEnv()
	switch {
	case envErr == nil:
		result.AuthConfig = envCfg
		result.HasAuthConfig = true
		result.FromEnv = true
	case errors.Is(envErr, auth.ErrOIDCConfigRequired):
		// Missing env issuer is normal; fall through to cache/remote.
	default:
		return StartupResult{}, envErr
	}

	store := NewFileStore(opts.ConfigDir)
	cachedSnapshot, hadCache, err := store.Load()
	if err != nil {
		return StartupResult{}, err
	}
	cachedIssuer := strings.TrimSpace(cachedSnapshot.OIDCIssuer)

	activeSnapshot := cachedSnapshot
	fetchCtx := ctx
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		fetchCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}
	fetch := Fetch
	if opts.Fetch != nil {
		fetch = opts.Fetch
	}
	fetched, fetchErr := fetch(fetchCtx, ProductionAPIBase, AppVersion, opts.Client)
	switch {
	case fetchErr == nil:
		if err := store.Save(fetched); err != nil {
			return StartupResult{}, err
		}
		activeSnapshot = fetched
		if hadCache && cachedIssuer != "" && !sameIssuer(cachedIssuer, fetched.OIDCIssuer) {
			result.IssuerChanged = true
		}
	case hadCache:
		// Keep cached snapshot when fetch fails.
	default:
		activeSnapshot = Snapshot{}
	}

	if below, err := CompareSemver(AppVersion, activeSnapshot.MinAppVersion); err != nil {
		return StartupResult{}, err
	} else if below {
		result.Blocked = ErrAppBelowMinimum
	}

	if !result.FromEnv {
		cfg, ok := authConfigFromSnapshot(activeSnapshot)
		if ok {
			result.AuthConfig = cfg
			result.HasAuthConfig = true
		}
	}
	if !result.FromEnv && !result.HasAuthConfig {
		result.Unavailable = true
	}

	return result, nil
}

func authConfigFromSnapshot(snapshot Snapshot) (auth.Config, bool) {
	cfg, err := snapshot.AuthConfig()
	if err != nil {
		return auth.Config{}, false
	}
	return cfg, true
}

func sameIssuer(left, right string) bool {
	return strings.TrimRight(strings.TrimSpace(left), "/") ==
		strings.TrimRight(strings.TrimSpace(right), "/")
}
