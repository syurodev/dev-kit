package main

import (
	"context"
	"fmt"

	"devkit/internal/agenthub"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// wailsNotifications is Wails' notification service as agenthub.Notifier
// needs it.
//
// It is deliberately not passed to application.New as a Service. Wails runs a
// service's ServiceStartup inside app.Run and fails Run when it errors, and
// main exits on that: Startup fails on Linux without a session D-Bus (WSL,
// minimal window managers, containers) and on macOS for a binary without a
// bundle identifier (a plain `go build`), so DevKit did not open at all.
// Started lazily by the Notifier instead, a failure only turns notifications
// off. Unregistered, its methods are not bound to the frontend either — nothing
// there called them — which is also why binding generation no longer writes
// files for it.
type wailsNotifications struct {
	service *notifications.NotificationService
}

func (w wailsNotifications) Start() (err error) {
	defer recoverNotificationPanic(&err)
	return w.service.ServiceStartup(context.Background(), application.ServiceOptions{})
}

func (w wailsNotifications) RequestAuthorization() (allowed bool, err error) {
	defer recoverNotificationPanic(&err)
	return w.service.RequestNotificationAuthorization()
}

func (w wailsNotifications) Send(n agenthub.DesktopNotification, update bool) (err error) {
	defer recoverNotificationPanic(&err)
	options := agentNotificationOptions(n)
	if update {
		return w.service.UpdateNotification(options)
	}
	return w.service.SendNotification(options)
}

// agentNotificationOptions keys the notification and its thread by session, so
// a later event replaces the session's notification instead of stacking, and
// carries the session id back when it is clicked.
func agentNotificationOptions(n agenthub.DesktopNotification) notifications.NotificationOptions {
	return notifications.NotificationOptions{
		ID:       "agenthub-" + n.SessionID,
		Title:    n.Title,
		Body:     n.Body,
		ThreadID: "agenthub-" + n.SessionID,
		Data:     map[string]interface{}{"sessionId": n.SessionID},
	}
}

// notificationSessionID is the session a clicked notification opens.
func notificationSessionID(response notifications.NotificationResponse) (string, bool) {
	sessionID, ok := response.UserInfo["sessionId"].(string)
	return sessionID, ok && sessionID != ""
}

// The platform backends cross into Objective-C, D-Bus and COM; a panic there
// must cost a notification, not the app.
func recoverNotificationPanic(err *error) {
	if recovered := recover(); recovered != nil {
		*err = fmt.Errorf("desktop notification panic: %v", recovered)
	}
}
