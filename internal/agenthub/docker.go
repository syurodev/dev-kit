package agenthub

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// DockerGrant is what a jail needs for `docker` to work inside it: the
// daemon's socket, and — on macOS — the app bundle the CLI actually lives in.
type DockerGrant struct {
	// Socket is the daemon socket, symlinks resolved: the path the kernel
	// connects to, which is what Seatbelt matches and what bwrap should bind.
	// "" when no daemon socket was found.
	Socket string
	// ReadOnly are paths the CLI needs to be readable: Docker Desktop and
	// OrbStack install `docker` (and the compose/buildx plugins, and the
	// credential helper) as symlinks into /Applications/<App>.app, which no
	// jail grants on its own.
	ReadOnly []string
	// PathDirs are directories to put on PATH so `docker` is found at all. An
	// app opened from the Dock inherits launchd's PATH, which has neither
	// /usr/local/bin nor ~/.orbstack/bin.
	PathDirs []string
}

// DetectDocker finds the Docker daemon the user's own `docker` would talk
// to, in the order the CLI itself decides: DOCKER_HOST, then the current
// context (OrbStack switches it to "orbstack", Docker Desktop to "desktop-linux"
// or "desktop"), then the default socket, then the well-known sockets of each
// desktop runtime in case none of that is set up. home is the host $HOME;
// getenv is os.Getenv outside tests.
func DetectDocker(home string, getenv func(string) string) DockerGrant {
	var g DockerGrant
	g.Socket = detectDockerSocket(home, getenv)
	g.ReadOnly, g.PathDirs = dockerCLI(home, getenv)
	return g
}

func detectDockerSocket(home string, getenv func(string) string) string {
	if host := getenv("DOCKER_HOST"); host != "" {
		// A tcp:// or ssh:// DOCKER_HOST is reached over the network, which
		// the jail already has; there is no socket to grant.
		return unixSocket(host)
	}
	if host := dockerContextHost(home, getenv); host != "" {
		return unixSocket(host)
	}
	candidates := []string{"/var/run/docker.sock"}
	if home != "" {
		candidates = append(candidates,
			filepath.Join(home, ".orbstack", "run", "docker.sock"),   // OrbStack
			filepath.Join(home, ".docker", "run", "docker.sock"),     // Docker Desktop, macOS
			filepath.Join(home, ".docker", "desktop", "docker.sock"), // Docker Desktop, Linux
			filepath.Join(home, ".colima", "default", "docker.sock"),
			filepath.Join(home, ".colima", "docker.sock"),
			filepath.Join(home, ".rd", "docker.sock"), // Rancher Desktop
		)
	}
	if runtimeDir := getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		candidates = append(candidates,
			filepath.Join(runtimeDir, "docker.sock"), // rootless Docker
			filepath.Join(runtimeDir, "podman", "podman.sock"),
		)
	}
	for _, path := range candidates {
		if sock := existingSocket(path); sock != "" {
			return sock
		}
	}
	return ""
}

// dockerContextHost is the docker endpoint of the current context, or "" for
// the default context (or no Docker config at all). The context's metadata
// lives under a directory named by the SHA-256 of its name — the CLI's own
// layout, stable since contexts were introduced.
func dockerContextHost(home string, getenv func(string) string) string {
	configDir := getenv("DOCKER_CONFIG")
	if configDir == "" {
		if home == "" {
			return ""
		}
		configDir = filepath.Join(home, ".docker")
	}
	name := getenv("DOCKER_CONTEXT")
	if name == "" {
		var cfg struct {
			CurrentContext string `json:"currentContext"`
		}
		data, err := os.ReadFile(filepath.Join(configDir, "config.json"))
		if err != nil || json.Unmarshal(data, &cfg) != nil {
			return ""
		}
		name = cfg.CurrentContext
	}
	if name == "" || name == "default" {
		return ""
	}
	sum := sha256.Sum256([]byte(name))
	data, err := os.ReadFile(filepath.Join(configDir, "contexts", "meta", hex.EncodeToString(sum[:]), "meta.json"))
	if err != nil {
		return ""
	}
	var meta struct {
		Endpoints map[string]struct {
			Host string `json:"Host"`
		} `json:"Endpoints"`
	}
	if json.Unmarshal(data, &meta) != nil {
		return ""
	}
	return meta.Endpoints["docker"].Host
}

// unixSocket turns a unix:// endpoint into an existing socket's resolved
// path, or "".
func unixSocket(host string) string {
	path, ok := strings.CutPrefix(host, "unix://")
	if !ok || !filepath.IsAbs(path) {
		return ""
	}
	return existingSocket(path)
}

func existingSocket(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ""
	}
	info, err := os.Stat(resolved)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		return ""
	}
	return resolved
}

// dockerCLI finds the `docker` binary the way a terminal would and says what
// a jail needs to run it: the directory to put on PATH, and — when the binary
// resolves into a macOS app bundle — the bundle, read-only, since the CLI
// plugins and credential helpers resolve into it too.
func dockerCLI(home string, getenv func(string) string) (readOnly, pathDirs []string) {
	candidates := []string{}
	for _, dir := range filepath.SplitList(getenv("PATH")) {
		if dir != "" {
			candidates = append(candidates, filepath.Join(dir, "docker"))
		}
	}
	candidates = append(candidates, "/usr/local/bin/docker", "/opt/homebrew/bin/docker")
	if home != "" {
		candidates = append(candidates, filepath.Join(home, ".orbstack", "bin", "docker"))
	}
	if runtime.GOOS == "darwin" {
		candidates = append(candidates,
			"/Applications/Docker.app/Contents/Resources/bin/docker",
			"/Applications/OrbStack.app/Contents/MacOS/xbin/docker",
		)
	}
	for _, path := range candidates {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			continue
		}
		pathDirs = []string{filepath.Dir(path)}
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			if bundle := appBundle(resolved); bundle != "" {
				readOnly = append(readOnly, bundle)
			}
		}
		return readOnly, pathDirs
	}
	return nil, nil
}

// appBundle is the ".app" directory path sits in, or "".
func appBundle(path string) string {
	for dir := filepath.Dir(path); dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		if strings.HasSuffix(dir, ".app") {
			return dir
		}
	}
	return ""
}
