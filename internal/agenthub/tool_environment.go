package agenthub

import (
	"os"
	"path/filepath"
	"strings"
)

// A shared-home session is the user's development environment: CLIs and MCP
// servers need its proxy settings, language runtimes and authentication env.
// Private-home sessions retain the narrow vendor allowlist. Never log values.
func inheritToolEnvironment(env map[string]string, spec OpenSpec) {
	if spec.PrivateHome == nil || *spec.PrivateHome {
		return
	}
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			env[key] = value
		}
	}
}

// Apply after the managed runtime's PATH prefix and session overrides, so
// those do not accidentally discard the developer's additional tool bins.
func completeToolPath(env map[string]string, spec OpenSpec) {
	if spec.PrivateHome == nil || *spec.PrivateHome {
		return
	}
	home := env["HOME"]
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	env["PATH"] = developmentPath(env["PATH"], home)
}

// Keep the user's explicit order, then add existing common toolchain bins.
// Dock-launched apps often have only the system PATH. No shell startup scripts
// are executed, and no guessed runtime version is selected.
func developmentPath(current, home string) string {
	paths := filepath.SplitList(current)
	for _, dir := range []string{"/opt/homebrew/bin", "/opt/homebrew/sbin", "/usr/local/bin", "/usr/local/sbin",
		filepath.Join(home, ".local", "bin"), filepath.Join(home, ".bun", "bin"),
		filepath.Join(home, "go", "bin"), filepath.Join(home, ".cargo", "bin"),
		filepath.Join(home, ".volta", "bin"), filepath.Join(home, ".asdf", "shims"), filepath.Join(home, ".pyenv", "shims")} {
		if !filepath.IsAbs(dir) {
			continue
		}
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			found := false
			for _, existing := range paths {
				if existing == dir {
					found = true
					break
				}
			}
			if !found {
				paths = append(paths, dir)
			}
		}
	}
	return strings.Join(paths, string(os.PathListSeparator))
}

func developmentToolRoots() []string {
	var roots []string
	for _, dir := range []string{"/opt/homebrew", "/usr/local"} {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			roots = append(roots, dir)
		}
	}
	return roots
}
