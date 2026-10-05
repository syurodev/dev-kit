package interaction

import (
	"context"
	"errors"
	"testing"
	"time"
)

var twoAccounts = []AccountOption{
	{ID: "acct-work", Label: "github · acme-dev", Provider: "github", Subject: "acme-dev"},
	{ID: "acct-me", Label: "github · syuro", Provider: "github", Subject: "syuro"},
}

func askAccount(t *testing.T, h *Hub, req SelectAccountRequest, answer string) (AccountChoice, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	type result struct {
		choice AccountChoice
		err    error
	}
	done := make(chan result, 1)
	go func() {
		choice, err := h.SelectAccount(ctx, req)
		done <- result{choice, err}
	}()
	waitForPending(t, h)
	pending := h.Pending()
	if pending == nil || pending.Kind != KindGitAccount || pending.Type != TypeSelect {
		t.Fatalf("pending = %+v", pending)
	}
	if len(pending.Options) != len(req.Options) {
		t.Fatalf("the UI was offered %d options, want %d", len(pending.Options), len(req.Options))
	}
	if err := h.Complete(pending.ID, answer); err != nil {
		t.Fatal(err)
	}
	r := <-done
	return r.choice, r.err
}

func TestSelectAccountReturnsTheChosenAccount(t *testing.T) {
	h := NewHub(NoopPrompter{})
	choice, err := askAccount(t, h, SelectAccountRequest{
		Summary: "Push acme-corp/api", Remember: "acme-corp on github.com", Options: twoAccounts,
	}, AccountAnswer("acct-work", true))
	if err != nil {
		t.Fatal(err)
	}
	if choice.ID != "acct-work" || !choice.Remember {
		t.Fatalf("choice = %+v", choice)
	}

	choice, err = askAccount(t, h, SelectAccountRequest{Remember: "acme-corp on github.com", Options: twoAccounts},
		AccountAnswer("acct-me", false))
	if err != nil {
		t.Fatal(err)
	}
	if choice.ID != "acct-me" || choice.Remember {
		t.Fatalf("choice = %+v", choice)
	}
}

// An answer naming an account the question never listed must not be accepted:
// the hub is the last place that can stop a credential going to the wrong
// identity.
func TestSelectAccountRefusesAnAccountThatWasNotOffered(t *testing.T) {
	h := NewHub(NoopPrompter{})
	_, err := askAccount(t, h, SelectAccountRequest{Options: twoAccounts}, AccountAnswer("acct-stranger", false))
	if !errors.Is(err, ErrConfirmationDenied) {
		t.Fatalf("err = %v, want a denial", err)
	}
}

func TestSelectAccountIgnoresARememberRequestThatWasNeverOffered(t *testing.T) {
	h := NewHub(NoopPrompter{})
	choice, err := askAccount(t, h, SelectAccountRequest{Options: twoAccounts}, AccountAnswer("acct-work", true))
	if err != nil {
		t.Fatal(err)
	}
	if choice.Remember {
		t.Fatal("an answer was remembered although the question offered no such thing")
	}
}

func TestSelectAccountCancelAndMalformedAnswersDeny(t *testing.T) {
	for _, answer := range []string{"", "acct-work", "later:acct-work", "once:"} {
		h := NewHub(NoopPrompter{})
		if _, err := askAccount(t, h, SelectAccountRequest{Options: twoAccounts}, answer); !errors.Is(err, ErrConfirmationDenied) {
			t.Errorf("answer %q: err = %v, want a denial", answer, err)
		}
	}
}

func TestSelectAccountFailsFastWhereNothingCanAsk(t *testing.T) {
	headless := NewHub(NoopPrompter{})
	headless.Headless = true
	if _, err := headless.SelectAccount(context.Background(), SelectAccountRequest{Options: twoAccounts}); !errors.Is(err, ErrSelectionRequired) {
		t.Fatalf("headless: %v", err)
	}
	if _, err := NewHub(nil).SelectAccount(context.Background(), SelectAccountRequest{Options: twoAccounts}); !errors.Is(err, ErrSelectionRequired) {
		t.Fatalf("no prompter: %v", err)
	}
	if _, err := NewHub(NoopPrompter{}).SelectAccount(context.Background(), SelectAccountRequest{}); !errors.Is(err, ErrSelectionRequired) {
		t.Fatalf("no options: %v", err)
	}
}
