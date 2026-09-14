package agenthub

import "testing"

func TestNotificationGateGroupsBySession(t *testing.T) {
	var gate NotificationGate
	if gate.Next("session-a") {
		t.Fatal("first notification for a session must send")
	}
	if !gate.Next("session-a") {
		t.Fatal("later notification for a session must update")
	}
	if gate.Next("session-b") {
		t.Fatal("a different session gets its own notification")
	}
	gate.Reset("session-a")
	if gate.Next("session-a") {
		t.Fatal("reset must allow a new first send")
	}
}
