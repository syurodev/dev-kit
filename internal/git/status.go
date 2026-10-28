package git

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"devkit/internal/gitparse"
)

// Status reads the work tree. It is the single source of "what changed" —
// codegraph's file-tree markers read this rather than running git themselves.
func (s *Service) Status(ctx context.Context) (Status, error) {
	r, err := s.active()
	if err != nil {
		return Status{}, err
	}
	return readStatus(ctx, r, "all")
}

// readStatus is Status for any work tree. untracked is git's
// --untracked-files mode: "all" lists every untracked file, which the Changes
// view needs, and "normal" stops at a directory's name, which is enough to
// count and does not walk a build output.
func readStatus(ctx context.Context, r runner, untracked string) (Status, error) {
	out, err := r.run(ctx, nil, "status", "--porcelain=v1", "-z", "--untracked-files="+untracked, "--branch")
	if err != nil {
		return Status{}, err
	}
	status := Status{Files: map[string]string{}}
	records := strings.Split(string(out), "\x00")
	for i := 0; i < len(records); i++ {
		entry := records[i]
		if strings.HasPrefix(entry, "## ") {
			readBranchLine(&status, entry[3:])
			continue
		}
		if len(entry) < 4 {
			continue
		}
		xy, file := entry[:2], entry[3:]
		if xy[0] == 'R' || xy[0] == 'C' {
			i++ // the next record is the rename source, not another entry
		}
		status.Files[file] = gitparse.State(xy)
		switch {
		case xy == "??":
			status.Untracked = append(status.Untracked, file)
		case isConflict(xy):
			// A conflicted path is not "modified in both": it needs the
			// three-pane resolve, and staging it blindly would commit the
			// markers.
			status.Conflicted = append(status.Conflicted, file)
		default:
			if xy[0] != ' ' {
				status.Staged = append(status.Staged, file)
			}
			if xy[1] != ' ' {
				status.Unstaged = append(status.Unstaged, file)
			}
		}
	}
	for _, list := range []*[]string{&status.Staged, &status.Unstaged, &status.Untracked, &status.Conflicted} {
		sort.Strings(*list)
	}
	return status, nil
}

// isConflict reads the porcelain pairs git uses for an unmerged path. DD, AU,
// UD, UA, DU, AA and UU are all "both sides touched it".
func isConflict(xy string) bool {
	switch xy {
	case "DD", "AU", "UD", "UA", "DU", "AA", "UU":
		return true
	}
	return false
}

// readBranchLine reads `## main...origin/main [ahead 1, behind 2]`.
func readBranchLine(status *Status, line string) {
	if strings.HasPrefix(line, "HEAD (no branch)") {
		status.Detached = true
		return
	}
	head, tracking, _ := strings.Cut(line, " [")
	branch, upstream, hasUpstream := strings.Cut(head, "...")
	status.Branch = branch
	if hasUpstream {
		status.Upstream = upstream
	}
	for _, part := range strings.Split(strings.TrimSuffix(tracking, "]"), ", ") {
		name, count, ok := strings.Cut(part, " ")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(count)
		if err != nil {
			continue
		}
		switch name {
		case "ahead":
			status.Ahead = n
		case "behind":
			status.Behind = n
		}
	}
}

// Diff reports how the work tree or a commit differs. An empty rev diffs the
// work tree against HEAD; a rev diffs that commit against its parent.
func (s *Service) Diff(ctx context.Context, rev string, staged bool) ([]gitparse.Change, error) {
	r, err := s.active()
	if err != nil {
		return nil, err
	}
	rawArgs := []string{"diff", "-r", "-z", "--raw", "--no-renames"}
	statArgs := []string{"diff", "-r", "-z", "--numstat", "--no-renames"}
	switch {
	case rev != "":
		sha, parent, err := commitComparison(ctx, r, rev)
		if err != nil {
			return nil, err
		}
		if parent == "" {
			rawArgs = []string{"diff-tree", "--root", "--no-commit-id", "-r", "-z", "--raw", "--no-renames", sha, "--"}
			statArgs = []string{"diff-tree", "--root", "--no-commit-id", "-r", "-z", "--numstat", "--no-renames", sha, "--"}
		} else {
			rawArgs = append(rawArgs, parent, sha, "--")
			statArgs = append(statArgs, parent, sha, "--")
		}
	case staged:
		rawArgs = append(rawArgs, "--cached")
		statArgs = append(statArgs, "--cached")
	}
	rawOut, err := r.run(ctx, nil, rawArgs...)
	if err != nil {
		return nil, err
	}
	changes, order, err := gitparse.Raw(rawOut)
	if err != nil {
		return nil, err
	}
	statOut, err := r.run(ctx, nil, statArgs...)
	if err != nil {
		return nil, err
	}
	if err := gitparse.Numstat(statOut, changes); err != nil {
		return nil, err
	}
	out := make([]gitparse.Change, 0, len(order))
	for _, path := range order {
		out = append(out, *changes[path])
	}
	return out, nil
}

// Resolve once so file statistics and the patch use the same first-parent
// comparison, including roots and merges. Options/ranges are not revisions.
func commitComparison(ctx context.Context, r runner, rev string) (sha, parent string, err error) {
	if rev == "" || strings.HasPrefix(rev, "-") {
		return "", "", fmt.Errorf("%w: name a single commit, not an option or a range", ErrNotAllowed)
	}
	out, err := r.run(ctx, nil, "rev-parse", "--verify", "--end-of-options", rev+"^{commit}")
	if err != nil {
		return "", "", err
	}
	sha = strings.TrimSpace(string(out))
	out, err = r.run(ctx, nil, "rev-list", "--parents", "-n", "1", sha, "--")
	if err != nil {
		return "", "", err
	}
	fields := strings.Fields(string(out))
	if len(fields) > 1 {
		parent = fields[1]
	}
	return sha, parent, nil
}

// commitFormat is the log's per-commit format. %x00 between fields and %x01
// between commits: a subject may contain anything, including whatever
// separator seemed safe. The body is last so a stray field boundary in it
// cannot shift the others.
const commitFormat = "--pretty=format:%H%x00%h%x00%an%x00%aI%x00%p%x00%s%x00%b%x01"

// parseCommits reads what commitFormat printed.
func parseCommits(out []byte) []Commit {
	var commits []Commit
	for _, record := range strings.Split(string(out), "\x01") {
		record = strings.TrimLeft(record, "\n")
		if record == "" {
			continue
		}
		fields := strings.Split(record, "\x00")
		if len(fields) < 7 {
			continue
		}
		parent := ""
		if parents := strings.Fields(fields[4]); len(parents) > 0 {
			parent = parents[0]
		}
		commits = append(commits, Commit{
			SHA: fields[0], Short: fields[1], Author: fields[2], Date: fields[3],
			Parent: parent, Subject: fields[5], Body: strings.TrimRight(fields[6], "\n"),
		})
	}
	return commits
}

// Log is the current branch's history, newest first and bounded.
func (s *Service) Log(ctx context.Context, limit int) ([]Commit, error) {
	r, err := s.active()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	out, err := r.run(ctx, nil, "log", "-n", strconv.Itoa(limit), commitFormat)
	if err != nil {
		// A repository with no commits yet is empty, not broken.
		if strings.Contains(err.Error(), "does not have any commits") {
			return nil, nil
		}
		return nil, err
	}
	return parseCommits(out), nil
}

// revPattern is what a commit view may ask about: a hash, and nothing git
// could read as an option or a range.
var revPattern = regexp.MustCompile(`^[0-9a-fA-F]{4,64}$`)

// Show is one commit — the log's entry for it, with its parent.
func (s *Service) Show(ctx context.Context, rev string) (Commit, error) {
	r, err := s.active()
	if err != nil {
		return Commit{}, err
	}
	if !revPattern.MatchString(rev) {
		return Commit{}, fmt.Errorf("%w: that is not a commit name", ErrNotAllowed)
	}
	out, err := r.run(ctx, nil, "show", "-s", commitFormat, rev)
	if err != nil {
		return Commit{}, err
	}
	commits := parseCommits(out)
	if len(commits) == 0 {
		return Commit{}, fmt.Errorf("%w: git shows no commit for that name", ErrNotAllowed)
	}
	return commits[0], nil
}

// RevPatch is one file's unified diff in one commit, against its parent —
// what a commit view draws as before and after. A binary file has no hunks.
func (s *Service) RevPatch(ctx context.Context, rev, path string) (string, error) {
	r, err := s.active()
	if err != nil {
		return "", err
	}
	if !revPattern.MatchString(rev) || path == "" {
		return "", fmt.Errorf("%w: name a commit and a file path", ErrNotAllowed)
	}
	sha, parent, err := commitComparison(ctx, r, rev)
	if err != nil {
		return "", err
	}
	args := []string{"--literal-pathspecs", "diff", "--no-color", "--no-ext-diff", "--no-textconv", "--no-renames", "-U3", parent, sha, "--", path}
	if parent == "" {
		args = []string{"--literal-pathspecs", "show", "--root", "--no-color", "--no-ext-diff", "--no-textconv", "--no-renames", "-U3", "--format=", sha, "--", path}
	}
	out, err := r.run(ctx, nil, args...)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// CommitsForTask finds the commits carrying a task's key in a trailer.
//
// This is the entire task-to-commit link, and it is asked rather than stored:
// a SHA rots under rebase, amend and squash, so a list of them in a synced
// task record would go quietly wrong. Grep costs one git call at display time
// and survives all three.
func (s *Service) CommitsForTask(ctx context.Context, key string, limit int) ([]Commit, error) {
	r, err := s.active()
	if err != nil {
		return nil, err
	}
	if key == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	out, err := r.run(ctx, nil, "log", "-n", strconv.Itoa(limit),
		"--grep=^Task: "+key+"$", "--extended-regexp",
		"--pretty=format:%H%x00%h%x00%an%x00%aI%x00%s%x00%b%x01")
	if err != nil {
		return nil, nil // no commits, no history, no repository state to report
	}
	var commits []Commit
	for _, record := range strings.Split(string(out), "\x01") {
		record = strings.TrimLeft(record, "\n")
		if record == "" {
			continue
		}
		fields := strings.Split(record, "\x00")
		if len(fields) < 6 {
			continue
		}
		commits = append(commits, Commit{
			SHA: fields[0], Short: fields[1], Author: fields[2],
			Date: fields[3], Subject: fields[4], Body: strings.TrimRight(fields[5], "\n"),
		})
	}
	return commits, nil
}

// FilePatch returns one file's unified diff, work tree against the index.
//
// That pairing is deliberate and is what makes hunk staging correct: the hunks
// come out measured against the index, and StageHunk applies them to the index
// with `git apply --cached`. It is the same pair `git add -p` uses, so a file
// that is already partly staged keeps working — the diff is against what is
// staged, not against HEAD.
//
// A binary file has no hunks to pick and comes back empty: the caller stages
// the whole file or nothing. An untracked file is not in the index, so git
// would not diff it; it comes back as a new-file patch (every line added) so a
// reader can see what staging it would add. That patch is for reading — hunk
// staging only ever asks about files git already tracks.
func (s *Service) FilePatch(ctx context.Context, path string) (string, error) {
	return s.filePatch(ctx, path, false)
}

// IndexPatch is the staged file as it will enter the next commit.
func (s *Service) IndexPatch(ctx context.Context, path string) (string, error) {
	return s.filePatch(ctx, path, true)
}

func (s *Service) filePatch(ctx context.Context, path string, staged bool) (string, error) {
	r, err := s.active()
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", fmt.Errorf("%w: name a file path", ErrNotAllowed)
	}
	// --no-ext-diff: a repository configuring an external differ would return
	// something that is not a patch at all.
	args := []string{"--literal-pathspecs", "diff", "--no-color", "--no-ext-diff", "--no-textconv", "--no-renames", "-U3"}
	if staged {
		args = append(args, "--cached")
	}
	args = append(args, "--", path)
	out, err := r.run(ctx, nil, args...)
	if err != nil {
		return "", err
	}
	if len(out) == 0 && !staged {
		return untrackedPatch(ctx, r, path)
	}
	return string(out), nil
}

// maxUntrackedPatch is the largest untracked file shown as text. A build
// output or a data dump left in the work tree must not be read whole into the
// window; past this it is reported the way git reports a binary file.
const maxUntrackedPatch = 2 << 20

// untrackedPatch is a new-file patch for an untracked path, "" for anything
// else. Membership is asked of git rather than assumed from an empty diff: an
// unchanged tracked file also diffs to nothing, and an ignored or missing path
// must not be read just because someone named it.
func untrackedPatch(ctx context.Context, r runner, path string) (string, error) {
	listed, err := r.run(ctx, nil, "--literal-pathspecs", "ls-files", "--others", "--exclude-standard", "-z", "--", path)
	if err != nil {
		return "", err
	}
	if strings.TrimRight(string(listed), "\x00") != path {
		return "", nil
	}
	if mode, size, err := r.lstatWork(path); err != nil {
		return "", nil
	} else if mode.IsRegular() && size > maxUntrackedPatch {
		return fmt.Sprintf("diff --git a/%[1]s b/%[1]s\nnew file mode 100644\nBinary files /dev/null and b/%[1]s differ\n", path), nil
	}
	// --no-index diffs two paths git does not have to track; it exits 1 when
	// they differ, which is the answer here and not a failure.
	out, err := r.run(ctx, nil, "diff", "--no-index", "--no-color", "--no-ext-diff", "--no-textconv", "-U3", "--", "/dev/null", filepath.FromSlash(path))
	var exit *exec.ExitError
	if err != nil && !(errors.As(err, &exit) && exit.ExitCode() == 1) {
		return "", err
	}
	return string(out), nil
}
