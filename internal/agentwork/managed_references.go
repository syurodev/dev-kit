package agentwork

import (
	"context"
	"errors"
	"sort"
	"strings"
)

// A package reference names a record the worker may be told to read: an
// identifier and the immutable revision or hash the host saw when it selected
// it. It carries no body, path or URL. The host both selects references from
// trusted state and resolves them again when asked, so a reference is never a
// claim the agent or a caller made about itself.
// ErrManagedRecordNotFound is what a ledger lookup returns for an unknown id,
// so a host that does not import storage can tell absent from unreadable.
var ErrManagedRecordNotFound = errors.New("managed ledger record not found")

var (
	ErrManagedReferenceMissing     = errors.New("managed package reference no longer exists")
	ErrManagedReferenceForeign     = errors.New("managed package reference is outside the work item's scope")
	ErrManagedReferenceStale       = errors.New("managed package reference revision changed")
	ErrManagedReferenceUnsupported = errors.New("managed package reference kind cannot be resolved")
)

// Reference kinds. "document" stays syntactically valid for later hosts, but no
// host resolves one yet, so one fails closed as unsupported.
const (
	ManagedReferenceTask     = "task"
	ManagedReferenceDocument = "document"
	ManagedReferenceArtifact = "managed_artifact"
)

// Reference states a check reports, one per stored reference.
const (
	ReferenceCurrent     = "current"
	ReferenceStale       = "stale"
	ReferenceMissing     = "missing"
	ReferenceForeign     = "foreign"
	ReferenceUnsupported = "unsupported"
)

// ReferenceStatus is one stored reference compared with the host's current
// state. It carries identifiers and a state only.
type ReferenceStatus struct {
	Kind, RecordID, State string
}

// canonicalReferences returns a sorted copy and rejects a repeated reference,
// so the same inputs always seal to the same body and a reference cannot be
// listed twice with different revisions.
func canonicalReferences(refs []PackageReference) ([]PackageReference, error) {
	out := append([]PackageReference(nil), refs...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].RecordID < out[j].RecordID
	})
	for i := 1; i < len(out); i++ {
		if out[i].Kind == out[i-1].Kind && out[i].RecordID == out[i-1].RecordID {
			return nil, ErrManagedPayloadScope
		}
	}
	return out, nil
}

// resolveReferences asks the host for each reference's current form and
// requires it to equal what was selected.
func (s *ManagedPayloadService) resolveReferences(ctx context.Context, scope PackageScope, refs []PackageReference) error {
	for _, ref := range refs {
		current, err := s.host.ResolvePackageReference(ctx, scope, ref)
		if err != nil {
			return err
		}
		if current != ref {
			return ErrManagedReferenceStale
		}
	}
	return nil
}

// CheckPackageReferences compares a stored package's references with current
// host state without changing anything. A dispatcher uses it to decide that an
// unsent package must be rebuilt: the stored package stays immutable history.
// A host or vault failure is returned as an error, never as a state.
func (s *ManagedPayloadService) CheckPackageReferences(ctx context.Context, packageID string) ([]ReferenceStatus, error) {
	body, err := s.ReadContextPackage(ctx, packageID)
	if err != nil {
		return nil, err
	}
	scope := PackageScope{WorkspaceID: body.WorkspaceID, BoardID: body.BoardID, ParentTaskID: body.ParentTaskID, GroupID: body.GroupID, WorkItemID: body.WorkItemID, TaskID: body.TaskID, AttemptID: body.AttemptID, Generation: body.Generation, RequirementsRevision: body.RequirementsRevision}
	out := make([]ReferenceStatus, 0, len(body.References))
	for _, ref := range body.References {
		state := ReferenceCurrent
		current, err := s.host.ResolvePackageReference(ctx, scope, ref)
		switch {
		case err == nil && current != ref:
			state = ReferenceStale
		case err == nil:
		case errors.Is(err, ErrManagedReferenceStale):
			state = ReferenceStale
		case errors.Is(err, ErrManagedReferenceMissing):
			state = ReferenceMissing
		case errors.Is(err, ErrManagedReferenceForeign):
			state = ReferenceForeign
		case errors.Is(err, ErrManagedReferenceUnsupported):
			state = ReferenceUnsupported
		default:
			return nil, err
		}
		out = append(out, ReferenceStatus{Kind: ref.Kind, RecordID: ref.RecordID, State: state})
	}
	return out, nil
}

func validReferenceDigest(ref PackageReference) bool {
	if ref.Kind == ManagedReferenceArtifact {
		return strings.HasPrefix(ref.Digest, "sha256:") && len(ref.Digest) > len("sha256:")
	}
	return true
}
