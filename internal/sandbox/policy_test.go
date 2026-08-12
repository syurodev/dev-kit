package sandbox_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"devkit/internal/sandbox"
)

func TestPolicyRequiresAbsoluteWorkDir(t *testing.T) {
	err := sandbox.Policy{WorkDir: "rel"}.Validate()
	if !errors.Is(err, sandbox.ErrInvalidPolicy) {
		t.Fatalf("got %v", err)
	}
}

func TestPolicyRejectsEmptyCommand(t *testing.T) {
	err := sandbox.Policy{WorkDir: t.TempDir()}.ValidateCmd(sandbox.Cmd{Path: ""})
	if !errors.Is(err, sandbox.ErrInvalidPolicy) {
		t.Fatalf("got %v", err)
	}
}

func TestPolicyRejectsUnknownNetwork(t *testing.T) {
	err := sandbox.Policy{
		WorkDir: t.TempDir(),
		Network: sandbox.NetworkMode("mystery"),
	}.Validate()
	if !errors.Is(err, sandbox.ErrInvalidPolicy) {
		t.Fatalf("got %v, want ErrInvalidPolicy", err)
	}
}

func TestDefaultSensitivePathsNormalized(t *testing.T) {
	p := sandbox.Policy{WorkDir: t.TempDir()}
	paths := p.EffectiveDenyRead() // includes ~/.ssh etc expanded
	if len(paths) == 0 {
		t.Fatal("expected default denies")
	}
}

func TestEffectiveDenyReadIncludesHostHomeWhenPolicyHomeDiffers(t *testing.T) {
	hostHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	fakeHome := filepath.Join(t.TempDir(), "fake-home")

	paths := sandbox.Policy{
		WorkDir: t.TempDir(),
		Env:     map[string]string{"HOME": fakeHome},
	}.EffectiveDenyRead()

	wantHostSSH := filepath.Join(hostHome, ".ssh")
	if !containsPath(paths, wantHostSSH) {
		t.Fatalf("EffectiveDenyRead missing host HOME deny %q in %v", wantHostSSH, paths)
	}
	wantPolicySSH := filepath.Join(fakeHome, ".ssh")
	if !containsPath(paths, wantPolicySSH) {
		t.Fatalf("EffectiveDenyRead missing policy HOME deny %q in %v", wantPolicySSH, paths)
	}
}

func TestPolicyRejectsReadOnlySymlinkToDeniedPath(t *testing.T) {
	root := t.TempDir()
	workDir := filepath.Join(root, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	secretDir := filepath.Join(root, "secret")
	if err := os.MkdirAll(secretDir, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(workDir, "secret-link")
	if err := os.Symlink(secretDir, link); err != nil {
		t.Fatal(err)
	}

	err := sandbox.Policy{
		WorkDir:       workDir,
		ReadOnlyPaths: []string{link},
		DenyReadPaths: []string{secretDir},
	}.Validate()
	if !errors.Is(err, sandbox.ErrInvalidPolicy) {
		t.Fatalf("got %v, want ErrInvalidPolicy", err)
	}
}

func containsPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}
