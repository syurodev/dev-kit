//go:build !server

package main

import (
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"

	"devkit/internal/appmenu"
)

// setsApplicationMenu reports whether this platform has somewhere to put an
// application menu that does not cost the window a strip of itself.
//
// macOS has the system menu bar and Windows keeps its native caption above an
// in-window one. GTK4 has neither: given a menu, Wails prepends a gtk_menu_bar
// to the window's box — a permanent row ABOVE the webview, which on a frameless
// window (see framelessWindow in native_webview_linux.go) means above DevKit's
// own title bar, so the window wears a strip of File/Edit/View/Window/Help it
// did not ask for and cannot dismiss: Wails' hideMenuBar and toggleMenuBar are
// empty functions on Linux, and UseApplicationMenu:false changes nothing
// because the window falls back to the application menu regardless. The other
// style Wails offers, LinuxMenuStylePrimaryMenu, calls gtk_window_set_titlebar
// — it would hand back the very GTK decoration Frameless exists to remove.
//
// Nothing is lost on Linux that the window does not already offer: every module
// command the Database and Agent menus list comes from the same module metadata
// the command palette reads (Workbench.tsx: availableModules.flatMap(m =>
// m.commands)), one click away on the title bar's Commands button. The single
// exception is Close Tab's accelerator, which the menu alone carried — the
// frontend binds Ctrl+W there instead (see isCloseTabShortcut in
// menuCommands.ts).
func setsApplicationMenu() bool { return runtime.GOOS != "linux" }

// buildApplicationMenu returns the platform's default menus plus DevKit's own.
//
// Both halves matter. designing-for-macos: "Use the menu bar to give people
// easy access to all the commands they need to do things in your app" — before
// this the bar held only what Wails installs when an app sets no menu, so every
// action DevKit has lived in a context menu or a hover button, out of reach of
// ⌃F2, VoiceOver and anyone looking for a keyboard shortcut. Choosing an item
// emits its id; the frontend decides what running it means (see
// menuCommands.ts).
//
// Built here rather than in appmenu, which stays free of the Wails application
// package: internal/app reads the same list for its module metadata, and that
// package has to cross-compile without a window toolkit.
//
// Only called where setsApplicationMenu says the platform has a bar worth
// filling.
func buildApplicationMenu(wailsApp *application.App) *application.Menu {
	applicationMenu := application.DefaultApplicationMenu()
	// ⌘W closes the tab in front of you and ⇧⌘W the window — see appmenu's
	// CloseTab. Close Tab goes into whichever menu the platform put Close
	// Window in, directly above it.
	if closeWindow := applicationMenu.FindByRole(application.CloseWindow); closeWindow != nil {
		closeWindow.SetAccelerator(appmenu.CloseWindowAccelerator)
		for i := 0; applicationMenu.ItemAt(i) != nil; i++ {
			submenu := applicationMenu.ItemAt(i).GetSubmenu()
			if submenu == nil || submenu.FindByRole(application.CloseWindow) == nil {
				continue
			}
			closeTab := application.NewMenu()
			closeTab.Add(appmenu.CloseTabLabel).
				SetAccelerator(appmenu.CloseTabAccelerator).
				OnClick(func(*application.Context) {
					wailsApp.Event.Emit(appmenu.EventName, appmenu.CloseTabID)
				})
			submenu.Prepend(closeTab)
			break
		}
	}
	for _, submenu := range appmenu.Submenus {
		target := applicationMenu.AddSubmenu(submenu.Title)
		for _, command := range submenu.Commands {
			if command.Separator {
				target.AddSeparator()
			}
			id := command.ID
			item := target.Add(command.Label).OnClick(func(*application.Context) {
				wailsApp.Event.Emit(appmenu.EventName, id)
			})
			if command.Accelerator != "" {
				item.SetAccelerator(command.Accelerator)
			}
		}
	}
	return applicationMenu
}
