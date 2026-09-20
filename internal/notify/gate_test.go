package notify

import "testing"

func target(id string) Target {
	return Target{Module: "agenthub", Kind: "session", ID: id}
}

func TestGateGroupsByTarget(t *testing.T) {
	var gate Gate
	if gate.Next(target("a")) {
		t.Fatal("first notification for a target must send")
	}
	if !gate.Next(target("a")) {
		t.Fatal("later notification for a target must update")
	}
	if gate.Next(target("b")) {
		t.Fatal("a different target gets its own notification")
	}
	gate.Reset(target("a"))
	if gate.Next(target("a")) {
		t.Fatal("reset must allow a new first send")
	}
}

// Two modules using the same id are two targets, not one thread. The old
// string key could not tell them apart without a format to invent.
func TestGateSeparatesModulesSharingAnID(t *testing.T) {
	var gate Gate
	if gate.Next(Target{Module: "agenthub", Kind: "session", ID: "x"}) {
		t.Fatal("first send")
	}
	if gate.Next(Target{Module: "tasks", Kind: "task", ID: "x"}) {
		t.Fatal("a different module with the same id must get its own thread")
	}
}
