package main

import (
	"testing"

	"devkit/internal/agenthub"

	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// One notification per session, and a click opens that session: the OS hands
// Data back as UserInfo.
func TestAgentNotificationIsKeyedBySessionAndOpensIt(t *testing.T) {
	a := agentNotificationOptions(agenthub.DesktopNotification{SessionID: "s1", Title: "Permission needed", Body: "x"})
	b := agentNotificationOptions(agenthub.DesktopNotification{SessionID: "s1", Title: "Answer ready", Body: "y"})
	other := agentNotificationOptions(agenthub.DesktopNotification{SessionID: "s2", Title: "Answer ready", Body: "y"})
	if a.ID != b.ID || a.ThreadID != b.ThreadID {
		t.Fatalf("one session's notifications differ in ID/thread: %q/%q vs %q/%q", a.ID, a.ThreadID, b.ID, b.ThreadID)
	}
	if a.ID == other.ID || a.ThreadID == other.ThreadID {
		t.Fatalf("two sessions share notification %q", a.ID)
	}

	got, ok := notificationSessionID(notifications.NotificationResponse{UserInfo: a.Data})
	if !ok || got != "s1" {
		t.Fatalf("clicked notification opens %q (ok=%v), want s1", got, ok)
	}
	if _, ok := notificationSessionID(notifications.NotificationResponse{}); ok {
		t.Fatal("a notification without a session opened one")
	}
}

func TestWailsNotificationsTurnsABackendPanicIntoAnError(t *testing.T) {
	err := func() (err error) {
		defer recoverNotificationPanic(&err)
		panic("objc exception")
	}()
	if err == nil {
		t.Fatal("panic was not recovered into an error")
	}
}
