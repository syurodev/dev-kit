package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrWorktreeDirty is returned when a worktree has uncommitted changes.
var ErrWorktreeDirty = errors.New("git.worktreeDirty")

// ErrWorktreeOutside is returned when a worktree path would escape the repository.
var ErrWorktreeOutside = errors.New("git.worktreeOutside")

// ErrWorktreeExists is returned when a worktree already exists at the target path.
var ErrWorktreeExists = errors.New("git.worktreeExists")

// ErrWorktreeNotFound is returned when a worktree cannot be found.
var ErrWorktreeNotFound = errors.New("git.worktreeNotFound")

// ErrBranchCheckedOut is returned when a branch is already checked out in another worktree.
var ErrBranchCheckedOut = errors.New("git.branchCheckedOut")

// Worktree represents a git worktree.
type Worktree struct {
	Path   string `json:"path"`
	Branch string `json:"branch"`
	Head   string `json:"head"`
	// Locked means the worktree is locked (git worktree lock).
	Locked bool `json:"locked"`
	// Prunable means the worktree is eligible for pruning (git worktree prune).
	Prunable bool `json:"prunable"`
	// Dirty means the worktree has uncommitted changes.
	Dirty bool `json:"dirty"`
}

// WorktreeAddInput specifies a new worktree.
type WorktreeAddInput struct {
	// Branch is the branch name to create or attach.
	Branch string
	// Base is the base commit/branch to create the new branch from. Empty = HEAD.
	Base string
}

// worktreeRoot returns the absolute path to the repository toplevel.
func (s *Service) worktreeRoot(ctx context.Context) (string, error) {
	r, err := s.active()
	if err != nil {
		return "", err
	}
	out, err := r.run(ctx, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	root := strings.TrimSpace(string(out))
	// Resolve symlinks for reliable containment checks.
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	return root, nil
}

// worktreeDir derives the directory name for a branch under .superpowers/.worktrees/.
func worktreeDir(branch string) string {
	// Replace / and other unsafe characters with -.
	name := strings.ReplaceAll(branch, "/", "-")
	name = strings.ReplaceAll(name, "\\", "-")
	name = strings.ReplaceAll(name, ":", "-")
	name = strings.ReplaceAll(name, "*", "-")
	name = strings.ReplaceAll(name, "?", "-")
	name = strings.ReplaceAll(name, "\"", "-")
	name = strings.ReplaceAll(name, "<", "-")
	name = strings.ReplaceAll(name, ">", "-")
	name = strings.ReplaceAll(name, "|", "-")
	name = strings.Trim(name, " .")
	if name == "" {
		name = "worktree"
	}
	return name
}

// validateWorktreeName refuses obviously bad names.
func validateWorktreeName(name string) error {
	if name == "" || strings.TrimSpace(name) != name || strings.ContainsAny(name, "\\\x00") || strings.Contains(name, "..") || filepath.IsAbs(name) || strings.HasPrefix(name, "/") {
		return fmt.Errorf("%w: invalid branch name", ErrWorktreeOutside)
	}
	// Let Git enforce the full refname grammar; string path checks alone miss
	// names such as `@{1}` and trailing dots.
	return nil
}

// WorktreeAdd creates a new worktree or attaches an existing branch.
// The worktree is created under <repo>/.superpowers/.worktrees/<branch-slug>.
func (s *Service) WorktreeAdd(ctx context.Context, in WorktreeAddInput) (Worktree, error) {
	if in.Branch == "" {
		return Worktree{}, fmt.Errorf("%w: name the branch for the worktree", ErrNotAllowed)
	}
	if err := validateWorktreeName(in.Branch); err != nil {
		return Worktree{}, err
	}

	root, err := s.worktreeRoot(ctx)
	if err != nil {
		return Worktree{}, err
	}

	r, err := s.active()
	if err != nil {
		return Worktree{}, err
	}
	if _, err := r.run(ctx, nil, "check-ref-format", "--branch", in.Branch); err != nil {
		return Worktree{}, fmt.Errorf("%w: invalid branch name", ErrWorktreeOutside)
	}
	// Check if the branch is already checked out elsewhere.
	existing, err := s.Worktrees(ctx)
	if err != nil {
		return Worktree{}, err
	}
	for _, wt := range existing {
		if wt.Branch == in.Branch {
			return Worktree{}, fmt.Errorf("%w: branch %s is already checked out at %s", ErrBranchCheckedOut, in.Branch, wt.Path)
		}
	}
	_, branchErr := r.run(ctx, nil, "show-ref", "--verify", "--quiet", "refs/heads/"+in.Branch)
	if branchErr == nil && in.Base != "" {
		return Worktree{}, fmt.Errorf("%w: base is only used when creating a branch", ErrNotAllowed)
	}
	if err := s.gate(ctx, in.Branch, "Create worktree", "for a new or existing branch"); err != nil {
		return Worktree{}, err
	}

	// Worktrees live under <repo>/.superpowers/.worktrees/<branch-slug>
	superpowersDir := filepath.Join(root, ".superpowers")
	if err := mkdirManagedDir(superpowersDir); err != nil {
		return Worktree{}, err
	}
	worktreesDir := filepath.Join(superpowersDir, ".worktrees")
	if err := mkdirManagedDir(worktreesDir); err != nil {
		return Worktree{}, err
	}
	resolvedWorktreesDir, err := filepath.EvalSymlinks(worktreesDir)
	if err != nil {
		return Worktree{}, err
	}
	if !withinPath(root, resolvedWorktreesDir) {
		return Worktree{}, fmt.Errorf("%w: worktree directory is outside the repository", ErrWorktreeOutside)
	}
	targetPath := filepath.Join(resolvedWorktreesDir, worktreeDir(in.Branch))
	// A different branch can slug to the same directory, so check the path too.
	if _, err := os.Lstat(targetPath); err == nil {
		return Worktree{}, fmt.Errorf("%w: %s", ErrWorktreeExists, targetPath)
	}

	args := []string{"worktree", "add", targetPath}
	if branchErr == nil {
		args = append(args, in.Branch)
	} else if in.Base != "" {
		args = append(args, "-b", in.Branch, in.Base)
	} else {
		args = append(args, "-b", in.Branch)
	}
	if _, err := r.run(ctx, nil, args...); err != nil {
		return Worktree{}, err
	}

	// Read back the new worktree's status.
	return s.worktreeInfo(ctx, targetPath, in.Branch)
}

func mkdirManagedDir(path string) error {
	if err := os.Mkdir(path, 0755); err != nil && !os.IsExist(err) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%w: managed worktree directory is not a real directory", ErrWorktreeOutside)
	}
	return nil
}

func withinPath(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// Worktrees lists all worktrees for the repository.
func (s *Service) Worktrees(ctx context.Context) ([]Worktree, error) {
	r, err := s.active()
	if err != nil {
		return nil, err
	}

	out, err := r.run(ctx, nil, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}

	var worktrees []Worktree
	lines := strings.Split(string(out), "\n")
	var current Worktree
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if current.Path != "" {
				// Check dirty status for this worktree.
				current.Dirty = s.isWorktreeDirty(ctx, current.Path)
				worktrees = append(worktrees, current)
				current = Worktree{}
			}
			continue
		}
		if strings.HasPrefix(line, "worktree ") {
			current.Path = strings.TrimPrefix(line, "worktree ")
		} else if strings.HasPrefix(line, "HEAD ") {
			current.Head = strings.TrimPrefix(line, "HEAD ")
		} else if strings.HasPrefix(line, "branch ") {
			current.Branch = strings.TrimPrefix(line, "branch ")
			// branch refs/heads/name -> name
			current.Branch = strings.TrimPrefix(current.Branch, "refs/heads/")
		} else if strings.HasPrefix(line, "locked") {
			current.Locked = true
		} else if strings.HasPrefix(line, "prunable") {
			current.Prunable = true
		}
	}
	// Last entry.
	if current.Path != "" {
		current.Dirty = s.isWorktreeDirty(ctx, current.Path)
		worktrees = append(worktrees, current)
	}
	return worktrees, nil
}

// isWorktreeDirty checks if a worktree has uncommitted changes.
func (s *Service) isWorktreeDirty(ctx context.Context, path string) bool {
	// Run status in the worktree directory with "normal" untracked mode.
	r := runner{git: s.runner.git, work: path}
	status, err := readStatus(ctx, r, "normal")
	if err != nil {
		return false
	}
	return len(status.Files) > 0
}

// worktreeInfo returns Worktree info for a given path and branch.
func (s *Service) worktreeInfo(ctx context.Context, path, branch string) (Worktree, error) {
	r := runner{git: s.runner.git, work: path}
	headOut, err := r.run(ctx, nil, "rev-parse", "HEAD")
	if err != nil {
		return Worktree{}, err
	}
	head := strings.TrimSpace(string(headOut))
	dirty := s.isWorktreeDirty(ctx, path)
	return Worktree{
		Path:   path,
		Branch: branch,
		Head:   head,
		Dirty:  dirty,
	}, nil
}

// WorktreeRemove removes a worktree by branch name or path.
// It refuses to remove a dirty worktree and never deletes the branch.
func (s *Service) WorktreeRemove(ctx context.Context, name string) error {
	if name == "" {
		return fmt.Errorf("%w: name the worktree to remove", ErrWorktreeNotFound)
	}

	worktrees, err := s.Worktrees(ctx)
	if err != nil {
		return err
	}

	var target *Worktree
	for _, wt := range worktrees {
		if wt.Branch == name || wt.Path == name {
			target = &wt
			break
		}
	}
	if target == nil {
		return fmt.Errorf("%w: %s", ErrWorktreeNotFound, name)
	}

	// Refuse to remove the main worktree.
	root, err := s.worktreeRoot(ctx)
	if err != nil {
		return err
	}
	targetRoot, err := filepath.EvalSymlinks(target.Path)
	if err != nil {
		return err
	}
	if targetRoot == root {
		return fmt.Errorf("%w: cannot remove the main worktree", ErrNotAllowed)
	}
	worktreesDir := filepath.Join(root, ".superpowers", ".worktrees")
	if !withinPath(worktreesDir, targetRoot) {
		return fmt.Errorf("%w: only worktrees under .superpowers/.worktrees can be removed", ErrWorktreeOutside)
	}

	// Check for dirty.
	if target.Dirty {
		return fmt.Errorf("%w: worktree at %s has uncommitted changes — commit or stash them first", ErrWorktreeDirty, target.Path)
	}
	if err := s.gate(ctx, target.Branch, "Remove worktree", "clean worktree at "+target.Path); err != nil {
		return err
	}

	r, err := s.active()
	if err != nil {
		return err
	}

	// Remove the worktree.
	_, err = r.run(ctx, nil, "worktree", "remove", target.Path)
	return err
}

// WorktreePrune prunes stale worktree administrative files.
func (s *Service) WorktreePrune(ctx context.Context) ([]string, error) {
	r, err := s.active()
	if err != nil {
		return nil, err
	}
	out, err := r.run(ctx, nil, "worktree", "prune", "-v")
	if err != nil {
		return nil, err
	}
	var pruned []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Pruning ") {
			pruned = append(pruned, strings.TrimPrefix(line, "Pruning "))
		}
	}
	return pruned, nil
}
