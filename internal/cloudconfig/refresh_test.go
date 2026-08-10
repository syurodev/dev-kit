package cloudconfig

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestRefreshUpdatesSnapshotAfterFailedFetch(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(dir)
	if err := store.Save(Snapshot{OIDCIssuer: "https://old.example.test/realms/devkit"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	var attempts atomic.Int32
	fetch := func(context.Context, string, string, *http.Client) (Snapshot, error) {
		if attempts.Add(1) == 1 {
			return Snapshot{}, errors.New("network down")
		}
		return Snapshot{
			OIDCIssuer:    "https://new.example.test/realms/devkit",
			MinAppVersion: "0.0.1",
		}, nil
	}

	var applied ApplyUpdate
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go runRefreshLoop(ctx, RefreshOptions{
		ConfigDir:    dir,
		FromEnv:      false,
		Fetch:        fetch,
		Dial:         func(context.Context, string, string) error { return nil },
		Sleep:        func(context.Context, time.Duration) error { return nil },
		PollInterval: time.Millisecond,
		OnApply: func(update ApplyUpdate) {
			applied = update
			cancel()
		},
	})

	<-ctx.Done()

	if attempts.Load() < 2 {
		t.Fatalf("fetch attempts = %d, want >= 2", attempts.Load())
	}
	got, found, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found || got.OIDCIssuer != "https://new.example.test/realms/devkit" {
		t.Fatalf("snapshot = %+v, found=%v", got, found)
	}
	if !applied.IssuerChanged {
		t.Fatal("expected issuer change")
	}
	if applied.Blocked != nil {
		t.Fatalf("blocked = %v", applied.Blocked)
	}
	if !applied.HasAuthConfig {
		t.Fatal("expected auth config on successful refresh")
	}
}

func TestRefreshRateLimitedBackoff(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(dir)
	if err := store.Save(Snapshot{OIDCIssuer: "https://old.example.test/realms/devkit"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	var attempts atomic.Int32
	var slept time.Duration
	fetch := func(context.Context, string, string, *http.Client) (Snapshot, error) {
		switch attempts.Add(1) {
		case 1:
			return Snapshot{}, ErrRateLimited
		default:
			return Snapshot{OIDCIssuer: "https://new.example.test/realms/devkit"}, nil
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go runRefreshLoop(ctx, RefreshOptions{
		ConfigDir: dir,
		Fetch:     fetch,
		Dial:      func(context.Context, string, string) error { return nil },
		Sleep: func(_ context.Context, d time.Duration) error {
			slept += d
			return nil
		},
		Jitter: func(max time.Duration) time.Duration { return max },
		PollInterval: time.Millisecond,
		OnApply: func(ApplyUpdate) { cancel() },
	})

	<-ctx.Done()

	want := rateLimitBackoffBase + rateLimitJitterMax
	if slept < want {
		t.Fatalf("slept = %v, want >= %v", slept, want)
	}
	got, found, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found || got.OIDCIssuer != "https://new.example.test/realms/devkit" {
		t.Fatalf("snapshot = %+v, found=%v", got, found)
	}
}

func TestApplyRefreshRespectsFromEnv(t *testing.T) {
	update := applyRefresh(
		Snapshot{OIDCIssuer: "https://new.example.test/realms/devkit"},
		"https://old.example.test/realms/devkit",
		true,
	)
	if update.IssuerChanged {
		t.Fatal("issuer change must be ignored when env OIDC wins")
	}
}

func TestApplyRefreshBlocksBelowMinimum(t *testing.T) {
	update := applyRefresh(
		Snapshot{OIDCIssuer: "https://auth.example.test/realms/devkit", MinAppVersion: "9.9.9"},
		"https://auth.example.test/realms/devkit",
		false,
	)
	if update.Blocked != ErrAppBelowMinimum {
		t.Fatalf("blocked = %v", update.Blocked)
	}
	if update.HasAuthConfig {
		t.Fatal("auth config must not be provided when below minimum")
	}
}

func TestApplyRefreshProvidesAuthConfig(t *testing.T) {
	update := applyRefresh(
		Snapshot{OIDCIssuer: "https://auth.example.test/realms/devkit", MinAppVersion: "0.0.1"},
		"",
		false,
	)
	if update.Blocked != nil {
		t.Fatalf("blocked = %v", update.Blocked)
	}
	if !update.HasAuthConfig {
		t.Fatal("expected auth config")
	}
	if update.AuthConfig.Issuer != "https://auth.example.test/realms/devkit" {
		t.Fatalf("issuer = %q", update.AuthConfig.Issuer)
	}
}

func TestApplySyncBlockedClearsUnavailable(t *testing.T) {
	got := ApplySyncBlocked(ErrUnavailable, nil)
	if got != nil {
		t.Fatalf("blocked = %v", got)
	}
}

func TestApplySyncBlockedClearsBelowMinimum(t *testing.T) {
	got := ApplySyncBlocked(ErrAppBelowMinimum, nil)
	if got != nil {
		t.Fatalf("blocked = %v", got)
	}
}

func TestApplySyncBlockedSetsBlocked(t *testing.T) {
	got := ApplySyncBlocked(ErrUnavailable, ErrAppBelowMinimum)
	if got != ErrAppBelowMinimum {
		t.Fatalf("blocked = %v", got)
	}
}

func TestApplySyncBlockedPreservesOtherErrors(t *testing.T) {
	other := errors.New("other")
	got := ApplySyncBlocked(other, nil)
	if got != other {
		t.Fatalf("blocked = %v", got)
	}
}
