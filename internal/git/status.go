package git

import (
	"context"
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
	out, err := r.run(ctx, nil, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--branch")
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
		rawArgs = []string{"diff-tree", "-r", "-z", "--raw", "--no-renames", rev + "^!"}
		statArgs = []string{"diff-tree", "-r", "-z", "--numstat", "--no-renames", rev + "^!"}
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

// Log is the current branch's history, newest first and bounded.
func (s *Service) Log(ctx context.Context, limit int) ([]Commit, error) {
	r, err := s.active()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	// %x00 between fields and %x01 between commits: a subject may contain
	// anything, including whatever separator seemed safe.
	out, err := r.run(ctx, nil, "log", "-n", strconv.Itoa(limit),
		"--pretty=format:%H%x00%h%x00%an%x00%aI%x00%s%x00%b%x01")
	if err != nil {
		// A repository with no commits yet is empty, not broken.
		if strings.Contains(err.Error(), "does not have any commits") {
			return nil, nil
		}
		return nil, err
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
