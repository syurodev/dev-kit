package agenthub

import (
	"os"
	"path/filepath"
	"runtime"
)

// System developer tools are a separate, read-only safety preference. They
// are useful with either a private home or a shared development environment.
func developerToolchainRoots() []string {
	if runtime.GOOS != "darwin" {
		return nil
	}
	candidates := []string{"/Library/Developer", "/private/var/db/xcode_select_link"}
	apps, _ := filepath.Glob("/Applications/Xcode*.app")
	candidates = append(candidates, apps...)
	var roots []string
	for _, path := range candidates {
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink == 0 && (info.IsDir() || path == "/private/var/db/xcode_select_link") {
			roots = append(roots, path)
		}
	}
	return roots
}
