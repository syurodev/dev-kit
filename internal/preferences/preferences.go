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

// ThemeFamily is the person's Color palette: the hues, code syntax colours
// and terminal colours. The chrome is one Mono design and no family changes
// it. It is a second preference rather than more values on Theme: Theme is
// the MODE, which the native window appearance is set from, and a palette
// supplies both modes, so "Dracula, following the system" is expressible.
type ThemeFamily string

const (
	// ThemeFamilyDevKit is the Mono palette in index.css: muted hues, code in
	// the text ramp. It is the default.
	ThemeFamilyDevKit     ThemeFamily = "devkit"
	ThemeFamilyCatppuccin ThemeFamily = "catppuccin"
	ThemeFamilyDracula    ThemeFamily = "dracula"
	ThemeFamilyAyu        ThemeFamily = "ayu"
	ThemeFamilyGitHub     ThemeFamily = "github"
	ThemeFamilyRosePine   ThemeFamily = "rosepine"
	ThemeFamilyLotus      ThemeFamily = "lotus"
	ThemeFamilyNord       ThemeFamily = "nord"
	ThemeFamilySolarized  ThemeFamily = "solarized"
	ThemeFamilyGruvbox    ThemeFamily = "gruvbox"
	ThemeFamilyTokyoNight ThemeFamily = "tokyonight"
	ThemeFamilyEverforest ThemeFamily = "everforest"
	ThemeFamilyXcode      ThemeFamily = "xcode"
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

// DefaultUpdateChecks is on: a person who never opens Settings still learns a
// release exists. The check is one unauthenticated GET for public release
// metadata, sends nothing about the person or the machine, and installs
// nothing; Settings turns it off for good.
const DefaultUpdateChecks = true

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

// AgentSandboxSettings shapes the jail Agent Hub's vendor processes run
// under, beyond what a session grants on its own (the devkit-mcp socket, the
// vendor's own credential paths).
//
// The default is a jail a developer can work in: the real $HOME, so ~/.docker,
// ~/.kube, ~/.npmrc, gh's config and the rest of what a CLI reads from home
// are there, and the Docker socket, found on its own. The two switches are
// pointers so that unset — a machine that never opened Settings, or a value
// written before the field existed — reads as on.
//
// What is NOT configurable here is what stays hidden whatever these say:
// sandbox.Policy's deny list (~/.ssh, ~/.gnupg, ~/.aws, ~/.config/gcloud) and
// DevKit's own data directory (the vault database, other sessions' sockets),
// which Agent Hub adds on top. A path resolving under the deny list is
// rejected at session-open time, not accepted here and silently dropped later.
//
// The path fields are not validated against the filesystem here: a path that
// does not exist yet (a Docker socket before the daemon starts) is not a
// typo, and sandbox.Policy already skips binds for paths that are not there.
type AgentSandboxSettings struct {
	// ShareHome lets a session see the real $HOME, read-write, instead of an
	// empty tmpfs (sandbox.Policy.PrivateHome). Nil means on. Off, a session
	// sees the project, the agent's own credentials and what is listed below,
	// nothing else of home — for a machine whose home holds things no agent
	// should read. (Was RelaxedHome, opt-in; a stored relaxedHome is ignored,
	// since on is now what unset means.)
	ShareHome *bool `json:"shareHome,omitempty"`
	// AllowDocker binds the Docker socket into the jail and points
	// DOCKER_HOST at it. Nil means on: agents build images, run compose and
	// read container logs as a matter of course. It is a real grant — the
	// socket is root on the host, in practice — which is why it is one switch
	// away rather than buried.
	AllowDocker *bool `json:"allowDocker,omitempty"`
	// DockerSocketPath overrides where the Docker socket is. Empty means
	// found at session open (agenthub.DetectDocker). Only a unix socket,
	// granted by its exact path, never a general grant on unix sockets.
	DockerSocketPath string `json:"dockerSocketPath,omitempty"`
	// ExtraReadOnlyPaths are additional directories or files outside the
	// project that every agent session may read — a shared library under
	// /opt, say. Under a shared $HOME they are readable already.
	ExtraReadOnlyPaths []string `json:"extraReadOnlyPaths,omitempty"`
}

// HomeShared reports ShareHome with its default applied.
func (s AgentSandboxSettings) HomeShared() bool { return s.ShareHome == nil || *s.ShareHome }

// DockerAllowed reports AllowDocker with its default applied.
func (s AgentSandboxSettings) DockerAllowed() bool { return s.AllowDocker == nil || *s.AllowDocker }

const maxSandboxExtraReadOnlyPaths = 32

// DefaultAgentSandbox is the zero value: the real $HOME, and Docker found on
// its own (see AgentSandboxSettings).
func DefaultAgentSandbox() AgentSandboxSettings {
	return AgentSandboxSettings{}
}

// StrictAgentSandbox is the narrowest jail these settings describe: private
// $HOME, no Docker. What a setting that cannot be read falls back to — the
// default is the permissive one, and a stored "don't share home" that fails
// to parse must not come back as its opposite.
func StrictAgentSandbox() AgentSandboxSettings {
	off := false
	return AgentSandboxSettings{ShareHome: &off, AllowDocker: &off}
}

// Normalized trims and dedupes paths, and drops anything blank. Applied on the
// way in and the way out, like AgentCheckpointSettings.Normalized, so a value
// written by hand or by an older build still reads as something usable.
func (s AgentSandboxSettings) Normalized() AgentSandboxSettings {
	s.DockerSocketPath = strings.TrimSpace(s.DockerSocketPath)

	paths := make([]string, 0, len(s.ExtraReadOnlyPaths))
	seen := map[string]bool{}
	for _, p := range s.ExtraReadOnlyPaths {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] || len(paths) == maxSandboxExtraReadOnlyPaths {
			continue
		}
		seen[p] = true
		paths = append(paths, p)
	}
	s.ExtraReadOnlyPaths = paths
	return s
}

type Repository interface {
	GetTheme() (Theme, bool, error)
	SetTheme(theme Theme) error
	GetThemeFamily() (ThemeFamily, bool, error)
	SetThemeFamily(family ThemeFamily) error
	GetAgentNotifications() (bool, bool, error)
	SetAgentNotifications(enabled bool) error
	GetUpdateChecks() (bool, bool, error)
	SetUpdateChecks(enabled bool) error
	GetRevealRequiresPassword() (bool, bool, error)
	SetRevealRequiresPassword(required bool) error
	GetAgentCheckpoints() (AgentCheckpointSettings, bool, error)
	SetAgentCheckpoints(settings AgentCheckpointSettings) error
	GetAgentSandbox() (AgentSandboxSettings, bool, error)
	SetAgentSandbox(settings AgentSandboxSettings) error
	GetCodexAPIKeyAccount() (string, bool, error)
	SetCodexAPIKeyAccount(accountID string) error
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
		ThemeFamilyRosePine, ThemeFamilyLotus, ThemeFamilyNord, ThemeFamilySolarized, ThemeFamilyGruvbox,
		ThemeFamilyTokyoNight, ThemeFamilyEverforest, ThemeFamilyXcode:
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

// UpdateChecks reports whether DevKit looks for a newer release when it
// starts. Unset reads as DefaultUpdateChecks.
func (s *Service) UpdateChecks() (bool, error) {
	enabled, found, err := s.repo.GetUpdateChecks()
	if err != nil {
		return false, err
	}
	if !found {
		return DefaultUpdateChecks, nil
	}
	return enabled, nil
}

func (s *Service) SetUpdateChecks(enabled bool) error {
	return s.repo.SetUpdateChecks(enabled)
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

// AgentSandbox reports how Agent Hub's vendor jail is shaped beyond a
// session's own grants. Unset reads as DefaultAgentSandbox, never an error.
func (s *Service) AgentSandbox() (AgentSandboxSettings, error) {
	settings, found, err := s.repo.GetAgentSandbox()
	if err != nil {
		return AgentSandboxSettings{}, err
	}
	if !found {
		return DefaultAgentSandbox(), nil
	}
	return settings.Normalized(), nil
}

func (s *Service) SetAgentSandbox(settings AgentSandboxSettings) error {
	return s.repo.SetAgentSandbox(settings.Normalized())
}

// CodexAPIKeyAccount is the Accounts id of the API key Codex sessions sign in
// with, or "" for Codex's own ChatGPT sign-in. Only the id is kept here: the
// key stays in Accounts and is resolved at spawn, so rotating it there is
// all it takes.
func (s *Service) CodexAPIKeyAccount() (string, error) {
	id, _, err := s.repo.GetCodexAPIKeyAccount()
	if err != nil {
		return "", err
	}
	return id, nil
}

// SetCodexAPIKeyAccount stores the id; "" goes back to the ChatGPT sign-in.
// Whether the id names a usable account is the caller's to check — this
// package does not know accounts.
func (s *Service) SetCodexAPIKeyAccount(accountID string) error {
	return s.repo.SetCodexAPIKeyAccount(strings.TrimSpace(accountID))
}
