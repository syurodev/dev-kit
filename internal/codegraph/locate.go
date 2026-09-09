// internal/codegraph/locate.go
package codegraph

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
)

// candidate is one place a program might live, with a name for where that is
// so the UI can say which one it picked. "Using the JDK bundled with your IDE"
// is a very different message from "java is missing", and a user who has
// IntelliJ installed deserves the first one.
type candidate struct {
	path   string
	source string
}

// maxCandidates bounds how many programs get their version probed. Each probe
// is a process, and a glob over a JDK directory on a busy machine can match
// more than anyone wants to spawn. Ordered best-guess first, so a truncated
// list is still the right list.
const maxCandidates = 24

// javaCandidates lists every java the machine plausibly has, ordered by how
// much the user has said about it.
//
// PATH is not enough, and assuming it was is the bug this function exists to
// fix. A very common setup — arguably the most common one among Java
// developers — is IntelliJ IDEA and nothing else: the IDE ships its own JetBrains
// Runtime and downloads project JDKs into ~/.jdks, and neither ends up on PATH.
// Reporting "java is not installed" to someone who has been writing Java all
// week is both wrong and unfixable-looking.
//
// Order: what the user configured (JAVA_HOME), then what the shell agrees with
// (PATH), then the IDE's own JDKs, then the system's. Within the list, the probe
// picks the NEWEST version that clears the minimum rather than the first match,
// because ~/.jdks routinely holds a 17 next to a 21.
func javaCandidates(_ string) []candidate {
	exe := programName("java")
	var found []candidate

	add := func(source string, paths ...string) {
		for _, path := range paths {
			if path != "" {
				found = append(found, candidate{path: path, source: source})
			}
		}
	}
	addGlob := func(source string, patterns ...string) {
		for _, pattern := range patterns {
			matches, err := filepath.Glob(pattern)
			if err != nil {
				continue
			}
			// Newest-looking first: jdk-21 should be probed before jdk-17 so a
			// truncated candidate list still contains the one that will win.
			sort.Sort(sort.Reverse(sort.StringSlice(matches)))
			add(source, matches...)
		}
	}

	if home := os.Getenv("JAVA_HOME"); home != "" {
		add("JAVA_HOME", filepath.Join(home, "bin", exe))
	}
	if path, err := exec.LookPath(exe); err == nil {
		add("PATH", path)
	}

	home, _ := os.UserHomeDir()
	if home != "" {
		// Where IntelliJ downloads and manages project JDKs, on every platform.
		addGlob("IDE-managed JDK", filepath.Join(home, ".jdks", "*", "bin", exe))
		// SDKMAN, the other common way a developer has a JDK that PATH may or
		// may not know about.
		add("SDKMAN", filepath.Join(home, ".sdkman", "candidates", "java", "current", "bin", exe))
	}

	switch runtime.GOOS {
	case "darwin":
		if home != "" {
			addGlob("IDE-managed JDK", filepath.Join(home, "Library", "Java", "JavaVirtualMachines", "*", "Contents", "Home", "bin", exe))
			addGlob("JetBrains IDE runtime", filepath.Join(home, "Applications", "*.app", "Contents", "jbr", "Contents", "Home", "bin", exe))
		}
		addGlob("JetBrains IDE runtime", filepath.Join("/Applications", "*.app", "Contents", "jbr", "Contents", "Home", "bin", exe))
		addGlob("system JDK", filepath.Join("/Library", "Java", "JavaVirtualMachines", "*", "Contents", "Home", "bin", exe))
	case "windows":
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			addGlob("JetBrains IDE runtime",
				filepath.Join(local, "Programs", "*", "jbr", "bin", exe),
				filepath.Join(local, "JetBrains", "Toolbox", "apps", "*", "jbr", "bin", exe),
				filepath.Join(local, "JetBrains", "Toolbox", "apps", "*", "*", "*", "jbr", "bin", exe),
			)
		}
		for _, programFiles := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
			if programFiles != "" {
				addGlob("system JDK", filepath.Join(programFiles, "Java", "*", "bin", exe))
				addGlob("JetBrains IDE runtime", filepath.Join(programFiles, "JetBrains", "*", "jbr", "bin", exe))
			}
		}
	default: // linux and the other unixes
		if home != "" {
			addGlob("JetBrains IDE runtime",
				filepath.Join(home, ".local", "share", "JetBrains", "Toolbox", "apps", "*", "jbr", "bin", exe),
				filepath.Join(home, ".local", "share", "JetBrains", "Toolbox", "apps", "*", "*", "*", "jbr", "bin", exe),
			)
		}
		addGlob("JetBrains IDE runtime",
			filepath.Join("/opt", "*", "jbr", "bin", exe),
			filepath.Join("/snap", "intellij-idea-*", "current", "jbr", "bin", exe),
		)
		addGlob("system JDK",
			filepath.Join("/usr", "lib", "jvm", "*", "bin", exe),
			filepath.Join("/usr", "lib", "jvm", "*", "jre", "bin", exe),
		)
	}

	return executable(found)
}

// mavenCandidates lists every Maven the machine plausibly has.
//
// Same reasoning as javaCandidates, with one addition that comes first: a
// project's own Maven Wrapper. If analyzer/ ever grows an mvnw, it is by
// definition the Maven this build was meant to run with, and it needs no Maven
// installed at all — only a JDK. Checking for it costs one stat and means
// adding the wrapper later requires no change here.
func mavenCandidates(dir string) []candidate {
	exe := programName("mvn")
	var found []candidate

	add := func(source string, paths ...string) {
		for _, path := range paths {
			if path != "" {
				found = append(found, candidate{path: path, source: source})
			}
		}
	}
	addGlob := func(source string, patterns ...string) {
		for _, pattern := range patterns {
			matches, err := filepath.Glob(pattern)
			if err != nil {
				continue
			}
			sort.Sort(sort.Reverse(sort.StringSlice(matches)))
			add(source, matches...)
		}
	}

	if dir != "" {
		wrapper := "mvnw"
		if runtime.GOOS == "windows" {
			wrapper = "mvnw.cmd"
		}
		add("Maven Wrapper", filepath.Join(dir, wrapper))
	}
	for _, env := range []string{"MAVEN_HOME", "M2_HOME"} {
		if home := os.Getenv(env); home != "" {
			add(env, filepath.Join(home, "bin", exe))
		}
	}
	if path, err := exec.LookPath(exe); err == nil {
		add("PATH", path)
	}

	home, _ := os.UserHomeDir()
	if home != "" {
		add("SDKMAN", filepath.Join(home, ".sdkman", "candidates", "maven", "current", "bin", exe))
	}

	// IntelliJ bundles a full Maven under its maven plugin. This is the one
	// that makes "I only have IntelliJ" a working setup rather than a blocked
	// one.
	const bundled = "plugins/maven/lib/maven3/bin"
	switch runtime.GOOS {
	case "darwin":
		if home != "" {
			addGlob("IDE-bundled Maven", filepath.Join(home, "Applications", "*.app", "Contents", bundled, exe))
		}
		addGlob("IDE-bundled Maven", filepath.Join("/Applications", "*.app", "Contents", bundled, exe))
		add("Homebrew", filepath.Join("/opt", "homebrew", "bin", exe), filepath.Join("/usr", "local", "bin", exe))
	case "windows":
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			addGlob("IDE-bundled Maven",
				filepath.Join(local, "Programs", "*", bundled, exe),
				filepath.Join(local, "JetBrains", "Toolbox", "apps", "*", bundled, exe),
				filepath.Join(local, "JetBrains", "Toolbox", "apps", "*", "*", "*", bundled, exe),
			)
		}
		if programFiles := os.Getenv("ProgramFiles"); programFiles != "" {
			addGlob("IDE-bundled Maven", filepath.Join(programFiles, "JetBrains", "*", bundled, exe))
		}
	default:
		if home != "" {
			addGlob("IDE-bundled Maven",
				filepath.Join(home, ".local", "share", "JetBrains", "Toolbox", "apps", "*", bundled, exe),
				filepath.Join(home, ".local", "share", "JetBrains", "Toolbox", "apps", "*", "*", "*", bundled, exe),
			)
		}
		addGlob("IDE-bundled Maven",
			filepath.Join("/opt", "*", bundled, exe),
			filepath.Join("/snap", "intellij-idea-*", "current", bundled, exe),
			filepath.Join("/usr", "share", "*", bundled, exe),
		)
	}

	return executable(found)
}

// executable drops candidates that are not there or not runnable, de-duplicates
// what is left by path, and caps the result.
func executable(candidates []candidate) []candidate {
	seen := make(map[string]bool, len(candidates))
	kept := make([]candidate, 0, len(candidates))
	for _, c := range candidates {
		resolved, err := filepath.Abs(c.path)
		if err != nil {
			resolved = c.path
		}
		if seen[resolved] {
			continue
		}
		info, err := os.Stat(resolved)
		if err != nil || info.IsDir() {
			continue
		}
		// The executable bit does not exist on Windows, where being there and
		// having the right extension is the whole test.
		if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
			continue
		}
		seen[resolved] = true
		c.path = resolved
		kept = append(kept, c)
		if len(kept) == maxCandidates {
			break
		}
	}
	return kept
}

func programName(base string) string {
	if runtime.GOOS == "windows" {
		// java is a real .exe; mvn ships as a .cmd batch script.
		if base == "java" {
			return base + ".exe"
		}
		return base + ".cmd"
	}
	return base
}
