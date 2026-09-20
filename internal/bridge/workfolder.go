// internal/bridge/workfolder.go
package bridge

import (
	"devkit/internal/workfolder"
)

// WorkFolderState is the folder the app is working in and the modules that have
// been pointed somewhere else. The frontend reads it once on start and then
// follows the workfolder:changed event.
func (s *AppService) WorkFolderState() workfolder.State {
	return s.container.WorkFolder.State()
}

// WorkFolderPin points one module at a folder, whatever the current folder is.
// It is the module-level "open a different folder": Code Graph is not moved,
// so nothing is re-indexed.
func (s *AppService) WorkFolderPin(module, folder string) error {
	return s.container.WorkFolder.Pin(module, folder)
}

// WorkFolderFollow sends a module back to following the current folder.
func (s *AppService) WorkFolderFollow(module string) {
	s.container.WorkFolder.Follow(module)
}
