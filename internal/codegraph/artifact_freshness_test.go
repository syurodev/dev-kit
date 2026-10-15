package codegraph

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestJavaLocalJarMustBeNewerThanProductionSource(t *testing.T) {
	root := t.TempDir()
	jar := filepath.Join(root, "analyzer", "target", javaAnalyzerJar)
	source := filepath.Join(root, "analyzer", "src", "main", "java", "Main.java")
	for _, path := range []string{jar, source} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("content"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(jar, old, old); err != nil {
		t.Fatal(err)
	}
	tc := JavaToolchain(root)
	tc.Search = []string{jar}
	if !tc.localArtifactStale(jar) || tc.installedArtifact() != "" {
		t.Fatal("accepted stale jar")
	}
	fresh := time.Now().Add(time.Hour)
	if err := os.Chtimes(jar, fresh, fresh); err != nil {
		t.Fatal(err)
	}
	if tc.localArtifactStale(jar) || tc.installedArtifact() != jar {
		t.Fatal("rejected rebuilt jar")
	}
	// Bundled and explicit external artifacts do not depend on checkout timestamps.
	bundled := filepath.Join(root, "bundled.jar")
	if err := os.WriteFile(bundled, []byte("bundled"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(jar, old, old); err != nil {
		t.Fatal(err)
	}
	tc.Search = []string{bundled, jar}
	if tc.installedArtifact() != bundled {
		t.Fatal("source freshness rejected bundled artifact")
	}
}
