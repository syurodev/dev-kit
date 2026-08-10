package bridge

import (
	"context"
	"time"

	"devkit/internal/syncclient"
)

const productionSyncGateway = "https://api.synx.io.vn"

// AuthStatusDTO deliberately omits all token material and refresh-token state.
type AuthStatusDTO struct {
	Authenticated bool      `json:"authenticated"`
	ExpiresAt     time.Time `json:"expires_at,omitempty"`
}

// SyncStatusDTO contains only connection metadata safe for display. The
// access token and transport internals never cross the bridge boundary.
type SyncStatusDTO struct {
	Connected bool      `json:"connected"`
	Endpoint  string    `json:"endpoint,omitempty"`
	AccountID string    `json:"account_id,omitempty"`
	DeviceID  string    `json:"device_id,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
}

// SyncReportDTO exposes aggregate counts only, never remote payloads.
type SyncReportDTO struct {
	PushAttempted int `json:"push_attempted"`
	PushSent      int `json:"push_sent"`
	PushRetried   int `json:"push_retried"`
	PushConflicts int `json:"push_conflicts"`
	Pulled        int `json:"pulled"`
	Applied       int `json:"applied"`
}

// SyncConflictDTO is non-secret conflict metadata for the UI. No record payload
// or plaintext secret crosses the boundary.
type SyncConflictDTO struct {
	RecordID        string    `json:"record_id"`
	RecordType      string    `json:"record_type"`
	LocalChangeKey  string    `json:"local_change_key"`
	RemoteChangeKey string    `json:"remote_change_key"`
	DetectedAt      time.Time `json:"detected_at"`
}

func (s *AppService) SyncConflicts() ([]SyncConflictDTO, error) {
	result, err := s.call(context.Background(), "builtin.sync", "sync:manage", "sync:conflicts", map[string]any{
		"action": "conflicts",
	}, func(ctx context.Context) (any, error) {
		if s.container.Sync == nil {
			return nil, syncclient.ErrInvalidDependencies
		}
		conflicts, err := s.container.Sync.Conflicts(ctx)
		if err != nil {
			return nil, err
		}
		dtos := make([]SyncConflictDTO, 0, len(conflicts))
		for _, conflict := range conflicts {
			dtos = append(dtos, SyncConflictDTO{
				RecordID:        conflict.RecordID,
				RecordType:      conflict.RecordType,
				LocalChangeKey:  conflict.LocalChangeKey,
				RemoteChangeKey: conflict.RemoteChangeKey,
				DetectedAt:      conflict.DetectedAt,
			})
		}
		return dtos, nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]SyncConflictDTO), nil
}

func (s *AppService) SyncStatus() (SyncStatusDTO, error) {
	result, err := s.call(context.Background(), "builtin.sync", "sync:manage", "sync:status", map[string]any{
		"action": "status",
	}, func(context.Context) (any, error) {
		if s.container.Sync == nil {
			return nil, syncclient.ErrInvalidDependencies
		}
		return syncStatusDTO(s.container.Sync.Status()), nil
	})
	if err != nil {
		return SyncStatusDTO{}, err
	}
	return result.(SyncStatusDTO), nil
}

func (s *AppService) AuthStatus() (AuthStatusDTO, error) {
	result, err := s.call(context.Background(), "builtin.sync", "sync:manage", "sync:status", map[string]any{
		"action": "auth_status",
	}, func(context.Context) (any, error) {
		if s.container.Auth == nil {
			return nil, syncclient.ErrInvalidDependencies
		}
		status := s.container.Auth.Status()
		return AuthStatusDTO{Authenticated: status.Authenticated, ExpiresAt: status.ExpiresAt}, nil
	})
	if err != nil {
		return AuthStatusDTO{}, err
	}
	return result.(AuthStatusDTO), nil
}

// AuthLogin opens Keycloak only when a stored refresh session cannot be used.
// No token crosses this bridge or is included in audit metadata.
func (s *AppService) AuthLogin(ctx context.Context) (AuthStatusDTO, error) {
	result, err := s.call(ctx, "builtin.sync", "sync:manage", "sync:login", map[string]any{
		"action": "login",
	}, func(callCtx context.Context) (any, error) {
		if s.container.Auth == nil {
			return nil, syncclient.ErrInvalidDependencies
		}
		status, err := s.container.Auth.EnsureAuthenticated(callCtx)
		if err != nil {
			return nil, err
		}
		return AuthStatusDTO{Authenticated: status.Authenticated, ExpiresAt: status.ExpiresAt}, nil
	})
	if err != nil {
		return AuthStatusDTO{}, err
	}
	return result.(AuthStatusDTO), nil
}

func (s *AppService) AuthLogout() error {
	_, err := s.call(context.Background(), "builtin.sync", "sync:manage", "sync:logout", map[string]any{
		"action": "logout",
	}, func(context.Context) (any, error) {
		if s.container.Auth == nil {
			return nil, syncclient.ErrInvalidDependencies
		}
		if s.container.Sync != nil {
			s.container.Sync.Disconnect()
		}
		return nil, s.container.Auth.Logout()
	})
	return err
}

// SyncConnect always targets the pinned production gateway. The OIDC service
// supplies fresh access tokens to sync at request time, including rotations.
func (s *AppService) SyncConnect(ctx context.Context) error {
	_, err := s.call(ctx, "builtin.sync", "sync:manage", "sync:connect", map[string]any{
		"action": "connect",
	}, func(callCtx context.Context) (any, error) {
		if s.container.Sync == nil || s.container.Auth == nil {
			return nil, syncclient.ErrInvalidDependencies
		}
		if _, err := s.container.Auth.EnsureAuthenticated(callCtx); err != nil {
			return nil, err
		}
		return nil, s.container.Sync.ConnectWithTokenSource(callCtx, productionSyncGateway, s.container.Auth)
	})
	return err
}

func (s *AppService) SyncNow(ctx context.Context) (SyncReportDTO, error) {
	result, err := s.call(ctx, "builtin.sync", "sync:manage", "sync:run", map[string]any{
		"action": "run",
	}, func(callCtx context.Context) (any, error) {
		if s.container.Sync == nil {
			return nil, syncclient.ErrInvalidDependencies
		}
		report, err := s.container.Sync.SyncNow(callCtx)
		return syncReportDTO(report), err
	})
	if err != nil {
		return SyncReportDTO{}, err
	}
	return result.(SyncReportDTO), nil
}

func (s *AppService) SyncDisconnect() error {
	_, err := s.call(context.Background(), "builtin.sync", "sync:manage", "sync:disconnect", map[string]any{
		"action": "disconnect",
	}, func(context.Context) (any, error) {
		if s.container.Sync == nil {
			return nil, syncclient.ErrInvalidDependencies
		}
		s.container.Sync.Disconnect()
		return nil, nil
	})
	return err
}

func syncStatusDTO(status syncclient.Status) SyncStatusDTO {
	return SyncStatusDTO{
		Connected: status.Connected,
		Endpoint:  status.Endpoint,
		AccountID: status.AccountID,
		DeviceID:  status.DeviceID,
		ExpiresAt: status.ExpiresAt,
	}
}

func syncReportDTO(report syncclient.Report) SyncReportDTO {
	return SyncReportDTO{
		PushAttempted: report.PushAttempted,
		PushSent:      report.PushSent,
		PushRetried:   report.PushRetried,
		PushConflicts: report.PushConflicts,
		Pulled:        report.Pulled,
		Applied:       report.Applied,
	}
}
