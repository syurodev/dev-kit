package git

import (
	"errors"

	"devkit/internal/apperrors"
)

// Descriptor-bound execution (platform parts in bound_unix.go and fdexec_unix.go).
//
// A handle opened for an agent used to run every command as `git -C <path>`, so the
// kernel looked the path up again for each command. An agent that can write in its
// granted folder can rename the directory away and put a link to another repository
// in its place, and the same handle then answers from the other one. For an agent-
// origin handle on a platform that has descriptors, the checkout is therefore opened
// once, component by component without following a link, and kept open: git is
// started with that directory as its working directory without a path lookup (the
// small helper in fdexec_unix.go does fchdir and then execve), and, on Linux, with
// its git directory also given as a descriptor (GIT_DIR=/proc/self/fd/N) so the
// repository's refs, objects, configuration and index are never looked up by name.
//
// What this does not do is make git itself descriptor-bound. A command that needs the
// work tree makes git change directory to the work tree's NAME, which it computes from
// its working directory (getcwd) a moment earlier; a name that is rebound in that
// moment (microseconds) sends that one command into another directory. The identity
// check before and after every command (verify) refuses the answer when the name no
// longer is the directory that was opened, which catches most of those and all of the
// swaps that stay in place; it cannot catch a name that was rebound and restored while
// a command ran. See the spec for the measurements.

// errMoved is what an agent-origin handle reports when the folder it was opened at is
// no longer the directory that was checked. It reuses the public code the scope
// checks already publish.
func errMoved() error {
	return apperrors.New("git.repoOutsideWorkspace",
		"That folder was moved or replaced while it was open, so it is no longer the repository that was checked.", true)
}

// isMoved reports whether err is errMoved's refusal, however it was wrapped.
func isMoved(err error) bool {
	var app *apperrors.AppError
	return errors.As(err, &app) && app.Code == "git.repoOutsideWorkspace"
}

// errBoundUnsupported is returned by the platform stubs: the handle then runs as the
// hardened, path-based handle of the first hardening delivery.
var errBoundUnsupported = errors.New("git: descriptor-bound execution is not available on this platform")

// DotGit is what the top of a checkout has at `.git`, read through the descriptor of
// that directory when the handle is bound.
type DotGit struct {
	// Kind is "dir", "file", "link" or "none".
	Kind string
	// Gitdir is, for a file, the path its `gitdir:` line names exactly as written;
	// for a link, the link's text.
	Gitdir string
	// IsGitDir reports that a directory or link is the very directory the handle
	// holds as the git directory git found (compared by device and inode).
	IsGitDir bool
}

// Identity is what was verified when the handle was opened: the resolved top of the
// checkout and the device and inode of it and of the git directory.
type Identity struct {
	Top                  string
	TopDev, TopIno       uint64
	GitDirDev, GitDirIno uint64
}
