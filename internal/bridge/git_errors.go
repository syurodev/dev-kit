package bridge

import (
	"errors"
	"strings"
	"unicode"

	"devkit/internal/apperrors"
	gitservice "devkit/internal/git"
)

// gitRefusals are the sentinels internal/git raises for a refusal of its own
// (a rule, not a failure of git): pushed history it will not rewrite, a work
// tree that is not clean, an operation under way, a branch an agent may not
// push to. They are plain errors, not AppErrors, so without a line here they
// reached the window and every agent as internal_error ("An unexpected error
// occurred.") and were logged as unclassified.
//
// The sentence is the one written at the fmt.Errorf("%w: sentence") site that
// raised it (refusalSentence takes exactly that site's text, never an outer
// wrap). Those sites are read: they name numbers, a short sha, an operation
// (rebase, cherry-pick, revert), a branch or revision the caller itself gave,
// or a remote and environment tag the person configured — never git's stderr
// or an underlying error's text. A new site must keep to that; one that must
// quote git does not wrap one of these sentinels. fallback is said when the
// sentinel arrives bare.
var gitRefusals = []struct {
	sentinel error
	code     string
	fallback string
}{
	{gitservice.ErrPushedHistory, "git.pushedHistory", "That history is already on a remote, and DevKit never force-pushes."},
	{gitservice.ErrDirtyWorkTree, "git.dirtyWorkTree", "The work tree has uncommitted changes — commit or stash them first."},
	{gitservice.ErrInProgress, "git.operationInProgress", "A rebase, cherry-pick or revert is already under way — continue or abort it first."},
	{gitservice.ErrNothingInProgress, "git.nothingInProgress", "No such operation is under way."},
	{gitservice.ErrProtectedBranch, "git.protectedBranch", "An agent may push only to a branch recorded on a task it holds."},
	{gitservice.ErrNotAllowed, "git.notAllowed", "DevKit refused this git operation."},
	{gitservice.ErrConflict, "git.conflict", "The work tree has unresolved conflicts — resolve them first."},
	{gitservice.ErrNoSuchSide, "git.noSuchSide", "A conflict side is \"ours\" or \"theirs\"."},
	{gitservice.ErrNoProject, "git.noProject", "No project folder is open for git."},
	{gitservice.ErrGitUnavailable, "git.unavailable", "git is not usable on this machine."},
	{gitservice.ErrNoRepository, "git.noRepository", "This folder is not a git repository."},
	{gitservice.ErrWorktreeDirty, "git.worktreeDirty", "The worktree has uncommitted changes — commit or stash them first."},
	{gitservice.ErrWorktreeOutside, "git.worktreeOutside", "The worktree path would escape the repository."},
	{gitservice.ErrWorktreeExists, "git.worktreeExists", "A worktree already exists at that location."},
	{gitservice.ErrWorktreeNotFound, "git.worktreeNotFound", "No such worktree."},
	{gitservice.ErrBranchCheckedOut, "git.branchCheckedOut", "That branch is already checked out in another worktree."},
}

// gitRefusalError maps one of gitRefusals to an AppError carrying its sentence,
// or returns nil for any other error.
func gitRefusalError(err error) *apperrors.AppError {
	for _, refusal := range gitRefusals {
		if !errors.Is(err, refusal.sentinel) {
			continue
		}
		message := tidySentence(refusalSentence(err, refusal.sentinel))
		if message == "" {
			message = refusal.fallback
		}
		return apperrors.New(refusal.code, message, true)
	}
	return nil
}

// refusalSentence is the text after "<sentinel>: " in the error that wraps the
// sentinel directly — the fmt.Errorf site's own sentence, without whatever an
// outer caller added around it. A bare sentinel has none.
func refusalSentence(err, sentinel error) string {
	for current := err; current != nil; current = errors.Unwrap(current) {
		if current == sentinel {
			return ""
		}
		if errors.Unwrap(current) == sentinel {
			// Only text that starts with the sentinel is a sentence written at
			// its site; an outer wrap around a bare sentinel is not one.
			sentence, ok := strings.CutPrefix(current.Error(), sentinel.Error()+": ")
			if !ok {
				return ""
			}
			return sentence
		}
	}
	return ""
}

// tidySentence folds a sentence to one line without control characters and
// bounds it, since part of it can be text the caller sent.
func tidySentence(text string) string {
	text = strings.Join(strings.FieldsFunc(text, unicode.IsSpace), " ")
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, text)
	if runes := []rune(text); len(runes) > 300 {
		text = string(runes[:299]) + "…"
	}
	return text
}
