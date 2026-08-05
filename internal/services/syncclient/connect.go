package syncclient

import (
	"context"
	"time"
)

// Connect performs the authenticated handshake using the stable local device.
// The account and final device identity are accepted only from AuthSession.
func (s *Service) Connect(ctx context.Context, baseURL, accessToken string) error {
	if s == nil {
		return ErrInvalidDependencies
	}
	ctx = nonNilContext(ctx)
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	deviceID, err := s.deps.DeviceIdentity.EnsureLocalDevice(ctx)
	if err != nil {
		return safeStageError(ctx, ErrConnectFailed)
	}
	authenticator, err := s.factories.Authenticator(baseURL, s.deps.HTTPClient)
	if err != nil {
		return safeStageError(ctx, ErrConnectFailed)
	}
	session, err := authenticator.Authenticate(ctx, accessToken, deviceID)
	if err != nil {
		return safeStageError(ctx, ErrConnectFailed)
	}
	// Check an existing claim before constructing transport, but do not make the
	// first durable account claim until the user explicitly chooses SyncNow.
	if err := s.deps.Remote.ValidateAuthenticatedAccount(ctx, session.AccountID()); err != nil {
		return safeStageError(ctx, ErrConnectFailed)
	}
	provider := &sessionProvider{session: session, set: true}
	transport, err := s.factories.Transport(baseURL, provider, s.deps.HTTPClient)
	if err != nil {
		provider.clear()
		return safeStageError(ctx, ErrConnectFailed)
	}

	s.stateMu.Lock()
	previous := s.state
	s.state = &connection{endpoint: baseURL, provider: provider, transport: transport}
	s.stateMu.Unlock()
	if previous != nil {
		previous.provider.clear()
	}
	return nil
}

// Disconnect clears all runtime connection state. AuthSession intentionally
// stores its token in a private string, which Go cannot reliably zero; dropping
// the session is the strongest practical action without persisting a copy.
func (s *Service) Disconnect() {
	if s == nil {
		return
	}
	s.runMu.Lock()
	s.stateMu.Lock()
	previous := s.state
	s.state = nil
	s.stateMu.Unlock()
	if previous != nil {
		previous.provider.clear()
	}
	s.runMu.Unlock()
}

func (s *Service) Status() Status {
	if s == nil {
		return Status{}
	}
	s.stateMu.RLock()
	current := s.state
	if current == nil {
		s.stateMu.RUnlock()
		return Status{}
	}
	endpoint := current.endpoint
	provider := current.provider
	s.stateMu.RUnlock()
	session, ok := provider.metadata()
	if !ok {
		return Status{}
	}
	if err := session.Validate(time.Now().UTC()); err != nil {
		// Expired credentials must not remain available to later requests. The
		// compare-before-clear keeps a concurrent replacement connection intact.
		s.clearConnection(current)
		return Status{}
	}
	return Status{
		Connected: true,
		Endpoint:  endpoint,
		AccountID: session.AccountID(),
		DeviceID:  session.DeviceID(),
		ExpiresAt: session.ExpiresAt(),
	}
}
