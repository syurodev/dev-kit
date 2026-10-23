package agenthub

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"devkit/internal/identity"
)

var (
	// ErrAccountsUnavailable: no account book, folder or shared home is wired.
	ErrAccountsUnavailable = errors.New("agenthub.accountsUnavailable")
	// ErrAccountLabelTaken: another account of the vendor has that name.
	ErrAccountLabelTaken = errors.New("agenthub.accountLabelTaken")
	// ErrAccountInUse: a running session's process runs as the account.
	ErrAccountInUse = errors.New("agenthub.accountInUse")
	// ErrAccountFolderLeft: the account was removed but its folder could not be
	// deleted. Not an ErrAccountHome: that one is "nothing was changed", and here the
	// row is already gone.
	ErrAccountFolderLeft = errors.New("agenthub.accountFolderLeft")
)

const maxAccountLabel = 40

// requireWindow refuses anything but the person's own window: accounts steer which
// of their subscriptions a session spends, so an agent never manages them.
func requireWindow(ctx context.Context) error {
	if caller, ok := identity.FromContext(ctx); ok && caller.Kind != identity.KindUI {
		return ErrPermissionDenied
	}
	return nil
}

func (s *Service) accountBook(ctx context.Context) (AccountBook, error) {
	if err := requireWindow(ctx); err != nil {
		return nil, err
	}
	if s.Accounts == nil {
		return nil, ErrAccountsUnavailable
	}
	return s.Accounts, nil
}

func (s *Service) accountsChanged() {
	if s.OnAccountsChanged != nil {
		s.OnAccountsChanged()
	}
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func accountSlug(label string) string {
	slug := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(label), "-"), "-")
	if slug == "" {
		slug = "account"
	}
	if len(slug) > 24 {
		slug = strings.TrimRight(slug[:24], "-")
	}
	return slug
}

// accountIDCandidate is the nth id for a label: "<vendor>-<slug of the label>", then
// "...-2", "...-3". n starts at 1.
func accountIDCandidate(vendor AgentType, label string, n int) string {
	base := string(vendor) + "-" + accountSlug(label)
	if n <= 1 {
		return base
	}
	return fmt.Sprintf("%s-%d", base, n)
}

// newAccountHome picks the first id for the label that no account has, that is not
// the built-in account's, and whose home does not exist on disk: a folder left by a
// failed removal or a crash is taken, so a new account never inherits its old login.
func (s *Service) newAccountHome(vendor AgentType, label string, taken map[string]bool) (id, home string, err error) {
	for n := 1; ; n++ {
		id = accountIDCandidate(vendor, label, n)
		if taken[id] || id == BuiltinAccountID(vendor) {
			continue
		}
		if home, err = s.accountHomeFor(vendor, id); err != nil {
			return "", "", err
		}
		if _, statErr := os.Lstat(home); statErr == nil {
			continue
		} else if !errors.Is(statErr, fs.ErrNotExist) {
			return "", "", fmt.Errorf("%w: %s: %w", ErrAccountHome, home, statErr)
		}
		return id, home, nil
	}
}

func cleanAccountLabel(label string) (string, error) {
	label = strings.TrimSpace(label)
	switch {
	case label == "":
		return "", fmt.Errorf("%w: an account needs a name", ErrInvalidAccount)
	case utf8.RuneCountInString(label) > maxAccountLabel:
		return "", fmt.Errorf("%w: a name is at most %d characters", ErrInvalidAccount, maxAccountLabel)
	case strings.ContainsFunc(label, unicode.IsControl):
		return "", fmt.Errorf("%w: a name cannot hold control characters", ErrInvalidAccount)
	}
	return label, nil
}

// accountHomeFor is where an added account's home lives, and the only place Put may
// be asked to point at: absolute, clean, inside AccountsDir. carveOutAll matches
// paths lexically, so it must be built from the same AccountsDir string the data
// directory protection (ProtectDataDir) was built from.
func (s *Service) accountHomeFor(vendor AgentType, id string) (string, error) {
	if s.AccountsDir == "" || !filepath.IsAbs(s.AccountsDir) || s.AccountSharedHome == nil {
		return "", fmt.Errorf("%w: the folder for added accounts is not set", ErrAccountsUnavailable)
	}
	home := filepath.Join(s.AccountsDir, string(vendor), id)
	if !pathIsInside(home, s.AccountsDir) {
		return "", fmt.Errorf("%w: %s is not inside the accounts folder", ErrAccountHome, home)
	}
	if err := noLinkBetween(s.AccountsDir, home); err != nil {
		return "", err
	}
	return home, nil
}

// noLinkBetween refuses a path whose folders below dir (the vendor folder, and
// home itself) are symbolic links: a link there would carry a create or a
// recursive delete out of the accounts folder, past the lexical check. dir
// itself may be a link (a data directory under /var on macOS), and a component
// that does not exist yet is fine.
func noLinkBetween(dir, path string) error {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return fmt.Errorf("%w: %s is not inside the accounts folder", ErrAccountHome, path)
	}
	cur := dir
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return nil
		case err != nil:
			return fmt.Errorf("%w: %s: %w", ErrAccountHome, cur, err)
		case info.Mode()&os.ModeSymlink != 0:
			return fmt.Errorf("%w: %s is a symbolic link", ErrAccountHome, cur)
		}
	}
	return nil
}

func (s *Service) ListAccounts(ctx context.Context, vendor AgentType) ([]AccountRecord, error) {
	book, err := s.accountBook(ctx)
	if err != nil {
		return nil, err
	}
	// List creates the vendor's built-in row, so only a vendor this service runs may
	// reach it: a caller-chosen string must not leave a row behind.
	if _, ok := s.reg.Get(vendor); !ok {
		return nil, ErrVendorUnknown
	}
	return book.List(ctx, vendor)
}

func (s *Service) AddAccount(ctx context.Context, vendor AgentType, label string) (AccountRecord, error) {
	book, err := s.accountBook(ctx)
	if err != nil {
		return AccountRecord{}, err
	}
	// One management call at a time: each reads the book, then writes what it read.
	// Taken before s.mu (accountInUse, forgetAccount), never after.
	s.accountMgmt.Lock()
	defer s.accountMgmt.Unlock()
	if !slices.Contains(s.AccountVendors, vendor) {
		return AccountRecord{}, fmt.Errorf("%w: %s has no added accounts", ErrInvalidAccount, vendor)
	}
	if label, err = cleanAccountLabel(label); err != nil {
		return AccountRecord{}, err
	}
	existing, err := book.List(ctx, vendor)
	if err != nil {
		return AccountRecord{}, err
	}
	taken := make(map[string]bool, len(existing))
	for _, a := range existing {
		taken[a.ID] = true
		if strings.EqualFold(a.Label, label) {
			return AccountRecord{}, ErrAccountLabelTaken
		}
	}
	id, home, err := s.newAccountHome(vendor, label, taken)
	if err != nil {
		return AccountRecord{}, err
	}
	if err := PrepareAccountHome(vendor, home, s.AccountSharedHome(vendor)); err != nil {
		return AccountRecord{}, err
	}
	if err := book.Put(ctx, AccountRecord{ID: id, Vendor: vendor, Label: label, HomeDir: home}); err != nil {
		return AccountRecord{}, err
	}
	s.accountsChanged()
	return book.Get(ctx, id)
}

// editAccount reads an added account, lets change adjust the editable fields and
// writes them back. Usage and markers are never touched (Put does not write them).
// The whole read-change-write holds accountMgmt, so a removal cannot slip between
// the read and the write and have the row put back.
func (s *Service) editAccount(ctx context.Context, id string, change func(AccountBook, *AccountRecord) error) (AccountRecord, error) {
	book, err := s.accountBook(ctx)
	if err != nil {
		return AccountRecord{}, err
	}
	s.accountMgmt.Lock()
	defer s.accountMgmt.Unlock()
	acct, err := book.Get(ctx, id)
	if err != nil {
		return AccountRecord{}, err
	}
	if acct.Builtin() {
		return AccountRecord{}, fmt.Errorf("%w: the built-in account cannot be changed here", ErrInvalidAccount)
	}
	if err := change(book, &acct); err != nil {
		return AccountRecord{}, err
	}
	if err := book.Put(ctx, acct); err != nil {
		return AccountRecord{}, err
	}
	s.accountsChanged()
	return book.Get(ctx, id)
}

func (s *Service) RenameAccount(ctx context.Context, id, label string) (AccountRecord, error) {
	if err := requireWindow(ctx); err != nil {
		return AccountRecord{}, err
	}
	label, err := cleanAccountLabel(label)
	if err != nil {
		return AccountRecord{}, err
	}
	return s.editAccount(ctx, id, func(book AccountBook, a *AccountRecord) error {
		others, err := book.List(ctx, a.Vendor)
		if err != nil {
			return err
		}
		for _, o := range others {
			if o.ID != a.ID && strings.EqualFold(o.Label, label) {
				return ErrAccountLabelTaken
			}
		}
		a.Label = label
		return nil
	})
}

func (s *Service) SetAccountShared(ctx context.Context, id string, shared bool) (AccountRecord, error) {
	return s.editAccount(ctx, id, func(_ AccountBook, a *AccountRecord) error {
		a.Shared = shared
		return nil
	})
}

// accountInUse reports whether a session's process runs as the account, or is
// being opened as it: resumeRuntime records the account long before it publishes
// s.runtimes[id], so a session with its resume barrier up counts too.
//
// Residual gaps, accepted: a resume that has not yet reached recordSessionAccount
// (it reads the session's row, which still names the account), and a moveSession
// whose target is this account, are not seen. Either can open a process in a home
// that is removed a moment later; that process fails on its next turn and the
// session returns to the built-in account when the row is rewritten.
func (s *Service) accountInUse(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for sessionID, acct := range s.sessionAccount {
		if acct != id {
			continue
		}
		if s.runtimes[sessionID] != nil || s.resuming[sessionID] != nil {
			return true
		}
	}
	return false
}

// RemoveAccount deletes an added account, returns its sessions to the built-in
// account and removes its home. os.RemoveAll never follows a symlink, so the shared
// conversation store its sessions link to is untouched.
func (s *Service) RemoveAccount(ctx context.Context, id string) error {
	book, err := s.accountBook(ctx)
	if err != nil {
		return err
	}
	s.accountMgmt.Lock()
	defer s.accountMgmt.Unlock()
	acct, err := book.Get(ctx, id)
	if err != nil {
		return err
	}
	if acct.Builtin() {
		return fmt.Errorf("%w: the built-in account cannot be removed", ErrInvalidAccount)
	}
	if s.accountInUse(id) {
		return ErrAccountInUse
	}
	// Refuse before deleting the row: with no accounts folder the home could not be
	// removed, and the person would be left with a row-less folder.
	if acct.HomeDir != "" && s.AccountsDir == "" {
		return fmt.Errorf("%w: the folder for added accounts is not set", ErrAccountsUnavailable)
	}
	if err := book.Delete(ctx, id); err != nil {
		return err
	}
	s.forgetAccount(id)
	s.forgetUsageCheck(id) // takes the check lock; s.mu was released by forgetAccount
	// Only the canonical home is DevKit's to delete. This check is not atomic with
	// the delete below and assumes nothing else writes to the accounts folder.
	want := filepath.Join(s.AccountsDir, string(acct.Vendor), acct.ID)
	linkErr := error(nil)
	if s.AccountsDir != "" {
		linkErr = noLinkBetween(s.AccountsDir, acct.HomeDir)
	}
	switch {
	case s.AccountsDir == "" || acct.HomeDir != want || linkErr != nil:
		log.Printf("agenthub: account %s removed; its home %q is not DevKit's to delete and was left alone (expected %q; link check: %v)", id, acct.HomeDir, want, linkErr)
	default:
		if err := os.RemoveAll(acct.HomeDir); err != nil {
			s.accountsChanged()
			return fmt.Errorf("%w: %s: %v", ErrAccountFolderLeft, acct.HomeDir, err)
		}
	}
	s.accountsChanged()
	return nil
}

// forgetAccount drops every in-memory trace of a removed account. The book's
// Delete returns the sessions' rows to the built-in account, but the service
// remembers which account a session's runtime runs as (a session whose process
// was evicted keeps its entry), what the last usage written for it was, and
// which account a session last moved away from: left behind, observed usage
// would be written to an account that is gone and the pick rules would name it.
func (s *Service) forgetAccount(id string) {
	s.mu.Lock()
	for sessionID, acct := range s.sessionAccount {
		if acct == id {
			delete(s.sessionAccount, sessionID)
		}
	}
	for sessionID, sw := range s.accountSwitches {
		if sw.from == id {
			delete(s.accountSwitches, sessionID)
		}
	}
	delete(s.usageSeen, id)
	s.mu.Unlock()
}

// NotifyAccountsChanged tells whoever listens (the window's Accounts pages) that the
// accounts or the rules that act on them changed. The bridge calls it after the
// switching rules are saved; the Service's own writes already call accountsChanged.
func (s *Service) NotifyAccountsChanged() { s.accountsChanged() }
