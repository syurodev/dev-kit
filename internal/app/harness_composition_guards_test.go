package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
	"devkit/internal/harnessconfig"
	"devkit/internal/interaction"
	"devkit/internal/mcp"
	"devkit/internal/permissions"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// AP2: the guards that live in the Tasks and Git handlers, exercised through the
// real harness controls. Real HarnessStore, preferences, workspaces, the tasks
// and git services, the capability gateway, the shared hub, the MCP server and
// SessionHost; only the vendor transport and the person's answers are scripted.
// Every repository is a temporary directory and every remote a local bare
// repository. Nothing here decides what an agent may do: a refusal is the owning
// service's, and each is checked against the repository's refs, the task's
// durable binding and the number of questions put to the person.

// must calls a tool that has to succeed.
func (r *compRig) must(cs *sdk.ClientSession, tool string, args map[string]any) {
	r.t.Helper()
	if res := r.callRaw(cs, tool, args); res.IsError {
		r.t.Fatalf("%s %v was refused: %s", tool, args, resultText(res))
	}
}

// refused calls a tool that has to be refused and returns the refusal.
func (r *compRig) refused(cs *sdk.ClientSession, tool string, args map[string]any) string {
	r.t.Helper()
	res := r.callRaw(cs, tool, args)
	if !res.IsError {
		r.t.Fatalf("%s %v must be refused, got %s", tool, args, resultText(res))
	}
	return resultText(res)
}

// refusedWith is refused plus the code or phrase the owning service answers with.
func (r *compRig) refusedWith(cs *sdk.ClientSession, tool string, args map[string]any, want string) {
	r.t.Helper()
	if got := r.refused(cs, tool, args); !strings.Contains(got, want) {
		r.t.Fatalf("%s %v refused with %q, want it to mention %q", tool, args, got, want)
	}
}

// hold makes the session hold task with branch recorded on it, through the tools
// an agent has, and leaves the work tree on that branch.
func (r *compRig) hold(cs *sdk.ClientSession, taskID, branch string) {
	r.t.Helper()
	r.must(cs, "tasks.claim", map[string]any{"taskId": taskID})
	r.must(cs, "tasks.setFields", map[string]any{"taskId": taskID, "branch": branch})
	r.must(cs, "git.branch.create", map[string]any{"name": branch})
}

// commitFile writes a file in dir and stages and commits it through the tools.
func (r *compRig) commitFile(cs *sdk.ClientSession, dir, name string) {
	r.t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(name+"\n"), 0o600); err != nil {
		r.t.Fatal(err)
	}
	r.must(cs, "git.stage", map[string]any{"paths": []string{name}})
	r.must(cs, "git.commit", map[string]any{"message": "add " + name})
}

// answerKind scripts the person's answer for one kind of question.
func (r *compRig) answerKind(kind interaction.Kind, answer string) {
	r.prompter.mu.Lock()
	defer r.prompter.mu.Unlock()
	if r.prompter.answers == nil {
		r.prompter.answers = map[interaction.Kind]string{}
	}
	r.prompter.answers[kind] = answer
}

// questions are the questions of one kind put to the person so far.
func (r *compRig) questions(kind interaction.Kind) []interaction.Pending {
	r.prompter.mu.Lock()
	defer r.prompter.mu.Unlock()
	var out []interaction.Pending
	for _, p := range r.prompter.prompts {
		if p.Kind == kind {
			out = append(out, p)
		}
	}
	return out
}

func (r *compRig) agentSession() (agenthub.AgentSession, *sdk.ClientSession) {
	r.t.Helper()
	sess, spec := r.create(agenthub.AgentTypeClaude)
	return sess, r.connect(spec)
}

func (r *compRig) listsNone(cs *sdk.ClientSession, groups ...string) {
	r.t.Helper()
	for _, n := range r.listed(cs) {
		for _, g := range groups {
			if harness.Group(n) == g {
				r.t.Fatalf("%s is listed although group %q is off", n, g)
			}
		}
	}
}

// A group that is off is hidden from discovery, and a direct (cached) call is
// refused before the tool's handler or any of its guards runs: nothing reaches
// the repository, the task or the person.
func TestHiddenGitAndTaskGroupsAreRefusedBeforeTheirHandlers(t *testing.T) {
	r := newWorkRig(t, compOptions{})
	bare := r.work.repo(r.root)
	task := r.task("hidden")
	r.workspaceOff("git", "tasks")
	_, cs := r.agentSession()
	r.listsNone(cs, "git", "tasks")

	refs, state := r.work.remoteRefs(bare), r.work.localState(r.root)
	handled := r.gate.handled.Load()
	for _, call := range []struct {
		tool string
		args map[string]any
	}{
		{"tasks.claim", map[string]any{"taskId": task.ID}},
		{"tasks.setFields", map[string]any{"taskId": task.ID, "branch": "feature/x"}},
		{"git.branch.create", map[string]any{"name": "feature/x"}},
		{"git.push", map[string]any{"branch": "main"}},
	} {
		r.refusedWith(cs, call.tool, call.args, harness.CodeGroupOff)
	}
	if r.gate.handled.Load() != handled {
		t.Fatal("a hidden group's call must stop before the gateway handler")
	}
	if got := r.taskNow(task.ID); got.SessionID != "" || got.Branch != "" {
		t.Fatalf("a refused claim left a binding: %+v", got)
	}
	if r.work.remoteRefs(bare) != refs || r.work.localState(r.root) != state {
		t.Fatal("a hidden group's call changed a repository")
	}
	if n := len(kinds(r.prompter)); n != 0 {
		t.Fatalf("a hidden group's call asked the person %d times", n)
	}
}

// Enabling the groups through the public proposal changes which tools are listed
// and nothing else: the agent still has to hold a task to push, a claim is not
// manufactured, and the host's own operation decides the outcome.
func TestEnablingTheGroupsByAProposalMakesNoClaimAndAddsNoPushRight(t *testing.T) {
	r := newWorkRig(t, compOptions{controls: true})
	r.prompter.answers = answers("session")
	bare := r.work.repo(r.root)
	task := r.task("unclaimed")
	r.workspaceOff("git", "tasks")
	_, cs := r.agentSession()
	r.refusedWith(cs, "git.push", map[string]any{"branch": "feature/x"}, harness.CodeGroupOff)

	if out := decode[harnessconfig.RequestResult](t, r.callRaw(cs, harness.ProposeTool, map[string]any{"groups": []string{"git", "tasks"}})).Outcome; out == nil || !out.Live {
		t.Fatalf("setup: expected a live enable: %+v", out)
	}
	if !hasGroup(r.listed(cs), "git") || !hasGroup(r.listed(cs), "tasks") {
		t.Fatal("the enabled groups must be listed")
	}
	if got := r.taskNow(task.ID); got.SessionID != "" || got.Branch != "" {
		t.Fatalf("enabling a group manufactured a claim: %+v", got)
	}

	// A commit on a branch the agent made, never recorded on a task it holds.
	r.must(cs, "git.branch.create", map[string]any{"name": "feature/x"})
	r.commitFile(cs, r.root, "x.txt")
	refs, local := r.work.remoteRefs(bare), r.work.localState(r.root)
	r.refusedWith(cs, "git.push", map[string]any{"branch": "feature/x"}, "not a branch recorded on a task you hold")
	r.refusedWith(cs, "git.push", map[string]any{}, "not a branch recorded on a task you hold") // the current branch
	if r.work.remoteRefs(bare) != refs {
		t.Fatalf("an unowned push reached the remote:\n%s", r.work.remoteRefs(bare))
	}
	if r.work.localState(r.root) != local {
		t.Fatal("a refused push changed the local repository")
	}
	if got := r.taskNow(task.ID); got.SessionID != "" || got.Branch != "" {
		t.Fatalf("a refused push wrote a binding: %+v", got)
	}
}

// The grant check is the gateway's, not the group's: with push not granted, the
// enabled git group still lists the tool and every other git tool works, and the
// push is refused before its handler. The task's recorded branch is no
// substitute for the grant.
func TestAnEnabledGitGroupStillNeedsTheGrantsOfItsTools(t *testing.T) {
	grants := []permissions.Grant{
		{SubjectID: "builtin.workspace", Capability: "ws:read", Scope: "workspace:list"},
		{SubjectID: "builtin.harness", Capability: "harness:read", Scope: "profile:self"},
		{SubjectID: "builtin.harness", Capability: "harness:propose", Scope: "profile:self"},
		{SubjectID: "builtin.tasks", Capability: "tasks:write", Scope: "task:*"},
		{SubjectID: "builtin.tasks", Capability: "tasks:read", Scope: "task:*"},
		{SubjectID: "builtin.git", Capability: "git:read", Scope: "repo:*"},
		{SubjectID: "builtin.git", Capability: "git:write", Scope: "repo:*"},
	}
	r := newWorkRig(t, compOptions{controls: true, grants: grants})
	r.prompter.answers = answers("session")
	bare := r.work.repo(r.root)
	task := r.task("no push grant")
	r.workspaceOff("git")
	_, cs := r.agentSession()
	if out := decode[harnessconfig.RequestResult](t, r.callRaw(cs, harness.ProposeTool, map[string]any{"groups": []string{"git"}})).Outcome; out == nil || !out.Live {
		t.Fatalf("setup: expected a live enable: %+v", out)
	}
	r.hold(cs, task.ID, "feature/grant")
	r.commitFile(cs, r.root, "g.txt")

	refs := r.work.remoteRefs(bare)
	handled := r.gate.handled.Load()
	r.refused(cs, "git.push", map[string]any{"branch": "feature/grant"})
	if r.gate.handled.Load() != handled {
		t.Fatal("an ungranted push must be refused by the gateway before its handler")
	}
	if r.work.remoteRefs(bare) != refs {
		t.Fatal("an ungranted push reached the remote")
	}
}

// Push is bound to the current permitted branch recorded on a task the caller
// holds. The default branch, an unowned branch, another session's branch, a
// released task and a missing remote all refuse with the remote unchanged; the
// owner's push, with everything correct, lands. The positive control is what
// makes each refusal mean something.
func TestPushNeedsABranchRecordedOnATaskTheCallerHolds(t *testing.T) {
	r := newWorkRig(t, compOptions{})
	bare := r.work.repo(r.root)
	mine, theirs := r.task("mine"), r.task("theirs")
	sess1, cs1 := r.agentSession()
	_, cs2 := r.agentSession()

	// The default branch is ahead of its remote so that a push would be visible.
	if err := os.WriteFile(filepath.Join(r.root, "ahead.txt"), []byte("ahead\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.work.run(r.root, "add", "ahead.txt")
	r.work.run(r.root, "commit", "-m", "ahead")
	r.work.run(r.root, "branch", "feature/unowned")

	r.hold(cs2, theirs.ID, "feature/theirs")
	r.commitFile(cs2, r.root, "theirs.txt")
	r.hold(cs1, mine.ID, "feature/mine")
	r.commitFile(cs1, r.root, "mine.txt")
	remote := r.work.remoteRefs(bare)

	check := func(name string, args map[string]any, why string) {
		t.Helper()
		r.refusedWith(cs1, "git.push", args, why)
		if got := r.work.remoteRefs(bare); got != remote {
			t.Fatalf("%s: the remote changed:\n%s\nwas:\n%s", name, got, remote)
		}
		if got := r.taskNow(mine.ID); got.SessionID != sess1.ID || got.Branch != "feature/mine" {
			t.Fatalf("%s: the task binding changed: %+v", name, got)
		}
	}
	check("default branch", map[string]any{"branch": "main"}, "default branch")
	check("unowned branch", map[string]any{"branch": "feature/unowned"}, "not a branch recorded on a task you hold")
	check("another session's branch", map[string]any{"branch": "feature/theirs"}, "not a branch recorded on a task you hold")
	check("an unrelated name", map[string]any{"branch": "feature/nothing"}, "not a branch recorded on a task you hold")
	check("a missing remote", map[string]any{"branch": "feature/mine", "remote": "nowhere"}, "")

	// The task records the default branch: it is still never pushed.
	other := r.task("names main")
	r.must(cs1, "tasks.claim", map[string]any{"taskId": other.ID})
	r.must(cs1, "tasks.setFields", map[string]any{"taskId": other.ID, "branch": "main"})
	check("a task naming the default branch", map[string]any{"branch": "main"}, "default branch")

	// Positive control: the owner, the recorded branch and the local remote.
	r.must(cs1, "git.push", map[string]any{"branch": "feature/mine"})
	if got := r.work.remoteRefs(bare); !strings.Contains(got, "refs/heads/feature/mine ") ||
		strings.Contains(got, "feature/theirs") || strings.Contains(got, "feature/unowned") {
		t.Fatalf("the owner's push must land and only it:\n%s", got)
	}
	if want := r.work.run(r.root, "rev-parse", "feature/mine"); !strings.Contains(r.work.remoteRefs(bare), "refs/heads/feature/mine "+want) {
		t.Fatalf("the remote did not receive the branch tip %s:\n%s", want, r.work.remoteRefs(bare))
	}

	// The permission lives on the task: once the claim is released nothing is left.
	r.work.run(r.root, "checkout", "feature/mine")
	r.commitFile(cs1, r.root, "later.txt")
	r.must(cs1, "tasks.release", map[string]any{"taskId": mine.ID})
	remote = r.work.remoteRefs(bare)
	r.refusedWith(cs1, "git.push", map[string]any{"branch": "feature/mine"}, "not a branch recorded on a task you hold")
	if r.work.remoteRefs(bare) != remote {
		t.Fatal("a released task still allowed a push")
	}
	if got := r.taskNow(mine.ID); got.SessionID != "" || got.Branch != "feature/mine" {
		t.Fatalf("release must clear only the holder: %+v", got)
	}
}

// Another live session's claim is not taken, dropped or re-aimed, and a refused
// attempt does not give its binding away. Release-then-claim used to take a live
// session's task, and with it the right to push the branch recorded on it.
func TestATaskHeldByAnotherLiveSessionIsNotTakenDroppedOrReaimed(t *testing.T) {
	r := newWorkRig(t, compOptions{})
	bare := r.work.repo(r.root)
	task := r.task("held")
	sess1, cs1 := r.agentSession()
	_, cs2 := r.agentSession()
	r.hold(cs1, task.ID, "feature/held")
	r.commitFile(cs1, r.root, "held.txt")
	remote := r.work.remoteRefs(bare)

	unchanged := func(step string) {
		t.Helper()
		if got := r.taskNow(task.ID); got.SessionID != sess1.ID || got.Branch != "feature/held" {
			t.Fatalf("%s changed the binding: %+v", step, got)
		}
	}
	r.refused(cs2, "tasks.claim", map[string]any{"taskId": task.ID})
	unchanged("a refused claim")
	r.refused(cs2, "tasks.setFields", map[string]any{"taskId": task.ID, "branch": "feature/hijack"})
	unchanged("a refused re-aim")
	r.refused(cs2, "tasks.release", map[string]any{"taskId": task.ID})
	unchanged("a refused release")

	// With the claim still the first session's, the second cannot push its branch.
	r.refusedWith(cs2, "git.push", map[string]any{"branch": "feature/held"}, "not a branch recorded on a task you hold")
	if r.work.remoteRefs(bare) != remote {
		t.Fatal("a session that does not hold the task pushed its branch")
	}
	// The holder still can.
	r.must(cs1, "git.push", map[string]any{"branch": "feature/held"})
}

// A guarded branch keeps asking through the shared hub whatever tool reaches it:
// commit, amend and push each put one question, a denial changes no ref and no
// file, a denial is not remembered, and an "always" answer is not kept (the
// environment decides, not a stored grant). A guard whose tag is unknown asks too;
// a tag that does not require confirmation does not.
func TestAGuardedBranchAsksForEveryMutationAndADenialChangesNothing(t *testing.T) {
	r := newWorkRig(t, compOptions{})
	bare := r.work.repo(r.root)
	r.guard(r.root, "release/*", "prod")
	r.guard(r.root, "ops/*", "a-tag-nobody-defined")
	r.guard(r.root, "dev/*", "dev")
	task := r.task("guarded")
	_, cs := r.agentSession()
	r.hold(cs, task.ID, "release/1.0")
	if err := os.WriteFile(filepath.Join(r.root, "a.txt"), []byte("a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.must(cs, "git.stage", map[string]any{"paths": []string{"a.txt"}})

	// A denial: nothing is committed, the staged file stays staged, and asking
	// again asks again (a denial is final for that call, not remembered).
	r.answerKind(interaction.KindGit, "deny")
	before := r.work.localState(r.root)
	for i := 1; i <= 2; i++ {
		r.refused(cs, "git.commit", map[string]any{"message": "denied"})
		if r.work.localState(r.root) != before {
			t.Fatalf("attempt %d: a denied commit changed the repository", i)
		}
		if n := len(r.questions(interaction.KindGit)); n != i {
			t.Fatalf("attempt %d: %d questions, want one per attempt", i, n)
		}
	}
	q := r.questions(interaction.KindGit)[0]
	if q.Env != "prod" || q.Target.Branch != "release/1.0" || !strings.Contains(strings.ToLower(q.Target.Action), "commit") {
		t.Fatalf("the question must name the environment, branch and action: %+v", q)
	}
	if q.AllowAlwaysOffered {
		t.Fatal("an environment-confirmed question must not offer to be remembered")
	}

	// "Always" is one allow and is never kept.
	r.answerKind(interaction.KindGit, "allow_always")
	asked := len(r.questions(interaction.KindGit))
	r.must(cs, "git.commit", map[string]any{"message": "first"})
	if err := os.WriteFile(filepath.Join(r.root, "b.txt"), []byte("b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.must(cs, "git.stage", map[string]any{"paths": []string{"b.txt"}})
	r.must(cs, "git.commit", map[string]any{"message": "second"})
	if n := len(r.questions(interaction.KindGit)) - asked; n != 2 {
		t.Fatalf("an always answer must not be kept: %d questions for two commits", n)
	}

	// Amend asks and a denial leaves the tip alone; the same for the push.
	tip := r.work.run(r.root, "rev-parse", "HEAD")
	r.answerKind(interaction.KindGit, "deny")
	asked = len(r.questions(interaction.KindGit))
	r.refused(cs, "git.amend", map[string]any{"message": "rewritten"})
	if got := r.work.run(r.root, "rev-parse", "HEAD"); got != tip {
		t.Fatalf("a denied amend moved HEAD: %s -> %s", tip, got)
	}
	remote := r.work.remoteRefs(bare)
	r.refused(cs, "git.push", map[string]any{"branch": "release/1.0"})
	if r.work.remoteRefs(bare) != remote {
		t.Fatal("a denied push reached the remote")
	}
	if n := len(r.questions(interaction.KindGit)) - asked; n != 2 {
		t.Fatalf("amend and push must each ask once: %d questions", n)
	}

	// The same push, allowed: the guard is a confirmation, not a veto.
	r.answerKind(interaction.KindGit, "allow")
	r.must(cs, "git.push", map[string]any{"branch": "release/1.0"})
	if want := r.work.run(r.root, "rev-parse", "release/1.0"); !strings.Contains(r.work.remoteRefs(bare), "refs/heads/release/1.0 "+want) {
		t.Fatalf("an allowed guarded push must land:\n%s", r.work.remoteRefs(bare))
	}

	// An unknown tag fails closed and asks; a tag that does not require
	// confirmation does not.
	r.work.run(r.root, "checkout", "-b", "ops/1")
	if err := os.WriteFile(filepath.Join(r.root, "c.txt"), []byte("c\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.must(cs, "git.stage", map[string]any{"paths": []string{"c.txt"}})
	r.answerKind(interaction.KindGit, "deny")
	before = r.work.localState(r.root)
	asked = len(r.questions(interaction.KindGit))
	r.refused(cs, "git.commit", map[string]any{"message": "ops"})
	if n := len(r.questions(interaction.KindGit)) - asked; n != 1 || r.work.localState(r.root) != before {
		t.Fatalf("an unknown tag must ask and a denial must change nothing (%d questions)", n)
	}
	r.work.run(r.root, "checkout", "-b", "dev/1")
	asked = len(r.questions(interaction.KindGit))
	r.must(cs, "git.commit", map[string]any{"message": "dev"})
	if n := len(r.questions(interaction.KindGit)) - asked; n != 0 {
		t.Fatalf("a tag that needs no confirmation must not ask: %d questions", n)
	}
}

// A repository outside the workspace is refused before any repository operation,
// however it is named: an absolute path, a relative escape, a linked worktree of
// the workspace's repository placed elsewhere, or a link inside the workspace
// that points out of it. The outside repository's refs and files never move.
func TestARepositoryOutsideTheWorkspaceIsRefusedHoweverItIsNamed(t *testing.T) {
	r := newWorkRig(t, compOptions{})
	r.work.repo(r.root)
	outside := t.TempDir()
	outsideBare := r.work.repo(outside)
	linked := filepath.Join(t.TempDir(), "wt")
	r.work.run(r.root, "worktree", "add", "-b", "elsewhere", linked)
	if err := os.Mkdir(filepath.Join(outside, "pkg"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(r.root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(linked, filepath.Join(r.root, "linked")); err != nil {
		t.Fatal(err)
	}
	task := r.task("scope")
	_, cs := r.agentSession()
	r.hold(cs, task.ID, "feature/scope")

	for _, named := range []struct{ name, repo string }{
		{"absolute path", outside},
		{"relative escape", "../" + filepath.Base(outside)},
		{"linked worktree elsewhere", linked},
		{"link to another repository", filepath.Join(r.root, "link")},
		{"link to a linked worktree", filepath.Join(r.root, "linked")},
		{"folder behind a link", filepath.Join(r.root, "link", "pkg")},
	} {
		outState, outRemote := r.work.localState(outside), r.work.remoteRefs(outsideBare)
		linkedState, rootState := r.work.localState(linked), r.work.localState(r.root)
		for _, call := range []struct {
			tool string
			args map[string]any
		}{
			{"git.status", map[string]any{"repo": named.repo}},
			{"git.branch.create", map[string]any{"repo": named.repo, "name": "evil"}},
			{"git.checkout", map[string]any{"repo": named.repo, "name": "main"}},
			{"git.stash", map[string]any{"repo": named.repo}},
			{"git.push", map[string]any{"repo": named.repo, "branch": "feature/scope"}},
		} {
			got := r.refused(cs, call.tool, call.args)
			if !strings.Contains(got, "git.repoOutsideWorkspace") && !strings.Contains(got, "git.repoNotGranted") {
				t.Fatalf("%s via %s: refused for the wrong reason: %s", call.tool, named.name, got)
			}
		}
		if r.work.localState(outside) != outState || r.work.remoteRefs(outsideBare) != outRemote {
			t.Fatalf("%s: the outside repository moved:\n%s", named.name, r.work.localState(outside))
		}
		if r.work.localState(linked) != linkedState || r.work.localState(r.root) != rootState {
			t.Fatalf("%s: a refused call moved another work tree", named.name)
		}
	}
}

// A second folder of the workspace that the session was not granted is readable
// and not writable. Enabling the groups does not widen the granted paths, and
// the same session granted that folder at creation writes there, which is what
// shows the refusals are the grant and not a broken fixture.
func TestAWorkspaceFolderTheSessionMayNotWriteIsReadOnly(t *testing.T) {
	r := newWorkRig(t, compOptions{controls: true})
	// Saved to the workspace, so the session granted the folder later launches with
	// the group on as well.
	r.prompter.answers = answers("workspace")
	r.work.repo(r.root)
	sibling := t.TempDir()
	siblingBare := r.work.repo(sibling)
	if _, _, err := r.ws.UpdateWorkspace(r.wsID, "one", []string{r.root, sibling}); err != nil {
		t.Fatal(err)
	}
	r.workspaceOff("git")
	_, cs := r.agentSession()
	if out := decode[harnessconfig.RequestResult](t, r.callRaw(cs, harness.ProposeTool, map[string]any{"groups": []string{"git"}})).Outcome; out == nil || !out.Live {
		t.Fatalf("setup: expected a live enable: %+v", out)
	}
	name := filepath.Base(sibling)

	r.must(cs, "git.status", map[string]any{"repo": name})
	r.must(cs, "git.log", map[string]any{"repo": name})
	state, remote := r.work.localState(sibling), r.work.remoteRefs(siblingBare)
	for _, call := range []struct {
		tool string
		args map[string]any
	}{
		{"git.branch.create", map[string]any{"repo": name, "name": "feature/ro"}},
		{"git.stash", map[string]any{"repo": name}},
		{"git.checkout", map[string]any{"repo": name, "name": "main"}},
		{"git.commit", map[string]any{"repo": name, "message": "no"}},
		{"git.push", map[string]any{"repo": name, "branch": "main"}},
	} {
		r.refusedWith(cs, call.tool, call.args, "git.repoNotGranted")
	}
	if r.work.localState(sibling) != state || r.work.remoteRefs(siblingBare) != remote {
		t.Fatal("a read-only folder was written")
	}

	// The same folder, granted at creation: the write is permitted and lands.
	granted, err := r.agents.CreateSession(r.ui(), agenthub.CreateInput{AgentType: agenthub.AgentTypeClaude, Cwd: r.root,
		WorkspaceID: r.wsID, ExtraReadWritePaths: []string{sibling}})
	if err != nil {
		t.Fatal(err)
	}
	gcs := r.connect(r.vendors[agenthub.AgentTypeClaude].LastSpec())
	r.must(gcs, "git.branch.create", map[string]any{"repo": name, "name": "feature/rw"})
	if r.work.run(sibling, "symbolic-ref", "--short", "HEAD") != "feature/rw" {
		t.Fatalf("session %s was granted %s and its branch was not created", granted.ID, sibling)
	}
}

// A stored profile that cannot be read is not an empty valid profile: a new session
// is refused (it is never opened unfiltered), the vendor is never started, and no
// endpoint socket is left behind.
func TestACorruptStoredProfileRefusesANewSessionAndNeverServesUnfiltered(t *testing.T) {
	r := newCompRig(t, compOptions{})
	r.workspaceOff("vault")
	if _, err := r.db.Exec(`UPDATE workspace_harness_profiles SET body='{bad' WHERE workspace_id=?`, r.wsID); err != nil {
		t.Fatal(err)
	}
	_, err := r.agents.CreateSession(r.ui(), agenthub.CreateInput{AgentType: agenthub.AgentTypeClaude, Cwd: r.root, WorkspaceID: r.wsID})
	if !errors.Is(err, agenthub.ErrHarnessUnavailable) {
		t.Fatalf("a corrupt stored profile must refuse the session with recovery guidance, got %v", err)
	}
	if n := r.vendors[agenthub.AgentTypeClaude].opens.Load(); n != 0 {
		t.Fatalf("the vendor was started %d times for a session that could not be configured", n)
	}
	if socks, _ := filepath.Glob(filepath.Join(r.sockDir, "*.sock")); len(socks) != 0 {
		t.Fatalf("a refused session left endpoints behind: %v", socks)
	}
}

// beforeOpen runs a hook just before the Git module is opened at a path, which is
// the gap between the server's scope check and the open.
type beforeOpen struct {
	mcp.GitRepos
	mu   sync.Mutex
	hook func()
}

func (b *beforeOpen) At(ctx context.Context, path string) (mcp.GitAPI, error) {
	b.mu.Lock()
	hook := b.hook
	b.mu.Unlock()
	if hook != nil {
		hook()
	}
	return b.GitRepos.At(ctx, path)
}

// A link that is pointed at another repository after the scope check, and before
// the repository is opened, does not move the operation: Git is opened at the
// path that was checked, so the branch is made in the workspace's repository and
// the outside one is untouched.
func TestALinkRepointedBetweenTheCheckAndTheOpenDoesNotMoveTheOperation(t *testing.T) {
	var gate *beforeOpen
	r := newWorkRig(t, compOptions{wrapGitRepos: func(inner mcp.GitRepos) mcp.GitRepos {
		gate = &beforeOpen{GitRepos: inner}
		return gate
	}})
	r.work.repo(r.root)
	outside := t.TempDir()
	outsideBare := r.work.repo(outside)
	// A repository of its own inside the workspace folder, so the alias is opened
	// as itself rather than as the folder around it.
	sub := filepath.Join(r.root, "sub")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	r.work.repo(sub)
	alias := filepath.Join(r.root, "alias")
	if err := os.Symlink(sub, alias); err != nil {
		t.Fatal(err)
	}
	gate.hook = func() {
		_ = os.Remove(alias)
		if err := os.Symlink(outside, alias); err != nil {
			t.Error(err)
		}
	}
	_, cs := r.agentSession()
	outState, outRemote := r.work.localState(outside), r.work.remoteRefs(outsideBare)

	res := r.callRaw(cs, "git.branch.create", map[string]any{"repo": alias, "name": "evil"})
	if r.work.localState(outside) != outState || r.work.remoteRefs(outsideBare) != outRemote {
		t.Fatalf("the repository outside the workspace was written (%s):\n%s", resultText(res), r.work.localState(outside))
	}
	if res.IsError {
		t.Fatalf("the operation on the checked path should have run there: %s", resultText(res))
	}
	if got := r.work.run(sub, "symbolic-ref", "--short", "HEAD"); got != "evil" {
		t.Fatalf("the branch was not made in the repository that was checked: HEAD is %s", got)
	}
}
