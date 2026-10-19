package prwatch

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"devkit/internal/forge"
)

// Status is why an item has, or lacks, a change request. Everything but
// "found" and "none" is a reason the card states, never a silence.
type Status string

const (
	StatusFound            Status = "found"
	StatusNone             Status = "none" // on a branch, no request yet
	StatusDetached         Status = "detached"
	StatusDefaultBranch    Status = "default_branch"
	StatusNotRepository    Status = "not_repository"
	StatusUnsupportedHost  Status = "unsupported_host"
	StatusNoAccount        Status = "no_account"
	StatusAmbiguousAccount Status = "ambiguous_account"
	StatusUnauthorized     Status = "unauthorized"
	StatusForbidden        Status = "forbidden"
	StatusNotFound         Status = "not_found"
	StatusRateLimited      Status = "rate_limited"
	StatusUnavailable      Status = "unavailable"
)

// Item is one folder's (or one attachment's) answer.
type Item struct {
	Folder string `json:"folder"`
	Branch string `json:"branch"`
	Status Status `json:"status"`
	// Message states the reason when Status is not found.
	Message string `json:"message,omitempty"`
	// Source is "branch" for a request found from the folder's branch and
	// "attached" for one tied to the session by hand or by pr.create.
	Source        string               `json:"source"`
	ChangeRequest *forge.ChangeRequest `json:"changeRequest,omitempty"`
	// RetryAt is when a rate limit lifts.
	RetryAt *time.Time `json:"retryAt,omitempty"`
	// Platform names the host's platform ("GitHub") for a message, when known.
	Platform string `json:"platform,omitempty"`
	// KeyAccountID is the SSH key account a "no token" notice is about, so the
	// token the window asks for can be attached to that key. Empty otherwise.
	KeyAccountID string `json:"keyAccountId,omitempty"`
}

// State is what the card draws, and when to ask again.
type State struct {
	Items []Item `json:"items"`
	// NextPollMs is the delay before the next State call is worth making; -1
	// when nothing will change by itself (merged, closed, or needs a person).
	NextPollMs int `json:"nextPollMs"`
}

// Poll delays. Quick while something is running, slow once it has settled.
const (
	pollRunning   = 15 * time.Second
	pollSettled   = 60 * time.Second
	pollNone      = 90 * time.Second
	pollRecover   = 60 * time.Second
	pollRateFloor = 60 * time.Second
)

// State reads every folder's branch and the session's attachments.
//
// It never opens a dialog and never fails as a whole: a folder that cannot be
// read is an Item with its reason, so one broken repository does not blank the
// card of a workspace of several.
func (s *Service) State(ctx context.Context, sessionID string, folders []string) State {
	items := make([]Item, len(folders))
	var wg sync.WaitGroup
	for i, folder := range folders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case s.slots <- struct{}{}:
				defer func() { <-s.slots }()
			case <-ctx.Done():
				items[i] = Item{Folder: folder, Source: "branch", Status: StatusUnavailable, Message: ctx.Err().Error()}
				return
			}
			items[i] = s.branchItem(ctx, folder)
		}()
	}
	wg.Wait()

	seen := map[string]bool{}
	for _, it := range items {
		if it.ChangeRequest != nil {
			seen[crKey(it.ChangeRequest.Repo, it.ChangeRequest.Number)] = true
		}
	}
	if attached, err := s.store.List(ctx, sessionID); err == nil {
		var extra []Item
		var mu sync.Mutex
		var awg sync.WaitGroup
		for _, a := range attached {
			if seen[crKey(forge.Repo{Host: a.Host, Path: a.Path}, a.Number)] {
				continue
			}
			awg.Add(1)
			go func() {
				defer awg.Done()
				select {
				case s.slots <- struct{}{}:
					defer func() { <-s.slots }()
				case <-ctx.Done():
					return
				}
				it := s.attachedItem(ctx, a)
				mu.Lock()
				extra = append(extra, it)
				mu.Unlock()
			}()
		}
		awg.Wait()
		sort.SliceStable(extra, func(i, j int) bool { return extra[i].Folder < extra[j].Folder })
		items = append(items, extra...)
	}
	return State{Items: items, NextPollMs: nextPoll(items, s.now())}
}

func crKey(repo forge.Repo, number int) string { return fmt.Sprintf("%s#%d", repo, number) }

func (s *Service) branchItem(ctx context.Context, folder string) Item {
	item := Item{Folder: folder, Source: "branch"}
	co, err := s.src.Checkout(ctx, folder)
	item.Branch = co.Branch
	switch {
	case errors.Is(err, ErrNotRepository):
		item.Status, item.Message = StatusNotRepository, "Not a git repository."
		return item
	case err != nil:
		item.Status, item.Message = StatusUnavailable, err.Error()
		return item
	case co.Detached || co.Branch == "":
		item.Status, item.Message = StatusDetached, "HEAD is not on a branch."
		return item
	case co.Origin.Host == "":
		item.Status, item.Message = StatusUnsupportedHost, "This repository has no remote on a code host."
		return item
	}
	provider := s.providers.For(co.Origin.Host)
	if provider == nil || co.Origin.Path == "" {
		item.Status, item.Message = StatusUnsupportedHost, "No pull request support for "+co.Origin.Host+" yet."
		return item
	}
	item.Platform = provider.Terms().Platform
	if co.DefaultBranch != "" && co.Branch == co.DefaultBranch && co.Push.Path == co.Origin.Path {
		item.Status, item.Message = StatusDefaultBranch, "On the default branch."
		return item
	}
	h, err := s.Open(ctx, OpenInput{Folder: folder})
	if err != nil {
		return failure(item, provider.Terms(), co.Origin, err)
	}
	list, err := h.List(ctx)
	if err != nil {
		return failure(item, provider.Terms(), co.Origin, err)
	}
	best := pick(list)
	if best == nil {
		item.Status = StatusNone
		return item
	}
	cr, err := s.fetch(ctx, h.provider, h.cred, h.repo, best.Number, true)
	if err != nil {
		return failure(item, provider.Terms(), co.Origin, err)
	}
	item.Status, item.ChangeRequest = StatusFound, &cr
	return item
}

func (s *Service) attachedItem(ctx context.Context, a Attachment) Item {
	item := Item{Folder: a.Folder, Source: "attached"}
	provider := s.providers.For(a.Host)
	if provider == nil {
		item.Status, item.Message = StatusUnsupportedHost, "No pull request support for "+a.Host+" yet."
		return item
	}
	terms := provider.Terms()
	item.Platform = terms.Platform
	owner, _, _ := cutOwner(a.Path)
	token, _, err := s.creds.Token(ctx, TokenRequest{Host: a.Host, Owner: owner, Repo: a.Path})
	if err != nil {
		return failure(item, terms, RemoteRef{Host: a.Host, Owner: owner, Path: a.Path}, err)
	}
	repo := forge.Repo{Host: a.Host, Path: a.Path}
	cr, err := s.fetch(ctx, provider, forge.Credential{Token: token}, repo, a.Number, true)
	if err != nil {
		return failure(item, terms, RemoteRef{Host: a.Host, Owner: owner, Path: a.Path}, err)
	}
	item.Branch = cr.Head
	item.Status, item.ChangeRequest = StatusFound, &cr
	return item
}

func cutOwner(path string) (owner, rest string, ok bool) {
	for i := 0; i < len(path); i++ {
		if path[i] == '/' {
			return path[:i], path[i+1:], true
		}
	}
	return path, "", false
}

// failure words a failed call as the reason on an Item.
func failure(item Item, terms forge.Terms, remote RemoteRef, err error) Item {
	platform := terms.Platform
	where := remote.Host
	if remote.Path != "" {
		where = remote.Path + " on " + remote.Host
	}
	switch {
	case errors.Is(err, ErrNoAccount):
		item.Status = StatusNoAccount
		var keyOnly *KeyOnlyError
		if errors.As(err, &keyOnly) {
			item.Message = keyOnlyMessage(platform, terms.Long, keyOnly.Account)
			item.KeyAccountID = keyOnly.AccountID
			break
		}
		item.Message = fmt.Sprintf("No %s account for this remote (%s). Add a token in Accounts.", platform, where)
	case errors.Is(err, ErrAmbiguousAccount):
		item.Status = StatusAmbiguousAccount
		item.Message = fmt.Sprintf("More than one %s account fits %s. Push once and choose, or bind one to the remote.", platform, where)
	case errors.Is(err, ErrUnsupportedHost):
		item.Status, item.Message = StatusUnsupportedHost, "No pull request support for "+remote.Host+" yet."
	case errors.Is(err, ErrNotRepository):
		item.Status, item.Message = StatusNotRepository, "Not a git repository."
	default:
		switch forge.KindOf(err) {
		case forge.ErrUnauthorized:
			item.Status = StatusUnauthorized
			item.Message = fmt.Sprintf("%s rejected the account's token (expired or revoked). Replace it in Accounts.", platform)
		case forge.ErrForbidden:
			item.Status = StatusForbidden
			item.Message = fmt.Sprintf("The token may not read %s: %s", where, err.Error())
		case forge.ErrNotFound:
			item.Status = StatusNotFound
			item.Message = fmt.Sprintf("%s cannot find %s — or the token cannot see it.", platform, where)
		case forge.ErrRateLimited:
			item.Status = StatusRateLimited
			if at := forge.RetryAtOf(err); !at.IsZero() {
				item.RetryAt = &at
			}
			item.Message = platform + " rate limit reached."
			if item.RetryAt != nil {
				item.Message += " Resumes at " + item.RetryAt.Local().Format("15:04") + "."
			}
		default:
			item.Status = StatusUnavailable
			item.Message = err.Error()
		}
	}
	return item
}

// keyOnlyMessage is the no_account notice for a remote whose only account is an
// SSH key. It names no repository, so the window folds every folder that uses
// the same key into one notice (sharedMessage), and a different key reads as a
// different notice. The words "with read_api access" avoid parentheses, which
// that folding would strip.
func keyOnlyMessage(platform, noun, key string) string {
	subject := "The " + platform + " SSH key"
	if key != "" {
		subject = "The " + platform + " key “" + key + "”"
	}
	scope := ""
	if platform == "GitLab" {
		scope = " with read_api access"
	}
	return fmt.Sprintf("%s pushes your code but can't read %ss. Add a %s token%s in Accounts.", subject, noun, platform, scope)
}

// nextPoll is the shortest delay any item asks for; -1 when none wants one.
func nextPoll(items []Item, now time.Time) int {
	best := time.Duration(-1)
	ask := func(d time.Duration) {
		if best < 0 || d < best {
			best = d
		}
	}
	for _, it := range items {
		switch it.Status {
		case StatusFound:
			cr := it.ChangeRequest
			switch {
			case cr == nil || cr.State.Terminal():
			case cr.Checks.Running > 0 || cr.Mergeable == forge.MergeUnknown:
				ask(pollRunning)
			default:
				ask(pollSettled)
			}
		case StatusNone:
			ask(pollNone)
		case StatusUnavailable:
			ask(pollRecover)
		case StatusRateLimited:
			var wait time.Duration
			if it.RetryAt != nil {
				wait = it.RetryAt.Sub(now)
			}
			if wait < pollRateFloor {
				wait = pollRateFloor
			}
			ask(wait)
		}
		// default branch, detached, no account, bad token, unsupported host:
		// nothing changes without the person (or a turn, which refreshes).
	}
	if best < 0 {
		return -1
	}
	return int(best / time.Millisecond)
}

// ---- attaching by hand --------------------------------------------------

// Attach ties a change request, named by the address a person pasted, to a
// session. The request is read first: an address that cannot be read (no
// account, no access) is refused with the reason rather than saved to fail on
// every poll.
func (s *Service) Attach(ctx context.Context, sessionID, folder, rawURL string) (Item, error) {
	provider, repo, number, ok := s.providers.ParseURL(rawURL)
	if !ok {
		return Item{}, fmt.Errorf("not the address of a pull or merge request on a supported host: %s", rawURL)
	}
	owner, _, _ := cutOwner(repo.Path)
	token, _, err := s.creds.Token(ctx, TokenRequest{Host: repo.Host, Owner: owner, Repo: repo.Path, Interactive: true})
	if err != nil {
		it := failure(Item{Folder: folder, Source: "attached"}, provider.Terms(), RemoteRef{Host: repo.Host, Owner: owner, Path: repo.Path}, err)
		return it, errors.New(it.Message)
	}
	cr, err := s.fetch(ctx, provider, forge.Credential{Token: token}, repo, number, true)
	if err != nil {
		it := failure(Item{Folder: folder, Source: "attached"}, provider.Terms(), RemoteRef{Host: repo.Host, Owner: owner, Path: repo.Path}, err)
		return it, errors.New(it.Message)
	}
	if err := s.store.Put(ctx, Attachment{
		SessionID: sessionID, Folder: folder, Host: repo.Host, Path: repo.Path,
		Number: number, URL: cr.URL, Source: "url", CreatedAt: s.now(),
	}); err != nil {
		return Item{}, err
	}
	s.changed(sessionID)
	return Item{Folder: folder, Branch: cr.Head, Source: "attached", Status: StatusFound, ChangeRequest: &cr, Platform: provider.Terms().Platform}, nil
}

// Detach forgets an attachment. It closes and deletes nothing on the host.
func (s *Service) Detach(ctx context.Context, sessionID, host, path string, number int) error {
	if err := s.store.Delete(ctx, sessionID, host, path, number); err != nil {
		return err
	}
	s.changed(sessionID)
	return nil
}
