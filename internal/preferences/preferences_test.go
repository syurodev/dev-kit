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
