package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Rewriting history, done so that nothing a remote has seen is rewritten.
//
// There is no force push in DevKit — no flag, no parameter, no code path — so
// a commit that has reached any remote can never be replaced there. Rewriting
// one locally would leave a branch that can no longer be pushed at all, which
// is the trap these refuse up front: amend, squash and rebase touch only
// commits no remote-tracking ref reaches (`git rev-list HEAD --not --remotes`).
// Revert and cherry-pick add commits rather than replace them, so they work
// on pushed history too.
//
// Each one that writes a commit goes through the branch's guard, like Commit,
// and is written as the account's identity when the repository has one.

var (
	// ErrPushedHistory is a rewrite that would touch a commit a remote has.
	ErrPushedHistory = errors.New("git.pushedHistory")
	// ErrDirtyWorkTree is an operation that needs a clean work tree and index.
	ErrDirtyWorkTree = errors.New("git.dirtyWorkTree")
	// ErrInProgress is a rebase or cherry-pick already under way: finish or
	// abort it first. Also what one that stopped on a conflict answers with.
	ErrInProgress = errors.New("git.operationInProgress")
	// ErrNothingInProgress is a continue or abort with nothing to continue.
	ErrNothingInProgress = errors.New("git.nothingInProgress")
)

// Operation is a multi-step history operation git can stop in the middle of.
type Operation string

const (
	OperationNone       Operation = ""
	OperationRebase     Operation = "rebase"
	OperationCherryPick Operation = "cherry-pick"
	OperationRevert     Operation = "revert"
	OperationMerge      Operation = "merge"
)

// HistoryResult is where a history operation left the branch.
type HistoryResult struct {
	// Head is the branch's newest commit afterwards.
	Head Commit `json:"head"`
	// Stopped is the operation that is waiting on conflicts, empty when it
	// finished. Resolve each path (git.resolve), then continue — or abort.
	Stopped Operation `json:"stopped,omitempty"`
	// Conflicted are the paths it stopped on.
	Conflicted []string `json:"conflicted,omitempty"`
}

// InProgress reports the operation git is in the middle of, if any.
func (s *Service) InProgress(ctx context.Context) (Operation, error) {
	r, err := s.active()
	if err != nil {
		return OperationNone, err
	}
	for _, probe := range []struct {
		path string
		op   Operation
	}{
		{"rebase-merge", OperationRebase},
		{"rebase-apply", OperationRebase},
		{"CHERRY_PICK_HEAD", OperationCherryPick},
		{"REVERT_HEAD", OperationRevert},
		{"MERGE_HEAD", OperationMerge},
	} {
		out, err := r.run(ctx, nil, "rev-parse", "--git-path", probe.path)
		if err != nil {
			return OperationNone, err
		}
		path := strings.TrimSpace(string(out))
		if !filepath.IsAbs(path) {
			path = filepath.Join(r.work, path)
		}
		if _, err := os.Stat(path); err == nil {
			return probe.op, nil
		}
	}
	return OperationNone, nil
}

// unpushed lists the commits on HEAD that no remote-tracking ref reaches,
// newest first. A repository with no remote has nothing pushed.
func (s *Service) unpushed(ctx context.Context, r runner) ([]string, error) {
	out, err := r.run(ctx, nil, "rev-list", "HEAD", "--not", "--remotes")
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(out)), nil
}

// requireClean refuses with staged, unstaged or conflicted changes. Untracked
// files are left alone: no history operation touches them.
func (s *Service) requireClean(ctx context.Context) (Status, error) {
	status, err := s.Status(ctx)
	if err != nil {
		return Status{}, err
	}
	if len(status.Conflicted) > 0 {
		return status, ErrConflict
	}
	if len(status.Staged) > 0 || len(status.Unstaged) > 0 {
		return status, fmt.Errorf("%w: commit or stash the changes first", ErrDirtyWorkTree)
	}
	return status, nil
}

func (s *Service) requireIdle(ctx context.Context) error {
	op, err := s.InProgress(ctx)
	if err != nil {
		return err
	}
	if op != OperationNone {
		return fmt.Errorf("%w: a %s is under way — continue or abort it first", ErrInProgress, op)
	}
	return nil
}

// runAsIdentity runs a commit-writing git command as the repository's account
// identity when it has one (see Commit), else as git's own configuration.
func (s *Service) runAsIdentity(ctx context.Context, r runner, keepAuthor bool, args ...string) error {
	if identity, err := s.CommitIdentity(ctx); err == nil && identity.Source == IdentityFromAccount {
		return r.runWithEnv(ctx, identityEnv(identity, keepAuthor), args...)
	}
	_, err := r.run(ctx, nil, args...)
	return err
}

func (s *Service) head(ctx context.Context) (Commit, error) {
	commits, err := s.Log(ctx, 1)
	if err != nil {
		return Commit{}, err
	}
	if len(commits) == 0 {
		return Commit{}, ErrNoRepository
	}
	return commits[0], nil
}

// AmendInput rewrites the last commit.
type AmendInput struct {
	// Message replaces the commit's message; empty keeps it.
	Message string
	// TaskKey adds the task trailer to the (new or kept) message.
	TaskKey string
}

// Amend folds what is staged into the last commit, and rewrites its message
// when one is given. Refused when that commit is on a remote.
func (s *Service) Amend(ctx context.Context, in AmendInput) (Commit, error) {
	r, err := s.active()
	if err != nil {
		return Commit{}, err
	}
	if err := s.requireIdle(ctx); err != nil {
		return Commit{}, err
	}
	status, err := s.Status(ctx)
	if err != nil {
		return Commit{}, err
	}
	if len(status.Conflicted) > 0 {
		return Commit{}, ErrConflict
	}
	local, err := s.unpushed(ctx, r)
	if err != nil {
		return Commit{}, err
	}
	if len(local) == 0 {
		return Commit{}, fmt.Errorf("%w: the last commit is already on a remote, and DevKit never force-pushes — make a new commit instead", ErrPushedHistory)
	}
	last, err := s.head(ctx)
	if err != nil {
		return Commit{}, err
	}
	message := in.Message
	if strings.TrimSpace(message) == "" {
		message = fullMessage(last)
	}
	message = WithTaskTrailer(message, in.TaskKey)
	if err := s.gate(ctx, status.Branch, "Amend", last.Short+" "+last.Subject); err != nil {
		return Commit{}, err
	}
	if err := s.runAsIdentity(ctx, r, true, "commit", "--amend", "--allow-empty", "-m", message); err != nil {
		return Commit{}, err
	}
	return s.head(ctx)
}

// SquashInput combines the newest commits into one.
type SquashInput struct {
	// Count is how many of the newest commits become one: at least 2.
	Count int
	// Message is the combined commit's; empty joins theirs, oldest first.
	Message string
}

// Squash replaces the newest Count commits with one holding all their changes.
// The commits must all be unpushed, none of them a merge. Every task trailer
// they carried is kept on the result, so `git.commitsForTask` still finds it.
func (s *Service) Squash(ctx context.Context, in SquashInput) (Commit, error) {
	r, err := s.active()
	if err != nil {
		return Commit{}, err
	}
	if in.Count < 2 {
		return Commit{}, fmt.Errorf("%w: squash needs at least two commits", ErrNotAllowed)
	}
	if err := s.requireIdle(ctx); err != nil {
		return Commit{}, err
	}
	status, err := s.requireClean(ctx)
	if err != nil {
		return Commit{}, err
	}
	local, err := s.unpushed(ctx, r)
	if err != nil {
		return Commit{}, err
	}
	if in.Count > len(local) {
		return Commit{}, fmt.Errorf("%w: only the newest %d commit(s) are not on a remote, and DevKit never force-pushes", ErrPushedHistory, len(local))
	}
	commits, err := s.Log(ctx, in.Count)
	if err != nil {
		return Commit{}, err
	}
	if len(commits) < in.Count {
		return Commit{}, fmt.Errorf("%w: the branch has only %d commit(s)", ErrNotAllowed, len(commits))
	}
	base := "HEAD~" + strconv.Itoa(in.Count)
	if out, err := r.run(ctx, nil, "rev-list", "--merges", base+"..HEAD"); err != nil {
		return Commit{}, err
	} else if strings.TrimSpace(string(out)) != "" {
		return Commit{}, fmt.Errorf("%w: a merge commit cannot be squashed", ErrNotAllowed)
	}
	if _, err := r.run(ctx, nil, "rev-parse", "--verify", "--quiet", base); err != nil {
		return Commit{}, fmt.Errorf("%w: the first commit of a repository cannot be squashed into nothing", ErrNotAllowed)
	}

	message := in.Message
	if strings.TrimSpace(message) == "" {
		parts := make([]string, 0, len(commits))
		for i := len(commits) - 1; i >= 0; i-- {
			parts = append(parts, strings.TrimSpace(withoutTaskTrailer(fullMessage(commits[i]))))
		}
		message = strings.Join(parts, "\n\n")
	}
	for i := len(commits) - 1; i >= 0; i-- {
		if key := TaskKeyOf(fullMessage(commits[i])); key != "" {
			message = WithTaskTrailer(message, key)
		}
	}
	oldest := commits[len(commits)-1]
	if err := s.gate(ctx, status.Branch, "Squash "+strconv.Itoa(in.Count)+" commits", oldest.Short+".."+commits[0].Short); err != nil {
		return Commit{}, err
	}

	before := commits[0].SHA
	if _, err := r.run(ctx, nil, "reset", "--soft", base); err != nil {
		return Commit{}, err
	}
	// The author of the oldest commit stays the author: squashing is tidying
	// someone's work, not taking it.
	args := []string{"commit", "-m", message}
	if author, err := r.run(ctx, nil, "log", "-1", "--format=%an <%ae>%n%aI", oldest.SHA); err == nil {
		if lines := strings.SplitN(strings.TrimSpace(string(author)), "\n", 2); len(lines) == 2 {
			args = append(args, "--author="+lines[0], "--date="+lines[1])
		}
	}
	if err := s.runAsIdentity(ctx, r, true, args...); err != nil {
		// Put the branch back exactly where it was: a soft reset moved only the
		// branch, and the index still holds every change.
		_, _ = r.run(ctx, nil, "reset", "--soft", before)
		return Commit{}, err
	}
	return s.head(ctx)
}

// RebaseAction is what a rebase call does.
type RebaseAction string

const (
	RebaseStart    RebaseAction = "start"
	RebaseContinue RebaseAction = "continue"
	RebaseAbort    RebaseAction = "abort"
)

// RebaseInput replays the branch's own commits onto another commit.
type RebaseInput struct {
	Action RebaseAction
	// Onto is a branch, tag or commit — required to start.
	Onto string
}

// Rebase moves the branch's commits onto `Onto`, non-interactively. Every
// commit it replays must be unpushed. A conflict stops it: the result names
// the paths, each is resolved (Resolve), and Continue carries on — or Abort
// puts the branch back exactly where it was.
func (s *Service) Rebase(ctx context.Context, in RebaseInput) (HistoryResult, error) {
	r, err := s.active()
	if err != nil {
		return HistoryResult{}, err
	}
	op, err := s.InProgress(ctx)
	if err != nil {
		return HistoryResult{}, err
	}
	switch in.Action {
	case RebaseAbort:
		if op != OperationRebase {
			return HistoryResult{}, fmt.Errorf("%w: no rebase is under way", ErrNothingInProgress)
		}
		if _, err := r.run(ctx, nil, "rebase", "--abort"); err != nil {
			return HistoryResult{}, err
		}
		return s.historyResult(ctx, OperationNone)
	case RebaseContinue:
		if op != OperationRebase {
			return HistoryResult{}, fmt.Errorf("%w: no rebase is under way", ErrNothingInProgress)
		}
		status, err := s.Status(ctx)
		if err != nil {
			return HistoryResult{}, err
		}
		if len(status.Conflicted) > 0 {
			return s.historyResult(ctx, OperationRebase)
		}
		return s.sequence(ctx, r, OperationRebase, "rebase", "--continue")
	case RebaseStart, "":
	default:
		return HistoryResult{}, fmt.Errorf("%w: action is start, continue or abort", ErrNotAllowed)
	}

	if op != OperationNone {
		return HistoryResult{}, fmt.Errorf("%w: a %s is under way — continue or abort it first", ErrInProgress, op)
	}
	onto := strings.TrimSpace(in.Onto)
	if onto == "" || strings.HasPrefix(onto, "-") {
		return HistoryResult{}, fmt.Errorf("%w: name the branch or commit to rebase onto", ErrNotAllowed)
	}
	if _, err := r.run(ctx, nil, "rev-parse", "--verify", "--quiet", onto+"^{commit}"); err != nil {
		return HistoryResult{}, fmt.Errorf("%w: %s is not a commit here", ErrNotAllowed, onto)
	}
	status, err := s.requireClean(ctx)
	if err != nil {
		return HistoryResult{}, err
	}
	if status.Detached {
		return HistoryResult{}, fmt.Errorf("%w: check out a branch first", ErrNotAllowed)
	}
	// The commits a rebase would replay are the branch's own: HEAD, not onto.
	out, err := r.run(ctx, nil, "rev-list", onto+"..HEAD")
	if err != nil {
		return HistoryResult{}, err
	}
	replayed := strings.Fields(string(out))
	local, err := s.unpushed(ctx, r)
	if err != nil {
		return HistoryResult{}, err
	}
	isLocal := make(map[string]bool, len(local))
	for _, sha := range local {
		isLocal[sha] = true
	}
	for _, sha := range replayed {
		if !isLocal[sha] {
			return HistoryResult{}, fmt.Errorf("%w: %s is already on a remote, and replaying it would need a force push DevKit never makes — merge %s instead, or rebase only unpushed work", ErrPushedHistory, sha[:min(len(sha), 7)], onto)
		}
	}
	if err := s.gate(ctx, status.Branch, "Rebase onto "+onto, strconv.Itoa(len(replayed))+" commit(s)"); err != nil {
		return HistoryResult{}, err
	}
	return s.sequence(ctx, r, OperationRebase, "rebase", onto)
}

// sequence runs a step of an operation that may stop on a conflict, and says
// whether it did. Nothing ever opens an editor: there is nobody to type in it.
func (s *Service) sequence(ctx context.Context, r runner, op Operation, args ...string) (HistoryResult, error) {
	env := []string{"GIT_EDITOR=true", "GIT_SEQUENCE_EDITOR=true"}
	if identity, err := s.CommitIdentity(ctx); err == nil && identity.Source == IdentityFromAccount {
		// Replayed commits keep their authors; the one replaying them commits.
		env = append(env, identityEnv(identity, true)...)
	}
	runErr := r.runWithEnv(ctx, env, args...)
	now, err := s.InProgress(ctx)
	if err != nil {
		return HistoryResult{}, err
	}
	if now == op {
		// Stopped. A conflict is the expected reason; anything else is the error.
		result, err := s.historyResult(ctx, op)
		if err != nil {
			return HistoryResult{}, err
		}
		if len(result.Conflicted) > 0 {
			return result, nil
		}
		if runErr != nil {
			return HistoryResult{}, runErr
		}
		return result, nil
	}
	if runErr != nil {
		return HistoryResult{}, runErr
	}
	return s.historyResult(ctx, OperationNone)
}

func (s *Service) historyResult(ctx context.Context, stopped Operation) (HistoryResult, error) {
	head, err := s.head(ctx)
	if err != nil {
		return HistoryResult{}, err
	}
	result := HistoryResult{Head: head, Stopped: stopped}
	if stopped != OperationNone {
		conflicted, err := s.Conflicts(ctx)
		if err != nil {
			return HistoryResult{}, err
		}
		result.Conflicted = conflicted
	}
	return result, nil
}

// PickAction is what a cherry-pick or revert call does.
type PickAction string

const (
	PickStart    PickAction = "start"
	PickContinue PickAction = "continue"
	PickAbort    PickAction = "abort"
)

// PickInput applies commits (cherry-pick) or undoes them (revert).
type PickInput struct {
	Action PickAction
	// Commits to apply, oldest first. Required to start.
	Commits []string
	// TaskKey adds the task trailer to each commit written. Not supported on
	// continue, which reuses the messages git already prepared.
	TaskKey string
}

// CherryPick copies commits onto the current branch as new commits. It adds
// history, rewrites none, so pushed commits may be picked. `-x` records where
// each came from.
func (s *Service) CherryPick(ctx context.Context, in PickInput) (HistoryResult, error) {
	return s.pick(ctx, in, OperationCherryPick, "cherry-pick", []string{"-x"}, "Cherry-pick")
}

// Revert writes commits that undo the given ones. It adds history, so it is
// the way to take back a commit that is already pushed.
func (s *Service) Revert(ctx context.Context, in PickInput) (HistoryResult, error) {
	return s.pick(ctx, in, OperationRevert, "revert", nil, "Revert")
}

func (s *Service) pick(ctx context.Context, in PickInput, op Operation, verb string, flags []string, what string) (HistoryResult, error) {
	r, err := s.active()
	if err != nil {
		return HistoryResult{}, err
	}
	current, err := s.InProgress(ctx)
	if err != nil {
		return HistoryResult{}, err
	}
	switch in.Action {
	case PickAbort:
		if current != op {
			return HistoryResult{}, fmt.Errorf("%w: no %s is under way", ErrNothingInProgress, verb)
		}
		if _, err := r.run(ctx, nil, verb, "--abort"); err != nil {
			return HistoryResult{}, err
		}
		return s.historyResult(ctx, OperationNone)
	case PickContinue:
		if current != op {
			return HistoryResult{}, fmt.Errorf("%w: no %s is under way", ErrNothingInProgress, verb)
		}
		status, err := s.Status(ctx)
		if err != nil {
			return HistoryResult{}, err
		}
		if len(status.Conflicted) > 0 {
			return s.historyResult(ctx, op)
		}
		return s.sequence(ctx, r, op, verb, "--continue")
	case PickStart, "":
	default:
		return HistoryResult{}, fmt.Errorf("%w: action is start, continue or abort", ErrNotAllowed)
	}
	if current != OperationNone {
		return HistoryResult{}, fmt.Errorf("%w: a %s is under way — continue or abort it first", ErrInProgress, current)
	}
	if len(in.Commits) == 0 {
		return HistoryResult{}, fmt.Errorf("%w: name at least one commit", ErrNotAllowed)
	}
	resolved := make([]string, 0, len(in.Commits))
	for _, rev := range in.Commits {
		rev = strings.TrimSpace(rev)
		if rev == "" || strings.HasPrefix(rev, "-") {
			return HistoryResult{}, fmt.Errorf("%w: %q is not a commit", ErrNotAllowed, rev)
		}
		out, err := r.run(ctx, nil, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
		if err != nil {
			return HistoryResult{}, fmt.Errorf("%w: %s is not a commit here", ErrNotAllowed, rev)
		}
		resolved = append(resolved, strings.TrimSpace(string(out)))
	}
	status, err := s.requireClean(ctx)
	if err != nil {
		return HistoryResult{}, err
	}
	if err := s.gate(ctx, status.Branch, what, strconv.Itoa(len(resolved))+" commit(s)"); err != nil {
		return HistoryResult{}, err
	}
	startedAt, err := s.head(ctx)
	if err != nil {
		return HistoryResult{}, err
	}
	args := append(append([]string{verb}, flags...), "--no-edit")
	args = append(args, resolved...)
	result, err := s.sequence(ctx, r, op, args...)
	if err != nil || result.Stopped != OperationNone || in.TaskKey == "" {
		return result, err
	}
	// The trailer, once the commits exist: on the newest of them, which is the
	// one an amend can reach without rewriting the others.
	if result.Head.SHA != startedAt.SHA {
		message := WithTaskTrailer(fullMessage(result.Head), in.TaskKey)
		if err := s.runAsIdentity(ctx, r, true, "commit", "--amend", "--allow-empty", "-m", message); err != nil {
			return HistoryResult{}, err
		}
		return s.historyResult(ctx, OperationNone)
	}
	return result, nil
}

// fullMessage is a commit's subject and body as one message.
func fullMessage(c Commit) string {
	if strings.TrimSpace(c.Body) == "" {
		return c.Subject
	}
	return c.Subject + "\n\n" + c.Body
}

// withoutTaskTrailer drops the task trailer, which Squash adds back once for
// every task the squashed commits named.
func withoutTaskTrailer(message string) string {
	lines := strings.Split(message, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), TaskTrailer) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.TrimRight(strings.Join(kept, "\n"), "\n")
}
