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
	// ErrPermissionNotWaiting: the request answered is no longer waiting — the
	// agent withdrew it, the turn ended or it expired. Nothing was sent.
	ErrPermissionNotWaiting = errors.New("agenthub.permissionNotWaiting")
	ErrCancelled            = errors.New("agenthub.cancelled")
	ErrProcessFailed        = errors.New("agenthub.processFailed")
	ErrSandboxUnavailable   = errors.New("agenthub.sandboxUnavailable")
	ErrUnavailable          = errors.New("agenthub.unavailable")
	// ErrUnavailableInServerMode: server mode serves the whole bridge over
	// unauthenticated loopback HTTP, and a jailed agent shares the host
	// loopback (NetworkHost). It could answer its own permission prompts, so
	// no vendor runs there at all.
	ErrUnavailableInServerMode = errors.New("agenthub.unavailableInServerMode")
	ErrUnsupported             = errors.New("agenthub.unsupported")
	ErrNotDescendant           = errors.New("agenthub.notDescendant")
	ErrCwdOutsideParent        = errors.New("agenthub.cwdOutsideParent")
	ErrInvalidCwd              = errors.New("agenthub.invalidCwd")
	ErrWorkspaceAmbiguous      = errors.New("agenthub.workspaceAmbiguous")
	ErrWorkspaceMismatch       = errors.New("agenthub.workspaceMismatch")
	ErrInvalidSessionTitle     = errors.New("agenthub.invalidSessionTitle")
	ErrInvalidConfigOption     = errors.New("agenthub.invalidConfigOption")
	// ErrModelChoiceNeedsApproval is an agent's pick of model or effort that goes
	// beyond its parent's (model_choice.go), made without the person saying yes.
	ErrModelChoiceNeedsApproval = errors.New("agenthub.modelChoiceNeedsApproval")
	// ErrModeNotInherited: a child session could not be put in a permission
	// mode as strict as its parent's, so it was not started (mode_inherit.go).
	ErrModeNotInherited = errors.New("agenthub.modeNotInherited")
	// ErrModelProfileUnknown: the profile named does not exist, or belongs to
	// another vendor, or conflicts with the model asked for beside it.
	ErrModelProfileUnknown = errors.New("agenthub.modelProfileUnknown")
	// ErrModelsUnavailable is a running agent that would not say which models
	// it offers. It rides beside the cause (and the vendor) so the picker can
	// say that, instead of the process-failed sentence that told nothing.
	ErrModelsUnavailable = errors.New("agenthub.modelsUnavailable")
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
	// ErrAgentUsageLimit is a vendor refusing a turn because the account's
	// allowance is spent. Nothing crashed: the process is up, the session is
	// usable, and the next prompt after the window resets goes through. It
	// arrives inside a VendorError whose Message is the vendor's own sentence,
	// which is the only place that says when the limit lifts.
	ErrAgentUsageLimit = errors.New("agenthub.usageLimit")
	// ErrAllAccountsLimited: switching is on and every signed-in account of the
	// vendor is marked as having reached its limit. The turn is not started; the
	// sentence names when the first account returns.
	ErrAllAccountsLimited = errors.New("agenthub.allAccountsLimited")
	// ErrTurnFailed is a turn the vendor ended with an error of its own — the
	// context window full, a budget spent, a provider error — while its
	// process stays up. Not ErrProcessFailed: that read "Agent process failed"
	// and offered Restart for a session that was working, and hid the one
	// sentence (the vendor's) that said what to do.
	ErrTurnFailed = errors.New("agenthub.turnFailed")
)

// VendorError is an error that is about one vendor rather than about Agent
// Hub: which agent is not installed, whose sign-in expired. The error itself
// is unchanged — Error and errors.Is see straight through — so the only thing
// it adds is the name the person has to act on. Without it the bridge could
// only guess, and it guessed Claude.
type VendorError struct {
	Agent AgentType
	Err   error
	// Message is the vendor's own words for the problem, meant for the person
	// reading it ("You've hit your usage limit… try again at 3:10 PM."), when
	// the vendor gave any. The bridge shows it; Err stays the internal detail.
	Message string
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

// vendorMessageError is vendorError carrying the vendor's own message.
func vendorMessageError(agent AgentType, err error, message string) error {
	if err == nil {
		return nil
	}
	return &VendorError{Agent: agent, Err: err, Message: message}
}

// ErrorVendorMessage is the vendor's own message an error carries, or "".
func ErrorVendorMessage(err error) string {
	var v *VendorError
	if errors.As(err, &v) {
		return v.Message
	}
	return ""
}

// ErrorVendor is the vendor an error names, or "" when it names none.
func ErrorVendor(err error) AgentType {
	var v *VendorError
	if errors.As(err, &v) {
		return v.Agent
	}
	return ""
}
