package agenthub

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/google/uuid"
)

func TestSessionTempEnvironmentWins(t *testing.T) {
	env := map[string]string{"TMPDIR": "/host/temp", "GOTMPDIR": "/host/go"}
	applySessionTempEnv(env, OpenSpec{SessionTempDir: "/private/tmp/dk-12345678"})
	if env["TMPDIR"] != "/private/tmp/dk-12345678" || env["GOTMPDIR"] != env["TMPDIR"] {
		t.Fatal(env)
	}
}

func TestSessionTempLifecycle(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("bwrap owns Linux temp isolation")
	}
	id := uuid.NewString()
	spec := OpenSpec{SessionID: id}
	t.Cleanup(func() { removeSessionTemp(id) })
	if err := prepareSessionTemp(&spec); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(spec.SessionTempDir)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("private temp: %v %v", info, err)
	}
	if filepath.Dir(spec.SessionTempDir) != "/private/tmp" || len(filepath.Base(spec.SessionTempDir)) != 11 {
		t.Fatal(spec.SessionTempDir)
	}
	if err := prepareSessionTemp(&spec); err != nil {
		t.Fatal("resume", err)
	}
	removeSessionTemp(id)
	if _, err := os.Stat(spec.SessionTempDir); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), spec.SessionTempDir); err != nil {
		t.Fatal(err)
	}
	if err := prepareSessionTemp(&spec); err == nil {
		t.Fatal("must refuse symlink scratch directory")
	}
}

func TestSessionTempReachesEveryVendorAfterOverrides(t *testing.T) {
	hostTemp, hostGoTemp := t.TempDir(), t.TempDir()
	t.Setenv("TMPDIR", hostTemp)
	t.Setenv("GOTMPDIR", hostGoTemp)
	shared := false
	spec := OpenSpec{PrivateHome: &shared, SessionTempDir: "/private/tmp/dk-12345678", ExtraEnv: map[string]string{"TMPDIR": "/override/tmp", "GOTMPDIR": "/override/go"}}
	claude, _ := sandboxFor(t.TempDir(), launch{}, spec)
	for vendor, env := range map[string]map[string]string{
		"claude":   claude.Env,
		"codex":    codexEnv(launch{}, spec, t.TempDir()),
		"opencode": openCodeEnv(launch{}, spec, openCodeRoots{}, "{}"),
	} {
		if env["TMPDIR"] != spec.SessionTempDir || env["GOTMPDIR"] != spec.SessionTempDir {
			t.Errorf("%s temp env was overwritten", vendor)
		}
	}
	if claude.SessionTempDir != spec.SessionTempDir {
		t.Fatal("session temp missing from policy")
	}
}

func TestSessionTempSweepOnlyRemovesOwnedShape(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"dk-1234abcd", "dk-1234abc", "dk-1234abcz", "other"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(root, "dk-abcdef12")); err != nil {
		t.Fatal(err)
	}
	if err := sweepSessionTemps(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "dk-1234abcd")); !os.IsNotExist(err) {
		t.Fatal("stale session directory remains", err)
	}
	for _, name := range []string{"dk-1234abc", "dk-1234abcz", "other", "dk-abcdef12"} {
		if _, err := os.Lstat(filepath.Join(root, name)); err != nil {
			t.Errorf("removed unrelated entry %s: %v", name, err)
		}
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal("followed symlink", err)
	}
}
