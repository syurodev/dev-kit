package main

import "testing"

func TestG1MultiEntityPullApply(t *testing.T) {
	baseURL, token := requireDeployed(t)
	vlt := newSharedVault(t)
	a := newBootstrappedDevice(t, baseURL, token, vlt)
	b := newEnrolledDevice(t, baseURL, token, a, vlt)

	noteID := a.createNote(t, "n1", "note-body")
	dbID := a.createDBProfile(t, "Primary")
	sshID := a.createSSHProfile(t, "Bastion")
	a.syncNow(t)
	report := b.syncNow(t)
	if report.Pulled < 3 || report.Applied < 3 {
		t.Fatalf("B report = %#v, want pulled/applied >= 3", report)
	}
	if got := b.body(t, noteID); got != "note-body" {
		t.Fatalf("note body on B = %q, want %q", got, "note-body")
	}
	if got := b.payload(t, dbID, "db-password"); got != "db-secret" {
		t.Fatalf("db password on B = %q, want %q", got, "db-secret")
	}
	if got := b.payload(t, sshID, "ssh-secret"); got != "ssh-secret" {
		t.Fatalf("ssh secret on B = %q, want %q", got, "ssh-secret")
	}
	a.editNote(t, noteID, "note-body-v2")
	a.syncNow(t)
	b.syncNow(t)
	if b.body(t, noteID) != "note-body-v2" {
		t.Fatalf("note not fast-forwarded")
	}
}
