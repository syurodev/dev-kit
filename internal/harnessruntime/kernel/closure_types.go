package kernel

import "sort"

// ClosureTargetKind identifies one persisted control fact consumed by an
// explicit user closure preview. Targets contain references and identifiers,
// never protected payload bytes.
type ClosureTargetKind uint8

const (
	ClosureTargetUnknownTool ClosureTargetKind = iota + 1
	ClosureTargetUnknownModel
	ClosureTargetUnavailableCause
	ClosureTargetPlannedTool
	ClosureTargetPlannedModel
)

const CurrentClosurePreviewVersion uint32 = 1

// ClosureTargetFact binds a preview target to the exact persisted attempt and
// interruption. TargetID is a tool ID or model invocation ID, except for an
// unavailable cause, where it is the interruption target ID.
type ClosureTargetFact struct {
	Kind          ClosureTargetKind
	TargetID      ID
	AttemptID     ID
	CauseRef      ID
	CauseEventRef ID
	ResultRef     ID
	// OwnerEvidenceRef is a trusted owning-service receipt proving invalidation
	// of the exact check/request generation before cancelling prepared work.
	OwnerEvidenceRef ID
	RequestRef       ID
	Generation       uint64
}

// ClosureInput is trusted-owner evidence that a user reviewed a current
// preview. It is only control evidence: the kernel cannot open ResultRef or
// verify vault contents itself.
type ClosureInput struct {
	PreviewRef              ID
	ReceiptRef              ID
	PreviewVersion          uint32
	Revision                uint64
	Actor                   ActorKind
	CauseEventRef           ID
	UnavailableReason       UnavailableReason
	PreviousContextRevision uint64
	ContextRevision         uint64
	ContextRequirement      ContextRequirement
	Targets                 []ClosureTargetFact
}

func canonicalClosure(in ClosureInput) ClosureInput {
	out := in
	if in.Targets != nil {
		out.Targets = append(make([]ClosureTargetFact, 0, len(in.Targets)), in.Targets...)
		sort.Slice(out.Targets, func(i, j int) bool {
			a, b := out.Targets[i], out.Targets[j]
			if a.Kind != b.Kind {
				return a.Kind < b.Kind
			}
			if a.TargetID != b.TargetID {
				return a.TargetID < b.TargetID
			}
			if a.AttemptID != b.AttemptID {
				return a.AttemptID < b.AttemptID
			}
			return a.CauseRef < b.CauseRef
		})
	}
	return out
}

func closureInputSet(in ClosureInput) bool {
	return in.PreviewRef != "" || in.ReceiptRef != "" || in.PreviewVersion != 0 || in.Revision != 0 || in.Actor != 0 || in.CauseEventRef != "" || in.UnavailableReason != 0 || in.PreviousContextRevision != 0 || in.ContextRevision != 0 || in.ContextRequirement != 0 || in.Targets != nil
}
