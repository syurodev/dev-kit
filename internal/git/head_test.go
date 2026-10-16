package git

import (
	"context"
	"testing"
)

func TestRepoPathKeepsTheWholePathAndItsCase(t *testing.T) {
	for raw, want := range map[string]string{
		"https://github.com/Acme-Corp/api.git":          "Acme-Corp/api",
		"git@github.com:acme/api.git":                   "acme/api",
		"git@github.com:/acme/api":                      "acme/api",
		"ssh://git@gitlab.com:22/group/sub/project.git": "group/sub/project",
		"https://gitlab.com/group/sub/project":          "group/sub/project",
		"https://github.com/loner.git":                  "",
		"/srv/git/origin.git":                           "",
		"git://example.com/acme/api.git":                "",
	} {
		if got := RepoPath(raw); got != want {
			t.Errorf("RepoPath(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestHeadFollowsThePushRemoteOfTheBranch(t *testing.T) {
	ctx := context.Background()
	svc, work, _ := newRepoWithRemote(t)
	runGit(t, svc, work, "remote", "set-url", "origin", "https://github.com/acme/api.git")
	runGit(t, svc, work, "remote", "add", "fork", "git@github.com:me/api.git")
	svc.SetRemotes(stubRemotes{{Name: "fork", Env: "dev", AccountID: "acct"}})
	runGit(t, svc, work, "checkout", "-b", "feat/x")

	head, err := svc.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if head.Branch != "feat/x" || head.Detached || head.Remote != "origin" || head.OriginURL != head.RemoteURL {
		t.Fatalf("an unconfigured branch pushes to origin: %+v", head)
	}

	runGit(t, svc, work, "config", "branch.feat/x.pushRemote", "fork")
	head, _ = svc.Head(ctx)
	if head.Remote != "fork" || head.RemoteURL != "git@github.com:me/api.git" || head.OriginURL != "https://github.com/acme/api.git" {
		t.Fatalf("pushRemote: %+v", head)
	}
	if head.RemoteEnv != "dev" || head.RemoteAccountID != "acct" {
		t.Errorf("the binding of the push remote was lost: %+v", head)
	}

	runGit(t, svc, work, "config", "--unset", "branch.feat/x.pushRemote")
	runGit(t, svc, work, "config", "remote.pushDefault", "fork")
	if head, _ = svc.Head(ctx); head.Remote != "fork" {
		t.Errorf("pushDefault: %+v", head)
	}

	runGit(t, svc, work, "checkout", "--detach")
	head, _ = svc.Head(ctx)
	if !head.Detached || head.Branch != "" {
		t.Errorf("detached: %+v", head)
	}
}
