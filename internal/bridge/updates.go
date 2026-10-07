package bridge

import (
	"context"
	"errors"
	"fmt"

	"devkit/internal/updater"
)

// errUpdateCheckFailed stands for every way the lookup can fail (offline, rate
// limited, a malformed answer). The cause is kept in the chain for the log; the
// person gets one sentence, because none of them is something they can act on
// beyond trying again.
var errUpdateCheckFailed = errors.New("update check failed")

// UpdateStatus is the answer to "is there a newer release". It describes; it
// never installs.
type UpdateStatus struct {
	// Current is the running version ("dev" for an unstamped build).
	Current string `json:"current"`
	// Skipped says why no lookup was made or why there is nothing to report:
	// "dev" (an unstamped build), "disabled" (the person turned automatic
	// checks off) or "no_release" (nothing published yet). Empty means the
	// lookup ran and Latest is set.
	Skipped string `json:"skipped"`
	// Latest is the newest published version. Empty when Skipped is set.
	Latest string `json:"latest"`
	// Available is true when Latest is newer than Current.
	Available bool `json:"available"`
	// Prerelease is true when Latest is a beta (only offered to a person
	// already running one).
	Prerelease bool `json:"prerelease"`
	// URL is the release page to open in the browser; empty unless Available.
	URL string `json:"url"`
	// PublishedAt is when Latest was published, RFC 3339.
	PublishedAt string `json:"publishedAt"`
}

// UpdatesCurrentVersion reports the version this binary was built from.
func (s *AppService) UpdatesCurrentVersion() string {
	return s.container.Version
}

// UpdatesCheck looks up the newest published release.
//
// automatic is true for the check DevKit makes by itself at startup and false
// for the person pressing "Check now". Only an automatic check honours the
// preference: pressing the button is a request, so it is answered even with
// automatic checks off, and with them off nothing else ever reaches the
// network.
func (s *AppService) UpdatesCheck(automatic bool) (UpdateStatus, error) {
	result, err := s.call(context.Background(), "builtin.settings", "preferences:read", "update_check", map[string]any{
		"action":    "check_update",
		"automatic": automatic,
	}, func(ctx context.Context) (any, error) {
		status := UpdateStatus{Current: s.container.Version}
		if automatic && s.container.Preferences != nil {
			enabled, err := s.container.Preferences.UpdateChecks()
			if err != nil {
				return nil, err
			}
			if !enabled {
				status.Skipped = "disabled"
				return status, nil
			}
		}
		if s.container.Updates == nil {
			return nil, errPreferencesUnavailable
		}
		found, err := s.container.Updates.Check(ctx, s.container.Version)
		switch {
		case errors.Is(err, updater.ErrDevBuild):
			status.Skipped = "dev"
			return status, nil
		case errors.Is(err, updater.ErrNoRelease):
			status.Skipped = "no_release"
			return status, nil
		case err != nil:
			return nil, fmt.Errorf("%w: %v", errUpdateCheckFailed, err)
		}
		return UpdateStatus{
			Current:     found.Current,
			Latest:      found.Latest,
			Available:   found.Available,
			Prerelease:  found.Prerelease,
			URL:         found.URL,
			PublishedAt: found.PublishedAt,
		}, nil
	})
	if err != nil {
		return UpdateStatus{}, err
	}
	return result.(UpdateStatus), nil
}

// PreferencesGetUpdateChecks reports whether DevKit looks for a newer release
// when it starts. On by default.
func (s *AppService) PreferencesGetUpdateChecks() (bool, error) {
	result, err := s.call(context.Background(), "builtin.settings", "preferences:read", "update_checks", map[string]any{
		"action": "get_update_checks",
	}, func(context.Context) (any, error) {
		if s.container.Preferences == nil {
			return nil, errPreferencesUnavailable
		}
		return s.container.Preferences.UpdateChecks()
	})
	if err != nil {
		return false, err
	}
	return result.(bool), nil
}

// PreferencesSetUpdateChecks turns the startup check on or off.
func (s *AppService) PreferencesSetUpdateChecks(enabled bool) error {
	_, err := s.call(context.Background(), "builtin.settings", "preferences:write", "update_checks", map[string]any{
		"action":  "set_update_checks",
		"enabled": enabled,
	}, func(context.Context) (any, error) {
		if s.container.Preferences == nil {
			return nil, errPreferencesUnavailable
		}
		return nil, s.container.Preferences.SetUpdateChecks(enabled)
	})
	return err
}
