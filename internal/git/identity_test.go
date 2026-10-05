package git

import (
	"context"
	"strings"
	"testing"
)

// stubIdentities is an account picker that can also say who an account commits
// as: what internal/app's remotePicker is.
type stubIdentities struct {
	stubPicker
	identity AccountIdentity
	asked    []PickAccountInput
	bound    []string
}

func (p *stubIdentities) IdentityFor(_ context.Context, in PickAccountInput, bound string) (AccountIdentity, error) {
	p.asked = append(p.asked, in)
	p.bound = append(p.bound, bound)
	return p.identity, nil
}

var acme = AccountIdentity{Name: "Acme Dev", Email: "dev@acme.example", Account: "github · acme-dev"}

// identityRepo is a repository with a remote on a host the test controls and no
// identity of its own, in a world with no global or system git config: the
// situation where an account's identity has to decide.
func identityRepo(t *testing.T, remoteURL string) (*Service, string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	// The cloud sandbox hands git url rewrites through GIT_CONFIG_COUNT.
	t.Setenv("GIT_CONFIG_COUNT", "0")
	svc, work := newRepo(t)
	runGit(t, svc, work, "config", "--local", "--unset", "user.name")
	runGit(t, svc, work, "config", "--local", "--unset", "user.email")
	runGit(t, svc, work, "remote", "add", "origin", remoteURL)
	return svc, work
}

func commitAs(t *testing.T, svc *Service, work, name string, amend bool) string {
	t.Helper()
	write(t, work, name, name+"\n")
	if err := svc.Stage(context.Background(), []string{name}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Commit(context.Background(), CommitInput{Message: "change " + name, Amend: amend}); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return strings.TrimSpace(runGit(t, svc, work, "log", "-1", "--format=%an|%ae|%cn|%ce"))
}

func TestACommitIsWrittenAsTheAccountOfItsRemote(t *testing.T) {
	svc, work := identityRepo(t, "git@code.example.test:acme/api.git")
	picker := &stubIdentities{identity: acme}
	svc.SetAccountPicker(picker)

	who, err := svc.CommitIdentity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if who.Source != IdentityFromAccount || who.Email != "dev@acme.example" || who.Account != "github · acme-dev" || who.Remote != "origin" {
		t.Fatalf("identity = %+v", who)
	}
	if got := commitAs(t, svc, work, "a.txt", false); got != "Acme Dev|dev@acme.example|Acme Dev|dev@acme.example" {
		t.Fatalf("commit was written as %q", got)
	}
	if len(picker.asked) == 0 || picker.asked[0].Owner != "acme" || picker.asked[0].Transport != TransportSSH {
		t.Fatalf("the account was looked up for %+v", picker.asked)
	}
}

// A user.email typed into this repository is a deliberate choice about this
// repository, more specific than an account chosen for its host. It wins, and
// the person is told the account would have committed differently.
func TestARepositorysOwnEmailWinsOverTheAccount(t *testing.T) {
	svc, work := identityRepo(t, "git@code.example.test:acme/api.git")
	runGit(t, svc, work, "config", "--local", "user.name", "Repo Person")
	runGit(t, svc, work, "config", "--local", "user.email", "repo@person.example")
	svc.SetAccountPicker(&stubIdentities{identity: acme})

	who, _ := svc.CommitIdentity(context.Background())
	if who.Source != IdentityFromRepository || who.Email != "repo@person.example" || !who.Differs() || who.AccountEmail != "dev@acme.example" {
		t.Fatalf("identity = %+v", who)
	}
	if got := commitAs(t, svc, work, "a.txt", false); got != "Repo Person|repo@person.example|Repo Person|repo@person.example" {
		t.Fatalf("commit was written as %q", got)
	}
}

// The case this exists for: the global identity is the personal one, and the
// repository belongs to the company account.
func TestTheAccountOverridesAPersonalGlobalIdentity(t *testing.T) {
	svc, work := identityRepo(t, "https://code.example.test/acme/api.git")
	globals := t.TempDir() + "/gitconfig"
	t.Setenv("GIT_CONFIG_GLOBAL", globals)
	runGit(t, svc, "", "config", "--file", globals, "user.name", "Me Personal")
	runGit(t, svc, "", "config", "--file", globals, "user.email", "me@personal.example")
	svc.SetAccountPicker(&stubIdentities{identity: acme})

	who, _ := svc.CommitIdentity(context.Background())
	if who.Source != IdentityFromAccount || who.Email != "dev@acme.example" {
		t.Fatalf("identity = %+v", who)
	}
	if got := commitAs(t, svc, work, "a.txt", false); got != "Acme Dev|dev@acme.example|Acme Dev|dev@acme.example" {
		t.Fatalf("commit was written as %q", got)
	}
}

// An account with no identity, or half of one, leaves git alone: a name from one
// place and an email from another is worse than either.
func TestAnAccountWithoutAnIdentityLeavesGitsConfigurationAlone(t *testing.T) {
	for name, identity := range map[string]AccountIdentity{
		"none":       {Account: "github · acme-dev"},
		"name only":  {Account: "github · acme-dev", Name: "Acme Dev"},
		"email only": {Account: "github · acme-dev", Email: "dev@acme.example"},
	} {
		svc, work := identityRepo(t, "git@code.example.test:acme/api.git")
		globals := t.TempDir() + "/gitconfig"
		t.Setenv("GIT_CONFIG_GLOBAL", globals)
		runGit(t, svc, "", "config", "--file", globals, "user.name", "Me Personal")
		runGit(t, svc, "", "config", "--file", globals, "user.email", "me@personal.example")
		svc.SetAccountPicker(&stubIdentities{identity: identity})

		who, _ := svc.CommitIdentity(context.Background())
		if who.Source != IdentityFromGit || who.Account != "github · acme-dev" {
			t.Errorf("%s: identity = %+v", name, who)
		}
		if got := commitAs(t, svc, work, "a.txt", false); got != "Me Personal|me@personal.example|Me Personal|me@personal.example" {
			t.Errorf("%s: commit was written as %q", name, got)
		}
	}
}

// Amending keeps the author the commit already had; the person amending is the
// committer.
func TestAmendKeepsTheOriginalAuthor(t *testing.T) {
	svc, work := identityRepo(t, "git@code.example.test:acme/api.git")
	globals := t.TempDir() + "/gitconfig"
	t.Setenv("GIT_CONFIG_GLOBAL", globals)
	runGit(t, svc, "", "config", "--file", globals, "user.name", "Me Personal")
	runGit(t, svc, "", "config", "--file", globals, "user.email", "me@personal.example")

	if got := commitAs(t, svc, work, "a.txt", false); got != "Me Personal|me@personal.example|Me Personal|me@personal.example" {
		t.Fatalf("first commit = %q", got)
	}
	svc.SetAccountPicker(&stubIdentities{identity: acme})
	if got := commitAs(t, svc, work, "b.txt", true); got != "Me Personal|me@personal.example|Acme Dev|dev@acme.example" {
		t.Fatalf("amended commit = %q, want the author kept and the committer changed", got)
	}
}

func TestACommitWithNoPickerIsUntouched(t *testing.T) {
	svc, work := identityRepo(t, "git@code.example.test:acme/api.git")
	runGit(t, svc, work, "config", "--local", "user.name", "Repo Person")
	runGit(t, svc, work, "config", "--local", "user.email", "repo@person.example")
	if got := commitAs(t, svc, work, "a.txt", false); got != "Repo Person|repo@person.example|Repo Person|repo@person.example" {
		t.Fatalf("commit = %q", got)
	}
}

func TestTheRemoteIsTheTrackedOneThenOriginThenTheOnlyOne(t *testing.T) {
	svc, work := identityRepo(t, "git@code.example.test:acme/api.git")
	runGit(t, svc, work, "remote", "add", "fork", "git@code.example.test:me/api.git")
	picker := &stubIdentities{identity: acme}
	svc.SetAccountPicker(picker)

	who, _ := svc.CommitIdentity(context.Background())
	if who.Remote != "origin" {
		t.Fatalf("no upstream: remote = %q, want origin", who.Remote)
	}

	runGit(t, svc, work, "update-ref", "refs/remotes/fork/main", "HEAD")
	runGit(t, svc, work, "branch", "--set-upstream-to=fork/main", "main")
	picker.asked = nil
	if who, _ = svc.CommitIdentity(context.Background()); who.Remote != "fork" || picker.asked[0].Owner != "me" {
		t.Fatalf("tracking fork: remote = %q, asked %+v", who.Remote, picker.asked)
	}

	// Two remotes, none of them origin or tracked: unknown, not a guess.
	runGit(t, svc, work, "branch", "--unset-upstream")
	runGit(t, svc, work, "remote", "rename", "origin", "upstream")
	picker.asked = nil
	if who, _ = svc.CommitIdentity(context.Background()); who.Remote != "" || who.Source == IdentityFromAccount || len(picker.asked) != 0 {
		t.Fatalf("an ambiguous remote chose an account: %+v asked %+v", who, picker.asked)
	}
}

func TestABoundRemotesAccountIsPassedToTheResolver(t *testing.T) {
	svc, _ := identityRepo(t, "git@code.example.test:acme/api.git")
	picker := &stubIdentities{identity: acme}
	svc.SetAccountPicker(picker)
	svc.SetRemotes(stubRemotes{{Name: "origin", AccountID: "acct-bound"}})

	if _, err := svc.CommitIdentity(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(picker.bound) == 0 || picker.bound[0] != "acct-bound" {
		t.Fatalf("resolver was given %v", picker.bound)
	}
}
