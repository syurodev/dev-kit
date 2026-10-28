package git

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// workTreePath resolves one project-relative path inside the work tree,
// refusing to escape it. A path from the frontend or from an agent is input,
// not a fact.
func workTreePath(work, rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) || strings.HasPrefix(rel, "..") {
		return "", errors.New("git: path escapes the project")
	}
	full := filepath.Join(work, filepath.FromSlash(rel))
	cleanWork := filepath.Clean(work)
	if full != cleanWork && !strings.HasPrefix(full, cleanWork+string(os.PathSeparator)) {
		return "", errors.New("git: path escapes the project")
	}
	return full, nil
}

// checkRel is the same refusal workTreePath makes, for the descriptor-bound readers.
func checkRel(rel string) error {
	if rel == "" || filepath.IsAbs(rel) || strings.HasPrefix(rel, "..") {
		return errors.New("git: path escapes the project")
	}
	return nil
}

// readWork, writeWork and lstatWork are the work tree's files as the runner sees
// them: through the held directory, refusing every link, for a bound handle (the
// name of the folder is not looked up again); by path for the others.
func (r runner) readWork(rel string) (string, bool, error) {
	if r.bound == nil {
		return readWorkTreeFile(r.work, rel)
	}
	if err := checkRel(rel); err != nil {
		return "", false, err
	}
	data, ok, err := r.bound.readUnder(filepath.FromSlash(rel))
	return string(data), ok, err
}

func (r runner) writeWork(rel, content string) error {
	if r.bound == nil {
		return writeWorkTreeFile(r.work, rel, content)
	}
	if err := checkRel(rel); err != nil {
		return err
	}
	return r.bound.writeUnder(filepath.FromSlash(rel), []byte(content))
}

func (r runner) lstatWork(rel string) (mode os.FileMode, size int64, err error) {
	if r.bound == nil {
		info, err := os.Lstat(filepath.Join(r.work, filepath.FromSlash(rel)))
		if err != nil {
			return 0, 0, err
		}
		return info.Mode(), info.Size(), nil
	}
	if err := checkRel(rel); err != nil {
		return 0, 0, err
	}
	return r.bound.lstatUnder(filepath.FromSlash(rel))
}

// writeWorkTreeFile writes one project-relative path.
func writeWorkTreeFile(work, rel, content string) error {
	full, err := workTreePath(work, rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, []byte(content), 0o644)
}

// readWorkTreeFile reads one project-relative path. A missing file is empty
// and not an error: during a merge it means the path is gone from the work
// tree, which is something the caller has to show rather than fail on.
func readWorkTreeFile(work, rel string) (string, bool, error) {
	full, err := workTreePath(work, rel)
	if err != nil {
		return "", false, err
	}
	content, err := os.ReadFile(full)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return string(content), true, nil
}
