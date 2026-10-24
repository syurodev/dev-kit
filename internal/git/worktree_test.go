package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorktreeAddCreatesNewBranch(t *testing.T) {
	svc, _ := newRepo(t)
	ctx := context.Background()

	wt, err := svc.WorktreeAdd(ctx, WorktreeAddInput{Branch: "feature/worktree-test"})
	if err != nil {
		t.Fatal(err)
	}

	if wt.Branch != "feature/worktree-test" {
		t.Fatalf("branch = %q", wt.Branch)
	}
	if !strings.HasSuffix(wt.Path, ".superpowers/.worktrees/feature-worktree-test") {
		t.Fatalf("path = %q", wt.Path)
	}
	if wt.Head == "" {
		t.Fatal("head is empty")
	}
	if wt.Dirty {
		t.Fatal("new worktree should not be dirty")
	}

	// Verify the worktree exists on disk.
	if _, err := os.Stat(wt.Path); err != nil {
		t.Fatalf("worktree path does not exist: %v", err)
	}
}

func TestWorktreeAddAttachesExistingBranch(t *testing.T) {
	svc, _ := newRepo(t)
	ctx := context.Background()

	// Create a branch first.
	if _, err := svc.CreateBranch(ctx, "existing-branch", ""); err != nil {
		t.Fatal(err)
	}

	// Now add a worktree for that existing branch.
	wt, err := svc.WorktreeAdd(ctx, WorktreeAddInput{Branch: "existing-branch"})
	if err != nil {
		t.Fatal(err)
	}

	if wt.Branch != "existing-branch" {
		t.Fatalf("branch = %q", wt.Branch)
	}
	if !strings.HasSuffix(wt.Path, ".superpowers/.worktrees/existing-branch") {
		t.Fatalf("path = %q", wt.Path)
	}
}

func TestWorktreeAddRefusesDuplicate(t *testing.T) {
	svc, _ := newRepo(t)
	ctx := context.Background()

	// First creation.
	_, err := svc.WorktreeAdd(ctx, WorktreeAddInput{Branch: "duplicate-test"})
	if err != nil {
		t.Fatal(err)
	}

	// Second attempt should fail.
	_, err = svc.WorktreeAdd(ctx, WorktreeAddInput{Branch: "duplicate-test"})
	if !errors.Is(err, ErrBranchCheckedOut) {
		t.Fatalf("second add = %v, want ErrBranchCheckedOut", err)
	}
}

func TestWorktreeAddRefusesSlugCollision(t *testing.T) {
	svc, _ := newRepo(t)
	if _, err := svc.WorktreeAdd(context.Background(), WorktreeAddInput{Branch: "feature/x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.WorktreeAdd(context.Background(), WorktreeAddInput{Branch: "feature-x"}); !errors.Is(err, ErrWorktreeExists) {
		t.Fatalf("different branch with same slug = %v, want ErrWorktreeExists", err)
	}
}

func TestWorktreeAddRefusesBranchCheckedOutElsewhere(t *testing.T) {
	svc, _ := newRepo(t)
	ctx := context.Background()

	// Create worktree for branch "shared".
	_, err := svc.WorktreeAdd(ctx, WorktreeAddInput{Branch: "shared"})
	if err != nil {
		t.Fatal(err)
	}

	// Try to create another worktree for the same branch.
	_, err = svc.WorktreeAdd(ctx, WorktreeAddInput{Branch: "shared"})
	if !errors.Is(err, ErrBranchCheckedOut) {
		t.Fatalf("second add for same branch = %v, want ErrBranchCheckedOut", err)
	}
}

func TestWorktreeListIncludesNewWorktree(t *testing.T) {
	svc, _ := newRepo(t)
	ctx := context.Background()

	wt, err := svc.WorktreeAdd(ctx, WorktreeAddInput{Branch: "list-test"})
	if err != nil {
		t.Fatal(err)
	}

	worktrees, err := svc.Worktrees(ctx)
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, w := range worktrees {
		if w.Path == wt.Path {
			found = true
			if w.Branch != "list-test" {
				t.Fatalf("branch = %q", w.Branch)
			}
			if w.Head != wt.Head {
				t.Fatalf("head = %q, want %q", w.Head, wt.Head)
			}
			break
		}
	}
	if !found {
		t.Fatalf("new worktree not listed: %+v", worktrees)
	}
}

func TestWorktreeRemoveRefusesDirty(t *testing.T) {
	svc, _ := newRepo(t)
	ctx := context.Background()

	wt, err := svc.WorktreeAdd(ctx, WorktreeAddInput{Branch: "dirty-worktree"})
	if err != nil {
		t.Fatal(err)
	}

	// Make a change in the worktree.
	write(t, wt.Path, "dirty.txt", "changed\n")

	err = svc.WorktreeRemove(ctx, "dirty-worktree")
	if !errors.Is(err, ErrWorktreeDirty) {
		t.Fatalf("remove dirty = %v, want ErrWorktreeDirty", err)
	}
}

func TestWorktreeRemoveRemovesClean(t *testing.T) {
	svc, _ := newRepo(t)
	ctx := context.Background()

	wt, err := svc.WorktreeAdd(ctx, WorktreeAddInput{Branch: "clean-worktree"})
	if err != nil {
		t.Fatal(err)
	}

	err = svc.WorktreeRemove(ctx, "clean-worktree")
	if err != nil {
		t.Fatal(err)
	}

	// Verify it's gone.
	worktrees, err := svc.Worktrees(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range worktrees {
		if w.Path == wt.Path {
			t.Fatal("removed worktree still listed")
		}
	}
}

func TestWorktreeRemoveRefusesMainWorktree(t *testing.T) {
	svc, dir := newRepo(t)
	ctx := context.Background()

	// Try to remove the main worktree by its path.
	err := svc.WorktreeRemove(ctx, dir)
	if !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("remove main worktree = %v, want ErrNotAllowed", err)
	}
}

func TestWorktreeRemoveByPath(t *testing.T) {
	svc, _ := newRepo(t)
	ctx := context.Background()

	wt, err := svc.WorktreeAdd(ctx, WorktreeAddInput{Branch: "remove-by-path"})
	if err != nil {
		t.Fatal(err)
	}

	err = svc.WorktreeRemove(ctx, wt.Path)
	if err != nil {
		t.Fatal(err)
	}

	worktrees, err := svc.Worktrees(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range worktrees {
		if w.Path == wt.Path {
			t.Fatal("removed worktree still listed")
		}
	}
}

func TestWorktreeRemoveNotFound(t *testing.T) {
	svc, _ := newRepo(t)
	ctx := context.Background()

	err := svc.WorktreeRemove(ctx, "nonexistent")
	if !errors.Is(err, ErrWorktreeNotFound) {
		t.Fatalf("remove nonexistent = %v, want ErrWorktreeNotFound", err)
	}
}

func TestWorktreePrune(t *testing.T) {
	svc, _ := newRepo(t)
	ctx := context.Background()

	// Just verify it runs without error.
	pruned, err := svc.worktreePrune(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = pruned // may be empty
}

func TestWorktreeDirSlugs(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"feature/test", "feature-test"},
		{"feature\\test", "feature-test"},
		{"feature:test", "feature-test"},
		{"feature?test", "feature-test"},
		{"feature*test", "feature-test"},
		{"feature\"test", "feature-test"},
		{"feature<test", "feature-test"},
		{"feature>test", "feature-test"},
		{"feature|test", "feature-test"},
		{"  feature  ", "feature"},
		{"", "worktree"},
	}
	for _, tc := range tests {
		got := worktreeDir(tc.input)
		if got != tc.expected {
			t.Errorf("worktreeDir(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestValidateWorktreeName(t *testing.T) {
	tests := []struct {
		input    string
		shouldOK bool
	}{
		{"valid-name", true},
		{"feature/test", true},
		{"", false},
		{"../escape", false},
		{"/absolute", false},
	}
	for _, tc := range tests {
		err := validateWorktreeName(tc.input)
		if tc.shouldOK && err != nil {
			t.Errorf("validateWorktreeName(%q) = %v, want nil", tc.input, err)
		}
		if !tc.shouldOK && err == nil {
			t.Errorf("validateWorktreeName(%q) = nil, want error", tc.input)
		}
	}
}

func TestWorktreeOutsideRepoRefused(t *testing.T) {
	svc, dir := newRepo(t)
	ctx := context.Background()

	// The worktree path is always under the repo, so this test verifies
	// the containment check logic by checking that worktreeRoot returns
	// the correct path.
	root, err := svc.worktreeRoot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if root != dir {
		t.Fatalf("worktreeRoot = %q, want %q", root, dir)
	}
}

func TestWorktreeRefusesSymlinkedManagedDirectory(t *testing.T) {
	svc, dir := newRepo(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, ".superpowers")); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.WorktreeAdd(context.Background(), WorktreeAddInput{Branch: "escape-test"}); !errors.Is(err, ErrWorktreeOutside) {
		t.Fatalf("add through symlink = %v, want ErrWorktreeOutside", err)
	}
	if _, err := os.Stat(filepath.Join(outside, ".worktrees")); !os.IsNotExist(err) {
		t.Fatalf("outside worktree directory was created: %v", err)
	}
}

func TestWorktreeRemoveRefusesExternalWorktree(t *testing.T) {
	svc, _ := newRepo(t)
	ctx := context.Background()
	external := filepath.Join(t.TempDir(), "external")
	if _, err := svc.runner.run(ctx, nil, "worktree", "add", external, "-b", "outside"); err != nil {
		t.Fatal(err)
	}
	if err := svc.WorktreeRemove(ctx, "outside"); !errors.Is(err, ErrWorktreeOutside) {
		t.Fatalf("remove external worktree = %v, want ErrWorktreeOutside", err)
	}
	if _, err := os.Stat(external); err != nil {
		t.Fatalf("external worktree was removed: %v", err)
	}
}

func TestWorktreeAddRefusesInvalidBranchPath(t *testing.T) {
	svc, _ := newRepo(t)
	if _, err := svc.WorktreeAdd(context.Background(), WorktreeAddInput{Branch: "feature/../../outside"}); !errors.Is(err, ErrWorktreeOutside) {
		t.Fatalf("add invalid branch = %v, want ErrWorktreeOutside", err)
	}
}

func TestWorktreeOperationsUseBranchGuard(t *testing.T) {
	svc, dir := newRepo(t)
	svc.SetGuards(guardsFor{dir: {{Pattern: "feature/*", Env: "prod"}}})
	svc.SetEnvs(stubEnvs{"prod": true})
	refused := &stubConfirm{allow: false}
	svc.SetConfirmer(refused)
	if _, err := svc.WorktreeAdd(context.Background(), WorktreeAddInput{Branch: "feature/guarded"}); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("guarded add = %v, want ErrNotAllowed", err)
	}
	if refused.asked != 1 {
		t.Fatalf("asked %d times, want once", refused.asked)
	}
	if _, err := os.Stat(filepath.Join(dir, ".superpowers")); !os.IsNotExist(err) {
		t.Fatalf("refused add created a worktree directory: %v", err)
	}
}

func TestWorktreeStatusAfterWrite(t *testing.T) {
	svc, _ := newRepo(t)
	ctx := context.Background()

	wt, err := svc.WorktreeAdd(ctx, WorktreeAddInput{Branch: "status-test"})
	if err != nil {
		t.Fatal(err)
	}

	// Write a file in the worktree.
	write(t, wt.Path, "new.txt", "content\n")

	worktrees, err := svc.Worktrees(ctx)
	if err != nil {
		t.Fatal(err)
	}

	var found *Worktree
	for i := range worktrees {
		if worktrees[i].Path == wt.Path {
			found = &worktrees[i]
			break
		}
	}
	if found == nil {
		t.Fatal("worktree not found in list")
	}
	if !found.Dirty {
		t.Fatal("worktree should be dirty after write")
	}
}

func TestWorktreeBaseParameter(t *testing.T) {
	svc, dir := newRepo(t)
	ctx := context.Background()

	// Create a commit to branch from.
	write(t, dir, "base.txt", "base\n")
	if err := svc.Stage(ctx, []string{"base.txt"}); err != nil {
		t.Fatal(err)
	}
	baseCommit, err := svc.Commit(ctx, CommitInput{Message: "base commit"})
	if err != nil {
		t.Fatal(err)
	}

	// Create worktree from that specific commit.
	wt, err := svc.WorktreeAdd(ctx, WorktreeAddInput{
		Branch: "from-base",
		Base:   baseCommit.SHA,
	})
	if err != nil {
		t.Fatal(err)
	}

	if wt.Branch != "from-base" {
		t.Fatalf("branch = %q", wt.Branch)
	}
	if wt.Head != baseCommit.SHA {
		t.Fatalf("head = %q, want %q", wt.Head, baseCommit.SHA)
	}
}

func TestWorktreeAddRejectsOptionShapedBase(t *testing.T) {
	svc, _ := newRepo(t)
	if _, err := svc.WorktreeAdd(context.Background(), WorktreeAddInput{Branch: "from-option", Base: "--force"}); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("option-shaped base = %v, want ErrNotAllowed", err)
	}
}

func TestLinkedWorktreeSupportsCommitStatusAndPush(t *testing.T) {
	ctx := context.Background()
	root, _, _ := newRepoWithRemote(t)
	if err := root.Push(ctx, PushInput{Remote: "origin", Branch: "main", SetUpstream: true}); err != nil {
		t.Fatal(err)
	}
	wt, err := root.WorktreeAdd(ctx, WorktreeAddInput{Branch: "feature/linked"})
	if err != nil {
		t.Fatal(err)
	}
	linked, err := root.At(ctx, wt.Path)
	if err != nil {
		t.Fatalf("At(%q): %v", wt.Path, err)
	}
	write(t, wt.Path, "linked.txt", "linked\n")
	if err := linked.Stage(ctx, []string{"linked.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := linked.Commit(ctx, CommitInput{Message: "linked worktree"}); err != nil {
		t.Fatal(err)
	}
	status, err := linked.Status(ctx)
	if err != nil || status.Branch != "feature/linked" || len(status.Files) != 0 {
		t.Fatalf("linked status = %+v, %v", status, err)
	}
	if err := linked.Push(ctx, PushInput{Remote: "origin", Branch: "feature/linked", SetUpstream: true}); err != nil {
		t.Fatalf("push from linked worktree: %v", err)
	}
}

func TestLinkedWorktreeKeepsParentRepositoryGuard(t *testing.T) {
	ctx := context.Background()
	root, dir := newRepo(t)
	root.SetGuards(guardsFor{dir: {{Pattern: "feature/*", Env: "prod"}}})
	root.SetEnvs(stubEnvs{"prod": true})
	allowAdd := &stubConfirm{allow: true}
	root.SetConfirmer(allowAdd)
	wt, err := root.WorktreeAdd(ctx, WorktreeAddInput{Branch: "feature/guarded"})
	if err != nil {
		t.Fatal(err)
	}
	denyCommit := &stubConfirm{allow: false}
	root.SetConfirmer(denyCommit)
	linked, err := root.At(ctx, wt.Path)
	if err != nil {
		t.Fatal(err)
	}
	if linked.Project() != wt.Path || linked.policyKey() != dir {
		t.Fatalf("linked project/policy = %q/%q; want checkout %q, policy %q", linked.Project(), linked.policyKey(), wt.Path, dir)
	}
	write(t, wt.Path, "guarded.txt", "guarded\n")
	if err := linked.Stage(ctx, []string{"guarded.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := linked.Commit(ctx, CommitInput{Message: "guarded linked commit"}); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("commit on guarded linked branch = %v, want ErrNotAllowed", err)
	}
	if denyCommit.asked != 1 {
		t.Fatalf("denying confirmer asked %d times, want once", denyCommit.asked)
	}
}

type keyedRemoteBindings map[string][]Remote

func (r keyedRemoteBindings) ListRemotes(project string) ([]Remote, error) { return r[project], nil }

func TestPolicyConfiguredFromLinkedGitViewAppliesToOriginalRepository(t *testing.T) {
	ctx := context.Background()
	root, _ := newRepo(t)
	wt, err := root.WorktreeAdd(ctx, WorktreeAddInput{Branch: "feature/policy-view"})
	if err != nil {
		t.Fatal(err)
	}
	linked, err := root.At(ctx, wt.Path)
	if err != nil {
		t.Fatal(err)
	}
	// The bridge stores Git view guard and remote configuration using
	// PolicyProject, even while Project is the linked checkout path.
	configuredKey := linked.PolicyProject()
	if configuredKey != root.PolicyProject() || configuredKey == linked.Project() {
		t.Fatalf("linked Git view policy key = %q, root key = %q, checkout = %q", configuredKey, root.PolicyProject(), linked.Project())
	}
	guards := guardsFor{configuredKey: {{Pattern: "main", Env: "prod"}}, wt.Path: {{Pattern: "feature/*", Env: "prod"}}}
	root.SetGuards(guards)
	linked.SetGuards(guards)
	root.SetEnvs(stubEnvs{"prod": true})
	linked.SetEnvs(stubEnvs{"prod": true})
	root.SetConfirmer(&stubConfirm{allow: false})
	linked.SetConfirmer(&stubConfirm{allow: false})
	write(t, root.Project(), "policy.txt", "guarded\n")
	if err := root.Stage(ctx, []string{"policy.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := root.Commit(ctx, CommitInput{Message: "must stay guarded"}); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("original repo commit after linked-view guard = %v, want ErrNotAllowed", err)
	}
	write(t, wt.Path, "linked-policy.txt", "legacy linked alias\n")
	if err := linked.Stage(ctx, []string{"linked-policy.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := linked.Commit(ctx, CommitInput{Message: "legacy alias stays guarded"}); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("linked commit with legacy alias guard = %v, want ErrNotAllowed", err)
	}

	runGit(t, root, root.Project(), "remote", "add", "origin", "https://example.test/acme/repo.git")
	bindings := keyedRemoteBindings{configuredKey: {{Name: "origin", AccountID: "account-from-linked-view", Env: "prod"}}, wt.Path: {{Name: "origin", AccountID: "legacy-account", Env: "dev"}}}
	root.SetRemotes(bindings)
	linked.SetRemotes(bindings)
	remotes, err := linked.Remotes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(remotes) != 1 || remotes[0].AccountID != "account-from-linked-view" || remotes[0].Env != "prod" {
		t.Fatalf("remote bindings from linked-view key = %+v", remotes)
	}
	// Canonical rows win, while a checkout-keyed legacy row remains readable
	// when the canonical record is absent.
	root.SetRemotes(keyedRemoteBindings{wt.Path: {{Name: "origin", AccountID: "legacy-account", Env: "dev"}}})
	linked.SetRemotes(keyedRemoteBindings{wt.Path: {{Name: "origin", AccountID: "legacy-account", Env: "dev"}}})
	remotes, err = linked.Remotes(ctx)
	if err != nil || len(remotes) != 1 || remotes[0].AccountID != "legacy-account" {
		t.Fatalf("legacy remote binding = %+v, %v", remotes, err)
	}
}
