// internal/checkpoint/maintenance.go
package checkpoint

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Keeping the shadow repositories small.
//
// Every turn writes two trees, and trees are cheap only for as long as nobody
// keeps them forever: the objects behind them are not. Maintenance runs at
// startup and once a day, under the same per-project mutex as a snapshot, so it
// can never delete a tree a turn is in the middle of writing.
//
// The rules are the plan's: 30 days, 200 checkpoints per session, a soft 1 GiB
// per project, and 7 days for the safety rows a restore or a rewind leaves
// behind — those exist to undo something the user just did, and a week later
// nobody is undoing it.

// safetyRetention is how long a rewind_safety / restore_safety row outlives its
// operation.
const safetyRetention = 7 * 24 * time.Hour

// looseObjectsBeforeGC is when repacking is worth its seconds. Below it,
// pack-refs alone keeps the directory tidy.
const looseObjectsBeforeGC = 5000

// Record is the stored side of a checkpoint, as maintenance needs to see it.
// The engine does not own the table — Phase D's store does — so this is the
// narrow view it asks for.
type Record struct {
	ID         string
	SessionID  string
	ProjectKey string
	// Kind is the checkpoint kind: "turn", "restore_safety", "rewind_safety".
	Kind      string
	CreatedAt time.Time
}

// IsSafety reports whether the record exists only to undo a restore or rewind.
func (r Record) IsSafety() bool { return strings.HasSuffix(r.Kind, "_safety") }

// Records is the store, as maintenance needs it.
type Records interface {
	ListCheckpoints(ctx context.Context) ([]Record, error)
	DeleteCheckpoints(ctx context.Context, ids []string) error
}

// MaintenanceReport is what one pass did, for the log and for the tests.
type MaintenanceReport struct {
	// Expired are the checkpoint ids removed from the store.
	Expired []string
	// RemovedProjects are shadow directories deleted because nothing referred
	// to them any more.
	RemovedProjects []string
	// Repacked are the project keys a gc ran in.
	Repacked []string
}

// Maintain prunes stored checkpoints, then brings each shadow repository in
// line with what survived.
//
// Store first, disk second, and deliberately so: a row with no tree is a
// checkpoint that shows the user nothing, while a tree with no row is invisible
// waste the next pass collects. Crashing halfway must leave the cheap problem.
func (e *Engine) Maintain(ctx context.Context, records Records) (MaintenanceReport, error) {
	var report MaintenanceReport
	if !e.git.OK() {
		return report, nil
	}
	all, err := records.ListCheckpoints(ctx)
	if err != nil {
		return report, err
	}
	now := time.Now()
	expired := expiredCheckpoints(all, now, e.root, e.limits)
	if len(expired) > 0 {
		if err := records.DeleteCheckpoints(ctx, expired); err != nil {
			return report, err
		}
		report.Expired = expired
	}

	live := map[string]map[string]bool{} // project key -> surviving checkpoint ids
	gone := map[string]bool{}
	for _, id := range expired {
		gone[id] = true
	}
	for _, record := range all {
		if gone[record.ID] {
			continue
		}
		if live[record.ProjectKey] == nil {
			live[record.ProjectKey] = map[string]bool{}
		}
		live[record.ProjectKey][record.ID] = true
	}

	entries, err := os.ReadDir(e.root)
	if os.IsNotExist(err) {
		return report, nil
	}
	if err != nil {
		return report, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		key := entry.Name()
		if len(live[key]) == 0 {
			// Nothing refers to this project any more: the whole shadow
			// repository goes, which is the only way disk actually comes back.
			if err := os.RemoveAll(filepath.Join(e.root, key)); err != nil {
				return report, err
			}
			e.forget(key)
			report.RemovedProjects = append(report.RemovedProjects, key)
			continue
		}
		repacked, err := e.sweepProject(ctx, key, live[key])
		if err != nil {
			return report, err
		}
		if repacked {
			report.Repacked = append(report.Repacked, key)
		}
	}
	sort.Strings(report.RemovedProjects)
	sort.Strings(report.Repacked)
	return report, nil
}

// expiredCheckpoints applies the three retention rules and returns the ids to
// drop, oldest first.
func expiredCheckpoints(all []Record, now time.Time, root string, limits Limits) []string {
	sorted := append([]Record(nil), all...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].CreatedAt.Before(sorted[j].CreatedAt) })

	doomed := map[string]bool{}
	ageCutoff := now.Add(-time.Duration(limits.RetentionDays) * 24 * time.Hour)
	safetyCutoff := now.Add(-safetyRetention)
	perSession := map[string]int{}
	// Newest first for the per-session cap: the 201st oldest is what goes.
	for i := len(sorted) - 1; i >= 0; i-- {
		record := sorted[i]
		switch {
		case record.CreatedAt.Before(ageCutoff):
			doomed[record.ID] = true
		case record.IsSafety() && record.CreatedAt.Before(safetyCutoff):
			doomed[record.ID] = true
		default:
			perSession[record.SessionID]++
			if perSession[record.SessionID] > limits.MaxPerSession {
				doomed[record.ID] = true
			}
		}
	}

	// The per-project soft cap is last and is a soft cap because the engine
	// cannot price one checkpoint: objects are shared between trees, so the
	// only honest move is to drop the oldest survivors until the directory is
	// back under the line and let the gc that follows reclaim what it can.
	byProject := map[string][]Record{}
	for _, record := range sorted {
		if !doomed[record.ID] {
			byProject[record.ProjectKey] = append(byProject[record.ProjectKey], record)
		}
	}
	for key, survivors := range byProject {
		size := projectDiskUsage(filepath.Join(root, key))
		for i := 0; size > limits.MaxProjectBytes && i < len(survivors)-1; i++ {
			doomed[survivors[i].ID] = true
			// Assume an even share, so the loop terminates on an estimate
			// rather than on a re-measure that cannot change until gc runs.
			size -= limits.MaxProjectBytes / int64(len(survivors))
		}
	}

	out := make([]string, 0, len(doomed))
	for _, record := range sorted {
		if doomed[record.ID] {
			out = append(out, record.ID)
		}
	}
	return out
}

func projectDiskUsage(dir string) int64 {
	var total int64
	filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if info, err := entry.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

// sweepProject drops the refs of checkpoints that no longer exist, then
// repacks. Reports whether a gc ran.
func (e *Engine) sweepProject(ctx context.Context, key string, live map[string]bool) (bool, error) {
	repo, err := e.openByKey(ctx, key)
	if err != nil {
		// A shadow directory whose project has been deleted from disk cannot be
		// opened (there is no work tree to point git at). Leaving it is right:
		// the store still has rows for it, and the project may come back on the
		// next external drive mount.
		return false, nil
	}
	var repacked bool
	err = repo.runLocked(func() error {
		raw, err := repo.git(ctx, nil, "for-each-ref", "--format=%(refname)", "refs/devkit/cp/")
		if err != nil {
			return err
		}
		for _, ref := range strings.Fields(string(raw)) {
			id := checkpointIDFromRef(ref)
			if id == "" || live[id] {
				continue
			}
			if _, err := repo.git(ctx, nil, "update-ref", "-d", ref); err != nil {
				return err
			}
		}
		if _, err := repo.git(ctx, nil, "pack-refs", "--all", "--prune"); err != nil {
			return err
		}
		loose, err := repo.looseObjects(ctx)
		if err != nil {
			return err
		}
		if loose < looseObjectsBeforeGC {
			return nil
		}
		// --prune=now is safe here and nowhere else: the project mutex is held,
		// so no snapshot is part-way through writing objects that nothing
		// points at yet.
		if _, err := repo.git(ctx, nil, "gc", "--prune=now", "--quiet"); err != nil {
			return err
		}
		repacked = true
		return nil
	})
	return repacked, err
}

// checkpointIDFromRef pulls the id out of refs/devkit/cp/<id>/<slot>.
func checkpointIDFromRef(ref string) string {
	const prefix = "refs/devkit/cp/"
	if !strings.HasPrefix(ref, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(ref, prefix)
	slash := strings.LastIndex(rest, "/")
	if slash <= 0 {
		return ""
	}
	return rest[:slash]
}

func (r *Repo) looseObjects(ctx context.Context) (int, error) {
	raw, err := r.git(ctx, nil, "count-objects", "-v")
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "count: "); ok {
			return strconv.Atoi(strings.TrimSpace(value))
		}
	}
	return 0, nil
}

// openByKey reopens a shadow repository from its directory, reading the project
// path back out of the marker file written at init.
func (e *Engine) openByKey(ctx context.Context, key string) (*Repo, error) {
	e.mu.Lock()
	repo, ok := e.repos[key]
	e.mu.Unlock()
	if ok {
		return repo, nil
	}
	raw, err := os.ReadFile(filepath.Join(e.root, key, projectFileName))
	if err != nil {
		return nil, err
	}
	return e.Open(ctx, strings.TrimSpace(string(raw)))
}

// forget drops a removed project's repository from the cache, so a project that
// comes back gets a freshly initialised one rather than a handle to a directory
// that is no longer there.
func (e *Engine) forget(key string) {
	e.mu.Lock()
	delete(e.repos, key)
	e.mu.Unlock()
}
