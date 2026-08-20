// internal/uistate/uistate_test.go
package uistate

import "testing"

func TestGetMissingKeyReturnsNotFound(t *testing.T) {
	service := NewService(NewMemoryRepository())
	_, found, err := service.Get("sidebar")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("found = true, want false for an unset key")
	}
}

func TestSetGetRoundTrip(t *testing.T) {
	service := NewService(NewMemoryRepository())
	if err := service.Set("sidebar", `{"open":true}`); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, found, err := service.Get("sidebar")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("found = false, want true")
	}
	if got != `{"open":true}` {
		t.Fatalf("value = %q, want %q", got, `{"open":true}`)
	}
}

func TestSetOverwritesExistingKey(t *testing.T) {
	service := NewService(NewMemoryRepository())
	if err := service.Set("sidebar", "first"); err != nil {
		t.Fatal(err)
	}
	if err := service.Set("sidebar", "second"); err != nil {
		t.Fatal(err)
	}
	got, _, err := service.Get("sidebar")
	if err != nil {
		t.Fatal(err)
	}
	if got != "second" {
		t.Fatalf("value = %q, want %q", got, "second")
	}
}

func TestKeysAreIndependent(t *testing.T) {
	service := NewService(NewMemoryRepository())
	if err := service.Set("sidebar", "a"); err != nil {
		t.Fatal(err)
	}
	if err := service.Set("other-key", "b"); err != nil {
		t.Fatal(err)
	}
	got, _, err := service.Get("sidebar")
	if err != nil {
		t.Fatal(err)
	}
	if got != "a" {
		t.Fatalf("sidebar = %q, want %q (must not leak from other-key)", got, "a")
	}
}
