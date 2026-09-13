// Package appmenu is the application menu, as data.
//
// It exists because DevKit had none of its own. Wails installs a default
// App/File/Edit/View/Window/Help menu when an app sets no menu, so copy, paste
// and close-window always worked — but nothing DevKit itself does was in the
// menu bar, and on macOS that is where people look for it and where ⌃F2 and
// VoiceOver go to enumerate what an app can do.
//
// The items are data, not code, and this package imports nothing: a menu is
// built once at startup, before the frontend exists, so the labels and the
// shortcuts live here; the ID is all that crosses over, and the frontend
// decides what running it means. main.go turns this into a real menu.
//
// No import of the Wails application package on purpose. This list is also
// what internal/app reports as a module's commands — the frontend compares the
// two at startup and says "Module metadata is unavailable or out of sync" when
// they differ — and internal/app must stay free of cgo and of a window
// toolkit.
package appmenu

// EventName is the event a menu selection emits. The payload is the Command ID.
const EventName = "devkit:command"

// Close Tab takes ⌘W, and Close Window moves to ⇧⌘W, as in every editor with
// tabs. The platform's default roles bind ⌘W to Close Window (File on macOS,
// Window elsewhere), and a native accelerator fires before the page sees the
// key, so main.go makes the swap in the menu itself.
//
// Not in Submenus: tabs belong to the shell, not to a module, so this is no
// module's command and no module's metadata reports it.
const (
	CloseTabID             = "workbench.tab.close"
	CloseTabLabel          = "Close Tab"
	CloseTabAccelerator    = "CmdOrCtrl+W"
	CloseWindowAccelerator = "CmdOrCtrl+Shift+W"
)

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
	// Module that declares it. The module's own metadata reports these ids,
	// so the menu and the manifest cannot describe different sets.
	Module string
}

// TabSwitcherAccelerator is the workbench's tab switcher (Workbench.tsx), held
// in the webview rather than the menu. A native accelerator is handled before
// the webview sees the key, so a menu item on the same chord swallows it: ERD
// Diagram sat on ⇧⌘E and the switcher never opened from the keyboard (#44).
const TabSwitcherAccelerator = "CmdOrCtrl+Shift+E"

// DatabaseCommands is the Database menu.
//
// Every accelerator here is chosen against the platform defaults the roles
// install: ⌘N, ⌘W, ⌘Q, ⌘A, ⌘S, ⌘X and the rest already mean something, so the
// module's own verbs take the shifted forms.
var DatabaseCommands = []Command{
	{Module: "database", Label: "New Console", ID: "database.console.new", Accelerator: "CmdOrCtrl+T"},
	{Module: "database", Label: "New Connection…", ID: "database.connection.new", Accelerator: "CmdOrCtrl+Shift+N"},
	{Module: "database", Label: "Run Query", ID: "database.query", Accelerator: "CmdOrCtrl+Return", Separator: true},
	{Module: "database", Label: "Re-index Schema", ID: "database.schema.reindex", Accelerator: "CmdOrCtrl+Shift+R"},
	{Module: "database", Label: "ERD Diagram", ID: "database.erd", Accelerator: "CmdOrCtrl+Shift+D", Separator: true},
	{Module: "database", Label: "Live Activity", ID: "database.activity", Accelerator: "CmdOrCtrl+Shift+A"},
	{Module: "database", Label: "Query History", ID: "database.history", Accelerator: "CmdOrCtrl+Shift+Y"},
	{Module: "database", Label: "Saved Queries", ID: "database.saved", Accelerator: "CmdOrCtrl+Shift+B"},
}

// Submenus is every menu DevKit adds to the platform defaults, in bar order.
var Submenus = []struct {
	Title    string
	Commands []Command
}{
	{Title: "Database", Commands: DatabaseCommands},
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

// ModuleCommandIDs is what a module reports as its commands.
//
// One list, read from both ends: the menu bar builds its items from it and
// internal/app hands the same ids to the frontend, which checks them against
// the manifest on every launch. Before this, the ids were typed out in three
// places and the third one was wrong — seven menu items pointed at commands
// the backend had never heard of, and the shell said so.
func ModuleCommandIDs(moduleID string) []string {
	ids := []string{}
	for _, submenu := range Submenus {
		for _, command := range submenu.Commands {
			if command.Module == moduleID {
				ids = append(ids, command.ID)
			}
		}
	}
	return ids
}
