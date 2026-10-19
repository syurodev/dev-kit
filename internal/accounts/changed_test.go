package accounts

import "testing"

// changes counts OnChanged calls so a test can say how many a write made.
func changes(svc *Service) *int {
	n := 0
	svc.OnChanged = func() { n++ }
	return &n
}

func expectChanges(t *testing.T, n *int, want int, what string) {
	t.Helper()
	if *n != want {
		t.Fatalf("%s: OnChanged fired %d times, want %d", what, *n, want)
	}
	*n = 0
}

// The pull-request card stops polling once a folder has no account, so every
// write that can give it one (or take it away) must say so, exactly once.
func TestOnChangedFiresOncePerAccountWrite(t *testing.T) {
	svc := identityService(t)
	n := changes(svc)

	for _, local := range []bool{true, false} {
		token, err := svc.CreateGitToken(GitTokenInput{Provider: "gitlab", Label: "GitLab", Token: "glpat-1", Local: local})
		if err != nil {
			t.Fatal(err)
		}
		expectChanges(t, n, 1, "CreateGitToken")

		if _, err := svc.RotateToken(token.ID, "", "glpat-2"); err != nil {
			t.Fatal(err)
		}
		expectChanges(t, n, 1, "RotateToken")

		if err := svc.Delete(token.ID); err != nil {
			t.Fatal(err)
		}
		expectChanges(t, n, 1, "Delete")
	}
}

func TestOnChangedFiresForPromoteAndChoices(t *testing.T) {
	svc := identityService(t)
	n := changes(svc)

	local, err := svc.CreateGitToken(GitTokenInput{Provider: "github", Label: "Local", Token: "ghp_1", Local: true})
	if err != nil {
		t.Fatal(err)
	}
	expectChanges(t, n, 1, "create")
	shared, err := svc.Promote(local.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	expectChanges(t, n, 1, "Promote")

	other, err := svc.CreateGitToken(GitTokenInput{Provider: "github", Label: "Other", Token: "ghp_2"})
	if err != nil {
		t.Fatal(err)
	}
	expectChanges(t, n, 1, "create")

	if err := svc.ChooseForRemote("github.com", "acme", "", ViaHTTPS, other.ID); err != nil {
		t.Fatal(err)
	}
	expectChanges(t, n, 1, "ChooseForRemote")
	if err := svc.ForgetRemoteChoice("github.com", "acme", ViaHTTPS); err != nil {
		t.Fatal(err)
	}
	expectChanges(t, n, 1, "ForgetRemoteChoice")
	if err := svc.ChooseForHost("github.com", "", shared.ID); err != nil {
		t.Fatal(err)
	}
	expectChanges(t, n, 1, "ChooseForHost")
	if err := svc.ForgetHostChoice("github.com", ""); err != nil {
		t.Fatal(err)
	}
	expectChanges(t, n, 1, "ForgetHostChoice")
}

func TestOnChangedStaysQuietForRefusalsAndConnectionPasswords(t *testing.T) {
	svc := identityService(t)
	n := changes(svc)

	if _, err := svc.Create(CreateInput{Provider: "github", Kind: KindToken, Label: ""}); err == nil {
		t.Fatal("an unlabeled account was created")
	}
	if _, err := svc.RotateToken("missing", "", "x"); err == nil {
		t.Fatal("a missing account was rotated")
	}
	// A connection's own password is no account anyone resolves for a remote.
	if _, err := svc.Create(CreateInput{Provider: ProviderDatabase, Kind: KindSecret, Label: "db", Secret: "pw", ScopeOwner: "profile-1"}); err != nil {
		t.Fatal(err)
	}
	expectChanges(t, n, 0, "refusals and a connection password")
}

func TestAServiceWithNoListenerNeverPanics(t *testing.T) {
	svc := identityService(t)
	if _, err := svc.CreateGitToken(GitTokenInput{Provider: "github", Label: "T", Token: "ghp_1"}); err != nil {
		t.Fatal(err)
	}
}
