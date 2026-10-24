package agenthub

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	if err := prepareSessionTemp(&spec, false); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(spec.SessionTempDir)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("private temp: %v %v", info, err)
	}
	if filepath.Dir(spec.SessionTempDir) != "/private/tmp" || len(filepath.Base(spec.SessionTempDir)) != 11 {
		t.Fatal(spec.SessionTempDir)
	}
	if err := prepareSessionTemp(&spec, false); err != nil {
		t.Fatal("resume", err)
	}
	if err := prepareSessionTemp(&spec, true); err == nil {
		t.Fatal("new session must not reuse a lease")
	}
	collision := OpenSpec{SessionID: id[:8] + uuid.NewString()[8:]}
	if err := prepareSessionTemp(&collision, false); err == nil {
		t.Fatal("full UUID collision must not reuse scratch")
	}
	removeSessionTemp(id)
	if _, err := os.Stat(spec.SessionTempDir); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Mkdir(spec.SessionTempDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := prepareSessionTemp(&spec, true); err == nil {
		t.Fatal("brand-new session reused an existing directory")
	}
	if err := os.Remove(spec.SessionTempDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(spec.SessionTempDir) })
	if err := os.Symlink(t.TempDir(), spec.SessionTempDir); err != nil {
		t.Fatal(err)
	}
	if err := prepareSessionTemp(&spec, false); err == nil {
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

func TestSessionTempSweepProtectsLiveForeignAndUnknown(t *testing.T) {
	root := t.TempDir()
	owner := "database-hash"
	ids := map[string]string{}
	for i, name := range []string{"stale", "locked", "pid-live", "db-live", "foreign", "failed", "later"} {
		id := fmt.Sprintf("%08x-1111-4111-8111-111111111111", i+1)
		ids[name] = id
		dir := filepath.Join(root, "dk-"+id[:8])
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		marker := tempOwner{Session: id, Owner: owner, PID: 12345}
		if name == "foreign" {
			marker.Owner = "another-database"
		}
		if name == "pid-live" {
			marker.PID = os.Getpid()
		}
		data, _ := json.Marshal(marker)
		if err := os.WriteFile(tempMarker(dir), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// A concurrent instance holds the marker lock, even when its stored PID
	// cannot be trusted for liveness (PID reuse is handled conservatively).
	locked, err := openTempMarker(tempMarker(filepath.Join(root, "dk-"+ids["locked"][:8])), os.O_RDWR)
	if err != nil {
		t.Fatal(err)
	}
	defer locked.Close()
	if err := lockTempMarker(locked); err != nil {
		t.Fatal(err)
	}
	unknown := filepath.Join(root, "dk-ffffffff")
	if err := os.Mkdir(unknown, 0o700); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(root, "dk-eeeeeeee")); err != nil {
		t.Fatal(err)
	}
	var removed []string
	err = sweepSessionTemps(root, owner, map[string]bool{ids["db-live"]: true}, func(path string) error {
		if strings.Contains(path, ids["failed"][:8]) {
			return os.ErrPermission
		}
		removed = append(removed, filepath.Base(path))
		return os.RemoveAll(path)
	}, func(pid int) bool { return pid == os.Getpid() })
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("must report failed cleanup: %v", err)
	}
	for _, name := range []string{"stale", "later"} {
		if _, err := os.Stat(filepath.Join(root, "dk-"+ids[name][:8])); !os.IsNotExist(err) {
			t.Errorf("didn't sweep %s after error: %v", name, err)
		}
	}
	for _, name := range []string{"locked", "pid-live", "db-live", "foreign", "failed"} {
		if _, err := os.Stat(filepath.Join(root, "dk-"+ids[name][:8])); err != nil {
			t.Errorf("removed %s", name)
		}
	}
	if len(removed) != 2 {
		t.Fatal(removed)
	}
	if _, err := os.Stat(unknown); err != nil {
		t.Fatal("removed unmarked directory")
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal("followed symlink")
	}
}
