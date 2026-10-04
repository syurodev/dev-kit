package ssh

import (
	"context"
	"encoding/json"
	"testing"
)

func TestUpdateProfileKeepsCredentialAndResetsPinOnlyForAddress(t *testing.T) {
	svc, store := newServiceWithStore(t)
	p, err := svc.CreateProfile(CreateProfileInput{Name: "original", Host: "old.example", Port: 22, Username: "deploy", Secret: "secret", Env: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	record, _ := store.Get(p.ID)
	meta, _ := decodeMetadata(record)
	meta.HostKeyFingerprint = "SHA256:pinned"
	raw, _ := json.Marshal(meta)
	if _, err := store.UpdateMetadata(p.ID, raw); err != nil {
		t.Fatal(err)
	}
	input := UpdateProfileInput{Name: "renamed", Host: p.Host, Port: p.Port, Username: p.Username, Env: "prod"}
	updated, err := svc.UpdateProfile(p.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if updated.HostKeyFingerprint != meta.HostKeyFingerprint || updated.Env != "prod" || updated.ID != p.ID {
		t.Fatalf("updated = %+v", updated)
	}
	record, _ = store.Get(p.ID)
	secret, err := store.OpenPayload(record, secretKeyID)
	if err != nil || string(secret) != "secret" {
		t.Fatalf("credential changed: %v", err)
	}
	input.Host = "new.example"
	updated, err = svc.UpdateProfile(p.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if updated.HostKeyFingerprint != "" {
		t.Fatal("address change kept the old pin")
	}
}

func TestUpdateProfileRejectsInvalidMetadataWithoutMutating(t *testing.T) {
	svc := newService(t)
	p, err := svc.CreateProfile(CreateProfileInput{Name: "original", Host: "server", Username: "deploy", Secret: "secret", Env: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	cases := []UpdateProfileInput{
		{Name: "new", Host: "server", Port: -1, Username: "deploy", Env: "dev"},
		{Name: "new", Host: "server", Port: 22, Username: "deploy", Env: "missing"},
		{Name: "new", Host: "https://server", Port: 22, Username: "deploy", Env: "dev"},
	}
	for _, input := range cases {
		if _, err := svc.UpdateProfile(p.ID, input); err == nil {
			t.Fatalf("accepted invalid input %+v", input)
		}
	}
	profiles, _ := svc.ListProfiles()
	if len(profiles) != 1 || profiles[0].Name != "original" {
		t.Fatalf("failed edits mutated profile: %+v", profiles)
	}
}

func TestUpdateProfileStopsOwnedForwards(t *testing.T) {
	svc := newService(t)
	p, err := svc.CreateProfile(CreateProfileInput{Name: "original", Host: "server", Username: "deploy", Secret: "secret", Env: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	svc.forwardDial = func(context.Context, string, string) (forwardingClient, error) { return &fakeForwardClient{}, nil }
	_, err = svc.StartForward(context.Background(), p.ID, ForwardSpec{Direction: ForwardDirectionLocal, ListenHost: "127.0.0.1", TargetHost: "remote", TargetPort: 5432}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	if _, err = svc.UpdateProfile(p.ID, UpdateProfileInput{Name: p.Name, Host: p.Host, Port: p.Port, Username: p.Username, Env: "missing"}); err == nil {
		t.Fatal("accepted bad environment")
	}
	if len(svc.ListForwards()) != 1 {
		t.Fatal("failed edit stopped a working forward")
	}
	if _, err = svc.UpdateProfile(p.ID, UpdateProfileInput{Name: p.Name, Host: "other", Port: p.Port, Username: p.Username, Env: p.Env}); err != nil {
		t.Fatal(err)
	}
	if len(svc.ListForwards()) != 0 {
		t.Fatal("edit left a forward on the old server")
	}
}
