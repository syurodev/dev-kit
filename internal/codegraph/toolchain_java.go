// internal/codegraph/toolchain_java.go
package codegraph

import (
	"path/filepath"
	"regexp"
	"runtime"
)

// Everything CodeGraph knows that is specific to Java lives in this file: how
// its analyzer is built, what that build needs, where a JDK and a Maven hide on
// a developer's machine, and how the built artifact is run.
//
// That is the point of the split. toolchain.go, locate.go and artifact.go carry
// none of it, so a second language is a second file shaped like this one plus a
// line in DefaultToolchains — no change to the machinery, the bridge or the UI.

// javaAnalyzerJar is analyzer/pom.xml's finalName plus .jar. Change it there,
// change it here.
const javaAnalyzerJar = "codegraph-analyzer.jar"

// JavaToolchain describes the Java analyzer.
//
// The 21 floor is not a guess and not the same thing as the Java the analyzer
// can read. analyzer/pom.xml sets maven.compiler.release to 21, so the jar's
// own bytecode needs a 21+ runtime — which is what keeps it working on the JDK
// people actually have, an IDE's bundled runtime or a distro's openjdk-21. What
// it can PARSE is ParserConfiguration.LanguageLevel in ProjectAnalyzer.java,
// currently JAVA_25, and that is independent: JavaParser is a parser, not
// javac, so reading Java 25 source needs no JDK 25 anywhere.
//
// A newer JDK is fine to build and run with. When a newer Java ships, raise the
// language level, not this floor.
//
// repoRoot is where the analyzer source lives, which for a process started from
// the repository root is ".". A packaged install has no source to build and
// finds its artifact through Search instead.
func JavaToolchain(repoRoot string) Toolchain {
	return Toolchain{
		Language: "java",
		Label:    "Java",
		Artifact: filepath.Join(repoRoot, "analyzer", "target", javaAnalyzerJar),
		// A packaged DevKit ships this jar and never builds it; building is the
		// path for people running from source. See analyzerSearchPaths.
		Search: analyzerSearchPaths("java", javaAnalyzerJar,
			filepath.Join(repoRoot, "analyzer", "target", javaAnalyzerJar)),
		Dir: filepath.Join(repoRoot, "analyzer"),
		// Build[0] and Run[0] name requirements, not binaries: they are
		// replaced with the paths those requirements resolved to, so a Maven
		// found inside IntelliJ is invoked where it actually lives rather than
		// looked up on a PATH it is not on.
		//
		// -B keeps Maven's output line-oriented instead of redrawing a progress
		// bar with carriage returns, which is what makes it readable streamed
		// into a log view a line at a time.
		Build: []string{"mvn", "-B", "-DskipTests", "package"},
		Run:   []string{"java", "-jar", ArtifactArg, ProjectArg, OutputArg},
		// Maven finds its JDK through JAVA_HOME. Set from whichever java the
		// probe settled on, so a Maven bundled inside an IDE builds with the
		// IDE's JDK without either being on PATH.
		HomeEnv: map[string]string{"JAVA_HOME": "java"},
		// Every build system a Java project plausibly uses. One of these in a
		// directory makes it a module, so a monorepo of services registers as
		// one module per service rather than as a single confusing root.
		Markers: []string{
			"pom.xml",
			"build.gradle", "build.gradle.kts",
			"settings.gradle", "settings.gradle.kts",
			"build.xml",   // Ant
			"BUILD.bazel", // Bazel
		},
		SourceExt:    ".java",
		Requirements: []Requirement{javaRequirement(), mavenRequirement()},
	}
}

func javaRequirement() Requirement {
	return Requirement{
		Program:    "java",
		Label:      "Java Development Kit",
		MinVersion: "21",
		locate:     javaCandidates,
		// java writes its version to stderr, hence CombinedOutput in the probe.
		// The quoted form covers both the modern "21.0.10" and the legacy
		// "1.8.0_402", which compares correctly against 21 without
		// special-casing: 1 < 21.
		versionArgs:    []string{"-version"},
		versionPattern: regexp.MustCompile(`version "([0-9][0-9._]*)"`),
		InstallHint:    "IntelliJ users already have one — File → Project Structure → SDKs, or let the IDE download a JDK into ~/.jdks. DevKit finds it there without any of this.",
		Install:        javaInstallCommands(),
	}
}

// javaInstallCommands are the ways to get a JDK 21+ on the platform DevKit is
// running on. Only that platform: a Linux user reading a brew command is
// reading noise, and the process already knows which one it is.
func javaInstallCommands() []InstallCommand {
	switch runtime.GOOS {
	case "darwin":
		return []InstallCommand{
			{Label: "Homebrew", Command: "brew install openjdk@21"},
			{Label: "SDKMAN", Command: "sdk install java 21-tem"},
		}
	case "windows":
		return []InstallCommand{
			{Label: "winget", Command: "winget install EclipseAdoptium.Temurin.21.JDK"},
			{Label: "Chocolatey", Command: "choco install temurin21"},
		}
	default:
		return []InstallCommand{
			{Label: "Debian / Ubuntu", Command: "sudo apt install openjdk-21-jdk"},
			{Label: "Fedora / RHEL", Command: "sudo dnf install java-21-openjdk-devel"},
			{Label: "Arch", Command: "sudo pacman -S jdk21-openjdk"},
		}
	}
}

func mavenRequirement() Requirement {
	return Requirement{
		Program:    "mvn",
		Label:      "Apache Maven",
		MinVersion: "3.6",
		locate:     mavenCandidates,
		// maven-shade-plugin 3.6.0 in analyzer/pom.xml needs 3.6.3+; 3.6 is the
		// floor checked here, close enough to catch the genuinely ancient ones
		// without failing a working machine.
		versionArgs:    []string{"-version"},
		versionPattern: regexp.MustCompile(`Apache Maven ([0-9][0-9.]*)`),
		InstallHint:    "IntelliJ bundles a Maven of its own — DevKit uses it automatically if it can find the IDE, so installing one is only needed when it cannot.",
		Install:        mavenInstallCommands(),
	}
}

func mavenInstallCommands() []InstallCommand {
	switch runtime.GOOS {
	case "darwin":
		return []InstallCommand{
			{Label: "Homebrew", Command: "brew install maven"},
			{Label: "SDKMAN", Command: "sdk install maven"},
		}
	case "windows":
		return []InstallCommand{
			{Label: "winget", Command: "winget install Apache.Maven"},
			{Label: "Chocolatey", Command: "choco install maven"},
		}
	default:
		return []InstallCommand{
			{Label: "Debian / Ubuntu", Command: "sudo apt install maven"},
			{Label: "Fedora / RHEL", Command: "sudo dnf install maven"},
			{Label: "Arch", Command: "sudo pacman -S maven"},
		}
	}
}

// javaCandidates lists every java the machine plausibly has, in preference
// order: what the user configured, then what the shell agrees with, then the
// IDE's own JDKs, then the system's.
//
// The IDE entries are the ones that matter. A very common setup — arguably the
// most common among Java developers — is IntelliJ IDEA and nothing else: it
// ships its own JetBrains Runtime and downloads project JDKs into ~/.jdks, and
// neither reaches PATH.
func javaCandidates(_ string) []candidate {
	exe := programName("java", nativeProgram)
	home := userHome()
	var s search

	s.envHome(exe, "JAVA_HOME")
	s.onPath(exe)

	if home != "" {
		// Where IntelliJ downloads and manages project JDKs, on every platform.
		s.glob("IDE-managed JDK", filepath.Join(home, ".jdks", "*", "bin", exe))
		// SDKMAN, the other common way to have a JDK that PATH may not know of.
		s.at("SDKMAN", filepath.Join(home, ".sdkman", "candidates", "java", "current", "bin", exe))
	}

	switch runtime.GOOS {
	case "darwin":
		if home != "" {
			s.glob("IDE-managed JDK", filepath.Join(home, "Library", "Java", "JavaVirtualMachines", "*", "Contents", "Home", "bin", exe))
			s.glob("JetBrains IDE runtime", filepath.Join(home, "Applications", "*.app", "Contents", "jbr", "Contents", "Home", "bin", exe))
		}
		s.glob("JetBrains IDE runtime", filepath.Join("/Applications", "*.app", "Contents", "jbr", "Contents", "Home", "bin", exe))
		s.glob("system JDK", filepath.Join("/Library", "Java", "JavaVirtualMachines", "*", "Contents", "Home", "bin", exe))
	case "windows":
		for _, local := range windowsAppRoots() {
			s.glob("JetBrains IDE runtime",
				filepath.Join(local, "Programs", "*", "jbr", "bin", exe),
				filepath.Join(local, "JetBrains", "Toolbox", "apps", "*", "jbr", "bin", exe),
				filepath.Join(local, "JetBrains", "Toolbox", "apps", "*", "*", "*", "jbr", "bin", exe),
			)
			s.glob("system JDK", filepath.Join(local, "Java", "*", "bin", exe))
			s.glob("JetBrains IDE runtime", filepath.Join(local, "JetBrains", "*", "jbr", "bin", exe))
		}
	default: // linux and the other unixes
		if home != "" {
			s.glob("JetBrains IDE runtime",
				filepath.Join(home, ".local", "share", "JetBrains", "Toolbox", "apps", "*", "jbr", "bin", exe),
				filepath.Join(home, ".local", "share", "JetBrains", "Toolbox", "apps", "*", "*", "*", "jbr", "bin", exe),
			)
		}
		s.glob("JetBrains IDE runtime",
			filepath.Join("/opt", "*", "jbr", "bin", exe),
			filepath.Join("/snap", "intellij-idea-*", "current", "jbr", "bin", exe),
		)
		s.glob("system JDK",
			filepath.Join("/usr", "lib", "jvm", "*", "bin", exe),
			filepath.Join("/usr", "lib", "jvm", "*", "jre", "bin", exe),
		)
	}

	return s.results()
}

// mavenCandidates lists every Maven the machine plausibly has.
//
// Same reasoning as javaCandidates, with one addition that comes first: the
// project's own Maven Wrapper. If analyzer/ ever grows an mvnw it is by
// definition the Maven this build was meant to use, and it needs no Maven
// installed at all — only a JDK. Checking costs one stat, and means adding the
// wrapper later requires no change here.
func mavenCandidates(dir string) []candidate {
	exe := programName("mvn", scriptProgram)
	home := userHome()
	var s search

	if dir != "" {
		wrapper := "mvnw"
		if runtime.GOOS == "windows" {
			wrapper = "mvnw.cmd"
		}
		s.at("Maven Wrapper", filepath.Join(dir, wrapper))
	}
	s.envHome(exe, "MAVEN_HOME", "M2_HOME")
	s.onPath(exe)

	if home != "" {
		s.at("SDKMAN", filepath.Join(home, ".sdkman", "candidates", "maven", "current", "bin", exe))
	}

	// IntelliJ bundles a full Maven under its maven plugin. This is the entry
	// that makes "I only have IntelliJ" a working setup rather than a blocked
	// one.
	const bundled = "plugins/maven/lib/maven3/bin"
	switch runtime.GOOS {
	case "darwin":
		if home != "" {
			s.glob("IDE-bundled Maven", filepath.Join(home, "Applications", "*.app", "Contents", bundled, exe))
		}
		s.glob("IDE-bundled Maven", filepath.Join("/Applications", "*.app", "Contents", bundled, exe))
		s.at("Homebrew", filepath.Join("/opt", "homebrew", "bin", exe), filepath.Join("/usr", "local", "bin", exe))
	case "windows":
		for _, local := range windowsAppRoots() {
			s.glob("IDE-bundled Maven",
				filepath.Join(local, "Programs", "*", bundled, exe),
				filepath.Join(local, "JetBrains", "Toolbox", "apps", "*", bundled, exe),
				filepath.Join(local, "JetBrains", "Toolbox", "apps", "*", "*", "*", bundled, exe),
				filepath.Join(local, "JetBrains", "*", bundled, exe),
			)
		}
	default:
		if home != "" {
			s.glob("IDE-bundled Maven",
				filepath.Join(home, ".local", "share", "JetBrains", "Toolbox", "apps", "*", bundled, exe),
				filepath.Join(home, ".local", "share", "JetBrains", "Toolbox", "apps", "*", "*", "*", bundled, exe),
			)
		}
		s.glob("IDE-bundled Maven",
			filepath.Join("/opt", "*", bundled, exe),
			filepath.Join("/snap", "intellij-idea-*", "current", bundled, exe),
			filepath.Join("/usr", "share", "*", bundled, exe),
		)
	}

	return s.results()
}
