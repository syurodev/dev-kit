package appmenu

import (
	"strings"
	"testing"
)

// Two items sharing a shortcut is a shortcut that does whichever the platform
// decides, which is not a decision this project gets to leave to it.
func TestAcceleratorsAndIDsAreUnique(t *testing.T) {
	accelerators := map[string]string{}
	ids := map[string]bool{}
	for _, submenu := range Submenus {
		for _, command := range submenu.Commands {
			if command.Label == "" || command.ID == "" {
				t.Fatalf("command %+v is missing a label or an id", command)
			}
			if ids[command.ID] {
				t.Fatalf("duplicate command id %q", command.ID)
			}
			ids[command.ID] = true
			if command.Accelerator == "" {
				continue
			}
			if other, clash := accelerators[command.Accelerator]; clash {
				t.Fatalf("%q and %q both bind %s", other, command.Label, command.Accelerator)
			}
			accelerators[command.Accelerator] = command.Label
		}
	}
}

// The platform's own shortcuts are installed by the default roles. Binding one
// of them a second time is how an app breaks Select All.
func TestAcceleratorsAvoidThePlatformDefaults(t *testing.T) {
	reserved := []string{
		"CmdOrCtrl+N", "CmdOrCtrl+O", "CmdOrCtrl+S", "CmdOrCtrl+W", "CmdOrCtrl+Q",
		"CmdOrCtrl+A", "CmdOrCtrl+C", "CmdOrCtrl+V", "CmdOrCtrl+X", "CmdOrCtrl+Z",
		"CmdOrCtrl+M", "CmdOrCtrl+H", "CmdOrCtrl+,",
	}
	for _, submenu := range Submenus {
		for _, command := range submenu.Commands {
			for _, taken := range reserved {
				if strings.EqualFold(command.Accelerator, taken) {
					t.Errorf("%q binds %s, which the platform already uses", command.Label, taken)
				}
			}
		}
	}
}

// The menu is what the bar shows; the default roles have to survive being
// appended to, or DevKit's menu would arrive at the cost of Edit.
// Every item has to name the module that declares it, or internal/app reports
// a set of commands that is missing one and the shell says the metadata is out
// of sync.
func TestEveryCommandNamesItsModule(t *testing.T) {
	for _, submenu := range Submenus {
		for _, command := range submenu.Commands {
			if command.Module == "" {
				t.Errorf("%q names no module", command.Label)
			}
		}
	}
}

// Close Tab and the moved Close Window are bound in main.go, outside Submenus,
// so the uniqueness check above cannot see them. No module command may take
// either key.
func TestCloseTabKeysAreFree(t *testing.T) {
	for _, submenu := range Submenus {
		for _, command := range submenu.Commands {
			for _, taken := range []string{CloseTabAccelerator, CloseWindowAccelerator} {
				if strings.EqualFold(command.Accelerator, taken) {
					t.Errorf("%q binds %s, which Close Tab / Close Window use", command.Label, taken)
				}
			}
			if command.ID == CloseTabID {
				t.Errorf("%q reuses the shell's Close Tab id", command.Label)
			}
		}
	}
}

func TestModuleCommandIDsIsTheModulesOwn(t *testing.T) {
	ids := ModuleCommandIDs("database")
	if len(ids) == 0 {
		t.Fatal("the database module has no commands")
	}
	for _, id := range ids {
		if !strings.HasPrefix(id, "database.") {
			t.Errorf("%q is reported as a database command", id)
		}
	}
	if len(ids) != len(CommandIDs()) {
		t.Fatalf("database has %d of %d commands; split the assertion once a second module has a menu",
			len(ids), len(CommandIDs()))
	}
	// A module with no menu gets an empty slice, not nil: this goes straight
	// into JSON the frontend reads, where null and [] are different answers.
	if other := ModuleCommandIDs("notes"); other == nil || len(other) != 0 {
		t.Fatalf("ModuleCommandIDs(\"notes\") = %#v, want an empty slice", other)
	}
}
