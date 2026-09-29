package codegraph

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// markerToolchain detects a language that does not exist, so the detection
// machinery is tested without any of it depending on Java.
func markerToolchain() Toolchain {
	return Toolchain{
		Language:  "fictional",
		Label:     "Fictional",
		Markers:   []string{"fict.build", "fict.build.*"},
		SourceExt: ".fict",
	}
}

func write(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDetectFindsEveryModuleInAMonorepo(t *testing.T) {
	// The case that motivated detection: a folder holding several services,
	// each with its own build file. Reporting one module would understate what
	// was opened, and reporting none would refuse a perfectly good project.
	root := t.TempDir()
	write(t, root, "services/orders/fict.build", "")
	write(t, root, "services/billing/fict.build", "")
	write(t, root, "libs/shared/fict.build.kts", "")

	detected, ok := markerToolchain().Detect(root)
	if !ok {
		t.Fatal("three build files is a project")
	}
	want := []string{
		filepath.FromSlash("libs/shared"),
		filepath.FromSlash("services/billing"),
		filepath.FromSlash("services/orders"),
	}
	if len(detected.Modules) != len(want) {
		t.Fatalf("expected %v, got %v", want, detected.Modules)
	}
	for i, module := range want {
		if detected.Modules[i] != module {
			t.Fatalf("expected %v, got %v", want, detected.Modules)
		}
	}
}

func TestDetectIgnoresDependencyAndBuildDirectories(t *testing.T) {
	// A build file inside target/ or node_modules is not a module of this
	// project; counting it would invent services that do not exist.
	root := t.TempDir()
	write(t, root, "fict.build", "")
	write(t, root, "target/generated/fict.build", "")
	write(t, root, "node_modules/somedep/fict.build", "")
	write(t, root, ".git/hooks/fict.build", "")

	detected, ok := markerToolchain().Detect(root)
	if !ok {
		t.Fatal("the root has a build file")
	}
	if len(detected.Modules) != 1 || detected.Modules[0] != "." {
		t.Fatalf("only the root is a module, got %v", detected.Modules)
	}
}

func TestDetectFallsBackToSourceWhenThereIsNoBuildFile(t *testing.T) {
	// A folder of source and no build system is still something to read.
	root := t.TempDir()
	write(t, root, "deep/nested/Thing.fict", "")

	detected, ok := markerToolchain().Detect(root)
	if !ok {
		t.Fatal("source with no build file is still a project")
	}
	if len(detected.Modules) != 1 || detected.Modules[0] != "." {
		t.Fatalf("the folder itself is the module, got %v", detected.Modules)
	}
}

func TestDetectRefusesAFolderWithNothingInIt(t *testing.T) {
	root := t.TempDir()
	write(t, root, "notes.txt", "hello")
	write(t, root, "photos/holiday.jpg", "")

	if _, ok := markerToolchain().Detect(root); ok {
		t.Fatal("a folder of text and images is not a project")
	}
}

type refusingAnalyzer struct{ t *testing.T }

func (a refusingAnalyzer) Analyze(context.Context, string) (AnalysisResult, error) {
	a.t.Error("the analyzer ran on a folder no toolchain recognised")
	return AnalysisResult{}, nil
}

func TestDetectProjectRefusesWhenNoToolchainRecognisesTheFolder(t *testing.T) {
	service := NewService(nilStore{}, NewSubprocessAnalyzer())
	service.SetToolchains([]Toolchain{markerToolchain()})

	root := t.TempDir()
	write(t, root, "README.md", "not code")

	if _, err := service.DetectProject(root); !errors.Is(err, ErrUnsupportedProject) {
		t.Fatalf("expected ErrUnsupportedProject, got %v", err)
	}
}

func TestOpenProjectBrowsesAFolderNoToolchainRecognises(t *testing.T) {
	// A Terraform or Helm repository is somebody's real project. It opens for
	// its files — and still never starts an analyzer, which was the point of
	// refusing it before: no subprocess walks a folder that is not code.
	service := NewService(nilStore{}, refusingAnalyzer{t})
	service.SetToolchains([]Toolchain{markerToolchain()})

	root := t.TempDir()
	write(t, root, "main.tf", `resource "null_resource" "x" {}`)
	write(t, root, "charts/api/values.yaml", "replicas: 2")

	ctx := context.Background()
	if err := service.OpenProject(ctx, root); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	if got := service.ProjectRoot(); got != root {
		t.Fatalf("ProjectRoot = %q, want %q", got, root)
	}

	files, err := service.ListFiles(ctx)
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	var paths []string
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	if !slices.Contains(paths, "main.tf") || !slices.Contains(paths, "charts/api/values.yaml") {
		t.Fatalf("files = %v, want the folder's files", paths)
	}
	if text, err := service.ReadFile(ctx, "main.tf"); err != nil || !strings.Contains(text, "null_resource") {
		t.Fatalf("ReadFile = %q, %v", text, err)
	}

	info, err := service.IndexInfo(ctx)
	if err != nil {
		t.Fatalf("IndexInfo: %v", err)
	}
	if !info.Unsupported || info.Error != "" || info.Indexing {
		t.Fatalf("IndexInfo = %+v, want unsupported with no error and nothing running", info)
	}
	// A reason, not an empty graph an agent would read as "no endpoints here".
	if _, err := service.WaitForIndexReady(ctx); !errors.Is(err, ErrUnsupportedProject) {
		t.Fatalf("WaitForIndexReady = %v, want ErrUnsupportedProject", err)
	}
	if _, err := service.ListEndpoints(ctx); !errors.Is(err, ErrUnsupportedProject) {
		t.Fatalf("ListEndpoints = %v, want ErrUnsupportedProject", err)
	}
}

func TestDetectProjectAcceptsAFolderAToolchainRecognises(t *testing.T) {
	service := NewService(nilStore{}, NewSubprocessAnalyzer())
	service.SetToolchains([]Toolchain{markerToolchain()})

	root := t.TempDir()
	write(t, root, "svc/fict.build", "")

	detected, err := service.DetectProject(root)
	if err != nil {
		t.Fatalf("DetectProject: %v", err)
	}
	if detected.Language != "fictional" || len(detected.Modules) != 1 {
		t.Fatalf("unexpected detection: %+v", detected)
	}
}

func TestJavaToolchainRecognisesTheShapesJavaProjectsCome(t *testing.T) {
	toolchain := JavaToolchain(".")
	for name, layout := range map[string]string{
		"maven":         "pom.xml",
		"gradle groovy": "build.gradle",
		"gradle kotlin": "build.gradle.kts",
		"loose source":  "Main.java",
	} {
		root := t.TempDir()
		write(t, root, layout, "")
		if _, ok := toolchain.Detect(root); !ok {
			t.Errorf("%s layout should be recognised (%s)", name, layout)
		}
	}

	notJava := t.TempDir()
	write(t, notJava, "package.json", "{}")
	write(t, notJava, "src/index.ts", "")
	if _, ok := toolchain.Detect(notJava); ok {
		t.Error("a TypeScript project is not a Java project")
	}
}
