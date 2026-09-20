package accounts

import (
	"errors"
	"testing"

	"devkit/internal/storage"
)

func hostService(t *testing.T) *Service {
	t.Helper()
	svc, db := newService(t)
	svc.SetHostChoices(db, storage.TableAccountHostChoices)
	return svc
}

// One match is answered, not asked. Remembering it would also go stale the
// moment a second account is added.
func TestForHostChoosesTheOnlyMatchWithoutRemembering(t *testing.T) {
	svc := hostService(t)
	only, err := svc.Create(CreateInput{Provider: "github", Kind: KindToken, Label: "Personal", Secret: "ghp_1"})
	if err != nil {
		t.Fatal(err)
	}

	match, err := svc.ForHost("api.github.com", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if !match.Decided || match.Account.ID != only.ID {
		t.Fatalf("one match was not chosen: %+v", match)
	}
	if match.Remembered {
		t.Fatal("a forced answer must not be reported as remembered")
	}

	// Adding a second must turn the same question back into a question.
	if _, err := svc.Create(CreateInput{Provider: "github", Kind: KindToken, Label: "Work", Secret: "ghp_2"}); err != nil {
		t.Fatal(err)
	}
	match, err = svc.ForHost("api.github.com", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if match.Decided {
		t.Fatalf("two matches were decided without asking: %+v", match)
	}
	if len(match.Options) != 2 {
		t.Fatalf("options = %d, want 2", len(match.Options))
	}
}

// The scenario from the spec: ask once per (host, env), and the same host on a
// different environment is a different question — the personal account on dev
// and the company one on prod is the ordinary case.
func TestChoiceIsRememberedPerHostAndEnvironment(t *testing.T) {
	svc := hostService(t)
	personal, err := svc.Create(CreateInput{Provider: "github", Kind: KindToken, Label: "Personal", Secret: "ghp_1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(CreateInput{Provider: "github", Kind: KindToken, Label: "Work", Secret: "ghp_2"}); err != nil {
		t.Fatal(err)
	}

	if err := svc.ChooseForHost("api.github.com", "dev", personal.ID); err != nil {
		t.Fatal(err)
	}
	match, err := svc.ForHost("api.github.com", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if !match.Decided || match.Account.ID != personal.ID || !match.Remembered {
		t.Fatalf("the answer was not remembered: %+v", match)
	}
	// Both options still come back, so the UI can say there was a choice and
	// offer to change it rather than hiding that one was made.
	if len(match.Options) != 2 {
		t.Fatalf("options = %d, want both", len(match.Options))
	}

	onProd, err := svc.ForHost("api.github.com", "prod")
	if err != nil {
		t.Fatal(err)
	}
	if onProd.Decided {
		t.Fatal("an answer for dev must not answer for prod")
	}

	if err := svc.ForgetHostChoice("api.github.com", "dev"); err != nil {
		t.Fatal(err)
	}
	if again, err := svc.ForHost("api.github.com", "dev"); err != nil || again.Decided {
		t.Fatalf("forgetting did not restore the question: %+v %v", again, err)
	}
}

// A remembered account that is gone is not an answer. Leaving the row would
// make the same stale id be ignored on every call for ever.
func TestAStaleChoiceIsDroppedRatherThanIgnoredForever(t *testing.T) {
	svc := hostService(t)
	doomed, err := svc.Create(CreateInput{Provider: "github", Kind: KindToken, Label: "Old", Secret: "ghp_1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(CreateInput{Provider: "github", Kind: KindToken, Label: "Work", Secret: "ghp_2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(CreateInput{Provider: "github", Kind: KindToken, Label: "Third", Secret: "ghp_3"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.ChooseForHost("github.com", "dev", doomed.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(doomed.ID); err != nil {
		t.Fatal(err)
	}

	match, err := svc.ForHost("github.com", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if match.Decided {
		t.Fatalf("a deleted account still answered: %+v", match)
	}
	if svc.choices == nil {
		t.Fatal("test wired no choice store")
	}
	if id, err := svc.choices.get("github.com", "dev"); err != nil || id != "" {
		t.Fatalf("the stale row survived: %q %v", id, err)
	}
}

// Pinning an account that does not match the host is the same
// credential-misdirection the whole rule exists to prevent, just written down
// rather than guessed.
func TestChooseRefusesAnAccountThatDoesNotMatch(t *testing.T) {
	svc := hostService(t)
	if _, err := svc.Create(CreateInput{Provider: "github", Kind: KindToken, Label: "Personal", Secret: "ghp_1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(CreateInput{Provider: "github", Kind: KindToken, Label: "Work", Secret: "ghp_2"}); err != nil {
		t.Fatal(err)
	}
	elsewhere, err := svc.Create(CreateInput{Provider: "openai", Kind: KindToken, Label: "OpenAI", Secret: "sk_1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ChooseForHost("api.github.com", "dev", elsewhere.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("pinning an unrelated account = %v, want ErrInvalid", err)
	}
}

// An account restricted to prod is not an option on dev — the restriction is
// the point of the restriction.
func TestForHostHonoursTheEnvironmentRestriction(t *testing.T) {
	svc := hostService(t)
	anywhere, err := svc.Create(CreateInput{Provider: "github", Kind: KindToken, Label: "Anywhere", Secret: "ghp_1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(CreateInput{
		Provider: "github", Kind: KindToken, Label: "Prod only", Secret: "ghp_2", Env: []string{"prod"},
	}); err != nil {
		t.Fatal(err)
	}

	match, err := svc.ForHost("api.github.com", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if !match.Decided || match.Account.ID != anywhere.ID {
		t.Fatalf("the prod-only account was offered on dev: %+v", match)
	}
	if onProd, err := svc.ForHost("api.github.com", "prod"); err != nil || onProd.Decided {
		t.Fatalf("both match on prod, so it is a question: %+v %v", onProd, err)
	}
}

// The suffix rule is the registry's, and a lookalike host must not reach a
// real account.
func TestForHostDoesNotMatchALookalikeHost(t *testing.T) {
	svc := hostService(t)
	if _, err := svc.Create(CreateInput{Provider: "github", Kind: KindToken, Label: "Personal", Secret: "ghp_1"}); err != nil {
		t.Fatal(err)
	}
	match, err := svc.ForHost("evil-github.com.attacker.test", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if match.Decided || len(match.Options) != 0 {
		t.Fatalf("a lookalike host found an account: %+v", match)
	}
}

// Without the table the rule still holds; it simply asks every time. A missing
// memory is an inconvenience, a wrong automatic answer is not.
func TestWithoutAChoiceStoreTheQuestionIsAskedEveryTime(t *testing.T) {
	svc, _ := newService(t)
	if _, err := svc.Create(CreateInput{Provider: "github", Kind: KindToken, Label: "Personal", Secret: "ghp_1"}); err != nil {
		t.Fatal(err)
	}
	second, err := svc.Create(CreateInput{Provider: "github", Kind: KindToken, Label: "Work", Secret: "ghp_2"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ChooseForHost("api.github.com", "dev", second.ID); err != nil {
		t.Fatalf("choosing with no store must be a no-op, not an error: %v", err)
	}
	if match, err := svc.ForHost("api.github.com", "dev"); err != nil || match.Decided {
		t.Fatalf("decided with nothing to remember it: %+v %v", match, err)
	}
}
