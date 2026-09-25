package agenthub

import "errors"

var (
	ErrSessionNotFound         = errors.New("agenthub.sessionNotFound")
	ErrTranscriptEntryNotFound = errors.New("agenthub.transcriptEntryNotFound")
	// ErrCheckpointNotFound is a checkpoint id that is not in the table —
	// retention swept it, or a session it belonged to was deleted.
	ErrCheckpointNotFound = errors.New("agenthub.checkpointNotFound")
	// ErrRestorePlanStale is an Apply whose preview is gone: already used,
	// expired, or from a process that has since restarted. The dialog the user
	// read described a project that may have moved on, so it is made again
	// rather than carried out.
	ErrRestorePlanStale = errors.New("agenthub.restorePlanStale")
	// ErrRestoreNotPermitted is an agent asking to restore files. Undoing a
	// turn is the person's, not the agent's: an agent that could reverse its
	// own work could hide it.
	ErrRestoreNotPermitted = errors.New("agenthub.restoreNotPermitted")
	ErrSessionBusy         = errors.New("agenthub.sessionBusy")
	// ErrSessionClosed is a settings call on a session whose process is parked.
	// Not sessionNotFound: the conversation is there and a prompt brings it
	// back, so the message has to say that rather than deny the session exists.
	ErrSessionClosed      = errors.New("agenthub.sessionClosed")
	ErrDepthLimitExceeded = errors.New("agenthub.depthLimitExceeded")
	ErrCycleDetected      = errors.New("agenthub.cycleDetected")
	ErrVendorUnknown      = errors.New("agenthub.vendorUnknown")
	ErrPermissionDenied   = errors.New("agenthub.permissionDenied")
	ErrCancelled          = errors.New("agenthub.cancelled")
	ErrProcessFailed      = errors.New("agenthub.processFailed")
	ErrSandboxUnavailable = errors.New("agenthub.sandboxUnavailable")
	ErrUnavailable        = errors.New("agenthub.unavailable")
	// ErrUnavailableInServerMode: server mode serves the whole bridge over
	// unauthenticated loopback HTTP, and a jailed agent shares the host
	// loopback (NetworkHost). It could answer its own permission prompts, so
	// no vendor runs there at all.
	ErrUnavailableInServerMode = errors.New("agenthub.unavailableInServerMode")
	ErrUnsupported             = errors.New("agenthub.unsupported")
	ErrNotDescendant           = errors.New("agenthub.notDescendant")
	ErrCwdOutsideParent        = errors.New("agenthub.cwdOutsideParent")
	ErrInvalidCwd              = errors.New("agenthub.invalidCwd")
	ErrInvalidSessionTitle     = errors.New("agenthub.invalidSessionTitle")
	ErrInvalidConfigOption     = errors.New("agenthub.invalidConfigOption")
	// ErrInvalidPrompt is a prompt DevKit refuses to send: empty, or with an
	// attachment or pasted image that is unreadable, unsupported or too large.
	ErrInvalidPrompt  = errors.New("agenthub.invalidPrompt")
	ErrCallerMismatch = errors.New("agenthub.callerMismatch")
	ErrProxyNotFound  = errors.New("agenthub.proxyNotFound")
	// ErrAgentAuthRequired is an agent with no working sign-in: an expired or
	// missing login, or an API key that is gone or refused. An adapter returns
	// it inside a VendorError, so the message can say whose sign-in to fix.
	ErrAgentAuthRequired = errors.New("agenthub.authRequired")
	// ErrAgentAPIKeyUnusable is the API key chosen in Settings failing: gone
	// from Accounts, unreadable, or refused by the vendor. It always comes
	// with ErrAgentAuthRequired — nothing about the agent works without it —
	// but the fix is a different one: signing in to the vendor's account
	// changes nothing while a key is chosen, since the key wins.
	ErrAgentAPIKeyUnusable = errors.New("agenthub.apiKeyUnusable")
	// ErrAgentNotInstalled is an adapter with no runtime DevKit installed and
	// no DEVKIT_*_COMMAND override. Never a reason to try PATH or npx. Like
	// ErrAgentAuthRequired, it arrives inside a VendorError.
	ErrAgentNotInstalled = errors.New("agenthub.notInstalled")
)

// VendorError is an error that is about one vendor rather than about Agent
// Hub: which agent is not installed, whose sign-in expired. The error itself
// is unchanged — Error and errors.Is see straight through — so the only thing
// it adds is the name the person has to act on. Without it the bridge could
// only guess, and it guessed Claude.
type VendorError struct {
	Agent AgentType
	Err   error
}

func (e *VendorError) Error() string { return e.Err.Error() }

func (e *VendorError) Unwrap() error { return e.Err }

// vendorError names the vendor err is about; nil stays nil.
func vendorError(agent AgentType, err error) error {
	if err == nil {
		return nil
	}
	return &VendorError{Agent: agent, Err: err}
}

// ErrorVendor is the vendor an error names, or "" when it names none.
func ErrorVendor(err error) AgentType {
	var v *VendorError
	if errors.As(err, &v) {
		return v.Agent
	}
	return ""
}
