// Package syncconflict owns the application logic for resolving sync conflicts:
// reading the captured "theirs" side, decrypting ours/theirs for comparison,
// and applying a chosen resolution strategy. Keeping it out of the bridge lets
// the bridge stay a thin capability-gated DTO boundary that never touches the
// storage or crypto layers directly.
package syncconflict

import (
	"context"
	"errors"
	"time"

	replicationsync "devkit/internal/sync"
	"devkit/internal/syncstore"
	"devkit/internal/vaultitem"
)

const (
	// noteBodyKeyID mirrors notes.bodyKeyID. Edit-merge is notes-only, so the
	// merged body is re-sealed under this inner key id.
	noteBodyKeyID = "note-body"
	// syncSnapshotKeyID mirrors vaultitem's localSnapshotKeyID: the outer key id
	// a materialised push uses, which a resolution mutation must reuse.
	syncSnapshotKeyID = "vault-item"
	itemTypeNote      = "note"
)

var (
	ErrUnavailable  = errors.New("conflict resolution is unavailable")
	ErrNotLinked    = errors.New("sync account is not linked")
	ErrBadStrategy  = errors.New("unknown resolution strategy")
	ErrNotMergeable = errors.New("merge is only supported for notes")
)

// ConflictReader reads the captured remote ("theirs") side of a conflict.
type ConflictReader interface {
	Get(ctx context.Context, accountID, recordID string) (syncstore.ConflictRecord, bool, error)
}

// ConflictResolver applies a resolution strategy atomically.
type ConflictResolver interface {
	ResolveUseTheirs(ctx context.Context, accountID, recordID string) error
	ResolveUseLocal(ctx context.Context, accountID, recordID string, res syncstore.LocalResolution) error
}

// AccountReader supplies the bound account for offline resolution.
type AccountReader interface {
	BoundAccountID(ctx context.Context) (string, bool, error)
}

// ItemStore reads and re-seals the local ("ours") record.
type ItemStore interface {
	Get(id string) (vaultitem.Record, error)
	OpenPayload(record vaultitem.Record, keyID string) ([]byte, error)
	Reseal(record vaultitem.Record, innerKeyID string, plaintext []byte) (vaultitem.Record, error)
}

// Deps are the collaborators the service needs. All are required.
type Deps struct {
	Conflicts ConflictReader
	Resolver  ConflictResolver
	Accounts  AccountReader
	Items     ItemStore
}

type Service struct {
	deps Deps
}

func New(deps Deps) *Service {
	return &Service{deps: deps}
}

// Detail is non-secret conflict metadata plus, for notes, the on-demand
// decrypted ours/theirs body text for a compare view. Secret item types return
// no text; the caller offers only Use ours / Use theirs.
type Detail struct {
	RecordID       string
	RecordType     string
	TheirsCaptured bool
	Mergeable      bool
	OursText       string
	TheirsText     string
}

func (s *Service) ready() bool {
	return s != nil && s.deps.Conflicts != nil && s.deps.Resolver != nil &&
		s.deps.Accounts != nil && s.deps.Items != nil
}

func (s *Service) Detail(ctx context.Context, recordID string) (Detail, error) {
	if !s.ready() {
		return Detail{}, ErrUnavailable
	}
	account, err := s.boundAccount(ctx)
	if err != nil {
		return Detail{}, err
	}
	ours, err := s.deps.Items.Get(recordID)
	if err != nil {
		return Detail{}, err
	}
	detail := Detail{RecordID: recordID, RecordType: ours.Type}
	conflict, found, err := s.deps.Conflicts.Get(ctx, account, recordID)
	if err != nil {
		return Detail{}, err
	}
	detail.TheirsCaptured = found
	if ours.Type == itemTypeNote {
		detail.Mergeable = true
		oursBody, err := s.deps.Items.OpenPayload(ours, noteBodyKeyID)
		if err != nil {
			return Detail{}, err
		}
		detail.OursText = string(oursBody)
		if found {
			theirsRecord, err := vaultitem.DecodeRecord(conflict.TheirsSnapshot)
			if err != nil {
				return Detail{}, err
			}
			theirsBody, err := s.deps.Items.OpenPayload(theirsRecord, noteBodyKeyID)
			if err != nil {
				return Detail{}, err
			}
			detail.TheirsText = string(theirsBody)
		}
	}
	return detail, nil
}

func (s *Service) Resolve(ctx context.Context, recordID, strategy, mergedText string) error {
	if !s.ready() {
		return ErrUnavailable
	}
	account, err := s.boundAccount(ctx)
	if err != nil {
		return err
	}
	switch strategy {
	case "theirs":
		return s.deps.Resolver.ResolveUseTheirs(ctx, account, recordID)
	case "ours":
		ours, err := s.deps.Items.Get(recordID)
		if err != nil {
			return err
		}
		return s.resolveUseLocal(ctx, account, recordID, ours)
	case "merge":
		ours, err := s.deps.Items.Get(recordID)
		if err != nil {
			return err
		}
		if ours.Type != itemTypeNote {
			return ErrNotMergeable
		}
		merged, err := s.deps.Items.Reseal(ours, noteBodyKeyID, []byte(mergedText))
		if err != nil {
			return err
		}
		return s.resolveUseLocal(ctx, account, recordID, merged)
	default:
		return ErrBadStrategy
	}
}

func (s *Service) boundAccount(ctx context.Context) (string, error) {
	account, bound, err := s.deps.Accounts.BoundAccountID(ctx)
	if err != nil {
		return "", err
	}
	if !bound {
		return "", ErrNotLinked
	}
	return account, nil
}

func (s *Service) resolveUseLocal(ctx context.Context, account, recordID string, record vaultitem.Record) error {
	recordJSON, snapshot, err := vaultitem.MarshalForResolution(record)
	if err != nil {
		return err
	}
	operationID, err := replicationsync.NewOperationID()
	if err != nil {
		return err
	}
	return s.deps.Resolver.ResolveUseLocal(ctx, account, recordID, syncstore.LocalResolution{
		OperationID: operationID,
		Operation:   replicationsync.OperationUpdate,
		KeyVersion:  uint32(record.KeyVersion),
		KeyID:       syncSnapshotKeyID,
		RecordJSON:  recordJSON,
		Snapshot:    snapshot,
		CreatedAt:   time.Now().UTC(),
	})
}
