package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Repository is one work tree found under a folder, with enough of its state to
// choose between it and its neighbours without opening each.
type Repository struct {
	// Path is the work tree's absolute path.
	Path string `json:"path"`
	// Name is Path relative to the folder searched, or the folder's own name
	// when the folder is itself the repository.
	Name     string `json:"name"`
	Branch   string `json:"branch"`
	Detached bool   `json:"detached"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`
	// Changed counts files with any change — staged, unstaged, untracked or
	// conflicted — and Conflicted the ones that are unmerged.
	Changed    int `json:"changed"`
	Conflicted int `json:"conflicted"`
	// Error is why this repository's state could not be read; the rest of the
	// row is empty then. One unreadable repository must not hide its neighbours.
	Error string `json:"error"`
}

const (
	// maxRepoDepth is how far below the folder a repository is looked for: the
	// folder's children, and theirs, and theirs. A folder of services is one
	// level; a folder of clients each holding services is two.
	maxRepoDepth = 3
	// maxRepos bounds the search. Past it the list is what was found first,
	// which is a folder this is no longer the right way to browse.
	maxRepos = 200
	// maxRepoDirs bounds the folders read while searching, so a root that is a
	// whole home directory costs a bounded walk and not an afternoon.
	maxRepoDirs = 5000
	// repoStatusTimeout bounds one repository's status, so a repo on a stalled
	// mount costs its own row and not the whole list.
	repoStatusTimeout = 10 * time.Second
	// repoStatusWorkers is how many repositories are read at once.
	repoStatusWorkers = 6
)

// notRepoDirs are folders never worth walking into. A repository inside one is
// a vendored copy, not something anyone is working in.
var notRepoDirs = map[string]bool{
	"node_modules": true, "vendor": true, "dist": true, "build": true,
	"target": true, "out": true, "coverage": true, "__pycache__": true,
}

// Repositories finds the work trees at or under root and reads each one's
// branch and change count.
//
// A root that is itself a work tree is the only result: what is inside it
// belongs to it, and this is the same answer Open gives. Otherwise the folder
// is searched, stopping at each repository it finds and never entering hidden
// or vendored folders.
func (s *Service) Repositories(ctx context.Context, root string) ([]Repository, error) {
	if !s.availability.OK() {
		return nil, ErrGitUnavailable
	}
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("git: repository search needs an absolute folder, got %q", root)
	}
	root = filepath.Clean(root)
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("git: %q is not a folder", root)
	}

	paths := []string{root}
	if _, err := (runner{git: s.availability.Path, work: root}).run(ctx, nil, "rev-parse", "--is-inside-work-tree"); err != nil {
		paths = findRepos(ctx, root)
	}

	repos := make([]Repository, len(paths))
	var wg sync.WaitGroup
	slots := make(chan struct{}, repoStatusWorkers)
	for i, path := range paths {
		repos[i] = Repository{Path: path, Name: repoName(root, path)}
		wg.Add(1)
		slots <- struct{}{}
		go func(repo *Repository) {
			defer wg.Done()
			defer func() { <-slots }()
			s.summarise(ctx, repo)
		}(&repos[i])
	}
	wg.Wait()
	return repos, ctx.Err()
}

// Summaries reads the branch and change counts of each folder given, in the
// order given, the way Repositories does for the ones it finds. A folder that
// is not a work tree carries its Error rather than failing the rest.
func (s *Service) Summaries(ctx context.Context, paths []string) ([]Repository, error) {
	if !s.availability.OK() {
		return nil, ErrGitUnavailable
	}
	repos := make([]Repository, len(paths))
	var wg sync.WaitGroup
	slots := make(chan struct{}, repoStatusWorkers)
	for i, path := range paths {
		repos[i] = Repository{Path: path, Name: filepath.Base(path)}
		wg.Add(1)
		slots <- struct{}{}
		go func(repo *Repository) {
			defer wg.Done()
			defer func() { <-slots }()
			s.summarise(ctx, repo)
		}(&repos[i])
	}
	wg.Wait()
	return repos, ctx.Err()
}

// summarise fills a repository's branch and counts, or its Error.
func (s *Service) summarise(ctx context.Context, repo *Repository) {
	ctx, cancel := context.WithTimeout(ctx, repoStatusTimeout)
	defer cancel()
	status, err := readStatus(ctx, runner{git: s.availability.Path, work: repo.Path}, "normal")
	if err != nil {
		repo.Error = err.Error()
		return
	}
	repo.Branch, repo.Detached = status.Branch, status.Detached
	repo.Ahead, repo.Behind = status.Ahead, status.Behind
	repo.Changed, repo.Conflicted = len(status.Files), len(status.Conflicted)
}

// repoName is a repository's path as the folder searched sees it.
func repoName(root, path string) string {
	if path == root {
		return filepath.Base(root)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

// findRepos walks below root for folders that hold a .git, without entering
// one it has found or any hidden or vendored folder.
func findRepos(ctx context.Context, root string) []string {
	var found []string
	read := 0
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if depth > maxRepoDepth || len(found) >= maxRepos || read >= maxRepoDirs || ctx.Err() != nil {
			return
		}
		read++
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, entry := range entries {
			name := entry.Name()
			if !entry.IsDir() || strings.HasPrefix(name, ".") || notRepoDirs[name] {
				continue
			}
			child := filepath.Join(dir, name)
			if _, err := os.Stat(filepath.Join(child, ".git")); err == nil {
				found = append(found, child)
				if len(found) >= maxRepos {
					return
				}
				continue
			}
			walk(child, depth+1)
		}
	}
	walk(root, 1)
	sort.Strings(found)
	return found
}
