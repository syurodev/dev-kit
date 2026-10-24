package git

import (
	"context"
	"net/url"
)

// Confirmation carries the execution target separately from display wording.
// Hosts can implement DetailedConfirmer without widening the base interface.
type Confirmation struct{ Title, Detail, Repo, Branch, Remote, Destination, Env, Action string }
type DetailedConfirmer interface {
	ConfirmGit(context.Context, Confirmation) (bool, error)
}

func (s *Service) confirmGit(ctx context.Context, req Confirmation) (bool, error) {
	req.Repo = s.policyKey()
	if c, ok := s.confirm.(DetailedConfirmer); ok {
		return c.ConfirmGit(ctx, req)
	}
	return s.confirm.Confirm(ctx, req.Title, req.Detail)
}
func safeDestination(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}
