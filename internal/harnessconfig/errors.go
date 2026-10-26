package harnessconfig

import (
	"errors"

	"devkit/internal/harness"
)

// Typed refusals. Messages are fixed host text: no SQL, path, JSON, command or
// vendor error ever reaches them.
var (
	ErrDenied             = &harness.Error{Code: harness.CodeProfileDenied, Message: "This caller cannot propose a harness change for that target."}
	ErrScopeGone          = &harness.Error{Code: harness.CodeProfileDenied, Message: "The session or workspace no longer exists."}
	ErrFactsUnavailable   = &harness.Error{Code: harness.CodeFactsUnavailable, Message: "DevKit cannot read the vendor's own servers and skills well enough to judge this change."}
	ErrExecutableDenied   = &harness.Error{Code: harness.CodeProfileDenied, Message: "Changes that add or alter a server or skill are not enabled."}
	ErrExecutableNotReady = &harness.Error{Code: harness.CodeNotAvailable, Message: "Changes that add or alter a server or skill cannot be shown in full yet."}
	ErrNoChange           = &harness.Error{Code: harness.CodeNoChange, Message: "The proposal changes nothing that is in effect."}
	ErrInvalid            = &harness.Error{Code: harness.CodeProfileInvalid, Message: "The proposal is not valid."}
	ErrStale              = &harness.Error{Code: harness.CodeProposalStale, Message: "The configuration changed after the proposal was prepared. Propose it again."}
	ErrConflict           = &harness.Error{Code: harness.CodeProfileConflict, Message: "The workspace profile changed. Propose it again."}
	ErrUnknownProposal    = &harness.Error{Code: harness.CodeProposalStale, Message: "That proposal is not pending."}
	ErrBusy               = &harness.Error{Code: harness.CodeProposalStale, Message: "Too many proposals are waiting."}
	ErrNotConfirmed       = &harness.Error{Code: harness.CodeProfileDenied, Message: "The change was not approved."}
	ErrConfirmUnavailable = &harness.Error{Code: harness.CodeProfileDenied, Message: "No one can approve a harness change right now."}
)

// invalid wraps a harness.Error from the resolver or validator without losing
// its code; anything else becomes the generic invalid proposal.
func invalid(err error) error {
	var he *harness.Error
	if errors.As(err, &he) {
		return he
	}
	return ErrInvalid
}
