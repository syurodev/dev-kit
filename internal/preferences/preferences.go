// Package preferences owns local, non-secret application preferences.
package preferences

import "errors"

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
)

const DefaultThemeFamily = ThemeFamilyDevKit

var ErrInvalidThemeFamily = errors.New("unsupported theme family")

// DefaultAgentNotifications is on: the notifications exist so a user who
// switched away is not left waiting on an agent that is waiting on them.
const DefaultAgentNotifications = true

// DefaultAgentCheckpoints is on: a user who never finds the setting still gets
// the undo, and a machine without a usable git falls back to off on its own —
// the availability check, not the preference, is what turns it off there.
const DefaultAgentCheckpoints = true

type Repository interface {
	GetTheme() (Theme, bool, error)
	SetTheme(theme Theme) error
	GetThemeFamily() (ThemeFamily, bool, error)
	SetThemeFamily(family ThemeFamily) error
	GetAgentNotifications() (bool, bool, error)
	SetAgentNotifications(enabled bool) error
	GetAgentCheckpoints() (bool, bool, error)
	SetAgentCheckpoints(enabled bool) error
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
	case ThemeFamilyDevKit, ThemeFamilyCatppuccin, ThemeFamilyDracula, ThemeFamilyAyu, ThemeFamilyGitHub:
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

// AgentCheckpoints reports whether Agent Hub snapshots a project's files around
// each turn. Off also means DevKit never asks the agent for its own file-change
// report, which is what keeps the vendor's native file history off the user's
// disk when they have said no.
func (s *Service) AgentCheckpoints() (bool, error) {
	enabled, found, err := s.repo.GetAgentCheckpoints()
	if err != nil {
		return false, err
	}
	if !found {
		return DefaultAgentCheckpoints, nil
	}
	return enabled, nil
}

func (s *Service) SetAgentCheckpoints(enabled bool) error {
	return s.repo.SetAgentCheckpoints(enabled)
}
