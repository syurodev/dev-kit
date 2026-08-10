package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
)

const maxAccessTokenLength = 8 * 1024

// AuthSession is an immutable snapshot of the authenticated account and
// device used by one sync request. Its fields are intentionally private so the
// bearer token cannot be changed or serialized accidentally.
type AuthSession struct {
	accountID   string
	deviceID    string
	accessToken string
	expiresAt   time.Time
}

// newAuthSession is deliberately package-private: account identity must come
// from the authenticated server handshake, never from UI or service input.
// Expiry is checked again by Validate for every request.
func newAuthSession(accountID, deviceID, accessToken string, expiresAt time.Time) (AuthSession, error) {
	session := AuthSession{
		accountID:   accountID,
		deviceID:    deviceID,
		accessToken: accessToken,
		expiresAt:   expiresAt.UTC(),
	}
	if err := session.validateIdentityAndToken(); err != nil {
		return AuthSession{}, err
	}
	return session, nil
}

// NewAuthSessionForRefresh reconstructs a server-authenticated session after
// only its bearer token changes. Callers receive identity from a previous
// successful handshake and must not use this constructor for initial login.
func NewAuthSessionForRefresh(accountID, deviceID, accessToken string, expiresAt time.Time) (AuthSession, error) {
	return newAuthSession(accountID, deviceID, accessToken, expiresAt)
}

func (s AuthSession) AccountID() string    { return s.accountID }
func (s AuthSession) DeviceID() string     { return s.deviceID }
func (s AuthSession) ExpiresAt() time.Time { return s.expiresAt }

// Validate fails closed when the caller cannot provide a trustworthy current
// time or when the session has reached its expiry instant.
func (s AuthSession) Validate(now time.Time) error {
	if err := s.validateIdentityAndToken(); err != nil {
		return err
	}
	if now.IsZero() {
		return errors.New("current time is required")
	}
	if !now.Before(s.expiresAt) {
		return errors.New("authentication session has expired")
	}
	return nil
}

func (s AuthSession) validateIdentityAndToken() error {
	if err := validateSyncIdentifier("account id", s.accountID, 256); err != nil {
		return err
	}
	if err := validateSyncIdentifier("device id", s.deviceID, 256); err != nil {
		return err
	}
	if s.expiresAt.IsZero() {
		return errors.New("authentication session expiry is required")
	}
	return validateAccessToken(s.accessToken)
}

func validateAccessToken(accessToken string) error {
	if accessToken == "" {
		return errors.New("access token is required")
	}
	if len(accessToken) > maxAccessTokenLength {
		return errors.New("access token is too long")
	}
	if strings.IndexFunc(accessToken, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) >= 0 {
		return errors.New("access token contains invalid characters")
	}
	return nil
}

// MarshalJSON deliberately exposes only non-secret session metadata. This is
// a defense in depth guard if a session reaches diagnostics or a DTO by error.
func (s AuthSession) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		AccountID string    `json:"account_id"`
		DeviceID  string    `json:"device_id"`
		ExpiresAt time.Time `json:"expires_at"`
	}{
		AccountID: s.accountID,
		DeviceID:  s.deviceID,
		ExpiresAt: s.expiresAt,
	})
}

// String and GoString prevent fmt-based diagnostics from exposing the token.
func (s AuthSession) String() string {
	return fmt.Sprintf("AuthSession{account=%q device=%q expires=%s}", s.accountID, s.deviceID, s.expiresAt.Format(time.RFC3339))
}

func (s AuthSession) GoString() string { return s.String() }

// Authenticator exchanges an existing access token and local device identity
// for server-authoritative sync session context.
type Authenticator interface {
	Authenticate(ctx context.Context, accessToken, localDeviceID string) (AuthSession, error)
}

// SessionProvider resolves the current authenticated session for every
// request. Implementations may refresh credentials, but must honor ctx.
type SessionProvider interface {
	Session(ctx context.Context) (AuthSession, error)
}
