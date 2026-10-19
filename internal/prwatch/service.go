package prwatch

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"devkit/internal/forge"
)

// RemoteRef is a git remote, already read: the parts a host call needs.
type RemoteRef struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	// Host is the lower-cased host; Path the repository path with its case
	// ("acme/api", "group/sub/project"); Owner its first segment.
	Host  string `json:"host"`
	Path  string `json:"path"`
	Owner string `json:"owner"`
	// Env is the remote's environment tag; AccountID the account bound to it.
	Env       string `json:"env,omitempty"`
	AccountID string `json:"accountId,omitempty"`
}

// Checkout is where one folder stands.
type Checkout struct {
	// Folder is the work tree asked about; Open fills it in.
	Folder   string
	Branch   string
	Detached bool
	// Origin is where a change request is opened and looked up; Push is where
	// the branch is pushed. They are the same remote unless the branch lives on
	// a fork.
	Origin RemoteRef
	Push   RemoteRef
	// DefaultBranch is the origin's default, as git last saw it; "" if unknown.
	DefaultBranch string
}

// Source reads a folder. ErrNotRepository means the folder is no work tree.
type Source interface {
	Checkout(ctx context.Context, folder string) (Checkout, error)
}

// TokenRequest asks for the token to call one repository as.
type TokenRequest struct {
	Host  string
	Owner string
	Repo  string
	// Env and BoundAccountID come from the remote: its tag, and the account
	// the person bound to it.
	Env            string
	BoundAccountID string
	// Interactive lets the answer ask the person when two accounts fit. The
	// window polling in the background never does: a dialog nobody asked for.
	Interactive bool
}

// Credentials hands over a token for one call. Account names who it belongs to
// ("work") for messages; the token is never logged or returned to a caller.
type Credentials interface {
	Token(ctx context.Context, req TokenRequest) (token, account string, err error)
}

var (
	// ErrNotRepository: the folder is not a git work tree.
	ErrNotRepository = errors.New("prwatch: not a git repository")
	// ErrNoAccount: no account on this host fits the repository.
	ErrNoAccount = errors.New("prwatch: no account for this remote")
	// ErrAmbiguousAccount: two accounts fit and nobody chose.
	ErrAmbiguousAccount = errors.New("prwatch: more than one account fits this remote")
	// ErrUnsupportedHost: no provider serves the remote's host.
	ErrUnsupportedHost = errors.New("prwatch: no provider for this host")
	// ErrNoChangeRequest: the branch has no pull request.
	ErrNoChangeRequest = errors.New("prwatch: no pull request for this branch")
	// ErrDetached: HEAD is not on a branch.
	ErrDetached = errors.New("prwatch: HEAD is detached")
	// ErrProtectedBranch: the branch is the default or protected, and is left alone.
	ErrProtectedBranch = errors.New("prwatch: the branch is the default or protected")
)

// KeyOnlyError is ErrNoAccount for a remote that does have an account — an SSH
// key, which pushes code but cannot call the host's API. It carries only the
// key account's label (never the key), so the notice can say so instead of
// "no account" about a remote the person sees bound to one.
type KeyOnlyError struct {
	// Account is the key account's label, made safe to put in a sentence.
	// Empty when several keys fit and none is bound.
	Account string
}

// NewKeyOnlyError builds the error for a key account's label (may be "").
func NewKeyOnlyError(label string) *KeyOnlyError {
	return &KeyOnlyError{Account: safeLabel(label)}
}

func (e *KeyOnlyError) Error() string { return "prwatch: only an SSH key account fits this remote" }

// Is makes errors.Is(err, ErrNoAccount) true: every caller that handled "no
// account" still does, and only the wording learns more.
func (e *KeyOnlyError) Is(target error) bool { return target == ErrNoAccount }

// safeLabel reduces a person's account label to one short line with no
// parentheses: the window folds notices that differ only by a parenthesised
// repository (sharedMessage), and a label holding "(work)" would be dropped
// from the key and merge two different keys.
func safeLabel(label string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(label) {
		switch {
		case r == '(':
			b.WriteRune('[')
		case r == ')':
			b.WriteRune(']')
		case unicode.IsControl(r):
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	if runes := []rune(out); len(runes) > 60 {
		out = string(runes[:60]) + "…"
	}
	return out
}

const (
	// minAge is how long an answer is reused without asking the host: two
	// windows (or two sessions on one branch) cost one request, not two.
	minAge = 3 * time.Second
	// threadsEvery is how often the unresolved-thread count is re-read while
	// the request itself has not changed: resolving a thread does not bump the
	// request's updated_at, so only a timer can notice it.
	threadsEvery = 3 * time.Minute
	// concurrent bounds folders resolved at once.
	concurrent = 4
)

// Service tracks change requests. Its zero value is not usable; see New.
type Service struct {
	providers *forge.Registry
	src       Source
	creds     Credentials
	store     Store
	now       func() time.Time
	slots     chan struct{}

	// OnChanged is told after a write (or an attach) changed what State
	// answers for a session, so a window need not wait for its next poll.
	OnChanged func(sessionID string)

	mu      sync.Mutex
	entries map[string]*entry
	finds   map[string]found
}

type entry struct {
	mu         sync.Mutex
	cr         forge.ChangeRequest
	have       bool
	at         time.Time
	threadsAt  time.Time
	threadsFor time.Time
}

type found struct {
	at   time.Time
	list []forge.ChangeRequest
}

// New wires the service. store may be nil: hand-attached requests then last
// only until the process ends.
func New(providers *forge.Registry, src Source, creds Credentials, store Store) *Service {
	if store == nil {
		store = NewMemoryStore()
	}
	return &Service{
		providers: providers, src: src, creds: creds, store: store, now: time.Now,
		slots: make(chan struct{}, concurrent), entries: map[string]*entry{}, finds: map[string]found{},
	}
}

func (s *Service) changed(sessionID string) {
	if sessionID != "" && s.OnChanged != nil {
		s.OnChanged(sessionID)
	}
}

func (s *Service) entryFor(repo forge.Repo, number int) *entry {
	key := repo.String() + "#" + strconv.Itoa(number)
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.entries[key]
	if e == nil {
		e = &entry{}
		s.entries[key] = e
	}
	return e
}

func (s *Service) forget(repo forge.Repo, number int) {
	key := repo.String() + "#" + strconv.Itoa(number)
	s.mu.Lock()
	delete(s.entries, key)
	for k := range s.finds {
		if strings.HasPrefix(k, repo.String()+"|") {
			delete(s.finds, k)
		}
	}
	s.mu.Unlock()
}
