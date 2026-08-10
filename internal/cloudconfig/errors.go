package cloudconfig

import "errors"

var (
	// ErrUnavailable means no usable OIDC configuration could be loaded.
	ErrUnavailable = errors.New("cloud configuration is unavailable")
	// ErrAppBelowMinimum blocks Sync login when the running build is too old.
	ErrAppBelowMinimum = errors.New("app version is below minimum")
)
