package harness

import (
	"errors"
	"fmt"
)

// The types below are the persistence contract between the harness service and
// whatever stores W and S. They carry no I/O; internal/storage implements them.

// Stamp is the stored state of one W or S record. Revision counts every write,
// a delete included (the row stays as a tombstone), so a record that is deleted
// and created again never repeats an earlier token. Revision 0 means the record
// never existed. A reader that saw "absent at revision 5" and a writer that
// later finds "present at revision 7" have different tokens.
//
// H1's nullable revisions map as: Present → Profile.Revision/Override.Revision
// is Stamp.Revision; not Present → the input is nil (never revision 0).
type Stamp struct {
	Revision int64
	Present  bool
}

// ProfileWrite replaces W (Value) or deletes it (Value nil) if its stamp still
// equals Expect.
type ProfileWrite struct {
	Expect Stamp
	Value  *Profile
}

// OverrideWrite replaces S (Value) or deletes it (Value nil) if its stamp still
// equals Expect.
type OverrideWrite struct {
	Expect Stamp
	Value  *Override
}

// StoreWrite is one atomic change. Profile and Override are each optional; when
// both are set the store commits both or neither. WorkspaceID scopes W and the
// session's workspace; SessionID scopes S.
type StoreWrite struct {
	WorkspaceID string
	SessionID   string
	Profile     *ProfileWrite
	Override    *OverrideWrite
}

// Applied is the stamp of each record the write touched; the other is zero.
type Applied struct {
	Profile  Stamp
	Override Stamp
}

var (
	// ErrStoreConflict is a lost compare-and-swap; see ConflictError.
	ErrStoreConflict = errors.New("harness record changed")
	// ErrStoreScope means the workspace or session does not exist or the
	// session does not belong to the workspace. Nothing is transferred silently.
	ErrStoreScope = errors.New("harness scope not found")
	// ErrStoreCommitUnknown means the final commit failed after every check
	// passed, so the write may or may not be durable. Every other store error is a
	// proven rollback. A caller reads the stamps back before it decides.
	ErrStoreCommitUnknown = errors.New("harness commit outcome unknown")
	// ErrStoreCorrupt is a stored record this build cannot read (unknown schema,
	// invalid JSON). It is never read as "no profile": that would turn every
	// tool group back on.
	ErrStoreCorrupt = errors.New("stored harness record is unreadable")
)

// ConflictError names which record moved: W is harness.profileConflict, S is a
// stale proposal.
type ConflictError struct{ Target string } // "profile" or "override"

func (e *ConflictError) Error() string        { return fmt.Sprintf("harness %s changed", e.Target) }
func (e *ConflictError) Is(target error) bool { return target == ErrStoreConflict }
