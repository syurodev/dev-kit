package main

import (
	"context"
	"testing"

	"devkit/internal/agenthub"

	"github.com/wailsapp/wails/v3/pkg/application"
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

// Notifications now start where Wails starts its services, so a click that
// launches the app reaches a delegate. A start that fails there — no session
// D-Bus, no bundle identifier — must still not fail app.Run, which is what
// kept DevKit from opening; and the Notifier's own Start must find that same
// outcome instead of trying the backend again.
func TestNotificationStartupNeverFailsTheApp(t *testing.T) {
	desktop := &wailsNotifications{} // no service: the backend panics on start
	startup := notificationStartup{desktop: desktop}
	if err := startup.ServiceStartup(context.Background(), application.ServiceOptions{}); err != nil {
		t.Fatalf("ServiceStartup = %v; a failed start must not fail app.Run", err)
	}
	first, second := desktop.Start(), desktop.Start()
	if first == nil {
		t.Fatal("the failed start was not kept for the Notifier to find")
	}
	if first != second {
		t.Fatalf("Start ran the backend again: %v, then %v", first, second)
	}
	if err := startup.ServiceShutdown(); err != nil {
		t.Fatalf("ServiceShutdown of a service that never started = %v", err)
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
