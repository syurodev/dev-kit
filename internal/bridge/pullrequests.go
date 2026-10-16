package bridge

import (
	"context"
	"path/filepath"

	"devkit/internal/apperrors"
	"devkit/internal/prwatch"
)

// maxTrackedFolders bounds one State call: a workspace of dozens of services
// is a workspace, not a reason to fan out dozens of host calls per poll.
const maxTrackedFolders = 24

var errNoPullRequests = apperrors.New("pullrequests.unavailable", "Pull request tracking is not available here.", true)

// PullRequestState reads, for each folder, the pull (or merge) request its
// branch became, plus the ones attached to the session by hand. The window
// polls it while the session is on screen and asks again after NextPollMs.
//
// The token is chosen and used in Go; what returns is the request's state and,
// when there is none to show, the reason in words.
func (s *AppService) PullRequestState(sessionID string, folders []string) (prwatch.State, error) {
	if s.container.PullRequests == nil {
		return prwatch.State{NextPollMs: -1}, nil
	}
	if len(folders) > maxTrackedFolders {
		folders = folders[:maxTrackedFolders]
	}
	for _, folder := range folders {
		if !filepath.IsAbs(folder) {
			return prwatch.State{}, apperrors.New("validation_failed", "Folders must be absolute paths.", false)
		}
	}
	result, err := s.call(context.Background(), "builtin.git", "git:read", "repo:pr", map[string]any{
		"action": "pr_state", "session": sessionID, "folders": len(folders),
	}, func(ctx context.Context) (any, error) {
		return s.container.PullRequests.State(ctx, sessionID, folders), nil
	})
	if err != nil {
		return prwatch.State{}, err
	}
	return result.(prwatch.State), nil
}

// PullRequestAttach ties a pull request, named by its address, to a session:
// for a fork, or a request opened somewhere DevKit did not see. The request is
// read first; an address the account cannot read is refused with the reason.
func (s *AppService) PullRequestAttach(sessionID, folder, url string) (prwatch.Item, error) {
	if s.container.PullRequests == nil {
		return prwatch.Item{}, errNoPullRequests
	}
	result, err := s.call(context.Background(), "builtin.git", "git:write", "repo:pr", map[string]any{
		"action": "pr_attach", "session": sessionID,
	}, func(ctx context.Context) (any, error) {
		item, err := s.container.PullRequests.Attach(ctx, sessionID, folder, url)
		if err != nil {
			return nil, apperrors.New("pullrequests.attachFailed", err.Error(), true)
		}
		return item, nil
	})
	if err != nil {
		return prwatch.Item{}, err
	}
	return result.(prwatch.Item), nil
}

// PullRequestDetach forgets an attachment. It closes and deletes nothing on
// the host.
func (s *AppService) PullRequestDetach(sessionID, host, path string, number int) error {
	if s.container.PullRequests == nil {
		return errNoPullRequests
	}
	_, err := s.call(context.Background(), "builtin.git", "git:write", "repo:pr", map[string]any{
		"action": "pr_detach", "session": sessionID,
	}, func(ctx context.Context) (any, error) {
		return nil, s.container.PullRequests.Detach(ctx, sessionID, host, path, number)
	})
	return err
}
