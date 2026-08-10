package auth

import (
	"context"
	"errors"
	"net/http"
	"sync"

	"golang.org/x/oauth2"
)

// refreshTokenSource loads the keychain refresh token and asks oauth2 to refresh.
// It persists a rotated refresh token before callers use the new access token.
type refreshTokenSource struct {
	mu        sync.Mutex
	config    *oauth2.Config
	store     RefreshTokenStore
	client    *http.Client
	callerCtx context.Context
}

func (s *refreshTokenSource) setCallerContext(ctx context.Context) {
	s.callerCtx = ctx
}

func (s *refreshTokenSource) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	refresh, found, err := s.store.Load()
	if err != nil || !found || !validToken(refresh) {
		return nil, ErrAuthenticationRequired
	}
	ctx := s.callerCtx
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, s.client)
	token, err := s.config.TokenSource(ctx, &oauth2.Token{RefreshToken: refresh}).Token()
	if err != nil {
		if isInvalidGrant(err) {
			_ = s.store.Delete()
			return nil, ErrAuthenticationRequired
		}
		return nil, ErrAuthenticationFailed
	}
	if token.RefreshToken == "" {
		return nil, ErrAuthenticationFailed
	}
	if err := s.store.Save(token.RefreshToken); err != nil {
		return nil, ErrAuthenticationFailed
	}
	if !validToken(token.AccessToken) || token.Expiry.IsZero() {
		return nil, ErrAuthenticationFailed
	}
	return token, nil
}

func isInvalidGrant(err error) bool {
	var rErr *oauth2.RetrieveError
	if errors.As(err, &rErr) {
		return rErr.Response != nil && (rErr.Response.StatusCode == 400 || rErr.Response.StatusCode == 401)
	}
	return false
}
