package bridge

import (
	"context"
	"time"

	"devkit/internal/syncclient"
)

const productionSyncGateway = "https://api.synx.io.vn"

func (s *AppService) syncDependencyErr() error {
	if blocked := s.container.SyncBlockedErr(); blocked != nil {
		return blocked
	}
	return syncclient.ErrInvalidDependencies
}

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
	PushFailed    int `json:"push_failed"`
	Pulled        int `json:"pulled"`
	Applied       int `json:"applied"`
}

// SyncDeviceDTO is non-secret device metadata safe for the UI.
type SyncDeviceDTO struct {
	DeviceID   string    `json:"device_id"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	Current    bool      `json:"current"`
}

// SyncConflictDTO is non-secret conflict metadata for the UI. No record payload
// or plaintext secret crosses the boundary.
type SyncConflictDTO struct {
	RecordID            string    `json:"record_id"`
	RecordType          string    `json:"record_type"`
	RemoteOperation     string    `json:"remote_operation"`
	RemoteEntityVersion uint32    `json:"remote_entity_version"`
	TheirsDeleted       bool      `json:"theirs_deleted"`
	DetectedAt          time.Time `json:"detected_at"`
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
				RecordID:            conflict.RecordID,
				RecordType:          conflict.RecordType,
				RemoteOperation:     conflict.RemoteOperation,
				RemoteEntityVersion: conflict.RemoteEntityVersion,
				TheirsDeleted:       conflict.TheirsDeleted,
				DetectedAt:          conflict.DetectedAt,
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
		authService := s.container.Auth()
		if authService == nil {
			return nil, s.syncDependencyErr()
		}
		status := authService.Status()
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
		if blocked := s.container.SyncBlockedErr(); blocked != nil {
			return nil, blocked
		}
		authService := s.container.Auth()
		if authService == nil {
			return nil, s.syncDependencyErr()
		}
		status, err := authService.EnsureAuthenticated(callCtx)
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
		authService := s.container.Auth()
		if authService == nil {
			return nil, s.syncDependencyErr()
		}
		if s.container.Sync != nil {
			s.container.Sync.Disconnect()
		}
		return nil, authService.Logout()
	})
	return err
}

// SyncConnect always targets the pinned production gateway. The OIDC service
// supplies fresh access tokens to sync at request time, including rotations.
func (s *AppService) SyncConnect(ctx context.Context) error {
	_, err := s.call(ctx, "builtin.sync", "sync:manage", "sync:connect", map[string]any{
		"action": "connect",
	}, func(callCtx context.Context) (any, error) {
		if blocked := s.container.SyncBlockedErr(); blocked != nil {
			return nil, blocked
		}
		authService := s.container.Auth()
		if s.container.Sync == nil || authService == nil {
			return nil, s.syncDependencyErr()
		}
		if _, err := authService.EnsureAuthenticated(callCtx); err != nil {
			return nil, err
		}
		return nil, s.container.Sync.ConnectWithTokenSource(callCtx, productionSyncGateway, authService)
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

func (s *AppService) SyncListDevices(ctx context.Context) ([]SyncDeviceDTO, error) {
	result, err := s.call(ctx, "builtin.sync", "sync:manage", "sync:devices", map[string]any{
		"action": "list_devices",
	}, func(callCtx context.Context) (any, error) {
		if blocked := s.container.SyncBlockedErr(); blocked != nil {
			return nil, blocked
		}
		authService := s.container.Auth()
		if s.container.Sync == nil || authService == nil {
			return nil, s.syncDependencyErr()
		}
		if _, err := authService.EnsureAuthenticated(callCtx); err != nil {
			return nil, err
		}
		devices, err := s.container.Sync.ListDevices(callCtx, s.container.Auth(), productionSyncGateway)
		if err != nil {
			return nil, err
		}
		dtos := make([]SyncDeviceDTO, 0, len(devices))
		for _, device := range devices {
			dtos = append(dtos, SyncDeviceDTO{
				DeviceID:   device.DeviceID,
				Status:     device.Status,
				CreatedAt:  device.CreatedAt,
				LastSeenAt: device.LastSeenAt,
				Current:    device.Current,
			})
		}
		return dtos, nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]SyncDeviceDTO), nil
}

func (s *AppService) SyncRevokeDevice(ctx context.Context, targetDeviceID string) error {
	_, err := s.call(ctx, "builtin.sync", "sync:manage", "sync:devices", map[string]any{
		"action": "revoke_device",
	}, func(callCtx context.Context) (any, error) {
		if blocked := s.container.SyncBlockedErr(); blocked != nil {
			return nil, blocked
		}
		authService := s.container.Auth()
		if s.container.Sync == nil || authService == nil {
			return nil, s.syncDependencyErr()
		}
		if _, err := authService.EnsureAuthenticated(callCtx); err != nil {
			return nil, err
		}
		selfRevoked, err := s.container.Sync.RevokeDevice(callCtx, s.container.Auth(), productionSyncGateway, targetDeviceID)
		if err != nil {
			return nil, err
		}
		if selfRevoked {
			s.container.Sync.Disconnect()
		}
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
		PushFailed:    report.PushFailed,
		Pulled:        report.Pulled,
		Applied:       report.Applied,
	}
}
