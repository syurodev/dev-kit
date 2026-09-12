package appmenu

import (
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
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
func TestAttachAddsEveryModuleMenu(t *testing.T) {
	menu := Attach(application.NewMenu(), func(string) {})
	for _, submenu := range Submenus {
		if menu.FindByLabel(submenu.Title) == nil {
			t.Fatalf("menu is missing the %s submenu", submenu.Title)
		}
		for _, command := range submenu.Commands {
			item := menu.FindByLabel(command.Label)
			if item == nil {
				t.Fatalf("menu is missing %q", command.Label)
			}
			if got := item.GetAccelerator(); command.Accelerator != "" && got == "" {
				t.Errorf("%q lost its accelerator", command.Label)
			}
		}
	}
}
