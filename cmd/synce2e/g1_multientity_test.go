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
	a.editNote(t, noteID, "note-body-v2")
	a.syncNow(t)
	b.syncNow(t)
	if b.body(t, noteID) != "note-body-v2" {
		t.Fatalf("note not fast-forwarded")
	}
	_ = dbID
	_ = sshID
}
