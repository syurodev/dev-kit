package preferences

import (
	"errors"
	"testing"
)

func TestThemeDefaultsToDark(t *testing.T) {
	service := NewService(NewMemoryRepository())
	theme, err := service.Theme()
	if err != nil {
		t.Fatal(err)
	}
	if theme != ThemeDark {
		t.Fatalf("theme = %q, want %q", theme, ThemeDark)
	}
}

func TestSetThemeRoundTrip(t *testing.T) {
	service := NewService(NewMemoryRepository())
	for _, want := range []Theme{ThemeSystem, ThemeLight, ThemeDark} {
		if err := service.SetTheme(want); err != nil {
			t.Fatalf("SetTheme(%q): %v", want, err)
		}
		got, err := service.Theme()
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("theme = %q, want %q", got, want)
		}
	}
}

func TestSetThemeRejectsUnknownValue(t *testing.T) {
	service := NewService(NewMemoryRepository())
	if err := service.SetTheme("sepia"); !errors.Is(err, ErrInvalidTheme) {
		t.Fatalf("error = %v, want ErrInvalidTheme", err)
	}
}

// The family is a second axis, not more values on Theme: the native window
// appearance is set from the mode and a titlebar has only light and dark, so
// "Dracula, following the system" has to be two preferences to be expressible.
func TestThemeFamilyDefaultsWhenUnset(t *testing.T) {
	service := NewService(NewMemoryRepository())
	family, err := service.ThemeFamily()
	if err != nil {
		t.Fatalf("ThemeFamily: %v", err)
	}
	if family != ThemeFamilyDevKit {
		t.Fatalf("family = %q, want %q", family, ThemeFamilyDevKit)
	}
}

func TestThemeFamilyRoundTrips(t *testing.T) {
	service := NewService(NewMemoryRepository())
	for _, want := range []ThemeFamily{
		ThemeFamilyDevKit, ThemeFamilyCatppuccin, ThemeFamilyDracula, ThemeFamilyAyu, ThemeFamilyGitHub,
		ThemeFamilyRosePine, ThemeFamilyLotus,
	} {
		if err := service.SetThemeFamily(want); err != nil {
			t.Fatalf("SetThemeFamily(%q): %v", want, err)
		}
		got, err := service.ThemeFamily()
		if err != nil {
			t.Fatalf("ThemeFamily: %v", err)
		}
		if got != want {
			t.Fatalf("family = %q, want %q", got, want)
		}
	}
}

// "not-a-family" rather than a real palette's name: this used "nord" until
// Nord shipped, and then the test was asserting that a valid family fails.
const unknownThemeFamily ThemeFamily = "not-a-family"

func TestSetThemeFamilyRejectsUnknown(t *testing.T) {
	service := NewService(NewMemoryRepository())
	if err := service.SetThemeFamily(unknownThemeFamily); err != ErrInvalidThemeFamily {
		t.Fatalf("err = %v, want %v", err, ErrInvalidThemeFamily)
	}
}

// A family dropped in a later version must not make the app unopenable for
// whoever had it selected — the same forgiveness Theme already has.
func TestThemeFamilyFallsBackForAStoredUnknown(t *testing.T) {
	repo := NewMemoryRepository()
	if err := repo.SetThemeFamily(unknownThemeFamily); err != nil {
		t.Fatalf("SetThemeFamily: %v", err)
	}
	family, err := NewService(repo).ThemeFamily()
	if err != nil {
		t.Fatalf("ThemeFamily: %v", err)
	}
	if family != ThemeFamilyDevKit {
		t.Fatalf("family = %q, want the default %q", family, ThemeFamilyDevKit)
	}
}

// The two axes must not share storage: setting one must not disturb the other.
func TestThemeAndFamilyAreIndependent(t *testing.T) {
	service := NewService(NewMemoryRepository())
	if err := service.SetTheme(ThemeLight); err != nil {
		t.Fatalf("SetTheme: %v", err)
	}
	if err := service.SetThemeFamily(ThemeFamilyDracula); err != nil {
		t.Fatalf("SetThemeFamily: %v", err)
	}
	theme, _ := service.Theme()
	family, _ := service.ThemeFamily()
	if theme != ThemeLight || family != ThemeFamilyDracula {
		t.Fatalf("theme = %q, family = %q", theme, family)
	}
}

// The checkpoint settings are stored as one value and read back through
// Normalized, so a number outside its bounds — typed, or written by an older
// build — becomes a usable one rather than a broken engine.
func TestAgentCheckpointSettingsAreClampedNotRefused(t *testing.T) {
	cases := []struct {
		name  string
		given AgentCheckpointSettings
		want  AgentCheckpointSettings
	}{
		{
			name:  "zero means default, not zero",
			given: AgentCheckpointSettings{Enabled: true},
			want:  AgentCheckpointSettings{Enabled: true, MaxFileSizeMiB: 10, RetentionDays: 30},
		},
		{
			// A 0 MiB ceiling would snapshot nothing and a 0-day retention
			// would delete every checkpoint as it was written.
			name:  "below the floor comes up to it",
			given: AgentCheckpointSettings{Enabled: true, MaxFileSizeMiB: -5, RetentionDays: -1},
			want:  AgentCheckpointSettings{Enabled: true, MaxFileSizeMiB: 1, RetentionDays: 1},
		},
		{
			name:  "above the ceiling comes down to it",
			given: AgentCheckpointSettings{Enabled: true, MaxFileSizeMiB: 99999, RetentionDays: 99999},
			want:  AgentCheckpointSettings{Enabled: true, MaxFileSizeMiB: 1024, RetentionDays: 3650},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.given.Normalized()
			if got.MaxFileSizeMiB != tc.want.MaxFileSizeMiB || got.RetentionDays != tc.want.RetentionDays {
				t.Fatalf("normalized = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// An empty pattern is a line git ignores, and a duplicate is a line that does
// nothing twice. Keeping either would make the list look longer than it is.
func TestAgentCheckpointExcludesAreTrimmedAndDeduplicated(t *testing.T) {
	got := AgentCheckpointSettings{
		Enabled:       true,
		ExtraExcludes: []string{"  generated/  ", "", "   ", "generated/", "*.snapshot"},
	}.Normalized()
	if len(got.ExtraExcludes) != 2 || got.ExtraExcludes[0] != "generated/" || got.ExtraExcludes[1] != "*.snapshot" {
		t.Fatalf("excludes = %q, want the two real patterns", got.ExtraExcludes)
	}
}

// The switch shipped on its own before the numbers joined it. A row written
// then still means what it said, and reading it must not lose the setting.
func TestAgentCheckpointsReadsTheOlderBooleanValue(t *testing.T) {
	repo := &MemoryRepository{}
	service := NewService(repo)

	// Nothing stored: the defaults, on.
	settings, err := service.AgentCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if !settings.Enabled || settings.MaxFileSizeMiB != 10 || settings.RetentionDays != 30 {
		t.Fatalf("defaults = %+v", settings)
	}

	if err := service.SetAgentCheckpoints(AgentCheckpointSettings{Enabled: false, MaxFileSizeMiB: 50}); err != nil {
		t.Fatal(err)
	}
	stored, err := service.AgentCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if stored.Enabled {
		t.Error("the switch did not survive the write")
	}
	if stored.MaxFileSizeMiB != 50 || stored.RetentionDays != 30 {
		t.Errorf("stored = %+v, want the ceiling kept and the retention defaulted", stored)
	}
}

// Unset shares home and allows Docker: the defaults are on.
func TestAgentSandboxDefaultsToHomeAndDocker(t *testing.T) {
	service := NewService(NewMemoryRepository())
	settings, err := service.AgentSandbox()
	if err != nil {
		t.Fatal(err)
	}
	if settings.DockerSocketPath != "" || len(settings.ExtraReadOnlyPaths) != 0 || !settings.HomeShared() || !settings.DockerAllowed() {
		t.Fatalf("settings = %+v, want the zero value", settings)
	}
}

func TestAgentSandboxRoundTrips(t *testing.T) {
	off := false
	service := NewService(NewMemoryRepository())
	want := AgentSandboxSettings{
		DockerSocketPath:   "/var/run/docker.sock",
		ExtraReadOnlyPaths: []string{"/opt/shared-lib", "/mnt/data"},
		ShareHome:          &off,
		AllowDocker:        &off,
	}
	if err := service.SetAgentSandbox(want); err != nil {
		t.Fatal(err)
	}
	got, err := service.AgentSandbox()
	if err != nil {
		t.Fatal(err)
	}
	if got.DockerSocketPath != want.DockerSocketPath || len(got.ExtraReadOnlyPaths) != 2 || got.HomeShared() || got.DockerAllowed() {
		t.Fatalf("settings = %+v, want %+v", got, want)
	}
}

// Blank and duplicate entries are a typo, not a widening of the jail: kept as
// entered they would make the list look wider than it is, not actually widen
// anything, since sandbox.Policy skips paths that do not exist anyway.
func TestAgentSandboxPathsAreTrimmedDedupedAndCapped(t *testing.T) {
	got := AgentSandboxSettings{
		DockerSocketPath:   "  /var/run/docker.sock  ",
		ExtraReadOnlyPaths: []string{"  /opt/a  ", "", "   ", "/opt/a", "/opt/b"},
	}.Normalized()
	if got.DockerSocketPath != "/var/run/docker.sock" {
		t.Fatalf("DockerSocketPath = %q", got.DockerSocketPath)
	}
	if len(got.ExtraReadOnlyPaths) != 2 || got.ExtraReadOnlyPaths[0] != "/opt/a" || got.ExtraReadOnlyPaths[1] != "/opt/b" {
		t.Fatalf("ExtraReadOnlyPaths = %q, want [/opt/a /opt/b]", got.ExtraReadOnlyPaths)
	}

	many := make([]string, 0, maxSandboxExtraReadOnlyPaths+10)
	for i := 0; i < maxSandboxExtraReadOnlyPaths+10; i++ {
		many = append(many, "/opt/path"+string(rune('a'+i%26))+string(rune(i)))
	}
	capped := AgentSandboxSettings{ExtraReadOnlyPaths: many}.Normalized()
	if len(capped.ExtraReadOnlyPaths) != maxSandboxExtraReadOnlyPaths {
		t.Fatalf("len(ExtraReadOnlyPaths) = %d, want the cap %d", len(capped.ExtraReadOnlyPaths), maxSandboxExtraReadOnlyPaths)
	}
}

// Unset means on. The person who has not thought about this yet is the one
// the prompt exists for.
func TestRevealRequiresPasswordDefaultsOn(t *testing.T) {
	svc := NewService(NewMemoryRepository())
	required, err := svc.RevealRequiresPassword()
	if err != nil {
		t.Fatal(err)
	}
	if !required {
		t.Fatal("an unset reveal preference must mean on")
	}
	if err := svc.SetRevealRequiresPassword(false); err != nil {
		t.Fatal(err)
	}
	if required, err = svc.RevealRequiresPassword(); err != nil || required {
		t.Fatalf("turning it off did not stick: %v %v", required, err)
	}
}
