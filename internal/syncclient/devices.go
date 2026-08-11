package syncclient

import (
	"context"
	"errors"
	"time"

	replicationsync "devkit/internal/sync"
)

var (
	ErrListDevicesFailed  = errors.New("sync device list failed")
	ErrRevokeDeviceFailed = errors.New("sync device revoke failed")
)

// DeviceInfo is non-secret device metadata safe for UI/diagnostics.
type DeviceInfo struct {
	DeviceID   string
	Status     string
	CreatedAt  time.Time
	LastSeenAt time.Time
	Current    bool
}

type deviceAuthenticator interface {
	ListDevices(ctx context.Context, accessToken, localDeviceID string) ([]replicationsync.DeviceInfo, error)
	RevokeDevice(ctx context.Context, accessToken, localDeviceID, targetDeviceID string) error
}

// ListDevices returns account-scoped device metadata for the authenticated caller.
func (s *Service) ListDevices(ctx context.Context, tokens AccessTokenSource, baseURL string) ([]DeviceInfo, error) {
	if s == nil || tokens == nil || baseURL == "" {
		return nil, ErrInvalidDependencies
	}
	ctx = nonNilContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	localDeviceID, err := s.deps.DeviceIdentity.EnsureLocalDevice(ctx)
	if err != nil {
		return nil, ErrListDevicesFailed
	}
	accessToken, _, err := tokens.AccessToken(ctx)
	if err != nil {
		return nil, safeStageError(ctx, ErrListDevicesFailed)
	}
	authenticator, err := s.factories.Authenticator(baseURL, s.deps.HTTPClient)
	if err != nil {
		return nil, safeStageError(ctx, ErrListDevicesFailed)
	}
	deviceAuth, ok := authenticator.(deviceAuthenticator)
	if !ok {
		return nil, ErrListDevicesFailed
	}
	devices, err := deviceAuth.ListDevices(ctx, accessToken, localDeviceID)
	if err != nil {
		s.clearConnectionOnAuthFailure(err)
		return nil, safeStageError(ctx, ErrListDevicesFailed)
	}
	dtos := make([]DeviceInfo, 0, len(devices))
	for _, device := range devices {
		dtos = append(dtos, DeviceInfo{
			DeviceID:   device.DeviceID,
			Status:     device.Status,
			CreatedAt:  device.CreatedAt,
			LastSeenAt: device.LastSeenAt,
			Current:    device.Current,
		})
	}
	return dtos, nil
}

// RevokeDevice revokes another device or the caller when targetDeviceID matches
// the stable local device identity. The returned selfRevoked flag is true when
// the local device was the revoke target.
func (s *Service) RevokeDevice(ctx context.Context, tokens AccessTokenSource, baseURL, targetDeviceID string) (bool, error) {
	if s == nil || tokens == nil || baseURL == "" || targetDeviceID == "" {
		return false, ErrInvalidDependencies
	}
	ctx = nonNilContext(ctx)
	if err := ctx.Err(); err != nil {
		return false, err
	}
	localDeviceID, err := s.deps.DeviceIdentity.EnsureLocalDevice(ctx)
	if err != nil {
		return false, ErrRevokeDeviceFailed
	}
	accessToken, _, err := tokens.AccessToken(ctx)
	if err != nil {
		return false, safeStageError(ctx, ErrRevokeDeviceFailed)
	}
	authenticator, err := s.factories.Authenticator(baseURL, s.deps.HTTPClient)
	if err != nil {
		return false, safeStageError(ctx, ErrRevokeDeviceFailed)
	}
	deviceAuth, ok := authenticator.(deviceAuthenticator)
	if !ok {
		return false, ErrRevokeDeviceFailed
	}
	if err := deviceAuth.RevokeDevice(ctx, accessToken, localDeviceID, targetDeviceID); err != nil {
		s.clearConnectionOnAuthFailure(err)
		return false, safeStageError(ctx, ErrRevokeDeviceFailed)
	}
	return targetDeviceID == localDeviceID, nil
}

func (s *Service) clearConnectionOnAuthFailure(err error) {
	if !errors.Is(err, replicationsync.ErrAuthentication) {
		return
	}
	s.stateMu.RLock()
	current := s.state
	s.stateMu.RUnlock()
	if current != nil {
		s.clearConnection(current)
	}
}
