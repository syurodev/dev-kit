package git

import (
	"context"
	"sort"
	"strings"
)

// Stage adds paths to the index. Paths are project-relative.
func (s *Service) Stage(ctx context.Context, paths []string) error {
	r, err := s.active()
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return nil
	}
	return s.withPaths(ctx, r, paths, "add", "--")
}

// Unstage takes paths back out of the index.
func (s *Service) Unstage(ctx context.Context, paths []string) error {
	r, err := s.active()
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return nil
	}
	return s.withPaths(ctx, r, paths, "restore", "--staged", "--")
}

func (s *Service) withPaths(ctx context.Context, r runner, paths []string, prefix ...string) error {
	// "--" is already the last element of prefix, so a path that looks like a
	// flag cannot become one.
	_, err := r.run(ctx, nil, append(append([]string(nil), prefix...), paths...)...)
	return err
}

// StageHunk applies one hunk to the index. patch is a unified diff for a
// single file, which is what the UI builds from the hunks the user ticked.
func (s *Service) StageHunk(ctx context.Context, patch string) error {
	r, err := s.active()
	if err != nil {
		return err
	}
	if !strings.HasSuffix(patch, "\n") {
		// git apply rejects a patch whose last line has no newline with a
		// message about corrupt input, which reads like the hunk is wrong.
		patch += "\n"
	}
	_, err = r.run(ctx, []byte(patch), "apply", "--cached", "--whitespace=nowarn", "-")
	return err
}

// CommitInput is one commit.
type CommitInput struct {
	Message string
	// Amend rewrites the last commit. Callers must not offer it for a commit
	// that is already pushed.
	Amend bool
	// TaskKey adds the `Task: <key>` trailer that links this commit to a task.
	// It is the whole link: nothing stores a SHA anywhere.
	TaskKey string
	// ByAgent marks a commit made by an agent, which changes nothing about
	// the commit and everything about what may follow it (see Push).
	ByAgent bool
}

// TaskTrailer is the line that ties a commit to a task. It is a git trailer so
// it survives rebase, amend and squash, and so `git log --grep` finds it on a
// machine that has never run DevKit.
const TaskTrailer = "Task: "

// Commit writes the index. It is gated only when the branch is guarded:
// committing is local and reversible, which is why it is not gated by default.
func (s *Service) Commit(ctx context.Context, in CommitInput) (Commit, error) {
	r, err := s.active()
	if err != nil {
		return Commit{}, err
	}
	status, err := s.Status(ctx)
	if err != nil {
		return Commit{}, err
	}
	if len(status.Conflicted) > 0 {
		return Commit{}, ErrConflict
	}
	message := WithTaskTrailer(in.Message, in.TaskKey)
	if strings.TrimSpace(message) == "" {
		return Commit{}, ErrNotAllowed
	}
	if err := s.gate(ctx, status.Branch, "Commit", summarise(status.Staged)); err != nil {
		return Commit{}, err
	}
	args := []string{"commit", "-m", message}
	if in.Amend {
		args = append(args, "--amend")
	}
	if _, err := r.run(ctx, nil, args...); err != nil {
		return Commit{}, err
	}
	commits, err := s.Log(ctx, 1)
	if err != nil || len(commits) == 0 {
		return Commit{}, err
	}
	return commits[0], nil
}

// WithTaskTrailer appends the trailer unless the message already carries it,
// so seeding it and the user typing it do not produce two.
func WithTaskTrailer(message, key string) string {
	if key == "" {
		return message
	}
	line := TaskTrailer + key
	for _, existing := range strings.Split(message, "\n") {
		if strings.TrimSpace(existing) == line {
			return message
		}
	}
	message = strings.TrimRight(message, "\n")
	if message == "" {
		return line
	}
	return message + "\n\n" + line
}

// TaskKeyOf reads a commit message's task trailer, empty when it has none.
func TaskKeyOf(message string) string {
	for _, line := range strings.Split(message, "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, TaskTrailer); ok {
			return strings.TrimSpace(after)
		}
	}
	return ""
}

func summarise(paths []string) string {
	switch len(paths) {
	case 0:
		return "nothing staged"
	case 1:
		return paths[0]
	default:
		return paths[0] + " and " + itoa(len(paths)-1) + " more"
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// StageTurn stages exactly the paths one agent turn changed.
//
// This is the reason the module exists: the agent already grouped the work,
// and picking those paths back out of `git status` by hand is what people do
// instead.
func (s *Service) StageTurn(ctx context.Context, paths []string) ([]string, error) {
	status, err := s.Status(ctx)
	if err != nil {
		return nil, err
	}
	wanted := make([]string, 0, len(paths))
	for _, p := range paths {
		// A path the turn touched and then reverted is no longer changed;
		// staging it would add an empty entry and confuse the commit screen.
		if _, changed := status.Files[p]; changed {
			wanted = append(wanted, p)
		}
	}
	sort.Strings(wanted)
	if len(wanted) == 0 {
		return nil, nil
	}
	return wanted, s.Stage(ctx, wanted)
}

// Branches lists local branches.
func (s *Service) Branches(ctx context.Context) ([]Branch, error) {
	r, err := s.active()
	if err != nil {
		return nil, err
	}
	out, err := r.run(ctx, nil, "for-each-ref", "--format=%(refname:short)%00%(upstream:short)%00%(HEAD)", "refs/heads")
	if err != nil {
		return nil, err
	}
	var branches []Branch
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\x00")
		if len(fields) < 3 {
			continue
		}
		branches = append(branches, Branch{Name: fields[0], Upstream: fields[1], Current: fields[2] == "*"})
	}
	return branches, nil
}

// CreateBranch makes a branch and checks it out. It returns the name so the
// caller — an agent, through MCP — can write it onto the task it holds, which
// is what makes the push permission survive a restart.
func (s *Service) CreateBranch(ctx context.Context, name, from string) (string, error) {
	r, err := s.active()
	if err != nil {
		return "", err
	}
	if name == "" {
		return "", ErrNotAllowed
	}
	args := []string{"switch", "-c", name}
	if from != "" {
		args = append(args, from)
	}
	if _, err := r.run(ctx, nil, args...); err != nil {
		return "", err
	}
	return name, nil
}

// Checkout switches branches, refusing to do it over uncommitted work.
//
// Not because git would refuse — it carries changes across when it can — but
// because a caller that is not looking (an agent) would move the user's
// uncommitted work onto another branch without anyone noticing.
func (s *Service) Checkout(ctx context.Context, name string) error {
	r, err := s.active()
	if err != nil {
		return err
	}
	status, err := s.Status(ctx)
	if err != nil {
		return err
	}
	if len(status.Staged) > 0 || len(status.Unstaged) > 0 || len(status.Conflicted) > 0 {
		return ErrNotAllowed
	}
	_, err = r.run(ctx, nil, "switch", name)
	return err
}

// Stash is the way out of "switch branches with work in progress".
func (s *Service) Stash(ctx context.Context, message string) error {
	r, err := s.active()
	if err != nil {
		return err
	}
	args := []string{"stash", "push", "--include-untracked"}
	if message != "" {
		args = append(args, "-m", message)
	}
	_, err = r.run(ctx, nil, args...)
	return err
}

// StashPop restores the most recent stash.
func (s *Service) StashPop(ctx context.Context) error {
	r, err := s.active()
	if err != nil {
		return err
	}
	_, err = r.run(ctx, nil, "stash", "pop")
	return err
}
