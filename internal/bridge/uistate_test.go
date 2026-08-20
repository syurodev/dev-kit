package bridge

import (
	"testing"

	"devkit/internal/app"
)

func TestUIStateSetGetRoundTrip(t *testing.T) {
	service := NewAppService(app.NewContainer())
	if err := service.UIStateSet("sidebar", `{"open":true}`); err != nil {
		t.Fatalf("UIStateSet: %v", err)
	}
	got, err := service.UIStateGet("sidebar")
	if err != nil {
		t.Fatalf("UIStateGet: %v", err)
	}
	if got != `{"open":true}` {
		t.Fatalf("value = %q, want %q", got, `{"open":true}`)
	}
}

func TestUIStateGetMissingKeyReturnsEmptyString(t *testing.T) {
	service := NewAppService(app.NewContainer())
	got, err := service.UIStateGet("never-set")
	if err != nil {
		t.Fatalf("UIStateGet: %v", err)
	}
	if got != "" {
		t.Fatalf("value = %q, want empty string for an unset key", got)
	}
}
