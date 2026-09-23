// internal/codegraph/indexinfo.go
package codegraph

import (
	"context"
	"errors"
	"os/exec"
	"strings"

	"devkit/internal/gitparse"
)

// IndexInfo is what the sidebar says about the index under the project name:
// when it was built, whether a run is in flight, and why the last one failed.
type IndexInfo struct {
	// IndexedAt is when the stored index was last replaced, Unix milliseconds;
	// 0 when unknown.
	IndexedAt int64 `json:"indexedAt"`
	// Indexing is true while an open or a re-index is running or queued.
	Indexing bool `json:"indexing"`
	// Stale is true while a reopened project is served from its stored index
	// and the refresh has not committed yet: queries answer, but from the
	// index as of IndexedAt.
	Stale bool `json:"stale"`
	// Error is the last run's failure, including the analyzer's output tail;
	// "" when it succeeded. The stored index, if any, is still the previous one.
	Error string `json:"error"`
	// Modules are the build modules the project was detected with, so a
	// restored session knows them too.
	Modules []string `json:"modules"`
	// Language is what detection decided the project is written in ("java",
	// "lua", "" when nothing was detected). Restored sessions need it for the
	// same reason they need Modules — the open that decided it happened in a
	// previous run — and the UI needs it to know WHICH analyzer has to be
	// ready before it can show a setup panel instead of the project.
	Language string `json:"language"`
	// Branch is the checked-out git branch, "" outside a work tree.
	Branch string `json:"branch"`
}

// IndexInfo reports the active project's index state. It needs the store open,
// not the index ready: a failed run is exactly when this is asked.
func (s *Service) IndexInfo(ctx context.Context) (IndexInfo, error) {
	s.mu.RLock()
	info := IndexInfo{
		Indexing: s.index.busy(),
		Stale:    s.index.stale,
		Modules:  append([]string(nil), s.detected.Modules...),
		Language: s.detected.Language,
	}
	if s.index.err != nil {
		info.Error = s.index.err.Error()
	}
	s.mu.RUnlock()

	store, release, err := s.projectStore()
	if err != nil {
		return IndexInfo{}, err
	}
	defer release()
	info.IndexedAt, err = store.IndexedAt(ctx)
	if root, rootErr := s.projectFiles(); rootErr == nil {
		if out, gitErr := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "--abbrev-ref", "HEAD").Output(); gitErr == nil {
			info.Branch = strings.TrimSpace(string(out))
		}
	}
	return info, err
}

// WorkTreeStatus is the Git module's status, as this package needs it. It is
// declared locally so codegraph does not import internal/git — the dependency
// runs the other way, and the composition root decides what satisfies this.
type WorkTreeStatus interface {
	// Changes maps a project-relative path to a one-letter state.
	Changes(ctx context.Context) (map[string]string, error)
}

// SetWorkTreeStatus installs the Git module as the source of what changed.
//
// It exists so there is ONE answer to that question. Before it, the file tree
// markers here and the Git module's own status ran two separate `git status`
// calls against the same work tree, which is two answers that can disagree and
// two processes where one would do. Nil keeps the built-in reader, so an
// ephemeral container and a build without the Git module still draw markers.
func (s *Service) SetWorkTreeStatus(status WorkTreeStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workTree = status
}

// GitStatus maps each changed file in the active project, by path relative to
// the project root with forward slashes, to a one-letter state: M modified,
// A added (staged or untracked), D deleted, R renamed. A project outside a git
// work tree, or a machine without git, has no changes to report — an empty map,
// not an error: this decorates the file tree and must never break it.
func (s *Service) GitStatus(ctx context.Context) (map[string]string, error) {
	s.mu.RLock()
	workTree := s.workTree
	s.mu.RUnlock()
	if workTree != nil {
		changes, err := workTree.Changes(ctx)
		if err != nil {
			// The Git module having nothing to say — no repository, no git —
			// must not stop the file tree drawing.
			return map[string]string{}, nil
		}
		return changes, nil
	}
	root, err := s.projectFiles()
	if err != nil {
		return nil, err
	}
	prefixOut, err := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "--show-prefix").Output()
	if err != nil {
		return map[string]string{}, nil
	}
	out, err := exec.CommandContext(ctx, "git", "-C", root, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--", ".").Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	return gitparse.Porcelain(out, strings.TrimSpace(string(prefixOut))), nil
}
