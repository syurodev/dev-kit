//go:build linux

package sandbox

import (
	"errors"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/unix"
)

// ErrLandlockSkipped indicates Landlock could not be applied (unsupported kernel or
// LSM not enabled). Callers such as bwrap integration should treat this as best-effort
// skip, not a hard failure.
var ErrLandlockSkipped = errors.New("landlock skipped")

const (
	landlockFSAccessRO = unix.LANDLOCK_ACCESS_FS_EXECUTE |
		unix.LANDLOCK_ACCESS_FS_READ_FILE |
		unix.LANDLOCK_ACCESS_FS_READ_DIR

	landlockFSAccessRW = landlockFSAccessRO |
		unix.LANDLOCK_ACCESS_FS_WRITE_FILE |
		unix.LANDLOCK_ACCESS_FS_REMOVE_DIR |
		unix.LANDLOCK_ACCESS_FS_REMOVE_FILE |
		unix.LANDLOCK_ACCESS_FS_MAKE_SYM |
		unix.LANDLOCK_ACCESS_FS_MAKE_DIR |
		unix.LANDLOCK_ACCESS_FS_MAKE_REG |
		unix.LANDLOCK_ACCESS_FS_MAKE_SOCK |
		unix.LANDLOCK_ACCESS_FS_MAKE_FIFO |
		unix.LANDLOCK_ACCESS_FS_MAKE_CHAR |
		unix.LANDLOCK_ACCESS_FS_MAKE_BLOCK |
		unix.LANDLOCK_ACCESS_FS_TRUNCATE
)

// LandlockAvailable reports whether the kernel exposes Landlock syscalls.
func LandlockAvailable() bool {
	abi, err := landlockProbeABI()
	if err != nil {
		return false
	}
	return abi > 0
}

// RestrictSelf applies Landlock path-beneath rules for policy paths to the current
// thread. WorkDir and ReadWritePaths receive read-write access; ReadOnlyPaths receive
// read-only access. Returns ErrLandlockSkipped when the kernel or LSM does not support
// Landlock; other errors indicate a failed apply.
func RestrictSelf(p Policy) error {
	if err := p.Validate(); err != nil {
		return err
	}

	attr := unix.LandlockRulesetAttr{
		Access_fs: landlockFSAccessRW,
	}
	fd, err := landlockCreateRuleset(&attr, unsafe.Sizeof(attr), 0)
	if err != nil {
		if landlockSkippedErr(err) {
			return ErrLandlockSkipped
		}
		return err
	}
	defer unix.Close(fd)

	paths := landlockPolicyPaths(p)
	for _, entry := range paths {
		if err := landlockAddPathBeneath(fd, entry.path, entry.access); err != nil {
			return err
		}
	}

	if err := landlockRestrictSelf(fd, 0); err != nil {
		if landlockSkippedErr(err) {
			return ErrLandlockSkipped
		}
		return err
	}
	return nil
}

type landlockPathEntry struct {
	path   string
	access uint64
}

func landlockPolicyPaths(p Policy) []landlockPathEntry {
	seen := make(map[string]struct{})
	var out []landlockPathEntry

	add := func(path string, access uint64) {
		path = filepath.Clean(path)
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		out = append(out, landlockPathEntry{path: path, access: access})
	}

	add(p.WorkDir, landlockFSAccessRW)
	for _, path := range p.ReadWritePaths {
		add(path, landlockFSAccessRW)
	}
	for _, path := range p.ReadOnlyPaths {
		add(path, landlockFSAccessRO)
	}
	return out
}

func landlockProbeABI() (int, error) {
	abi, _, errno := unix.RawSyscall(
		unix.SYS_LANDLOCK_CREATE_RULESET,
		0,
		0,
		uintptr(unix.LANDLOCK_CREATE_RULESET_VERSION),
	)
	if errno != 0 {
		return 0, errno
	}
	return int(abi), nil
}

func landlockCreateRuleset(attr *unix.LandlockRulesetAttr, size uintptr, flags uint32) (int, error) {
	var attrPtr unsafe.Pointer
	if attr != nil {
		attrPtr = unsafe.Pointer(attr)
	}
	fd, _, errno := unix.Syscall6(
		unix.SYS_LANDLOCK_CREATE_RULESET,
		uintptr(attrPtr),
		size,
		uintptr(flags),
		0,
		0,
		0,
	)
	if errno != 0 {
		return -1, errno
	}
	return int(fd), nil
}

func landlockAddPathBeneath(rulesetFD int, path string, access uint64) error {
	parentFD, err := unix.Open(path, unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(parentFD)

	rule := unix.LandlockPathBeneathAttr{
		Allowed_access: access,
		Parent_fd:      int32(parentFD),
	}
	_, _, errno := unix.Syscall6(
		unix.SYS_LANDLOCK_ADD_RULE,
		uintptr(rulesetFD),
		uintptr(unix.LANDLOCK_RULE_PATH_BENEATH),
		uintptr(unsafe.Pointer(&rule)),
		0,
		0,
		0,
	)
	if errno != 0 {
		return errno
	}
	return nil
}

func landlockRestrictSelf(rulesetFD int, flags uint32) error {
	_, _, errno := unix.Syscall(
		unix.SYS_LANDLOCK_RESTRICT_SELF,
		uintptr(rulesetFD),
		uintptr(flags),
		0,
	)
	if errno != 0 {
		return errno
	}
	return nil
}

func landlockSkippedErr(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, unix.ENOSYS) ||
		errors.Is(err, unix.ENOPKG) ||
		errors.Is(err, unix.EOPNOTSUPP) ||
		errors.Is(err, unix.EPERM)
}
