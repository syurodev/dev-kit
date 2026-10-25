package database

import (
	"encoding/json"
	"testing"
	"time"
)

func TestApprovalRevisionTracksInheritedParentAndDefeatsAba(t *testing.T) {
	svc, store := newServiceWithStore(t)
	parent, err := svc.CreateProfile(CreateProfileInput{Name: "server", Host: "127.0.0.1", Port: 5432, Username: "reader", Password: "test-only", Database: "postgres", Env: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	childMeta, _ := json.Marshal(metadata{ParentProfileID: parent.ID, Database: "orders"})
	child, err := store.Create(itemType, passwordKeyID, childMeta, []byte{})
	if err != nil {
		t.Fatal(err)
	}

	initial, err := svc.ApprovalRevision(child.ID)
	if err != nil {
		t.Fatal(err)
	}
	updateParent := func(host string) {
		t.Helper()
		time.Sleep(2 * time.Millisecond)
		parentRecord, err := store.Get(parent.ID)
		if err != nil {
			t.Fatal(err)
		}
		parentMeta, err := decodeMetadata(parentRecord)
		if err != nil {
			t.Fatal(err)
		}
		parentMeta.Host = host
		raw, err := json.Marshal(parentMeta)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.UpdateMetadata(parent.ID, raw); err != nil {
			t.Fatal(err)
		}
	}

	updateParent("127.0.0.2")
	afterParentEdit, err := svc.ApprovalRevision(child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterParentEdit == initial {
		t.Fatal("child revision ignored inherited parent endpoint change")
	}
	updateParent("127.0.0.1")
	afterAba, err := svc.ApprovalRevision(child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterAba == initial {
		t.Fatal("child revision returned to its old value after parent endpoint ABA")
	}
}
