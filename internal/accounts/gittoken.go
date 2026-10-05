package accounts

import (
	"errors"
	"strings"
	"unicode"
)

// ErrTokenInvalid is a token or username that cannot be what git is given: empty,
// or holding whitespace or control characters, which is what a paste with a
// trailing newline or a wrapped line looks like.
var ErrTokenInvalid = errors.New("accounts.tokenInvalid")

// ErrUsernameRequired is a provider that authenticates a token with a username
// and none was given.
var ErrUsernameRequired = errors.New("accounts.usernameRequired")

// GitTokenInput is a personal access token for a git host, used over HTTPS.
type GitTokenInput struct {
	Provider string
	Label    string
	// User is the username git sends with the token. GitHub and GitLab do not
	// look at it for a personal access token (so it may be empty, and the
	// account's login is used); Bitbucket does, and requires it.
	User  string
	Token string
	// Local keeps the token on this machine; false syncs it with the vault.
	Local bool
}

// CreateGitToken stores a token that authenticates git over HTTPS for one of the
// git hosts.
//
// The token is stored as it would be for any other use of the account, so the
// API client can use the same account for api.github.com; what makes this a git
// account is only that the provider is one that hosts repositories.
func (s *Service) CreateGitToken(in GitTokenInput) (Account, error) {
	provider, ok := s.providers.Get(in.Provider)
	// SSHHost is what marks a provider that hosts repositories: GitHub, GitLab
	// and Bitbucket. A token for Anthropic is not something git is given.
	if !ok || provider.SSHHost == "" {
		return Account{}, ErrInvalid
	}
	token, user := strings.TrimSpace(in.Token), strings.TrimSpace(in.User)
	if !plainCredential(token) || (user != "" && !plainCredential(user)) {
		return Account{}, ErrTokenInvalid
	}
	kind := KindToken
	if provider.ID == "bitbucket" {
		// Bitbucket's app passwords are a username and a password, and it
		// refuses the pair without the username.
		if user == "" {
			return Account{}, ErrUsernameRequired
		}
		kind = KindBasic
	}
	return s.Create(CreateInput{
		Provider: in.Provider, Kind: kind, Label: in.Label,
		User: user, Secret: token, Local: in.Local,
	})
}

// plainCredential is a non-empty string with no whitespace or control character
// in it.
func plainCredential(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}
