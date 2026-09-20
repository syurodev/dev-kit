package main

import (
	"context"
	"fmt"
	"sync"

	"devkit/internal/notify"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// wailsNotifications is Wails' notification service as notify.Notifier
// needs it.
//
// It is deliberately not passed to application.New as a Service. Wails runs a
// service's ServiceStartup inside app.Run and fails Run when it errors, and
// main exits on that: Startup fails on Linux without a session D-Bus (WSL,
// minimal window managers, containers) and on macOS for a binary without a
// bundle identifier (a plain `go build`), so DevKit did not open at all.
// notificationStartup starts it at the same point without that failure mode.
// Unregistered, its methods are not bound to the frontend either — nothing
// there called them — which is also why binding generation writes no files
// for it.
type wailsNotifications struct {
	service *notifications.NotificationService

	startOnce sync.Once
	startErr  error
}

// Start brings the backend up once. notificationStartup and the Notifier's
// first Authorize both call it and share the one outcome.
func (w *wailsNotifications) Start() error {
	w.startOnce.Do(func() { w.startErr = w.start() })
	return w.startErr
}

func (w *wailsNotifications) start() (err error) {
	defer recoverNotificationPanic(&err)
	return w.service.ServiceStartup(context.Background(), application.ServiceOptions{})
}

func (w *wailsNotifications) RequestAuthorization() (allowed bool, err error) {
	defer recoverNotificationPanic(&err)
	return w.service.RequestNotificationAuthorization()
}

func (w *wailsNotifications) Send(n notify.Notification, update bool) (err error) {
	defer recoverNotificationPanic(&err)
	options := notificationOptions(n)
	if update {
		return w.service.UpdateNotification(options)
	}
	return w.service.SendNotification(options)
}

// notificationStartup starts desktop notifications where Wails starts its
// services — inside app.Run, before the platform run loop — and cannot fail
// Run.
//
// Starting on the first window focus instead lost clicks. macOS hands a
// notification response only to a UNUserNotificationCenter delegate set
// before the app finishes launching, and Windows has no toast activation
// callback or COM class factory until Startup ran, so a click that launched or
// activated DevKit before any focus went nowhere. A start that fails here
// changes nothing else: ServiceStartup still returns nil, and the Notifier
// finds the failure on its first Authorize and turns notifications off.
//
// Its only methods are the two Wails treats as lifecycle hooks, so it binds
// nothing to the frontend.
type notificationStartup struct{ desktop *wailsNotifications }

func (n notificationStartup) ServiceStartup(context.Context, application.ServiceOptions) error {
	_ = n.desktop.Start()
	return nil
}

// ServiceShutdown stops a backend that started; on Windows that cancels
// scheduled toasts before the activator goes away. Wails shuts down only
// services whose ServiceStartup ran, so Start here only reads the outcome.
func (n notificationStartup) ServiceShutdown() (err error) {
	if n.desktop.Start() != nil {
		return nil
	}
	defer recoverNotificationPanic(&err)
	return n.desktop.service.ServiceShutdown()
}

// notificationOptions keys the notification and its thread by target, so a
// later event about the same thing replaces its notification instead of
// stacking, and carries the target back when it is clicked.
//
// The key holds the module too: two modules using the same id are two
// notifications, which the session-only key could not express.
func notificationOptions(n notify.Notification) notifications.NotificationOptions {
	key := notificationKey(n.Target)
	return notifications.NotificationOptions{
		ID:       key,
		Title:    n.Title,
		Body:     n.Body,
		ThreadID: key,
		Data: map[string]interface{}{
			"module": n.Target.Module,
			"kind":   n.Target.Kind,
			"id":     n.Target.ID,
		},
	}
}

func notificationKey(t notify.Target) string {
	return t.Module + "-" + t.Kind + "-" + t.ID
}

// notificationTarget is what a clicked notification opens.
func notificationTarget(response notifications.NotificationResponse) (notify.Target, bool) {
	module, _ := response.UserInfo["module"].(string)
	kind, _ := response.UserInfo["kind"].(string)
	id, _ := response.UserInfo["id"].(string)
	target := notify.Target{Module: module, Kind: kind, ID: id}
	return target, module != "" && id != ""
}

// The platform backends cross into Objective-C, D-Bus and COM; a panic there
// must cost a notification, not the app.
func recoverNotificationPanic(err *error) {
	if recovered := recover(); recovered != nil {
		*err = fmt.Errorf("desktop notification panic: %v", recovered)
	}
}

// agentTarget is one Agent Hub session as the notifier addresses it.
func agentTarget(sessionID string) notify.Target {
	return notify.Target{Module: "agenthub", Kind: "session", ID: sessionID}
}
