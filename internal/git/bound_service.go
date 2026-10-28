package git

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Close lets go of the descriptors an agent-origin handle holds (bound.go). A handle
// that holds none ignores it; a handle that is dropped without it is closed when it
// is collected.
func (s *Service) Close() {
	s.mu.RLock()
	b := s.runner.bound
	s.mu.RUnlock()
	b.Close()
}

// Bound reports whether commands of this handle run in a directory it holds open, with
// no path lookup, which only an agent-origin handle does, and only on a platform that
// has descriptors.
func (s *Service) Bound() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.runner.bound != nil
}

// BoundPath is the symlink-free path the handle's directory was opened at, "" when
// the handle is not bound. A caller that checked a path compares it with this to know
// the directory it holds is the one it judged.
func (s *Service) BoundPath() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.runner.bound.boundPath()
}

// Identity is the device and inode of the top of the checkout and of the git directory
// that were verified when the handle was opened, with the resolved top; ok is false for
// a handle that is not bound.
func (s *Service) Identity() (id Identity, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.runner.bound == nil {
		return Identity{}, false
	}
	return s.runner.bound.identity(), true
}

// DotGit reads what is at `.git` in the top of the checkout: through the held
// directory for a bound handle, by path otherwise.
func (s *Service) DotGit() (DotGit, error) {
	s.mu.RLock()
	b, top, gitDir := s.runner.bound, s.toplevel, s.gitDir
	s.mu.RUnlock()
	if b != nil {
		return b.dotGit()
	}
	dotGit := filepath.Join(top, ".git")
	info, err := os.Lstat(dotGit)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return DotGit{Kind: "none"}, nil
	case err != nil:
		return DotGit{}, err
	case info.Mode()&os.ModeSymlink != 0:
		text, _ := os.Readlink(dotGit)
		real, err := filepath.EvalSymlinks(dotGit)
		return DotGit{Kind: "link", Gitdir: text, IsGitDir: err == nil && real == gitDir}, nil
	case info.IsDir():
		real, err := filepath.EvalSymlinks(dotGit)
		return DotGit{Kind: "dir", IsGitDir: err == nil && real == gitDir}, nil
	}
	raw, err := os.ReadFile(dotGit)
	if err != nil {
		return DotGit{}, err
	}
	line, _, _ := strings.Cut(string(raw), "\n")
	target, ok := strings.CutPrefix(strings.TrimSpace(line), "gitdir:")
	if !ok {
		return DotGit{Kind: "file"}, nil
	}
	return DotGit{Kind: "file", Gitdir: strings.TrimSpace(target)}, nil
}

// GitDirFile reads one file directly under the git directory git found (the commondir
// and gitdir files of a linked worktree): through the held directory for a bound
// handle, by path otherwise.
func (s *Service) GitDirFile(name string) ([]byte, error) {
	s.mu.RLock()
	b, gitDir := s.runner.bound, s.gitDir
	s.mu.RUnlock()
	if b != nil {
		return b.gitDirFile(name)
	}
	return os.ReadFile(filepath.Join(gitDir, name))
}
