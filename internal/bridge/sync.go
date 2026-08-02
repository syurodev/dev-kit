package bridge

import (
	"context"
	"time"

	"changeme/internal/services/syncclient"
)

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

func (s *AppService) SyncConnect(ctx context.Context, endpoint, accessToken string) error {
	_, err := s.call(ctx, "builtin.sync", "sync:manage", "sync:connect", map[string]any{
		// Security boundary: credentials, endpoint, and authenticated identity
		// are deliberately absent from audit metadata.
		"action": "connect",
	}, func(callCtx context.Context) (any, error) {
		if s.container.Sync == nil {
			return nil, syncclient.ErrInvalidDependencies
		}
		return nil, s.container.Sync.Connect(callCtx, endpoint, accessToken)
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
