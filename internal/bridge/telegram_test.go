package bridge

import (
	"context"
	"strings"
	"testing"

	"devkit/internal/app"
	"devkit/internal/identity"
	"devkit/internal/telegramcontrol"
)

type telegramMemoryStore struct{ state telegramcontrol.State }

func (s *telegramMemoryStore) Load() (telegramcontrol.State, error)   { return s.state, nil }
func (s *telegramMemoryStore) Save(state telegramcontrol.State) error { s.state = state; return nil }
func TestTelegramSettingsRejectAgentAndTerminal(t *testing.T) {
	service := NewAppService(app.NewContainer())
	for _, caller := range []identity.Caller{identity.Agent("session"), identity.UserTerminal()} {
		ctx := identity.WithCaller(context.Background(), caller)
		if _, err := service.TelegramStatus(ctx); err == nil {
			t.Fatal("remote configuration read allowed")
		}
		if err := service.TelegramConfigure(ctx, `{"enabled":false}`, ""); err == nil {
			t.Fatal("configuration write allowed")
		}
		if err := service.TelegramSetSessionEnabled(ctx, "session", true); err == nil {
			t.Fatal("session opted in by non-UI caller")
		}
		if _, err := service.TelegramBeginPair(ctx); err == nil {
			t.Fatal("pairing allowed")
		}
	}
}
func TestTelegramUnavailableWithoutDesktopHost(t *testing.T) {
	service := NewAppService(app.NewContainer())
	_, err := service.TelegramStatus(context.Background())
	if err == nil || !strings.Contains(err.Error(), "desktop") {
		t.Fatalf("error=%v", err)
	}
}
func TestTelegramUIHasRequiredDefaultGrants(t *testing.T) {
	container := app.NewContainer()
	control, err := telegramcontrol.New(&telegramMemoryStore{}, telegramcontrol.Deps{Locked: func() bool { return false }})
	if err != nil {
		t.Fatal(err)
	}
	container.Telegram = control
	service := NewAppService(container)
	if _, err := service.TelegramStatus(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := service.TelegramSetSessionEnabled(context.Background(), "deleted-session", false); err != nil {
		t.Fatal(err)
	}
	if err := service.TelegramConfigure(context.Background(), `{"enabled":false}`, ""); err != nil {
		t.Fatal(err)
	}
}
