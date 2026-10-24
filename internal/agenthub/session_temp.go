package agenthub

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/google/uuid"
)

type tempOwner struct {
	Session string `json:"session"`
	Owner   string `json:"owner"`
	PID     int    `json:"pid"`
}
type tempLease struct {
	file  *os.File
	owner tempOwner
}

var sessionTemps = struct {
	sync.Mutex
	leases map[string]tempLease
}{leases: map[string]tempLease{}}

// Keep the approved short name; the full UUID marker and exclusive creation
// prevent two sessions with the same eight-digit prefix from sharing scratch.
func sessionTempPath(id string) string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	parsed, err := uuid.Parse(id)
	if err != nil {
		return ""
	}
	return "/private/tmp/dk-" + parsed.String()[:8]
}
func tempOwnerKey(owner string) string {
	digest := sha256.Sum256([]byte(resolveTempOwner(owner)))
	return hex.EncodeToString(digest[:])
}
func resolveTempOwner(owner string) string {
	if resolved, err := filepath.EvalSymlinks(owner); err == nil {
		return resolved
	}
	return filepath.Clean(owner)
}

// The marker is a sibling, so the dk-* file deny hides it from the jail;
// the own-directory exception does not let the session unlink its lock.
func tempMarker(dir string) string { return dir + ".owner" }

func prepareSessionTemp(spec *OpenSpec, fresh bool) error {
	dir := sessionTempPath(spec.SessionID)
	if dir == "" {
		return nil
	}
	sessionTemps.Lock()
	defer sessionTemps.Unlock()
	creating := fresh
	owner := tempOwner{Session: spec.SessionID, Owner: tempOwnerKey(spec.TempOwner), PID: os.Getpid()}
	if lease, ok := sessionTemps.leases[dir]; ok {
		if creating || lease.owner != owner {
			return fmt.Errorf("session temp: directory already leased")
		}
		spec.SessionTempDir = dir
		return nil
	}
	err := os.Mkdir(dir, 0o700)
	made := err == nil
	if err != nil && (creating || !os.IsExist(err)) {
		return fmt.Errorf("session temp: %w", err)
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 || !tempOwnedByUser(info) {
		return fmt.Errorf("session temp: expected owned private directory at %s", dir)
	}
	flags := os.O_RDWR
	if made {
		flags |= os.O_CREATE | os.O_EXCL
	}
	file, err := openTempMarker(tempMarker(dir), flags)
	if err != nil {
		if made {
			_ = os.Remove(dir)
		}
		return fmt.Errorf("session temp marker: %w", err)
	}
	success := false
	defer func() {
		if !success {
			file.Close()
			if made {
				_ = os.Remove(dir)
				_ = os.Remove(tempMarker(dir))
			}
		}
	}()
	markerInfo, err := file.Stat()
	if err != nil || !markerInfo.Mode().IsRegular() || !tempOwnedByUser(markerInfo) {
		return fmt.Errorf("session temp: invalid owner marker")
	}
	if err := lockTempMarker(file); err != nil {
		return fmt.Errorf("session temp: owned by another DevKit process: %w", err)
	}
	if !made {
		var previous tempOwner
		if err := json.NewDecoder(file).Decode(&previous); err != nil || previous.Session != owner.Session || previous.Owner != owner.Owner {
			return fmt.Errorf("session temp: owner mismatch")
		}
		if tempPIDAlive(previous.PID) {
			return fmt.Errorf("session temp: owner process is still live")
		}
	}
	data, err := json.Marshal(owner)
	if err != nil {
		return err
	}
	if err := file.Truncate(0); err != nil {
		return err
	}
	if _, err := file.WriteAt(data, 0); err != nil {
		return err
	}
	sessionTemps.leases[dir] = tempLease{file: file, owner: owner}
	success = true
	spec.SessionTempDir = dir
	return nil
}

func applySessionTempEnv(env map[string]string, spec OpenSpec) {
	if spec.SessionTempDir != "" {
		env["TMPDIR"] = spec.SessionTempDir
		env["GOTMPDIR"] = spec.SessionTempDir
	}
}
func removeSessionTemp(id string) {
	dir := sessionTempPath(id)
	if dir == "" {
		return
	}
	sessionTemps.Lock()
	defer sessionTemps.Unlock()
	lease, ok := sessionTemps.leases[dir]
	if !ok || lease.owner.Session != id {
		return
	}
	// Keep the lease until deletion finishes, so a sweep cannot race cleanup.
	if err := os.RemoveAll(dir); err == nil {
		_ = os.Remove(tempMarker(dir))
	}
	lease.file.Close()
	delete(sessionTemps.leases, dir)
}

// SweepSessionTemps preserves unknown, foreign, DB-live, locked, and live-PID
// directories. Call only from the desktop host, before starting sessions.
func SweepSessionTemps(owner string, live map[string]bool) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	return sweepSessionTemps("/private/tmp", tempOwnerKey(owner), live, os.RemoveAll, tempPIDAlive)
}
func sweepSessionTemps(root, owner string, live map[string]bool, remove func(string) error, alive func(int) bool) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	var failures []error
	for _, entry := range entries {
		name := entry.Name()
		if len(name) != 11 || !strings.HasPrefix(name, "dk-") || !entry.IsDir() {
			continue
		}
		if _, err := hex.DecodeString(name[3:]); err != nil {
			continue
		}
		dir := filepath.Join(root, name)
		info, err := os.Lstat(dir)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if !tempOwnedByUser(info) {
			continue
		}
		file, err := openTempMarker(tempMarker(dir), os.O_RDWR)
		if os.IsNotExist(err) {
			continue
		} // pre-marker versions/other applications
		if err != nil {
			failures = append(failures, err)
			continue
		}
		func() {
			defer file.Close()
			info, err := file.Stat()
			if err != nil || !info.Mode().IsRegular() || !tempOwnedByUser(info) {
				return
			}
			if err := lockTempMarker(file); err != nil {
				return
			}
			var marker tempOwner
			if err := json.NewDecoder(file).Decode(&marker); err != nil {
				return
			}
			parsed, err := uuid.Parse(marker.Session)
			if err != nil || "dk-"+parsed.String()[:8] != name || marker.Owner != owner || live[marker.Session] || alive(marker.PID) {
				return
			}
			if err := remove(dir); err != nil {
				failures = append(failures, err)
				return
			}
			if err := os.Remove(tempMarker(dir)); err != nil {
				failures = append(failures, err)
			}
		}()
	}
	return errors.Join(failures...)
}
