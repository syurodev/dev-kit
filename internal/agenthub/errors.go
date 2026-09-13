package agenthub

import "errors"

var (
	ErrSessionNotFound         = errors.New("agenthub.sessionNotFound")
	ErrTranscriptEntryNotFound = errors.New("agenthub.transcriptEntryNotFound")
	ErrSessionBusy             = errors.New("agenthub.sessionBusy")
	ErrDepthLimitExceeded      = errors.New("agenthub.depthLimitExceeded")
	ErrCycleDetected           = errors.New("agenthub.cycleDetected")
	ErrVendorUnknown           = errors.New("agenthub.vendorUnknown")
	ErrPermissionDenied        = errors.New("agenthub.permissionDenied")
	ErrCancelled               = errors.New("agenthub.cancelled")
	ErrProcessFailed           = errors.New("agenthub.processFailed")
	ErrSandboxUnavailable      = errors.New("agenthub.sandboxUnavailable")
	ErrUnavailable             = errors.New("agenthub.unavailable")
	ErrNotDescendant           = errors.New("agenthub.notDescendant")
	ErrCwdOutsideParent        = errors.New("agenthub.cwdOutsideParent")
	ErrInvalidCwd              = errors.New("agenthub.invalidCwd")
	ErrCallerMismatch          = errors.New("agenthub.callerMismatch")
	ErrProxyNotFound           = errors.New("agenthub.proxyNotFound")
	// ErrAgentAuthRequired is an agent with no working sign-in: an expired or
	// missing Claude login, or no API key. DevKit never signs anyone in; the
	// user does that in Claude Code itself.
	ErrAgentAuthRequired = errors.New("agenthub.authRequired")
	// ErrAgentNotInstalled is an adapter with no runtime DevKit installed and
	// no DEVKIT_CLAUDE_ACP_COMMAND override. Never a reason to try PATH or npx.
	ErrAgentNotInstalled = errors.New("agenthub.notInstalled")
)
