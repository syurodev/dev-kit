//go:build unix

package git

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"devkit/internal/fchdirexec"

	"golang.org/x/sys/unix"
)

type fileID struct{ dev, ino uint64 }

func fstatID(fd int) (fileID, error) {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return fileID{}, err
	}
	return fileID{uint64(st.Dev), uint64(st.Ino)}, nil
}

// boundCheckout is a checkout held open by descriptors: the directory the handle was
// opened at, the top of the checkout above it, and the git directory git found.
type boundCheckout struct {
	mu     sync.Mutex
	closed bool

	// path is the symlink-free path cwd was opened at.
	path  string
	cwd   *os.File
	cwdID fileID

	// top and gitDir are set by attach, once git has said where they are.
	top      *os.File
	topPath  string
	topID    fileID
	depth    int // directories between top and cwd
	gitDir   *os.File
	gitPath  string
	gitID    fileID
	dotGitIs bool // .git at top is a directory, and is the git directory
	procFD   bool // the git directory can be named as /proc/self/fd/N for git
}

// procFDUsable reports whether /proc/self/fd names descriptors here (Linux with /proc).
var procFDUsable = sync.OnceValue(func() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	info, err := os.Stat("/proc/self/fd")
	return err == nil && info.IsDir()
})

const dirFlags = unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC

// walkDir opens the directory at an absolute, symlink-free path: from the root, one
// component at a time with O_NOFOLLOW, so a component that is (or becomes) a link, or
// is not a directory, fails the walk. It never looks a name up as a whole path, so the
// result is the directory those names led to at that moment and nothing a later
// rename can change. (Linux's openat2 with RESOLVE_NO_SYMLINKS is the same walk in one
// call; macOS has none, so the one manual walk serves both.)
func walkDir(path string) (*os.File, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("git: %q is not absolute", path)
	}
	fd, err := unix.Open("/", dirFlags, 0)
	if err != nil {
		return nil, err
	}
	for _, name := range strings.Split(filepath.Clean(path), string(os.PathSeparator)) {
		if name == "" {
			continue
		}
		next, err := unix.Openat(fd, name, dirFlags, 0)
		_ = unix.Close(fd)
		if err != nil {
			return nil, err
		}
		fd = next
	}
	return os.NewFile(uintptr(fd), path), nil
}

func walkID(path string) (fileID, error) {
	f, err := walkDir(path)
	if err != nil {
		return fileID{}, err
	}
	defer f.Close()
	return fstatID(int(f.Fd()))
}

// movedErr maps the errors a walk can fail with: a name that is gone is "not there",
// a name that is now a link or a file is "moved or replaced".
func walkError(err error) error {
	switch {
	case errors.Is(err, unix.ENOENT):
		return fs.ErrNotExist
	case errors.Is(err, unix.ELOOP), errors.Is(err, unix.ENOTDIR):
		return errMoved()
	}
	return err
}

// openBound opens the checkout at path and returns it with only its working
// directory held; attach adds the rest once git has said where the top and the git
// directory are. The path is resolved once here and walked without following links.
func openBound(path string) (*boundCheckout, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	if resolved, err = filepath.Abs(resolved); err != nil {
		return nil, err
	}
	f, err := walkDir(resolved)
	if err != nil {
		return nil, walkError(err)
	}
	id, err := fstatID(int(f.Fd()))
	if err != nil {
		f.Close()
		return nil, err
	}
	return &boundCheckout{path: resolved, cwd: f, cwdID: id, procFD: procFDUsable()}, nil
}

// attach holds the top of the checkout and the git directory, both as git reported
// them, and checks that what git reported is what the descriptors say: the top is
// reached from the working directory by walking up through the directory itself, and
// must be the directory its name leads to; the git directory must be the one a name
// leads to. A command that was answered from another repository shows up here as
// names that lead somewhere else.
func (b *boundCheckout) attach(top, gitDir string) error {
	rel, err := filepath.Rel(top, b.path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return errMoved()
	}
	depth := 0
	if rel != "." {
		depth = strings.Count(rel, string(os.PathSeparator)) + 1
	}
	cur := int(b.cwd.Fd())
	owned := -1
	for i := 0; i < depth; i++ {
		up, err := unix.Openat(cur, "..", dirFlags, 0)
		if owned >= 0 {
			_ = unix.Close(owned)
		}
		if err != nil {
			return walkError(err)
		}
		cur, owned = up, up
	}
	if owned < 0 {
		var err error
		if owned, err = unix.Dup(cur); err != nil {
			return err
		}
	}
	topFile := os.NewFile(uintptr(owned), top)
	topID, err := fstatID(owned)
	if err != nil {
		topFile.Close()
		return err
	}
	if named, err := walkID(top); err != nil || named != topID {
		topFile.Close()
		return errMoved()
	}
	gitFile, err := walkDir(gitDir)
	if err != nil {
		topFile.Close()
		return walkError(err)
	}
	gitID, err := fstatID(int(gitFile.Fd()))
	if err != nil {
		topFile.Close()
		gitFile.Close()
		return err
	}
	dotGitIs := false
	var st unix.Stat_t
	if err := unix.Fstatat(owned, ".git", &st, unix.AT_SYMLINK_NOFOLLOW); err == nil &&
		st.Mode&unix.S_IFMT == unix.S_IFDIR && (fileID{uint64(st.Dev), uint64(st.Ino)}) == gitID {
		dotGitIs = true
	}
	b.mu.Lock()
	b.top, b.topPath, b.topID, b.depth = topFile, top, topID, depth
	b.gitDir, b.gitPath, b.gitID = gitFile, gitDir, gitID
	b.dotGitIs = dotGitIs
	b.mu.Unlock()
	return nil
}

// Close lets go of every descriptor. It is safe to call twice.
func (b *boundCheckout) Close() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for _, f := range []*os.File{b.cwd, b.top, b.gitDir} {
		if f != nil {
			_ = f.Close()
		}
	}
}

// statID is the identity of whatever a name leads to now (links followed): what the
// identity check compares with the identity of the directory that is held.
func statID(path string) (fileID, error) {
	var st unix.Stat_t
	if err := unix.Stat(path, &st); err != nil {
		return fileID{}, err
	}
	return fileID{uint64(st.Dev), uint64(st.Ino)}, nil
}

// verify is the identity check made before and after every command: each name the
// handle was judged by must still lead to the directory that is held. It narrows the
// window and refuses the answer of a command that ran while a name was rebound; it is
// not what binds the command (the descriptors are). A name that leads to the same
// directory through a link is the same directory, so this is a plain stat.
func (b *boundCheckout) verify() error {
	if b == nil {
		return nil
	}
	if id, err := statID(b.path); err != nil || id != b.cwdID {
		return errMoved()
	}
	if b.top == nil {
		return nil
	}
	if b.topPath != b.path {
		if id, err := statID(b.topPath); err != nil || id != b.topID {
			return errMoved()
		}
	}
	if id, err := statID(b.gitPath); err != nil || id != b.gitID {
		return errMoved()
	}
	if b.dotGitIs {
		var st unix.Stat_t
		if err := unix.Fstatat(int(b.top.Fd()), ".git", &st, unix.AT_SYMLINK_NOFOLLOW); err != nil ||
			(fileID{uint64(st.Dev), uint64(st.Ino)}) != b.gitID {
			return errMoved()
		}
	}
	return nil
}

// workTreeArg is the work tree as seen from the working directory.
func (b *boundCheckout) workTreeArg() string {
	if b.depth == 0 {
		return "."
	}
	return strings.TrimSuffix(strings.Repeat("../", b.depth), "/")
}

// forceHelper makes the helper (internal/fchdirexec) start every command, as it does
// where there is no /proc to name a descriptor by. Tests set it on Linux.
var forceHelper bool

// pins are the GIT_DIR and GIT_WORK_TREE an agent-origin command gets: relative to
// the working directory, and, for the git directory on Linux, a descriptor the child
// inherited (gitDirFD, its number in the child; 0 when none was passed).
func (b *boundCheckout) pins(gitDirFD int) []string {
	if b.top == nil {
		return nil
	}
	dir := b.gitPath
	switch {
	case gitDirFD > 0:
		dir = "/proc/self/fd/" + strconv.Itoa(gitDirFD)
	case b.dotGitIs:
		dir = strings.Repeat("../", b.depth) + ".git"
	}
	return []string{"GIT_DIR=" + dir, "GIT_WORK_TREE=" + b.workTreeArg()}
}

// command builds the process that runs git in the held directory with no path lookup,
// and the pins for its environment.
//
// On Linux the directory is named as /proc/<this pid>/fd/<n>, which the kernel
// resolves to the directory the descriptor holds, not to a name (os/exec changes into
// Dir in the child, before it executes git). Where there is no /proc, git is started
// through the helper (internal/fchdirexec), which does fchdir and execve. In both,
// the git directory, when held, is inherited as a descriptor and named by it.
func (b *boundCheckout) command(ctx context.Context, gitPath string, argv []string) (*exec.Cmd, []string, error) {
	b.mu.Lock()
	closed := b.closed
	b.mu.Unlock()
	if closed {
		return nil, nil, errMoved()
	}
	var cmd *exec.Cmd
	childFD := 3 // the first extra file of the child
	if b.procFD && !forceHelper {
		cmd = exec.CommandContext(ctx, gitPath, argv...)
		cmd.Dir = "/proc/" + strconv.Itoa(os.Getpid()) + "/fd/" + strconv.Itoa(int(b.cwd.Fd()))
	} else {
		self, err := fchdirexec.Self()
		if err != nil {
			return nil, nil, err
		}
		cmd = exec.CommandContext(ctx, self)
		cmd.Args = append([]string{fchdirexec.Arg0, gitPath}, argv...)
		cmd.ExtraFiles = []*os.File{b.cwd} // the helper changes into fd 3
		childFD = 4
	}
	gitDirFD := 0
	if b.procFD && b.gitDir != nil {
		cmd.ExtraFiles = append(cmd.ExtraFiles, b.gitDir)
		gitDirFD = childFD
	}
	return cmd, b.pins(gitDirFD), nil
}

// run runs cmd, and on Linux watches where the process is while it runs. git changes
// into the work tree by NAME (see bound.go), so a name that is rebound in that moment
// sends the process into another directory, and it stays there until it exits. Its
// working directory is readable from outside (/proc/<pid>/cwd), so a process whose
// working directory is neither the directory that was opened nor the top of the
// checkout is stopped and the command refused. It catches the process within a
// fraction of a millisecond of it going there; a command that finishes faster than the
// watcher's first look is not caught by this (the identity check after the command is
// then what remains).
func (b *boundCheckout) run(cmd *exec.Cmd) error {
	if !b.procFD {
		return cmd.Run()
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	var strayed atomic.Bool
	stop := make(chan struct{})
	watched := make(chan struct{})
	go func() {
		defer close(watched)
		cwd := "/proc/" + strconv.Itoa(cmd.Process.Pid) + "/cwd"
		for {
			select {
			case <-stop:
				return
			default:
			}
			if id, err := statID(cwd); err == nil && id != b.cwdID && id != b.topID {
				strayed.Store(true)
				_ = cmd.Process.Kill()
				return
			}
			time.Sleep(20 * time.Microsecond)
		}
	}()
	err := cmd.Wait()
	close(stop)
	<-watched
	if strayed.Load() {
		return errMoved()
	}
	return err
}

// ---- reads and writes in the work tree, through the held directory ----

// walkBelow opens the directory that holds rel's last element, below dir, without
// following a link, creating the directories when mk is set.
func walkBelow(dir int, rel string, mk bool) (fd int, leaf string, err error) {
	parts := strings.Split(filepath.ToSlash(filepath.Clean(rel)), "/")
	if len(parts) == 0 || parts[0] == ".." || filepath.IsAbs(rel) {
		return -1, "", errors.New("git: path escapes the project")
	}
	cur, err := unix.Dup(dir)
	if err != nil {
		return -1, "", err
	}
	for _, name := range parts[:len(parts)-1] {
		next, err := unix.Openat(cur, name, dirFlags, 0)
		if err != nil && mk && errors.Is(err, unix.ENOENT) {
			if err = unix.Mkdirat(cur, name, 0o755); err == nil || errors.Is(err, unix.EEXIST) {
				next, err = unix.Openat(cur, name, dirFlags, 0)
			}
		}
		_ = unix.Close(cur)
		if err != nil {
			return -1, "", err
		}
		cur = next
	}
	return cur, parts[len(parts)-1], nil
}

// readUnder reads one project-relative file below the held working directory; a link
// anywhere on the way, the last element included, is refused. Missing is (nil, false).
func (b *boundCheckout) readUnder(rel string) ([]byte, bool, error) {
	dir, leaf, err := walkBelow(int(b.cwd.Fd()), rel, false)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil, false, nil
		}
		return nil, false, err
	}
	defer unix.Close(dir)
	fd, err := unix.Openat(dir, leaf, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil, false, nil
		}
		return nil, false, err
	}
	f := os.NewFile(uintptr(fd), rel)
	defer f.Close()
	data, err := io.ReadAll(f)
	return data, err == nil, err
}

// writeUnder writes one project-relative file below the held working directory,
// creating its directories, refusing links on the way and at the file.
func (b *boundCheckout) writeUnder(rel string, content []byte) error {
	dir, leaf, err := walkBelow(int(b.cwd.Fd()), rel, true)
	if err != nil {
		return err
	}
	defer unix.Close(dir)
	fd, err := unix.Openat(dir, leaf, unix.O_WRONLY|unix.O_CREAT|unix.O_TRUNC|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o644)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), rel)
	defer f.Close()
	_, err = f.Write(content)
	return err
}

// lstatUnder is Lstat of one project-relative path below the held working directory:
// its mode and size, or the error (fs.ErrNotExist for a missing path).
func (b *boundCheckout) lstatUnder(rel string) (mode fs.FileMode, size int64, err error) {
	dir, leaf, err := walkBelow(int(b.cwd.Fd()), rel, false)
	if err != nil {
		return 0, 0, notExist(err)
	}
	defer unix.Close(dir)
	return statAt(dir, leaf)
}

func statAt(dir int, name string) (fs.FileMode, int64, error) {
	var st unix.Stat_t
	if err := unix.Fstatat(dir, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return 0, 0, notExist(err)
	}
	mode := fs.FileMode(st.Mode & 0o777)
	switch st.Mode & unix.S_IFMT {
	case unix.S_IFDIR:
		mode |= fs.ModeDir
	case unix.S_IFLNK:
		mode |= fs.ModeSymlink
	case unix.S_IFREG:
	default:
		mode |= fs.ModeIrregular
	}
	return mode, int64(st.Size), nil
}

func notExist(err error) error {
	if errors.Is(err, unix.ENOENT) {
		return fs.ErrNotExist
	}
	return err
}

// existsInGitDir reports whether name exists directly under the held git directory;
// known is false when none is held.
func (b *boundCheckout) existsInGitDir(name string) (found, known bool) {
	if b == nil || b.gitDir == nil {
		return false, false
	}
	_, _, err := statAt(int(b.gitDir.Fd()), name)
	return err == nil, true
}

// boundPath is the symlink-free path the working directory was opened at.
func (b *boundCheckout) boundPath() string {
	if b == nil {
		return ""
	}
	return b.path
}

// gitDirFile reads one file directly under the held git directory (never a link).
func (b *boundCheckout) gitDirFile(name string) ([]byte, error) {
	if b.gitDir == nil {
		return nil, fs.ErrNotExist
	}
	fd, err := unix.Openat(int(b.gitDir.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, notExist(err)
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, 1<<16))
}

// dotGit reads what is at `.git` in the top of the checkout, through the held top.
func (b *boundCheckout) dotGit() (DotGit, error) {
	top := int(b.top.Fd())
	var st unix.Stat_t
	if err := unix.Fstatat(top, ".git", &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return DotGit{Kind: "none"}, nil
		}
		return DotGit{}, err
	}
	switch st.Mode & unix.S_IFMT {
	case unix.S_IFDIR:
		return DotGit{Kind: "dir", IsGitDir: (fileID{uint64(st.Dev), uint64(st.Ino)}) == b.gitID}, nil
	case unix.S_IFLNK:
		buf := make([]byte, 4096)
		n, _ := unix.Readlinkat(top, ".git", buf)
		text := string(buf[:max(n, 0)])
		var followed unix.Stat_t
		same := unix.Fstatat(top, ".git", &followed, 0) == nil && (fileID{uint64(followed.Dev), uint64(followed.Ino)}) == b.gitID
		return DotGit{Kind: "link", Gitdir: text, IsGitDir: same}, nil
	}
	fd, err := unix.Openat(top, ".git", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return DotGit{}, err
	}
	f := os.NewFile(uintptr(fd), ".git")
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 1<<16))
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

func (b *boundCheckout) identity() Identity {
	return Identity{Top: b.topPath, TopDev: b.topID.dev, TopIno: b.topID.ino, GitDirDev: b.gitID.dev, GitDirIno: b.gitID.ino}
}
