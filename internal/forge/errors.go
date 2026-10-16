package forge

import (
	"errors"
	"fmt"
	"time"
)

// ErrorKind classifies a failure by what a person can do about it.
type ErrorKind string

const (
	// ErrUnauthorized: the host refused the token (401) — it is wrong, expired or revoked.
	ErrUnauthorized ErrorKind = "unauthorized"
	// ErrForbidden: the token is valid but may not do this (403, missing scope, SSO not authorised).
	ErrForbidden ErrorKind = "forbidden"
	// ErrNotFound: the repository or change request does not exist — or the token cannot see it.
	ErrNotFound ErrorKind = "not_found"
	// ErrRateLimited: the host's limit is spent; RetryAt says when it lifts.
	ErrRateLimited ErrorKind = "rate_limited"
	// ErrValidation: the host rejected what was sent (422): no commits between branches,
	// a change request already exists, a branch it cannot find.
	ErrValidation ErrorKind = "validation"
	// ErrUnavailable: the host or the network failed (5xx, timeout).
	ErrUnavailable ErrorKind = "unavailable"
	// ErrInvalid: the request was malformed before it was sent.
	ErrInvalid ErrorKind = "invalid"
)

// Error is a failed call to a host, safe to show: it carries the host's own
// message and never the token that made the call.
type Error struct {
	Kind    ErrorKind
	Status  int
	Message string
	// RetryAt is when a rate limit lifts; zero when not known or not a rate limit.
	RetryAt time.Time
}

func (e *Error) Error() string {
	if e.Message == "" {
		return string(e.Kind)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

// KindOf is the kind of err when it is (or wraps) an *Error, "" otherwise.
func KindOf(err error) ErrorKind {
	var fe *Error
	if errors.As(err, &fe) {
		return fe.Kind
	}
	return ""
}

// RetryAtOf is when a rate limit lifts, the zero time otherwise.
func RetryAtOf(err error) time.Time {
	var fe *Error
	if errors.As(err, &fe) {
		return fe.RetryAt
	}
	return time.Time{}
}
