package agenthub

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"
)

// statusCheckTimeout bounds AfterAccountSignIn's status read, which has no caller
// context of its own; a var for tests.
var statusCheckTimeout = time.Minute

// accountForSignIn finds the account a sign-in is for and the vendor that signs it in.
// Window only, like every account call.
func (s *Service) accountForSignIn(ctx context.Context, id string) (AccountRecord, VendorAdapter, error) {
	book, err := s.accountBook(ctx)
	if err != nil {
		return AccountRecord{}, nil, err
	}
	acct, err := book.Get(ctx, id)
	if err != nil {
		return AccountRecord{}, nil, err
	}
	adapter, ok := s.reg.Get(acct.Vendor)
	if !ok {
		return AccountRecord{}, nil, ErrVendorUnknown
	}
	return acct, adapter, nil
}

// prepareForSignIn makes the home (and its shared-store link) a login is about to
// write into. The built-in account is refused: it signs in from Settings → Agents →
// Runtimes, with the vendor's own home. It holds accountMgmt for the account's
// existence check and the folder work (no vendor call is made under it), so a
// removal cannot delete the home and have it created again here.
func (s *Service) prepareForSignIn(ctx context.Context, acct AccountRecord) error {
	if acct.Builtin() {
		return fmt.Errorf("%w: the built-in account signs in from Settings → Agents → Runtimes", ErrInvalidAccount)
	}
	if s.AccountSharedHome == nil {
		return ErrAccountsUnavailable
	}
	s.accountMgmt.Lock()
	defer s.accountMgmt.Unlock()
	if _, err := s.Accounts.Get(ctx, acct.ID); err != nil {
		return err
	}
	return PrepareAccountHome(acct.Vendor, acct.HomeDir, s.AccountSharedHome(acct.Vendor))
}

// AccountSignInCommand is the terminal sign-in for an added account (Claude): the
// vendor's own login command with the account's home as its config directory. DevKit
// never sees the login; the terminal tab runs it and the window learns how it ended
// from its exit (AfterAccountSignIn). Window only.
func (s *Service) AccountSignInCommand(ctx context.Context, accountID, methodID string) (SignInCommand, error) {
	acct, adapter, err := s.accountForSignIn(ctx, accountID)
	if err != nil {
		return SignInCommand{}, err
	}
	provider, ok := adapter.(AccountAuthProvider)
	if !ok {
		return SignInCommand{}, ErrAuthMethodUnknown
	}
	if err := s.prepareForSignIn(ctx, acct); err != nil {
		return SignInCommand{}, err
	}
	return provider.SignInCommandFor(ctx, methodID, acct.HomeDir)
}

// StartAccountSignIn is the in-app sign-in for an added account (Codex). method is
// "browser" or "deviceCode". It runs under accountProbeSpec(acct), the jail a probe
// and a session get (the account's home, DevKit's data directory hidden, the person's
// home-sharing choice), and ends with agenthub:signInCompleted carrying accountId.
// A sign-in holds the loopback callback port, so starting one cancels another in
// progress, for any account and the built-in one. Window only; the built-in account
// is refused (it signs in from Runtimes).
func (s *Service) StartAccountSignIn(ctx context.Context, accountID, method string) (SignInFlow, error) {
	acct, adapter, err := s.accountForSignIn(ctx, accountID)
	if err != nil {
		return SignInFlow{}, err
	}
	starter, ok := adapter.(AccountInAppSignIn)
	if !ok || (method != SignInBrowser && method != SignInDeviceCode) {
		return SignInFlow{}, ErrAuthMethodUnknown
	}
	if err := s.prepareForSignIn(ctx, acct); err != nil {
		return SignInFlow{}, err
	}
	spec := s.accountProbeSpec(acct)
	if method == SignInDeviceCode {
		return starter.StartDeviceSignInAt(ctx, spec, acct.ID)
	}
	return starter.StartBrowserSignInAt(ctx, spec, acct.ID)
}

// refreshStatus asks the vendor, under the account's probe jail, whether its home is
// signed in and records the answer, for any account including the built-in one. An
// answer it cannot get (the vendor could not start, did not answer, or does not
// report a status) is "unknown", not "signed out": the error is returned and the mark
// stays as it was. Neither s.mu nor accountMgmt is held across the vendor call.
func (s *Service) refreshStatus(ctx context.Context, acct AccountRecord) (AccountRecord, error) {
	adapter, ok := s.reg.Get(acct.Vendor)
	if !ok {
		return acct, ErrVendorUnknown
	}
	checker, ok := adapter.(SignInChecker)
	if !ok {
		return acct, ErrAuthMethodUnknown
	}
	vctx, cancel := context.WithTimeout(ctx, usageCheckTimeout)
	defer cancel()
	state, err := checker.CheckSignIn(vctx, s.accountProbeSpec(acct))
	if err != nil {
		return acct, err
	}
	// The answer is in: write it even if the caller's box ended in the meantime.
	saveCtx, cancelSave := context.WithTimeout(context.WithoutCancel(ctx), usageSaveTimeout)
	defer cancelSave()
	if state.SignedIn != acct.SignedIn {
		if err := s.Accounts.SetSignedIn(saveCtx, acct.ID, state.SignedIn); err != nil {
			return acct, err
		}
		s.accountsChanged()
	}
	return s.Accounts.Get(saveCtx, acct.ID)
}

// accountRow reads an account, creating the vendor's built-in row first when the id
// is one and nothing has listed the vendor yet.
func (s *Service) accountRow(ctx context.Context, book AccountBook, id string) (AccountRecord, error) {
	acct, err := book.Get(ctx, id)
	if !errors.Is(err, ErrAccountNotFound) {
		return acct, err
	}
	for _, vendor := range s.reg.Types() {
		if BuiltinAccountID(vendor) != id {
			continue
		}
		if _, err := book.List(ctx, vendor); err != nil {
			return AccountRecord{}, err
		}
		return book.Get(ctx, id)
	}
	return AccountRecord{}, ErrAccountNotFound
}

// RefreshAccountStatus is the "Refresh status" action, window only: it records what
// the vendor says about the account's home, so it is also the way an account marked
// signed out by a dead login is marked signed in again.
func (s *Service) RefreshAccountStatus(ctx context.Context, id string) (AccountRecord, error) {
	book, err := s.accountBook(ctx)
	if err != nil {
		return AccountRecord{}, err
	}
	acct, err := s.accountRow(ctx, book, id)
	if err != nil {
		return AccountRecord{}, err
	}
	return s.refreshStatus(ctx, acct)
}

// AfterAccountSignIn runs when a sign-in the window started has ended (the terminal
// exited, or the in-app flow completed): it re-reads the account's status. It has no
// caller identity of its own: the bridge and main.go call it after their own checks.
func (s *Service) AfterAccountSignIn(accountID string) {
	if s.Accounts == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), statusCheckTimeout)
	defer cancel()
	acct, err := s.accountRow(ctx, s.Accounts, accountID)
	if err != nil {
		return
	}
	if _, err := s.refreshStatus(ctx, acct); err != nil && !errors.Is(err, ErrAccountNotFound) {
		log.Printf("agenthub: account %s: reading its status after a sign-in: %v", accountID, err)
	}
}
