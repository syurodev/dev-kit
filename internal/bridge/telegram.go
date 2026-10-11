package bridge

import (
	"context"
	"encoding/json"
	"errors"

	"devkit/internal/accounts"
	"devkit/internal/identity"
	"devkit/internal/telegramcontrol"
)

// telegramCall has no MCP counterpart. Explicit identity check prevents an agent
// caller from acquiring desktop privileges through the capability boundary.
func (s *AppService) telegramCall(ctx context.Context, write bool, handler func(context.Context) (any, error)) (any, error) {
	ctx = callerContext(ctx)
	caller, _ := identity.FromContext(ctx)
	if caller.Kind != identity.KindUI {
		return nil, errors.New("telegram settings are desktop-only")
	}
	capability := "preferences:read"
	if write {
		capability = "preferences:write"
	}
	return s.call(ctx, "builtin.settings", capability, "telegram", map[string]any{"action": "telegram_settings"}, func(ctx context.Context) (any, error) {
		if s.container.Telegram == nil {
			return nil, &telegramcontrol.SettingsError{Message: "Telegram needs the desktop app"}
		}
		result, err := handler(ctx)
		if err != nil {
			return nil, &telegramcontrol.SettingsError{Message: err.Error()}
		}
		return result, nil
	})
}
func (s *AppService) TelegramStatus(ctx context.Context) (string, error) {
	result, err := s.telegramCall(ctx, false, func(context.Context) (any, error) {
		raw, err := json.Marshal(s.container.Telegram.Status())
		return string(raw), err
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}

// token is input-only and kept in a local encrypted Accounts row, never preferences.
func (s *AppService) TelegramConfigure(ctx context.Context, configJSON, token string) error {
	_, err := s.telegramCall(ctx, true, func(ctx context.Context) (any, error) {
		var cfg telegramcontrol.Config
		if len(configJSON) > 32000 || json.Unmarshal([]byte(configJSON), &cfg) != nil {
			return nil, errors.New("invalid Telegram configuration")
		}
		var created string
		if token != "" {
			if len(token) > 256 {
				return nil, errors.New("invalid bot token")
			}
			account, err := s.container.Accounts.Create(accounts.CreateInput{Provider: accounts.ProviderGeneric, Kind: accounts.KindToken, Label: "Telegram bot", Secret: token, Local: true})
			if err != nil {
				return nil, publicError("builtin.accounts", err)
			}
			cfg.AccountID = account.ID
			created = account.ID
		}
		err := s.container.Telegram.Configure(ctx, cfg)
		if err != nil && created != "" {
			_ = s.container.Accounts.Delete(created)
		}
		return nil, err
	})
	return err
}
func (s *AppService) TelegramBeginPair(ctx context.Context) (string, error) {
	result, err := s.telegramCall(ctx, true, func(context.Context) (any, error) { return s.container.Telegram.Pair() })
	if err != nil {
		return "", err
	}
	return result.(string), nil
}
func (s *AppService) TelegramAcceptPair(ctx context.Context, userID int64) error {
	_, err := s.telegramCall(ctx, true, func(context.Context) (any, error) { return nil, s.container.Telegram.AcceptPair(userID) })
	return err
}
func (s *AppService) TelegramUnpair(ctx context.Context) error {
	_, err := s.telegramCall(ctx, true, func(context.Context) (any, error) { return nil, s.container.Telegram.Unpair() })
	return err
}
