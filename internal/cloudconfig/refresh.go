package cloudconfig

import (
	"context"
	"errors"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"time"

	"devkit/internal/auth"
)

const (
	refreshDebounce      = 2 * time.Second
	rateLimitBackoffBase = 60 * time.Second
	rateLimitJitterMax   = 5 * time.Second
	refreshPollInterval  = 5 * time.Second
	refreshFetchTimeout  = 5 * time.Second
)

// ApplyUpdate is the outcome of applying a refreshed config snapshot.
type ApplyUpdate struct {
	Blocked       error
	IssuerChanged bool
	AuthConfig    auth.Config
	HasAuthConfig bool
}

// RefreshOptions configures the background desktop config refresh loop.
type RefreshOptions struct {
	ConfigDir      string
	Client         *http.Client
	FromEnv        bool
	StartupFetched bool
	OnApply        func(ApplyUpdate)

	// PollInterval is the delay between connectivity checks while offline or
	// after a successful refresh. Tests may shorten this.
	PollInterval time.Duration
	// Fetch replaces the default remote fetch (tests only).
	Fetch func(context.Context, string, string, *http.Client) (Snapshot, error)
	// Dial checks TCP connectivity to the pinned API host (tests only).
	Dial func(ctx context.Context, network, address string) error
	// Sleep waits between loop steps (tests only).
	Sleep func(context.Context, time.Duration) error
	// Jitter adds random delay to rate-limit backoff (tests only).
	Jitter func(time.Duration) time.Duration
}

// StartRefresh launches the background refresh loop. It returns immediately.
func StartRefresh(ctx context.Context, opts RefreshOptions) {
	go runRefreshLoop(ctx, opts)
}

func runRefreshLoop(ctx context.Context, opts RefreshOptions) {
	opts = opts.withDefaults()
	store := NewFileStore(opts.ConfigDir)

	wasOffline := !opts.StartupFetched
	for {
		if err := ctx.Err(); err != nil {
			return
		}

		online := opts.reachable(ctx)
		if !online {
			wasOffline = true
			if !opts.wait(ctx, opts.PollInterval) {
				return
			}
			continue
		}
		if !wasOffline {
			if !opts.wait(ctx, opts.PollInterval) {
				return
			}
			continue
		}

		if !opts.wait(ctx, refreshDebounce) {
			return
		}
		if !opts.reachable(ctx) {
			continue
		}

		cachedSnapshot, _, err := store.Load()
		if err != nil {
			if !opts.wait(ctx, opts.PollInterval) {
				return
			}
			continue
		}
		cachedIssuer := cachedSnapshot.OIDCIssuer

		fetchCtx, cancel := context.WithTimeout(ctx, refreshFetchTimeout)
		fetched, fetchErr := opts.Fetch(fetchCtx, ProductionAPIBase, AppVersion, opts.Client)
		cancel()

		switch {
		case errors.Is(fetchErr, ErrRateLimited):
			backoff := rateLimitBackoffBase
			if opts.Jitter != nil {
				backoff += opts.Jitter(rateLimitJitterMax)
			} else {
				backoff += time.Duration(rand.Int63n(int64(rateLimitJitterMax)))
			}
			if !opts.wait(ctx, backoff) {
				return
			}
			continue
		case fetchErr != nil:
			if !opts.wait(ctx, opts.PollInterval) {
				return
			}
			continue
		}

		if err := store.Save(fetched); err != nil {
			if !opts.wait(ctx, opts.PollInterval) {
				return
			}
			continue
		}

		update := applyRefresh(fetched, cachedIssuer, opts.FromEnv)
		if opts.OnApply != nil {
			opts.OnApply(update)
		}
		wasOffline = false

		if !opts.wait(ctx, opts.PollInterval) {
			return
		}
	}
}

func applyRefresh(snapshot Snapshot, cachedIssuer string, fromEnv bool) ApplyUpdate {
	var update ApplyUpdate
	if below, err := CompareSemver(AppVersion, snapshot.MinAppVersion); err == nil && below {
		update.Blocked = ErrAppBelowMinimum
	}
	if !fromEnv && cachedIssuer != "" && !sameIssuer(cachedIssuer, snapshot.OIDCIssuer) {
		update.IssuerChanged = true
	}
	if !fromEnv && update.Blocked == nil {
		if cfg, ok := authConfigFromSnapshot(snapshot); ok {
			update.AuthConfig = cfg
			update.HasAuthConfig = true
		}
	}
	return update
}

// ApplySyncBlocked merges a refresh apply outcome into the current block state.
func ApplySyncBlocked(current, update error) error {
	if update != nil {
		return update
	}
	if errors.Is(current, ErrAppBelowMinimum) || errors.Is(current, ErrUnavailable) {
		return nil
	}
	return current
}

func (opts RefreshOptions) withDefaults() RefreshOptions {
	if opts.PollInterval <= 0 {
		opts.PollInterval = refreshPollInterval
	}
	if opts.Fetch == nil {
		opts.Fetch = Fetch
	}
	if opts.Dial == nil {
		opts.Dial = defaultPinnedDial
	}
	if opts.Sleep == nil {
		opts.Sleep = sleepContext
	}
	return opts
}

func (opts RefreshOptions) reachable(ctx context.Context) bool {
	dialCtx, cancel := context.WithTimeout(ctx, refreshFetchTimeout)
	defer cancel()
	return opts.Dial(dialCtx, "tcp", pinnedDialAddr()) == nil
}

func (opts RefreshOptions) wait(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	if err := opts.Sleep(ctx, d); err != nil {
		return false
	}
	return ctx.Err() == nil
}

func defaultPinnedDial(ctx context.Context, network, address string) error {
	var d net.Dialer
	_, err := d.DialContext(ctx, network, address)
	return err
}

func pinnedDialAddr() string {
	parsed, err := url.Parse(ProductionAPIBase)
	if err != nil || parsed.Hostname() == "" {
		return "api.synx.io.vn:443"
	}
	return net.JoinHostPort(parsed.Hostname(), "443")
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
