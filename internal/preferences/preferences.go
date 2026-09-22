// Package preferences owns local, non-secret application preferences.
package preferences

import (
	"errors"
	"strings"
)

type Theme string

const (
	ThemeSystem Theme = "system"
	ThemeLight  Theme = "light"
	ThemeDark   Theme = "dark"
)

const DefaultTheme = ThemeDark

var ErrInvalidTheme = errors.New("unsupported theme preference")

// ThemeFamily is which palette the UI is painted with. It is deliberately a
// second preference rather than more values on Theme: Theme is the MODE, and
// the mode is what the native window appearance is set from — a titlebar can
// be light or dark and nothing else. A family supplies both modes, so the two
// are orthogonal and "Dracula, following the system" is expressible.
type ThemeFamily string

const (
	// ThemeFamilyDevKit is the palette in index.css. It is the absence of an
	// override rather than a set of values, which is why it is the default.
	ThemeFamilyDevKit     ThemeFamily = "devkit"
	ThemeFamilyCatppuccin ThemeFamily = "catppuccin"
	ThemeFamilyDracula    ThemeFamily = "dracula"
	ThemeFamilyAyu        ThemeFamily = "ayu"
	ThemeFamilyGitHub     ThemeFamily = "github"
	ThemeFamilyRosePine   ThemeFamily = "rosepine"
	ThemeFamilyLotus      ThemeFamily = "lotus"
)

const DefaultThemeFamily = ThemeFamilyDevKit

var ErrInvalidThemeFamily = errors.New("unsupported theme family")

// DefaultRevealRequiresPassword is on.
//
// The setting is the user's because the cost falls on exactly them, and
// somebody rotating a token ten times a day knows what they are trading. It
// defaults to on because the person who has not thought about it yet is the
// one the prompt is for.
const DefaultRevealRequiresPassword = true

// DefaultAgentNotifications is on: the notifications exist so a user who
// switched away is not left waiting on an agent that is waiting on them.
const DefaultAgentNotifications = true

// AgentCheckpointSettings is how Agent Hub's file checkpoints behave.
//
// One value rather than four keys: the three numbers only make sense against
// the switch — a retention of 30 days on a feature that is off means nothing —
// and reading them apart would let the UI show a half-applied state while two
// writes were in flight.
type AgentCheckpointSettings struct {
	// Enabled is the user's switch. A machine with no usable git reports the
	// feature off whatever this says; the two are separate facts and the
	// Settings row shows both.
	Enabled bool `json:"enabled"`
	// MaxFileSizeMiB is the per-file ceiling. A file over it is left out of
	// every snapshot, and a restore will not touch it: no tree holds its
	// content, so acting on one would be a guess.
	MaxFileSizeMiB int `json:"maxFileSizeMiB"`
	// RetentionDays is how long a checkpoint outlives its turn.
	RetentionDays int `json:"retentionDays"`
	// ExtraExcludes are gitignore-syntax patterns on top of the built-in deny
	// list, for a project with a large generated directory DevKit does not know
	// about.
	ExtraExcludes []string `json:"extraExcludes,omitempty"`
}

// The bounds a stored value is clamped into. Not validation that refuses — a
// number outside them is a number the user typed, and refusing the write would
// leave the setting showing something that is not what is stored.
const (
	minCheckpointFileSizeMiB = 1
	maxCheckpointFileSizeMiB = 1024
	minCheckpointRetention   = 1
	maxCheckpointRetention   = 3650
	maxCheckpointExcludes    = 64
)

// DefaultAgentCheckpoints is on, with the numbers Phase C measured against.
//
// On by default because a user who never finds the setting still gets the undo,
// and a machine without a usable git falls back to off on its own — the
// availability check, not the preference, is what turns it off there.
func DefaultAgentCheckpoints() AgentCheckpointSettings {
	return AgentCheckpointSettings{Enabled: true, MaxFileSizeMiB: 10, RetentionDays: 30}
}

// Normalized returns the settings with every value inside its bounds and the
// patterns trimmed. Applied on the way in and on the way out, so a value
// written by an older build — or by hand — still reads as something usable.
func (s AgentCheckpointSettings) Normalized() AgentCheckpointSettings {
	defaults := DefaultAgentCheckpoints()
	if s.MaxFileSizeMiB == 0 {
		s.MaxFileSizeMiB = defaults.MaxFileSizeMiB
	}
	if s.RetentionDays == 0 {
		s.RetentionDays = defaults.RetentionDays
	}
	s.MaxFileSizeMiB = clamp(s.MaxFileSizeMiB, minCheckpointFileSizeMiB, maxCheckpointFileSizeMiB)
	s.RetentionDays = clamp(s.RetentionDays, minCheckpointRetention, maxCheckpointRetention)

	patterns := make([]string, 0, len(s.ExtraExcludes))
	seen := map[string]bool{}
	for _, pattern := range s.ExtraExcludes {
		pattern = strings.TrimSpace(pattern)
		// An empty pattern is not "exclude nothing" to git — it is a line git
		// ignores, and keeping it would make the list look longer than it is.
		if pattern == "" || seen[pattern] || len(patterns) == maxCheckpointExcludes {
			continue
		}
		seen[pattern] = true
		patterns = append(patterns, pattern)
	}
	s.ExtraExcludes = patterns
	return s
}

func clamp(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

type Repository interface {
	GetTheme() (Theme, bool, error)
	SetTheme(theme Theme) error
	GetThemeFamily() (ThemeFamily, bool, error)
	SetThemeFamily(family ThemeFamily) error
	GetAgentNotifications() (bool, bool, error)
	SetAgentNotifications(enabled bool) error
	GetRevealRequiresPassword() (bool, bool, error)
	SetRevealRequiresPassword(required bool) error
	GetAgentCheckpoints() (AgentCheckpointSettings, bool, error)
	SetAgentCheckpoints(settings AgentCheckpointSettings) error
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func ValidTheme(theme Theme) bool {
	switch theme {
	case ThemeSystem, ThemeLight, ThemeDark:
		return true
	default:
		return false
	}
}

func (s *Service) Theme() (Theme, error) {
	theme, found, err := s.repo.GetTheme()
	if err != nil {
		return "", err
	}
	if !found || !ValidTheme(theme) {
		return DefaultTheme, nil
	}
	return theme, nil
}

func (s *Service) SetTheme(theme Theme) error {
	if !ValidTheme(theme) {
		return ErrInvalidTheme
	}
	return s.repo.SetTheme(theme)
}

func ValidThemeFamily(family ThemeFamily) bool {
	switch family {
	case ThemeFamilyDevKit, ThemeFamilyCatppuccin, ThemeFamilyDracula, ThemeFamilyAyu, ThemeFamilyGitHub,
		ThemeFamilyRosePine, ThemeFamilyLotus:
		return true
	default:
		return false
	}
}

// ThemeFamily falls back to the default for an unknown value rather than
// erroring, the way Theme does: a family removed in a later version must not
// make the app unopenable for whoever had it selected.
func (s *Service) ThemeFamily() (ThemeFamily, error) {
	family, found, err := s.repo.GetThemeFamily()
	if err != nil {
		return "", err
	}
	if !found || !ValidThemeFamily(family) {
		return DefaultThemeFamily, nil
	}
	return family, nil
}

func (s *Service) SetThemeFamily(family ThemeFamily) error {
	if !ValidThemeFamily(family) {
		return ErrInvalidThemeFamily
	}
	return s.repo.SetThemeFamily(family)
}

// AgentNotifications reports whether Agent Hub raises OS notifications when an
// agent needs the user or finishes its turn.
func (s *Service) AgentNotifications() (bool, error) {
	enabled, found, err := s.repo.GetAgentNotifications()
	if err != nil {
		return false, err
	}
	if !found {
		return DefaultAgentNotifications, nil
	}
	return enabled, nil
}

func (s *Service) SetAgentNotifications(enabled bool) error {
	return s.repo.SetAgentNotifications(enabled)
}

// RevealRequiresPassword reports whether showing a credential's value asks for
// the Master Password again. Off leaves reveal behind a confirmation only.
func (s *Service) RevealRequiresPassword() (bool, error) {
	required, found, err := s.repo.GetRevealRequiresPassword()
	if err != nil {
		return false, err
	}
	if !found {
		return DefaultRevealRequiresPassword, nil
	}
	return required, nil
}

func (s *Service) SetRevealRequiresPassword(required bool) error {
	return s.repo.SetRevealRequiresPassword(required)
}

// AgentCheckpoints reports how Agent Hub snapshots a project's files around
// each turn. Off also means DevKit never asks the agent for its own file-change
// report, which is what keeps the vendor's native file history off the user's
// disk when they have said no.
func (s *Service) AgentCheckpoints() (AgentCheckpointSettings, error) {
	settings, found, err := s.repo.GetAgentCheckpoints()
	if err != nil {
		return AgentCheckpointSettings{}, err
	}
	if !found {
		return DefaultAgentCheckpoints(), nil
	}
	return settings.Normalized(), nil
}

func (s *Service) SetAgentCheckpoints(settings AgentCheckpointSettings) error {
	return s.repo.SetAgentCheckpoints(settings.Normalized())
}
