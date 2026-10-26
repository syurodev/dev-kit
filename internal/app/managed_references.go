package app

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"devkit/internal/agentwork"
	"devkit/internal/tasks"
)

// managedItemByTask finds the work item a group tracks for a task. The lookup
// is group-scoped on purpose: a task tracked by another group is not an input.
type managedItemByTask interface {
	GetWorkItemByTask(ctx context.Context, groupID, taskID string) (agentwork.WorkItem, error)
}

// selectDependencyReferences is the host's own choice of inputs for a package:
// the tasks this task is blocked by that the same group actively tracks, each
// bound to that task's current requirements digest. Nothing the agent, the
// coordinator's prose or a caller names is consulted, and no transcript,
// comment or memory body is read. A blocker the group does not track is an
// ordinary board ordering constraint, not an input, and is left out. A tracked
// dependency whose task has vanished fails the build instead of being dropped.
// Artifacts are never auto-selected: until results are reviewed they are only
// agent claims, so a future explicit selection owns them.
func (h *managedOwnershipHost) selectDependencyReferences(ctx context.Context, group agentwork.GroupState, item agentwork.WorkItem, requirements agentwork.RequirementsSnapshot) ([]agentwork.PackageReference, error) {
	var refs []agentwork.PackageReference
	for _, dependency := range managedDependencySet(requirements, item.TaskID) {
		tracked, err := h.store.GetWorkItemByTask(ctx, group.ID, dependency)
		if errors.Is(err, agentwork.ErrManagedRecordNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if tracked.EndedAt != nil || tracked.TombstoneReason != "" {
			continue
		}
		ref, err := h.resolveTaskReference(ctx, group, dependency)
		if err != nil {
			return nil, fmt.Errorf("dependency %s: %w", dependency, err)
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

func managedDependencySet(requirements agentwork.RequirementsSnapshot, own string) []string {
	set := slices.Clone(requirements.Dependencies)
	slices.Sort(set)
	set = slices.Compact(set)
	return slices.DeleteFunc(set, func(id string) bool { return id == "" || id == own })
}

func (h *managedOwnershipHost) resolveTaskReference(ctx context.Context, group agentwork.GroupState, taskID string) (agentwork.PackageReference, error) {
	if h.container == nil || h.container.Tasks == nil {
		return agentwork.PackageReference{}, agentwork.ErrManagedHostUnavailable
	}
	if _, err := h.container.Tasks.Get(managedTasksCaller(ctx), taskID); err != nil {
		if errors.Is(err, tasks.ErrNotFound) {
			return agentwork.PackageReference{}, agentwork.ErrManagedReferenceMissing
		}
		return agentwork.PackageReference{}, agentwork.ErrManagedReferenceForeign
	}
	requirements, err := h.requirementsOf(ctx, group, taskID)
	if err != nil {
		return agentwork.PackageReference{}, agentwork.ErrManagedReferenceForeign
	}
	digest, err := agentwork.RequirementsDigest(requirements)
	if err != nil {
		return agentwork.PackageReference{}, err
	}
	return agentwork.PackageReference{Kind: agentwork.ManagedReferenceTask, RecordID: taskID, Revision: digest, WorkspaceID: group.WorkspaceID, GroupID: group.ID}, nil
}

// ResolvePackageReference returns a reference's current canonical form, derived
// only from host state and the work item's own scope. The revision it was given
// is never trusted; the caller compares.
func (h *managedOwnershipHost) ResolvePackageReference(ctx context.Context, scope agentwork.PackageScope, ref agentwork.PackageReference) (agentwork.PackageReference, error) {
	group, item, _, err := h.packageScope(ctx, scope)
	if err != nil {
		return agentwork.PackageReference{}, err
	}
	if ref.WorkspaceID != group.WorkspaceID || ref.GroupID != "" && ref.GroupID != group.ID {
		return agentwork.PackageReference{}, agentwork.ErrManagedReferenceForeign
	}
	requirements, err := h.requirementsOf(ctx, group, item.TaskID)
	if err != nil {
		return agentwork.PackageReference{}, err
	}
	dependencies := managedDependencySet(requirements, item.TaskID)
	switch ref.Kind {
	case agentwork.ManagedReferenceTask:
		if !slices.Contains(dependencies, ref.RecordID) {
			return agentwork.PackageReference{}, agentwork.ErrManagedReferenceForeign
		}
		tracked, err := h.store.GetWorkItemByTask(ctx, group.ID, ref.RecordID)
		if errors.Is(err, agentwork.ErrManagedRecordNotFound) {
			return agentwork.PackageReference{}, agentwork.ErrManagedReferenceForeign
		}
		if err != nil {
			return agentwork.PackageReference{}, err
		}
		if tracked.EndedAt != nil || tracked.TombstoneReason != "" {
			return agentwork.PackageReference{}, agentwork.ErrManagedReferenceMissing
		}
		return h.resolveTaskReference(ctx, group, ref.RecordID)
	case agentwork.ManagedReferenceArtifact:
		artifact, err := h.store.GetArtifact(ctx, ref.RecordID)
		if errors.Is(err, agentwork.ErrManagedRecordNotFound) {
			return agentwork.PackageReference{}, agentwork.ErrManagedReferenceMissing
		}
		if err != nil {
			return agentwork.PackageReference{}, err
		}
		if artifact.WorkspaceID != group.WorkspaceID || artifact.GroupID != group.ID || artifact.BoardID != group.BoardID {
			return agentwork.PackageReference{}, agentwork.ErrManagedReferenceForeign
		}
		producer, err := h.store.GetWorkItem(ctx, artifact.WorkItemID)
		if err != nil {
			return agentwork.PackageReference{}, err
		}
		if producer.GroupID != group.ID || !slices.Contains(dependencies, producer.TaskID) {
			return agentwork.PackageReference{}, agentwork.ErrManagedReferenceForeign
		}
		return agentwork.PackageReference{
			Kind: agentwork.ManagedReferenceArtifact, RecordID: artifact.ID,
			Revision:    fmt.Sprintf("v%d.g%d", artifact.SchemaVersion, artifact.Generation),
			Digest:      artifact.BodyHash,
			WorkspaceID: group.WorkspaceID, GroupID: group.ID,
		}, nil
	default:
		return agentwork.PackageReference{}, agentwork.ErrManagedReferenceUnsupported
	}
}
