package app

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// How long macOS' `open` or `xdg-open` may take to hand the file to the file manager.
const revealTimeout = 10 * time.Second

// RevealInFileManager shows a file in the operating system's file manager:
// selected in Finder on macOS, selected in File Explorer on Windows, and — since
// the freedesktop file managers have no portable "select" — its folder opened
// elsewhere. The path must already be one DevKit resolved (WorkspaceCode.RevealFile).
func RevealInFileManager(path string) error {
	if runtime.GOOS == "windows" {
		// explorer exits 1 even when it worked, so it is started and not waited on for a status.
		cmd := exec.Command("explorer.exe", "/select,"+path)
		if err := cmd.Start(); err != nil {
			return err
		}
		go func() { _ = cmd.Wait() }()
		return nil
	}
	// open and xdg-open report a failure in their exit status, and return once the file manager has the request.
	ctx, cancel := context.WithTimeout(context.Background(), revealTimeout)
	defer cancel()
	if runtime.GOOS == "darwin" {
		return exec.CommandContext(ctx, "open", "-R", path).Run()
	}
	return exec.CommandContext(ctx, "xdg-open", filepath.Dir(path)).Run()
}
