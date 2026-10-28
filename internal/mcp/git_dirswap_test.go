//go:build linux

package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gitservice "devkit/internal/git"

	"golang.org/x/sys/unix"
)

// The directory-swap probes of the independent review (section 3.2, probes 1 and 2),
// inverted: a handle that gitFor opened for a granted folder must never answer from
// another repository after that folder's name is rebound, whether the agent renames
// the directory away and puts a link in its place (probe 1, deterministic) or flips
// the two with renameat2(RENAME_EXCHANGE) while the tools are called (probe 2).
//
// A refusal (git.repoOutsideWorkspace) and the granted repository's own answer are
// both acceptable; the other repository's answer is not.

const (
	grantedSubject = "GRANTED-REPO"
	victimSubject  = "VICTIM-REPO-OUTSIDE-THE-GRANT"
)

func swapFixture(t *testing.T) (srv *Server, pkg, other, outside string) {
	t.Helper()
	requireGit(t)
	svc := gitservice.NewService(context.Background())
	srv, root, outside := linkedScopeServer(t, GitReposOf(svc))
	pkg, other = filepath.Join(root, "pkg"), filepath.Join(root, "pkg.x")
	probeRepo(t, pkg, grantedSubject)
	probeRepo(t, outside, victimSubject)
	// A file only the other repository's work tree has, to tell a status that was
	// read in the wrong directory.
	if err := os.WriteFile(filepath.Join(outside, "VICTIM_ONLY.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "GRANTED_ONLY.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	return srv, pkg, other, outside
}

// Probe 1: after gitFor opened a real directory, renaming it away and linking another
// repository into its place must not make the same handle answer from the other one.
func TestAgentGitHandleSurvivesTheDirectoryBeingSwappedForALink(t *testing.T) {
	srv, pkg, _, outside := swapFixture(t)

	api, _, err := srv.gitFor(agentCtx(), pkg, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := subject(t, api); got != grantedSubject {
		t.Fatalf("before the swap: %q", got)
	}
	if err := os.Rename(pkg, pkg+".moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, pkg); err != nil {
		t.Fatal(err)
	}
	commits, err := api.Log(context.Background(), 1)
	if err != nil {
		requireRefused(t, err, "log after the swap")
		return
	}
	if len(commits) != 1 || commits[0].Subject != grantedSubject {
		t.Errorf("DIRECTORY SWAP AFTER OPEN: the handle checked for %s answered %v after the swap", pkg, commits)
	}
	status, err := api.Status(context.Background())
	if err != nil {
		requireRefused(t, err, "status after the swap")
		return
	}
	for _, name := range status.Untracked {
		if strings.Contains(name, "VICTIM_ONLY") {
			t.Errorf("DIRECTORY SWAP AFTER OPEN: status read the other repository's work tree: %v", status.Untracked)
		}
	}
}

// Probe 1b: the same swap between two tool calls, each opening the handle afresh,
// is refused or answered by the granted repository: gitFor resolves the name again.
func TestAgentGitFreshOpenAfterTheSwapIsTheLinksTargetAndIsRefused(t *testing.T) {
	srv, pkg, _, outside := swapFixture(t)
	if err := os.Rename(pkg, pkg+".moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, pkg); err != nil {
		t.Fatal(err)
	}
	api, _, err := srv.gitFor(agentCtx(), pkg, false)
	if err != nil {
		requireRefused(t, err, "open at a link to another repository")
		return
	}
	t.Fatalf("a link to a repository outside the grant opened; it answers %q", subject(t, api))
}

// flipper exchanges two names with renameat2(RENAME_EXCHANGE) until stop, so a
// name is the real directory half of the time and a link to the other repository
// the other half. It never runs more than maxFlips times: a broken test must stop.
func flipper(t *testing.T, a, b string, stop *atomic.Bool, flips *atomic.Int64, maxFlips int64) (wait func()) {
	t.Helper()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for !stop.Load() && flips.Load() < maxFlips {
			if err := unix.Renameat2(unix.AT_FDCWD, a, unix.AT_FDCWD, b, unix.RENAME_EXCHANGE); err != nil {
				t.Errorf("exchange: %v", err)
				return
			}
			// Bursts, not a solid wall of flips: a pause now and then lets some calls
			// open the granted directory, so the run tells refusals from answers.
			if flips.Add(1)%16 == 0 {
				time.Sleep(60 * time.Microsecond)
			}
		}
	}()
	return wg.Wait
}

type swapTally struct {
	granted, victim, refused, other atomic.Int64
}

// swapRace runs iterations tool calls (open + one read) on workers goroutines against a
// directory that is being flipped, and tallies where the answers came from. It stops
// early, failing the test, when the test's own deadline is near.
func swapRace(t *testing.T, srv *Server, pkg, other, outside string, iterations, workers int, maxFlips int64, read func(api GitAPI) string) (*swapTally, int64) {
	t.Helper()
	if err := os.Symlink(outside, other); err != nil {
		t.Fatal(err)
	}
	var stop atomic.Bool
	var flips atomic.Int64
	waitFlipper := flipper(t, pkg, other, &stop, &flips, maxFlips)

	budget := time.Now().Add(20 * time.Second)
	if d, ok := t.Deadline(); ok && d.Add(-5*time.Second).Before(budget) {
		budget = d.Add(-5 * time.Second)
	}
	var tally swapTally
	var next atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := next.Add(1)
				if i > int64(iterations) || time.Now().After(budget) || stop.Load() {
					return
				}
				api, _, err := srv.gitFor(agentCtx(), pkg, false)
				if err != nil {
					tally.refused.Add(1)
					continue
				}
				switch got := read(api); {
				case strings.Contains(got, "VICTIM"):
					tally.victim.Add(1)
				case strings.Contains(got, "GRANTED"):
					tally.granted.Add(1)
				case got == "refused":
					tally.refused.Add(1)
				default:
					tally.other.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	stop.Store(true)
	waitFlipper()
	if time.Now().After(budget) {
		t.Errorf("stress run did not finish inside its budget (a broken implementation must fail, not loop)")
	}
	return &tally, flips.Load()
}

func logAnswer(api GitAPI) string {
	commits, err := api.Log(context.Background(), 1)
	switch {
	case err != nil:
		if appCode(err) == "git.repoOutsideWorkspace" {
			return "refused"
		}
		return "error: " + err.Error()
	case len(commits) != 1:
		return "none"
	}
	return commits[0].Subject
}

func statusAnswer(api GitAPI) string {
	status, err := api.Status(context.Background())
	switch {
	case err != nil:
		if appCode(err) == "git.repoOutsideWorkspace" {
			return "refused"
		}
		return "error: " + err.Error()
	}
	for _, name := range status.Untracked {
		if strings.Contains(name, "VICTIM_ONLY") {
			return "VICTIM work tree"
		}
		if strings.Contains(name, "GRANTED_ONLY") {
			return "GRANTED work tree"
		}
	}
	return "other"
}

// Probe 2: the flipper. Zero answers may come from the repository outside the grant over
// a fixed number of tool calls; the other outcomes (refused, granted) are both fine. The
// iteration count is high so that the 3% the unbound handle lost at 400 calls would be
// seen about sixty times.
func TestAgentGitDirectorySwapRaceNeverAnswersFromAnotherRepository(t *testing.T) {
	srv, pkg, other, outside := swapFixture(t)
	tally, flips := swapRace(t, srv, pkg, other, outside, 2000, 8, 400_000, logAnswer)
	t.Logf("log: flips=%d granted=%d refused=%d victim=%d other=%d", flips, tally.granted.Load(), tally.refused.Load(), tally.victim.Load(), tally.other.Load())
	if n := tally.victim.Load(); n > 0 {
		t.Errorf("RACE WON: %d of the calls were answered from a repository outside the grant", n)
	}
	if tally.granted.Load() == 0 {
		t.Errorf("no call was ever answered by the granted repository: the test is not exercising the handle")
	}
}

// The same flipper against status, which reads the work tree and not only the refs:
// it must not show the other repository's files.
func TestAgentGitDirectorySwapRaceNeverReadsAnotherWorkTree(t *testing.T) {
	srv, pkg, other, outside := swapFixture(t)
	tally, flips := swapRace(t, srv, pkg, other, outside, 1000, 8, 400_000, statusAnswer)
	t.Logf("status: flips=%d granted=%d refused=%d victim=%d other=%d", flips, tally.granted.Load(), tally.refused.Load(), tally.victim.Load(), tally.other.Load())
	if n := tally.victim.Load(); n > 0 {
		t.Errorf("RACE WON: %d status calls read the work tree of a repository outside the grant", n)
	}
}
