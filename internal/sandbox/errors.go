package sandbox

import "errors"

var (
	ErrUnavailable         = errors.New("sandbox unavailable")
	ErrUnsupportedPlatform = errors.New("sandbox unsupported on this platform")
	ErrInvalidPolicy       = errors.New("invalid sandbox policy")
)
