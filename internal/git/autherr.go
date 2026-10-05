package git

import (
	"fmt"
	"strings"
)

// AuthError is a remote refusing the identity git offered.
//
// Git's own stderr says "Permission denied (publickey)" or "Repository not
// found" and nothing about which of the identities on this machine it was.
// With more than one account that sentence is the whole problem, so the error
// carries what DevKit knows: the remote, how it was reached, and the account
// bound to it, if any.
//
// It holds no secret and no URL with credentials in it, so a caller may word
// it for the person without redacting.
type AuthError struct {
	// Op is "push", "fetch" or "pull".
	Op     string
	Remote string
	// URL is the address with any password removed.
	URL       string
	Transport Transport
	// AccountID and Account are the bound account's id and label. Both are
	// empty when no account answered the remote, which means git's own
	// configuration — an ssh key or a credential helper — did.
	AccountID string
	Account   string
	Err       error
}

func (e *AuthError) Error() string {
	who := "git's own configuration"
	if e.Account != "" {
		who = "account " + e.Account
	} else if e.AccountID != "" {
		who = "the bound account"
	}
	return fmt.Sprintf("git %s %s: authentication failed (%s)", e.Op, e.Remote, who)
}

func (e *AuthError) Unwrap() error { return e.Err }

// authFailureMarkers are what git, GitHub and GitLab print when a remote turns
// an identity away. Matched on the lower-cased message.
//
// "could not read from remote repository" is deliberately absent: it follows a
// refused key, but it also follows a network failure, and calling a dead
// network a permissions problem sends the person to the wrong place.
var authFailureMarkers = []string{
	"permission denied (publickey",
	"authentication failed",
	"invalid username or password",
	"invalid username or token",
	"support for password authentication was removed",
	"could not read username",
	"could not read password",
	"http basic: access denied",
	"the requested url returned error: 401",
	"the requested url returned error: 403",
	"repository not found",
	"denied to ",
	"you are not allowed to push code to this project",
	"you are not allowed to download code from this project",
}

// isAuthFailure reports whether a git error is a remote refusing an identity.
func isAuthFailure(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, marker := range authFailureMarkers {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

// accountLabeler is the optional half of the credential resolver that can name
// an account. internal/app's resolver has it; a stub that does not simply
// leaves the label empty.
type accountLabeler interface {
	AccountLabel(id string) string
}

// explainRemoteError turns an authentication failure into an *AuthError that
// names the account involved, and returns every other error untouched.
func (s *Service) explainRemoteError(op string, remote Remote, err error) error {
	if !isAuthFailure(err) {
		return err
	}
	out := &AuthError{
		Op: op, Remote: remote.Name, URL: redactURL(remote.URL),
		Transport: transportOf(remote.URL), AccountID: remote.AccountID, Err: err,
	}
	// An SSH remote is authenticated by a key, never by the bound account
	// (see credentialEnv), so naming that account would blame the wrong thing.
	if out.Transport == TransportSSH {
		out.AccountID = ""
	}
	if namer, ok := s.creds.(accountLabeler); ok && out.AccountID != "" {
		out.Account = namer.AccountLabel(out.AccountID)
	}
	return out
}
