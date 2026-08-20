// Package uistate owns generic, non-secret UI preferences that need to
// survive an app restart — a toggle, a panel width, a window bound. One
// key-value table, not a table per feature; see
// docs/superpowers/specs/2026-08-20-app-shell-sidebar-design.md.
package uistate

type Repository interface {
	Get(key string) (value string, found bool, err error)
	Set(key, value string) error
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Get(key string) (string, bool, error) {
	return s.repo.Get(key)
}

func (s *Service) Set(key, value string) error {
	return s.repo.Set(key, value)
}
