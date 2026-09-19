// internal/codegraph/toolchain_ts.go
package codegraph

import (
	"path/filepath"
	"regexp"
	"runtime"
)

// Everything CodeGraph knows that is specific to TypeScript as a PROJECT: what
// makes a folder one, and how its analyzer is built and launched. How the
// analysis works is analyzer_ts*.go.
//
// This is the third shape a Toolchain takes, and it is the good one. Java's
// artifact is a jar that needs a JDK to launch, so `Ready` means artifact AND
// runtime and a user without a JDK gets a setup panel. Lua's analyzer is
// `Native` and needs nothing at all. TypeScript's artifact is `tsgo` — the
// TypeScript compiler rewritten in Go, and therefore a self-contained
// executable that cross-compiles beside DevKit — so the artifact IS the
// runtime. `Run[0]` is ArtifactArg rather than the name of a requirement, and
// nothing has to be installed to RUN it. Go is a requirement only to BUILD it,
// which is the from-source path, exactly as Maven is for Java.

// tsgoBinary is what analyzer-ts's build writes. Change it there, change it here.
var tsgoBinary = programName("tsgo", nativeProgram)

// TypeScriptToolchain describes the TypeScript analyzer.
//
// The Go floor is typescript-go's, not ours: analyzer-ts/go.mod inherits it
// from the pinned compiler, which needs 1.26 to build. What that binary can
// then READ is every version of TypeScript it implements, and the two are
// independent — the same split the Java toolchain keeps between the JDK that
// runs the analyzer and the language level it parses.
func TypeScriptToolchain(repoRoot string) Toolchain {
	buildOutput := filepath.Join(repoRoot, "analyzer-ts", "bin", tsgoBinary)
	return Toolchain{
		Language: "typescript",
		Label:    "TypeScript",
		Artifact: buildOutput,
		Search:   analyzerSearchPaths("typescript", tsgoBinary, buildOutput),
		Dir:      filepath.Join(repoRoot, "analyzer-ts"),
		Build: []string{
			"go", "build", "-ldflags=-s -w",
			"-o", filepath.Join("bin", tsgoBinary),
			"github.com/microsoft/typescript-go/cmd/tsgo",
		},
		// The artifact launches itself. -stdio is how tsgo's language server
		// is told to speak over the pipes rather than a socket.
		Run:   []string{ArtifactArg, "--lsp", "-stdio"},
		Serve: NewTypeScriptAnalyzer,
		// What marks a module root. tsconfig.json is the build file; a
		// package.json is what a workspace package has even without one, which
		// is how a monorepo registers as one module per package rather than as
		// a single confusing root.
		Markers: []string{
			"tsconfig.json", "tsconfig.base.json", "jsconfig.json", "package.json",
		},
		SourceExt:    ".ts",
		Requirements: []Requirement{goRequirement()},
	}
}

func goRequirement() Requirement {
	return Requirement{
		Program:     "go",
		Label:       "Go toolchain",
		MinVersion:  "1.26",
		locate:      goCandidates,
		versionArgs: []string{"version"},
		// "go version go1.26.8 linux/amd64"
		versionPattern: regexp.MustCompile(`go([0-9][0-9.]*)`),
		InstallHint: "Only needed to BUILD the analyzer, never to run it — the built binary is self-contained. " +
			"A packaged DevKit ships it already built, and anyone working on DevKit itself has Go anyway.",
		Install: goInstallCommands(),
	}
}

func goInstallCommands() []InstallCommand {
	switch runtime.GOOS {
	case "darwin":
		return []InstallCommand{
			{Label: "Homebrew", Command: "brew install go"},
			{Label: "Download", Command: "open https://go.dev/dl/"},
		}
	case "windows":
		return []InstallCommand{
			{Label: "winget", Command: "winget install GoLang.Go"},
			{Label: "Chocolatey", Command: "choco install golang"},
		}
	default:
		return []InstallCommand{
			{Label: "Download", Command: "curl -LO https://go.dev/dl/ && tar -C /usr/local -xzf go*.tar.gz"},
			{Label: "Debian / Ubuntu", Command: "sudo apt install golang-go"},
			{Label: "Arch", Command: "sudo pacman -S go"},
		}
	}
}

// goCandidates lists every Go the machine plausibly has.
//
// Shorter than javaCandidates on purpose: Go's whole distribution story is one
// tarball on PATH, and the version managers that exist put their shims there
// too. What PATH misses is the official installer's fixed location and the two
// version managers that do not shim.
func goCandidates(_ string) []candidate {
	exe := programName("go", nativeProgram)
	home := userHome()
	var s search

	s.envHome(exe, "GOROOT")
	s.onPath(exe)

	if home != "" {
		s.glob("gvm", filepath.Join(home, ".gvm", "gos", "*", "bin", exe))
		s.glob("asdf", filepath.Join(home, ".asdf", "installs", "golang", "*", "go", "bin", exe))
		s.at("mise", filepath.Join(home, ".local", "share", "mise", "installs", "go", "latest", "bin", exe))
		s.at("user install", filepath.Join(home, "go", "bin", exe), filepath.Join(home, "sdk", "go", "bin", exe))
		s.glob("user install", filepath.Join(home, "sdk", "go*", "bin", exe))
	}

	switch runtime.GOOS {
	case "darwin":
		s.at("official installer", filepath.Join("/usr", "local", "go", "bin", exe))
		s.at("Homebrew", filepath.Join("/opt", "homebrew", "bin", exe))
	case "windows":
		for _, local := range windowsAppRoots() {
			s.at("official installer", filepath.Join(local, "Programs", "Go", "bin", exe))
		}
		s.at("official installer", filepath.Join(`C:\`, "Go", "bin", exe))
	default:
		s.at("official installer", filepath.Join("/usr", "local", "go", "bin", exe))
		s.at("system", filepath.Join("/usr", "lib", "go", "bin", exe))
	}

	return s.results()
}
