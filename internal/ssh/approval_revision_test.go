package ssh

import (
	"testing"
	"time"
)

func TestApprovalRevisionTracksTunnelEndpointAndDefeatsAba(t *testing.T) {
	svc := newService(t)
	profile, err := svc.CreateProfile(CreateProfileInput{Name: "tunnel", Host: "127.0.0.1", Username: "dev", AuthMethod: AuthPassword, Secret: "test-only", Env: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	initial, err := svc.ApprovalRevision(profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	updateHost := func(host string) {
		t.Helper()
		time.Sleep(2 * time.Millisecond)
		if _, err := svc.UpdateProfile(profile.ID, UpdateProfileInput{Name: "tunnel", Host: host, Port: profile.Port, Username: "dev", Env: "dev"}); err != nil {
			t.Fatal(err)
		}
	}
	updateHost("127.0.0.2")
	changed, err := svc.ApprovalRevision(profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if changed == initial {
		t.Fatal("SSH tunnel endpoint change did not change approval revision")
	}
	updateHost("127.0.0.1")
	returned, err := svc.ApprovalRevision(profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if returned == initial {
		t.Fatal("SSH tunnel revision returned to its old value after endpoint ABA")
	}
}
