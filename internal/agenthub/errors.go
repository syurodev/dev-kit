package agenthub

import "errors"

var (
	ErrSessionNotFound    = errors.New("agenthub.sessionNotFound")
	ErrSessionBusy        = errors.New("agenthub.sessionBusy")
	ErrDepthLimitExceeded = errors.New("agenthub.depthLimitExceeded")
	ErrCycleDetected      = errors.New("agenthub.cycleDetected")
	ErrVendorUnknown      = errors.New("agenthub.vendorUnknown")
	ErrPermissionDenied   = errors.New("agenthub.permissionDenied")
	ErrCancelled          = errors.New("agenthub.cancelled")
	ErrProcessFailed      = errors.New("agenthub.processFailed")
	ErrSandboxUnavailable = errors.New("agenthub.sandboxUnavailable")
	ErrUnavailable        = errors.New("agenthub.unavailable")
)
