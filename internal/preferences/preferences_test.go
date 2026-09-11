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

func TestSetThemeFamilyRejectsUnknown(t *testing.T) {
	service := NewService(NewMemoryRepository())
	if err := service.SetThemeFamily("nord"); err != ErrInvalidThemeFamily {
		t.Fatalf("err = %v, want %v", err, ErrInvalidThemeFamily)
	}
}

// A family dropped in a later version must not make the app unopenable for
// whoever had it selected — the same forgiveness Theme already has.
func TestThemeFamilyFallsBackForAStoredUnknown(t *testing.T) {
	repo := NewMemoryRepository()
	if err := repo.SetThemeFamily("nord"); err != nil {
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
