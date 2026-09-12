// Package appmenu builds the application menu.
//
// It exists because DevKit had none of its own. Wails installs a default
// App/File/Edit/View/Window/Help menu when an app sets no menu, so copy, paste
// and close-window always worked — but nothing DevKit itself does was in the
// menu bar, and on macOS that is where people look for it and where ⌃F2 and
// VoiceOver go to enumerate what an app can do.
//
// The items are data, not code. A menu is built once at startup, before the
// frontend exists, so the labels and the shortcuts live here; the ID is all
// that crosses over, and the frontend decides what running it means. That
// split is what lets the same list feed the command palette without the two
// drifting into different names for the same action.
package appmenu

import "github.com/wailsapp/wails/v3/pkg/application"

// EventName is the event a menu selection emits. The payload is the Command ID.
const EventName = "devkit:command"

// A Command is one menu item the frontend knows how to run.
type Command struct {
	// Label as it appears in the menu. Title-style capitalisation, and an
	// ellipsis where choosing it opens something that asks for more.
	Label string
	// ID the frontend dispatches on. Matches a module manifest's command id.
	ID string
	// Accelerator in Wails' own syntax, or "" for none. "CmdOrCtrl" so the
	// same list gives ⌘ on macOS and Ctrl elsewhere.
	Accelerator string
	// Separator asks for a divider ABOVE this item.
	Separator bool
}

// DatabaseCommands is the Database menu.
//
// Every accelerator here is chosen against the platform defaults the roles
// install: ⌘N, ⌘W, ⌘Q, ⌘A, ⌘S, ⌘X and the rest already mean something, so the
// module's own verbs take the shifted forms.
var DatabaseCommands = []Command{
	{Label: "New Console", ID: "database.console.new", Accelerator: "CmdOrCtrl+T"},
	{Label: "New Connection…", ID: "database.connection.new", Accelerator: "CmdOrCtrl+Shift+N"},
	{Label: "Run Query", ID: "database.query", Accelerator: "CmdOrCtrl+Return", Separator: true},
	{Label: "Re-index Schema", ID: "database.schema.reindex", Accelerator: "CmdOrCtrl+Shift+R"},
	{Label: "ERD Diagram", ID: "database.erd", Accelerator: "CmdOrCtrl+Shift+E", Separator: true},
	{Label: "Live Activity", ID: "database.activity", Accelerator: "CmdOrCtrl+Shift+A"},
	{Label: "Query History", ID: "database.history", Accelerator: "CmdOrCtrl+Shift+Y"},
	{Label: "Saved Queries", ID: "database.saved", Accelerator: "CmdOrCtrl+Shift+B"},
}

// Submenus is every menu DevKit adds to the platform defaults, in bar order.
var Submenus = []struct {
	Title    string
	Commands []Command
}{
	{Title: "Database", Commands: DatabaseCommands},
}

// Attach adds DevKit's own menus to `menu` and returns it.
//
// Takes the base menu rather than building it, because
// application.DefaultApplicationMenu() reaches for the running application and
// panics without one — so a Build that called it could never be exercised off
// a live app. main.go passes the defaults in; a test passes an empty menu and
// checks the part this package is actually responsible for.
//
// `emit` is called with a Command ID when an item is chosen.
func Attach(menu *application.Menu, emit func(id string)) *application.Menu {
	for _, submenu := range Submenus {
		target := menu.AddSubmenu(submenu.Title)
		for _, command := range submenu.Commands {
			if command.Separator {
				target.AddSeparator()
			}
			id := command.ID
			item := target.Add(command.Label).OnClick(func(*application.Context) { emit(id) })
			if command.Accelerator != "" {
				item.SetAccelerator(command.Accelerator)
			}
		}
	}
	return menu
}

// CommandIDs is every id the menu can emit, for the parity check that keeps
// the menu and the frontend's command table describing the same actions.
func CommandIDs() []string {
	var ids []string
	for _, submenu := range Submenus {
		for _, command := range submenu.Commands {
			ids = append(ids, command.ID)
		}
	}
	return ids
}
