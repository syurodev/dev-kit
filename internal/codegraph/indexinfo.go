// internal/codegraph/indexinfo.go
package codegraph

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"path"
	"strings"
)

// IndexInfo is what the sidebar says about the index under the project name:
// when it was built, whether a run is in flight, and why the last one failed.
type IndexInfo struct {
	// IndexedAt is when the stored index was last replaced, Unix milliseconds;
	// 0 when unknown.
	IndexedAt int64 `json:"indexedAt"`
	// Indexing is true while an open or a re-index is running or queued.
	Indexing bool `json:"indexing"`
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

// GitStatus maps each changed file in the active project, by path relative to
// the project root with forward slashes, to a one-letter state: M modified,
// A added (staged or untracked), D deleted, R renamed. A project outside a git
// work tree, or a machine without git, has no changes to report — an empty map,
// not an error: this decorates the file tree and must never break it.
func (s *Service) GitStatus(ctx context.Context) (map[string]string, error) {
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
	return parsePorcelain(out, strings.TrimSpace(string(prefixOut))), nil
}

// parsePorcelain reads `git status --porcelain=v1 -z`. Paths come relative to
// the repository root; prefix (from rev-parse --show-prefix) is where the
// project sits inside it, and is cut off so the keys match ListFiles.
func parsePorcelain(out []byte, prefix string) map[string]string {
	changes := map[string]string{}
	fields := bytes.Split(out, []byte{0})
	for i := 0; i < len(fields); i++ {
		entry := string(fields[i])
		if len(entry) < 4 {
			continue
		}
		xy, file := entry[:2], entry[3:]
		// A rename or copy carries its source as the next field.
		if xy[0] == 'R' || xy[0] == 'C' {
			i++
		}
		if prefix != "" {
			if !strings.HasPrefix(file, prefix) {
				continue
			}
			file = strings.TrimPrefix(file, prefix)
		}
		changes[path.Clean(file)] = gitState(xy)
	}
	return changes
}

func gitState(xy string) string {
	switch {
	case xy == "??" || strings.ContainsRune(xy, 'A'):
		return "A"
	case strings.ContainsRune(xy, 'D'):
		return "D"
	case strings.ContainsRune(xy, 'R'):
		return "R"
	default:
		return "M"
	}
}
