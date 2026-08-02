package sync

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestAuthSessionValidateAndRedactToken(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	session, err := newAuthSession("acct-1", "dev-1", "secret-token", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Validate(now); err != nil {
		t.Fatalf("valid session: %v", err)
	}
	if session.AccountID() != "acct-1" || session.DeviceID() != "dev-1" || !session.ExpiresAt().Equal(now.Add(time.Hour)) {
		t.Fatalf("unexpected session metadata: %v", session)
	}

	encoded, err := json.Marshal(session)
	if err != nil {
		t.Fatal(err)
	}
	for _, rendered := range []string{string(encoded), fmt.Sprint(session), fmt.Sprintf("%#v", session)} {
		if strings.Contains(rendered, "secret-token") {
			t.Fatalf("session exposed access token: %s", rendered)
		}
	}
}

func TestAuthSessionFailsClosed(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	expired, err := newAuthSession("acct-1", "dev-1", "token", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := expired.Validate(now); err == nil {
		t.Fatal("session must be expired at its expiry instant")
	}
	if err := expired.Validate(time.Time{}); err == nil {
		t.Fatal("zero current time must fail closed")
	}
	if _, err := newAuthSession("account/forged", "dev-1", "token", now.Add(time.Hour)); err == nil {
		t.Fatal("invalid account ID must be rejected")
	}
	if _, err := newAuthSession("acct-1", "dev-1", "token\nheader", now.Add(time.Hour)); err == nil {
		t.Fatal("header-unsafe token must be rejected")
	}
}
