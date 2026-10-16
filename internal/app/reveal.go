package app

import (
	"os/exec"
	"path/filepath"
	"runtime"
)

// RevealInFileManager shows a file in the operating system's file manager:
// selected in Finder on macOS, selected in File Explorer on Windows, and — since
// the freedesktop file managers have no portable "select" — its folder opened
// elsewhere. The path must already be one DevKit resolved (WorkspaceCode.RevealFile).
func RevealInFileManager(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", "-R", path)
	case "windows":
		// explorer exits 1 even when it worked, so it is started and not waited on for a status.
		cmd = exec.Command("explorer.exe", "/select,"+path)
	default:
		cmd = exec.Command("xdg-open", filepath.Dir(path))
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
