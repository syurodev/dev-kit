package cloudconfig

import (
	"context"
	"errors"
	"log"
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
	AuthConfig     auth.Config
	HasAuthConfig  bool
	FromEnv        bool
	Blocked        error
	Unavailable    bool
	IssuerChanged  bool
	StartupFetched bool
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
		// A corrupt cache must never veto launching a local-first app: ignore
		// it and fall through to a fresh fetch.
		log.Printf("desktop config: ignoring unreadable cache: %v", err)
		cachedSnapshot, hadCache = Snapshot{}, false
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
		result.StartupFetched = true
		if err := store.Save(fetched); err != nil {
			// A failed cache write must not block applying the fetched config.
			log.Printf("desktop config: could not write cache: %v", err)
		}
		activeSnapshot = fetched
		if !result.FromEnv && hadCache && cachedIssuer != "" && !sameIssuer(cachedIssuer, fetched.OIDCIssuer) {
			result.IssuerChanged = true
		}
	case hadCache:
		// Keep cached snapshot when fetch fails.
	default:
		activeSnapshot = Snapshot{}
	}

	if activeSnapshot.MinAppVersion != "" {
		below, semverErr := CompareSemver(AppVersion, activeSnapshot.MinAppVersion)
		if semverErr != nil {
			// The gateway serves min_app_version as an unvalidated string, so a
			// malformed value can reach clients verbatim. A malformed minimum
			// skips the gate (like a missing one) instead of failing the launch
			// of a local-first app; blocking is Sync/AuthLogin-only by spec.
			log.Printf("desktop config: skipping invalid min_app_version %q: %v", activeSnapshot.MinAppVersion, semverErr)
		} else if below {
			result.Blocked = ErrAppBelowMinimum
		}
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
