//go:build linux

package sandbox

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var bwrapSystemReadOnlyPaths = []string{"/usr", "/bin", "/lib", "/lib64", "/etc"}

func buildBwrapArgs(p Policy, c Cmd) []string {
	p = p.withDefaults()

	args := []string{
		"--unshare-user",
		"--unshare-pid",
		"--unshare-ipc",
		"--die-with-parent",
	}
	if p.Network == NetworkNone {
		args = append(args, "--unshare-net")
	}

	args = append(args, "--dev", "/dev", "--proc", "/proc")
	for _, path := range bwrapSystemReadOnlyPaths {
		if pathExists(path) {
			args = append(args, "--ro-bind", path, path)
		}
	}

	args = append(args, "--bind", p.WorkDir, p.WorkDir)
	for _, path := range p.ReadWritePaths {
		if pathExists(path) {
			args = append(args, "--bind", path, path)
		}
	}
	for _, path := range p.ReadOnlyPaths {
		if pathExists(path) {
			args = append(args, "--ro-bind", path, path)
		}
	}

	denyPaths := p.EffectiveDenyRead()
	for _, path := range denyPaths {
		appendDenyReadMount(&args, path)
	}

	home := policyHome(p)
	if p.EffectivePrivateHome() && home != "" {
		args = append(args, "--tmpfs", home)
		appendHomeAllowlist(&args, home, denyPaths, p.ReadWritePaths, "--bind")
		appendHomeAllowlist(&args, home, denyPaths, p.ReadOnlyPaths, "--ro-bind")
	}

	args = appendEnvArgs(args, p.Env)
	args = append(args, "--chdir", p.WorkDir, "--", c.Path)
	args = append(args, c.Args...)
	return args
}

func appendDenyReadMount(args *[]string, path string) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if info.IsDir() {
		*args = append(*args, "--tmpfs", path)
		return
	}
	*args = append(*args, "--bind", "/dev/null", path)
}

func appendHomeAllowlist(args *[]string, home string, denyPaths []string, paths []string, flag string) {
	for _, path := range paths {
		if !pathExists(path) || !pathIsUnder(path, home) || pathIsDenied(path, denyPaths) {
			continue
		}
		*args = append(*args, flag, path, path)
	}
}

func appendEnvArgs(args []string, env map[string]string) []string {
	args = append(args, "--clearenv")
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		args = append(args, "--setenv", key, env[key])
	}
	return args
}

func policyHome(p Policy) string {
	if p.Env != nil && p.Env["HOME"] != "" {
		return p.Env["HOME"]
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func pathIsUnder(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func pathIsDenied(path string, denyPaths []string) bool {
	for _, denyPath := range denyPaths {
		if path == denyPath || pathIsUnder(path, denyPath) {
			return true
		}
	}
	return false
}
